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

export function DeleteResourceDialog({ open, onOpenChange, resourceKind, name, description, riskLevel, riskReasons = [], onConfirm }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resourceKind: string;
  name: string;
  description?: string;
  riskLevel?: "normal" | "high" | "critical";
  riskReasons?: string[];
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
          <AlertDialogDescription className="break-words">{description || "此操作不可撤销。请确认该资源不再被其他工作负载或用户依赖。"}</AlertDialogDescription>
        </AlertDialogHeader>
        {riskLevel === "high" || riskLevel === "critical" ? (
          <div className={`min-w-0 rounded-md border px-3 py-2 text-xs ${riskLevel === "critical" ? "border-destructive/45 bg-destructive/10 text-destructive-strong" : "border-warning/45 bg-warning/10 text-warning"}`}>
            <p className="font-semibold">高风险删除 · {riskLevel === "critical" ? "严重" : "高"}</p>
            {riskReasons.length ? <ul className="mt-1 list-inside list-disc space-y-0.5 break-words">{riskReasons.map((reason) => <li key={reason}>{reason}</li>)}</ul> : null}
          </div>
        ) : null}
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
