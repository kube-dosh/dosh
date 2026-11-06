import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { FolderTree, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { NamespaceItem } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { formatAge } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { useCapabilities } from "@/features/auth/use-session";
import { NamespaceDeleteDialog } from "@/features/namespaces/namespace-delete-dialog";

const column = createColumnHelper<NamespaceItem>();

export function NamespacesPage() {
  const search = useSearch({ from: "/app/namespaces" });
  const navigate = useNavigate();
  const [deleting, setDeleting] = useState<NamespaceItem>();
  const capabilities = useCapabilities().data;
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
          column.display({ id: "actions", header: "操作", size: 72, cell: (info) => {
            const item = info.row.original;
            const protectedNamespace = item.name === "default" || item.name.startsWith("kube-");
            if (!capabilities?.deleteNamespace || protectedNamespace) return <span className="text-xs text-muted-foreground">—</span>;
            return <Button variant="ghost" size="icon-sm" aria-label={`删除命名空间 ${item.name}`} onClick={() => setDeleting(item)}><Trash2 className="h-4 w-4" /></Button>;
          } }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        empty={<div className="flex flex-col items-center gap-2"><FolderTree className="h-6 w-6" /><span>没有匹配的命名空间</span></div>}
      />
      <NamespaceDeleteDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => { if (!open) setDeleting(undefined); }}
        name={deleting?.name ?? ""}
        resourceVersion={deleting?.resourceVersion}
        onDeleted={async () => { await query.refetch(); }}
      />
    </>
  );
}
