import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Archive, ArrowRight, PackageSearch, RefreshCw, Search, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useCapabilities } from "@/features/auth/use-session";
import { HelmSectionNav } from "@/features/helm/helm-section-nav";

const ALL_REPOSITORIES = "__all__";

export function HelmChartsPage() {
  const capabilities = useCapabilities().data;
  const [repositoryId, setRepositoryId] = useState(ALL_REPOSITORIES);
  const [search, setSearch] = useState("");
  const repositories = useQuery({
    queryKey: ["helm-repositories"],
    queryFn: api.helmRepositories,
    enabled: Boolean(capabilities?.helmChartRepositories),
  });
  const charts = useQuery({
    queryKey: ["helm-charts", repositoryId, search],
    queryFn: () => api.helmCharts({ repositoryId: repositoryId === ALL_REPOSITORIES ? undefined : repositoryId, q: search }),
    enabled: Boolean(capabilities?.helmChartRepositories),
    staleTime: 30_000,
  });

  if (!capabilities) return <LoadingState label="正在检查 Helm Chart 能力" />;
  if (!capabilities.helmChartRepositories) {
    return <ErrorState error={new Error("当前身份没有 list dosh.io/helm-repositories 权限，无法浏览 Chart 仓库。")} />;
  }
  if (repositories.isError) return <ErrorState error={repositories.error} onRetry={() => void repositories.refetch()} />;
  if (charts.isError) return <ErrorState error={charts.error} onRetry={() => void charts.refetch()} />;

  const enabledRepositories = repositories.data?.filter((item) => item.enabled) ?? [];
  return (
    <>
      <PageHeader
        title="Chart 目录"
        breadcrumb="应用 / Helm / Chart 目录"
        description="从管理员登记的受控仓库选择固定 Chart 版本，安装前先审阅资源、Hook、CRD 与 RBAC 预检结果。"
        actions={(
          <Button variant="outline" onClick={() => void charts.refetch()} disabled={charts.isFetching}><RefreshCw className={`h-4 w-4 ${charts.isFetching ? "animate-spin" : ""}`} />刷新目录</Button>
        )}
      />
      <HelmSectionNav active="charts" />

      <div className="mb-4 flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
        <Select value={repositoryId} onValueChange={setRepositoryId}>
          <SelectTrigger className="w-full sm:w-56" aria-label="Chart 仓库筛选"><SelectValue placeholder="全部仓库" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_REPOSITORIES}>全部仓库</SelectItem>
            {enabledRepositories.map((repository) => <SelectItem key={repository.id} value={repository.id}>{repository.name}</SelectItem>)}
          </SelectContent>
        </Select>
        <SearchInput value={search} onChange={setSearch} placeholder="搜索 Chart 名称或说明" className="sm:w-80" />
        <span className="text-xs text-muted-foreground sm:ml-auto">{charts.isLoading ? "正在同步仓库索引" : `共 ${charts.data?.items.length ?? 0} 个 Chart`}</span>
      </div>

      {charts.data?.warnings?.length ? (
        <div role="status" className="mb-4 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span className="min-w-0 break-words">{charts.data.warnings.join("；")}</span>
        </div>
      ) : null}

      {enabledRepositories.length === 0 && !repositories.isLoading ? (
        <EmptyState
          title="尚未启用 Chart 仓库"
          description="先登记受控 Chart 仓库，Dosh 会同步 index.yaml 并提供可搜索的 Chart 目录。"
          action={<Button asChild><Link to="/helm/repositories"><Archive className="h-4 w-4" />配置 Chart 仓库</Link></Button>}
        />
      ) : charts.isLoading ? (
        <LoadingState label="正在读取 Chart 目录" />
      ) : (charts.data?.items.length ?? 0) === 0 ? (
        <EmptyState
          title={search ? "没有匹配的 Chart" : "仓库中没有可用 Chart"}
          description={search ? "尝试更换关键字或选择其他仓库。" : "同步仓库后，有效的 index.yaml 条目会显示在这里。"}
          action={search ? <Button variant="outline" onClick={() => setSearch("")}>清空搜索</Button> : <Button variant="outline" asChild><Link to="/helm/repositories">检查仓库同步状态</Link></Button>}
        />
      ) : (
        <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {charts.data?.items.map((chart) => (
            <Card key={`${chart.repositoryId}/${chart.name}`} className="flex min-w-0 flex-col transition-colors hover:border-primary/40">
              <CardHeader className="min-w-0 gap-2 p-4 sm:p-5">
                <div className="flex min-w-0 items-start gap-3">
                  <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-primary/10 text-primary"><PackageSearch className="h-4 w-4" /></div>
                  <div className="min-w-0 flex-1">
                    <CardTitle className="break-all text-base leading-5">{chart.name}</CardTitle>
                    <CardDescription className="mt-1 break-all text-xs">{chart.repositoryName}</CardDescription>
                  </div>
                  {chart.deprecated ? <Badge variant="destructive">Deprecated</Badge> : null}
                </div>
              </CardHeader>
              <CardContent className="min-w-0 flex-1 px-4 pb-3 sm:px-5">
                <p className="line-clamp-3 min-h-[3.75rem] break-words text-sm text-muted-foreground">{chart.description || "该 Chart 未提供说明。"}</p>
                <div className="mt-3 flex flex-wrap items-center gap-1.5">
                  <Badge variant="secondary" className="max-w-full font-mono"><span className="truncate">v{chart.version}</span></Badge>
                  {chart.appVersion ? <Badge variant="outline" className="max-w-full font-mono"><span className="truncate">app {chart.appVersion}</span></Badge> : null}
                </div>
              </CardContent>
              <CardFooter className="min-w-0 border-t px-4 py-3 sm:px-5">
                <Button asChild variant="ghost" className="h-8 w-full justify-between px-2 text-primary">
                  <Link to="/helm/charts/$repositoryId/$chartName" params={{ repositoryId: chart.repositoryId, chartName: chart.name }}>
                    查看详情与安装计划<ArrowRight className="h-4 w-4" />
                  </Link>
                </Button>
              </CardFooter>
            </Card>
          ))}
        </div>
      )}

      {search ? (
        <p className="mt-4 flex items-center gap-1 text-xs text-muted-foreground"><Search className="h-3.5 w-3.5" />搜索仅匹配当前已同步的索引；不会发起任意远程请求。</p>
      ) : null}
    </>
  );
}
