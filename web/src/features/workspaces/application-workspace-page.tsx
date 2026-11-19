import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import {
  Activity,
  AlertTriangle,
  AppWindow,
  Boxes,
  CircleDot,
  Database,
  ExternalLink,
  FileCode2,
  FolderTree,
  Gauge,
  HeartPulse,
  Loader2,
  Minus,
  Network,
  RefreshCw,
  Route,
  Settings2,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { WorkloadSummary } from "@/api/types";
import { EmptyState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useCapabilities } from "@/features/auth/use-session";
import { EventObjectLink } from "@/features/events/event-object-link";
import { summarizeWorkspace, workspacePageSize, type WorkspaceHealthStatus } from "@/features/workspaces/workspace-model";
import { workloadCapabilities } from "@/lib/capabilities";
import { formatAge, formatMemory } from "@/lib/utils";

const healthTone: Record<WorkspaceHealthStatus, { badge: "success" | "warning" | "secondary"; icon: string; label: string }> = {
  healthy: { badge: "success", icon: "text-success", label: "健康" },
  degraded: { badge: "warning", icon: "text-warning", label: "需要关注" },
  empty: { badge: "secondary", icon: "text-muted-foreground", label: "暂无工作负载" },
};

function CountCard({ label, value, detail, icon: Icon }: { label: string; value: string; detail?: string; icon: typeof Boxes }) {
  return (
    <Card className="min-w-0">
      <CardContent className="flex min-w-0 items-start gap-3 p-4">
        <span className="rounded-md bg-muted p-2 text-muted-foreground"><Icon className="h-4 w-4" /></span>
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className="mt-0.5 break-words text-lg font-semibold tabular-nums">{value}</p>
          {detail ? <p className="mt-0.5 break-words text-[11px] text-muted-foreground">{detail}</p> : null}
        </div>
      </CardContent>
    </Card>
  );
}

function SectionError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-md border border-warning/30 bg-warning/5 p-4 text-sm sm:flex-row sm:items-center sm:justify-between">
      <span className="flex min-w-0 items-start gap-2"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" /><span className="break-words">{message}</span></span>
      <Button variant="outline" size="sm" className="shrink-0" onClick={onRetry}><RefreshCw className="h-3.5 w-3.5" />重试</Button>
    </div>
  );
}

function InlineEmpty({ children }: { children: string }) {
  return <p className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">{children}</p>;
}

export function ApplicationWorkspacePage() {
  const search = useSearch({ from: "/app/applications" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const namespace = search.namespace ?? "";
  const namespaces = useQuery({
    queryKey: ["namespaces", "selector"],
    queryFn: () => api.namespaces({ pageSize: 200 }),
    staleTime: 30_000,
  });
  const preferredNamespace = namespaces.data?.items.find((item) => item.name === "default")?.name ?? namespaces.data?.items[0]?.name ?? "";

  useEffect(() => {
    if (namespace || !preferredNamespace) return;
    void navigate({ to: "/applications", search: { ...search, namespace: preferredNamespace }, replace: true });
  }, [navigate, namespace, preferredNamespace, search]);

  const enabled = Boolean(namespace);
  const workloads = useQuery({
    queryKey: ["applications", namespace, "workloads"],
    queryFn: () => api.workloads({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const pods = useQuery({
    queryKey: ["applications", namespace, "pods"],
    queryFn: () => api.pods({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const services = useQuery({
    queryKey: ["applications", namespace, "services"],
    queryFn: () => api.services({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const ingresses = useQuery({
    queryKey: ["applications", namespace, "ingresses"],
    queryFn: () => api.ingresses({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const configMaps = useQuery({
    queryKey: ["applications", namespace, "configmaps"],
    queryFn: () => api.configmaps({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const claims = useQuery({
    queryKey: ["applications", namespace, "claims"],
    queryFn: () => api.persistentvolumeclaims({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const autoscalers = useQuery({
    queryKey: ["applications", namespace, "autoscalers"],
    queryFn: () => api.autoscalers(namespace),
    enabled,
    staleTime: 15_000,
  });
  const disruptionBudgets = useQuery({
    queryKey: ["applications", namespace, "disruption-budgets"],
    queryFn: () => api.disruptionBudgets(namespace),
    enabled,
    staleTime: 15_000,
  });
  const events = useQuery({
    queryKey: ["applications", namespace, "events"],
    queryFn: () => api.events({ namespace, pageSize: workspacePageSize }),
    enabled,
    staleTime: 15_000,
  });
  const capabilities = useCapabilities(namespace || undefined).data;

  const warningEvents = useMemo(() => (events.data?.items ?? [])
    .filter((event) => event.type === "Warning")
    .sort((left, right) => new Date(right.lastSeen).getTime() - new Date(left.lastSeen).getTime())
    .slice(0, 8), [events.data?.items]);

  const sectionErrors = [
    workloads.isError ? "工作负载" : "",
    pods.isError ? "Pod" : "",
    services.isError ? "Service" : "",
    ingresses.isError ? "Ingress" : "",
    configMaps.isError ? "ConfigMap" : "",
    claims.isError ? "PVC" : "",
    autoscalers.isError ? "HPA" : "",
    disruptionBudgets.isError ? "PDB" : "",
    events.isError ? "事件" : "",
  ].filter(Boolean);

  const health = useMemo(() => summarizeWorkspace({
    workloads: workloads.data?.items ?? [],
    pods: pods.data?.items ?? [],
    services: services.data?.items ?? [],
    ingresses: ingresses.data?.items ?? [],
    configMaps: configMaps.data?.items ?? [],
    claims: claims.data?.items ?? [],
    autoscalers: autoscalers.data?.items ?? [],
    disruptionBudgets: disruptionBudgets.data?.items ?? [],
    warningEvents,
    errors: sectionErrors,
  }), [autoscalers.data?.items, claims.data?.items, configMaps.data?.items, disruptionBudgets.data?.items, events.isError, ingresses.data?.items, pods.data?.items, sectionErrors.join("|"), services.data?.items, warningEvents, workloads.data?.items]);

  const [scaling, setScaling] = useState<WorkloadSummary>();
  const [replicas, setReplicas] = useState("1");
  const [restarting, setRestarting] = useState<WorkloadSummary>();
  const refresh = async () => { await queryClient.invalidateQueries({ queryKey: ["applications", namespace] }); };
  const scale = useMutation({
    mutationFn: () => api.scaleWorkload(scaling!.kind.toLowerCase(), scaling!.namespace, scaling!.name, Number(replicas)),
    onSuccess: async () => {
      toast.success(`${scaling?.name} 副本数已调整为 ${replicas}`);
      setScaling(undefined);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });
  const restart = useMutation({
    mutationFn: () => api.restartWorkload(restarting!.kind.toLowerCase(), restarting!.namespace, restarting!.name),
    onSuccess: async () => {
      toast.success(`${restarting?.name} 已触发滚动重启`);
      setRestarting(undefined);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const openScale = (workload: WorkloadSummary) => {
    setReplicas(String(workload.desired));
    setScaling(workload);
  };

  const workloadActions = (workload: WorkloadSummary): ResourceAction[] => {
    const kind = workload.kind.toLowerCase();
    const caps = workloadCapabilities(capabilities, kind);
    return [
      { label: "查看详情", icon: <ExternalLink className="h-4 w-4" />, onSelect: () => void navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind, namespace: workload.namespace, name: workload.name } }) },
      { label: "查看日志", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => void navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind, namespace: workload.namespace, name: workload.name }, search: { tab: "logs" } }) },
      { label: "扩缩容", icon: <Minus className="h-4 w-4" />, onSelect: () => openScale(workload), disabled: !caps.scale || !["deployment", "statefulset"].includes(kind) },
      { label: "滚动重启", icon: <RefreshCw className="h-4 w-4" />, onSelect: () => setRestarting(workload), disabled: !caps.update || ["job", "cronjob"].includes(kind) },
    ];
  };

  const isFetching = [workloads, pods, services, ingresses, configMaps, claims, autoscalers, disruptionBudgets, events].some((query) => query.isFetching);
  const tone = healthTone[health.status];

  if (!namespace && namespaces.isLoading) return <div className="flex min-h-[60vh] items-center justify-center"><Loader2 className="h-5 w-5 animate-spin text-primary" /></div>;
  if (!namespace) {
    return (
      <>
        <PageHeader title="应用工作台" breadcrumb="集群 / 应用工作台" description="围绕命名空间聚合工作负载、Pod、入口、配置、存储、弹性与事件。" actions={<NamespaceSelect value="" onChange={(value) => void navigate({ to: "/applications", search: { ...search, namespace: value }, replace: true })} allowAll={false} ariaLabel="选择应用命名空间" />} />
        <EmptyState title="选择一个命名空间" description="应用工作台以命名空间为应用边界。选择命名空间后，将在一个页面展示运行状态、依赖与常用运维操作。" />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={<span className="flex min-w-0 items-center gap-2"><AppWindow className="h-5 w-5 shrink-0 text-muted-foreground" /><span className="truncate">应用工作台</span></span>}
        breadcrumb="集群 / 应用工作台"
        description={`以命名空间 ${namespace} 为应用边界，集中查看运行状态、入口、依赖、弹性与最近异常。`}
        actions={<>
          <NamespaceSelect value={namespace} onChange={(value) => void navigate({ to: "/applications", search: { ...search, namespace: value }, replace: true })} allowAll={false} ariaLabel="选择应用命名空间" />
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={isFetching}>{isFetching ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}刷新</Button>
          <Button variant="outline" size="sm" asChild><Link to="/namespaces/$name" params={{ name: namespace }} search={{}}><FolderTree className="h-4 w-4" />命名空间详情</Link></Button>
        </>}
      />

      <Card className="mb-4 overflow-hidden">
        <CardContent className="flex min-w-0 flex-col gap-4 p-5 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex min-w-0 items-start gap-3">
            <span className="rounded-full bg-muted p-2.5"><HeartPulse className={`h-5 w-5 ${tone.icon}`} /></span>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2"><h2 className="text-base font-semibold">命名空间健康</h2><Badge variant={tone.badge}>{tone.label}</Badge></div>
              <p className="mt-1 break-words text-sm text-muted-foreground">{health.summary}</p>
              {health.issues.length ? <ul className="mt-3 space-y-1.5 text-sm">{health.issues.map((issue) => <li key={issue} className="flex min-w-0 items-start gap-2"><AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" /><span className="min-w-0 break-words">{issue}</span></li>)}</ul> : null}
            </div>
          </div>
          <div className="grid shrink-0 grid-cols-2 gap-2 text-center sm:grid-cols-3 lg:grid-cols-2">
            <div className="rounded-md border bg-muted/20 px-3 py-2"><p className="text-[11px] text-muted-foreground">工作负载</p><p className="font-mono text-lg font-semibold">{health.counts.readyWorkloads}/{health.counts.workloads}</p></div>
            <div className="rounded-md border bg-muted/20 px-3 py-2"><p className="text-[11px] text-muted-foreground">Pod</p><p className="font-mono text-lg font-semibold">{health.counts.readyPods}/{health.counts.pods}</p></div>
            <div className="rounded-md border bg-muted/20 px-3 py-2"><p className="text-[11px] text-muted-foreground">Warning</p><p className="font-mono text-lg font-semibold">{health.counts.warningEvents}</p></div>
            <div className="rounded-md border bg-muted/20 px-3 py-2"><p className="text-[11px] text-muted-foreground">重启</p><p className="font-mono text-lg font-semibold">{health.counts.restarts}</p></div>
          </div>
        </CardContent>
      </Card>

      {sectionErrors.length ? <div className="mb-4"><SectionError message={`部分数据加载失败：${sectionErrors.join("、")}。其他可用数据仍会正常展示。`} onRetry={() => void refresh()} /></div> : null}

      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <CountCard label="工作负载" value={`${health.counts.readyWorkloads}/${health.counts.workloads}`} detail="就绪 / 总数" icon={Boxes} />
        <CountCard label="Pod" value={`${health.counts.readyPods}/${health.counts.pods}`} detail={`${health.counts.restarts} 次重启`} icon={CircleDot} />
        <CountCard label="Service" value={String(health.counts.services)} detail="服务入口" icon={Network} />
        <CountCard label="Ingress" value={String(health.counts.ingresses)} detail="外部路由" icon={Route} />
        <CountCard label="配置与存储" value={`${health.counts.configMaps}/${health.counts.claims}`} detail="ConfigMap / PVC" icon={Settings2} />
        <CountCard label="弹性与预算" value={`${health.counts.autoscalers}/${health.counts.disruptionBudgets}`} detail="HPA / PDB" icon={Activity} />
      </div>

      <Card className="mb-4">
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div><CardTitle className="flex items-center gap-2"><Boxes className="h-4 w-4 text-muted-foreground" />工作负载</CardTitle><CardDescription>可直接查看详情、日志、扩缩容或滚动重启。</CardDescription></div>
          <Button variant="ghost" size="sm" asChild><Link to="/workloads" search={{ namespace }}>查看全部<ExternalLink className="h-3.5 w-3.5" /></Link></Button>
        </CardHeader>
        <CardContent className="px-0 pb-0">
          {workloads.isLoading ? <div className="flex h-32 items-center justify-center"><Loader2 className="h-5 w-5 animate-spin text-primary" /></div> : workloads.isError ? <div className="px-5 pb-5"><SectionError message="工作负载加载失败。" onRetry={() => void workloads.refetch()} /></div> : workloads.data?.items.length ? (
            <Table>
              <TableHeader><TableRow><TableHead>名称</TableHead><TableHead>类型</TableHead><TableHead>状态</TableHead><TableHead>就绪</TableHead><TableHead>重启</TableHead><TableHead>镜像</TableHead><TableHead>年龄</TableHead><TableHead className="w-14">操作</TableHead></TableRow></TableHeader>
              <TableBody>
                {workloads.data.items.slice(0, 12).map((workload) => (
                  <TableRow key={`${workload.kind}-${workload.name}`}>
                    <TableCell><Link to="/workloads/$kind/$namespace/$name" params={{ kind: workload.kind.toLowerCase(), namespace, name: workload.name }} search={{}} className="font-medium text-primary hover:underline">{workload.name}</Link></TableCell>
                    <TableCell><span className="font-mono text-xs">{workload.kind}</span></TableCell>
                    <TableCell><StatusBadge status={workload.status} /></TableCell>
                    <TableCell><span className="font-mono text-xs">{workload.ready}</span></TableCell>
                    <TableCell><span className="tabular-nums">{workload.restarts}</span></TableCell>
                    <TableCell><span className="block max-w-64 truncate font-mono text-xs text-muted-foreground" title={workload.images.join("\n")}>{workload.images.join(", ") || "—"}</span></TableCell>
                    <TableCell className="text-muted-foreground">{formatAge(workload.createdAt)}</TableCell>
                    <TableCell><ResourceActionsMenu resource={`${workload.kind} ${namespace}/${workload.name}`} actions={workloadActions(workload)} /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : <div className="px-5 pb-5"><InlineEmpty>该命名空间没有工作负载</InlineEmpty></div>}
          {(workloads.data?.items.length ?? 0) > 12 ? <p className="border-t px-5 py-3 text-xs text-muted-foreground">当前展示前 12 个工作负载，共 {workloads.data?.meta.total ?? workloads.data?.items.length} 个。</p> : null}
        </CardContent>
      </Card>

      <div className="mb-4 grid min-w-0 gap-4 xl:grid-cols-2">
        <Card className="min-w-0">
          <CardHeader className="flex-row items-start justify-between gap-3"><div><CardTitle className="flex items-center gap-2"><CircleDot className="h-4 w-4 text-muted-foreground" />Pod 实例</CardTitle><CardDescription>按最近就绪状态快速定位实例。</CardDescription></div><Button variant="ghost" size="sm" asChild><Link to="/pods" search={{ namespace }}>查看全部<ExternalLink className="h-3.5 w-3.5" /></Link></Button></CardHeader>
          <CardContent className="px-0 pb-0">
            {pods.isLoading ? <div className="flex h-28 items-center justify-center"><Loader2 className="h-5 w-5 animate-spin text-primary" /></div> : pods.isError ? <div className="px-5 pb-5"><SectionError message="Pod 加载失败。" onRetry={() => void pods.refetch()} /></div> : pods.data?.items.length ? (
              <Table>
                <TableHeader><TableRow><TableHead>名称</TableHead><TableHead>状态</TableHead><TableHead>就绪</TableHead><TableHead>重启</TableHead><TableHead>节点</TableHead></TableRow></TableHeader>
                <TableBody>{pods.data.items.slice(0, 8).map((pod) => <TableRow key={pod.name}><TableCell><Link to="/pods/$namespace/$name" params={{ namespace, name: pod.name }} search={{}} className="font-medium text-primary hover:underline">{pod.name}</Link></TableCell><TableCell><StatusBadge status={pod.status} /></TableCell><TableCell><span className="font-mono text-xs">{pod.ready}</span></TableCell><TableCell><span className="tabular-nums">{pod.restarts}</span></TableCell><TableCell>{pod.node ? <Link to="/nodes/$name" params={{ name: pod.node }} search={{}} className="font-mono text-xs text-primary hover:underline">{pod.node}</Link> : "—"}</TableCell></TableRow>)}</TableBody>
              </Table>
            ) : <div className="px-5 pb-5"><InlineEmpty>该命名空间没有活跃 Pod</InlineEmpty></div>}
          </CardContent>
        </Card>

        <Card className="min-w-0">
          <CardHeader><CardTitle className="flex items-center gap-2"><AlertTriangle className="h-4 w-4 text-warning" />近期 Warning 事件</CardTitle><CardDescription>按最近发生时间展示，最多 8 条。</CardDescription></CardHeader>
          <CardContent>
            {events.isLoading ? <div className="flex h-24 items-center justify-center"><Loader2 className="h-5 w-5 animate-spin text-primary" /></div> : events.isError ? <SectionError message="事件加载失败。" onRetry={() => void events.refetch()} /> : warningEvents.length ? (
              <ul className="space-y-3">{warningEvents.map((event) => <li key={event.uid} className="min-w-0 rounded-md border p-3"><div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><EventObjectLink event={event} /><span className="text-xs text-muted-foreground">{formatAge(event.lastSeen)}</span></div><p className="mt-1 break-words text-xs font-medium text-warning">{event.reason}</p><p className="mt-1 break-words text-xs text-muted-foreground">{event.message || "—"}</p></li>)}</ul>
            ) : <InlineEmpty>该命名空间最近没有 Warning 事件</InlineEmpty>}
            <Button variant="ghost" size="sm" className="mt-3 px-0" asChild><Link to="/events" search={{ namespace }}>打开事件中心<ExternalLink className="h-3.5 w-3.5" /></Link></Button>
          </CardContent>
        </Card>
      </div>

      <div className="grid min-w-0 gap-4 xl:grid-cols-2">
        <Card className="min-w-0">
          <CardHeader><CardTitle className="flex items-center gap-2"><Network className="h-4 w-4 text-muted-foreground" />Service 与 Ingress</CardTitle><CardDescription>应用入口和内部访问关系。</CardDescription></CardHeader>
          <CardContent className="space-y-5">
            <div>
              <div className="mb-2 flex items-center justify-between gap-2"><h3 className="text-sm font-medium">Service ({health.counts.services})</h3><Button variant="ghost" size="sm" asChild><Link to="/network" search={{ namespace }}>管理<ExternalLink className="h-3.5 w-3.5" /></Link></Button></div>
              {services.isError ? <SectionError message="Service 加载失败。" onRetry={() => void services.refetch()} /> : services.data?.items.length ? <ul className="space-y-2">{services.data.items.slice(0, 6).map((service) => <li key={service.name} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border p-3"><div className="min-w-0"><Link to="/network/services/$namespace/$name" params={{ namespace, name: service.name }} search={{}} className="font-medium text-primary hover:underline">{service.name}</Link><p className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">{service.type} · {service.clusterIP} · {service.ports.map((port) => `${port.port}/${port.protocol}`).join(", ") || "无端口"}</p></div><Badge variant="outline">{service.endpointCount} endpoints</Badge></li>)}</ul> : <InlineEmpty>没有 Service</InlineEmpty>}
            </div>
            <div>
              <div className="mb-2 flex items-center justify-between gap-2"><h3 className="text-sm font-medium">Ingress ({health.counts.ingresses})</h3></div>
              {ingresses.isError ? <SectionError message="Ingress 加载失败。" onRetry={() => void ingresses.refetch()} /> : ingresses.data?.items.length ? <ul className="space-y-2">{ingresses.data.items.slice(0, 6).map((ingress) => <li key={ingress.name} className="min-w-0 rounded-md border p-3"><div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><Link to="/network/ingresses/$namespace/$name" params={{ namespace, name: ingress.name }} search={{}} className="font-medium text-primary hover:underline">{ingress.name}</Link><Badge variant="outline">{ingress.class || "默认 Class"}</Badge></div><p className="mt-1 break-all font-mono text-[11px] text-muted-foreground">{ingress.paths.map((path) => `${path.host || "*"}${path.path} → ${path.service}:${path.port}`).join(" · ") || "未配置路由"}</p></li>)}</ul> : <InlineEmpty>没有 Ingress</InlineEmpty>}
            </div>
          </CardContent>
        </Card>

        <Card className="min-w-0">
          <CardHeader><CardTitle className="flex items-center gap-2"><Settings2 className="h-4 w-4 text-muted-foreground" />配置、存储与弹性</CardTitle><CardDescription>应用依赖和可用性保护。</CardDescription></CardHeader>
          <CardContent className="space-y-5">
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="min-w-0 rounded-md border p-3"><div className="mb-2 flex items-center gap-2 text-sm font-medium"><Settings2 className="h-4 w-4 text-muted-foreground" />ConfigMap</div>{configMaps.isError ? <p className="text-xs text-warning">加载失败</p> : configMaps.data?.items.length ? <ul className="space-y-1.5">{configMaps.data.items.slice(0, 5).map((item) => <li key={item.name} className="min-w-0"><Link to="/configuration" search={{ namespace, tab: "configmaps", q: item.name }} className="block truncate text-sm text-primary hover:underline">{item.name}</Link><span className="text-[11px] text-muted-foreground">{item.keys.length} 个键 · {item.dataBytes} B</span></li>)}</ul> : <p className="text-xs text-muted-foreground">没有 ConfigMap</p>}</div>
              <div className="min-w-0 rounded-md border p-3"><div className="mb-2 flex items-center gap-2 text-sm font-medium"><Database className="h-4 w-4 text-muted-foreground" />PersistentVolumeClaim</div>{claims.isError ? <p className="text-xs text-warning">加载失败</p> : claims.data?.items.length ? <ul className="space-y-2">{claims.data.items.slice(0, 5).map((claim) => <li key={claim.name} className="min-w-0"><Link to="/storage/claims/$namespace/$name" params={{ namespace, name: claim.name }} search={{}} className="block truncate text-sm text-primary hover:underline">{claim.name}</Link><div className="mt-1 flex flex-wrap items-center gap-1.5"><StatusBadge status={claim.status} /><span className="text-[11px] text-muted-foreground">{formatMemory(claim.capacityBytes)}</span></div></li>)}</ul> : <p className="text-xs text-muted-foreground">没有 PVC</p>}</div>
            </div>
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="min-w-0 rounded-md border p-3"><div className="mb-2 flex items-center gap-2 text-sm font-medium"><Gauge className="h-4 w-4 text-muted-foreground" />HPA</div>{autoscalers.isError ? <p className="text-xs text-warning">加载失败</p> : autoscalers.data?.items.length ? <ul className="space-y-1.5">{autoscalers.data.items.slice(0, 5).map((item) => <li key={item.name} className="min-w-0"><Link to="/resilience" search={{ namespace, tab: "hpa", q: item.name }} className="block truncate text-sm text-primary hover:underline">{item.name}</Link><span className="block truncate text-[11px] text-muted-foreground">{item.targetKind}/{item.targetName} · {item.currentReplicas}/{item.maxReplicas}</span></li>)}</ul> : <p className="text-xs text-muted-foreground">没有 HPA</p>}</div>
              <div className="min-w-0 rounded-md border p-3"><div className="mb-2 flex items-center gap-2 text-sm font-medium"><Activity className="h-4 w-4 text-muted-foreground" />PDB</div>{disruptionBudgets.isError ? <p className="text-xs text-warning">加载失败</p> : disruptionBudgets.data?.items.length ? <ul className="space-y-1.5">{disruptionBudgets.data.items.slice(0, 5).map((item) => <li key={item.name} className="min-w-0"><Link to="/resilience" search={{ namespace, tab: "pdb", q: item.name }} className="block truncate text-sm text-primary hover:underline">{item.name}</Link><span className="block truncate text-[11px] text-muted-foreground">允许驱逐 {item.disruptionsAllowed} · 健康 {item.currentHealthy}/{item.expectedPods}</span></li>)}</ul> : <p className="text-xs text-muted-foreground">没有 PDB</p>}</div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Dialog open={Boolean(scaling)} onOpenChange={(open) => { if (!open) setScaling(undefined); }}>
        <DialogContent className="max-w-md">
          <DialogHeader><DialogTitle>调整副本数</DialogTitle><DialogDescription>{scaling?.namespace}/{scaling?.name} · 当前期望副本数 {scaling?.desired}</DialogDescription></DialogHeader>
          <div className="space-y-2"><Label htmlFor="workspace-replicas">期望副本数</Label><Input id="workspace-replicas" inputMode="numeric" value={replicas} onChange={(event) => setReplicas(event.target.value)} /></div>
          <DialogFooter><Button variant="outline" onClick={() => setScaling(undefined)} disabled={scale.isPending}>取消</Button><Button onClick={() => scale.mutate()} disabled={scale.isPending || !/^\d+$/.test(replicas) || Number(replicas) > 10000}>{scale.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认调整</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(restarting)} onOpenChange={(open) => { if (!open) setRestarting(undefined); }}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>滚动重启 {restarting?.name}？</AlertDialogTitle><AlertDialogDescription>将更新 Pod 模板注解并逐步替换实例。服务可用性取决于副本数与探针配置。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel disabled={restart.isPending}>取消</AlertDialogCancel><AlertDialogAction onClick={(event) => { event.preventDefault(); restart.mutate(); }} disabled={restart.isPending}>{restart.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认重启</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
    </>
  );
}
