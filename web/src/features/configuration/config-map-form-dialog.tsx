import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { ConfigMapDetail } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

type DataRow = { key: string; value: string };
type FormState = { name: string; data: DataRow[] };

function initialForm(item?: ConfigMapDetail): FormState {
  return {
    name: item?.name || "",
    data: item && Object.keys(item.data || {}).length ? Object.entries(item.data).map(([key, value]) => ({ key, value })) : [{ key: "", value: "" }],
  };
}

export function ConfigMapFormDialog({ open, onOpenChange, namespace, item, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  namespace: string;
  item?: ConfigMapDetail;
  onSaved: () => Promise<void> | void;
}) {
  const creating = !item;
  const [form, setForm] = useState<FormState>(() => initialForm(item));
  const [formError, setFormError] = useState("");
  useEffect(() => { if (open) { setForm(initialForm(item)); setFormError(""); } }, [open, item]);
  const save = useMutation({
    mutationFn: async () => {
      if (creating && !/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(form.name)) throw new Error("ConfigMap 名称必须是小写 DNS-1123 标签");
      const data: Record<string, string> = {};
      for (const row of form.data) {
        const key = row.key.trim();
        if (!key) continue;
        if (Object.prototype.hasOwnProperty.call(data, key)) throw new Error(`键 ${key} 重复`);
        data[key] = row.value;
      }
      if (creating) return api.createConfigMap({ name: form.name, namespace, data });
      return api.updateConfigMap(namespace, item.name, { data, resourceVersion: item.resourceVersion });
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `ConfigMap ${saved.name} 已创建` : `ConfigMap ${saved.name} 已更新`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => setFormError(error instanceof ApiError && error.status === 409 ? "ConfigMap 已被其他操作更新，请关闭后刷新再试" : error.message),
  });
  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };
  const updateRow = (index: number, patch: Partial<DataRow>) => update({ data: form.data.map((row, rowIndex) => rowIndex === index ? { ...row, ...patch } : row) });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader><DialogTitle>{creating ? "创建 ConfigMap" : "编辑 ConfigMap"}</DialogTitle><DialogDescription>{namespace}{item ? ` / ${item.name}` : ""} · 维护键值数据；二进制内容请使用 YAML。</DialogDescription></DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2"><Label htmlFor="configmap-name">ConfigMap 名称</Label><Input id="configmap-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} /></div>
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3"><div><Label>数据</Label><p className="text-xs text-muted-foreground">留空键的行会被忽略。</p></div><Button type="button" variant="outline" size="sm" onClick={() => update({ data: [...form.data, { key: "", value: "" }] })}><Plus className="h-4 w-4" />添加键值</Button></div>
            {form.data.map((row, index) => (
              <div key={index} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start">
                <div className="grid min-w-0 gap-2 md:grid-cols-[minmax(0,0.8fr)_minmax(0,1.4fr)]">
                  <div className="space-y-1.5"><Label htmlFor={`configmap-key-${index}`} className="text-xs">键</Label><Input id={`configmap-key-${index}`} value={row.key} onChange={(event) => updateRow(index, { key: event.target.value })} placeholder="app.properties" /></div>
                  <div className="space-y-1.5"><Label htmlFor={`configmap-value-${index}`} className="text-xs">值</Label><Textarea id={`configmap-value-${index}`} value={row.value} onChange={(event) => updateRow(index, { value: event.target.value })} className="min-h-20 font-mono text-xs" spellCheck={false} /></div>
                </div>
                <Button type="button" variant="ghost" size="icon" aria-label={`删除键值 ${index + 1}`} onClick={() => update({ data: form.data.filter((_, rowIndex) => rowIndex !== index) })} disabled={form.data.length === 1}><Trash2 className="h-4 w-4" /></Button>
              </div>
            ))}
          </div>
          {formError ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button><Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{creating ? "创建 ConfigMap" : "保存修改"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
