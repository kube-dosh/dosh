import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Box, FileCode2, ScrollText, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { ContainerInfo } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { PageHeader } from "@/components/layout/page-header";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { YamlDialog, YamlPanel } from "@/components/resources/yaml-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ResourceEvents } from "@/features/events/resource-events";
import { useCapabilities } from "@/features/auth/use-session";
import { LogViewer } from "@/features/workloads/log-viewer";
import { resourceCapabilities } from "@/lib/capabilities";
import { formatCPU, formatDate, formatMemory } from "@/lib/utils";

const column = createColumnHelper<ContainerInfo>();
const podTabs = ["overview", "containers", "events", "logs", "yaml"] as const;
type PodTab = (typeof podTabs)[number];

function isPodTab(value: string | undefined): value is PodTab {
  return podTabs.includes(value as PodTab);
}

export function PodDetailPage() {
  const params = useParams({ strict: false }) as { namespace: string; name: string };
  const search = useSearch({ from: "/app/pods/$namespace/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [yamlOpen, setYamlOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const tab = isPodTab(search.tab) ? search.tab : "overview";
  const changeTab = (value: string) => {
    if (!isPodTab(value)) return;
    void navigate({
      to: "/pods/$namespace/$name",
      params: { namespace: params.namespace, name: params.name },
      search: { ...search, tab: value === "overview" ? undefined : value },
      replace: true,
    });
  };
  const capabilities = useCapabilities(params.namespace).data;
  const podCaps = resourceCapabilities(capabilities, "pods");
  const query = useQuery({ queryKey: ["pod", params.namespace, params.name], queryFn: () => api.pod(params.namespace, params.name), refetchInterval: 20_000 });
  if (query.isLoading) return <LoadingState label="正在加载 Pod 详情" className="min-h-[60vh]" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const pod = query.data!;
  return (
    <>
      <PageHeader
        title={pod.name}
        breadcrumb={<span>工作负载 / Pod / {pod.namespace}</span>}
        description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={pod.status} /><span>{pod.ready} 就绪</span><span>·</span><span>{pod.node}</span></span>}
        actions={<div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setYamlOpen(true)}><FileCode2 className="h-4 w-4" />YAML</Button>
          {podCaps.delete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}><Trash2 className="h-4 w-4" />删除</Button> : null}
        </div>}
      />
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList><TabsTrigger value="overview">概览</TabsTrigger><TabsTrigger value="containers">实例 ({pod.containers.length})</TabsTrigger><TabsTrigger value="events">事件</TabsTrigger><TabsTrigger value="logs"><ScrollText className="h-3.5 w-3.5" />日志</TabsTrigger><TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger></TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-2">
            <Card><CardHeader><CardTitle>Pod 信息</CardTitle></CardHeader><CardContent><dl className="grid gap-5 sm:grid-cols-2">
              <DetailItem label="名称" value={pod.name} mono />
              <DetailItem label="命名空间" value={pod.namespace} />
              <DetailItem label="状态" value={<StatusBadge status={pod.status} />} />
              <DetailItem label="就绪" value={pod.ready} />
              <DetailItem label="Pod IP" value={pod.ip || "—"} mono />
              <DetailItem label="节点" value={pod.node || "—"} mono />
              <DetailItem label="QoS Class" value={pod.qosClass || "—"} />
              <DetailItem label="Service Account" value={pod.serviceAccount || "default"} mono />
              <DetailItem label="创建时间" value={formatDate(pod.createdAt)} />
              <DetailItem label="重启次数" value={String(pod.restarts)} />
              <DetailItem label="CPU 使用" value={pod.hasMetrics ? formatCPU(pod.cpuUsageMilli ?? 0) : "Metrics 不可用"} />
              <DetailItem label="内存使用" value={pod.hasMetrics ? formatMemory(pod.memoryUsageBytes ?? 0) : "Metrics 不可用"} />
            </dl></CardContent></Card>
            <Card><CardHeader><CardTitle>控制器与标签</CardTitle></CardHeader><CardContent className="space-y-5">
              <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">归属控制器</p>{pod.ownerKind ? <Link to="/workloads" className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm hover:bg-muted"><Box className="h-4 w-4" />{pod.ownerKind}/{pod.ownerName}</Link> : <p className="text-sm text-muted-foreground">无控制器（裸 Pod）</p>}</div>
              <div><p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Labels</p><div className="flex flex-wrap gap-1.5">{Object.entries(pod.labels || {}).map(([key, value]) => <span key={key} className="rounded border bg-muted/30 px-2 py-1 font-mono text-[11px]">{key}={value}</span>)}</div></div>
            </CardContent></Card>
            <Card className="xl:col-span-2"><CardHeader><CardTitle>Pod Conditions</CardTitle></CardHeader><CardContent className="space-y-2">{pod.conditions.length ? pod.conditions.map((condition) => <div key={condition.type} className="grid gap-2 rounded-md border px-3 py-2.5 text-sm sm:grid-cols-[180px_120px_1fr]"><span className="font-medium">{condition.type}</span><StatusBadge status={condition.status === "True" ? "Ready" : "Failed"} /><span className="text-muted-foreground">{condition.message || condition.reason || "—"}</span></div>) : <p className="py-6 text-center text-sm text-muted-foreground">暂无 Condition 数据</p>}</CardContent></Card>
          </div>
        </TabsContent>
        <TabsContent value="logs"><LogViewer namespace={pod.namespace} name={pod.name} containers={pod.containers} /></TabsContent>
        <TabsContent value="containers">
          <DataTable columns={[
            column.accessor("name", { header: "容器", cell: (info) => <span className="font-medium">{info.getValue()}</span> }),
            column.accessor("image", { header: "镜像", cell: (info) => <span className="block max-w-96 truncate font-mono text-xs" title={info.getValue()}>{info.getValue()}</span> }),
            column.accessor("ready", { header: "就绪", cell: (info) => <StatusBadge status={info.getValue() ? "Ready" : "NotReady"} /> }),
            column.accessor("state", { header: "状态", cell: (info) => <span>{info.getValue()}{info.row.original.reason ? ` · ${info.row.original.reason}` : ""}</span> }),
            column.accessor("restartCount", { header: "重启" }),
            column.accessor("cpuRequestMilli", { header: "CPU Request/Limit", cell: (info) => <span className="text-xs">{formatCPU(info.getValue())} / {formatCPU(info.row.original.cpuLimitMilli)}</span> }),
            column.accessor("memoryRequestBytes", { header: "内存 Request/Limit", cell: (info) => <span className="text-xs">{formatMemory(info.getValue())} / {formatMemory(info.row.original.memoryLimitBytes)}</span> }),
          ]} data={pod.containers} />
        </TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={pod.namespace} objectKind="Pod" objectName={pod.name} /></TabsContent>
        <TabsContent value="yaml"><YamlPanel type="pod" name={pod.name} namespace={pod.namespace} height="64vh" /></TabsContent>
      </Tabs>
      <YamlDialog open={yamlOpen} onOpenChange={setYamlOpen} type="pod" name={pod.name} namespace={pod.namespace} />
      <DeleteResourceDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        resourceKind="Pod"
        name={pod.name}
        description="Pod 将被删除；若由 Deployment/StatefulSet 等控制器管理，控制器会自动创建替代实例。"
        onConfirm={async () => {
          await api.deletePod(pod.namespace, pod.name);
          await queryClient.invalidateQueries({ queryKey: ["pods"] });
          await queryClient.invalidateQueries({ queryKey: ["workloads"] });
          await navigate({ to: "/pods" });
        }}
      />
    </>
  );
}
