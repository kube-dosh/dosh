import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Check, ChevronLeft, ChevronRight, FileCode2, Loader2, Rocket } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { DeploymentCreateInput, DeploymentEnvVar } from "@/api/types";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const RESERVED_LABELS = new Set(["app.kubernetes.io/name", "app.kubernetes.io/managed-by"]);

type FormState = {
  namespace: string;
  name: string;
  replicas: string;
  image: string;
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

const initialForm = (namespace: string): FormState => ({
  namespace: namespace || "default",
  name: "",
  replicas: "1",
  image: "nginx:alpine",
  containerName: "",
  containerPort: "80",
  cpuRequest: "0.1",
  cpuLimit: "0.5",
  memoryRequestMi: "128",
  memoryLimitMi: "512",
  env: "",
  labels: "",
});

export function DeploymentCreateDialog({ open, onOpenChange, initialNamespace }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialNamespace: string;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [step, setStep] = useState(0);
  const [form, setForm] = useState<FormState>(() => initialForm(initialNamespace));
  const [errors, setErrors] = useState<FormErrors>({});
  const [preview, setPreview] = useState("");

  useEffect(() => {
    if (!open) return;
    setStep(0);
    setForm(initialForm(initialNamespace));
    setErrors({});
    setPreview("");
  }, [open, initialNamespace]);

  const input = useMemo(() => buildInput(form, errors), [form, errors]);

  const previewMutation = useMutation({
    mutationFn: (value: DeploymentCreateInput) => api.previewDeployment(value),
    onSuccess: (result) => setPreview(result.yaml),
    onError: (error) => toast.error(error.message),
  });

  const createMutation = useMutation({
    mutationFn: (value: DeploymentCreateInput) => api.createDeployment(value),
    onSuccess: async (created) => {
      toast.success(`Deployment ${created.name} 已创建`);
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      onOpenChange(false);
      await navigate({ to: "/workloads/$kind/$namespace/$name", params: { kind: "deployment", namespace: created.namespace, name: created.name } });
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

  const update = (key: keyof FormState, value: string) => {
    setForm((current) => ({ ...current, [key]: value }));
    setErrors((current) => {
      const next = { ...current };
      delete next[key];
      delete next.form;
      return next;
    });
    setPreview("");
  };

  const validateBasics = (): FormErrors => {
    const next: FormErrors = {};
    if (!form.namespace) next.namespace = "请选择命名空间";
    else if (form.namespace.length > 63 || !DNS_LABEL.test(form.namespace)) next.namespace = "命名空间必须是 DNS-1123 标签";
    if (!form.name) next.name = "请输入 Deployment 名称";
    else if (form.name.length > 63 || !DNS_LABEL.test(form.name)) next.name = "名称必须是小写 DNS-1123 标签，最长 63 字符";
    const replicas = Number(form.replicas);
    if (!Number.isInteger(replicas) || replicas < 0 || replicas > 1000) next.replicas = "副本数必须是 0 到 1000 的整数";
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
          <DialogTitle className="flex items-center gap-2"><Rocket className="h-5 w-5 text-primary" />创建 Deployment</DialogTitle>
          <DialogDescription>通过受校验的向导创建单个应用容器；提交前由 Kubernetes API Server 执行 Dry Run。</DialogDescription>
        </DialogHeader>

        <StepIndicator step={step} />

        {errors.form ? <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{errors.form}</div> : null}

        {step === 0 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="命名空间" htmlFor="deployment-namespace" error={errors.namespace}>
              <NamespaceSelect value={form.namespace} onChange={(value) => update("namespace", value)} allowAll={false} className="w-full" />
            </Field>
            <Field label="Deployment 名称" htmlFor="deployment-name" error={errors.name}>
              <Input id="deployment-name" value={form.name} onChange={(event) => update("name", event.target.value)} placeholder="web-api" autoComplete="off" />
            </Field>
            <Field label="副本数" htmlFor="deployment-replicas" error={errors.replicas}>
              <Input id="deployment-replicas" type="number" min={0} max={1000} step={1} value={form.replicas} onChange={(event) => update("replicas", event.target.value)} />
            </Field>
            <div className="rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground sm:col-span-2">
              将自动添加 <span className="font-mono">app.kubernetes.io/name</span> 与 <span className="font-mono">app.kubernetes.io/managed-by=dosh</span> 标签作为稳定选择器。
            </div>
          </div>
        ) : null}

        {step === 1 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="容器镜像" htmlFor="deployment-image" error={errors.image} className="sm:col-span-2">
              <Input id="deployment-image" value={form.image} onChange={(event) => update("image", event.target.value)} placeholder="registry.example.com/team/app:1.0.0" className="font-mono" />
            </Field>
            <Field label="容器名称（可选）" htmlFor="deployment-container-name" error={errors.containerName}>
              <Input id="deployment-container-name" value={form.containerName} onChange={(event) => update("containerName", event.target.value)} placeholder="默认使用 Deployment 名称" />
            </Field>
            <Field label="容器端口" htmlFor="deployment-port" error={errors.containerPort}>
              <Input id="deployment-port" type="number" min={1} max={65535} value={form.containerPort} onChange={(event) => update("containerPort", event.target.value)} />
            </Field>
            <Field label="CPU 请求（核）" htmlFor="deployment-cpu-request" error={errors.cpuRequest}>
              <Input id="deployment-cpu-request" type="number" min={0} step={0.1} value={form.cpuRequest} onChange={(event) => update("cpuRequest", event.target.value)} />
            </Field>
            <Field label="CPU 上限（核）" htmlFor="deployment-cpu-limit" error={errors.cpuLimit}>
              <Input id="deployment-cpu-limit" type="number" min={0} step={0.1} value={form.cpuLimit} onChange={(event) => update("cpuLimit", event.target.value)} />
            </Field>
            <Field label="内存请求（MiB）" htmlFor="deployment-memory-request" error={errors.memoryRequestMi}>
              <Input id="deployment-memory-request" type="number" min={0} step={1} value={form.memoryRequestMi} onChange={(event) => update("memoryRequestMi", event.target.value)} />
            </Field>
            <Field label="内存上限（MiB）" htmlFor="deployment-memory-limit" error={errors.memoryLimitMi}>
              <Input id="deployment-memory-limit" type="number" min={0} step={1} value={form.memoryLimitMi} onChange={(event) => update("memoryLimitMi", event.target.value)} />
            </Field>
            <Field label="环境变量" htmlFor="deployment-env" error={errors.env} hint="每行一个 KEY=value，值可包含等号。">
              <Textarea id="deployment-env" value={form.env} onChange={(event) => update("env", event.target.value)} placeholder={"MODE=production\nLOG_LEVEL=info"} className="min-h-28 font-mono text-xs" />
            </Field>
            <Field label="自定义标签" htmlFor="deployment-labels" error={errors.labels} hint="每行一个 key=value。">
              <Textarea id="deployment-labels" value={form.labels} onChange={(event) => update("labels", event.target.value)} placeholder={"team=platform\nenvironment=production"} className="min-h-28 font-mono text-xs" />
            </Field>
          </div>
        ) : null}

        {step === 2 ? (
          <div className="space-y-4">
            <Summary input={input} />
            <div>
              <div className="mb-2 flex items-center gap-2 text-sm font-medium"><FileCode2 className="h-4 w-4" />YAML 预览</div>
              <div className="max-h-72 overflow-auto rounded-md border bg-slate-950 p-3 text-xs text-slate-100">
                {previewMutation.isPending ? <span className="flex items-center gap-2 text-slate-300"><Loader2 className="h-4 w-4 animate-spin" />正在生成预览</span> : null}
                {preview ? <pre className="whitespace-pre-wrap font-mono">{preview}</pre> : null}
                {!previewMutation.isPending && !preview ? <span className="text-slate-300">预览暂不可用，请返回修改后重试。</span> : null}
              </div>
            </div>
          </div>
        ) : null}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={createMutation.isPending}>取消</Button>
          {step > 0 ? <Button variant="outline" onClick={() => setStep((value) => value - 1)} disabled={createMutation.isPending}><ChevronLeft className="h-4 w-4" />上一步</Button> : null}
          {step < 2 ? <Button onClick={goNext}>下一步<ChevronRight className="h-4 w-4" /></Button> : (
            <Button onClick={submit} disabled={createMutation.isPending || previewMutation.isPending || !preview}>
              {createMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Rocket className="h-4 w-4" />}
              创建 Deployment
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StepIndicator({ step }: { step: number }) {
  const steps = ["基本信息", "运行配置", "确认创建"];
  return (
    <ol aria-label="创建步骤" className="grid grid-cols-3 gap-2">
      {steps.map((label, index) => (
        <li key={label} className={cn("flex items-center gap-2 rounded-md border px-3 py-2 text-xs", index === step ? "border-primary/40 bg-primary/5 text-primary" : "text-muted-foreground")}>
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

function Summary({ input }: { input: DeploymentCreateInput | null }) {
  if (!input) return null;
  return (
    <dl className="grid gap-3 rounded-md border bg-muted/20 p-4 text-sm sm:grid-cols-2">
      <SummaryItem label="命名空间" value={input.namespace} />
      <SummaryItem label="名称" value={input.name} />
      <SummaryItem label="镜像" value={input.image} mono />
      <SummaryItem label="副本" value={String(input.replicas)} />
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

function buildInput(form: FormState, errors: FormErrors): DeploymentCreateInput | null {
  const env = parseEnv(form.env);
  const labels = parseLabels(form.labels);
  const port = Number(form.containerPort);
  const replicas = Number(form.replicas);
  const cpuRequest = parseCPU(form.cpuRequest);
  const cpuLimit = parseCPU(form.cpuLimit);
  const memoryRequest = parseMiB(form.memoryRequestMi);
  const memoryLimit = parseMiB(form.memoryLimitMi);
  if (Object.keys(errors).length > 0 || env === null || labels === null || !Number.isInteger(port) || !Number.isInteger(replicas) || cpuRequest === null || cpuLimit === null || memoryRequest === null || memoryLimit === null) return null;
  return {
    namespace: form.namespace,
    name: form.name,
    replicas,
    containerName: form.containerName || undefined,
    image: form.image.trim(),
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
