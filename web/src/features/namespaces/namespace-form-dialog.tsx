import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

type FormState = { name: string; labels: string; annotations: string };

function parseMetadata(value: string): Record<string, string> | null {
  const result: Record<string, string> = {};
  for (const rawLine of value.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const separator = line.indexOf("=");
    if (separator <= 0) return null;
    const key = line.slice(0, separator).trim();
    const itemValue = line.slice(separator + 1).trim();
    if (!key || Object.prototype.hasOwnProperty.call(result, key)) return null;
    result[key] = itemValue;
  }
  return result;
}

export function NamespaceFormDialog({ open, onOpenChange, onCreated }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => Promise<void> | void;
}) {
  const [form, setForm] = useState<FormState>({ name: "", labels: "", annotations: "" });
  const [formError, setFormError] = useState("");
  useEffect(() => {
    if (!open) return;
    setForm({ name: "", labels: "", annotations: "" });
    setFormError("");
  }, [open]);

  const create = useMutation({
    mutationFn: async () => {
      const name = form.name.trim();
      if (!name || name.length > 63 || !DNS_LABEL.test(name)) throw new Error("命名空间名称必须是小写 DNS-1123 标签，且不超过 63 个字符");
      const labels = parseMetadata(form.labels);
      const annotations = parseMetadata(form.annotations);
      if (!labels) throw new Error("Labels 格式应为每行 key=value，且 key 不能重复");
      if (!annotations) throw new Error("Annotations 格式应为每行 key=value，且 key 不能重复");
      return api.createNamespace({ name, labels, annotations });
    },
    onSuccess: async (created) => {
      toast.success(`命名空间 ${created.name} 已创建`);
      await onCreated();
      onOpenChange(false);
    },
    onError: (error) => setFormError(error.message),
  });
  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>创建命名空间</DialogTitle>
          <DialogDescription>命名空间用于隔离工作负载、配置、Secret 和访问策略。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="namespace-name">名称</Label>
            <Input id="namespace-name" value={form.name} onChange={(event) => update({ name: event.target.value })} placeholder="production" autoComplete="off" spellCheck={false} />
            <p className="text-xs text-muted-foreground">创建后名称不可修改。</p>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="namespace-labels">Labels</Label>
              <Textarea id="namespace-labels" value={form.labels} onChange={(event) => update({ labels: event.target.value })} className="min-h-28 font-mono text-xs" placeholder={"team=platform\nenv=production"} spellCheck={false} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="namespace-annotations">Annotations</Label>
              <Textarea id="namespace-annotations" value={form.annotations} onChange={(event) => update({ annotations: event.target.value })} className="min-h-28 font-mono text-xs" placeholder={"owner=ops\ncontact=ops@example.com"} spellCheck={false} />
            </div>
          </div>
          <p className="text-xs text-muted-foreground">每行一个 key=value，可留空。</p>
          {formError ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={create.isPending}>取消</Button>
          <Button onClick={() => create.mutate()} disabled={create.isPending}>
            {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            创建命名空间
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
