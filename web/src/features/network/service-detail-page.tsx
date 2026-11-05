import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { FileCode2, Network } from "lucide-react";
import { api } from "@/api/client";
import type { ServiceEndpoint } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { YamlPanel } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ResourceEvents } from "@/features/events/resource-events";
import { NetworkMetadataCard } from "@/features/network/network-detail-shared";
import { formatDate } from "@/lib/utils";

const endpointColumn = createColumnHelper<ServiceEndpoint>();
const serviceTabs = ["overview", "endpoints", "events", "yaml"] as const;
type ServiceTab = (typeof serviceTabs)[number];

function isServiceTab(value: string | undefined): value is ServiceTab {
  return serviceTabs.includes(value as ServiceTab);
}

export function ServiceDetailPage() {
  const params = useParams({ strict: false }) as { namespace: string; name: string };
  const search = useSearch({ from: "/app/network/services/$namespace/$name" });
  const navigate = useNavigate();
  const tab = isServiceTab(search.tab) ? search.tab : "overview";
  const changeTab = (value: string) => {
    if (!isServiceTab(value)) return;
    void navigate({
      to: "/network/services/$namespace/$name",
      params,
      search: { ...search, tab: value === "overview" ? undefined : value },
      replace: true,
    });
  };
  const query = useQuery({
    queryKey: ["service", params.namespace, params.name],
    queryFn: () => api.service(params.namespace, params.name),
    refetchInterval: 20_000,
  });
  if (query.isLoading) return <LoadingState label="正在加载 Service 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const service = query.data!;
  return (
    <>
      <PageHeader
        breadcrumb={<><Link to="/network" search={{ q: search.q, namespace: search.namespace, tab: "services" }} className="text-primary underline underline-offset-2">服务与路由</Link> / {service.namespace} / {service.name}</>}
        title={<span className="flex items-center gap-3"><Network className="h-5 w-5 text-muted-foreground" />{service.name}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><Badge variant="secondary">{service.type}</Badge><StatusBadge status={service.endpointCount > 0 ? "Ready" : "NoReadyEndpoints"} /><span>{service.endpointCount} 个就绪 Endpoint</span><span>·</span><span>{service.namespace}</span></span>}
        actions={<span className="text-xs text-muted-foreground"><FileCode2 className="mr-1 inline h-3.5 w-3.5" />YAML 可在下方标签查看</span>}
      />
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="endpoints">Endpoint ({service.endpoints.length})</TabsTrigger>
          <TabsTrigger value="events">事件</TabsTrigger>
          <TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>Service 信息</CardTitle></CardHeader>
              <CardContent><dl className="grid gap-5 sm:grid-cols-2">
                <DetailItem label="名称" value={service.name} mono />
                <DetailItem label="命名空间" value={service.namespace} />
                <DetailItem label="类型" value={service.type} />
                <DetailItem label="ClusterIP" value={service.clusterIP} mono />
                <DetailItem label="外部地址" value={service.externalIP || "—"} mono />
                <DetailItem label="ExternalName" value={service.externalName || "—"} mono />
                <DetailItem label="会话保持" value={service.sessionAffinity || "None"} />
                <DetailItem label="IP 族" value={service.ipFamilies.join(", ") || "—"} />
                <DetailItem label="IP 族策略" value={service.ipFamilyPolicy || "—"} />
                <DetailItem label="LoadBalancer Class" value={service.loadBalancerClass || "—"} />
                <DetailItem label="创建时间" value={formatDate(service.createdAt)} />
              </dl></CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle>端口与 Selector</CardTitle></CardHeader>
              <CardContent className="space-y-5">
                <div className="space-y-2">
                  {service.ports.map((port) => (
                    <div key={`${port.name}-${port.protocol}-${port.port}`} className="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm">
                      <span className="font-medium">{port.name || "未命名"}</span>
                      <span className="font-mono text-xs text-muted-foreground">{port.port} → {port.targetPort}/{port.protocol}{port.nodePort ? ` · NodePort ${port.nodePort}` : ""}</span>
                    </div>
                  ))}
                </div>
                <div>
                  <p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Selector</p>
                  <div className="flex flex-wrap gap-1.5">
                    {Object.entries(service.selector || {}).map(([key, value]) => <Badge key={key} variant="outline" className="font-mono text-[11px]">{key}={value}</Badge>)}
                    {!Object.keys(service.selector || {}).length ? <span className="text-sm text-muted-foreground">无 Selector</span> : null}
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
          <div className="mt-4"><NetworkMetadataCard labels={service.labels} annotations={service.annotations} /></div>
        </TabsContent>
        <TabsContent value="endpoints">
          <DataTable
            columns={[
              endpointColumn.accessor("addresses", { header: "地址", cell: (info) => <span className="font-mono text-xs">{info.getValue().join(", ")}</span> }),
              endpointColumn.accessor("ready", { header: "状态", cell: (info) => <StatusBadge status={info.getValue() ? "Ready" : "NotReady"} /> }),
              endpointColumn.accessor("targetKind", { header: "目标类型", cell: (info) => info.getValue() || "—" }),
              endpointColumn.accessor("targetName", { header: "目标资源", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
              endpointColumn.accessor("targetNamespace", { header: "目标命名空间", cell: (info) => info.getValue() || "—" }),
              endpointColumn.accessor("nodeName", { header: "节点", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
            ]}
            data={service.endpoints}
            empty="当前 Service 没有 EndpointSlice 地址"
            pageSize={20}
          />
        </TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={service.namespace} objectKind="Service" objectName={service.name} /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type="service" name={service.name} namespace={service.namespace} height="64vh" /></TabsContent>
      </Tabs>
    </>
  );
}
