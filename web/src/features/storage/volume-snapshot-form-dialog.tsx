import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { PersistentVolumeClaim, VolumeSnapshotFeature } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

function generatedName(sourcePVC: string): string {
  const stamp = new Date().toISOString().replace(/\D/g, "").slice(0, 14);
  const prefix = sourcePVC.slice(0, Math.max(1, 52));
  return `${prefix}-${stamp}`.replace(/-+$/, "").slice(0, 63).replace(/-+$/, "");
}

export function VolumeSnapshotFormDialog({ open, onOpenChange, namespace, sourcePVC: initialSource, claims, feature, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  namespace: string;
  sourcePVC?: string;
  claims: PersistentVolumeClaim[];
  feature?: VolumeSnapshotFeature;
  onSaved: () => Promise<void> | void;
}) {
  const [name, setName] = useState("");
  const [sourcePVC, setSourcePVC] = useState("");
  const [className, setClassName] = useState("");
  const [manualSource, setManualSource] = useState(false);
  const [manualClass, setManualClass] = useState(false);
  const [nameTouched, setNameTouched] = useState(false);
  const [formError, setFormError] = useState("");
  useEffect(() => {
    if (!open) return;
    const source = initialSource ?? "";
    setName(source ? generatedName(source) : "");
    setSourcePVC(source);
    setClassName(feature?.defaultClass ?? "");
    setManualSource(false);
    setManualClass(false);
    setNameTouched(false);
    setFormError("");
  }, [feature?.defaultClass, initialSource, open]);
  const chooseSource = (value: string) => {
    setSourcePVC(value);
    if (!nameTouched) setName(generatedName(value));
    setFormError("");
  };
  const save = useMutation({
    mutationFn: async () => {
      if (!namespace) throw new Error("请先在页面顶部选择命名空间");
      if (!sourcePVC.trim()) throw new Error("请选择或输入源 PVC");
      if (!DNS_LABEL.test(sourcePVC.trim())) throw new Error("源 PVC 名称必须是 DNS-1123 标签");
      if (!DNS_LABEL.test(name)) throw new Error("快照名称必须是 DNS-1123 标签");
      if (name.length > 63) throw new Error("快照名称不能超过 63 个字符");
      return api.createVolumeSnapshot({ name, namespace, sourcePvc: sourcePVC.trim(), volumeSnapshotClassName: className.trim() || undefined });
    },
    onSuccess: async (saved) => {
      toast.success(`VolumeSnapshot ${saved.name} 已创建，等待 CSI snapshot-controller 完成`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => setFormError(error instanceof ApiError && error.status === 409 ? "同名快照已存在，请更换名称" : error.message),
  });
  const claimOptions = claims.filter((claim) => claim.namespace === namespace).map((claim) => ({ value: claim.name, label: `${claim.name} · ${claim.status} · ${claim.storageClass || "无 StorageClass"}` }));
  const classOptions = (feature?.classes ?? []).map((value) => ({ value, label: value === feature?.defaultClass ? `${value} · Default` : value }));
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>创建 VolumeSnapshot</DialogTitle>
          <DialogDescription className="break-words">从 {namespace || "所选命名空间"} 中的 PVC 创建 CSI 快照。快照会异步完成，ReadyToUse=true 后才能用于恢复。</DialogDescription>
        </DialogHeader>
        <div className="min-w-0 space-y-5">
          <div className="space-y-2">
            <Label htmlFor="snapshot-source">源 PVC</Label>
            {manualSource ? <Input id="snapshot-source" value={sourcePVC} onChange={(event) => chooseSource(event.target.value)} placeholder="data-claim" autoComplete="off" spellCheck={false} /> : (
              <SearchableSelect id="snapshot-source" label="源 PVC" value={sourcePVC} options={claimOptions} onChange={chooseSource} onManualSelect={() => setManualSource(true)} placeholder={claimOptions.length ? "选择 PVC" : "没有可选项，手动输入 PVC 名称"} searchPlaceholder="搜索 PVC…" />
            )}
            {!manualSource && claimOptions.length > 0 ? <button type="button" className="text-xs text-primary underline underline-offset-2" onClick={() => setManualSource(true)}>手动输入 PVC 名称</button> : null}
            {manualSource && claimOptions.length > 0 ? <button type="button" className="text-xs text-primary underline underline-offset-2" onClick={() => setManualSource(false)}>返回 PVC 下拉选择</button> : null}
          </div>
          <div className="space-y-2">
            <Label htmlFor="snapshot-name">快照名称</Label>
            <Input id="snapshot-name" value={name} onChange={(event) => { setName(event.target.value); setNameTouched(true); setFormError(""); }} autoComplete="off" spellCheck={false} />
            <p className="text-xs text-muted-foreground">必须唯一，长度不超过 63 个字符。</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="snapshot-class">VolumeSnapshotClass</Label>
            {manualClass ? <Input id="snapshot-class" value={className} onChange={(event) => { setClassName(event.target.value); setFormError(""); }} placeholder="csi-snapclass" autoComplete="off" spellCheck={false} /> : (
              <SearchableSelect id="snapshot-class" label="VolumeSnapshotClass" value={className} options={classOptions} onChange={(value) => { setClassName(value); setFormError(""); }} onManualSelect={() => setManualClass(true)} placeholder={feature?.defaultClass ? `使用默认类 ${feature.defaultClass}` : "使用集群默认类"} searchPlaceholder="搜索快照类…" />
            )}
            {!manualClass ? <button type="button" className="text-xs text-primary underline underline-offset-2" onClick={() => setManualClass(true)}>手动输入快照类</button> : null}
            <p className="text-xs text-muted-foreground">留空时由集群默认 VolumeSnapshotClass 处理；未配置默认类时可能保持 Pending。</p>
          </div>
          {formError ? <p role="alert" className="break-words rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button><Button onClick={() => save.mutate()} disabled={save.isPending || !feature?.available}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}创建快照</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
