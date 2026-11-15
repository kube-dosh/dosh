import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, KeyRound, ShieldAlert, ShieldX } from "lucide-react";
import { api } from "@/api/client";
import type { ResourceDescriptor } from "@/api/types";
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

export function AccessReviewPage() {
  const [form, setForm] = useState({ verb: "list", apiGroup: "", resource: "pods", subresource: "", namespace: "default", name: "" });
  const [manual, setManual] = useState<Record<ManualField, boolean>>({ resource: false, apiGroup: false, subresource: false, namespace: false, name: false });
  const discovery = useQuery({ queryKey: ["resource-discovery", "all"], queryFn: () => api.resourceDiscovery(), staleTime: 60_000 });
  const namespaces = useQuery({ queryKey: ["namespaces", "access-review"], queryFn: () => api.namespaces({ pageSize: 200 }), staleTime: 60_000, retry: false });
  const review = useMutation({ mutationFn: () => api.accessReview({ ...form, apiGroup: form.apiGroup || undefined, subresource: form.subresource || undefined, namespace: form.namespace || undefined, name: form.name || undefined }) });

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
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!form.verb.trim() || !form.resource.trim()) return;
    review.mutate();
  };
  const applyPreset = (preset: typeof presets[number]) => {
    setForm({ ...preset, name: "" });
    resetManual();
    review.reset();
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
  const result = review.data;
  const clusterScoped = Boolean(selectedDescriptor && !selectedDescriptor.namespaced);
  const resourceHint = discovery.isLoading ? "正在读取 Kubernetes API Discovery…" : discovery.isError ? "资源列表加载失败，可手动输入 Resource 与 API Group。" : undefined;
  const namespaceHint = clusterScoped ? "当前资源为集群级，Namespace 将留空。" : namespaces.isError ? "命名空间列表加载失败，可手动输入。" : undefined;

  return (
    <>
      <PageHeader title="权限检查" breadcrumb="治理与策略 / 权限检查" description="使用 Kubernetes SubjectAccessReview 解释当前登录身份能否执行某个操作，并区分 Dosh 只读模式与 RBAC 拒绝。" />
      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,380px)_minmax(0,1fr)]">
        <Card className="min-w-0">
          <CardHeader><CardTitle className="flex items-center gap-2"><KeyRound className="h-4 w-4" />检查条件</CardTitle><CardDescription>选择资源、子资源和对象；列表没有可用数据时会自动切换为手动输入。</CardDescription></CardHeader>
          <CardContent className="space-y-4">
            <div>
              <p className="mb-2 text-xs font-medium text-muted-foreground">常用场景</p>
              <div className="flex flex-wrap gap-2">{presets.map((preset) => <Button key={preset.label} type="button" variant="outline" size="sm" onClick={() => applyPreset(preset)}>{preset.label}</Button>)}</div>
            </div>
            <form className="space-y-4" onSubmit={submit}>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <div className="space-y-1.5"><Label htmlFor="review-verb">Verb</Label><Select value={form.verb} onValueChange={(value) => setForm((current) => ({ ...current, verb: value }))}><SelectTrigger id="review-verb" aria-label="Verb"><SelectValue /></SelectTrigger><SelectContent>{verbs.map((verb) => <SelectItem key={verb} value={verb}>{verb}</SelectItem>)}</SelectContent></Select></div>
                <LinkedField
                  id="review-resource"
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
                  id="review-group"
                  label="API Group"
                  value={form.apiGroup}
                  options={groupOptions}
                  onChange={selectGroup}
                  manual={manual.apiGroup}
                  onManualChange={(value) => setManualField("apiGroup", value)}
                  placeholder="选择 API Group"
                  manualPlaceholder="apps；core 留空"
                  loading={discovery.isLoading}
                />
                <LinkedField
                  id="review-subresource"
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
                  id="review-namespace"
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
                  id="review-name"
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
