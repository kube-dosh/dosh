import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Database, HardDrive } from "lucide-react";
import { api } from "@/api/client";
import type { PersistentVolume, PersistentVolumeClaim, StorageClass } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { formatAge, formatMemory } from "@/lib/utils";

const classColumn = createColumnHelper<StorageClass>();
const pvColumn = createColumnHelper<PersistentVolume>();
const pvcColumn = createColumnHelper<PersistentVolumeClaim>();

export function StoragePage() {
  const search = useSearch({ from: "/app/storage" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const tab = search.tab === "volumes" || search.tab === "claims" ? search.tab : "classes";
  const updateSearch = (patch: { q?: string; namespace?: string; tab?: string }) => {
    void navigate({ to: "/storage", search: { ...search, ...patch }, replace: true });
  };
  const classes = useQuery({ queryKey: ["storageclasses", q], queryFn: () => api.storageclasses({ q }) });
  const volumes = useQuery({ queryKey: ["persistentvolumes", q], queryFn: () => api.persistentvolumes({ q }) });
  const claims = useQuery({ queryKey: ["persistentvolumeclaims", namespace, q], queryFn: () => api.persistentvolumeclaims({ namespace, q }) });
  const error = classes.error || volumes.error || claims.error;
  if (error) return <ErrorState error={error} onRetry={() => { void classes.refetch(); void volumes.refetch(); void claims.refetch(); }} />;
  const noClasses = !classes.isLoading && !classes.data?.items.length;
  return (
    <>
      <PageHeader title="存储" breadcrumb="存储 / 持久化存储" description="查看 StorageClass、PersistentVolume 和 PersistentVolumeClaim。" />
      {noClasses ? <div className="mb-4 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-sm text-warning"><HardDrive className="mr-2 inline h-4 w-4" />当前集群没有 StorageClass，不支持动态持久化卷申请。已有 PV/PVC 仍可正常查看。</div> : null}
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索存储资源" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>
      <Tabs value={tab} onValueChange={(value) => updateSearch({ tab: value === "classes" ? undefined : value })}>
        <TabsList><TabsTrigger value="classes">StorageClass ({classes.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="volumes">PV ({volumes.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="claims">PVC ({claims.data?.meta.total ?? 0})</TabsTrigger></TabsList>
        <TabsContent value="classes">
          {noClasses ? <EmptyState title="没有 StorageClass" description="当前集群未安装动态存储供应器。需要持久卷时请先安装 CSI Driver 并创建 StorageClass。" /> : (
            <DataTable columns={[
              classColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/classes/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              classColumn.accessor("provisioner", { header: "Provisioner", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
              classColumn.accessor("default", { header: "默认", cell: (info) => info.getValue() ? <Badge>Default</Badge> : "—" }),
              classColumn.accessor("reclaimPolicy", { header: "回收策略" }),
              classColumn.accessor("volumeBinding", { header: "绑定模式" }),
              classColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
            ]} data={classes.data?.items ?? []} loading={classes.isLoading} empty="没有 StorageClass" />
          )}
        </TabsContent>
        <TabsContent value="volumes">
          {!volumes.isLoading && !volumes.data?.items.length ? <EmptyState title="没有 PersistentVolume" description="当前集群没有静态或动态创建的 PV。" /> : (
            <DataTable columns={[
              pvColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/volumes/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              pvColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              pvColumn.accessor("capacityBytes", { header: "容量", cell: (info) => formatMemory(info.getValue()) }),
              pvColumn.accessor("accessModes", { header: "访问模式", enableSorting: false, cell: (info) => <span className="font-mono text-xs">{info.getValue().join(", ")}</span> }),
              pvColumn.accessor("claim", { header: "绑定的 PVC", cell: (info) => {
                const claim = info.getValue();
                const [namespace, name] = claim?.split("/") ?? [];
                return namespace && name ? <Link to="/storage/claims/$namespace/$name" params={{ namespace, name }} search={{}} className="font-mono text-xs text-primary underline underline-offset-2">{claim}</Link> : <span className="font-mono text-xs">—</span>;
              } }),
              pvColumn.accessor("storageClass", { header: "StorageClass" }),
              pvColumn.accessor("reclaimPolicy", { header: "回收策略" }),
              pvColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
            ]} data={volumes.data?.items ?? []} loading={volumes.isLoading} empty="没有 PersistentVolume" />
          )}
        </TabsContent>
        <TabsContent value="claims">
          {!claims.isLoading && !claims.data?.items.length ? <EmptyState title="没有 PersistentVolumeClaim" description="当前筛选条件下没有 PVC。没有 StorageClass 时，动态卷申请会保持 Pending。" /> : (
            <DataTable columns={[
              pvcColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/claims/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              pvcColumn.accessor("namespace", { header: "命名空间" }),
              pvcColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              pvcColumn.accessor("capacityBytes", { header: "容量", cell: (info) => formatMemory(info.getValue()) }),
              pvcColumn.accessor("volumeName", { header: "PV", cell: (info) => info.getValue() ? <Link to="/storage/volumes/$name" params={{ name: info.getValue()! }} search={{}} className="font-mono text-xs text-primary underline underline-offset-2">{info.getValue()}</Link> : <span className="font-mono text-xs">—</span> }),
              pvcColumn.accessor("storageClass", { header: "StorageClass" }),
              pvcColumn.accessor("accessModes", { header: "访问模式", enableSorting: false, cell: (info) => <span className="font-mono text-xs">{info.getValue().join(", ")}</span> }),
              pvcColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
            ]} data={claims.data?.items ?? []} loading={claims.isLoading} empty={<div className="flex flex-col items-center gap-2"><Database className="h-6 w-6" /><span>没有 PVC</span></div>} />
          )}
        </TabsContent>
      </Tabs>
    </>
  );
}
