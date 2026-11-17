import type { ReactNode } from "react";
import { AlertTriangle, CheckCircle2, CircleAlert, CircleHelp, Copy, ExternalLink, Info, ShieldAlert, SkipForward } from "lucide-react";
import { toast } from "sonner";
import type { Diagnosis, DiagnosisCheckStatus, DiagnosisRecommendation, DiagnosisSeverity } from "@/api/types";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

const severityMeta: Record<DiagnosisSeverity, { label: string; variant: "success" | "default" | "warning" | "destructive" }> = {
  healthy: { label: "正常", variant: "success" },
  info: { label: "提示", variant: "default" },
  warning: { label: "警告", variant: "warning" },
  critical: { label: "严重", variant: "destructive" },
};

const checkMeta: Record<DiagnosisCheckStatus, { label: string; icon: typeof CheckCircle2; tone: string }> = {
  pass: { label: "通过", icon: CheckCircle2, tone: "text-success" },
  info: { label: "提示", icon: Info, tone: "text-primary" },
  warning: { label: "警告", icon: AlertTriangle, tone: "text-warning" },
  critical: { label: "严重", icon: CircleAlert, tone: "text-destructive-strong" },
  skipped: { label: "跳过", icon: SkipForward, tone: "text-muted-foreground" },
};

export function DiagnosisDialog({ open, onOpenChange, diagnosis, loading, error, onRetry }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  diagnosis?: Diagnosis;
  loading: boolean;
  error: unknown;
  onRetry: () => void;
}) {
  const title = diagnosis ? `诊断 ${diagnosis.target.kind} ${diagnosis.target.name}` : "一键故障诊断";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl gap-0 p-4 sm:p-6" data-testid="diagnosis-dialog">
        <DialogHeader className="min-w-0 border-b pb-4 pr-7">
          <DialogTitle className="min-w-0 break-words text-base sm:text-lg">{title}</DialogTitle>
          <DialogDescription className="min-w-0 break-words">
            只读、确定性的 Kubernetes 健康检查。诊断不会修改资源，也不会自动执行建议命令。
          </DialogDescription>
        </DialogHeader>
        <div className="min-w-0 py-4">
          {loading ? <LoadingState label="正在收集诊断证据" className="min-h-64" /> : null}
          {!loading && error ? <ErrorState error={error} onRetry={onRetry} className="min-h-64" /> : null}
          {!loading && !error && diagnosis ? <DiagnosisReport diagnosis={diagnosis} /> : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DiagnosisReport({ diagnosis }: { diagnosis: Diagnosis }) {
  const severity = severityMeta[diagnosis.severity] ?? severityMeta.info;
  return (
    <div className="min-w-0 space-y-6">
      <section className="min-w-0 rounded-lg border bg-muted/20 p-4" aria-label="诊断摘要">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Badge variant={severity.variant} className="shrink-0">{severity.label}</Badge>
          <p className="min-w-0 flex-1 break-words text-sm font-medium">{diagnosis.summary}</p>
        </div>
        <div className="mt-3 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="break-all">{diagnosis.target.kind} · {diagnosis.target.namespace ? `${diagnosis.target.namespace}/` : ""}{diagnosis.target.name}</span>
          {diagnosis.target.status ? <span>状态：{diagnosis.target.status}</span> : null}
          <span>生成于 {formatDiagnosisTime(diagnosis.generatedAt)}</span>
        </div>
      </section>

      <section className="min-w-0" aria-labelledby="diagnosis-checks-title">
        <h3 id="diagnosis-checks-title" className="mb-3 text-sm font-semibold">检查项</h3>
        {diagnosis.checks.length ? (
          <div className="grid min-w-0 gap-2 sm:grid-cols-2">
            {diagnosis.checks.map((check) => {
              const meta = checkMeta[check.status] ?? checkMeta.info;
              const Icon = meta.icon;
              return (
                <div key={check.id} className="min-w-0 rounded-md border p-3">
                  <div className="flex min-w-0 items-start gap-2">
                    <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", meta.tone)} />
                    <div className="min-w-0">
                      <p className="break-words text-sm font-medium">{check.label}</p>
                      <p className="mt-1 break-words text-xs text-muted-foreground">{meta.label} · {check.message}</p>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        ) : <p className="text-sm text-muted-foreground">没有执行检查。</p>}
      </section>

      <section className="min-w-0" aria-labelledby="diagnosis-findings-title">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h3 id="diagnosis-findings-title" className="text-sm font-semibold">原因与证据</h3>
          <Badge variant="secondary">{diagnosis.findings.length} 项</Badge>
        </div>
        {diagnosis.findings.length ? (
          <div className="space-y-3">
            {diagnosis.findings.map((finding) => {
              const meta = severityMeta[finding.severity] ?? severityMeta.info;
              return (
                <article key={finding.id} className={cn("min-w-0 rounded-lg border p-3 sm:p-4", findingBorder(finding.severity))} data-testid="diagnosis-finding">
                  <div className="flex min-w-0 flex-wrap items-start gap-2">
                    <Badge variant={meta.variant} className="shrink-0">{meta.label}</Badge>
                    <h4 className="min-w-0 flex-1 break-words text-sm font-semibold">{finding.title}</h4>
                  </div>
                  <p className="mt-2 break-words text-sm leading-6 text-muted-foreground">{finding.cause}</p>
                  {finding.evidence.length ? (
                    <dl className="mt-3 space-y-2 rounded-md bg-background/70 p-3">
                      {finding.evidence.map((evidence, index) => (
                        <div key={`${evidence.label}-${index}`} className="grid min-w-0 gap-1 text-xs sm:grid-cols-[minmax(5rem,0.35fr)_minmax(0,1fr)]">
                          <dt className="break-words font-medium text-muted-foreground">{evidence.label}</dt>
                          <dd className="min-w-0 break-words font-mono leading-5 text-foreground">{evidence.value}</dd>
                        </div>
                      ))}
                    </dl>
                  ) : null}
                  {finding.recommendations.length ? (
                    <div className="mt-3 space-y-2">
                      {finding.recommendations.map((recommendation, index) => (
                        <Recommendation key={`${recommendation.label}-${index}`} recommendation={recommendation} />
                      ))}
                    </div>
                  ) : null}
                </article>
              );
            })}
          </div>
        ) : (
          <div className="flex items-center gap-2 rounded-md border border-success/25 bg-success/5 p-3 text-sm text-success">
            <CheckCircle2 className="h-4 w-4 shrink-0" />没有需要展示的异常原因。
          </div>
        )}
      </section>

      {diagnosis.related.length ? (
        <section className="min-w-0" aria-labelledby="diagnosis-related-title">
          <h3 id="diagnosis-related-title" className="mb-3 text-sm font-semibold">相关资源</h3>
          <div className="space-y-2">
            {diagnosis.related.map((related, index) => {
              const content = (
                <>
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <Badge variant="outline" className="shrink-0">{related.kind}</Badge>
                    <span className="min-w-0 break-all font-mono text-xs">{related.namespace ? `${related.namespace}/` : ""}{related.name}</span>
                    {related.status ? <Badge variant="secondary" className="shrink-0">{related.status}</Badge> : null}
                  </div>
                  <span className="mt-1 block break-words text-xs text-muted-foreground">{related.relationship}</span>
                </>
              );
              return related.path ? (
                <a key={`${related.kind}-${related.namespace}-${related.name}-${index}`} href={related.path} className="flex min-w-0 items-start justify-between gap-3 rounded-md border p-3 text-sm transition-colors hover:bg-muted/50">
                  <span className="min-w-0">{content}</span><ExternalLink className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                </a>
              ) : (
                <div key={`${related.kind}-${related.namespace}-${related.name}-${index}`} className="min-w-0 rounded-md border p-3 text-sm">{content}</div>
              );
            })}
          </div>
        </section>
      ) : null}

      {diagnosis.warnings.length ? (
        <section className="min-w-0 rounded-lg border border-warning/30 bg-warning/5 p-3" aria-labelledby="diagnosis-warnings-title">
          <h3 id="diagnosis-warnings-title" className="flex items-center gap-2 text-sm font-semibold"><ShieldAlert className="h-4 w-4 text-warning" />诊断范围提示</h3>
          <ul className="mt-2 list-disc space-y-1 pl-5 text-xs leading-5 text-muted-foreground">
            {diagnosis.warnings.map((warning, index) => <li key={`${warning}-${index}`} className="break-words">{warning}</li>)}
          </ul>
        </section>
      ) : null}
    </div>
  );
}

function Recommendation({ recommendation }: { recommendation: DiagnosisRecommendation }) {
  const icon: ReactNode = recommendation.command ? <Copy className="h-3.5 w-3.5" /> : <ExternalLink className="h-3.5 w-3.5" />;
  const content = (
    <>
      <span className="flex min-w-0 items-center gap-2 break-words font-medium">{icon}{recommendation.label}</span>
      {recommendation.description ? <span className="mt-1 block break-words text-xs text-muted-foreground">{recommendation.description}</span> : null}
      {recommendation.command ? <code className="mt-2 block max-w-full break-all rounded bg-muted px-2 py-1.5 font-mono text-[11px] leading-5">{recommendation.command}</code> : null}
    </>
  );
  if (recommendation.command) {
    return (
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-auto w-full min-w-0 justify-start whitespace-normal py-2 text-left"
        onClick={() => {
          void copyText(recommendation.command!).then(
            () => toast.success("命令已复制"),
            () => toast.error("复制失败，请手动选择命令"),
          );
        }}
      >
        <span className="min-w-0">{content}</span>
      </Button>
    );
  }
  if (recommendation.path) {
    return <a href={recommendation.path} className="block min-w-0 rounded-md border p-2.5 text-sm transition-colors hover:bg-muted/50">{content}</a>;
  }
  return <div className="min-w-0 rounded-md border p-2.5 text-sm"><span className="flex items-center gap-2"><CircleHelp className="h-3.5 w-3.5 shrink-0" />{recommendation.label}</span>{recommendation.description ? <span className="mt-1 block break-words text-xs text-muted-foreground">{recommendation.description}</span> : null}</div>;
}

function findingBorder(severity: DiagnosisSeverity) {
  switch (severity) {
    case "critical": return "border-destructive/35 bg-destructive/5";
    case "warning": return "border-warning/35 bg-warning/5";
    case "info": return "border-primary/25 bg-primary/5";
    default: return "border-border bg-card";
  }
}

function formatDiagnosisTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false });
}
