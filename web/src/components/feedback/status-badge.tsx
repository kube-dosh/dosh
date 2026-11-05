import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

function tone(status: string): "success" | "warning" | "destructive" | "secondary" | "default" {
  const normalized = status.toLowerCase();
  if (["running", "ready", "active", "healthy", "complete", "succeeded", "bound", "available"].includes(normalized)) return "success";
  if (["pending", "updating", "progressing", "containercreating", "podinitializing", "terminating", "warning", "released", "noreadyendpoints"].includes(normalized)) return "warning";
  if (["failed", "error", "crashloopbackoff", "notready", "unavailable", "evicted", "imagepullbackoff", "errimagepull", "servicenotfound", "portnotfound"].includes(normalized)) return "destructive";
  if (["stopped", "suspended", "idle", "scaled to zero"].includes(normalized)) return "secondary";
  return "default";
}

export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const variant = tone(status);
  return (
    <Badge variant={variant} className={cn("capitalize", className)}>
      <span className={cn("h-1.5 w-1.5 rounded-full", {
        "bg-success": variant === "success",
        "bg-warning": variant === "warning",
        "bg-destructive": variant === "destructive",
        "bg-muted-foreground": variant === "secondary",
        "bg-primary": variant === "default",
      })} />
      {status || "Unknown"}
    </Badge>
  );
}

