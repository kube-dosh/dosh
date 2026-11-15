import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Loader2, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { NodeDrainAction, NodeDrainResult, NodeSummary } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { cn } from "@/lib/utils";

const actionLabels: Record<NodeDrainAction, string> = {
  evict: "将驱逐",
  skip: "跳过",
  block: "需确认",
  blocked: "已阻止",
  failed: "失败",
  remaining: "未处理",
};

const actionClasses: Record<NodeDrainAction, string> = {
  evict: "border-sky-500/35 bg-sky-500/10 text-sky-700 dark:text-sky-300",
  skip: "border-border bg-muted/50 text-muted-foreground",
  block: "border-warning/35 bg-warning/10 text-warning",
  blocked: "border-destructive/35 bg-destructive/10 text-destructive",
  failed: "border-destructive/35 bg-destructive/10 text-destructive",
  remaining: "border-border bg-muted/50 text-muted-foreground",
};

export function NodeDrainDialog({ node, open, onOpenChange }: { node: NodeSummary; open: boolean; onOpenChange: (open: boolean) => void }) {
  const queryClient = useQueryClient();
  const [ignoreDaemonSets, setIgnoreDaemonSets] = useState(false);
  const [force, setForce] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [result, setResult] = useState<NodeDrainResult>();
  const plan = useQuery({
    queryKey: ["node-drain-plan", node.name, ignoreDaemonSets, force],
    queryFn: () => api.nodeDrainPlan(node.name, { ignoreDaemonSets, force }),
    enabled: open,
    retry: false,
  });
  const drain = useMutation({
    mutationFn: () => api.drainNode(node.name, { ignoreDaemonSets, force, timeoutSeconds: 90, confirmation }),
    onSuccess: async (value) => {
      setResult(value);
      if (value.blockedCount || value.failedCount) {
        toast.warning(`节点 ${node.name} 已停止调度，但仍有 Pod 未被驱逐`);
      } else {
        toast.success(`节点 ${node.name} 已完成排空`);
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
        queryClient.invalidateQueries({ queryKey: ["node", node.name] }),
        queryClient.invalidateQueries({ queryKey: ["pods"] }),
      ]);
    },
    onError: (error) => toast.error(error.message),
  });
  const close = (next: boolean) => {
    if (!next) {
      setConfirmation("");
      setResult(undefined);
      setIgnoreDaemonSets(false);
      setForce(false);
    }
    onOpenChange(next);
  };
  const canDrain = confirmation === node.name && !drain.isPending;
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto p-4 sm:p-6">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><ShieldAlert className="h-5 w-5 text-warning" />排空节点 {node.name}</DialogTitle>
          <DialogDescription>先停止新 Pod 调度，再通过 Kubernetes Eviction API 逐个驱逐 Pod。PodDisruptionBudget 阻止时将立即停止后续驱逐。</DialogDescription>
        </DialogHeader>
        {result ? <DrainResultView result={result} /> : (
          <div className="min-w-0 space-y-4">
            <div className="grid min-w-0 gap-2 rounded-lg border bg-muted/25 p-3 text-sm sm:grid-cols-2">
              <label className="flex min-w-0 items-start gap-2"><input type="checkbox" className="mt-1" checked={ignoreDaemonSets} onChange={(event) => setIgnoreDaemonSets(event.target.checked)} /><span>忽略 DaemonSet Pod<br /><span className="text-xs text-muted-foreground">常规节点维护通常需要此项；DaemonSet Pod 不会驱逐。</span></span></label>
              <label className="flex min-w-0 items-start gap-2"><input type="checkbox" className="mt-1" checked={force} onChange={(event) => setForce(event.target.checked)} /><span>允许驱逐无控制器 Pod<br /><span className="text-xs text-muted-foreground">默认阻止裸 Pod，避免无替代实例的业务中断。</span></span></label>
            </div>
            {plan.isLoading ? <LoadingState label="正在生成排空计划" className="min-h-40" /> : null}
            {plan.isError ? <ErrorState error={plan.error} onRetry={() => void plan.refetch()} /> : null}
            {plan.data ? (
              <>
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <Badge variant="secondary">将驱逐 {plan.data.evictCount}</Badge>
                  <Badge variant="outline">跳过/阻止 {plan.data.skippedCount}</Badge>
                  {plan.data.unschedulable ? <Badge variant="warning">节点已停止调度</Badge> : null}
                </div>
                {plan.data.warnings?.length ? <div role="alert" className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{plan.data.warnings.join("；")}</div> : null}
                {plan.data.pods.length ? <div className="min-w-0 space-y-2">{plan.data.pods.map((pod) => (
                  <div key={`${pod.namespace}/${pod.name}`} className="min-w-0 rounded-md border p-3">
                    <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0"><p className="break-all text-sm font-medium">{pod.name}</p><p className="mt-0.5 break-all text-xs text-muted-foreground">{pod.namespace} · {pod.ownerKind || "无控制器"}{pod.ownerName ? `/${pod.ownerName}` : ""}</p></div>
                      <span className={cn("shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-medium", actionClasses[pod.action])}>{actionLabels[pod.action]}</span>
                    </div>
                    {pod.reason ? <p className="mt-2 break-words text-xs text-muted-foreground">{pod.reason}</p> : null}
                    {pod.pdbNames?.length ? <p className="mt-1 break-all text-xs text-warning">关联 PDB：{pod.pdbNames.join("、")}</p> : null}
                  </div>
                ))}</div> : <p className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">节点上没有需要处理的 Pod。</p>}
              </>
            ) : null}
            <div className="min-w-0 rounded-md border border-destructive/25 bg-destructive/5 p-3">
              <Label htmlFor="drain-confirmation" className="text-sm">输入节点名称确认风险操作</Label>
              <p className="mt-1 break-all font-mono text-xs text-muted-foreground">{node.name}</p>
              <Input id="drain-confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" className="mt-2" placeholder={node.name} />
            </div>
          </div>
        )}
        <DialogFooter className="gap-2">
          {result ? <Button onClick={() => close(false)}>完成</Button> : (
            <>
              <Button variant="outline" onClick={() => close(false)}>取消</Button>
              <Button variant="destructive" disabled={!canDrain || !plan.data} onClick={() => drain.mutate()}>
                {drain.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <AlertTriangle className="h-4 w-4" />}确认排空
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DrainResultView({ result }: { result: NodeDrainResult }) {
  const problematic = result.blockedCount + result.failedCount;
  return (
    <div className="min-w-0 space-y-3">
      <div className={cn("rounded-lg border p-3 text-sm", problematic ? "border-warning/35 bg-warning/10" : "border-success/35 bg-success/10")}>
        <p className="font-medium">{problematic ? "排空未完全完成" : "节点排空完成"}</p>
        <p className="mt-1 text-xs text-muted-foreground">已驱逐 {result.evictedCount} · 阻止 {result.blockedCount} · 失败 {result.failedCount} · 跳过 {result.skippedCount}</p>
      </div>
      {result.warnings?.length ? <div role="alert" className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{result.warnings.join("；")}</div> : null}
      <div className="min-w-0 space-y-2">{result.pods.map((pod) => (
        <div key={`${pod.namespace}/${pod.name}`} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm">
          <span className="min-w-0 break-all">{pod.namespace}/{pod.name}</span>
          <span className={cn("shrink-0 rounded-full border px-2 py-0.5 text-[11px]", actionClasses[pod.action])}>{actionLabels[pod.action]}</span>
        </div>
      ))}</div>
      <p className="break-words text-xs text-muted-foreground">节点保持停止调度状态。处理阻塞 Pod 后可再次排空，或在节点详情中恢复调度。</p>
    </div>
  );
}
