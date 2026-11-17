import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, Loader2, Pencil, Plus, RefreshCw, Star, Trash2, Warehouse } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { ContainerRegistry, ContainerRegistryUpsertInput } from "@/api/types";
import { ErrorState } from "@/components/feedback/states";
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

type FormState = {
  name: string;
  endpoint: string;
  repositoryPrefix: string;
  authType: "anonymous" | "basic" | "bearer";
  username: string;
  password: string;
  token: string;
  caPem: string;
  pullSecretName: string;
  default: boolean;
  insecureSkipTlsVerify: boolean;
  replaceCredentials: boolean;
};

const emptyForm: FormState = {
  name: "", endpoint: "https://", repositoryPrefix: "", authType: "anonymous",
  username: "", password: "", token: "", caPem: "", pullSecretName: "",
  default: false, insecureSkipTlsVerify: false, replaceCredentials: false,
};

function formFor(registry?: ContainerRegistry): FormState {
  if (!registry) return { ...emptyForm };
  return {
    name: registry.name,
    endpoint: registry.endpoint,
    repositoryPrefix: registry.repositoryPrefix ?? "",
    authType: registry.authType,
    username: "", password: "", token: "", caPem: "",
    pullSecretName: registry.pullSecretName ?? "",
    default: registry.default,
    insecureSkipTlsVerify: registry.insecureSkipTlsVerify,
    replaceCredentials: false,
  };
}

export function RegistriesPage() {
  const queryClient = useQueryClient();
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<ContainerRegistry>();
  const [form, setForm] = useState<FormState>(emptyForm);
  const [formError, setFormError] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<ContainerRegistry>();

  const registries = useQuery({
    queryKey: ["container-registries"],
    queryFn: api.containerRegistries,
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["container-registries"] }),
      queryClient.invalidateQueries({ queryKey: ["container-registry-repositories"] }),
      queryClient.invalidateQueries({ queryKey: ["container-registry-tags"] }),
    ]);
  };

  const save = useMutation({
    mutationFn: (input: ContainerRegistryUpsertInput) => editing
      ? api.updateContainerRegistry(editing.id, input)
      : api.createContainerRegistry(input),
    onSuccess: async (saved) => {
      toast.success(editing ? `镜像仓 ${saved.name} 已更新` : `镜像仓 ${saved.name} 已创建`);
      setEditorOpen(false);
      setEditing(undefined);
      await refresh();
    },
    onError: (error) => setFormError(error.message),
  });

  const test = useMutation({
    mutationFn: api.testContainerRegistry,
    onSuccess: async (registry) => {
      if (registry.lastTest?.ok) toast.success(`${registry.name} 连接成功`);
      else toast.error(`${registry.name} 连接失败：${registry.lastTest?.message ?? "未知错误"}`);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const setDefault = useMutation({
    mutationFn: (registry: ContainerRegistry) => api.updateContainerRegistry(registry.id, toInput(registry, { default: true })),
    onSuccess: async (registry) => {
      toast.success(`${registry.name} 已设为默认镜像仓`);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const remove = useMutation({
    mutationFn: (registry: ContainerRegistry) => api.deleteContainerRegistry(registry.id, registry.revision),
    onSuccess: async (_, registry) => {
      toast.success(`镜像仓 ${registry.name} 已删除`);
      setDeleteTarget(undefined);
      await refresh();
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

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!form.name.trim()) return setFormError("请输入镜像仓名称");
    if (!form.endpoint.trim()) return setFormError("请输入镜像仓地址");
    if (form.authType === "basic" && !form.username.trim() && !(editing?.hasCredentials && !form.replaceCredentials)) {
      return setFormError("Basic 认证需要用户名；编辑时可开启“替换已有凭证”后填写");
    }
    if (form.authType === "bearer" && !form.token.trim() && !(editing?.hasCredentials && !form.replaceCredentials)) {
      return setFormError("Bearer 认证需要 Token；编辑时可开启“替换已有凭证”后填写");
    }
    const credentials = form.authType === "anonymous"
      ? undefined
      : ((!editing || form.replaceCredentials) ? {
        username: form.username || undefined,
        password: form.password || undefined,
        token: form.token || undefined,
        caPem: form.caPem.trim() || undefined,
      } : (form.caPem.trim() ? { caPem: form.caPem.trim() } : undefined));
    save.mutate({
      name: form.name.trim(),
      endpoint: form.endpoint.trim(),
      repositoryPrefix: form.repositoryPrefix.trim() || undefined,
      authType: form.authType,
      credentials,
      insecureSkipTlsVerify: form.insecureSkipTlsVerify,
      pullSecretName: form.pullSecretName.trim() || undefined,
      default: form.default,
      revision: editing?.revision,
    });
  };

  if (registries.isError) return <ErrorState error={registries.error} onRetry={() => void registries.refetch()} />;

  return (
    <>
      <PageHeader
        title="镜像仓库"
        breadcrumb="其他功能 / 镜像仓库"
        description="配置多个容器镜像仓，供创建和更新工作负载时搜索仓库、Tag 并自动关联拉取凭证。凭证仅保存在服务端。"
        actions={<Button onClick={() => { setEditing(undefined); setEditorOpen(true); }}><Plus className="h-4 w-4" />添加镜像仓</Button>}
      />

      <div className="mb-4 rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
        镜像浏览使用 Dosh 服务端保存的凭证；工作负载拉取镜像仍依赖目标命名空间中的 <span className="font-mono">imagePullSecrets</span>。如果镜像仓需要认证，请配置对应的拉取 Secret 名称。
      </div>

      {registries.isLoading ? (
        <div className="flex items-center gap-2 rounded-lg border bg-card p-6 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在读取镜像仓配置…</div>
      ) : registries.data?.length ? (
        <div className="grid min-w-0 grid-cols-1 gap-4 xl:grid-cols-2">
          {registries.data.map((registry) => (
            <Card key={registry.id} className="min-w-0 overflow-hidden">
              <CardHeader className="pb-3">
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <CardTitle className="flex min-w-0 flex-wrap items-center gap-2 break-words text-base">
                      <Warehouse className="h-4 w-4 shrink-0 text-primary" />
                      <span className="min-w-0 break-words">{registry.name}</span>
                      {registry.default ? <Badge variant="default" className="shrink-0"><Star className="h-3 w-3" />默认</Badge> : null}
                    </CardTitle>
                    <CardDescription className="mt-1 break-all font-mono text-xs">{registry.endpoint}</CardDescription>
                  </div>
                  <Badge variant={registry.lastTest?.ok ? "success" : registry.lastTest ? "destructive" : "outline"} className="shrink-0">
                    {registry.lastTest?.ok ? <CheckCircle2 className="h-3 w-3" /> : registry.lastTest ? <CircleAlert className="h-3 w-3" /> : null}
                    {registry.lastTest?.ok ? "连接正常" : registry.lastTest ? "连接失败" : "未测试"}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="min-w-0 space-y-4">
                <dl className="grid min-w-0 grid-cols-1 gap-3 text-xs sm:grid-cols-2">
                  <div className="min-w-0"><dt className="text-muted-foreground">仓库范围</dt><dd className="mt-0.5 break-all font-mono">{registry.repositoryPrefix || "全部仓库"}</dd></div>
                  <div className="min-w-0"><dt className="text-muted-foreground">认证方式</dt><dd className="mt-0.5">{authLabel(registry.authType)}{registry.hasCredentials ? " · 已保存凭证" : " · 无凭证"}</dd></div>
                  <div className="min-w-0"><dt className="text-muted-foreground">拉取 Secret</dt><dd className="mt-0.5 break-all font-mono">{registry.pullSecretName || "未配置"}</dd></div>
                  <div className="min-w-0"><dt className="text-muted-foreground">Catalog / Tag</dt><dd className="mt-0.5">{registry.capabilities.catalog ? "支持" : "不可用"} / {registry.capabilities.tags ? "支持" : "未知"}</dd></div>
                </dl>
                {registry.lastTest?.message ? <p className="break-words rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">{registry.lastTest.message}</p> : null}
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={() => test.mutate(registry.id)} disabled={test.isPending}>{test.isPending && test.variables === registry.id ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}测试连接</Button>
                  <Button size="sm" variant="outline" onClick={() => { setEditing(registry); setEditorOpen(true); }}><Pencil className="h-4 w-4" />编辑</Button>
                  {!registry.default ? <Button size="sm" variant="outline" onClick={() => setDefault.mutate(registry)} disabled={setDefault.isPending}><Star className="h-4 w-4" />设为默认</Button> : null}
                  <Button size="sm" variant="outline" className="text-destructive-strong" onClick={() => setDeleteTarget(registry)}><Trash2 className="h-4 w-4" />删除</Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (
        <Card className="min-w-0">
          <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
            <Warehouse className="h-10 w-10 text-muted-foreground" />
            <div><p className="font-medium">尚未配置镜像仓</p><p className="mt-1 text-sm text-muted-foreground">配置后，在创建 Deployment、更新镜像和调试容器时即可搜索选择。</p></div>
            <Button onClick={() => { setEditing(undefined); setEditorOpen(true); }}><Plus className="h-4 w-4" />添加第一个镜像仓</Button>
          </CardContent>
        </Card>
      )}

      <Dialog open={editorOpen} onOpenChange={(next) => { if (!save.isPending) { setEditorOpen(next); if (!next) setEditing(undefined); } }}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑镜像仓" : "添加镜像仓"}</DialogTitle>
            <DialogDescription className="break-words">地址仅填写协议和主机，例如 https://harbor.example.com；仓库范围填写可选路径前缀。</DialogDescription>
          </DialogHeader>
          <form className="min-w-0 space-y-4" onSubmit={submit}>
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <div className="min-w-0 space-y-1.5"><Label htmlFor="registry-name">名称</Label><Input id="registry-name" value={form.name} onChange={(event) => update("name", event.target.value)} placeholder="生产 Harbor" maxLength={128} /></div>
              <div className="min-w-0 space-y-1.5"><Label htmlFor="registry-endpoint">镜像仓地址</Label><Input id="registry-endpoint" value={form.endpoint} onChange={(event) => update("endpoint", event.target.value)} placeholder="https://harbor.example.com" className="font-mono" autoComplete="off" spellCheck={false} /></div>
              <div className="min-w-0 space-y-1.5"><Label htmlFor="registry-prefix">仓库范围（可选）</Label><Input id="registry-prefix" value={form.repositoryPrefix} onChange={(event) => update("repositoryPrefix", event.target.value)} placeholder="platform" className="font-mono" /></div>
              <div className="min-w-0 space-y-1.5"><Label htmlFor="registry-pull-secret">拉取 Secret（可选）</Label><Input id="registry-pull-secret" value={form.pullSecretName} onChange={(event) => update("pullSecretName", event.target.value)} placeholder="registry-pull-secret" className="font-mono" /></div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="registry-auth">认证方式</Label>
                <Select value={form.authType} onValueChange={(value) => update("authType", value as FormState["authType"])}>
                  <SelectTrigger id="registry-auth"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="anonymous">匿名</SelectItem><SelectItem value="basic">用户名 / 密码</SelectItem><SelectItem value="bearer">Bearer Token</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="flex min-w-0 flex-col justify-end gap-2 text-sm">
                <label className="flex items-center gap-2"><input type="checkbox" checked={form.default} onChange={(event) => update("default", event.target.checked)} />设为默认镜像仓</label>
                <label className="flex items-center gap-2"><input type="checkbox" checked={form.insecureSkipTlsVerify} onChange={(event) => update("insecureSkipTlsVerify", event.target.checked)} />跳过 TLS 证书校验</label>
              </div>
            </div>

            {form.authType !== "anonymous" ? (
              <div className="min-w-0 space-y-3 rounded-md border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <p className="text-sm font-medium">访问凭证</p>
                  {editing?.hasCredentials ? <label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={form.replaceCredentials} onChange={(event) => update("replaceCredentials", event.target.checked)} />替换已有凭证</label> : null}
                </div>
                {!editing || form.replaceCredentials ? (
                  <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                    {form.authType === "basic" ? <>
                      <div className="min-w-0 space-y-1.5"><Label htmlFor="registry-username">用户名</Label><Input id="registry-username" value={form.username} onChange={(event) => update("username", event.target.value)} autoComplete="username" /></div>
                      <div className="min-w-0 space-y-1.5"><Label htmlFor="registry-password">密码</Label><Input id="registry-password" type="password" value={form.password} onChange={(event) => update("password", event.target.value)} autoComplete="new-password" /></div>
                    </> : <div className="min-w-0 space-y-1.5 sm:col-span-2"><Label htmlFor="registry-token">Bearer Token</Label><Input id="registry-token" type="password" value={form.token} onChange={(event) => update("token", event.target.value)} autoComplete="new-password" /></div>}
                  </div>
                ) : <p className="text-xs text-muted-foreground">已保存的凭证不会返回浏览器。留空表示继续使用原凭证。</p>}
              </div>
            ) : null}

            <div className="min-w-0 space-y-1.5">
              <Label htmlFor="registry-ca">自定义 CA（PEM，可选）</Label>
              <Textarea id="registry-ca" value={form.caPem} onChange={(event) => update("caPem", event.target.value)} placeholder="-----BEGIN CERTIFICATE-----" className="min-h-24 font-mono text-xs" spellCheck={false} />
            </div>

            {form.insecureSkipTlsVerify ? <p className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">跳过 TLS 校验会降低连接安全性，仅建议用于受控的临时环境。</p> : null}
            {formError ? <p className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">{formError}</p> : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditorOpen(false)} disabled={save.isPending}>取消</Button>
              <Button type="submit" disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editing ? "保存修改" : "添加镜像仓"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !remove.isPending) setDeleteTarget(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除镜像仓 {deleteTarget?.name}？</AlertDialogTitle>
            <AlertDialogDescription>已创建工作负载不会受影响，但后续将不能在镜像选择器中浏览该镜像仓。</AlertDialogDescription>
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

function authLabel(value: ContainerRegistry["authType"]) {
  switch (value) {
    case "basic": return "用户名 / 密码";
    case "bearer": return "Bearer Token";
    default: return "匿名";
  }
}

function toInput(registry: ContainerRegistry, overrides: Partial<ContainerRegistryUpsertInput> = {}): ContainerRegistryUpsertInput {
  return {
    name: registry.name,
    endpoint: registry.endpoint,
    repositoryPrefix: registry.repositoryPrefix,
    authType: registry.authType,
    insecureSkipTlsVerify: registry.insecureSkipTlsVerify,
    pullSecretName: registry.pullSecretName,
    default: registry.default,
    revision: registry.revision,
    ...overrides,
  };
}

