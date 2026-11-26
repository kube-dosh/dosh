import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, ExternalLink, Loader2, LockKeyhole, Network, RadioTower, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { PortForwardSession } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

export interface SuggestedPort {
  port: number;
  label: string;
  protocol?: string;
}

const MAX_PORTS = 8;

function tunnelEndpoints(session: PortForwardSession) {
  return session.ports?.length
    ? session.ports
    : [{
        id: session.id,
        namespace: session.namespace,
        pod: session.pod,
        service: session.service,
        requestedPort: session.port,
        port: session.port,
        protocol: "TCP" as const,
        path: session.path,
        expiresAt: session.expiresAt,
      }];
}

function podReady(pod: { status: string; ready: string }) {
  if (pod.status !== "Running") return false;
  const [ready, total] = pod.ready.split("/").map(Number);
  return Number.isFinite(ready) && Number.isFinite(total) && total > 0 && ready === total;
}

export function PortForwardPanel({ namespace, name, kind, ports, canPortForward }: {
  namespace: string;
  name: string;
  kind: "pod" | "service";
  ports: SuggestedPort[];
  canPortForward: boolean;
}) {
  const [selectedPorts, setSelectedPorts] = useState<number[]>(ports[0]?.port ? [ports[0].port] : []);
  const [manualPort, setManualPort] = useState("");
  const [selectedPod, setSelectedPod] = useState("");
  const [session, setSession] = useState<PortForwardSession | null>(null);
  const [creating, setCreating] = useState(false);
  const [closingId, setClosingId] = useState("");
  const [closingAll, setClosingAll] = useState(false);
  const [error, setError] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const queryClient = useQueryClient();
  const sessionsQuery = useQuery({
    queryKey: ["port-forward-sessions"],
    queryFn: () => api.listTunnels().then((response) => response.items),
    enabled: canPortForward,
    refetchInterval: 10_000,
    staleTime: 5_000,
  });
  const backendPods = useQuery({
    queryKey: ["port-forward-service-pods", namespace, name],
    queryFn: () => api.serviceTunnelTargets(namespace, name).then((response) => response.items),
    enabled: canPortForward && kind === "service",
    staleTime: 15_000,
  });

  useEffect(() => {
    setSelectedPorts(ports[0]?.port ? [ports[0].port] : []);
    setSelectedPod("");
    setManualPort("");
    setSession(null);
    setError("");
  }, [kind, name, namespace]);

  useEffect(() => {
    if (!canPortForward) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, [canPortForward]);

  useEffect(() => {
    if (session && new Date(session.expiresAt).getTime() <= now) setSession(null);
  }, [now, session]);

  const candidatePods = useMemo(() => {
    return (backendPods.data ?? [])
      .filter(podReady)
      .map((pod) => ({ value: pod.name, label: `${pod.name} · ${pod.ready} Ready · ${pod.node || "node 未知"}` }));
  }, [backendPods.data]);

  const podOptions = useMemo(() => [{ value: "", label: "自动选择（按名称排序的 Ready Pod）" }, ...candidatePods], [candidatePods]);
  const activeSessions = useMemo(() => {
    const items = [...(sessionsQuery.data ?? [])];
    if (session && !items.some((item) => item.id === session.id)) items.unshift(session);
    return items.filter((item) => new Date(item.expiresAt).getTime() > now);
  }, [now, session, sessionsQuery.data]);
  const kubectlCommand = kind === "service"
    ? `kubectl -n ${namespace} port-forward svc/${name} ${selectedPorts.map((port) => `${port}:${port}`).join(" ")}`
    : `kubectl -n ${namespace} port-forward pod/${name} ${selectedPorts.map((port) => `${port}:${port}`).join(" ")}`;

  const togglePort = (port: number) => {
    setError("");
    setSelectedPorts((current) => {
      if (current.includes(port)) return current.filter((item) => item !== port);
      if (current.length >= MAX_PORTS) {
        setError(`单个会话最多转发 ${MAX_PORTS} 个端口`);
        return current;
      }
      return [...current, port].sort((left, right) => left - right);
    });
  };

  const addManualPort = () => {
    const value = Number(manualPort);
    if (!Number.isInteger(value) || value < 1 || value > 65535) {
      setError("请输入 1 到 65535 之间的端口");
      return;
    }
    togglePort(value);
    setManualPort("");
  };

  const close = async (current: PortForwardSession) => {
    setClosingId(current.id);
    try {
      await api.closeTunnel(current.id);
      toast.success("隧道已关闭");
    } catch (closeError) {
      toast.warning(closeError instanceof Error ? closeError.message : "关闭隧道失败，服务端会在到期后自动回收");
    } finally {
      setClosingId("");
      setSession((value) => value?.id === current.id ? null : value);
      await queryClient.invalidateQueries({ queryKey: ["port-forward-sessions"] });
    }
  };

  const closeAll = async () => {
    setClosingAll(true);
    try {
      const result = await api.closeAllTunnels();
      setSession(null);
      toast.success(result.closed ? `已关闭 ${result.closed} 个隧道会话` : "当前没有活动隧道");
    } catch (closeError) {
      toast.warning(closeError instanceof Error ? closeError.message : "关闭全部隧道失败");
    } finally {
      setClosingAll(false);
      await queryClient.invalidateQueries({ queryKey: ["port-forward-sessions"] });
    }
  };

  const create = async () => {
    if (!selectedPorts.length) {
      setError("至少选择一个目标端口");
      return;
    }
    setCreating(true);
    setError("");
    try {
      const input = { ports: selectedPorts, pod: kind === "service" && selectedPod ? selectedPod : undefined };
      const created = kind === "pod"
        ? await api.createPodTunnel(namespace, name, input)
        : await api.createServiceTunnel(namespace, name, input);
      setSession(created);
      setNow(Date.now());
      await queryClient.invalidateQueries({ queryKey: ["port-forward-sessions"] });
      toast.success(`临时隧道已建立，共 ${tunnelEndpoints(created).length} 个端口`);
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : "创建端口转发失败");
    } finally {
      setCreating(false);
    }
  };

  if (!canPortForward) {
    return (
      <Card className="min-w-0 border-amber-500/35">
        <CardHeader><CardTitle className="flex items-center gap-2"><LockKeyhole className="h-5 w-5 shrink-0 text-amber-500" />当前身份不能创建端口转发</CardTitle></CardHeader>
        <CardContent className="space-y-3 text-sm leading-6 text-muted-foreground">
          <p>端口转发需要 Kubernetes <code className="break-all rounded bg-muted px-1 py-0.5 font-mono text-xs">create pods/portforward</code> 权限。Service 转发会先选择或校验一个 Ready 的后端 Pod，因此仍以 Pod 端口转发权限为最终边界。</p>
          <p>隧道只监听 Dosh 容器回环地址，浏览器通过随机化的会话路径访问，并会在到期或手动关闭后失效。</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-2">
      <Card className="min-w-0" data-testid="port-forward-create">
        <CardHeader><CardTitle className="flex items-center gap-2"><RadioTower className="h-4 w-4 shrink-0 text-muted-foreground" />新建临时隧道</CardTitle></CardHeader>
        <CardContent className="min-w-0 space-y-4">
          <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">
            隧道等价于从 Dosh 容器访问目标端口，不是公网 Ingress。访问地址包含高熵会话 ID，请勿转发给无关人员。
          </div>
          {kind === "service" ? (
            <div className="min-w-0 space-y-1.5">
              <Label htmlFor="port-forward-backend-pod">后端 Pod</Label>
              <SearchableSelect
                id="port-forward-backend-pod"
                label="后端 Pod"
                value={selectedPod}
                options={podOptions}
                onChange={setSelectedPod}
                onManualSelect={() => undefined}
                allowManual={false}
                placeholder={backendPods.isLoading ? "正在读取 Ready Pod…" : candidatePods.length ? "自动选择或搜索指定 Pod" : "自动选择（当前未读取到 Ready Pod）"}
                searchPlaceholder="搜索 Ready 后端 Pod…"
                disabled={backendPods.isLoading}
              />
              <p className="break-words text-xs leading-5 text-muted-foreground">多端口会话会固定到同一个 Pod；显式选择可避免不同端口落到不同副本。服务端仍会校验 Pod 标签是否属于当前 Service。</p>
            </div>
          ) : null}
          {ports.length ? (
            <div className="min-w-0">
              <p className="mb-2 text-xs font-medium text-muted-foreground">已声明的端口（可多选，最多 {MAX_PORTS} 个）</p>
              <div className="flex min-w-0 flex-wrap gap-2">
                {ports.map((item) => {
                  const selected = selectedPorts.includes(item.port);
                  return (
                    <button key={`${item.protocol}-${item.port}-${item.label}`} type="button" onClick={() => togglePort(item.port)} aria-pressed={selected} className={cn("min-w-0 max-w-full rounded-md border px-2.5 py-1.5 text-left text-xs transition-colors hover:bg-muted", selected && "border-primary bg-primary/5")}>
                      <span className="font-mono font-semibold">{item.port}</span>
                      <span className="ml-1.5 break-all text-muted-foreground">{item.label}{item.protocol ? ` · ${item.protocol}` : ""}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}
          <div className="min-w-0 space-y-1.5">
            <Label htmlFor="port-forward-manual-port">添加自定义端口</Label>
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
              <Input id="port-forward-manual-port" inputMode="numeric" type="number" min={1} max={65535} value={manualPort} onChange={(event) => setManualPort(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); addManualPort(); } }} className="min-w-0 sm:max-w-40" placeholder="例如 8080" />
              <Button type="button" variant="outline" onClick={addManualPort} disabled={!manualPort}><Network className="h-4 w-4" />添加端口</Button>
            </div>
            {selectedPorts.length ? <div className="flex min-w-0 flex-wrap gap-1.5 pt-1">{selectedPorts.map((port) => <button key={port} type="button" onClick={() => togglePort(port)} className="inline-flex items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 font-mono text-xs hover:bg-muted">{port}<X className="h-3 w-3" /></button>)}</div> : <p className="text-xs text-muted-foreground">尚未选择端口。</p>}
          </div>
          <div className="min-w-0 rounded-md border bg-muted/20 p-2.5">
            <p className="text-xs text-muted-foreground">本地 kubectl 等价命令（不经过 Dosh 隧道）</p>
            <p className="mt-1 break-all font-mono text-[11px] leading-5">{kubectlCommand || "kubectl port-forward …"}</p>
          </div>
          <Button type="button" disabled={creating || !selectedPorts.length} onClick={() => void create()} className="w-full sm:w-auto">
            {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : <Network className="h-4 w-4" />}
            {`建立隧道${selectedPorts.length > 1 ? `（${selectedPorts.length} 个端口）` : ""}`}
          </Button>
          {error ? <p role="alert" className="break-words text-sm text-destructive-strong">{error}</p> : null}
        </CardContent>
      </Card>
      <Card className="min-w-0" data-testid="port-forward-session">
        <CardHeader className="gap-2">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
            <CardTitle>活动隧道 {activeSessions.length ? `(${activeSessions.length})` : ""}</CardTitle>
            {activeSessions.length ? <Button type="button" size="sm" variant="destructive" disabled={closingAll} onClick={() => void closeAll()}><Trash2 className="h-3.5 w-3.5" />{closingAll ? "正在关闭…" : "关闭全部"}</Button> : null}
          </div>
          <p className="break-words text-xs text-muted-foreground">服务端会保留当前身份的活动会话，刷新页面后仍可管理；会话到期后自动回收。</p>
        </CardHeader>
        <CardContent className="min-w-0">
          {sessionsQuery.isError ? <p role="alert" className="mb-3 break-words rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">活动会话列表暂时不可用；已打开的地址仍按服务端到期时间生效。</p> : null}
          {!activeSessions.length ? <p className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">尚未创建端口转发。创建后会显示每个端口的临时访问地址、后端 Pod 和到期时间。</p> : (
            <div className="min-w-0 space-y-4">
              {activeSessions.map((item) => {
                const endpoints = tunnelEndpoints(item);
                const remaining = Math.max(0, Math.ceil((new Date(item.expiresAt).getTime() - now) / 1000));
                const minutes = Math.floor(remaining / 60);
                const seconds = remaining % 60;
                return (
                  <div key={item.id} className="min-w-0 space-y-3 rounded-lg border p-3">
                    <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0"><p className="break-all font-mono text-sm font-semibold">{item.namespace}/{item.pod}</p><p className="break-all text-xs text-muted-foreground">{item.service ? `Service ${item.service} · ` : "Pod 直连 · "}会话 {item.id.slice(0, 12)}…</p></div>
                      <Button type="button" size="sm" variant="outline" disabled={closingId === item.id} onClick={() => void close(item)}><Trash2 className="h-3.5 w-3.5" />{closingId === item.id ? "关闭中…" : "关闭"}</Button>
                    </div>
                    <dl className="grid min-w-0 grid-cols-1 gap-2 text-sm sm:grid-cols-3">
                      <div className="min-w-0"><dt className="text-xs text-muted-foreground">端口数</dt><dd className="font-mono text-xs">{endpoints.length}</dd></div>
                      <div className="min-w-0"><dt className="text-xs text-muted-foreground">剩余时间</dt><dd className="font-mono text-xs">{minutes}:{String(seconds).padStart(2, "0")}</dd></div>
                      <div className="min-w-0"><dt className="text-xs text-muted-foreground">到期时间</dt><dd className="break-all text-xs">{new Date(item.expiresAt).toLocaleString()}</dd></div>
                    </dl>
                    <div className="min-w-0 space-y-2">
                      {endpoints.map((endpoint) => {
                        const url = `${window.location.origin}${endpoint.path}`;
                        return (
                          <div key={endpoint.id} className="min-w-0 rounded-md border bg-muted/20 p-3">
                            <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
                              <span className="font-mono font-semibold">请求端口 {endpoint.requestedPort}</span>
                              <span className="text-muted-foreground">→ Pod 端口 {endpoint.port}</span>
                              {endpoint.service ? <span className="break-all text-muted-foreground">· {endpoint.service}</span> : null}
                            </div>
                            <p className="mt-1 break-all font-mono text-[11px] leading-5">{url}</p>
                            <div className="mt-2 flex min-w-0 flex-wrap gap-1.5">
                              <Button type="button" size="sm" variant="outline" onClick={() => void copyText(url).then(() => toast.success(`端口 ${endpoint.requestedPort} 地址已复制`)).catch(() => toast.error("复制失败"))}><Copy className="h-3.5 w-3.5" />复制</Button>
                              <Button type="button" size="sm" variant="outline" asChild><a href={url} target="_blank" rel="noreferrer"><ExternalLink className="h-3.5 w-3.5" />打开</a></Button>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                );
              })}
              <p className="text-xs leading-5 text-muted-foreground">关闭会话会同时回收全部端口。上游应用无法获取 Dosh 的 CSRF Token，因此隧道地址不支持写操作防伪头；请只访问你信任的内部应用。</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
