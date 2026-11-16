import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { ArrowRightLeft, Braces, FileCode2, History, Loader2, Package, RotateCcw, ShieldAlert, Upload } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { HelmReleaseSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { GenericYamlPanel } from "@/features/resources/generic-yaml-panel";
import { useCapabilities } from "@/features/auth/use-session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { formatDate } from "@/lib/utils";

const historyColumn = createColumnHelper<HelmReleaseSummary>();
const helmTabs = ["overview", "values", "manifest", "history", "diff", "upgrade"] as const;
type HelmTab = (typeof helmTabs)[number];

function isHelmTab(value: string | undefined): value is HelmTab {
  return helmTabs.includes(value as HelmTab);
}

function DiffPanel({ title, diff }: { title: string; diff: string }) {
  return (
    <Card className="min-w-0">
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="min-w-0">
        {!diff ? <p className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">两个版本在该内容上一致。</p> : (
          <pre className="max-h-[54vh] min-h-48 overflow-auto whitespace-pre p-3 font-mono text-[11px] leading-5 scrollbar-thin">
            {diff.split("\n").map((line, index) => (
              <span key={index} className={line.startsWith("+") && !line.startsWith("+++") ? "block bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" : line.startsWith("-") && !line.startsWith("---") ? "block bg-red-500/10 text-red-700 dark:text-red-300" : line.startsWith("@@") ? "block bg-blue-500/10 text-blue-700 dark:text-blue-300" : "block"}>{line || " "}</span>
            ))}
          </pre>
        )}
      </CardContent>
    </Card>
  );
}

export function HelmReleaseDetailPage() {
  const params = useParams({ strict: false }) as { namespace: string; name: string };
  const search = useSearch({ from: "/app/helm/releases/$namespace/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const capabilities = useCapabilities(params.namespace).data;
  const [fromRevision, setFromRevision] = useState(0);
  const [toRevision, setToRevision] = useState(0);
  const [diffRequest, setDiffRequest] = useState<{ from: number; to: number } | null>(null);
  const [rollbackTarget, setRollbackTarget] = useState<HelmReleaseSummary | null>(null);
  const [confirmation, setConfirmation] = useState("");
  const [upgradeValues, setUpgradeValues] = useState("");
  const [upgradeConfirmation, setUpgradeConfirmation] = useState("");
  const [upgradeOpen, setUpgradeOpen] = useState(false);
  const tab = isHelmTab(search.tab) ? search.tab : "overview";
  const changeTab = (value: string) => {
    if (!isHelmTab(value)) return;
    void navigate({ to: "/helm/releases/$namespace/$name", params, search: { ...search, tab: value === "overview" ? undefined : value }, replace: true });
  };
  const query = useQuery({
    queryKey: ["helm-release", params.namespace, params.name],
    queryFn: () => api.helmRelease(params.namespace, params.name),
    enabled: Boolean(capabilities?.helmReleases),
  });
  const diff = useQuery({
    queryKey: ["helm-release-diff", params.namespace, params.name, diffRequest?.from, diffRequest?.to],
    queryFn: () => api.helmReleaseDiff(params.namespace, params.name, diffRequest!.from, diffRequest!.to),
    enabled: Boolean(capabilities?.helmReleases && diffRequest),
  });
  const rollback = useMutation({
    mutationFn: () => api.rollbackHelmRelease(params.namespace, params.name, { targetRevision: rollbackTarget!.revision, confirmation }),
    onSuccess: async (result) => {
      toast.success(`已创建 Helm revision #${result.revision}，应用 ${result.appliedObjects} 个对象`);
      if (result.warnings?.length) toast.warning(result.warnings.join("；"));
      setRollbackTarget(null);
      setConfirmation("");
      setDiffRequest(null);
      await query.refetch();
      await queryClient.invalidateQueries({ queryKey: ["helm-releases"] });
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      await queryClient.invalidateQueries({ queryKey: ["pods"] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 回滚失败"),
  });
  const previewUpgrade = useMutation({
    mutationFn: () => api.previewHelmReleaseUpgrade(params.namespace, params.name, { values: upgradeValues }),
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 升级预览失败"),
  });
  const upgrade = useMutation({
    mutationFn: () => api.upgradeHelmRelease(params.namespace, params.name, { values: upgradeValues, confirmation: upgradeConfirmation }),
    onSuccess: async (result) => {
      toast.success(`已创建 Helm revision #${result.revision}，应用 ${result.appliedObjects} 个对象`);
      if (result.warnings?.length) toast.warning(result.warnings.join("；"));
      setUpgradeOpen(false);
      setUpgradeConfirmation("");
      setUpgradeValues("");
      previewUpgrade.reset();
      setDiffRequest(null);
      await query.refetch();
      await queryClient.invalidateQueries({ queryKey: ["helm-releases"] });
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      await queryClient.invalidateQueries({ queryKey: ["pods"] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 升级失败"),
  });
  if (!capabilities) return <LoadingState label="正在检查 Helm 读取能力" />;
  if (!capabilities.helmReleases) return <ErrorState error={new Error("当前身份没有 list secrets 权限，无法读取 Helm Release。")} />;
  if (query.isLoading) return <LoadingState label="正在加载 Helm Release" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const release = query.data!;
  const secretCaps = capabilities.resourceActions?.secrets;
  const canRollback = Boolean(secretCaps?.update && secretCaps?.create);
  const canUpgrade = Boolean(capabilities.helmUpgrade && release.status === "deployed");
  const defaultFrom = release.history.find((item) => item.revision < release.revision)?.revision ?? release.revision;
  const selectedFrom = fromRevision || defaultFrom;
  const selectedTo = toRevision || release.revision;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/helm/releases" search={{ namespace: release.namespace }} className="text-primary underline underline-offset-2">Helm Releases</Link> / {release.namespace} / {release.name}</>}
        title={<span className="flex min-w-0 items-center gap-2"><Package className="h-5 w-5 shrink-0 text-muted-foreground" /><span className="truncate">{release.name}</span></span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={release.status} /><Badge variant="secondary" className="font-mono">revision #{release.revision}</Badge><span>{release.chart} {release.chartVersion}</span></span>}
        actions={<div className="flex flex-wrap gap-2">{canUpgrade ? <Button variant="outline" size="sm" onClick={() => changeTab("upgrade")}><Upload className="h-4 w-4" />升级</Button> : null}{canRollback ? <Button variant="outline" size="sm" disabled={!release.history.some((item) => item.revision < release.revision)} onClick={() => { setConfirmation(""); setRollbackTarget(release.history.find((item) => item.revision < release.revision) ?? null); }}><RotateCcw className="h-4 w-4" />回滚</Button> : null}</div>}
      />
      {release.warnings?.length ? <div role="status" className="mb-3 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><span className="min-w-0 break-words">{release.warnings.join("；")}</span></div> : null}
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="values"><Braces className="h-3.5 w-3.5" />Values</TabsTrigger><TabsTrigger value="manifest"><FileCode2 className="h-3.5 w-3.5" />Manifest</TabsTrigger><TabsTrigger value="history"><History className="h-3.5 w-3.5" />历史 ({release.history.length})</TabsTrigger><TabsTrigger value="diff"><ArrowRightLeft className="h-3.5 w-3.5" />Diff</TabsTrigger><TabsTrigger value="upgrade"><Upload className="h-3.5 w-3.5" />升级</TabsTrigger></TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-2">
            <Card><CardHeader><CardTitle>Release 信息</CardTitle></CardHeader><CardContent><dl className="grid gap-5 sm:grid-cols-2">
              <DetailItem label="名称" value={release.name} mono />
              <DetailItem label="命名空间" value={release.namespace} />
              <DetailItem label="状态" value={<StatusBadge status={release.status} />} />
              <DetailItem label="Revision" value={`#${release.revision}`} mono />
              <DetailItem label="Chart" value={release.chart} mono />
              <DetailItem label="Chart 版本" value={release.chartVersion} mono />
              <DetailItem label="App 版本" value={release.appVersion || "—"} mono />
              <DetailItem label="首次部署" value={formatDate(release.firstDeployed)} />
              <DetailItem label="最近部署" value={formatDate(release.lastDeployed)} />
              <DetailItem label="更新时间" value={formatDate(release.updatedAt)} />
            </dl></CardContent></Card>
            <Card><CardHeader><CardTitle>部署说明</CardTitle></CardHeader><CardContent>
              <p className="whitespace-pre-wrap break-words text-sm leading-6 text-muted-foreground">{release.description || "该 Release 没有部署说明。"}</p>
              {release.notes ? <div className="mt-4 min-w-0 rounded-md border bg-muted/20 p-3"><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Notes</p><pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-5">{release.notes}</pre></div> : null}
              <p className="mt-4 rounded-md border bg-muted/25 px-3 py-2 text-xs leading-5 text-muted-foreground">Helm 数据来自 Secret 存储。values 中的常见敏感键和 manifest 中的所有 Secret data/stringData 已在服务端遮蔽。</p>
            </CardContent></Card>
          </div>
        </TabsContent>
        <TabsContent value="values"><GenericYamlPanel yaml={release.values} height="62vh" /></TabsContent>
        <TabsContent value="manifest"><GenericYamlPanel yaml={release.manifest} height="62vh" /></TabsContent>
        <TabsContent value="diff">
          <Card className="mb-4 min-w-0"><CardHeader><CardTitle>版本对比</CardTitle></CardHeader><CardContent className="space-y-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-end">
              <div className="min-w-0 sm:w-44"><Label className="mb-1.5 block text-xs">基准版本</Label><Select value={String(selectedFrom)} onValueChange={(value) => setFromRevision(Number(value))}><SelectTrigger className="w-full" aria-label="基准版本"><SelectValue /></SelectTrigger><SelectContent>{release.history.map((item) => <SelectItem key={item.revision} value={String(item.revision)}>revision #{item.revision} · {item.status}</SelectItem>)}</SelectContent></Select></div>
              <ArrowRightLeft className="hidden h-4 w-4 self-center text-muted-foreground sm:block" />
              <div className="min-w-0 sm:w-44"><Label className="mb-1.5 block text-xs">目标版本</Label><Select value={String(selectedTo)} onValueChange={(value) => setToRevision(Number(value))}><SelectTrigger className="w-full" aria-label="目标版本"><SelectValue /></SelectTrigger><SelectContent>{release.history.map((item) => <SelectItem key={item.revision} value={String(item.revision)}>revision #{item.revision} · {item.status}</SelectItem>)}</SelectContent></Select></div>
              <Button className="sm:ml-auto" disabled={selectedFrom === selectedTo || diff.isFetching} onClick={() => setDiffRequest({ from: selectedFrom, to: selectedTo })}>{diff.isFetching ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowRightLeft className="h-4 w-4" />}生成 Diff</Button>
            </div>
            <p className="text-xs leading-5 text-muted-foreground">Diff 在服务端读取两个 revision 并先做敏感字段遮蔽；最多比较 6000 行。此处不会修改集群。</p>
          </CardContent></Card>
          {diff.isError ? <ErrorState error={diff.error} onRetry={() => void diff.refetch()} /> : null}
          {diff.data?.warnings?.length ? <div role="status" className="mb-4 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{diff.data.warnings.join("；")}</span></div> : null}
          {diff.isLoading ? <LoadingState label="正在生成 Helm Diff" /> : diff.data ? <div className="grid min-w-0 gap-4 xl:grid-cols-2"><DiffPanel title="Values Diff" diff={diff.data.valuesDiff} /><DiffPanel title="Manifest Diff" diff={diff.data.manifestDiff} /></div> : <EmptyState title="选择两个版本" description="选择基准版本和目标版本后生成服务端 Diff。" />}
        </TabsContent>
        <TabsContent value="upgrade">
          <Card className="mb-4 min-w-0">
            <CardHeader><CardTitle>Values Override</CardTitle></CardHeader>
            <CardContent className="space-y-4">
              <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">此输入框是增量 Override，不会预填遮蔽后的当前 Values。留空表示沿用当前服务端 Values；核对预览后再执行升级。</div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-upgrade-values">升级 Values（YAML）</Label>
                <Textarea id="helm-upgrade-values" value={upgradeValues} onChange={(event) => { setUpgradeValues(event.target.value); previewUpgrade.reset(); }} rows={12} spellCheck={false} placeholder={"replicaCount: 3\nimage:\n  tag: \"1.2.4\"\n"} className="min-h-56 w-full min-w-0 font-mono text-xs leading-5" />
                <p className="text-xs leading-5 text-muted-foreground">输入会与服务端保存的当前原始 Values 深度合并。敏感键仍会在预览中遮蔽，但真实值会写入新的 Helm revision Secret。</p>
              </div>
              <div className="flex min-w-0 flex-wrap gap-2">
                <Button type="button" variant="outline" disabled={!canUpgrade || previewUpgrade.isPending} onClick={() => previewUpgrade.mutate()}>{previewUpgrade.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowRightLeft className="h-4 w-4" />}生成升级预览</Button>
                <Button type="button" disabled={!canUpgrade || !previewUpgrade.data || previewUpgrade.isPending} onClick={() => { setUpgradeConfirmation(""); setUpgradeOpen(true); }}><Upload className="h-4 w-4" />执行升级</Button>
              </div>
              {!canUpgrade ? <p role="note" className="text-sm text-muted-foreground">只有状态为 deployed 且具备 Secrets create/update 权限时才能执行升级。</p> : null}
              {previewUpgrade.isError ? <p role="alert" className="break-words text-sm text-destructive-strong">{previewUpgrade.error instanceof Error ? previewUpgrade.error.message : "升级预览失败"}</p> : null}
            </CardContent>
          </Card>
          {previewUpgrade.data?.warnings?.length ? <div role="status" className="mb-4 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{previewUpgrade.data.warnings.join("；")}</span></div> : null}
          {previewUpgrade.data ? <div className="grid min-w-0 gap-4 xl:grid-cols-2"><DiffPanel title="升级后 Values Diff" diff={previewUpgrade.data.valuesDiff} /><DiffPanel title="升级后 Manifest Diff" diff={previewUpgrade.data.manifestDiff} /></div> : <EmptyState title="尚未生成升级预览" description="填写增量 Values 后生成服务端 Diff；预览不会修改集群。" />}
        </TabsContent>
        <TabsContent value="history">
          <DataTable
            columns={[
              historyColumn.accessor("revision", { header: "Revision", cell: (info) => <span className="font-mono text-xs font-semibold">#{info.getValue()}</span> }),
              historyColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              historyColumn.accessor("chart", { header: "Chart", cell: (info) => <span className="break-all font-mono text-xs">{info.getValue()} {info.row.original.chartVersion}</span> }),
              historyColumn.accessor("description", { header: "说明", cell: (info) => <span className="block max-w-64 truncate text-xs text-muted-foreground" title={info.getValue()}>{info.getValue() || "—"}</span> }),
              historyColumn.accessor("updatedAt", { header: "更新时间", cell: (info) => <span className="whitespace-nowrap text-xs text-muted-foreground">{formatDate(info.getValue())}</span> }),
              historyColumn.display({ id: "actions", header: "操作", cell: (info) => info.row.original.revision === release.revision ? <Badge variant="secondary">当前版本</Badge> : canRollback ? <Button variant="outline" size="sm" className="h-7" onClick={() => { setConfirmation(""); setRollbackTarget(info.row.original); }}><RotateCcw className="h-3.5 w-3.5" />回滚到此版本</Button> : <span className="text-xs text-muted-foreground">无权限</span> }),
            ]}
            data={release.history}
            stickyFirstColumn={false}
            empty={<EmptyState title="没有历史版本" />}
          />
        </TabsContent>
      </Tabs>
      <Dialog open={Boolean(rollbackTarget)} onOpenChange={(open) => { if (!open && !rollback.isPending) { setRollbackTarget(null); setConfirmation(""); } }}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><RotateCcw className="h-5 w-5" />受控回滚 Helm Release</DialogTitle>
            <DialogDescription>将 revision #{rollbackTarget?.revision ?? "—"} 的渲染清单应用到集群，并创建新的 Helm revision #{release.revision + 1}。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">回滚会覆盖目标清单中的同名对象。Helm Hook 不会执行；当前存在但目标清单中不存在的对象不会被删除。若清单对象权限不足，预检 Dry Run 会在任何写入前失败。</div>
            <div><Label htmlFor="helm-rollback-confirmation">输入 Release 名称 <span className="font-mono">{release.name}</span> 以确认</Label><Input id="helm-rollback-confirmation" className="mt-1.5" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" /></div>
            {rollback.isError ? <p role="alert" className="text-sm text-destructive">{rollback.error instanceof Error ? rollback.error.message : "回滚失败"}</p> : null}
          </div>
          <DialogFooter>
            <Button variant="outline" disabled={rollback.isPending} onClick={() => { setRollbackTarget(null); setConfirmation(""); }}>取消</Button>
            <Button variant="destructive" disabled={!rollbackTarget || confirmation !== release.name || rollback.isPending} onClick={() => rollback.mutate()}>{rollback.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}确认回滚</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={upgradeOpen} onOpenChange={(open) => { if (!open && !upgrade.isPending) setUpgradeOpen(false); }}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><Upload className="h-5 w-5" />执行 Helm 升级</DialogTitle>
            <DialogDescription>将在 revision #{release.revision} 的 Chart 与原始 Values 基础上合并 Override，渲染并应用新的 revision #{release.revision + 1}。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">开始写入前会先对所有对象执行 Dry Run。Helm Hook 不会执行；新清单中不存在的旧对象不会被删除。任何预检失败都会阻止后续写入。</div>
            <div><Label htmlFor="helm-upgrade-confirmation">输入 Release 名称 <span className="font-mono">{release.name}</span> 以确认</Label><Input id="helm-upgrade-confirmation" className="mt-1.5" value={upgradeConfirmation} onChange={(event) => setUpgradeConfirmation(event.target.value)} autoComplete="off" /></div>
            {upgrade.isError ? <p role="alert" className="break-words text-sm text-destructive-strong">{upgrade.error instanceof Error ? upgrade.error.message : "升级失败"}</p> : null}
          </div>
          <DialogFooter>
            <Button variant="outline" disabled={upgrade.isPending} onClick={() => setUpgradeOpen(false)}>取消</Button>
            <Button disabled={upgradeConfirmation !== release.name || upgrade.isPending} onClick={() => upgrade.mutate()}>{upgrade.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}确认升级</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
