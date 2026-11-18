import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Check, ChevronLeft, ChevronRight, Loader2, Plus, Rocket, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type {
  Capabilities,
  DeploymentEnvVar,
  WorkloadContainerSecurityContext,
  WorkloadContainerSpec,
  WorkloadCreateInput,
  WorkloadCreateKind,
  WorkloadNodeSelectorRequirement,
  WorkloadPodSecurityContext,
  WorkloadToleration,
  WorkloadTopologySpreadConstraint,
} from "@/api/types";
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
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
const CAPABILITY = /^[A-Z][A-Z0-9_]*$/;
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

type TriState = "" | "true" | "false";
type ContainerKind = "containers" | "initContainers";
type TolerationEffect = "" | "NoSchedule" | "PreferNoSchedule" | "NoExecute";

type ContainerForm = {
  id: string;
  name: string;
  image: string;
  registryId: string;
  containerPort: string;
  cpuRequest: string;
  cpuLimit: string;
  memoryRequestMi: string;
  memoryLimitMi: string;
  env: string;
  command: string;
  args: string;
  runAsUser: string;
  runAsGroup: string;
  runAsNonRoot: TriState;
  readOnlyRootFilesystem: boolean;
  allowPrivilegeEscalation: TriState;
  capabilitiesAdd: string;
  capabilitiesDrop: string;
};

type TolerationForm = {
  id: string;
  key: string;
  operator: "Equal" | "Exists";
  value: string;
  effect: TolerationEffect;
  tolerationSeconds: string;
};

type NodeAffinityForm = {
  id: string;
  key: string;
  operator: "In" | "NotIn" | "Exists" | "DoesNotExist";
  values: string;
};

type TopologyForm = {
  id: string;
  maxSkew: string;
  topologyKey: string;
  whenUnsatisfiable: "DoNotSchedule" | "ScheduleAnyway";
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
  labels: string;
  containers: ContainerForm[];
  initContainers: ContainerForm[];
  nodeSelector: string;
  tolerations: TolerationForm[];
  nodeAffinity: NodeAffinityForm[];
  topologySpreadConstraints: TopologyForm[];
  priorityClassName: string;
  serviceAccountName: string;
  podRunAsUser: string;
  podRunAsGroup: string;
  podFsGroup: string;
  podRunAsNonRoot: TriState;
  podSeccompProfile: "" | "RuntimeDefault" | "Localhost" | "Unconfined";
  jobCompletions: string;
  jobParallelism: string;
  jobBackoffLimit: string;
  jobActiveDeadlineSeconds: string;
  jobTtlSecondsAfterFinished: string;
};

type FormErrors = Record<string, string>;

let sequence = 0;
function nextID(prefix: string): string {
  sequence += 1;
  return `${prefix}-${sequence}`;
}

function newContainer(primary = false, init = false): ContainerForm {
  return {
    id: nextID(init ? "init" : "container"),
    name: "",
    image: primary ? "nginx:alpine" : "",
    registryId: "",
    containerPort: "80",
    cpuRequest: primary ? "0.1" : "0",
    cpuLimit: primary ? "0.5" : "0",
    memoryRequestMi: primary ? "128" : "0",
    memoryLimitMi: primary ? "512" : "0",
    env: "",
    command: "",
    args: "",
    runAsUser: "",
    runAsGroup: "",
    runAsNonRoot: "",
    readOnlyRootFilesystem: false,
    allowPrivilegeEscalation: "",
    capabilitiesAdd: "",
    capabilitiesDrop: "",
  };
}

function newToleration(): TolerationForm {
  return { id: nextID("toleration"), key: "", operator: "Equal", value: "", effect: "", tolerationSeconds: "" };
}

function newAffinity(): NodeAffinityForm {
  return { id: nextID("affinity"), key: "", operator: "In", values: "" };
}

function newTopology(): TopologyForm {
  return { id: nextID("topology"), maxSkew: "1", topologyKey: "topology.kubernetes.io/zone", whenUnsatisfiable: "ScheduleAnyway" };
}

const initialForm = (namespace: string, kind: WorkloadCreateKind): FormState => ({
  kind,
  namespace: namespace || "default",
  name: "",
  replicas: "1",
  serviceName: "",
  schedule: "*/5 * * * *",
  concurrencyPolicy: "Allow",
  suspend: false,
  labels: "",
  containers: [newContainer(true)],
  initContainers: [],
  nodeSelector: "",
  tolerations: [],
  nodeAffinity: [],
  topologySpreadConstraints: [],
  priorityClassName: "",
  serviceAccountName: "",
  podRunAsUser: "",
  podRunAsGroup: "",
  podFsGroup: "",
  podRunAsNonRoot: "",
  podSeccompProfile: "",
  jobCompletions: "",
  jobParallelism: "",
  jobBackoffLimit: "",
  jobActiveDeadlineSeconds: "",
  jobTtlSecondsAfterFinished: "",
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
  const isBatch = form.kind === "job" || form.kind === "cronjob";

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
        setErrors({ ...error.fieldErrors, form: error.message });
      } else {
        toast.error(error.message);
        setErrors({ form: error.message });
      }
      setStep(1);
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

  const updateContainer = (list: ContainerKind, index: number, patch: Partial<ContainerForm>) => {
    setForm((current) => ({ ...current, [list]: current[list].map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item) }));
    setErrors((current) => {
      const next = { ...current };
      Object.keys(next).filter((key) => key.startsWith(`${list}.${index}.`)).forEach((key) => delete next[key]);
      delete next.form;
      return next;
    });
    setPreview("");
  };

  const addContainer = (list: ContainerKind) => update(list, [...form[list], newContainer(false, list === "initContainers")]);
  const removeContainer = (list: ContainerKind, index: number) => {
    if (form[list].length <= 1 && list === "containers") return;
    update(list, form[list].filter((_, itemIndex) => itemIndex !== index));
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
    const names = new Set<string>();
    const resolvedNames: Array<{ key: string; name: string }> = [];
    const validateList = (list: ContainerKind) => {
      form[list].forEach((container, index) => {
        const prefix = `${list}.${index}`;
        const fallbackName = list === "containers" && index === 0 ? form.name : "";
        const name = container.name.trim() || fallbackName;
        if (!name || name.length > 63 || !DNS_LABEL.test(name)) next[`${prefix}.name`] = "容器名称必须是 DNS-1123 标签";
        else resolvedNames.push({ key: `${prefix}.name`, name });
        if (!container.image.trim() || container.image.trim().length > 512) next[`${prefix}.image`] = "请输入有效的镜像地址";
        const port = parsePort(container.containerPort);
        if (port === null) next[`${prefix}.containerPort`] = "端口必须是空值或 1 到 65535 的整数";
        const cpuRequest = parseCPU(container.cpuRequest);
        const cpuLimit = parseCPU(container.cpuLimit);
        if (cpuRequest === null || cpuLimit === null) next[`${prefix}.cpuRequest`] = "CPU 必须是非负数字，例如 0.5";
        else if (cpuLimit > 0 && cpuLimit < cpuRequest) next[`${prefix}.cpuLimit`] = "CPU 上限不能小于请求";
        const memoryRequest = parseMiB(container.memoryRequestMi);
        const memoryLimit = parseMiB(container.memoryLimitMi);
        if (memoryRequest === null || memoryLimit === null) next[`${prefix}.memoryRequestMi`] = "内存必须是非负 MiB 整数";
        else if (memoryLimit > 0 && memoryLimit < memoryRequest) next[`${prefix}.memoryLimitMi`] = "内存上限不能小于请求";
        if (parseEnv(container.env) === null) next[`${prefix}.env`] = "环境变量格式应为每行 KEY=value";
        const command = parseLines(container.command);
        const args = parseLines(container.args);
        if (command === null || command.length > 128 || command.some((value) => value.length > 4096)) next[`${prefix}.command`] = "命令最多 128 行，每行不超过 4096 字符";
        if (args === null || args.length > 128 || args.some((value) => value.length > 4096)) next[`${prefix}.args`] = "参数最多 128 行，每行不超过 4096 字符";
        if (parseOptionalNonNegative(container.runAsUser) === null) next[`${prefix}.runAsUser`] = "必须是空值或非负整数";
        if (parseOptionalNonNegative(container.runAsGroup) === null) next[`${prefix}.runAsGroup`] = "必须是空值或非负整数";
        if (parseCapabilities(container.capabilitiesAdd) === null) next[`${prefix}.capabilitiesAdd`] = "使用逗号分隔大写能力名称，例如 NET_ADMIN";
        if (parseCapabilities(container.capabilitiesDrop) === null) next[`${prefix}.capabilitiesDrop`] = "使用逗号分隔大写能力名称，例如 ALL";
      });
    };
    validateList("containers");
    validateList("initContainers");
    resolvedNames.forEach((item) => {
      if (names.has(item.name)) next[item.key] = `容器名称 ${item.name} 重复`;
      names.add(item.name);
    });
    if (parseKeyValues(form.labels, true) === null) next.labels = "标签格式应为每行 key=value，且不能覆盖 dosh 管理标签";
    if (parseKeyValues(form.nodeSelector) === null) next.nodeSelector = "节点选择器格式应为每行 key=value";
    if ((form.priorityClassName && (form.priorityClassName.length > 63 || !DNS_LABEL.test(form.priorityClassName))) ||
      (form.serviceAccountName && (form.serviceAccountName.length > 63 || !DNS_LABEL.test(form.serviceAccountName)))) {
      next.scheduling = "PriorityClass 和 ServiceAccount 必须是 DNS-1123 标签";
    }
    form.tolerations.forEach((toleration, index) => {
      if (!toleration.key.trim() || toleration.key.length > 253) next[`tolerations.${index}.key`] = "容忍键不能为空";
      if (toleration.operator === "Equal" && !toleration.value) next[`tolerations.${index}.value`] = "Equal 操作符需要值";
      if (toleration.tolerationSeconds && parseOptionalNonNegative(toleration.tolerationSeconds) === null) next[`tolerations.${index}.tolerationSeconds`] = "必须是空值或非负整数";
    });
    form.nodeAffinity.forEach((affinity, index) => {
      if (!affinity.key.trim()) next[`nodeAffinity.${index}.key`] = "节点亲和性键不能为空";
      const values = commaValues(affinity.values);
      if ((affinity.operator === "In" || affinity.operator === "NotIn") && !values.length) next[`nodeAffinity.${index}.values`] = "该操作符至少需要一个值";
    });
    form.topologySpreadConstraints.forEach((constraint, index) => {
      const skew = Number(constraint.maxSkew);
      if (!Number.isInteger(skew) || skew < 1 || skew > 100) next[`topology.${index}.maxSkew`] = "maxSkew 必须是 1 到 100 的整数";
      if (!constraint.topologyKey.trim()) next[`topology.${index}.topologyKey`] = "拓扑键不能为空";
    });
    if (parseOptionalNonNegative(form.podRunAsUser) === null || parseOptionalNonNegative(form.podRunAsGroup) === null || parseOptionalNonNegative(form.podFsGroup) === null) {
      next.podSecurity = "Pod SecurityContext 的 UID/GID 必须是空值或非负整数";
    }
    if (isBatch) {
      if (parseOptionalInteger(form.jobCompletions, 1, 10000) === null) next.jobCompletions = "完成数必须是 1 到 10000 的整数";
      if (parseOptionalInteger(form.jobParallelism, 0, 10000) === null) next.jobParallelism = "并行度必须是 0 到 10000 的整数";
      if (parseOptionalInteger(form.jobBackoffLimit, 0, 10000) === null) next.jobBackoffLimit = "重试上限必须是 0 到 10000 的整数";
      if (parseOptionalInteger(form.jobActiveDeadlineSeconds, 1, 604800) === null) next.jobActiveDeadlineSeconds = "活动期限必须是 1 到 604800 秒";
      if (parseOptionalInteger(form.jobTtlSecondsAfterFinished, 0, 2592000) === null) next.jobTtlSecondsAfterFinished = "TTL 必须是 0 到 2592000 秒";
    }
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
    setPreview("");
    setStep(2);
    previewMutation.mutate(input);
  };

  const submit = () => {
    const basics = validateBasics();
    const runtime = validateRuntime();
    const next = { ...basics, ...runtime };
    setErrors(next);
    if (Object.keys(next).length > 0) {
      setStep(Object.keys(basics).length > 0 ? 0 : 1);
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
      <DialogContent className="max-h-[92vh] w-[calc(100vw-1rem)] max-w-3xl overflow-y-auto sm:w-[calc(100%-2rem)]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Rocket className="h-5 w-5 text-primary" />创建工作负载</DialogTitle>
          <DialogDescription>支持多容器、Init Container、调度约束和安全上下文；提交前由 Kubernetes API Server 执行 Dry Run。</DialogDescription>
        </DialogHeader>

        <StepIndicator step={step} />

        {errors.form ? <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 break-words text-sm text-destructive-strong">{errors.form}</div> : null}
        {!allowedKinds.length ? <div role="alert" className="rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-sm text-warning">当前身份没有创建任何受支持工作负载的权限。</div> : null}

        {step === 0 ? (
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
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
              <label className="flex min-w-0 items-center gap-2 text-sm sm:col-span-2"><input type="checkbox" checked={form.suspend} onChange={(event) => update("suspend", event.target.checked)} />创建后暂停调度</label>
            </> : null}
          </div>
        ) : null}

        {step === 1 ? (
          <div className="min-w-0 space-y-5">
            <section className="min-w-0 space-y-3" aria-labelledby="workload-containers-heading">
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <h2 id="workload-containers-heading" className="text-sm font-semibold">应用容器 ({form.containers.length})</h2>
                  <p className="text-xs text-muted-foreground">首个容器可省略名称，默认使用工作负载名称；所有容器名称必须唯一。</p>
                </div>
                <Button type="button" variant="outline" size="sm" onClick={() => addContainer("containers")} disabled={form.containers.length >= 32}><Plus className="h-4 w-4" />添加应用容器</Button>
              </div>
              {form.containers.map((container, index) => (
                <ContainerEditor
                  key={container.id}
                  list="containers"
                  index={index}
                  container={container}
                  errors={errors}
                  placeholderName={index === 0 ? form.name : ""}
                  onChange={(patch) => updateContainer("containers", index, patch)}
                  onRemove={form.containers.length > 1 ? () => removeContainer("containers", index) : undefined}
                />
              ))}
            </section>

            <section className="min-w-0 space-y-3" aria-labelledby="workload-init-containers-heading">
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <h2 id="workload-init-containers-heading" className="text-sm font-semibold">Init Container ({form.initContainers.length})</h2>
                  <p className="text-xs text-muted-foreground">按顺序在主容器之前运行，通常用于初始化配置、数据或依赖等待。</p>
                </div>
                <Button type="button" variant="outline" size="sm" onClick={() => addContainer("initContainers")} disabled={form.initContainers.length >= 32}><Plus className="h-4 w-4" />添加 Init Container</Button>
              </div>
              {form.initContainers.length ? form.initContainers.map((container, index) => (
                <ContainerEditor
                  key={container.id}
                  list="initContainers"
                  index={index}
                  container={container}
                  errors={errors}
                  onChange={(patch) => updateContainer("initContainers", index, patch)}
                  onRemove={() => removeContainer("initContainers", index)}
                />
              )) : <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">当前没有 Init Container</div>}
            </section>

            <Field label="自定义标签" htmlFor="workload-labels" error={errors.labels} hint="每行一个 key=value，不能覆盖 dosh 管理标签。">
              <Textarea id="workload-labels" value={form.labels} onChange={(event) => update("labels", event.target.value)} placeholder={"team=platform\nenvironment=production"} className="min-h-24 font-mono text-xs" />
            </Field>

            <SchedulingEditor form={form} errors={errors} update={update} />

            {isBatch ? (
              <details className="min-w-0 rounded-lg border bg-muted/10" open>
                <summary className="cursor-pointer px-3 py-3 text-sm font-semibold marker:text-primary">Job 执行参数</summary>
                <div className="grid min-w-0 gap-3 border-t p-3 sm:grid-cols-2">
                  <Field label="完成数 completions" htmlFor="workload-job-completions" error={errors.jobCompletions} hint="留空由 Kubernetes 使用默认值。">
                    <Input id="workload-job-completions" type="number" min={1} max={10000} value={form.jobCompletions} onChange={(event) => update("jobCompletions", event.target.value)} />
                  </Field>
                  <Field label="并行度 parallelism" htmlFor="workload-job-parallelism" error={errors.jobParallelism}>
                    <Input id="workload-job-parallelism" type="number" min={0} max={10000} value={form.jobParallelism} onChange={(event) => update("jobParallelism", event.target.value)} />
                  </Field>
                  <Field label="失败重试上限 backoffLimit" htmlFor="workload-job-backoff-limit" error={errors.jobBackoffLimit}>
                    <Input id="workload-job-backoff-limit" type="number" min={0} max={10000} value={form.jobBackoffLimit} onChange={(event) => update("jobBackoffLimit", event.target.value)} />
                  </Field>
                  <Field label="活动期限（秒）" htmlFor="workload-job-deadline" error={errors.jobActiveDeadlineSeconds}>
                    <Input id="workload-job-deadline" type="number" min={1} max={604800} value={form.jobActiveDeadlineSeconds} onChange={(event) => update("jobActiveDeadlineSeconds", event.target.value)} />
                  </Field>
                  <Field label="完成后 TTL（秒）" htmlFor="workload-job-ttl" error={errors.jobTtlSecondsAfterFinished} className="sm:col-span-2">
                    <Input id="workload-job-ttl" type="number" min={0} max={2592000} value={form.jobTtlSecondsAfterFinished} onChange={(event) => update("jobTtlSecondsAfterFinished", event.target.value)} />
                  </Field>
                </div>
              </details>
            ) : null}
          </div>
        ) : null}

        {step === 2 ? (
          <div className="grid min-w-0 gap-4 lg:grid-cols-2">
            <div className="min-w-0 space-y-3">
              <Summary input={input} form={form} />
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

        <DialogFooter className="gap-2 sm:gap-2">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={createMutation.isPending}>取消</Button>
          {step > 0 ? <Button type="button" variant="outline" onClick={() => setStep((value) => value - 1)} disabled={createMutation.isPending}><ChevronLeft className="h-4 w-4" />上一步</Button> : null}
          {step < 2 ? <Button type="button" onClick={goNext} disabled={!canCreateCurrent}>下一步<ChevronRight className="h-4 w-4" /></Button> : (
            <Button type="button" onClick={submit} disabled={!canCreateCurrent || createMutation.isPending || previewMutation.isPending || !preview}>
              {createMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Rocket className="h-4 w-4" />}
              创建 {kindLabel}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ContainerEditor({ list, index, container, errors, placeholderName, onChange, onRemove }: {
  list: ContainerKind;
  index: number;
  container: ContainerForm;
  errors: FormErrors;
  placeholderName?: string;
  onChange: (patch: Partial<ContainerForm>) => void;
  onRemove?: () => void;
}) {
  const init = list === "initContainers";
  const prefix = `${list}.${index}`;
  const idBase = init ? `workload-init-${index}` : index === 0 ? "workload" : `workload-${index}`;
  const title = init ? `Init Container ${index + 1}` : `应用容器 ${index + 1}`;
  return (
    <div className="min-w-0 overflow-hidden rounded-lg border bg-card">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 border-b bg-muted/20 px-3 py-2.5">
        <p className="min-w-0 break-all text-sm font-medium">{title}{container.name ? <span className="ml-2 font-mono text-xs text-muted-foreground">{container.name}</span> : null}</p>
        {onRemove ? <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-destructive" onClick={onRemove} aria-label={`删除 ${title}`}><Trash2 className="h-3.5 w-3.5" />删除</Button> : null}
      </div>
      <div className="grid min-w-0 gap-3 p-3 sm:grid-cols-2 sm:p-4">
        <Field label={init ? "容器名称" : "容器名称（可选）"} htmlFor={`${idBase}-container-name`} error={errors[`${prefix}.name`]}>
          <Input id={`${idBase}-container-name`} value={container.name} onChange={(event) => onChange({ name: event.target.value })} placeholder={placeholderName || (init ? `init-${index + 1}` : "")} autoComplete="off" />
        </Field>
        <Field label="容器端口" htmlFor={`${idBase}-port`} error={errors[`${prefix}.containerPort`]} hint="可留空；仅声明容器监听端口，不创建 Service。">
          <Input id={`${idBase}-port`} type="number" min={0} max={65535} value={container.containerPort} onChange={(event) => onChange({ containerPort: event.target.value })} />
        </Field>
        <div className="min-w-0 space-y-1.5 sm:col-span-2">
          <ImageReferencePicker
            id={`${idBase}-image`}
            value={container.image}
            registryId={container.registryId || undefined}
            onChange={(image, registryId) => onChange({ image, registryId: registryId ?? "" })}
            label={`${init ? "Init Container " : ""}容器镜像${index > 0 ? ` ${index + 1}` : ""}`}
            hint="支持从已配置镜像仓搜索仓库与 Tag，也能直接输入完整镜像地址。"
          />
          {errors[`${prefix}.image`] ? <p role="alert" className="text-xs text-destructive-strong">{errors[`${prefix}.image`]}</p> : null}
        </div>
        <Field label="CPU 请求（核）" htmlFor={`${idBase}-cpu-request`} error={errors[`${prefix}.cpuRequest`]}>
          <Input id={`${idBase}-cpu-request`} type="number" min={0} step={0.1} value={container.cpuRequest} onChange={(event) => onChange({ cpuRequest: event.target.value })} />
        </Field>
        <Field label="CPU 上限（核）" htmlFor={`${idBase}-cpu-limit`} error={errors[`${prefix}.cpuLimit`]}>
          <Input id={`${idBase}-cpu-limit`} type="number" min={0} step={0.1} value={container.cpuLimit} onChange={(event) => onChange({ cpuLimit: event.target.value })} />
        </Field>
        <Field label="内存请求（MiB）" htmlFor={`${idBase}-memory-request`} error={errors[`${prefix}.memoryRequestMi`]}>
          <Input id={`${idBase}-memory-request`} type="number" min={0} step={1} value={container.memoryRequestMi} onChange={(event) => onChange({ memoryRequestMi: event.target.value })} />
        </Field>
        <Field label="内存上限（MiB）" htmlFor={`${idBase}-memory-limit`} error={errors[`${prefix}.memoryLimitMi`]}>
          <Input id={`${idBase}-memory-limit`} type="number" min={0} step={1} value={container.memoryLimitMi} onChange={(event) => onChange({ memoryLimitMi: event.target.value })} />
        </Field>
        <Field label="环境变量" htmlFor={`${idBase}-env`} error={errors[`${prefix}.env`]} hint="每行一个 KEY=value，值可包含等号。" className="sm:col-span-2">
          <Textarea id={`${idBase}-env`} value={container.env} onChange={(event) => onChange({ env: event.target.value })} placeholder={"MODE=production\nLOG_LEVEL=info"} className="min-h-24 font-mono text-xs" />
        </Field>
        <Field label="Command" htmlFor={`${idBase}-command`} error={errors[`${prefix}.command`]} hint="每行一个命令参数，留空使用镜像默认 ENTRYPOINT。">
          <Textarea id={`${idBase}-command`} value={container.command} onChange={(event) => onChange({ command: event.target.value })} placeholder={"/bin/sh"} className="min-h-20 font-mono text-xs" spellCheck={false} />
        </Field>
        <Field label="Args" htmlFor={`${idBase}-args`} error={errors[`${prefix}.args`]} hint="每行一个参数，留空使用镜像默认 CMD。">
          <Textarea id={`${idBase}-args`} value={container.args} onChange={(event) => onChange({ args: event.target.value })} placeholder={"-c\nsleep 3600"} className="min-h-20 font-mono text-xs" spellCheck={false} />
        </Field>
        <details className="min-w-0 rounded-md border bg-muted/10 sm:col-span-2">
          <summary className="cursor-pointer px-3 py-2.5 text-xs font-medium">容器安全上下文（可选）</summary>
          <div className="grid min-w-0 gap-3 border-t p-3 sm:grid-cols-2">
            <Field label="runAsUser" htmlFor={`${idBase}-run-as-user`} error={errors[`${prefix}.runAsUser`]}><Input id={`${idBase}-run-as-user`} type="number" min={0} value={container.runAsUser} onChange={(event) => onChange({ runAsUser: event.target.value })} /></Field>
            <Field label="runAsGroup" htmlFor={`${idBase}-run-as-group`} error={errors[`${prefix}.runAsGroup`]}><Input id={`${idBase}-run-as-group`} type="number" min={0} value={container.runAsGroup} onChange={(event) => onChange({ runAsGroup: event.target.value })} /></Field>
            <Field label="runAsNonRoot" htmlFor={`${idBase}-run-as-non-root`}>
              <Select value={container.runAsNonRoot || "unset"} onValueChange={(value) => onChange({ runAsNonRoot: (value === "unset" ? "" : value) as TriState })}><SelectTrigger id={`${idBase}-run-as-non-root`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="unset">未设置</SelectItem><SelectItem value="true">是</SelectItem><SelectItem value="false">否</SelectItem></SelectContent></Select>
            </Field>
            <Field label="allowPrivilegeEscalation" htmlFor={`${idBase}-privilege-escalation`}>
              <Select value={container.allowPrivilegeEscalation || "unset"} onValueChange={(value) => onChange({ allowPrivilegeEscalation: (value === "unset" ? "" : value) as TriState })}><SelectTrigger id={`${idBase}-privilege-escalation`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="unset">未设置</SelectItem><SelectItem value="true">允许</SelectItem><SelectItem value="false">禁止</SelectItem></SelectContent></Select>
            </Field>
            <label className="flex min-w-0 items-center gap-2 text-sm sm:col-span-2"><input type="checkbox" checked={container.readOnlyRootFilesystem} onChange={(event) => onChange({ readOnlyRootFilesystem: event.target.checked })} />只读根文件系统</label>
            <Field label="添加 Linux Capabilities" htmlFor={`${idBase}-capabilities-add`} error={errors[`${prefix}.capabilitiesAdd`]} hint="逗号分隔，例如 NET_ADMIN,SYS_TIME。">
              <Input id={`${idBase}-capabilities-add`} value={container.capabilitiesAdd} onChange={(event) => onChange({ capabilitiesAdd: event.target.value })} placeholder="NET_ADMIN" className="font-mono" />
            </Field>
            <Field label="移除 Linux Capabilities" htmlFor={`${idBase}-capabilities-drop`} error={errors[`${prefix}.capabilitiesDrop`]} hint="常用 ALL 移除全部能力。">
              <Input id={`${idBase}-capabilities-drop`} value={container.capabilitiesDrop} onChange={(event) => onChange({ capabilitiesDrop: event.target.value })} placeholder="ALL" className="font-mono" />
            </Field>
          </div>
        </details>
      </div>
    </div>
  );
}

function SchedulingEditor({ form, errors, update }: {
  form: FormState;
  errors: FormErrors;
  update: <K extends keyof FormState>(key: K, value: FormState[K]) => void;
}) {
  return (
    <details className="min-w-0 rounded-lg border bg-muted/10">
      <summary className="cursor-pointer px-3 py-3 text-sm font-semibold marker:text-primary">高级调度与 Pod 安全上下文</summary>
      <div className="min-w-0 space-y-5 border-t p-3">
        <div className="grid min-w-0 gap-3 sm:grid-cols-2">
          <Field label="PriorityClass" htmlFor="workload-priority-class" error={errors.scheduling} hint="必须是集群中已存在的 PriorityClass。">
            <Input id="workload-priority-class" value={form.priorityClassName} onChange={(event) => update("priorityClassName", event.target.value)} placeholder="high-priority" />
          </Field>
          <Field label="ServiceAccount" htmlFor="workload-service-account" hint="工作负载将使用该身份访问 Kubernetes API。">
            <Input id="workload-service-account" value={form.serviceAccountName} onChange={(event) => update("serviceAccountName", event.target.value)} placeholder="default" />
          </Field>
          <Field label="NodeSelector" htmlFor="workload-node-selector" error={errors.nodeSelector} hint="每行一个 key=value，Pod 只会调度到全部匹配的节点。" className="sm:col-span-2">
            <Textarea id="workload-node-selector" value={form.nodeSelector} onChange={(event) => update("nodeSelector", event.target.value)} placeholder={"node-pool=gpu\nkubernetes.io/os=linux"} className="min-h-20 font-mono text-xs" />
          </Field>
        </div>

        <section className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Tolerations</h3><Button type="button" variant="outline" size="sm" onClick={() => update("tolerations", [...form.tolerations, newToleration()])} disabled={form.tolerations.length >= 32}><Plus className="h-3.5 w-3.5" />添加容忍</Button></div>
          {form.tolerations.length ? form.tolerations.map((row, index) => (
            <div key={row.id} className="min-w-0 rounded-md border bg-card p-3">
              <div className="mb-2 flex items-center justify-between gap-2"><span className="text-xs text-muted-foreground">容忍 {index + 1}</span><Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-destructive" aria-label={`删除容忍 ${index + 1}`} onClick={() => update("tolerations", form.tolerations.filter((item) => item.id !== row.id))}><Trash2 className="h-3.5 w-3.5" /></Button></div>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <Field label="键" htmlFor={`toleration-key-${row.id}`} error={errors[`tolerations.${index}.key`]}><Input id={`toleration-key-${row.id}`} value={row.key} onChange={(event) => update("tolerations", form.tolerations.map((item) => item.id === row.id ? { ...item, key: event.target.value } : item))} placeholder="dedicated" /></Field>
                <Field label="值" htmlFor={`toleration-value-${row.id}`} error={errors[`tolerations.${index}.value`]}><Input id={`toleration-value-${row.id}`} value={row.value} onChange={(event) => update("tolerations", form.tolerations.map((item) => item.id === row.id ? { ...item, value: event.target.value } : item))} placeholder="gpu" /></Field>
                <Field label="操作符" htmlFor={`toleration-operator-${row.id}`}><Select value={row.operator} onValueChange={(value) => update("tolerations", form.tolerations.map((item) => item.id === row.id ? { ...item, operator: value as TolerationForm["operator"] } : item))}><SelectTrigger id={`toleration-operator-${row.id}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="Equal">Equal</SelectItem><SelectItem value="Exists">Exists</SelectItem></SelectContent></Select></Field>
                <Field label="效果" htmlFor={`toleration-effect-${row.id}`}><Select value={row.effect || "all"} onValueChange={(value) => update("tolerations", form.tolerations.map((item) => item.id === row.id ? { ...item, effect: (value === "all" ? "" : value) as TolerationEffect } : item))}><SelectTrigger id={`toleration-effect-${row.id}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">全部效果</SelectItem><SelectItem value="NoSchedule">NoSchedule</SelectItem><SelectItem value="PreferNoSchedule">PreferNoSchedule</SelectItem><SelectItem value="NoExecute">NoExecute</SelectItem></SelectContent></Select></Field>
              </div>
            </div>
          )) : <p className="text-xs text-muted-foreground">未配置容忍。</p>}
        </section>

        <section className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">节点亲和性（required）</h3><Button type="button" variant="outline" size="sm" onClick={() => update("nodeAffinity", [...form.nodeAffinity, newAffinity()])} disabled={form.nodeAffinity.length >= 32}><Plus className="h-3.5 w-3.5" />添加规则</Button></div>
          {form.nodeAffinity.length ? form.nodeAffinity.map((row, index) => (
            <div key={row.id} className="grid min-w-0 gap-3 rounded-md border bg-card p-3 sm:grid-cols-[minmax(0,1fr)_120px_minmax(0,1fr)_auto] sm:items-end">
              <Field label="标签键" htmlFor={`affinity-key-${row.id}`} error={errors[`nodeAffinity.${index}.key`]}><Input id={`affinity-key-${row.id}`} value={row.key} onChange={(event) => update("nodeAffinity", form.nodeAffinity.map((item) => item.id === row.id ? { ...item, key: event.target.value } : item))} placeholder="topology.kubernetes.io/zone" /></Field>
              <Field label="操作符" htmlFor={`affinity-operator-${row.id}`}><Select value={row.operator} onValueChange={(value) => update("nodeAffinity", form.nodeAffinity.map((item) => item.id === row.id ? { ...item, operator: value as NodeAffinityForm["operator"] } : item))}><SelectTrigger id={`affinity-operator-${row.id}`}><SelectValue /></SelectTrigger><SelectContent>{(["In", "NotIn", "Exists", "DoesNotExist"] as const).map((operator) => <SelectItem key={operator} value={operator}>{operator}</SelectItem>)}</SelectContent></Select></Field>
              <Field label="值" htmlFor={`affinity-values-${row.id}`} error={errors[`nodeAffinity.${index}.values`]} hint="逗号分隔。"><Input id={`affinity-values-${row.id}`} value={row.values} onChange={(event) => update("nodeAffinity", form.nodeAffinity.map((item) => item.id === row.id ? { ...item, values: event.target.value } : item))} placeholder="az-a,az-b" /></Field>
              <Button type="button" variant="ghost" size="sm" className="h-9 px-2 text-destructive" aria-label={`删除节点亲和性 ${index + 1}`} onClick={() => update("nodeAffinity", form.nodeAffinity.filter((item) => item.id !== row.id))}><Trash2 className="h-4 w-4" /></Button>
            </div>
          )) : <p className="text-xs text-muted-foreground">未配置节点亲和性。</p>}
        </section>

        <section className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">拓扑分布</h3><Button type="button" variant="outline" size="sm" onClick={() => update("topologySpreadConstraints", [...form.topologySpreadConstraints, newTopology()])} disabled={form.topologySpreadConstraints.length >= 8}><Plus className="h-3.5 w-3.5" />添加约束</Button></div>
          {form.topologySpreadConstraints.length ? form.topologySpreadConstraints.map((row, index) => (
            <div key={row.id} className="grid min-w-0 gap-3 rounded-md border bg-card p-3 sm:grid-cols-[90px_minmax(0,1fr)_160px_auto] sm:items-end">
              <Field label="maxSkew" htmlFor={`topology-skew-${row.id}`} error={errors[`topology.${index}.maxSkew`]}><Input id={`topology-skew-${row.id}`} type="number" min={1} max={100} value={row.maxSkew} onChange={(event) => update("topologySpreadConstraints", form.topologySpreadConstraints.map((item) => item.id === row.id ? { ...item, maxSkew: event.target.value } : item))} /></Field>
              <Field label="拓扑键" htmlFor={`topology-key-${row.id}`} error={errors[`topology.${index}.topologyKey`]}><Input id={`topology-key-${row.id}`} value={row.topologyKey} onChange={(event) => update("topologySpreadConstraints", form.topologySpreadConstraints.map((item) => item.id === row.id ? { ...item, topologyKey: event.target.value } : item))} className="font-mono text-xs" /></Field>
              <Field label="无法满足时" htmlFor={`topology-policy-${row.id}`}><Select value={row.whenUnsatisfiable} onValueChange={(value) => update("topologySpreadConstraints", form.topologySpreadConstraints.map((item) => item.id === row.id ? { ...item, whenUnsatisfiable: value as TopologyForm["whenUnsatisfiable"] } : item))}><SelectTrigger id={`topology-policy-${row.id}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="DoNotSchedule">DoNotSchedule</SelectItem><SelectItem value="ScheduleAnyway">ScheduleAnyway</SelectItem></SelectContent></Select></Field>
              <Button type="button" variant="ghost" size="sm" className="h-9 px-2 text-destructive" aria-label={`删除拓扑分布 ${index + 1}`} onClick={() => update("topologySpreadConstraints", form.topologySpreadConstraints.filter((item) => item.id !== row.id))}><Trash2 className="h-4 w-4" /></Button>
            </div>
          )) : <p className="text-xs text-muted-foreground">未配置拓扑分布约束。</p>}
        </section>

        <section className="min-w-0 space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Pod SecurityContext</h3>
          <div className="grid min-w-0 gap-3 sm:grid-cols-2">
            <Field label="runAsUser" htmlFor="workload-pod-run-as-user"><Input id="workload-pod-run-as-user" type="number" min={0} value={form.podRunAsUser} onChange={(event) => update("podRunAsUser", event.target.value)} /></Field>
            <Field label="runAsGroup" htmlFor="workload-pod-run-as-group"><Input id="workload-pod-run-as-group" type="number" min={0} value={form.podRunAsGroup} onChange={(event) => update("podRunAsGroup", event.target.value)} /></Field>
            <Field label="fsGroup" htmlFor="workload-pod-fs-group"><Input id="workload-pod-fs-group" type="number" min={0} value={form.podFsGroup} onChange={(event) => update("podFsGroup", event.target.value)} /></Field>
            <Field label="runAsNonRoot" htmlFor="workload-pod-run-as-non-root"><Select value={form.podRunAsNonRoot || "unset"} onValueChange={(value) => update("podRunAsNonRoot", (value === "unset" ? "" : value) as TriState)}><SelectTrigger id="workload-pod-run-as-non-root"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="unset">未设置</SelectItem><SelectItem value="true">是</SelectItem><SelectItem value="false">否</SelectItem></SelectContent></Select></Field>
            <Field label="seccompProfile" htmlFor="workload-pod-seccomp" error={errors.podSecurity} className="sm:col-span-2"><Select value={form.podSeccompProfile || "unset"} onValueChange={(value) => update("podSeccompProfile", (value === "unset" ? "" : value) as FormState["podSeccompProfile"])}><SelectTrigger id="workload-pod-seccomp"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="unset">未设置</SelectItem><SelectItem value="RuntimeDefault">RuntimeDefault</SelectItem><SelectItem value="Localhost">Localhost</SelectItem><SelectItem value="Unconfined">Unconfined</SelectItem></SelectContent></Select></Field>
          </div>
        </section>
      </div>
    </details>
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
    <div className={cn("min-w-0 space-y-1.5", className)}>
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {error ? <p role="alert" className="break-words text-xs text-destructive-strong">{error}</p> : hint ? <p className="break-words text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function Summary({ input, form }: { input: WorkloadCreateInput | null; form: FormState }) {
  if (!input) return null;
  const containers = input.containers ?? [];
  const initContainers = input.initContainers ?? [];
  return (
    <dl className="grid min-w-0 gap-3 rounded-md border bg-muted/20 p-4 text-sm sm:grid-cols-2">
      <SummaryItem label="类型" value={kindLabels[input.kind]} />
      <SummaryItem label="命名空间" value={input.namespace} />
      <SummaryItem label="名称" value={input.name} />
      <SummaryItem label="应用容器 / Init" value={`${containers.length} / ${initContainers.length}`} />
      <SummaryItem label="应用镜像" value={containers.map((container) => container.image).join("\n")} mono />
      {initContainers.length ? <SummaryItem label="Init 镜像" value={initContainers.map((container) => container.image).join("\n")} mono /> : null}
      {input.kind === "deployment" || input.kind === "statefulset" ? <SummaryItem label="副本" value={String(input.replicas ?? 1)} /> : null}
      {input.kind === "statefulset" ? <SummaryItem label="Headless Service" value={input.serviceName || input.name} /> : null}
      {input.kind === "cronjob" ? <><SummaryItem label="调度" value={input.schedule ?? ""} mono /><SummaryItem label="并发策略 / 初始暂停" value={`${input.concurrencyPolicy ?? "Allow"} / ${input.suspend ? "是" : "否"}`} /></> : null}
      <SummaryItem label="标签 / 节点选择器" value={`${Object.keys(input.labels ?? {}).length} / ${Object.keys(input.nodeSelector ?? {}).length}`} />
      <SummaryItem label="容忍 / 亲和性 / 拓扑" value={`${input.tolerations?.length ?? 0} / ${input.nodeAffinity?.length ?? 0} / ${input.topologySpreadConstraints?.length ?? 0}`} />
      <SummaryItem label="PriorityClass / ServiceAccount" value={`${input.priorityClassName || "—"} / ${input.serviceAccountName || "—"}`} mono />
      {form.kind === "job" || form.kind === "cronjob" ? <SummaryItem label="Job 参数" value={`completions=${input.jobCompletions ?? "默认"} · parallelism=${input.jobParallelism ?? "默认"} · backoff=${input.jobBackoffLimit ?? "默认"} · ttl=${input.jobTtlSecondsAfterFinished ?? "默认"}`} /> : null}
    </dl>
  );
}

function SummaryItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return <div className="min-w-0"><dt className="text-xs text-muted-foreground">{label}</dt><dd className={cn("mt-0.5 whitespace-pre-wrap break-all", mono && "font-mono text-xs")}>{value || "—"}</dd></div>;
}

function buildInput(form: FormState): WorkloadCreateInput | null {
  const labels = parseKeyValues(form.labels, true);
  const nodeSelector = parseKeyValues(form.nodeSelector);
  if (labels === null || nodeSelector === null) return null;
  const containers = form.containers.map((container) => buildContainer(container));
  const initContainers = form.initContainers.map((container) => buildContainer(container));
  if (containers.some((container) => container === null) || initContainers.some((container) => container === null)) return null;
  const replicas = Number(form.replicas);
  if ((form.kind === "deployment" || form.kind === "statefulset") && !Number.isInteger(replicas)) return null;
  const podSecurityContext = buildPodSecurityContext(form);
  if (podSecurityContext === null) return null;
  const jobCompletions = parseOptionalInteger(form.jobCompletions, 1, 10000);
  const jobParallelism = parseOptionalInteger(form.jobParallelism, 0, 10000);
  const jobBackoffLimit = parseOptionalInteger(form.jobBackoffLimit, 0, 10000);
  const jobActiveDeadlineSeconds = parseOptionalInteger(form.jobActiveDeadlineSeconds, 1, 604800);
  const jobTtlSecondsAfterFinished = parseOptionalInteger(form.jobTtlSecondsAfterFinished, 0, 2592000);
  if ([jobCompletions, jobParallelism, jobBackoffLimit, jobActiveDeadlineSeconds, jobTtlSecondsAfterFinished].some((value) => value === null)) return null;
  const primary = (containers[0] ?? null) as WorkloadContainerSpec | null;
  return {
    kind: form.kind,
    namespace: form.namespace,
    name: form.name,
    replicas: form.kind === "deployment" || form.kind === "statefulset" ? replicas : undefined,
    serviceName: form.kind === "statefulset" ? form.serviceName || form.name : undefined,
    schedule: form.kind === "cronjob" ? form.schedule.trim() : undefined,
    concurrencyPolicy: form.kind === "cronjob" ? form.concurrencyPolicy : undefined,
    suspend: form.kind === "cronjob" ? form.suspend : undefined,
    labels,
    containerName: primary?.name,
    image: primary?.image,
    registryId: primary?.registryId,
    containerPort: primary?.containerPort,
    cpuRequestMilli: primary?.cpuRequestMilli,
    cpuLimitMilli: primary?.cpuLimitMilli,
    memoryRequestBytes: primary?.memoryRequestBytes,
    memoryLimitBytes: primary?.memoryLimitBytes,
    env: primary?.env,
    command: primary?.command,
    args: primary?.args,
    containers: containers as WorkloadContainerSpec[],
    initContainers: initContainers as WorkloadContainerSpec[],
    nodeSelector,
    tolerations: form.tolerations.map((row): WorkloadToleration => ({
      key: row.key.trim(), operator: row.operator, value: row.operator === "Exists" ? "" : row.value.trim(), effect: row.effect,
      tolerationSeconds: parseOptionalNonNegative(row.tolerationSeconds) ?? undefined,
    })),
    nodeAffinity: form.nodeAffinity.map((row): WorkloadNodeSelectorRequirement => ({
      key: row.key.trim(), operator: row.operator,
      values: row.operator === "Exists" || row.operator === "DoesNotExist" ? undefined : commaValues(row.values),
    })),
    topologySpreadConstraints: form.topologySpreadConstraints.map((row): WorkloadTopologySpreadConstraint => ({
      maxSkew: Number(row.maxSkew), topologyKey: row.topologyKey.trim(), whenUnsatisfiable: row.whenUnsatisfiable,
    })),
    priorityClassName: form.priorityClassName.trim() || undefined,
    serviceAccountName: form.serviceAccountName.trim() || undefined,
    podSecurityContext: podSecurityContext ?? undefined,
    jobCompletions: jobCompletions ?? undefined,
    jobParallelism: jobParallelism ?? undefined,
    jobBackoffLimit: jobBackoffLimit ?? undefined,
    jobActiveDeadlineSeconds: jobActiveDeadlineSeconds ?? undefined,
    jobTtlSecondsAfterFinished: jobTtlSecondsAfterFinished ?? undefined,
  };
}

function buildContainer(container: ContainerForm): WorkloadContainerSpec | null {
  const env = parseEnv(container.env);
  const command = parseLines(container.command);
  const args = parseLines(container.args);
  const port = parsePort(container.containerPort);
  const cpuRequest = parseCPU(container.cpuRequest);
  const cpuLimit = parseCPU(container.cpuLimit);
  const memoryRequest = parseMiB(container.memoryRequestMi);
  const memoryLimit = parseMiB(container.memoryLimitMi);
  const securityContext = buildContainerSecurityContext(container);
  if (env === null || command === null || args === null || port === null || cpuRequest === null || cpuLimit === null || memoryRequest === null || memoryLimit === null || securityContext === null) return null;
  return {
    name: container.name.trim() || undefined,
    image: container.image.trim(),
    registryId: container.registryId || undefined,
    containerPort: port || undefined,
    cpuRequestMilli: cpuRequest,
    cpuLimitMilli: cpuLimit,
    memoryRequestBytes: memoryRequest,
    memoryLimitBytes: memoryLimit,
    env,
    command,
    args,
    securityContext: securityContext ?? undefined,
  };
}

function buildContainerSecurityContext(container: ContainerForm): WorkloadContainerSecurityContext | undefined | null {
  const runAsUser = parseOptionalNonNegative(container.runAsUser);
  const runAsGroup = parseOptionalNonNegative(container.runAsGroup);
  const runAsNonRoot = parseTriState(container.runAsNonRoot);
  const allowPrivilegeEscalation = parseTriState(container.allowPrivilegeEscalation);
  const capabilitiesAdd = parseCapabilities(container.capabilitiesAdd);
  const capabilitiesDrop = parseCapabilities(container.capabilitiesDrop);
  if (runAsUser === null || runAsGroup === null || runAsNonRoot === null || allowPrivilegeEscalation === null || capabilitiesAdd === null || capabilitiesDrop === null) return null;
  if (runAsUser === undefined && runAsGroup === undefined && runAsNonRoot === undefined && allowPrivilegeEscalation === undefined && !container.readOnlyRootFilesystem && !capabilitiesAdd.length && !capabilitiesDrop.length) return undefined;
  return {
    runAsUser,
    runAsGroup,
    runAsNonRoot,
    readOnlyRootFilesystem: container.readOnlyRootFilesystem || undefined,
    allowPrivilegeEscalation,
    capabilitiesAdd: capabilitiesAdd.length ? capabilitiesAdd : undefined,
    capabilitiesDrop: capabilitiesDrop.length ? capabilitiesDrop : undefined,
  };
}

function buildPodSecurityContext(form: FormState): WorkloadPodSecurityContext | undefined | null {
  const runAsUser = parseOptionalNonNegative(form.podRunAsUser);
  const runAsGroup = parseOptionalNonNegative(form.podRunAsGroup);
  const fsGroup = parseOptionalNonNegative(form.podFsGroup);
  const runAsNonRoot = parseTriState(form.podRunAsNonRoot);
  if (runAsUser === null || runAsGroup === null || fsGroup === null || runAsNonRoot === null) return null;
  const seccompProfile = form.podSeccompProfile || undefined;
  if (runAsUser === undefined && runAsGroup === undefined && fsGroup === undefined && runAsNonRoot === undefined && !seccompProfile) return undefined;
  return { runAsUser, runAsGroup, fsGroup, runAsNonRoot, seccompProfile };
}

function parseTriState(value: TriState): boolean | undefined | null {
  if (value === "") return undefined;
  if (value === "true") return true;
  if (value === "false") return false;
  return null;
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

function parsePort(value: string): number | null {
  if (value.trim() === "") return 0;
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null;
  return port;
}

function parseOptionalNonNegative(value: string): number | undefined | null {
  if (value.trim() === "") return undefined;
  const number = Number(value);
  if (!Number.isInteger(number) || number < 0) return null;
  return number;
}

function parseOptionalInteger(value: string, min: number, max: number): number | undefined | null {
  if (value.trim() === "") return undefined;
  const number = Number(value);
  if (!Number.isInteger(number) || number < min || number > max) return null;
  return number;
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
    if (!ENV_NAME.test(name) || seen.has(name)) return null;
    seen.add(name);
    out.push({ name, value: trimmed.slice(separator + 1) });
  }
  return out;
}

function parseLines(value: string): string[] | null {
  const out = value.split("\n").map((line) => line.trim()).filter(Boolean);
  return out.some((line) => line.length > 4096) ? null : out;
}

function parseCapabilities(value: string): string[] | null {
  const out = commaValues(value).map((capability) => capability.toUpperCase());
  return out.some((capability) => !CAPABILITY.test(capability)) ? null : out;
}

function commaValues(value: string): string[] {
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

function parseKeyValues(value: string, reserved = false): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const line of value.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    if (separator < 1) return null;
    const key = trimmed.slice(0, separator).trim();
    const itemValue = trimmed.slice(separator + 1).trim();
    if (!key || key.length > 253 || itemValue.length > 63 || (reserved && RESERVED_LABELS.has(key)) || key in out) return null;
    out[key] = itemValue;
  }
  return out;
}
