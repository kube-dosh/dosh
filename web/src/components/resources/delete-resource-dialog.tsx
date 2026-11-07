import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function DeleteResourceDialog({ open, onOpenChange, resourceKind, name, description, onConfirm }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resourceKind: string;
  name: string;
  description?: string;
  onConfirm: () => Promise<unknown>;
}) {
  const [confirmName, setConfirmName] = useState("");
  useEffect(() => { if (open) setConfirmName(""); }, [open, name]);
  const remove = useMutation({
    mutationFn: onConfirm,
    onSuccess: () => {
      toast.success(`${resourceKind} ${name} 的删除请求已提交`);
      onOpenChange(false);
    },
    onError: (error) => toast.error(error.message),
  });
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2"><Trash2 className="h-5 w-5 text-destructive-strong" />删除 {resourceKind} {name}？</AlertDialogTitle>
          <AlertDialogDescription>{description || "此操作不可撤销。请确认该资源不再被其他工作负载或用户依赖。"}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-2">
          <Label htmlFor={`delete-${resourceKind}-confirm`}>输入名称确认</Label>
          <Input id={`delete-${resourceKind}-confirm`} value={confirmName} onChange={(event) => setConfirmName(event.target.value)} placeholder={name} autoComplete="off" spellCheck={false} />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            onClick={(event) => { event.preventDefault(); if (confirmName === name) remove.mutate(); }}
            disabled={confirmName !== name || remove.isPending}
          >
            {remove.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            确认删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
