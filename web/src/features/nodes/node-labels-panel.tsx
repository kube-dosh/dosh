import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Pencil, Plus, Tag, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { NodeDetail } from "@/api/types";
import { DetailItem } from "@/components/data/detail-item";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";

export function NodeLabelsPanel({ node, canEdit }: { node: NodeDetail; canEdit: boolean }) {
  const queryClient = useQueryClient();
  const labels = useMemo(() => Object.entries(node.labels ?? {}).sort(([left], [right]) => left.localeCompare(right)), [node.labels]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [keyInput, setKeyInput] = useState("");
  const [valueInput, setValueInput] = useState("");
  const [localError, setLocalError] = useState("");
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: (next: Record<string, string>) => api.setNodeLabels(node.name, { labels: next, resourceVersion: node.resourceVersion }),
    onSuccess: async (detail) => {
      queryClient.setQueryData(["node", node.name], detail);
      await queryClient.invalidateQueries({ queryKey: ["nodes"] });
      toast.success("节点标签已保存");
      setDialogOpen(false);
    },
    onError: async (error) => {
      toast.error(error.message);
      await queryClient.invalidateQueries({ queryKey: ["node", node.name] });
    },
  });

  const openAdd = () => {
    setEditingKey(null);
    setKeyInput("");
    setValueInput("");
    setLocalError("");
    setDialogOpen(true);
  };

  const openEdit = (key: string, value: string) => {
    setEditingKey(key);
    setKeyInput(key);
    setValueInput(value);
    setLocalError("");
    setDialogOpen(true);
  };

  const save = (next: Record<string, string>) => {
    mutation.mutate(next);
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setLocalError("");
    const key = keyInput.trim();
    const value = valueInput.trim();
    if (!key) {
      setLocalError("标签键不能为空。");
      return;
    }
    if (key !== editingKey && Object.prototype.hasOwnProperty.call(node.labels, key)) {
      setLocalError(`标签键 ${key} 已存在。`);
      return;
    }
    const next = { ...node.labels };
    if (editingKey && editingKey !== key) delete next[editingKey];
    next[key] = value;
    save(next);
  };

  const confirmDelete = () => {
    if (!pendingDelete) return;
    const next = { ...node.labels };
    delete next[pendingDelete];
    save(next);
    setPendingDelete(null);
  };

  return (
    <>
      <Card>
        <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2"><Tag className="h-4 w-4 text-muted-foreground" />节点标签</CardTitle>
            <CardDescription className="mt-1.5">标签用于节点选择、亲和性调度和资源分组，共 {labels.length} 项。</CardDescription>
          </div>
          {canEdit ? <Button onClick={openAdd}><Plus className="h-4 w-4" />添加标签</Button> : null}
        </CardHeader>
        <CardContent className="p-0">
          {labels.length ? (
            <div className="divide-y border-t">
              {labels.map(([key, value]) => (
                <div key={key} className="flex min-w-0 flex-col gap-3 px-5 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] sm:gap-5">
                    <DetailItem label="键"><span className="break-all font-mono text-xs">{key}</span></DetailItem>
                    <DetailItem label="值"><span className="break-all font-mono text-xs">{value || "—"}</span></DetailItem>
                  </div>
                  {canEdit ? (
                    <div className="flex shrink-0 items-center gap-1 self-end sm:self-auto">
                      <Button variant="ghost" size="sm" onClick={() => openEdit(key, value)} aria-label={`编辑标签 ${key}`}><Pencil className="h-3.5 w-3.5" />编辑</Button>
                      <Button variant="ghost" size="sm" className="text-destructive-strong hover:bg-destructive/10" onClick={() => setPendingDelete(key)} aria-label={`删除标签 ${key}`}><Trash2 className="h-3.5 w-3.5" />删除</Button>
                    </div>
                  ) : null}
                </div>
              ))}
            </div>
          ) : <p className="border-t px-5 py-10 text-center text-sm text-muted-foreground">该节点还没有标签。</p>}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <form className="grid gap-4" onSubmit={handleSubmit}>
            <DialogHeader>
              <DialogTitle>{editingKey ? "编辑节点标签" : "添加节点标签"}</DialogTitle>
              <DialogDescription>标签键和值需符合 Kubernetes 标签命名规则。修改标签不会重启节点上的 Pod。</DialogDescription>
            </DialogHeader>
            <div className="grid gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="node-label-key">标签键</Label>
                <Input id="node-label-key" autoFocus value={keyInput} onChange={(event) => setKeyInput(event.target.value)} placeholder="例如 topology.kubernetes.io/zone" className="font-mono" spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="node-label-value">标签值</Label>
                <Input id="node-label-value" value={valueInput} onChange={(event) => setValueInput(event.target.value)} placeholder="例如 az-1（可为空）" className="font-mono" spellCheck={false} />
              </div>
              {localError ? <p className="text-sm text-destructive-strong">{localError}</p> : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
              <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{editingKey ? "保存修改" : "添加标签"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingDelete !== null} onOpenChange={(open) => { if (!open) setPendingDelete(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除节点标签？</AlertDialogTitle>
            <AlertDialogDescription>将删除标签 {pendingDelete}。依赖该标签的节点亲和性或调度策略可能受到影响。</AlertDialogDescription>
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
