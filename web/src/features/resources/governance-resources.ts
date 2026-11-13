import type { ResourceDescriptor } from "@/api/types";

export interface GovernanceResource {
  group: string;
  resource: string;
  kind: string;
  label: string;
  category: "RBAC" | "隔离与可用性" | "配额与默认值";
  description: string;
}

export const governanceResources: GovernanceResource[] = [
  { group: "", resource: "serviceaccounts", kind: "ServiceAccount", label: "服务账号", category: "RBAC", description: "工作负载使用的集群内身份，决定 Pod 调用 API 时采用的凭据。" },
  { group: "rbac.authorization.k8s.io", resource: "roles", kind: "Role", label: "Role", category: "RBAC", description: "命名空间范围内的权限规则集合。" },
  { group: "rbac.authorization.k8s.io", resource: "rolebindings", kind: "RoleBinding", label: "RoleBinding", category: "RBAC", description: "把 Role 或 ClusterRole 的权限授予用户、组或 ServiceAccount。" },
  { group: "rbac.authorization.k8s.io", resource: "clusterroles", kind: "ClusterRole", label: "ClusterRole", category: "RBAC", description: "可跨命名空间复用或用于集群级资源的权限规则集合。" },
  { group: "rbac.authorization.k8s.io", resource: "clusterrolebindings", kind: "ClusterRoleBinding", label: "ClusterRoleBinding", category: "RBAC", description: "在整个集群范围授予 ClusterRole 权限。" },
  { group: "networking.k8s.io", resource: "networkpolicies", kind: "NetworkPolicy", label: "NetworkPolicy", category: "隔离与可用性", description: "按 Pod 标签选择器约束入站和出站网络流量。" },
  { group: "policy", resource: "poddisruptionbudgets", kind: "PodDisruptionBudget", label: "PDB", category: "隔离与可用性", description: "限制自愿驱逐期间可同时中断的 Pod 数量。" },
  { group: "autoscaling", resource: "horizontalpodautoscalers", kind: "HorizontalPodAutoscaler", label: "HPA", category: "隔离与可用性", description: "根据指标自动调整工作负载副本数。" },
  { group: "", resource: "resourcequotas", kind: "ResourceQuota", label: "ResourceQuota", category: "配额与默认值", description: "限制命名空间内资源对象数量和计算资源总量。" },
  { group: "", resource: "limitranges", kind: "LimitRange", label: "LimitRange", category: "配额与默认值", description: "为命名空间内 Pod 和容器提供默认资源请求、限制及边界。" },
  { group: "scheduling.k8s.io", resource: "priorityclasses", kind: "PriorityClass", label: "PriorityClass", category: "配额与默认值", description: "定义 Pod 调度优先级和抢占行为。" },
];

export function findGovernanceDescriptor(descriptor: ResourceDescriptor): GovernanceResource | undefined {
  return governanceResources.find((item) => item.group === descriptor.group && item.resource === descriptor.resource);
}

export function governanceRelationships(resource: string | undefined, object: Record<string, unknown>): Array<{ label: string; value: string }> {
  if (!resource) return [];
  switch (resource) {
    case "rolebindings":
    case "clusterrolebindings":
      return bindingRelationships(object);
    case "networkpolicies":
      return networkPolicyRelationships(object);
    case "horizontalpodautoscalers":
      return hpaRelationships(object);
    case "poddisruptionbudgets":
      return pdbRelationships(object);
    case "resourcequotas":
      return mapRelationships("配额项", objectMap(object.hard));
    case "limitranges":
      return limitRangeRelationships(object);
    case "serviceaccounts":
      return serviceAccountRelationships(object);
    default:
      return [];
  }
}

function bindingRelationships(object: Record<string, unknown>) {
  const result: Array<{ label: string; value: string }> = [];
  const roleRef = objectMap(object.roleRef);
  if (roleRef.kind || roleRef.name) result.push({ label: "角色引用", value: `${text(roleRef.kind)}/${text(roleRef.name)}` });
  const subjects = Array.isArray(object.subjects) ? object.subjects : [];
  if (subjects.length) {
    result.push({ label: "授权主体", value: subjects.map((item) => {
      const subject = objectMap(item);
      const scope = subject.namespace ? `@${text(subject.namespace)}` : "";
      return `${text(subject.kind)}:${text(subject.name)}${scope}`;
    }).join("，") });
  }
  return result;
}

function networkPolicyRelationships(object: Record<string, unknown>) {
  const spec = objectMap(object.spec);
  const selector = objectMap(spec.podSelector);
  const labels = objectMap(selector.matchLabels);
  return [
    { label: "Pod 选择器", value: Object.entries(labels).map(([key, value]) => `${key}=${text(value)}`).join("，") || "全部 Pod" },
    { label: "策略类型", value: Array.isArray(spec.policyTypes) ? spec.policyTypes.map(text).join("，") : "未声明" },
  ];
}

function hpaRelationships(object: Record<string, unknown>) {
  const spec = objectMap(object.spec);
  const target = objectMap(spec.scaleTargetRef);
  return [
    { label: "扩缩目标", value: `${text(target.kind)}/${text(target.name)}` },
    { label: "副本范围", value: `${text(spec.minReplicas)} - ${text(spec.maxReplicas)}` },
  ];
}

function pdbRelationships(object: Record<string, unknown>) {
  const spec = objectMap(object.spec);
  const selector = objectMap(spec.selector);
  const labels = objectMap(selector.matchLabels);
  return [
    { label: "Pod 选择器", value: Object.entries(labels).map(([key, value]) => `${key}=${text(value)}`).join("，") || "全部 Pod" },
    { label: "可用性约束", value: spec.minAvailable !== undefined ? `minAvailable=${text(spec.minAvailable)}` : `maxUnavailable=${text(spec.maxUnavailable)}` },
  ];
}

function limitRangeRelationships(object: Record<string, unknown>) {
  const spec = objectMap(object.spec);
  const limits = Array.isArray(spec.limits) ? spec.limits : [];
  return limits.map((item, index) => {
    const limit = objectMap(item);
    return { label: `限制项 ${index + 1}`, value: `${text(limit.type)} · ${Object.keys(objectMap(limit.default)).join("，") || "无默认值"}` };
  });
}

function serviceAccountRelationships(object: Record<string, unknown>) {
  const secrets = Array.isArray(object.secrets) ? object.secrets : [];
  const imagePullSecrets = Array.isArray(object.imagePullSecrets) ? object.imagePullSecrets : [];
  return [
    { label: "令牌 Secret", value: secrets.map((item) => text(objectMap(item).name)).filter(Boolean).join("，") || "—" },
    { label: "镜像拉取 Secret", value: imagePullSecrets.map((item) => text(objectMap(item).name)).filter(Boolean).join("，") || "—" },
    { label: "自动挂载令牌", value: object.automountServiceAccountToken === false ? "关闭" : "开启" },
  ];
}

function mapRelationships(label: string, values: Record<string, unknown>) {
  const entries = Object.entries(values);
  return entries.length ? [{ label, value: entries.map(([key, value]) => `${key}=${text(value)}`).join("，") }] : [];
}

function objectMap(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function text(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  return String(value);
}
