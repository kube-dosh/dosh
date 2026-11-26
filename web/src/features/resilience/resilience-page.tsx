import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Activity, CircleHelp, Gauge, Loader2, Pencil, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type {
  AutoscalerInput,
  AutoscalerSummary,
  DisruptionBudgetInput,
  DisruptionBudgetSummary,
  VPAContainerPolicy,
  VerticalAutoscalerInput,
  VerticalAutoscalerSummary,
} from "@/api/types";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useCapabilities } from "@/features/auth/use-session";
import { resourceCapabilities } from "@/lib/capabilities";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const QUANTITY = /^\d+%?$/;

type AutoscalerForm = {
  name: string;
  targetKind: "Deployment" | "StatefulSet";
  targetName: string;
  manualTarget: boolean;
  minReplicas: string;
  maxReplicas: string;
  cpuUtilization: string;
  memoryUtilization: string;
  resourceVersion?: string;
};

type DisruptionBudgetForm = {
  name: string;
  mode: "minAvailable" | "maxUnavailable";
  value: string;
  selector: string;
  resourceVersion?: string;
};

type VPAContainerPolicyForm = {
  containerName: string;
  mode: "" | "Off" | "Auto";
  controlledValues: "" | "RequestsAndLimits" | "RequestsOnly";
  controlledResources: Array<"cpu" | "memory">;
  minCPU: string;
  minMemory: string;
  maxCPU: string;
  maxMemory: string;
};

type VPAForm = {
  name: string;
  targetKind: "Deployment" | "StatefulSet" | "DaemonSet" | "ReplicaSet";
  targetName: string;
  manualTarget: boolean;
  updatePolicy: "Off" | "Initial" | "Recreate" | "Auto" | "InPlaceOrRecreate";
  minReplicas: string;
  containerPolicies: VPAContainerPolicyForm[];
  resourceVersion?: string;
};

function emptyAutoscalerForm(): AutoscalerForm {
  return {
    name: "", targetKind: "Deployment", targetName: "", manualTarget: false,
    minReplicas: "1", maxReplicas: "5", cpuUtilization: "80", memoryUtilization: "",
  };
}

function autoscalerForm(item: AutoscalerSummary): AutoscalerForm {
  return {
    name: item.name,
    targetKind: item.targetKind === "StatefulSet" ? "StatefulSet" : "Deployment",
    targetName: item.targetName,
    manualTarget: true,
    minReplicas: String(item.minReplicas),
    maxReplicas: String(item.maxReplicas),
    cpuUtilization: metricTarget(item, "cpu"),
    memoryUtilization: metricTarget(item, "memory"),
    resourceVersion: item.resourceVersion,
  };
}

function emptyDisruptionBudgetForm(): DisruptionBudgetForm {
  return { name: "", mode: "minAvailable", value: "1", selector: "" };
}

function disruptionBudgetForm(item: DisruptionBudgetSummary): DisruptionBudgetForm {
  const mode = item.maxUnavailable !== undefined && item.minAvailable === undefined ? "maxUnavailable" : "minAvailable";
  return {
    name: item.name,
    mode,
    value: mode === "maxUnavailable" ? item.maxUnavailable ?? "1" : item.minAvailable ?? "1",
    selector: Object.entries(item.selector).map(([key, value]) => `${key}=${value}`).join("\n"),
    resourceVersion: item.resourceVersion,
  };
}

function metricTarget(item: AutoscalerSummary, resource: "cpu" | "memory"): string {
  const metric = item.metrics.find((entry) => entry.resource?.toLowerCase() === resource);
  return metric?.targetUtilization === undefined ? "" : String(metric.targetUtilization);
}

function emptyVPAContainerPolicy(): VPAContainerPolicyForm {
  return { containerName: "*", mode: "", controlledValues: "", controlledResources: ["cpu", "memory"], minCPU: "", minMemory: "", maxCPU: "", maxMemory: "" };
}

function emptyVPAForm(): VPAForm {
  return {
    name: "", targetKind: "Deployment", targetName: "", manualTarget: false,
    updatePolicy: "Auto", minReplicas: "", containerPolicies: [emptyVPAContainerPolicy()],
  };
}

function vpaFormFromItem(item: VerticalAutoscalerSummary): VPAForm {
  return {
    name: item.name,
    targetKind: item.targetKind === "StatefulSet" || item.targetKind === "DaemonSet" || item.targetKind === "ReplicaSet" ? item.targetKind : "Deployment",
    targetName: item.targetName,
    manualTarget: true,
    updatePolicy: item.updatePolicy === "Off" || item.updatePolicy === "Initial" || item.updatePolicy === "Recreate" || item.updatePolicy === "InPlaceOrRecreate" ? item.updatePolicy : "Auto",
    minReplicas: item.minReplicas === undefined ? "" : String(item.minReplicas),
    containerPolicies: item.containerPolicies.length ? item.containerPolicies.map(containerPolicyForm) : [emptyVPAContainerPolicy()],
    resourceVersion: item.resourceVersion,
  };
}

function containerPolicyForm(policy: VPAContainerPolicy): VPAContainerPolicyForm {
  return {
    containerName: policy.containerName || "*",
    mode: policy.mode === "Off" || policy.mode === "Auto" ? policy.mode : "",
    controlledValues: policy.controlledValues === "RequestsAndLimits" || policy.controlledValues === "RequestsOnly" ? policy.controlledValues : "",
    controlledResources: (policy.controlledResources ?? []).filter((item): item is "cpu" | "memory" => item === "cpu" || item === "memory"),
    minCPU: policy.minAllowed?.cpu ?? "", minMemory: policy.minAllowed?.memory ?? "",
    maxCPU: policy.maxAllowed?.cpu ?? "", maxMemory: policy.maxAllowed?.memory ?? "",
  };
}

export function ResiliencePage() {
  const search = useSearch({ from: "/app/resilience" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const namespaces = useQuery({ queryKey: ["namespaces", "resilience"], queryFn: () => api.namespaces({ pageSize: 200 }), staleTime: 60_000 });
  const namespace = search.namespace ?? "";
  const namespaceReady = namespace || namespaces.data?.items[0]?.name || "";
  const tab = search.tab === "pdb" ? "pdb" : search.tab === "vpa" ? "vpa" : "hpa";
  const capabilities = useCapabilities(namespaceReady || undefined).data;
  const hpaCapabilities = resourceCapabilities(capabilities, "horizontalpodautoscalers");
  const pdbCapabilities = resourceCapabilities(capabilities, "poddisruptionbudgets");
  const vpaCapabilities = resourceCapabilities(capabilities, "verticalpodautoscalers");
  const [hpaOpen, setHpaOpen] = useState(false);
  const [hpaForm, setHpaForm] = useState<AutoscalerForm>(emptyAutoscalerForm);
  const [hpaError, setHpaError] = useState("");
  const [editingHpa, setEditingHpa] = useState<AutoscalerSummary>();
  const [deleteHpa, setDeleteHpa] = useState<AutoscalerSummary>();
  const [pdbOpen, setPdbOpen] = useState(false);
  const [pdbForm, setPdbForm] = useState<DisruptionBudgetForm>(emptyDisruptionBudgetForm);
  const [pdbError, setPdbError] = useState("");
  const [editingPdb, setEditingPdb] = useState<DisruptionBudgetSummary>();
  const [deletePdb, setDeletePdb] = useState<DisruptionBudgetSummary>();
  const [vpaOpen, setVpaOpen] = useState(false);
  const [vpaForm, setVpaForm] = useState<VPAForm>(emptyVPAForm);
  const [vpaError, setVpaError] = useState("");
  const [editingVpa, setEditingVpa] = useState<VerticalAutoscalerSummary>();
  const [deleteVpa, setDeleteVpa] = useState<VerticalAutoscalerSummary>();

  useEffect(() => {
    if (namespace || !namespaces.data?.items[0]?.name) return;
    void navigate({ to: "/resilience", search: { ...search, namespace: namespaces.data.items[0].name }, replace: true });
  }, [navigate, namespace, namespaces.data?.items, search]);

  const autoscalers = useQuery({
    queryKey: ["autoscalers", namespaceReady],
    queryFn: () => api.autoscalers(namespaceReady),
    enabled: Boolean(namespaceReady),
    staleTime: 15_000,
  });
  const disruptionBudgets = useQuery({
    queryKey: ["disruption-budgets", namespaceReady],
    queryFn: () => api.disruptionBudgets(namespaceReady),
    enabled: Boolean(namespaceReady),
    staleTime: 15_000,
  });
  const verticalAutoscalers = useQuery({
    queryKey: ["verticalpodautoscalers", namespaceReady],
    queryFn: () => api.verticalAutoscalers(namespaceReady),
    enabled: Boolean(namespaceReady),
    staleTime: 15_000,
  });
  const workloads = useQuery({
    queryKey: ["workloads", namespaceReady, "resilience-targets"],
    queryFn: () => api.workloads({ namespace: namespaceReady, pageSize: 200 }),
    enabled: Boolean(namespaceReady),
    staleTime: 30_000,
  });
  const targetOptions = useMemo(() => (workloads.data?.items ?? [])
    .filter((item) => item.kind.toLowerCase() === hpaForm.targetKind.toLowerCase())
    .map((item) => ({ value: item.name, label: `${item.name} · ${item.ready} 就绪` })), [hpaForm.targetKind, workloads.data?.items]);
  const vpaTargetOptions = useMemo(() => (workloads.data?.items ?? [])
    .filter((item) => item.kind.toLowerCase() === vpaForm.targetKind.toLowerCase())
    .map((item) => ({ value: item.name, label: `${item.name} · ${item.ready} 就绪` })), [vpaForm.targetKind, workloads.data?.items]);

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["autoscalers", namespaceReady] }),
      queryClient.invalidateQueries({ queryKey: ["disruption-budgets", namespaceReady] }),
      queryClient.invalidateQueries({ queryKey: ["verticalpodautoscalers", namespaceReady] }),
    ]);
  };

  const saveHpa = useMutation({
    mutationFn: (input: AutoscalerInput) => api.upsertAutoscaler(input),
    onSuccess: async (saved) => {
      toast.success(editingHpa ? `HPA ${saved.name} 已更新` : `HPA ${saved.name} 已创建`);
      setHpaOpen(false);
      setEditingHpa(undefined);
      await refresh();
    },
    onError: (error) => setHpaError(error.message),
  });
  const removeHpa = useMutation({
    mutationFn: (item: AutoscalerSummary) => api.deleteAutoscaler(item.namespace, item.name, item.resourceVersion),
    onSuccess: async (_, item) => {
      toast.success(`HPA ${item.name} 已删除`);
      setDeleteHpa(undefined);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });
  const savePdb = useMutation({
    mutationFn: (input: DisruptionBudgetInput) => api.upsertDisruptionBudget(input),
    onSuccess: async (saved) => {
      toast.success(editingPdb ? `PDB ${saved.name} 已更新` : `PDB ${saved.name} 已创建`);
      setPdbOpen(false);
      setEditingPdb(undefined);
      await refresh();
    },
    onError: (error) => setPdbError(error.message),
  });
  const removePdb = useMutation({
    mutationFn: (item: DisruptionBudgetSummary) => api.deleteDisruptionBudget(item.namespace, item.name, item.resourceVersion),
    onSuccess: async (_, item) => {
      toast.success(`PDB ${item.name} 已删除`);
      setDeletePdb(undefined);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });
  const saveVpa = useMutation({
    mutationFn: (input: VerticalAutoscalerInput) => api.upsertVerticalAutoscaler(input),
    onSuccess: async (saved) => {
      toast.success(editingVpa ? `VPA ${saved.name} 已更新` : `VPA ${saved.name} 已创建`);
      setVpaOpen(false);
      setEditingVpa(undefined);
      await refresh();
    },
    onError: (error) => setVpaError(error.message),
  });
  const removeVpa = useMutation({
    mutationFn: (item: VerticalAutoscalerSummary) => {
      if (!item.resourceVersion) throw new Error("VPA 缺少 resourceVersion，请刷新后再删除");
      return api.deleteVerticalAutoscaler(item.namespace, item.name, item.resourceVersion);
    },
    onSuccess: async (_, item) => {
      toast.success(`VPA ${item.name} 已删除`);
      setDeleteVpa(undefined);
      await refresh();
    },
    onError: (error) => toast.error(error.message),
  });

  const openCreateHpa = () => {
    setEditingHpa(undefined);
    setHpaForm(emptyAutoscalerForm());
    setHpaError("");
    setHpaOpen(true);
  };
  const openEditHpa = (item: AutoscalerSummary) => {
    setEditingHpa(item);
    setHpaForm(autoscalerForm(item));
    setHpaError("");
    setHpaOpen(true);
  };
  const openCreatePdb = () => {
    setEditingPdb(undefined);
    setPdbForm(emptyDisruptionBudgetForm());
    setPdbError("");
    setPdbOpen(true);
  };
  const openEditPdb = (item: DisruptionBudgetSummary) => {
    setEditingPdb(item);
    setPdbForm(disruptionBudgetForm(item));
    setPdbError("");
    setPdbOpen(true);
  };
  const openCreateVpa = () => {
    setEditingVpa(undefined);
    setVpaForm(emptyVPAForm());
    setVpaError("");
    setVpaOpen(true);
  };
  const openEditVpa = (item: VerticalAutoscalerSummary) => {
    setEditingVpa(item);
    setVpaForm(vpaFormFromItem(item));
    setVpaError("");
    setVpaOpen(true);
  };

  const submitHpa = (event: FormEvent) => {
    event.preventDefault();
    const error = validateAutoscalerForm(hpaForm);
    setHpaError(error);
    if (error || !namespaceReady) return;
    saveHpa.mutate({
      name: hpaForm.name.trim(),
      namespace: namespaceReady,
      targetKind: hpaForm.targetKind,
      targetName: hpaForm.targetName.trim(),
      minReplicas: Number(hpaForm.minReplicas),
      maxReplicas: Number(hpaForm.maxReplicas),
      cpuUtilization: hpaForm.cpuUtilization.trim() ? Number(hpaForm.cpuUtilization) : undefined,
      memoryUtilization: hpaForm.memoryUtilization.trim() ? Number(hpaForm.memoryUtilization) : undefined,
      resourceVersion: hpaForm.resourceVersion,
    });
  };

  const submitPdb = (event: FormEvent) => {
    event.preventDefault();
    const validation = validateDisruptionBudgetForm(pdbForm);
    setPdbError(validation.error);
    if (validation.error || !namespaceReady) return;
    savePdb.mutate({
      name: pdbForm.name.trim(),
      namespace: namespaceReady,
      minAvailable: pdbForm.mode === "minAvailable" ? pdbForm.value.trim() : undefined,
      maxUnavailable: pdbForm.mode === "maxUnavailable" ? pdbForm.value.trim() : undefined,
      selector: validation.selector,
      resourceVersion: pdbForm.resourceVersion,
    });
  };
  const submitVpa = (event: FormEvent) => {
    event.preventDefault();
    const validation = validateVPAForm(vpaForm);
    setVpaError(validation.error);
    if (validation.error || !namespaceReady) return;
    saveVpa.mutate({
      name: vpaForm.name.trim(),
      namespace: namespaceReady,
      targetKind: vpaForm.targetKind,
      targetName: vpaForm.targetName.trim(),
      updatePolicy: vpaForm.updatePolicy,
      minReplicas: vpaForm.minReplicas.trim() ? Number(vpaForm.minReplicas) : undefined,
      containerPolicies: validation.policies,
      resourceVersion: vpaForm.resourceVersion,
    });
  };

  return (
    <>
      <PageHeader
        title="弹性与可用性"
        breadcrumb="集群 / 弹性与可用性"
        description="集中管理 HPA 水平扩缩、VPA 垂直推荐与 PDB 自愿中断预算，并解释当前指标、健康副本和允许驱逐数量。"
      />
      <div className="mb-4 flex min-w-0 flex-wrap items-center gap-3">
        <NamespaceSelect value={namespaceReady} onChange={(value) => void navigate({ to: "/resilience", search: { ...search, namespace: value || undefined }, replace: true })} />
        <span className="min-w-0 text-xs text-muted-foreground">HPA、VPA 和 PDB 是命名空间资源，请选择目标命名空间。</span>
      </div>

      <div className="mb-4 flex items-start gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-muted-foreground">
        <CircleHelp className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
        <span className="min-w-0 break-words">HPA 根据 CPU/内存利用率调整副本；VPA 根据历史负载给出 requests/limits 建议并按策略更新；PDB 只限制节点维护等自愿驱逐。</span>
      </div>

      {!namespaceReady ? (
        <EmptyState title="正在选择命名空间" description={namespaces.isError ? "命名空间加载失败，请检查权限后重试。" : "选择命名空间后可查看自动扩缩和中断预算。"} action={namespaces.isError ? <Button variant="outline" onClick={() => void namespaces.refetch()}>重试</Button> : undefined} />
      ) : (
        <Tabs value={tab} onValueChange={(value) => void navigate({ to: "/resilience", search: { ...search, namespace: namespaceReady, tab: value === "hpa" ? undefined : value }, replace: true })}>
          <TabsList>
            <TabsTrigger value="hpa"><Gauge className="mr-1.5 h-4 w-4" />HPA 自动扩缩</TabsTrigger>
            <TabsTrigger value="vpa"><Activity className="mr-1.5 h-4 w-4" />VPA 垂直治理</TabsTrigger>
            <TabsTrigger value="pdb"><ShieldCheck className="mr-1.5 h-4 w-4" />PDB 中断预算</TabsTrigger>
          </TabsList>

          <TabsContent value="hpa">
            <div className="mb-4 flex min-w-0 flex-wrap items-center justify-between gap-2">
              <div className="min-w-0"><p className="text-sm font-semibold">HorizontalPodAutoscaler</p><p className="text-xs text-muted-foreground">{namespaceReady} · 当前 {autoscalers.data?.items.length ?? 0} 个自动扩缩策略</p></div>
              {hpaCapabilities.create ? <Button size="sm" onClick={openCreateHpa}><Plus className="h-4 w-4" />创建 HPA</Button> : null}
            </div>
            {autoscalers.isLoading ? <LoadingState label="正在加载 HPA" /> : autoscalers.isError ? <ErrorState error={autoscalers.error} onRetry={() => void autoscalers.refetch()} /> : autoscalers.data?.items.length ? (
              <div className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
                {autoscalers.data.items.map((item) => <AutoscalerCard key={`${item.namespace}/${item.name}`} item={item} canEdit={hpaCapabilities.update} canDelete={hpaCapabilities.delete} onEdit={() => openEditHpa(item)} onDelete={() => setDeleteHpa(item)} />)}
              </div>
            ) : <EmptyState title="暂无 HPA" description="为 Deployment 或 StatefulSet 创建基于 CPU 或内存利用率的自动扩缩策略。" action={hpaCapabilities.create ? <Button size="sm" onClick={openCreateHpa}><Plus className="h-4 w-4" />创建 HPA</Button> : undefined} />}
          </TabsContent>

          <TabsContent value="vpa">
            <div className="mb-4 flex min-w-0 flex-wrap items-center justify-between gap-2">
              <div className="min-w-0"><p className="text-sm font-semibold">VerticalPodAutoscaler</p><p className="text-xs text-muted-foreground">{namespaceReady} · 当前 {verticalAutoscalers.data?.items.length ?? 0} 个垂直治理策略</p></div>
              {vpaCapabilities.create ? <Button size="sm" onClick={openCreateVpa}><Plus className="h-4 w-4" />创建 VPA</Button> : null}
            </div>
            {verticalAutoscalers.isLoading ? <LoadingState label="正在加载 VPA" /> : verticalAutoscalers.isError ? <ErrorState error={verticalAutoscalers.error} onRetry={() => void verticalAutoscalers.refetch()} /> : verticalAutoscalers.data?.items.length ? (
              <div className="grid min-w-0 grid-cols-1 gap-3 xl:grid-cols-2">
                {verticalAutoscalers.data.items.map((item) => <VerticalAutoscalerCard key={`${item.namespace}/${item.name}`} item={item} canEdit={vpaCapabilities.update} canDelete={vpaCapabilities.delete} onEdit={() => openEditVpa(item)} onDelete={() => setDeleteVpa(item)} />)}
              </div>
            ) : <EmptyState title="暂无 VPA" description="VPA 需要集群已安装 autoscaling.k8s.io/v1 CRD 和 VPA 控制器。安装后可为 Deployment、StatefulSet、DaemonSet 或 ReplicaSet 生成资源建议。" action={vpaCapabilities.create ? <Button size="sm" onClick={openCreateVpa}><Plus className="h-4 w-4" />创建 VPA</Button> : undefined} />}
          </TabsContent>

          <TabsContent value="pdb">
            <div className="mb-4 flex min-w-0 flex-wrap items-center justify-between gap-2">
              <div className="min-w-0"><p className="text-sm font-semibold">PodDisruptionBudget</p><p className="text-xs text-muted-foreground">{namespaceReady} · 当前 {disruptionBudgets.data?.items.length ?? 0} 个中断预算</p></div>
              {pdbCapabilities.create ? <Button size="sm" onClick={openCreatePdb}><Plus className="h-4 w-4" />创建 PDB</Button> : null}
            </div>
            {disruptionBudgets.isLoading ? <LoadingState label="正在加载 PDB" /> : disruptionBudgets.isError ? <ErrorState error={disruptionBudgets.error} onRetry={() => void disruptionBudgets.refetch()} /> : disruptionBudgets.data?.items.length ? (
              <div className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
                {disruptionBudgets.data.items.map((item) => <DisruptionBudgetCard key={`${item.namespace}/${item.name}`} item={item} canEdit={pdbCapabilities.update} canDelete={pdbCapabilities.delete} onEdit={() => openEditPdb(item)} onDelete={() => setDeletePdb(item)} />)}
              </div>
            ) : <EmptyState title="暂无 PDB" description="使用 Pod 标签选择器定义维护期间必须保持可用的最小副本或允许中断的最大副本。" action={pdbCapabilities.create ? <Button size="sm" onClick={openCreatePdb}><Plus className="h-4 w-4" />创建 PDB</Button> : undefined} />}
          </TabsContent>
        </Tabs>
      )}

      <Dialog open={hpaOpen} onOpenChange={(open) => { if (!saveHpa.isPending) { setHpaOpen(open); if (!open) setEditingHpa(undefined); } }}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle>{editingHpa ? "编辑 HPA" : "创建 HPA"}</DialogTitle>
            <DialogDescription>基于 CPU 或内存平均利用率调整副本。创建前会由 API Server 执行 Dry Run。</DialogDescription>
          </DialogHeader>
          <form className="min-w-0 space-y-4" onSubmit={submitHpa}>
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <FormField label="名称" htmlFor="hpa-name"><Input id="hpa-name" value={hpaForm.name} onChange={(event) => setHpaForm({ ...hpaForm, name: event.target.value })} placeholder="web-api" disabled={Boolean(editingHpa)} /></FormField>
              <FormField label="目标类型" htmlFor="hpa-target-kind"><Select value={hpaForm.targetKind} onValueChange={(value) => setHpaForm({ ...hpaForm, targetKind: value as AutoscalerForm["targetKind"], targetName: "", manualTarget: false })} disabled={Boolean(editingHpa)}><SelectTrigger id="hpa-target-kind"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="Deployment">Deployment</SelectItem><SelectItem value="StatefulSet">StatefulSet</SelectItem></SelectContent></Select></FormField>
              <div className="min-w-0 space-y-1.5 sm:col-span-2">
                <Label htmlFor="hpa-target-name">目标工作负载</Label>
                {hpaForm.manualTarget ? <div className="flex min-w-0 flex-col gap-2 sm:flex-row"><Input id="hpa-target-name" value={hpaForm.targetName} onChange={(event) => setHpaForm({ ...hpaForm, targetName: event.target.value })} placeholder="web-api" disabled={Boolean(editingHpa)} /><Button type="button" variant="outline" onClick={() => setHpaForm({ ...hpaForm, manualTarget: false })} disabled={Boolean(editingHpa)}>从列表选择</Button></div> : <SearchableSelect id="hpa-target-name" label="选择目标工作负载" value={hpaForm.targetName} options={targetOptions} onChange={(value) => setHpaForm({ ...hpaForm, targetName: value })} onManualSelect={() => setHpaForm({ ...hpaForm, manualTarget: true })} placeholder={workloads.isLoading ? "正在加载工作负载…" : targetOptions.length ? "搜索并选择工作负载" : "暂无可选工作负载，请手动输入"} searchPlaceholder="搜索工作负载…" disabled={Boolean(editingHpa) || workloads.isLoading} />}
                <p className="text-xs text-muted-foreground">只列出当前命名空间中的 {hpaForm.targetKind}。</p>
              </div>
              <FormField label="最小副本" htmlFor="hpa-min-replicas"><Input id="hpa-min-replicas" type="number" min={1} max={1000} value={hpaForm.minReplicas} onChange={(event) => setHpaForm({ ...hpaForm, minReplicas: event.target.value })} /></FormField>
              <FormField label="最大副本" htmlFor="hpa-max-replicas"><Input id="hpa-max-replicas" type="number" min={1} max={1000} value={hpaForm.maxReplicas} onChange={(event) => setHpaForm({ ...hpaForm, maxReplicas: event.target.value })} /></FormField>
              <FormField label="CPU 利用率目标（%）" htmlFor="hpa-cpu" hint="留空表示不使用 CPU 指标。"><Input id="hpa-cpu" type="number" min={1} max={100} value={hpaForm.cpuUtilization} onChange={(event) => setHpaForm({ ...hpaForm, cpuUtilization: event.target.value })} placeholder="80" /></FormField>
              <FormField label="内存利用率目标（%）" htmlFor="hpa-memory" hint="CPU 和内存至少配置一项。"><Input id="hpa-memory" type="number" min={1} max={100} value={hpaForm.memoryUtilization} onChange={(event) => setHpaForm({ ...hpaForm, memoryUtilization: event.target.value })} placeholder="75" /></FormField>
            </div>
            {hpaError ? <p role="alert" className="break-words rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-xs text-destructive-strong">{hpaError}</p> : null}
            <DialogFooter><Button type="button" variant="outline" onClick={() => setHpaOpen(false)} disabled={saveHpa.isPending}>取消</Button><Button type="submit" disabled={saveHpa.isPending}>{saveHpa.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editingHpa ? "保存 HPA" : "创建 HPA"}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={pdbOpen} onOpenChange={(open) => { if (!savePdb.isPending) { setPdbOpen(open); if (!open) setEditingPdb(undefined); } }}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle>{editingPdb ? "编辑 PDB" : "创建 PDB"}</DialogTitle>
            <DialogDescription>选择需要保护的一组 Pod，并设置必须可用或允许中断的副本约束。</DialogDescription>
          </DialogHeader>
          <form className="min-w-0 space-y-4" onSubmit={submitPdb}>
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <FormField label="名称" htmlFor="pdb-name"><Input id="pdb-name" value={pdbForm.name} onChange={(event) => setPdbForm({ ...pdbForm, name: event.target.value })} placeholder="web-api-pdb" disabled={Boolean(editingPdb)} /></FormField>
              <FormField label="约束类型" htmlFor="pdb-mode"><Select value={pdbForm.mode} onValueChange={(value) => setPdbForm({ ...pdbForm, mode: value as DisruptionBudgetForm["mode"] })}><SelectTrigger id="pdb-mode"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="minAvailable">minAvailable · 最少可用</SelectItem><SelectItem value="maxUnavailable">maxUnavailable · 最多不可用</SelectItem></SelectContent></Select></FormField>
              <FormField label="约束值" htmlFor="pdb-value" hint="填写副本数或百分比，例如 2 或 25%。"><Input id="pdb-value" value={pdbForm.value} onChange={(event) => setPdbForm({ ...pdbForm, value: event.target.value })} placeholder="1" className="font-mono" /></FormField>
              <FormField label="Pod 选择器" htmlFor="pdb-selector" hint="每行一个 key=value，例如 app=web-api。" className="sm:col-span-2"><Textarea id="pdb-selector" value={pdbForm.selector} onChange={(event) => setPdbForm({ ...pdbForm, selector: event.target.value })} placeholder={"app=web-api\ncomponent=api"} className="min-h-24 font-mono text-xs" /></FormField>
            </div>
            {pdbError ? <p role="alert" className="break-words rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-xs text-destructive-strong">{pdbError}</p> : null}
            <DialogFooter><Button type="button" variant="outline" onClick={() => setPdbOpen(false)} disabled={savePdb.isPending}>取消</Button><Button type="submit" disabled={savePdb.isPending}>{savePdb.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editingPdb ? "保存 PDB" : "创建 PDB"}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={vpaOpen} onOpenChange={(open) => { if (!saveVpa.isPending) { setVpaOpen(open); if (!open) setEditingVpa(undefined); } }}>
        <DialogContent className="max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-4xl overflow-y-auto p-4 sm:w-[calc(100%-2rem)] sm:p-6">
          <DialogHeader>
            <DialogTitle>{editingVpa ? "编辑 VPA" : "创建 VPA"}</DialogTitle>
            <DialogDescription>定义 VPA 的更新策略、资源上下界与容器覆盖规则。创建和更新前会由 API Server 执行 Dry Run。</DialogDescription>
          </DialogHeader>
          <form className="min-w-0 space-y-4" onSubmit={submitVpa}>
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <FormField label="名称" htmlFor="vpa-name"><Input id="vpa-name" value={vpaForm.name} onChange={(event) => setVpaForm({ ...vpaForm, name: event.target.value })} placeholder="web-api-vpa" disabled={Boolean(editingVpa)} /></FormField>
              <FormField label="目标类型" htmlFor="vpa-target-kind"><Select value={vpaForm.targetKind} onValueChange={(value) => setVpaForm({ ...vpaForm, targetKind: value as VPAForm["targetKind"], targetName: "", manualTarget: false })} disabled={Boolean(editingVpa)}><SelectTrigger id="vpa-target-kind"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="Deployment">Deployment</SelectItem><SelectItem value="StatefulSet">StatefulSet</SelectItem><SelectItem value="DaemonSet">DaemonSet</SelectItem><SelectItem value="ReplicaSet">ReplicaSet</SelectItem></SelectContent></Select></FormField>
              <div className="min-w-0 space-y-1.5 sm:col-span-2">
                <Label htmlFor="vpa-target-name">目标工作负载</Label>
                {vpaForm.manualTarget ? <div className="flex min-w-0 flex-col gap-2 sm:flex-row"><Input id="vpa-target-name" value={vpaForm.targetName} onChange={(event) => setVpaForm({ ...vpaForm, targetName: event.target.value })} placeholder="web-api" disabled={Boolean(editingVpa)} /><Button type="button" variant="outline" onClick={() => setVpaForm({ ...vpaForm, manualTarget: false })} disabled={Boolean(editingVpa)}>从列表选择</Button></div> : <SearchableSelect id="vpa-target-name" label="选择目标工作负载" value={vpaForm.targetName} options={vpaTargetOptions} onChange={(value) => setVpaForm({ ...vpaForm, targetName: value })} onManualSelect={() => setVpaForm({ ...vpaForm, manualTarget: true })} placeholder={workloads.isLoading ? "正在加载工作负载…" : vpaTargetOptions.length ? "搜索并选择工作负载" : "暂无可选工作负载，请手动输入"} searchPlaceholder="搜索工作负载…" disabled={Boolean(editingVpa) || workloads.isLoading} />}
                <p className="text-xs text-muted-foreground">目标工作负载必须与 VPA 位于同一命名空间。</p>
              </div>
              <FormField label="更新策略" htmlFor="vpa-update-policy" hint="Auto/Recreate 会驱逐 Pod 应用新资源；InPlaceOrRecreate 优先原地更新。"><Select value={vpaForm.updatePolicy} onValueChange={(value) => setVpaForm({ ...vpaForm, updatePolicy: value as VPAForm["updatePolicy"] })}><SelectTrigger id="vpa-update-policy"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="Off">Off · 仅推荐</SelectItem><SelectItem value="Initial">Initial · 仅创建时</SelectItem><SelectItem value="Recreate">Recreate · 重建 Pod</SelectItem><SelectItem value="Auto">Auto · 自动应用</SelectItem><SelectItem value="InPlaceOrRecreate">InPlaceOrRecreate · 优先原地</SelectItem></SelectContent></Select></FormField>
              <FormField label="最小副本（可选）" htmlFor="vpa-min-replicas" hint="留空使用 VPA 默认行为。"><Input id="vpa-min-replicas" type="number" min={0} value={vpaForm.minReplicas} onChange={(event) => setVpaForm({ ...vpaForm, minReplicas: event.target.value })} /></FormField>
            </div>

            <div className="min-w-0 space-y-3 rounded-md border p-3">
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><div className="min-w-0"><p className="text-sm font-medium">容器资源策略</p><p className="break-words text-xs text-muted-foreground">containerName 使用 * 表示全部容器；CPU/内存上下界使用 Kubernetes quantity，例如 100m、256Mi。</p></div><Button type="button" size="sm" variant="outline" onClick={() => setVpaForm({ ...vpaForm, containerPolicies: [...vpaForm.containerPolicies, emptyVPAContainerPolicy()] })}><Plus className="h-3.5 w-3.5" />添加策略</Button></div>
              {vpaForm.containerPolicies.map((policy, index) => (
                <div key={index} className="min-w-0 space-y-3 rounded-md border bg-muted/15 p-3">
                  <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                    <FormField label="容器名" htmlFor={`vpa-policy-container-${index}`}><Input id={`vpa-policy-container-${index}`} value={policy.containerName} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, containerName: event.target.value } : item) })} placeholder="*" className="font-mono" /></FormField>
                    <FormField label="容器模式" htmlFor={`vpa-policy-mode-${index}`}><Select value={policy.mode || "inherit"} onValueChange={(value) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, mode: value === "inherit" ? "" : value as VPAContainerPolicyForm["mode"] } : item) })}><SelectTrigger id={`vpa-policy-mode-${index}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="inherit">继承默认</SelectItem><SelectItem value="Off">Off</SelectItem><SelectItem value="Auto">Auto</SelectItem></SelectContent></Select></FormField>
                    <FormField label="受控值" htmlFor={`vpa-policy-values-${index}`}><Select value={policy.controlledValues || "inherit"} onValueChange={(value) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, controlledValues: value === "inherit" ? "" : value as VPAContainerPolicyForm["controlledValues"] } : item) })}><SelectTrigger id={`vpa-policy-values-${index}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="inherit">继承默认</SelectItem><SelectItem value="RequestsAndLimits">RequestsAndLimits</SelectItem><SelectItem value="RequestsOnly">RequestsOnly</SelectItem></SelectContent></Select></FormField>
                    <FormField label="受控资源" htmlFor={`vpa-policy-resources-${index}`}><div id={`vpa-policy-resources-${index}`} className="flex flex-wrap gap-3 pt-2 text-sm"><label className="flex items-center gap-1.5"><input type="checkbox" checked={policy.controlledResources.includes("cpu")} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, controlledResources: event.target.checked ? ["cpu" as const, ...item.controlledResources.filter((resource) => resource !== "cpu")] : item.controlledResources.filter((resource) => resource !== "cpu") } : item) })} />CPU</label><label className="flex items-center gap-1.5"><input type="checkbox" checked={policy.controlledResources.includes("memory")} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, controlledResources: event.target.checked ? ["memory" as const, ...item.controlledResources.filter((resource) => resource !== "memory")] : item.controlledResources.filter((resource) => resource !== "memory") } : item) })} />内存</label></div></FormField>
                  </div>
                  <div className="grid min-w-0 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    <FormField label="Min CPU" htmlFor={`vpa-min-cpu-${index}`}><Input id={`vpa-min-cpu-${index}`} value={policy.minCPU} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, minCPU: event.target.value } : item) })} placeholder="100m" className="font-mono" /></FormField>
                    <FormField label="Min Memory" htmlFor={`vpa-min-memory-${index}`}><Input id={`vpa-min-memory-${index}`} value={policy.minMemory} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, minMemory: event.target.value } : item) })} placeholder="128Mi" className="font-mono" /></FormField>
                    <FormField label="Max CPU" htmlFor={`vpa-max-cpu-${index}`}><Input id={`vpa-max-cpu-${index}`} value={policy.maxCPU} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, maxCPU: event.target.value } : item) })} placeholder="2" className="font-mono" /></FormField>
                    <FormField label="Max Memory" htmlFor={`vpa-max-memory-${index}`}><Input id={`vpa-max-memory-${index}`} value={policy.maxMemory} onChange={(event) => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.map((item, itemIndex) => itemIndex === index ? { ...item, maxMemory: event.target.value } : item) })} placeholder="1Gi" className="font-mono" /></FormField>
                  </div>
                  {vpaForm.containerPolicies.length > 1 ? <Button type="button" size="sm" variant="ghost" onClick={() => setVpaForm({ ...vpaForm, containerPolicies: vpaForm.containerPolicies.filter((_, itemIndex) => itemIndex !== index) })}>移除策略</Button> : null}
                </div>
              ))}
            </div>
            {vpaError ? <p role="alert" className="break-words rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2 text-xs text-destructive-strong">{vpaError}</p> : null}
            <DialogFooter><Button type="button" variant="outline" onClick={() => setVpaOpen(false)} disabled={saveVpa.isPending}>取消</Button><Button type="submit" disabled={saveVpa.isPending}>{saveVpa.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editingVpa ? "保存 VPA" : "创建 VPA"}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleteHpa)} onOpenChange={(open) => { if (!open && !removeHpa.isPending) setDeleteHpa(undefined); }}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除 HPA {deleteHpa?.name}？</AlertDialogTitle><AlertDialogDescription>删除后目标工作负载将不再自动扩缩，当前副本数保持不变。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel disabled={removeHpa.isPending}>取消</AlertDialogCancel><AlertDialogAction disabled={removeHpa.isPending} onClick={(event) => { event.preventDefault(); if (deleteHpa) removeHpa.mutate(deleteHpa); }}>{removeHpa.isPending ? "正在删除…" : "确认删除"}</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={Boolean(deletePdb)} onOpenChange={(open) => { if (!open && !removePdb.isPending) setDeletePdb(undefined); }}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除 PDB {deletePdb?.name}？</AlertDialogTitle><AlertDialogDescription>删除后该组 Pod 将不再受自愿中断预算保护，节点维护可能同时驱逐更多 Pod。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel disabled={removePdb.isPending}>取消</AlertDialogCancel><AlertDialogAction disabled={removePdb.isPending} onClick={(event) => { event.preventDefault(); if (deletePdb) removePdb.mutate(deletePdb); }}>{removePdb.isPending ? "正在删除…" : "确认删除"}</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={Boolean(deleteVpa)} onOpenChange={(open) => { if (!open && !removeVpa.isPending) setDeleteVpa(undefined); }}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除 VPA {deleteVpa?.name}？</AlertDialogTitle><AlertDialogDescription>删除后目标工作负载不再接收垂直资源建议；已写入 Pod 的 requests/limits 不会被自动回滚。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel disabled={removeVpa.isPending}>取消</AlertDialogCancel><AlertDialogAction disabled={removeVpa.isPending} onClick={(event) => { event.preventDefault(); if (deleteVpa) removeVpa.mutate(deleteVpa); }}>{removeVpa.isPending ? "正在删除…" : "确认删除"}</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function AutoscalerCard({ item, canEdit, canDelete, onEdit, onDelete }: {
  item: AutoscalerSummary;
  canEdit: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader className="gap-2">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0"><CardTitle className="break-words text-base">{item.name}</CardTitle><CardDescription className="mt-1 break-words">{item.namespace} · <Link to="/workloads/$kind/$namespace/$name" params={{ kind: item.targetKind.toLowerCase(), namespace: item.namespace, name: item.targetName }} className="text-primary hover:underline">{item.targetKind}/{item.targetName}</Link></CardDescription></div>
          <Badge variant={statusVariant(item.status)} className="shrink-0">{statusLabel(item.status)}</Badge>
        </div>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4">
        <dl className="grid min-w-0 grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <MetricItem label="当前 / 期望" value={`${item.currentReplicas} / ${item.desiredReplicas}`} />
          <MetricItem label="最小 / 最大" value={`${item.minReplicas} / ${item.maxReplicas}`} />
          <MetricItem label="最近扩缩" value={item.lastScaleTime ? formatRecent(item.lastScaleTime) : "—"} />
          <MetricItem label="创建时间" value={formatRecent(item.createdAt)} />
        </dl>
        <div className="min-w-0 space-y-2">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">指标与目标</p>
          {item.metrics.length ? <div className="grid min-w-0 gap-2 sm:grid-cols-2">{item.metrics.map((metric, index) => <div key={`${metric.type}-${metric.resource ?? index}`} className="min-w-0 rounded-md border bg-muted/20 px-3 py-2 text-xs"><p className="font-medium">{metric.resource?.toUpperCase() || metric.type}</p><p className="mt-1 break-words text-muted-foreground">当前 {metric.current || "不可用"} · 目标 {metric.target || "未声明"}</p></div>)}</div> : <p className="text-xs text-muted-foreground">Metrics Server 暂未返回当前指标；HPA 仍会根据目标利用率进行调度。</p>}
        </div>
        <ConditionDetails conditions={item.conditions} />
        <div className="flex min-w-0 flex-wrap gap-2">
          {canEdit ? <Button size="sm" variant="outline" onClick={onEdit}><Pencil className="h-3.5 w-3.5" />编辑</Button> : null}
          {canDelete ? <Button size="sm" variant="outline" className="text-destructive-strong" onClick={onDelete}><Trash2 className="h-3.5 w-3.5" />删除</Button> : null}
        </div>
      </CardContent>
    </Card>
  );
}

function DisruptionBudgetCard({ item, canEdit, canDelete, onEdit, onDelete }: {
  item: DisruptionBudgetSummary;
  canEdit: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader className="gap-2">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0"><CardTitle className="break-words text-base">{item.name}</CardTitle><CardDescription className="mt-1 break-words">{item.namespace} · {item.minAvailable !== undefined ? `minAvailable=${item.minAvailable}` : `maxUnavailable=${item.maxUnavailable ?? "—"}`}</CardDescription></div>
          <Badge variant={statusVariant(item.status)} className="shrink-0">{statusLabel(item.status)}</Badge>
        </div>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4">
        <dl className="grid min-w-0 grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <MetricItem label="健康 / 期望" value={`${item.currentHealthy} / ${item.desiredHealthy}`} />
          <MetricItem label="预期 Pod" value={String(item.expectedPods)} />
          <MetricItem label="允许驱逐" value={String(item.disruptionsAllowed)} />
          <MetricItem label="创建时间" value={formatRecent(item.createdAt)} />
        </dl>
        <div className="min-w-0 space-y-2"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Pod 选择器</p><div className="flex min-w-0 flex-wrap gap-1.5">{Object.entries(item.selector).map(([key, value]) => <Badge key={key} variant="outline" className="max-w-full whitespace-normal break-all font-mono">{key}={value}</Badge>)}</div></div>
        <p className="min-w-0 break-words rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">{disruptionExplanation(item)}</p>
        <ConditionDetails conditions={item.conditions} />
        <div className="flex min-w-0 flex-wrap gap-2">
          {canEdit ? <Button size="sm" variant="outline" onClick={onEdit}><Pencil className="h-3.5 w-3.5" />编辑</Button> : null}
          {canDelete ? <Button size="sm" variant="outline" className="text-destructive-strong" onClick={onDelete}><Trash2 className="h-3.5 w-3.5" />删除</Button> : null}
        </div>
      </CardContent>
    </Card>
  );
}

function VerticalAutoscalerCard({ item, canEdit, canDelete, onEdit, onDelete }: {
  item: VerticalAutoscalerSummary;
  canEdit: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader className="gap-2">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0"><CardTitle className="break-words text-base">{item.name}</CardTitle><CardDescription className="mt-1 break-words">{item.namespace} · <Link to="/workloads/$kind/$namespace/$name" params={{ kind: item.targetKind.toLowerCase(), namespace: item.namespace, name: item.targetName }} className="text-primary hover:underline">{item.targetKind}/{item.targetName}</Link></CardDescription></div>
          <Badge variant={statusVariant(item.status)} className="shrink-0">{statusLabel(item.status)}</Badge>
        </div>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4">
        <dl className="grid min-w-0 grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <MetricItem label="更新策略" value={item.updatePolicy} />
          <MetricItem label="最小副本" value={item.minReplicas === undefined ? "默认" : String(item.minReplicas)} />
          <MetricItem label="建议容器" value={String(item.recommendation.length)} />
          <MetricItem label="建议时间" value={item.recommendationAge ? formatRecent(item.recommendationAge) : "等待推荐"} />
        </dl>
        {item.message ? <p className="min-w-0 break-words rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">{item.message}</p> : null}
        <div className="min-w-0 space-y-2">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">资源建议</p>
          {item.recommendation.length ? (
            <div className="min-w-0 overflow-x-auto rounded-md border">
              <table className="w-full min-w-[42rem] text-left text-xs">
                <thead className="bg-muted/40 text-muted-foreground"><tr><th className="px-3 py-2 font-medium">容器</th><th className="px-3 py-2 font-medium">资源</th><th className="px-3 py-2 font-medium">Lower</th><th className="px-3 py-2 font-medium">Target</th><th className="px-3 py-2 font-medium">Upper</th><th className="px-3 py-2 font-medium">Uncapped</th></tr></thead>
                <tbody className="divide-y">
                  {item.recommendation.flatMap((recommendation) => (["cpu", "memory"] as const).map((resource) => (
                    <tr key={`${recommendation.containerName}-${resource}`}>
                      <td className="max-w-48 break-all px-3 py-2 font-mono">{recommendation.containerName}</td>
                      <td className="px-3 py-2">{resource.toUpperCase()}</td>
                      <td className="px-3 py-2 font-mono">{recommendation.lowerBound?.[resource] ?? "—"}</td>
                      <td className="px-3 py-2 font-mono font-semibold">{recommendation.target[resource] ?? "—"}</td>
                      <td className="px-3 py-2 font-mono">{recommendation.upperBound?.[resource] ?? "—"}</td>
                      <td className="px-3 py-2 font-mono">{recommendation.uncappedTarget?.[resource] ?? "—"}</td>
                    </tr>
                  )))}
                </tbody>
              </table>
            </div>
          ) : <p className="rounded-md border border-dashed px-3 py-4 text-xs text-muted-foreground">VPA 尚未生成资源建议。请确认 VPA 控制器、Metrics Server 与目标工作负载均正常。</p>}
        </div>
        {item.containerPolicies.length ? (
          <div className="min-w-0 space-y-2"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">容器策略</p><div className="flex min-w-0 flex-wrap gap-1.5">{item.containerPolicies.map((policy, index) => <Badge key={`${policy.containerName}-${index}`} variant="outline" className="max-w-full whitespace-normal break-all font-mono">{policy.containerName}{policy.mode ? ` · ${policy.mode}` : ""}{policy.controlledValues ? ` · ${policy.controlledValues}` : ""}{(policy.controlledResources?.length ?? 0) ? ` · ${policy.controlledResources?.join("+")}` : ""}</Badge>)}</div></div>
        ) : null}
        <ConditionDetails conditions={item.conditions} />
        <div className="flex min-w-0 flex-wrap gap-2">
          {canEdit ? <Button size="sm" variant="outline" onClick={onEdit}><Pencil className="h-3.5 w-3.5" />编辑</Button> : null}
          {canDelete ? <Button size="sm" variant="outline" className="text-destructive-strong" onClick={onDelete}><Trash2 className="h-3.5 w-3.5" />删除</Button> : null}
        </div>
      </CardContent>
    </Card>
  );
}

function ConditionDetails({ conditions }: { conditions: AutoscalerSummary["conditions"] }) {
  if (!conditions.length) return null;
  return <details className="min-w-0 rounded-md border bg-muted/10"><summary className="cursor-pointer px-3 py-2 text-xs font-medium">状态条件 ({conditions.length})</summary><div className="space-y-2 border-t p-3">{conditions.map((condition) => <div key={`${condition.type}-${condition.reason}`} className="min-w-0 text-xs"><p className="break-words font-medium">{condition.type} · {condition.status}{condition.reason ? ` · ${condition.reason}` : ""}</p>{condition.message ? <p className="mt-1 break-words text-muted-foreground">{condition.message}</p> : null}</div>)}</div></details>;
}

function MetricItem({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-0.5 break-words font-medium">{value}</dd></div>;
}

function FormField({ label, htmlFor, hint, className, children }: { label: string; htmlFor: string; hint?: string; className?: string; children: ReactNode }) {
  return <div className={`min-w-0 space-y-1.5 ${className ?? ""}`}><Label htmlFor={htmlFor}>{label}</Label>{children}{hint ? <p className="break-words text-xs text-muted-foreground">{hint}</p> : null}</div>;
}

function validateAutoscalerForm(form: AutoscalerForm): string {
  if (!DNS_LABEL.test(form.name.trim())) return "HPA 名称必须是小写 DNS-1123 标签，最长 63 字符。";
  if (!DNS_LABEL.test(form.targetName.trim())) return "目标工作负载名称必须是小写 DNS-1123 标签。";
  const min = Number(form.minReplicas);
  const max = Number(form.maxReplicas);
  if (!Number.isInteger(min) || min < 1 || min > 1000) return "最小副本必须是 1 到 1000 的整数。";
  if (!Number.isInteger(max) || max < min || max > 1000) return "最大副本必须大于等于最小副本且不超过 1000。";
  if (!form.cpuUtilization.trim() && !form.memoryUtilization.trim()) return "CPU 和内存利用率目标至少填写一项。";
  for (const [label, value] of [["CPU", form.cpuUtilization], ["内存", form.memoryUtilization]] as const) {
    if (!value.trim()) continue;
    const target = Number(value);
    if (!Number.isInteger(target) || target < 1 || target > 100) return `${label} 利用率目标必须是 1 到 100 的整数。`;
  }
  return "";
}

function validateDisruptionBudgetForm(form: DisruptionBudgetForm): { error: string; selector: Record<string, string> } {
  const selector: Record<string, string> = {};
  if (!DNS_LABEL.test(form.name.trim())) return { error: "PDB 名称必须是小写 DNS-1123 标签，最长 63 字符。", selector };
  if (!QUANTITY.test(form.value.trim())) return { error: "约束值必须是非负整数或百分比。", selector };
  if (form.value.trim().endsWith("%") && Number(form.value.trim().slice(0, -1)) > 100) return { error: "百分比约束值不能超过 100%。", selector };
  for (const line of form.selector.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    if (separator < 1) return { error: "Pod 选择器格式应为每行 key=value。", selector };
    const key = trimmed.slice(0, separator).trim();
    const value = trimmed.slice(separator + 1).trim();
    if (!key || key.length > 253 || value.length > 63 || key in selector) return { error: "Pod 选择器包含空键、重复键或超长值。", selector };
    selector[key] = value;
  }
  if (!Object.keys(selector).length) return { error: "Pod 选择器至少需要一个 key=value。", selector };
  return { error: "", selector };
}

function validateVPAForm(form: VPAForm): { error: string; policies: VPAContainerPolicy[] } {
  if (!DNS_LABEL.test(form.name.trim())) return { error: "VPA 名称必须是小写 DNS-1123 标签，最长 63 字符。", policies: [] };
  if (!DNS_LABEL.test(form.targetName.trim())) return { error: "目标工作负载名称必须是小写 DNS-1123 标签。", policies: [] };
  if (form.minReplicas.trim()) {
    const replicas = Number(form.minReplicas);
    if (!Number.isInteger(replicas) || replicas < 1 || replicas > 1000) return { error: "最小副本必须是 1 到 1000 之间的整数。", policies: [] };
  }
  const quantity = /^([+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?)([numkKMGTPE]i?|m)?$/;
  const policies: VPAContainerPolicy[] = [];
  for (const [index, policy] of form.containerPolicies.entries()) {
    const containerName = policy.containerName.trim();
    if (containerName !== "*" && !DNS_LABEL.test(containerName)) return { error: `第 ${index + 1} 个容器策略的容器名无效。`, policies: [] };
    for (const [label, value] of [["Min CPU", policy.minCPU], ["Min Memory", policy.minMemory], ["Max CPU", policy.maxCPU], ["Max Memory", policy.maxMemory]] as const) {
      if (value.trim() && !quantity.test(value.trim())) return { error: `第 ${index + 1} 个容器策略的 ${label} 不是有效的 Kubernetes quantity。`, policies: [] };
    }
    const minAllowed: Record<string, string> = {};
    const maxAllowed: Record<string, string> = {};
    if (policy.minCPU.trim()) minAllowed.cpu = policy.minCPU.trim();
    if (policy.minMemory.trim()) minAllowed.memory = policy.minMemory.trim();
    if (policy.maxCPU.trim()) maxAllowed.cpu = policy.maxCPU.trim();
    if (policy.maxMemory.trim()) maxAllowed.memory = policy.maxMemory.trim();
    policies.push({
      containerName,
      mode: policy.mode || undefined,
      controlledValues: policy.controlledValues || undefined,
      controlledResources: policy.controlledResources.length ? policy.controlledResources : undefined,
      minAllowed: Object.keys(minAllowed).length ? minAllowed : undefined,
      maxAllowed: Object.keys(maxAllowed).length ? maxAllowed : undefined,
    });
  }
  return { error: "", policies };
}

function statusVariant(status: string): "success" | "warning" | "destructive" | "secondary" | "default" {
  switch (status.toLowerCase()) {
    case "ready": return "success";
    case "scaling": return "warning";
    case "pending": return "warning";
    case "protected": return "warning";
    case "degraded":
    case "blocked": return "destructive";
    case "idle": return "secondary";
    default: return "default";
  }
}

function statusLabel(status: string): string {
  return ({ ready: "就绪", scaling: "扩缩中", pending: "等待推荐", idle: "空闲", degraded: "异常", protected: "受保护", blocked: "已阻塞" } as Record<string, string>)[status.toLowerCase()] ?? status;
}

function disruptionExplanation(item: DisruptionBudgetSummary): string {
  if (item.status === "Blocked") return `当前健康 Pod ${item.currentHealthy} 个，低于期望健康数 ${item.desiredHealthy}，自愿驱逐已被阻止。请恢复副本或检查就绪探针。`;
  if (item.status === "Protected") return "当前没有可安全驱逐的 Pod；这是保护状态，并不代表 PDB 配置错误。";
  return `当前允许驱逐 ${item.disruptionsAllowed} 个 Pod；节点维护会遵守该上限逐个处理。`;
}

function formatRecent(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
}
