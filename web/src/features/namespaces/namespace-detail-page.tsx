import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { FileCode2, FolderTree, Gauge, ShieldCheck, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { LimitRange, LimitRangeItem, ResourceQuota } from "@/api/types";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { MetadataCard } from "@/components/resources/metadata-card";
import { YamlPanel } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ResourceEvents } from "@/features/events/resource-events";
import { useCapabilities } from "@/features/auth/use-session";
import { NamespaceDeleteDialog } from "@/features/namespaces/namespace-delete-dialog";
import { formatDate } from "@/lib/utils";

const namespaceTabs = ["overview", "quotas", "limits", "events", "yaml"] as const;
type NamespaceTab = (typeof namespaceTabs)[number];

function isNamespaceTab(value: string | undefined): value is NamespaceTab {
  return namespaceTabs.includes(value as NamespaceTab);
}

function quotaResources(quota: ResourceQuota): string[] {
  return Array.from(new Set([...Object.keys(quota.hard || {}), ...Object.keys(quota.used || {})])).sort();
}

function QuotaCard({ quota }: { quota: ResourceQuota }) {
  const resources = quotaResources(quota);
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2"><Gauge className="h-4 w-4 text-muted-foreground" />{quota.name}</CardTitle>
          <span className="text-xs text-muted-foreground">创建于 {formatDate(quota.createdAt)}</span>
        </div>
      </CardHeader>
      <CardContent className="px-0 pb-0">
        {resources.length ? (
          <Table>
            <TableHeader>
              <TableRow><TableHead>资源</TableHead><TableHead>已使用</TableHead><TableHead>硬限制</TableHead></TableRow>
            </TableHeader>
            <TableBody>
              {resources.map((resource) => (
                <TableRow key={resource}>
                  <TableCell className="font-mono text-xs">{resource}</TableCell>
                  <TableCell className="font-mono text-xs">{quota.used?.[resource] || "0"}</TableCell>
                  <TableCell className="font-mono text-xs font-medium">{quota.hard?.[resource] || "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : <p className="px-5 py-8 text-center text-sm text-muted-foreground">该 ResourceQuota 没有状态数据</p>}
      </CardContent>
    </Card>
  );
}

function ResourceValues({ values }: { values?: Record<string, string> }) {
  const entries = Object.entries(values || {});
  if (!entries.length) return <span className="text-muted-foreground">—</span>;
  return <span className="flex flex-wrap gap-1.5">{entries.map(([key, value]) => <Badge key={key} variant="secondary" className="font-mono">{key}={value}</Badge>)}</span>;
}

function LimitItem({ limit }: { limit: LimitRangeItem }) {
  return (
    <div className="rounded-md border p-4">
      <div className="mb-3"><Badge>{limit.type}</Badge></div>
      <dl className="grid gap-3 sm:grid-cols-2">
        <DetailItem label="最小资源" value={<ResourceValues values={limit.min} />} />
        <DetailItem label="最大资源" value={<ResourceValues values={limit.max} />} />
        <DetailItem label="默认限制" value={<ResourceValues values={limit.default} />} />
        <DetailItem label="默认请求" value={<ResourceValues values={limit.defaultRequest} />} />
      </dl>
    </div>
  );
}

function LimitRangeCard({ limitRange }: { limitRange: LimitRange }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-muted-foreground" />{limitRange.name}</CardTitle>
          <span className="text-xs text-muted-foreground">创建于 {formatDate(limitRange.createdAt)}</span>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {limitRange.limits.length ? limitRange.limits.map((limit, index) => <LimitItem key={`${limit.type}-${index}`} limit={limit} />) : <p className="py-6 text-center text-sm text-muted-foreground">该 LimitRange 没有限制项</p>}
      </CardContent>
    </Card>
  );
}

export function NamespaceDetailPage() {
  const params = useParams({ strict: false }) as { name: string };
  const search = useSearch({ from: "/app/namespaces/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const tab = isNamespaceTab(search.tab) ? search.tab : "overview";
  const query = useQuery({ queryKey: ["namespace", params.name], queryFn: () => api.namespace(params.name), refetchInterval: 20_000 });
  const capabilities = useCapabilities().data;
  const protectedNamespace = params.name === "default" || params.name.startsWith("kube-");
  const canDelete = Boolean(capabilities?.deleteNamespace) && !protectedNamespace;
  if (query.isLoading) return <LoadingState label="正在加载命名空间详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const namespace = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/namespaces" search={{ q: search.q }} className="text-primary underline underline-offset-2">命名空间</Link> / {namespace.name}</>}
        title={<span className="flex items-center gap-3"><FolderTree className="h-5 w-5 text-muted-foreground" />{namespace.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={namespace.status} /><span>{namespace.resourceQuotas.length} 个 ResourceQuota</span><span>·</span><span>{namespace.limitRanges.length} 个 LimitRange</span></span>}
        actions={canDelete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}><Trash2 className="h-4 w-4" />删除命名空间</Button> : undefined}
      />
      <Tabs value={tab} onValueChange={(value) => {
        if (!isNamespaceTab(value)) return;
        void navigate({ to: "/namespaces/$name", params, search: { ...search, tab: value === "overview" ? undefined : value }, replace: true });
      }}>
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="quotas">资源配额 ({namespace.resourceQuotas.length})</TabsTrigger>
          <TabsTrigger value="limits">限制范围 ({namespace.limitRanges.length})</TabsTrigger>
          <TabsTrigger value="events">事件</TabsTrigger>
          <TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>命名空间信息</CardTitle></CardHeader>
              <CardContent><dl className="grid gap-5 sm:grid-cols-2">
                <DetailItem label="名称" value={namespace.name} mono />
                <DetailItem label="状态" value={<StatusBadge status={namespace.status} />} />
                <DetailItem label="标签数" value={String(Object.keys(namespace.labels || {}).length)} />
                <DetailItem label="注解数" value={String(Object.keys(namespace.annotations || {}).length)} />
                <DetailItem label="创建时间" value={formatDate(namespace.createdAt)} />
              </dl></CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle>资源治理</CardTitle></CardHeader>
              <CardContent><dl className="grid gap-5 sm:grid-cols-2">
                <DetailItem label="ResourceQuota" value={`${namespace.resourceQuotas.length} 个`} />
                <DetailItem label="LimitRange" value={`${namespace.limitRanges.length} 个`} />
                <DetailItem label="配额对象" value={namespace.resourceQuotas.map((item) => item.name).join(", ") || "未配置"} mono />
                <DetailItem label="限制对象" value={namespace.limitRanges.map((item) => item.name).join(", ") || "未配置"} mono />
              </dl></CardContent>
            </Card>
          </div>
          <div className="mt-4"><MetadataCard labels={namespace.labels} annotations={namespace.annotations} /></div>
        </TabsContent>
        <TabsContent value="quotas">
          {namespace.resourceQuotas.length ? <div className="space-y-4">{namespace.resourceQuotas.map((quota) => <QuotaCard key={quota.name} quota={quota} />)}</div> : <Card><CardContent><p className="py-10 text-center text-sm text-muted-foreground">该命名空间未配置 ResourceQuota。</p></CardContent></Card>}
        </TabsContent>
        <TabsContent value="limits">
          {namespace.limitRanges.length ? <div className="space-y-4">{namespace.limitRanges.map((limitRange) => <LimitRangeCard key={limitRange.name} limitRange={limitRange} />)}</div> : <Card><CardContent><p className="py-10 text-center text-sm text-muted-foreground">该命名空间未配置 LimitRange。</p></CardContent></Card>}
        </TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={namespace.name} objectKind="Namespace" objectName={namespace.name} /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type="namespace" name={namespace.name} height="64vh" /></TabsContent>
      </Tabs>
      <NamespaceDeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        name={namespace.name}
        resourceVersion={namespace.resourceVersion}
        onDeleted={async () => {
          await queryClient.invalidateQueries({ queryKey: ["namespaces"] });
          await navigate({ to: "/namespaces", search: {} });
        }}
      />
    </>
  );
}
