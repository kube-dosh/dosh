import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { BookmarkPlus, Check, Eye, FileCode2, Layers3, Plus, SlidersHorizontal, Star, Trash2, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { GenericResourceSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { GenericYamlPanel } from "@/features/resources/generic-yaml-panel";
import { GenericResourceEditorDialog } from "@/features/resources/generic-resource-editor-dialog";
import { cursorPaginationProps, retryListQuery, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { formatAge } from "@/lib/utils";
import { deleteResourceView, favoriteResourceTypes, hiddenResourceColumns, resourceTypeKey, saveResourceView, savedResourceViews, setHiddenResourceColumns, toggleFavoriteResourceType, useResourcePreferencesRevision } from "@/lib/resource-preferences";

const column = createColumnHelper<GenericResourceSummary>();

function ResourceYamlDialog({ target, group, version, resource, namespace, onClose }: {
  target: GenericResourceSummary;
  group: string;
  version: string;
  resource: string;
  namespace: string;
  onClose: () => void;
}) {
  const query = useQuery({
    queryKey: ["generic-resource", group, version, resource, target.namespace || namespace, target.name],
    queryFn: () => api.resource({ group, version, resource }, target.name, target.namespace || namespace),
  });
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="max-w-5xl">
        <DialogHeader>
          <DialogTitle>{target.name}</DialogTitle>
          <DialogDescription>{target.kind}{target.namespace ? ` · ${target.namespace}` : ""} · 只读 YAML</DialogDescription>
        </DialogHeader>
        {query.isLoading ? <LoadingState /> : query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : query.data ? <GenericYamlPanel yaml={query.data.yaml} height="56vh" /> : null}
      </DialogContent>
    </Dialog>
  );
}

export function ResourceListPage() {
  const params = useParams({ strict: false }) as { group: string; version: string; resource: string };
  const search = useSearch({ from: "/app/resources/$group/$version/$resource" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [yamlTarget, setYamlTarget] = useState<GenericResourceSummary>();
  const [createOpen, setCreateOpen] = useState(false);
  const version = params.version;
  const resource = params.resource;
  const apiGroup = params.group === "_core" ? "" : params.group;
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const preferencesRevision = useResourcePreferencesRevision();
  const preferenceKey = resourceTypeKey(apiGroup, resource);
  const favorite = useMemo(() => favoriteResourceTypes().includes(preferenceKey), [preferenceKey, preferencesRevision]);
  const savedViews = useMemo(() => savedResourceViews(preferenceKey), [preferenceKey, preferencesRevision]);
  const hiddenColumns = useMemo(() => new Set(hiddenResourceColumns(preferenceKey)), [preferenceKey, preferencesRevision]);
  const discovery = useQuery({ queryKey: ["resource-discovery", "all"], queryFn: () => api.resourceDiscovery(), staleTime: 60_000 });
  const versions = useMemo(() => (discovery.data?.items ?? [])
    .filter((item) => item.group === apiGroup && item.resource === resource)
    .sort((left, right) => Number(right.preferred) - Number(left.preferred) || right.version.localeCompare(left.version)), [apiGroup, discovery.data?.items, resource]);
  const descriptor = discovery.data?.items.find((item) => item.group === apiGroup && item.version === version && item.resource === resource);
  const capabilities = useQuery({
    queryKey: ["generic-resource-capabilities", apiGroup, version, resource, namespace],
    queryFn: () => api.genericResourceCapabilities({ group: apiGroup, version, resource }, namespace || undefined),
    enabled: Boolean(descriptor) && (!descriptor?.namespaced || Boolean(namespace)),
    staleTime: 30_000,
  });
  const canCreate = Boolean(capabilities.data?.create);
  const createDisabled = descriptor?.namespaced && !namespace;
  const pager = useCursorPagination(`resource|${apiGroup}|${version}|${resource}|${namespace}|${q}`, 20);
  const query = useQuery({
    queryKey: ["generic-resources", apiGroup, version, resource, namespace, q, pager.page, pager.cursor],
    queryFn: () => api.resources({ group: apiGroup, version, resource }, {
      namespace, q, page: pager.page, pageSize: pager.pageSize, cursor: pager.cursor,
    }),
    enabled: Boolean(descriptor),
    refetchInterval: 20_000,
  });
  const updateSearch = (patch: { q?: string; namespace?: string }) => {
    void navigate({
      to: "/resources/$group/$version/$resource",
      params: { group: params.group, version, resource },
      search: { ...search, ...patch },
      replace: true,
    });
  };
  const changeVersion = (nextVersion: string) => {
    void navigate({
      to: "/resources/$group/$version/$resource",
      params: { group: params.group, version: nextVersion, resource },
      search: { ...search },
    });
  };
  const detailSearch = (item: GenericResourceSummary) => ({ namespace: item.namespace || namespace || undefined });
  const actions = (item: GenericResourceSummary): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({
      to: "/resources/$group/$version/$resource/$name",
      params: { group: params.group, version, resource, name: item.name },
      search: detailSearch(item),
    }) },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget(item) },
  ];
  const tableColumns = useMemo(() => {
    const apiColumns = query.data?.meta.columns ?? [];
    if (apiColumns.length > 0) {
      return apiColumns
        .filter((item) => item.name.toLowerCase() !== "name")
        .map((item) => column.accessor((row) => resourceCellText(row.cells?.[item.name]), {
          id: `cell-${item.name}`,
          header: item.name,
          enableSorting: false,
          cell: (info) => <span className="block max-w-[24rem] truncate" title={info.getValue()}>{info.getValue() || "—"}</span>,
        }));
    }
    return [
      column.accessor("namespace", { header: "命名空间", cell: (info) => info.getValue() || "—" }),
      column.accessor("kind", { header: "Kind", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
      column.accessor("labels", {
        header: "Labels",
        enableSorting: false,
        cell: (info) => <span className="block max-w-xl truncate font-mono text-[11px] text-muted-foreground">{Object.entries(info.getValue() || {}).map(([key, value]) => `${key}=${value}`).join(", ") || "—"}</span>,
      }),
      column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
    ];
  }, [query.data?.meta.columns]);
  const tableColumnID = (item: (typeof tableColumns)[number]) => String(item.id ?? (item as { accessorKey?: string }).accessorKey ?? "");
  const columnChoices = useMemo(() => tableColumns.map((item) => ({ id: tableColumnID(item), label: String(item.header ?? tableColumnID(item) ?? "列") })).filter((item) => item.id), [tableColumns]);
  const visibleTableColumns = useMemo(() => tableColumns.filter((item) => !hiddenColumns.has(tableColumnID(item))), [hiddenColumns, tableColumns]);
  const toggleColumn = (id: string) => {
    const next = new Set(hiddenColumns);
    if (next.has(id)) next.delete(id); else next.add(id);
    setHiddenResourceColumns(preferenceKey, [...next]);
  };
  const saveCurrentView = () => {
    const name = q ? `搜索：${q}` : namespace ? `${namespace} · 默认视图` : "全部对象";
    saveResourceView(preferenceKey, { name, q: q || undefined, namespace: namespace || undefined, hiddenColumns: [...hiddenColumns] });
    toast.success(`已保存视图“${name}”`);
  };

  if (discovery.isError) return <ErrorState error={discovery.error} onRetry={() => void discovery.refetch()} />;
  if (!discovery.isLoading && !descriptor) {
    return (
      <EmptyState
        title="资源类型不存在"
        description="该 API Group、版本和资源名不在当前 Discovery 结果中，可能已被移除或当前集群不提供该 API。"
        action={<Button asChild variant="outline"><Link to="/resources">返回资源浏览器</Link></Button>}
      />
    );
  }
  if (query.isError) return <ErrorState error={query.error} onRetry={() => retryListQuery(pager, query.error, query.refetch)} />;
  return (
    <>
      <PageHeader
        title={descriptor?.kind || resource}
        breadcrumb={<><Link to="/resources" className="hover:text-foreground hover:underline">资源浏览器</Link>{` / ${apiGroup || "core"}/${version}/${resource}`}</>}
        description={descriptor ? `${descriptor.namespaced ? "Namespaced" : "Cluster-scoped"} · ${descriptor.group || "core"}/${descriptor.version}${descriptor.preferred ? " · Preferred" : ""} · 当前读取受 Kubernetes RBAC 约束。` : "正在读取资源类型。"}
          actions={<div className="flex flex-wrap items-center gap-2"><Button variant="outline" size="sm" aria-pressed={favorite} onClick={() => { const active = toggleFavoriteResourceType(preferenceKey); toast.success(active ? "已收藏资源类型" : "已取消收藏"); }}><Star className={favorite ? "h-4 w-4 fill-current text-warning" : "h-4 w-4"} />{favorite ? "已收藏" : "收藏类型"}</Button>{canCreate ? <Button size="sm" disabled={createDisabled} title={createDisabled ? "请先选择命名空间" : undefined} onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" />创建 YAML</Button> : null}</div>}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索名称或命名空间" />
        {versions.length > 1 ? (
          <Select value={version} onValueChange={changeVersion}>
            <SelectTrigger className="w-full sm:w-44" aria-label="API 版本"><SelectValue /></SelectTrigger>
            <SelectContent>
              {versions.map((item) => (
                <SelectItem key={item.version} value={item.version}>
                  {item.version}{item.preferred ? " · Preferred" : ""}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : descriptor?.preferred ? <Badge variant="secondary" className="w-fit">Preferred version</Badge> : null}
        {descriptor?.namespaced ? <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /> : null}
        <DropdownMenu>
          <DropdownMenuTrigger asChild><Button variant="outline" size="sm"><BookmarkPlus className="h-4 w-4" />视图</Button></DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuItem onSelect={saveCurrentView}><BookmarkPlus className="h-4 w-4" />保存当前视图</DropdownMenuItem>
            {savedViews.length ? <DropdownMenuSeparator /> : null}
            {savedViews.length ? <DropdownMenuLabel>已保存视图</DropdownMenuLabel> : null}
            {savedViews.map((view) => (
              <div key={view.id} className="flex items-center gap-1">
                <DropdownMenuItem className="min-w-0 flex-1" onSelect={() => { setHiddenResourceColumns(preferenceKey, view.hiddenColumns); void navigate({ to: "/resources/$group/$version/$resource", params: { group: params.group, version, resource }, search: { ...search, q: view.q, namespace: view.namespace }, replace: true }); }}><span className="truncate">{view.name}</span></DropdownMenuItem>
                <DropdownMenuItem aria-label={`删除视图 ${view.name}`} className="h-8 w-8 shrink-0 justify-center p-0 text-muted-foreground" onSelect={() => { deleteResourceView(preferenceKey, view.id); toast.success("已删除保存视图"); }}><Trash2 className="h-3.5 w-3.5" /></DropdownMenuItem>
              </div>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <DropdownMenu>
          <DropdownMenuTrigger asChild><Button variant="outline" size="sm"><SlidersHorizontal className="h-4 w-4" />列</Button></DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>显示列</DropdownMenuLabel>
            {columnChoices.map((item) => {
              const visible = !hiddenColumns.has(item.id);
              return <DropdownMenuItem key={item.id} onSelect={(event) => { event.preventDefault(); toggleColumn(item.id); }}>{visible ? <Check className="h-4 w-4" /> : <span className="h-4 w-4" />}<span className="truncate">{item.label}</span></DropdownMenuItem>;
            })}
          </DropdownMenuContent>
        </DropdownMenu>
        {query.data?.meta.truncated ? (
          <span className="flex items-center gap-1 text-xs text-warning"><TriangleAlert className="h-3.5 w-3.5" />结果已截断</span>
        ) : null}
      </div>
      <DataTable
        columns={[
          column.accessor("name", {
            header: "名称",
            cell: (info) => (
              <Link
                to="/resources/$group/$version/$resource/$name"
                params={{ group: params.group, version, resource, name: info.getValue() }}
                search={detailSearch(info.row.original)}
                className="font-medium text-primary hover:underline"
              >
                {info.getValue()}
              </Link>
            ),
          }),
          ...visibleTableColumns,
          column.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`${info.row.original.kind} ${info.row.original.name}`} actions={actions(info.row.original)} /> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading || discovery.isLoading}
        pagination={cursorPaginationProps(pager, query.data?.meta, query.data?.items.length ?? 0)}
        stickyFirstColumn={false}
        empty={<div className="flex flex-col items-center gap-2"><Layers3 className="h-6 w-6" /><span>没有匹配的 {descriptor?.kind || resource} 对象</span></div>}
        onRowClick={(item) => void navigate({
          to: "/resources/$group/$version/$resource/$name",
          params: { group: params.group, version, resource, name: item.name },
          search: detailSearch(item),
        })}
      />
      {yamlTarget ? <ResourceYamlDialog target={yamlTarget} group={apiGroup} version={version} resource={resource} namespace={namespace} onClose={() => setYamlTarget(undefined)} /> : null}
      {descriptor ? (
        <GenericResourceEditorDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          descriptor={descriptor}
          reference={{ group: apiGroup, version, resource }}
          namespace={namespace}
          mode="create"
          capabilities={capabilities.data}
          onSaved={async () => {
            await queryClient.invalidateQueries({ queryKey: ["generic-resources"] });
            await queryClient.invalidateQueries({ queryKey: ["resource-discovery"] });
            await queryClient.invalidateQueries({ queryKey: ["generic-resource-capabilities"] });
          }}
        />
      ) : null}
    </>
  );
}

function resourceCellText(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}
