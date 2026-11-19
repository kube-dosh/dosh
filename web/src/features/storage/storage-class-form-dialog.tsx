import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { StorageClassDetail, StorageClassWriteInput } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const RECLAIM_POLICIES = ["Delete", "Retain"] as const;
const BINDING_MODES = ["Immediate", "WaitForFirstConsumer"] as const;
type KeyValueRow = { key: string; value: string };
type FormState = {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBinding: string;
  allowVolumeExpansion: boolean;
  default: boolean;
  parameters: KeyValueRow[];
  mountOptions: KeyValueRow[];
};

function initialForm(item?: StorageClassDetail): FormState {
  return {
    name: item?.name ?? "",
    provisioner: item?.provisioner ?? "",
    reclaimPolicy: item?.reclaimPolicy || "Delete",
    volumeBinding: item?.volumeBinding || "Immediate",
    allowVolumeExpansion: item?.allowVolumeExpansion ?? false,
    default: item?.default ?? false,
    parameters: Object.entries(item?.parameters ?? {}).map(([key, value]) => ({ key, value })),
    mountOptions: (item?.mountOptions ?? []).map((value) => ({ key: value, value })),
  };
}

function toMap(rows: KeyValueRow[], label: string): Record<string, string> {
  const result: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (Object.prototype.hasOwnProperty.call(result, key)) throw new Error(`${label} ${key} 重复`);
    result[key] = row.value;
  }
  return result;
}

export function StorageClassFormDialog({ open, onOpenChange, item, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  item?: StorageClassDetail;
  onSaved: () => Promise<void> | void;
}) {
  const creating = !item;
  const [form, setForm] = useState<FormState>(() => initialForm(item));
  const [formError, setFormError] = useState("");
  useEffect(() => { if (open) { setForm(initialForm(item)); setFormError(""); } }, [open, item]);
  const save = useMutation({
    mutationFn: async () => {
      if (creating && !DNS_LABEL.test(form.name)) throw new Error("StorageClass 名称必须是小写 DNS-1123 标签");
      if (!form.provisioner.trim()) throw new Error("Provisioner 不能为空");
      const parameters = toMap(form.parameters, "参数");
      const mountOptions = form.mountOptions.map((row) => row.key.trim()).filter(Boolean);
      if (new Set(mountOptions).size !== mountOptions.length) throw new Error("挂载选项不能重复");
      const input: StorageClassWriteInput = {
        name: form.name,
        provisioner: form.provisioner.trim(),
        reclaimPolicy: form.reclaimPolicy,
        volumeBinding: form.volumeBinding,
        allowVolumeExpansion: form.allowVolumeExpansion,
        default: form.default,
        parameters,
        mountOptions,
        labels: item?.labels,
        annotations: item?.annotations,
        resourceVersion: item?.resourceVersion,
      };
      return creating ? api.createStorageClass(input) : api.updateStorageClass(item.name, input);
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `StorageClass ${saved.name} 已创建` : `StorageClass ${saved.name} 已更新`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => setFormError(error instanceof ApiError && error.status === 409 ? "StorageClass 已被其他操作更新，请关闭后刷新再试" : error.message),
  });
  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };
  const updateRow = (field: "parameters" | "mountOptions", index: number, patch: Partial<KeyValueRow>) =>
    update({ [field]: form[field].map((row, rowIndex) => rowIndex === index ? { ...row, ...patch } : row) });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{creating ? "创建 StorageClass" : "编辑 StorageClass"}</DialogTitle>
          <DialogDescription className="break-words">
            {creating ? "定义动态供应器、绑定模式和扩容策略。参数由 CSI 驱动解释。" : `${item.name} · Provisioner 创建后不可修改；策略变更只影响后续供应和扩容请求。`}
          </DialogDescription>
        </DialogHeader>
        <div className="min-w-0 space-y-5">
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="sc-name">名称</Label><Input id="sc-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} /></div>
            <div className="space-y-2"><Label htmlFor="sc-provisioner">Provisioner</Label><Input id="sc-provisioner" value={form.provisioner} onChange={(event) => update({ provisioner: event.target.value })} disabled={!creating} placeholder="csi.example.com" autoComplete="off" spellCheck={false} /></div>
          </div>
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="sc-reclaim">回收策略</Label><Select value={form.reclaimPolicy} onValueChange={(value) => update({ reclaimPolicy: value })}><SelectTrigger id="sc-reclaim"><SelectValue /></SelectTrigger><SelectContent>{RECLAIM_POLICIES.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent></Select></div>
            <div className="space-y-2"><Label htmlFor="sc-binding">卷绑定模式</Label><Select value={form.volumeBinding} onValueChange={(value) => update({ volumeBinding: value })}><SelectTrigger id="sc-binding"><SelectValue /></SelectTrigger><SelectContent>{BINDING_MODES.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent></Select></div>
          </div>
          <div className="grid min-w-0 gap-2 sm:grid-cols-2">
            <Button type="button" variant={form.allowVolumeExpansion ? "default" : "outline"} aria-pressed={form.allowVolumeExpansion} className="h-auto min-w-0 justify-start whitespace-normal px-3 py-2.5 text-left" onClick={() => update({ allowVolumeExpansion: !form.allowVolumeExpansion })}>
              <span><span className="block text-sm">允许在线扩容</span><span className="mt-0.5 block text-xs opacity-75">allowVolumeExpansion=true</span></span>
            </Button>
            <Button type="button" variant={form.default ? "default" : "outline"} aria-pressed={form.default} className="h-auto min-w-0 justify-start whitespace-normal px-3 py-2.5 text-left" onClick={() => update({ default: !form.default })}>
              <span><span className="block text-sm">设为默认 StorageClass</span><span className="mt-0.5 block text-xs opacity-75">新的无 class PVC 将默认使用它</span></span>
            </Button>
          </div>
          <KeyValueEditor title="存储参数" description="由 CSI Provisioner 解释，例如 type、fsType、encrypted。" rows={form.parameters} onChange={(rows) => update({ parameters: rows })} onRowChange={(index, patch) => updateRow("parameters", index, patch)} valueLabel="参数值" />
          <KeyValueEditor title="挂载选项" description="每一行一个 mount option。" rows={form.mountOptions} onChange={(rows) => update({ mountOptions: rows })} onRowChange={(index, patch) => updateRow("mountOptions", index, patch)} valueLabel="挂载选项" singleValue />
          {!creating && item.allowVolumeExpansion && !form.allowVolumeExpansion ? <p className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">关闭扩容后，已有 PVC 的后续扩容请求将被拒绝；已提交的扩容不会回滚。</p> : null}
          {formError ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button><Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{creating ? "创建 StorageClass" : "保存修改"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function KeyValueEditor({ title, description, rows, onChange, onRowChange, valueLabel, singleValue = false }: {
  title: string;
  description: string;
  rows: KeyValueRow[];
  onChange: (rows: KeyValueRow[]) => void;
  onRowChange: (index: number, patch: Partial<KeyValueRow>) => void;
  valueLabel: string;
  singleValue?: boolean;
}) {
  return (
    <div className="min-w-0 space-y-3">
      <div className="flex min-w-0 flex-col justify-between gap-2 sm:flex-row sm:items-center"><div className="min-w-0"><Label>{title}</Label><p className="text-xs text-muted-foreground">{description}</p></div><Button type="button" variant="outline" size="sm" onClick={() => onChange([...rows, { key: "", value: "" }])}><Plus className="h-4 w-4" />添加</Button></div>
      {rows.length ? rows.map((row, index) => (
        <div key={index} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start">
          <div className={cn("grid min-w-0 gap-2", !singleValue && "md:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]")}>
            <div className="space-y-1.5"><Label htmlFor={`sc-${title}-${index}`} className="text-xs">{singleValue ? valueLabel : "键"}</Label><Input id={`sc-${title}-${index}`} value={row.key} onChange={(event) => onRowChange(index, { key: event.target.value })} autoComplete="off" spellCheck={false} /></div>
            {!singleValue ? <div className="space-y-1.5"><Label htmlFor={`sc-${title}-value-${index}`} className="text-xs">{valueLabel}</Label><Input id={`sc-${title}-value-${index}`} value={row.value} onChange={(event) => onRowChange(index, { value: event.target.value })} autoComplete="off" /></div> : null}
          </div>
          <Button type="button" variant="ghost" size="icon" aria-label={`删除 ${title} ${index + 1}`} onClick={() => onChange(rows.filter((_, rowIndex) => rowIndex !== index))}><Trash2 className="h-4 w-4" /></Button>
        </div>
      )) : <p className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">未配置</p>}
    </div>
  );
}
