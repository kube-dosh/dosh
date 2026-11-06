import { useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { Activity } from "lucide-react";
import { api } from "@/api/client";
import type { EventItem } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { EventMessage } from "@/features/events/event-message";
import { formatDate } from "@/lib/utils";

const column = createColumnHelper<EventItem>();

export function ResourceEvents({ namespace, objectKind, objectName }: {
  namespace: string;
  objectKind: string;
  objectName: string;
}) {
  const query = useQuery({
    queryKey: ["events", "object", namespace, objectKind, objectName],
    queryFn: () => api.events({ namespace, objectKind, objectName, pageSize: 100, sort: "lastSeen", order: "desc" }),
    refetchInterval: 20_000,
  });
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  return (
    <DataTable
      columns={[
        column.accessor("type", { header: "类型", cell: (info) => <StatusBadge status={info.getValue()} /> }),
        column.accessor("reason", { header: "原因", cell: (info) => <span className="font-medium">{info.getValue()}</span> }),
        column.accessor("message", { header: "消息", size: 420, cell: (info) => <EventMessage message={info.getValue()} /> }),
        column.accessor("count", { header: "次数", cell: (info) => <span className="tabular-nums">{info.getValue()}</span> }),
        column.accessor("firstSeen", { header: "首次发生", cell: (info) => <span className="whitespace-nowrap text-muted-foreground">{formatDate(info.getValue())}</span> }),
        column.accessor("lastSeen", { header: "最近发生", cell: (info) => <span className="whitespace-nowrap text-muted-foreground">{formatDate(info.getValue())}</span> }),
      ]}
      data={query.data?.items ?? []}
      loading={query.isLoading}
      stickyFirstColumn={false}
      empty={<div className="flex flex-col items-center gap-2"><Activity className="h-6 w-6" /><span>该资源暂无事件</span></div>}
      pageSize={20}
    />
  );
}
