import { useMemo, useState } from "react";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

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

const directionLabels = {
  upstream: "上游依赖",
  downstream: "下游对象",
  related: "关联对象",
} as const;

const abnormalStatuses = new Set([
  "failed", "error", "crashloopbackoff", "notready", "unavailable", "evicted",
  "imagepullbackoff", "errimagepull", "servicenotfound", "portnotfound",
]);

const allOption = "__all__";

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

function isAbnormal(relation: ResourceRelation): boolean {
  return abnormalStatuses.has((relation.status ?? "").toLowerCase());
}

export function ResourceRelationsPanel({ reference, name, namespace, title = "资源关系" }: {
  reference: ResourceReference;
  name: string;
  namespace?: string;
  title?: string;
}) {
  const navigate = useNavigate();
  const [direction, setDirection] = useState(allOption);
  const [kind, setKind] = useState(allOption);
  const [relationship, setRelationship] = useState(allOption);
  const query = useQuery({
    queryKey: ["resource-relations", reference.group, reference.version, reference.resource, namespace, name],
    queryFn: () => api.resourceRelations(reference, name, namespace),
    enabled: Boolean(name),
    staleTime: 15_000,
  });
  const items = query.data?.items ?? [];
  const stats = useMemo(() => ({
    upstream: items.filter((item) => item.direction === "upstream").length,
    downstream: items.filter((item) => item.direction === "downstream").length,
    related: items.filter((item) => item.direction === "related").length,
    abnormal: items.filter(isAbnormal).length,
  }), [items]);
  const kinds = useMemo(() => [...new Set(items.map((item) => item.kind))].sort(), [items]);
  const relationships = useMemo(() => [...new Set(items.map((item) => item.relationship))].sort((left, right) => (relationshipLabels[left] ?? left).localeCompare(relationshipLabels[right] ?? right)), [items]);
  const grouped = useMemo(() => {
    const filtered = items.filter((item) => (direction === allOption || item.direction === direction)
      && (kind === allOption || item.kind === kind)
      && (relationship === allOption || item.relationship === relationship));
    return (["upstream", "downstream", "related"] as const).map((currentDirection) => {
      const directionItems = filtered.filter((item) => item.direction === currentDirection);
      const byKind = new Map<string, ResourceRelation[]>();
      for (const item of directionItems) byKind.set(item.kind, [...(byKind.get(item.kind) ?? []), item]);
      return {
        direction: currentDirection,
        groups: [...byKind.entries()].sort(([left], [right]) => left.localeCompare(right)),
      };
    }).filter((group) => group.groups.length > 0);
  }, [direction, items, kind, relationship]);
  const filteredCount = grouped.reduce((count, group) => count + group.groups.reduce((total, [, entries]) => total + entries.length, 0), 0);

  return (
    <Card className="min-w-0">
      <CardHeader className="min-w-0">
        <CardTitle className="flex min-w-0 items-center gap-2"><Network className="h-4 w-4 shrink-0 text-muted-foreground" /><span className="min-w-0 break-words">{title}</span></CardTitle>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4">
        {query.isLoading ? <LoadingState label="正在读取直接关系" /> : query.isError ? <p className="text-sm text-muted-foreground">无法读取资源关系。</p> : (
          <>
            {items.length ? (
              <>
                <div className="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-4" aria-label="关系摘要">
                  <RelationStat label="上游依赖" value={stats.upstream} />
                  <RelationStat label="下游对象" value={stats.downstream} />
                  <RelationStat label="关联对象" value={stats.related} />
                  <RelationStat label="异常关联" value={stats.abnormal} alert={stats.abnormal > 0} />
                </div>
                <div className="grid min-w-0 gap-2 sm:grid-cols-3" aria-label="关系筛选">
                  <Select value={direction} onValueChange={setDirection}>
                    <SelectTrigger className="w-full min-w-0" aria-label="关系方向"><SelectValue placeholder="全部方向" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value={allOption}>全部方向</SelectItem>
                      {Object.entries(directionLabels).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  <Select value={kind} onValueChange={setKind}>
                    <SelectTrigger className="w-full min-w-0" aria-label="资源类型"><SelectValue placeholder="全部类型" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value={allOption}>全部类型</SelectItem>
                      {kinds.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  <Select value={relationship} onValueChange={setRelationship}>
                    <SelectTrigger className="w-full min-w-0" aria-label="关系类型"><SelectValue placeholder="全部关系" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value={allOption}>全部关系</SelectItem>
                      {relationships.map((value) => <SelectItem key={value} value={value}>{relationshipLabels[value] ?? value}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <p className="text-xs text-muted-foreground">显示 {filteredCount} / {items.length} 条直接关系</p>
              </>
            ) : null}

            {!items.length ? <EmptyState title="没有直接关系" description="当前对象没有直接的所有者、Pod、Service、Ingress、节点或存储关联。" /> : grouped.length ? (
              <div className="min-w-0 space-y-5">
                {grouped.map(({ direction: currentDirection, groups }) => (
                  <section key={currentDirection} className="min-w-0" aria-labelledby={`relation-direction-${currentDirection}`}>
                    <h3 id={`relation-direction-${currentDirection}`} className="mb-2 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                      {currentDirection === "upstream" ? <ArrowUpRight className="h-3.5 w-3.5 shrink-0" /> : currentDirection === "downstream" ? <ArrowDownLeft className="h-3.5 w-3.5 shrink-0" /> : <Network className="h-3.5 w-3.5 shrink-0" />}
                      {directionLabels[currentDirection]}
                    </h3>
                    <div className="min-w-0 space-y-3">
                      {groups.map(([currentKind, entries]) => (
                        <div key={currentKind} className="min-w-0">
                          <div className="mb-1.5 flex min-w-0 flex-wrap items-center gap-2"><Badge variant="secondary">{currentKind}</Badge><span className="text-xs text-muted-foreground">{entries.length} 个</span></div>
                          <div className="grid min-w-0 gap-2 sm:grid-cols-2 xl:grid-cols-3">
                            {entries.map((item) => <RelationItem key={`${item.apiVersion}/${item.kind}/${item.namespace}/${item.name}/${item.relationship}`} item={item} onNavigate={(target) => void navigate({ to: target.to, search: target.search })} />)}
                          </div>
                        </div>
                      ))}
                    </div>
                  </section>
                ))}
              </div>
            ) : <p className="rounded-md border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">没有符合当前筛选条件的关系。</p>}

            {query.data?.warnings?.length ? <div role="status" className="flex min-w-0 items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{query.data.warnings.join("；")}</span></div> : null}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function RelationStat({ label, value, alert = false }: { label: string; value: number; alert?: boolean }) {
  return (
    <div className={cn("min-w-0 rounded-md border bg-muted/20 px-3 py-2", alert && "border-destructive/35 bg-destructive/10")}>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={cn("text-lg font-semibold tabular-nums", alert && "text-destructive-strong")}>{value}</p>
    </div>
  );
}

function RelationItem({ item, onNavigate }: { item: ResourceRelation; onNavigate: (target: RelationPath) => void }) {
  const target = relationPath(item);
  return (
    <div className="flex min-w-0 max-w-full flex-wrap items-center gap-1.5 rounded-md border bg-muted/20 px-2.5 py-1.5 text-xs">
      <Badge variant="outline" className="shrink-0">{item.kind}</Badge>
      {target ? <Button variant="link" size="sm" className="h-auto min-w-0 max-w-full justify-start p-0 text-left text-xs" onClick={() => onNavigate(target)}><span className="break-all">{item.name}</span></Button> : <span className="min-w-0 break-all font-medium">{item.name}</span>}
      <span className="min-w-0 text-muted-foreground">{relationshipLabels[item.relationship] ?? item.relationship}</span>
      {item.status ? <StatusBadge status={item.status} /> : null}
    </div>
  );
}
