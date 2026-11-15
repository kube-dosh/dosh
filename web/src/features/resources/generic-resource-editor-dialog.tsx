import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import CodeMirror from "@uiw/react-codemirror";
import { yaml as yamlLanguage } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import { parse, stringify } from "yaml";
import { BookmarkPlus, CheckCircle2, Clock3, FileCode2, FormInput, GitCompareArrows, Loader2, Maximize2, Minimize2, Save, Trash2, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { GenericResourceDetail, ResourceCapabilities, ResourceDescriptor, ResourceReference } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { SchemaResourceForm, schemaIssueSummary } from "@/features/resources/schema-resource-form";
import { isRecord, validateResourceObject, type ResourceObject } from "@/features/resources/resource-schema";
import { diffStats, diffYAML } from "@/features/resources/yaml-diff";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { clearGenericDraft, deleteGenericTemplate, genericDraft, genericTemplateKey, listGenericTemplates, saveGenericDraft, saveGenericTemplate, type GenericResourceTemplate } from "@/lib/generic-templates";

type EditorMode = "create" | "update";
type EditorViewMode = "form" | "yaml" | "diff";

function expectedAPIVersion(descriptor: ResourceDescriptor): string {
  return descriptor.group ? `${descriptor.group}/${descriptor.version}` : descriptor.version;
}

export function genericManifestTemplate(descriptor: ResourceDescriptor, namespace: string): string {
  const metadata = [`  name: example`];
  if (descriptor.namespaced) metadata.push(`  namespace: ${namespace || "default"}`);
  return `apiVersion: ${expectedAPIVersion(descriptor)}\nkind: ${descriptor.kind}\nmetadata:\n${metadata.join("\n")}\nspec: {}\n`;
}

export function GenericResourceEditorDialog({ open, onOpenChange, descriptor, reference, namespace, mode, detail, capabilities, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  descriptor: ResourceDescriptor;
  reference: ResourceReference;
  namespace: string;
  mode: EditorMode;
  detail?: GenericResourceDetail;
  capabilities?: ResourceCapabilities;
  onSaved?: (detail: GenericResourceDetail) => void | Promise<void>;
}) {
  const { theme } = useTheme();
  const [value, setValue] = useState("");
  const [view, setView] = useState<EditorViewMode>("yaml");
  const [confirmation, setConfirmation] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [templates, setTemplates] = useState<GenericResourceTemplate[]>([]);
  const [draft, setDraft] = useState<{ yaml: string; savedAt: number }>();
  const templateStorageKey = useMemo(() => genericTemplateKey(reference.group, reference.version, reference.resource), [reference.group, reference.resource, reference.version]);
  const resourceSchema = useQuery({
    queryKey: ["generic-resource-schema", reference.group, reference.version, reference.resource],
    queryFn: () => api.resourceSchema(reference),
    enabled: open && Boolean(descriptor),
    staleTime: 5 * 60_000,
  });

  useEffect(() => {
    if (!open) return;
    setValue(mode === "update" && detail ? detail.yaml : genericManifestTemplate(descriptor, namespace));
    setView("yaml");
    setConfirmation("");
    setTemplates(listGenericTemplates(templateStorageKey));
    setDraft(genericDraft(templateStorageKey));
  }, [descriptor, detail, mode, namespace, open]);

  useEffect(() => {
    if (!open || !value.trim()) return;
    const timer = window.setTimeout(() => saveGenericDraft(templateStorageKey, value), 700);
    return () => window.clearTimeout(timer);
  }, [open, templateStorageKey, value]);

  const parsed = useMemo(() => parseManifest(value), [value]);
  const metadata = parsed.object && isRecord(parsed.object.metadata) ? parsed.object.metadata : {};
  const manifestName = typeof metadata.name === "string" ? metadata.name : "";
  const issues = useMemo(() => parsed.object && resourceSchema.data ? validateResourceObject(resourceSchema.data, parsed.object) : [], [parsed.object, resourceSchema.data]);
  const requireConfirmation = Boolean(capabilities?.requireConfirmation);
  const confirmationMatches = !requireConfirmation || confirmation === manifestName;
  const writeDisabled = !value.trim() || Boolean(parsed.error) || issues.length > 0 || !manifestName || !confirmationMatches;
  const save = useMutation({
    mutationFn: (dryRun: boolean) => mode === "create"
      ? api.createGenericResource(reference, {
        namespace: namespace || undefined, yaml: value, dryRun,
        confirmation: dryRun ? undefined : requireConfirmation ? confirmation : undefined,
      })
      : api.updateGenericResource(reference, detail?.name ?? "", {
        namespace: namespace || detail?.namespace, yaml: value, resourceVersion: detail?.resourceVersion, dryRun,
        confirmation: dryRun ? undefined : requireConfirmation ? confirmation : undefined,
      }),
    onSuccess: async (result, dryRun) => {
      if (dryRun) {
        toast.success("Dry Run 校验通过，未写入集群");
        return;
      }
      toast.success(mode === "create" ? `${result.kind} ${result.name} 已创建` : `${result.kind} ${result.name} 已更新`);
      onOpenChange(false);
      await onSaved?.(result);
    },
    onError: (error) => toast.error(error.message),
  });
  const submit = (dryRun: boolean) => {
    if (parsed.error) {
      toast.error("YAML 解析失败，请先修复语法错误");
      return;
    }
    if (!manifestName) {
      toast.error("metadata.name 不能为空");
      return;
    }
    if (!dryRun && issues.length > 0) {
      toast.error(`Schema 校验未通过：${schemaIssueSummary(issues)}`);
      return;
    }
    if (!dryRun && !confirmationMatches) {
      toast.error("请输入资源名称完成高风险写入确认");
      return;
    }
    save.mutate(dryRun);
  };
  const diff = useMemo(() => diffYAML(detail?.yaml ?? "", value), [detail?.yaml, value]);
  const stats = useMemo(() => diffStats(diff), [diff]);
  const schemaWarnings = resourceSchema.data?.warnings ?? [];
  const refreshTemplates = () => {
    setTemplates(listGenericTemplates(templateStorageKey));
    setDraft(genericDraft(templateStorageKey));
  };
  const saveCurrentTemplate = () => {
    if (!value.trim()) return;
    const name = manifestName ? `${manifestName} · ${descriptor.kind}` : `${descriptor.kind} 模板`;
    saveGenericTemplate(templateStorageKey, name, value);
    saveGenericDraft(templateStorageKey, value);
    refreshTemplates();
    toast.success(`已保存本地模板“${name}”`);
  };

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!save.isPending) onOpenChange(next); }}>
      <DialogContent className={cn("max-w-5xl", expanded && "h-[calc(100dvh-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto")}>
        <DialogHeader className="min-w-0 pr-8">
          <DialogTitle className="flex min-w-0 items-center gap-2"><FileCode2 className="h-5 w-5 shrink-0 text-primary" /><span className="min-w-0 break-words">{mode === "create" ? `创建 ${descriptor.kind}` : `编辑 ${descriptor.kind}`}</span></DialogTitle>
          <DialogDescription className="break-words">{expectedAPIVersion(descriptor)} · {descriptor.namespaced ? namespace || "请选择命名空间" : "集群级资源"} · Schema 表单 / YAML 双模式</DialogDescription>
        </DialogHeader>

        {capabilities?.requireConfirmation ? (
          <section className={cn("min-w-0 rounded-md border px-3 py-2 text-xs", capabilities.riskLevel === "critical" ? "border-destructive/45 bg-destructive/10 text-destructive-strong" : "border-warning/45 bg-warning/10 text-warning")} aria-labelledby="resource-risk-heading">
            <h3 id="resource-risk-heading" className="flex items-center gap-2 font-semibold"><TriangleAlert className="h-4 w-4 shrink-0" />高风险资源 · {capabilities.riskLevel === "critical" ? "严重" : "高"}</h3>
            <ul className="mt-1 list-inside list-disc space-y-0.5 break-words">
              {(capabilities.riskReasons ?? []).map((reason) => <li key={reason}>{reason}</li>)}
            </ul>
          </section>
        ) : null}

        {resourceSchema.isLoading ? <p className="text-xs text-muted-foreground" aria-live="polite">正在读取 OpenAPI Schema…</p> : null}
        {resourceSchema.isError ? <SchemaWarning text="OpenAPI Schema 读取失败，当前仅可使用 YAML；API Server Dry Run 仍是最终校验依据。" /> : null}
        {resourceSchema.data?.source === "unavailable" ? <SchemaWarning text={schemaWarnings.join("；") || "当前资源类型没有可用 Schema，请使用 YAML 编辑。"} /> : null}
        {resourceSchema.data?.source === "openapi-v2" && schemaWarnings.length ? <SchemaWarning text={schemaWarnings.join("；")} /> : null}
        {parsed.error ? <SchemaWarning text={`YAML 语法错误：${parsed.error}`} /> : null}
        {issues.length ? <SchemaWarning text={`Schema 校验：${schemaIssueSummary(issues)}`} /> : null}

        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild><Button variant="outline" size="sm"><BookmarkPlus className="h-4 w-4" />本地模板</Button></DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              {draft && draft.yaml !== value ? <DropdownMenuItem onSelect={() => setValue(draft.yaml)}><Clock3 className="h-4 w-4" />恢复最近草稿</DropdownMenuItem> : <DropdownMenuItem disabled><Clock3 className="h-4 w-4" />暂无最近草稿</DropdownMenuItem>}
              {draft ? <DropdownMenuItem onSelect={() => { clearGenericDraft(templateStorageKey); refreshTemplates(); toast.success("已清除最近草稿"); }}><Trash2 className="h-4 w-4" />清除最近草稿</DropdownMenuItem> : null}
              {templates.length ? <DropdownMenuSeparator /> : null}
              {templates.length ? <DropdownMenuLabel>已保存模板</DropdownMenuLabel> : null}
              {templates.map((item) => (
                <DropdownMenuItem key={item.id} onSelect={() => setValue(item.yaml)}><FileCode2 className="h-4 w-4" /><span className="min-w-0 flex-1 truncate" title={item.name}>{item.name}</span><button type="button" aria-label={`删除模板 ${item.name}`} onClick={(event) => { event.preventDefault(); event.stopPropagation(); deleteGenericTemplate(templateStorageKey, item.id); refreshTemplates(); }} className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"><Trash2 className="h-3.5 w-3.5" /></button></DropdownMenuItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={saveCurrentTemplate}><BookmarkPlus className="h-4 w-4" />从当前 YAML 另存模板</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="outline" size="sm" onClick={() => setExpanded((current) => !current)}>{expanded ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}{expanded ? "退出全屏" : "全屏编辑"}</Button>
        </div>

        <Tabs value={view} onValueChange={(next) => setView(next as EditorViewMode)} className="min-w-0">
          <TabsList aria-label="资源编辑模式">
            <TabsTrigger value="form"><FormInput className="mr-1 h-3.5 w-3.5" />表单</TabsTrigger>
            <TabsTrigger value="yaml"><FileCode2 className="mr-1 h-3.5 w-3.5" />YAML</TabsTrigger>
            {mode === "update" ? <TabsTrigger value="diff"><GitCompareArrows className="mr-1 h-3.5 w-3.5" />变更预览 {stats.added + stats.removed ? `(+${stats.added}/-${stats.removed})` : ""}</TabsTrigger> : null}
          </TabsList>
          <TabsContent value="form" className="min-w-0">
            {parsed.object ? (
              <SchemaResourceForm
                schema={resourceSchema.data}
                object={parsed.object}
                namespaced={descriptor.namespaced}
                issues={issues}
                disabled={save.isPending}
                onChange={(next) => setValue(toManifest(next))}
              />
            ) : <p className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">请先在 YAML 模式中输入合法对象，再使用动态表单。</p>}
          </TabsContent>
          <TabsContent value="yaml" className="min-w-0">
            <div className="min-w-0 overflow-hidden rounded-md border">
              <CodeMirror value={value} onChange={setValue} extensions={[yamlLanguage(), EditorView.lineWrapping]} editable={!save.isPending} height={expanded ? "calc(100dvh - 24rem)" : "56vh"} theme={theme} basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true }} aria-label="YAML 编辑器" />
            </div>
          </TabsContent>
          {mode === "update" ? (
            <TabsContent value="diff" className="min-w-0">
              <div className="max-h-[56vh] min-w-0 overflow-y-auto rounded-md border bg-muted/10 font-mono text-xs" aria-label="YAML 变更预览">
                {diff.map((line, index) => (
                  <div key={`${line.kind}-${line.oldLine ?? "n"}-${line.newLine ?? "n"}-${index}`} className={cn("flex min-w-0 items-start gap-2 border-b border-border/40 px-2 py-0.5 last:border-b-0", line.kind === "added" && "bg-success/10 text-success", line.kind === "removed" && "bg-destructive/10 text-destructive-strong", line.kind === "meta" && "bg-warning/10 font-sans text-warning")}>
                    <span className="w-7 shrink-0 select-none text-right text-muted-foreground">{line.oldLine ?? ""}</span>
                    <span className="w-7 shrink-0 select-none text-right text-muted-foreground">{line.newLine ?? ""}</span>
                    <span className="w-3 shrink-0 select-none">{line.kind === "added" ? "+" : line.kind === "removed" ? "-" : " "}</span>
                    <span className="min-w-0 whitespace-pre-wrap break-words">{line.text || " "}</span>
                  </div>
                ))}
              </div>
            </TabsContent>
          ) : null}
        </Tabs>

        {requireConfirmation ? (
          <div className="min-w-0 space-y-2 rounded-md border border-warning/35 bg-warning/5 p-3">
            <Label htmlFor="generic-resource-confirmation" className="break-words">输入资源名称确认高风险写入</Label>
            <Input id="generic-resource-confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} placeholder={manifestName || "资源名称"} autoComplete="off" spellCheck={false} disabled={save.isPending} />
            <p className="text-xs text-muted-foreground">Dry Run 不需要确认；正式创建、保存或删除必须在服务端再次匹配名称。</p>
          </div>
        ) : null}

        <p className="text-xs leading-5 text-muted-foreground">服务端会校验 GVR、名称、命名空间、单文档、1 MiB 上限，并在 Schema 可用时执行必填字段和基础类型预检。正式写入前会自动执行一次 API Server Dry Run；Dry Run 和准入结果始终优先于本地 Schema 提示。Secret 通用写入已关闭，请使用 Secret 专用页面。</p>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button>
          <Button variant="outline" onClick={() => submit(true)} disabled={save.isPending || !value.trim() || Boolean(parsed.error)}><CheckCircle2 className="h-4 w-4" />Dry Run</Button>
          <Button onClick={() => submit(false)} disabled={save.isPending || writeDisabled}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}{mode === "create" ? "创建" : "保存"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SchemaWarning({ text }: { text: string }) {
  return (
    <div role="status" className="flex min-w-0 items-start gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">
      <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
      <span className="min-w-0 break-words">{text}</span>
    </div>
  );
}

function parseManifest(value: string): { object?: ResourceObject; error?: string } {
  if (!value.trim()) return {};
  try {
    const parsed = parse(value);
    if (!isRecord(parsed)) return { error: "YAML 根节点必须是对象" };
    return { object: parsed };
  } catch (error) {
    return { error: error instanceof Error ? error.message : "YAML 解析失败" };
  }
}

function toManifest(object: ResourceObject): string {
  return `${stringify(object, { lineWidth: 0 }).trimEnd()}\n`;
}
