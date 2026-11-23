import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  ArrowLeft,
  Boxes,
  Loader2,
  PackageSearch,
  ShieldAlert,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type {
  HelmChartPlan,
  HelmExecutionOptionsInput,
  HelmReleaseDetail,
} from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Textarea } from "@/components/ui/textarea";
import {
  defaultHelmExecutionOptions,
  HelmExecutionOptionsFields,
} from "@/features/helm/helm-execution-options";

const chartKey = (repositoryId: string, name: string) =>
  `${repositoryId}\u0000${name}`;

export function HelmChartVersionUpgradeDialog({
  release,
  open,
  onOpenChange,
}: {
  release: HelmReleaseDetail;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedChartKey, setSelectedChartKey] = useState("");
  const [version, setVersion] = useState("");
  const [values, setValues] = useState("");
  const [options, setOptions] = useState<HelmExecutionOptionsInput>({
    ...defaultHelmExecutionOptions,
  });
  const [plan, setPlan] = useState<HelmChartPlan>();
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");

  const charts = useQuery({
    queryKey: ["helm-charts", "release-chart-switch"],
    queryFn: () => api.helmCharts({}),
    enabled: open,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });

  const chartOptions = useMemo(() => {
    const items = charts.data?.items ?? [];
    const exact = items.filter((item) => item.name === release.chart);
    const others = items.filter((item) => item.name !== release.chart);
    return [...exact, ...others].map((item) => ({
      value: chartKey(item.repositoryId, item.name),
      label: `${item.name} · ${item.repositoryName} · v${item.version}${item.name === release.chart ? " · 当前 Chart" : ""}`,
    }));
  }, [charts.data?.items, release.chart]);

  const selectedChart = useMemo(() => {
    const items = charts.data?.items ?? [];
    return items.find(
      (item) => chartKey(item.repositoryId, item.name) === selectedChartKey,
    );
  }, [charts.data?.items, selectedChartKey]);

  const detail = useQuery({
    queryKey: [
      "helm-chart",
      selectedChart?.repositoryId,
      selectedChart?.name,
    ],
    queryFn: () => api.helmChart(selectedChart!.repositoryId, selectedChart!.name),
    enabled: open && Boolean(selectedChart),
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });
  const detailKey = detail.data
    ? `${detail.data.repositoryId}/${detail.data.name}`
    : "";

  const invalidatePlan = () => {
    setPlan(undefined);
    setConfirmation("");
    setError("");
  };

  useEffect(() => {
    if (!open) return;
    setSelectedChartKey("");
    setVersion("");
    setValues("");
    setOptions({ ...defaultHelmExecutionOptions });
    setPlan(undefined);
    setConfirmation("");
    setError("");
  }, [open, release.name, release.revision]);

  useEffect(() => {
    if (!open || selectedChartKey || !charts.data?.items.length) return;
    const exact = charts.data.items.find((item) => item.name === release.chart);
    const fallback = exact ?? charts.data.items[0];
    setSelectedChartKey(chartKey(fallback.repositoryId, fallback.name));
  }, [charts.data?.items, open, release.chart, selectedChartKey]);

  useEffect(() => {
    if (!open || !detail.data) return;
    const currentVersion =
      detail.data.name === release.chart
        ? detail.data.versions.find(
            (item) => item.version === release.chartVersion,
          )
        : undefined;
    setVersion(currentVersion?.version ?? detail.data.version);
    setPlan(undefined);
    setConfirmation("");
    setError("");
  }, [
    detailKey,
    open,
    release.chart,
    release.chartVersion,
  ]);

  const versionOptions = useMemo(
    () =>
      (detail.data?.versions ?? []).map((item) => ({
        value: item.version,
        label: `v${item.version}${item.appVersion ? ` · app ${item.appVersion}` : ""}${item.deprecated ? " · 已弃用" : ""}`,
      })),
    [detail.data?.versions],
  );

  const planMutation = useMutation({
    mutationFn: () => {
      if (!selectedChart) throw new Error("请选择目标 Chart");
      return api.planHelmChart({
        repositoryId: selectedChart.repositoryId,
        chartName: selectedChart.name,
        version,
        digest: detail.data?.versions.find((item) => item.version === version)
          ?.digest,
        mode: "upgrade",
        namespace: release.namespace,
        releaseName: release.name,
        values,
        options,
      });
    },
    onSuccess: (result) => {
      setPlan(result);
      setConfirmation("");
      setError("");
    },
    onError: (mutationError) => setError(mutationError.message),
  });

  const executeMutation = useMutation({
    mutationFn: async () => {
      if (!plan) throw new Error("请先生成升级计划");
      return api.upgradeHelmChart({
        repositoryId: plan.repositoryId,
        chartName: plan.chartName,
        version: plan.chartVersion,
        digest: plan.digest,
        mode: "upgrade",
        namespace: release.namespace,
        releaseName: release.name,
        values,
        options: plan.options,
        planDigest: plan.planDigest,
        confirmation,
      });
    },
    onSuccess: async (operation) => {
      toast.success(`已提交 ${operation.releaseName} 的 Chart 升级操作`);
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ["helm-releases"] });
      void navigate({ to: "/helm/operations", search: { q: operation.releaseName } });
    },
    onError: (mutationError) => setError(mutationError.message),
  });

  const canPlan = Boolean(
    selectedChart && detail.data && version && !planMutation.isPending,
  );
  const canExecute = Boolean(
    plan &&
      confirmation.trim() === release.name &&
      !executeMutation.isPending,
  );
  const targetChanged = Boolean(
    selectedChart &&
      (selectedChart.name !== release.chart || version !== release.chartVersion),
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!executeMutation.isPending) onOpenChange(next);
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-3xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
        <DialogHeader>
          <DialogTitle className="flex min-w-0 items-center gap-2">
            <PackageSearch className="h-5 w-5 shrink-0" />
            <span className="break-words">更换 Helm Chart 版本</span>
          </DialogTitle>
          <DialogDescription className="break-words">
            从已登记仓库选择目标 Chart 与版本，沿用当前 Release Values
            作为基础，先生成受控升级计划再执行。
          </DialogDescription>
        </DialogHeader>

        {!plan ? (
          <div className="min-w-0 space-y-4">
            <div className="min-w-0 rounded-md border bg-muted/25 px-3 py-2 text-xs leading-5">
              <p className="break-all">
                <span className="text-muted-foreground">Release：</span>
                <span className="font-medium">
                  {release.namespace}/{release.name}
                </span>
              </p>
              <p className="break-all">
                <span className="text-muted-foreground">当前 Chart：</span>
                <span className="font-mono">
                  {release.chart} v{release.chartVersion}
                </span>
                <span className="text-muted-foreground"> · revision #{release.revision}</span>
              </p>
            </div>

            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-chart-switch-chart">目标 Chart</Label>
                <SearchableSelect
                  id="helm-chart-switch-chart"
                  label="目标 Chart"
                  value={selectedChartKey}
                  options={chartOptions}
                  onChange={(value) => {
                    setSelectedChartKey(value);
                    setVersion("");
                    invalidatePlan();
                  }}
                  onManualSelect={() => undefined}
                  allowManual={false}
                  placeholder={
                    charts.isLoading
                      ? "正在读取 Chart 目录…"
                      : chartOptions.length
                        ? "搜索 Chart 名称或仓库"
                        : "没有可用 Chart"
                  }
                  searchPlaceholder="搜索 Chart 名称或仓库…"
                  disabled={charts.isLoading || chartOptions.length === 0}
                />
              </div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-chart-switch-version">目标版本</Label>
                <SearchableSelect
                  id="helm-chart-switch-version"
                  label="目标版本"
                  value={version}
                  options={versionOptions}
                  onChange={(value) => {
                    setVersion(value);
                    invalidatePlan();
                  }}
                  onManualSelect={() => undefined}
                  allowManual={false}
                  placeholder={
                    detail.isLoading
                      ? "正在读取版本…"
                      : versionOptions.length
                        ? "选择 Chart 版本"
                        : "没有可用版本"
                  }
                  searchPlaceholder="搜索版本或 appVersion…"
                  disabled={detail.isLoading || versionOptions.length === 0}
                />
              </div>
            </div>

            {charts.isError ? (
              <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">
                无法读取 Chart 目录：{charts.error.message}
              </p>
            ) : null}
            {detail.isError ? (
              <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">
                无法读取目标 Chart：{detail.error.message}
              </p>
            ) : null}
            {charts.data && chartOptions.length === 0 ? (
              <p className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
                当前没有启用的 Chart 仓库。请先在仓库管理中登记并同步 Chart。
              </p>
            ) : null}
            {selectedChart && selectedChart.name !== release.chart ? (
              <div className="flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
                <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
                <span className="break-words">
                  目标 Chart 名称与当前 Release 不同。只有明确进行 Chart
                  迁移时才应继续，资源所有权和保留对象需要人工核对。
                </span>
              </div>
            ) : null}
            {selectedChart && !targetChanged ? (
              <p className="rounded-md border bg-muted/25 px-3 py-2 text-xs text-muted-foreground">
                目标 Chart 与版本仍是当前值。继续只会基于增量 Values 生成新的
                revision；如需只修改配置，可直接使用 Release 的 Values 升级。
              </p>
            ) : null}

            <div className="min-w-0 space-y-1.5">
              <Label htmlFor="helm-chart-switch-values">Values Override（YAML）</Label>
              <Textarea
                id="helm-chart-switch-values"
                value={values}
                onChange={(event) => {
                  setValues(event.target.value);
                  invalidatePlan();
                }}
                className="min-h-40 max-h-64 resize-y font-mono text-xs"
                spellCheck={false}
                placeholder={"replicaCount: 3\nimage:\n  tag: \"1.27.0\"\n"}
              />
              <p className="text-xs leading-5 text-muted-foreground">
                留空会沿用 Release 当前 Values；填写内容会作为增量覆盖。服务端会用目标版本的
                values.schema.json 校验，并在计划中遮蔽敏感字段。
              </p>
            </div>

            <HelmExecutionOptionsFields
              idPrefix="helm-chart-switch-options"
              mode="upgrade"
              value={options}
              onChange={(next) => {
                setOptions(next);
                invalidatePlan();
              }}
              disabled={planMutation.isPending}
            />

            {error ? (
              <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">
                {error}
              </p>
            ) : null}
            <DialogFooter className="gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
                disabled={planMutation.isPending}
              >
                取消
              </Button>
              <Button
                type="button"
                onClick={() => planMutation.mutate()}
                disabled={!canPlan}
              >
                {planMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Boxes className="h-4 w-4" />
                )}
                生成升级计划
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="min-w-0 space-y-4">
            <div className="grid min-w-0 gap-2 rounded-md border bg-muted/25 p-3 text-sm sm:grid-cols-2">
              <p className="min-w-0 break-all">
                <span className="text-muted-foreground">Release：</span>
                <span className="font-medium">
                  {plan.namespace}/{plan.releaseName}
                </span>
              </p>
              <p className="min-w-0 break-all">
                <span className="text-muted-foreground">Revision：</span>
                当前 #{release.revision} → 计划 #{plan.currentRevision ? plan.currentRevision + 1 : release.revision + 1}
              </p>
              <p className="min-w-0 break-all">
                <span className="text-muted-foreground">当前 Chart：</span>
                <span className="font-mono">
                  {release.chart} v{release.chartVersion}
                </span>
              </p>
              <p className="min-w-0 break-all">
                <span className="text-muted-foreground">目标 Chart：</span>
                <span className="font-mono">
                  {plan.chartName} v{plan.chartVersion}
                </span>
              </p>
              <p className="min-w-0 break-all">
                <span className="text-muted-foreground">资源：</span>
                {plan.resources.length} 项 · {plan.hookCount} Hook · {plan.crdCount} CRD
              </p>
              <p className="min-w-0 break-all font-mono text-xs">
                <span className="text-muted-foreground">包摘要：</span>
                {plan.digest}
              </p>
              <p className="min-w-0 break-all font-mono text-xs sm:col-span-2">
                <span className="text-muted-foreground">计划摘要：</span>
                {plan.planDigest}
              </p>
              <p className="min-w-0 break-all sm:col-span-2">
                <span className="text-muted-foreground">执行策略：</span>
                超时 {plan.options.timeoutSeconds}s · 历史 {plan.options.maxHistory} 个 ·{" "}
                {plan.options.wait ? "等待就绪" : "不等待"} ·{" "}
                {plan.options.atomic ? "原子回滚" : "非原子"}
              </p>
            </div>

            {plan.warnings?.length ? (
              <div role="alert" className="space-y-1 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
                {plan.warnings.map((warning, index) => (
                  <p key={index} className="flex min-w-0 items-start gap-2 break-words">
                    <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    {warning}
                  </p>
                ))}
              </div>
            ) : null}

            <div className="flex items-start gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-muted-foreground">
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
              <span className="break-words">
                计划已锁定目标包摘要、Values、目标资源和当前 revision；执行前
                Release 若发生变化会被拒绝，需要重新生成计划。
              </span>
            </div>

            <div className="min-w-0 space-y-2">
              <p className="text-sm font-medium">渲染资源预检</p>
              <div className="grid min-w-0 gap-2 sm:grid-cols-2">
                {plan.resources.map((resource) => (
                  <div
                    key={`${resource.apiVersion}/${resource.kind}/${resource.namespace}/${resource.name}`}
                    className="min-w-0 rounded-md border px-3 py-2"
                  >
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <Badge variant={resource.action === "create" ? "success" : resource.action === "delete" ? "destructive" : "warning"}>
                        {resource.action}
                      </Badge>
                      <Badge variant="secondary" className="max-w-full">
                        <span className="truncate">{resource.kind}</span>
                      </Badge>
                      {resource.hook ? <Badge variant="outline">Hook</Badge> : null}
                      {resource.crd ? <Badge variant="outline">CRD</Badge> : null}
                    </div>
                    <p className="mt-1 break-all font-mono text-xs">
                      {resource.namespace ? `${resource.namespace}/` : ""}
                      {resource.name}
                    </p>
                    <p className="mt-0.5 break-all text-[11px] text-muted-foreground">
                      {resource.apiVersion} · {resource.resource}
                    </p>
                  </div>
                ))}
                {plan.resources.length === 0 ? (
                  <p className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">
                    该 Chart 不包含可部署资源。
                  </p>
                ) : null}
              </div>
            </div>

            <details className="min-w-0 rounded-md border">
              <summary className="cursor-pointer px-3 py-2 text-sm font-medium">
                查看脱敏后的渲染清单
              </summary>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words border-t bg-muted/20 p-3 text-xs">
                {plan.manifest || "（空）"}
              </pre>
            </details>

            <div className="min-w-0 space-y-1.5 rounded-md border border-warning/30 p-3">
              <Label htmlFor="helm-chart-switch-confirmation">
                输入 Release 名称确认
              </Label>
              <p className="break-words text-xs text-muted-foreground">
                请输入{" "}
                <code className="break-all rounded bg-muted px-1 py-0.5 font-mono">
                  {release.name}
                </code>{" "}
                后执行。升级会真实修改集群并创建新的 Helm revision。
              </p>
              <Input
                id="helm-chart-switch-confirmation"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                autoComplete="off"
                spellCheck={false}
                className="font-mono"
              />
            </div>

            {error ? (
              <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">
                {error}
              </p>
            ) : null}
            <DialogFooter className="gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setPlan(undefined);
                  setConfirmation("");
                  setError("");
                }}
                disabled={executeMutation.isPending}
              >
                <ArrowLeft className="h-4 w-4" />
                修改配置
              </Button>
              <Button
                type="button"
                onClick={() => executeMutation.mutate()}
                disabled={!canExecute}
              >
                {executeMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <PackageSearch className="h-4 w-4" />
                )}
                确认升级
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
