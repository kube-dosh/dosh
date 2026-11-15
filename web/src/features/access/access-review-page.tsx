import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, KeyRound, ShieldAlert, ShieldX } from "lucide-react";
import { api } from "@/api/client";
import { CopyButton } from "@/components/data/copy-button";
import { ErrorState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const presets = [
  { label: "查看 Pod", verb: "list", apiGroup: "", resource: "pods", subresource: "", namespace: "default" },
  { label: "查看日志", verb: "get", apiGroup: "", resource: "pods", subresource: "log", namespace: "default" },
  { label: "打开终端", verb: "create", apiGroup: "", resource: "pods", subresource: "exec", namespace: "default" },
  { label: "更新 Deployment", verb: "update", apiGroup: "apps", resource: "deployments", subresource: "", namespace: "default" },
  { label: "读取 Secret", verb: "get", apiGroup: "", resource: "secrets", subresource: "", namespace: "default" },
  { label: "恢复节点调度", verb: "update", apiGroup: "", resource: "nodes", subresource: "", namespace: "" },
];

const verbs = ["get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"];

export function AccessReviewPage() {
  const [form, setForm] = useState({ verb: "list", apiGroup: "", resource: "pods", subresource: "", namespace: "default", name: "" });
  const review = useMutation({ mutationFn: () => api.accessReview({ ...form, apiGroup: form.apiGroup || undefined, subresource: form.subresource || undefined, namespace: form.namespace || undefined, name: form.name || undefined }) });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!form.verb.trim() || !form.resource.trim()) return;
    review.mutate();
  };
  const applyPreset = (preset: typeof presets[number]) => {
    setForm({ ...preset, name: "" });
    review.reset();
  };
  const result = review.data;
  return (
    <>
      <PageHeader title="权限检查" breadcrumb="治理与策略 / 权限检查" description="使用 Kubernetes SubjectAccessReview 解释当前登录身份能否执行某个操作，并区分 Dosh 只读模式与 RBAC 拒绝。" />
      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,380px)_minmax(0,1fr)]">
        <Card className="min-w-0">
          <CardHeader><CardTitle className="flex items-center gap-2"><KeyRound className="h-4 w-4" />检查条件</CardTitle><CardDescription>只检查当前登录身份，不接受任意用户名。</CardDescription></CardHeader>
          <CardContent className="space-y-4">
            <div>
              <p className="mb-2 text-xs font-medium text-muted-foreground">常用场景</p>
              <div className="flex flex-wrap gap-2">{presets.map((preset) => <Button key={preset.label} type="button" variant="outline" size="sm" onClick={() => applyPreset(preset)}>{preset.label}</Button>)}</div>
            </div>
            <form className="space-y-4" onSubmit={submit}>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <div className="space-y-1.5"><Label htmlFor="review-verb">Verb</Label><Select value={form.verb} onValueChange={(value) => setForm((current) => ({ ...current, verb: value }))}><SelectTrigger id="review-verb"><SelectValue /></SelectTrigger><SelectContent>{verbs.map((verb) => <SelectItem key={verb} value={verb}>{verb}</SelectItem>)}</SelectContent></Select></div>
                <div className="space-y-1.5"><Label htmlFor="review-resource">Resource</Label><Input id="review-resource" value={form.resource} onChange={(event) => setForm((current) => ({ ...current, resource: event.target.value }))} placeholder="pods" autoComplete="off" /></div>
                <div className="space-y-1.5"><Label htmlFor="review-group">API Group</Label><Input id="review-group" value={form.apiGroup} onChange={(event) => setForm((current) => ({ ...current, apiGroup: event.target.value }))} placeholder="apps；core 留空" autoComplete="off" /></div>
                <div className="space-y-1.5"><Label htmlFor="review-subresource">Subresource</Label><Input id="review-subresource" value={form.subresource} onChange={(event) => setForm((current) => ({ ...current, subresource: event.target.value }))} placeholder="logs / exec / scale" autoComplete="off" /></div>
                <div className="space-y-1.5"><Label htmlFor="review-namespace">Namespace</Label><Input id="review-namespace" value={form.namespace} onChange={(event) => setForm((current) => ({ ...current, namespace: event.target.value }))} placeholder="集群级资源留空" autoComplete="off" /></div>
                <div className="space-y-1.5"><Label htmlFor="review-name">资源名称</Label><Input id="review-name" value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} placeholder="可选" autoComplete="off" /></div>
              </div>
              <Button type="submit" className="w-full" disabled={review.isPending || !form.verb.trim() || !form.resource.trim()}>{review.isPending ? "正在检查…" : "检查权限"}</Button>
            </form>
          </CardContent>
        </Card>
        <div className="min-w-0 space-y-4">
          {review.isError ? <ErrorState error={review.error} onRetry={() => review.mutate()} /> : null}
          {!result && !review.isError ? <Card><CardContent className="flex min-h-56 flex-col items-center justify-center gap-3 py-10 text-center text-sm text-muted-foreground"><KeyRound className="h-7 w-7" /><p>选择常用场景或填写条件后开始检查。</p><p className="max-w-md text-xs">结果来自 Kubernetes SubjectAccessReview；页面不会缓存或展示其他身份的权限。</p></CardContent></Card> : null}
          {result ? (
            <Card className={result.allowed ? "border-success/35" : "border-destructive/35"}>
              <CardHeader>
                <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
                  <div><CardTitle className="flex items-center gap-2">{result.allowed ? <CheckCircle2 className="h-5 w-5 text-success" /> : result.reason === "read_only" ? <ShieldAlert className="h-5 w-5 text-warning" /> : <ShieldX className="h-5 w-5 text-destructive-strong" />}{result.allowed ? "允许执行" : "不允许执行"}</CardTitle><CardDescription className="mt-1 break-all font-mono">{result.verb} {result.resource}{result.apiGroup ? `.${result.apiGroup}` : ""}{result.subresource ? `/${result.subresource}` : ""}{result.namespace ? ` · ${result.namespace}` : " · 集群级"}{result.name ? ` · ${result.name}` : ""}</CardDescription></div>
                  <Badge variant={result.allowed ? "success" : result.reason === "read_only" ? "warning" : "destructive"}>{result.reason === "allowed" ? "Allowed" : result.reason === "read_only" ? "Read-only" : result.reason === "rbac_denied" ? "RBAC denied" : "Unavailable"}</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                <p className="break-words text-sm leading-6 text-muted-foreground">{result.guidance}</p>
                <div className="min-w-0 rounded-md border bg-muted/20 p-3"><div className="mb-2 flex flex-wrap items-center justify-between gap-2"><p className="text-xs font-medium">复现命令</p><CopyButton value={result.kubectlCommand} label="复制" /></div><pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs">{result.kubectlCommand}</pre></div>
                {!result.allowed ? <p className="text-xs leading-5 text-muted-foreground">排查建议：先确认 SubjectAccessReview 条件与页面请求完全一致，再检查 RoleBinding / ClusterRoleBinding 是否覆盖当前用户或所属 Group。Role 只对所在命名空间生效。</p> : null}
              </CardContent>
            </Card>
          ) : null}
        </div>
      </div>
    </>
  );
}
