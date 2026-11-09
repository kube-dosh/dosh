import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Eye, FileCode2, FolderTree, Plus, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { NamespaceItem } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import { Button } from "@/components/ui/button";
import { useCapabilities } from "@/features/auth/use-session";
import { NamespaceDeleteDialog } from "@/features/namespaces/namespace-delete-dialog";
import { NamespaceFormDialog } from "@/features/namespaces/namespace-form-dialog";
import { cursorPaginationProps, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge } from "@/lib/utils";

const column = createColumnHelper<NamespaceItem>();

export function NamespacesPage() {
  const search = useSearch({ from: "/app/namespaces" });
  const navigate = useNavigate();
  const [deleting, setDeleting] = useState<NamespaceItem>();
  const [createOpen, setCreateOpen] = useState(false);
  const [yamlTarget, setYamlTarget] = useState<NamespaceItem>();
  const capabilities = useCapabilities().data;
  const namespaceCaps = resourceCapabilities(capabilities, "namespaces");
  const canDelete = namespaceCaps.delete || Boolean(capabilities?.deleteNamespace);
  const q = search.q ?? "";
  const pager = useCursorPagination(`namespaces|${q}`, 20);
  const updateSearch = (value: string) => {
    void navigate({ to: "/namespaces", search: { ...search, q: value || undefined }, replace: true });
  };
  const query = useQuery({
    queryKey: ["namespaces", q, pager.page, pager.cursor],
    queryFn: () => api.namespaces({ q, page: pager.page, pageSize: pager.pageSize, cursor: pager.cursor }),
  });
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const actions = (item: NamespaceItem): ResourceAction[] => {
    const protectedNamespace = item.name === "default" || item.name.startsWith("kube-");
    return [
      { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/namespaces/$name", params: { name: item.name } }) },
      { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget(item) },
      { label: "删除命名空间", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeleting(item), destructive: true, disabled: !canDelete || protectedNamespace, separatorBefore: true },
    ];
  };
  return (
    <>
      <PageHeader title="命名空间" breadcrumb="集群 / 命名空间" description="查看命名空间状态、标签和创建时间。" actions={<div className="flex flex-wrap items-center gap-2"><SearchInput value={q} onChange={updateSearch} placeholder="搜索命名空间" />{namespaceCaps.create ? <Button onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" />创建命名空间</Button> : null}</div>} />
      <DataTable
        columns={[
          column.accessor("name", { header: "名称", cell: (info) => <Link to="/namespaces/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("labels", { header: "Labels", enableSorting: false, cell: (info) => <span className="block max-w-xl truncate font-mono text-[11px] text-muted-foreground">{Object.entries(info.getValue() || {}).map(([key, value]) => `${key}=${value}`).join(", ") || "—"}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
          column.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`命名空间 ${info.row.original.name}`} actions={actions(info.row.original)} /> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={cursorPaginationProps(pager, query.data?.meta, query.data?.items.length ?? 0)}
        empty={<div className="flex flex-col items-center gap-2"><FolderTree className="h-6 w-6" /><span>没有匹配的命名空间</span></div>}
      />
      <NamespaceDeleteDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => { if (!open) setDeleting(undefined); }}
        name={deleting?.name ?? ""}
        resourceVersion={deleting?.resourceVersion}
        onDeleted={async () => { await query.refetch(); }}
      />
      <NamespaceFormDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={async () => { await query.refetch(); }} />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type="namespace" name={yamlTarget?.name ?? ""} />
    </>
  );
}
