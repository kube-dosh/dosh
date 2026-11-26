import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleHelp, Network, Play, ShieldAlert, ShieldCheck, Waypoints } from "lucide-react";
import { api } from "@/api/client";
import type { ConnectivityDiagnosis, ConnectivityEndpoint, ConnectivityPolicyDecision } from "@/api/types";
import { EmptyState } from "@/components/feedback/states";
import { StatusBadge } from "@/components/feedback/status-badge";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const VERDICT_LABEL: Record<string, string> = { allowed: "按策略推演可达", degraded: "可能降级", blocked: "被阻断", unknown: "无法确定" };
const PROTOCOLS = ["TCP", "UDP", "SCTP"];

function options(items: { name: string; namespace?: string }[], includeNamespace = false) {
  return items.map((item) => ({
    value: item.name,
    label: includeNamespace && item.namespace ? `${item.namespace}/${item.name}` : item.name,
  }));
}

function boolText(value?: boolean) {
  if (value === undefined) return "无法评估";
  return value ? "允许" : "拒绝";
}

function PolicyDecisions({ title, items }: { title: string; items: ConnectivityPolicyDecision[] }) {
  return (
    <div className="min-w-0 space-y-2">
      <div className="flex flex-wrap items-center gap-2"><p className="text-sm font-medium">{title}</p><Badge variant="secondary">{items.length}</Badge></div>
      {items.length ? <div className="space-y-2">{items.map((decision) => (
        <div key={`${decision.namespace}/${decision.name}`} className="min-w-0 rounded-md border bg-muted/20 p-2.5 text-xs">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><span className="break-all font-mono">{decision.namespace}/{decision.name}</span><Badge variant={decision.allowed ? "success" : "destructive"}>{decision.allowed ? "允许" : "拒绝"}</Badge></div>
          <p className="mt-1 break-words text-muted-foreground">{decision.reason}</p>
        </div>
      ))}</div> : <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">没有策略选择该方向的流量，默认不隔离。</p>}
    </div>
  );
}

function EndpointCard({ endpoint }: { endpoint: ConnectivityEndpoint }) {
  return (
    <div className="min-w-0 rounded-md border bg-muted/20 p-2.5 text-xs">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><span className="break-all font-mono">{endpoint.pod || "未知 Pod"}{endpoint.podIP ? ` · ${endpoint.podIP}` : ""}</span><div className="flex flex-wrap gap-1"><Badge variant={endpoint.ready ? "success" : "warning"}>{endpoint.ready ? "Ready" : "NotReady"}</Badge><Badge variant="outline">{endpoint.allowed}</Badge></div></div>
      <div className="mt-2 grid gap-1 text-muted-foreground sm:grid-cols-2"><span>Egress: {boolText(endpoint.egressAllowed)}</span><span>Ingress: {boolText(endpoint.ingressAllowed)}</span></div>
      {endpoint.reasons?.length ? <ul className="mt-2 space-y-1 text-muted-foreground">{endpoint.reasons.map((reason, index) => <li key={index} className="break-words">• {reason}</li>)}</ul> : null}
    </div>
  );
}

function DiagnosisResult({ result }: { result: ConnectivityDiagnosis }) {
  const verdictTone = result.verdict === "allowed" ? "success" : result.verdict === "blocked" ? "destructive" : result.verdict === "degraded" ? "warning" : "secondary";
  return (
    <div className="min-w-0 space-y-4">
      <Card>
        <CardHeader className="pb-3">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><CardTitle className="flex items-center gap-2 text-base"><Network className="h-4 w-4" />诊断结论</CardTitle><Badge variant={verdictTone}>{VERDICT_LABEL[result.verdict] ?? result.verdict}</Badge></div>
          <CardDescription className="break-words">{result.summary}</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-3 text-xs sm:grid-cols-2 xl:grid-cols-4">
          <div className="min-w-0 rounded-md border p-3"><p className="text-muted-foreground">源 Pod</p><p className="mt-1 break-all font-mono">{result.source.namespace}/{result.source.name}</p><p className="mt-1 break-all text-muted-foreground">{result.source.ip || "无 Pod IP"} · {result.source.status || "Unknown"}</p></div>
          <div className="min-w-0 rounded-md border p-3"><p className="text-muted-foreground">目标 Service</p><p className="mt-1 break-all font-mono">{result.destination.namespace}/{result.destination.name}</p><p className="mt-1 break-all text-muted-foreground">{result.destination.headless ? "Headless" : result.destination.clusterIP || "—"} · {result.destination.type}</p></div>
          <div className="min-w-0 rounded-md border p-3"><p className="text-muted-foreground">DNS</p><p className="mt-1 break-all font-mono">{result.dns.name}</p><p className="mt-1 break-words text-muted-foreground">{result.dns.explanation}</p></div>
          <div className="min-w-0 rounded-md border p-3"><p className="text-muted-foreground">端口映射</p><p className="mt-1 font-mono">{result.port.protocol}/{result.port.servicePort} → {result.port.targetPort || "—"}</p><p className="mt-1 text-muted-foreground">{result.endpoints.length} 个 Endpoint</p></div>
        </CardContent>
      </Card>
      {result.warnings?.length ? <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{result.warnings.join("；")}</div> : null}
      <div className="grid min-w-0 gap-4 xl:grid-cols-2">
        <Card className="min-w-0"><CardHeader className="pb-3"><CardTitle className="flex items-center gap-2 text-base"><ShieldCheck className="h-4 w-4" />策略决策</CardTitle></CardHeader><CardContent className="space-y-4"><PolicyDecisions title="源 Pod Egress" items={result.egressPolicies} /><PolicyDecisions title="目标 Pod Ingress" items={result.ingressPolicies} /></CardContent></Card>
        <Card className="min-w-0"><CardHeader className="pb-3"><CardTitle className="flex items-center gap-2 text-base"><Waypoints className="h-4 w-4" />Endpoint 路径</CardTitle></CardHeader><CardContent className="space-y-2">{result.endpoints.length ? result.endpoints.map((endpoint, index) => <EndpointCard key={`${endpoint.pod}-${index}`} endpoint={endpoint} />) : <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">Service 没有 Endpoint，流量无法到达 Pod。</p>}</CardContent></Card>
      </div>
      <div className="grid min-w-0 gap-4 xl:grid-cols-2">
        <Card className="min-w-0"><CardHeader className="pb-3"><CardTitle className="text-base">检查项</CardTitle></CardHeader><CardContent className="space-y-2">{result.checks.map((check) => <div key={check.id} className="flex min-w-0 items-start gap-2 rounded-md border p-2.5 text-xs"><StatusBadge status={check.status} /><div className="min-w-0"><p className="font-medium">{check.label}</p><p className="mt-1 break-words text-muted-foreground">{check.message}</p></div></div>)}</CardContent></Card>
        <Card className="min-w-0"><CardHeader className="pb-3"><CardTitle className="text-base">发现与建议</CardTitle></CardHeader><CardContent className="space-y-2">{result.findings.length ? result.findings.map((finding) => <div key={finding.id} className="min-w-0 rounded-md border p-2.5 text-xs"><div className="flex flex-wrap items-center gap-2"><StatusBadge status={finding.severity} /><span className="font-medium">{finding.title}</span></div><p className="mt-1 break-words text-muted-foreground">{finding.cause}</p>{finding.recommendations?.length ? <ul className="mt-2 space-y-1 text-muted-foreground">{finding.recommendations.map((recommendation) => <li key={recommendation.label} className="break-words">• {recommendation.label}{recommendation.description ? `：${recommendation.description}` : ""}</li>)}</ul> : null}</div>) : <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">没有额外发现。</p>}</CardContent></Card>
      </div>
      <p className="text-xs leading-5 text-muted-foreground">以上是基于 Kubernetes API 对象的静态推演，不发送真实探测包。CNI 实现差异、节点路由、FQDN 策略与运行时状态仍需结合网络插件验证。</p>
    </div>
  );
}

export function ConnectivityDiagnosisPanel({ namespace }: { namespace: string }) {
  const [sourceNamespace, setSourceNamespace] = useState(namespace);
  const [sourcePod, setSourcePod] = useState("");
  const [destinationNamespace, setDestinationNamespace] = useState(namespace);
  const [destinationService, setDestinationService] = useState("");
  const [port, setPort] = useState("80");
  const [protocol, setProtocol] = useState("TCP");
  const namespaces = useQuery({ queryKey: ["namespaces", "connectivity"], queryFn: () => api.namespaces({ pageSize: 200 }) });
  const pods = useQuery({ queryKey: ["connectivity-pods", sourceNamespace], queryFn: () => api.pods({ namespace: sourceNamespace, pageSize: 200 }), enabled: Boolean(sourceNamespace) });
  const services = useQuery({ queryKey: ["connectivity-services", destinationNamespace], queryFn: () => api.services({ namespace: destinationNamespace, pageSize: 200 }), enabled: Boolean(destinationNamespace) });
  const diagnosis = useMutation({ mutationFn: api.connectivityDiagnosis });

  useEffect(() => {
    if (!namespace) return;
    setSourceNamespace((current) => current || namespace);
    setDestinationNamespace((current) => current || namespace);
  }, [namespace]);

  useEffect(() => {
    if (sourcePod && !pods.data?.items.some((pod) => pod.name === sourcePod)) setSourcePod("");
  }, [pods.data?.items, sourcePod]);
  useEffect(() => {
    if (destinationService && !services.data?.items.some((service) => service.name === destinationService)) setDestinationService("");
  }, [destinationService, services.data?.items]);

  const namespaceOptions = useMemo(() => options((namespaces.data?.items ?? []).map((item) => ({ name: item.name }))), [namespaces.data?.items]);
  const podOptions = useMemo(() => options(pods.data?.items ?? []), [pods.data?.items]);
  const serviceOptions = useMemo(() => options(services.data?.items ?? []), [services.data?.items]);
  const ready = sourceNamespace && sourcePod && destinationNamespace && destinationService && Number(port) >= 1 && Number(port) <= 65535;

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,360px)_minmax(0,1fr)]">
      <Card className="min-w-0 self-start">
        <CardHeader className="pb-3"><CardTitle className="text-base">连通性诊断</CardTitle><CardDescription>从源 Pod 到目标 Service 推演 DNS、端口、Endpoint 与 NetworkPolicy。不会发送网络探测包。</CardDescription></CardHeader>
        <CardContent>
          <form className="min-w-0 space-y-3" onSubmit={(event) => { event.preventDefault(); if (!ready) return; diagnosis.mutate({ sourceNamespace, sourcePod, destinationNamespace, destinationService, port: Number(port), protocol }); }}>
            <div className="space-y-1.5"><Label>源命名空间</Label><SearchableSelect id="connectivity-source-namespace" label="源命名空间" value={sourceNamespace} options={namespaceOptions} onChange={setSourceNamespace} onManualSelect={() => undefined} placeholder={namespaces.isLoading ? "正在加载" : "选择命名空间"} allowManual={false} disabled={namespaces.isLoading || namespaces.isError} /></div>
            <div className="space-y-1.5"><Label>源 Pod</Label><SearchableSelect id="connectivity-source-pod" label="源 Pod" value={sourcePod} options={podOptions} onChange={setSourcePod} onManualSelect={() => undefined} placeholder={!sourceNamespace ? "请先选择命名空间" : pods.isLoading ? "正在加载 Pod" : "选择 Pod"} allowManual={false} disabled={!sourceNamespace || pods.isLoading || pods.isError} /></div>
            <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_96px] gap-2">
              <div className="space-y-1.5"><Label htmlFor="connectivity-port">目标端口</Label><Input id="connectivity-port" inputMode="numeric" value={port} onChange={(event) => setPort(event.target.value.replace(/[^0-9]/g, ""))} placeholder="80" /></div>
              <div className="space-y-1.5"><Label htmlFor="connectivity-protocol">协议</Label><select id="connectivity-protocol" value={protocol} onChange={(event) => setProtocol(event.target.value)} className="flex h-9 w-full rounded-md border border-input bg-card px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">{PROTOCOLS.map((item) => <option key={item} value={item}>{item}</option>)}</select></div>
            </div>
            <div className="space-y-1.5"><Label>目标命名空间</Label><SearchableSelect id="connectivity-destination-namespace" label="目标命名空间" value={destinationNamespace} options={namespaceOptions} onChange={setDestinationNamespace} onManualSelect={() => undefined} placeholder={namespaces.isLoading ? "正在加载" : "选择命名空间"} allowManual={false} disabled={namespaces.isLoading || namespaces.isError} /></div>
            <div className="space-y-1.5"><Label>目标 Service</Label><SearchableSelect id="connectivity-destination-service" label="目标 Service" value={destinationService} options={serviceOptions} onChange={setDestinationService} onManualSelect={() => undefined} placeholder={!destinationNamespace ? "请先选择命名空间" : services.isLoading ? "正在加载 Service" : "选择 Service"} allowManual={false} disabled={!destinationNamespace || services.isLoading || services.isError} /></div>
            {diagnosis.isError ? <p className="break-words rounded-md border border-destructive/30 bg-destructive/5 p-2.5 text-xs text-destructive-strong">{diagnosis.error instanceof Error ? diagnosis.error.message : "诊断失败"}</p> : null}
            <Button type="submit" className="w-full" disabled={!ready || diagnosis.isPending}>{diagnosis.isPending ? <CircleHelp className="h-4 w-4 animate-pulse" /> : <Play className="h-4 w-4" />}{diagnosis.isPending ? "正在推演" : "开始诊断"}</Button>
          </form>
          <div className="mt-4 space-y-2 rounded-md border bg-muted/20 p-3 text-xs text-muted-foreground">
            <p className="flex gap-2"><CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />先解析 Service DNS 与端口，再逐个 Endpoint 检查 Egress/Ingress 策略。</p>
            <p className="flex gap-2"><ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />结论是配置层推演，最终可达性以 CNI 实际执行与运行时网络为准。</p>
          </div>
        </CardContent>
      </Card>
      <div className="min-w-0">{diagnosis.data ? <DiagnosisResult result={diagnosis.data} /> : <EmptyState title="选择源 Pod 与目标 Service" description="Dosh 会解释 DNS、Service 端口映射、EndpointSlice、Egress 与 Ingress NetworkPolicy，并给出阻断原因与排查建议。" />}</div>
    </div>
  );
}
