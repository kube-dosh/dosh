import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { FolderTree } from "lucide-react";
import { api } from "@/api/client";
import type { NamespaceItem } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { formatAge } from "@/lib/utils";

const column = createColumnHelper<NamespaceItem>();

export function NamespacesPage() {
  const search = useSearch({ from: "/app/namespaces" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const updateSearch = (value: string) => {
    void navigate({ to: "/namespaces", search: { ...search, q: value || undefined }, replace: true });
  };
  const query = useQuery({ queryKey: ["namespaces", q], queryFn: () => api.namespaces({ q }) });
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  return (
    <>
      <PageHeader title="命名空间" breadcrumb="集群 / 命名空间" description="查看命名空间状态、标签和创建时间。" actions={<SearchInput value={q} onChange={updateSearch} placeholder="搜索命名空间" />} />
      <DataTable
        columns={[
          column.accessor("name", { header: "名称", cell: (info) => <Link to="/namespaces/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("labels", { header: "Labels", enableSorting: false, cell: (info) => <span className="block max-w-xl truncate font-mono text-[11px] text-muted-foreground">{Object.entries(info.getValue() || {}).map(([key, value]) => `${key}=${value}`).join(", ") || "—"}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        empty={<div className="flex flex-col items-center gap-2"><FolderTree className="h-6 w-6" /><span>没有匹配的命名空间</span></div>}
      />
    </>
  );
}
