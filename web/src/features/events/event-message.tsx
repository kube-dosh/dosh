import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export function EventMessage({ message }: { message: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        className="group block w-full max-w-full rounded-sm text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        onClick={() => setOpen(true)}
        title="查看完整事件消息"
        aria-haspopup="dialog"
      >
        <span className="line-clamp-2 whitespace-normal break-words text-muted-foreground underline decoration-transparent underline-offset-4 transition-colors group-hover:text-foreground group-hover:decoration-border">{message}</span>
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>事件消息</DialogTitle>
            <DialogDescription>完整内容</DialogDescription>
          </DialogHeader>
          <p className="soft-wrap max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/30 p-4 text-sm">{message}</p>
        </DialogContent>
      </Dialog>
    </>
  );
}
