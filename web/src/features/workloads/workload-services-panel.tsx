import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Loader2, Network, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { ServiceSummary } from "@/api/types";
import { ErrorState } from "@/components/feedback/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCapabilities } from "@/features/auth/use-session";
import { ServiceFormDialog } from "@/features/workloads/service-form-dialog";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatAge } from "@/lib/utils";

function associatedService(service: ServiceSummary, workloadSelector: Record<string, string>): boolean {
  const entries = Object.entries(service.selector || {});
  return entries.length > 0 && entries.every(([key, value]) => workloadSelector[key] === value);
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

  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const services = (query.data?.items ?? []).filter((service) => associatedService(service, workloadSelector));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold">关联 Service</h2>
          <p className="mt-1 text-sm text-muted-foreground">Selector 与此工作负载匹配的访问入口，可直接创建、编辑和删除。</p>
        </div>
        {canCreate ? (
          <Button size="sm" onClick={() => { setEditing(undefined); setFormOpen(true); }} disabled={!Object.keys(workloadSelector).length}>
            <Plus className="h-4 w-4" />配置访问方式
          </Button>
        ) : null}
      </div>

      {query.isLoading ? (
        <Card><CardContent><div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在加载 Service</div></CardContent></Card>
      ) : services.length ? (
        <div className="grid gap-4 xl:grid-cols-2">
          {services.map((service) => (
            <Card key={`${service.namespace}/${service.name}`} className="min-w-0">
              <CardHeader className="gap-2">
                <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                  <CardTitle className="flex min-w-0 items-center gap-2"><Network className="h-4 w-4 shrink-0 text-muted-foreground" /><Link to="/network/services/$namespace/$name" params={{ namespace: service.namespace, name: service.name }} search={{}} className="truncate text-primary hover:underline">{service.name}</Link></CardTitle>
                  <div className="flex flex-wrap items-center gap-1.5"><Badge variant={service.headless ? "outline" : "secondary"}>{service.headless ? "Headless" : service.type}</Badge><Badge variant={service.endpointCount > 0 ? "default" : "outline"}>{service.endpointCount} 就绪 Endpoint</Badge></div>
                </div>
                <p className="text-xs text-muted-foreground">ClusterIP <span className="font-mono text-foreground">{service.headless ? "None（Headless）" : service.clusterIP || "—"}</span> · 创建于 {formatAge(service.createdAt)}</p>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  {service.ports.map((port) => (
                    <div key={`${port.name}-${port.protocol}-${port.port}`} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border bg-muted/20 px-3 py-2 text-sm">
                      <span className="font-medium">{port.name || "未命名"}</span>
                      <span className="break-all font-mono text-xs text-muted-foreground">{port.port} → {port.targetPort}/{port.protocol}{port.nodePort ? ` · NodePort ${port.nodePort}` : ""}</span>
                    </div>
                  ))}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {Object.entries(service.selector || {}).map(([key, value]) => <Badge key={key} variant="outline" className="max-w-full break-all font-mono text-[11px]">{key}={value}</Badge>)}
                </div>
                {canUpdate || canDelete ? (
                  <div className="flex flex-wrap justify-end gap-2 border-t pt-3">
                    {canUpdate ? <Button variant="outline" size="sm" onClick={() => { setEditing(service); setFormOpen(true); }}><Pencil className="h-4 w-4" />编辑</Button> : null}
                    {canDelete ? <Button variant="outline" size="sm" onClick={() => { setDeleting(service); setDeleteConfirm(""); }}><Trash2 className="h-4 w-4" />删除</Button> : null}
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 px-6 py-12 text-center">
            <Network className="h-7 w-7 text-muted-foreground" />
            <div><p className="font-medium">尚未配置访问方式</p><p className="mt-1 text-sm text-muted-foreground">创建 Service 后，可通过 ClusterIP、Headless、NodePort 或 LoadBalancer 访问该工作负载。</p></div>
            {canCreate ? <Button size="sm" onClick={() => { setEditing(undefined); setFormOpen(true); }} disabled={!Object.keys(workloadSelector).length}><Plus className="h-4 w-4" />配置访问方式</Button> : null}
          </CardContent>
        </Card>
      )}

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
