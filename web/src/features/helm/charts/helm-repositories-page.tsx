import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, CalendarClock, CheckCircle2, CircleAlert, Database, Loader2, LockKeyhole, Pencil, Plus, RefreshCw, Star, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { HelmRepository, HelmRepositoryUpsertInput } from "@/api/types";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useCapabilities } from "@/features/auth/use-session";
import { HelmSectionNav } from "@/features/helm/helm-section-nav";
import { formatDate } from "@/lib/utils";

type AuthType = "anonymous" | "basic" | "bearer";
type RepositoryProtocol = "http" | "oci";

type FormState = {
  protocol: RepositoryProtocol;
  name: string;
  url: string;
  description: string;
  authType: AuthType;
  username: string;
  password: string;
  token: string;
  caPEM: string;
  default: boolean;
  enabled: boolean;
  syncEnabled: boolean;
  syncIntervalHours: string;
  insecureSkipTLSVerify: boolean;
  replaceCredentials: boolean;
  clearCredentials: boolean;
};

const emptyForm: FormState = {
  protocol: "http", name: "", url: "https://", description: "", authType: "anonymous", username: "", password: "", token: "", caPEM: "",
  default: false, enabled: true, syncEnabled: false, syncIntervalHours: "3", insecureSkipTLSVerify: false, replaceCredentials: false, clearCredentials: false,
};

function formFor(repository?: HelmRepository): FormState {
  if (!repository) return { ...emptyForm };
  return {
    protocol: repository.url.startsWith("oci://") ? "oci" : "http",
    name: repository.name,
    url: repository.url,
    description: repository.description ?? "",
    authType: repository.authType ?? "anonymous",
    username: "", password: "", token: "", caPEM: "",
    default: repository.default,
    enabled: repository.enabled,
    syncEnabled: repository.syncEnabled,
    syncIntervalHours: String(Math.max(1, Math.round(repository.syncIntervalSeconds / 3600))),
    insecureSkipTLSVerify: repository.insecureSkipTLSVerify,
    replaceCredentials: false,
    clearCredentials: false,
  };
}

export function HelmRepositoriesPage() {
  const capabilities = useCapabilities().data;
  const queryClient = useQueryClient();
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<HelmRepository>();
  const [form, setForm] = useState<FormState>(emptyForm);
  const [formError, setFormError] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<HelmRepository>();
  const repositories = useQuery({
    queryKey: ["helm-repositories"],
    queryFn: api.helmRepositories,
    enabled: Boolean(capabilities?.helmChartRepositories),
  });
  const cache = useQuery({
    queryKey: ["helm-cache"],
    queryFn: api.helmCacheStats,
    enabled: Boolean(capabilities?.helmChartRepositories),
    refetchInterval: 60_000,
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["helm-repositories"] }),
      queryClient.invalidateQueries({ queryKey: ["helm-charts"] }),
    ]);
  };

  const save = useMutation({
    mutationFn: (input: HelmRepositoryUpsertInput) => editing ? api.updateHelmRepository(editing.id, input) : api.createHelmRepository(input),
    onSuccess: async (saved) => {
      toast.success(editing ? `Chart 仓库 ${saved.name} 已更新` : `Chart 仓库 ${saved.name} 已创建`);
      if (saved.lastSync && !saved.lastSync.ok) toast.warning(`仓库已保存，但首次同步失败：${saved.lastSync.message}`);
      setEditorOpen(false);
      setEditing(undefined);
      await refresh();
    },
    onError: (error) => setFormError(error.message),
  });

  const sync = useMutation({
    mutationFn: api.syncHelmRepository,
    onSuccess: async (repository) => {
      if (repository.lastSync?.ok) toast.success(`${repository.name} 同步成功，共 ${repository.lastSync.chartCount} 个 Chart`);
      else toast.error(`${repository.name} 同步失败：${repository.lastSync?.message ?? "未知错误"}`);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const setDefault = useMutation({
    mutationFn: (repository: HelmRepository) => api.updateHelmRepository(repository.id, toInput(repository, { default: true })),
    onSuccess: async () => {
      toast.success("默认 Chart 仓库已更新");
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const remove = useMutation({
    mutationFn: (repository: HelmRepository) => api.deleteHelmRepository(repository.id, repository.revision),
    onSuccess: async (_, repository) => {
      toast.success(`Chart 仓库 ${repository.name} 已删除`);
      setDeleteTarget(undefined);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const cleanupCache = useMutation({
    mutationFn: api.cleanupHelmCache,
    onSuccess: async (result) => {
      toast.success(result.deletedFiles > 0 ? `已清理 ${result.deletedFiles} 个缓存文件，释放 ${formatBytes(result.freedBytes)}` : "缓存未超过容量上限，无需清理");
      await queryClient.invalidateQueries({ queryKey: ["helm-cache"] });
    },
    onError: (error) => toast.error(error.message),
  });

  useEffect(() => {
    if (!editorOpen) return;
    setForm(formFor(editing));
    setFormError("");
  }, [editing, editorOpen]);

  const update = <Key extends keyof FormState>(key: Key, value: FormState[Key]) => {
    setForm((current) => ({ ...current, [key]: value }));
    setFormError("");
  };

  const updateProtocol = (protocol: RepositoryProtocol) => {
    setForm((current) => ({
      ...current,
      protocol,
      url: protocol === "oci"
        ? (current.url.startsWith("oci://") ? current.url : "oci://")
        : (current.url.startsWith("oci://") ? "https://" : current.url),
    }));
    setFormError("");
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const url = form.url.trim();
    if (form.protocol === "oci") {
      const match = /^oci:\/\/([^/]+)\/(.+)$/i.exec(url);
      const chartPath = match?.[2]?.replace(/\/+$/, "") ?? "";
      const chartName = chartPath.split("/").filter(Boolean).pop() ?? "";
      if (!match || !chartName || chartName.includes(":")) {
        setFormError("OCI 地址必须指向单个 Chart 且不能包含 :tag，例如 oci://registry.example.com/charts/nginx");
        return;
      }
    }
    if (form.protocol === "http" && !/^https?:\/\//i.test(url)) {
      setFormError("HTTP Chart 仓库地址必须以 http:// 或 https:// 开头");
      return;
    }
    const replace = !editing || form.replaceCredentials;
    const syncIntervalHours = Number(form.syncIntervalHours);
    if (form.syncEnabled && (!Number.isFinite(syncIntervalHours) || syncIntervalHours < 1 || syncIntervalHours > 168)) {
      setFormError("定时同步间隔必须在 1 到 168 小时之间");
      return;
    }
    const input: HelmRepositoryUpsertInput = {
      name: form.name,
      url,
      description: form.description,
      default: form.default,
      enabled: form.enabled,
      syncEnabled: form.syncEnabled,
      syncIntervalSeconds: Math.round(syncIntervalHours * 3600),
      insecureSkipTLSVerify: form.insecureSkipTLSVerify,
      clearCredentials: form.clearCredentials,
      revision: editing?.revision,
    };
    if (!form.clearCredentials && replace && form.authType !== "anonymous") {
      input.authType = form.authType;
      input.username = form.authType === "basic" ? form.username : "";
      input.password = form.authType === "basic" ? form.password : "";
      input.token = form.authType === "bearer" ? form.token : "";
      input.caPEM = form.caPEM;
    } else if (!form.clearCredentials && replace) {
      input.authType = "anonymous";
      input.caPEM = form.caPEM;
    }
    if (form.clearCredentials) input.authType = "anonymous";
    save.mutate(input);
  };

  if (!capabilities) return <LoadingState label="正在检查 Chart 仓库能力" />;
  if (!capabilities.helmChartRepositories) {
    return <ErrorState error={new Error("当前身份没有 list dosh.io/helm-repositories 权限，无法管理 Chart 仓库。")} />;
  }
  if (repositories.isError) return <ErrorState error={repositories.error} onRetry={() => void repositories.refetch()} />;
  const canWrite = Boolean(capabilities.helmChartWrite);
  const failedRepositories = (repositories.data ?? []).filter((item) => item.enabled && item.lastSync?.ok === false);

  return (
    <>
      <PageHeader
        title="Chart 仓库"
        breadcrumb="应用 / Helm / 仓库管理"
        description="仅管理员登记的 HTTP/HTTPS 或 OCI Chart 仓库会进入目录。凭据保存在控制面 Secret 中，不会返回浏览器。"
        actions={<Button onClick={() => { setEditing(undefined); setEditorOpen(true); }} disabled={!canWrite}><Plus className="h-4 w-4" />添加仓库</Button>}
      />
      <HelmSectionNav active="repositories" />

      {!canWrite ? (
        <div className="mb-4 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />当前身份可以浏览仓库，但没有 create dosh.io/helm-repositories 权限。
        </div>
      ) : null}

      {failedRepositories.length > 0 ? (
        <div role="alert" className="mb-4 flex min-w-0 items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span className="min-w-0 break-words">
            {failedRepositories.length} 个仓库最近同步失败：{failedRepositories.map((item) => item.name).join("、")}。失败后会自动退避重试，仍可手动同步验证凭据与网络。
          </span>
        </div>
      ) : null}

      {repositories.isLoading ? (
        <LoadingState label="正在读取 Chart 仓库" />
      ) : (repositories.data?.length ?? 0) === 0 ? (
        <EmptyState
          title="尚未配置 Chart 仓库"
          description="登记提供 index.yaml 的 HTTPS 仓库，或指向单个 Chart 的 OCI 地址后，即可浏览并安装固定版本。"
          action={canWrite ? <Button onClick={() => { setEditing(undefined); setEditorOpen(true); }}><Plus className="h-4 w-4" />添加第一个仓库</Button> : undefined}
        />
      ) : (
        <div className="grid min-w-0 gap-3 lg:grid-cols-2">
          {repositories.data?.map((repository) => (
            <Card key={repository.id} className="min-w-0">
              <CardHeader className="min-w-0 gap-2 p-4 sm:p-5">
                <div className="flex min-w-0 items-start gap-3">
                  <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-primary/10 text-primary"><Archive className="h-4 w-4" /></div>
                  <div className="min-w-0 flex-1">
                    <CardTitle className="flex min-w-0 flex-wrap items-center gap-2 text-base"><span className="break-all">{repository.name}</span>{repository.default ? <Badge>默认</Badge> : null}{!repository.enabled ? <Badge variant="secondary">已停用</Badge> : null}</CardTitle>
                    {repository.syncEnabled ? <Badge variant="outline"><CalendarClock className="h-3 w-3" />每 {formatInterval(repository.syncIntervalSeconds)} 同步</Badge> : null}
                    <CardDescription className="mt-1 break-all font-mono text-xs" title={repository.url}>{repository.url}</CardDescription>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="min-w-0 space-y-4 px-4 pb-4 sm:px-5">
                {repository.description ? <p className="break-words text-sm text-muted-foreground">{repository.description}</p> : null}
                <div className="flex flex-wrap gap-1.5">
                  <Badge variant="outline">{repository.chartCount} 个 Chart</Badge>
                  {repository.hasCredentials ? <Badge variant="secondary"><LockKeyhole className="h-3 w-3" />凭据</Badge> : null}
                  {repository.hasCustomCA ? <Badge variant="secondary">自定义 CA</Badge> : null}
                  {repository.insecureSkipTLSVerify ? <Badge variant="warning">跳过 TLS 校验</Badge> : null}
                  {repository.url.startsWith("http://") ? <Badge variant="destructive">明文 HTTP</Badge> : null}
                  {repository.url.startsWith("oci://") ? <Badge variant="outline">OCI</Badge> : null}
                  {(repository.syncFailureCount ?? 0) > 0 ? <Badge variant="destructive">连续失败 {repository.syncFailureCount} 次</Badge> : null}
                </div>
                <div className={`rounded-md border px-3 py-2 text-xs ${repository.lastSync?.ok ? "border-success/25 bg-success/5" : "border-warning/30 bg-warning/5"}`}>
                  <div className="flex min-w-0 items-start gap-2">
                    {repository.lastSync?.ok ? <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" /> : <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />}
                    <div className="min-w-0">
                      <p className="break-words font-medium">{repository.lastSync ? (repository.lastSync.ok ? "最近同步成功" : "最近同步失败") : "尚未同步"}</p>
                      <p className="mt-0.5 break-words text-muted-foreground">{repository.lastSync?.message ?? "点击同步检查仓库可用性。"}</p>
                      {repository.lastSync ? <p className="mt-1 text-muted-foreground">{formatDate(repository.lastSync.syncedAt)}</p> : null}
                      {repository.syncEnabled && repository.nextSyncAt ? <p className="mt-1 text-muted-foreground">{repository.lastSync?.ok === false ? "下次重试" : "下次同步"}：{formatDate(repository.nextSyncAt)}</p> : null}
                      {repository.lastSync?.trigger ? <p className="mt-1 text-muted-foreground">触发方式：{triggerLabel(repository.lastSync.trigger)}{repository.lastSync.attempt ? ` · 第 ${repository.lastSync.attempt} 次尝试` : ""}</p> : null}
                    </div>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={() => sync.mutate(repository.id)} disabled={!canWrite || sync.isPending}><RefreshCw className={`h-3.5 w-3.5 ${sync.isPending && sync.variables === repository.id ? "animate-spin" : ""}`} />同步</Button>
                  <Button size="sm" variant="outline" onClick={() => { setEditing(repository); setEditorOpen(true); }} disabled={!canWrite}><Pencil className="h-3.5 w-3.5" />编辑</Button>
                  {!repository.default ? <Button size="sm" variant="outline" onClick={() => setDefault.mutate(repository)} disabled={!canWrite || setDefault.isPending}><Star className="h-3.5 w-3.5" />设为默认</Button> : null}
                  <Button size="sm" variant="outline" className="text-destructive-strong" onClick={() => setDeleteTarget(repository)} disabled={!canWrite}><Trash2 className="h-3.5 w-3.5" />删除</Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Card className="mt-4 min-w-0">
        <CardHeader className="min-w-0 flex-row items-start justify-between gap-3 p-4 sm:p-5">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base"><Database className="h-4 w-4" />Chart 缓存</CardTitle>
            <CardDescription className="mt-1 break-words">索引、Chart 包和 OCI Tag 元数据持久化在 Dosh 数据目录；达到容量上限时优先淘汰最旧的 Chart 包。</CardDescription>
          </div>
          <Button size="sm" variant="outline" onClick={() => cleanupCache.mutate()} disabled={!canWrite || cleanupCache.isPending}>{cleanupCache.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}清理缓存</Button>
        </CardHeader>
        <CardContent className="min-w-0 px-4 pb-4 sm:px-5">
          {cache.isLoading ? <LoadingState label="正在读取缓存统计" /> : cache.isError ? <p className="break-words text-sm text-destructive-strong">缓存统计不可用：{cache.error.message}</p> : cache.data ? (
            <div className="grid min-w-0 gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <div className="min-w-0 rounded-md border p-3"><p className="text-xs text-muted-foreground">总使用量</p><p className="mt-1 break-all font-medium">{formatBytes(cache.data.totalBytes)} / {formatBytes(cache.data.limitBytes)}</p></div>
              <div className="min-w-0 rounded-md border p-3"><p className="text-xs text-muted-foreground">索引 / Chart 包</p><p className="mt-1 break-all font-medium">{cache.data.indexCount} / {cache.data.packageCount}</p></div>
              <div className="min-w-0 rounded-md border p-3"><p className="text-xs text-muted-foreground">索引大小</p><p className="mt-1 break-all font-medium">{formatBytes(cache.data.indexBytes)}</p></div>
              <div className="min-w-0 rounded-md border p-3"><p className="text-xs text-muted-foreground">Chart 包大小</p><p className="mt-1 break-all font-medium">{formatBytes(cache.data.packageBytes)}</p></div>
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Dialog open={editorOpen} onOpenChange={(next) => { if (!save.isPending) { setEditorOpen(next); if (!next) setEditing(undefined); } }}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑 Chart 仓库" : "添加 Chart 仓库"}</DialogTitle>
            <DialogDescription className="break-words">
              {form.protocol === "oci"
                ? "OCI 地址应指向单个 Chart，版本从镜像 Tag 读取。例如 oci://registry.example.com/charts/nginx。"
                : "填写提供 index.yaml 的仓库根地址。HTTPS 可保护索引和 Chart 包，明文 HTTP 仅建议用于隔离的受控网络。"}
            </DialogDescription>
          </DialogHeader>
          <form className="min-w-0 space-y-4" onSubmit={submit}>
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <div className="min-w-0 space-y-1.5"><Label htmlFor="helm-repo-name">名称</Label><Input id="helm-repo-name" value={form.name} onChange={(event) => update("name", event.target.value)} placeholder="Bitnami" maxLength={128} required /></div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-repo-protocol">仓库类型</Label>
                <Select value={form.protocol} onValueChange={(value) => updateProtocol(value as RepositoryProtocol)}>
                  <SelectTrigger id="helm-repo-protocol"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="http">HTTP / HTTPS 索引</SelectItem><SelectItem value="oci">OCI Chart</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="min-w-0 space-y-1.5 sm:col-span-2">
                <Label htmlFor="helm-repo-url">{form.protocol === "oci" ? "OCI Chart 地址" : "仓库地址"}</Label>
                <Input id="helm-repo-url" value={form.url} onChange={(event) => update("url", event.target.value)} placeholder={form.protocol === "oci" ? "oci://registry.example.com/charts/nginx" : "https://charts.example.com"} className="font-mono" autoComplete="off" spellCheck={false} required />
                <p className="break-words text-xs text-muted-foreground">{form.protocol === "oci" ? "地址应指向一个 Chart，不能包含 :tag；版本将通过 OCI Tag 自动列出。" : "仓库根地址必须提供标准 index.yaml，Chart 包必须位于同一主机。"}</p>
              </div>
              <div className="min-w-0 space-y-1.5 sm:col-span-2"><Label htmlFor="helm-repo-description">说明（可选）</Label><Input id="helm-repo-description" value={form.description} onChange={(event) => update("description", event.target.value)} placeholder="生产环境内部 Chart 仓库" maxLength={500} /></div>
              <div className="flex min-w-0 flex-col justify-end gap-2 text-sm">
                <label className="flex items-center gap-2"><input type="checkbox" checked={form.default} onChange={(event) => update("default", event.target.checked)} />设为默认仓库</label>
                <label className="flex items-center gap-2"><input type="checkbox" checked={form.enabled} onChange={(event) => update("enabled", event.target.checked)} />启用并展示到 Chart 目录</label>
              </div>
              <div className="flex min-w-0 flex-col justify-end gap-2 text-sm">
                <label className="flex items-center gap-2"><input type="checkbox" checked={form.insecureSkipTLSVerify} onChange={(event) => update("insecureSkipTLSVerify", event.target.checked)} />跳过 TLS 证书校验</label>
                {editing?.hasCredentials || editing?.hasCustomCA ? <label className="flex items-center gap-2"><input type="checkbox" checked={form.clearCredentials} onChange={(event) => update("clearCredentials", event.target.checked)} />清除已保存认证与 CA</label> : null}
              </div>
            </div>

            <div className="min-w-0 space-y-3 rounded-md border p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="text-sm font-medium">定时同步</p>
                  <p className="mt-0.5 break-words text-xs text-muted-foreground">默认关闭。开启后由 Dosh 按固定周期刷新 index.yaml 与 OCI Tag；Chart 目录只读取已同步结果。</p>
                </div>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.syncEnabled} onChange={(event) => update("syncEnabled", event.target.checked)} />启用定时同步</label>
              </div>
              <div className="max-w-xs min-w-0 space-y-1.5">
                <Label htmlFor="helm-repo-sync-interval">同步间隔（小时）</Label>
                <Input id="helm-repo-sync-interval" type="number" min={1} max={168} step={1} value={form.syncIntervalHours} onChange={(event) => update("syncIntervalHours", event.target.value)} disabled={!form.syncEnabled} />
                <p className="text-xs text-muted-foreground">默认 3 小时，允许 1 到 168 小时。</p>
              </div>
            </div>

            {!form.clearCredentials ? (
              <div className="min-w-0 space-y-3 rounded-md border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <p className="text-sm font-medium">仓库认证</p>
                  {editing?.hasCredentials || editing?.hasCustomCA ? <label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={form.replaceCredentials} onChange={(event) => update("replaceCredentials", event.target.checked)} />替换已保存认证或 CA</label> : null}
                </div>
                {!editing || form.replaceCredentials ? (
                  <>
                    <div className="min-w-0 space-y-1.5">
                      <Label htmlFor="helm-repo-auth">认证方式</Label>
                      <Select value={form.authType} onValueChange={(value) => update("authType", value as AuthType)}>
                        <SelectTrigger id="helm-repo-auth"><SelectValue /></SelectTrigger>
                        <SelectContent><SelectItem value="anonymous">匿名</SelectItem><SelectItem value="basic">用户名 / 密码</SelectItem><SelectItem value="bearer">Bearer Token</SelectItem></SelectContent>
                      </Select>
                    </div>
                    {form.authType === "basic" ? (
                      <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                        <div className="min-w-0 space-y-1.5"><Label htmlFor="helm-repo-username">用户名</Label><Input id="helm-repo-username" value={form.username} onChange={(event) => update("username", event.target.value)} autoComplete="username" /></div>
                        <div className="min-w-0 space-y-1.5"><Label htmlFor="helm-repo-password">密码</Label><Input id="helm-repo-password" type="password" value={form.password} onChange={(event) => update("password", event.target.value)} autoComplete="new-password" /></div>
                      </div>
                    ) : null}
                    {form.authType === "bearer" ? <div className="min-w-0 space-y-1.5"><Label htmlFor="helm-repo-token">Bearer Token</Label><Input id="helm-repo-token" type="password" value={form.token} onChange={(event) => update("token", event.target.value)} autoComplete="new-password" /></div> : null}
                  </>
                ) : (
                  <div className="flex flex-wrap gap-1.5" role="status">
                    {editing?.hasCredentials ? <Badge variant="secondary"><LockKeyhole className="h-3 w-3" />已保存{editing.authType === "basic" ? "用户名 / 密码" : editing.authType === "bearer" ? "Bearer Token" : "认证凭据"}</Badge> : null}
                    {editing?.hasCustomCA ? <Badge variant="secondary"><LockKeyhole className="h-3 w-3" />已保存自定义 CA</Badge> : null}
                    <span className="w-full text-xs text-muted-foreground">敏感内容不会返回浏览器。勾选“替换已保存认证或 CA”后才能更新。</span>
                  </div>
                )}
                {!editing || form.replaceCredentials ? (
                  <div className="min-w-0 space-y-1.5">
                    <Label htmlFor="helm-repo-ca">自定义 CA（PEM，可选）</Label>
                    <Textarea id="helm-repo-ca" value={form.caPEM} onChange={(event) => update("caPEM", event.target.value)} placeholder="-----BEGIN CERTIFICATE-----" className="min-h-24 font-mono text-xs" spellCheck={false} />
                  </div>
                ) : null}
              </div>
            ) : null}

            {form.insecureSkipTLSVerify ? <p className="break-words rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">跳过 TLS 校验会降低供应链安全性，仅建议用于受控的临时环境。</p> : null}
            {formError ? <p className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">{formError}</p> : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditorOpen(false)} disabled={save.isPending}>取消</Button>
              <Button type="submit" disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editing ? "保存修改" : "添加仓库"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !remove.isPending) setDeleteTarget(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 Chart 仓库 {deleteTarget?.name}？</AlertDialogTitle>
            <AlertDialogDescription>已安装的 Release 不受影响，但目录中不再展示该仓库，已保存的访问凭据也会删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction disabled={remove.isPending} onClick={(event) => { event.preventDefault(); if (deleteTarget) remove.mutate(deleteTarget); }}>{remove.isPending ? "删除中…" : "确认删除"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function toInput(repository: HelmRepository, overrides: Partial<HelmRepositoryUpsertInput> = {}): HelmRepositoryUpsertInput {
  return {
    name: repository.name,
    url: repository.url,
    description: repository.description,
    default: repository.default,
    enabled: repository.enabled,
    syncEnabled: repository.syncEnabled,
    syncIntervalSeconds: repository.syncIntervalSeconds,
    insecureSkipTLSVerify: repository.insecureSkipTLSVerify,
    revision: repository.revision,
    ...overrides,
  };
}

function formatInterval(seconds: number): string {
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))} 分钟`;
  const hours = seconds / 3600;
  return Number.isInteger(hours) ? `${hours} 小时` : `${hours.toFixed(1)} 小时`;
}

function triggerLabel(trigger: string): string {
  switch (trigger) {
    case "scheduled": return "定时同步";
    case "manual": return "手动同步";
    case "create": return "创建仓库";
    case "update": return "保存配置";
    default: return trigger;
  }
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(1024)));
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}
