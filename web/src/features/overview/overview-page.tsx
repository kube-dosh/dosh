import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Boxes, CircleDot, Cpu, MemoryStick, Server, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import { MetricCard } from "@/components/data/metric-card";
import { ResourceBar } from "@/components/data/resource-bar";
import { StatusBadge } from "@/components/feedback/status-badge";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatCPU, formatDate, formatMemory, percent } from "@/lib/utils";
import { useOverviewQuery } from "@/features/overview/use-overview-query";

export function OverviewPage() {
  const overview = useOverviewQuery();
  const nodes = useQuery({ queryKey: ["nodes", "overview"], queryFn: () => api.nodes(), staleTime: 15_000 });
  if (overview.isLoading) return <LoadingState label="正在加载集群总览" className="min-h-[60vh]" />;
  if (overview.isError) return <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />;
  const data = overview.data!;
  const requestCPUPercent = percent(data.capacity.cpuRequestsMilli, data.capacity.cpuAllocatableMilli);
  const requestMemoryPercent = percent(data.capacity.memoryRequestsBytes, data.capacity.memoryAllocatableBytes);
  const cpuPercent = data.cluster.metricsAvailable ? percent(data.capacity.cpuUsageMilli, data.capacity.cpuAllocatableMilli) : requestCPUPercent;
  const memoryPercent = data.cluster.metricsAvailable ? percent(data.capacity.memoryUsageBytes, data.capacity.memoryAllocatableBytes) : requestMemoryPercent;
  return (
    <>
      <PageHeader title="集群总览" breadcrumb={`总览 / ${data.cluster.name || "未知集群"}`} description={<span className="flex flex-wrap items-center gap-2"><StatusBadge status={data.cluster.status} /> Kubernetes {data.cluster.version} · {data.cluster.cni || "CNI 未知"}</span>} actions={<Button variant="outline" onClick={() => { void overview.refetch(); void nodes.refetch(); }}>刷新</Button>} />
      {data.warnings?.length ? <div className="mb-4 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-sm text-warning"><TriangleAlert className="mr-2 inline h-4 w-4" />部分数据暂不可用：{data.warnings.join("，")}</div> : null}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="节点" value={`${data.counts.readyNodes}/${data.counts.nodes}`} hint="Ready / 总数" icon={Server} tone={data.counts.readyNodes === data.counts.nodes ? "success" : "warning"} />
        <MetricCard label="Pod" value={`${data.counts.runningPods}/${data.counts.pods}`} hint="Running / 总数" icon={CircleDot} tone={data.counts.unhealthyPods ? "warning" : "success"} />
        <MetricCard label="工作负载" value={data.counts.workloads} hint={`${data.counts.namespaces} 个命名空间`} icon={Boxes} tone="primary" />
        <MetricCard label="异常 Pod" value={data.counts.unhealthyPods} hint={`${data.counts.warningEvents} 条 Warning 事件`} icon={TriangleAlert} tone={data.counts.unhealthyPods ? "danger" : "neutral"} />
      </div>
      <div className="mt-4 grid gap-4 xl:grid-cols-[1.15fr_0.85fr]">
        <Card>
          <CardHeader><CardTitle>资源使用</CardTitle><CardDescription>{data.cluster.metricsAvailable ? "实际使用量来自 Metrics API，requests 同时保留用于调度容量判断。" : "Metrics API 不可用，当前展示 Pod requests 与节点 allocatable。"}</CardDescription></CardHeader>
          <CardContent className="space-y-5">
            <div className="grid gap-5 sm:grid-cols-2">
              <div className="rounded-lg border bg-muted/25 p-4">
                <div className="flex items-center justify-between"><span className="flex items-center gap-2 text-sm font-medium"><Cpu className="h-4 w-4 text-primary" />CPU</span><span className="text-xl font-semibold tabular-nums">{cpuPercent}%</span></div>
                <Progress value={cpuPercent} className="mt-4 h-2" aria-label="CPU 使用率" aria-valuetext={`${cpuPercent}%`} />
                <p className="mt-2 text-xs text-muted-foreground">{data.cluster.metricsAvailable ? `${formatCPU(data.capacity.cpuUsageMilli)} 实际 / ${formatCPU(data.capacity.cpuAllocatableMilli)} · ` : ""}{formatCPU(data.capacity.cpuRequestsMilli)} requests</p>
              </div>
              <div className="rounded-lg border bg-muted/25 p-4">
                <div className="flex items-center justify-between"><span className="flex items-center gap-2 text-sm font-medium"><MemoryStick className="h-4 w-4 text-primary" />内存</span><span className="text-xl font-semibold tabular-nums">{memoryPercent}%</span></div>
                <Progress value={memoryPercent} className="mt-4 h-2" aria-label="内存使用率" aria-valuetext={`${memoryPercent}%`} />
                <p className="mt-2 text-xs text-muted-foreground">{data.cluster.metricsAvailable ? `${formatMemory(data.capacity.memoryUsageBytes)} 实际 / ${formatMemory(data.capacity.memoryAllocatableBytes)} · ` : ""}{formatMemory(data.capacity.memoryRequestsBytes)} requests</p>
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="rounded-lg border bg-muted/25 p-4">
                <ResourceBar label="Pod 容量" used={data.counts.pods} total={Number(data.capacity.podCapacity)} displayUsed={String(data.counts.pods)} displayTotal={String(data.capacity.podCapacity)} />
              </div>
              <div className="flex items-center justify-between rounded-lg border bg-muted/25 p-4 text-sm"><span className="text-muted-foreground">Metrics Server</span><StatusBadge status={data.cluster.metricsAvailable ? "Available" : "Not installed"} /></div>
            </div>
            {!data.cluster.metricsAvailable ? <p className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">当前未安装 Metrics Server，因此仅展示容量与 requests/limits，不展示实际利用率。</p> : null}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><div className="flex items-center justify-between"><div><CardTitle>节点状态</CardTitle><CardDescription>节点 Ready 状态与资源压力</CardDescription></div><Button variant="ghost" size="sm" asChild><Link to="/nodes">全部节点</Link></Button></div></CardHeader>
          <CardContent className="space-y-2">
            {nodes.isLoading ? <LoadingState className="h-40" /> : nodes.data?.items.slice(0, 6).map((node) => (
              <Link key={node.name} to="/nodes/$name" params={{ name: node.name }} className="flex items-center justify-between rounded-md border px-3 py-2.5 transition-colors hover:bg-muted/40">
                <div className="min-w-0"><p className="truncate text-sm font-medium">{node.name}</p><p className="text-xs text-muted-foreground">{node.roles.join(", ")} · {node.internalIP}</p></div>
                <StatusBadge status={node.unschedulable ? "SchedulingDisabled" : node.status} />
              </Link>
            ))}
          </CardContent>
        </Card>
      </div>
      <Card className="mt-4">
        <CardHeader><div className="flex items-center justify-between"><div><CardTitle>最近事件</CardTitle><CardDescription>按最近发生时间排序</CardDescription></div><Button variant="ghost" size="sm" asChild><Link to="/events">查看全部</Link></Button></div></CardHeader>
        <CardContent className="p-0">
          <Table><TableHeader><TableRow><TableHead>类型</TableHead><TableHead>对象</TableHead><TableHead>原因</TableHead><TableHead>消息</TableHead><TableHead>时间</TableHead></TableRow></TableHeader><TableBody>
            {data.recentEvents.length ? data.recentEvents.map((event, index) => (
              <TableRow key={`${event.objectKind}-${event.objectName}-${index}`}>
                <TableCell><StatusBadge status={event.type} /></TableCell><TableCell className="font-mono text-xs">{event.objectKind}/{event.objectName}</TableCell><TableCell>{event.reason}</TableCell><TableCell className="max-w-xl truncate text-muted-foreground">{event.message}</TableCell><TableCell className="whitespace-nowrap text-muted-foreground">{formatDate(event.lastSeen)}</TableCell>
              </TableRow>
            )) : <TableRow><TableCell colSpan={5} className="h-24 text-center text-muted-foreground">暂无事件</TableCell></TableRow>}
          </TableBody></Table>
        </CardContent>
      </Card>
    </>
  );
}

