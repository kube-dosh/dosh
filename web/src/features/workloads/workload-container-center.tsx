import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Activity, Boxes, FileCode2, Loader2, Pencil, RadioTower, RefreshCw,
  ScrollText, Server, TerminalSquare,
} from "lucide-react";
import { api } from "@/api/client";
import type { WorkloadContainerInstance, WorkloadContainerState } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ResourceEvents } from "@/features/events/resource-events";
import { PodTerminal } from "@/features/workloads/pod-terminal";
import { PortForwardPanel } from "@/features/workloads/port-forward-panel";
import { WorkloadLogViewer } from "@/features/workloads/workload-log-viewer";
import { useCapabilities } from "@/features/auth/use-session";
import { formatCPU, formatDate, formatMemory } from "@/lib/utils";
import { cn } from "@/lib/utils";

type ContainerDialog = { type: "logs" | "terminal" | "forward" | "events"; container: WorkloadContainerState; pod?: WorkloadContainerInstance };

function stateTone(state: string): "success" | "warning" | "secondary" {
  if (state === "Running") return "success";
  if (state === "Waiting" || state === "Pending") return "warning";
  if (state === "Terminated") return "secondary";
  return "secondary";
}

function containerTypeLabel(container: WorkloadContainerState): string {
  if (container.sidecar) return "Sidecar / Init";
  if (container.type === "initContainer") return "Init Container";
  return "业务容器";
}

function aggregateState(container: WorkloadContainerState): string {
  if (!container.runtime.pods) return "无实例";
  if (container.runtime.running > 0) return "Running";
  if (container.runtime.terminated > 0) return "Terminated";
  return container.instances[0]?.state || "Pending";
}

function preferredInstance(container: WorkloadContainerState): WorkloadContainerInstance | undefined {
  return container.instances.find((instance) => instance.state === "Running") ?? container.instances[0];
}

export function WorkloadContainerCenter({ kind, namespace, name, onEditTemplate, onUpdateImages }: {
  kind: string;
  namespace: string;
  name: string;
  onEditTemplate: (containerName: string) => void;
  onUpdateImages: (containerName: string) => void;
}) {
  const [search, setSearch] = useState("");
  const [type, setType] = useState<"all" | "container" | "init">("all");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [dialog, setDialog] = useState<ContainerDialog | null>(null);
  const capabilities = useCapabilities(namespace).data;
  const query = useQuery({
    queryKey: ["workload-container-inventory", kind, namespace, name],
    queryFn: () => api.workloadContainerInventory(kind, namespace, name),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  });
  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return (query.data?.containers ?? []).filter((container) => {
      if (type === "container" && (container.init || container.sidecar)) return false;
      if (type === "init" && !container.init) return false;
      if (!needle) return true;
      return `${container.name} ${container.image} ${container.instances.map((instance) => instance.pod).join(" ")}`.toLowerCase().includes(needle);
    });
  }, [query.data?.containers, search, type]);
  const runningInstances = query.data?.containers.reduce((total, container) => total + container.runtime.running, 0) ?? 0;
  const instanceCount = query.data?.containers.reduce((total, container) => total + container.runtime.pods, 0) ?? 0;
  const editable = Boolean(query.data?.templateEditable);
  const canExec = Boolean(capabilities?.podExec);
  const canPortForward = Boolean(capabilities?.podPortForward);

  if (query.isLoading) {
    return <div className="flex min-h-56 items-center justify-center gap-2 rounded-lg border bg-card text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在汇总容器与实例</div>;
  }
  if (query.isError) {
    return <div role="alert" className="rounded-lg border border-destructive/35 bg-destructive/5 p-4 text-sm text-destructive-strong">读取容器信息失败：{query.error.message}<Button variant="outline" size="sm" className="ml-3" onClick={() => void query.refetch()}><RefreshCw className="h-3.5 w-3.5" />重试</Button></div>;
  }
  if (!query.data) return null;

  return (
    <div className="min-w-0 space-y-4" data-testid="workload-container-center">
      <div className="rounded-lg border bg-card p-3 sm:p-4">
        <div className="flex min-w-0 flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <Badge variant="secondary">{query.data.containers.length} 个容器</Badge>
            <Badge variant="outline">{instanceCount} 个实例</Badge>
            <Badge variant="success">{runningInstances} 个运行中</Badge>
            <Badge variant={query.data.metricsAvailable ? "success" : "outline"}>{query.data.metricsAvailable ? "容器指标可用" : "指标暂不可用"}</Badge>
            <span className="text-xs text-muted-foreground">刷新于 {formatDate(query.data.refreshedAt)}</span>
          </div>
          <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
            <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索容器、镜像或 Pod" aria-label="搜索容器、镜像或 Pod" className="min-w-0 sm:w-64" />
            <div className="flex rounded-md border p-0.5" role="group" aria-label="容器类型筛选">
              {([["all", "全部"], ["container", "业务容器"], ["init", "Init / Sidecar"]] as const).map(([value, label]) => <button key={value} type="button" className={cn("min-w-0 flex-1 rounded px-2.5 py-1.5 text-xs font-medium text-muted-foreground", type === value && "bg-muted text-foreground")} onClick={() => setType(value)}>{label}</button>)}
            </div>
            <Button variant="outline" size="icon" aria-label="刷新容器信息" onClick={() => void query.refetch()} disabled={query.isFetching}><RefreshCw className={cn("h-4 w-4", query.isFetching && "animate-spin")} /></Button>
          </div>
        </div>
        {query.data.warnings?.length ? <div className="mt-3 min-w-0 space-y-1 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">{query.data.warnings.map((warning) => <p key={warning} className="break-words">{warning}</p>)}</div> : null}
      </div>

      {!editable ? <div className="rounded-lg border border-dashed bg-muted/20 px-4 py-3 text-sm text-muted-foreground">当前工作负载的 Pod 模板只读；仍可对运行实例执行日志、终端、端口转发和事件排查。</div> : null}

      {rows.length ? <div className="grid min-w-0 gap-3">
        {rows.map((container) => {
          const isOpen = Boolean(expanded[container.name]);
          const instance = preferredInstance(container);
          const canOpenTerminal = canExec && Boolean(instance?.state === "Running");
          const canOpenForward = canPortForward && Boolean(instance?.state === "Running");
          return <section key={container.name} className="min-w-0 overflow-hidden rounded-lg border bg-card" data-testid={`container-${container.name}`}>
            <div className="min-w-0 p-3 sm:p-4">
              <div className="flex min-w-0 flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <Boxes className={cn("h-4 w-4 shrink-0", container.runtime.running ? "text-emerald-500" : "text-muted-foreground")} />
                    <h3 className="min-w-0 break-all font-mono text-sm font-semibold">{container.name}</h3>
                    <Badge variant={container.type === "container" ? "secondary" : "outline"}>{containerTypeLabel(container)}</Badge>
                    <Badge variant={stateTone(aggregateState(container))}>{aggregateState(container)}</Badge>
                    {container.runtime.ready ? <Badge variant="success">{container.runtime.ready}/{container.runtime.pods} Ready</Badge> : null}
                    {container.runtime.restarts ? <Badge variant="warning">{container.runtime.restarts} 次重启</Badge> : null}
                  </div>
                  <p className="break-all font-mono text-xs leading-5 text-muted-foreground">{container.image || "未配置镜像"}</p>
                  <div className="flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span>{container.runtime.pods} 个实例 · {container.runtime.running} 运行中</span>
                    <span>CPU {formatCPU(container.resources.cpuRequestMilli ?? 0)} / {formatCPU(container.resources.cpuLimitMilli ?? 0)}</span>
                    <span>内存 {formatMemory(container.resources.memoryRequestBytes ?? 0)} / {formatMemory(container.resources.memoryLimitBytes ?? 0)}</span>
                    <span>{container.ports.length} 个端口 · {container.env.length} 个环境变量 · {container.volumeMounts.length} 个挂载</span>
                    {container.runtime.metricsAvailable ? <span>当前使用 CPU {formatCPU(container.runtime.cpuUsageMilli ?? 0)} · 内存 {formatMemory(container.runtime.memoryUsageBytes ?? 0)}</span> : null}
                  </div>
                </div>
                <div className="flex min-w-0 flex-wrap gap-1.5 xl:max-w-[26rem] xl:justify-end">
                  <Button variant="outline" size="sm" onClick={() => setExpanded((current) => ({ ...current, [container.name]: !current[container.name] }))} aria-expanded={isOpen}><Server className="h-3.5 w-3.5" />{isOpen ? "收起详情" : "详情与实例"}</Button>
                  {editable ? <Button variant="outline" size="sm" onClick={() => onUpdateImages(container.name)}><Pencil className="h-3.5 w-3.5" />更新镜像</Button> : null}
                  {editable ? <Button variant="outline" size="sm" onClick={() => onEditTemplate(container.name)}><FileCode2 className="h-3.5 w-3.5" />编辑配置</Button> : null}
                  <Button variant="outline" size="sm" disabled={!container.instances.length} onClick={() => setDialog({ type: "logs", container, pod: preferredInstance(container) })}><ScrollText className="h-3.5 w-3.5" />日志</Button>
                  <Button variant="outline" size="sm" disabled={!canOpenTerminal} title={!canExec ? "需要 create pods/exec 权限" : !instance ? "当前没有可连接的运行实例" : undefined} onClick={() => instance && setDialog({ type: "terminal", container, pod: instance })}><TerminalSquare className="h-3.5 w-3.5" />终端</Button>
                  <Button variant="outline" size="sm" disabled={!canOpenForward} title={!canPortForward ? "需要 create pods/portforward 权限" : !instance ? "当前没有运行实例" : undefined} onClick={() => instance && setDialog({ type: "forward", container, pod: instance })}><RadioTower className="h-3.5 w-3.5" />端口转发</Button>
                  <Button variant="outline" size="sm" disabled={!instance} onClick={() => instance && setDialog({ type: "events", container, pod: instance })}><Activity className="h-3.5 w-3.5" />事件</Button>
                </div>
              </div>
            </div>

            {isOpen ? <div className="min-w-0 border-t bg-muted/10 p-3 sm:p-4">
              <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
                <div className="min-w-0 space-y-3 rounded-md border bg-card p-3">
                  <h4 className="text-sm font-semibold">声明式配置</h4>
                  <dl className="grid min-w-0 grid-cols-2 gap-3 text-xs">
                    <div className="min-w-0"><dt className="text-muted-foreground">CPU Request / Limit</dt><dd className="mt-0.5 break-words font-mono">{formatCPU(container.resources.cpuRequestMilli ?? 0)} / {formatCPU(container.resources.cpuLimitMilli ?? 0)}</dd></div>
                    <div className="min-w-0"><dt className="text-muted-foreground">内存 Request / Limit</dt><dd className="mt-0.5 break-words font-mono">{formatMemory(container.resources.memoryRequestBytes ?? 0)} / {formatMemory(container.resources.memoryLimitBytes ?? 0)}</dd></div>
                  </dl>
                  <div><p className="mb-1.5 text-xs text-muted-foreground">端口</p>{container.ports.length ? <div className="flex min-w-0 flex-wrap gap-1.5">{container.ports.map((port) => <Badge key={`${port.protocol}-${port.containerPort}-${port.name ?? ""}`} variant="outline" className="max-w-full whitespace-normal break-all font-mono">{port.name ? `${port.name}:` : ""}{port.containerPort}/{port.protocol}</Badge>)}</div> : <p className="text-xs text-muted-foreground">未声明端口</p>}</div>
                  <div><p className="mb-1.5 text-xs text-muted-foreground">环境变量</p>{container.env.length ? <div className="flex min-w-0 flex-wrap gap-1.5">{container.env.map((env) => <Badge key={env.name} variant="outline" className="max-w-full whitespace-normal break-all font-mono">{env.name}{env.value ? `=${env.value}` : env.valueFrom ? ` ← ${env.valueFrom.kind}${env.valueFrom.name ? `/${env.valueFrom.name}` : ""}` : ""}</Badge>)}</div> : <p className="text-xs text-muted-foreground">未配置环境变量</p>}</div>
                  <div><p className="mb-1.5 text-xs text-muted-foreground">卷挂载</p>{container.volumeMounts.length ? <div className="space-y-1.5">{container.volumeMounts.map((mount) => <div key={`${mount.name}-${mount.mountPath}`} className="min-w-0 rounded border bg-muted/20 px-2 py-1.5 font-mono text-[11px]"><span className="break-all">{mount.name} → {mount.mountPath}</span>{mount.subPath ? <span className="break-all text-muted-foreground"> · {mount.subPath}</span> : null}{mount.readOnly ? <Badge variant="outline" className="ml-2">只读</Badge> : null}</div>)}</div> : <p className="text-xs text-muted-foreground">未配置卷挂载</p>}</div>
                  <div><p className="mb-1.5 text-xs text-muted-foreground">健康检查</p><div className="flex min-w-0 flex-wrap gap-1.5">{container.livenessProbe ? <Badge variant="outline">Liveness · {container.livenessProbe.type}</Badge> : null}{container.readinessProbe ? <Badge variant="outline">Readiness · {container.readinessProbe.type}</Badge> : null}{container.startupProbe ? <Badge variant="outline">Startup · {container.startupProbe.type}</Badge> : null}{!container.livenessProbe && !container.readinessProbe && !container.startupProbe ? <span className="text-xs text-muted-foreground">未配置探针</span> : null}</div></div>
                </div>
                <div className="min-w-0 space-y-2">
                  <h4 className="text-sm font-semibold">运行实例 ({container.instances.length})</h4>
                  {container.instances.length ? container.instances.map((instance) => <InstanceCard key={`${instance.namespace}/${instance.pod}`} instance={instance} namespace={namespace} />) : <div className="rounded-md border border-dashed px-3 py-8 text-center text-sm text-muted-foreground">模板存在，但当前没有匹配的 Pod 实例。</div>}
                </div>
              </div>
            </div> : null}
          </section>;
        })}
      </div> : <div className="rounded-lg border border-dashed bg-card px-4 py-12 text-center text-sm text-muted-foreground">没有符合筛选条件的容器。</div>}

      <ContainerLogDialog dialog={dialog?.type === "logs" ? dialog : null} onClose={() => setDialog(null)} kind={kind} namespace={namespace} name={name} containers={query.data.containers} />
      <ContainerTerminalDialog dialog={dialog?.type === "terminal" ? dialog : null} onClose={() => setDialog(null)} namespace={namespace} canDebug={Boolean(capabilities?.podEphemeralDebug)} />
      <ContainerForwardDialog dialog={dialog?.type === "forward" ? dialog : null} onClose={() => setDialog(null)} namespace={namespace} canPortForward={canPortForward} />
      <Dialog open={dialog?.type === "events"} onOpenChange={(open) => { if (!open) setDialog(null); }}>
        <DialogContent className="max-h-[88dvh] w-[calc(100vw-1rem)] max-w-5xl overflow-y-auto">
          <DialogHeader><DialogTitle className="break-all">Pod 事件 · {dialog?.pod?.pod}</DialogTitle><DialogDescription>容器 {dialog?.container.name} 的运行实例事件，用于定位调度、拉取、启动和健康检查问题。</DialogDescription></DialogHeader>
          {dialog?.pod ? <ResourceEvents namespace={dialog.pod.namespace} objectKind="Pod" objectName={dialog.pod.pod} /> : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function InstanceCard({ instance, namespace }: { instance: WorkloadContainerInstance; namespace: string }) {
  return <div className="min-w-0 rounded-md border bg-card p-3" data-testid="container-instance">
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <Link to="/pods/$namespace/$name" params={{ namespace: instance.namespace || namespace, name: instance.pod }} className="min-w-0 break-all font-mono text-xs font-semibold text-primary hover:underline">{instance.pod}</Link>
      <Badge variant={stateTone(instance.state)}>{instance.state}</Badge>
      <Badge variant={instance.ready ? "success" : "outline"}>{instance.ready ? "Ready" : "NotReady"}</Badge>
      {instance.restartCount ? <Badge variant="warning">重启 {instance.restartCount}</Badge> : null}
      {instance.job ? <Badge variant="outline" className="max-w-full whitespace-normal break-all">Job {instance.job}</Badge> : null}
    </div>
    <p className="mt-2 break-all font-mono text-[11px] leading-5 text-muted-foreground">{instance.image}</p>
    <div className="mt-2 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
      <span>节点 {instance.node || "待调度"}</span>
      {instance.reason ? <span className="break-words">原因 {instance.reason}</span> : null}
      {instance.metricsAvailable ? <span>CPU {formatCPU(instance.cpuUsageMilli ?? 0)} · 内存 {formatMemory(instance.memoryUsageBytes ?? 0)}</span> : <span>指标不可用</span>}
      {instance.startedAt ? <span>启动于 {formatDate(instance.startedAt)}</span> : null}
    </div>
    {instance.message ? <p className="mt-2 break-words rounded border border-warning/25 bg-warning/5 px-2 py-1.5 text-[11px] leading-5 text-warning">{instance.message}</p> : null}
  </div>;
}

function ContainerLogDialog({ dialog, onClose, kind, namespace, name, containers }: {
  dialog: ContainerDialog | null;
  onClose: () => void;
  kind: string;
  namespace: string;
  name: string;
  containers: WorkloadContainerState[];
}) {
  return <Dialog open={Boolean(dialog)} onOpenChange={(open) => { if (!open) onClose(); }}>
    <DialogContent className="h-[100dvh] w-screen max-w-none gap-0 overflow-hidden p-0 sm:h-[min(92dvh,900px)] sm:w-[calc(100%-2rem)] sm:max-w-6xl sm:rounded-lg">
      <DialogHeader className="shrink-0 border-b px-4 py-4 sm:px-6"><DialogTitle className="break-all">容器日志 · {dialog?.container.name}</DialogTitle><DialogDescription className="break-words">{namespace}/{name} · 聚合该容器在所有匹配实例上的日志，并可直接切换到 Init / Sidecar。</DialogDescription></DialogHeader>
      <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-4">{dialog ? <WorkloadLogViewer kind={kind} namespace={namespace} name={name} containers={containers.map((container) => ({ name: container.name, init: container.init }))} initialContainer={dialog.container.name} /> : null}</div>
    </DialogContent>
  </Dialog>;
}

function ContainerTerminalDialog({ dialog, onClose, namespace, canDebug }: { dialog: ContainerDialog | null; onClose: () => void; namespace: string; canDebug: boolean }) {
  const [instanceKey, setInstanceKey] = useState("");
  const instances = dialog?.container.instances.filter((instance) => instance.state === "Running") ?? [];
  const selected = instances.find((instance) => `${instance.namespace}/${instance.pod}` === instanceKey) ?? instances[0] ?? dialog?.pod;
  const podQuery = useQuery({
    queryKey: ["pod", selected?.namespace ?? namespace, selected?.pod],
    queryFn: () => api.pod(selected!.namespace || namespace, selected!.pod),
    enabled: Boolean(dialog && selected?.pod),
    refetchInterval: (current) => current.state.data?.containers.some((container) => container.ephemeral && container.state !== "Running") ? 2_000 : 20_000,
  });
  return <Dialog open={Boolean(dialog)} onOpenChange={(open) => { if (!open) { setInstanceKey(""); onClose(); } }}>
    <DialogContent className="h-[100dvh] w-screen max-w-none gap-0 overflow-hidden p-0 sm:h-[min(94dvh,900px)] sm:w-[calc(100%-2rem)] sm:max-w-6xl sm:rounded-lg">
      <DialogHeader className="shrink-0 border-b px-4 py-4 pr-12 sm:px-6"><DialogTitle className="break-all">容器终端 · {dialog?.container.name}</DialogTitle><DialogDescription className="break-words">{selected?.namespace || namespace}/{selected?.pod} · 可从当前容器的运行实例中选择指定 Pod 进入。</DialogDescription></DialogHeader>
      {instances.length > 1 ? <div className="shrink-0 border-b px-4 py-3 sm:px-6"><p className="mb-2 text-xs font-medium text-muted-foreground">选择运行实例</p><div className="flex min-w-0 flex-wrap gap-1.5">{instances.map((instance) => <button key={`${instance.namespace}/${instance.pod}`} type="button" className={cn("min-w-0 max-w-full break-all rounded-md border px-2.5 py-1.5 text-left font-mono text-xs", selected?.pod === instance.pod && "border-primary bg-primary/5")} onClick={() => setInstanceKey(`${instance.namespace}/${instance.pod}`)}>{instance.pod}</button>)}</div></div> : null}
      <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-4">
        {podQuery.isLoading ? <div className="flex h-48 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在读取 Pod 容器</div> : null}
        {podQuery.isError ? <div role="alert" className="rounded-md border border-destructive/35 p-3 text-sm text-destructive-strong">{podQuery.error.message}</div> : null}
        {podQuery.data && dialog ? <PodTerminal key={`${podQuery.data.namespace}/${podQuery.data.name}`} namespace={podQuery.data.namespace} name={podQuery.data.name} containers={podQuery.data.containers} canDebugContainer={canDebug} onContainerCreated={() => podQuery.refetch()} initialContainer={dialog.container.name} /> : null}
      </div>
    </DialogContent>
  </Dialog>;
}

function ContainerForwardDialog({ dialog, onClose, namespace, canPortForward }: { dialog: ContainerDialog | null; onClose: () => void; namespace: string; canPortForward: boolean }) {
  const [instanceKey, setInstanceKey] = useState("");
  const instances = dialog?.container.instances.filter((instance) => instance.state === "Running") ?? [];
  const selected = instances.find((instance) => `${instance.namespace}/${instance.pod}` === instanceKey) ?? instances[0];
  const ports = dialog?.container.ports.map((port) => ({ port: port.containerPort, protocol: port.protocol, label: `${port.name || "端口"} ${port.containerPort}/${port.protocol}` })) ?? [];
  return <Dialog open={Boolean(dialog)} onOpenChange={(open) => { if (!open) { setInstanceKey(""); onClose(); } }}>
    <DialogContent className="max-h-[92dvh] w-[calc(100vw-1rem)] max-w-4xl overflow-y-auto">
      <DialogHeader><DialogTitle className="break-all">端口转发 · {dialog?.container.name}</DialogTitle><DialogDescription>隧道从 Dosh 容器访问指定 Pod 端口，不会修改工作负载或暴露公网入口。</DialogDescription></DialogHeader>
      {instances.length > 1 ? <div className="min-w-0 space-y-1.5"><p className="text-xs font-medium text-muted-foreground">选择运行实例</p><div className="flex min-w-0 flex-wrap gap-1.5">{instances.map((instance) => <button key={`${instance.namespace}/${instance.pod}`} type="button" className={cn("min-w-0 max-w-full break-all rounded-md border px-2.5 py-1.5 text-left font-mono text-xs", selected?.pod === instance.pod && "border-primary bg-primary/5")} onClick={() => setInstanceKey(`${instance.namespace}/${instance.pod}`)}>{instance.pod}</button>)}</div></div> : null}
      {selected ? <PortForwardPanel namespace={selected.namespace || namespace} name={selected.pod} kind="pod" canPortForward={canPortForward} ports={ports} /> : <p className="rounded-md border border-dashed px-3 py-8 text-center text-sm text-muted-foreground">该容器当前没有 Running 实例，无法建立端口转发。</p>}
    </DialogContent>
  </Dialog>;
}
