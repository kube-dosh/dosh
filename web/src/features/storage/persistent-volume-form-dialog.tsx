import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { PersistentVolumeDetail, StorageClass } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

const ACCESS_MODES = ["ReadWriteOnce", "ReadOnlyMany", "ReadWriteMany", "ReadWriteOncePod"] as const;
const VOLUME_MODES = ["Filesystem", "Block"] as const;
const CAPACITY_UNITS = ["Mi", "Gi", "Ti"] as const;
const UNIT_BYTES: Record<(typeof CAPACITY_UNITS)[number], number> = { Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4 };
const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
type CapacityUnit = (typeof CAPACITY_UNITS)[number];
type FormState = {
  name: string;
  capacity: string;
  unit: CapacityUnit;
  accessModes: string[];
  volumeMode: string;
  storageClass: string;
  reclaimPolicy: string;
  csiDriver: string;
  csiVolumeHandle: string;
  csiFSType: string;
  csiReadOnly: boolean;
  mountOptions: string;
};

function capacityDraft(bytes?: number): Pick<FormState, "capacity" | "unit"> {
  if (!bytes || bytes <= 0) return { capacity: "10", unit: "Gi" };
  for (const unit of ["Ti", "Gi", "Mi"] as const) {
    const factor = UNIT_BYTES[unit];
    if (bytes >= factor && bytes % factor === 0) return { capacity: String(bytes / factor), unit };
  }
  return { capacity: String(Math.max(1, Math.ceil(bytes / UNIT_BYTES.Mi))), unit: "Mi" };
}

function initialForm(item?: PersistentVolumeDetail): FormState {
  return {
    name: item?.name ?? "",
    ...capacityDraft(item?.capacityBytes),
    accessModes: item?.accessModes?.length ? item.accessModes : ["ReadWriteOnce"],
    volumeMode: item?.volumeMode || "Filesystem",
    storageClass: item?.storageClass || "",
    reclaimPolicy: item?.reclaimPolicy || "Retain",
    csiDriver: item?.source?.csiDriver ?? item?.csiDriver ?? "",
    csiVolumeHandle: item?.source?.csiVolumeHandle ?? "",
    csiFSType: item?.source?.csiFsType ?? "",
    csiReadOnly: item?.source?.csiReadOnly ?? false,
    mountOptions: (item?.mountOptions ?? []).join(", "),
  };
}

function parseCapacity(form: FormState): number | null {
  if (!/^\d+$/.test(form.capacity.trim())) return null;
  const bytes = Number(form.capacity) * UNIT_BYTES[form.unit];
  return Number.isSafeInteger(bytes) && bytes > 0 && bytes <= 2 ** 50 ? bytes : null;
}

export function PersistentVolumeFormDialog({ open, onOpenChange, item, storageClasses, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  item?: PersistentVolumeDetail;
  storageClasses: StorageClass[];
  onSaved: () => Promise<void> | void;
}) {
  const creating = !item;
  const [form, setForm] = useState<FormState>(() => initialForm(item));
  const [formError, setFormError] = useState("");
  useEffect(() => { if (open) { setForm(initialForm(item)); setFormError(""); } }, [open, item]);
  const save = useMutation({
    mutationFn: async () => {
      if (creating && !DNS_LABEL.test(form.name)) throw new Error("PersistentVolume 名称必须是小写 DNS-1123 标签");
      const capacityBytes = parseCapacity(form);
      if (!capacityBytes) throw new Error("容量必须是大于 0 的整数，且不超过 1 PiB");
      if (!form.accessModes.length) throw new Error("请至少选择一种访问模式");
      const mountOptions = form.mountOptions.split(",").map((value) => value.trim()).filter(Boolean);
      if (creating) {
        if (!form.csiDriver.trim()) throw new Error("CSI Driver 不能为空");
        if (!form.csiVolumeHandle.trim()) throw new Error("CSI VolumeHandle 不能为空");
        return api.createPersistentVolume({
          name: form.name,
          capacityBytes,
          accessModes: form.accessModes,
          reclaimPolicy: form.reclaimPolicy,
          volumeMode: form.volumeMode,
          storageClass: form.storageClass || undefined,
          mountOptions,
          csi: { driver: form.csiDriver.trim(), volumeHandle: form.csiVolumeHandle.trim(), fsType: form.csiFSType.trim() || undefined, readOnly: form.csiReadOnly },
        });
      }
      if (capacityBytes < item.capacityBytes) throw new Error("PersistentVolume 容量不能缩小");
      return api.updatePersistentVolume(item.name, {
        capacityBytes,
        reclaimPolicy: form.reclaimPolicy,
        mountOptions,
        labels: item.labels,
        annotations: item.annotations,
        resourceVersion: item.resourceVersion ?? "",
      });
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `PersistentVolume ${saved.name} 已创建` : `PersistentVolume ${saved.name} 已更新`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => setFormError(error instanceof ApiError && error.status === 409 ? "PersistentVolume 已被其他操作更新，请关闭后刷新再试" : error.message),
  });
  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };
  const toggleAccessMode = (mode: string) => {
    if (!creating) return;
    update({ accessModes: form.accessModes.includes(mode) ? form.accessModes.filter((value) => value !== mode) : [...form.accessModes, mode] });
  };
  const classOptions = form.storageClass && !storageClasses.some((storageClass) => storageClass.name === form.storageClass)
    ? [{ name: form.storageClass, provisioner: "当前值", default: false } as StorageClass, ...storageClasses]
    : storageClasses;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{creating ? "创建 PersistentVolume" : "编辑 PersistentVolume"}</DialogTitle>
          <DialogDescription className="break-words">
            {creating ? "以结构化 CSI 卷源创建集群级 PV。创建后卷源、访问模式和卷模式不可修改。" : `${item.name} · 可调整容量、回收策略、挂载选项和元数据；绑定关系与卷源保持不变。`}
          </DialogDescription>
        </DialogHeader>
        <div className="min-w-0 space-y-5">
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="pv-name">名称</Label><Input id="pv-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} /></div>
            <div className="space-y-2"><Label htmlFor="pv-class">StorageClass</Label><Select value={form.storageClass || "__none__"} onValueChange={(value) => update({ storageClass: value === "__none__" ? "" : value })} disabled={!creating}><SelectTrigger id="pv-class"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="__none__">不指定</SelectItem>{classOptions.map((storageClass) => <SelectItem key={storageClass.name} value={storageClass.name}>{storageClass.name}{storageClass.default ? " · Default" : ""}</SelectItem>)}</SelectContent></Select></div>
          </div>
          <div className="grid min-w-0 gap-4 sm:grid-cols-[minmax(0,1fr)_140px]">
            <div className="space-y-2"><Label htmlFor="pv-capacity">容量</Label><Input id="pv-capacity" inputMode="numeric" value={form.capacity} onChange={(event) => update({ capacity: event.target.value })} /></div>
            <div className="space-y-2"><Label htmlFor="pv-unit">单位</Label><Select value={form.unit} onValueChange={(value) => update({ unit: value as CapacityUnit })}><SelectTrigger id="pv-unit"><SelectValue /></SelectTrigger><SelectContent>{CAPACITY_UNITS.map((unit) => <SelectItem key={unit} value={unit}>{unit}</SelectItem>)}</SelectContent></Select></div>
          </div>
          <div className="space-y-2"><Label>访问模式</Label><div className="flex flex-wrap gap-2" role="group" aria-label="PV 访问模式">{ACCESS_MODES.map((mode) => <Button key={mode} type="button" size="sm" variant={form.accessModes.includes(mode) ? "default" : "outline"} aria-pressed={form.accessModes.includes(mode)} onClick={() => toggleAccessMode(mode)} disabled={!creating} className={cn("font-mono text-xs", !creating && "opacity-100")}>{mode}</Button>)}</div></div>
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="pv-volume-mode">卷模式</Label><Select value={form.volumeMode} onValueChange={(value) => update({ volumeMode: value })} disabled={!creating}><SelectTrigger id="pv-volume-mode"><SelectValue /></SelectTrigger><SelectContent>{VOLUME_MODES.map((mode) => <SelectItem key={mode} value={mode}>{mode}</SelectItem>)}</SelectContent></Select></div>
            <div className="space-y-2"><Label htmlFor="pv-reclaim">回收策略</Label><Select value={form.reclaimPolicy} onValueChange={(value) => update({ reclaimPolicy: value })}><SelectTrigger id="pv-reclaim"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="Retain">Retain</SelectItem><SelectItem value="Delete">Delete</SelectItem><SelectItem value="Recycle">Recycle</SelectItem></SelectContent></Select></div>
          </div>
          <div className="min-w-0 rounded-md border p-3">
            <div className="mb-3"><Label>CSI 卷源</Label><p className="text-xs text-muted-foreground">{creating ? "创建时必须提供驱动与后端卷句柄。" : "卷源创建后不可修改。"}</p></div>
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5"><Label htmlFor="pv-csi-driver" className="text-xs">CSI Driver</Label><Input id="pv-csi-driver" value={form.csiDriver} onChange={(event) => update({ csiDriver: event.target.value })} disabled={!creating} placeholder="csi.example.com" autoComplete="off" spellCheck={false} /></div>
              <div className="space-y-1.5"><Label htmlFor="pv-csi-handle" className="text-xs">VolumeHandle</Label><Input id="pv-csi-handle" value={form.csiVolumeHandle} onChange={(event) => update({ csiVolumeHandle: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} /></div>
              <div className="space-y-1.5"><Label htmlFor="pv-csi-fs" className="text-xs">文件系统类型</Label><Input id="pv-csi-fs" value={form.csiFSType} onChange={(event) => update({ csiFSType: event.target.value })} disabled={!creating} placeholder="ext4" autoComplete="off" spellCheck={false} /></div>
              <div className="flex items-end"><Button type="button" variant={form.csiReadOnly ? "default" : "outline"} aria-pressed={form.csiReadOnly} onClick={() => update({ csiReadOnly: !form.csiReadOnly })} disabled={!creating} className="w-full whitespace-normal">{form.csiReadOnly ? "只读卷源" : "读写卷源"}</Button></div>
            </div>
          </div>
          <div className="space-y-2"><Label htmlFor="pv-mount-options">挂载选项</Label><Input id="pv-mount-options" value={form.mountOptions} onChange={(event) => update({ mountOptions: event.target.value })} placeholder="discard, noatime" autoComplete="off" spellCheck={false} /><p className="text-xs text-muted-foreground">使用逗号分隔；留空表示不设置。</p></div>
          {item?.status === "Bound" ? <p className="break-words rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">该 PV 已绑定 PVC。修改容量或回收策略会影响 PVC 删除后的卷处理；当前有 {item.impact?.mountCount ?? 0} 个 Pod 挂载使用。</p> : null}
          {formError ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button><Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{creating ? "创建 PersistentVolume" : "保存修改"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
