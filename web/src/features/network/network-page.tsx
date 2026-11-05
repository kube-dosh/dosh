import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Route } from "lucide-react";
import { api } from "@/api/client";
import type { IngressPath, IngressSummary, ServicePort, ServiceSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { formatAge } from "@/lib/utils";

const serviceColumn = createColumnHelper<ServiceSummary>();
const ingressColumn = createColumnHelper<IngressSummary>();

export function NetworkPage() {
  const search = useSearch({ from: "/app/network" });
  const navigate = useNavigate();
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const tab = search.tab === "ingresses" ? "ingresses" : "services";
  const updateSearch = (patch: { q?: string; namespace?: string; tab?: string }) => {
    void navigate({ to: "/network", search: { ...search, ...patch }, replace: true });
  };
  const services = useQuery({ queryKey: ["services", namespace, q], queryFn: () => api.services({ namespace, q }) });
  const ingresses = useQuery({ queryKey: ["ingresses", namespace, q], queryFn: () => api.ingresses({ namespace, q }) });
  const ingressClasses = useQuery({ queryKey: ["ingressclasses", q], queryFn: () => api.ingressClasses({ q }) });
  const error = services.error || ingresses.error || ingressClasses.error;
  if (error) return <ErrorState error={error} onRetry={() => { void services.refetch(); void ingresses.refetch(); void ingressClasses.refetch(); }} />;
  const noIngressClasses = !ingressClasses.isLoading && !ingressClasses.data?.items.length;
  const detectedClasses = ingressClasses.data?.items.map((item) => item.name).join(", ") || "—";
  return (
    <>
      <PageHeader title="服务与路由" breadcrumb="网络 / 服务与路由" description="查看集群内 Service、ClusterIP、NodePort 和 Ingress 路由规则。" />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索名称" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>
      <Tabs value={tab} onValueChange={(value) => updateSearch({ tab: value === "services" ? undefined : value })}>
        <TabsList><TabsTrigger value="services">Service ({services.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="ingresses">Ingress ({ingresses.data?.meta.total ?? 0})</TabsTrigger></TabsList>
        <TabsContent value="services">
          <DataTable
            columns={[
              serviceColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/network/services/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
              serviceColumn.accessor("namespace", { header: "命名空间" }),
              serviceColumn.accessor("type", { header: "类型", cell: (info) => <Badge variant={info.getValue() === "LoadBalancer" ? "default" : "secondary"}>{info.getValue()}</Badge> }),
              serviceColumn.accessor("clusterIP", { header: "ClusterIP", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
              serviceColumn.accessor("externalIP", { header: "外部地址", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
              serviceColumn.accessor("ports", { header: "端口", enableSorting: false, cell: (info) => <div className="flex flex-wrap gap-1">{info.getValue().map((port: ServicePort) => <span key={`${port.protocol}-${port.port}`} className="rounded border bg-muted/30 px-1.5 py-0.5 font-mono text-[11px]">{port.port}{port.nodePort ? `:${port.nodePort}` : ""}/{port.protocol}</span>)}</div> }),
              serviceColumn.accessor("selector", { header: "Selector", enableSorting: false, cell: (info) => <span className="block max-w-72 truncate font-mono text-[11px] text-muted-foreground">{Object.entries(info.getValue() || {}).map(([key, value]) => `${key}=${value}`).join(", ") || "—"}</span> }),
              serviceColumn.accessor("endpointCount", { header: "Endpoint", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
              serviceColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
            ]}
            data={services.data?.items ?? []}
            loading={services.isLoading}
            empty="当前筛选条件下没有 Service"
          />
        </TabsContent>
        <TabsContent value="ingresses">
          {ingressClasses.isLoading ? <DataTable columns={[]} data={[]} loading empty="正在加载 IngressClass" /> : noIngressClasses ? (
            <EmptyState title="未安装 Ingress Controller" description="当前集群没有 IngressClass。请先安装并注册 Ingress Controller；Dosh 仍可通过 NodePort 32626 访问。" />
          ) : !ingresses.isLoading && !ingresses.data?.items.length ? (
            <EmptyState title="没有 Ingress 资源" description={`已检测到 IngressClass：${detectedClasses}。创建 Ingress 后，路由规则会显示在此页面。`} />
          ) : (
            <DataTable
              columns={[
                ingressColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/network/ingresses/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
                ingressColumn.accessor("namespace", { header: "命名空间" }),
                ingressColumn.accessor("class", { header: "Class", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
                ingressColumn.accessor("paths", { header: "路由与目标", enableSorting: false, cell: (info) => <div className="space-y-1.5">{info.getValue().map((path: IngressPath, index: number) => <div key={index} className="flex flex-wrap items-center gap-2"><p className="font-mono text-[11px]">{path.host || "*"}{path.path} → {path.service}:{path.port}</p><StatusBadge status={path.targetStatus} /></div>)}</div> }),
                ingressColumn.accessor("tlsHosts", { header: "TLS Hosts", cell: (info) => <span className="font-mono text-[11px]">{info.getValue()?.join(", ") || "—"}</span> }),
                ingressColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              ]}
              data={ingresses.data?.items ?? []}
              loading={ingresses.isLoading}
              empty={<div className="flex flex-col items-center gap-2"><Route className="h-6 w-6" /><span>没有 Ingress</span></div>}
            />
          )}
        </TabsContent>
      </Tabs>
    </>
  );
}
