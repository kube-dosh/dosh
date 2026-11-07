import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Eye, FileCode2, Pencil, Plus, Route, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { IngressDetail, IngressPath, IngressSummary, ServicePort, ServiceSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ActionTooltip } from "@/components/feedback/action-tooltip";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCapabilities } from "@/features/auth/use-session";
import { IngressFormDialog } from "@/features/network/ingress-form-dialog";
import { ServiceFormDialog } from "@/features/workloads/service-form-dialog";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge } from "@/lib/utils";

const serviceColumn = createColumnHelper<ServiceSummary>();
const ingressColumn = createColumnHelper<IngressSummary>();
const EMPTY_SELECTOR: Record<string, string> = {};

type YamlTarget = { type: "service" | "ingress"; namespace: string; name: string };

export function NetworkPage() {
  const search = useSearch({ from: "/app/network" });
  const navigate = useNavigate();
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const tab = search.tab === "ingresses" ? "ingresses" : "services";
  const [serviceFormOpen, setServiceFormOpen] = useState(false);
  const [editingService, setEditingService] = useState<ServiceSummary>();
  const [deletingService, setDeletingService] = useState<ServiceSummary>();
  const [ingressFormOpen, setIngressFormOpen] = useState(false);
  const [editingIngress, setEditingIngress] = useState<IngressDetail>();
  const [deletingIngress, setDeletingIngress] = useState<IngressSummary>();
  const [yamlTarget, setYamlTarget] = useState<YamlTarget>();
  const updateSearch = (patch: { q?: string; namespace?: string; tab?: string }) => {
    void navigate({ to: "/network", search: { ...search, ...patch }, replace: true });
  };
  const services = useQuery({ queryKey: ["services", namespace, q], queryFn: () => api.services({ namespace, q }) });
  const ingresses = useQuery({ queryKey: ["ingresses", namespace, q], queryFn: () => api.ingresses({ namespace, q }) });
  const ingressClasses = useQuery({ queryKey: ["ingressclasses", q], queryFn: () => api.ingressClasses({ q }) });
  const capabilities = useCapabilities(namespace || undefined).data;
  const serviceCaps = resourceCapabilities(capabilities, "services");
  const ingressCaps = resourceCapabilities(capabilities, "ingresses");
  const error = services.error || ingresses.error || ingressClasses.error;
  if (error) return <ErrorState error={error} onRetry={() => { void services.refetch(); void ingresses.refetch(); void ingressClasses.refetch(); }} />;
  const noIngressClasses = !ingressClasses.isLoading && !ingressClasses.data?.items.length;
  const serviceCreateHint = !namespace ? "请先选择命名空间，再创建 Service" : !serviceCaps.create ? "当前身份没有创建 Service 的权限" : undefined;
  const ingressCreateHint = !namespace ? "请先选择命名空间，再创建 Ingress" : noIngressClasses ? "集群未安装 Ingress Controller" : !ingressCaps.create ? "当前身份没有创建 Ingress 的权限" : undefined;
  const detectedClasses = ingressClasses.data?.items.map((item) => item.name).join(", ") || "—";
  const refreshServices = async () => { await services.refetch(); };
  const refreshIngresses = async () => { await ingresses.refetch(); };
  const openServiceEdit = (service: ServiceSummary) => { setEditingService(service); setServiceFormOpen(true); };
  const openIngressEdit = async (ingress: IngressSummary) => {
    try {
      setEditingIngress(await api.ingress(ingress.namespace, ingress.name));
      setIngressFormOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "加载 Ingress 失败");
    }
  };
  const serviceActions = (service: ServiceSummary): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/network/services/$namespace/$name", params: { namespace: service.namespace, name: service.name } }) },
    { label: "编辑 Service", icon: <Pencil className="h-4 w-4" />, onSelect: () => openServiceEdit(service), disabled: !serviceCaps.update },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "service", namespace: service.namespace, name: service.name }) },
    { label: "删除 Service", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingService(service), destructive: true, disabled: !serviceCaps.delete, separatorBefore: true },
  ];
  const ingressActions = (ingress: IngressSummary): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/network/ingresses/$namespace/$name", params: { namespace: ingress.namespace, name: ingress.name } }) },
    { label: "编辑 Ingress", icon: <Pencil className="h-4 w-4" />, onSelect: () => void openIngressEdit(ingress), disabled: !ingressCaps.update },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget({ type: "ingress", namespace: ingress.namespace, name: ingress.name }) },
    { label: "删除 Ingress", icon: <Trash2 className="h-4 w-4" />, onSelect: () => setDeletingIngress(ingress), destructive: true, disabled: !ingressCaps.delete, separatorBefore: true },
  ];
  return (
    <>
      <PageHeader
        title="服务与路由"
        breadcrumb="网络 / 服务与路由"
        description="管理集群内 Service、ClusterIP、NodePort 和 Ingress 路由规则。"
        actions={tab === "services" ? (
          <ActionTooltip content={serviceCreateHint}><Button onClick={() => { setEditingService(undefined); setServiceFormOpen(true); }} disabled={Boolean(serviceCreateHint)}><Plus className="h-4 w-4" />创建 Service</Button></ActionTooltip>
        ) : (
          <ActionTooltip content={ingressCreateHint}><Button onClick={() => { setEditingIngress(undefined); setIngressFormOpen(true); }} disabled={Boolean(ingressCreateHint)}><Plus className="h-4 w-4" />创建 Ingress</Button></ActionTooltip>
        )}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索名称" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /></div>
      <Tabs value={tab} onValueChange={(value) => updateSearch({ tab: value === "services" ? undefined : value })}>
        <TabsList><TabsTrigger value="services">Service ({services.data?.meta.total ?? 0})</TabsTrigger><TabsTrigger value="ingresses">Ingress ({ingresses.data?.meta.total ?? 0})</TabsTrigger></TabsList>
        <TabsContent value="services">
          <DataTable
            columns={[
              serviceColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/network/services/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
              serviceColumn.accessor("namespace", { header: "命名空间" }),
              serviceColumn.accessor("type", { header: "类型", cell: (info) => <Badge variant={info.row.original.headless ? "outline" : info.getValue() === "LoadBalancer" ? "default" : "secondary"}>{info.row.original.headless ? "Headless" : info.getValue()}</Badge> }),
              serviceColumn.accessor("clusterIP", { header: "ClusterIP", cell: (info) => <span className="font-mono text-xs">{info.row.original.headless ? "None" : info.getValue()}</span> }),
              serviceColumn.accessor("externalIP", { header: "外部地址", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
              serviceColumn.accessor("ports", { header: "端口", enableSorting: false, cell: (info) => <div className="flex flex-wrap gap-1">{info.getValue().map((port: ServicePort) => <span key={`${port.protocol}-${port.port}`} className="rounded border bg-muted/30 px-1.5 py-0.5 font-mono text-[11px]">{port.port}{port.nodePort ? `:${port.nodePort}` : ""}/{port.protocol}</span>)}</div> }),
              serviceColumn.accessor("selector", { header: "Selector", enableSorting: false, cell: (info) => <span className="block max-w-72 truncate font-mono text-[11px] text-muted-foreground">{Object.entries(info.getValue() || {}).map(([key, value]) => `${key}=${value}`).join(", ") || "—"}</span> }),
              serviceColumn.accessor("endpointCount", { header: "Endpoint", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
              serviceColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) }),
              serviceColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`Service ${info.row.original.namespace}/${info.row.original.name}`} actions={serviceActions(info.row.original)} /> }),
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
                ingressColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`Ingress ${info.row.original.namespace}/${info.row.original.name}`} actions={ingressActions(info.row.original)} /> }),
              ]}
              data={ingresses.data?.items ?? []}
              loading={ingresses.isLoading}
              empty={<div className="flex flex-col items-center gap-2"><Route className="h-6 w-6" /><span>没有 Ingress</span></div>}
            />
          )}
        </TabsContent>
      </Tabs>
      <ServiceFormDialog
        open={serviceFormOpen}
        onOpenChange={(open) => { setServiceFormOpen(open); if (!open) setEditingService(undefined); }}
        namespace={editingService?.namespace || namespace}
        workloadName=""
        workloadSelector={EMPTY_SELECTOR}
        contextLabel="手动配置 Service 的 Selector 与端口"
        service={editingService}
        onSaved={refreshServices}
      />
      <IngressFormDialog
        open={ingressFormOpen}
        onOpenChange={(open) => { setIngressFormOpen(open); if (!open) setEditingIngress(undefined); }}
        namespace={editingIngress?.namespace || namespace}
        ingress={editingIngress}
        ingressClasses={ingressClasses.data?.items ?? []}
        onSaved={refreshIngresses}
      />
      <DeleteResourceDialog
        open={Boolean(deletingService)}
        onOpenChange={(open) => { if (!open) setDeletingService(undefined); }}
        resourceKind="Service"
        name={deletingService?.name ?? ""}
        description="删除后 EndpointSlice 与访问入口将被移除，已有 Pod 不会受影响。"
        onConfirm={async () => { if (!deletingService) return; await api.deleteService(deletingService.namespace, deletingService.name, deletingService.resourceVersion); await refreshServices(); }}
      />
      <DeleteResourceDialog
        open={Boolean(deletingIngress)}
        onOpenChange={(open) => { if (!open) setDeletingIngress(undefined); }}
        resourceKind="Ingress"
        name={deletingIngress?.name ?? ""}
        description="删除后对应路由规则将立即失效，后端 Service 和 Pod 不会受影响。"
        onConfirm={async () => { if (!deletingIngress) return; await api.deleteIngress(deletingIngress.namespace, deletingIngress.name, deletingIngress.resourceVersion); await refreshIngresses(); }}
      />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type={yamlTarget?.type ?? "service"} name={yamlTarget?.name ?? ""} namespace={yamlTarget?.namespace} />
    </>
  );
}
