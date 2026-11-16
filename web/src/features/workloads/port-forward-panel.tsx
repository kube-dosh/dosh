import { useEffect, useState } from "react";
import { Copy, ExternalLink, Loader2, LockKeyhole, Network, RadioTower, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { PortForwardSession } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

export interface SuggestedPort {
  port: number;
  label: string;
  protocol?: string;
}

export function PortForwardPanel({ namespace, name, kind, ports, canPortForward }: {
  namespace: string;
  name: string;
  kind: "pod" | "service";
  ports: SuggestedPort[];
  canPortForward: boolean;
}) {
  const [port, setPort] = useState(String(ports[0]?.port ?? ""));
  const [session, setSession] = useState<PortForwardSession | null>(null);
  const [creating, setCreating] = useState(false);
  const [closing, setClosing] = useState(false);
  const [error, setError] = useState("");
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!session) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, [session]);

  useEffect(() => {
    if (session && new Date(session.expiresAt).getTime() <= now) setSession(null);
  }, [now, session]);

  const close = async (current: PortForwardSession) => {
    setClosing(true);
    try {
      await api.closeTunnel(current.id);
    } catch (closeError) {
      toast.warning(closeError instanceof Error ? closeError.message : "关闭隧道失败，服务端会在到期后自动回收");
    } finally {
      setClosing(false);
      setSession((value) => value?.id === current.id ? null : value);
    }
  };

  const create = async () => {
    const requested = Number(port);
    if (!Number.isInteger(requested) || requested < 1 || requested > 65535) {
      setError("请输入 1 到 65535 之间的端口");
      return;
    }
    setCreating(true);
    setError("");
    try {
      if (session) await close(session);
      const created = kind === "pod"
        ? await api.createPodTunnel(namespace, name, requested)
        : await api.createServiceTunnel(namespace, name, requested);
      setSession(created);
      setNow(Date.now());
      toast.success(`临时隧道已建立，Pod 端口 ${created.port}`);
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : "创建端口转发失败");
    } finally {
      setCreating(false);
    }
  };

  const tunnelURL = session ? `${window.location.origin}${session.path}` : "";
  const remaining = session ? Math.max(0, Math.ceil((new Date(session.expiresAt).getTime() - now) / 1000)) : 0;
  const minutes = Math.floor(remaining / 60);
  const seconds = remaining % 60;

  if (!canPortForward) {
    return (
      <Card className="min-w-0 border-amber-500/35">
        <CardHeader><CardTitle className="flex items-center gap-2"><LockKeyhole className="h-5 w-5 shrink-0 text-amber-500" />当前身份不能创建端口转发</CardTitle></CardHeader>
        <CardContent className="space-y-3 text-sm leading-6 text-muted-foreground">
          <p>端口转发需要 Kubernetes <code className="break-all rounded bg-muted px-1 py-0.5 font-mono text-xs">create pods/portforward</code> 权限。Service 转发会先选择一个 Ready 的后端 Pod，因此仍以 Pod 端口转发权限为最终边界。</p>
          <p>隧道只监听 Dosh 容器回环地址，浏览器通过随机化的会话路径访问，并会在到期或手动关闭后失效。</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-2">
      <Card className="min-w-0">
        <CardHeader><CardTitle className="flex items-center gap-2"><RadioTower className="h-4 w-4 shrink-0 text-muted-foreground" />新建临时隧道</CardTitle></CardHeader>
        <CardContent className="min-w-0 space-y-4">
          <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">
            隧道等价于从 Dosh 容器访问目标端口，不是公网 Ingress。访问地址包含高熵会话 ID，请勿转发给无关人员。
          </div>
          {ports.length ? (
            <div className="min-w-0">
              <p className="mb-2 text-xs font-medium text-muted-foreground">已声明的端口</p>
              <div className="flex min-w-0 flex-wrap gap-2">
                {ports.map((item) => (
                  <button key={`${item.protocol}-${item.port}-${item.label}`} type="button" onClick={() => setPort(String(item.port))} className={cn("min-w-0 max-w-full rounded-md border px-2.5 py-1.5 text-left text-xs transition-colors hover:bg-muted", Number(port) === item.port && "border-primary bg-primary/5")}>
                    <span className="font-mono font-semibold">{item.port}</span>
                    <span className="ml-1.5 break-all text-muted-foreground">{item.label}{item.protocol ? ` · ${item.protocol}` : ""}</span>
                  </button>
                ))}
              </div>
            </div>
          ) : null}
          <div className="space-y-1.5">
            <Label htmlFor="port-forward-port">目标端口</Label>
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
              <Input id="port-forward-port" inputMode="numeric" type="number" min={1} max={65535} value={port} onChange={(event) => setPort(event.target.value)} className="min-w-0 sm:max-w-40" />
              <Button type="button" disabled={creating || !port} onClick={() => void create()} className="shrink-0">
                {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : <Network className="h-4 w-4" />}
                {session ? "重建隧道" : "建立隧道"}
              </Button>
            </div>
            <p className="text-xs leading-5 text-muted-foreground">端口由服务端固定到单个 Pod，浏览器不能指定上游地址。{kind === "service" ? " Service 会选择一个 Ready 后端 Pod。" : ""}</p>
          </div>
          {error ? <p role="alert" className="break-words text-sm text-destructive-strong">{error}</p> : null}
        </CardContent>
      </Card>
      <Card className="min-w-0">
        <CardHeader><CardTitle>当前隧道</CardTitle></CardHeader>
        <CardContent className="min-w-0">
          {!session ? <p className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">尚未创建端口转发。创建后会显示临时访问地址、后端 Pod 和到期时间。</p> : (
            <div className="min-w-0 space-y-4">
              <dl className="grid min-w-0 gap-3 text-sm sm:grid-cols-2">
                <div className="min-w-0"><dt className="text-xs text-muted-foreground">后端 Pod</dt><dd className="break-all font-mono text-xs">{session.pod}</dd></div>
                <div className="min-w-0"><dt className="text-xs text-muted-foreground">Pod 端口</dt><dd className="font-mono text-xs">{session.port}</dd></div>
                <div className="min-w-0"><dt className="text-xs text-muted-foreground">剩余时间</dt><dd className="font-mono text-xs">{minutes}:{String(seconds).padStart(2, "0")}</dd></div>
                <div className="min-w-0"><dt className="text-xs text-muted-foreground">到期时间</dt><dd className="break-all text-xs">{new Date(session.expiresAt).toLocaleString()}</dd></div>
              </dl>
              <div className="min-w-0 rounded-md border bg-muted/25 p-3">
                <p className="mb-1 text-xs text-muted-foreground">临时访问地址</p>
                <p className="break-all font-mono text-[11px] leading-5">{tunnelURL}</p>
              </div>
              <div className="flex min-w-0 flex-wrap gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => void copyText(tunnelURL).then(() => toast.success("访问地址已复制")).catch(() => toast.error("复制失败"))}><Copy className="h-4 w-4" />复制地址</Button>
                <Button type="button" size="sm" variant="outline" asChild><a href={tunnelURL} target="_blank" rel="noreferrer"><ExternalLink className="h-4 w-4" />打开</a></Button>
                <Button type="button" size="sm" variant="destructive" disabled={closing} onClick={() => void close(session)}>{closing ? <Loader2 className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}关闭隧道</Button>
              </div>
              <p className="text-xs leading-5 text-muted-foreground">上游应用无法获取 Dosh 的 CSRF Token，因此隧道地址不支持写操作防伪头；请只访问你信任的内部应用。</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

