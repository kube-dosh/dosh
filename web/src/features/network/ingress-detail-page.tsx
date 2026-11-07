import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { FileCode2, LockKeyhole, Pencil, Route as RouteIcon, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { IngressPath } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { YamlPanel } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ResourceEvents } from "@/features/events/resource-events";
import { useCapabilities } from "@/features/auth/use-session";
import { IngressFormDialog } from "@/features/network/ingress-form-dialog";
import { NetworkMetadataCard } from "@/features/network/network-detail-shared";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatDate } from "@/lib/utils";

const routeColumn = createColumnHelper<IngressPath>();
const ingressTabs = ["overview", "routes", "events", "yaml"] as const;
type IngressTab = (typeof ingressTabs)[number];

function isIngressTab(value: string | undefined): value is IngressTab {
  return ingressTabs.includes(value as IngressTab);
}

export function IngressDetailPage() {
  const params = useParams({ strict: false }) as { namespace: string; name: string };
  const search = useSearch({ from: "/app/network/ingresses/$namespace/$name" });
  const navigate = useNavigate();
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const tab = isIngressTab(search.tab) ? search.tab : "overview";
  const changeTab = (value: string) => {
    if (!isIngressTab(value)) return;
    void navigate({
      to: "/network/ingresses/$namespace/$name",
      params,
      search: { ...search, tab: value === "overview" ? undefined : value },
      replace: true,
    });
  };
  const query = useQuery({
    queryKey: ["ingress", params.namespace, params.name],
    queryFn: () => api.ingress(params.namespace, params.name),
    refetchInterval: 20_000,
  });
  const ingressClasses = useQuery({ queryKey: ["ingressclasses"], queryFn: () => api.ingressClasses({}) });
  const capabilities = useCapabilities(params.namespace).data;
  const ingressCaps = resourceCapabilities(capabilities, "ingresses");
  if (query.isLoading) return <LoadingState label="正在加载 Ingress 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const ingress = query.data!;
  const readyRoutes = ingress.paths.filter((route) => route.targetStatus === "Ready").length;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/network" search={{ q: search.q, namespace: search.namespace, tab: "ingresses" }} className="text-primary underline underline-offset-2">服务与路由</Link> / {ingress.namespace} / {ingress.name}</>}
        title={<span className="flex items-center gap-3"><RouteIcon className="h-5 w-5 text-muted-foreground" />{ingress.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><Badge variant="secondary">Class {ingress.class || "未指定"}</Badge><StatusBadge status={readyRoutes === ingress.paths.length && readyRoutes > 0 ? "Ready" : "NoReadyEndpoints"} /><span>{readyRoutes}/{ingress.paths.length} 条路由后端就绪</span><span>·</span><span>{ingress.namespace}</span></span>}
        actions={<div className="flex items-center gap-2">
          {ingressCaps.update ? <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}><Pencil className="h-4 w-4" />编辑</Button> : null}
          {ingressCaps.delete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}><Trash2 className="h-4 w-4" />删除</Button> : null}
        </div>}
      />
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="routes">路由 ({ingress.paths.length})</TabsTrigger>
          <TabsTrigger value="events">事件</TabsTrigger>
          <TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>Ingress 信息</CardTitle></CardHeader>
              <CardContent><dl className="grid gap-5 sm:grid-cols-2">
                <DetailItem label="名称" value={ingress.name} mono />
                <DetailItem label="命名空间" value={ingress.namespace} />
                <DetailItem label="IngressClass" value={ingress.class || "—"} mono />
                <DetailItem label="创建时间" value={formatDate(ingress.createdAt)} />
                <DetailItem label="TLS Hosts" value={ingress.tlsHosts?.join(", ") || "—"} mono />
                <DetailItem label="默认后端" value={ingress.defaultBackend ? <span className="flex flex-wrap items-center gap-2"><span className="font-mono text-xs">{ingress.defaultBackend.service}:{ingress.defaultBackend.port}</span><StatusBadge status={ingress.defaultBackend.targetStatus} /></span> : "未配置"} />
              </dl></CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="flex items-center gap-2"><LockKeyhole className="h-4 w-4" />TLS 证书</CardTitle></CardHeader>
              <CardContent>
                {ingress.tls?.length ? <div className="space-y-2">{ingress.tls.map((tls, index) => (
                  <div key={`${tls.secretName}-${index}`} className="rounded-md border px-3 py-2">
                    <p className="font-mono text-xs font-medium">{tls.secretName || "未引用 Secret"}</p>
                    <p className="mt-1 break-all font-mono text-[11px] text-muted-foreground">{tls.hosts.join(", ") || "未限制 Host"}</p>
                  </div>
                ))}</div> : <p className="py-6 text-center text-sm text-muted-foreground">未配置 TLS</p>}
              </CardContent>
            </Card>
          </div>
          <div className="mt-4"><NetworkMetadataCard labels={ingress.labels} annotations={ingress.annotations} /></div>
        </TabsContent>
        <TabsContent value="routes">
          <DataTable
            columns={[
              routeColumn.accessor("host", { header: "Host", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "*"}</span> }),
              routeColumn.accessor("path", { header: "Path", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "/"}</span> }),
              routeColumn.accessor("pathType", { header: "匹配类型", cell: (info) => info.getValue() || "—" }),
              routeColumn.accessor("service", { header: "后端 Service", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
              routeColumn.accessor("port", { header: "端口", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
              routeColumn.accessor("targetStatus", { header: "目标状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
            ]}
            data={ingress.defaultBackend ? [{ ...ingress.defaultBackend, host: "默认后端", path: "" }, ...ingress.paths] : ingress.paths}
            empty="该 Ingress 没有路由规则"
            pageSize={20}
          />
        </TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={ingress.namespace} objectKind="Ingress" objectName={ingress.name} /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type="ingress" name={ingress.name} namespace={ingress.namespace} height="64vh" /></TabsContent>
      </Tabs>
      <IngressFormDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        namespace={ingress.namespace}
        ingress={ingress}
        ingressClasses={ingressClasses.data?.items ?? []}
        onSaved={async () => { await query.refetch(); }}
      />
      <DeleteResourceDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        resourceKind="Ingress"
        name={ingress.name}
        description="删除后对应路由规则将立即失效，后端 Service 和 Pod 不会受影响。"
        onConfirm={async () => {
          await api.deleteIngress(ingress.namespace, ingress.name, ingress.resourceVersion);
          await navigate({ to: "/network", search: { ...search, tab: "ingresses" } });
        }}
      />
    </>
  );
}
