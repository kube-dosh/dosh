import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { ArrowUpRight, Blocks, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import type { ResourceDescriptor } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { SearchInput } from "@/components/layout/search-input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

const column = createColumnHelper<ResourceDescriptor>();
const ALL_GROUPS = "__all__";

function groupLabel(group: string): string {
  return group || "core";
}

function resourcePath(descriptor: ResourceDescriptor, namespace: string) {
  return {
    to: "/resources/$group/$version/$resource" as const,
    params: {
      group: descriptor.group || "_core",
      version: descriptor.version,
      resource: descriptor.resource,
    },
    search: { namespace: namespace || undefined },
  };
}

export function ResourcesPage() {
  const search = useSearch({ from: "/app/resources" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const groupParam = search.group;
  const group = groupParam === undefined || groupParam === "_core" ? "" : groupParam;
  const query = useQuery({
    queryKey: ["resource-discovery", "namespace", namespace],
    queryFn: () => api.resourceDiscovery(namespace || undefined),
    staleTime: 60_000,
  });
  const items = query.data?.items ?? [];
  const groups = useMemo(() => Array.from(new Set(items.map((item) => item.group))).sort((left, right) => {
    if (left === right) return 0;
    if (!left) return -1;
    if (!right) return 1;
    return left.localeCompare(right);
  }), [items]);
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return items.filter((item) => {
      if (groupParam !== undefined && item.group !== group) return false;
      if (!needle) return true;
      return [item.kind, item.singularName, item.resource, item.group, item.version, ...(item.shortNames ?? []), ...(item.categories ?? [])]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle));
    });
  }, [group, groupParam, items, q]);
  const updateSearch = (patch: { q?: string; group?: string; namespace?: string }) => {
    void navigate({ to: "/resources", search: { ...search, ...patch }, replace: true });
  };

  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  return (
    <>
      <PageHeader
        title="资源浏览器"
        breadcrumb="集群 / 资源浏览器"
        description="通过 Kubernetes API Discovery 浏览集群暴露的内置资源与 CRD 类型；对象读取仍由当前身份的 Kubernetes RBAC 决定。"
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索 Kind、资源名、API Group 或短名称" />
        <Select value={groupParam ?? ALL_GROUPS} onValueChange={(value) => updateSearch({ group: value === ALL_GROUPS ? undefined : value })}>
          <SelectTrigger className="w-full sm:w-56" aria-label="API Group 筛选"><SelectValue placeholder="全部 API Group" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_GROUPS}>全部 API Group</SelectItem>
            {groups.map((item) => <SelectItem key={item || "_core"} value={item || "_core"}>{groupLabel(item)}</SelectItem>)}
          </SelectContent>
        </Select>
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} />
        <span className="text-xs text-muted-foreground sm:ml-auto">共 {filtered.length} 个资源类型</span>
      </div>
      {query.data?.warnings?.length ? (
        <div className="mb-3 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{query.data.warnings.join("；")}</span>
        </div>
      ) : null}
      <DataTable
        columns={[
          column.accessor("kind", {
            header: "Kind",
            cell: (info) => (
              <Link {...resourcePath(info.row.original, namespace)} className="inline-flex items-center gap-1 font-medium text-primary hover:underline">
                {info.getValue()}<ArrowUpRight className="h-3 w-3" aria-hidden="true" />
              </Link>
            ),
          }),
          column.accessor("resource", { header: "资源名", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("group", {
            header: "API Group / Version",
            cell: (info) => <span className={cn("font-mono text-xs", !info.getValue() && "text-muted-foreground")}>{groupLabel(info.getValue())}/{info.row.original.version}</span>,
          }),
          column.accessor("namespaced", {
            header: "作用域",
            cell: (info) => <Badge variant={info.getValue() ? "secondary" : "outline"}>{info.getValue() ? "Namespaced" : "Cluster"}</Badge>,
          }),
          column.accessor("shortNames", {
            header: "短名称",
            enableSorting: false,
            cell: (info) => <span className="font-mono text-xs text-muted-foreground">{info.getValue()?.join(", ") || "—"}</span>,
          }),
          column.accessor("verbs", {
            header: "支持动词",
            enableSorting: false,
            cell: (info) => <span className="block max-w-64 truncate font-mono text-[11px] text-muted-foreground">{info.getValue().join(", ")}</span>,
          }),
        ]}
        data={filtered}
        loading={query.isLoading}
        stickyFirstColumn={false}
        empty={<div className="flex flex-col items-center gap-2"><Blocks className="h-6 w-6" /><span>没有匹配的资源类型</span></div>}
      />
    </>
  );
}
