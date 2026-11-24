import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  CheckCircle2, Clock3, KeyRound, Loader2, Plus, ShieldAlert, ShieldCheck, ShieldX, Sparkles, Trash2, UserRoundCheck,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { AccessReview, AccessTokenRule, IssuedAccessToken, ResourceDescriptor } from "@/api/types";
import { CopyButton } from "@/components/data/copy-button";
import { ErrorState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useSessionQuery } from "@/features/auth/use-session";
import { formatDate } from "@/lib/utils";

const presets = [
  { label: "查看 Pod", verb: "list", apiGroup: "", resource: "pods", subresource: "", namespace: "default" },
  { label: "查看日志", verb: "get", apiGroup: "", resource: "pods", subresource: "log", namespace: "default" },
  { label: "打开终端", verb: "create", apiGroup: "", resource: "pods", subresource: "exec", namespace: "default" },
  { label: "更新 Deployment", verb: "update", apiGroup: "apps", resource: "deployments", subresource: "", namespace: "default" },
  { label: "读取 Secret", verb: "get", apiGroup: "", resource: "secrets", subresource: "", namespace: "default" },
  { label: "恢复节点调度", verb: "update", apiGroup: "", resource: "nodes", subresource: "", namespace: "" },
];

const verbs = ["get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"];
const subresourceLabels: Record<string, string> = {
  log: "log（日志）",
  exec: "exec（终端）",
  status: "status（状态）",
  scale: "scale（扩缩容）",
  ephemeralcontainers: "ephemeralcontainers（临时容器）",
  proxy: "proxy（代理）",
  finalize: "finalize（终结）",
};

type FieldOption = { value: string; label: string };
type ManualField = "resource" | "apiGroup" | "subresource" | "namespace" | "name";
type AccessTab = "current" | "token" | "issue";
type GrantRuleDraft = {
  verbs: string[];
  apiGroup: string;
  resource: string;
  subresource: string;
  resourceName: string;
  manualResource: boolean;
};

function descriptorKey(descriptor: Pick<ResourceDescriptor, "group" | "resource">) {
  return `${descriptor.group}/${descriptor.resource}`;
}

function versionRank(version: string) {
  return (version.match(/\d+/g) ?? []).reduce((rank, part) => rank * 1000 + Number(part), 0);
}

function preferredResourceDescriptors(items: ResourceDescriptor[]) {
  const descriptors = new Map<string, ResourceDescriptor>();
  for (const item of items) {
    const key = descriptorKey(item);
    const current = descriptors.get(key);
    if (!current
      || (item.preferred && !current.preferred)
      || (!item.preferred && !current.preferred && versionRank(item.version) > versionRank(current.version))) {
      descriptors.set(key, item);
    }
  }
  return [...descriptors.values()].sort((left, right) => (
    (left.group || "core").localeCompare(right.group || "core")
    || left.kind.localeCompare(right.kind)
    || left.resource.localeCompare(right.resource)
  ));
}

function LinkedField({ id, label, value, options, onChange, manual, onManualChange, placeholder, manualPlaceholder, manualValue, onManualValueChange, loading = false, disabled = false, hint }: {
  id: string;
  label: string;
  value: string;
  options: FieldOption[];
  onChange: (value: string) => void;
  manual: boolean;
  onManualChange: (manual: boolean) => void;
  placeholder: string;
  manualPlaceholder: string;
  manualValue?: string;
  onManualValueChange?: (value: string) => void;
  loading?: boolean;
  disabled?: boolean;
  hint?: string;
}) {
  const inputValue = manualValue ?? value;
  const hasValue = options.some((option) => option.value === value);
  const isManual = manual || (!loading && (options.length === 0 || (inputValue !== "" && !hasValue)));
  const returnToList = () => {
    onManualChange(false);
    if (!hasValue) {
      if (onManualValueChange) onManualValueChange("");
      else onChange("");
    }
  };
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <Label htmlFor={id}>{label}</Label>
        {isManual && options.length > 0 ? <button type="button" className="shrink-0 text-xs text-primary underline-offset-4 hover:underline" aria-label={`返回 ${label} 列表`} onClick={returnToList}>返回列表</button> : null}
      </div>
      {loading && options.length === 0 ? (
        <SearchableSelect id={id} label={label} value="" options={[]} onChange={onChange} onManualSelect={() => onManualChange(true)} placeholder="正在读取列表…" disabled />
      ) : isManual ? (
        <Input id={id} aria-label={label} value={inputValue} onChange={(event) => (onManualValueChange ?? onChange)(event.target.value)} placeholder={manualPlaceholder} autoComplete="off" disabled={disabled} />
      ) : (
        <SearchableSelect
          id={id}
          label={label}
          value={value}
          options={options}
          onChange={onChange}
          onManualSelect={() => onManualChange(true)}
          placeholder={placeholder}
          disabled={disabled}
        />
      )}
      {hint ? <p className="break-words text-xs leading-5 text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function AccessReviewResult({ review }: { review: AccessReview }) {
  return (
    <Card className={review.allowed ? "border-success/35" : "border-destructive/35"}>
      <CardHeader>
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2">
              {review.allowed ? <CheckCircle2 className="h-5 w-5 text-success" /> : review.reason === "read_only" ? <ShieldAlert className="h-5 w-5 text-warning" /> : <ShieldX className="h-5 w-5 text-destructive-strong" />}
              {review.allowed ? "允许执行" : "不允许执行"}
            </CardTitle>
            <CardDescription className="mt-1 break-all font-mono">{review.verb} {review.resource}{review.apiGroup ? `.${review.apiGroup}` : ""}{review.subresource ? `/${review.subresource}` : ""}{review.namespace ? ` · ${review.namespace}` : " · 集群级"}{review.name ? ` · ${review.name}` : ""}</CardDescription>
          </div>
          <Badge variant={review.allowed ? "success" : review.reason === "read_only" ? "warning" : "destructive"}>{review.reason === "allowed" ? "Allowed" : review.reason === "read_only" ? "Read-only" : review.reason === "rbac_denied" ? "RBAC denied" : "Unavailable"}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="break-words text-sm leading-6 text-muted-foreground">{review.guidance}</p>
        <div className="min-w-0 rounded-md border bg-muted/20 p-3">
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2"><p className="text-xs font-medium">复现命令</p><CopyButton value={review.kubectlCommand} label="复制" /></div>
          <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs">{review.kubectlCommand}</pre>
        </div>
        {!review.allowed ? <p className="text-xs leading-5 text-muted-foreground">排查建议：先确认 SubjectAccessReview 条件与页面请求完全一致，再检查 RoleBinding / ClusterRoleBinding 是否覆盖目标身份或所属 Group。Role 只对所在命名空间生效。</p> : null}
      </CardContent>
    </Card>
  );
}

function AccessCheckPanel({ mode }: { mode: "current" | "token" }) {
  const [form, setForm] = useState({ verb: "list", apiGroup: "", resource: "pods", subresource: "", namespace: "default", name: "" });
  const [manual, setManual] = useState<Record<ManualField, boolean>>({ resource: false, apiGroup: false, subresource: false, namespace: false, name: false });
  const [token, setToken] = useState("");
  const prefix = mode === "token" ? "token" : "current";
  const discovery = useQuery({ queryKey: ["resource-discovery", "all"], queryFn: () => api.resourceDiscovery(), staleTime: 60_000 });
  const namespaces = useQuery({ queryKey: ["namespaces", "access-review"], queryFn: () => api.namespaces({ pageSize: 200 }), staleTime: 60_000, retry: false });
  const currentReview = useMutation({ mutationFn: () => api.accessReview({ ...form, apiGroup: form.apiGroup || undefined, subresource: form.subresource || undefined, namespace: form.namespace || undefined, name: form.name || undefined }) });
  const tokenReview = useMutation({ mutationFn: () => api.reviewAccessToken({ token, ...form, apiGroup: form.apiGroup || undefined, subresource: form.subresource || undefined, namespace: form.namespace || undefined, name: form.name || undefined }) });

  const descriptors = useMemo(() => preferredResourceDescriptors(discovery.data?.items ?? []), [discovery.data?.items]);
  const selectedDescriptor = useMemo(() => descriptors.find((item) => item.group === form.apiGroup && item.resource === form.resource), [descriptors, form.apiGroup, form.resource]);
  const resourceOptions = useMemo<FieldOption[]>(() => descriptors.map((descriptor) => ({
    value: descriptorKey(descriptor),
    label: `${descriptor.kind} · ${descriptor.resource} · ${descriptor.group || "core"}/${descriptor.version}`,
  })), [descriptors]);
  const groupOptions = useMemo<FieldOption[]>(() => [...new Set(descriptors.map((descriptor) => descriptor.group))]
    .sort((left, right) => (left || "core").localeCompare(right || "core"))
    .map((group) => ({ value: group, label: group || "core（留空）" })), [descriptors]);
  const subresourceOptions = useMemo<FieldOption[]>(() => {
    if (!selectedDescriptor) return [];
    const names = [...new Set(selectedDescriptor.subresources ?? [])].sort((left, right) => left.localeCompare(right));
    return [
      { value: "", label: "无子资源（留空）" },
      ...names.map((name) => ({ value: name, label: subresourceLabels[name] ?? name })),
    ];
  }, [selectedDescriptor]);
  const namespaceOptions = useMemo<FieldOption[]>(() => [
    { value: "", label: "集群级资源（留空）" },
    ...(namespaces.data?.items ?? []).map((item) => ({ value: item.name, label: item.name })),
  ], [namespaces.data?.items]);
  const nameReference = selectedDescriptor
    ? { group: selectedDescriptor.group, version: selectedDescriptor.version, resource: selectedDescriptor.resource }
    : undefined;
  const names = useQuery({
    queryKey: ["access-review-resource-names", nameReference?.group, nameReference?.version, nameReference?.resource, selectedDescriptor?.namespaced ? form.namespace : ""],
    queryFn: () => api.resources(nameReference!, {
      namespace: selectedDescriptor?.namespaced ? form.namespace : undefined,
      pageSize: 200,
    }),
    enabled: Boolean(nameReference) && Boolean(!selectedDescriptor?.namespaced || form.namespace),
    staleTime: 30_000,
    retry: false,
  });
  const nameOptions = useMemo<FieldOption[]>(() => {
    const items = [...new Set((names.data?.items ?? []).map((item) => item.name).filter(Boolean))].sort((left, right) => left.localeCompare(right));
    if (items.length === 0) return [];
    return [{ value: "", label: "任意资源（不填名称）" }, ...items.map((name) => ({ value: name, label: name }))];
  }, [names.data?.items]);

  const setManualField = (field: ManualField, value: boolean) => setManual((current) => ({ ...current, [field]: value }));
  const resetManual = () => setManual({ resource: false, apiGroup: false, subresource: false, namespace: false, name: false });
  const mutation = mode === "token" ? tokenReview : currentReview;
  const result = mode === "token" ? tokenReview.data?.review : currentReview.data;
  const identity = mode === "token" ? tokenReview.data?.identity : undefined;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!form.verb.trim() || !form.resource.trim() || (mode === "token" && !token.trim())) return;
    if (mode === "token") tokenReview.mutate();
    else currentReview.mutate();
  };
  const applyPreset = (preset: typeof presets[number]) => {
    setForm({ ...preset, name: "" });
    resetManual();
    currentReview.reset();
    tokenReview.reset();
  };
  const selectResource = (key: string) => {
    const descriptor = descriptors.find((item) => descriptorKey(item) === key);
    if (!descriptor) return;
    setForm((current) => current.apiGroup === descriptor.group && current.resource === descriptor.resource ? current : ({
      ...current,
      apiGroup: descriptor.group,
      resource: descriptor.resource,
      subresource: "",
      namespace: descriptor.namespaced ? (current.namespace || (namespaces.data?.items.some((item) => item.name === "default") ? "default" : "")) : "",
      name: "",
    }));
    setManual((current) => ({ ...current, resource: false, apiGroup: false, subresource: false, name: false }));
  };
  const selectGroup = (group: string) => {
    setForm((current) => current.apiGroup === group ? current : ({ ...current, apiGroup: group, subresource: "", name: "" }));
    setManual((current) => ({ ...current, apiGroup: false, subresource: false, name: false }));
  };
  const selectNamespace = (namespace: string) => {
    setForm((current) => current.namespace === namespace ? current : ({ ...current, namespace, name: "" }));
    setManual((current) => ({ ...current, namespace: false, name: false }));
  };
  const clusterScoped = Boolean(selectedDescriptor && !selectedDescriptor.namespaced);
  const resourceHint = discovery.isLoading ? "正在读取 Kubernetes API Discovery…" : discovery.isError ? "资源列表加载失败，可手动输入 Resource 与 API Group。" : undefined;
  const namespaceHint = clusterScoped ? "当前资源为集群级，Namespace 将留空。" : namespaces.isError ? "命名空间列表加载失败，可手动输入。" : undefined;

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,400px)_minmax(0,1fr)]">
      <Card className="min-w-0">
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><KeyRound className="h-4 w-4" />{mode === "token" ? "Token 检查条件" : "检查条件"}</CardTitle>
          <CardDescription>{mode === "token" ? "先验证 Token 的 Kubernetes 身份，再用 SubjectAccessReview 检查其权限。" : "使用当前登录身份执行 SubjectAccessReview，并区分 Dosh 只读模式与 RBAC 拒绝。"}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <p className="mb-2 text-xs font-medium text-muted-foreground">常用场景</p>
            <div className="flex flex-wrap gap-2">{presets.map((preset) => <Button key={preset.label} type="button" variant="outline" size="sm" onClick={() => applyPreset(preset)}>{preset.label}</Button>)}</div>
          </div>
          <form className="space-y-4" onSubmit={submit}>
            {mode === "token" ? (
              <div className="space-y-1.5">
                <Label htmlFor={`${prefix}-kubernetes-token`}>Kubernetes Token</Label>
                <Textarea id={`${prefix}-kubernetes-token`} aria-label="Kubernetes Token" value={token} onChange={(event) => setToken(event.target.value)} className="min-h-24 font-mono text-xs" autoComplete="off" spellCheck={false} placeholder="粘贴需要检查的 Bearer Token" />
                <p className="text-xs leading-5 text-muted-foreground">Token 仅用于本次 TokenReview 和 SubjectAccessReview，不会写入浏览器存储、审计详情或服务端状态。</p>
              </div>
            ) : null}
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5"><Label htmlFor={`${prefix}-verb`}>Verb</Label><Select value={form.verb} onValueChange={(value) => setForm((current) => ({ ...current, verb: value }))}><SelectTrigger id={`${prefix}-verb`} aria-label="Verb"><SelectValue /></SelectTrigger><SelectContent>{verbs.map((verb) => <SelectItem key={verb} value={verb}>{verb}</SelectItem>)}</SelectContent></Select></div>
              <LinkedField
                id={`${prefix}-resource`}
                label="Resource"
                value={selectedDescriptor ? descriptorKey(selectedDescriptor) : ""}
                options={resourceOptions}
                onChange={selectResource}
                manual={manual.resource}
                onManualChange={(value) => setManualField("resource", value)}
                manualValue={form.resource}
                onManualValueChange={(resource) => setForm((current) => ({ ...current, resource }))}
                placeholder="选择资源类型"
                manualPlaceholder="pods"
                loading={discovery.isLoading}
                hint={resourceHint}
              />
              <LinkedField
                id={`${prefix}-api-group`}
                label="API Group"
                value={form.apiGroup}
                options={groupOptions}
                onChange={selectGroup}
                manual={manual.apiGroup}
                onManualChange={(value) => setManualField("apiGroup", value)}
                manualValue={form.apiGroup}
                onManualValueChange={(apiGroup) => setForm((current) => ({ ...current, apiGroup }))}
                placeholder="选择 API Group"
                manualPlaceholder="core 资源留空"
              />
              <LinkedField
                id={`${prefix}-subresource`}
                label="Subresource"
                value={form.subresource}
                options={subresourceOptions}
                onChange={(subresource) => setForm((current) => ({ ...current, subresource }))}
                manual={manual.subresource}
                onManualChange={(value) => setManualField("subresource", value)}
                placeholder="选择子资源"
                manualPlaceholder="log / exec / scale"
                hint={!selectedDescriptor && form.resource ? "资源未在 Discovery 中匹配，Subresource 可手动输入。" : undefined}
              />
              <LinkedField
                id={`${prefix}-namespace`}
                label="Namespace"
                value={form.namespace}
                options={namespaceOptions}
                onChange={selectNamespace}
                manual={manual.namespace}
                onManualChange={(value) => setManualField("namespace", value)}
                placeholder="选择命名空间"
                manualPlaceholder="集群级资源留空"
                loading={namespaces.isLoading}
                disabled={clusterScoped}
                hint={namespaceHint}
              />
              <LinkedField
                id={`${prefix}-name`}
                label="资源名称"
                value={form.name}
                options={nameOptions}
                onChange={(name) => setForm((current) => ({ ...current, name }))}
                manual={manual.name}
                onManualChange={(value) => setManualField("name", value)}
                placeholder="任意资源（不填名称）"
                manualPlaceholder="可选"
                loading={names.isLoading}
                hint={names.isError ? "资源名称列表加载失败，可手动输入。" : undefined}
              />
            </div>
            <Button type="submit" className="w-full" disabled={mutation.isPending || !form.verb.trim() || !form.resource.trim() || (mode === "token" && !token.trim())}>
              {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : mode === "token" ? <UserRoundCheck className="h-4 w-4" /> : <ShieldCheck className="h-4 w-4" />}
              {mutation.isPending ? "正在检查…" : mode === "token" ? "验证 Token 并检查权限" : "检查权限"}
            </Button>
          </form>
        </CardContent>
      </Card>
      <div className="min-w-0 space-y-4">
        {mutation.isError ? <ErrorState error={mutation.error} onRetry={() => mutation.mutate()} /> : null}
        {identity ? (
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-base"><UserRoundCheck className="h-4 w-4 text-primary" />Token 身份</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <p className="break-all font-mono">{identity.username}</p>
              <div className="flex flex-wrap gap-2">{identity.admin ? <Badge variant="warning">Dosh 管理员</Badge> : <Badge variant="secondary">普通身份</Badge>}{identity.groups?.length ? identity.groups.map((group) => <Badge key={group} variant="outline">{group}</Badge>) : <span className="text-xs text-muted-foreground">无 Group</span>}</div>
            </CardContent>
          </Card>
        ) : null}
        {!result && !mutation.isError ? <Card><CardContent className="flex min-h-56 flex-col items-center justify-center gap-3 py-10 text-center text-sm text-muted-foreground"><KeyRound className="h-7 w-7" /><p>{mode === "token" ? "粘贴 Token 并选择条件后开始检查。" : "选择常用场景或填写条件后开始检查。"}</p><p className="max-w-md text-xs">结果来自 Kubernetes SubjectAccessReview；Token 不会被保存，也不会显示在审计详情中。</p></CardContent></Card> : null}
        {result ? <AccessReviewResult review={result} /> : null}
      </div>
    </div>
  );
}

function emptyGrantRule(): GrantRuleDraft {
  return { verbs: ["get", "list"], apiGroup: "", resource: "pods", subresource: "", resourceName: "", manualResource: false };
}

function namespacedRule(apiGroup: string, resource: string, verbs: string[]): GrantRuleDraft {
  return { verbs, apiGroup, resource, subresource: "", resourceName: "", manualResource: false };
}

function AccessTokenIssuePanel() {
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [namespaceManual, setNamespaceManual] = useState(false);
  const [ttlSeconds, setTTLSeconds] = useState("3600");
  const [rules, setRules] = useState<GrantRuleDraft[]>([emptyGrantRule()]);
  const [issued, setIssued] = useState<IssuedAccessToken>();
  const discovery = useQuery({ queryKey: ["resource-discovery", "all"], queryFn: () => api.resourceDiscovery(), staleTime: 60_000 });
  const namespaces = useQuery({ queryKey: ["namespaces", "access-review"], queryFn: () => api.namespaces({ pageSize: 200 }), staleTime: 60_000, retry: false });
  const issue = useMutation({ mutationFn: () => api.issueAccessToken({ name: name || undefined, namespace, ttlSeconds: Number(ttlSeconds), rules: rules.map(toAccessTokenRule) }) });
  const cleanup = useMutation({ mutationFn: () => api.cleanupAccessTokens(namespace) });

  const descriptors = useMemo(() => preferredResourceDescriptors(discovery.data?.items ?? []).filter((item) => item.namespaced), [discovery.data?.items]);
  const resourceOptions = useMemo<FieldOption[]>(() => descriptors.map((descriptor) => ({
    value: descriptorKey(descriptor), label: `${descriptor.kind} · ${descriptor.resource} · ${descriptor.group || "core"}/${descriptor.version}`,
  })), [descriptors]);
  const namespaceOptions = useMemo<FieldOption[]>(() => (namespaces.data?.items ?? []).map((item) => ({ value: item.name, label: item.name })), [namespaces.data?.items]);
  const grantValid = namespace.trim() !== "" && rules.length > 0 && rules.every((rule) => (
    rule.resource.trim() !== ""
    && !rule.resource.includes("*")
    && !rule.apiGroup.includes("*")
    && !rule.subresource.includes("*")
    && !rule.resourceName.includes("*")
    && rule.verbs.length > 0
  ));

  const updateRule = (index: number, patch: Partial<GrantRuleDraft>) => {
    setRules((current) => current.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, ...patch } : rule));
  };
  const toggleVerb = (index: number, verb: string) => {
    setRules((current) => current.map((rule, ruleIndex) => {
      if (ruleIndex !== index) return rule;
      return { ...rule, verbs: rule.verbs.includes(verb) ? rule.verbs.filter((item) => item !== verb) : [...rule.verbs, verb] };
    }));
  };
  const selectResource = (index: number, key: string) => {
    const descriptor = descriptors.find((item) => descriptorKey(item) === key);
    if (!descriptor) return;
    updateRule(index, { apiGroup: descriptor.group, resource: descriptor.resource, subresource: "", manualResource: false });
  };
  const applyReadonlyPreset = () => {
    setRules([
      namespacedRule("", "pods", ["get", "list", "watch"]),
      namespacedRule("", "services", ["get", "list", "watch"]),
      namespacedRule("", "configmaps", ["get", "list", "watch"]),
      namespacedRule("", "events", ["get", "list", "watch"]),
      namespacedRule("apps", "deployments", ["get", "list", "watch"]),
      namespacedRule("apps", "statefulsets", ["get", "list", "watch"]),
      namespacedRule("apps", "daemonsets", ["get", "list", "watch"]),
      namespacedRule("batch", "jobs", ["get", "list", "watch"]),
      namespacedRule("batch", "cronjobs", ["get", "list", "watch"]),
      namespacedRule("networking.k8s.io", "ingresses", ["get", "list", "watch"]),
    ]);
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!grantValid) return;
    issue.mutate(undefined, { onSuccess: (value) => setIssued(value) });
  };

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,420px)]">
      <Card className="min-w-0">
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><Sparkles className="h-4 w-4" />签发临时 Token</CardTitle>
          <CardDescription>为指定命名空间创建独立 ServiceAccount、Role 和 RoleBinding，并签发有时效的 Token。权限必须以显式规则授予。</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-5" onSubmit={submit}>
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5"><Label htmlFor="grant-name">用途名称</Label><Input id="grant-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 web-oncall" maxLength={80} autoComplete="off" /></div>
              <LinkedField id="grant-namespace" label="Namespace" value={namespace} options={namespaceOptions} onChange={setNamespace} manual={namespaceManual} onManualChange={setNamespaceManual} placeholder="选择命名空间" manualPlaceholder="default" loading={namespaces.isLoading} hint={namespaces.isError ? "命名空间列表加载失败，可手动输入。" : undefined} />
              <div className="space-y-1.5">
                <Label htmlFor="grant-ttl">有效期</Label>
                <Select value={ttlSeconds} onValueChange={setTTLSeconds}><SelectTrigger id="grant-ttl" aria-label="Token 有效期"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="900">15 分钟</SelectItem><SelectItem value="3600">1 小时</SelectItem><SelectItem value="28800">8 小时</SelectItem><SelectItem value="86400">24 小时</SelectItem></SelectContent></Select>
              </div>
            </div>
            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div><p className="text-sm font-medium">权限规则</p><p className="text-xs text-muted-foreground">仅支持命名空间级资源；Subresource 请填写 log、exec、scale 等值。签发规则不接受 * 通配符。</p></div>
                <div className="flex flex-wrap gap-2"><Button type="button" variant="outline" size="sm" onClick={applyReadonlyPreset}><ShieldCheck className="h-3.5 w-3.5" />命名空间只读</Button><Button type="button" variant="outline" size="sm" onClick={() => setRules((current) => [...current, emptyGrantRule()])} disabled={rules.length >= 20}><Plus className="h-3.5 w-3.5" />添加规则</Button></div>
              </div>
              <div className="space-y-3">
                {rules.map((rule, index) => (
                  <div key={index} className="min-w-0 space-y-3 rounded-lg border bg-muted/20 p-3">
                    <div className="flex items-center justify-between gap-2"><p className="text-sm font-medium">规则 {index + 1}</p><Button type="button" variant="ghost" size="icon" aria-label={`删除规则 ${index + 1}`} onClick={() => setRules((current) => current.filter((_, ruleIndex) => ruleIndex !== index))} disabled={rules.length === 1}><Trash2 className="h-4 w-4" /></Button></div>
                    <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                      <div className="min-w-0 space-y-1.5">
                        <div className="flex items-center justify-between gap-2"><Label htmlFor={`grant-resource-${index}`}>资源 {index + 1}</Label>{rule.manualResource && resourceOptions.length > 0 ? <button type="button" className="text-xs text-primary underline-offset-4 hover:underline" onClick={() => updateRule(index, { manualResource: false })}>返回列表</button> : null}</div>
                        {!rule.manualResource && (discovery.isLoading || resourceOptions.length > 0) ? (
                          <SearchableSelect id={`grant-resource-${index}`} label={`资源 ${index + 1}`} value={descriptorKey({ group: rule.apiGroup, resource: rule.resource })} options={resourceOptions} onChange={(value) => selectResource(index, value)} onManualSelect={() => updateRule(index, { manualResource: true })} placeholder="选择命名空间级资源" />
                        ) : (
                          <Input id={`grant-resource-${index}`} aria-label={`资源 ${index + 1}`} value={rule.resource} onChange={(event) => updateRule(index, { resource: event.target.value })} placeholder="pods" autoComplete="off" />
                        )}
                        {!rule.manualResource && !discovery.isLoading && resourceOptions.length === 0 ? <p className="text-xs leading-5 text-muted-foreground">资源列表不可用，已切换为手动输入。</p> : null}
                      </div>
                      <div className="space-y-1.5"><Label htmlFor={`grant-subresource-${index}`}>Subresource {index + 1}</Label><Input id={`grant-subresource-${index}`} aria-label={`Subresource ${index + 1}`} value={rule.subresource} onChange={(event) => updateRule(index, { subresource: event.target.value })} placeholder="可选，例如 log / exec" autoComplete="off" /></div>
                      <div className="space-y-1.5"><Label htmlFor={`grant-resource-name-${index}`}>资源名称 {index + 1}</Label><Input id={`grant-resource-name-${index}`} aria-label={`资源名称 ${index + 1}`} value={rule.resourceName} onChange={(event) => updateRule(index, { resourceName: event.target.value })} placeholder="可选；不填表示该类型全部对象" autoComplete="off" /></div>
                      <div className="space-y-1.5"><Label htmlFor={`grant-api-group-${index}`}>API Group {index + 1}</Label><Input id={`grant-api-group-${index}`} aria-label={`API Group ${index + 1}`} value={rule.apiGroup} onChange={(event) => updateRule(index, { apiGroup: event.target.value })} placeholder="core 资源留空" autoComplete="off" /></div>
                    </div>
                    <div className="space-y-2"><p className="text-xs font-medium text-muted-foreground">Verbs</p><div className="flex flex-wrap gap-1.5">{verbs.map((verb) => <Button key={verb} type="button" size="sm" variant={rule.verbs.includes(verb) ? "default" : "outline"} aria-pressed={rule.verbs.includes(verb)} aria-label={`${verb} 权限`} onClick={() => toggleVerb(index, verb)}>{verb}</Button>)}</div>{rule.verbs.length === 0 ? <p className="text-xs text-destructive-strong">至少选择一个 Verb</p> : null}</div>
                  </div>
                ))}
              </div>
            </div>
            {issue.isError ? <ErrorState error={issue.error} /> : null}
            <Button type="submit" className="w-full" disabled={!grantValid || issue.isPending}>{issue.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}{issue.isPending ? "正在创建临时身份并签发…" : "签发 Token"}</Button>
          </form>
        </CardContent>
      </Card>
      <div className="min-w-0 space-y-4">
        {issued ? (
          <Card className="border-success/35">
            <CardHeader><CardTitle className="flex items-center gap-2"><CheckCircle2 className="h-5 w-5 text-success" />Token 已签发</CardTitle><CardDescription>Token 只在此处显示一次。请立即复制并通过安全渠道交付。</CardDescription></CardHeader>
            <CardContent className="space-y-3">
              <div className="flex flex-wrap gap-2"><Badge variant="secondary" className="font-mono">Token</Badge><Badge variant="outline" className="break-all font-mono">{issued.namespace}/{issued.serviceAccount}</Badge></div>
              <div className="min-w-0 rounded-md border bg-muted/30 p-3"><div className="mb-2 flex flex-wrap items-center justify-between gap-2"><p className="text-xs font-medium">Bearer Token</p><CopyButton value={issued.token} label="复制 Token" /></div><Textarea readOnly aria-label="签发结果 Token" value={issued.token} className="min-h-32 font-mono text-xs" /></div>
              <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2"><p>签发时间：{formatDate(issued.createdAt)}</p><p>到期时间：{formatDate(issued.expiresAt)}</p></div>
              <p className="text-xs leading-5 text-muted-foreground">权限：{issued.rules.map((rule) => `${rule.verbs.join("/")} ${rule.resources.join(",")}`).join("；")}</p>
              <p className="text-xs leading-5 text-muted-foreground">Token 到期后自动失效，但 ServiceAccount/Role/RoleBinding 需要清理。可使用下方操作清理当前命名空间内已过期授权。</p>
              {issued.warning ? <p className="text-xs text-warning">{issued.warning}</p> : null}
            </CardContent>
          </Card>
        ) : (
          <Card><CardContent className="flex min-h-56 flex-col items-center justify-center gap-3 py-10 text-center text-sm text-muted-foreground"><KeyRound className="h-7 w-7" /><p>填写有期限的命名空间权限后签发 Token。</p><p className="max-w-md text-xs">Dosh 不保存 Token。签发的身份拥有独立 RBAC，不会复用当前登录用户身份。</p></CardContent></Card>
        )}
        <Card>
          <CardHeader className="pb-3"><CardTitle className="flex items-center gap-2 text-base"><Clock3 className="h-4 w-4" />过期授权清理</CardTitle><CardDescription>清理当前命名空间内由 Dosh 签发且已过期的 ServiceAccount、Role 和 RoleBinding。</CardDescription></CardHeader>
          <CardContent className="space-y-3">
            {cleanup.isError ? <ErrorState error={cleanup.error} /> : null}
            <Button type="button" variant="outline" className="w-full" disabled={!namespace || cleanup.isPending} onClick={() => cleanup.mutate(undefined, { onSuccess: (result) => toast.success(result.deleted > 0 ? `已清理 ${result.deleted} 个过期授权` : "当前命名空间没有过期授权") })}>{cleanup.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}{cleanup.isPending ? "正在清理…" : `清理 ${namespace || "当前命名空间"} 的过期授权`}</Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function toAccessTokenRule(rule: GrantRuleDraft): AccessTokenRule {
  const resource = rule.resource.trim();
  const subresource = rule.subresource.trim();
  const resourceName = rule.resourceName.trim();
  return {
    verbs: rule.verbs,
    apiGroups: [rule.apiGroup.trim()],
    resources: [subresource ? `${resource}/${subresource}` : resource],
    ...(resourceName ? { resourceNames: [resourceName] } : {}),
  };
}

export function AccessReviewPage() {
  const session = useSessionQuery();
  const [tab, setTab] = useState<AccessTab>("current");
  const admin = Boolean(session.data?.user.admin);
  const effectiveTab = tab === "issue" && !admin ? "current" : tab;
  return (
    <>
      <PageHeader title="权限与检查" breadcrumb="其他功能 / 权限与检查" description="检查当前身份或指定 Token 的 Kubernetes 权限，并由管理员签发有时效的命名空间级访问 Token。" />
      <Tabs value={effectiveTab} onValueChange={(value) => setTab(value as AccessTab)}>
        <TabsList aria-label="权限与检查模式">
          <TabsTrigger value="current">当前身份</TabsTrigger>
          <TabsTrigger value="token">Token 检查</TabsTrigger>
          {admin ? <TabsTrigger value="issue">签发 Token</TabsTrigger> : null}
        </TabsList>
        <TabsContent value="current"><AccessCheckPanel mode="current" /></TabsContent>
        <TabsContent value="token"><AccessCheckPanel mode="token" /></TabsContent>
        {admin ? <TabsContent value="issue"><AccessTokenIssuePanel /></TabsContent> : null}
      </Tabs>
    </>
  );
}
