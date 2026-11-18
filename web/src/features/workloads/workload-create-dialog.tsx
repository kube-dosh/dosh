import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Check, ChevronLeft, ChevronRight, Loader2, Rocket } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { Capabilities, DeploymentEnvVar, WorkloadCreateInput, WorkloadCreateKind } from "@/api/types";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ImageReferencePicker } from "@/features/registries/image-reference-picker";
import { workloadCapabilities } from "@/lib/capabilities";
import { cn } from "@/lib/utils";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const RESERVED_LABELS = new Set(["app.kubernetes.io/name", "app.kubernetes.io/managed-by"]);
const workloadKinds: WorkloadCreateKind[] = ["deployment", "statefulset", "daemonset", "job", "cronjob"];
const kindLabels: Record<WorkloadCreateKind, string> = {
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
  job: "Job",
  cronjob: "CronJob",
};
const kindDescriptions: Record<WorkloadCreateKind, string> = {
  deployment: "无状态副本，支持扩缩容和滚动发布",
  statefulset: "稳定网络标识和有序副本，需要关联 Headless Service",
  daemonset: "在符合调度条件的每个节点上运行一个 Pod",
  job: "运行至成功完成的一次性任务",
  cronjob: "按 Cron 表达式周期性创建 Job",
};

type FormState = {
  kind: WorkloadCreateKind;
  namespace: string;
  name: string;
  replicas: string;
  serviceName: string;
  schedule: string;
  concurrencyPolicy: "Allow" | "Forbid" | "Replace";
  suspend: boolean;
  image: string;
  registryId: string;
  containerName: string;
  containerPort: string;
  cpuRequest: string;
  cpuLimit: string;
  memoryRequestMi: string;
  memoryLimitMi: string;
  env: string;
  labels: string;
};

type FormErrors = Partial<Record<keyof FormState | "form", string>>;

const initialForm = (namespace: string, kind: WorkloadCreateKind): FormState => ({
  kind,
  namespace: namespace || "default",
  name: "",
  replicas: "1",
  serviceName: "",
  schedule: "*/5 * * * *",
  concurrencyPolicy: "Allow",
  suspend: false,
  image: "nginx:alpine",
  registryId: "",
  containerName: "",
  containerPort: "80",
  cpuRequest: "0.1",
  cpuLimit: "0.5",
  memoryRequestMi: "128",
  memoryLimitMi: "512",
  env: "",
  labels: "",
});

export function WorkloadCreateDialog({ open, onOpenChange, initialNamespace, initialKind, capabilities }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialNamespace: string;
  initialKind?: WorkloadCreateKind;
  capabilities?: Capabilities;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const allowedKinds = useMemo(() => workloadKinds.filter((kind) => workloadCapabilities(capabilities, kind).create), [capabilities]);
  const [step, setStep] = useState(0);
  const [form, setForm] = useState<FormState>(() => initialForm(initialNamespace, resolveInitialKind(initialKind, allowedKinds)));
  const [errors, setErrors] = useState<FormErrors>({});
  const [preview, setPreview] = useState("");

  useEffect(() => {
    if (!open) return;
    setStep(0);
    setForm(initialForm(initialNamespace, resolveInitialKind(initialKind, allowedKinds)));
    setErrors({});
    setPreview("");
  }, [open, initialNamespace, initialKind, allowedKinds]);

  const input = useMemo(() => buildInput(form), [form]);
  const kindLabel = kindLabels[form.kind];
  const canCreateCurrent = allowedKinds.includes(form.kind);
  const needsReplicas = form.kind === "deployment" || form.kind === "statefulset";

  const previewMutation = useMutation({
    mutationFn: (value: WorkloadCreateInput) => api.previewWorkload(value),
    onSuccess: (result) => setPreview(result.yaml),
    onError: (error) => toast.error(error.message),
  });

  const createMutation = useMutation({
    mutationFn: (value: WorkloadCreateInput) => api.createWorkload(value),
    onSuccess: async (created) => {
      toast.success(`${kindLabels[created.kind.toLowerCase() as WorkloadCreateKind] ?? created.kind} ${created.name} 已创建`);
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      onOpenChange(false);
      await navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind: created.kind.toLowerCase(), namespace: created.namespace, name: created.name } });
    },
    onError: (error) => {
      if (error instanceof ApiError && error.fieldErrors) {
        setErrors((current) => ({ ...current, ...error.fieldErrors, form: error.message }));
      } else {
        toast.error(error.message);
      }
      setStep(0);
    },
  });

  const update = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
    setErrors((current) => {
      if (key === "kind") return {};
      const next = { ...current };
      delete next[key];
      delete next.form;
      return next;
    });
    setPreview("");
  };

  const updateImage = (image: string, registryId?: string) => {
    setForm((current) => ({ ...current, image, registryId: registryId ?? "" }));
    setErrors((current) => {
      const next = { ...current };
      delete next.image;
      delete next.registryId;
      delete next.form;
      return next;
    });
    setPreview("");
  };

  const validateBasics = (): FormErrors => {
    const next: FormErrors = {};
    if (!allowedKinds.includes(form.kind)) next.kind = "当前身份没有创建该工作负载类型的权限";
    if (!form.namespace) next.namespace = "请选择命名空间";
    else if (form.namespace.length > 63 || !DNS_LABEL.test(form.namespace)) next.namespace = "命名空间必须是 DNS-1123 标签";
    if (!form.name) next.name = `请输入 ${kindLabel} 名称`;
    else if (form.name.length > 63 || !DNS_LABEL.test(form.name)) next.name = "名称必须是小写 DNS-1123 标签，最长 63 字符";
    if (needsReplicas) {
      const replicas = Number(form.replicas);
      if (!Number.isInteger(replicas) || replicas < 0 || replicas > 1000) next.replicas = "副本数必须是 0 到 1000 的整数";
    }
    if (form.kind === "statefulset" && form.serviceName && (form.serviceName.length > 63 || !DNS_LABEL.test(form.serviceName))) {
      next.serviceName = "Headless Service 名称必须是小写 DNS-1123 标签";
    }
    if (form.kind === "cronjob" && (!form.schedule.trim() || form.schedule.trim().length > 100)) {
      next.schedule = "请输入最长 100 字符的 Cron 表达式";
    }
    return next;
  };

  const validateRuntime = (): FormErrors => {
    const next: FormErrors = {};
    if (!form.image.trim() || form.image.trim().length > 512) next.image = "请输入有效的镜像地址";
    if (form.containerName && (form.containerName.length > 63 || !DNS_LABEL.test(form.containerName))) {
      next.containerName = "容器名称必须是小写 DNS-1123 标签";
    }
    const port = Number(form.containerPort);
    if (!Number.isInteger(port) || port < 1 || port > 65535) next.containerPort = "端口必须是 1 到 65535 的整数";
    const cpuRequest = parseCPU(form.cpuRequest);
    const cpuLimit = parseCPU(form.cpuLimit);
    if (cpuRequest === null || cpuLimit === null) next.cpuRequest = "CPU 必须是非负数字，例如 0.5";
    else if (cpuLimit > 0 && cpuLimit < cpuRequest) next.cpuLimit = "CPU 上限不能小于请求";
    const memoryRequest = parseMiB(form.memoryRequestMi);
    const memoryLimit = parseMiB(form.memoryLimitMi);
    if (memoryRequest === null || memoryLimit === null) next.memoryRequestMi = "内存必须是非负 MiB 数字";
    else if (memoryLimit > 0 && memoryLimit < memoryRequest) next.memoryLimitMi = "内存上限不能小于请求";
    if (parseEnv(form.env) === null) next.env = "环境变量格式应为每行 KEY=value";
    if (parseLabels(form.labels) === null) next.labels = "标签格式应为每行 key=value，且不能覆盖 dosh 管理标签";
    return next;
  };

  const goNext = () => {
    const next = step === 0 ? validateBasics() : validateRuntime();
    setErrors(next);
    if (Object.keys(next).length > 0) return;
    if (step === 0) {
      setStep(1);
      return;
    }
    if (!input) {
      setErrors({ form: "请修正运行配置后再生成预览" });
      return;
    }
    setStep(2);
    previewMutation.mutate(input);
  };

  const submit = () => {
    const next = { ...validateBasics(), ...validateRuntime() };
    setErrors(next);
    if (Object.keys(next).length > 0) {
      setStep(0);
      return;
    }
    if (!input) {
      setErrors({ form: "请修正表单后再提交" });
      return;
    }
    createMutation.mutate(input);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Rocket className="h-5 w-5 text-primary" />创建工作负载</DialogTitle>
          <DialogDescription>选择工作负载类型并填写单个应用容器；提交前由 Kubernetes API Server 执行 Dry Run。</DialogDescription>
        </DialogHeader>

        <StepIndicator step={step} />

        {errors.form ? <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{errors.form}</div> : null}
        {!allowedKinds.length ? <div role="alert" className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-sm text-warning">当前身份没有创建任何受支持工作负载的权限。</div> : null}

        {step === 0 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="工作负载类型" htmlFor="workload-kind" error={errors.kind} className="sm:col-span-2">
              <Select value={form.kind} onValueChange={(value) => update("kind", value as WorkloadCreateKind)}>
                <SelectTrigger id="workload-kind" aria-label="工作负载类型"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {workloadKinds.map((kind) => {
                    const allowed = allowedKinds.includes(kind);
                    return <SelectItem key={kind} value={kind} disabled={!allowed}>{kindLabels[kind]} · {kindDescriptions[kind]}{allowed ? "" : " · 无 create 权限"}</SelectItem>;
                  })}
                </SelectContent>
              </Select>
            </Field>
            <Field label="命名空间" htmlFor="workload-namespace" error={errors.namespace}>
              <NamespaceSelect value={form.namespace} onChange={(value) => update("namespace", value)} allowAll={false} className="w-full" />
            </Field>
            <Field label={`${kindLabel} 名称`} htmlFor="workload-name" error={errors.name}>
              <Input id="workload-name" value={form.name} onChange={(event) => update("name", event.target.value)} placeholder="web-api" autoComplete="off" />
            </Field>
            {needsReplicas ? <Field label="副本数" htmlFor="workload-replicas" error={errors.replicas}>
              <Input id="workload-replicas" type="number" min={0} max={1000} step={1} value={form.replicas} onChange={(event) => update("replicas", event.target.value)} />
            </Field> : null}
            {form.kind === "statefulset" ? <Field label="Headless Service" htmlFor="workload-service-name" error={errors.serviceName} hint="留空时使用 StatefulSet 名称；Service 需要另行创建或已存在。" className="sm:col-span-2">
              <Input id="workload-service-name" value={form.serviceName} onChange={(event) => update("serviceName", event.target.value)} placeholder={form.name || "web-headless"} />
            </Field> : null}
            {form.kind === "cronjob" ? <>
              <Field label="Cron 表达式" htmlFor="workload-schedule" error={errors.schedule} hint="例如 */5 * * * * 表示每 5 分钟。">
                <Input id="workload-schedule" value={form.schedule} onChange={(event) => update("schedule", event.target.value)} className="font-mono" spellCheck={false} />
              </Field>
              <Field label="并发策略" htmlFor="workload-concurrency-policy">
                <Select value={form.concurrencyPolicy} onValueChange={(value) => update("concurrencyPolicy", value as FormState["concurrencyPolicy"])}>
                  <SelectTrigger id="workload-concurrency-policy"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="Allow">Allow · 允许并发</SelectItem>
                    <SelectItem value="Forbid">Forbid · 禁止并发</SelectItem>
                    <SelectItem value="Replace">Replace · 替换旧任务</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <label className="flex items-center gap-2 text-sm sm:col-span-2"><input type="checkbox" checked={form.suspend} onChange={(event) => update("suspend", event.target.checked)} />创建后暂停调度</label>
            </> : null}
            <div className="rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground sm:col-span-2">
              {kindDescriptions[form.kind]}。将自动添加 <span className="font-mono">app.kubernetes.io/name</span> 与 <span className="font-mono">app.kubernetes.io/managed-by=dosh</span> 标签作为稳定选择器。
            </div>
          </div>
        ) : null}

        {step === 1 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="容器镜像" htmlFor="workload-image" error={errors.image} className="sm:col-span-2">
              <ImageReferencePicker
                id="workload-image"
                value={form.image}
                registryId={form.registryId || undefined}
                onChange={updateImage}
                hideLabel
                hint="支持从已配置镜像仓搜索仓库和 Tag；直接输入时仍按完整镜像地址提交。"
              />
            </Field>
            <Field label="容器名称（可选）" htmlFor="workload-container-name" error={errors.containerName}>
              <Input id="workload-container-name" value={form.containerName} onChange={(event) => update("containerName", event.target.value)} placeholder={`默认使用 ${kindLabel} 名称`} />
            </Field>
            <Field label="容器端口" htmlFor="workload-port" error={errors.containerPort}>
              <Input id="workload-port" type="number" min={1} max={65535} value={form.containerPort} onChange={(event) => update("containerPort", event.target.value)} />
            </Field>
            <Field label="CPU 请求（核）" htmlFor="workload-cpu-request" error={errors.cpuRequest}>
              <Input id="workload-cpu-request" type="number" min={0} step={0.1} value={form.cpuRequest} onChange={(event) => update("cpuRequest", event.target.value)} />
            </Field>
            <Field label="CPU 上限（核）" htmlFor="workload-cpu-limit" error={errors.cpuLimit}>
              <Input id="workload-cpu-limit" type="number" min={0} step={0.1} value={form.cpuLimit} onChange={(event) => update("cpuLimit", event.target.value)} />
            </Field>
            <Field label="内存请求（MiB）" htmlFor="workload-memory-request" error={errors.memoryRequestMi}>
              <Input id="workload-memory-request" type="number" min={0} step={1} value={form.memoryRequestMi} onChange={(event) => update("memoryRequestMi", event.target.value)} />
            </Field>
            <Field label="内存上限（MiB）" htmlFor="workload-memory-limit" error={errors.memoryLimitMi}>
              <Input id="workload-memory-limit" type="number" min={0} step={1} value={form.memoryLimitMi} onChange={(event) => update("memoryLimitMi", event.target.value)} />
            </Field>
            <Field label="环境变量" htmlFor="workload-env" error={errors.env} hint="每行一个 KEY=value，值可包含等号。">
              <Textarea id="workload-env" value={form.env} onChange={(event) => update("env", event.target.value)} placeholder={"MODE=production\nLOG_LEVEL=info"} className="min-h-28 font-mono text-xs" />
            </Field>
            <Field label="自定义标签" htmlFor="workload-labels" error={errors.labels} hint="每行一个 key=value。">
              <Textarea id="workload-labels" value={form.labels} onChange={(event) => update("labels", event.target.value)} placeholder={"team=platform\nenvironment=production"} className="min-h-28 font-mono text-xs" />
            </Field>
          </div>
        ) : null}

        {step === 2 ? (
          <div className="grid min-w-0 gap-4 lg:grid-cols-2">
            <div className="min-w-0 space-y-3">
              <Summary input={input} />
              <p className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">将先执行本地校验与 API Server Dry Run。任何校验或权限错误都会阻止实际写入。</p>
            </div>
            <div className="min-w-0">
              <p className="mb-2 text-sm font-medium">清单预览</p>
              <div className="max-h-96 min-w-0 overflow-auto rounded-md border bg-slate-950 p-3 text-xs text-slate-100">
                {previewMutation.isPending ? <span className="inline-flex items-center gap-2 text-slate-300"><Loader2 className="h-4 w-4 animate-spin" />正在生成预览</span> : null}
                {preview ? <pre className="soft-wrap max-w-full whitespace-pre-wrap font-mono">{preview}</pre> : null}
                {!previewMutation.isPending && !preview ? <span className="text-slate-300">预览暂不可用，请返回修改后重试。</span> : null}
              </div>
            </div>
          </div>
        ) : null}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={createMutation.isPending}>取消</Button>
          {step > 0 ? <Button variant="outline" onClick={() => setStep((value) => value - 1)} disabled={createMutation.isPending}><ChevronLeft className="h-4 w-4" />上一步</Button> : null}
          {step < 2 ? <Button onClick={goNext} disabled={!canCreateCurrent}>下一步<ChevronRight className="h-4 w-4" /></Button> : (
            <Button onClick={submit} disabled={!canCreateCurrent || createMutation.isPending || previewMutation.isPending || !preview}>
              {createMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Rocket className="h-4 w-4" />}
              创建 {kindLabel}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function resolveInitialKind(initialKind: WorkloadCreateKind | undefined, allowedKinds: WorkloadCreateKind[]): WorkloadCreateKind {
  if (initialKind && allowedKinds.includes(initialKind)) return initialKind;
  return allowedKinds[0] ?? "deployment";
}

function StepIndicator({ step }: { step: number }) {
  const steps = ["基本信息", "运行配置", "确认创建"];
  return (
    <ol aria-label="创建步骤" className="grid gap-2 sm:grid-cols-3">
      {steps.map((label, index) => (
        <li key={label} className={cn("flex min-w-0 items-center gap-2 whitespace-nowrap rounded-md border px-3 py-2 text-xs", index === step ? "border-primary/40 bg-primary/5 text-primary" : "text-muted-foreground")}>
          <span className={cn("grid h-5 w-5 place-items-center rounded-full border text-[11px]", index < step ? "border-primary bg-primary text-primary-foreground" : "")}>{index < step ? <Check className="h-3 w-3" /> : index + 1}</span>
          {label}
        </li>
      ))}
    </ol>
  );
}

function Field({ label, htmlFor, error, hint, className, children }: {
  label: string;
  htmlFor: string;
  error?: string;
  hint?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={cn("space-y-1.5", className)}>
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {error ? <p role="alert" className="text-xs text-destructive-strong">{error}</p> : hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function Summary({ input }: { input: WorkloadCreateInput | null }) {
  if (!input) return null;
  return (
    <dl className="grid gap-3 rounded-md border bg-muted/20 p-4 text-sm sm:grid-cols-2">
      <SummaryItem label="类型" value={kindLabels[input.kind]} />
      <SummaryItem label="命名空间" value={input.namespace} />
      <SummaryItem label="名称" value={input.name} />
      <SummaryItem label="镜像" value={input.image} mono />
      {input.kind === "deployment" || input.kind === "statefulset" ? <SummaryItem label="副本" value={String(input.replicas ?? 1)} /> : null}
      {input.kind === "statefulset" ? <SummaryItem label="Headless Service" value={input.serviceName || input.name} /> : null}
      {input.kind === "cronjob" ? <><SummaryItem label="调度" value={input.schedule ?? ""} mono /><SummaryItem label="并发策略 / 初始暂停" value={`${input.concurrencyPolicy ?? "Allow"} / ${input.suspend ? "是" : "否"}`} /></> : null}
      <SummaryItem label="端口" value={String(input.containerPort)} />
      <SummaryItem label="CPU 请求 / 上限" value={`${formatMilli(input.cpuRequestMilli)} / ${formatMilli(input.cpuLimitMilli)}`} />
      <SummaryItem label="内存请求 / 上限" value={`${formatMiB(input.memoryRequestBytes)} / ${formatMiB(input.memoryLimitBytes)}`} />
      <SummaryItem label="环境变量 / 标签" value={`${input.env?.length ?? 0} / ${Object.keys(input.labels ?? {}).length}`} />
    </dl>
  );
}

function SummaryItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return <div><dt className="text-xs text-muted-foreground">{label}</dt><dd className={cn("mt-0.5 break-all", mono && "font-mono text-xs")}>{value || "—"}</dd></div>;
}

function buildInput(form: FormState): WorkloadCreateInput | null {
  const env = parseEnv(form.env);
  const labels = parseLabels(form.labels);
  const port = Number(form.containerPort);
  const cpuRequest = parseCPU(form.cpuRequest);
  const cpuLimit = parseCPU(form.cpuLimit);
  const memoryRequest = parseMiB(form.memoryRequestMi);
  const memoryLimit = parseMiB(form.memoryLimitMi);
  if (env === null || labels === null || !Number.isInteger(port) || cpuRequest === null || cpuLimit === null || memoryRequest === null || memoryLimit === null) return null;
  const replicas = Number(form.replicas);
  if ((form.kind === "deployment" || form.kind === "statefulset") && !Number.isInteger(replicas)) return null;
  return {
    kind: form.kind,
    namespace: form.namespace,
    name: form.name,
    replicas: form.kind === "deployment" || form.kind === "statefulset" ? replicas : undefined,
    serviceName: form.kind === "statefulset" ? form.serviceName || form.name : undefined,
    schedule: form.kind === "cronjob" ? form.schedule.trim() : undefined,
    concurrencyPolicy: form.kind === "cronjob" ? form.concurrencyPolicy : undefined,
    suspend: form.kind === "cronjob" ? form.suspend : undefined,
    containerName: form.containerName || undefined,
    image: form.image.trim(),
    registryId: form.registryId || undefined,
    containerPort: port,
    cpuRequestMilli: cpuRequest,
    cpuLimitMilli: cpuLimit,
    memoryRequestBytes: memoryRequest,
    memoryLimitBytes: memoryLimit,
    env,
    labels,
  };
}

function parseCPU(value: string): number | null {
  if (value.trim() === "") return 0;
  const cores = Number(value);
  if (!Number.isFinite(cores) || cores < 0) return null;
  const milli = Math.round(cores * 1000);
  return milli <= 128000 ? milli : null;
}

function parseMiB(value: string): number | null {
  if (value.trim() === "") return 0;
  const mib = Number(value);
  if (!Number.isFinite(mib) || mib < 0 || !Number.isInteger(mib)) return null;
  return mib <= 1_048_576 ? mib * 1024 * 1024 : null;
}

function parseEnv(value: string): DeploymentEnvVar[] | null {
  const out: DeploymentEnvVar[] = [];
  const seen = new Set<string>();
  for (const line of value.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    if (separator < 1) return null;
    const name = trimmed.slice(0, separator).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) || seen.has(name)) return null;
    seen.add(name);
    out.push({ name, value: trimmed.slice(separator + 1) });
  }
  return out;
}

function parseLabels(value: string): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const line of value.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    if (separator < 1) return null;
    const key = trimmed.slice(0, separator).trim();
    const labelValue = trimmed.slice(separator + 1).trim();
    if (!key || key.length > 253 || labelValue.length > 63 || RESERVED_LABELS.has(key) || key in out) return null;
    out[key] = labelValue;
  }
  return out;
}

function formatMilli(value?: number): string {
  return value ? `${value / 1000} 核` : "未设置";
}

function formatMiB(value?: number): string {
  return value ? `${Math.round(value / 1024 / 1024)} MiB` : "未设置";
}
