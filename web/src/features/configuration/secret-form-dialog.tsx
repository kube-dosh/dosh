import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { SecretReveal } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type DataRow = { key: string; value: string };
type FormState = { name: string; type: string; data: DataRow[] };
const SECRET_TYPES = ["Opaque", "kubernetes.io/tls", "kubernetes.io/dockerconfigjson", "kubernetes.io/basic-auth", "kubernetes.io/ssh-auth"] as const;

function initialForm(item?: SecretReveal): FormState {
  return {
    name: item?.name || "",
    type: item?.type || "Opaque",
    data: item && Object.keys(item.data || {}).length ? Object.entries(item.data).map(([key, value]) => ({ key, value })) : [{ key: "", value: "" }],
  };
}

export function SecretFormDialog({ open, onOpenChange, namespace, item, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  namespace: string;
  item?: SecretReveal;
  onSaved: () => Promise<void> | void;
}) {
  const creating = !item;
  const [form, setForm] = useState<FormState>(() => initialForm(item));
  const [formError, setFormError] = useState("");
  useEffect(() => { if (open) { setForm(initialForm(item)); setFormError(""); } }, [open, item]);
  const save = useMutation({
    mutationFn: async () => {
      if (creating && !/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(form.name)) throw new Error("Secret 名称必须是小写 DNS-1123 标签");
      const data: Record<string, string> = {};
      for (const row of form.data) {
        const key = row.key.trim();
        if (!key) continue;
        if (Object.prototype.hasOwnProperty.call(data, key)) throw new Error(`键 ${key} 重复`);
        try { atob(row.value); } catch { throw new Error(`键 ${key} 的值不是有效 Base64`); }
        data[key] = row.value;
      }
      if (creating) return api.createSecret({ name: form.name, namespace, type: form.type, data });
      return api.updateSecret(namespace, item.name, { type: form.type, data, resourceVersion: item.resourceVersion });
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `Secret ${saved.name} 已创建` : `Secret ${saved.name} 已更新`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => setFormError(error instanceof ApiError && error.status === 409 ? "Secret 已被其他操作更新，请关闭后刷新再试" : error.message),
  });
  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };
  const updateRow = (index: number, patch: Partial<DataRow>) => update({ data: form.data.map((row, rowIndex) => rowIndex === index ? { ...row, ...patch } : row) });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader><DialogTitle>{creating ? "创建 Secret" : "编辑 Secret"}</DialogTitle><DialogDescription>{namespace}{item ? ` / ${item.name}` : ""} · 值使用 Base64，避免二进制内容损坏。TLS 类型需包含 tls.crt / tls.key。</DialogDescription></DialogHeader>
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="secret-name">Secret 名称</Label><Input id="secret-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} /></div>
            <div className="space-y-2"><Label htmlFor="secret-type">类型</Label><Select value={form.type} onValueChange={(value) => update({ type: value })}><SelectTrigger id="secret-type" aria-label="Secret 类型"><SelectValue /></SelectTrigger><SelectContent>{SECRET_TYPES.map((type) => <SelectItem key={type} value={type}>{type}</SelectItem>)}</SelectContent></Select></div>
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3"><div><Label>数据</Label><p className="text-xs text-muted-foreground">值为 Base64；留空键的行会被忽略。</p></div><Button type="button" variant="outline" size="sm" onClick={() => update({ data: [...form.data, { key: "", value: "" }] })}><Plus className="h-4 w-4" />添加键值</Button></div>
            {form.data.map((row, index) => (
              <div key={index} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,0.7fr)_minmax(0,1.5fr)_auto] sm:items-end">
                <div className="space-y-1.5"><Label htmlFor={`secret-key-${index}`} className="text-xs">键</Label><Input id={`secret-key-${index}`} value={row.key} onChange={(event) => updateRow(index, { key: event.target.value })} placeholder="tls.crt" /></div>
                <div className="space-y-1.5"><Label htmlFor={`secret-value-${index}`} className="text-xs">Base64 值</Label><Input id={`secret-value-${index}`} value={row.value} onChange={(event) => updateRow(index, { value: event.target.value })} className="font-mono text-xs" spellCheck={false} /></div>
                <Button type="button" variant="ghost" size="icon" aria-label={`删除键值 ${index + 1}`} onClick={() => update({ data: form.data.filter((_, rowIndex) => rowIndex !== index) })} disabled={form.data.length === 1}><Trash2 className="h-4 w-4" /></Button>
              </div>
            ))}
          </div>
          {formError ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button><Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{creating ? "创建 Secret" : "保存修改"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
