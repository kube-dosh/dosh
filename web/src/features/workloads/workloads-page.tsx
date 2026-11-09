import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Boxes, CircleDot, Eye, FileCode2, Loader2, Minus, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { WorkloadSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useCapabilities } from "@/features/auth/use-session";
import { DeploymentCreateDialog } from "@/features/workloads/deployment-create-dialog";
import { cursorPaginationProps, retryListQuery, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { workloadCapabilities } from "@/lib/capabilities";
import { formatAge } from "@/lib/utils";

const column = createColumnHelper<WorkloadSummary>();
const kinds = ["deployment", "statefulset", "daemonset", "job", "cronjob"] as const;
const statuses = ["Running", "Updating", "Pending", "Failed", "Stopped", "Complete", "Suspended", "Idle"] as const;
type YamlTarget = { kind: string; namespace: string; name: string };

export function WorkloadsPage() {
  const search = useSearch({ from: "/app/workloads" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const kind = search.kind ?? "";
  const status = search.status ?? "";
  const pager = useCursorPagination(`workloads|${namespace}|${kind}|${status}|${q}`, 20);
  const [createOpen, setCreateOpen] = useState(false);
  const [scaling, setScaling] = useState<WorkloadSummary>();
  const [replicas, setReplicas] = useState("1");
  const [restarting, setRestarting] = useState<WorkloadSummary>();
  const [deleting, setDeleting] = useState<WorkloadSummary>();
  const [yamlTarget, setYamlTarget] = useState<YamlTarget>();
  const updateSearch = (patch: { q?: string; namespace?: string; kind?: string; status?: string }) => {
    void navigate({ to: "/workloads", search: { ...search, ...patch }, replace: true });
  };
  const query = useQuery({
    queryKey: ["workloads", namespace, kind, status, q, pager.page, pager.cursor],
    queryFn: () => api.workloads({ namespace, kind, status, q, page: pager.page, pageSize: pager.pageSize, cursor: pager.cursor }),
    refetchInterval: 20_000,
  });
  const capabilities = useCapabilities(namespace || undefined).data;
  const deploymentCaps = workloadCapabilities(capabilities, "deployment");
  const refresh = async () => { await query.refetch(); await queryClient.invalidateQueries({ queryKey: ["workloads"] }); };
  const scale = useMutation({
    mutationFn: () => api.scaleWorkload(scaling!.kind.toLowerCase(), scaling!.namespace, scaling!.name, Number(replicas)),
    onSuccess: async () => { toast.success(`${scaling?.name} 副本数已调整为 ${replicas}`); setScaling(undefined); await refresh(); },
    onError: (error) => toast.error(error.message),
  });
  const restart = useMutation({
    mutationFn: () => api.restartWorkload(restarting!.kind.toLowerCase(), restarting!.namespace, restarting!.name),
    onSuccess: async () => { toast.success(`${restarting?.name} 已触发滚动重启`); setRestarting(undefined); await refresh(); },
    onError: (error) => toast.error(error.message),
  });
  if (query.isError) return <ErrorState error={query.error} onRetry={() => retryListQuery(pager, query.error, query.refetch)} />;
  const actions = (workload: WorkloadSummary): ResourceAction[] => {
    const normalizedKind = workload.kind.toLowerCase();
    const caps = workloadCapabilities(capabilities, normalizedKind);
    const canScale = caps.scale && ["deployment", "statefulset"].includes(normalizedKind);
    const canRestart = caps.update && !["job", "cronjob"].includes(normalizedKind);
    return [
      { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind: normalizedKind, namespace: workload.namespace, name: workload.name } }) },
      { label: "扩缩容", icon: <Minus className="h-4 w-4" />, onSelect: () => { setReplicas(String(workload.desired)); setScaling(workload); }, disabled: !canScale },
      { label: "滚动重启", icon: <RefreshCw className="h-4 w-4" />, onSelect: () => setRestarting(workload), disabled: !canRestart },
      { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ kind: normalizedKind, namespace: workload.namespace, name: workload.name }) },
      { label: "删除工作负载", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeleting(workload), destructive: true, disabled: !caps.delete, separatorBefore: true },
    ];
  };
  return (
    <>
      <PageHeader
        title="工作负载"
        breadcrumb="工作负载 / 全部工作负载"
        description="统一查看并管理工作负载 Deployment、StatefulSet、DaemonSet、Job 和 CronJob。"
        actions={<>
          {deploymentCaps.create ? <Button onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" />创建 Deployment</Button> : null}
          <Button variant="outline" asChild><Link to="/pods"><CircleDot className="h-4 w-4" />Pod 实例</Link></Button>
        </>}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索工作负载名称" />
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} />
        <Select value={kind || "__all__"} onValueChange={(value) => updateSearch({ kind: value === "__all__" ? undefined : value })}>
          <SelectTrigger className="w-44" aria-label="工作负载类型筛选"><SelectValue placeholder="全部类型" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部类型</SelectItem>
            {kinds.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={status || "__all__"} onValueChange={(value) => updateSearch({ status: value === "__all__" ? undefined : value })}>
          <SelectTrigger className="w-40" aria-label="工作负载状态筛选"><SelectValue placeholder="全部状态" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部状态</SelectItem>
            {statuses.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
          </SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground sm:ml-auto">{query.data?.meta.totalKnown ? `共 ${query.data.meta.total} 个工作负载` : `已加载 ${query.data?.items.length ?? 0} 个工作负载`}</span>
      </div>
      <DataTable
        columns={[
          column.accessor("name", { header: "名称", cell: (info) => <Link to="/workloads/$kind/$namespace/$name" params={{ kind: info.row.original.kind.toLowerCase(), namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          column.accessor("kind", { header: "类型", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("namespace", { header: "命名空间" }),
          column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("ready", { header: "就绪", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("restarts", { header: "重启", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
          column.accessor("images", { header: "镜像", enableSorting: false, cell: (info) => <span className="block max-w-64 truncate font-mono text-xs text-muted-foreground" title={info.getValue().join("\n")}>{info.getValue().join(", ") || "—"}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
          column.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`${info.row.original.kind} ${info.row.original.namespace}/${info.row.original.name}`} actions={actions(info.row.original)} /> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={cursorPaginationProps(pager, query.data?.meta, query.data?.items.length ?? 0)}
        onRowClick={(row) => void navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind: row.kind.toLowerCase(), namespace: row.namespace, name: row.name } })}
        empty={<div className="flex flex-col items-center gap-2"><Boxes className="h-6 w-6" /><span>没有匹配的工作负载</span></div>}
      />
      <DeploymentCreateDialog open={createOpen} onOpenChange={setCreateOpen} initialNamespace={namespace} />
      <Dialog open={Boolean(scaling)} onOpenChange={(open) => { if (!open) setScaling(undefined); }}>
        <DialogContent className="max-w-md">
          <DialogHeader><DialogTitle>调整副本数</DialogTitle><DialogDescription>{scaling?.namespace}/{scaling?.name} · 当前期望副本数 {scaling?.desired}</DialogDescription></DialogHeader>
          <div className="space-y-2"><Label htmlFor="workload-list-replicas">期望副本数</Label><Input id="workload-list-replicas" inputMode="numeric" value={replicas} onChange={(event) => setReplicas(event.target.value)} /></div>
          <DialogFooter><Button variant="outline" onClick={() => setScaling(undefined)} disabled={scale.isPending}>取消</Button><Button onClick={() => scale.mutate()} disabled={scale.isPending || !/^\d+$/.test(replicas) || Number(replicas) > 10000}>{scale.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认调整</Button></DialogFooter>
        </DialogContent>
      </Dialog>
      <AlertDialog open={Boolean(restarting)} onOpenChange={(open) => { if (!open) setRestarting(undefined); }}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>滚动重启 {restarting?.name}？</AlertDialogTitle><AlertDialogDescription>将更新 Pod 模板注解并逐步替换实例。服务可用性取决于副本数与探针配置。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel disabled={restart.isPending}>取消</AlertDialogCancel><AlertDialogAction onClick={(event) => { event.preventDefault(); restart.mutate(); }} disabled={restart.isPending}>{restart.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认重启</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
      <DeleteResourceDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => { if (!open) setDeleting(undefined); }}
        resourceKind={deleting?.kind ?? "工作负载"}
        name={deleting?.name ?? ""}
        description="关联 Pod 将按 Background 策略删除，此操作不可撤销。"
        onConfirm={async () => { if (!deleting) return; await api.deleteWorkload(deleting.kind.toLowerCase(), deleting.namespace, deleting.name); await refresh(); }}
      />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type={yamlTarget?.kind ?? "deployment"} name={yamlTarget?.name ?? ""} namespace={yamlTarget?.namespace} />
    </>
  );
}
