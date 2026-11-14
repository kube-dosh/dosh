import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { History, Loader2, RefreshCw, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { WorkloadRevision } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatAge, formatDate } from "@/lib/utils";

const column = createColumnHelper<WorkloadRevision>();

export function WorkloadRevisionHistory({ kind, namespace, name, canRollback, onRolledBack }: {
  kind: string;
  namespace: string;
  name: string;
  canRollback: boolean;
  onRolledBack: () => Promise<void>;
}) {
  const [target, setTarget] = useState<WorkloadRevision>();
  const query = useQuery({
    queryKey: ["workload-revisions", kind, namespace, name],
    queryFn: () => api.workloadRevisions(kind, namespace, name),
  });
  const rollback = useMutation({
    mutationFn: (revision: WorkloadRevision) => api.rollbackWorkload(kind, namespace, name, {
      revision: revision.revision,
    }),
    onSuccess: async (_, revision) => {
      setTarget(undefined);
      await onRolledBack();
      toast.success(`已提交回滚到 revision ${revision.revision}`);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast.error("工作负载已被其他操作更新，历史版本已刷新，请重新确认");
        await query.refetch();
        await onRolledBack();
        return;
      }
      toast.error(error.message);
    },
  });

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-3 space-y-0">
        <div>
          <CardTitle>发布历史</CardTitle>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">Deployment 使用 ReplicaSet、StatefulSet 使用 ControllerRevision 记录 Pod 模板。回滚通过定向 Patch 只恢复 Pod 模板和变更原因，保留副本数、Service 及其他并发修改。</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void query.refetch()} disabled={query.isFetching}>
          <RefreshCw className={`h-4 w-4 ${query.isFetching ? "animate-spin" : ""}`} />刷新
        </Button>
      </CardHeader>
      <CardContent>
        {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : (
          <DataTable
            columns={[
              column.accessor("revision", { header: "Revision", cell: (info) => <span className="font-mono text-xs font-semibold">#{info.getValue()}</span> }),
              column.accessor("current", { header: "状态", cell: (info) => <Badge variant={info.getValue() ? "success" : "secondary"}>{info.getValue() ? "当前" : "历史"}</Badge> }),
              column.accessor("changeCause", { header: "变更原因", cell: (info) => <span className="block max-w-56 truncate text-xs" title={info.getValue()}>{info.getValue() || "—"}</span> }),
              column.accessor("images", { header: "镜像", cell: (info) => <span className="block max-w-72 truncate font-mono text-xs text-muted-foreground" title={info.getValue().join("\n")}>{info.getValue().join(", ") || "—"}</span> }),
              column.accessor("replicas", { header: "副本", cell: (info) => <span className="font-mono text-xs">{info.row.original.readyReplicas}/{info.getValue()}</span> }),
              column.accessor("createdAt", { header: "创建时间", cell: (info) => <span title={formatDate(info.getValue())}>{formatAge(info.getValue())}</span> }),
              column.display({
                id: "actions", header: "操作", size: 88,
                cell: (info) => (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!canRollback || info.row.original.current || rollback.isPending}
                    title={info.row.original.current ? "当前版本无需回滚" : !canRollback ? "当前身份没有回滚权限" : undefined}
                    onClick={() => setTarget(info.row.original)}
                  >
                    <RotateCcw className="h-3.5 w-3.5" />回滚
                  </Button>
                ),
              }),
            ]}
            data={query.data ?? []}
            loading={query.isLoading}
            empty={<div className="flex flex-col items-center gap-2"><History className="h-6 w-6" /><span>该工作负载还没有可用的发布历史</span></div>}
          />
        )}
      </CardContent>
      <AlertDialog open={Boolean(target)} onOpenChange={(open) => { if (!open && !rollback.isPending) setTarget(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>回滚到 revision {target?.revision}？</AlertDialogTitle>
            <AlertDialogDescription>将把 {namespace}/{name} 的 Pod 模板和变更原因恢复为该版本并触发滚动更新。回滚只 Patch 目标字段，不会覆盖副本数、标签或其他并发修改。此操作会记录审计。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rollback.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => { event.preventDefault(); if (target) rollback.mutate(target); }} disabled={rollback.isPending}>
              {rollback.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              确认回滚
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}
