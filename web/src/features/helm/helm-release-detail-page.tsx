import { lazy, Suspense, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { ArrowRightLeft, Braces, CheckCircle2, FileCode2, History, Loader2, Package, PackageSearch, Play, RotateCcw, ShieldAlert, Trash2, Upload, XCircle } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type {
  HelmExecutionOptionsInput,
  HelmReleaseSummary,
  HelmReleaseTestResult,
} from "@/api/types";
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
import {
  defaultHelmExecutionOptions,
  HelmExecutionOptionsFields,
} from "@/features/helm/helm-execution-options";
import { formatDate } from "@/lib/utils";

const HelmChartVersionUpgradeDialog = lazy(() =>
  import("@/features/helm/helm-chart-version-upgrade-dialog").then((module) => ({
    default: module.HelmChartVersionUpgradeDialog,
  })),
);

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
  const [rollbackOptions, setRollbackOptions] = useState<HelmExecutionOptionsInput>({ ...defaultHelmExecutionOptions });
  const [upgradeValues, setUpgradeValues] = useState("");
  const [upgradeOptions, setUpgradeOptions] = useState<HelmExecutionOptionsInput>({ ...defaultHelmExecutionOptions });
  const [upgradeConfirmation, setUpgradeConfirmation] = useState("");
  const [upgradeOpen, setUpgradeOpen] = useState(false);
  const [chartUpgradeOpen, setChartUpgradeOpen] = useState(false);
  const [uninstallOpen, setUninstallOpen] = useState(false);
  const [uninstallConfirmation, setUninstallConfirmation] = useState("");
  const [uninstallOptions, setUninstallOptions] = useState<HelmExecutionOptionsInput>({ ...defaultHelmExecutionOptions });
  const [testOpen, setTestOpen] = useState(false);
  const [testResult, setTestResult] = useState<HelmReleaseTestResult>();
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
    mutationFn: () => api.rollbackHelmChart({ namespace: params.namespace, releaseName: params.name, targetRevision: rollbackTarget!.revision, confirmation, options: rollbackOptions }),
    onSuccess: async (operation) => {
      toast.success(`已提交 ${operation.releaseName} 的回滚操作`);
      setRollbackTarget(null);
      setConfirmation("");
      setRollbackOptions({ ...defaultHelmExecutionOptions });
      setDiffRequest(null);
      await queryClient.invalidateQueries({ queryKey: ["helm-releases"] });
      void navigate({ to: "/helm/operations", search: { q: operation.releaseName } });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 回滚失败"),
  });
  const previewUpgrade = useMutation({
    mutationFn: () => api.previewHelmReleaseUpgrade(params.namespace, params.name, { values: upgradeValues }),
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 升级预览失败"),
  });
  const upgrade = useMutation({
    mutationFn: () => api.upgradeHelmRelease(params.namespace, params.name, { values: upgradeValues, confirmation: upgradeConfirmation, currentRevision: release.revision, options: upgradeOptions }),
    onSuccess: async (operation) => {
      toast.success(`已提交 ${operation.releaseName} 的升级操作`);
      setUpgradeOpen(false);
      setUpgradeConfirmation("");
      setUpgradeValues("");
      setUpgradeOptions({ ...defaultHelmExecutionOptions });
      previewUpgrade.reset();
      setDiffRequest(null);
      await queryClient.invalidateQueries({ queryKey: ["helm-releases"] });
      void navigate({ to: "/helm/operations", search: { q: operation.releaseName } });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 升级失败"),
  });
  const uninstall = useMutation({
    mutationFn: () => api.uninstallHelmChart({ namespace: params.namespace, releaseName: params.name, confirmation: uninstallConfirmation, options: uninstallOptions }),
    onSuccess: async (operation) => {
      toast.success(`已提交 ${operation.releaseName} 的卸载操作`);
      setUninstallOpen(false);
      setUninstallConfirmation("");
      setUninstallOptions({ ...defaultHelmExecutionOptions });
      await queryClient.invalidateQueries({ queryKey: ["helm-releases"] });
      void navigate({ to: "/helm/operations", search: { q: operation.releaseName } });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm 卸载失败"),
  });
  const helmTest = useMutation({
    mutationFn: () => api.testHelmRelease(params.namespace, params.name),
    onSuccess: (result) => {
      setTestResult(result);
      setTestOpen(true);
      if (result.status === "failed") toast.error(result.message || "Helm test 未通过");
      else toast.success(result.message || "Helm test 已通过");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Helm test 执行失败"),
  });
  if (!capabilities) return <LoadingState label="正在检查 Helm 读取能力" />;
  if (!capabilities.helmReleases) return <ErrorState error={new Error("当前身份没有 list secrets 权限，无法读取 Helm Release。")} />;
  if (query.isLoading) return <LoadingState label="正在加载 Helm Release" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const release = query.data!;
  const secretCaps = capabilities.resourceActions?.secrets;
  const canRollback = Boolean(secretCaps?.update && secretCaps?.create);
  const canUpgrade = Boolean(capabilities.helmUpgrade && ["deployed", "failed", "superseded"].includes(release.status));
  const canSwitchChart = Boolean(canUpgrade && capabilities.helmChartRepositories);
  const canUninstall = Boolean(secretCaps?.delete && secretCaps?.update);
  const canTest = Boolean(capabilities.helmUpgrade && release.status === "deployed");
  const defaultFrom = release.history.find((item) => item.revision < release.revision)?.revision ?? release.revision;
  const selectedFrom = fromRevision || defaultFrom;
  const selectedTo = toRevision || release.revision;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/helm/releases" search={{ namespace: release.namespace }} className="text-primary underline underline-offset-2">Helm Releases</Link> / {release.namespace} / {release.name}</>}
        title={<span className="flex min-w-0 items-center gap-2"><Package className="h-5 w-5 shrink-0 text-muted-foreground" /><span className="truncate">{release.name}</span></span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={release.status} /><Badge variant="secondary" className="font-mono">revision #{release.revision}</Badge><span>{release.chart} {release.chartVersion}</span></span>}
        actions={<div className="flex flex-wrap gap-2">{canSwitchChart ? <Button variant="outline" size="sm" onClick={() => setChartUpgradeOpen(true)}><PackageSearch className="h-4 w-4" />更换 Chart</Button> : null}{canUpgrade ? <Button variant="outline" size="sm" onClick={() => changeTab("upgrade")}><Upload className="h-4 w-4" />Values 升级</Button> : null}{canRollback ? <Button variant="outline" size="sm" disabled={!release.history.some((item) => item.revision < release.revision)} onClick={() => { setConfirmation(""); setRollbackOptions({ ...defaultHelmExecutionOptions }); setRollbackTarget(release.history.find((item) => item.revision < release.revision) ?? null); }}><RotateCcw className="h-4 w-4" />回滚</Button> : null}{canTest ? <Button variant="outline" size="sm" disabled={helmTest.isPending} onClick={() => helmTest.mutate()}>{helmTest.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}运行 test</Button> : null}{canUninstall ? <Button variant="destructive" size="sm" onClick={() => { setUninstallConfirmation(""); setUninstallOptions({ ...defaultHelmExecutionOptions }); setUninstallOpen(true); }}><Trash2 className="h-4 w-4" />卸载</Button> : null}</div>}
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
              {canSwitchChart ? <div className="flex min-w-0 flex-col gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-muted-foreground sm:flex-row sm:items-center"><span className="min-w-0 flex-1 break-words">需要更换 Chart 或 Chart 版本时，请使用受控版本升级计划。</span><Button type="button" variant="outline" size="sm" className="self-start sm:self-auto" onClick={() => setChartUpgradeOpen(true)}><PackageSearch className="h-4 w-4" />更换 Chart</Button></div> : null}
              <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">此输入框是增量 Override，不会预填遮蔽后的当前 Values。留空表示沿用当前服务端 Values；核对预览后再执行升级。</div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-upgrade-values">升级 Values（YAML）</Label>
                <Textarea id="helm-upgrade-values" value={upgradeValues} onChange={(event) => { setUpgradeValues(event.target.value); previewUpgrade.reset(); }} rows={12} spellCheck={false} placeholder={"replicaCount: 3\nimage:\n  tag: \"1.2.4\"\n"} className="min-h-56 w-full min-w-0 font-mono text-xs leading-5" />
                <p className="text-xs leading-5 text-muted-foreground">输入会与服务端保存的当前原始 Values 深度合并。敏感键仍会在预览中遮蔽，但真实值会写入新的 Helm revision Secret。</p>
              </div>
              <HelmExecutionOptionsFields
                idPrefix="helm-release-upgrade-options"
                mode="upgrade"
                value={upgradeOptions}
                onChange={setUpgradeOptions}
                disabled={upgrade.isPending}
              />
              <div className="flex min-w-0 flex-wrap gap-2">
                <Button type="button" variant="outline" disabled={!canUpgrade || previewUpgrade.isPending} onClick={() => previewUpgrade.mutate()}>{previewUpgrade.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowRightLeft className="h-4 w-4" />}生成升级预览</Button>
                <Button type="button" disabled={!canUpgrade || !previewUpgrade.data || previewUpgrade.isPending} onClick={() => { setUpgradeConfirmation(""); setUpgradeOpen(true); }}><Upload className="h-4 w-4" />执行升级</Button>
              </div>
              {!canUpgrade ? <p role="note" className="text-sm text-muted-foreground">只有状态为 deployed、failed 或 superseded 且具备 Secrets create/update 权限时才能执行升级。</p> : null}
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
            <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">回滚由 Helm 原生操作执行，会按目标 revision 的清单恢复该 Release 管理的资源，并创建新的 Helm revision。CRD、PVC 等 Release 清单外资源不会被自动清理。</div>
            <HelmExecutionOptionsFields
              idPrefix="helm-rollback-options"
              mode="rollback"
              value={rollbackOptions}
              onChange={setRollbackOptions}
              disabled={rollback.isPending}
            />
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
            <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning">执行时使用 Helm 原生升级：按 Hook 生命周期运行 Hook，清理目标清单中已移除的对象，并使用 Atomic / CleanupOnFail 控制失败回滚。请先核对预览内容。</div>
            <div><Label htmlFor="helm-upgrade-confirmation">输入 Release 名称 <span className="font-mono">{release.name}</span> 以确认</Label><Input id="helm-upgrade-confirmation" className="mt-1.5" value={upgradeConfirmation} onChange={(event) => setUpgradeConfirmation(event.target.value)} autoComplete="off" /></div>
            {upgrade.isError ? <p role="alert" className="break-words text-sm text-destructive-strong">{upgrade.error instanceof Error ? upgrade.error.message : "升级失败"}</p> : null}
          </div>
          <DialogFooter>
            <Button variant="outline" disabled={upgrade.isPending} onClick={() => setUpgradeOpen(false)}>取消</Button>
            <Button disabled={upgradeConfirmation !== release.name || upgrade.isPending} onClick={() => upgrade.mutate()}>{upgrade.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}确认升级</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {chartUpgradeOpen ? (
        <Suspense fallback={null}>
          <HelmChartVersionUpgradeDialog
            release={release}
            open={chartUpgradeOpen}
            onOpenChange={setChartUpgradeOpen}
          />
        </Suspense>
      ) : null}
      <Dialog open={uninstallOpen} onOpenChange={(open) => { if (!open && !uninstall.isPending) { setUninstallOpen(false); setUninstallConfirmation(""); } }}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-lg overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><Trash2 className="h-5 w-5 text-destructive" />卸载 Helm Release</DialogTitle>
            <DialogDescription className="break-words">卸载 {release.namespace}/{release.name} 会删除该 Release 管理的 Kubernetes 资源，并清除 Helm 历史记录。</DialogDescription>
          </DialogHeader>
          <div className="min-w-0 space-y-4">
            <div className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs leading-5 text-destructive-strong">
              Helm 会按当前 Release 清单删除对象。PVC、CRD、外部资源和带 `helm.sh/resource-policy: keep` 注解的资源可能保留，需要在卸载后单独核对。卸载不可通过操作中心回滚。
            </div>
            <HelmExecutionOptionsFields
              idPrefix="helm-uninstall-options"
              mode="uninstall"
              value={uninstallOptions}
              onChange={setUninstallOptions}
              disabled={uninstall.isPending}
            />
            <div className="min-w-0">
              <Label htmlFor="helm-uninstall-confirmation">输入 Release 名称 <span className="break-all font-mono">{release.name}</span> 以确认</Label>
              <Input id="helm-uninstall-confirmation" className="mt-1.5" value={uninstallConfirmation} onChange={(event) => setUninstallConfirmation(event.target.value)} autoComplete="off" spellCheck={false} />
            </div>
            {uninstall.isError ? <p role="alert" className="break-words text-sm text-destructive-strong">{uninstall.error instanceof Error ? uninstall.error.message : "卸载失败"}</p> : null}
          </div>
          <DialogFooter className="gap-2">
            <Button variant="outline" disabled={uninstall.isPending} onClick={() => { setUninstallOpen(false); setUninstallConfirmation(""); }}>取消</Button>
            <Button variant="destructive" disabled={uninstallConfirmation !== release.name || uninstall.isPending} onClick={() => uninstall.mutate()}>
              {uninstall.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}确认卸载
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={testOpen} onOpenChange={setTestOpen}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-3xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle className="flex min-w-0 items-center gap-2">
              {testResult?.status === "failed" ? <XCircle className="h-5 w-5 shrink-0 text-destructive" /> : <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-600" />}
              <span className="break-words">Helm test 结果</span>
            </DialogTitle>
            <DialogDescription className="break-words">{testResult?.namespace}/{testResult?.name} · revision #{testResult?.revision ?? "—"} · {testResult?.startedAt ? formatDate(testResult.startedAt) : "—"}</DialogDescription>
          </DialogHeader>
          {testResult ? (
            <div className="min-w-0 space-y-4">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <Badge variant={testResult.status === "failed" ? "destructive" : "success"}>{testResult.status === "failed" ? "测试失败" : "测试通过"}</Badge>
                <span className="min-w-0 break-words text-sm text-muted-foreground">{testResult.message || "Helm test 已完成"}</span>
              </div>
              {testResult.warnings?.length ? <div className="min-w-0 space-y-1 rounded-md border border-warning/35 bg-warning/10 p-3 text-xs text-warning">{testResult.warnings.map((warning, index) => <p key={index} className="break-words">{warning}</p>)}</div> : null}
              <section className="min-w-0 space-y-2">
                <h3 className="text-sm font-medium">测试 Hook ({testResult.hooks.length})</h3>
                {testResult.hooks.length ? testResult.hooks.map((hook, index) => (
                  <div key={`${hook.kind}-${hook.name}-${index}`} className="min-w-0 rounded-md border px-3 py-2">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <Badge variant={/failed|error/i.test(hook.status) ? "destructive" : "secondary"}>{hook.status || "Unknown"}</Badge>
                      <Badge variant="outline"><span className="max-w-full truncate">{hook.kind}</span></Badge>
                    </div>
                    <p className="mt-1 break-all font-mono text-xs">{hook.name}</p>
                    {hook.path ? <p className="mt-1 break-all text-[11px] text-muted-foreground">{hook.path}</p> : null}
                    {hook.message && hook.message !== hook.name ? <p className="mt-1 break-words text-xs text-muted-foreground">{hook.message}</p> : null}
                    <p className="mt-1 text-[11px] text-muted-foreground">{hook.startedAt ? formatDate(hook.startedAt) : "开始时间未知"} → {hook.completedAt ? formatDate(hook.completedAt) : "结束时间未知"}</p>
                  </div>
                )) : <p className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">该 Release 没有声明可执行的 test Hook。</p>}
              </section>
              <section className="min-w-0 space-y-2">
                <h3 className="text-sm font-medium">测试日志</h3>
                <pre className="max-h-[42vh] min-h-32 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/25 p-3 font-mono text-xs leading-5">{testResult.logs || "没有可用的测试 Pod 日志。"}</pre>
              </section>
            </div>
          ) : <EmptyState title="没有测试结果" description="请重新运行 Helm test。" />}
          <DialogFooter>
            <Button variant="outline" onClick={() => setTestOpen(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
