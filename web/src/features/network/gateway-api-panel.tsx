import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Boxes, FileCode2, Network, Route, ShieldCheck } from "lucide-react";
import { api } from "@/api/client";
import type { Condition, GatewayAPIListener, GatewayClassSummary, GatewayRouteBackend, GatewayRouteRule, GatewayRouteSummary, GatewaySummary, ResourceDescriptor } from "@/api/types";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatAge } from "@/lib/utils";

const MESH_GROUPS = new Set([
  "istio.io",
  "networking.istio.io",
  "security.istio.io",
  "telemetry.istio.io",
  "extensions.istio.io",
  "linkerd.io",
  "policy.linkerd.io",
  "gateway.networking.k8s.io",
]);

function conditionsText(conditions?: Condition[]) {
  return (conditions ?? []).map((condition) => `${condition.type}=${condition.status}${condition.reason ? ` (${condition.reason})` : ""}`).join(" · ");
}

function descriptorPath(descriptor: Pick<ResourceDescriptor, "group" | "version" | "resource">, name: string, namespace?: string) {
  const group = descriptor.group || "_core";
  return `/resources/${encodeURIComponent(group)}/${encodeURIComponent(descriptor.version)}/${encodeURIComponent(descriptor.resource)}/${encodeURIComponent(name)}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ""}`;
}

function gatewayPath(group: string, version: string, resource: string, name?: string, namespace?: string) {
  const groupPart = encodeURIComponent(group);
  const base = `/resources/${groupPart}/${encodeURIComponent(version)}/${encodeURIComponent(resource)}`;
  const suffix = name ? `/${encodeURIComponent(name)}` : "";
  return `${base}${suffix}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ""}`;
}

function splitAPIVersion(apiVersion: string) {
  const [group, version] = apiVersion.includes("/") ? apiVersion.split("/", 2) : ["", apiVersion];
  return { group, version };
}

function meshResources(descriptors: ResourceDescriptor[]) {
  return descriptors
    .filter((item) => MESH_GROUPS.has(item.group) && (item.group.includes("istio") || item.group.includes("linkerd")))
    .sort((left, right) => left.group.localeCompare(right.group) || left.resource.localeCompare(right.resource));
}

function MeshNotice({ descriptors }: { descriptors: ResourceDescriptor[] }) {
  const mesh = meshResources(descriptors);
  if (!mesh.length) {
    return (
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base"><ShieldCheck className="h-4 w-4" />Service Mesh 检测</CardTitle>
          <CardDescription>Dosh 不内置服务网格控制面，只提供只读发现和通用资源入口。</CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">当前 API Discovery 未发现 Istio 或 Linkerd CRD。安装网格后，其资源会出现在通用资源中心。</CardContent>
      </Card>
    );
  }
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base"><ShieldCheck className="h-4 w-4" />Service Mesh 检测</CardTitle>
        <CardDescription>已检测到网格 API。Dosh 仅做资源发现与通用 YAML 编辑，不托管网格控制面。</CardDescription>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-wrap gap-2">
        {mesh.slice(0, 12).map((item) => (
          <Link key={`${item.group}/${item.version}/${item.resource}`} to={gatewayPath(item.group, item.version, item.resource, undefined, undefined)} className="inline-flex max-w-full items-center gap-1 rounded-md border bg-muted/30 px-2 py-1 text-xs hover:bg-muted">
            <span className="break-all font-mono">{item.group}/{item.resource}</span><ArrowUpRight className="h-3 w-3 shrink-0" />
          </Link>
        ))}
      </CardContent>
    </Card>
  );
}

function Section({ title, description, count, icon: Icon, children }: { title: string; description: string; count: number; icon: typeof Network; children: React.ReactNode }) {
  return (
    <section className="min-w-0 space-y-3">
      <div className="flex min-w-0 flex-wrap items-end justify-between gap-2">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-sm font-semibold"><Icon className="h-4 w-4" />{title}<Badge variant="secondary">{count}</Badge></h2>
          <p className="mt-1 text-xs text-muted-foreground">{description}</p>
        </div>
      </div>
      {children}
    </section>
  );
}

function ClassCard({ item }: { item: GatewayClassSummary }) {
  const { group, version } = splitAPIVersion(item.apiVersion);
  return (
    <Card className="min-w-0">
      <CardHeader className="pb-3">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><CardTitle className="min-w-0 break-all text-base">{item.name}</CardTitle><StatusBadge status={item.status} /></div>
        <CardDescription className="break-all font-mono text-xs">{item.controller || "未声明 controllerName"}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2 text-xs text-muted-foreground">
        <p className="break-words">{conditionsText(item.conditions) || "暂无 conditions"}</p>
        <div className="flex flex-wrap items-center justify-between gap-2"><span>创建于 {formatAge(item.createdAt)}</span><Link to={descriptorPath({ group, version, resource: item.resource }, item.name)} className="inline-flex items-center gap-1 text-primary hover:underline"><FileCode2 className="h-3.5 w-3.5" />编辑 YAML</Link></div>
      </CardContent>
    </Card>
  );
}

function ListenerRow({ listener }: { listener: GatewayAPIListener }) {
  return (
    <div className="min-w-0 rounded-md border bg-muted/20 p-2.5 text-xs">
      <div className="flex min-w-0 flex-wrap items-center gap-2"><span className="font-medium">{listener.name || "未命名 listener"}</span><Badge variant="outline">{listener.protocol}:{listener.port}</Badge>{listener.tlsMode ? <Badge variant="secondary">TLS {listener.tlsMode}</Badge> : null}</div>
      {listener.hostname ? <p className="mt-1 break-all font-mono text-muted-foreground">hostname: {listener.hostname}</p> : null}
      {listener.allowedRoutes ? <p className="mt-1 text-muted-foreground">允许路由来源: {listener.allowedRoutes}</p> : null}
      {listener.conditions?.length ? <p className="mt-1 break-words text-muted-foreground">{conditionsText(listener.conditions)}</p> : null}
    </div>
  );
}

function GatewayCard({ item }: { item: GatewaySummary }) {
  const { group, version } = splitAPIVersion(item.apiVersion);
  return (
    <Card className="min-w-0">
      <CardHeader className="pb-3">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><CardTitle className="min-w-0 break-all text-base">{item.namespace}/{item.name}</CardTitle><StatusBadge status={item.status} /></div>
        <CardDescription>GatewayClass: <span className="break-all font-mono">{item.className || "—"}</span></CardDescription>
      </CardHeader>
      <CardContent className="min-w-0 space-y-3">
        <div className="flex min-w-0 flex-wrap gap-1.5">{(item.addresses ?? []).length ? item.addresses?.map((address) => <Badge key={`${address.type}-${address.value}`} variant="outline" className="max-w-full break-all font-mono">{address.type}: {address.value}</Badge>) : <span className="text-xs text-muted-foreground">暂无地址</span>}</div>
        <div className="space-y-2">{(item.listeners ?? []).map((listener) => <ListenerRow key={`${listener.name}-${listener.port}`} listener={listener} />)}</div>
        <p className="break-words text-xs text-muted-foreground">{conditionsText(item.conditions) || "暂无 conditions"}</p>
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground"><span>创建于 {formatAge(item.createdAt)}</span><Link to={descriptorPath({ group, version, resource: item.resource }, item.name, item.namespace)} className="inline-flex items-center gap-1 text-primary hover:underline"><FileCode2 className="h-3.5 w-3.5" />编辑 YAML</Link></div>
      </CardContent>
    </Card>
  );
}

function backendText(backend: GatewayRouteBackend) {
  const kind = backend.kind || "Service";
  const target = `${kind} ${backend.namespace ? `${backend.namespace}/` : ""}${backend.name}${backend.port ? `:${backend.port}` : ""}`;
  return backend.weight ? `${target} (weight ${backend.weight})` : target;
}

function ruleText(rule: GatewayRouteRule) {
  const matches = (rule.matches ?? []).map((match) => {
    const path = match.path ? `${match.pathType || "PathPrefix"} ${match.path}` : "任意路径";
    const extras = [match.method, ...(match.headers ?? []), ...(match.query ?? [])].filter(Boolean).join(", ");
    return extras ? `${path} · ${extras}` : path;
  });
  const filters = (rule.filters ?? []).length ? `Filters: ${(rule.filters ?? []).join(", ")}` : "";
  return { matches: matches.length ? matches : ["任意请求"], filters };
}

function RouteCard({ item }: { item: GatewayRouteSummary }) {
  const { group, version } = splitAPIVersion(item.apiVersion);
  return (
    <Card className="min-w-0">
      <CardHeader className="pb-3">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><CardTitle className="min-w-0 break-all text-base">{item.kind} {item.namespace}/{item.name}</CardTitle><StatusBadge status={item.status} /></div>
        <CardDescription className="break-all">Hosts: {(item.hostnames ?? []).join(", ") || "任意 Host"}</CardDescription>
      </CardHeader>
      <CardContent className="min-w-0 space-y-3 text-xs">
        <div className="space-y-1"><p className="font-medium">父级引用</p>{(item.parentRefs ?? []).map((parent, index) => <p key={`${parent.name}-${index}`} className="break-all font-mono text-muted-foreground">{parent.kind || "Gateway"} {parent.namespace ? `${parent.namespace}/` : ""}{parent.name}{parent.sectionName ? `#${parent.sectionName}` : ""}{parent.port ? `:${parent.port}` : ""}</p>)}</div>
        <div className="space-y-2"><p className="font-medium">规则</p>{(item.rules ?? []).map((rule, index) => {
          const text = ruleText(rule);
          return <div key={index} className="min-w-0 rounded-md border bg-muted/20 p-2.5"><p className="break-words font-mono">{text.matches.join(" · ")}</p>{text.filters ? <p className="mt-1 break-words text-muted-foreground">{text.filters}</p> : null}<p className="mt-1 break-all text-muted-foreground">→ {(rule.backends ?? []).map(backendText).join(", ") || "无 backendRefs"}</p></div>;
        })}</div>
        <p className="break-words text-muted-foreground">{conditionsText(item.conditions) || "暂无 conditions"}</p>
        <div className="flex flex-wrap items-center justify-between gap-2 text-muted-foreground"><span>创建于 {formatAge(item.createdAt)}</span><Link to={descriptorPath({ group, version, resource: item.resource }, item.name, item.namespace)} className="inline-flex items-center gap-1 text-primary hover:underline"><FileCode2 className="h-3.5 w-3.5" />编辑 YAML</Link></div>
      </CardContent>
    </Card>
  );
}

export function GatewayAPIPanel({ namespace, q }: { namespace: string; q: string }) {
  const overview = useQuery({ queryKey: ["network-gateway-api", namespace, q], queryFn: () => api.gatewayAPI({ namespace, q }) });
  const discovery = useQuery({ queryKey: ["resource-discovery", "mesh"], queryFn: () => api.resourceDiscovery(), staleTime: 60_000 });
  if (overview.isError) return <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />;
  if (overview.isLoading) return <LoadingState label="正在读取 Gateway API" />;
  const data = overview.data;
  if (!data?.available) {
    return (
      <div className="space-y-4">
        <EmptyState title="未安装 Gateway API CRD" description="Dosh 会动态发现 GatewayClass、Gateway、HTTPRoute 和 GRPCRoute；安装 Gateway API 标准 CRD 或对应实现后刷新即可。"/>
        {data?.warnings?.length ? <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{data.warnings.join("；")}</div> : null}
        {!discovery.isLoading ? <MeshNotice descriptors={discovery.data?.items ?? []} /> : null}
      </div>
    );
  }
  return (
    <div className="min-w-0 space-y-5">
      {data.warnings?.length ? <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{data.warnings.join("；")}</div> : null}
      <div className="grid gap-3 sm:grid-cols-3">
        <Card><CardHeader className="pb-2"><CardDescription>GatewayClass</CardDescription><CardTitle className="text-2xl">{data.classes.length}</CardTitle></CardHeader></Card>
        <Card><CardHeader className="pb-2"><CardDescription>Gateway</CardDescription><CardTitle className="text-2xl">{data.gateways.length}</CardTitle></CardHeader></Card>
        <Card><CardHeader className="pb-2"><CardDescription>Route</CardDescription><CardTitle className="text-2xl">{data.routes.length}</CardTitle></CardHeader></Card>
      </div>
      <Section title="GatewayClass" description="控制器实现与 accepted 状态；集群级资源。" count={data.classes.length} icon={Network}>
        {data.classes.length ? <div className="grid min-w-0 gap-3 xl:grid-cols-2">{data.classes.map((item) => <ClassCard key={item.name} item={item} />)}</div> : <EmptyState title="没有 GatewayClass" description="当前筛选条件下没有 GatewayClass。"/>}
      </Section>
      <Section title="Gateway" description="监听器、地址与编程状态。" count={data.gateways.length} icon={Route}>
        {data.gateways.length ? <div className="grid min-w-0 gap-3 xl:grid-cols-2">{data.gateways.map((item) => <GatewayCard key={`${item.namespace}/${item.name}`} item={item} />)}</div> : <EmptyState title="没有 Gateway" description="当前筛选条件下没有 Gateway。"/>}
      </Section>
      <Section title="HTTPRoute / GRPCRoute" description="Hostname、父级引用、匹配规则和后端权重。" count={data.routes.length} icon={Boxes}>
        {data.routes.length ? <div className="grid min-w-0 gap-3 xl:grid-cols-2">{data.routes.map((item) => <RouteCard key={`${item.kind}/${item.namespace}/${item.name}`} item={item} />)}</div> : <EmptyState title="没有 Gateway Route" description="当前筛选条件下没有 HTTPRoute 或 GRPCRoute。"/>}
      </Section>
      {!discovery.isLoading && !discovery.isError ? <MeshNotice descriptors={discovery.data?.items ?? []} /> : null}
      <p className="text-xs leading-5 text-muted-foreground">Gateway API 通过运行时 API Discovery 动态发现，不在 Dosh 内固定版本。编辑入口复用通用资源 YAML 编辑器，并受当前身份 RBAC 约束。</p>
    </div>
  );
}

export { gatewayPath };
