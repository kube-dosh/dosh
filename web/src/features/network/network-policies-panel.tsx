import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRightLeft, FileCode2, Plus, ShieldCheck, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import type { LabelSelectorRequirement, NetworkPolicyPeer, NetworkPolicyPort, NetworkPolicyRule, NetworkPolicySummary } from "@/api/types";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { ActionTooltip } from "@/components/feedback/action-tooltip";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatAge } from "@/lib/utils";

function selectorText(selector?: Record<string, string>, expressions?: LabelSelectorRequirement[]) {
  const labels = Object.entries(selector ?? {}).map(([key, value]) => `${key}=${value}`);
  const matches = (expressions ?? []).map((expression) => `${expression.key} ${expression.operator} ${(expression.values ?? []).join("|")}`.trim());
  return [...labels, ...matches].join(", ") || "全部 Pod";
}

function peerText(peer: NetworkPolicyPeer) {
  if (peer.ipBlock) return `IPBlock ${peer.ipBlock.cidr}${peer.ipBlock.except?.length ? ` (except ${peer.ipBlock.except.join(", ")})` : ""}`;
  const namespace = peer.namespaceSelector !== undefined ? `ns[${selectorText(peer.namespaceSelector, peer.namespaceSelectorExpressions)}]` : "";
  const pod = peer.podSelector !== undefined ? `pod[${selectorText(peer.podSelector, peer.podSelectorExpressions)}]` : "";
  return [namespace, pod].filter(Boolean).join(" ") || "所有来源/目标";
}

function portText(port: NetworkPolicyPort) {
  const end = port.endPort ? `-${port.endPort}` : "";
  return `${port.protocol || "TCP"}/${port.port || "全部端口"}${end}`;
}

function RuleCard({ rule, direction, index }: { rule: NetworkPolicyRule; direction: "ingress" | "egress"; index: number }) {
  const peers = rule.peers ?? [];
  const ports = rule.ports ?? [];
  return (
    <div className="min-w-0 rounded-md border bg-muted/20 p-3 text-xs">
      <p className="font-medium">{direction === "ingress" ? "允许来源" : "允许目标"} #{index + 1}</p>
      <div className="mt-2 grid min-w-0 gap-2 lg:grid-cols-2">
        <div className="min-w-0"><p className="text-muted-foreground">Peers</p><div className="mt-1 flex flex-wrap gap-1">{peers.length ? peers.map((peer, peerIndex) => <Badge key={peerIndex} variant="outline" className="max-w-full whitespace-normal break-all text-left">{peerText(peer)}</Badge>) : <Badge variant="secondary">所有来源/目标</Badge>}</div></div>
        <div className="min-w-0"><p className="text-muted-foreground">Ports</p><div className="mt-1 flex flex-wrap gap-1">{ports.length ? ports.map((port, portIndex) => <Badge key={portIndex} variant="outline" className="font-mono">{portText(port)}</Badge>) : <Badge variant="secondary">所有端口</Badge>}</div></div>
      </div>
    </div>
  );
}

function DirectionRules({ title, rules, direction }: { title: string; rules: NetworkPolicyRule[]; direction: "ingress" | "egress" }) {
  return (
    <div className="min-w-0 space-y-2">
      <div className="flex flex-wrap items-center gap-2"><p className="text-sm font-medium">{title}</p><Badge variant={rules.length ? "secondary" : "destructive"}>{rules.length} 条</Badge>{!rules.length ? <span className="text-xs text-muted-foreground">已隔离，默认拒绝</span> : null}</div>
      <div className="space-y-2">{rules.map((rule, index) => <RuleCard key={index} rule={rule} direction={direction} index={index} />)}</div>
    </div>
  );
}

function PolicyCard({ policy, namespace }: { policy: NetworkPolicySummary; namespace: string }) {
  const group = policy.apiVersion.includes("/") ? policy.apiVersion.split("/")[0] : "";
  const version = policy.apiVersion.includes("/") ? policy.apiVersion.split("/")[1] : policy.apiVersion;
  return (
    <Card className="min-w-0">
      <CardHeader className="pb-3">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
          <div className="min-w-0"><CardTitle className="break-all text-base">{policy.namespace}/{policy.name}</CardTitle><CardDescription className="mt-1 break-all font-mono">{selectorText(policy.podSelector, policy.podSelectorExpressions)} · 匹配 {policy.matchedPods} 个 Pod</CardDescription></div>
          <div className="flex flex-wrap gap-1">{policy.policyTypes.map((type) => <Badge key={type} variant="outline">{type}</Badge>)}</div>
        </div>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4">
        {policy.warnings?.length ? <div className="space-y-1 rounded-md border border-warning/35 bg-warning/10 p-2.5 text-xs text-warning">{policy.warnings.map((warning) => <p key={warning} className="flex gap-1.5 break-words"><TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />{warning}</p>)}</div> : null}
        {policy.policyTypes.includes("Ingress") ? <DirectionRules title="入站规则" rules={policy.ingress} direction="ingress" /> : null}
        {policy.policyTypes.includes("Egress") ? <DirectionRules title="出站规则" rules={policy.egress} direction="egress" /> : null}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3 text-xs text-muted-foreground">
          <span>创建于 {formatAge(policy.createdAt)}</span>
          <Link to="/resources/$group/$version/$resource/$name" params={{ group: group || "_core", version, resource: policy.resource, name: policy.name }} search={{ namespace: policy.namespace || namespace }} className="inline-flex items-center gap-1 text-primary hover:underline"><FileCode2 className="h-3.5 w-3.5" />编辑 YAML</Link>
        </div>
      </CardContent>
    </Card>
  );
}

export function NetworkPoliciesPanel({ namespace, q }: { namespace: string; q: string }) {
  const overview = useQuery({ queryKey: ["network-policies", namespace, q], queryFn: () => api.networkPolicies({ namespace, q }) });
  if (overview.isError) return <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />;
  if (overview.isLoading) return <LoadingState label="正在解析 NetworkPolicy" />;
  const data = overview.data;
  return (
    <div className="min-w-0 space-y-4">
      <div className="flex min-w-0 flex-col gap-2 rounded-lg border bg-muted/20 p-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 gap-2 text-xs leading-5 text-muted-foreground"><ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" /><p className="break-words">只要 Pod 被某条策略选中，该方向的流量就会按策略规则执行；存在隔离但无允许规则时默认拒绝。跨命名空间 Pod 访问需同时匹配 namespaceSelector 与 podSelector。</p></div>
        {!namespace ? (
          <ActionTooltip content="请先选择命名空间，再创建 NetworkPolicy"><Button variant="outline" size="sm" disabled className="shrink-0"><Plus className="h-4 w-4" />新建策略</Button></ActionTooltip>
        ) : (
          <Button asChild variant="outline" size="sm" className="shrink-0"><Link to="/resources/$group/$version/$resource" params={{ group: "networking.k8s.io", version: "v1", resource: "networkpolicies" }} search={{ namespace }}><Plus className="h-4 w-4" />新建策略</Link></Button>
        )}
      </div>
      {data?.warnings?.length ? <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{data.warnings.join("；")}</div> : null}
      {!data?.items.length ? <EmptyState title={q ? "没有匹配的 NetworkPolicy" : "没有 NetworkPolicy"} description={namespace ? "当前命名空间没有策略，Pod 间流量默认不受 NetworkPolicy 限制。创建后可在此解释入站、出站、默认拒绝与 DNS 风险。" : "选择命名空间后可计算策略实际匹配的 Pod 数量，并查看逐条规则解释。"} /> : (
        <>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><ArrowRightLeft className="h-4 w-4" /><span>共 {data.items.length} 条策略</span>{!namespace ? <Badge variant="outline">选择命名空间以计算匹配 Pod</Badge> : null}</div>
          <div className="grid min-w-0 gap-3 xl:grid-cols-2">{data.items.map((policy) => <PolicyCard key={`${policy.namespace}/${policy.name}`} policy={policy} namespace={namespace} />)}</div>
        </>
      )}
    </div>
  );
}
