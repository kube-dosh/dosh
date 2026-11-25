import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import CodeMirror from "@uiw/react-codemirror";
import { yaml as yamlLanguage } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import { AlertTriangle, CheckCircle2, ChevronDown, FileCode2, Loader2, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { ContainerRegistry, WorkloadPodTemplateResult } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { ImageReferencePicker } from "@/features/registries/image-reference-picker";
import {
  addImagePullSecret,
  arrayValue,
  asRecord,
  cloneTemplateDocument,
  containerImage,
  containerName,
  containerRegistrySecret,
  formatLineValues,
  parseLineValues,
  parseTemplateYAML,
  serializeTemplateYAML,
  stringMap,
  stringValue,
  templateContainers,
  templateMetadata,
  templateSpec,
  type PodContainerDocument,
  type PodTemplateDocument,
} from "@/features/workloads/pod-template-document";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

type EditorMode = "form" | "yaml";

export function WorkloadPodTemplateEditor({ open, onOpenChange, kind, namespace, name, focusContainer, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  kind: string;
  namespace: string;
  name: string;
  focusContainer?: string;
  onSaved: () => Promise<unknown> | unknown;
}) {
  const queryClient = useQueryClient();
  const { theme } = useTheme();
  const [document, setDocument] = useState<PodTemplateDocument | null>(null);
  const [mode, setMode] = useState<EditorMode>("form");
  const [yamlText, setYamlText] = useState("");
  const [changeCause, setChangeCause] = useState("");
  const [acknowledgeRisks, setAcknowledgeRisks] = useState(false);
  const [conflict, setConflict] = useState("");
  const [validation, setValidation] = useState<WorkloadPodTemplateResult | null>(null);
  const [serverResourceVersion, setServerResourceVersion] = useState("");
  const initializedRef = useRef(false);
  const query = useQuery({
    queryKey: ["workload-pod-template", kind, namespace, name],
    queryFn: () => api.workloadPodTemplate(kind, namespace, name),
    enabled: open,
    staleTime: 0,
  });
  const registries = useQuery({
    queryKey: ["container-registries"],
    queryFn: api.containerRegistries,
    enabled: open,
    staleTime: 30_000,
  });

  useEffect(() => {
    if (!open) {
      initializedRef.current = false;
      return;
    }
    if (!query.data || initializedRef.current) return;
    const next = cloneTemplateDocument(query.data.template);
    setDocument(next);
    setYamlText(serializeTemplateYAML(next));
    setServerResourceVersion(query.data.resourceVersion ?? "");
    setMode("form");
    setChangeCause("");
    setAcknowledgeRisks(false);
    setConflict("");
    setValidation(null);
    initializedRef.current = true;
  }, [open, query.data]);

  useEffect(() => {
    if (!open || !focusContainer) return;
    window.setTimeout(() => {
      window.document.getElementById(`template-container-${CSS.escape(focusContainer)}`)?.scrollIntoView({ block: "center", behavior: "smooth" });
    }, 100);
  }, [focusContainer, mode, open]);

  const documentForSave = (): PodTemplateDocument => {
    if (mode === "yaml") return parseTemplateYAML(yamlText);
    if (!document) throw new Error("模板尚未加载");
    return document;
  };

  const save = useMutation({
    mutationFn: async (dryRun: boolean) => {
      if (!query.data) throw new Error("模板尚未加载");
      const desired = documentForSave();
      const validated = await api.updateWorkloadPodTemplate(kind, namespace, name, {
        resourceVersion: serverResourceVersion,
        template: desired,
        changeCause: changeCause.trim() || `Dosh: 更新 ${query.data.kind} Pod 模板`,
        source: mode,
        dryRun: true,
        acknowledgeRisks: true,
      });
      setValidation(validated);
      if (dryRun) return { result: validated, dryRun };
      if (validated.newWarnings?.length && !acknowledgeRisks) {
        toast.warning("本次修改新增了高风险配置，请确认风险后再次保存");
        return { result: validated, dryRun };
      }
      const result = await api.updateWorkloadPodTemplate(kind, namespace, name, {
        resourceVersion: serverResourceVersion,
        template: desired,
        changeCause: changeCause.trim() || `Dosh: 更新 ${query.data.kind} Pod 模板`,
        source: mode,
        dryRun: false,
        acknowledgeRisks,
      });
      return { result, dryRun };
    },
    onSuccess: async ({ result, dryRun }) => {
      if (dryRun) {
        toast.success(result.changed ? `校验通过，检测到 ${result.diff.length} 处变更` : "校验通过，模板没有实际变化");
        return;
      }
      if (result.newWarnings?.length && !acknowledgeRisks) return;
      toast.success(result.changed ? "Pod 模板已保存，发布变更已触发" : "Pod 模板没有实际变化");
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      await queryClient.invalidateQueries({ queryKey: ["workload-pod-template", kind, namespace, name] });
      await queryClient.invalidateQueries({ queryKey: ["workload-container-inventory", kind, namespace, name] });
      await onSaved();
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        setConflict("Pod 模板已被其他操作更新。当前草稿未丢失，已刷新服务端版本；请核对差异后再次保存。");
        try {
          const latest = await queryClient.fetchQuery({
            queryKey: ["workload-pod-template", kind, namespace, name],
            queryFn: () => api.workloadPodTemplate(kind, namespace, name),
            staleTime: 0,
          });
          setServerResourceVersion(latest.resourceVersion ?? "");
        } catch {
          setConflict("Pod 模板已被其他操作更新，且最新版本读取失败。你的草稿仍保留，请关闭后重新打开并核对。");
        }
        return;
      }
      toast.error(error instanceof Error ? error.message : "保存 Pod 模板失败");
    },
  });

  const switchMode = (next: EditorMode) => {
    if (next === mode) return;
    if (next === "yaml") {
      if (document) setYamlText(serializeTemplateYAML(document));
      setMode("yaml");
      return;
    }
    try {
      const parsed = parseTemplateYAML(yamlText);
      setDocument(parsed);
      setMode("form");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "YAML 解析失败");
    }
  };

  const dryRun = useMutation({
    mutationFn: async () => {
      if (!query.data) throw new Error("模板尚未加载");
      return api.updateWorkloadPodTemplate(kind, namespace, name, {
        resourceVersion: serverResourceVersion,
        template: documentForSave(),
        changeCause: changeCause.trim() || "Dosh: 校验 Pod 模板",
        source: mode,
        dryRun: true,
        acknowledgeRisks: true,
      });
    },
    onSuccess: (result) => {
      setValidation(result);
      toast.success(result.changed ? `校验通过，检测到 ${result.diff.length} 处变更` : "校验通过，模板没有实际变化");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "模板校验失败"),
  });

  const existingWarnings = query.data?.warnings ?? [];
  const newWarnings = validation?.newWarnings ?? [];
  const warningCount = newWarnings.length;
  const busy = save.isPending || dryRun.isPending;
  const editable = query.data?.editable ?? false;

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next); }}>
      <DialogContent className="flex h-[100dvh] w-screen max-w-none flex-col gap-0 overflow-hidden p-0 sm:h-[min(94dvh,980px)] sm:w-[calc(100%-2rem)] sm:max-w-7xl sm:rounded-lg">
        <DialogHeader className="shrink-0 border-b px-4 py-4 pr-12 sm:px-6">
          <DialogTitle className="flex min-w-0 items-center gap-2"><FileCode2 className="h-5 w-5 shrink-0 text-primary" /><span className="min-w-0 break-all">Pod 模板 · {name}</span></DialogTitle>
          <DialogDescription className="break-words">{namespace} · {query.data?.kind ?? kind} · 表单和 YAML 共用同一份模板文档，保存时一次原子更新。</DialogDescription>
        </DialogHeader>

        {!query.isError && (!query.data || query.isLoading) ? <div className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在读取 Pod 模板</div> : null}
        {query.isError ? <div role="alert" className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-sm text-destructive-strong"><AlertTriangle className="h-5 w-5" /><p className="max-w-lg break-words">读取 Pod 模板失败：{query.error instanceof Error ? query.error.message : "未知错误"}</p><Button type="button" variant="outline" size="sm" onClick={() => void query.refetch()} disabled={query.isFetching}>{query.isFetching ? <Loader2 className="h-4 w-4 animate-spin" /> : null}重新读取</Button></div> : null}
        {query.isError ? <div className="flex flex-1 items-center justify-center px-6 text-sm text-destructive-strong">{query.error.message}</div> : null}
        {query.data && document ? <>
          <div className="shrink-0 border-b px-4 py-3 sm:px-6">
            <div className="flex min-w-0 flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <Tabs value={mode} onValueChange={(value) => switchMode(value as EditorMode)}>
                <TabsList><TabsTrigger value="form">结构化表单</TabsTrigger><TabsTrigger value="yaml">完整 YAML</TabsTrigger></TabsList>
              </Tabs>
              <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="outline">资源版本 {query.data.resourceVersion || "—"}</Badge>
                {query.data.protectedLabels?.length ? <Badge variant="secondary">{query.data.protectedLabels.length} 个锁定标签</Badge> : null}
                <Badge variant={editable ? "success" : "warning"}>{editable ? "可编辑" : "只读"}</Badge>
              </div>
            </div>
          </div>

          {conflict ? <div role="alert" className="flex shrink-0 items-start gap-2 border-b border-warning/40 bg-warning/10 px-4 py-2.5 text-sm text-warning sm:px-6"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{conflict}</span></div> : null}
          {query.data.restrictions?.length ? <div className="shrink-0 border-b px-4 py-2 text-xs text-muted-foreground sm:px-6">{query.data.restrictions.join("；")}</div> : null}

          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
            {mode === "form" ? <TemplateForm document={document} setDocument={(next) => { setDocument(next); setValidation(null); }} registries={registries.data ?? []} protectedLabels={query.data.protectedLabels ?? []} focusContainer={focusContainer} editable={editable} /> : (
              <div className="h-full min-h-[30rem] p-3 sm:p-4">
                <CodeMirror
                  value={yamlText}
                  onChange={(value) => { setYamlText(value); setValidation(null); }}
                  extensions={[yamlLanguage(), EditorView.lineWrapping]}
                  editable={editable && !busy}
                  height="100%"
                  minHeight="30rem"
                  theme={theme}
                  basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true, bracketMatching: true }}
                  aria-label="Pod 模板 YAML 编辑器"
                  className="[&_.cm-editor]:min-h-[30rem] [&_.cm-editor]:max-w-full"
                />
              </div>
            )}
          </div>

          {existingWarnings.length ? <div className="shrink-0 border-t bg-warning/5 px-4 py-2 text-xs leading-5 text-warning sm:px-6"><span className="font-medium">模板已有 {existingWarnings.length} 项高风险配置（仅提示，不阻塞本次保存）：</span> <span className="break-words">{existingWarnings.slice(0, 3).join("；")}{existingWarnings.length > 3 ? "；…" : ""}</span></div> : null}
          <DialogFooter className="shrink-0 flex-col items-stretch gap-3 border-t bg-background px-4 py-3 sm:flex-row sm:items-center sm:px-6">
            <div className="min-w-0 flex-1 space-y-2">
              <Input value={changeCause} onChange={(event) => setChangeCause(event.target.value)} placeholder="变更说明（写入 kubernetes.io/change-cause）" maxLength={256} disabled={!editable || busy} />
              {validation ? <p className="break-words text-xs text-muted-foreground">{validation.rollout.message}{validation.diff.length ? ` · ${validation.diff.length} 处变更` : ""}</p> : null}
            </div>
            {newWarnings.length ? <label className="flex max-w-xl items-start gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning"><input type="checkbox" className="mt-0.5 h-4 w-4 shrink-0" checked={acknowledgeRisks} onChange={(event) => setAcknowledgeRisks(event.target.checked)} /><span className="min-w-0 break-words">本次修改新增 {warningCount} 项高风险配置，我已确认风险。<span className="mt-1 block text-[11px] opacity-80">{newWarnings.slice(0, 2).join("；")}{newWarnings.length > 2 ? "；…" : ""}</span></span></label> : null}
            <div className="flex shrink-0 flex-wrap justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>取消</Button>
              <Button type="button" variant="outline" onClick={() => dryRun.mutate()} disabled={!editable || busy}><CheckCircle2 className="h-4 w-4" />校验</Button>
              <Button type="button" onClick={() => save.mutate(false)} disabled={!editable || busy || (newWarnings.length > 0 && !acknowledgeRisks)}>
                {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}保存并发布
              </Button>
            </div>
          </DialogFooter>
        </> : null}
      </DialogContent>
    </Dialog>
  );
}

function TemplateForm({ document, setDocument, registries, protectedLabels, focusContainer, editable }: {
  document: PodTemplateDocument;
  setDocument: (document: PodTemplateDocument) => void;
  registries: ContainerRegistry[];
  protectedLabels: string[];
  focusContainer?: string;
  editable: boolean;
}) {
  const mutate = (callback: (next: PodTemplateDocument) => void) => {
    const next = cloneTemplateDocument(document);
    callback(next);
    setDocument(next);
  };
  const spec = templateSpec(document);
  const metadata = templateMetadata(document);
  const labelValues = stringMap(metadata.labels);
  const annotations = stringMap(metadata.annotations);
  const imagePullSecrets = arrayValue(spec.imagePullSecrets).map((item) => stringValue(asRecord(item).name)).filter(Boolean);

  return (
    <div className="space-y-4 p-4 sm:p-6">
      <TemplateSection title="Pod 元数据" description="Pod 标签会参与 Service 和 PDB 选择；工作负载 Selector 必需的标签已锁定。">
        <div className="grid min-w-0 gap-4 lg:grid-cols-2">
          <KeyValueEditor id="pod-labels" label="Labels" values={labelValues} protectedKeys={protectedLabels} disabled={!editable} onChange={(values) => mutate((next) => { const target = templateMetadata(next); if (Object.keys(values).length) target.labels = values; else delete target.labels; })} />
          <KeyValueEditor id="pod-annotations" label="Annotations" values={annotations} disabled={!editable} onChange={(values) => mutate((next) => { const target = templateMetadata(next); if (Object.keys(values).length) target.annotations = values; else delete target.annotations; })} />
        </div>
      </TemplateSection>

      <TemplateSection title="容器" description="业务容器全部列在这里；支持多容器同时编辑，保存后一次发布。">
        <ContainerList document={document} init={false} registries={registries} focusContainer={focusContainer} editable={editable} mutate={mutate} />
      </TemplateSection>

      <TemplateSection title="初始化容器" description="Init Container 按列表顺序执行；Sidecar 类型仍属于 Init Container。">
        <ContainerList document={document} init registries={registries} focusContainer={focusContainer} editable={editable} mutate={mutate} />
      </TemplateSection>

      <TemplateSection title="存储卷" description="常用卷类型可在表单中维护；CSI、projected 等高级类型可保留并通过 YAML 编辑。">
        <VolumeList document={document} editable={editable} mutate={mutate} />
      </TemplateSection>

      <TemplateSection title="Pod 运行与调度" description="ServiceAccount、运行类、节点选择和 Pod 级安全上下文。">
        <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
          <Field label="ServiceAccount" value={stringValue(spec.serviceAccountName)} disabled={!editable} onChange={(value) => mutate((next) => setOrDelete(templateSpec(next), "serviceAccountName", value))} />
          <Field label="PriorityClass" value={stringValue(spec.priorityClassName)} disabled={!editable} onChange={(value) => mutate((next) => setOrDelete(templateSpec(next), "priorityClassName", value))} />
          <Field label="RuntimeClass" value={stringValue(spec.runtimeClassName)} disabled={!editable} onChange={(value) => mutate((next) => setOrDelete(templateSpec(next), "runtimeClassName", value))} />
          <Field label="SchedulerName" value={stringValue(spec.schedulerName)} disabled={!editable} onChange={(value) => mutate((next) => setOrDelete(templateSpec(next), "schedulerName", value))} />
          <Field label="terminationGracePeriodSeconds" value={stringValue(spec.terminationGracePeriodSeconds)} type="number" disabled={!editable} onChange={(value) => mutate((next) => setNumberOrDelete(templateSpec(next), "terminationGracePeriodSeconds", value))} />
          <Field label="ImagePullSecrets（逗号分隔）" value={imagePullSecrets.join(", ")} disabled={!editable} onChange={(value) => mutate((next) => {
            const names = value.split(",").map((item) => item.trim()).filter(Boolean);
            const target = templateSpec(next);
            if (names.length) target.imagePullSecrets = names.map((name) => ({ name }));
            else delete target.imagePullSecrets;
          })} />
        </div>
        <div className="mt-4 grid min-w-0 gap-4 lg:grid-cols-2">
          <KeyValueEditor id="node-selector" label="NodeSelector" values={stringMap(spec.nodeSelector)} disabled={!editable} onChange={(values) => mutate((next) => { const target = templateSpec(next); if (Object.keys(values).length) target.nodeSelector = values; else delete target.nodeSelector; })} />
          <div className="space-y-3 rounded-md border p-3">
            <p className="text-sm font-medium">Pod 安全上下文</p>
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <Field label="runAsUser" value={stringValue(asRecord(spec.securityContext).runAsUser)} type="number" disabled={!editable} onChange={(value) => mutate((next) => updatePodSecurityContext(next, "runAsUser", value, "number"))} />
              <Field label="runAsGroup" value={stringValue(asRecord(spec.securityContext).runAsGroup)} type="number" disabled={!editable} onChange={(value) => mutate((next) => updatePodSecurityContext(next, "runAsGroup", value, "number"))} />
              <Field label="fsGroup" value={stringValue(asRecord(spec.securityContext).fsGroup)} type="number" disabled={!editable} onChange={(value) => mutate((next) => updatePodSecurityContext(next, "fsGroup", value, "number"))} />
              <CheckField label="runAsNonRoot" checked={asRecord(spec.securityContext).runAsNonRoot === true} disabled={!editable} onChange={(checked) => mutate((next) => updatePodSecurityContext(next, "runAsNonRoot", checked, "boolean"))} />
            </div>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-4 rounded-md border p-3 text-sm">
          <CheckField label="hostNetwork" checked={spec.hostNetwork === true} disabled={!editable} onChange={(checked) => mutate((next) => setBooleanOrDelete(templateSpec(next), "hostNetwork", checked))} />
          <CheckField label="hostPID" checked={spec.hostPID === true} disabled={!editable} onChange={(checked) => mutate((next) => setBooleanOrDelete(templateSpec(next), "hostPID", checked))} />
          <CheckField label="hostIPC" checked={spec.hostIPC === true} disabled={!editable} onChange={(checked) => mutate((next) => setBooleanOrDelete(templateSpec(next), "hostIPC", checked))} />
          <CheckField label="shareProcessNamespace" checked={spec.shareProcessNamespace === true} disabled={!editable} onChange={(checked) => mutate((next) => setBooleanOrDelete(templateSpec(next), "shareProcessNamespace", checked))} />
        </div>
      </TemplateSection>
    </div>
  );
}

function ContainerList({ document, init, registries, focusContainer, editable, mutate }: {
  document: PodTemplateDocument;
  init: boolean;
  registries: ContainerRegistry[];
  focusContainer?: string;
  editable: boolean;
  mutate: (callback: (next: PodTemplateDocument) => void) => void;
}) {
  const containers = templateContainers(document, init);
  return <div className="space-y-3">
    {containers.map((container, index) => <ContainerEditor key={`${containerName(container)}-${index}`} document={document} container={container} index={index} init={init} registries={registries} focused={focusContainer === containerName(container)} editable={editable} mutate={mutate} />)}
    <Button type="button" variant="outline" size="sm" disabled={!editable || containers.length >= 32} onClick={() => mutate((next) => templateContainers(next, init).push(newContainer(init, containers.length)))}><Plus className="h-4 w-4" />添加{init ? " Init Container" : "业务容器"}</Button>
  </div>;
}

function ContainerEditor({ document, container, index, init, registries, focused, editable, mutate }: {
  document: PodTemplateDocument;
  container: PodContainerDocument;
  index: number;
  init: boolean;
  registries: ContainerRegistry[];
  focused: boolean;
  editable: boolean;
  mutate: (callback: (next: PodTemplateDocument) => void) => void;
}) {
  const name = containerName(container) || `container-${index + 1}`;
  const patch = (callback: (target: PodContainerDocument) => void) => mutate((next) => { const target = templateContainers(next, init)[index]; if (target) callback(target); });
  const move = (direction: -1 | 1) => mutate((next) => {
    const list = templateContainers(next, init);
    const target = index + direction;
    if (target < 0 || target >= list.length) return;
    [list[index], list[target]] = [list[target], list[index]];
  });
  const remove = () => mutate((next) => { const list = templateContainers(next, init); list.splice(index, 1); });
  const security = asRecord(container.securityContext);
  return <details id={`template-container-${CSS.escape(name)}`} open={focused} className={cn("min-w-0 rounded-lg border bg-card", focused && "border-primary/50 ring-1 ring-primary/20")}>
    <summary className="flex min-w-0 cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-3 marker:hidden sm:px-4">
      <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 break-all font-mono text-sm font-medium">{name}</span>
      <Badge variant={init ? "outline" : "secondary"}>{init ? "Init" : "业务"}</Badge>
      <span className="min-w-0 break-all font-mono text-xs text-muted-foreground">{containerImage(container) || "未配置镜像"}</span>
      <span className="ml-auto flex flex-wrap gap-1">
        <Button type="button" variant="ghost" size="sm" className="h-7 px-2" disabled={!editable || index === 0} onClick={(event) => { event.preventDefault(); move(-1); }}>上移</Button>
        <Button type="button" variant="ghost" size="sm" className="h-7 px-2" disabled={!editable || index === templateContainers(document, init).length - 1} onClick={(event) => { event.preventDefault(); move(1); }}>下移</Button>
        <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-destructive" disabled={!editable} onClick={(event) => { event.preventDefault(); remove(); }}><Trash2 className="h-3.5 w-3.5" /></Button>
      </span>
    </summary>
    <div className="min-w-0 space-y-4 border-t p-3 sm:p-4">
      <div className="grid min-w-0 gap-3 md:grid-cols-2">
        <Field label="容器名称" value={name} disabled={!editable} onChange={(value) => patch((target) => { target.name = value.trim(); })} />
        <Field label="ImagePullPolicy" value={stringValue(container.imagePullPolicy)} placeholder="IfNotPresent" disabled={!editable} onChange={(value) => patch((target) => setOrDelete(target, "imagePullPolicy", value))} />
      </div>
      <ImageReferencePicker id={`template-image-${init ? "init" : "app"}-${index}`} value={containerImage(container)} onChange={(image, registryId) => mutate((next) => {
        const target = templateContainers(next, init)[index];
        if (!target) return;
        target.image = image;
        const secret = containerRegistrySecret(registries, registryId);
        if (secret) addImagePullSecret(next, secret);
      })} disabled={!editable} label="容器镜像" hint="选择镜像仓会同时把该镜像仓的拉取 Secret 加入 Pod 模板。" />
      <div className="grid min-w-0 gap-3 md:grid-cols-2">
        <LinesField label="Command（每行一个参数）" value={formatLineValues(container.command)} disabled={!editable} onChange={(value) => patch((target) => setStringArrayOrDelete(target, "command", parseLineValues(value)))} />
        <LinesField label="Args（每行一个参数）" value={formatLineValues(container.args)} disabled={!editable} onChange={(value) => patch((target) => setStringArrayOrDelete(target, "args", parseLineValues(value)))} />
      </div>
      <div className="grid min-w-0 gap-3 md:grid-cols-4">
        <Field label="CPU request" value={stringValue(asRecord(asRecord(container.resources).requests).cpu)} placeholder="100m" disabled={!editable} onChange={(value) => patch((target) => setResourceQuantity(target, "requests", "cpu", value))} />
        <Field label="CPU limit" value={stringValue(asRecord(asRecord(container.resources).limits).cpu)} placeholder="1" disabled={!editable} onChange={(value) => patch((target) => setResourceQuantity(target, "limits", "cpu", value))} />
        <Field label="Memory request" value={stringValue(asRecord(asRecord(container.resources).requests).memory)} placeholder="128Mi" disabled={!editable} onChange={(value) => patch((target) => setResourceQuantity(target, "requests", "memory", value))} />
        <Field label="Memory limit" value={stringValue(asRecord(asRecord(container.resources).limits).memory)} placeholder="512Mi" disabled={!editable} onChange={(value) => patch((target) => setResourceQuantity(target, "limits", "memory", value))} />
      </div>
      <ContainerEnvironment container={container} editable={editable} patch={patch} />
      <ContainerPorts container={container} editable={editable} patch={patch} />
      <ContainerMounts container={container} editable={editable} patch={patch} />
      <ContainerSecurity container={container} security={security} editable={editable} patch={patch} />
      <div className="grid min-w-0 gap-3 lg:grid-cols-3">
        <ProbeEditor label="存活检查" field="livenessProbe" container={container} editable={editable} patch={patch} />
        <ProbeEditor label="就绪检查" field="readinessProbe" container={container} editable={editable} patch={patch} />
        <ProbeEditor label="启动检查" field="startupProbe" container={container} editable={editable} patch={patch} />
      </div>
    </div>
  </details>;
}

function ContainerEnvironment({ container, editable, patch }: { container: PodContainerDocument; editable: boolean; patch: (callback: (target: PodContainerDocument) => void) => void }) {
  const env = arrayValue(container.env).map((item) => asRecord(item));
  const update = (index: number, callback: (item: Record<string, unknown>) => void) => patch((target) => { const rows = arrayValue(target.env); const row = asRecord(rows[index]); callback(row); rows[index] = row; target.env = rows; });
  return <details className="min-w-0 rounded-md border p-3"><summary className="cursor-pointer text-sm font-medium marker:text-primary">环境变量（{env.length}）</summary>
    <div className="mt-3 space-y-2">
      {env.map((row, index) => {
        const valueFrom = asRecord(row.valueFrom);
        const kind = typeof valueFrom.kind === "string" ? valueFrom.kind : "literal";
        return <div key={index} className="grid min-w-0 gap-2 rounded-md border bg-muted/10 p-2 md:grid-cols-[minmax(0,1fr)_150px_minmax(0,1fr)_minmax(0,1fr)_auto]">
          <Input value={stringValue(row.name)} placeholder="变量名" disabled={!editable} onChange={(event) => update(index, (item) => { item.name = event.target.value; })} />
          <select className="h-10 min-w-0 rounded-md border bg-background px-2 text-sm" value={kind} disabled={!editable} onChange={(event) => update(index, (item) => {
            const next = event.target.value;
            if (next === "literal") delete item.valueFrom;
            else item.valueFrom = { kind: next, name: stringValue(valueFrom.name), key: stringValue(valueFrom.key), optional: valueFrom.optional === true };
          })}>
            <option value="literal">直接值</option><option value="configMapKeyRef">ConfigMap</option><option value="secretKeyRef">Secret</option><option value="fieldRef">FieldRef</option><option value="resourceFieldRef">ResourceFieldRef</option>
          </select>
          {kind === "literal" ? <Input className="md:col-span-2" value={stringValue(row.value)} placeholder="变量值" disabled={!editable} onChange={(event) => update(index, (item) => { item.value = event.target.value; })} /> : <>
            <Input value={stringValue(valueFrom.name)} placeholder={kind === "fieldRef" ? "metadata.name" : "资源名称"} disabled={!editable} onChange={(event) => update(index, (item) => { item.valueFrom = { ...asRecord(item.valueFrom), name: event.target.value }; })} />
            <Input value={stringValue(valueFrom.key)} placeholder={kind === "fieldRef" ? "可留空" : "键"} disabled={!editable} onChange={(event) => update(index, (item) => { item.valueFrom = { ...asRecord(item.valueFrom), key: event.target.value }; })} />
          </>}
          <Button type="button" variant="ghost" size="sm" className="h-10 px-2 text-destructive" disabled={!editable} onClick={() => patch((target) => { const rows = arrayValue(target.env); rows.splice(index, 1); if (rows.length) target.env = rows; else delete target.env; })}><Trash2 className="h-4 w-4" /></Button>
        </div>;
      })}
      <Button type="button" variant="outline" size="sm" disabled={!editable || env.length >= 128} onClick={() => patch((target) => { const rows = arrayValue(target.env); rows.push({ name: "", value: "" }); target.env = rows; })}><Plus className="h-4 w-4" />添加环境变量</Button>
    </div>
  </details>;
}

function ContainerPorts({ container, editable, patch }: { container: PodContainerDocument; editable: boolean; patch: (callback: (target: PodContainerDocument) => void) => void }) {
  const ports = arrayValue(container.ports).map((item) => asRecord(item));
  return <details className="min-w-0 rounded-md border p-3"><summary className="cursor-pointer text-sm font-medium marker:text-primary">容器端口（{ports.length}）</summary>
    <div className="mt-3 space-y-2">
      {ports.map((port, index) => <div key={index} className="grid min-w-0 gap-2 rounded-md border bg-muted/10 p-2 md:grid-cols-[minmax(0,1fr)_140px_140px_auto]">
        <Input value={stringValue(port.name)} placeholder="端口名称（可选）" disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.ports); const row = asRecord(rows[index]); row.name = event.target.value; rows[index] = row; target.ports = rows; })} />
        <Input type="number" min={1} max={65535} value={stringValue(port.containerPort)} placeholder="8080" disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.ports); const row = asRecord(rows[index]); row.containerPort = Number(event.target.value); rows[index] = row; target.ports = rows; })} />
        <select className="h-10 rounded-md border bg-background px-2 text-sm" value={stringValue(port.protocol) || "TCP"} disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.ports); const row = asRecord(rows[index]); row.protocol = event.target.value; rows[index] = row; target.ports = rows; })}><option>TCP</option><option>UDP</option><option>SCTP</option></select>
        <Button type="button" variant="ghost" size="sm" className="h-10 px-2 text-destructive" disabled={!editable} onClick={() => patch((target) => { const rows = arrayValue(target.ports); rows.splice(index, 1); if (rows.length) target.ports = rows; else delete target.ports; })}><Trash2 className="h-4 w-4" /></Button>
      </div>)}
      <Button type="button" variant="outline" size="sm" disabled={!editable} onClick={() => patch((target) => { const rows = arrayValue(target.ports); rows.push({ containerPort: 8080, protocol: "TCP" }); target.ports = rows; })}><Plus className="h-4 w-4" />添加端口</Button>
    </div>
  </details>;
}

function ContainerMounts({ container, editable, patch }: { container: PodContainerDocument; editable: boolean; patch: (callback: (target: PodContainerDocument) => void) => void }) {
  const mounts = arrayValue(container.volumeMounts).map((item) => asRecord(item));
  return <details className="min-w-0 rounded-md border p-3"><summary className="cursor-pointer text-sm font-medium marker:text-primary">存储挂载（{mounts.length}）</summary>
    <div className="mt-3 space-y-2">
      {mounts.map((mount, index) => <div key={index} className="grid min-w-0 gap-2 rounded-md border bg-muted/10 p-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto_auto]">
        <Input value={stringValue(mount.name)} placeholder="卷名称" disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.volumeMounts); const row = asRecord(rows[index]); row.name = event.target.value; rows[index] = row; target.volumeMounts = rows; })} />
        <Input value={stringValue(mount.mountPath)} placeholder="/data" disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.volumeMounts); const row = asRecord(rows[index]); row.mountPath = event.target.value; rows[index] = row; target.volumeMounts = rows; })} />
        <Input value={stringValue(mount.subPath)} placeholder="subPath（可选）" disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.volumeMounts); const row = asRecord(rows[index]); setOrDelete(row, "subPath", event.target.value); rows[index] = row; target.volumeMounts = rows; })} />
        <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={mount.readOnly === true} disabled={!editable} onChange={(event) => patch((target) => { const rows = arrayValue(target.volumeMounts); const row = asRecord(rows[index]); if (event.target.checked) row.readOnly = true; else delete row.readOnly; rows[index] = row; target.volumeMounts = rows; })} />只读</label>
        <Button type="button" variant="ghost" size="sm" className="h-10 px-2 text-destructive" disabled={!editable} onClick={() => patch((target) => { const rows = arrayValue(target.volumeMounts); rows.splice(index, 1); if (rows.length) target.volumeMounts = rows; else delete target.volumeMounts; })}><Trash2 className="h-4 w-4" /></Button>
      </div>)}
      <Button type="button" variant="outline" size="sm" disabled={!editable} onClick={() => patch((target) => { const rows = arrayValue(target.volumeMounts); rows.push({ name: "", mountPath: "" }); target.volumeMounts = rows; })}><Plus className="h-4 w-4" />添加挂载</Button>
    </div>
  </details>;
}

function ContainerSecurity({ security, editable, patch }: { container: PodContainerDocument; security: Record<string, unknown>; editable: boolean; patch: (callback: (target: PodContainerDocument) => void) => void }) {
  return <details className="min-w-0 rounded-md border p-3"><summary className="cursor-pointer text-sm font-medium marker:text-primary">容器安全上下文</summary>
    <div className="mt-3 grid min-w-0 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <Field label="runAsUser" value={stringValue(security.runAsUser)} type="number" disabled={!editable} onChange={(value) => patch((target) => updateSecurityContext(target, "runAsUser", value, "number"))} />
      <Field label="runAsGroup" value={stringValue(security.runAsGroup)} type="number" disabled={!editable} onChange={(value) => patch((target) => updateSecurityContext(target, "runAsGroup", value, "number"))} />
      <CheckField label="runAsNonRoot" checked={security.runAsNonRoot === true} disabled={!editable} onChange={(checked) => patch((target) => updateSecurityContext(target, "runAsNonRoot", checked, "boolean"))} />
      <CheckField label="只读根文件系统" checked={security.readOnlyRootFilesystem === true} disabled={!editable} onChange={(checked) => patch((target) => updateSecurityContext(target, "readOnlyRootFilesystem", checked, "boolean"))} />
      <CheckField label="允许权限提升" checked={security.allowPrivilegeEscalation === true} disabled={!editable} onChange={(checked) => patch((target) => updateSecurityContext(target, "allowPrivilegeEscalation", checked, "boolean"))} />
      <Field label="Capabilities Add（逗号分隔）" value={Array.isArray(asRecord(security.capabilities).add) ? (asRecord(security.capabilities).add as string[]).join(", ") : ""} disabled={!editable} onChange={(value) => patch((target) => {
        const sc = asRecord(target.securityContext);
        const caps = asRecord(sc.capabilities);
        const values = value.split(",").map((item) => item.trim()).filter(Boolean);
        if (values.length) caps.add = values; else delete caps.add;
        if (Object.keys(caps).length) sc.capabilities = caps; else delete sc.capabilities;
        if (Object.keys(sc).length) target.securityContext = sc; else delete target.securityContext;
      })} />
    </div>
  </details>;
}

function ProbeEditor({ label, field, container, editable, patch }: { label: string; field: "livenessProbe" | "readinessProbe" | "startupProbe"; container: PodContainerDocument; editable: boolean; patch: (callback: (target: PodContainerDocument) => void) => void }) {
  const probe = asRecord(container[field]);
  const type = probe.httpGet ? "httpGet" : probe.tcpSocket ? "tcpSocket" : probe.exec ? "exec" : probe.grpc ? "grpc" : "disabled";
  const target = asRecord(probe.httpGet ?? probe.tcpSocket ?? probe.grpc);
  return <div className="min-w-0 rounded-md border p-3">
    <p className="mb-2 text-sm font-medium">{label}</p>
    <select className="h-10 w-full rounded-md border bg-background px-2 text-sm" value={type} disabled={!editable} onChange={(event) => patch((containerTarget) => {
      const nextType = event.target.value;
      if (nextType === "disabled") delete containerTarget[field];
      else if (nextType === "exec") containerTarget[field] = { exec: { command: [] }, initialDelaySeconds: 0, periodSeconds: 10, timeoutSeconds: 1, failureThreshold: 3 };
      else if (nextType === "grpc") containerTarget[field] = { grpc: { port: 50051 }, initialDelaySeconds: 0, periodSeconds: 10, timeoutSeconds: 1, failureThreshold: 3 };
      else containerTarget[field] = { [nextType]: { port: 8080, path: nextType === "httpGet" ? "/" : undefined, scheme: nextType === "httpGet" ? "HTTP" : undefined }, initialDelaySeconds: 0, periodSeconds: 10, timeoutSeconds: 1, failureThreshold: 3 };
    })}><option value="disabled">未启用</option><option value="httpGet">HTTP GET</option><option value="tcpSocket">TCP Socket</option><option value="exec">Exec</option><option value="grpc">gRPC</option></select>
    {type !== "disabled" ? <div className="mt-2 space-y-2">
      {type === "httpGet" ? <Input value={stringValue(target.path)} placeholder="/healthz" disabled={!editable} onChange={(event) => patch((containerTarget) => { const next = asRecord(containerTarget[field]); next.httpGet = { ...asRecord(next.httpGet), path: event.target.value }; containerTarget[field] = next; })} /> : null}
      {type === "exec" ? <Textarea value={formatLineValues(asRecord(probe.exec).command)} placeholder={"cat\n/tmp/healthy"} className="min-h-16 font-mono text-xs" disabled={!editable} onChange={(event) => patch((containerTarget) => { const next = asRecord(containerTarget[field]); next.exec = { command: parseLineValues(event.target.value) }; containerTarget[field] = next; })} /> : null}
      {type !== "exec" ? <Input value={stringValue(type === "grpc" ? asRecord(probe.grpc).port : target.port)} placeholder="8080" disabled={!editable} onChange={(event) => patch((containerTarget) => { const next = asRecord(containerTarget[field]); if (type === "grpc") next.grpc = { ...asRecord(next.grpc), port: Number(event.target.value) || event.target.value }; else next[type] = { ...asRecord(next[type]), port: event.target.value }; containerTarget[field] = next; })} /> : null}
      <div className="grid grid-cols-2 gap-2">
        <Field label="initialDelay" value={stringValue(probe.initialDelaySeconds)} type="number" disabled={!editable} onChange={(value) => patch((containerTarget) => { const next = asRecord(containerTarget[field]); next.initialDelaySeconds = Number(value) || 0; containerTarget[field] = next; })} />
        <Field label="period" value={stringValue(probe.periodSeconds)} type="number" disabled={!editable} onChange={(value) => patch((containerTarget) => { const next = asRecord(containerTarget[field]); next.periodSeconds = Number(value) || 10; containerTarget[field] = next; })} />
      </div>
    </div> : null}
  </div>;
}

function VolumeList({ document, editable, mutate }: { document: PodTemplateDocument; editable: boolean; mutate: (callback: (next: PodTemplateDocument) => void) => void }) {
  const volumes = arrayValue(templateSpec(document).volumes).map((item) => asRecord(item));
  return <div className="space-y-2">
    {volumes.map((volume, index) => {
      const type = volume.configMap ? "configMap" : volume.secret ? "secret" : volume.persistentVolumeClaim ? "persistentVolumeClaim" : volume.emptyDir ? "emptyDir" : "advanced";
      const source = asRecord(volume.configMap ?? volume.secret ?? volume.persistentVolumeClaim);
      return <div key={index} className="grid min-w-0 gap-2 rounded-md border p-2 md:grid-cols-[minmax(0,1fr)_170px_minmax(0,1.4fr)_auto]">
        <Input value={stringValue(volume.name)} placeholder="卷名称" disabled={!editable} onChange={(event) => mutate((next) => { const rows = arrayValue(templateSpec(next).volumes); const row = asRecord(rows[index]); row.name = event.target.value; rows[index] = row; templateSpec(next).volumes = rows; })} />
        <select className="h-10 rounded-md border bg-background px-2 text-sm" value={type} disabled={!editable || type === "advanced"} onChange={(event) => mutate((next) => { const rows = arrayValue(templateSpec(next).volumes); const row = asRecord(rows[index]); for (const key of ["configMap", "secret", "persistentVolumeClaim", "emptyDir"]) delete row[key]; const value = event.target.value; if (value === "emptyDir") row.emptyDir = {}; else row[value] = { [value === "configMap" ? "name" : value === "secret" ? "secretName" : "claimName"]: "" }; rows[index] = row; templateSpec(next).volumes = rows; })}><option value="configMap">ConfigMap</option><option value="secret">Secret</option><option value="persistentVolumeClaim">PVC</option><option value="emptyDir">EmptyDir</option>{type === "advanced" ? <option value="advanced">高级类型（YAML）</option> : null}</select>
        {type === "emptyDir" ? <p className="self-center text-xs text-muted-foreground">EmptyDir 无需来源</p> : type === "advanced" ? <p className="self-center break-all text-xs text-muted-foreground">高级卷类型请通过 YAML 编辑</p> : <Input value={stringValue(source.name ?? source.secretName ?? source.claimName)} placeholder="来源名称" disabled={!editable} onChange={(event) => mutate((next) => { const rows = arrayValue(templateSpec(next).volumes); const row = asRecord(rows[index]); const sourceKey = type === "configMap" ? "name" : type === "secret" ? "secretName" : "claimName"; row[type] = { ...asRecord(row[type]), [sourceKey]: event.target.value }; rows[index] = row; templateSpec(next).volumes = rows; })} />}
        <Button type="button" variant="ghost" size="sm" className="h-10 px-2 text-destructive" disabled={!editable} onClick={() => mutate((next) => { const rows = arrayValue(templateSpec(next).volumes); rows.splice(index, 1); if (rows.length) templateSpec(next).volumes = rows; else delete templateSpec(next).volumes; })}><Trash2 className="h-4 w-4" /></Button>
      </div>;
    })}
    <Button type="button" variant="outline" size="sm" disabled={!editable} onClick={() => mutate((next) => { const rows = arrayValue(templateSpec(next).volumes); rows.push({ name: "", configMap: { name: "" } }); templateSpec(next).volumes = rows; })}><Plus className="h-4 w-4" />添加卷</Button>
  </div>;
}

function KeyValueEditor({ id, label, values, protectedKeys = [], disabled, onChange }: { id: string; label: string; values: Record<string, string>; protectedKeys?: string[]; disabled: boolean; onChange: (values: Record<string, string>) => void }) {
  const [draftKey, setDraftKey] = useState("");
  const [draftValue, setDraftValue] = useState("");
  const entries = Object.entries(values);
  return <div className="min-w-0 space-y-2 rounded-md border p-3">
    <p className="text-sm font-medium">{label}</p>
    {entries.map(([key, value]) => <div key={key} className="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
      <Input value={key} disabled={disabled || protectedKeys.includes(key)} onChange={(event) => { const next = { ...values }; delete next[key]; next[event.target.value] = value; onChange(next); }} />
      <Input value={value} disabled={disabled} onChange={(event) => onChange({ ...values, [key]: event.target.value })} />
      <Button type="button" variant="ghost" size="sm" className="h-10 px-2 text-destructive" disabled={disabled || protectedKeys.includes(key)} onClick={() => { const next = { ...values }; delete next[key]; onChange(next); }}><Trash2 className="h-4 w-4" /></Button>
    </div>)}
    <div className="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
      <Input id={`${id}-key`} value={draftKey} placeholder="新增键" disabled={disabled} onChange={(event) => setDraftKey(event.target.value)} />
      <Input value={draftValue} placeholder="值" disabled={disabled} onChange={(event) => setDraftValue(event.target.value)} />
      <Button type="button" variant="outline" size="sm" className="h-10" disabled={disabled || !draftKey.trim()} onClick={() => { onChange({ ...values, [draftKey.trim()]: draftValue }); setDraftKey(""); setDraftValue(""); }}><Plus className="h-4 w-4" />添加</Button>
    </div>
  </div>;
}

function TemplateSection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return <section className="min-w-0 rounded-lg border bg-card">
    <div className="border-b bg-muted/20 px-3 py-3 sm:px-4"><h2 className="text-sm font-semibold">{title}</h2><p className="mt-1 text-xs leading-5 text-muted-foreground">{description}</p></div>
    <div className="min-w-0 p-3 sm:p-4">{children}</div>
  </section>;
}

function Field({ label, value, onChange, disabled, placeholder, type = "text" }: { label: string; value: string; onChange: (value: string) => void; disabled: boolean; placeholder?: string; type?: "text" | "number" }) {
  const id = `template-field-${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return <div className="min-w-0 space-y-1.5"><Label htmlFor={id} className="break-words">{label}</Label><Input id={id} type={type} value={value} placeholder={placeholder} disabled={disabled} onChange={(event) => onChange(event.target.value)} /></div>;
}

function LinesField({ label, value, onChange, disabled }: { label: string; value: string; onChange: (value: string) => void; disabled: boolean }) {
  const id = `template-lines-${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return <div className="min-w-0 space-y-1.5"><Label htmlFor={id}>{label}</Label><Textarea id={id} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)} className="min-h-20 font-mono text-xs" spellCheck={false} /></div>;
}

function CheckField({ label, checked, onChange, disabled }: { label: string; checked: boolean; onChange: (checked: boolean) => void; disabled: boolean }) {
  return <label className="flex items-center gap-2 text-sm"><input type="checkbox" className="h-4 w-4" checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} />{label}</label>;
}

function newContainer(init: boolean, index: number): PodContainerDocument {
  return { name: `${init ? "init" : "container"}-${index + 1}`, image: "", imagePullPolicy: "IfNotPresent" };
}

function setOrDelete(target: Record<string, unknown>, key: string, value: string) {
  if (value.trim()) target[key] = value.trim();
  else delete target[key];
}

function setNumberOrDelete(target: Record<string, unknown>, key: string, value: string) {
  if (value.trim() === "") { delete target[key]; return; }
  const parsed = Number(value);
  if (Number.isFinite(parsed)) target[key] = parsed;
}

function setBooleanOrDelete(target: Record<string, unknown>, key: string, value: boolean) {
  if (value) target[key] = true;
  else delete target[key];
}

function setStringArrayOrDelete(target: Record<string, unknown>, key: string, values: string[]) {
  if (values.length) target[key] = values;
  else delete target[key];
}

function setResourceQuantity(target: PodContainerDocument, section: "requests" | "limits", resource: "cpu" | "memory", value: string) {
  const resources = asRecord(target.resources);
  const quantities = asRecord(resources[section]);
  if (value.trim()) quantities[resource] = value.trim();
  else delete quantities[resource];
  if (Object.keys(quantities).length) resources[section] = quantities;
  else delete resources[section];
  if (Object.keys(resources).length) target.resources = resources;
  else delete target.resources;
}

function updatePodSecurityContext(document: PodTemplateDocument, key: string, value: string | boolean, kind: "number" | "boolean") {
  const spec = templateSpec(document);
  const security = asRecord(spec.securityContext);
  if (kind === "boolean") {
    if (value === true) security[key] = true;
    else delete security[key];
  } else {
    const text = String(value);
    if (text.trim() === "") delete security[key];
    else security[key] = Number(text);
  }
  if (Object.keys(security).length) spec.securityContext = security;
  else delete spec.securityContext;
}

function updateSecurityContext(target: PodContainerDocument, key: string, value: string | boolean, kind: "number" | "boolean") {
  const security = asRecord(target.securityContext);
  if (kind === "boolean") {
    if (value === true) security[key] = true;
    else delete security[key];
  } else {
    const text = String(value);
    if (text.trim() === "") delete security[key];
    else security[key] = Number(text);
  }
  if (Object.keys(security).length) target.securityContext = security;
  else delete target.securityContext;
}
