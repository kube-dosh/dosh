import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeft, ExternalLink, GitBranch, PackageCheck, PackageSearch, RefreshCw, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCapabilities } from "@/features/auth/use-session";
import { formatDate } from "@/lib/utils";
import { HelmInstallDialog } from "./helm-install-dialog";

export function HelmChartDetailPage() {
  const params = useParams({ strict: false }) as { repositoryId: string; chartName: string };
  const capabilities = useCapabilities().data;
  const queryClient = useQueryClient();
  const [installOpen, setInstallOpen] = useState(false);
  const chartQueryKey = ["helm-chart", params.repositoryId, params.chartName] as const;
  const detail = useQuery({
    queryKey: chartQueryKey,
    queryFn: () => api.helmChart(params.repositoryId, params.chartName),
    enabled: Boolean(capabilities?.helmChartRepositories && params.repositoryId && params.chartName),
  });
  const sync = useMutation({
    mutationFn: () => api.syncHelmChart(params.repositoryId, params.chartName),
    onSuccess: (updated) => {
      queryClient.setQueryData(chartQueryKey, updated);
      toast.success("Chart 包已重新同步，README、Values 与 Schema 已刷新");
    },
    onError: (error) => toast.error(`Chart 包同步失败：${error.message}`),
  });

  if (!capabilities) return <LoadingState label="正在检查 Helm Chart 能力" />;
  if (!capabilities.helmChartRepositories) {
    return <ErrorState error={new Error("当前身份没有 list dosh.io/helm-repositories 权限，无法查看 Chart 详情。")} />;
  }
  if (detail.isLoading) return <LoadingState label="正在读取 Chart 详情" />;
  if (detail.isError) return <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />;
  const chart = detail.data;
  const canSyncChart = Boolean(capabilities.helmChartWrite);
  if (!chart) return <EmptyState title="Chart 不存在" description="该 Chart 可能已被仓库移除或停用。" />;

  return (
    <>
      <PageHeader
        title={chart.name}
        breadcrumb={<Link to="/helm/charts" className="inline-flex items-center gap-1 text-primary hover:underline"><ArrowLeft className="h-3.5 w-3.5" />返回 Chart 目录</Link>}
        description={<span className="break-words">来自 {chart.repositoryName} · 最新版本 v{chart.version}{chart.appVersion ? ` · app ${chart.appVersion}` : ""}</span>}
        actions={<>
          <Button variant="outline" onClick={() => sync.mutate()} disabled={sync.isPending || !canSyncChart} title={canSyncChart ? undefined : "需要 Chart 仓库更新权限"}><RefreshCw className={`h-4 w-4 ${sync.isPending ? "animate-spin" : ""}`} />{sync.isPending ? "同步中" : "同步 Chart 包"}</Button>
          <Button onClick={() => setInstallOpen(true)}><PackageCheck className="h-4 w-4" />安装 / 升级</Button>
        </>}
      />

      {chart.deprecated ? (
        <div role="alert" className="mb-4 flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive-strong"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><span className="break-words">该 Chart 已被上游标记为 Deprecated，请评估替代方案后再安装。</span></div>
      ) : null}
      {chart.warnings?.length ? (
        <div role="status" className="mb-4 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{chart.warnings.join("；")}</span></div>
      ) : null}

      <div className="mb-4 grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,18rem)]">
        <Card className="min-w-0">
          <CardHeader className="p-4 sm:p-5"><CardTitle className="text-base">Chart 说明</CardTitle></CardHeader>
          <CardContent className="min-w-0 space-y-4 px-4 pb-4 text-sm sm:px-5">
            <p className="break-words text-muted-foreground">{chart.description || "该 Chart 未提供 description。"}</p>
            <div className="flex flex-wrap gap-1.5">
              <Badge variant="secondary" className="max-w-full font-mono"><span className="truncate">v{chart.version}</span></Badge>
              {chart.appVersion ? <Badge variant="outline" className="max-w-full font-mono"><span className="truncate">app {chart.appVersion}</span></Badge> : null}
              {(chart.keywords ?? []).slice(0, 8).map((keyword) => <Badge key={keyword} variant="outline" className="max-w-full"><span className="truncate">{keyword}</span></Badge>)}
            </div>
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="min-w-0"><p className="text-xs text-muted-foreground">仓库</p><p className="mt-0.5 break-all font-medium">{chart.repositoryName}</p></div>
              <div className="min-w-0"><p className="text-xs text-muted-foreground">发布时间</p><p className="mt-0.5 break-all">{chart.createdAt ? formatDate(chart.createdAt) : "—"}</p></div>
              <div className="min-w-0"><p className="text-xs text-muted-foreground">Chart 摘要</p><p className="mt-0.5 break-all font-mono text-xs">{chart.digest || "索引未提供 digest"}</p></div>
              <div className="min-w-0"><p className="text-xs text-muted-foreground">维护者</p><p className="mt-0.5 break-words">{chart.maintainers?.length ? chart.maintainers.map((item) => item.name).join("、") : "—"}</p></div>
            </div>
            {(chart.home || chart.sources?.length) ? (
              <div className="flex min-w-0 flex-wrap gap-3 text-xs">
                {chart.home ? <a href={chart.home} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-1 break-all text-primary hover:underline"><ExternalLink className="h-3.5 w-3.5 shrink-0" />项目主页</a> : null}
                {chart.sources?.[0] ? <a href={chart.sources[0]} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-1 break-all text-primary hover:underline"><GitBranch className="h-3.5 w-3.5 shrink-0" />源码</a> : null}
              </div>
            ) : null}
          </CardContent>
        </Card>
        <Card className="min-w-0">
          <CardHeader className="p-4 sm:p-5"><CardTitle className="text-base">安装前检查</CardTitle><CardDescription>计划阶段不会写入集群。</CardDescription></CardHeader>
          <CardContent className="min-w-0 space-y-2 px-4 pb-4 text-sm sm:px-5">
            <p className="break-words text-muted-foreground">Dosh 会校验 Chart 包 SHA-256，渲染资源并逐项检查目标命名空间的 create/update/patch 权限。</p>
            <p className="break-words text-xs text-muted-foreground">Chart 包按版本缓存在服务端；需要重新获取当前版本或刷新 README、Values 时，请使用“同步 Chart 包”。</p>
            <div className="flex flex-wrap gap-1.5"><Badge variant="outline">固定版本</Badge><Badge variant="outline">摘要校验</Badge><Badge variant="outline">RBAC 预检</Badge>{chart.schema ? <Badge variant="outline">Schema 校验</Badge> : null}<Badge variant="outline">操作审计</Badge></div>
            <Button className="mt-2 w-full" onClick={() => setInstallOpen(true)}><PackageSearch className="h-4 w-4" />打开安装计划</Button>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="versions" className="min-w-0">
        <TabsList className="w-full justify-start">
          <TabsTrigger value="versions">版本</TabsTrigger>
          <TabsTrigger value="values">默认 Values</TabsTrigger>
          <TabsTrigger value="readme">README</TabsTrigger>
          <TabsTrigger value="dependencies">依赖</TabsTrigger>
          <TabsTrigger value="schema">Schema</TabsTrigger>
        </TabsList>
        <TabsContent value="versions" className="min-w-0">
          <Card className="min-w-0"><CardContent className="min-w-0 divide-y p-0">
            {chart.versions.map((item) => (
              <div key={item.version} className="flex min-w-0 flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0"><div className="flex flex-wrap items-center gap-1.5"><span className="break-all font-mono text-sm">v{item.version}</span>{item.appVersion ? <Badge variant="outline">app {item.appVersion}</Badge> : null}{item.deprecated ? <Badge variant="destructive">Deprecated</Badge> : null}</div><p className="mt-1 break-all font-mono text-[11px] text-muted-foreground">{item.digest || "无 digest"}</p></div>
                <p className="shrink-0 text-xs text-muted-foreground">{item.createdAt ? formatDate(item.createdAt) : "时间未知"}</p>
              </div>
            ))}
          </CardContent></Card>
        </TabsContent>
        <TabsContent value="values" className="min-w-0"><Card className="min-w-0"><CardContent className="p-0"><pre className="max-h-[65vh] overflow-auto whitespace-pre-wrap break-words p-4 text-xs">{chart.values || "{}"}</pre></CardContent></Card></TabsContent>
        <TabsContent value="readme" className="min-w-0"><Card className="min-w-0"><CardContent className="p-0"><pre className="max-h-[65vh] overflow-auto whitespace-pre-wrap break-words p-4 text-xs leading-5">{chart.readme || "该 Chart 未提供 README.md。"}</pre></CardContent></Card></TabsContent>
        <TabsContent value="dependencies" className="min-w-0">
          <Card className="min-w-0"><CardContent className="min-w-0 p-4">
            {chart.dependencies?.length ? <div className="space-y-2">{chart.dependencies.map((dependency, index) => <div key={`${dependency.name}-${index}`} className="min-w-0 rounded-md border px-3 py-2"><p className="break-all font-mono text-sm">{dependency.name} {dependency.version}</p>{dependency.repository ? <p className="mt-1 break-all text-xs text-muted-foreground">{dependency.repository}</p> : null}</div>)}</div> : <p className="text-sm text-muted-foreground">该 Chart 没有声明子 Chart 依赖。</p>}
          </CardContent></Card>
        </TabsContent>
        <TabsContent value="schema" className="min-w-0">
          <Card className="min-w-0">
            <CardContent className="min-w-0 p-0">
              {chart.schema ? <p className="border-b px-4 py-3 text-xs text-muted-foreground">生成安装或升级计划时，服务端会使用该 JSON Schema 校验合并后的 Values；校验失败会阻止执行。</p> : null}
              <pre className="max-h-[65vh] overflow-auto whitespace-pre-wrap break-words p-4 text-xs">{chart.schema || "该 Chart 未提供 values.schema.json。"}</pre>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <HelmInstallDialog chart={chart} open={installOpen} onOpenChange={setInstallOpen} />
    </>
  );
}
