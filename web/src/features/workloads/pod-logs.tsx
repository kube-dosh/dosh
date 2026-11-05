import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { PodSummary } from "@/api/types";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { LogViewer } from "@/features/workloads/log-viewer";

export function PodLogs({ pods, emptyMessage }: {
  pods: PodSummary[];
  emptyMessage: string;
}) {
  const [selectedKey, setSelectedKey] = useState("");
  const pod = pods.find((item) => `${item.namespace}/${item.name}` === selectedKey) ?? pods[0];
  const detail = useQuery({
    queryKey: ["pod", pod?.namespace, pod?.name],
    queryFn: () => api.pod(pod!.namespace, pod!.name),
    enabled: Boolean(pod),
  });
  if (!pod) return <p className="rounded-lg border border-dashed px-5 py-10 text-center text-sm text-muted-foreground">{emptyMessage}</p>;
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <Select value={`${pod.namespace}/${pod.name}`} onValueChange={setSelectedKey}>
          <SelectTrigger className="w-96 max-w-full" aria-label="选择实例查看日志"><SelectValue /></SelectTrigger>
          <SelectContent>{pods.map((item) => <SelectItem key={`${item.namespace}/${item.name}`} value={`${item.namespace}/${item.name}`}>{item.namespace}/{item.name} · {item.status}</SelectItem>)}</SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground">日志按实例查看，避免混合多个容器的输出。</span>
      </div>
      {detail.isLoading ? <LoadingState label="正在加载实例容器" /> : detail.isError ? <ErrorState error={detail.error} onRetry={() => void detail.refetch()} /> : detail.data ? <LogViewer namespace={pod.namespace} name={pod.name} containers={detail.data.containers} /> : null}
    </div>
  );
}
