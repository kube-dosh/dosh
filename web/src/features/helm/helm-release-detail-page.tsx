import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Braces, FileCode2, History, Package } from "lucide-react";
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
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { formatDate } from "@/lib/utils";

const historyColumn = createColumnHelper<HelmReleaseSummary>();
const helmTabs = ["overview", "values", "manifest", "history"] as const;
type HelmTab = (typeof helmTabs)[number];

function isHelmTab(value: string | undefined): value is HelmTab {
  return helmTabs.includes(value as HelmTab);
}

export function HelmReleaseDetailPage() {
  const params = useParams({ strict: false }) as { namespace: string; name: string };
  const search = useSearch({ from: "/app/helm/releases/$namespace/$name" });
  const navigate = useNavigate();
  const capabilities = useCapabilities().data;
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
  if (!capabilities) return <LoadingState label="正在检查 Helm 读取能力" />;
  if (!capabilities.helmReleases) return <ErrorState error={new Error("当前身份没有 list secrets 权限，无法读取 Helm Release。")} />;
  if (query.isLoading) return <LoadingState label="正在加载 Helm Release" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const release = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/helm/releases" search={{ namespace: release.namespace }} className="text-primary underline underline-offset-2">Helm Releases</Link> / {release.namespace} / {release.name}</>}
        title={<span className="flex min-w-0 items-center gap-2"><Package className="h-5 w-5 shrink-0 text-muted-foreground" /><span className="truncate">{release.name}</span></span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={release.status} /><Badge variant="secondary" className="font-mono">revision #{release.revision}</Badge><span>{release.chart} {release.chartVersion}</span></span>}
      />
      {release.warnings?.length ? <div role="status" className="mb-3 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><span className="min-w-0 break-words">{release.warnings.join("；")}</span></div> : null}
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="values"><Braces className="h-3.5 w-3.5" />Values</TabsTrigger><TabsTrigger value="manifest"><FileCode2 className="h-3.5 w-3.5" />Manifest</TabsTrigger><TabsTrigger value="history"><History className="h-3.5 w-3.5" />历史 ({release.history.length})</TabsTrigger></TabsList>
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
        <TabsContent value="history">
          <DataTable
            columns={[
              historyColumn.accessor("revision", { header: "Revision", cell: (info) => <span className="font-mono text-xs font-semibold">#{info.getValue()}</span> }),
              historyColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
              historyColumn.accessor("chart", { header: "Chart", cell: (info) => <span className="break-all font-mono text-xs">{info.getValue()} {info.row.original.chartVersion}</span> }),
              historyColumn.accessor("description", { header: "说明", cell: (info) => <span className="block max-w-64 truncate text-xs text-muted-foreground" title={info.getValue()}>{info.getValue() || "—"}</span> }),
              historyColumn.accessor("updatedAt", { header: "更新时间", cell: (info) => <span className="whitespace-nowrap text-xs text-muted-foreground">{formatDate(info.getValue())}</span> }),
            ]}
            data={release.history}
            stickyFirstColumn={false}
            empty={<EmptyState title="没有历史版本" />}
          />
        </TabsContent>
      </Tabs>
    </>
  );
}
