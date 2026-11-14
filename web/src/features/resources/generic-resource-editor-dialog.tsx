import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import CodeMirror from "@uiw/react-codemirror";
import { yaml as yamlLanguage } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import { CheckCircle2, FileCode2, Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { GenericResourceDetail, ResourceDescriptor, ResourceReference } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useTheme } from "@/lib/theme";

type EditorMode = "create" | "update";

function expectedAPIVersion(descriptor: ResourceDescriptor): string {
  return descriptor.group ? `${descriptor.group}/${descriptor.version}` : descriptor.version;
}

export function genericManifestTemplate(descriptor: ResourceDescriptor, namespace: string): string {
  const metadata = [`  name: example`];
  if (descriptor.namespaced) metadata.push(`  namespace: ${namespace || "default"}`);
  return `apiVersion: ${expectedAPIVersion(descriptor)}\nkind: ${descriptor.kind}\nmetadata:\n${metadata.join("\n")}\nspec: {}\n`;
}

export function GenericResourceEditorDialog({ open, onOpenChange, descriptor, reference, namespace, mode, detail, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  descriptor: ResourceDescriptor;
  reference: ResourceReference;
  namespace: string;
  mode: EditorMode;
  detail?: GenericResourceDetail;
  onSaved?: (detail: GenericResourceDetail) => void | Promise<void>;
}) {
  const { theme } = useTheme();
  const [value, setValue] = useState("");
  useEffect(() => {
    if (!open) return;
    setValue(mode === "update" && detail ? detail.yaml : genericManifestTemplate(descriptor, namespace));
  }, [descriptor, detail, mode, namespace, open]);
  const save = useMutation({
    mutationFn: (dryRun: boolean) => mode === "create"
      ? api.createGenericResource(reference, { namespace: namespace || undefined, yaml: value, dryRun })
      : api.updateGenericResource(reference, detail?.name ?? "", { namespace: namespace || detail?.namespace, yaml: value, resourceVersion: detail?.resourceVersion, dryRun }),
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
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!save.isPending) onOpenChange(next); }}>
      <DialogContent className="max-w-5xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><FileCode2 className="h-5 w-5 text-primary" />{mode === "create" ? `创建 ${descriptor.kind}` : `编辑 ${descriptor.kind}`}</DialogTitle>
          <DialogDescription>{expectedAPIVersion(descriptor)} · {descriptor.namespaced ? namespace || "请选择命名空间" : "集群级资源"} · YAML-only 写入</DialogDescription>
        </DialogHeader>
        <div className="overflow-hidden rounded-md border">
          <CodeMirror value={value} onChange={setValue} extensions={[yamlLanguage(), EditorView.lineWrapping]} editable={!save.isPending} height="56vh" theme={theme} basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true }} />
        </div>
        <p className="text-xs text-muted-foreground">服务端会校验 GVR、名称、命名空间、单文档和 1 MiB 上限。正式创建或保存前会自动执行一次 API Server Dry Run，并记录 RBAC 校验和审计。Secret 通用写入已关闭，请使用 Secret 专用页面。</p>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button>
          <Button variant="outline" onClick={() => save.mutate(true)} disabled={save.isPending || !value.trim()}><CheckCircle2 className="h-4 w-4" />Dry Run</Button>
          <Button onClick={() => save.mutate(false)} disabled={save.isPending || !value.trim()}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}{mode === "create" ? "创建" : "保存"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
