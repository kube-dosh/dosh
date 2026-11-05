import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { Activity, Radio, RotateCw, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import type { EventItem } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { SearchInput } from "@/components/layout/search-input";
import { Button } from "@/components/ui/button";
import { mergeEventRows, useEventStream } from "@/features/events/use-event-stream";
import { formatDate } from "@/lib/utils";

const column = createColumnHelper<EventItem>();

export function EventsPage() {
  const search = useSearch({ from: "/app/events" });
  const navigate = useNavigate();
  const namespace = search.namespace ?? "";
  const q = search.q ?? "";
  const updateSearch = (patch: { q?: string; namespace?: string }) => {
    void navigate({ to: "/events", search: { ...search, ...patch }, replace: true });
  };
  const [live, setLive] = useState(false);
  const stream = useEventStream(namespace, live);
  const query = useQuery({ queryKey: ["events", namespace, q], queryFn: () => api.events({ namespace, q }), refetchInterval: live ? false : 20_000 });

  const items = useMemo(() => {
    return mergeEventRows(query.data?.items ?? [], stream.events, 500);
  }, [query.data?.items, stream.events]);
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  return (
    <>
      <PageHeader
        title="集群事件"
        breadcrumb="可观测性 / 事件"
        description="查看 Kubernetes Events，识别调度失败、镜像拉取、探针和资源异常。"
        actions={<Button variant={live ? "default" : "outline"} onClick={() => setLive((value) => !value)}><Radio className={live ? "h-4 w-4 animate-pulse" : "h-4 w-4"} />{live ? "停止实时流" : "开启实时流"}</Button>}
      />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center"><SearchInput value={q} onChange={(value) => updateSearch({ q: value || undefined })} placeholder="搜索原因、对象或消息" /><NamespaceSelect value={namespace} onChange={(value) => updateSearch({ namespace: value || undefined })} /><span className="text-xs text-muted-foreground sm:ml-auto">{items.length} 条事件</span></div>
      {stream.error ? <div className="mb-3 flex items-center gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><TriangleAlert className="h-4 w-4" />{stream.error}<Button variant="outline" size="sm" className="ml-auto h-7" onClick={stream.reconnect}><RotateCw className="h-3.5 w-3.5" />立即重连</Button></div> : null}
      {live && stream.status === "connected" ? <div className="mb-3 flex items-center gap-2 rounded-md border border-success/30 bg-success/10 px-3 py-2 text-xs text-success"><span className="h-1.5 w-1.5 animate-pulse rounded-full bg-success" />实时事件流已连接，新事件按 UID 去重后插入列表顶部。</div> : null}
      <DataTable
        columns={[
          column.accessor("type", { header: "类型", cell: (info) => <StatusBadge status={info.getValue()} /> }),
          column.accessor("namespace", { header: "命名空间" }),
          column.accessor("objectKind", { header: "对象", cell: (info) => <span className="font-mono text-xs">{info.getValue()}/{info.row.original.objectName}</span> }),
          column.accessor("reason", { header: "原因", cell: (info) => <span className="font-medium">{info.getValue()}</span> }),
          column.accessor("message", { header: "消息", cell: (info) => <span className="block max-w-xl whitespace-normal text-muted-foreground">{info.getValue()}</span> }),
          column.accessor("count", { header: "次数", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
          column.accessor("lastSeen", { header: "最近发生", cell: (info) => <span className="whitespace-nowrap text-muted-foreground">{formatDate(info.getValue())}</span> }),
        ]}
        data={items}
        loading={query.isLoading}
        empty={<div className="flex flex-col items-center gap-2"><Activity className="h-6 w-6" /><span>没有匹配的事件</span></div>}
        pageSize={30}
      />
    </>
  );
}
