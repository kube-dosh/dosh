import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, RefreshCw, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { ServerCertificate } from "@/api/types";
import { CopyButton } from "@/components/data/copy-button";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useCapabilities } from "@/features/auth/use-session";
import { formatDate } from "@/lib/utils";

const certificateQueryKey = ["server-certificate"] as const;

const statusLabels: Record<ServerCertificate["status"], string> = {
  valid: "有效",
  expiring: "即将过期",
  expired: "已过期",
  not_yet_valid: "尚未生效",
  disabled: "HTTP 模式",
  unavailable: "不可用",
};

function statusTone(status: ServerCertificate["status"]): "success" | "warning" | "destructive" | "secondary" {
  switch (status) {
    case "valid":
      return "success";
    case "expiring":
      return "warning";
    case "expired":
    case "unavailable":
      return "destructive";
    default:
      return "secondary";
  }
}

export function ServerCertificatePanel() {
  const queryClient = useQueryClient();
  const capabilities = useCapabilities().data;
  const query = useQuery({ queryKey: certificateQueryKey, queryFn: api.serverCertificate });
  const reload = useMutation({
    mutationFn: api.reloadServerCertificate,
    onSuccess: (result) => {
      queryClient.setQueryData(certificateQueryKey, result);
      toast.success("服务端证书已重新加载");
    },
    onError: (error) => {
      toast.error(error.message);
      void query.refetch();
    },
  });

  if (query.isLoading) return <LoadingState label="正在读取服务端证书" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const certificate = query.data;
  if (!certificate) return null;

  if (certificate.mode === "http" || !certificate.enabled) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><ShieldCheck className="h-5 w-5 text-muted-foreground" />服务端证书</CardTitle>
          <CardDescription>当前实例以 HTTP 运行。启用 TLS 后可在此查看证书有效期、SAN、指纹和热加载状态。</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const status = statusLabels[certificate.status] ?? certificate.status;
  const warning = certificate.status === "expiring" || certificate.status === "expired" || certificate.status === "not_yet_valid" || certificate.status === "unavailable";
  const sans = [...certificate.dnsNames, ...certificate.ipAddresses];

  return (
    <div className="space-y-3">
      <Card>
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2"><ShieldCheck className="h-5 w-5 text-primary" />服务端证书</CardTitle>
            <CardDescription>HTTPS 证书只读状态。私钥不会通过 API、审计日志或浏览器存储返回。</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={statusTone(certificate.status)}>{status}</Badge>
            {capabilities?.reloadCertificate ? (
              <Button variant="outline" size="sm" onClick={() => reload.mutate()} disabled={reload.isPending}>
                <RefreshCw className={reload.isPending ? "h-3.5 w-3.5 animate-spin" : "h-3.5 w-3.5"} />重载证书
              </Button>
            ) : null}
          </div>
        </CardHeader>
        <CardContent className="space-y-5">
          {warning ? (
            <div role="status" className="flex gap-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-sm text-warning">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>{certificate.status === "expired" ? "证书已过期，请立即更新 Secret 并重载。" : certificate.status === "not_yet_valid" ? "证书尚未生效，请检查系统时间和证书有效期。" : certificate.status === "unavailable" ? "当前无法读取证书状态，请检查挂载文件和服务端日志。" : "证书已进入续期窗口，请安排轮换。"}</span>
            </div>
          ) : null}
          {certificate.lastReloadError ? (
            <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">
              最近一次热加载失败，当前仍使用上一份可用证书：{certificate.lastReloadError}
            </div>
          ) : null}
          <dl className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
            <DetailItem label="证书主题" value={certificate.subject} />
            <DetailItem label="签发者" value={certificate.issuer} />
            <DetailItem label="密钥算法" value={certificate.keyAlgorithm} />
            <DetailItem label="生效时间" value={formatDate(certificate.notBefore)} />
            <DetailItem label="到期时间" value={formatDate(certificate.notAfter)} />
            <DetailItem label="剩余天数" value={certificate.status === "expired" ? "已过期" : `${certificate.daysRemaining} 天`} />
            <DetailItem label="序列号" value={certificate.serialNumber} mono />
            <DetailItem label="自动热加载" value={certificate.autoReload ? `每 ${certificate.reloadIntervalSeconds} 秒检查` : "已关闭"} />
            <DetailItem label="最近加载" value={formatDate(certificate.lastReloadedAt)} />
          </dl>
          <div>
            <p className="text-xs text-muted-foreground">SAN（DNS / IP）</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {sans.length ? sans.map((value) => <span key={value} className="rounded border bg-muted/30 px-1.5 py-0.5 font-mono text-[11px]">{value}</span>) : <span className="text-sm text-muted-foreground">未声明 SAN</span>}
            </div>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">SHA-256 指纹</p>
            <div className="mt-1 flex items-start gap-2">
              <code className="break-all font-mono text-xs">{certificate.fingerprintSha256 || "—"}</code>
              {certificate.fingerprintSha256 ? <CopyButton value={certificate.fingerprintSha256} label="复制指纹" /> : null}
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
