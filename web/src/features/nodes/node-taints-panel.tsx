import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Pencil, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { NodeDetail, Taint } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { formatDate } from "@/lib/utils";

const effects = [
  { value: "NoSchedule", label: "NoSchedule", description: "不允许新的 Pod 调度到该节点，已有 Pod 不受影响。" },
  { value: "PreferNoSchedule", label: "PreferNoSchedule", description: "尽量不调度新的 Pod 到该节点，但调度器仍可能选择该节点。" },
  { value: "NoExecute", label: "NoExecute", description: "不允许新的 Pod 调度，并驱逐不 toleration 该污点的已有 Pod。" },
] as const;

export function NodeTaintsPanel({ node, canEdit }: { node: NodeDetail; canEdit: boolean }) {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [keyInput, setKeyInput] = useState("");
  const [valueInput, setValueInput] = useState("");
  const [effectInput, setEffectInput] = useState("NoSchedule");
  const [localError, setLocalError] = useState("");
  const [pendingDelete, setPendingDelete] = useState<number | null>(null);
  const mutation = useMutation({
    mutationFn: (next: Taint[]) => api.setNodeTaints(node.name, { taints: next, resourceVersion: node.resourceVersion }),
    onSuccess: async (detail) => {
      queryClient.setQueryData(["node", node.name], detail);
      await queryClient.invalidateQueries({ queryKey: ["nodes"] });
      toast.success("节点污点已保存");
      setDialogOpen(false);
    },
    onError: async (error) => {
      toast.error(error.message);
      await queryClient.invalidateQueries({ queryKey: ["node", node.name] });
    },
  });

  const openAdd = () => {
    setEditingIndex(null);
    setKeyInput("");
    setValueInput("");
    setEffectInput("NoSchedule");
    setLocalError("");
    setDialogOpen(true);
  };

  const openEdit = (index: number, taint: Taint) => {
    setEditingIndex(index);
    setKeyInput(taint.key);
    setValueInput(taint.value ?? "");
    setEffectInput(taint.effect);
    setLocalError("");
    setDialogOpen(true);
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setLocalError("");
    const key = keyInput.trim();
    const value = valueInput.trim();
    const effect = effectInput;
    if (!key) {
      setLocalError("污点键不能为空。");
      return;
    }
    const duplicate = node.taints.some((taint, index) => index !== editingIndex && taint.key === key && taint.effect === effect);
    if (duplicate) {
      setLocalError(`污点 ${key} 与 ${effect} 的组合已存在。`);
      return;
    }
    const next = node.taints.map((taint) => ({ ...taint }));
    if (editingIndex === null) next.push({ key, value, effect });
    else next[editingIndex] = { ...next[editingIndex], key, value, effect };
    mutation.mutate(next);
  };

  const confirmDelete = () => {
    if (pendingDelete === null) return;
    mutation.mutate(node.taints.filter((_, index) => index !== pendingDelete));
    setPendingDelete(null);
  };

  const selectedEffect = effects.find((effect) => effect.value === effectInput) ?? effects[0];

  return (
    <>
      <Card>
        <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2"><ShieldAlert className="h-4 w-4 text-muted-foreground" />节点污点</CardTitle>
            <CardDescription className="mt-1.5">污点用于限制或驱逐 Pod，工作负载需要配置对应的 toleration 才能匹配，共 {node.taints.length} 项。</CardDescription>
          </div>
          {canEdit ? <Button onClick={openAdd}><Plus className="h-4 w-4" />添加污点</Button> : null}
        </CardHeader>
        <CardContent className="p-0">
          {node.taints.length ? (
            <div className="divide-y border-t">
              {node.taints.map((taint, index) => {
                const effect = effects.find((item) => item.value === taint.effect);
                return (
                  <div key={`${taint.key}-${taint.effect}`} className="flex min-w-0 flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
                    <div className="min-w-0 space-y-2">
                      <div className="flex flex-wrap items-center gap-2"><span className="break-all font-mono text-sm">{taint.key}{taint.value ? `=${taint.value}` : ""}</span><Badge variant="warning">{taint.effect}</Badge></div>
                      <p className="text-xs leading-5 text-muted-foreground">{effect?.description ?? "未知的污点效果。"}</p>
                      {taint.timeAdded ? <p className="text-xs text-muted-foreground">添加时间：{formatDate(taint.timeAdded)}</p> : null}
                    </div>
                    {canEdit ? (
                      <div className="flex shrink-0 items-center gap-1 self-end sm:self-auto">
                        <Button variant="ghost" size="sm" onClick={() => openEdit(index, taint)} aria-label={`编辑污点 ${taint.key} ${taint.effect}`}><Pencil className="h-3.5 w-3.5" />编辑</Button>
                        <Button variant="ghost" size="sm" className="text-destructive-strong hover:bg-destructive/10" onClick={() => setPendingDelete(index)} aria-label={`删除污点 ${taint.key} ${taint.effect}`}><Trash2 className="h-3.5 w-3.5" />删除</Button>
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
          ) : <p className="border-t px-5 py-10 text-center text-sm text-muted-foreground">该节点还没有污点，所有未设置 toleration 的 Pod 都可以参与调度。</p>}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <form className="grid gap-4" onSubmit={handleSubmit}>
            <DialogHeader>
              <DialogTitle>{editingIndex === null ? "添加节点污点" : "编辑节点污点"}</DialogTitle>
              <DialogDescription>污点会影响 Pod 调度，NoExecute 还可能驱逐已有 Pod。请确认工作负载已配置 toleration。</DialogDescription>
            </DialogHeader>
            <div className="grid gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="node-taint-key">污点键</Label>
                <Input id="node-taint-key" autoFocus value={keyInput} onChange={(event) => setKeyInput(event.target.value)} placeholder="例如 dedicated" className="font-mono" spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="node-taint-value">污点值</Label>
                <Input id="node-taint-value" value={valueInput} onChange={(event) => setValueInput(event.target.value)} placeholder="例如 gpu（可为空）" className="font-mono" spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="node-taint-effect">污点效果</Label>
                <Select value={effectInput} onValueChange={setEffectInput}>
                  <SelectTrigger id="node-taint-effect" aria-label="污点效果"><SelectValue /></SelectTrigger>
                  <SelectContent>{effects.map((effect) => <SelectItem key={effect.value} value={effect.value}>{effect.label}</SelectItem>)}</SelectContent>
                </Select>
                <p className="text-xs leading-5 text-muted-foreground">{selectedEffect.description}</p>
              </div>
              {localError ? <p className="text-sm text-destructive-strong">{localError}</p> : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
              <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editingIndex === null ? "添加污点" : "保存修改"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingDelete !== null} onOpenChange={(open) => { if (!open) setPendingDelete(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除节点污点？</AlertDialogTitle>
            <AlertDialogDescription>{pendingDelete !== null ? `将删除污点 ${node.taints[pendingDelete]?.key ?? ""}。删除 NoSchedule 或 PreferNoSchedule 污点后，新的 Pod 可能被调度到该节点。` : ""}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => { event.preventDefault(); confirmDelete(); }} disabled={mutation.isPending}>
              {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
