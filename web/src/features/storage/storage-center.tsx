import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  BadgeCheck,
  Boxes,
  CircleCheck,
  CircleX,
  Database,
  HardDrive,
  Info,
  Loader2,
  RefreshCw,
  ShieldAlert,
  Sparkles,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type {
  HelmChartDetail,
  StorageCatalogItem,
  StorageCheck,
  StorageCheckStatus,
  StorageIntegration,
  StoragePreflight,
  StorageVerification,
} from "@/api/types";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import {
  NamespacePicker,
  type NamespacePlan,
} from "@/components/layout/namespace-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { HelmInstallDialog } from "@/features/helm/charts/helm-install-dialog";
import { cn } from "@/lib/utils";

const catalogStatusLabel: Record<StorageCatalogItem["status"], string> = {
  available: "可安装",
  "repository-missing": "未配置仓库",
  "chart-missing": "未找到 Chart",
  forbidden: "无仓库权限",
};

const integrationStatusLabel: Record<StorageIntegration["status"], string> = {
  healthy: "运行正常",
  degraded: "运行异常",
  unknown: "状态未知",
};

function checkVariant(status: StorageCheckStatus): "success" | "warning" | "destructive" | "secondary" | "default" {
  if (status === "pass") return "success";
  if (status === "warning") return "warning";
  if (status === "fail") return "destructive";
  return "secondary";
}

function checkIcon(status: StorageCheckStatus) {
  if (status === "pass") return <CircleCheck className="h-4 w-4" />;
  if (status === "warning") return <TriangleAlert className="h-4 w-4" />;
  if (status === "fail") return <CircleX className="h-4 w-4" />;
  return <Info className="h-4 w-4" />;
}

function CheckList({ checks }: { checks: StorageCheck[] }) {
  if (checks.length === 0) return null;
  return (
    <ul className="min-w-0 space-y-2">
      {checks.map((check) => (
        <li key={check.id} className="min-w-0 rounded-md border px-3 py-2">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className={cn("shrink-0", check.status === "pass" ? "text-success" : check.status === "fail" ? "text-destructive" : check.status === "warning" ? "text-warning" : "text-muted-foreground")}>{checkIcon(check.status)}</span>
            <span className="min-w-0 break-words text-sm font-medium">{check.title}</span>
            <Badge variant={checkVariant(check.status)}>{check.status}</Badge>
          </div>
          <p className="mt-1 break-words text-xs text-muted-foreground">{check.message}</p>
          {check.suggestion ? <p className="mt-1 break-words text-xs text-foreground/70">建议：{check.suggestion}</p> : null}
        </li>
      ))}
    </ul>
  );
}

function SummaryCard({ icon, label, value, detail }: { icon: React.ReactNode; label: string; value: React.ReactNode; detail?: React.ReactNode }) {
  return (
    <div className="min-w-0 rounded-lg border bg-card p-4">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className="mt-1 break-words text-2xl font-semibold tracking-tight">{value}</p>
          {detail ? <div className="mt-1 break-words text-xs text-muted-foreground">{detail}</div> : null}
        </div>
        <span className="shrink-0 rounded-md border bg-muted/40 p-2 text-muted-foreground">{icon}</span>
      </div>
    </div>
  );
}

function CatalogCard({ item, onSelect }: { item: StorageCatalogItem; onSelect: () => void }) {
  const available = item.status === "available";
  return (
    <Card className="flex min-w-0 flex-col">
      <CardHeader className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <Badge variant={available ? "success" : item.status === "forbidden" ? "destructive" : "warning"}>{catalogStatusLabel[item.status]}</Badge>
          <Badge variant="outline">{item.supportLevel === "verified" ? "Dosh Verified" : "通用"}</Badge>
          {item.installed ? <Badge variant="secondary">已检测到</Badge> : null}
        </div>
        <CardTitle className="break-words text-base">{item.name}</CardTitle>
        <p className="break-words text-xs text-muted-foreground">{item.vendor} · {item.category}</p>
        <p className="break-all font-mono text-[11px] text-muted-foreground">{item.project}</p>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-1 flex-col gap-3">
        <p className="break-words text-sm text-muted-foreground">{item.description}</p>
        <div className="flex min-w-0 flex-wrap gap-1">
          {item.capabilities.map((capability) => <Badge key={capability} variant="outline" className="max-w-full">{capability}</Badge>)}
        </div>
        {item.chart ? <p className="break-all font-mono text-[11px] text-muted-foreground">{item.chart.repositoryName} / {item.chart.chartName} v{item.chart.version}</p> : null}
        {item.statusMessage ? <p className="break-words text-xs text-warning">{item.statusMessage}</p> : null}
        {item.warnings?.length ? <p className="flex min-w-0 items-start gap-1.5 break-words text-xs text-warning"><TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />{item.warnings[0]}</p> : null}
        <div className="mt-auto flex flex-wrap gap-2 pt-1">
          <Button type="button" size="sm" variant="outline" onClick={onSelect}>查看方案与预检</Button>
          {!item.chart && item.status !== "forbidden" ? <Button type="button" size="sm" variant="ghost" asChild><Link to="/helm/repositories">配置仓库</Link></Button> : null}
        </div>
      </CardContent>
    </Card>
  );
}

function IntegrationCard({ item, onVerify }: { item: StorageIntegration; onVerify: () => void }) {
  return (
    <Card className="min-w-0">
      <CardContent className="min-w-0 space-y-3 p-4">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <Badge variant={item.source === "managed" ? "default" : "secondary"}>{item.source === "managed" ? "Dosh 管理" : "纳管"}</Badge>
          <Badge variant={item.status === "healthy" ? "success" : item.status === "degraded" ? "destructive" : "warning"}>{integrationStatusLabel[item.status]}</Badge>
          <Badge variant="outline">{item.kind}</Badge>
          {item.driverRegistered ? <Badge variant="outline">CSI 已注册</Badge> : null}
        </div>
        <div className="min-w-0">
          <p className="break-words font-medium">{item.name}</p>
          <p className="mt-0.5 break-all font-mono text-xs text-muted-foreground">{item.provisioner || "未识别 Provisioner"}</p>
        </div>
        {item.releaseName ? <p className="break-all text-xs text-muted-foreground">Release：{item.releaseNamespace}/{item.releaseName}</p> : null}
        <div className="flex min-w-0 flex-wrap gap-1">
          {item.storageClasses.length > 0 ? item.storageClasses.map((name) => <Badge key={name} variant="outline" className="max-w-full break-all">SC · {name}</Badge>) : <span className="text-xs text-muted-foreground">尚未发现关联 StorageClass</span>}
        </div>
        <p className="break-words text-xs text-muted-foreground">{item.message}</p>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">{item.readyPodCount}/{item.podCount} 工作负载 Running</span>
          <Button type="button" size="sm" variant="outline" onClick={onVerify} className="ml-auto">验证存储</Button>
        </div>
      </CardContent>
    </Card>
  );
}

type VerifyTarget = {
  namespace: string;
  storageClass: string;
  catalogId?: string;
  releaseName?: string;
};

export function StorageCenterPanel() {
  const queryClient = useQueryClient();
  const center = useQuery({ queryKey: ["storage-center"], queryFn: api.storageCenter, staleTime: 20_000, retry: false });
  const [selectedCatalog, setSelectedCatalog] = useState<StorageCatalogItem>();
  const [preflightNamespace, setPreflightNamespace] = useState("");
  const [preflightNamespacePlan, setPreflightNamespacePlan] = useState<NamespacePlan>();
  const [preflightRelease, setPreflightRelease] = useState("");
  const [preflight, setPreflight] = useState<StoragePreflight>();
  const [preflightError, setPreflightError] = useState("");
  const [installTarget, setInstallTarget] = useState<{ item: StorageCatalogItem; chart: HelmChartDetail }>();
  const [installLoading, setInstallLoading] = useState(false);
  const [verifyTarget, setVerifyTarget] = useState<VerifyTarget>();
  const [verification, setVerification] = useState<StorageVerification>();
  const [verifyError, setVerifyError] = useState("");

  const preflightMutation = useMutation({
    mutationFn: () => {
      if (!selectedCatalog) throw new Error("请选择存储方案");
      return api.storagePreflight({ catalogId: selectedCatalog.id, namespace: preflightNamespace, releaseName: preflightRelease });
    },
    onSuccess: (result) => { setPreflight(result); setPreflightError(""); },
    onError: (error) => setPreflightError(error instanceof Error ? error.message : "预检失败"),
  });

  const verifyMutation = useMutation({
    mutationFn: (input: { target: VerifyTarget; smokeTest: boolean; image: string }) => api.verifyStorage({
      catalogId: input.target.catalogId,
      namespace: input.target.namespace,
      storageClass: input.target.storageClass,
      releaseName: input.target.releaseName,
      smokeTest: input.smokeTest,
      smokeTestImage: input.image || undefined,
    }),
    onSuccess: (result) => { setVerification(result); setVerifyError(""); void queryClient.invalidateQueries({ queryKey: ["storage-center"] }); },
    onError: (error) => setVerifyError(error instanceof Error ? error.message : "验证失败"),
  });

  const allClasses = useMemo(() => {
    const values = new Set<string>();
    for (const integration of center.data?.integrations ?? []) integration.storageClasses.forEach((name) => values.add(name));
    for (const item of center.data?.catalog ?? []) item.expectedStorageClasses.forEach((name) => values.add(name));
    return Array.from(values).sort((left, right) => left.localeCompare(right));
  }, [center.data]);

  const openCatalog = (item: StorageCatalogItem) => {
    setSelectedCatalog(item);
    setPreflight(undefined);
    setPreflightError("");
    setPreflightNamespace(item.defaultNamespace);
    setPreflightNamespacePlan(undefined);
    setPreflightRelease(item.defaultReleaseName);
  };

  const openVerify = (target: VerifyTarget) => {
    setVerification(undefined);
    setVerifyError("");
    setVerifyTarget(target);
  };

  const startInstall = async () => {
    if (!selectedCatalog?.chart) return;
    setInstallLoading(true);
    try {
      const chart = await api.helmChart(selectedCatalog.chart.repositoryId, selectedCatalog.chart.chartName);
      setInstallTarget({ item: selectedCatalog, chart });
      setSelectedCatalog(undefined);
      setPreflight(undefined);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载 Chart 失败");
    } finally {
      setInstallLoading(false);
    }
  };

  if (center.isLoading) return <LoadingState label="正在发现存储方案与驱动" />;
  if (center.isError) return <ErrorState error={center.error} onRetry={() => void center.refetch()} />;
  if (!center.data) return null;
  const data = center.data;
  const preflightReady = preflight?.status === "pass" || preflight?.status === "warning";

  return (
    <div className="min-w-0 space-y-6">
      <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-base font-semibold">存储中心</h2>
          <p className="break-words text-sm text-muted-foreground">发现、安装、验证并诊断集群存储驱动。StorageClass 仍是 Kubernetes 中的独立资源。</p>
        </div>
        <Button variant="outline" size="sm" className="self-start" onClick={() => void center.refetch()} disabled={center.isFetching}>
          {center.isFetching ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
          刷新
        </Button>
      </div>

      {data.warnings?.length ? (
        <div className="min-w-0 space-y-1 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-xs text-warning">
          {data.warnings.map((warning) => <p key={warning} className="flex min-w-0 items-start gap-2 break-words"><TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />{warning}</p>)}
        </div>
      ) : null}

      <section className="min-w-0 space-y-3" aria-labelledby="storage-center-summary">
        <h2 id="storage-center-summary" className="text-sm font-semibold">环境概览</h2>
        <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <SummaryCard icon={<HardDrive className="h-4 w-4" />} label="节点" value={`${data.summary.readyNodeCount}/${data.summary.nodeCount}`} detail="Ready / 总数" />
          <SummaryCard icon={<Database className="h-4 w-4" />} label="StorageClass" value={data.summary.storageClassCount} detail={`${data.summary.defaultStorageClassCount} 个默认类`} />
          <SummaryCard icon={<Boxes className="h-4 w-4" />} label="存储驱动" value={data.summary.managedIntegrationCount + data.summary.adoptedIntegrationCount} detail={`${data.summary.managedIntegrationCount} 管理 · ${data.summary.adoptedIntegrationCount} 纳管`} />
          <SummaryCard icon={<Sparkles className="h-4 w-4" />} label="PVC" value={data.summary.persistentVolumeClaimCount} detail={`${data.summary.pendingClaimCount} 个 Pending · ${data.summary.volumeSnapshotAvailable ? "快照可用" : "快照不可用"}`} />
        </div>
      </section>

      <section className="min-w-0 space-y-3" aria-labelledby="storage-center-catalog">
        <div className="flex min-w-0 flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h2 id="storage-center-catalog" className="text-sm font-semibold">存储方案目录</h2>
            <p className="text-xs text-muted-foreground">只承诺已验证方案的完整体验；其他驱动走通用 Helm，不会因缺少目录元数据而无法安装。</p>
          </div>
        </div>
        <div className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
          {data.catalog.map((item) => <CatalogCard key={item.id} item={item} onSelect={() => openCatalog(item)} />)}
          <Card className="min-w-0 border-dashed">
            <CardHeader>
              <div className="flex flex-wrap gap-1.5"><Badge variant="outline">兜底路径</Badge><Badge variant="secondary">未验证</Badge></div>
              <CardTitle className="text-base">通用 Helm Chart</CardTitle>
            </CardHeader>
            <CardContent className="flex min-w-0 flex-1 flex-col gap-3">
              <p className="break-words text-sm text-muted-foreground">管理员已登记的可信仓库中的任意存储 Chart 都可以通过现有 Helm 受控安装。Dosh 会继续执行计划摘要、资源冲突和 RBAC 预检，但不承诺开箱即用的存储结果。</p>
              <Button asChild size="sm" variant="outline" className="mt-auto self-start"><Link to="/helm/charts">打开 Helm Chart 目录</Link></Button>
            </CardContent>
          </Card>
        </div>
      </section>

      <section className="min-w-0 space-y-3" aria-labelledby="storage-center-integrations">
        <h2 id="storage-center-integrations" className="text-sm font-semibold">已有存储驱动</h2>
        {data.integrations.length === 0 ? (
          <div className="rounded-lg border border-dashed p-5 text-sm text-muted-foreground">尚未发现 CSI Driver 或 Provisioner。可从上方目录安装，或在任意 Helm Chart 中部署后回来纳管。</div>
        ) : (
          <div className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
            {data.integrations.map((item) => <IntegrationCard key={item.id} item={item} onVerify={() => openVerify({ namespace: "", storageClass: item.storageClasses[0] ?? "", releaseName: item.releaseName })} />)}
          </div>
        )}
      </section>

      <section className="min-w-0 space-y-3" aria-labelledby="storage-center-diagnostics">
        <h2 id="storage-center-diagnostics" className="text-sm font-semibold">诊断</h2>
        {data.diagnostics.length === 0 ? (
          <div className="flex items-center gap-2 rounded-lg border border-success/30 bg-success/5 p-4 text-sm text-success"><CircleCheck className="h-4 w-4" />当前未发现明显的存储配置问题。</div>
        ) : (
          <div className="grid min-w-0 gap-2">
            {data.diagnostics.map((item) => (
              <div key={item.id} className={cn("min-w-0 rounded-lg border px-4 py-3", item.severity === "critical" ? "border-destructive/30 bg-destructive/5" : item.severity === "warning" ? "border-warning/30 bg-warning/5" : "bg-muted/20")}>
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <Badge variant={item.severity === "critical" ? "destructive" : item.severity === "warning" ? "warning" : "secondary"}>{item.severity}</Badge>
                  <span className="min-w-0 break-words text-sm font-medium">{item.title}</span>
                </div>
                <p className="mt-1 break-words text-xs text-muted-foreground">{item.message}</p>
                {item.suggestion ? <p className="mt-1 break-words text-xs">建议：{item.suggestion}</p> : null}
              </div>
            ))}
          </div>
        )}
      </section>

      {selectedCatalog ? (
        <Dialog open onOpenChange={(open) => { if (!open && !installLoading) { setSelectedCatalog(undefined); setPreflight(undefined); } }}>
          <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-3xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
            <DialogHeader>
              <DialogTitle className="break-words">{selectedCatalog.name}</DialogTitle>
              <DialogDescription className="break-words">{selectedCatalog.description}</DialogDescription>
            </DialogHeader>
            <div className="min-w-0 space-y-4">
              <div className="grid min-w-0 gap-3 rounded-md border bg-muted/20 p-3 sm:grid-cols-2">
                <div className="min-w-0"><p className="text-xs text-muted-foreground">方案状态</p><p className="mt-1 break-words text-sm font-medium">{catalogStatusLabel[selectedCatalog.status]}</p></div>
                <div className="min-w-0"><p className="text-xs text-muted-foreground">Chart</p><p className="mt-1 break-all font-mono text-xs">{selectedCatalog.chart ? `${selectedCatalog.chart.repositoryName}/${selectedCatalog.chart.chartName} v${selectedCatalog.chart.version}` : "不可用"}</p></div>
                <div className="min-w-0"><p className="text-xs text-muted-foreground">Provisioner（Kubernetes 标识）</p><p className="mt-1 break-all font-mono text-xs">{selectedCatalog.provisioners.join(", ")}</p><p className="mt-1 break-words text-[11px] text-muted-foreground">用于匹配 StorageClass，不是项目网址。</p></div>
                <div className="min-w-0"><p className="text-xs text-muted-foreground">上游项目</p><p className="mt-1 break-all font-mono text-xs">{selectedCatalog.project}</p></div>
                <div className="min-w-0"><p className="text-xs text-muted-foreground">预期 StorageClass</p><p className="mt-1 break-all font-mono text-xs">{selectedCatalog.expectedStorageClasses.join(", ") || "由 Chart Values 决定"}</p></div>
              </div>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <div className="min-w-0 space-y-1.5">
                  <Label>目标命名空间</Label>
                  <NamespacePicker
                    id="storage-provider-namespace"
                    label="目标命名空间"
                    value={preflightNamespace}
                    recommended={selectedCatalog.defaultNamespace}
                    onChange={(value) => {
                      setPreflightNamespace(value);
                      setPreflightNamespacePlan(undefined);
                      setPreflight(undefined);
                    }}
                    onPlanChange={setPreflightNamespacePlan}
                  />
                </div>
                <div className="min-w-0 space-y-1.5">
                  <Label>Release 名称</Label>
                  <Input value={preflightRelease} onChange={(event) => { setPreflightRelease(event.target.value); setPreflight(undefined); }} className="font-mono" autoComplete="off" spellCheck={false} />
                </div>
              </div>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <div className="min-w-0">
                  <p className="text-xs font-medium">能力</p>
                  <div className="mt-1 flex flex-wrap gap-1">{selectedCatalog.capabilities.map((value) => <Badge key={value} variant="outline" className="max-w-full">{value}</Badge>)}</div>
                </div>
                <div className="min-w-0">
                  <p className="text-xs font-medium">前置条件</p>
                  <ul className="mt-1 space-y-1 text-xs text-muted-foreground">{selectedCatalog.prerequisites.map((value) => <li key={value} className="break-words">• {value}</li>)}</ul>
                </div>
              </div>
              {selectedCatalog.warnings?.length ? <div className="space-y-1 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{selectedCatalog.warnings.map((warning) => <p key={warning} className="flex min-w-0 items-start gap-2 break-words"><TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />{warning}</p>)}</div> : null}
              {selectedCatalog.status !== "available" ? <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><ShieldAlert className="mr-1 inline h-3.5 w-3.5" />{selectedCatalog.statusMessage || "该方案尚不可安装。"} {selectedCatalog.status !== "forbidden" ? <Link to="/helm/repositories" className="underline underline-offset-2">前往仓库管理</Link> : null}</div> : null}
              {preflight ? <div className="space-y-2"><p className="text-sm font-medium">预检结果</p><CheckList checks={preflight.checks} /></div> : null}
              {preflightError ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">{preflightError}</p> : null}
            </div>
            <DialogFooter className="gap-2">
              <Button variant="outline" type="button" onClick={() => setSelectedCatalog(undefined)} disabled={installLoading}>关闭</Button>
              <Button type="button" variant="secondary" onClick={() => preflightMutation.mutate()} disabled={preflightMutation.isPending || !preflightNamespace.trim() || !preflightRelease.trim()}>
                {preflightMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <BadgeCheck className="h-4 w-4" />}
                运行预检
              </Button>
              <Button type="button" onClick={() => void startInstall()} disabled={!preflightReady || !selectedCatalog.chart || installLoading}>
                {installLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Boxes className="h-4 w-4" />}
                进入 Helm 安装
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}

      {installTarget ? (
        <HelmInstallDialog
          chart={installTarget.chart}
          open
          onOpenChange={(open) => { if (!open) setInstallTarget(undefined); }}
          initialNamespace={preflightNamespace.trim() || installTarget.item.defaultNamespace}
          initialCreateNamespace={Boolean(preflightNamespacePlan?.createRequired)}
          initialReleaseName={preflightRelease || installTarget.item.defaultReleaseName}
          onInstalled={(operation) => {
            const item = installTarget.item;
            setInstallTarget(undefined);
            setSelectedCatalog(undefined);
            setPreflight(undefined);
            void center.refetch();
            toast.success(`${item.name} 安装完成，开始验证存储`);
            openVerify({ namespace: operation.namespace, storageClass: item.expectedStorageClasses[0] ?? "", catalogId: item.id, releaseName: operation.releaseName });
          }}
        />
      ) : null}

      {verifyTarget ? (
        <StorageVerifyDialog
          target={verifyTarget}
          classes={allClasses}
          result={verification}
          error={verifyError}
          pending={verifyMutation.isPending}
          onClose={() => { if (!verifyMutation.isPending) setVerifyTarget(undefined); }}
          onSubmit={(next, smokeTest, image) => { setVerifyTarget(next); verifyMutation.mutate({ target: next, smokeTest, image }); }}
        />
      ) : null}
    </div>
  );
}

function StorageVerifyDialog({ target, classes, result, error, pending, onClose, onSubmit }: {
  target: VerifyTarget;
  classes: string[];
  result?: StorageVerification;
  error: string;
  pending: boolean;
  onClose: () => void;
  onSubmit: (target: VerifyTarget, smokeTest: boolean, image: string) => void;
}) {
  const [namespace, setNamespace] = useState(target.namespace);
  const [storageClass, setStorageClass] = useState(target.storageClass);
  const [manualClass, setManualClass] = useState(classes.length === 0);
  const [smokeTest, setSmokeTest] = useState(true);
  const [image, setImage] = useState("");
  const classOptions = classes.map((name) => ({ value: name, label: name }));
  const canvasReady = Boolean(namespace.trim() && storageClass.trim());
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
        <DialogHeader>
          <DialogTitle>验证存储</DialogTitle>
          <DialogDescription className="break-words">检查 StorageClass、驱动工作负载，并可选创建临时 PVC + Pod 验证动态供应和挂载。验证成功后临时资源会自动清理。</DialogDescription>
        </DialogHeader>
        <div className="min-w-0 space-y-4">
          <div className="min-w-0 space-y-1.5"><Label>命名空间</Label><NamespaceSelect value={namespace} onChange={setNamespace} allowAll={false} ariaLabel="存储验证命名空间" /></div>
          <div className="min-w-0 space-y-1.5">
            <Label>StorageClass</Label>
            {!manualClass && classOptions.length > 0 ? (
              <SearchableSelect id="storage-verify-class" label="StorageClass" value={storageClass} options={classOptions} onChange={setStorageClass} onManualSelect={() => setManualClass(true)} placeholder="选择 StorageClass" searchPlaceholder="搜索 StorageClass…" />
            ) : (
              <Input value={storageClass} onChange={(event) => setStorageClass(event.target.value)} className="font-mono" autoComplete="off" spellCheck={false} placeholder="输入 StorageClass 名称" />
            )}
            {manualClass && classOptions.length > 0 ? <button type="button" className="text-xs text-primary hover:underline" onClick={() => setManualClass(false)}>返回 StorageClass 列表</button> : null}
          </div>
          <label className="flex min-w-0 items-start gap-2 rounded-md border px-3 py-2 text-sm">
            <input type="checkbox" className="mt-0.5 h-4 w-4 shrink-0 accent-primary" checked={smokeTest} onChange={(event) => setSmokeTest(event.target.checked)} />
            <span className="min-w-0"><span className="font-medium">运行 Smoke Test</span><span className="mt-0.5 block break-words text-xs text-muted-foreground">会在目标命名空间创建 1Gi PVC 和探针 Pod，验证 WaitForFirstConsumer 供应与挂载，完成后自动删除。</span></span>
          </label>
          {smokeTest ? <div className="min-w-0 space-y-1.5"><Label htmlFor="storage-smoke-image">探针镜像（可选）</Label><Input id="storage-smoke-image" value={image} onChange={(event) => setImage(event.target.value)} className="font-mono" placeholder="registry.k8s.io/pause:3.10" autoComplete="off" spellCheck={false} /><p className="text-xs text-muted-foreground">隔离网络或镜像仓库受限时，请填写节点可拉取的 pause 镜像。</p></div> : null}
          {result ? (
            <div className="min-w-0 space-y-3">
              <div className={cn("rounded-md border px-3 py-2 text-sm", result.status === "pass" ? "border-success/30 bg-success/5 text-success" : result.status === "fail" ? "border-destructive/30 bg-destructive/10 text-destructive-strong" : "border-warning/30 bg-warning/10 text-warning")}>验证结果：{result.status === "pass" ? "通过" : result.status === "fail" ? "失败" : "需要关注"}</div>
              <CheckList checks={result.checks} />
              {result.smokeTest ? <div className="min-w-0 rounded-md border bg-muted/20 px-3 py-2 text-xs"><p className="font-medium">Smoke Test</p><p className="mt-1 break-words text-muted-foreground">{result.smokeTest.message}</p><p className="mt-1 break-all text-muted-foreground">PVC {result.smokeTest.pvcName} · Pod {result.smokeTest.podName} · 耗时 {Math.round(result.smokeTest.durationMs / 100) / 10}s</p></div> : null}
            </div>
          ) : null}
          {error ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">{error}</p> : null}
        </div>
        <DialogFooter className="gap-2">
          <Button variant="outline" type="button" onClick={onClose} disabled={pending}>关闭</Button>
          <Button type="button" onClick={() => onSubmit({ ...target, namespace, storageClass }, smokeTest, image)} disabled={!canvasReady || pending}>
            {pending ? <Loader2 className="h-4 w-4 animate-spin" /> : <BadgeCheck className="h-4 w-4" />}
            开始验证
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
