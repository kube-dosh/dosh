import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { CircleDot } from "lucide-react";
import { api } from "@/api/client";
import type { PodSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { formatAge, formatCPU, formatMemory } from "@/lib/utils";

const column = createColumnHelper<PodSummary>();

export function PodsPage() {
  const search = useSearch({ from: "/app/pods" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const updateSearch = (patch: { q?: string; namespace?: string }) => {
    void navigate({ to: "/pods", search: { ...search, ...patch }, replace: true });
  };
  const query = useQuery({
    queryKey: ["pods", namespace, q],
    queryFn: () => api.pods({ namespace, q }),
    refetchInterval: 15_000,
  });
  const metricsAvailable = query.data?.items.some((item) => item.hasMetrics) ?? false;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  return (
    <>
      <PageHeader title="Pod 实例" breadcrumb="工作负载 / Pod 实例" description="查看 Pod 运行状态、重启次数、节点分布和所属控制器。" />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索 Pod 名称" />
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} />
        <span className="text-xs text-muted-foreground sm:ml-auto">{query.data?.meta.total ?? 0} 个 Pod</span>
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
          column.accessor("node", { header: "节点", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("ip", { header: "IP", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        onRowClick={(row) => void navigate({ to: "/pods/$namespace/$name", params: { namespace: row.namespace, name: row.name } })}
        empty={<div className="flex flex-col items-center gap-2"><CircleDot className="h-6 w-6" /><span>没有匹配的 Pod</span></div>}
      />
    </>
  );
}
