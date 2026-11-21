import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Boxes, Loader2, PackageCheck, ShieldAlert, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { HelmChartDetail, HelmChartOperation, HelmChartPlan } from "@/api/types";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

type Mode = "install" | "upgrade";

export function HelmInstallDialog({ chart, open, onOpenChange }: { chart: HelmChartDetail; open: boolean; onOpenChange: (open: boolean) => void }) {
  const navigate = useNavigate();
  const [mode, setMode] = useState<Mode>("install");
  const [namespace, setNamespace] = useState("");
  const [releaseName, setReleaseName] = useState("");
  const [version, setVersion] = useState(chart.version);
  const [manualVersion, setManualVersion] = useState(false);
  const [manualRelease, setManualRelease] = useState(false);
  const [values, setValues] = useState(chart.values || "{}");
  const [plan, setPlan] = useState<HelmChartPlan>();
  const [operation, setOperation] = useState<HelmChartOperation>();
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setMode("install");
    setNamespace("");
    setReleaseName("");
    setVersion(chart.version);
    setManualVersion(false);
    setManualRelease(false);
    setValues(chart.values || "{}");
    setPlan(undefined);
    setOperation(undefined);
    setConfirmation("");
    setError("");
  }, [open, chart.repositoryId, chart.name, chart.version, chart.values]);

  const releases = useQuery({
    queryKey: ["helm-releases", namespace],
    queryFn: () => api.helmReleases(namespace),
    enabled: open && mode === "upgrade" && Boolean(namespace),
    retry: false,
    staleTime: 15_000,
  });

  const versionOptions = useMemo(() => chart.versions.map((item) => ({
    value: item.version,
    label: `v${item.version}${item.appVersion ? ` · app ${item.appVersion}` : ""}${item.deprecated ? " · 已弃用" : ""}`,
  })), [chart.versions]);
  const releaseOptions = useMemo(() => (releases.data?.items ?? []).map((item) => ({
    value: item.name,
    label: `${item.name} · rev ${item.revision} · ${item.status}`,
  })), [releases.data?.items]);

  const invalidatePlan = () => {
    setPlan(undefined);
    setOperation(undefined);
    setConfirmation("");
    setError("");
  };

  const changeMode = (next: Mode) => {
    setMode(next);
    setManualRelease(false);
    invalidatePlan();
  };
  const changeNamespace = (next: string) => {
    setNamespace(next);
    setReleaseName("");
    setManualRelease(false);
    invalidatePlan();
  };
  const changeRelease = (next: string) => {
    setReleaseName(next);
    invalidatePlan();
  };
  const changeVersion = (next: string) => {
    setVersion(next);
    setManualVersion(false);
    invalidatePlan();
  };
  const changeValues = (next: string) => {
    setValues(next);
    invalidatePlan();
  };

  const planMutation = useMutation({
    mutationFn: () => api.planHelmChart({
      repositoryId: chart.repositoryId,
      chartName: chart.name,
      version,
      digest: chart.versions.find((item) => item.version === version)?.digest,
      mode,
      namespace,
      releaseName,
      values,
    }),
    onSuccess: (result) => {
      setPlan(result);
      setOperation(undefined);
      setConfirmation("");
      setError("");
    },
    onError: (mutationError) => setError(mutationError.message),
  });

  const executeMutation = useMutation({
    mutationFn: () => {
      if (!plan) throw new Error("请先生成安装计划");
      const input = {
        repositoryId: chart.repositoryId,
        chartName: chart.name,
        version,
        digest: plan.digest,
        mode,
        namespace,
        releaseName,
        values,
        planDigest: plan.planDigest,
        confirmation,
      };
      return mode === "install" ? api.installHelmChart(input) : api.upgradeHelmChart(input);
    },
    onSuccess: (result) => {
      setOperation(result);
      toast.success(mode === "install" ? `${result.releaseName} 安装完成` : `${result.releaseName} 升级完成`);
      onOpenChange(false);
      void navigate({ to: "/helm/releases/$namespace/$name", params: { namespace: result.namespace, name: result.releaseName } });
    },
    onError: (mutationError) => setError(mutationError.message),
  });

  const canPlan = Boolean(namespace && releaseName.trim() && version.trim() && !planMutation.isPending);
  const canExecute = Boolean(plan && confirmation.trim() === releaseName.trim() && !executeMutation.isPending);

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!executeMutation.isPending) onOpenChange(next); }}>
      <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-3xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
        <DialogHeader>
          <DialogTitle className="break-all">{mode === "install" ? "安装" : "升级"} {chart.name}</DialogTitle>
          <DialogDescription className="break-words">Dosh 会固定 Chart 版本和包摘要，在写入集群前逐项检查 Hook、CRD、资源冲突与 RBAC。</DialogDescription>
        </DialogHeader>

        {!plan ? (
          <div className="min-w-0 space-y-4">
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-install-mode">操作类型</Label>
                <Select value={mode} onValueChange={(value) => changeMode(value as Mode)}>
                  <SelectTrigger id="helm-install-mode"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="install">安装新 Release</SelectItem><SelectItem value="upgrade">升级已有 Release</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="min-w-0 space-y-1.5">
                <Label>目标命名空间</Label>
                <NamespaceSelect value={namespace} onChange={changeNamespace} allowAll={false} ariaLabel="Chart 安装命名空间" className="w-full" />
              </div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-install-release">Release 名称</Label>
                {mode === "upgrade" && releaseOptions.length > 0 && !manualRelease ? (
                  <SearchableSelect
                    id="helm-install-release"
                    label="Release"
                    value={releaseName}
                    options={releaseOptions}
                    onChange={changeRelease}
                    onManualSelect={() => setManualRelease(true)}
                    placeholder="选择要升级的 Release"
                    searchPlaceholder="搜索 Release…"
                  />
                ) : (
                  <Input id="helm-install-release" value={releaseName} onChange={(event) => changeRelease(event.target.value)} placeholder="my-release" maxLength={53} className="font-mono" autoComplete="off" spellCheck={false} />
                )}
                {mode === "upgrade" && manualRelease ? <button type="button" className="flex items-center gap-1 text-xs text-primary hover:underline" onClick={() => setManualRelease(false)}><ArrowLeft className="h-3 w-3" />返回 Release 列表</button> : null}
                {mode === "upgrade" && releases.isError ? <p className="text-xs text-warning">无法读取该命名空间的 Release 列表，可直接输入名称。</p> : null}
              </div>
              <div className="min-w-0 space-y-1.5">
                <Label htmlFor="helm-install-version">Chart 版本</Label>
                {!manualVersion && versionOptions.length > 0 ? (
                  <SearchableSelect
                    id="helm-install-version"
                    label="Chart 版本"
                    value={version}
                    options={versionOptions}
                    onChange={changeVersion}
                    onManualSelect={() => setManualVersion(true)}
                    placeholder="选择 Chart 版本"
                    searchPlaceholder="搜索版本、appVersion…"
                  />
                ) : (
                  <Input id="helm-install-version" value={version} onChange={(event) => changeVersion(event.target.value)} maxLength={128} className="font-mono" autoComplete="off" spellCheck={false} />
                )}
                {manualVersion ? <button type="button" className="flex items-center gap-1 text-xs text-primary hover:underline" onClick={() => setManualVersion(false)}><ArrowLeft className="h-3 w-3" />返回版本列表</button> : null}
              </div>
            </div>

            <div className="min-w-0 space-y-1.5">
              <Label htmlFor="helm-install-values">Values YAML</Label>
              <Textarea id="helm-install-values" value={values} onChange={(event) => changeValues(event.target.value)} className="min-h-44 max-h-72 resize-y font-mono text-xs" spellCheck={false} />
              <p className="text-xs text-muted-foreground">默认 values 来自当前 Chart 包；服务端会先用 values.schema.json 校验合并结果，并对敏感字段做遮蔽后再返回计划。</p>
            </div>

            {error ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">{error}</p> : null}
            <DialogFooter>
              <Button variant="outline" type="button" onClick={() => onOpenChange(false)} disabled={planMutation.isPending}>取消</Button>
              <Button type="button" onClick={() => planMutation.mutate()} disabled={!canPlan}>{planMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Boxes className="h-4 w-4" />}生成安装计划</Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="min-w-0 space-y-4">
            <div className="grid min-w-0 gap-2 rounded-md border bg-muted/25 p-3 text-sm sm:grid-cols-2">
              <p className="min-w-0 break-all"><span className="text-muted-foreground">Release：</span><span className="font-medium">{plan.namespace}/{plan.releaseName}</span></p>
              <p className="min-w-0 break-all"><span className="text-muted-foreground">Chart：</span><span className="font-medium">{plan.chartName} v{plan.chartVersion}</span></p>
              <p className="min-w-0 break-all"><span className="text-muted-foreground">资源：</span>{plan.resources.length} 项 · {plan.hookCount} Hook · {plan.crdCount} CRD</p>
              {chart.schema ? <p className="min-w-0 break-all"><span className="text-muted-foreground">Values Schema：</span>已通过校验</p> : null}
              <p className="min-w-0 break-all font-mono text-xs"><span className="text-muted-foreground">包摘要：</span>{plan.digest}</p>
              <p className="min-w-0 break-all font-mono text-xs"><span className="text-muted-foreground">计划摘要：</span>{plan.planDigest}</p>
            </div>

            {plan.warnings?.length ? (
              <div role="alert" className="space-y-1 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
                {plan.warnings.map((warning, index) => <p key={index} className="flex min-w-0 items-start gap-2 break-words"><TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />{warning}</p>)}
              </div>
            ) : null}

            {plan.crdCount > 0 || plan.hookCount > 0 ? (
              <div className="flex items-start gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-muted-foreground"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-primary" /><span>已审核的计划会锁定 Chart 包摘要、Values、目标资源和当前 Revision；执行时若发生变化会拒绝写入。</span></div>
            ) : null}

            <div className="min-w-0 space-y-2">
              <p className="text-sm font-medium">渲染资源预检</p>
              <div className="grid min-w-0 gap-2 sm:grid-cols-2">
                {plan.resources.map((resource) => (
                  <div key={`${resource.apiVersion}/${resource.kind}/${resource.namespace}/${resource.name}`} className="min-w-0 rounded-md border px-3 py-2">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <Badge variant={resource.action === "create" ? "success" : "warning"}>{resource.action}</Badge>
                      <Badge variant="secondary" className="max-w-full"><span className="truncate">{resource.kind}</span></Badge>
                      {resource.hook ? <Badge variant="outline">Hook</Badge> : null}
                      {resource.crd ? <Badge variant="outline">CRD</Badge> : null}
                    </div>
                    <p className="mt-1 break-all font-mono text-xs">{resource.namespace ? `${resource.namespace}/` : ""}{resource.name}</p>
                    <p className="mt-0.5 break-all text-[11px] text-muted-foreground">{resource.apiVersion} · {resource.resource}</p>
                  </div>
                ))}
                {plan.resources.length === 0 ? <p className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">该 Chart 不包含可部署资源。</p> : null}
              </div>
            </div>

            <details className="min-w-0 rounded-md border">
              <summary className="cursor-pointer px-3 py-2 text-sm font-medium">查看脱敏后的渲染清单</summary>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words border-t bg-muted/20 p-3 text-xs">{plan.manifest || "（空）"}</pre>
            </details>

            <div className="min-w-0 space-y-1.5 rounded-md border border-warning/30 p-3">
              <Label htmlFor="helm-install-confirmation">输入 Release 名称确认</Label>
              <p className="text-xs text-muted-foreground">请输入 <code className="break-all rounded bg-muted px-1 py-0.5 font-mono">{plan.releaseName}</code> 后执行。安装会真实修改集群。</p>
              <Input id="helm-install-confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" spellCheck={false} className="font-mono" />
            </div>

            {error ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive-strong">{error}</p> : null}
            {operation ? <p className="break-words rounded-md border border-success/30 bg-success/5 px-3 py-2 text-xs text-success">操作 {operation.status}，Revision {operation.revision ?? "—"}</p> : null}
            <DialogFooter className="gap-2">
              <Button variant="outline" type="button" onClick={() => { setPlan(undefined); setError(""); }} disabled={executeMutation.isPending}><ArrowLeft className="h-4 w-4" />修改配置</Button>
              <Button type="button" onClick={() => executeMutation.mutate()} disabled={!canExecute}>{executeMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <PackageCheck className="h-4 w-4" />}{mode === "install" ? "确认安装" : "确认升级"}</Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
