import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Eye, FileCode2, LockKeyhole, Pencil, Plus, Settings2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { ConfigMapDetail, ConfigMapSummary, SecretReveal, SecretSummary } from "@/api/types";
import { CopyButton } from "@/components/data/copy-button";
import { DataTable } from "@/components/data/data-table";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { ActionTooltip } from "@/components/feedback/action-tooltip";
import { DetailItem } from "@/components/data/detail-item";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCapabilities } from "@/features/auth/use-session";
import { ConfigMapFormDialog } from "@/features/configuration/config-map-form-dialog";
import { SecretFormDialog } from "@/features/configuration/secret-form-dialog";
import { ServerCertificatePanel } from "@/features/configuration/server-certificate-panel";
import { resourceCapabilities } from "@/lib/capabilities";
import { decodeSecretValue } from "@/lib/secret-value";
import { formatAge } from "@/lib/utils";

const configMapColumn = createColumnHelper<ConfigMapSummary>();
const secretColumn = createColumnHelper<SecretSummary>();
type YamlTarget = { type: "configmap" | "secret"; namespace: string; name: string };

function SecretValueBlock({ name, encoded }: { name: string; encoded: string }) {
  const decoded = decodeSecretValue(encoded);
  const label = decoded.kind === "text" ? "文本" : decoded.kind === "binary" ? "Base64（二进制）" : "无法解码";
  return (
    <div className="min-w-0 rounded-md border">
      <div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2">
        <div className="flex min-w-0 items-center gap-2"><span className="truncate font-mono text-xs font-medium">{name}</span><Badge variant="outline">{label}</Badge></div>
        <CopyButton value={decoded.copyValue} label={decoded.kind === "text" ? "复制明文" : "复制原值"} />
      </div>
      {decoded.kind === "binary" ? <p className="border-b bg-muted/20 px-3 py-2 text-xs text-muted-foreground">该值不是 UTF-8 文本，按原始 Base64 显示和复制。</p> : null}
      {decoded.kind === "invalid" ? <p className="border-b bg-warning/10 px-3 py-2 text-xs text-warning">该值不是有效 Base64，按原始响应显示和复制。</p> : null}
      <pre className="soft-wrap max-w-full overflow-auto whitespace-pre-wrap p-3 font-mono text-xs" aria-label={`${name} 值`}>{decoded.display || "（空值）"}</pre>
    </div>
  );
}

export function ConfigurationPage() {
  const search = useSearch({ from: "/app/configuration" });
  const navigate = useNavigate();
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const tab = search.tab === "secrets" ? "secrets" : search.tab === "certificate" ? "certificate" : "configmaps";
  const updateSearch = (patch: { q?: string; namespace?: string; tab?: string }) => {
    void navigate({ to: "/configuration", search: { ...search, ...patch }, replace: true });
  };
  const [configMap, setConfigMap] = useState<ConfigMapSummary | null>(null);
  const [configMapFormOpen, setConfigMapFormOpen] = useState(false);
  const [editingConfigMap, setEditingConfigMap] = useState<ConfigMapDetail>();
  const [deletingConfigMap, setDeletingConfigMap] = useState<ConfigMapSummary>();
  const [secretFormOpen, setSecretFormOpen] = useState(false);
  const [editingSecret, setEditingSecret] = useState<SecretReveal>();
  const [deletingSecret, setDeletingSecret] = useState<SecretSummary>();
  const [yamlTarget, setYamlTarget] = useState<YamlTarget>();
  const [revealResult, setRevealResult] = useState<SecretReveal | null>(null);
  const configMaps = useQuery({ queryKey: ["configmaps", namespace, q], queryFn: () => api.configmaps({ namespace, q }), enabled: tab !== "certificate" });
  const secrets = useQuery({ queryKey: ["secrets", namespace, q], queryFn: () => api.secrets({ namespace, q }), enabled: tab !== "certificate" });
  const capabilities = useCapabilities(namespace || undefined).data;
  const configMapCaps = resourceCapabilities(capabilities, "configmaps");
  const secretCaps = resourceCapabilities(capabilities, "secrets");
  const reveal = useMutation({ mutationFn: (target: SecretSummary) => api.revealSecret(target.namespace, target.name), onSuccess: (data) => setRevealResult(data), onError: (error) => toast.error(error.message) });
  const refreshConfigMaps = async () => { await configMaps.refetch(); };
  const refreshSecrets = async () => { await secrets.refetch(); };
  const openConfigMapEdit = async (item: ConfigMapSummary) => {
    try { setEditingConfigMap(await api.configmap(item.namespace, item.name)); setConfigMapFormOpen(true); } catch (error) { toast.error(error instanceof Error ? error.message : "加载 ConfigMap 失败"); }
  };
  const openSecretEdit = async (item: SecretSummary) => {
    try { setEditingSecret(await api.revealSecret(item.namespace, item.name)); setSecretFormOpen(true); } catch (error) { toast.error(error instanceof Error ? error.message : "加载 Secret 失败"); }
  };
  const configMapActions = (item: ConfigMapSummary): ResourceAction[] => [
    { label: "查看数据", icon: <Eye className="h-4 w-4" />, onSelect: () => setConfigMap(item) },
    { label: "编辑 ConfigMap", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openConfigMapEdit(item), disabled: !configMapCaps.update },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "configmap", namespace: item.namespace, name: item.name }) },
    { label: "删除 ConfigMap", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingConfigMap(item), destructive: true, disabled: !configMapCaps.delete, separatorBefore: true },
  ];
  const secretActions = (item: SecretSummary): ResourceAction[] => [
    { label: "查看数据", icon: <Eye className="h-4 w-4" />, onSelect: () => reveal.mutate(item), disabled: !capabilities?.revealSecrets },
    { label: "编辑 Secret", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openSecretEdit(item), disabled: !secretCaps.update || !capabilities?.revealSecrets },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "secret", namespace: item.namespace, name: item.name }) },
    { label: "删除 Secret", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingSecret(item), destructive: true, disabled: !secretCaps.delete, separatorBefore: true },
  ];
  const error = tab === "certificate" ? null : configMaps.error || secrets.error;
  if (error) return <ErrorState error={error} onRetry={() => { void configMaps.refetch(); void secrets.refetch(); }} />;
  const configMapCreateHint = !namespace ? "请先选择命名空间，再创建 ConfigMap" : !configMapCaps.create ? "当前身份没有创建 ConfigMap 的权限" : undefined;
  const secretCreateHint = !namespace ? "请先选择命名空间，再创建 Secret" : !secretCaps.create ? "当前身份没有创建 Secret 的权限" : undefined;
  return (
    <>
      <PageHeader
        title="配置中心"
        breadcrumb="配置 / 配置中心"
        description="管理 ConfigMap、Secret 与服务端证书。"
        actions={tab === "configmaps" ? (
          <ActionTooltip content={configMapCreateHint}><Button onClick={() => { setEditingConfigMap(undefined); setConfigMapFormOpen(true); }} disabled={Boolean(configMapCreateHint)}><Plus className="h-4 w-4" />创建 ConfigMap</Button></ActionTooltip>
        ) : tab === "secrets" ? (
          <ActionTooltip content={secretCreateHint}><Button onClick={() => { setEditingSecret(undefined); setSecretFormOpen(true); }} disabled={Boolean(secretCreateHint)}><Plus className="h-4 w-4" />创建 Secret</Button></ActionTooltip>
        ) : null}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索名称" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>
      <Tabs value={tab} onValueChange={(value) => updateSearch({ tab: value === "configmaps" ? undefined : value })}>
        <TabsList><TabsTrigger value="configmaps">ConfigMap ({configMaps.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="secrets">Secret ({secrets.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="certificate">服务端证书</TabsTrigger></TabsList>
        <TabsContent value="configmaps">
          <DataTable
            columns={[
              configMapColumn.accessor("name", { header: "名称", cell: (info) => <button type="button" className="font-medium text-primary hover:underline" onClick={() => setConfigMap(info.row.original)}>{info.getValue()}</button> }),
              configMapColumn.accessor("namespace", { header: "命名空间" }),
              configMapColumn.accessor("dataBytes", { header: "大小", cell: (info) => <span className="tabular-nums">{info.getValue()} B</span> }),
              configMapColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              configMapColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`ConfigMap ${info.row.original.namespace}/${info.row.original.name}`} actions={configMapActions(info.row.original)} /> }),
            ]}
            data={configMaps.data?.items ?? []}
            loading={configMaps.isLoading}
            empty={<div className="flex flex-col items-center gap-2"><Settings2 className="h-6 w-6" /><span>没有 ConfigMap</span></div>}
            onRowClick={(row) => setConfigMap(row)}
          />
        </TabsContent>
        <TabsContent value="secrets">
          <DataTable
            columns={[
              secretColumn.accessor("name", { header: "名称", cell: (info) => <span className="font-medium">{info.getValue()}</span> }),
              secretColumn.accessor("namespace", { header: "命名空间" }),
              secretColumn.accessor("type", { header: "类型", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
              secretColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              secretColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`Secret ${info.row.original.namespace}/${info.row.original.name}`} actions={secretActions(info.row.original)} /> }),
            ]}
            data={secrets.data?.items ?? []}
            loading={secrets.isLoading}
            empty={<div className="flex flex-col items-center gap-2"><LockKeyhole className="h-6 w-6" /><span>没有 Secret</span></div>}
          />
        </TabsContent>
        <TabsContent value="certificate"><ServerCertificatePanel /></TabsContent>
      </Tabs>

      <ConfigMapDialog item={configMap} onClose={() => setConfigMap(null)} />
      <ConfigMapFormDialog open={configMapFormOpen} onOpenChange={(open) => { setConfigMapFormOpen(open); if (!open) setEditingConfigMap(undefined); }} namespace={editingConfigMap?.namespace || namespace} item={editingConfigMap} onSaved={refreshConfigMaps} />
      <SecretFormDialog open={secretFormOpen} onOpenChange={(open) => { setSecretFormOpen(open); if (!open) setEditingSecret(undefined); }} namespace={editingSecret?.namespace || namespace} item={editingSecret} onSaved={refreshSecrets} />
      <DeleteResourceDialog open={Boolean(deletingConfigMap)} onOpenChange={(open) => { if (!open) setDeletingConfigMap(undefined); }} resourceKind="ConfigMap" name={deletingConfigMap?.name ?? ""} description="引用该 ConfigMap 的工作负载可能无法重新加载配置。" onConfirm={async () => { if (!deletingConfigMap) return; await api.deleteConfigMap(deletingConfigMap.namespace, deletingConfigMap.name, deletingConfigMap.resourceVersion); await refreshConfigMaps(); }} />
      <DeleteResourceDialog open={Boolean(deletingSecret)} onOpenChange={(open) => { if (!open) setDeletingSecret(undefined); }} resourceKind="Secret" name={deletingSecret?.name ?? ""} description="引用该 Secret 的工作负载可能无法拉取镜像、挂载证书或读取凭据。" onConfirm={async () => { if (!deletingSecret) return; await api.deleteSecret(deletingSecret.namespace, deletingSecret.name, deletingSecret.resourceVersion); await refreshSecrets(); }} />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type={yamlTarget?.type ?? "configmap"} name={yamlTarget?.name ?? ""} namespace={yamlTarget?.namespace} />

      <Dialog open={Boolean(revealResult)} onOpenChange={(open) => { if (!open) setRevealResult(null); }}>
        <DialogContent className="max-w-3xl">
          <DialogHeader><DialogTitle>{revealResult?.name}</DialogTitle><DialogDescription>{revealResult?.namespace} · {revealResult?.type} · 可解码文本按明文显示，二进制保留 Base64</DialogDescription></DialogHeader>
          <div className="max-h-[65vh] min-w-0 max-w-full space-y-3 overflow-auto pr-1 scrollbar-thin">{revealResult ? Object.entries(revealResult.data ?? {}).map(([key, value]) => <SecretValueBlock key={key} name={key} encoded={value} />) : null}</div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function ConfigMapDialog({ item, onClose }: { item: ConfigMapSummary | null; onClose: () => void }) {
  const query = useQuery({ queryKey: ["configmap", item?.namespace, item?.name], queryFn: () => api.configmap(item!.namespace, item!.name), enabled: Boolean(item) });
  return (
    <Dialog open={Boolean(item)} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="max-w-4xl">
        <DialogHeader><DialogTitle>{item?.name}</DialogTitle><DialogDescription>{item?.namespace} · ConfigMap</DialogDescription></DialogHeader>
        {query.isLoading ? <LoadingState /> : query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : query.data ? <Tabs defaultValue="data"><TabsList><TabsTrigger value="data">数据</TabsTrigger><TabsTrigger value="detail">详情</TabsTrigger></TabsList><TabsContent value="data"><div className="max-h-[60vh] min-w-0 max-w-full space-y-3 overflow-auto pr-1 scrollbar-thin">{Object.entries(query.data.data ?? {}).length ? Object.entries(query.data.data ?? {}).map(([key, value]) => <div key={key} className="min-w-0 rounded-md border"><div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2"><span className="min-w-0 break-words font-mono text-xs font-medium">{key}</span><CopyButton value={value} /></div><pre className="soft-wrap max-w-full overflow-auto whitespace-pre-wrap p-3 font-mono text-xs">{value}</pre></div>) : <p className="py-8 text-center text-sm text-muted-foreground">ConfigMap 没有文本数据</p>}</div></TabsContent><TabsContent value="detail"><Card><CardHeader><CardTitle>基本信息</CardTitle></CardHeader><CardContent><dl className="grid gap-5 sm:grid-cols-2"><DetailItem label="名称" value={query.data.name} mono /><DetailItem label="命名空间" value={query.data.namespace} /><DetailItem label="数据大小" value={`${query.data.dataBytes} B`} /><DetailItem label="创建时间" value={formatAge(query.data.createdAt)} /></dl></CardContent></Card></TabsContent></Tabs> : null}
      </DialogContent>
    </Dialog>
  );
}
