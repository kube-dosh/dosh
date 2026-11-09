import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { ScrollText, ShieldCheck } from "lucide-react";
import { api } from "@/api/client";
import type { AuditRecord } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { formatDate } from "@/lib/utils";

const column = createColumnHelper<AuditRecord>();
const actions = [
  "apply", "cordon", "create-configmap", "create-deployment", "create-ingress", "create-namespace",
  "create-persistentvolumeclaim", "create-secret", "create-service", "delete", "delete-configmap",
  "delete-ingress", "delete-namespace", "delete-persistentvolumeclaim", "delete-secret",
  "delete-service", "dry-run", "login", "login_started", "logout", "reload", "restart", "resume",
  "reveal", "scale", "suspend", "uncordon", "update-configmap", "update-container-config",
  "update-image", "update-ingress", "update-labels", "update-persistentvolumeclaim", "update-secret",
  "update-service", "update-taints",
];
const results = ["success", "failure", "denied"] as const;

export function AuditPage() {
  const search = useSearch({ from: "/app/audit" });
  const navigate = useNavigate();
  const q = search.q ?? "";
  const namespace = search.namespace ?? "";
  const action = search.action ?? "";
  const result = search.result ?? "";
  const [selected, setSelected] = useState<AuditRecord | null>(null);
  const updateSearch = (patch: { q?: string; namespace?: string; action?: string; result?: string }) => {
    void navigate({ to: "/audit", search: { ...search, ...patch }, replace: true });
  };
  const query = useQuery({
    queryKey: ["audit-events", namespace, q, action, result],
    queryFn: () => api.auditEvents({ namespace, q, action, result }),
    refetchInterval: 30_000,
  });

  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;

  return (
    <>
      <PageHeader
        title="审计日志"
        breadcrumb="可观测性 / 审计日志"
        description="查看当前实例记录的登录、Secret reveal、变更、拒绝和失败操作。"
      />
      <div role="note" className="mb-3 flex items-start gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-muted-foreground">
        <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
        <span>仅展示当前实例最近 1000 条记录，Pod 重启或副本切换后可能不同。长期保留、告警和不可变存储仍由 stdout 采集或企业 SIEM 负责。</span>
      </div>
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索用户、动作、资源或请求 ID" />
        <NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} />
        <Select value={action || "__all__"} onValueChange={(value) => updateSearch({ action: value === "__all__" ? undefined : value })}>
          <SelectTrigger className="w-40" aria-label="审计动作筛选"><SelectValue placeholder="全部动作" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部动作</SelectItem>
            {actions.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={result || "__all__"} onValueChange={(value) => updateSearch({ result: value === "__all__" ? undefined : value })}>
          <SelectTrigger className="w-36" aria-label="审计结果筛选"><SelectValue placeholder="全部结果" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部结果</SelectItem>
            {results.map((item) => <SelectItem key={item} value={item}>{item}</SelectItem>)}
          </SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground sm:ml-auto">{query.data?.meta.total ?? 0} 条记录</span>
      </div>
      <DataTable
        columns={[
          column.accessor("timestamp", { header: "时间", cell: (info) => <span className="whitespace-nowrap tabular-nums text-muted-foreground">{formatDate(info.getValue())}</span> }),
          column.accessor("user", { header: "用户", cell: (info) => <span className="font-medium">{info.getValue() || "anonymous"}</span> }),
          column.accessor("action", { header: "动作", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("resource", { header: "资源", cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span> }),
          column.accessor("namespace", { header: "命名空间 / 名称", cell: (info) => <span className="font-mono text-xs text-muted-foreground">{info.getValue() || "—"}{info.row.original.name ? ` / ${info.row.original.name}` : ""}</span> }),
          column.accessor("result", { header: "结果", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("requestId", { header: "请求 ID", cell: (info) => <span className="block max-w-40 truncate font-mono text-xs text-muted-foreground" title={info.getValue()}>{info.getValue() || "—"}</span> }),
        ]}
        data={query.data?.items ?? []}
        loading={query.isLoading}
        onRowClick={setSelected}
        stickyFirstColumn={false}
        empty={<div className="flex flex-col items-center gap-2"><ScrollText className="h-6 w-6" /><span>没有匹配的审计记录</span></div>}
      />
      <Dialog open={selected !== null} onOpenChange={(open) => { if (!open) setSelected(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>审计记录 #{selected?.id}</DialogTitle>
            <DialogDescription>{selected ? `${formatDate(selected.timestamp)} · ${selected.user || "anonymous"}` : ""}</DialogDescription>
          </DialogHeader>
          {selected ? (
            <div className="space-y-4 text-sm">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Detail label="动作" value={selected.action} mono />
                <Detail label="资源" value={selected.resource} mono />
                <Detail label="命名空间" value={selected.namespace || "—"} mono />
                <Detail label="名称" value={selected.name || "—"} mono />
                <Detail label="结果" value={selected.result} />
                <Detail label="请求 ID" value={selected.requestId || "—"} mono />
                <Detail label="用户" value={selected.user || "anonymous"} />
                <Detail label="用户组" value={selected.groups?.join(", ") || "—"} />
              </div>
              {selected.error ? (
                <div>
                  <p className="mb-1 font-medium">错误信息</p>
                  <pre className="soft-wrap max-h-56 max-w-full overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-3 font-mono text-xs text-destructive-strong">{selected.error}</pre>
                </div>
              ) : null}
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}

function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={mono ? "mt-1 break-all font-mono text-xs" : "mt-1"}>{value}</p>
    </div>
  );
}
