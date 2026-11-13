import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Eye, FileCode2, Layers3, TriangleAlert } from "lucide-react";
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
import { GenericYamlPanel } from "@/features/resources/generic-yaml-panel";
import { cursorPaginationProps, retryListQuery, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { formatAge } from "@/lib/utils";

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
  const [yamlTarget, setYamlTarget] = useState<GenericResourceSummary>();
  const version = params.version;
  const resource = params.resource;
  const apiGroup = params.group === "_core" ? "" : params.group;
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const discovery = useQuery({ queryKey: ["resource-discovery"], queryFn: api.resourceDiscovery, staleTime: 60_000 });
  const descriptor = discovery.data?.items.find((item) => item.group === apiGroup && item.version === version && item.resource === resource);
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
  const detailSearch = (item: GenericResourceSummary) => ({ namespace: item.namespace });
  const actions = (item: GenericResourceSummary): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({
      to: "/resources/$group/$version/$resource/$name",
      params: { group: params.group, version, resource, name: item.name },
      search: detailSearch(item),
    }) },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget(item) },
  ];

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
        description={descriptor ? `${descriptor.namespaced ? "Namespaced" : "Cluster-scoped"} · ${descriptor.group || "core"}/${descriptor.version} · 当前读取受 Kubernetes RBAC 约束。` : "正在读取资源类型。"}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索名称或命名空间" />
        {descriptor?.namespaced ? <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /> : null}
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
          column.accessor("namespace", { header: "命名空间", cell: (info) => info.getValue() || "—" }),
          column.accessor("kind", { header: "Kind", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("labels", {
            header: "Labels",
            enableSorting: false,
            cell: (info) => <span className="block max-w-xl truncate font-mono text-[11px] text-muted-foreground">{Object.entries(info.getValue() || {}).map(([key, value]) => `${key}=${value}`).join(", ") || "—"}</span>,
          }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
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
    </>
  );
}
