import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { CircleDot, Eye, FileCode2, ScrollText, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { PodSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import { useCapabilities } from "@/features/auth/use-session";
import { cursorPaginationProps, retryListQuery, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge, formatCPU, formatMemory } from "@/lib/utils";

const column = createColumnHelper<PodSummary>();

export function PodsPage() {
  const search = useSearch({ from: "/app/pods" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const pager = useCursorPagination(`pods|${namespace}|${q}`, 20);
  const [deleting, setDeleting] = useState<PodSummary>();
  const [yamlTarget, setYamlTarget] = useState<PodSummary>();
  const capabilities = useCapabilities(namespace || undefined).data;
  const podCaps = resourceCapabilities(capabilities, "pods");
  const updateSearch = (patch: { q?: string; namespace?: string }) => {
    void navigate({ to: "/pods", search: { ...search, ...patch }, replace: true });
  };
  const query = useQuery({
    queryKey: ["pods", namespace, q, pager.page, pager.cursor],
    queryFn: () => api.pods({ namespace, q, page: pager.page, pageSize: pager.pageSize, cursor: pager.cursor }),
    refetchInterval: 15_000,
  });
  const metricsAvailable = query.data?.items.some((item) => item.hasMetrics) ?? false;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => retryListQuery(pager, query.error, query.refetch)} />;
  const actions = (pod: PodSummary): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/pods/$namespace/$name", params: { namespace: pod.namespace, name: pod.name } }) },
    { label: "查看日志", icon: <ScrollText className="h-4 w-4" />, onSelect: () => void navigate({ to: "/pods/$namespace/$name", params: { namespace: pod.namespace, name: pod.name }, search: { tab: "logs" } }) },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget(pod) },
    { label: "删除 Pod", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeleting(pod), destructive: true, disabled: !podCaps.delete, separatorBefore: true },
  ];
  return (
    <>
      <PageHeader title="Pod 实例" breadcrumb="工作负载 / Pod 实例" description="查看 Pod 运行状态、重启次数、节点分布和所属控制器。" />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索 Pod 名称" />
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} />
        <span className="text-xs text-muted-foreground sm:ml-auto">{query.data?.meta.totalKnown ? `共 ${query.data.meta.total} 个 Pod` : `已加载 ${query.data?.items.length ?? 0} 个 Pod`}</span>
      </div>
      <DataTable
        columns={[
          column.accessor("name", { header: "Pod 名称", cell: (info) => <Link to="/pods/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          column.accessor("namespace", { header: "命名空间" }),
          column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("ready", { header: "就绪" }),
          column.accessor("restarts", { header: "重启", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
          ...(metricsAvailable ? [
            column.accessor("cpuUsageMilli", { header: "CPU 使用", cell: (info) => <span className="tabular-nums text-xs">{formatCPU(info.getValue() ?? 0)}</span> }),
            column.accessor("memoryUsageBytes", { header: "内存使用", cell: (info) => <span className="tabular-nums text-xs">{formatMemory(info.getValue() ?? 0)}</span> }),
          ] : []),
          column.accessor("ownerKind", { header: "控制器", cell: (info) => <span className="text-xs text-muted-foreground">{info.getValue() ? `${info.getValue()}/${info.row.original.ownerName}` : "—"}</span> }),
          column.accessor("node", { header: "节点", cell: (info) => info.getValue() ? <Link to="/nodes/$name" params={{ name: info.getValue() }} className="font-mono text-xs text-primary hover:underline">{info.getValue()}</Link> : <span className="font-mono text-xs">—</span> }),
          column.accessor("ip", { header: "IP", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
          column.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`Pod ${info.row.original.namespace}/${info.row.original.name}`} actions={actions(info.row.original)} /> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={cursorPaginationProps(pager, query.data?.meta, query.data?.items.length ?? 0)}
        onRowClick={(row) => void navigate({ to: "/pods/$namespace/$name", params: { namespace: row.namespace, name: row.name } })}
        empty={<div className="flex flex-col items-center gap-2"><CircleDot className="h-6 w-6" /><span>没有匹配的 Pod</span></div>}
      />
      <DeleteResourceDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => { if (!open) setDeleting(undefined); }}
        resourceKind="Pod"
        name={deleting?.name ?? ""}
        description="Pod 将被删除；若由 Deployment/StatefulSet 等控制器管理，控制器会自动创建替代实例。"
        onConfirm={async () => { if (!deleting) return; await api.deletePod(deleting.namespace, deleting.name); await query.refetch(); }}
      />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type="pod" name={yamlTarget?.name ?? ""} namespace={yamlTarget?.namespace} />
    </>
  );
}
