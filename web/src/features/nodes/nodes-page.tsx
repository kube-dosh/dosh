import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Eye, FileCode2, Server, Settings2, ShieldOff, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { NodeSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ResourceBar } from "@/components/data/resource-bar";
import { StatusBadge } from "@/components/feedback/status-badge";
import { ErrorState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { ResourceActionsMenu, type ResourceAction } from "@/components/resources/resource-actions-menu";
import { YamlDialog } from "@/components/resources/yaml-dialog";
import { Badge } from "@/components/ui/badge";
import { useCapabilities } from "@/features/auth/use-session";
import { cursorPaginationProps, useCursorPagination } from "@/hooks/use-cursor-pagination";
import { formatAge, formatCPU, formatMemory } from "@/lib/utils";

const column = createColumnHelper<NodeSummary>();

export function NodesPage() {
  const search = useSearch({ from: "/app/nodes" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const [yamlTarget, setYamlTarget] = useState<NodeSummary>();
  const pager = useCursorPagination(`nodes|${q}`, 20);
  const query = useQuery({
    queryKey: ["nodes", q, pager.page, pager.cursor],
    queryFn: () => api.nodes({ q, page: pager.page, pageSize: pager.pageSize, cursor: pager.cursor }),
    refetchInterval: 15_000,
  });
  const capabilities = useCapabilities().data;
  const setScheduling = useMutation({
    mutationFn: (node: NodeSummary) => node.unschedulable ? api.uncordonNode(node.name) : api.cordonNode(node.name),
    onSuccess: async (_, node) => { toast.success(`${node.name} 已${node.unschedulable ? "恢复调度" : "停止调度"}`); await query.refetch(); },
    onError: (error) => toast.error(error.message),
  });
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const actions = (node: NodeSummary): ResourceAction[] => [
    { label: "查看详情", icon: <Eye className="h-4 w-4" />, onSelect: () => void navigate({ to: "/nodes/$name", params: { name: node.name } }) },
    { label: "标签与污点", icon: <Settings2 className="h-4 w-4" />, onSelect: () => void navigate({ to: "/nodes/$name", params: { name: node.name } }) },
    { label: "查看 YAML", icon: <FileCode2 className="h-4 w-4" />, onSelect: () => setYamlTarget(node) },
    { label: node.unschedulable ? "恢复调度" : "停止调度", icon: node.unschedulable ? <ShieldCheck className="h-4 w-4" /> : <ShieldOff className="h-4 w-4" />, onSelect: () => setScheduling.mutate(node), disabled: !capabilities?.nodeCordon, separatorBefore: true },
  ];
  return (
    <>
      <PageHeader title="节点管理" breadcrumb="节点管理 / 节点" description="查看 Control Plane 和 Worker 节点状态、资源容量与调度状态。" actions={<SearchInput value={q} onChange={(value) => void navigate({ to: "/nodes", search: { ...search, q: value || undefined }, replace: true })} placeholder="搜索节点名称或 IP" />} />
      <DataTable
        columns={[
          column.accessor("name", { header: "节点名称", cell: (info) => <Link to="/nodes/$name" params={{ name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.row.original.unschedulable ? "SchedulingDisabled" : info.getValue()} /> }),
          column.accessor("roles", { header: "角色", enableSorting: false, cell: (info) => <div className="flex flex-wrap gap-1">{info.getValue().map((role: string) => <Badge key={role} variant="secondary">{role}</Badge>)}</div> }),
          column.accessor("internalIP", { header: "IP 地址", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("version", { header: "版本", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("cpuRequestsMilli", { header: "CPU 分配", cell: (info) => <div className="w-36"><ResourceBar label="" used={info.getValue()} total={info.row.original.cpuCapacityMilli} displayUsed={formatCPU(info.getValue())} displayTotal={formatCPU(info.row.original.cpuCapacityMilli)} /></div> }),
          column.accessor("memoryRequestsBytes", { header: "内存分配", cell: (info) => <div className="w-40"><ResourceBar label="" used={info.getValue()} total={info.row.original.memoryCapacityBytes} displayUsed={formatMemory(info.getValue())} displayTotal={formatMemory(info.row.original.memoryCapacityBytes)} /></div> }),
          column.accessor("podCount", { header: "Pod", cell: (info) => <span className="tabular-nums">{info.getValue()} / {info.row.original.podCapacity}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
          column.display({ id: "actions", header: "操作", size: 56, cell: (info) => <ResourceActionsMenu resource={`节点 ${info.row.original.name}`} actions={actions(info.row.original)} /> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={cursorPaginationProps(pager, query.data?.meta, query.data?.items.length ?? 0)}
        onRowClick={(row) => void navigate({ to: "/nodes/$name", params: { name: row.name } })}
        empty={<div className="flex flex-col items-center gap-2"><Server className="h-6 w-6" /><span>没有匹配的节点</span></div>}
      />
      <YamlDialog open={Boolean(yamlTarget)} onOpenChange={(open) => { if (!open) setYamlTarget(undefined); }} type="node" name={yamlTarget?.name ?? ""} />
    </>
  );
}

