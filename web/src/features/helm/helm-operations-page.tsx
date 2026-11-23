import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Activity, Ban, CircleAlert, Clock3, ExternalLink, Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { HelmChartOperation, HelmOperationResource } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { SearchInput } from "@/components/layout/search-input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useCapabilities } from "@/features/auth/use-session";
import { HelmSectionNav } from "@/features/helm/helm-section-nav";
import { formatAge, formatDate } from "@/lib/utils";

const column = createColumnHelper<HelmChartOperation>();
const allValue = "__all__";

const operationTypeLabels: Record<HelmChartOperation["type"], string> = {
  install: "安装",
  upgrade: "升级",
  uninstall: "卸载",
  rollback: "回滚",
};

const operationStatusMeta: Record<HelmChartOperation["status"], { label: string; variant: "default" | "secondary" | "success" | "warning" | "destructive" }> = {
  pending: { label: "等待中", variant: "warning" },
  running: { label: "运行中", variant: "default" },
  success: { label: "成功", variant: "success" },
  failed: { label: "失败", variant: "destructive" },
  cancelled: { label: "已取消", variant: "secondary" },
};

const resourceStatusMeta: Record<HelmOperationResource["status"], { label: string; variant: "default" | "secondary" | "success" | "warning" | "destructive" | "outline" }> = {
  pending: { label: "等待执行", variant: "secondary" },
  waiting: { label: "等待就绪", variant: "warning" },
  ready: { label: "已就绪", variant: "success" },
  failed: { label: "失败", variant: "destructive" },
  created: { label: "已创建", variant: "default" },
  unknown: { label: "状态未知", variant: "outline" },
};

function OperationStatusBadge({ status }: { status: HelmChartOperation["status"] }) {
  const meta = operationStatusMeta[status] ?? { label: status, variant: "secondary" as const };
  return <Badge variant={meta.variant}>{meta.label}</Badge>;
}

function ResourceStatusBadge({ status }: { status: HelmOperationResource["status"] }) {
  const meta = resourceStatusMeta[status] ?? { label: status, variant: "secondary" as const };
  return <Badge variant={meta.variant}>{meta.label}</Badge>;
}

export function HelmOperationsPage() {
  const search = useSearch({ from: "/app/helm/operations" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const capabilities = useCapabilities().data;
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const [status, setStatus] = useState("__all__");
  const [selectedId, setSelectedId] = useState<string>();
  const [cancelTarget, setCancelTarget] = useState<HelmChartOperation>();

  const operations = useQuery({
    queryKey: ["helm-operations", namespace, status],
    queryFn: () => api.helmChartOperations({
      namespace: namespace || undefined,
      status: status === allValue ? undefined : status,
      limit: 200,
    }),
    enabled: Boolean(capabilities?.helmChartRepositories || capabilities?.helmReleases),
    refetchInterval: (query) => {
      const items = query.state.data;
      return items?.some((item) => item.status === "running" || item.status === "pending") ? 3_000 : 15_000;
    },
  });
  const selected = useQuery({
    queryKey: ["helm-operation", selectedId],
    queryFn: () => api.helmChartOperation(selectedId!),
    enabled: Boolean(selectedId),
    refetchInterval: (query) => {
      const item = query.state.data;
      return item?.status === "running" || item?.status === "pending" ? 2_000 : false;
    },
  });
  const cancel = useMutation({
    mutationFn: (operation: HelmChartOperation) => api.cancelHelmChartOperation(operation.id),
    onSuccess: async (operation) => {
      toast.success(`已请求取消 ${operation.releaseName} 的操作`);
      setCancelTarget(undefined);
      await queryClient.invalidateQueries({ queryKey: ["helm-operations"] });
      await queryClient.invalidateQueries({ queryKey: ["helm-operation", operation.id] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "取消操作失败"),
  });

  const updateSearch = (patch: { namespace?: string; q?: string }) => {
    void navigate({ to: "/helm/operations", search: { ...search, ...patch }, replace: true });
  };

  const items = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return operations.data ?? [];
    return (operations.data ?? []).filter((item) =>
      [item.namespace, item.releaseName, item.chartName, item.chartVersion, item.type, item.status, item.message]
        .some((value) => value?.toLowerCase().includes(needle)),
    );
  }, [operations.data, q]);

  if (!capabilities) return <LoadingState label="正在检查 Helm 操作能力" />;
  if (!capabilities.helmChartRepositories && !capabilities.helmReleases) {
    return <ErrorState error={new Error("当前身份没有 Helm 仓库或命名空间 Release 读取权限，无法查看 Helm 操作中心。")} />;
  }
  if (operations.isError) return <ErrorState error={operations.error} onRetry={() => void operations.refetch()} />;

  const runningCount = (operations.data ?? []).filter((item) => item.status === "running" || item.status === "pending").length;

  return (
    <>
      <PageHeader
        title="Helm 操作中心"
        breadcrumb="应用 / Helm / 操作中心"
        description="统一查看安装、升级、卸载和回滚的执行状态、资源进度与失败原因；运行中的操作可在这里取消。"
        actions={<Button variant="outline" onClick={() => void operations.refetch()} disabled={operations.isFetching}><RefreshCw className={`h-4 w-4 ${operations.isFetching ? "animate-spin" : ""}`} />刷新</Button>}
      />
      <HelmSectionNav active="operations" />

      <div className="mb-4 flex min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} ariaLabel="操作命名空间筛选" className="w-full sm:w-56" />
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger className="w-full sm:w-44" aria-label="操作状态筛选"><SelectValue placeholder="全部状态" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={allValue}>全部状态</SelectItem>
            <SelectItem value="running">运行中</SelectItem>
            <SelectItem value="pending">等待中</SelectItem>
            <SelectItem value="failed">失败</SelectItem>
            <SelectItem value="success">成功</SelectItem>
            <SelectItem value="cancelled">已取消</SelectItem>
          </SelectContent>
        </Select>
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索 Release、Chart 或错误信息" className="sm:w-80" />
        <span className="whitespace-nowrap text-xs text-muted-foreground sm:ml-auto">
          {runningCount > 0 ? `${runningCount} 个操作执行中 · ` : ""}共 {items.length} 条
        </span>
      </div>

      <DataTable
        columns={[
          column.accessor("releaseName", {
            header: "Release",
            cell: (info) => <div className="min-w-0"><p className="break-all font-medium">{info.getValue()}</p><p className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">{info.row.original.namespace}</p></div>,
          }),
          column.accessor("type", {
            header: "操作",
            cell: (info) => <Badge variant="secondary">{operationTypeLabels[info.getValue() as HelmChartOperation["type"]] ?? info.getValue()}</Badge>,
          }),
          column.accessor("status", { header: "状态", cell: (info) => <OperationStatusBadge status={info.getValue()} /> }),
          column.accessor("chartName", {
            header: "Chart",
            cell: (info) => <div className="min-w-0"><p className="break-all font-mono text-xs">{info.getValue() || "—"}</p><p className="mt-0.5 break-all text-[11px] text-muted-foreground">{info.row.original.chartVersion || ""}</p></div>,
          }),
          column.accessor("revision", { header: "Revision", cell: (info) => info.getValue() ? <Badge variant="outline" className="font-mono">#{info.getValue()}</Badge> : <span className="text-xs text-muted-foreground">—</span> }),
          column.accessor("startedAt", { header: "开始时间", cell: (info) => <span className="whitespace-nowrap text-xs text-muted-foreground" title={formatDate(info.getValue())}>{formatAge(info.getValue())}</span> }),
          column.accessor("message", { header: "结果", cell: (info) => <span className="block max-w-64 truncate text-xs text-muted-foreground" title={info.getValue()}>{info.getValue() || "—"}</span> }),
        ]}
        data={items}
        loading={operations.isLoading}
        onRowClick={(item) => setSelectedId(item.id)}
        stickyFirstColumn={false}
        empty={<div className="flex flex-col items-center gap-2"><Activity className="h-6 w-6" /><span>{q || status !== allValue ? "没有匹配的操作" : "还没有 Helm 操作记录"}</span></div>}
      />

      <Dialog open={Boolean(selectedId)} onOpenChange={(open) => { if (!open) setSelectedId(undefined); }}>
        <DialogContent className="max-h-[88vh] w-[calc(100vw-2rem)] max-w-3xl overflow-y-auto p-0 sm:w-full">
          <DialogHeader className="border-b px-4 py-4 pr-12 sm:px-6">
            <DialogTitle className="min-w-0 break-words">操作详情</DialogTitle>
            <DialogDescription>运行中的状态会自动刷新；取消请求由持有操作心跳的 Dosh 副本接收，并等待当前 Helm 操作安全退出。</DialogDescription>
          </DialogHeader>
          {selected.isLoading ? <LoadingState label="正在读取操作详情" /> : selected.isError ? <div className="p-4"><ErrorState error={selected.error} onRetry={() => void selected.refetch()} /></div> : selected.data ? (
            <OperationDetail operation={selected.data} onCancel={() => setCancelTarget(selected.data)} />
          ) : null}
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(cancelTarget)} onOpenChange={(open) => { if (!open && !cancel.isPending) setCancelTarget(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>取消 {cancelTarget?.releaseName} 的 Helm 操作？</AlertDialogTitle>
            <AlertDialogDescription>
              取消请求会通过操作心跳发送给实际执行副本。已经提交到 Kubernetes 的资源不会自动回退；卸载和回滚受 Helm SDK 限制，可能直到当前步骤结束才能响应。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancel.isPending}>保留操作</AlertDialogCancel>
            <AlertDialogAction disabled={cancel.isPending} onClick={(event) => { event.preventDefault(); if (cancelTarget) cancel.mutate(cancelTarget); }}>
              {cancel.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Ban className="h-4 w-4" />}
              确认取消
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function OperationDetail({ operation, onCancel }: { operation: HelmChartOperation; onCancel: () => void }) {
  const running = operation.status === "running" || operation.status === "pending";
  const resources = operation.resources ?? [];
  const completed = resources.filter((item) => item.status === "ready" || item.status === "created").length;
  const failed = resources.filter((item) => item.status === "failed").length;
  return (
    <div className="min-w-0 space-y-4 p-4 sm:p-6">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <OperationStatusBadge status={operation.status} />
        <Badge variant="secondary">{operationTypeLabels[operation.type] ?? operation.type}</Badge>
        <Link to="/helm/releases/$namespace/$name" params={{ namespace: operation.namespace, name: operation.releaseName }} className="inline-flex min-w-0 items-center gap-1 break-all text-sm font-medium text-primary hover:underline">
          {operation.namespace}/{operation.releaseName}
          <ExternalLink className="h-3.5 w-3.5 shrink-0" />
        </Link>
        {running ? <Button variant="outline" size="sm" className="ml-auto" onClick={onCancel} disabled={operation.cancelRequested}><Ban className="h-3.5 w-3.5" />{operation.cancelRequested ? "取消中…" : "取消操作"}</Button> : null}
      </div>

      <div className="grid min-w-0 grid-cols-1 gap-3 text-sm sm:grid-cols-2">
        <Detail label="Chart" value={operation.chartName ? `${operation.chartName} ${operation.chartVersion ?? ""}` : "—"} />
        <Detail label="Revision" value={operation.revision ? `#${operation.revision}` : "—"} />
        <Detail label="开始时间" value={formatDate(operation.startedAt)} />
        <Detail label="结束时间" value={operation.finishedAt ? formatDate(operation.finishedAt) : "—"} />
        <Detail label="资源进度" value={resources.length ? `${completed}/${resources.length} 已就绪${failed ? ` · ${failed} 失败` : ""}` : "暂无资源进度"} />
        <Detail label="Digest" value={operation.digest || "—"} mono />
      </div>

      {operation.message ? <div className={`min-w-0 break-words rounded-md border px-3 py-2 text-sm ${operation.status === "failed" ? "border-destructive/30 bg-destructive/10 text-destructive-strong" : "bg-muted/40"}`}>{operation.message}</div> : null}

      {operation.warnings?.length ? <div className="min-w-0 space-y-1 rounded-md border border-warning/35 bg-warning/10 p-3 text-xs text-warning">{operation.warnings.map((warning, index) => <p key={index} className="break-words">{warning}</p>)}</div> : null}

      <Card className="min-w-0">
        <CardHeader className="px-0 pt-0">
          <CardTitle className="flex items-center gap-2 text-base"><Clock3 className="h-4 w-4" />资源执行状态</CardTitle>
          <CardDescription>{resources.length ? "每一项资源均为计划中审核过的对象。" : "该操作没有生成资源清单，可能只修改了 Helm Release 历史或正在等待执行器。"}</CardDescription>
        </CardHeader>
        <CardContent className="min-w-0 px-0 pb-0">
          {resources.length ? (
            <div className="max-h-[46vh] space-y-2 overflow-y-auto pr-1">
              {resources.map((resource, index) => (
                <div key={`${resource.apiVersion}-${resource.kind}-${resource.namespace}-${resource.name}-${index}`} className="min-w-0 rounded-md border px-3 py-2">
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                    <ResourceStatusBadge status={resource.status} />
                    <Badge variant="secondary" className="max-w-full"><span className="truncate">{resource.kind}</span></Badge>
                    {resource.hook ? <Badge variant="outline">Hook</Badge> : null}
                    {resource.crd ? <Badge variant="outline">CRD</Badge> : null}
                    <Badge variant={resource.action === "delete" ? "destructive" : "outline"}>{resource.action}</Badge>
                  </div>
                  <p className="mt-1 break-all font-mono text-xs">{resource.namespace ? `${resource.namespace}/` : ""}{resource.name}</p>
                  {resource.message ? <p className="mt-1 break-words text-xs text-muted-foreground">{resource.message}</p> : null}
                </div>
              ))}
            </div>
          ) : <EmptyState title="暂无资源进度" description="操作仍可能在执行，请稍后刷新。" className="min-h-32" />}
        </CardContent>
      </Card>

      {failed > 0 ? <p className="flex items-start gap-2 break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong"><CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />存在失败资源，请结合 Release 详情、事件和 Pod 状态继续排查。</p> : null}
    </div>
  );
}

function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="min-w-0 rounded-md border px-3 py-2"><p className="text-xs text-muted-foreground">{label}</p><p className={`mt-1 break-all ${mono ? "font-mono text-xs" : "text-sm"}`}>{value}</p></div>;
}
