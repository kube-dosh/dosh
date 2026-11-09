import { useDeferredValue, useMemo, useRef, useState, type ChangeEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import CodeMirror from "@uiw/react-codemirror";
import { yaml as yamlLanguage } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import {
  CheckCircle2, Copy, Download, FileCode2, FolderInput, Info, ListChecks, Loader2,
  RotateCcw, ShieldCheck, TriangleAlert, Upload, WandSparkles, XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { YAMLResource } from "@/api/types";
import { CopyButton } from "@/components/data/copy-button";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useCapabilities } from "@/features/auth/use-session";
import { formatYAMLDocuments, inspectYAMLDocuments, setNamespaceOnYAMLDocuments, type ParsedYAMLDocument } from "@/lib/yaml-documents";
import { copyText } from "@/lib/clipboard";
import { useTheme } from "@/lib/theme";

type TemplateID = "deployment" | "batch" | "service" | "configmap" | "secret" | "pvc" | "ingress";

type YAMLTemplate = {
  id: TemplateID;
  label: string;
  kind: string;
  description: string;
  render: (namespace: string) => string;
};

type ExecutionItem = {
  target: ParsedYAMLDocument;
  status: "success" | "error";
  resource?: YAMLResource;
  error?: string;
};

type ExecutionResult = {
  dryRun: boolean;
  items: ExecutionItem[];
  total: number;
  stopped: boolean;
};

type ExecutionProgress = {
  dryRun: boolean;
  current: number;
  total: number;
  kind: string;
  name: string;
};

const deploymentManifest = (namespace: string) => `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: ${namespace}
  labels:
    app.kubernetes.io/name: web
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: web
  template:
    metadata:
      labels:
        app.kubernetes.io/name: web
    spec:
      containers:
        - name: web
          image: nginx:alpine
          ports:
            - name: http
              containerPort: 80
`;

const serviceManifest = (namespace: string) => `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: ${namespace}
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: web
  ports:
    - name: http
      port: 80
      targetPort: http
`;

const configMapManifest = (namespace: string) => `apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
  namespace: ${namespace}
data:
  APP_MODE: production
  LOG_LEVEL: info
`;

const templates: YAMLTemplate[] = [
  {
    id: "deployment",
    label: "Deployment",
    kind: "无状态工作负载",
    description: "双副本 Deployment 与容器端口示例",
    render: deploymentManifest,
  },
  {
    id: "batch",
    label: "组合示例",
    kind: "多资源",
    description: "ConfigMap、Deployment、Service 三个文档顺序应用",
    render: (namespace) => `${configMapManifest(namespace)}---\n${deploymentManifest(namespace)}---\n${serviceManifest(namespace)}`,
  },
  {
    id: "service",
    label: "Service",
    kind: "服务",
    description: "ClusterIP Service 与工作负载选择器",
    render: serviceManifest,
  },
  {
    id: "configmap",
    label: "ConfigMap",
    kind: "配置",
    description: "保存非敏感文本配置",
    render: configMapManifest,
  },
  {
    id: "secret",
    label: "Secret",
    kind: "密钥",
    description: "使用 stringData 提交明文，由 API Server 负责编码",
    render: (namespace) => `apiVersion: v1
kind: Secret
metadata:
  name: web-secret
  namespace: ${namespace}
type: Opaque
stringData:
  username: replace-me
  password: replace-me
`,
  },
  {
    id: "pvc",
    label: "PVC",
    kind: "存储",
    description: "1 GiB ReadWriteOnce 持久卷声明",
    render: (namespace) => `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: web-data
  namespace: ${namespace}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
`,
  },
  {
    id: "ingress",
    label: "Ingress",
    kind: "路由",
    description: "将域名流量转发至同名 Service",
    render: (namespace) => `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web
  namespace: ${namespace}
spec:
  ingressClassName: nginx
  rules:
    - host: web.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: web
                port:
                  number: 80
`,
  },
];

const templateByID = Object.fromEntries(templates.map((template) => [template.id, template])) as Record<TemplateID, YAMLTemplate>;

function templateYAML(id: TemplateID, namespace: string): string {
  return templateByID[id].render(namespace || "default");
}

function hasYAMLApplyPermission(capabilities: ReturnType<typeof useCapabilities>["data"]): boolean {
  if (!capabilities) return false;
  if (capabilities.editYAML) return true;
  return Object.values(capabilities.resourceActions ?? {}).some((resource) => resource.create || resource.update);
}

function ResourceResultLink({ resource }: { resource: YAMLResource }) {
  const className = "text-sm font-medium text-primary underline underline-offset-4";
  const namespace = resource.namespace ?? "";
  switch (resource.kind) {
    case "Deployment":
    case "StatefulSet":
    case "DaemonSet":
    case "Job":
    case "CronJob":
      return <Link className={className} to="/workloads/$kind/$namespace/$name" params={{ kind: resource.kind.toLowerCase(), namespace, name: resource.name }} search={{}}>查看工作负载</Link>;
    case "Pod":
      return <Link className={className} to="/pods/$namespace/$name" params={{ namespace, name: resource.name }} search={{}}>查看 Pod</Link>;
    case "Service":
      return <Link className={className} to="/network/services/$namespace/$name" params={{ namespace, name: resource.name }} search={{}}>查看 Service</Link>;
    case "Ingress":
      return <Link className={className} to="/network/ingresses/$namespace/$name" params={{ namespace, name: resource.name }} search={{}}>查看 Ingress</Link>;
    case "PersistentVolumeClaim":
      return <Link className={className} to="/storage/claims/$namespace/$name" params={{ namespace, name: resource.name }} search={{}}>查看 PVC</Link>;
    case "PersistentVolume":
      return <Link className={className} to="/storage/volumes/$name" params={{ name: resource.name }} search={{}}>查看 PV</Link>;
    case "StorageClass":
      return <Link className={className} to="/storage/classes/$name" params={{ name: resource.name }} search={{}}>查看 StorageClass</Link>;
    case "ConfigMap":
    case "Secret":
      return <Link className={className} to="/configuration" search={{ namespace, tab: resource.kind === "Secret" ? "secrets" : "configmaps" }}>查看配置列表</Link>;
    default:
      return null;
  }
}

function resourceIdentity(resource: YAMLResource): string {
  return `${resource.kind} ${resource.namespace ? `${resource.namespace}/` : ""}${resource.name}`;
}

export function YAMLCreatePage() {
  const { theme } = useTheme();
  const queryClient = useQueryClient();
  const capabilities = useCapabilities().data;
  const canApply = hasYAMLApplyPermission(capabilities);
  const [templateID, setTemplateID] = useState<TemplateID>("deployment");
  const [namespace, setNamespace] = useState("default");
  const [value, setValue] = useState(() => templateYAML("deployment", "default"));
  const deferredValue = useDeferredValue(value);
  const inspection = useMemo(() => inspectYAMLDocuments(deferredValue), [deferredValue]);
  const [execution, setExecution] = useState<ExecutionResult | null>(null);
  const [progress, setProgress] = useState<ExecutionProgress | null>(null);
  const [resultDialog, setResultDialog] = useState<YAMLResource | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const apply = useMutation({
    mutationFn: async (dryRun: boolean): Promise<ExecutionResult> => {
      const current = inspectYAMLDocuments(value);
      if (current.errors.length > 0) throw new Error(current.errors[0]);
      const items: ExecutionItem[] = [];
      for (let index = 0; index < current.documents.length; index += 1) {
        const document = current.documents[index];
        setProgress({ dryRun, current: index + 1, total: current.documents.length, kind: document.kind, name: document.name });
        try {
          const resource = await api.applyYAML(document.source, undefined, dryRun);
          items.push({ target: document, status: "success", resource });
        } catch (error) {
          items.push({ target: document, status: "error", error: error instanceof Error ? error.message : "YAML 请求失败" });
          return { dryRun, items, total: current.documents.length, stopped: true };
        }
      }
      return { dryRun, items, total: current.documents.length, stopped: false };
    },
    onMutate: () => {
      setExecution(null);
    },
    onSuccess: async (result) => {
      setExecution(result);
      const successes = result.items.filter((item) => item.status === "success").length;
      if (result.dryRun) {
        if (result.stopped) toast.error(`Dry Run 在第 ${result.items.length} 个文档失败`);
        else toast.success(`${successes} 个文档 Dry Run 校验通过`);
        return;
      }
      if (successes > 0) await queryClient.invalidateQueries();
      if (result.stopped) toast.error(`应用在第 ${result.items.length} 个文档停止`);
      else toast.success(`${successes} 个资源已应用`);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "YAML 应用失败"),
    onSettled: () => setProgress(null),
  });

  const replaceValue = (next: string) => {
    setValue(next);
    setExecution(null);
    if (apply.isError) apply.reset();
  };

  const selectTemplate = (next: TemplateID) => {
    setTemplateID(next);
    replaceValue(templateYAML(next, namespace));
  };

  const changeNamespace = (next: string) => {
    const previousTemplate = templateYAML(templateID, namespace);
    setNamespace(next);
    if (value === previousTemplate) replaceValue(templateYAML(templateID, next));
    else setExecution(null);
  };

  const restoreTemplate = () => replaceValue(templateYAML(templateID, namespace));

  const writeNamespace = () => {
    const result = setNamespaceOnYAMLDocuments(value, namespace);
    if (result.errors.length > 0) {
      toast.error(result.errors[0]);
      return;
    }
    replaceValue(result.yaml);
    const skipped = result.skipped > 0 ? `，跳过 ${result.skipped} 个集群级资源` : "";
    toast.success(`已写入 ${result.updated} 个文档的命名空间${skipped}`);
  };

  const formatDocument = () => {
    const result = formatYAMLDocuments(value);
    if (result.errors.length > 0) {
      toast.error(result.errors[0]);
      return;
    }
    replaceValue(result.yaml);
    toast.success("YAML 格式已整理");
  };

  const copyDocument = async () => {
    try {
      await copyText(value);
      toast.success("YAML 已复制");
    } catch {
      toast.error("复制失败，请手动选择内容复制");
    }
  };

  const downloadDocument = () => {
    const first = inspection.documents[0];
    const basename = first ? `${first.kind.toLowerCase()}-${first.name}` : "dosh-resource";
    const url = URL.createObjectURL(new Blob([value], { type: "application/yaml;charset=utf-8" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${basename}.yaml`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  };

  const importDocument = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    if (file.size > 1024 * 1024) {
      toast.error("YAML 文件不能超过 1 MiB");
      return;
    }
    try {
      replaceValue(await file.text());
      toast.success(`已导入 ${file.name}`);
    } catch {
      toast.error("读取 YAML 文件失败");
    }
  };

  const hasParseErrors = inspection.errors.length > 0;
  const submitDisabled = !canApply || hasParseErrors || inspection.documents.length === 0 || apply.isPending || deferredValue !== value;
  const namespacedMismatch = inspection.documents.filter((document) => !document.clusterScoped && document.namespace !== namespace).length;

  return (
    <>
      <PageHeader
        title="YAML 创建"
        breadcrumb="总览 / YAML 创建"
        description="粘贴一个或多个 Kubernetes YAML 文档，先执行 API Server Dry Run，再按文档顺序创建或合并资源。多个资源不是原子事务。"
        actions={<Button variant="outline" onClick={restoreTemplate} disabled={apply.isPending}><RotateCcw className="h-4 w-4" />恢复模板</Button>}
      />

      {!canApply ? (
        <div className="mb-4 flex items-start gap-2 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-sm text-warning" role="alert">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <div><p className="font-medium">当前身份没有 YAML Apply 权限</p><p className="mt-0.5 text-xs">请联系管理员授予目标资源的 create（新建）或 update / patch（更新）权限。后端会按每个文档中的资源类型、命名空间和资源是否存在执行最终 RBAC 检查。</p></div>
        </div>
      ) : null}

      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="min-w-0 space-y-4">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2"><FileCode2 className="h-4 w-4 text-primary" />资源模板</CardTitle>
              <CardDescription>选择模板后可直接修改 YAML；模板仅用于起始内容，不会限制资源字段。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap gap-2" role="group" aria-label="YAML 模板">
                {templates.map((template) => (
                  <Button key={template.id} type="button" size="sm" variant={template.id === templateID ? "default" : "outline"} aria-pressed={template.id === templateID} disabled={apply.isPending} onClick={() => selectTemplate(template.id)} title={template.description}>
                    {template.label}
                  </Button>
                ))}
              </div>
              <div className="grid min-w-0 gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
                <div className="min-w-0 rounded-md border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                  <span className="font-medium text-foreground">{templateByID[templateID].label}</span> · {templateByID[templateID].kind} · {templateByID[templateID].description}
                </div>
                <div className="flex min-w-0 flex-col gap-1.5 sm:w-[19rem]">
                  <label className="text-xs font-medium text-muted-foreground">目标命名空间</label>
                  <div className="flex min-w-0 gap-2">
                    <NamespaceSelect value={namespace} onChange={changeNamespace} allowAll={false} ariaLabel="目标命名空间" className="min-w-0 flex-1" />
                    <Button type="button" variant="outline" size="default" className="shrink-0 px-3" onClick={writeNamespace} disabled={apply.isPending || inspection.documents.length === 0 || hasParseErrors} title="将命名空间写入所有命名空间级文档">
                      <FolderInput className="h-4 w-4" />写入
                    </Button>
                  </div>
                </div>
              </div>
              <p className="text-xs text-muted-foreground">切换模板或命名空间时，仅在编辑器内容未修改的情况下重新生成模板。点击“写入”可批量覆盖所有命名空间级文档的 <span className="font-mono">metadata.namespace</span>，集群级资源保持不变。</p>
            </CardContent>
          </Card>

          <Card className="min-w-0 overflow-hidden">
            <CardHeader className="pb-3">
              <div className="flex min-w-0 flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <CardTitle>资源清单</CardTitle>
                  <CardDescription>支持多个 YAML 文档，按编辑顺序逐个提交。</CardDescription>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <input ref={fileInputRef} type="file" accept=".yaml,.yml,.txt,application/yaml,text/yaml,text/plain" className="hidden" onChange={(event) => void importDocument(event)} />
                  <Button type="button" variant="outline" size="sm" onClick={() => fileInputRef.current?.click()} disabled={apply.isPending}><Upload className="h-3.5 w-3.5" />导入</Button>
                  <Button type="button" variant="outline" size="sm" onClick={formatDocument} disabled={apply.isPending || hasParseErrors}><WandSparkles className="h-3.5 w-3.5" />格式化</Button>
                  <Button type="button" variant="outline" size="sm" onClick={() => void copyDocument()} disabled={!value}><Copy className="h-3.5 w-3.5" />复制</Button>
                  <Button type="button" variant="outline" size="sm" onClick={downloadDocument} disabled={!value}><Download className="h-3.5 w-3.5" />导出</Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="min-w-0 space-y-3">
              <div className={`rounded-md border px-3 py-2 ${hasParseErrors ? "border-destructive/30 bg-destructive/5" : "bg-muted/30"}`}>
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <ListChecks className="h-3.5 w-3.5 text-primary" />
                  <span className="font-medium">{inspection.documents.length} 个有效文档</span>
                  <span className="text-muted-foreground">· {value.split("\n").length} 行 · {(new TextEncoder().encode(value).length / 1024).toFixed(1)} KiB</span>
                  {inspection.documents.length > 1 ? <Badge variant="secondary">顺序应用 · 非原子</Badge> : null}
                  {!hasParseErrors && namespacedMismatch > 0 ? <Badge variant="warning">{namespacedMismatch} 个文档命名空间与目标不同</Badge> : null}
                </div>
                {hasParseErrors ? (
                  <ul className="mt-2 list-disc space-y-1 pl-5 text-xs text-destructive-strong" role="alert">
                    {inspection.errors.map((error) => <li key={error}>{error}</li>)}
                  </ul>
                ) : (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {inspection.documents.slice(0, 8).map((document) => (
                      <Badge key={`${document.index}-${document.source.slice(0, 24)}`} variant="outline" className="max-w-full font-mono text-[10px]">
                        <span className="truncate">{document.kind} {document.namespace ? `${document.namespace}/` : ""}{document.name}</span>
                      </Badge>
                    ))}
                    {inspection.documents.length > 8 ? <Badge variant="secondary">+{inspection.documents.length - 8}</Badge> : null}
                  </div>
                )}
              </div>

              <div className="min-w-0 overflow-hidden rounded-md border">
                <CodeMirror
                  value={value}
                  onChange={replaceValue}
                  extensions={[yamlLanguage(), EditorView.lineWrapping]}
                  editable={!apply.isPending}
                  height="52vh"
                  theme={theme}
                  basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true }}
                  aria-label="YAML 资源清单"
                />
              </div>

              {progress ? (
                <div className="flex items-center gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs" role="status" aria-label="YAML 执行进度">
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
                  正在{progress.dryRun ? "校验" : "应用"} {progress.current}/{progress.total}：<span className="font-mono">{progress.kind} {progress.name}</span>
                </div>
              ) : null}

              {apply.isError ? <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{apply.error instanceof Error ? apply.error.message : "YAML 应用失败"}</div> : null}

              <div className="flex flex-wrap items-center justify-end gap-2">
                <Button variant="outline" onClick={() => apply.mutate(true)} disabled={submitDisabled}>
                  {apply.isPending && apply.variables === true ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}Dry Run
                </Button>
                <Button onClick={() => apply.mutate(false)} disabled={submitDisabled}>
                  {apply.isPending && apply.variables === false ? <Loader2 className="h-4 w-4 animate-spin" /> : <FileCode2 className="h-4 w-4" />}{inspection.documents.length > 1 ? `创建 / 应用 ${inspection.documents.length} 个资源` : "创建 / 应用"}
                </Button>
              </div>
            </CardContent>
          </Card>

          {execution ? (
            <Card role="status" aria-label={execution.dryRun ? "Dry Run 校验结果" : "YAML 应用结果"} className={execution.stopped ? "border-warning/35" : "border-success/30"}>
              <CardHeader className="pb-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <CardTitle className="flex items-center gap-2">
                    {execution.stopped ? <TriangleAlert className="h-4 w-4 text-warning" /> : <CheckCircle2 className="h-4 w-4 text-success" />}
                    {execution.dryRun ? "Dry Run 结果" : "应用结果"}
                  </CardTitle>
                  <Badge variant={execution.stopped ? "warning" : "success"}>{execution.items.filter((item) => item.status === "success").length}/{execution.total} 成功</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                {execution.items.map((item, index) => (
                  <div key={`${index}-${item.target.kind}-${item.target.name}`} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2">
                    <div className="flex min-w-0 items-start gap-2">
                      {item.status === "success" ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" /> : <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />}
                      <div className="min-w-0">
                        <p className="truncate font-mono text-xs">{item.target.kind} {item.target.namespace ? `${item.target.namespace}/` : ""}{item.target.name}</p>
                        {item.error ? <p className="mt-0.5 break-words text-xs text-destructive-strong">{item.error}</p> : null}
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-3">
                      {item.resource ? <Button type="button" variant="ghost" size="sm" onClick={() => setResultDialog(item.resource ?? null)}>返回 YAML</Button> : null}
                      {!execution.dryRun && item.resource ? <ResourceResultLink resource={item.resource} /> : null}
                    </div>
                  </div>
                ))}
                {execution.stopped ? (
                  <div className="rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
                    已在失败文档处停止，剩余 {execution.total - execution.items.length} 个文档未执行。此前已成功应用的资源不会自动回滚。
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ) : null}
        </div>

        <aside className="min-w-0 space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2"><Info className="h-4 w-4 text-primary" />执行说明</CardTitle>
              <CardDescription>与应用服务器的交互顺序</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-xs text-muted-foreground">
              <div className="flex gap-2"><span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-primary/10 font-semibold text-primary">1</span><p>在浏览器中解析文档并检查名称、命名空间和重复目标。</p></div>
              <div className="flex gap-2"><span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-primary/10 font-semibold text-primary">2</span><p>先检查资源是否存在，再按 create 或 update / patch 逐文档授权，并记录独立审计事件。</p></div>
              <div className="flex gap-2"><span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-primary/10 font-semibold text-primary">3</span><p>Dry Run 模式不会写入；正式应用前，后端仍会为每个文档自动执行一次 API Server Dry Run。</p></div>
              <div className="flex gap-2"><span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-primary/10 font-semibold text-primary">4</span><p>按文档顺序 Server-Side Apply，字段管理器为 <span className="font-mono">dosh</span>。</p></div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-primary" />安全边界</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-xs text-muted-foreground">
              <p>多文档顺序提交不是原子事务；某个文档失败时后续文档停止，已成功资源不会回滚。</p>
              <p>同名资源存在时会按 Server-Side Apply 合并更新，请确认字段所有权和变更范围。</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>支持范围</CardTitle>
              <CardDescription>当前版本允许通过 YAML 管理的资源</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-xs">
              <div><p className="mb-1.5 font-medium">命名空间级</p><div className="flex flex-wrap gap-1.5">{["Pod", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Service", "Ingress", "ConfigMap", "Secret", "PVC"].map((kind) => <Badge key={kind} variant="outline" className="font-mono">{kind}</Badge>)}</div></div>
              <div><p className="mb-1.5 font-medium">集群级</p><div className="flex flex-wrap gap-1.5">{["StorageClass", "PV"].map((kind) => <Badge key={kind} variant="outline" className="font-mono">{kind}</Badge>)}</div></div>
              <p className="border-t pt-3 text-muted-foreground">前端会预检 API 版本、资源类型、命名空间和 1 MiB 大小上限；后端仍按白名单执行最终校验。暂不支持 Namespace、Node 和 CRD，需要时请使用 Kubernetes 原生工具或扩展后续 API Discovery 能力。</p>
            </CardContent>
          </Card>
        </aside>
      </div>

      <Dialog open={Boolean(resultDialog)} onOpenChange={(open) => { if (!open) setResultDialog(null); }}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <div className="flex flex-wrap items-start justify-between gap-3 pr-6">
              <div>
                <DialogTitle>{resultDialog ? resourceIdentity(resultDialog) : "API Server 返回 YAML"}</DialogTitle>
                <DialogDescription>API Server 规范化后的响应。</DialogDescription>
              </div>
              {resultDialog ? <CopyButton value={resultDialog.yaml} label="复制 YAML" /> : null}
            </div>
          </DialogHeader>
          <pre className="soft-wrap max-h-[65vh] max-w-full overflow-auto whitespace-pre-wrap rounded-md border bg-muted/20 p-3 font-mono text-xs">{resultDialog?.yaml || ""}</pre>
        </DialogContent>
      </Dialog>
    </>
  );
}
