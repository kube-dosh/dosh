import { lazy, Suspense, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Camera, Database, FileCode2, HardDrive, Pencil, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { api } from "@/api/client";
import type { Condition, PersistentVolumeClaimDetail, PersistentVolumeDetail, StorageClassDetail } from "@/api/types";
import { DetailItem } from "@/components/data/detail-item";
import { ActionTooltip } from "@/components/feedback/action-tooltip";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { MetadataCard } from "@/components/resources/metadata-card";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { YamlPanel } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ResourceEvents } from "@/features/events/resource-events";
import { useCapabilities } from "@/features/auth/use-session";
import { DiagnoseButton } from "@/features/diagnostics/diagnose-button";
import { StorageImpactCard } from "@/features/storage/storage-impact-card";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatDate, formatMemory } from "@/lib/utils";

const storageTabs = ["overview", "events", "yaml"] as const;
type StorageTab = (typeof storageTabs)[number];
const PersistentVolumeClaimFormDialog = lazy(() => import("@/features/storage/persistent-volume-claim-form-dialog").then((module) => ({ default: module.PersistentVolumeClaimFormDialog })));
const PersistentVolumeFormDialog = lazy(() => import("@/features/storage/persistent-volume-form-dialog").then((module) => ({ default: module.PersistentVolumeFormDialog })));
const StorageClassFormDialog = lazy(() => import("@/features/storage/storage-class-form-dialog").then((module) => ({ default: module.StorageClassFormDialog })));
const VolumeSnapshotFormDialog = lazy(() => import("@/features/storage/volume-snapshot-form-dialog").then((module) => ({ default: module.VolumeSnapshotFormDialog })));

function isStorageTab(value: string | undefined): value is StorageTab {
  return storageTabs.includes(value as StorageTab);
}

function StorageTabs({ tab, onChange, events, yaml }: {
  tab: StorageTab;
  onChange: (value: string) => void;
  events: ReactNode;
  yaml: ReactNode;
}) {
  return (
    <Tabs value={tab} onValueChange={onChange}>
      <TabsList>
        <TabsTrigger value="overview">概览</TabsTrigger>
        <TabsTrigger value="events">事件</TabsTrigger>
        <TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger>
      </TabsList>
      <TabsContent value="events">{events}</TabsContent>
      <TabsContent value="yaml">{yaml}</TabsContent>
    </Tabs>
  );
}

function KeyValueCard({ title, data }: { title: string; data?: Record<string, string> }) {
  const entries = Object.entries(data || {});
  return (
    <Card>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent>
        {entries.length ? (
          <dl className="grid gap-4 sm:grid-cols-2">
            {entries.map(([key, value]) => <DetailItem key={key} label={key} value={value} mono />)}
          </dl>
        ) : <p className="py-6 text-center text-sm text-muted-foreground">未配置</p>}
      </CardContent>
    </Card>
  );
}

function StorageClassOverview({ item }: { item: StorageClassDetail }) {
  return (
    <>
      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader><CardTitle>StorageClass 信息</CardTitle></CardHeader>
          <CardContent><dl className="grid gap-5 sm:grid-cols-2">
            <DetailItem label="名称" value={item.name} mono />
            <DetailItem label="Provisioner" value={item.provisioner} mono />
            <DetailItem label="默认 StorageClass" value={item.default ? <Badge>Default</Badge> : "否"} />
            <DetailItem label="回收策略" value={item.reclaimPolicy} />
            <DetailItem label="绑定模式" value={item.volumeBinding} />
            <DetailItem label="允许扩容" value={item.allowVolumeExpansion ? "是" : "否"} />
            <DetailItem label="挂载选项" value={item.mountOptions?.join(", ") || "—"} mono />
            <DetailItem label="创建时间" value={formatDate(item.createdAt)} />
          </dl></CardContent>
        </Card>
        <KeyValueCard title="存储参数" data={item.parameters} />
      </div>
      <div className="mt-4"><MetadataCard labels={item.labels} annotations={item.annotations} /></div>
    </>
  );
}

export function StorageClassDetailPage() {
  const params = useParams({ strict: false }) as { name: string };
  const search = useSearch({ from: "/app/storage/classes/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const capabilities = useCapabilities().data;
  const caps = resourceCapabilities(capabilities, "storageclasses");
  const tab = isStorageTab(search.tab) ? search.tab : "overview";
  const query = useQuery({ queryKey: ["storageclass", params.name], queryFn: () => api.storageclass(params.name), refetchInterval: 20_000 });
  if (query.isLoading) return <LoadingState label="正在加载 StorageClass 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const item = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/storage" search={{ ...search, tab: "classes" }} className="text-primary underline underline-offset-2">存储</Link> / StorageClass / {item.name}</>}
        title={<span className="flex items-center gap-3"><Database className="h-5 w-5 text-muted-foreground" />{item.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2">{item.default ? <Badge>Default</Badge> : null}<span>{item.provisioner}</span><span>·</span><span>{item.volumeBinding}</span></span>}
        actions={<div className="flex flex-wrap items-center gap-2">{caps.update ? <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}><Pencil className="h-4 w-4" />编辑</Button> : null}{caps.delete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)} disabled={item.default}><Trash2 className="h-4 w-4" />删除</Button> : null}</div>}
      />
      <Tabs value={tab} onValueChange={(value) => {
        if (!isStorageTab(value)) return;
        void navigate({ to: "/storage/classes/$name", params, search: { ...search, tab: value === "overview" ? undefined : value }, replace: true });
      }}>
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="events">事件</TabsTrigger>
          <TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger>
        </TabsList>
        <TabsContent value="overview"><StorageClassOverview item={item} /></TabsContent>
        <TabsContent value="events"><ResourceEvents namespace="" objectKind="StorageClass" objectName={item.name} /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type="storageclass" name={item.name} height="64vh" /></TabsContent>
      </Tabs>
      {editOpen ? <Suspense fallback={null}><StorageClassFormDialog open onOpenChange={setEditOpen} item={item} onSaved={async () => { await query.refetch(); await queryClient.invalidateQueries({ queryKey: ["storageclasses"] }); }} /></Suspense> : null}
      <DeleteResourceDialog open={deleteOpen} onOpenChange={setDeleteOpen} resourceKind="StorageClass" name={item.name} description="删除 StorageClass 不会删除已有 PV/PVC，但后续动态供应会失败。默认 StorageClass 不允许删除。" riskLevel="high" riskReasons={["使用该类的现有 PVC 仍保留，但新建 PVC 和扩容策略检查将受影响。"]} onConfirm={async () => {
        await api.deleteStorageClass(item.name, item.resourceVersion);
        await queryClient.invalidateQueries({ queryKey: ["storageclasses"] });
        await navigate({ to: "/storage", search: { ...search, tab: "classes" } });
      }} />
    </>
  );
}

function PersistentVolumeOverview({ item }: { item: PersistentVolumeDetail }) {
  return (
    <>
      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader><CardTitle>PersistentVolume 信息</CardTitle></CardHeader>
          <CardContent><dl className="grid gap-5 sm:grid-cols-2">
            <DetailItem label="名称" value={item.name} mono />
            <DetailItem label="状态" value={<StatusBadge status={item.status} />} />
            <DetailItem label="容量" value={formatMemory(item.capacityBytes)} />
            <DetailItem label="访问模式" value={item.accessModes.join(", ")} mono />
            <DetailItem label="回收策略" value={item.reclaimPolicy} />
            <DetailItem label="卷模式" value={item.volumeMode} />
            <DetailItem label="StorageClass" value={item.storageClass ? <Link to="/storage/classes/$name" params={{ name: item.storageClass }} search={{}} className="text-primary underline underline-offset-2">{item.storageClass}</Link> : "—"} />
            <DetailItem label="绑定 PVC" value={item.claimNamespace && item.claimName ? <Link to="/storage/claims/$namespace/$name" params={{ namespace: item.claimNamespace, name: item.claimName }} search={{}} className="text-primary underline underline-offset-2">{item.claimNamespace}/{item.claimName}</Link> : "—"} />
            <DetailItem label="CSI Driver" value={item.csiDriver || "—"} mono />
            <DetailItem label="创建时间" value={formatDate(item.createdAt)} />
          </dl></CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>挂载与绑定</CardTitle></CardHeader>
          <CardContent><dl className="grid gap-5">
            <DetailItem label="挂载选项" value={item.mountOptions?.join(", ") || "—"} mono />
            <DetailItem label="Claim 命名空间" value={item.claimNamespace || "—"} mono />
            <DetailItem label="Claim 名称" value={item.claimName || "—"} mono />
            <DetailItem label="回收策略" value={item.reclaimPolicy} />
          </dl></CardContent>
        </Card>
      </div>
      <div className="mt-4"><StorageImpactCard impact={item.impact} title="PV 使用者与绑定影响" emptyMessage={item.claimName ? "该 PV 已绑定 PVC，但当前没有运行中的 Pod 挂载。" : "该 PV 尚未绑定 PVC。"} /></div>
      <div className="mt-4"><MetadataCard labels={item.labels} annotations={item.annotations} /></div>
    </>
  );
}

export function PersistentVolumeDetailPage() {
  const params = useParams({ strict: false }) as { name: string };
  const search = useSearch({ from: "/app/storage/volumes/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const capabilities = useCapabilities().data;
  const caps = resourceCapabilities(capabilities, "persistentvolumes");
  const tab = isStorageTab(search.tab) ? search.tab : "overview";
  const query = useQuery({ queryKey: ["persistentvolume", params.name], queryFn: () => api.persistentvolume(params.name), refetchInterval: 20_000 });
  const storageClasses = useQuery({ queryKey: ["storageclasses"], queryFn: () => api.storageclasses({ pageSize: 200 }), enabled: editOpen });
  if (query.isLoading) return <LoadingState label="正在加载 PersistentVolume 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const item = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/storage" search={{ ...search, tab: "volumes" }} className="text-primary underline underline-offset-2">存储</Link> / PersistentVolume / {item.name}</>}
        title={<span className="flex items-center gap-3"><HardDrive className="h-5 w-5 text-muted-foreground" />{item.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={item.status} /><span>{formatMemory(item.capacityBytes)}</span><span>·</span><span>{item.storageClass || "无 StorageClass"}</span></span>}
        actions={<div className="flex flex-wrap items-center gap-2">{caps.update ? <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}><Pencil className="h-4 w-4" />编辑</Button> : null}{caps.delete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}><Trash2 className="h-4 w-4" />删除</Button> : null}</div>}
      />
      <StorageTabs
        tab={tab}
        onChange={(value) => {
          if (!isStorageTab(value)) return;
          void navigate({ to: "/storage/volumes/$name", params, search: { ...search, tab: value === "overview" ? undefined : value }, replace: true });
        }}
        events={<ResourceEvents namespace="" objectKind="PersistentVolume" objectName={item.name} />}
        yaml={<YamlPanel type="persistentvolume" name={item.name} height="64vh" />}
      />
      {tab === "overview" ? <div className="mt-4"><PersistentVolumeOverview item={item} /></div> : null}
      {editOpen ? <Suspense fallback={null}><PersistentVolumeFormDialog open onOpenChange={setEditOpen} item={item} storageClasses={storageClasses.data?.items ?? []} onSaved={async () => { await query.refetch(); await queryClient.invalidateQueries({ queryKey: ["persistentvolumes"] }); }} /></Suspense> : null}
      <DeleteResourceDialog open={deleteOpen} onOpenChange={setDeleteOpen} resourceKind="PersistentVolume" name={item.name} description="删除 PV 可能导致绑定 PVC 丢失底层卷访问；回收策略和 CSI 驱动会决定数据是否保留。" riskLevel={item.status === "Bound" ? "critical" : "high"} riskReasons={item.status === "Bound" ? ["该 PV 已绑定 PVC，删除会中断使用它的工作负载并可能造成数据不可访问。", "Kubernetes 可能通过 PV protection finalizer 延迟删除，直到绑定解除。"] : ["删除后静态卷不会被 Dosh 自动恢复。"]} onConfirm={async () => {
        await api.deletePersistentVolume(item.name, item.resourceVersion);
        await queryClient.invalidateQueries({ queryKey: ["persistentvolumes"] });
        await navigate({ to: "/storage", search: { ...search, tab: "volumes" } });
      }} />
    </>
  );
}

function ConditionList({ conditions }: { conditions: Condition[] }) {
  return (
    <Card>
      <CardHeader><CardTitle>PVC 状态条件</CardTitle></CardHeader>
      <CardContent>
        {conditions.length ? <div className="space-y-2">{conditions.map((condition, index) => (
          <div key={`${condition.type}-${index}`} className="rounded-md border px-3 py-2">
            <div className="flex flex-wrap items-center gap-2"><StatusBadge status={condition.status} /><span className="font-medium">{condition.type}</span><span className="text-xs text-muted-foreground">{formatDate(condition.lastTransitionTime)}</span></div>
            {condition.reason || condition.message ? <p className="mt-1 text-sm text-muted-foreground">{condition.reason ? `${condition.reason}: ` : ""}{condition.message}</p> : null}
          </div>
        ))}</div> : <p className="py-6 text-center text-sm text-muted-foreground">暂无状态条件</p>}
      </CardContent>
    </Card>
  );
}

function ExpansionCard({ item }: { item: PersistentVolumeClaimDetail }) {
  const status = item.expansionStatus ?? (item.allowVolumeExpansion ? "ready" : "blocked");
  const tone = status === "ready" ? "border-success/35 bg-success/10" : status === "expanding" ? "border-warning/40 bg-warning/10" : "border-destructive/30 bg-destructive/5";
  const label = status === "ready" ? "可扩容" : status === "expanding" ? "扩容进行中" : "扩容受阻";
  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader><CardTitle>容量与扩容</CardTitle></CardHeader>
      <CardContent className="min-w-0 space-y-3">
        <div className={`min-w-0 rounded-md border px-3 py-2 ${tone}`}>
          <p className="font-medium">{label}</p>
          <p className="mt-1 break-words text-sm text-muted-foreground">{item.expansionMessage || (status === "blocked" ? "当前 PVC 不满足扩容前置条件。" : "扩容请求将由 CSI 驱动异步完成。")}</p>
        </div>
        <dl className="grid min-w-0 gap-4 sm:grid-cols-2">
          <DetailItem label="申请容量" value={formatMemory(item.requestedCapacityBytes)} />
          <DetailItem label="实际容量" value={formatMemory(item.capacityBytes)} />
          <DetailItem label="StorageClass 可读" value={item.storageClassAvailable ? "是" : "否"} />
          <DetailItem label="allowVolumeExpansion" value={item.allowVolumeExpansion ? "true" : "false"} mono />
        </dl>
        <p className="break-words text-xs text-muted-foreground">扩容只允许增加容量。提交前服务端会重新检查 StorageClass 策略、资源版本和使用者影响；驱动不支持在线扩容时可能需要重启 Pod 才能完成文件系统扩展。</p>
      </CardContent>
    </Card>
  );
}

function PersistentVolumeClaimOverview({ item }: { item: PersistentVolumeClaimDetail }) {
  return (
    <>
      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader><CardTitle>PersistentVolumeClaim 信息</CardTitle></CardHeader>
          <CardContent><dl className="grid gap-5 sm:grid-cols-2">
            <DetailItem label="名称" value={item.name} mono />
            <DetailItem label="命名空间" value={item.namespace} />
            <DetailItem label="状态" value={<StatusBadge status={item.status} />} />
            <DetailItem label="卷模式" value={item.volumeMode} />
            <DetailItem label="申请容量" value={formatMemory(item.requestedCapacityBytes)} />
            <DetailItem label="实际容量" value={formatMemory(item.capacityBytes)} />
            <DetailItem label="访问模式" value={item.accessModes.join(", ")} mono />
            <DetailItem label="StorageClass" value={item.storageClass ? <Link to="/storage/classes/$name" params={{ name: item.storageClass }} search={{}} className="text-primary underline underline-offset-2">{item.storageClass}</Link> : "—"} />
            <DetailItem label="绑定 PV" value={item.volumeName ? <Link to="/storage/volumes/$name" params={{ name: item.volumeName }} search={{}} className="text-primary underline underline-offset-2">{item.volumeName}</Link> : "—"} />
            <DetailItem label="创建时间" value={formatDate(item.createdAt)} />
          </dl></CardContent>
        </Card>
        <ConditionList conditions={item.conditions} />
      </div>
      <div className="mt-4 grid min-w-0 gap-4 xl:grid-cols-2">
        <ExpansionCard item={item} />
        <StorageImpactCard impact={item.impact} title="PVC 使用者与影响" />
      </div>
      <div className="mt-4"><MetadataCard labels={item.labels} annotations={item.annotations} /></div>
    </>
  );
}

export function PersistentVolumeClaimDetailPage() {
  const params = useParams({ strict: false }) as { namespace: string; name: string };
  const search = useSearch({ from: "/app/storage/claims/$namespace/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [resizeOpen, setResizeOpen] = useState(false);
  const [snapshotOpen, setSnapshotOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const tab = isStorageTab(search.tab) ? search.tab : "overview";
  const capabilities = useCapabilities(params.namespace).data;
  const claimCaps = resourceCapabilities(capabilities, "persistentvolumeclaims");
  const snapshotCaps = resourceCapabilities(capabilities, "volumesnapshots");
  const query = useQuery({ queryKey: ["persistentvolumeclaim", params.namespace, params.name], queryFn: () => api.persistentvolumeclaim(params.namespace, params.name), refetchInterval: 20_000 });
  const snapshotFeature = useQuery({ queryKey: ["volume-snapshot-feature"], queryFn: api.volumeSnapshotFeature, staleTime: 60_000, retry: false });
  const storageClasses = useQuery({ queryKey: ["storageclasses"], queryFn: () => api.storageclasses({ pageSize: 200 }), enabled: resizeOpen });
  if (query.isLoading) return <LoadingState label="正在加载 PersistentVolumeClaim 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const item = query.data!;
  const expansionBlocked = item.expansionStatus === "blocked" || item.expansionStatus === "expanding";
  const expansionHint = item.expansionStatus === "expanding" ? "扩容请求正在处理中，请等待完成" : item.expansionMessage || "当前 StorageClass 不支持扩容";
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/storage" search={{ ...search, tab: "claims" }} className="text-primary underline underline-offset-2">存储</Link> / PersistentVolumeClaim / {item.namespace} / {item.name}</>}
        title={<span className="flex items-center gap-3"><Database className="h-5 w-5 text-muted-foreground" />{item.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={item.status} /><span>{formatMemory(item.requestedCapacityBytes)}</span><span>·</span><span>{item.storageClass || "无 StorageClass"}</span></span>}
        actions={<div className="flex flex-wrap items-center gap-2">
          <DiagnoseButton kind="pvc" namespace={item.namespace} name={item.name} />
          {snapshotCaps.create && snapshotFeature.data?.available ? <Button variant="outline" size="sm" onClick={() => setSnapshotOpen(true)}><Camera className="h-4 w-4" />创建快照</Button> : null}
          {claimCaps.update ? <ActionTooltip content={expansionBlocked ? expansionHint : undefined}><Button variant="outline" size="sm" onClick={() => setResizeOpen(true)} disabled={expansionBlocked}><Pencil className="h-4 w-4" />扩容</Button></ActionTooltip> : null}
          {claimCaps.delete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}><Trash2 className="h-4 w-4" />删除</Button> : null}
        </div>}
      />
      <StorageTabs
        tab={tab}
        onChange={(value) => {
          if (!isStorageTab(value)) return;
          void navigate({ to: "/storage/claims/$namespace/$name", params, search: { ...search, tab: value === "overview" ? undefined : value }, replace: true });
        }}
        events={<ResourceEvents namespace={item.namespace} objectKind="PersistentVolumeClaim" objectName={item.name} />}
        yaml={<YamlPanel type="persistentvolumeclaim" name={item.name} namespace={item.namespace} height="64vh" />}
      />
      {tab === "overview" ? <div className="mt-4"><PersistentVolumeClaimOverview item={item} /></div> : null}
      {resizeOpen ? <Suspense fallback={null}><PersistentVolumeClaimFormDialog
        open
        onOpenChange={setResizeOpen}
        namespace={item.namespace}
        item={item}
        storageClasses={storageClasses.data?.items ?? []}
        onSaved={async () => {
          await query.refetch();
          await queryClient.invalidateQueries({ queryKey: ["persistentvolumeclaims"] });
        }}
      /></Suspense> : null}
      <DeleteResourceDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        resourceKind="PersistentVolumeClaim"
        name={item.name}
        description={`删除 PVC 后，绑定 PV 是否保留取决于回收策略；当前 ${item.impact?.mountCount ?? 0} 个 Pod 挂载该卷，${item.impact?.volumeSnapshotCount ?? 0} 个快照关联此 PVC。正在挂载该卷的工作负载可能中断，快照不会自动删除。`}
        onConfirm={async () => {
          await api.deletePersistentVolumeClaim(item.namespace, item.name, item.resourceVersion);
          await queryClient.invalidateQueries({ queryKey: ["persistentvolumeclaims"] });
          await navigate({ to: "/storage", search: { ...search, tab: "claims" } });
        }}
      />
      {snapshotOpen ? <Suspense fallback={null}><VolumeSnapshotFormDialog open onOpenChange={setSnapshotOpen} namespace={item.namespace} sourcePVC={item.name} claims={[item]} feature={snapshotFeature.data} onSaved={async () => { await query.refetch(); await queryClient.invalidateQueries({ queryKey: ["volume-snapshots"] }); }} /></Suspense> : null}
    </>
  );
}
