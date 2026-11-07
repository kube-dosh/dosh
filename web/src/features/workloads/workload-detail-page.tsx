import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Boxes, FileCode2, Loader2, Minus, Pause, Pencil, Play, Plus, RefreshCw, ScrollText, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { PodSummary } from "@/api/types";
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
import { useCapabilities } from "@/features/auth/use-session";
import { workloadCapabilities } from "@/lib/capabilities";
import { ResourceEvents } from "@/features/events/resource-events";
import { PodLogs } from "@/features/workloads/pod-logs";
import { WorkloadServicesPanel } from "@/features/workloads/workload-services-panel";
import { formatAge, formatDate } from "@/lib/utils";

const podColumn = createColumnHelper<PodSummary>();
const workloadTabs = ["overview", "pods", "services", "events", "logs", "yaml"] as const;
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
  const [imageOpen, setImageOpen] = useState(false);
  const [imageContainer, setImageContainer] = useState("");
  const [imageValue, setImageValue] = useState("");
  const [imageResourceVersion, setImageResourceVersion] = useState("");
  const [replicas, setReplicas] = useState(1);
  const [confirmName, setConfirmName] = useState("");
  const kind = params.kind.toLowerCase();
  const tab = isWorkloadTab(search.tab) ? search.tab : "overview";
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
  const capabilities = useCapabilities(params.namespace).data;
  const workloadCaps = workloadCapabilities(capabilities, kind);
  const canScale = workloadCaps.scale && ["deployment", "statefulset"].includes(kind);
  const canRestart = workloadCaps.update && !["job", "cronjob"].includes(kind);
  const canUpdateImage = workloadCaps.update && ["deployment", "statefulset", "daemonset"].includes(kind);
  const canSuspend = workloadCaps.update && kind === "cronjob";
  const canDelete = workloadCaps.delete;
  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ["workload", kind, params.namespace, params.name] });
    await queryClient.invalidateQueries({ queryKey: ["workloads"] });
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
  const updateImage = useMutation({
    mutationFn: () => api.updateWorkloadImage(kind, params.namespace, params.name, {
      containerName: imageContainer,
      image: imageValue.trim(),
      resourceVersion: imageResourceVersion || undefined,
    }),
    onSuccess: async () => {
      toast.success("镜像已更新，滚动发布已触发");
      setImageOpen(false);
      await invalidate();
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
    setImageResourceVersion(workload.resourceVersion ?? "");
    setImageOpen(true);
  };

  return (
    <>
      <PageHeader
        title={workload.name}
        breadcrumb={<span>工作负载 / <Link to="/workloads" className="text-primary underline underline-offset-2">{kind}</Link> / {workload.namespace}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={workload.status} /><span>{workload.ready} 就绪</span><span>·</span><span>{workload.namespace}</span></span>}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => setYamlOpen(true)}><FileCode2 className="h-4 w-4" />YAML</Button>
            {canSuspend ? <Button variant="outline" size="sm" onClick={() => setSuspendOpen(true)}>{suspended ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}{suspended ? "恢复调度" : "暂停调度"}</Button> : null}
            {canUpdateImage && workload.containers.length ? <Button variant="outline" size="sm" onClick={openImageDialog}><Pencil className="h-4 w-4" />更新镜像</Button> : null}
            {canRestart ? <Button variant="outline" size="sm" onClick={() => setRestartOpen(true)}><RefreshCw className="h-4 w-4" />重启</Button> : null}
            {canScale ? <Button size="sm" onClick={() => { setReplicas(workload.desired); setScaleOpen(true); }}><Plus className="h-4 w-4" />扩缩容</Button> : null}
            {canDelete ? <Button variant="destructive" size="sm" onClick={() => { setConfirmName(""); setDeleteOpen(true); }}><Trash2 className="h-4 w-4" />删除</Button> : null}
          </>
        }
      />
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="pods">实例 ({workload.pods.length})</TabsTrigger><TabsTrigger value="services">访问方式</TabsTrigger><TabsTrigger value="events">事件</TabsTrigger><TabsTrigger value="logs"><ScrollText className="h-3.5 w-3.5" />日志</TabsTrigger><TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger></TabsList>
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
                <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">容器镜像</p><div className="space-y-2">{workload.containers.length ? workload.containers.map((container) => <div key={container.name} className="rounded-md border bg-muted/30 px-3 py-2"><p className="mb-1 text-xs font-medium">{container.name}</p><p className="break-all font-mono text-xs text-muted-foreground">{container.image}</p></div>) : <p className="text-sm text-muted-foreground">—</p>}</div></div>
                <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Selector</p><div className="flex flex-wrap gap-1.5">{Object.entries(workload.selector).map(([key, value]) => <span key={key} className="rounded border bg-card px-2 py-1 font-mono text-[11px]">{key}={value}</span>)}</div></div>
              </CardContent>
            </Card>
            <Card className="xl:col-span-2">
              <CardHeader><CardTitle>Conditions</CardTitle></CardHeader>
              <CardContent className="space-y-2">
                {workload.conditions.length ? workload.conditions.map((condition) => <div key={condition.type} className="grid gap-2 rounded-md border px-3 py-2.5 text-sm sm:grid-cols-[180px_120px_1fr]"><span className="font-medium">{condition.type}</span><StatusBadge status={condition.status === "True" ? "Ready" : "Failed"} /><span className="text-muted-foreground">{condition.message || condition.reason || "—"}</span></div>) : <p className="py-6 text-center text-sm text-muted-foreground">暂无 Condition 数据</p>}
              </CardContent>
            </Card>
          </div>
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
            ]}
            data={workload.pods}
            empty={<div className="flex flex-col items-center gap-2"><Boxes className="h-6 w-6" /><span>该工作负载当前没有 Pod</span></div>}
            pageSize={10}
          />
        </TabsContent>
        <TabsContent value="services"><WorkloadServicesPanel namespace={workload.namespace} workloadName={workload.name} workloadSelector={workload.selector} /></TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={workload.namespace} objectKind={kubernetesKinds[kind] ?? workload.kind} objectName={workload.name} /></TabsContent>
        <TabsContent value="logs"><PodLogs pods={workload.pods} emptyMessage="该工作负载当前没有可查看日志的实例。" /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type={kind} name={workload.name} namespace={workload.namespace} height="64vh" /></TabsContent>
      </Tabs>

      <Dialog open={scaleOpen} onOpenChange={setScaleOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader><DialogTitle>调整副本数</DialogTitle><DialogDescription>{workload.namespace}/{workload.name} · 当前 {workload.desired} 个副本</DialogDescription></DialogHeader>
          <div className="space-y-2"><Label htmlFor="replicas">期望副本数</Label><div className="flex items-center gap-2"><Button variant="outline" size="icon" onClick={() => setReplicas((value) => Math.max(0, value - 1))}><Minus className="h-4 w-4" /></Button><Input id="replicas" type="number" min={0} max={1000} value={replicas} onChange={(event) => setReplicas(Math.max(0, Number(event.target.value) || 0))} /><Button variant="outline" size="icon" onClick={() => setReplicas((value) => Math.min(1000, value + 1))}><Plus className="h-4 w-4" /></Button></div></div>
          <DialogFooter><Button variant="outline" onClick={() => setScaleOpen(false)}>取消</Button><Button onClick={() => scale.mutate()} disabled={scale.isPending}>{scale.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认调整</Button></DialogFooter>
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
              }}>
                <SelectTrigger id="workload-image-container" aria-label="选择容器"><SelectValue placeholder="选择容器" /></SelectTrigger>
                <SelectContent>{workload.containers.map((container) => <SelectItem key={container.name} value={container.name}>{container.name}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="workload-image">镜像地址</Label>
              <Input id="workload-image" value={imageValue} onChange={(event) => setImageValue(event.target.value)} placeholder="registry.example.com/app:1.2.3" autoComplete="off" spellCheck={false} />
              <p className="text-xs text-muted-foreground">使用不可变 digest 或明确版本标签；当前资源版本 {imageResourceVersion || "未锁定"}。</p>
            </div>
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
