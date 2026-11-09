import { AlertTriangle, Inbox, Loader2, RefreshCw, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/api/client";
import { cn } from "@/lib/utils";

export function LoadingState({ label = "正在加载", className }: { label?: string; className?: string }) {
  return (
    <div role="status" aria-live="polite" className={cn("flex h-40 flex-col items-center justify-center gap-3 text-sm text-muted-foreground", className)}>
      <Loader2 className="h-5 w-5 animate-spin text-primary" />
      <span>{label}</span>
    </div>
  );
}

export function EmptyState({ title = "暂无数据", description, action, className }: {
  title?: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-h-52 flex-col items-center justify-center gap-3 rounded-lg border border-dashed bg-card/50 px-6 py-10 text-center", className)}>
      <div className="rounded-full bg-muted p-3 text-muted-foreground"><Inbox className="h-5 w-5" /></div>
      <div>
        <p className="font-medium text-foreground">{title}</p>
        {description ? <p className="mt-1 max-w-md text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {action}
    </div>
  );
}

export function ErrorState({ error, onRetry, className }: { error: unknown; onRetry?: () => void; className?: string }) {
  const apiError = error instanceof ApiError ? error : null;
  const forbidden = apiError?.status === 403;
  const Icon = forbidden ? ShieldAlert : AlertTriangle;
  return (
    <div role="alert" className={cn("flex min-h-52 flex-col items-center justify-center gap-3 rounded-lg border border-destructive/25 bg-destructive/5 px-6 py-10 text-center", className)}>
      <div className="rounded-full bg-destructive/10 p-3 text-destructive-strong"><Icon className="h-5 w-5" /></div>
      <div>
        <p className="font-medium text-foreground">{forbidden ? "没有访问权限" : "加载失败"}</p>
        <p className="mt-1 max-w-lg text-sm text-muted-foreground">{error instanceof Error ? error.message : "请求未能完成"}</p>
        {apiError?.requestId ? <p className="mt-1 font-mono text-xs text-muted-foreground">请求 ID: {apiError.requestId}</p> : null}
      </div>
      {onRetry ? <Button variant="outline" size="sm" onClick={onRetry}><RefreshCw className="h-3.5 w-3.5" />重试</Button> : null}
    </div>
  );
}

