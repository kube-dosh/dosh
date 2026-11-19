import { AlertTriangle, Boxes, Camera, HardDrive } from "lucide-react";
import type { StorageImpact } from "@/api/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export function StorageImpactCard({ impact, title = "使用与影响", emptyMessage = "当前没有运行中的 Pod 挂载此卷。" }: {
  impact?: StorageImpact;
  title?: string;
  emptyMessage?: string;
}) {
  const mounts = impact?.mounts ?? [];
  const warnings = impact?.warnings ?? [];
  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader><CardTitle className="flex min-w-0 items-center gap-2"><HardDrive className="h-4 w-4 shrink-0" /><span className="truncate">{title}</span></CardTitle></CardHeader>
      <CardContent className="min-w-0 space-y-3">
        <div className="grid min-w-0 gap-2 sm:grid-cols-2">
          <div className="min-w-0 rounded-md border px-3 py-2">
            <p className="flex items-center gap-2 text-xs text-muted-foreground"><Boxes className="h-3.5 w-3.5" />挂载 Pod / 工作负载</p>
            <p className="mt-1 text-lg font-semibold">{impact?.mountCount ?? mounts.length}</p>
          </div>
          <div className="min-w-0 rounded-md border px-3 py-2">
            <p className="flex items-center gap-2 text-xs text-muted-foreground"><Camera className="h-3.5 w-3.5" />关联快照</p>
            <p className="mt-1 text-lg font-semibold">{impact?.volumeSnapshotCount ?? 0}</p>
          </div>
        </div>
        {warnings.length ? <div className="min-w-0 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
          <p className="flex items-center gap-2 font-medium"><AlertTriangle className="h-3.5 w-3.5 shrink-0" />影响面不完整</p>
          {warnings.map((warning) => <p key={warning} className="mt-1 break-words">{warning}</p>)}
        </div> : null}
        {mounts.length ? <div className="min-w-0 space-y-2">
          {mounts.map((mount) => (
            <div key={`${mount.namespace}/${mount.pod}`} className="min-w-0 rounded-md border px-3 py-2 text-sm">
              <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                <span className="break-all font-mono text-xs">{mount.namespace}/{mount.pod}</span>
                {mount.workloadKind || mount.workloadName ? <span className="break-all text-xs text-muted-foreground">{mount.workloadKind} {mount.workloadName}</span> : null}
              </div>
              <p className="mt-1 break-words text-xs text-muted-foreground">容器：{mount.containers.join(", ") || "—"}</p>
              <p className="mt-0.5 break-all font-mono text-xs text-muted-foreground">挂载：{mount.mountPaths.join(", ") || "—"}</p>
            </div>
          ))}
        </div> : <p className="rounded-md border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">{emptyMessage}</p>}
        {mounts.length ? <p className="break-words text-xs text-muted-foreground">删除、回收或中断该卷会影响上述运行中的工作负载；请先迁移或停止挂载。</p> : null}
      </CardContent>
    </Card>
  );
}
