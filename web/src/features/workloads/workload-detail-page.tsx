import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Boxes, FileCode2, Loader2, Minus, Pause, Pencil, Play, PlayCircle, Plus, RefreshCw, ScrollText, TerminalSquare, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { CronJobRunInput, PodSummary, WorkloadBatchStatus, WorkloadDetail, WorkloadRolloutStatus } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { YamlDialog, YamlPanel } from "@/components/resources/yaml-dialog";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useCapabilities } from "@/features/auth/use-session";
import { DiagnoseButton } from "@/features/diagnostics/diagnose-button";
import { resourceCapabilities, workloadCapabilities } from "@/lib/capabilities";
import { ResourceEvents } from "@/features/events/resource-events";
import { PodTerminal } from "@/features/workloads/pod-terminal";
import { WorkloadLogViewer } from "@/features/workloads/workload-log-viewer";
import { ResourceRelationsPanel } from "@/features/resources/resource-relations-panel";
import { WorkloadContainerConfigPanel } from "@/features/workloads/workload-container-config-panel";
import { ImageReferencePicker } from "@/features/registries/image-reference-picker";
import { WorkloadRevisionHistory } from "@/features/workloads/workload-revision-history";
import { WorkloadServicesPanel } from "@/features/workloads/workload-services-panel";
import { formatAge, formatDate } from "@/lib/utils";

const podColumn = createColumnHelper<PodSummary>();
const workloadTabs = ["overview", "config", "pods", "services", "events", "logs", "history", "yaml"] as const;
type WorkloadTab = (typeof workloadTabs)[number];

const kubernetesKinds: Record<string, string> = {
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
  job: "Job",
  cronjob: "CronJob",
};

function isWorkloadTab(value: string | undefined): value is WorkloadTab {
  return workloadTabs.includes(value as WorkloadTab);
}

function workloadKindLabel(kind: string): string {
  const normalized = kind.toLowerCase();
  return kubernetesKinds[normalized] ?? `${kind.charAt(0).toUpperCase()}${kind.slice(1).toLowerCase()}`;
}

export function WorkloadDetailPage() {
  const params = useParams({ strict: false }) as { kind: string; namespace: string; name: string };
  const search = useSearch({ from: "/app/workloads/$kind/$namespace/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [yamlOpen, setYamlOpen] = useState(false);
  const [scaleOpen, setScaleOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [restartOpen, setRestartOpen] = useState(false);
  const [suspendOpen, setSuspendOpen] = useState(false);
  const [runOpen, setRunOpen] = useState(false);
  const [rolloutOpen, setRolloutOpen] = useState(false);
  const [rolloutPaused, setRolloutPaused] = useState(false);
  const [maxSurge, setMaxSurge] = useState("");
  const [maxUnavailable, setMaxUnavailable] = useState("");
  const [partition, setPartition] = useState("0");
  const [runContainer, setRunContainer] = useState("");
  const [runCommand, setRunCommand] = useState("");
  const [runArgs, setRunArgs] = useState("");
  const [imageOpen, setImageOpen] = useState(false);
  const [imageContainer, setImageContainer] = useState("");
  const [imageValue, setImageValue] = useState("");
  const [imageRegistryId, setImageRegistryId] = useState("");
  const [imageResourceVersion, setImageResourceVersion] = useState("");
  const [replicas, setReplicas] = useState(1);
  const [confirmName, setConfirmName] = useState("");
  const [terminalPod, setTerminalPod] = useState<PodSummary | null>(null);
  const kind = params.kind.toLowerCase();
  const kindLabel = workloadKindLabel(kind);
  const relationGroup = ["job", "cronjob"].includes(kind) ? "batch" : "apps";
  const relationResource = kind === "cronjob" ? "cronjobs" : kind === "job" ? "jobs" : kind === "daemonset" ? "daemonsets" : kind === "statefulset" ? "statefulsets" : "deployments";
  const supportsRevisions = ["deployment", "statefulset"].includes(kind);
  const requestedTab = isWorkloadTab(search.tab) ? search.tab : "overview";
  const tab = requestedTab === "history" && !supportsRevisions ? "overview" : requestedTab;
  const changeTab = (value: string) => {
    if (!isWorkloadTab(value)) return;
    void navigate({
      to: "/workloads/$kind/$namespace/$name",
      params: { kind: params.kind, namespace: params.namespace, name: params.name },
      search: { ...search, tab: value === "overview" ? undefined : value },
      replace: true,
    });
  };
  const query = useQuery({
    queryKey: ["workload", kind, params.namespace, params.name],
    queryFn: () => api.workload(kind, params.namespace, params.name),
  });
  const podTerminalQuery = useQuery({
    queryKey: ["pod", terminalPod?.namespace, terminalPod?.name],
    queryFn: () => api.pod(terminalPod!.namespace, terminalPod!.name),
    enabled: Boolean(terminalPod),
    refetchInterval: (current) => current.state.data?.containers.some((container) => container.ephemeral && container.state !== "Running") ? 2_000 : 20_000,
  });
  const capabilities = useCapabilities(params.namespace).data;
  const canExec = Boolean(capabilities?.podExec);
  const workloadCaps = workloadCapabilities(capabilities, kind);
  const canScale = workloadCaps.scale && ["deployment", "statefulset"].includes(kind);
  const canRestart = workloadCaps.update && !["job", "cronjob"].includes(kind);
  const canUpdateImage = workloadCaps.update && ["deployment", "statefulset", "daemonset"].includes(kind);
  const canUpdateContainerConfig = workloadCaps.update && ["deployment", "statefulset", "daemonset"].includes(kind);
  const canSuspend = workloadCaps.update && kind === "cronjob";
  const canRunNow = kind === "cronjob" && resourceCapabilities(capabilities, "jobs").create;
  const canRollout = workloadCaps.update && ["deployment", "statefulset", "daemonset"].includes(kind);
  const canRollback = workloadCaps.update && supportsRevisions;
  const canDelete = workloadCaps.delete;
  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ["workload", kind, params.namespace, params.name] });
    await queryClient.invalidateQueries({ queryKey: ["workloads"] });
    await queryClient.invalidateQueries({ queryKey: ["workload-revisions", kind, params.namespace, params.name] });
  };
  const scale = useMutation({
    mutationFn: () => api.scaleWorkload(kind, params.namespace, params.name, replicas),
    onSuccess: async () => { toast.success(`副本数已调整为 ${replicas}`); setScaleOpen(false); await invalidate(); },
    onError: (error) => toast.error(error.message),
  });
  const restart = useMutation({
    mutationFn: () => api.restartWorkload(kind, params.namespace, params.name),
    onSuccess: async () => { toast.success("已触发滚动重启"); setRestartOpen(false); await invalidate(); },
    onError: (error) => toast.error(error.message),
  });
  const updateRollout = useMutation({
    mutationFn: () => api.updateWorkloadRollout(kind, params.namespace, params.name, {
      paused: kind === "deployment" ? rolloutPaused : undefined,
      maxSurge: kind === "deployment" || kind === "daemonset" ? maxSurge.trim() || undefined : undefined,
      maxUnavailable: kind === "deployment" || kind === "daemonset" ? maxUnavailable.trim() || undefined : undefined,
      partition: kind === "statefulset" ? Number(partition) : undefined,
      resourceVersion: query.data?.resourceVersion,
    }),
    onSuccess: async () => {
      const message = kind === "deployment" && rolloutPaused ? "Deployment 发布已暂停" : `${kindLabel} 发布控制已更新`;
      toast.success(message);
      setRolloutOpen(false);
      await invalidate();
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast.error(`${kindLabel} 已被其他操作更新，请刷新后重试`);
        void invalidate();
        return;
      }
      toast.error(error.message);
    },
  });
  const updateImage = useMutation({
    mutationFn: () => api.updateWorkloadImage(kind, params.namespace, params.name, {
      containerName: imageContainer,
      image: imageValue.trim(),
      registryId: imageRegistryId || undefined,
      resourceVersion: imageResourceVersion || undefined,
    }),
    onSuccess: async () => {
      setImageOpen(false);
      await invalidate();
      toast.success("镜像已更新，滚动发布已触发");
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast.error("工作负载已被其他操作更新，请刷新后重试");
        void invalidate();
        return;
      }
      toast.error(error.message);
    },
  });
  const suspendCronJob = useMutation({
    mutationFn: (suspend: boolean) => api.setCronJobSuspend(kind, params.namespace, params.name, {
      suspend,
      resourceVersion: query.data?.resourceVersion,
    }),
    onSuccess: async (_, suspend) => {
      toast.success(suspend ? "CronJob 已暂停调度" : "CronJob 已恢复调度");
      setSuspendOpen(false);
      await invalidate();
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast.error("CronJob 已被其他操作更新，请刷新后重试");
        void invalidate();
        return;
      }
      toast.error(error.message);
    },
  });
  const runCronJob = useMutation({
    mutationFn: (input: CronJobRunInput) => api.runCronJobOnce(params.namespace, params.name, input),
    onSuccess: async (created) => {
      toast.success(`已从 CronJob 创建 Job ${created.name}`);
      setRunOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      await navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind: "job", namespace: created.namespace, name: created.name }, search: {} });
    },
    onError: (error) => toast.error(error.message),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteWorkload(kind, params.namespace, params.name, "Background"),
    onSuccess: async () => {
      toast.success("删除请求已提交");
      setDeleteOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      await navigate({ to: "/workloads" });
    },
    onError: (error) => toast.error(error.message),
  });

  if (query.isLoading) return <LoadingState label="正在加载工作负载" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const workload = query.data!;
  const suspended = workload.status === "Suspended";
  const selectedImageContainer = workload.containers.find((container) => container.name === imageContainer);
  const openImageDialog = () => {
    const container = workload.containers[0];
    setImageContainer(container?.name ?? "");
    setImageValue(container?.image ?? "");
    setImageRegistryId("");
    setImageResourceVersion(workload.resourceVersion ?? "");
    setImageOpen(true);
  };
  const openRolloutDialog = () => {
    setRolloutPaused(Boolean(workload.rollout?.paused));
    setMaxSurge(workload.rollout?.maxSurge ?? "");
    setMaxUnavailable(workload.rollout?.maxUnavailable ?? "");
    setPartition(String(workload.rollout?.partition ?? 0));
    setRolloutOpen(true);
  };
  const openRunDialog = () => {
    setRunContainer(workload.containers[0]?.name ?? "");
    setRunCommand("");
    setRunArgs("");
    setRunOpen(true);
  };
  const rolloutValueValid = (value: string) => value.trim() === "" || /^\d+%?$/.test(value.trim());
  const partitionValid = /^\d+$/.test(partition.trim()) && Number(partition) >= 0 && Number(partition) <= 100000;
  const rolloutFormValid = kind === "statefulset"
    ? partitionValid
    : rolloutValueValid(maxSurge) && rolloutValueValid(maxUnavailable) && (kind === "deployment" || maxSurge.trim() !== "" || maxUnavailable.trim() !== "");

  return (
    <>
      <PageHeader
        title={workload.name}
        breadcrumb={<span>工作负载 / <Link to="/workloads" className="text-primary underline underline-offset-2">{kindLabel}</Link> / {workload.namespace}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={workload.status} /><span>{workload.ready} 就绪</span><span>·</span><span>{workload.namespace}</span></span>}
        actions={
          <>
            <DiagnoseButton kind={kind} namespace={workload.namespace} name={workload.name} />
            <Button variant="outline" size="sm" onClick={() => setYamlOpen(true)}><FileCode2 className="h-4 w-4" />YAML</Button>
            {canRunNow ? <Button variant="outline" size="sm" onClick={openRunDialog}><PlayCircle className="h-4 w-4" />立即执行</Button> : null}
            {canSuspend ? <Button variant="outline" size="sm" onClick={() => setSuspendOpen(true)}>{suspended ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}{suspended ? "恢复调度" : "暂停调度"}</Button> : null}
            {canRollout ? <Button variant="outline" size="sm" onClick={openRolloutDialog}>{workload.rollout?.paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}发布控制</Button> : null}
            {canUpdateImage && workload.containers.length ? <Button variant="outline" size="sm" onClick={openImageDialog}><Pencil className="h-4 w-4" />更新镜像</Button> : null}
            {canRestart ? <Button variant="outline" size="sm" onClick={() => setRestartOpen(true)}><RefreshCw className="h-4 w-4" />重启</Button> : null}
            {canScale ? <Button size="sm" onClick={() => { setReplicas(workload.desired); setScaleOpen(true); }}><Plus className="h-4 w-4" />扩缩容</Button> : null}
            {canDelete ? <Button variant="destructive" size="sm" onClick={() => { setConfirmName(""); setDeleteOpen(true); }}><Trash2 className="h-4 w-4" />删除</Button> : null}
          </>
        }
      />
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="config">容器配置</TabsTrigger><TabsTrigger value="pods">实例 ({workload.pods.length})</TabsTrigger><TabsTrigger value="services">访问方式</TabsTrigger><TabsTrigger value="events">事件</TabsTrigger><TabsTrigger value="logs"><ScrollText className="h-3.5 w-3.5" />日志</TabsTrigger>{supportsRevisions ? <TabsTrigger value="history"><RefreshCw className="h-3.5 w-3.5" />发布历史</TabsTrigger> : null}<TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger></TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
            <Card>
              <CardHeader><CardTitle>基本信息</CardTitle></CardHeader>
              <CardContent><dl className="grid gap-5 sm:grid-cols-2">
                <DetailItem label="名称" value={workload.name} mono />
                <DetailItem label="类型" value={workload.kind} />
                <DetailItem label="命名空间" value={workload.namespace} />
                <DetailItem label="状态" value={<StatusBadge status={workload.status} />} />
                <DetailItem label="就绪副本" value={workload.ready} />
                <DetailItem label="期望副本" value={String(workload.desired)} />
                <DetailItem label="重启次数" value={String(workload.restarts)} />
                <DetailItem label="创建时间" value={formatDate(workload.createdAt)} />
                <DetailItem label="资源版本" value={workload.resourceVersion || "—"} mono />
              </dl></CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle>镜像与选择器</CardTitle></CardHeader>
              <CardContent className="space-y-5">
                <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">容器镜像</p><div className="space-y-2">{workload.containers.length ? workload.containers.map((container) => <div key={container.name} className="min-w-0 rounded-md border bg-muted/30 px-3 py-2"><p className="mb-1 break-all text-xs font-medium">{container.name}</p><p className="break-all font-mono text-xs text-muted-foreground">{container.image}</p></div>) : <p className="text-sm text-muted-foreground">—</p>}</div></div>
                {workload.initContainers?.length ? <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Init Container 镜像</p><div className="space-y-2">{workload.initContainers.map((container) => <div key={container.name} className="min-w-0 rounded-md border border-dashed bg-muted/20 px-3 py-2"><p className="mb-1 break-all text-xs font-medium">{container.name}</p><p className="break-all font-mono text-xs text-muted-foreground">{container.image}</p></div>)}</div></div> : null}
                <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Selector</p><div className="flex flex-wrap gap-1.5">{Object.entries(workload.selector).map(([key, value]) => <span key={key} className="rounded border bg-card px-2 py-1 font-mono text-[11px]">{key}={value}</span>)}</div></div>
              </CardContent>
            </Card>
            {workload.rollout ? <RolloutPanel rollout={workload.rollout} desired={workload.desired} canEdit={canRollout} onEdit={openRolloutDialog} /> : null}
            {workload.batch ? <BatchPanel batch={workload.batch} namespace={workload.namespace} /> : null}
            {workload.replicaSets?.length ? <ReplicaSetPanel replicaSets={workload.replicaSets} /> : null}
            <Card className="xl:col-span-2">
              <CardHeader><CardTitle>Conditions</CardTitle></CardHeader>
              <CardContent className="space-y-2">
                {workload.conditions.length ? workload.conditions.map((condition) => <div key={condition.type} className="grid gap-2 rounded-md border px-3 py-2.5 text-sm sm:grid-cols-[180px_120px_1fr]"><span className="font-medium">{condition.type}</span><StatusBadge status={condition.status === "True" ? "Ready" : "Failed"} /><span className="text-muted-foreground">{condition.message || condition.reason || "—"}</span></div>) : <p className="py-6 text-center text-sm text-muted-foreground">暂无 Condition 数据</p>}
              </CardContent>
            </Card>
          </div>
          <div className="mt-4"><ResourceRelationsPanel reference={{ group: relationGroup, version: "v1", resource: relationResource }} name={workload.name} namespace={workload.namespace} title="故障拓扑与直接关系" /></div>
        </TabsContent>
        <TabsContent value="config" className="space-y-4">
          {workload.initContainers?.length ? <InitContainerPanel containers={workload.initContainers} /> : null}
          <WorkloadContainerConfigPanel workload={workload} canEdit={canUpdateContainerConfig} onSaved={invalidate} />
        </TabsContent>
        <TabsContent value="pods">
          <DataTable
            columns={[
              podColumn.accessor("name", { header: "Pod", cell: (info) => <Link to="/pods/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
              podColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              podColumn.accessor("ready", { header: "就绪" }),
              podColumn.accessor("restarts", { header: "重启" }),
              podColumn.accessor("node", { header: "节点", cell: (info) => info.getValue() ? <Link to="/nodes/$name" params={{ name: info.getValue() }} search={{}} className="font-mono text-xs font-medium text-primary hover:underline">{info.getValue()}</Link> : <span className="text-muted-foreground">—</span> }),
              podColumn.accessor("ip", { header: "IP", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
              podColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              podColumn.display({ id: "terminal", header: "终端", size: 52, cell: (info) => <Button variant="ghost" size="sm" className="h-8 px-2" aria-label={`进入终端 ${info.row.original.name}`} title={canExec ? "进入该 Pod 的容器终端" : "需要 create pods/exec 权限"} disabled={!canExec} onClick={() => setTerminalPod(info.row.original)}><TerminalSquare className="h-4 w-4" /></Button> }),
            ]}
            data={workload.pods}
            empty={<div className="flex flex-col items-center gap-2"><Boxes className="h-6 w-6" /><span>该工作负载当前没有 Pod</span></div>}
            pageSize={10}
          />
        </TabsContent>
        <TabsContent value="services"><WorkloadServicesPanel namespace={workload.namespace} workloadName={workload.name} workloadSelector={workload.selector} /></TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={workload.namespace} objectKind={kindLabel} objectName={workload.name} /></TabsContent>
        <TabsContent value="logs"><WorkloadLogViewer kind={kind} namespace={workload.namespace} name={workload.name} containers={workload.containers} /></TabsContent>
        {supportsRevisions ? <TabsContent value="history"><WorkloadRevisionHistory kind={kind} namespace={workload.namespace} name={workload.name} canRollback={canRollback} onRolledBack={invalidate} /></TabsContent> : null}
        <TabsContent value="yaml"><YamlPanel type={kind} name={workload.name} namespace={workload.namespace} height="64vh" /></TabsContent>
      </Tabs>

      <Dialog open={terminalPod !== null} onOpenChange={(open) => { if (!open) setTerminalPod(null); }}>
        <DialogContent className="w-[calc(100%-1rem)] max-w-6xl p-3 sm:p-5">
          <DialogHeader>
            <DialogTitle>Pod 终端 · {terminalPod?.name}</DialogTitle>
            <DialogDescription>{terminalPod?.namespace}/{terminalPod?.name} · 选择目标 Pod 后再选择需要进入的容器。终端会话受 RBAC、会话限额和审计保护。</DialogDescription>
          </DialogHeader>
          {podTerminalQuery.isLoading ? <LoadingState label="正在加载 Pod 容器" /> : podTerminalQuery.isError ? <ErrorState error={podTerminalQuery.error} onRetry={() => void podTerminalQuery.refetch()} /> : podTerminalQuery.data ? <PodTerminal namespace={podTerminalQuery.data.namespace} name={podTerminalQuery.data.name} containers={podTerminalQuery.data.containers} canDebugContainer={Boolean(capabilities?.podEphemeralDebug)} onContainerCreated={() => podTerminalQuery.refetch()} /> : null}
        </DialogContent>
      </Dialog>

      <Dialog open={scaleOpen} onOpenChange={setScaleOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader><DialogTitle>调整副本数</DialogTitle><DialogDescription>{workload.namespace}/{workload.name} · 当前 {workload.desired} 个副本</DialogDescription></DialogHeader>
          <div className="space-y-2"><Label htmlFor="replicas">期望副本数</Label><div className="flex items-center gap-2"><Button variant="outline" size="icon" onClick={() => setReplicas((value) => Math.max(0, value - 1))}><Minus className="h-4 w-4" /></Button><Input id="replicas" type="number" min={0} max={1000} value={replicas} onChange={(event) => setReplicas(Math.max(0, Number(event.target.value) || 0))} /><Button variant="outline" size="icon" onClick={() => setReplicas((value) => Math.min(1000, value + 1))}><Plus className="h-4 w-4" /></Button></div></div>
          <DialogFooter><Button variant="outline" onClick={() => setScaleOpen(false)}>取消</Button><Button onClick={() => scale.mutate()} disabled={scale.isPending}>{scale.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认调整</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={rolloutOpen} onOpenChange={setRolloutOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>发布控制</DialogTitle>
            <DialogDescription>
              {kind === "deployment" ? "暂停只会冻结 Deployment Pod 模板变更，不影响现有 Pod；maxSurge 和 maxUnavailable 支持整数或百分比。" : kind === "statefulset" ? "Partition 之前的序号保持当前版本；Partition 为 0 时所有副本滚动更新。" : "DaemonSet 发布期间可配置滚动超量节点和不可用节点数量，支持整数或百分比。"}
            </DialogDescription>
          </DialogHeader>
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            {kind === "deployment" ? <div className="min-w-0 space-y-2 sm:col-span-2">
              <Label htmlFor="rollout-state">发布状态</Label>
              <Select value={rolloutPaused ? "paused" : "running"} onValueChange={(value) => setRolloutPaused(value === "paused")}>
                <SelectTrigger id="rollout-state"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="running"><Play className="mr-2 inline h-4 w-4" />继续发布</SelectItem>
                  <SelectItem value="paused"><Pause className="mr-2 inline h-4 w-4" />暂停发布</SelectItem>
                </SelectContent>
              </Select>
            </div> : null}
            {kind === "statefulset" ? <div className="min-w-0 space-y-2 sm:col-span-2">
              <Label htmlFor="rollout-partition">Partition</Label>
              <Input id="rollout-partition" type="number" min={0} max={100000} step={1} value={partition} onChange={(event) => setPartition(event.target.value)} className="font-mono" />
              <p className="text-xs text-muted-foreground">只更新序号大于等于 Partition 的 Pod。设置为 0 表示更新全部副本。</p>
              {!partitionValid ? <p role="alert" className="text-xs text-destructive-strong">Partition 必须是 0 到 100000 的整数。</p> : null}
            </div> : null}
            {kind === "deployment" || kind === "daemonset" ? <>
              <div className="min-w-0 space-y-2">
                <Label htmlFor="rollout-max-surge">maxSurge</Label>
                <Input id="rollout-max-surge" value={maxSurge} onChange={(event) => setMaxSurge(event.target.value)} placeholder="25%" className="font-mono" />
                <p className="text-xs text-muted-foreground">滚动期间允许超过期望副本的数量。</p>
              </div>
              <div className="min-w-0 space-y-2">
                <Label htmlFor="rollout-max-unavailable">maxUnavailable</Label>
                <Input id="rollout-max-unavailable" value={maxUnavailable} onChange={(event) => setMaxUnavailable(event.target.value)} placeholder="25%" className="font-mono" />
                <p className="text-xs text-muted-foreground">滚动期间允许不可用的副本数量。</p>
              </div>
              {!rolloutValueValid(maxSurge) || !rolloutValueValid(maxUnavailable) ? <p role="alert" className="text-xs text-destructive-strong sm:col-span-2">请输入非负整数或 0% 到 100% 的百分比。</p> : null}
              {kind === "daemonset" && !maxSurge.trim() && !maxUnavailable.trim() ? <p role="alert" className="text-xs text-destructive-strong sm:col-span-2">maxSurge 和 maxUnavailable 至少填写一项。</p> : null}
            </> : null}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRolloutOpen(false)} disabled={updateRollout.isPending}>取消</Button>
            <Button onClick={() => updateRollout.mutate()} disabled={updateRollout.isPending || !rolloutFormValid}>
              {updateRollout.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : rolloutPaused ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
              保存发布设置
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={imageOpen} onOpenChange={setImageOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>更新容器镜像</DialogTitle>
            <DialogDescription>{workload.namespace}/{workload.name} · 将更新 Pod 模板并触发滚动发布</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="workload-image-container">容器</Label>
              <Select value={imageContainer} onValueChange={(value) => {
                setImageContainer(value);
                const container = workload.containers.find((item) => item.name === value);
                setImageValue(container?.image ?? "");
                setImageRegistryId("");
              }}>
                <SelectTrigger id="workload-image-container" aria-label="选择容器"><SelectValue placeholder="选择容器" /></SelectTrigger>
                <SelectContent>{workload.containers.map((container) => <SelectItem key={container.name} value={container.name}>{container.name}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <ImageReferencePicker
              id="workload-image"
              value={imageValue}
              registryId={imageRegistryId || undefined}
              onChange={(image, registryId) => { setImageValue(image); setImageRegistryId(registryId ?? ""); }}
              label="镜像地址"
              hint={`使用不可变 digest 或明确版本标签；当前资源版本 ${imageResourceVersion || "未锁定"}。`}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setImageOpen(false)} disabled={updateImage.isPending}>取消</Button>
            <Button onClick={() => updateImage.mutate()} disabled={updateImage.isPending || !imageContainer || !imageValue.trim() || imageValue.trim() === selectedImageContainer?.image}>
              {updateImage.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Pencil className="h-4 w-4" />}
              确认更新
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={restartOpen} onOpenChange={setRestartOpen}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>触发滚动重启？</AlertDialogTitle><AlertDialogDescription>将更新 Pod 模板注解并逐步替换实例。服务可用性取决于副本数与探针配置。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction onClick={(event) => { event.preventDefault(); restart.mutate(); }} disabled={restart.isPending}>确认重启</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={suspendOpen} onOpenChange={setSuspendOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{suspended ? "恢复 CronJob 调度？" : "暂停 CronJob 调度？"}</AlertDialogTitle>
            <AlertDialogDescription>{suspended ? "恢复后将按调度规则创建新的 Job，不会补跑暂停期间遗漏的任务。" : "暂停后不再创建新的 Job，已运行的 Job 和 Pod 不受影响。恢复时不会补跑遗漏任务。"}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={suspendCronJob.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => { event.preventDefault(); suspendCronJob.mutate(!suspended); }} disabled={suspendCronJob.isPending}>
              {suspendCronJob.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {suspended ? "确认恢复" : "确认暂停"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={runOpen} onOpenChange={setRunOpen}>
        <AlertDialogContent className="max-h-[88vh] overflow-y-auto">
          <AlertDialogHeader>
            <AlertDialogTitle>立即执行 {workload.name}？</AlertDialogTitle>
            <AlertDialogDescription>将从当前 CronJob 的 Job 模板创建一个一次性 Job，不会修改 CronJob 调度，也不会补跑历史任务。可选择目标容器并临时覆盖命令与参数。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="run-container">目标容器</Label>
              <Select value={runContainer} onValueChange={setRunContainer}>
                <SelectTrigger id="run-container"><SelectValue placeholder="选择容器" /></SelectTrigger>
                <SelectContent>{workload.containers.map((container) => <SelectItem key={container.name} value={container.name}>{container.name} · {container.image}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="run-command">Command 覆盖（可选）</Label>
              <Textarea id="run-command" value={runCommand} onChange={(event) => setRunCommand(event.target.value)} placeholder={"/bin/sh"} className="min-h-20 font-mono text-xs" spellCheck={false} />
              <p className="text-xs text-muted-foreground">每行一个参数；留空则沿用 CronJob 模板。</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="run-args">Args 覆盖（可选）</Label>
              <Textarea id="run-args" value={runArgs} onChange={(event) => setRunArgs(event.target.value)} placeholder={"-c\nbackup --once"} className="min-h-20 font-mono text-xs" spellCheck={false} />
            </div>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={runCronJob.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => {
              event.preventDefault();
              runCronJob.mutate({
                containerName: runContainer || undefined,
                command: splitCommandLines(runCommand),
                args: splitCommandLines(runArgs),
              });
            }} disabled={runCronJob.isPending}>
              {runCronJob.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <PlayCircle className="h-4 w-4" />}
              确认执行
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除 {workload.name}？</AlertDialogTitle><AlertDialogDescription>此操作不可撤销，关联 Pod 将按 Background 策略删除。请输入资源名称确认。</AlertDialogDescription></AlertDialogHeader>
          <div className="space-y-2"><Label htmlFor="delete-confirm">资源名称</Label><Input id="delete-confirm" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} placeholder={workload.name} autoComplete="off" /></div>
          <AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={(event) => { event.preventDefault(); remove.mutate(); }} disabled={confirmName !== workload.name || remove.isPending}>确认删除</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <YamlDialog open={yamlOpen} onOpenChange={setYamlOpen} type={kind} name={workload.name} namespace={workload.namespace} />
    </>
  );
}

function RolloutPanel({ rollout, desired, canEdit, onEdit }: {
  rollout: WorkloadRolloutStatus;
  desired: number;
  canEdit: boolean;
  onEdit: () => void;
}) {
  const progress = desired > 0 ? Math.min(100, Math.round((rollout.updatedReplicas / desired) * 100)) : 100;
  return (
    <Card className="min-w-0 overflow-hidden xl:col-span-2">
      <CardHeader className="gap-2">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
          <CardTitle>发布进度</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={rollout.paused ? "warning" : "secondary"}>{rollout.paused ? "已暂停" : rollout.partition !== undefined ? `Partition ${rollout.partition}` : "进行中"}</Badge>
            {canEdit ? <Button variant="outline" size="sm" onClick={onEdit}><Pause className="h-4 w-4" />调整策略</Button> : null}
          </div>
        </div>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4">
        <div className="min-w-0 space-y-1.5">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground"><span>{rollout.strategy || "RollingUpdate"}</span><span>{rollout.updatedReplicas}/{desired} 已更新 · {progress}%</span></div>
          <div className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-label="发布更新进度" aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}>
            <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${progress}%` }} />
          </div>
        </div>
        <dl className="grid min-w-0 gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
          <DetailItem label="maxSurge" value={rollout.maxSurge || "—"} mono />
          <DetailItem label="maxUnavailable" value={rollout.maxUnavailable || "—"} mono />
          <DetailItem label="已更新 / 就绪" value={`${rollout.updatedReplicas} / ${rollout.readyReplicas}`} />
          <DetailItem label="可用 / 不可用" value={`${rollout.availableReplicas} / ${rollout.unavailableReplicas}`} />
          {rollout.partition !== undefined ? <DetailItem label="StatefulSet Partition" value={String(rollout.partition)} /> : null}
          <DetailItem label="Observed Generation" value={String(rollout.observedGeneration)} mono />
        </dl>
      </CardContent>
    </Card>
  );
}

function BatchPanel({ batch, namespace }: { batch: WorkloadBatchStatus; namespace: string }) {
  const parameters = [
    ["completions", batch.completions],
    ["parallelism", batch.parallelism],
    ["backoffLimit", batch.backoffLimit],
    ["activeDeadlineSeconds", batch.activeDeadlineSeconds],
    ["ttlSecondsAfterFinished", batch.ttlSecondsAfterFinished],
  ].filter(([, value]) => value !== undefined);
  return (
    <Card className="min-w-0 overflow-hidden xl:col-span-2">
      <CardHeader><CardTitle>批处理状态</CardTitle></CardHeader>
      <CardContent className="min-w-0 space-y-4">
        <dl className="grid min-w-0 grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <DetailItem label="运行中" value={String(batch.active)} />
          <DetailItem label="成功" value={String(batch.succeeded)} />
          <DetailItem label="失败" value={String(batch.failed)} />
          <DetailItem label="失败原因" value={batch.failureReason || "—"} />
          <DetailItem label="最近调度" value={batch.lastScheduleTime ? formatDate(batch.lastScheduleTime) : "—"} />
          <DetailItem label="最近成功" value={batch.lastSuccessfulTime ? formatDate(batch.lastSuccessfulTime) : "—"} />
        </dl>
        {parameters.length ? <div className="flex min-w-0 flex-wrap gap-1.5">{parameters.map(([label, value]) => <Badge key={String(label)} variant="outline" className="max-w-full whitespace-normal break-all font-mono">{label}={String(value)}</Badge>)}</div> : <p className="text-xs text-muted-foreground">使用 Kubernetes 默认 Job 参数。</p>}
        {batch.failureMessage ? <div role="alert" className="min-w-0 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive-strong"><p className="font-medium">最近失败</p><p className="mt-1 break-words">{batch.failureMessage}</p></div> : null}
        {batch.activeJobs?.length ? <div className="min-w-0 space-y-2"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">最近 Job</p>{batch.activeJobs.map((job) => <div key={job.name} className="flex min-w-0 flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm"><Link to="/workloads/$kind/$namespace/$name" params={{ kind: "job", namespace, name: job.name }} className="min-w-0 break-all font-medium text-primary hover:underline">{job.name}</Link><StatusBadge status={job.status} /><span className="text-xs text-muted-foreground">成功 {job.succeeded} · 失败 {job.failed}</span>{job.startTime ? <span className="text-xs text-muted-foreground">开始于 {formatDate(job.startTime)}</span> : null}</div>)}</div> : <p className="text-xs text-muted-foreground">没有关联的近期 Job。</p>}
      </CardContent>
    </Card>
  );
}

function ReplicaSetPanel({ replicaSets }: { replicaSets: NonNullable<WorkloadDetail["replicaSets"]> }) {
  return (
    <Card className="min-w-0 overflow-hidden xl:col-span-2">
      <CardHeader><CardTitle>ReplicaSet</CardTitle></CardHeader>
      <CardContent className="min-w-0 space-y-2">
        {replicaSets.map((replicaSet) => (
          <div key={replicaSet.name} className="min-w-0 rounded-md border px-3 py-3">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="min-w-0 break-all font-mono text-sm font-medium">{replicaSet.name}</span>
              {replicaSet.current ? <Badge>当前版本</Badge> : <Badge variant="outline">历史版本</Badge>}
              <span className="text-xs text-muted-foreground">revision {replicaSet.revision} · {formatAge(replicaSet.createdAt)}</span>
            </div>
            <div className="mt-2 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground"><span>期望 {replicaSet.desired}</span><span>就绪 {replicaSet.ready}</span><span>可用 {replicaSet.available}</span></div>
            <p className="mt-2 break-all font-mono text-xs text-muted-foreground">{replicaSet.images.join(" · ") || "—"}</p>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function InitContainerPanel({ containers }: { containers: NonNullable<WorkloadDetail["initContainers"]> }) {
  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader><CardTitle>Init Container</CardTitle></CardHeader>
      <CardContent className="grid min-w-0 gap-2 sm:grid-cols-2">
        {containers.map((container) => (
          <div key={container.name} className="min-w-0 rounded-md border border-dashed bg-muted/20 px-3 py-2">
            <p className="break-all font-mono text-xs font-medium">{container.name}</p>
            <p className="mt-1 break-all font-mono text-xs text-muted-foreground">{container.image}</p>
            <p className="mt-1 text-xs text-muted-foreground">{container.env.length} 个环境变量 · {container.volumeMounts.length} 个挂载</p>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function splitCommandLines(value: string): string[] | undefined {
  const items = value.split("\n").map((line) => line.trim()).filter(Boolean);
  return items.length ? items : undefined;
}
