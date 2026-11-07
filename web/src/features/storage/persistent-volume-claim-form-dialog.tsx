import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { PersistentVolumeClaimDetail, StorageClass } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

const ACCESS_MODES = ["ReadWriteOnce", "ReadOnlyMany", "ReadWriteMany", "ReadWriteOncePod"] as const;
const VOLUME_MODES = ["Filesystem", "Block"] as const;
const CAPACITY_UNITS = ["Mi", "Gi", "Ti"] as const;
const UNIT_BYTES: Record<(typeof CAPACITY_UNITS)[number], number> = {
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
};
const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

type CapacityUnit = (typeof CAPACITY_UNITS)[number];
type FormState = {
  name: string;
  storageClass: string;
  accessModes: string[];
  volumeMode: string;
  capacity: string;
  unit: CapacityUnit;
};

function capacityDraft(bytes?: number): Pick<FormState, "capacity" | "unit"> {
  if (!bytes || bytes <= 0) return { capacity: "1", unit: "Gi" };
  for (const unit of ["Ti", "Gi", "Mi"] as const) {
    const factor = UNIT_BYTES[unit];
    if (bytes >= factor && bytes % factor === 0) return { capacity: String(bytes / factor), unit };
  }
  return { capacity: String(Math.max(1, Math.ceil(bytes / UNIT_BYTES.Mi))), unit: "Mi" };
}

function initialForm(item?: PersistentVolumeClaimDetail): FormState {
  const capacity = capacityDraft(item?.requestedCapacityBytes);
  return {
    name: item?.name || "",
    storageClass: item?.storageClass || "",
    accessModes: item?.accessModes?.length ? item.accessModes : ["ReadWriteOnce"],
    volumeMode: item?.volumeMode || "Filesystem",
    ...capacity,
  };
}

function parseCapacity(form: FormState): number | null {
  if (!/^\d+$/.test(form.capacity.trim())) return null;
  const value = Number(form.capacity);
  const bytes = value * UNIT_BYTES[form.unit];
  return Number.isSafeInteger(bytes) && bytes > 0 && bytes <= 2 ** 50 ? bytes : null;
}

export function PersistentVolumeClaimFormDialog({ open, onOpenChange, namespace, item, storageClasses, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  namespace: string;
  item?: PersistentVolumeClaimDetail;
  storageClasses: StorageClass[];
  onSaved: () => Promise<void> | void;
}) {
  const creating = !item;
  const [form, setForm] = useState<FormState>(() => initialForm(item));
  const [formError, setFormError] = useState("");
  useEffect(() => {
    if (!open) return;
    setForm(initialForm(item));
    setFormError("");
  }, [open, item]);

  const save = useMutation({
    mutationFn: async () => {
      if (creating && !DNS_LABEL.test(form.name)) throw new Error("PVC 名称必须是小写 DNS-1123 标签");
      const requestedCapacityBytes = parseCapacity(form);
      if (!requestedCapacityBytes) throw new Error("容量必须是大于 0 的整数，且不超过 1 PiB");
      if (!form.accessModes.length) throw new Error("请至少选择一种访问模式");
      if (creating) {
        return api.createPersistentVolumeClaim({
          name: form.name,
          namespace,
          storageClass: form.storageClass || undefined,
          accessModes: form.accessModes,
          requestedCapacityBytes,
          volumeMode: form.volumeMode,
        });
      }
      if (requestedCapacityBytes < item.requestedCapacityBytes) throw new Error("PVC 容量只能扩容，不能缩容");
      return api.updatePersistentVolumeClaim(namespace, item.name, {
        requestedCapacityBytes,
        resourceVersion: item.resourceVersion,
      });
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `PVC ${saved.name} 已创建` : `PVC ${saved.name} 扩容请求已提交`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => {
      setFormError(error instanceof ApiError && error.status === 409 ? "PVC 已被其他操作更新，请关闭后刷新再试" : error.message);
    },
  });

  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };
  const toggleAccessMode = (mode: string) => {
    if (!creating) return;
    update({ accessModes: form.accessModes.includes(mode) ? form.accessModes.filter((itemMode) => itemMode !== mode) : [...form.accessModes, mode] });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{creating ? "创建 PersistentVolumeClaim" : "扩容 PersistentVolumeClaim"}</DialogTitle>
          <DialogDescription>
            {namespace}{item ? ` / ${item.name}` : ""} · {creating ? "填写存储容量和访问模式，绑定结果取决于 StorageClass 与存储驱动。" : "StorageClass、访问模式和卷模式创建后不可修改；容量只能增加。"}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="pvc-name">PVC 名称</Label>
              <Input id="pvc-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pvc-storage-class">StorageClass</Label>
              <Select value={form.storageClass || "__default__"} onValueChange={(value) => update({ storageClass: value === "__default__" ? "" : value })} disabled={!creating}>
                <SelectTrigger id="pvc-storage-class" aria-label="StorageClass"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="__default__">使用集群默认</SelectItem>
                  {storageClasses.map((storageClass) => <SelectItem key={storageClass.name} value={storageClass.name}>{storageClass.name}{storageClass.default ? " · Default" : ""}</SelectItem>)}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">无 StorageClass 时可创建静态绑定 PVC，但可能保持 Pending；创建后不可修改。</p>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_140px]">
            <div className="space-y-2">
              <Label htmlFor="pvc-capacity">申请容量</Label>
              <Input id="pvc-capacity" inputMode="numeric" value={form.capacity} onChange={(event) => update({ capacity: event.target.value })} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pvc-capacity-unit">单位</Label>
              <Select value={form.unit} onValueChange={(value) => update({ unit: value as CapacityUnit })}>
                <SelectTrigger id="pvc-capacity-unit" aria-label="容量单位"><SelectValue /></SelectTrigger>
                <SelectContent>{CAPACITY_UNITS.map((unit) => <SelectItem key={unit} value={unit}>{unit}</SelectItem>)}</SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-2">
            <Label>访问模式</Label>
            <div className="flex flex-wrap gap-2" role="group" aria-label="访问模式">
              {ACCESS_MODES.map((mode) => (
                <Button
                  key={mode}
                  type="button"
                  size="sm"
                  variant={form.accessModes.includes(mode) ? "default" : "outline"}
                  aria-pressed={form.accessModes.includes(mode)}
                  onClick={() => toggleAccessMode(mode)}
                  disabled={!creating}
                  className={cn("font-mono text-xs", !creating && "opacity-100")}
                >
                  {mode}
                </Button>
              ))}
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="pvc-volume-mode">卷模式</Label>
            <Select value={form.volumeMode} onValueChange={(value) => update({ volumeMode: value })} disabled={!creating}>
              <SelectTrigger id="pvc-volume-mode" aria-label="卷模式"><SelectValue /></SelectTrigger>
              <SelectContent>{VOLUME_MODES.map((mode) => <SelectItem key={mode} value={mode}>{mode}</SelectItem>)}</SelectContent>
            </Select>
          </div>

          {formError ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button>
          <Button onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {creating ? "创建 PVC" : "提交扩容"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
