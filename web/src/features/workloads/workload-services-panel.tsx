import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { ServicePort, ServiceSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { EmptyState, ErrorState } from "@/components/feedback/states";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCapabilities } from "@/features/auth/use-session";
import { ServiceFormDialog } from "@/features/workloads/service-form-dialog";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge } from "@/lib/utils";

const serviceColumn = createColumnHelper<ServiceSummary>();

function associatedService(service: ServiceSummary, workloadSelector: Record<string, string>): boolean {
  const entries = Object.entries(service.selector || {});
  return entries.length > 0 && entries.every(([key, value]) => workloadSelector[key] === value);
}

function formatPort(port: ServicePort): string {
  const nodePort = port.nodePort ? ` · NodePort ${port.nodePort}` : "";
  return `${port.port} → ${port.targetPort}/${port.protocol}${nodePort}`;
}

export function WorkloadServicesPanel({ namespace, workloadName, workloadSelector }: {
  namespace: string;
  workloadName: string;
  workloadSelector: Record<string, string>;
}) {
  const queryClient = useQueryClient();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<ServiceSummary>();
  const [deleting, setDeleting] = useState<ServiceSummary>();
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const query = useQuery({
    queryKey: ["services", namespace],
    queryFn: () => api.services({ namespace, pageSize: 200 }),
    refetchInterval: 20_000,
  });
  const capabilities = useCapabilities(namespace).data;
  const serviceCaps = resourceCapabilities(capabilities, "services");
  const canCreate = serviceCaps.create;
  const canUpdate = serviceCaps.update;
  const canDelete = serviceCaps.delete;

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ["services"] });
    await queryClient.invalidateQueries({ queryKey: ["service"] });
  };

  const remove = useMutation({
    mutationFn: (service: ServiceSummary) => api.deleteService(service.namespace, service.name, service.resourceVersion),
    onSuccess: async () => {
      toast.success("Service 删除请求已提交");
      setDeleting(undefined);
      setDeleteConfirm("");
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const actionsFor = (service: ServiceSummary): ResourceAction[] => {
    const actions: ResourceAction[] = [];
    if (canUpdate) {
      actions.push({
        label: "编辑",
        icon: <Pencil className="h-4 w-4" />,
        onSelect: () => { setEditing(service); setFormOpen(true); },
      });
    }
    if (canDelete) {
      actions.push({
        label: "删除",
        icon: <Trash2 className="h-4 w-4" />,
        onSelect: () => { setDeleting(service); setDeleteConfirm(""); },
        destructive: true,
        separatorBefore: actions.length > 0,
      });
    }
    return actions;
  };

  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const services = (query.data?.items ?? []).filter((service) => associatedService(service, workloadSelector));
  const createDisabled = !Object.keys(workloadSelector).length;
  const createButton = canCreate ? (
    <Button size="sm" onClick={() => { setEditing(undefined); setFormOpen(true); }} disabled={createDisabled}>
      <Plus className="h-4 w-4" />配置访问方式
    </Button>
  ) : undefined;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold">关联 Service</h2>
          <p className="mt-1 text-sm text-muted-foreground">Selector 与此工作负载匹配的访问入口，可直接创建、编辑和删除。</p>
        </div>
        {createButton}
      </div>

      <DataTable
        columns={[
          serviceColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/network/services/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} search={{}} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          serviceColumn.accessor("type", { header: "类型", cell: (info) => <Badge variant={info.row.original.headless ? "outline" : info.getValue() === "LoadBalancer" ? "default" : "secondary"}>{info.row.original.headless ? "Headless" : info.getValue()}</Badge> }),
          serviceColumn.accessor("clusterIP", {
            header: "访问地址",
            cell: (info) => {
              const service = info.row.original;
              return (
                <div className="space-y-1">
                  <div className="font-mono text-xs">{service.headless ? "None（Headless）" : info.getValue() || "—"}</div>
                  {service.externalIP ? <div className="break-all font-mono text-[11px] text-muted-foreground">外部 {service.externalIP}</div> : null}
                </div>
              );
            },
          }),
          serviceColumn.accessor("ports", {
            header: "端口",
            enableSorting: false,
            size: 260,
            cell: (info) => <div className="flex flex-wrap gap-1.5">{info.getValue().map((port: ServicePort) => <span key={`${port.name}-${port.protocol}-${port.port}`} className="rounded border bg-muted/30 px-1.5 py-0.5 font-mono text-[11px]">{formatPort(port)}</span>)}</div>,
          }),
          serviceColumn.accessor("endpointCount", {
            header: "Endpoint",
            size: 120,
            cell: (info) => <Badge variant={info.getValue() > 0 ? "default" : "outline"}>{info.getValue()} 就绪</Badge>,
          }),
          serviceColumn.accessor("createdAt", { header: "年龄", size: 120, cell: (info) => formatAge(info.getValue()) }),
          serviceColumn.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`Service ${info.row.original.namespace}/${info.row.original.name}`} actions={actionsFor(info.row.original)} /> }),
        ]}
        data={services}
        loading={query.isLoading}
        empty={(
          <EmptyState
            title="尚未配置访问方式"
            description="创建 Service 后，可通过 ClusterIP、Headless、NodePort 或 LoadBalancer 访问该工作负载。"
            action={createButton}
            className="border-0 bg-transparent"
          />
        )}
      />

      <ServiceFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        namespace={namespace}
        workloadName={workloadName}
        workloadSelector={workloadSelector}
        service={editing}
        onSaved={refresh}
      />

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => { if (!open) { setDeleting(undefined); setDeleteConfirm(""); } }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 Service {deleting?.name}？</AlertDialogTitle>
            <AlertDialogDescription>删除后该访问入口及 EndpointSlice 将被移除，运行中的 Pod 不会受影响。请输入 Service 名称确认。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-2"><Label htmlFor="service-delete-confirm">Service 名称</Label><Input id="service-delete-confirm" value={deleteConfirm} onChange={(event) => setDeleteConfirm(event.target.value)} placeholder={deleting?.name} autoComplete="off" /></div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => { event.preventDefault(); if (deleting) remove.mutate(deleting); }} disabled={!deleting || deleteConfirm !== deleting.name || remove.isPending}>
              {remove.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
