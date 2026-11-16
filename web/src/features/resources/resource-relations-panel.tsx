import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowDownLeft, ArrowUpRight, Network, ShieldAlert } from "lucide-react";
import { api } from "@/api/client";
import type { ResourceReference, ResourceRelation } from "@/api/types";
import { EmptyState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

const workloadKinds: Record<string, string> = {
  Deployment: "deployment", StatefulSet: "statefulset", DaemonSet: "daemonset", Job: "job", CronJob: "cronjob",
};

const relationshipLabels: Record<string, string> = {
  owner: "所有者",
  "managed by": "由…管理",
  "scheduled on": "调度节点",
  manages: "管理工作负载",
  selects: "选择 Pod",
  exposes: "对外暴露",
  "routes traffic": "流量入口",
  "routes to": "后端服务",
  mounts: "挂载",
  "bound to": "绑定关系",
  creates: "创建",
};

interface RelationPath {
  to: string;
  search?: Record<string, string>;
}

function relationPath(relation: ResourceRelation): RelationPath | null {
  const namespace = relation.namespace || "";
  if (relation.kind === "Pod" && namespace) return { to: `/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(relation.name)}` };
  if (relation.kind === "Node") return { to: `/nodes/${encodeURIComponent(relation.name)}` };
  if (relation.kind === "Service" && namespace) return { to: `/network/services/${encodeURIComponent(namespace)}/${encodeURIComponent(relation.name)}` };
  if (relation.kind === "Ingress" && namespace) return { to: `/network/ingresses/${encodeURIComponent(namespace)}/${encodeURIComponent(relation.name)}` };
  if (relation.kind === "PersistentVolumeClaim" && namespace) return { to: `/storage/claims/${encodeURIComponent(namespace)}/${encodeURIComponent(relation.name)}` };
  if (relation.kind === "PersistentVolume") return { to: `/storage/volumes/${encodeURIComponent(relation.name)}` };
  const workload = workloadKinds[relation.kind];
  if (workload && namespace) return { to: `/workloads/${workload}/${encodeURIComponent(namespace)}/${encodeURIComponent(relation.name)}` };
  if (!relation.resource) return null;
  const parts = relation.apiVersion.split("/");
  const group = parts.length > 1 ? parts[0] : "_core";
  const version = parts.length > 1 ? parts[1] : parts[0];
  if (!version) return null;
  return {
    to: `/resources/${encodeURIComponent(group)}/${encodeURIComponent(version)}/${encodeURIComponent(relation.resource)}/${encodeURIComponent(relation.name)}`,
    search: namespace ? { namespace } : undefined,
  };
}

export function ResourceRelationsPanel({ reference, name, namespace, title = "资源关系" }: {
  reference: ResourceReference;
  name: string;
  namespace?: string;
  title?: string;
}) {
  const navigate = useNavigate();
  const query = useQuery({
    queryKey: ["resource-relations", reference.group, reference.version, reference.resource, namespace, name],
    queryFn: () => api.resourceRelations(reference, name, namespace),
    enabled: Boolean(name),
    staleTime: 15_000,
  });
  const grouped = useMemo(() => {
    const result = { upstream: [] as ResourceRelation[], downstream: [] as ResourceRelation[], related: [] as ResourceRelation[] };
    for (const item of query.data?.items ?? []) result[item.direction]?.push(item);
    return result;
  }, [query.data?.items]);
  return (
    <Card className="min-w-0">
      <CardHeader><CardTitle className="flex items-center gap-2"><Network className="h-4 w-4 text-muted-foreground" />{title}</CardTitle></CardHeader>
      <CardContent className="min-w-0 space-y-4">
        {query.isLoading ? <LoadingState label="正在读取直接关系" /> : query.isError ? <p className="text-sm text-muted-foreground">无法读取资源关系。</p> : !query.data?.items.length ? <EmptyState title="没有直接关系" description="当前对象没有直接的所有者、Pod、Service、Ingress、节点或存储关联。" /> : (
          <>
            {(["upstream", "downstream", "related"] as const).map((direction) => grouped[direction].length ? (
              <div key={direction} className="min-w-0">
                <p className="mb-2 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {direction === "upstream" ? <ArrowUpRight className="h-3.5 w-3.5" /> : direction === "downstream" ? <ArrowDownLeft className="h-3.5 w-3.5" /> : <Network className="h-3.5 w-3.5" />}
                  {direction === "upstream" ? "上游依赖" : direction === "downstream" ? "下游对象" : "关联对象"}
                </p>
                <div className="flex min-w-0 flex-wrap gap-2">
                  {grouped[direction].map((item) => {
                    const target = relationPath(item);
                    return (
                      <div key={`${item.apiVersion}/${item.kind}/${item.namespace}/${item.name}/${item.relationship}`} className="flex min-w-0 max-w-full flex-wrap items-center gap-1.5 rounded-md border bg-muted/20 px-2.5 py-1.5 text-xs sm:max-w-72">
                        <Badge variant="outline" className="shrink-0">{item.kind}</Badge>
                        {target ? <Button variant="link" size="sm" className="h-auto min-w-0 max-w-full justify-start p-0 text-left text-xs" onClick={() => void navigate({ to: target.to, search: target.search })}><span className="break-all">{item.name}</span></Button> : <span className="break-all font-medium">{item.name}</span>}
                        <span className="text-muted-foreground">{relationshipLabels[item.relationship] ?? item.relationship}</span>
                        {item.status ? <StatusBadge status={item.status} /> : null}
                      </div>
                    );
                  })}
                </div>
              </div>
            ) : null)}
            {query.data.warnings?.length ? <div role="status" className="flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{query.data.warnings.join("；")}</span></div> : null}
          </>
        )}
      </CardContent>
    </Card>
  );
}
