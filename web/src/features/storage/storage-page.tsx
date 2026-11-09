import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Database, Eye, FileCode2, HardDrive, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { PersistentVolume, PersistentVolumeClaim, PersistentVolumeClaimDetail, StorageClass } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ActionTooltip } from "@/components/feedback/action-tooltip";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCapabilities } from "@/features/auth/use-session";
import { PersistentVolumeClaimFormDialog } from "@/features/storage/persistent-volume-claim-form-dialog";
import { cursorPaginationProps, retryListQuery, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge, formatMemory } from "@/lib/utils";

const classColumn = createColumnHelper<StorageClass>();
const pvColumn = createColumnHelper<PersistentVolume>();
const pvcColumn = createColumnHelper<PersistentVolumeClaim>();
type YamlTarget = { type: "storageclass" | "persistentvolume" | "persistentvolumeclaim"; name: string; namespace?: string };

export function StoragePage() {
  const search = useSearch({ from: "/app/storage" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const tab = search.tab === "volumes" || search.tab === "claims" ? search.tab : "classes";
  const classPager = useCursorPagination(`storageclasses|${q}`, 20);
  const volumePager = useCursorPagination(`persistentvolumes|${q}`, 20);
  const claimPager = useCursorPagination(`persistentvolumeclaims|${namespace}|${q}`, 20);
  const [claimFormOpen, setClaimFormOpen] = useState(false);
  const [editingClaim, setEditingClaim] = useState<PersistentVolumeClaimDetail>();
  const [deletingClaim, setDeletingClaim] = useState<PersistentVolumeClaim>();
  const [yamlTarget, setYamlTarget] = useState<YamlTarget>();
  const updateSearch = (patch: { q?: string; namespace?: string; tab?: string }) => {
    void navigate({ to: "/storage", search: { ...search, ...patch }, replace: true });
  };
  const classes = useQuery({
    queryKey: ["storageclasses", q, classPager.page, classPager.cursor],
    queryFn: () => api.storageclasses({ q, page: classPager.page, pageSize: classPager.pageSize, cursor: classPager.cursor }),
  });
  const volumes = useQuery({
    queryKey: ["persistentvolumes", q, volumePager.page, volumePager.cursor],
    queryFn: () => api.persistentvolumes({ q, page: volumePager.page, pageSize: volumePager.pageSize, cursor: volumePager.cursor }),
  });
  const claims = useQuery({
    queryKey: ["persistentvolumeclaims", namespace, q, claimPager.page, claimPager.cursor],
    queryFn: () => api.persistentvolumeclaims({ namespace, q, page: claimPager.page, pageSize: claimPager.pageSize, cursor: claimPager.cursor }),
  });
  const capabilities = useCapabilities(namespace || undefined).data;
  const claimCaps = resourceCapabilities(capabilities, "persistentvolumeclaims");
  const noClasses = !classes.isLoading && !classes.isError && !classes.data?.items.length;
  const claimCreateHint = !namespace ? "请先选择命名空间，再创建 PVC" : !claimCaps.create ? "当前身份没有创建 PVC 的权限" : undefined;
  const refreshClaims = async () => { await claims.refetch(); };
  const openClaimEdit = async (item: PersistentVolumeClaim) => {
    try {
      setEditingClaim(await api.persistentvolumeclaim(item.namespace, item.name));
      setClaimFormOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载 PVC 失败");
    }
  };
  const classActions = (item: StorageClass): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/classes/$name", params: { name: item.name }, search: {} }) },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "storageclass", name: item.name }) },
  ];
  const volumeActions = (item: PersistentVolume): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/volumes/$name", params: { name: item.name }, search: {} }) },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "persistentvolume", name: item.name }) },
  ];
  const claimActions = (item: PersistentVolumeClaim): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/claims/$namespace/$name", params: { namespace: item.namespace, name: item.name }, search: {} }) },
    { label: "扩容 PVC", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openClaimEdit(item), disabled: !claimCaps.update },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "persistentvolumeclaim", name: item.name, namespace: item.namespace }) },
    { label: "删除 PVC", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingClaim(item), destructive: true, disabled: !claimCaps.delete, separatorBefore: true },
  ];
  return (
    <>
      <PageHeader
        title="存储"
        breadcrumb="存储 / 持久化存储"
        description="查看 StorageClass、PersistentVolume 和 PersistentVolumeClaim。"
        actions={<ActionTooltip content={claimCreateHint}><Button onClick={() => { setEditingClaim(undefined); setClaimFormOpen(true); }} disabled={Boolean(claimCreateHint)}><Plus className="h-4 w-4" />创建 PVC</Button></ActionTooltip>}
      />
      {noClasses ? <div className="mb-4 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-sm text-warning"><HardDrive className="mr-2 inline h-4 w-4" />当前集群没有 StorageClass，不支持动态持久化卷申请。已有 PV/PVC 仍可正常查看。</div> : null}
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索存储资源" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>
      <Tabs value={tab} onValueChange={(value) => updateSearch({ tab: value === "classes" ? undefined : value })}>
        <TabsList><TabsTrigger value="classes">StorageClass ({classes.data?.meta.totalKnown ? classes.data.meta.total : classes.data?.items.length ?? 0})</TabsTrigger><TabsTrigger value="volumes">PV ({volumes.data?.meta.totalKnown ? volumes.data.meta.total : volumes.data?.items.length ?? 0})</TabsTrigger><TabsTrigger value="claims">PVC ({claims.data?.meta.totalKnown ? claims.data.meta.total : claims.data?.items.length ?? 0})</TabsTrigger></TabsList>
        <TabsContent value="classes">
          {classes.isError ? <ErrorState error={classes.error} onRetry={() => retryListQuery(classPager, classes.error, classes.refetch)} /> : noClasses ? <EmptyState title="没有 StorageClass" description="当前集群未安装动态存储供应器。需要持久卷时请先安装 CSI Driver 并创建 StorageClass。" /> : (
            <DataTable columns={[
              classColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/classes/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              classColumn.accessor("provisioner", { header: "Provisioner", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
              classColumn.accessor("default", { header: "默认", cell: (info) => info.getValue() ? <Badge>Default</Badge> : "—" }),
              classColumn.accessor("reclaimPolicy", { header: "回收策略" }),
              classColumn.accessor("volumeBinding", { header: "绑定模式" }),
              classColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              classColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`StorageClass ${info.row.original.name}`} actions={classActions(info.row.original)} /> }),
            ]} data={classes.data?.items ?? []} loading={classes.isLoading} pagination={cursorPaginationProps(classPager, classes.data?.meta, classes.data?.items.length ?? 0)} empty="没有 StorageClass" />
          )}
        </TabsContent>
        <TabsContent value="volumes">
          {volumes.isError ? <ErrorState error={volumes.error} onRetry={() => retryListQuery(volumePager, volumes.error, volumes.refetch)} /> : !volumes.isLoading && !volumes.data?.items.length ? <EmptyState title="没有 PersistentVolume" description="当前集群没有静态或动态创建的 PV。" /> : (
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
              pvColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`PersistentVolume ${info.row.original.name}`} actions={volumeActions(info.row.original)} /> }),
            ]} data={volumes.data?.items ?? []} loading={volumes.isLoading} pagination={cursorPaginationProps(volumePager, volumes.data?.meta, volumes.data?.items.length ?? 0)} empty="没有 PersistentVolume" />
          )}
        </TabsContent>
        <TabsContent value="claims">
          {claims.isError ? <ErrorState error={claims.error} onRetry={() => retryListQuery(claimPager, claims.error, claims.refetch)} /> : !claims.isLoading && !claims.data?.items.length ? <EmptyState title="没有 PersistentVolumeClaim" description="当前筛选条件下没有 PVC。没有 StorageClass 时，动态卷申请会保持 Pending。" /> : (
            <DataTable columns={[
              pvcColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/claims/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              pvcColumn.accessor("namespace", { header: "命名空间" }),
              pvcColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              pvcColumn.accessor("capacityBytes", { header: "容量", cell: (info) => formatMemory(info.getValue()) }),
              pvcColumn.accessor("volumeName", { header: "PV", cell: (info) => info.getValue() ? <Link to="/storage/volumes/$name" params={{ name: info.getValue()! }} search={{}} className="font-mono text-xs text-primary underline underline-offset-2">{info.getValue()}</Link> : <span className="font-mono text-xs">—</span> }),
              pvcColumn.accessor("storageClass", { header: "StorageClass" }),
              pvcColumn.accessor("accessModes", { header: "访问模式", enableSorting: false, cell: (info) => <span className="font-mono text-xs">{info.getValue().join(", ")}</span> }),
              pvcColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              pvcColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`PVC ${info.row.original.namespace}/${info.row.original.name}`} actions={claimActions(info.row.original)} /> }),
            ]} data={claims.data?.items ?? []} loading={claims.isLoading} pagination={cursorPaginationProps(claimPager, claims.data?.meta, claims.data?.items.length ?? 0)} empty={<div className="flex flex-col items-center gap-2"><Database className="h-6 w-6" /><span>没有 PVC</span></div>} />
          )}
        </TabsContent>
      </Tabs>
      <PersistentVolumeClaimFormDialog
        open={claimFormOpen}
        onOpenChange={(open) => { setClaimFormOpen(open); if (!open) setEditingClaim(undefined); }}
        namespace={editingClaim?.namespace || namespace}
        item={editingClaim}
        storageClasses={classes.data?.items ?? []}
        onSaved={refreshClaims}
      />
      <DeleteResourceDialog
        open={Boolean(deletingClaim)}
        onOpenChange={(open) => { if (!open) setDeletingClaim(undefined); }}
        resourceKind="PersistentVolumeClaim"
        name={deletingClaim?.name ?? ""}
        description="删除 PVC 后，绑定 PV 是否保留取决于回收策略；正在挂载该卷的工作负载可能中断。"
        onConfirm={async () => { if (!deletingClaim) return; await api.deletePersistentVolumeClaim(deletingClaim.namespace, deletingClaim.name, deletingClaim.resourceVersion); await refreshClaims(); }}
      />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type={yamlTarget?.type ?? "persistentvolumeclaim"} name={yamlTarget?.name ?? ""} namespace={yamlTarget?.namespace} />
    </>
  );
}
