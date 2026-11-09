import type {
  AuditRecord,
  AuthConfig,
  Capabilities,
  ClusterConfiguration,
  ConfigMapDetail,
  ConfigMapCreateInput,
  ConfigMapSummary,
  ConfigMapUpdateInput,
  CronJobSuspendInput,
  DeploymentCreateInput,
  DeploymentPreview,
  EventItem,
  IngressDetail,
  IngressCreateInput,
  IngressClassSummary,
  IngressSummary,
  IngressUpdateInput,
  ListParams,
  ListResponse,
  NamespaceItem,
  NamespaceCreateInput,
  NamespaceDetail,
  NodeDetail,
  NodeLabelUpdate,
  NodeSummary,
  NodeTaintUpdate,
  Overview,
  PersistentVolume,
  PersistentVolumeClaim,
  PersistentVolumeClaimCreateInput,
  PersistentVolumeClaimDetail,
  PersistentVolumeClaimUpdateInput,
  PersistentVolumeDetail,
  PodDetail,
  PodSummary,
  Problem,
  SecretReveal,
  SecretSummary,
  SecretWriteInput,
  ServiceCreateInput,
  ServiceDetail,
  ServiceSummary,
  ServiceUpdateInput,
  SessionResponse,
  ServerCertificate,
  StorageClass,
  StorageClassDetail,
  WorkloadDetail,
  WorkloadContainerConfigUpdateInput,
  WorkloadImageUpdateInput,
  WorkloadProbe,
  WorkloadSummary,
  YAMLResource,
} from "./types";

export class ApiError extends Error {
  status: number;
  code: string;
  requestId?: string;
  fieldErrors?: Record<string, string>;

  constructor(status: number, problem: Partial<Problem>) {
    super(localizedProblemMessage(problem, status));
    this.name = "ApiError";
    this.status = status;
    this.code = problem.code || "request_failed";
    this.requestId = problem.requestId;
    this.fieldErrors = problem.fieldErrors;
  }
}

const problemMessages: Record<string, string> = {
  unauthorized: "认证失败，请检查凭据或重新登录",
  forbidden: "没有执行该操作的权限",
  not_found: "资源不存在或已被删除",
  conflict: "资源已发生变化，请刷新后重试",
  already_exists: "资源已存在，请更换名称",
  dependency_unavailable: "集群服务暂不可用，请稍后重试",
  not_supported: "当前资源或操作不受支持",
  rate_limited: "操作过于频繁，请稍后重试",
  stale_cursor: "列表数据已更新，请刷新后重试",
  internal_error: "服务端处理失败，请稍后重试",
  request_failed: "请求失败，请稍后重试",
};

function localizedProblemMessage(problem: Partial<Problem>, status: number): string {
  if (problem.code && problemMessages[problem.code]) return problemMessages[problem.code];
  return problem.message || `请求失败（HTTP ${status}）`;
}

let csrfToken = "";
const unauthorizedHandlers = new Set<() => void>();

export function setCSRFToken(token: string) {
  csrfToken = token;
}

export function onUnauthorized(handler: () => void): () => void {
  unauthorizedHandlers.add(handler);
  return () => {
    unauthorizedHandlers.delete(handler);
  };
}

function notifyUnauthorized() {
  setCSRFToken("");
  unauthorizedHandlers.forEach((handler) => handler());
}

function query(params?: ListParams): string {
  if (!params) return "";
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== "") search.set(key, String(value));
  });
  const value = search.toString();
  return value ? `?${value}` : "";
}

function list<T>(value: T[] | null | undefined): T[] {
  return value ?? [];
}

function record<T>(value: Record<string, T> | null | undefined): Record<string, T> {
  return value ?? {};
}

function normalizeList<T>(value: ListResponse<T>): ListResponse<T> {
  const items = list(value.items);
  const pageSize = value.meta?.pageSize ?? Math.max(items.length, 1);
  return {
    ...value,
    items,
    meta: value.meta ?? {
      total: items.length,
      totalKnown: true,
      page: 1,
      pageSize,
      totalPages: items.length ? 1 : 0,
      hasMore: false,
      warnings: [],
    },
  };
}

function normalizeOverview(value: Overview): Overview {
  return { ...value, recentEvents: list(value.recentEvents), warnings: list(value.warnings) };
}

function normalizeWorkloadDetail(value: WorkloadDetail): WorkloadDetail {
  return {
    ...value,
    images: list(value.images),
    conditions: list(value.conditions),
    selector: record(value.selector),
    pods: list(value.pods),
    volumes: list(value.volumes).map((volume) => ({ ...volume, readOnly: Boolean(volume.readOnly) })),
    containers: list(value.containers).map((container) => ({
      ...container,
      ports: list(container.ports),
      resources: container.resources ?? {
        cpuRequestMilli: 0, cpuLimitMilli: 0, memoryRequestBytes: 0, memoryLimitBytes: 0,
      },
      env: list(container.env).map((variable) => ({ ...variable, value: variable.value ?? "" })),
      volumeMounts: list(container.volumeMounts).map((mount) => ({ ...mount, readOnly: Boolean(mount.readOnly) })),
      livenessProbe: normalizeWorkloadProbe(container.livenessProbe),
      readinessProbe: normalizeWorkloadProbe(container.readinessProbe),
      startupProbe: normalizeWorkloadProbe(container.startupProbe),
    })),
  };
}

function normalizeWorkloadProbe(probe: WorkloadProbe | undefined): WorkloadProbe | undefined {
  return probe ? { ...probe, command: list(probe.command) } : undefined;
}

function normalizePodDetail(value: PodDetail): PodDetail {
  return { ...value, containers: list(value.containers), conditions: list(value.conditions) };
}

function normalizeNodeDetail(value: NodeDetail): NodeDetail {
  return {
    ...value,
    roles: list(value.roles),
    conditions: list(value.conditions),
    taints: list(value.taints),
    labels: record(value.labels),
    annotations: record(value.annotations),
    pods: list(value.pods),
  };
}

function normalizeNamespaceDetail(value: NamespaceDetail): NamespaceDetail {
  return {
    ...value,
    resourceQuotas: list(value.resourceQuotas).map((quota) => ({
      ...quota,
      hard: record(quota.hard),
      used: record(quota.used),
    })),
    limitRanges: list(value.limitRanges).map((limitRange) => ({
      ...limitRange,
      limits: list(limitRange.limits).map((limit) => ({
        ...limit,
        min: record(limit.min),
        max: record(limit.max),
        default: record(limit.default),
        defaultRequest: record(limit.defaultRequest),
      })),
    })),
  };
}

function normalizeServiceDetail(value: ServiceDetail): ServiceDetail {
  return {
    ...value,
    ports: list(value.ports),
    ipFamilies: list(value.ipFamilies),
    endpoints: list(value.endpoints).map((endpoint) => ({ ...endpoint, addresses: list(endpoint.addresses) })),
  };
}

function normalizeIngressDetail(value: IngressDetail): IngressDetail {
  return {
    ...value,
    paths: list(value.paths),
    tlsHosts: list(value.tlsHosts),
    tls: list(value.tls).map((tls) => ({ ...tls, hosts: list(tls.hosts) })),
  };
}

function normalizeConfigMapDetail(value: ConfigMapDetail): ConfigMapDetail {
  return { ...value, keys: list(value.keys), data: record(value.data) };
}

function normalizeSecretReveal(value: SecretReveal): SecretReveal {
  return { ...value, keys: list(value.keys), data: record(value.data) };
}

function normalizeStorageClassDetail(value: StorageClassDetail): StorageClassDetail {
  return { ...value, mountOptions: list(value.mountOptions), parameters: record(value.parameters) };
}

function normalizePersistentVolumeDetail(value: PersistentVolumeDetail): PersistentVolumeDetail {
  return { ...value, accessModes: list(value.accessModes), mountOptions: list(value.mountOptions) };
}

function normalizePersistentVolumeClaimDetail(value: PersistentVolumeClaimDetail): PersistentVolumeClaimDetail {
  return { ...value, accessModes: list(value.accessModes), conditions: list(value.conditions) };
}

function normalizeServerCertificate(value: ServerCertificate): ServerCertificate {
  return { ...value, dnsNames: list(value.dnsNames), ipAddresses: list(value.ipAddresses) };
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method || "GET").toUpperCase();
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  if (method !== "GET" && method !== "HEAD" && csrfToken) headers.set("X-CSRF-Token", csrfToken);
  headers.set("Accept", "application/json");
  const controller = new AbortController();
  const abort = () => controller.abort();
  const timeout = window.setTimeout(abort, 30_000);
  if (init.signal?.aborted) controller.abort();
  init.signal?.addEventListener("abort", abort, { once: true });
  try {
    const response = await fetch(path, { ...init, headers, credentials: "same-origin", signal: controller.signal });
    if (!response.ok) {
      let problem: Partial<Problem> = {};
      try {
        problem = (await response.json()) as Problem;
      } catch {
        problem = { code: "request_failed", message: response.statusText };
      }
      if (response.status === 401) notifyUnauthorized();
      throw new ApiError(response.status, problem);
    }
    if (response.status === 204) return undefined as T;
    try {
      return (await response.json()) as T;
    } catch {
      throw new ApiError(response.status, { code: "invalid_response", message: "服务端响应格式无效" });
    }
  } catch (error) {
    if (error instanceof ApiError) throw error;
    if (init.signal?.aborted) throw error;
    if (controller.signal.aborted) {
      throw new ApiError(0, { code: "request_timeout", message: "请求超时，请稍后重试" });
    }
    throw new ApiError(0, { code: "network_error", message: "网络连接失败，请检查网络后重试" });
  } finally {
    window.clearTimeout(timeout);
    init.signal?.removeEventListener("abort", abort);
  }
}

async function listRequest<T>(path: string): Promise<ListResponse<T>> {
  return normalizeList(await request<ListResponse<T>>(path));
}

export const api = {
  authConfig: () => request<AuthConfig>("/api/v1/auth/config"),
  async login(username: string, password: string): Promise<SessionResponse> {
    const session = await request<SessionResponse>("/api/v1/session", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    setCSRFToken(session.csrfToken);
    return session;
  },
  async loginWithToken(token: string): Promise<SessionResponse> {
    const session = await request<SessionResponse>("/api/v1/session", {
      method: "POST",
      body: JSON.stringify({ token }),
    });
    setCSRFToken(session.csrfToken);
    return session;
  },
  async session(): Promise<SessionResponse> {
    const session = await request<SessionResponse>("/api/v1/session");
    setCSRFToken(session.csrfToken);
    return session;
  },
  async logout(): Promise<void> {
    await request<void>("/api/v1/session", { method: "DELETE" });
    setCSRFToken("");
  },
  capabilities: (namespace?: string) => request<Capabilities>(`/api/v1/capabilities${query({ namespace })}`),
  overview: async () => normalizeOverview(await request<Overview>("/api/v1/overview")),
  clusterConfiguration: async () => {
    const configuration = await request<ClusterConfiguration>("/api/v1/cluster/config");
    return {
      ...configuration,
      nodeCIDRs: list(configuration.nodeCIDRs).map((node) => ({ ...node, podCIDRs: list(node.podCIDRs) })),
      warnings: list(configuration.warnings),
    };
  },
  auditEvents: (params?: ListParams) => listRequest<AuditRecord>(`/api/v1/audit-events${query(params)}`),
  nodes: (params?: ListParams) => listRequest<NodeSummary>(`/api/v1/nodes${query(params)}`),
  node: async (name: string) => normalizeNodeDetail(await request<NodeDetail>(`/api/v1/nodes/${encodeURIComponent(name)}`)),
  cordonNode: (name: string) => request<NodeSummary>(`/api/v1/nodes/${encodeURIComponent(name)}/cordon`, { method: "POST" }),
  uncordonNode: (name: string) => request<NodeSummary>(`/api/v1/nodes/${encodeURIComponent(name)}/uncordon`, { method: "POST" }),
  setNodeLabels: async (name: string, input: NodeLabelUpdate) =>
    normalizeNodeDetail(await request<NodeDetail>(`/api/v1/nodes/${encodeURIComponent(name)}/labels`, { method: "PUT", body: JSON.stringify(input) })),
  setNodeTaints: async (name: string, input: NodeTaintUpdate) =>
    normalizeNodeDetail(await request<NodeDetail>(`/api/v1/nodes/${encodeURIComponent(name)}/taints`, { method: "PUT", body: JSON.stringify(input) })),
  namespaces: (params?: ListParams) => listRequest<NamespaceItem>(`/api/v1/namespaces${query(params)}`),
  createNamespace: (input: NamespaceCreateInput) =>
    request<NamespaceItem>("/api/v1/namespaces", { method: "POST", body: JSON.stringify(input) }),
  namespace: async (name: string) => normalizeNamespaceDetail(await request<NamespaceDetail>(`/api/v1/namespaces/${encodeURIComponent(name)}`)),
  deleteNamespace: (name: string, resourceVersion?: string) =>
    request<{ status: string }>(`/api/v1/namespaces/${encodeURIComponent(name)}${resourceVersion ? `?resourceVersion=${encodeURIComponent(resourceVersion)}` : ""}`, {
      method: "DELETE",
    }),
  workloads: (params?: ListParams) => listRequest<WorkloadSummary>(`/api/v1/workloads${query(params)}`),
  workload: async (kind: string, namespace: string, name: string) =>
    normalizeWorkloadDetail(await request<WorkloadDetail>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  createDeployment: (input: DeploymentCreateInput) =>
    request<WorkloadSummary>("/api/v1/workloads/deployments", { method: "POST", body: JSON.stringify(input) }),
  previewDeployment: (input: DeploymentCreateInput) =>
    request<DeploymentPreview>("/api/v1/workloads/deployments/preview", { method: "POST", body: JSON.stringify(input) }),
  scaleWorkload: (kind: string, namespace: string, name: string, replicas: number) =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scale`, {
      method: "POST",
      body: JSON.stringify({ replicas }),
    }),
  restartWorkload: (kind: string, namespace: string, name: string) =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restart`, { method: "POST" }),
  updateWorkloadImage: (kind: string, namespace: string, name: string, input: WorkloadImageUpdateInput) =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/image`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateWorkloadContainerConfig: (kind: string, namespace: string, name: string, container: string, input: WorkloadContainerConfigUpdateInput) =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/containers/${encodeURIComponent(container)}/config`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  setCronJobSuspend: (kind: string, namespace: string, name: string, input: CronJobSuspendInput) =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/suspend`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteWorkload: (kind: string, namespace: string, name: string, propagation = "Background") =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}?propagation=${encodeURIComponent(propagation)}`, {
      method: "DELETE",
    }),
  pods: (params?: ListParams) => listRequest<PodSummary>(`/api/v1/pods${query(params)}`),
  pod: async (namespace: string, name: string) => normalizePodDetail(await request<PodDetail>(`/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  deletePod: (namespace: string, name: string) =>
    request<{ status: string }>(`/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, { method: "DELETE" }),
  events: (params?: ListParams) => listRequest<EventItem>(`/api/v1/events${query(params)}`),
  services: (params?: ListParams) => listRequest<ServiceSummary>(`/api/v1/services${query(params)}`),
  service: async (namespace: string, name: string) => normalizeServiceDetail(await request<ServiceDetail>(`/api/v1/services/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  createService: async (input: ServiceCreateInput) => normalizeServiceDetail(await request<ServiceDetail>("/api/v1/services", {
    method: "POST",
    body: JSON.stringify(input),
  })),
  updateService: async (namespace: string, name: string, input: ServiceUpdateInput) => normalizeServiceDetail(await request<ServiceDetail>(`/api/v1/services/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  })),
  deleteService: (namespace: string, name: string, resourceVersion?: string) =>
    request<{ status: string }>(`/api/v1/services/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}${resourceVersion ? `?resourceVersion=${encodeURIComponent(resourceVersion)}` : ""}`, {
      method: "DELETE",
    }),
  ingresses: (params?: ListParams) => listRequest<IngressSummary>(`/api/v1/ingresses${query(params)}`),
  ingress: async (namespace: string, name: string) => normalizeIngressDetail(await request<IngressDetail>(`/api/v1/ingresses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  createIngress: async (input: IngressCreateInput) => normalizeIngressDetail(await request<IngressDetail>("/api/v1/ingresses", {
    method: "POST",
    body: JSON.stringify(input),
  })),
  updateIngress: async (namespace: string, name: string, input: IngressUpdateInput) => normalizeIngressDetail(await request<IngressDetail>(`/api/v1/ingresses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  })),
  deleteIngress: (namespace: string, name: string, resourceVersion?: string) =>
    request<{ status: string }>(`/api/v1/ingresses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}${resourceVersion ? `?resourceVersion=${encodeURIComponent(resourceVersion)}` : ""}`, { method: "DELETE" }),
  ingressClasses: (params?: ListParams) => listRequest<IngressClassSummary>(`/api/v1/ingressclasses${query(params)}`),
  configmaps: (params?: ListParams) => listRequest<ConfigMapSummary>(`/api/v1/configmaps${query(params)}`),
  configmap: async (namespace: string, name: string) => normalizeConfigMapDetail(await request<ConfigMapDetail>(`/api/v1/configmaps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  createConfigMap: async (input: ConfigMapCreateInput) => normalizeConfigMapDetail(await request<ConfigMapDetail>("/api/v1/configmaps", {
    method: "POST",
    body: JSON.stringify(input),
  })),
  updateConfigMap: async (namespace: string, name: string, input: ConfigMapUpdateInput) => normalizeConfigMapDetail(await request<ConfigMapDetail>(`/api/v1/configmaps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  })),
  deleteConfigMap: (namespace: string, name: string, resourceVersion?: string) =>
    request<{ status: string }>(`/api/v1/configmaps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}${resourceVersion ? `?resourceVersion=${encodeURIComponent(resourceVersion)}` : ""}`, { method: "DELETE" }),
  serverCertificate: async () => normalizeServerCertificate(await request<ServerCertificate>("/api/v1/certificate")),
  reloadServerCertificate: async () => normalizeServerCertificate(await request<ServerCertificate>("/api/v1/certificate/reload", { method: "POST" })),
  secrets: (params?: ListParams) => listRequest<SecretSummary>(`/api/v1/secrets${query(params)}`),
  revealSecret: async (namespace: string, name: string) =>
    normalizeSecretReveal(await request<SecretReveal>(`/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/reveal`, { method: "POST" })),
  createSecret: (input: SecretWriteInput) => request<SecretSummary>("/api/v1/secrets", { method: "POST", body: JSON.stringify(input) }),
  updateSecret: (namespace: string, name: string, input: SecretWriteInput) =>
    request<SecretSummary>(`/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(input) }),
  deleteSecret: (namespace: string, name: string, resourceVersion?: string) =>
    request<{ status: string }>(`/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}${resourceVersion ? `?resourceVersion=${encodeURIComponent(resourceVersion)}` : ""}`, { method: "DELETE" }),
  storageclasses: (params?: ListParams) => listRequest<StorageClass>(`/api/v1/storageclasses${query(params)}`),
  storageclass: async (name: string) => normalizeStorageClassDetail(await request<StorageClassDetail>(`/api/v1/storageclasses/${encodeURIComponent(name)}`)),
  persistentvolumes: (params?: ListParams) => listRequest<PersistentVolume>(`/api/v1/persistentvolumes${query(params)}`),
  persistentvolume: async (name: string) => normalizePersistentVolumeDetail(await request<PersistentVolumeDetail>(`/api/v1/persistentvolumes/${encodeURIComponent(name)}`)),
  persistentvolumeclaims: (params?: ListParams) =>
    listRequest<PersistentVolumeClaim>(`/api/v1/persistentvolumeclaims${query(params)}`),
  persistentvolumeclaim: async (namespace: string, name: string) =>
    normalizePersistentVolumeClaimDetail(await request<PersistentVolumeClaimDetail>(`/api/v1/persistentvolumeclaims/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  createPersistentVolumeClaim: async (input: PersistentVolumeClaimCreateInput) =>
    normalizePersistentVolumeClaimDetail(await request<PersistentVolumeClaimDetail>("/api/v1/persistentvolumeclaims", { method: "POST", body: JSON.stringify(input) })),
  updatePersistentVolumeClaim: async (namespace: string, name: string, input: PersistentVolumeClaimUpdateInput) =>
    normalizePersistentVolumeClaimDetail(await request<PersistentVolumeClaimDetail>(`/api/v1/persistentvolumeclaims/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(input) })),
  deletePersistentVolumeClaim: (namespace: string, name: string, resourceVersion?: string) =>
    request<{ status: string }>(`/api/v1/persistentvolumeclaims/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}${resourceVersion ? `?resourceVersion=${encodeURIComponent(resourceVersion)}` : ""}`, { method: "DELETE" }),
  yaml: (type: string, name: string, namespace?: string) =>
    request<YAMLResource>(`/api/v1/yaml?${new URLSearchParams({ type, name, ...(namespace ? { namespace } : {}) }).toString()}`),
  applyYAML: (yaml: string, resourceVersion?: string, dryRun = false) =>
    request<YAMLResource>("/api/v1/yaml/apply", {
      method: "POST",
      body: JSON.stringify({ yaml, resourceVersion, dryRun }),
    }),
};

export async function streamLogs(
  namespace: string,
  name: string,
  options: { container?: string; tailLines?: number; follow?: boolean; timestamps?: boolean; signal?: AbortSignal } = {},
): Promise<ReadableStreamDefaultReader<Uint8Array>> {
  const params = new URLSearchParams();
  if (options.container) params.set("container", options.container);
  if (options.tailLines) params.set("tailLines", String(options.tailLines));
  if (options.follow) params.set("follow", "true");
  if (options.timestamps) params.set("timestamps", "true");
  const response = await fetch(`/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs?${params}`, {
    credentials: "same-origin",
    signal: options.signal,
  });
  if (!response.ok || !response.body) {
    let problem: Partial<Problem> = {};
    try { problem = (await response.json()) as Problem; } catch { /* noop */ }
    if (response.status === 401) notifyUnauthorized();
    throw new ApiError(response.status, problem);
  }
  return response.body.getReader();
}

export function eventSource(namespace?: string): EventSource {
  const suffix = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return new EventSource(`/api/v1/events/stream${suffix}`);
}

