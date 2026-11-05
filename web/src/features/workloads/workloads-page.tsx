import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Boxes, CircleDot, Plus } from "lucide-react";
import { api } from "@/api/client";
import type { WorkloadSummary } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useCapabilities } from "@/features/auth/use-session";
import { DeploymentCreateDialog } from "@/features/workloads/deployment-create-dialog";
import { formatAge } from "@/lib/utils";
import { useState } from "react";

const column = createColumnHelper<WorkloadSummary>();
const kinds = ["deployment", "statefulset", "daemonset", "job", "cronjob"] as const;
const statuses = ["Running", "Updating", "Pending", "Failed", "Stopped", "Complete", "Suspended", "Idle"] as const;

export function WorkloadsPage() {
  const search = useSearch({ from: "/app/workloads" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const kind = search.kind ?? "";
  const status = search.status ?? "";
  const [createOpen, setCreateOpen] = useState(false);
  const updateSearch = (patch: { q?: string; namespace?: string; kind?: string; status?: string }) => {
    void navigate({ to: "/workloads", search: { ...search, ...patch }, replace: true });
  };
  const query = useQuery({
    queryKey: ["workloads", namespace, kind, status, q],
    queryFn: () => api.workloads({ namespace, kind, status, q }),
    refetchInterval: 20_000,
  });
  const capabilities = useCapabilities(namespace || undefined).data;

  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;

  return (
    <>
      <PageHeader
        title="工作负载"
        breadcrumb="工作负载 / 全部工作负载"
        description="统一查看 Deployment、StatefulSet、DaemonSet、Job 和 CronJob。"
        actions={<>
          {capabilities?.create ? <Button onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" />创建 Deployment</Button> : null}
          <Button variant="outline" asChild><Link to="/pods"><CircleDot className="h-4 w-4" />Pod 实例</Link></Button>
        </>}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索工作负载名称" />
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} />
        <Select value={kind || "__all__"} onValueChange={(value) => updateSearch({ kind: value === "__all__" ? undefined : value })}>
          <SelectTrigger className="w-44" aria-label="工作负载类型筛选"><SelectValue placeholder="全部类型" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部类型</SelectItem>
            {kinds.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={status || "__all__"} onValueChange={(value) => updateSearch({ status: value === "__all__" ? undefined : value })}>
          <SelectTrigger className="w-40" aria-label="工作负载状态筛选"><SelectValue placeholder="全部状态" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部状态</SelectItem>
            {statuses.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
          </SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground sm:ml-auto">{query.data?.meta.total ?? 0} 个工作负载</span>
      </div>
      <DataTable
        columns={[
          column.accessor("name", { header: "名称", cell: (info) => <Link to="/workloads/$kind/$namespace/$name" params={{ kind: info.row.original.kind.toLowerCase(), namespace: info.row.original.namespace, name: info.getValue() }} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
          column.accessor("kind", { header: "类型", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("namespace", { header: "命名空间" }),
          column.accessor("status", { header: "状态", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("ready", { header: "就绪", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("restarts", { header: "重启", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
          column.accessor("images", { header: "镜像", enableSorting: false, cell: (info) => <span className="block max-w-64 truncate font-mono text-xs text-muted-foreground" title={info.getValue().join("\n")}>{info.getValue().join(", ") || "—"}</span> }),
          column.accessor("createdAt", { header: "年龄", cell: (info) => <span className="text-muted-foreground">{formatAge(info.getValue())}</span> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        onRowClick={(row) => void navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind: row.kind.toLowerCase(), namespace: row.namespace, name: row.name } })}
        empty={<div className="flex flex-col items-center gap-2"><Boxes className="h-6 w-6" /><span>没有匹配的工作负载</span></div>}
      />
      <DeploymentCreateDialog open={createOpen} onOpenChange={setCreateOpen} initialNamespace={namespace} />
    </>
  );
}
