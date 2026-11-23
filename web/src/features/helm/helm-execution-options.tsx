import type { HelmExecutionOptionsInput } from "@/api/types";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export type HelmExecutionMode = "install" | "upgrade" | "uninstall" | "rollback";

export const defaultHelmExecutionOptions: HelmExecutionOptionsInput = {
  timeoutSeconds: 600,
  wait: true,
  waitForJobs: true,
  atomic: true,
  cleanupOnFail: true,
  maxHistory: 10,
  createNamespace: false,
  disableHooks: false,
  skipCRDs: false,
  keepHistory: false,
};

export function HelmExecutionOptionsFields({
  idPrefix,
  mode,
  value,
  onChange,
  disabled = false,
}: {
  idPrefix: string;
  mode: HelmExecutionMode;
  value: HelmExecutionOptionsInput;
  onChange: (value: HelmExecutionOptionsInput) => void;
  disabled?: boolean;
}) {
  const update = <Key extends keyof HelmExecutionOptionsInput>(key: Key, next: HelmExecutionOptionsInput[Key]) => {
    const candidate = { ...value, [key]: next };
    if (key === "wait" && next === false) {
      candidate.atomic = false;
      candidate.cleanupOnFail = false;
    }
    onChange(candidate);
  };
  const checkbox = (key: keyof HelmExecutionOptionsInput, id: string, label: string, help?: string) => (
    <label className="flex min-w-0 items-start gap-2 text-sm" htmlFor={`${idPrefix}-${id}`}>
      <input
        id={`${idPrefix}-${id}`}
        type="checkbox"
        className="mt-0.5 h-4 w-4 shrink-0"
        checked={Boolean(value[key])}
        disabled={disabled}
        onChange={(event) => update(key, event.target.checked as never)}
      />
      <span className="min-w-0">
        <span className="block font-medium">{label}</span>
        {help ? <span className="block break-words text-xs leading-5 text-muted-foreground">{help}</span> : null}
      </span>
    </label>
  );

  return (
    <details className="min-w-0 rounded-md border">
      <summary className="cursor-pointer px-3 py-2 text-sm font-medium">高级执行选项</summary>
      <div className="grid min-w-0 gap-3 border-t px-3 py-3 sm:grid-cols-2">
        <div className="min-w-0 space-y-1.5">
          <Label htmlFor={`${idPrefix}-timeout`}>超时时间（秒）</Label>
          <Input
            id={`${idPrefix}-timeout`}
            type="number"
            min={30}
            max={3600}
            value={value.timeoutSeconds ?? 600}
            disabled={disabled}
            onChange={(event) => update("timeoutSeconds", Number(event.target.value) || 600)}
          />
          <p className="text-xs leading-5 text-muted-foreground">范围 30–3600 秒，默认 600 秒；慢启动 Chart 应适当增大。</p>
        </div>
        {mode !== "uninstall" ? (
          <div className="min-w-0 space-y-1.5">
            <Label htmlFor={`${idPrefix}-history`}>历史版本上限</Label>
            <Input
              id={`${idPrefix}-history`}
              type="number"
              min={1}
              max={100}
              value={value.maxHistory ?? 10}
              disabled={disabled}
              onChange={(event) => update("maxHistory", Number(event.target.value) || 10)}
            />
            <p className="text-xs leading-5 text-muted-foreground">范围 1–100，默认 10；用于控制 Helm Release 历史记录数量。</p>
          </div>
        ) : null}
        <div className="min-w-0 space-y-2 sm:col-span-2">
          {checkbox("wait", "wait", "等待资源就绪", "开启后 Helm 会等待工作负载、Job 和 Hook 完成；关闭后只提交资源。")}
          {mode === "install" || mode === "upgrade" || mode === "rollback" ? checkbox("waitForJobs", "wait-jobs", "等待 Job 完成", "Helm 会额外等待 Job 型 Hook 和资源完成。") : null}
          {mode === "install" || mode === "upgrade" ? checkbox("atomic", "atomic", "失败原子回滚", "要求同时开启等待；升级失败时自动回滚到上一 revision。") : null}
          {mode === "install" || mode === "upgrade" ? checkbox("cleanupOnFail", "cleanup", "失败清理新建资源", "要求同时开启等待；失败时删除本次新建资源。") : null}
          {mode === "rollback" ? checkbox("cleanupOnFail", "cleanup", "回滚失败清理新建资源", "要求同时开启等待；失败时删除回滚过程中新建的资源。") : null}
          {mode === "install" ? checkbox("createNamespace", "create-namespace", "自动创建命名空间", "目标命名空间不存在时由 Helm 创建。") : null}
          {checkbox("disableHooks", "disable-hooks", "跳过 Hook", "不执行 pre/post/delete/test Hook；适用于已单独管理 Hook 的场景。")}
          {mode === "install" || mode === "upgrade" ? checkbox("skipCRDs", "skip-crds", "跳过 CRD", "不安装 Chart 的 crds/ 目录；CRD 需由集群管理者单独维护。") : null}
          {mode === "uninstall" ? checkbox("keepHistory", "keep-history", "保留 Helm 历史", "卸载后保留 uninstalled Release 记录，便于审计和后续恢复；会占用额外 Secret。") : null}
        </div>
        {value.wait === false ? (
          <p className="min-w-0 break-words rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs leading-5 text-warning sm:col-span-2">
            未开启等待时，Helm 只会提交资源，不会确认工作负载是否真正就绪；Atomic 和 CleanupOnFail 已自动关闭。
          </p>
        ) : null}
      </div>
    </details>
  );
}
