import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, CheckCircle2, Cpu, ExternalLink, GitCommitHorizontal, Network, PackageSearch, RefreshCw, Settings2, ShieldCheck, Sparkles, TriangleAlert } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { EgressNetworkPolicyStatus, EgressPreflight, EgressSettings, EgressSettingsUpdate, EgressStatus, SystemInfo, UpdateStatus, UpdateStatusValue } from "@/api/types";
import { CopyButton } from "@/components/data/copy-button";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useSessionQuery } from "@/features/auth/use-session";
import { formatDate } from "@/lib/utils";

const systemInfoQueryKey = ["system", "info"] as const;
const systemUpdateQueryKey = ["system", "update"] as const;
const systemEgressQueryKey = ["system", "egress"] as const;

const authLabels: Record<SystemInfo["authMode"], string> = {
  local: "本地管理员",
  oidc: "企业身份（OIDC）",
  token: "Kubernetes Token",
};

const egressModeLabels: Record<EgressStatus["mode"], string> = {
  environment: "继承环境变量",
  explicit: "显式代理",
  direct: "强制直连",
};

const egressSourceLabels: Record<EgressSettings["source"], string> = {
  bootstrap: "启动配置",
  runtime: "运行时配置",
};

const egressSettingsModeLabels: Record<EgressSettings["mode"], string> = {
  bootstrap: "使用启动配置",
  explicit: "启用显式代理",
  direct: "强制直连",
};

const updateMeta: Record<UpdateStatusValue, { label: string; variant: "secondary" | "success" | "warning" | "destructive" | "default" }> = {
  disabled: { label: "未启用", variant: "secondary" },
  current: { label: "已是最新", variant: "success" },
  update_available: { label: "发现新版本", variant: "warning" },
  unknown: { label: "无法比较版本", variant: "secondary" },
  error: { label: "检查失败", variant: "destructive" },
};

export function SystemPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const info = useQuery({ queryKey: systemInfoQueryKey, queryFn: api.systemInfo, staleTime: 60_000 });
  const update = useQuery({ queryKey: systemUpdateQueryKey, queryFn: api.systemUpdate, staleTime: 60_000 });
  const egress = useQuery({ queryKey: systemEgressQueryKey, queryFn: api.systemEgress, staleTime: 15_000 });
  const [egressOpen, setEgressOpen] = useState(false);
  const preflight = useMutation({ mutationFn: api.preflightSystemEgress });
  const check = useMutation({
    mutationFn: api.checkSystemUpdate,
    onSuccess: (status) => {
      queryClient.setQueryData(systemUpdateQueryKey, status);
      if (status.status === "update_available") toast.success(`发现新版本 v${status.latestVersion}`);
      else if (status.status === "current") toast.success("当前已是最新版本");
      else toast.info(status.message || "更新检查已完成");
    },
    onError: (error) => toast.error(error instanceof Error ? `更新检查失败：${error.message}` : "更新检查失败"),
  });
  const saveEgress = useMutation({
    mutationFn: api.updateSystemEgress,
    onSuccess: (settings) => {
      queryClient.setQueryData(systemEgressQueryKey, settings);
      void queryClient.invalidateQueries({ queryKey: systemInfoQueryKey });
      setEgressOpen(false);
      preflight.reset();
      toast.success(settings.enabled ? "统一出站网络已启用并立即生效" : "统一出站网络策略已更新并立即生效");
    },
    onError: (error) => toast.error(error instanceof Error ? `代理配置保存失败：${error.message}` : "代理配置保存失败"),
  });

  if (info.isLoading) return <LoadingState label="正在读取 Dosh 系统信息" className="min-h-[60vh]" />;
  if (info.isError) return <ErrorState error={info.error} onRetry={() => void info.refetch()} />;
  const data = info.data;
  if (!data) return null;

  const refresh = () => {
    void info.refetch();
    void update.refetch();
    void egress.refetch();
  };
  const canForceCheck = Boolean(session.data?.user.admin && update.data?.enabled);
  const egressSettings = egress.data;
  const egressEnabled = egressSettings?.enabled ?? data.egress.enabled;
  const egressEffectiveMode = egressSettings?.effectiveMode ?? data.egress.mode;
  const egressHTTPProxy = egressSettings?.httpProxy ?? data.egress.httpProxy;
  const egressHTTPSProxy = egressSettings?.httpsProxy ?? data.egress.httpsProxy;
  const egressNoProxy = egressSettings?.noProxy ?? data.egress.noProxy;
  const egressCredentialsConfigured = egressSettings?.credentialsConfigured ?? data.egress.credentialsConfigured;
  const egressCAConfigured = egressSettings?.caConfigured ?? data.egress.caConfigured;
  const egressSource = egressSettings?.source ?? data.egress.source;
  const networkPolicy = egressSettings?.networkPolicy;

  const openEgressDialog = () => {
    preflight.reset();
    setEgressOpen(true);
  };

  return (
    <>
      <PageHeader
        title="系统信息"
        breadcrumb="其他功能 / 系统信息"
        description="查看 Dosh 构建版本、运行时配置和统一出站网络状态。此页面不会展示代理密码、更新令牌或 Kubernetes Secret。"
        actions={<><CopyButton value={diagnosticText(data, update.data)} label="复制诊断信息" /><Button variant="outline" onClick={refresh}><RefreshCw className="h-4 w-4" />刷新</Button></>}
      />

      <div className="grid min-w-0 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard icon={Sparkles} label="Dosh 版本" value={display(data.version)} detail={data.chartVersion && data.chartVersion !== "unknown" ? `Helm Chart ${data.chartVersion}` : "Chart 版本未注入"} />
        <SummaryCard icon={GitCommitHorizontal} label="提交" value={display(data.commit)} detail={displayDate(data.buildDate)} mono />
        <SummaryCard icon={ShieldCheck} label="认证模式" value={authLabels[data.authMode] ?? data.authMode} detail={data.readOnly ? "只读模式已启用" : "允许受控写操作"} />
        <SummaryCard icon={Network} label="出站网络" value={egressEnabled ? "统一代理" : egressModeLabels[egressEffectiveMode]} detail={egressEnabled ? "Helm、镜像仓、OIDC、审计与更新检查共用" : "业务请求按当前策略直连"} />
      </div>

      <div className="mt-4 grid min-w-0 gap-4 xl:grid-cols-2">
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Cpu className="h-4 w-4 text-primary" />构建与运行环境</CardTitle>
            <CardDescription>用于问题排查和版本核对，不包含任何凭据。</CardDescription>
          </CardHeader>
          <CardContent>
            <DetailGrid items={[
              ["应用版本", display(data.version)],
              ["Helm Chart", display(data.chartVersion)],
              ["Git Commit", display(data.commit), true],
              ["构建时间", displayDate(data.buildDate)],
              ["Go 运行时", display(data.goVersion), true],
              ["集群名称", display(data.clusterName)],
              ["认证模式", authLabels[data.authMode] ?? data.authMode],
              ["请求超时", display(data.requestTimeout), true],
            ]} />
          </CardContent>
        </Card>

        <Card className="min-w-0">
          <CardHeader>
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0">
                <CardTitle className="flex items-center gap-2"><Network className="h-4 w-4 text-primary" />统一出站网络</CardTitle>
                <CardDescription className="mt-1">仅覆盖 Dosh 发起的业务资源拉取；Kubernetes API、Exec 和端口转发始终直连集群。</CardDescription>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={egressEnabled ? "success" : "secondary"}>{egressEnabled ? "已启用" : "未启用"}</Badge>
                {session.data?.user.admin ? <Button size="sm" variant="outline" onClick={openEgressDialog} disabled={!egressSettings}><Settings2 className="h-3.5 w-3.5" />配置代理</Button> : null}
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <DetailGrid items={[
              ["代理模式", egressModeLabels[egressEffectiveMode] ?? egressEffectiveMode],
              ["配置来源", egressSourceLabels[egressSource] ?? egressSource],
              ["HTTP 代理", display(egressHTTPProxy), true],
              ["HTTPS 代理", display(egressHTTPSProxy), true],
              ["NO_PROXY", display(egressNoProxy), true],
              ["代理凭据", egressCredentialsConfigured ? "已通过 Secret 配置" : "未配置"],
              ["代理 CA", egressCAConfigured ? "已加载自定义 CA" : "使用系统信任库"],
              ["NetworkPolicy", networkPolicy ? networkPolicyModeLabel(networkPolicy) : "读取中"],
            ]} />
            {networkPolicy ? <NetworkPolicyHint policy={networkPolicy} /> : null}
            {egress.isError ? <div className="mt-4 flex flex-wrap items-center justify-between gap-2 rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong"><span>无法读取运行时代理设置，当前展示系统信息快照。</span><Button size="sm" variant="outline" onClick={() => void egress.refetch()}>重试</Button></div> : null}
            {session.data?.user.admin ? <p className="mt-3 text-xs text-muted-foreground">保存后立即影响 Dosh 新发起的 Helm、镜像仓、OIDC、审计 Webhook 与更新检查请求，无需重启 Pod。</p> : <p className="mt-3 text-xs text-muted-foreground">只有管理员可以修改统一出站网络。</p>}
          </CardContent>
        </Card>
      </div>

      <Card className="mt-4 min-w-0">
        <CardHeader>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <CardTitle className="flex items-center gap-2"><PackageSearch className="h-4 w-4 text-primary" />Dosh 更新检查</CardTitle>
              <CardDescription className="mt-1">只提示可用版本，不会自动下载、替换镜像或触发升级。检查请求遵循统一出站代理。</CardDescription>
            </div>
            <div className="flex shrink-0 flex-wrap items-center gap-2">
              {update.data ? <Badge variant={(updateMeta[update.data.status] ?? updateMeta.unknown).variant}>{(updateMeta[update.data.status] ?? updateMeta.unknown).label}</Badge> : null}
              <Button size="sm" onClick={() => check.mutate()} disabled={!canForceCheck || check.isPending}>
                <RefreshCw className={`h-3.5 w-3.5 ${check.isPending ? "animate-spin" : ""}`} />
                {check.isPending ? "检查中" : "立即检查"}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="min-w-0">
          {update.isLoading ? <p className="text-sm text-muted-foreground">正在读取更新状态…</p> : null}
          {update.isError ? <div className="flex items-start gap-2 rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong"><TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" /><div className="min-w-0"><p>无法读取更新状态。</p><Button variant="outline" size="sm" className="mt-2" onClick={() => void update.refetch()}>重试</Button></div></div> : null}
          {update.data ? <UpdateDetails status={update.data} /> : null}
          {session.data && !session.data.user.admin ? <p className="mt-3 text-xs text-muted-foreground">只有管理员可以手动触发更新检查。</p> : null}
        </CardContent>
      </Card>

      <EgressSettingsDialog
        open={egressOpen}
        onOpenChange={(open) => {
          setEgressOpen(open);
          if (!open) {
            saveEgress.reset();
            preflight.reset();
          }
        }}
        settings={egressSettings}
        isSaving={saveEgress.isPending}
        error={saveEgress.error}
        onSave={(input) => saveEgress.mutate(input)}
        onPreflight={(input) => preflight.mutateAsync(input)}
        isPreflighting={preflight.isPending}
        preflight={preflight.data}
        preflightError={preflight.error}
      />
    </>
  );
}

interface EgressFormState {
  mode: EgressSettings["mode"];
  httpProxy: string;
  httpsProxy: string;
  noProxy: string;
  username: string;
  password: string;
  clearCredentials: boolean;
  caPEM: string;
  clearCA: boolean;
}

function egressFormState(settings: EgressSettings | undefined): EgressFormState {
  return {
    mode: settings?.mode ?? "bootstrap",
    httpProxy: settings?.httpProxy ?? "",
    httpsProxy: settings?.httpsProxy ?? "",
    noProxy: settings?.noProxy ?? "",
    username: settings?.username ?? "",
    password: "",
    clearCredentials: false,
    caPEM: "",
    clearCA: false,
  };
}

function EgressSettingsDialog({ open, onOpenChange, settings, isSaving, error, onSave, onPreflight, isPreflighting, preflight, preflightError }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  settings: EgressSettings | undefined;
  isSaving: boolean;
  error: Error | null;
  onSave: (input: EgressSettingsUpdate) => void;
  onPreflight: (input: EgressSettingsUpdate) => Promise<EgressPreflight>;
  isPreflighting: boolean;
  preflight: EgressPreflight | undefined;
  preflightError: Error | null;
}) {
  const [form, setForm] = useState<EgressFormState>(() => egressFormState(settings));

  useEffect(() => {
    if (open) setForm(egressFormState(settings));
  }, [open, settings]);

  const explicit = form.mode === "explicit";
  const input = () => egressUpdateInput(form, settings);
  const runPreflight = async () => {
    try {
      return await onPreflight(input());
    } catch {
      return undefined;
    }
  };
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const result = await runPreflight();
    if (!result || result.status === "error") return;
    onSave(input());
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-1rem)] max-w-2xl sm:w-[calc(100%-2rem)]">
        <DialogHeader>
          <DialogTitle>统一出站网络配置</DialogTitle>
          <DialogDescription>配置会保存到集群 Secret，并立即应用到当前及后续由 Dosh 发起的业务资源拉取。密码和 CA 不会回显。</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="min-w-0 space-y-4">
          {settings?.networkPolicy ? <NetworkPolicyHint policy={settings.networkPolicy} compact /> : null}
          <div className="space-y-2">
            <Label htmlFor="egress-mode">运行模式</Label>
            <Select value={form.mode} onValueChange={(value) => setForm((current) => ({ ...current, mode: value as EgressSettings["mode"] }))}>
              <SelectTrigger id="egress-mode" aria-label="代理模式"><SelectValue /></SelectTrigger>
              <SelectContent>
                {(Object.keys(egressSettingsModeLabels) as EgressSettings["mode"][]).map((mode) => <SelectItem key={mode} value={mode}>{egressSettingsModeLabels[mode]}</SelectItem>)}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {form.mode === "bootstrap" ? "恢复 Helm values 或环境变量定义的启动配置，并覆盖其他副本的运行时设置。" : form.mode === "explicit" ? "所有受控业务请求经下方代理发出；至少填写 HTTP 或 HTTPS 代理之一。" : "受控业务请求绕过代理直连，适用于临时排障或内网环境。"}
            </p>
          </div>

          {explicit ? (
            <>
              <div className="grid min-w-0 gap-4 sm:grid-cols-2">
                <div className="min-w-0 space-y-2">
                  <Label htmlFor="egress-http-proxy">HTTP 代理</Label>
                  <Input id="egress-http-proxy" value={form.httpProxy} onChange={(event) => setForm((current) => ({ ...current, httpProxy: event.target.value }))} placeholder="http://proxy.example.internal:3128" autoComplete="off" spellCheck={false} />
                </div>
                <div className="min-w-0 space-y-2">
                  <Label htmlFor="egress-https-proxy">HTTPS 代理</Label>
                  <Input id="egress-https-proxy" value={form.httpsProxy} onChange={(event) => setForm((current) => ({ ...current, httpsProxy: event.target.value }))} placeholder="http://proxy.example.internal:3128" autoComplete="off" spellCheck={false} />
                </div>
              </div>
              <div className="min-w-0 space-y-2">
                <Label htmlFor="egress-no-proxy">NO_PROXY</Label>
                <Input id="egress-no-proxy" value={form.noProxy} onChange={(event) => setForm((current) => ({ ...current, noProxy: event.target.value }))} placeholder=".svc,.cluster.local,kubernetes.default.svc" autoComplete="off" spellCheck={false} />
                <p className="break-words text-xs text-muted-foreground">使用逗号分隔。Kubernetes API、Exec、日志流和端口转发始终直连，不受此配置影响。</p>
              </div>
              <div className="grid min-w-0 gap-4 sm:grid-cols-2">
                <div className="min-w-0 space-y-2">
                  <Label htmlFor="egress-username">代理用户名</Label>
                  <Input id="egress-username" value={form.username} onChange={(event) => setForm((current) => ({ ...current, username: event.target.value }))} disabled={form.clearCredentials} autoComplete="username" />
                </div>
                <div className="min-w-0 space-y-2">
                  <Label htmlFor="egress-password">代理密码</Label>
                  <Input id="egress-password" type="password" value={form.password} onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))} disabled={form.clearCredentials} placeholder={settings?.credentialsConfigured ? "留空保持已保存密码" : "未配置"} autoComplete="new-password" />
                </div>
              </div>
              <label className="flex items-start gap-2 text-sm">
                <input type="checkbox" className="mt-0.5 h-4 w-4 shrink-0 accent-primary" checked={form.clearCredentials} onChange={(event) => setForm((current) => ({ ...current, clearCredentials: event.target.checked }))} />
                <span className="min-w-0"><span className="font-medium">清除已保存的代理凭据</span><span className="block text-xs text-muted-foreground">启用后用户名和密码将同时删除。</span></span>
              </label>
              <div className="min-w-0 space-y-2">
                <Label htmlFor="egress-ca">自定义代理 CA（PEM）</Label>
                <Textarea id="egress-ca" value={form.caPEM} onChange={(event) => setForm((current) => ({ ...current, caPEM: event.target.value }))} disabled={form.clearCA} placeholder={settings?.caConfigured ? "已保存 CA，留空保持不变" : "-----BEGIN CERTIFICATE-----"} className="min-h-28 break-all font-mono text-xs" spellCheck={false} />
              </div>
              <label className="flex items-start gap-2 text-sm">
                <input type="checkbox" className="mt-0.5 h-4 w-4 shrink-0 accent-primary" checked={form.clearCA} onChange={(event) => setForm((current) => ({ ...current, clearCA: event.target.checked }))} />
                <span className="min-w-0"><span className="font-medium">清除自定义 CA</span><span className="block text-xs text-muted-foreground">清除后回退到容器系统信任库。</span></span>
              </label>
            </>
          ) : null}

          {explicit ? (
            <div className="min-w-0 space-y-3 rounded-md border bg-muted/20 p-3">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <p className="text-sm font-medium">保存前预检</p>
                  <p className="text-xs text-muted-foreground">解析代理 IP、检查 NetworkPolicy 放行范围并探测 TCP 连通性。</p>
                </div>
                <Button type="button" variant="outline" size="sm" className="shrink-0" onClick={() => void runPreflight()} disabled={isPreflighting || isSaving}>
                  {isPreflighting ? "正在预检…" : "预检目标与策略"}
                </Button>
              </div>
              {preflightError ? <div className="break-words rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-xs text-destructive-strong">{preflightError.message}</div> : null}
              {preflight ? <PreflightResult result={preflight} /> : null}
            </div>
          ) : null}

          {error ? <div className="rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{error.message}</div> : null}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isSaving}>取消</Button>
            <Button type="submit" disabled={isSaving}>{isSaving ? "正在应用…" : "保存并应用"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function egressUpdateInput(form: EgressFormState, settings: EgressSettings | undefined): EgressSettingsUpdate {
  const explicit = form.mode === "explicit";
  return {
    mode: form.mode,
    httpProxy: explicit ? form.httpProxy.trim() : "",
    httpsProxy: explicit ? form.httpsProxy.trim() : "",
    noProxy: explicit ? form.noProxy.trim() : "",
    username: explicit && !form.clearCredentials ? form.username.trim() : "",
    password: explicit && !form.clearCredentials && form.password ? form.password : undefined,
    clearCredentials: explicit && form.clearCredentials,
    caPEM: explicit && !form.clearCA && form.caPEM.trim() ? form.caPEM.trim() : undefined,
    clearCA: explicit && form.clearCA,
    revision: settings?.revision,
  };
}

function networkPolicyModeLabel(policy: EgressNetworkPolicyStatus): string {
  if (policy.mode === "runtime") return "Dosh 自动维护";
  if (policy.mode === "helm") return "Helm 控制";
  return "未启用";
}

function NetworkPolicyHint({ policy, compact = false }: { policy: EgressNetworkPolicyStatus; compact?: boolean }) {
  const runtime = policy.mode === "runtime";
  const disabled = policy.mode === "disabled";
  const tone = disabled ? "border bg-muted/20" : runtime ? "border border-success/25 bg-success/5" : "border border-warning/30 bg-warning/5";
  return (
    <div className={`${compact ? "mt-0" : "mt-4"} min-w-0 rounded-md px-3 py-2 text-xs leading-5 ${tone}`}>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="font-medium">NetworkPolicy：{networkPolicyModeLabel(policy)}</span>
        {policy.policyName ? <code className="break-all font-mono text-[11px] text-muted-foreground">{policy.policyName}</code> : null}
      </div>
      <p className="mt-1 break-words text-muted-foreground">{policy.message || "当前策略管理信息不可用。"}</p>
      {!runtime && !disabled ? <p className="mt-1 break-words text-muted-foreground">使用 Helm 管理时，请把预检结果中的 <code className="break-all font-mono">networkPolicy.outboundProxy.ipBlocks</code> 与 <code className="font-mono">ports</code> 写入 values 后升级。</p> : null}
      {runtime ? <p className="mt-1 break-words text-muted-foreground">Dosh 只维护独立策略并放行解析出的精确 IP/端口，不会修改 Helm 主策略，也不会使用 0.0.0.0/0。</p> : null}
    </div>
  );
}

function PreflightResult({ result }: { result: EgressPreflight }) {
  const tone = result.status === "ok" ? "border-success/25 bg-success/5" : result.status === "warning" ? "border-warning/30 bg-warning/5" : "border-destructive/25 bg-destructive/5";
  const badge = result.status === "ok" ? "success" : result.status === "warning" ? "warning" : "destructive";
  return (
    <div className={`min-w-0 space-y-3 rounded-md border px-3 py-2 ${tone}`}>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Badge variant={badge}>{result.status === "ok" ? "预检通过" : result.status === "warning" ? "预检警告" : "预检失败"}</Badge>
        <span className="break-words text-xs">{result.message}</span>
      </div>
      {result.targets.length ? (
        <div className="grid min-w-0 gap-2 sm:grid-cols-2">
          {result.targets.map((target) => (
            <div key={`${target.kind}-${target.host}-${target.port}`} className="min-w-0 rounded border bg-background/60 p-2 text-xs">
              <p className="break-all font-mono">{target.kind.toUpperCase()} {target.url}</p>
              <p className="mt-1 break-all text-muted-foreground">IP：{target.addresses.join(", ") || "未解析"} · 端口：{target.port}</p>
              <div className="mt-1 flex flex-wrap gap-1.5">
                <Badge variant={target.allowed ? "success" : "destructive"}>{target.allowed ? "策略已放行" : "策略未放行"}</Badge>
                <Badge variant={target.reachable ? "success" : "warning"}>{target.reachable ? `TCP 可达${target.latencyMs != undefined ? ` · ${target.latencyMs}ms` : ""}` : "TCP 不可达"}</Badge>
              </div>
              {target.error ? <p className="mt-1 break-words text-destructive-strong">{target.error}</p> : null}
            </div>
          ))}
        </div>
      ) : null}
      {result.requiredIpBlocks.length || result.requiredPorts.length ? (
        <div className="min-w-0 space-y-1 text-xs">
          <p className="font-medium">需要的精确放行目标</p>
          <p className="break-all font-mono text-muted-foreground">CIDR: {result.requiredIpBlocks.join(", ") || "—"}</p>
          <p className="break-all font-mono text-muted-foreground">Ports: {result.requiredPorts.join(", ") || "—"}</p>
        </div>
      ) : null}
    </div>
  );
}

function SummaryCard({ icon: Icon, label, value, detail, mono = false }: { icon: typeof Cpu; label: string; value: string; detail: string; mono?: boolean }) {
  return (
    <Card className="min-w-0">
      <CardContent className="p-5">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="text-xs font-medium text-muted-foreground">{label}</p>
            <p className={`mt-2 break-words text-lg font-semibold leading-tight ${mono ? "font-mono text-base" : ""}`}>{value}</p>
            <p className="mt-1 break-words text-xs text-muted-foreground">{detail}</p>
          </div>
          <div className="rounded-md bg-primary/10 p-2 text-primary"><Icon className="h-4 w-4" /></div>
        </div>
      </CardContent>
    </Card>
  );
}

function DetailGrid({ items }: { items: Array<[string, string, boolean?]> }) {
  return (
    <dl className="grid min-w-0 gap-4 sm:grid-cols-2">
      {items.map(([label, value, mono]) => (
        <div key={label} className="min-w-0">
          <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
          <dd className={`mt-1 min-w-0 break-words text-sm ${mono ? "break-all font-mono text-xs leading-5" : ""}`}>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function UpdateDetails({ status }: { status: UpdateStatus }) {
  if (status.status === "disabled") {
    return <div className="rounded-md border bg-muted/25 px-4 py-3 text-sm text-muted-foreground">{status.message || "更新检查未启用。可通过 Helm values 的 updateCheck.enabled 开启。"}</div>;
  }
  return (
    <div className="min-w-0 space-y-4">
      <DetailGrid items={[
        ["当前版本", display(status.currentVersion)],
        ["最新版本", display(status.latestVersion)],
        ["发布时间", displayDate(status.publishedAt)],
        ["检查时间", displayDate(status.checkedAt)],
        ["更新源", display(status.source), true],
        ["最近状态", status.message || status.status],
      ]} />
      {status.latestUrl ? <a href={status.latestUrl} target="_blank" rel="noreferrer" className="inline-flex max-w-full items-center gap-1.5 break-all text-sm text-primary hover:underline"><ExternalLink className="h-3.5 w-3.5 shrink-0" />查看 Release 说明</a> : null}
      {status.notes ? (
        <div className="min-w-0 rounded-md border bg-muted/20 p-3">
          <p className="mb-2 flex items-center gap-1.5 text-xs font-medium"><CalendarClock className="h-3.5 w-3.5" />Release Notes</p>
          <pre className="max-h-48 overflow-y-auto whitespace-pre-wrap break-words font-mono text-xs leading-5 text-muted-foreground">{status.notes}</pre>
        </div>
      ) : null}
      {status.status === "current" ? <p className="flex items-center gap-1.5 text-xs text-success"><CheckCircle2 className="h-3.5 w-3.5" />当前版本无需更新。</p> : null}
    </div>
  );
}

function display(value: string | undefined): string {
  return value && value.trim() ? value : "—";
}

function displayDate(value: string | undefined): string {
  if (!value || value === "unknown") return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : formatDate(value);
}

function diagnosticText(info: SystemInfo, update: UpdateStatus | undefined): string {
  const egress = info.egress;
  return [
    "Dosh 诊断信息",
    `版本: ${display(info.version)}`,
    `提交: ${display(info.commit)}`,
    `构建时间: ${display(info.buildDate)}`,
    `Helm Chart: ${display(info.chartVersion)}`,
    `Go: ${display(info.goVersion)}`,
    `集群: ${display(info.clusterName)}`,
    `认证模式: ${authLabels[info.authMode] ?? info.authMode}`,
    `只读模式: ${info.readOnly ? "是" : "否"}`,
    `请求超时: ${display(info.requestTimeout)}`,
    `出站代理模式: ${egressModeLabels[egress.mode] ?? egress.mode}`,
    `出站代理启用: ${egress.enabled ? "是" : "否"}`,
    `HTTP 代理: ${display(egress.httpProxy)}`,
    `HTTPS 代理: ${display(egress.httpsProxy)}`,
    `NO_PROXY: ${display(egress.noProxy)}`,
    `代理凭据: ${egress.credentialsConfigured ? "已配置" : "未配置"}`,
    `代理 CA: ${egress.caConfigured ? "已配置" : "未配置"}`,
    `更新检查: ${update ? (updateMeta[update.status]?.label ?? update.status) : "未知"}`,
    `最新版本: ${display(update?.latestVersion)}`,
    `检查时间: ${displayDate(update?.checkedAt)}`,
  ].join("\n");
}
