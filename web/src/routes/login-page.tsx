import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Boxes, KeyRound, Loader2, LockKeyhole, LogIn, Server, ShieldCheck } from "lucide-react";
import { api } from "@/api/client";
import { sessionQueryKey, useSessionQuery } from "@/features/auth/use-session";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

export function LoginPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useSessionQuery();
  const authConfig = useQuery({ queryKey: ["auth-config"], queryFn: api.authConfig, retry: false, staleTime: 60_000 });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const oidcError = typeof window !== "undefined" && new URLSearchParams(window.location.search).get("error") === "oidc";
  const mutation = useMutation({
    mutationFn: () => api.login(username, password),
    onSuccess: async (data) => {
      queryClient.setQueryData(sessionQueryKey, data);
      await navigate({ to: "/", replace: true });
    },
  });
  const tokenMutation = useMutation({
    mutationFn: () => api.loginWithToken(token),
    onSuccess: async (data) => {
      queryClient.setQueryData(sessionQueryKey, data);
      await navigate({ to: "/", replace: true });
    },
  });
  useEffect(() => {
    if (session.data) void navigate({ to: "/", replace: true });
  }, [navigate, session.data]);
  return (
    <div className="grid min-h-screen bg-background lg:grid-cols-[1.08fr_1fr]">
      <section className="relative hidden overflow-hidden bg-[#071a36] p-12 text-white lg:flex lg:flex-col">
        <div className="absolute inset-0 opacity-25" style={{ backgroundImage: "linear-gradient(rgba(255,255,255,.08) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.08) 1px, transparent 1px)", backgroundSize: "44px 44px" }} />
        <div className="absolute -right-32 -top-32 h-96 w-96 rounded-full bg-blue-500/25 blur-3xl" />
        <div className="absolute -bottom-48 left-16 h-[28rem] w-[28rem] rounded-full bg-cyan-400/10 blur-3xl" />
        <div className="relative flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-xl bg-blue-500 font-bold shadow-lg shadow-blue-950/30">D</div>
          <div><p className="font-semibold">Dosh Console</p><p className="text-xs text-blue-200/75">Kubernetes Operations</p></div>
        </div>
        <div className="relative my-auto max-w-xl">
          <p className="mb-5 text-xs font-semibold uppercase tracking-[0.25em] text-blue-300">Unified Cluster Operations</p>
          <h1 className="text-4xl font-semibold leading-tight tracking-tight xl:text-5xl">让集群运维<br /><span className="text-blue-300">清晰、稳定、可审计。</span></h1>
          <p className="mt-6 max-w-lg text-sm leading-7 text-slate-300">面向 Kubernetes 日常运维的统一控制台，提供资源总览、工作负载管理、日志、事件和配置查看能力。</p>
          <div className="mt-10 grid max-w-lg grid-cols-3 gap-4">
            <Feature icon={Server} title="节点" text="状态与容量" />
            <Feature icon={Boxes} title="工作负载" text="统一管理" />
            <Feature icon={ShieldCheck} title="安全" text="RBAC 与审计" />
          </div>
        </div>
        <p className="relative text-xs text-slate-400">Dosh Console · Internal operations platform</p>
      </section>
      <section className="flex items-center justify-center px-5 py-10">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex items-center gap-3 lg:hidden">
            <div className="grid h-10 w-10 place-items-center rounded-xl bg-primary font-bold text-primary-foreground">D</div>
            <div><p className="font-semibold">Dosh Console</p><p className="text-xs text-muted-foreground">Kubernetes Operations</p></div>
          </div>
          {authConfig.isPending ? (
            <div className="flex items-center gap-2 py-10 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在读取认证配置…</div>
          ) : authConfig.isError ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive-strong">无法读取认证配置，请刷新页面后重试。</div>
          ) : authConfig.data.mode === "oidc" ? (
            <div>
              <div className="mb-7">
                <div className="mb-4 grid h-10 w-10 place-items-center rounded-lg border bg-card text-primary shadow-sm"><ShieldCheck className="h-5 w-5" /></div>
                <h2 className="text-2xl font-semibold tracking-tight">企业身份登录</h2>
                <p className="mt-1.5 text-sm text-muted-foreground">使用组织身份提供商安全登录，权限由 Kubernetes RBAC 决定。</p>
              </div>
              {oidcError ? <div className="mb-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive-strong">身份认证未完成，请重新登录。</div> : null}
              <Button className="w-full" onClick={() => window.location.assign("/api/v1/auth/oidc/login?returnTo=%2F")}>
                <LogIn className="h-4 w-4" />使用 {authConfig.data.providerName || "企业身份"} 登录
              </Button>
              <div className="mt-6 rounded-lg border bg-card p-3 text-xs leading-5 text-muted-foreground">
                登录采用 Authorization Code + PKCE，校验签名、issuer、audience、state 和 nonce；控制台不会保存 ID Token。
              </div>
            </div>
          ) : authConfig.data.mode === "token" ? (
            <div>
              <div className="mb-7">
                <div className="mb-4 grid h-10 w-10 place-items-center rounded-lg border bg-card text-primary shadow-sm"><ShieldCheck className="h-5 w-5" /></div>
                <h2 className="text-2xl font-semibold tracking-tight">Kubernetes Token 登录</h2>
                <p className="mt-1.5 text-sm text-muted-foreground">粘贴 kubeconfig 或 ServiceAccount 的 Bearer Token，权限由集群 RBAC 决定。</p>
              </div>
              <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); tokenMutation.mutate(); }}>
                <div className="space-y-1.5">
                  <Label htmlFor="kubernetes-token">Bearer Token</Label>
                  <Textarea id="kubernetes-token" autoFocus autoComplete="off" spellCheck={false} rows={5} className="font-mono text-xs" value={token} onChange={(event) => setToken(event.target.value)} placeholder="eyJhbGciOi..." />
                </div>
                {tokenMutation.error ? <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive-strong">{tokenMutation.error.message}</div> : null}
                <Button type="submit" className="w-full" disabled={tokenMutation.isPending || !token.trim()}>
                  {tokenMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <LogIn className="h-4 w-4" />}
                  登录
                </Button>
              </form>
              <div className="mt-6 rounded-lg border bg-card p-3 text-xs leading-5 text-muted-foreground">
                Token 仅用于 Kubernetes TokenReview 身份校验，不会写入会话 Cookie、审计日志或浏览器存储。
              </div>
            </div>
          ) : (
            <>
              <div className="mb-7">
                <div className="mb-4 grid h-10 w-10 place-items-center rounded-lg border bg-card text-primary shadow-sm"><KeyRound className="h-5 w-5" /></div>
                <h2 className="text-2xl font-semibold tracking-tight">登录控制台</h2>
                <p className="mt-1.5 text-sm text-muted-foreground">使用集群管理员凭据继续。</p>
              </div>
              <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}>
                <div className="space-y-1.5"><Label htmlFor="username">用户名</Label><Input id="username" autoFocus autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} placeholder="admin" /></div>
                <div className="space-y-1.5"><Label htmlFor="password">密码</Label><Input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="请输入密码" /></div>
                {mutation.error ? <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive-strong">{mutation.error.message}</div> : null}
                <Button type="submit" className="w-full" disabled={mutation.isPending || !username || !password}>
                  {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <LockKeyhole className="h-4 w-4" />}
                  登录
                </Button>
              </form>
            </>
          )}
          <div className="mt-6 rounded-lg border bg-card p-3 text-xs leading-5 text-muted-foreground">
            登录会创建加密 HttpOnly 会话；连续失败将触发临时限速，所有认证行为均写入审计日志。
          </div>
        </div>
      </section>
    </div>
  );
}

function Feature({ icon: Icon, title, text }: { icon: typeof Server; title: string; text: string }) {
  return <div className="rounded-lg border border-white/10 bg-white/5 p-3 backdrop-blur-sm"><Icon className="h-4 w-4 text-blue-300" /><p className="mt-3 text-sm font-medium">{title}</p><p className="mt-0.5 text-xs text-slate-400">{text}</p></div>;
}

