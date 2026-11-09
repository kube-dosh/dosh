import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Database, FileCode2, HardDrive, Pencil, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { api } from "@/api/client";
import type { Condition, PersistentVolumeClaimDetail, PersistentVolumeDetail, StorageClassDetail } from "@/api/types";
import { DetailItem } from "@/components/data/detail-item";
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
import { PersistentVolumeClaimFormDialog } from "@/features/storage/persistent-volume-claim-form-dialog";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatDate, formatMemory } from "@/lib/utils";

const storageTabs = ["overview", "events", "yaml"] as const;
type StorageTab = (typeof storageTabs)[number];

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
      <div className="mt-4"><MetadataCard labels={item.labels} annotations={item.annotations} /></div>
    </>
  );
}

export function PersistentVolumeDetailPage() {
  const params = useParams({ strict: false }) as { name: string };
  const search = useSearch({ from: "/app/storage/volumes/$name" });
  const navigate = useNavigate();
  const tab = isStorageTab(search.tab) ? search.tab : "overview";
  const query = useQuery({ queryKey: ["persistentvolume", params.name], queryFn: () => api.persistentvolume(params.name), refetchInterval: 20_000 });
  if (query.isLoading) return <LoadingState label="正在加载 PersistentVolume 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const item = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/storage" search={{ ...search, tab: "volumes" }} className="text-primary underline underline-offset-2">存储</Link> / PersistentVolume / {item.name}</>}
        title={<span className="flex items-center gap-3"><HardDrive className="h-5 w-5 text-muted-foreground" />{item.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={item.status} /><span>{formatMemory(item.capacityBytes)}</span><span>·</span><span>{item.storageClass || "无 StorageClass"}</span></span>}
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
  const [deleteOpen, setDeleteOpen] = useState(false);
  const tab = isStorageTab(search.tab) ? search.tab : "overview";
  const capabilities = useCapabilities(params.namespace).data;
  const claimCaps = resourceCapabilities(capabilities, "persistentvolumeclaims");
  const query = useQuery({ queryKey: ["persistentvolumeclaim", params.namespace, params.name], queryFn: () => api.persistentvolumeclaim(params.namespace, params.name), refetchInterval: 20_000 });
  const storageClasses = useQuery({ queryKey: ["storageclasses"], queryFn: () => api.storageclasses({ pageSize: 200 }), enabled: resizeOpen });
  if (query.isLoading) return <LoadingState label="正在加载 PersistentVolumeClaim 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const item = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/storage" search={{ ...search, tab: "claims" }} className="text-primary underline underline-offset-2">存储</Link> / PersistentVolumeClaim / {item.namespace} / {item.name}</>}
        title={<span className="flex items-center gap-3"><Database className="h-5 w-5 text-muted-foreground" />{item.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={item.status} /><span>{formatMemory(item.requestedCapacityBytes)}</span><span>·</span><span>{item.storageClass || "无 StorageClass"}</span></span>}
        actions={<div className="flex items-center gap-2">
          {claimCaps.update ? <Button variant="outline" size="sm" onClick={() => setResizeOpen(true)}><Pencil className="h-4 w-4" />扩容</Button> : null}
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
      <PersistentVolumeClaimFormDialog
        open={resizeOpen}
        onOpenChange={setResizeOpen}
        namespace={item.namespace}
        item={item}
        storageClasses={storageClasses.data?.items ?? []}
        onSaved={async () => {
          await query.refetch();
          await queryClient.invalidateQueries({ queryKey: ["persistentvolumeclaims"] });
        }}
      />
      <DeleteResourceDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        resourceKind="PersistentVolumeClaim"
        name={item.name}
        description="删除 PVC 后，绑定 PV 是否保留取决于回收策略；正在挂载该卷的工作负载可能中断。"
        onConfirm={async () => {
          await api.deletePersistentVolumeClaim(item.namespace, item.name, item.resourceVersion);
          await queryClient.invalidateQueries({ queryKey: ["persistentvolumeclaims"] });
          await navigate({ to: "/storage", search: { ...search, tab: "claims" } });
        }}
      />
    </>
  );
}
