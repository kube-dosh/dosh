import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function NamespaceDeleteDialog({ open, onOpenChange, name, resourceVersion, onDeleted }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  name: string;
  resourceVersion?: string;
  onDeleted: () => Promise<void> | void;
}) {
  const [confirmName, setConfirmName] = useState("");

  useEffect(() => {
    if (open) setConfirmName("");
  }, [open, name]);

  const remove = useMutation({
    mutationFn: () => api.deleteNamespace(name, resourceVersion),
    onSuccess: async () => {
      toast.success(`命名空间 ${name} 的删除请求已提交`);
      await onDeleted();
      onOpenChange(false);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2"><Trash2 className="h-5 w-5 text-destructive-strong" />删除命名空间 {name}？</AlertDialogTitle>
          <AlertDialogDescription>
            将以前台级联方式删除该命名空间及其中全部工作负载、Pod、Service、ConfigMap、Secret、PVC 等资源。此操作不可撤销。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">
          系统命名空间 default 和 kube-* 受保护，不会提供删除入口。
        </div>
        <div className="space-y-2">
          <Label htmlFor="namespace-delete-confirm">请输入命名空间名称确认</Label>
          <Input id="namespace-delete-confirm" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} placeholder={name} autoComplete="off" spellCheck={false} />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            onClick={(event) => { event.preventDefault(); remove.mutate(); }}
            disabled={confirmName !== name || remove.isPending}
          >
            {remove.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            确认级联删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
