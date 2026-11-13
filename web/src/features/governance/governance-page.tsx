import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowUpRight, LockKeyhole, Scale, ShieldCheck, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import { ErrorState } from "@/components/feedback/states";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { governanceResources, type GovernanceResource } from "@/features/resources/governance-resources";

const categoryMeta = {
  RBAC: { icon: LockKeyhole, description: "身份、角色与授权绑定" },
  "隔离与可用性": { icon: ShieldCheck, description: "网络边界、自动扩缩与中断预算" },
  "配额与默认值": { icon: Scale, description: "命名空间配额、默认资源边界与调度优先级" },
} as const;

export function GovernancePage() {
  const search = useSearch({ from: "/app/governance" });
  const navigate = useNavigate();
  const namespace = search.namespace ?? "";
  const discovery = useQuery({
    queryKey: ["resource-discovery", namespace, "governance"],
    queryFn: () => api.resourceDiscovery(namespace || undefined),
    staleTime: 60_000,
  });
  const descriptors = useMemo(() => new Map((discovery.data?.items ?? []).map((item) => [`${item.group}/${item.resource}`, item])), [discovery.data?.items]);
  const groups = useMemo(() => Array.from(new Set(governanceResources.map((item) => item.category))), []);

  if (discovery.isError) return <ErrorState error={discovery.error} onRetry={() => void discovery.refetch()} />;
  return (
    <>
      <PageHeader
        title="治理与策略"
        breadcrumb="集群 / 治理与策略"
        description="集中查看 RBAC、网络隔离、可用性和资源配额对象。所有列表与详情均复用通用 Kubernetes 资源读取路径，并受当前身份 RBAC 约束。"
      />
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <NamespaceSelect value={namespace} onChange={(value) => void navigate({ to: "/governance", search: { ...search, namespace: value || undefined }, replace: true })} />
        <span className="text-xs text-muted-foreground">选择命名空间后，目录会按当前身份的 list 权限过滤。</span>
      </div>
      {discovery.data?.warnings?.length ? (
        <div className="mb-4 flex items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{discovery.data.warnings.join("；")}</span>
        </div>
      ) : null}
      <div className="space-y-6">
        {groups.map((group) => {
          const meta = categoryMeta[group];
          const Icon = meta.icon;
          return (
            <section key={group} aria-labelledby={`governance-${group}`}>
              <div className="mb-3 flex items-center gap-2">
                <Icon className="h-4 w-4 text-primary" />
                <h2 id={`governance-${group}`} className="text-sm font-semibold">{group}</h2>
                <span className="text-xs text-muted-foreground">{meta.description}</span>
              </div>
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                {governanceResources.filter((item) => item.category === group).map((item) => (
                  <GovernanceCard key={`${item.group}/${item.resource}`} item={item} descriptor={descriptors.get(`${item.group}/${item.resource}`)} namespace={namespace} />
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </>
  );
}

function GovernanceCard({ item, descriptor, namespace }: {
  item: GovernanceResource;
  descriptor?: import("@/api/types").ResourceDescriptor;
  namespace: string;
}) {
  const available = Boolean(descriptor);
  const body = (
    <Card className={available ? "h-full transition-colors hover:border-primary/50" : "h-full opacity-65"}>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle className="text-sm">{item.label}</CardTitle>
            <CardDescription className="mt-1 font-mono text-[11px]">{item.group || "core"}/{item.resource}</CardDescription>
          </div>
          <Badge variant={available ? "secondary" : "outline"}>{available ? descriptor?.version : "未发现"}</Badge>
        </div>
      </CardHeader>
      <CardContent>
        <p className="text-xs leading-5 text-muted-foreground">{item.description}</p>
      </CardContent>
    </Card>
  );
  if (!descriptor) return body;
  return (
    <Link
      to="/resources/$group/$version/$resource"
      params={{ group: descriptor.group || "_core", version: descriptor.version, resource: descriptor.resource }}
      search={{ namespace: descriptor.namespaced ? namespace || undefined : undefined }}
      className="group relative block rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {body}
      <ArrowUpRight className="absolute right-3 top-3 h-3.5 w-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
    </Link>
  );
}
