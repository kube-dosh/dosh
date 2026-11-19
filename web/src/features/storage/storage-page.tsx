import { lazy, Suspense, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Camera, Database, Eye, FileCode2, HardDrive, Pencil, Plus, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { PersistentVolume, PersistentVolumeClaim, PersistentVolumeClaimDetail, PersistentVolumeDetail, StorageClass, StorageClassDetail, VolumeSnapshot } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ActionTooltip } from "@/components/feedback/action-tooltip";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
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
import { cursorPaginationProps, retryListQuery, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge, formatMemory } from "@/lib/utils";

const classColumn = createColumnHelper<StorageClass>();
const pvColumn = createColumnHelper<PersistentVolume>();
const pvcColumn = createColumnHelper<PersistentVolumeClaim>();
const snapshotColumn = createColumnHelper<VolumeSnapshot>();
type YamlTarget = { type: "storageclass" | "persistentvolume" | "persistentvolumeclaim"; name: string; namespace?: string };
const storageTabs = ["classes", "volumes", "claims", "snapshots"] as const;
type StorageTab = (typeof storageTabs)[number];
const PersistentVolumeClaimFormDialog = lazy(() => import("@/features/storage/persistent-volume-claim-form-dialog").then((module) => ({ default: module.PersistentVolumeClaimFormDialog })));
const PersistentVolumeFormDialog = lazy(() => import("@/features/storage/persistent-volume-form-dialog").then((module) => ({ default: module.PersistentVolumeFormDialog })));
const StorageClassFormDialog = lazy(() => import("@/features/storage/storage-class-form-dialog").then((module) => ({ default: module.StorageClassFormDialog })));
const VolumeSnapshotFormDialog = lazy(() => import("@/features/storage/volume-snapshot-form-dialog").then((module) => ({ default: module.VolumeSnapshotFormDialog })));

function isStorageTab(value: string | undefined): value is StorageTab {
  return storageTabs.includes(value as StorageTab);
}

export function StoragePage() {
  const search = useSearch({ from: "/app/storage" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const tab: StorageTab = isStorageTab(search.tab) ? search.tab : "classes";
  const classPager = useCursorPagination(`storageclasses|${q}`, 20);
  const volumePager = useCursorPagination(`persistentvolumes|${q}`, 20);
  const claimPager = useCursorPagination(`persistentvolumeclaims|${namespace}|${q}`, 20);
  const [claimFormOpen, setClaimFormOpen] = useState(false);
  const [editingClaim, setEditingClaim] = useState<PersistentVolumeClaimDetail>();
  const [restoringSnapshot, setRestoringSnapshot] = useState<VolumeSnapshot>();
  const [deletingClaim, setDeletingClaim] = useState<PersistentVolumeClaim>();
  const [classFormOpen, setClassFormOpen] = useState(false);
  const [editingClass, setEditingClass] = useState<StorageClassDetail>();
  const [deletingClass, setDeletingClass] = useState<StorageClass>();
  const [volumeFormOpen, setVolumeFormOpen] = useState(false);
  const [editingVolume, setEditingVolume] = useState<PersistentVolumeDetail>();
  const [deletingVolume, setDeletingVolume] = useState<PersistentVolume>();
  const [snapshotForm, setSnapshotForm] = useState<{ namespace: string; sourcePvc?: string }>();
  const [deletingSnapshot, setDeletingSnapshot] = useState<VolumeSnapshot>();
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
  const snapshotFeature = useQuery({ queryKey: ["volume-snapshot-feature"], queryFn: api.volumeSnapshotFeature, staleTime: 60_000, retry: false });
  const snapshots = useQuery({
    queryKey: ["volume-snapshots", namespace, q],
    queryFn: () => api.volumeSnapshots({ namespace, q }),
    enabled: snapshotFeature.data?.available === true,
  });
  const snapshotClaims = useQuery({
    queryKey: ["volume-snapshot-source-claims", snapshotForm?.namespace],
    queryFn: () => api.persistentvolumeclaims({ namespace: snapshotForm?.namespace, pageSize: 200 }),
    enabled: Boolean(snapshotForm?.namespace && snapshotFeature.data?.available),
  });
  const capabilities = useCapabilities(namespace || undefined).data;
  const classCaps = resourceCapabilities(capabilities, "storageclasses");
  const volumeCaps = resourceCapabilities(capabilities, "persistentvolumes");
  const claimCaps = resourceCapabilities(capabilities, "persistentvolumeclaims");
  const snapshotCaps = resourceCapabilities(capabilities, "volumesnapshots");
  const noClasses = !classes.isLoading && !classes.isError && !classes.data?.items.length;
  const claimCreateHint = !namespace ? "请先选择命名空间，再创建 PVC" : !claimCaps.create ? "当前身份没有创建 PVC 的权限" : undefined;
  const classCreateHint = !classCaps.create ? "当前身份没有创建 StorageClass 的权限" : undefined;
  const volumeCreateHint = !volumeCaps.create ? "当前身份没有创建 PersistentVolume 的权限" : undefined;
  const snapshotCreateHint = !snapshotCaps.create ? "当前身份没有创建 VolumeSnapshot 的权限" : !snapshotFeature.data?.available ? snapshotFeature.data?.reason || "VolumeSnapshot API 不可用" : !namespace ? "请先选择命名空间，再创建快照" : undefined;
  const refreshClaims = async () => { await claims.refetch(); };
  const refreshSnapshots = async () => {
    await snapshots.refetch();
    await queryClient.invalidateQueries({ queryKey: ["persistentvolumeclaim"] });
  };
  const openClaimCreate = () => { setEditingClaim(undefined); setRestoringSnapshot(undefined); setClaimFormOpen(true); };
  const openClaimEdit = async (item: PersistentVolumeClaim) => {
    try {
      setRestoringSnapshot(undefined);
      setEditingClaim(await api.persistentvolumeclaim(item.namespace, item.name));
      setClaimFormOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载 PVC 失败");
    }
  };
  const openClassCreate = () => { setEditingClass(undefined); setClassFormOpen(true); };
  const openClassEdit = async (item: StorageClass) => {
    try {
      setEditingClass(await api.storageclass(item.name));
      setClassFormOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载 StorageClass 失败");
    }
  };
  const openVolumeCreate = () => { setEditingVolume(undefined); setVolumeFormOpen(true); };
  const openVolumeEdit = async (item: PersistentVolume) => {
    try {
      setEditingVolume(await api.persistentvolume(item.name));
      setVolumeFormOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载 PersistentVolume 失败");
    }
  };
  const classActions = (item: StorageClass): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/classes/$name", params: { name: item.name }, search: {} }) },
    { label: "编辑 StorageClass", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openClassEdit(item), disabled: !classCaps.update },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "storageclass", name: item.name }) },
    { label: "删除 StorageClass", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingClass(item), destructive: true, disabled: !classCaps.delete || item.default, separatorBefore: true },
  ];
  const volumeActions = (item: PersistentVolume): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/volumes/$name", params: { name: item.name }, search: {} }) },
    { label: "编辑 PersistentVolume", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openVolumeEdit(item), disabled: !volumeCaps.update },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "persistentvolume", name: item.name }) },
    { label: "删除 PersistentVolume", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingVolume(item), destructive: true, disabled: !volumeCaps.delete, separatorBefore: true },
  ];
  const claimActions = (item: PersistentVolumeClaim): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/claims/$namespace/$name", params: { namespace: item.namespace, name: item.name }, search: {} }) },
    { label: "扩容 PVC", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openClaimEdit(item), disabled: !claimCaps.update },
    { label: "创建快照", icon: <Camera className="h-4 w-4" />, onSelect: () => setSnapshotForm({ namespace: item.namespace, sourcePvc: item.name }), disabled: !snapshotCaps.create || !snapshotFeature.data?.available },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "persistentvolumeclaim", name: item.name, namespace: item.namespace }) },
    { label: "删除 PVC", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingClaim(item), destructive: true, disabled: !claimCaps.delete, separatorBefore: true },
  ];
  const snapshotActions = (item: VolumeSnapshot): ResourceAction[] => [
    { label: "查看源 PVC", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/storage/claims/$namespace/$name", params: { namespace: item.namespace, name: item.sourcePvc }, search: {} }), disabled: !item.sourcePvc },
    { label: "从快照恢复 PVC", icon: <RotateCcw className="h-4 w-4" />, onSelect: () => { setRestoringSnapshot(item); setEditingClaim(undefined); setClaimFormOpen(true); }, disabled: !item.readyToUse || !claimCaps.create },
    { label: "删除快照", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingSnapshot(item), destructive: true, disabled: !snapshotCaps.delete, separatorBefore: true },
  ];
  const createAction = () => {
    if (tab === "classes") return <ActionTooltip content={classCreateHint}><Button onClick={openClassCreate} disabled={Boolean(classCreateHint)}><Plus className="h-4 w-4" />创建 StorageClass</Button></ActionTooltip>;
    if (tab === "volumes") return <ActionTooltip content={volumeCreateHint}><Button onClick={openVolumeCreate} disabled={Boolean(volumeCreateHint)}><Plus className="h-4 w-4" />创建 PV</Button></ActionTooltip>;
    if (tab === "snapshots") return <ActionTooltip content={snapshotCreateHint}><Button onClick={() => setSnapshotForm({ namespace })} disabled={Boolean(snapshotCreateHint)}><Plus className="h-4 w-4" />创建快照</Button></ActionTooltip>;
    return <ActionTooltip content={claimCreateHint}><Button onClick={openClaimCreate} disabled={Boolean(claimCreateHint)}><Plus className="h-4 w-4" />创建 PVC</Button></ActionTooltip>;
  };
  return (
    <>
      <PageHeader
        title="存储"
        breadcrumb="存储 / 持久化存储"
        description="管理 StorageClass、PersistentVolume、PersistentVolumeClaim 与 CSI 快照，并检查绑定和使用影响。"
        actions={createAction()}
      />
      {noClasses ? <div className="mb-4 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-sm text-warning"><HardDrive className="mr-2 inline h-4 w-4" />当前集群没有 StorageClass，不支持动态持久化卷申请。已有 PV/PVC 仍可正常查看。</div> : null}
      <div className="mb-3 flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索存储资源" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>
      <Tabs value={tab} onValueChange={(value) => { if (isStorageTab(value)) updateSearch({ tab: value === "classes" ? undefined : value }); }}>
        <TabsList><TabsTrigger value="classes">StorageClass ({classes.data?.meta.totalKnown ? classes.data.meta.total : classes.data?.items.length ?? 0})</TabsTrigger><TabsTrigger value="volumes">PV ({volumes.data?.meta.totalKnown ? volumes.data.meta.total : volumes.data?.items.length ?? 0})</TabsTrigger><TabsTrigger value="claims">PVC ({claims.data?.meta.totalKnown ? claims.data.meta.total : claims.data?.items.length ?? 0})</TabsTrigger><TabsTrigger value="snapshots">快照 ({snapshotFeature.data?.available ? (snapshots.data?.meta.totalKnown ? snapshots.data.meta.total : snapshots.data?.items.length ?? 0) : "不可用"})</TabsTrigger></TabsList>
        <TabsContent value="classes">
          {classes.isError ? <ErrorState error={classes.error} onRetry={() => retryListQuery(classPager, classes.error, classes.refetch)} /> : noClasses ? <EmptyState title="没有 StorageClass" description="当前集群未安装动态存储供应器。需要持久卷时请先安装 CSI Driver 并创建 StorageClass。" /> : (
            <DataTable columns={[
              classColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/classes/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              classColumn.accessor("provisioner", { header: "Provisioner", cell: (info) => <span className="break-all font-mono text-xs">{info.getValue()}</span> }),
              classColumn.accessor("default", { header: "默认", cell: (info) => info.getValue() ? <Badge>Default</Badge> : "—" }),
              classColumn.accessor("reclaimPolicy", { header: "回收策略" }),
              classColumn.accessor("volumeBinding", { header: "绑定模式" }),
              classColumn.accessor("allowVolumeExpansion", { header: "扩容", cell: (info) => info.getValue() ? <Badge variant="outline">允许</Badge> : "—" }),
              classColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              classColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`StorageClass ${info.row.original.name}`} actions={classActions(info.row.original)} /> }),
            ]} data={classes.data?.items ?? []} loading={classes.isLoading} pagination={cursorPaginationProps(classPager, classes.data?.meta, classes.data?.items.length ?? 0)} empty="没有 StorageClass" />
          )}
        </TabsContent>
        <TabsContent value="volumes">
          {volumes.isError ? <ErrorState error={volumes.error} onRetry={() => retryListQuery(volumePager, volumes.error, volumes.refetch)} /> : !volumes.isLoading && !volumes.data?.items.length ? <EmptyState title="没有 PersistentVolume" description="静态卷或动态供应器创建的 PV 会显示在这里。" /> : (
            <DataTable columns={[
              pvColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/storage/volumes/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary underline underline-offset-2">{info.getValue()}</Link> }),
              pvColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              pvColumn.accessor("capacityBytes", { header: "容量", cell: (info) => formatMemory(info.getValue()) }),
              pvColumn.accessor("accessModes", { header: "访问模式", enableSorting: false, cell: (info) => <span className="font-mono text-xs">{info.getValue().join(", ")}</span> }),
              pvColumn.accessor("claim", { header: "绑定的 PVC", cell: (info) => { const claim = info.getValue(); const [claimNamespace, name] = claim?.split("/") ?? []; return claimNamespace && name ? <Link to="/storage/claims/$namespace/$name" params={{ namespace: claimNamespace, name }} search={{}} className="break-all font-mono text-xs text-primary underline underline-offset-2">{claim}</Link> : <span className="font-mono text-xs">—</span>; } }),
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
              pvcColumn.accessor("volumeName", { header: "PV", cell: (info) => info.getValue() ? <Link to="/storage/volumes/$name" params={{ name: info.getValue()! }} search={{}} className="break-all font-mono text-xs text-primary underline underline-offset-2">{info.getValue()}</Link> : <span className="font-mono text-xs">—</span> }),
              pvcColumn.accessor("storageClass", { header: "StorageClass" }),
              pvcColumn.accessor("accessModes", { header: "访问模式", enableSorting: false, cell: (info) => <span className="font-mono text-xs">{info.getValue().join(", ")}</span> }),
              pvcColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              pvcColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`PVC ${info.row.original.namespace}/${info.row.original.name}`} actions={claimActions(info.row.original)} /> }),
            ]} data={claims.data?.items ?? []} loading={claims.isLoading} pagination={cursorPaginationProps(claimPager, claims.data?.meta, claims.data?.items.length ?? 0)} empty={<div className="flex flex-col items-center gap-2"><Database className="h-6 w-6" /><span>没有 PVC</span></div>} />
          )}
        </TabsContent>
        <TabsContent value="snapshots">
          {snapshotFeature.isLoading ? <LoadingState label="正在探测 VolumeSnapshot 能力" /> : snapshotFeature.isError ? <ErrorState error={snapshotFeature.error} onRetry={() => void snapshotFeature.refetch()} /> : !snapshotFeature.data?.available ? <EmptyState title="CSI 快照能力不可用" description={snapshotFeature.data?.reason || "当前集群未安装 VolumeSnapshot CRD。"} /> : snapshots.isError ? <ErrorState error={snapshots.error} onRetry={() => void snapshots.refetch()} /> : !snapshots.isLoading && !snapshots.data?.items.length ? <EmptyState title="没有 VolumeSnapshot" description={namespace ? "当前命名空间没有快照。可从 PVC 操作菜单创建快照。" : "当前可见范围内没有快照。选择命名空间或从 PVC 创建快照。"} /> : (
            <DataTable columns={[
              snapshotColumn.accessor("name", { header: "名称", cell: (info) => <span className="break-all font-medium">{info.getValue()}</span> }),
              snapshotColumn.accessor("namespace", { header: "命名空间" }),
              snapshotColumn.accessor("sourcePvc", { header: "源 PVC", cell: (info) => info.getValue() ? <Link to="/storage/claims/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} search={{}} className="break-all font-mono text-xs text-primary underline underline-offset-2">{info.getValue()}</Link> : "—" }),
              snapshotColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              snapshotColumn.accessor("volumeSnapshotClassName", { header: "快照类", cell: (info) => <span className="break-all font-mono text-xs">{info.getValue() || "默认"}</span> }),
              snapshotColumn.accessor("restoreSizeBytes", { header: "恢复大小", cell: (info) => info.getValue() ? formatMemory(info.getValue()!) : "—" }),
              snapshotColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              snapshotColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`VolumeSnapshot ${info.row.original.namespace}/${info.row.original.name}`} actions={snapshotActions(info.row.original)} /> }),
            ]} data={snapshots.data?.items ?? []} loading={snapshots.isLoading} empty="没有 VolumeSnapshot" />
          )}
        </TabsContent>
      </Tabs>
      {claimFormOpen ? <Suspense fallback={null}><PersistentVolumeClaimFormDialog open onOpenChange={(open) => { setClaimFormOpen(open); if (!open) { setEditingClaim(undefined); setRestoringSnapshot(undefined); } }} namespace={restoringSnapshot?.namespace || editingClaim?.namespace || namespace} item={editingClaim} snapshot={restoringSnapshot} storageClasses={classes.data?.items ?? []} onSaved={refreshClaims} /></Suspense> : null}
      {classFormOpen ? <Suspense fallback={null}><StorageClassFormDialog open onOpenChange={(open) => { setClassFormOpen(open); if (!open) setEditingClass(undefined); }} item={editingClass} onSaved={async () => { await classes.refetch(); await queryClient.invalidateQueries({ queryKey: ["storageclass"] }); }} /></Suspense> : null}
      {volumeFormOpen ? <Suspense fallback={null}><PersistentVolumeFormDialog open onOpenChange={(open) => { setVolumeFormOpen(open); if (!open) setEditingVolume(undefined); }} item={editingVolume} storageClasses={classes.data?.items ?? []} onSaved={async () => { await volumes.refetch(); await queryClient.invalidateQueries({ queryKey: ["persistentvolume"] }); }} /></Suspense> : null}
      {snapshotForm ? <Suspense fallback={null}><VolumeSnapshotFormDialog open onOpenChange={(open) => { if (!open) setSnapshotForm(undefined); }} namespace={snapshotForm.namespace} sourcePVC={snapshotForm.sourcePvc} claims={snapshotClaims.data?.items ?? []} feature={snapshotFeature.data} onSaved={async () => { await refreshSnapshots(); await refreshClaims(); }} /></Suspense> : null}
      <DeleteResourceDialog open={Boolean(deletingClaim)} onOpenChange={(open) => { if (!open) setDeletingClaim(undefined); }} resourceKind="PersistentVolumeClaim" name={deletingClaim?.name ?? ""} description="删除 PVC 后，绑定 PV 是否保留取决于回收策略；正在挂载该卷的工作负载可能中断。快照不会自动删除。" onConfirm={async () => { if (!deletingClaim) return; await api.deletePersistentVolumeClaim(deletingClaim.namespace, deletingClaim.name, deletingClaim.resourceVersion); await refreshClaims(); }} />
      <DeleteResourceDialog open={Boolean(deletingClass)} onOpenChange={(open) => { if (!open) setDeletingClass(undefined); }} resourceKind="StorageClass" name={deletingClass?.name ?? ""} description="删除 StorageClass 不会删除已有 PV/PVC，但后续动态供应会失败；默认 StorageClass 和 kube-* 受保护。" riskLevel="high" riskReasons={["使用该类的现有 PVC 仍保留，但新建 PVC 和扩容策略检查将受影响。"]} onConfirm={async () => { if (!deletingClass) return; await api.deleteStorageClass(deletingClass.name, deletingClass.resourceVersion); await classes.refetch(); }} />
      <DeleteResourceDialog open={Boolean(deletingVolume)} onOpenChange={(open) => { if (!open) setDeletingVolume(undefined); }} resourceKind="PersistentVolume" name={deletingVolume?.name ?? ""} description="删除 PV 可能导致绑定 PVC 丢失底层卷访问；回收策略和 CSI 驱动会决定数据是否保留。" riskLevel={deletingVolume?.status === "Bound" ? "critical" : "high"} riskReasons={deletingVolume?.status === "Bound" ? ["该 PV 已绑定 PVC，删除会中断使用它的工作负载并可能造成数据不可访问。", "Kubernetes 可能通过 PV protection finalizer 延迟删除，直到绑定解除。"] : ["删除后静态卷不会被 Dosh 自动恢复。"]} onConfirm={async () => { if (!deletingVolume) return; await api.deletePersistentVolume(deletingVolume.name, deletingVolume.resourceVersion); await volumes.refetch(); }} />
      <DeleteResourceDialog open={Boolean(deletingSnapshot)} onOpenChange={(open) => { if (!open) setDeletingSnapshot(undefined); }} resourceKind="VolumeSnapshot" name={deletingSnapshot?.name ?? ""} description="删除快照后无法再使用它恢复 PVC。源 PVC 不受影响；底层 VolumeSnapshotContent 是否保留取决于删除策略。" riskLevel="high" riskReasons={["快照删除通常不可撤销，请确认不需要用于恢复或审计。"]} onConfirm={async () => { if (!deletingSnapshot) return; await api.deleteVolumeSnapshot(deletingSnapshot.namespace, deletingSnapshot.name, deletingSnapshot.resourceVersion); await refreshSnapshots(); }} />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type={yamlTarget?.type ?? "persistentvolumeclaim"} name={yamlTarget?.name ?? ""} namespace={yamlTarget?.namespace} />
    </>
  );
}
