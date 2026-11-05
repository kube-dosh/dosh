import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Eye, FileCode2, KeyRound, Loader2, LockKeyhole, Settings2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { ConfigMapSummary, SecretReveal, SecretSummary } from "@/api/types";
import { CopyButton } from "@/components/data/copy-button";
import { DataTable } from "@/components/data/data-table";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { DetailItem } from "@/components/data/detail-item";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCapabilities } from "@/features/auth/use-session";
import { ServerCertificatePanel } from "@/features/configuration/server-certificate-panel";
import { decodeSecretValue } from "@/lib/secret-value";
import { formatAge } from "@/lib/utils";

const configMapColumn = createColumnHelper<ConfigMapSummary>();
const secretColumn = createColumnHelper<SecretSummary>();

function SecretRevealAction({ secret, onReveal }: { secret: SecretSummary; onReveal: (secret: SecretSummary) => void }) {
  const capabilities = useCapabilities(secret.namespace).data;
  if (!capabilities?.revealSecrets) return <span className="text-xs text-muted-foreground">无权限</span>;
  return <Button variant="outline" size="sm" onClick={() => onReveal(secret)}><Eye className="h-3.5 w-3.5" />查看明文</Button>;
}

function SecretValueBlock({ name, encoded }: { name: string; encoded: string }) {
  const decoded = decodeSecretValue(encoded);
  const label = decoded.kind === "text" ? "文本" : decoded.kind === "binary" ? "Base64（二进制）" : "无法解码";
  return (
    <div className="rounded-md border">
      <div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2">
        <div className="flex min-w-0 items-center gap-2"><span className="truncate font-mono text-xs font-medium">{name}</span><Badge variant="outline">{label}</Badge></div>
        <CopyButton value={decoded.copyValue} label={decoded.kind === "text" ? "复制明文" : "复制原值"} />
      </div>
      {decoded.kind === "binary" ? <p className="border-b bg-muted/20 px-3 py-2 text-xs text-muted-foreground">该值不是 UTF-8 文本，按原始 Base64 显示和复制。</p> : null}
      {decoded.kind === "invalid" ? <p className="border-b bg-warning/10 px-3 py-2 text-xs text-warning">该值不是有效 Base64，按原始响应显示和复制。</p> : null}
      <pre className="overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-xs" aria-label={`${name} 值`}>{decoded.display || "（空值）"}</pre>
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
  const [yamlTarget, setYamlTarget] = useState<ConfigMapSummary | null>(null);
  const [secret, setSecret] = useState<SecretSummary | null>(null);
  const [revealResult, setRevealResult] = useState<SecretReveal | null>(null);
  const [confirmName, setConfirmName] = useState("");
  const configMaps = useQuery({ queryKey: ["configmaps", namespace, q], queryFn: () => api.configmaps({ namespace, q }), enabled: tab !== "certificate" });
  const secrets = useQuery({ queryKey: ["secrets", namespace, q], queryFn: () => api.secrets({ namespace, q }), enabled: tab !== "certificate" });
  const reveal = useMutation({
    mutationFn: (target: SecretSummary) => api.revealSecret(target.namespace, target.name),
    onSuccess: (data) => { setRevealResult(data); setSecret(null); setConfirmName(""); },
    onError: (error) => toast.error(error.message),
  });
  const error = tab === "certificate" ? null : configMaps.error || secrets.error;
  if (error) return <ErrorState error={error} onRetry={() => { void configMaps.refetch(); void secrets.refetch(); }} />;
  return (
    <>
      <PageHeader title="配置与密钥" breadcrumb="配置 / ConfigMap、Secret 与服务端证书" description="查看配置项、密钥元数据和服务端 TLS 证书状态。Secret 明文查看与服务端证书重载均需要授权并记录审计。" />
      {tab === "certificate" ? null : <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索名称或键" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>}
      <Tabs value={tab} onValueChange={(value) => updateSearch({ tab: value === "configmaps" ? undefined : value })}>
        <TabsList><TabsTrigger value="configmaps">ConfigMap ({configMaps.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="secrets">Secret ({secrets.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="certificate">服务端证书</TabsTrigger></TabsList>
        <TabsContent value="configmaps">
          <DataTable
            columns={[
              configMapColumn.accessor("name", { header: "名称", cell: (info) => <button type="button" onClick={() => setConfigMap(info.row.original)} className="font-medium text-primary hover:underline">{info.getValue()}</button> }),
              configMapColumn.accessor("namespace", { header: "命名空间" }),
              configMapColumn.accessor("keys", { header: "键", enableSorting: false, cell: (info) => <div className="flex max-w-md flex-wrap gap-1">{info.getValue().slice(0, 5).map((key: string) => <span key={key} className="rounded border bg-muted/30 px-1.5 py-0.5 font-mono text-[11px]">{key}</span>)}{info.getValue().length > 5 ? <span className="text-xs text-muted-foreground">+{info.getValue().length - 5}</span> : null}</div> }),
              configMapColumn.accessor("dataBytes", { header: "大小", cell: (info) => <span className="tabular-nums">{info.getValue()} B</span> }),
              configMapColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              configMapColumn.display({ id: "actions", header: "", cell: (info) => <Button variant="ghost" size="icon-sm" aria-label="查看 YAML" onClick={() => setYamlTarget(info.row.original)}><FileCode2 className="h-4 w-4" /></Button> }),
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
              secretColumn.accessor("keys", { header: "键 / 大小", enableSorting: false, cell: (info) => <div className="flex max-w-md flex-wrap gap-1">{info.getValue().map((key: { key: string; size: number }) => <span key={key.key} className="rounded border bg-muted/30 px-1.5 py-0.5 font-mono text-[11px]">{key.key} · {key.size} B</span>)}</div> }),
              secretColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              secretColumn.display({ id: "reveal", header: "", cell: (info) => <SecretRevealAction secret={info.row.original} onReveal={(secret) => { setConfirmName(""); setSecret(secret); }} /> }),
            ]}
            data={secrets.data?.items ?? []}
            loading={secrets.isLoading}
            empty={<div className="flex flex-col items-center gap-2"><LockKeyhole className="h-6 w-6" /><span>没有 Secret</span></div>}
          />
        </TabsContent>
        <TabsContent value="certificate"><ServerCertificatePanel /></TabsContent>
      </Tabs>

      <ConfigMapDialog item={configMap} onClose={() => setConfigMap(null)} />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(null); }} type="configmap" name={yamlTarget?.name || ""} namespace={yamlTarget?.namespace} />

      <AlertDialog open={Boolean(secret)} onOpenChange={(open) => { if (!open) { setSecret(null); setConfirmName(""); } }}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle className="flex items-center gap-2"><KeyRound className="h-5 w-5 text-warning" />查看 Secret 明文？</AlertDialogTitle><AlertDialogDescription>此操作会读取敏感数据并写入审计日志。请确认当前环境安全，并输入 Secret 名称完成二次确认。</AlertDialogDescription></AlertDialogHeader>
          <div className="space-y-2"><Label htmlFor="secret-confirm">Secret 名称</Label><Input id="secret-confirm" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} placeholder={secret?.name} autoComplete="off" /></div>
          <AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction onClick={(event) => { event.preventDefault(); if (secret) reveal.mutate(secret); }} disabled={!secret || confirmName !== secret.name || reveal.isPending}>{reveal.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Eye className="h-4 w-4" />}确认查看</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={Boolean(revealResult)} onOpenChange={(open) => { if (!open) setRevealResult(null); }}>
        <DialogContent className="max-w-3xl">
          <DialogHeader><DialogTitle>{revealResult?.name}</DialogTitle><DialogDescription>{revealResult?.namespace} · {revealResult?.type} · 敏感数据仅显示于当前弹窗；可解码文本按明文显示，二进制保留 Base64</DialogDescription></DialogHeader>
          <div className="max-h-[65vh] space-y-3 overflow-auto pr-1 scrollbar-thin">{revealResult ? Object.entries(revealResult.data).map(([key, value]) => <SecretValueBlock key={key} name={key} encoded={value} />) : null}</div>
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
        {query.isLoading ? <LoadingState /> : query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : query.data ? <Tabs defaultValue="data"><TabsList><TabsTrigger value="data">数据</TabsTrigger><TabsTrigger value="detail">详情</TabsTrigger></TabsList><TabsContent value="data"><div className="max-h-[60vh] space-y-3 overflow-auto pr-1 scrollbar-thin">{Object.entries(query.data.data).length ? Object.entries(query.data.data).map(([key, value]) => <div key={key} className="rounded-md border"><div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2"><span className="font-mono text-xs font-medium">{key}</span><CopyButton value={value} /></div><pre className="overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-xs">{value}</pre></div>) : <p className="py-8 text-center text-sm text-muted-foreground">ConfigMap 没有文本数据</p>}</div></TabsContent><TabsContent value="detail"><Card><CardHeader><CardTitle>基本信息</CardTitle></CardHeader><CardContent><dl className="grid gap-5 sm:grid-cols-2"><DetailItem label="名称" value={query.data.name} mono /><DetailItem label="命名空间" value={query.data.namespace} /><DetailItem label="数据大小" value={`${query.data.dataBytes} B`} /><DetailItem label="创建时间" value={formatAge(query.data.createdAt)} /></dl></CardContent></Card></TabsContent></Tabs> : null}
      </DialogContent>
    </Dialog>
  );
}
