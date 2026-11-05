import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Ban, CirclePlay, Cpu, FileCode2, HardDrive, Loader2, MemoryStick, ScrollText, Server } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { Condition, PodSummary, Taint } from "@/api/types";
import { createColumnHelper } from "@tanstack/react-table";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { ResourceBar } from "@/components/data/resource-bar";
import { StatusBadge } from "@/components/feedback/status-badge";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { YamlDialog, YamlPanel } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { useCapabilities } from "@/features/auth/use-session";
import { ResourceEvents } from "@/features/events/resource-events";
import { PodLogs } from "@/features/workloads/pod-logs";
import { formatAge, formatCPU, formatDate, formatMemory } from "@/lib/utils";

const podColumn = createColumnHelper<PodSummary>();
const nodeTabs = ["overview", "pods", "events", "logs", "yaml"] as const;
type NodeTab = (typeof nodeTabs)[number];

function isNodeTab(value: string | undefined): value is NodeTab {
  return nodeTabs.includes(value as NodeTab);
}

export function NodeDetailPage() {
  const params = useParams({ strict: false }) as { name: string };
  const search = useSearch({ from: "/app/nodes/$name" });
  const navigate = useNavigate();
  const { name } = params;
  const queryClient = useQueryClient();
  const [yamlOpen, setYamlOpen] = useState(false);
  const [schedulingOpen, setSchedulingOpen] = useState(false);
  const tab = isNodeTab(search.tab) ? search.tab : "overview";
  const changeTab = (value: string) => {
    if (!isNodeTab(value)) return;
    void navigate({
      to: "/nodes/$name",
      params: { name },
      search: { ...search, tab: value === "overview" ? undefined : value },
      replace: true,
    });
  };
  const query = useQuery({ queryKey: ["node", name], queryFn: () => api.node(name) });
  const capabilities = useCapabilities().data;
  const canCordon = Boolean(capabilities?.nodeCordon);
  const scheduling = useMutation({
    mutationFn: (action: "cordon" | "uncordon") => action === "cordon" ? api.cordonNode(name) : api.uncordonNode(name),
    onSuccess: async (_node, action) => {
      toast.success(action === "cordon" ? "节点已停止调度" : "节点已恢复调度");
      setSchedulingOpen(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["node", name] }),
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
      ]);
    },
    onError: (error) => toast.error(error.message),
  });
  if (query.isLoading) return <LoadingState label="正在加载节点详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const node = query.data!;
  return (
    <>
      <PageHeader breadcrumb={<><Link to="/nodes" className="text-primary underline underline-offset-2">节点管理</Link> / {node.name}</>} title={<span className="flex items-center gap-3"><Server className="h-5 w-5 text-muted-foreground" />{node.name}</span>} description={<span className="flex items-center gap-2"><StatusBadge status={node.unschedulable ? "SchedulingDisabled" : node.status} />{node.roles.map((role) => <Badge key={role} variant="secondary">{role}</Badge>)}</span>} actions={<div className="flex items-center gap-2">{canCordon ? <Button variant="outline" onClick={() => setSchedulingOpen(true)}>{node.unschedulable ? <CirclePlay className="h-4 w-4" /> : <Ban className="h-4 w-4" />}{node.unschedulable ? "恢复调度" : "停止调度"}</Button> : null}<Button variant="outline" onClick={() => setYamlOpen(true)}><FileCode2 className="h-4 w-4" />YAML</Button></div>} />
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="pods">实例 ({node.pods.length})</TabsTrigger><TabsTrigger value="events">事件</TabsTrigger><TabsTrigger value="logs"><ScrollText className="h-3.5 w-3.5" />日志</TabsTrigger><TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger></TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-[1.05fr_0.95fr]">
            <Card><CardHeader><CardTitle>节点信息</CardTitle></CardHeader><CardContent><dl className="grid gap-4 sm:grid-cols-2"><DetailItem label="内部 IP"><span className="font-mono">{node.internalIP}</span></DetailItem><DetailItem label="Kubelet">{node.version}</DetailItem><DetailItem label="操作系统">{node.osImage}</DetailItem><DetailItem label="内核">{node.kernelVersion}</DetailItem><DetailItem label="容器运行时">{node.containerRuntime}</DetailItem><DetailItem label="创建时间">{formatDate(node.createdAt)}</DetailItem></dl></CardContent></Card>
            <Card><CardHeader><CardTitle>资源使用</CardTitle><CardDescription>{node.hasMetrics ? "实际使用量来自 Metrics API，requests 同时展示" : "Metrics API 不可用，仅展示 requests"}</CardDescription></CardHeader><CardContent className="space-y-5">{node.hasMetrics ? <><ResourceBar label="CPU 实际使用" used={node.cpuUsageMilli ?? 0} total={node.cpuCapacityMilli} displayUsed={formatCPU(node.cpuUsageMilli ?? 0)} displayTotal={formatCPU(node.cpuCapacityMilli)} /><ResourceBar label="内存实际使用" used={node.memoryUsageBytes ?? 0} total={node.memoryCapacityBytes} displayUsed={formatMemory(node.memoryUsageBytes ?? 0)} displayTotal={formatMemory(node.memoryCapacityBytes)} /></> : null}<ResourceBar label="CPU requests" used={node.cpuRequestsMilli} total={node.cpuCapacityMilli} displayUsed={formatCPU(node.cpuRequestsMilli)} displayTotal={formatCPU(node.cpuCapacityMilli)} /><ResourceBar label="内存 requests" used={node.memoryRequestsBytes} total={node.memoryCapacityBytes} displayUsed={formatMemory(node.memoryRequestsBytes)} displayTotal={formatMemory(node.memoryCapacityBytes)} /><ResourceBar label="Pod" used={node.podCount} total={node.podCapacity} displayUsed={String(node.podCount)} displayTotal={String(node.podCapacity)} /><div className="grid grid-cols-3 gap-3 border-t pt-4 text-center"><Mini icon={Cpu} label="CPU" value={formatCPU(node.cpuCapacityMilli)} /><Mini icon={MemoryStick} label="内存" value={formatMemory(node.memoryCapacityBytes)} /><Mini icon={HardDrive} label="Pod" value={String(node.podCapacity)} /></div></CardContent></Card>
          </div>
          <div className="mt-4 grid gap-4 xl:grid-cols-2">
            <Card><CardHeader><CardTitle>Conditions</CardTitle></CardHeader><CardContent className="p-0"><SimpleConditions conditions={node.conditions} /></CardContent></Card>
            <Card><CardHeader><CardTitle>Taints</CardTitle></CardHeader><CardContent className="p-0"><SimpleTaints taints={node.taints} /></CardContent></Card>
          </div>
          <Card className="mt-4"><CardHeader><CardTitle>标签与注解</CardTitle></CardHeader><CardContent className="grid gap-5 lg:grid-cols-2"><MetadataTable title="Labels" data={node.labels} /><MetadataTable title="Annotations" data={node.annotations} /></CardContent></Card>
        </TabsContent>
        <TabsContent value="pods">
          <Card><CardHeader><CardTitle>节点上的 Pod</CardTitle><CardDescription>{node.pods.length} 个实例</CardDescription></CardHeader><CardContent className="px-0 pb-0"><DataTable columns={[podColumn.accessor("name", { header: "名称", cell: (info) => <Link to="/pods/$namespace/$name" params={{ namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }), podColumn.accessor("namespace", { header: "命名空间" }), podColumn.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }), podColumn.accessor("ready", { header: "就绪" }), podColumn.accessor("restarts", { header: "重启" }), podColumn.accessor("createdAt", { header: "年龄", cell: (info) => formatAge(info.getValue()) })]} data={node.pods} pageSize={10} /></CardContent></Card>
        </TabsContent>
        <TabsContent value="events"><ResourceEvents namespace="" objectKind="Node" objectName={node.name} /></TabsContent>
        <TabsContent value="logs"><PodLogs pods={node.pods} emptyMessage="该节点当前没有可查看日志的 Pod。" /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type="node" name={node.name} height="64vh" /></TabsContent>
      </Tabs>
      <AlertDialog open={schedulingOpen} onOpenChange={setSchedulingOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{node.unschedulable ? "恢复节点调度？" : "停止节点调度？"}</AlertDialogTitle>
            <AlertDialogDescription>{node.unschedulable ? "恢复后，新创建的 Pod 可以重新调度到该节点。已有 Pod 不受影响。" : "停止调度后，新创建的 Pod 将不再调度到该节点，已有 Pod 不会被驱逐。请确认该节点上的业务容量仍然充足。"}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => { event.preventDefault(); scheduling.mutate(node.unschedulable ? "uncordon" : "cordon"); }} disabled={scheduling.isPending}>
              {scheduling.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : node.unschedulable ? <CirclePlay className="h-4 w-4" /> : <Ban className="h-4 w-4" />}
              {node.unschedulable ? "确认恢复" : "确认停止调度"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <YamlDialog open={yamlOpen} onOpenChange={setYamlOpen} type="node" name={node.name} />
    </>
  );
}

function Mini({ icon: Icon, label, value }: { icon: typeof Cpu; label: string; value: string }) {
  return <div><Icon className="mx-auto h-4 w-4 text-muted-foreground" /><p className="mt-1 text-xs text-muted-foreground">{label}</p><p className="text-sm font-semibold tabular-nums">{value}</p></div>;
}

function SimpleConditions({ conditions }: { conditions: Condition[] }) {
  return <Table><TableHeader><TableRow><TableHead>类型</TableHead><TableHead>状态</TableHead><TableHead>原因</TableHead><TableHead>更新时间</TableHead></TableRow></TableHeader><TableBody>{conditions.map((condition) => <TableRow key={condition.type}><TableCell>{condition.type}</TableCell><TableCell><StatusBadge status={condition.status === "True" ? "Ready" : condition.status === "False" ? "Failed" : condition.status} /></TableCell><TableCell className="text-muted-foreground">{condition.reason || "—"}</TableCell><TableCell className="text-muted-foreground">{formatDate(condition.lastTransitionTime)}</TableCell></TableRow>)}</TableBody></Table>;
}

function SimpleTaints({ taints }: { taints: Taint[] }) {
  if (!taints.length) return <p className="px-5 py-8 text-sm text-muted-foreground">没有配置 Taint。</p>;
  return <div className="space-y-2 p-5">{taints.map((taint) => <div key={`${taint.key}-${taint.effect}`} className="flex items-center justify-between rounded-md border px-3 py-2 text-sm"><span className="font-mono">{taint.key}{taint.value ? `=${taint.value}` : ""}</span><Badge variant="warning">{taint.effect}</Badge></div>)}</div>;
}

function MetadataTable({ title, data }: { title: string; data: Record<string, string> }) {
  const entries = Object.entries(data || {});
  return <div><h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h4><div className="max-h-64 overflow-auto rounded-md border scrollbar-thin">{entries.length ? entries.map(([key, value]) => <div key={key} className="grid grid-cols-[minmax(120px,0.4fr)_1fr] gap-3 border-b px-3 py-2 text-xs last:border-0"><span className="break-all font-mono text-muted-foreground">{key}</span><span className="break-all font-mono">{value}</span></div>) : <p className="p-3 text-sm text-muted-foreground">无</p>}</div></div>;
}

