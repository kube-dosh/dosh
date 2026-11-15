import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Package, ShieldAlert } from "lucide-react";
import { api } from "@/api/client";
import type { HelmReleaseSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { StatusBadge } from "@/components/feedback/status-badge";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Badge } from "@/components/ui/badge";
import { useCapabilities } from "@/features/auth/use-session";
import { formatAge, formatDate } from "@/lib/utils";

const column = createColumnHelper<HelmReleaseSummary>();

export function HelmReleasesPage() {
  const search = useSearch({ from: "/app/helm/releases" });
  const navigate = useNavigate();
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const capabilities = useCapabilities().data;
  const query = useQuery({
    queryKey: ["helm-releases", namespace],
    queryFn: () => api.helmReleases(namespace),
    enabled: Boolean(namespace) && Boolean(capabilities?.helmReleases),
    refetchInterval: 30_000,
  });
  const items = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return query.data?.items ?? [];
    return (query.data?.items ?? []).filter((item) => [item.name, item.chart, item.chartVersion, item.appVersion, item.status].some((value) => value?.toLowerCase().includes(needle)));
  }, [q, query.data?.items]);
  const updateSearch = (patch: { q?: string; namespace?: string }) => {
    void navigate({ to: "/helm/releases", search: { ...search, ...patch }, replace: true });
  };

  if (!capabilities) return <EmptyState title="正在读取 Helm 能力" description="正在检查当前身份是否可以读取 Helm Release 的 Secret 存储。" />;
  if (!capabilities.helmReleases) {
    return <ErrorState error={new Error("当前身份没有 list secrets 权限，因此无法读取 Helm Release 存储。")} />;
  }
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;

  return (
    <>
      <PageHeader
        title="Helm Releases"
        breadcrumb="应用 / Helm Releases"
        description="只读浏览 Helm Secret 存储中的 Release、values、渲染清单和历史版本。敏感字段在服务端遮蔽。"
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} allowAll={false} ariaLabel="Helm Release 命名空间" className="w-full sm:w-56" />
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索 Release、Chart 或状态" className="sm:w-72" />
        {namespace ? <span className="text-xs text-muted-foreground sm:ml-auto">共 {items.length} 个 Release</span> : null}
      </div>
      {!namespace ? (
        <EmptyState title="选择命名空间" description="Helm Release 只能从对应命名空间的 Helm Secret 中读取。请选择一个命名空间开始。" />
      ) : (
        <>
          {query.data?.warnings?.length ? (
            <div role="status" className="mb-3 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <span className="min-w-0 break-words">{query.data.warnings.join("；")}</span>
            </div>
          ) : null}
          <DataTable
            columns={[
              column.accessor("name", {
                header: "Release",
                cell: (info) => <Link to="/helm/releases/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link>,
              }),
              column.accessor("chart", {
                header: "Chart",
                cell: (info) => <div className="min-w-0"><p className="break-all font-mono text-xs">{info.getValue()}</p><p className="mt-0.5 text-[11px] text-muted-foreground">{info.row.original.chartVersion}{info.row.original.appVersion ? ` · app ${info.row.original.appVersion}` : ""}</p></div>,
              }),
              column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              column.accessor("revision", { header: "Revision", cell: (info) => <Badge variant="secondary" className="font-mono">#{info.getValue()}</Badge> }),
              column.accessor("description", { header: "说明", cell: (info) => <span className="block max-w-64 truncate text-xs text-muted-foreground" title={info.getValue()}>{info.getValue() || "—"}</span> }),
              column.accessor("updatedAt", { header: "更新时间", cell: (info) => <span className="whitespace-nowrap text-xs text-muted-foreground" title={formatDate(info.getValue())}>{formatAge(info.getValue())}</span> }),
            ]}
            data={items}
            loading={query.isLoading}
            stickyFirstColumn={false}
            empty={<div className="flex flex-col items-center gap-2"><Package className="h-6 w-6" /><span>{q ? "没有匹配的 Release" : "该命名空间没有 Helm Release"}</span></div>}
          />
        </>
      )}
    </>
  );
}
