import type {
  AuditRecord,
  AuthConfig,
  Capabilities,
  ConfigMapDetail,
  ConfigMapSummary,
  CronJobSuspendInput,
  DeploymentCreateInput,
  DeploymentPreview,
  EventItem,
  IngressDetail,
  IngressClassSummary,
  IngressSummary,
  ListParams,
  ListResponse,
  NamespaceItem,
  NamespaceDetail,
  NodeDetail,
  NodeSummary,
  Overview,
  PersistentVolume,
  PersistentVolumeClaim,
  PersistentVolumeClaimDetail,
  PersistentVolumeDetail,
  PodDetail,
  PodSummary,
  Problem,
  SecretReveal,
  SecretSummary,
  ServiceDetail,
  ServiceSummary,
  SessionResponse,
  ServerCertificate,
  StorageClass,
  StorageClassDetail,
  WorkloadDetail,
  WorkloadImageUpdateInput,
  WorkloadSummary,
  YAMLResource,
} from "./types";

export class ApiError extends Error {
  status: number;
  code: string;
  requestId?: string;
  fieldErrors?: Record<string, string>;

  constructor(status: number, problem: Partial<Problem>) {
    super(problem.message || `Request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.code = problem.code || "request_failed";
    this.requestId = problem.requestId;
    this.fieldErrors = problem.fieldErrors;
  }
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

function normalizeWorkloadDetail(value: WorkloadDetail): WorkloadDetail {
  return {
    ...value,
    images: list(value.images),
    conditions: list(value.conditions),
    selector: record(value.selector),
    pods: list(value.pods),
    containers: list(value.containers),
  };
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

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method || "GET").toUpperCase();
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  if (method !== "GET" && method !== "HEAD" && csrfToken) headers.set("X-CSRF-Token", csrfToken);
  headers.set("Accept", "application/json");
  const response = await fetch(path, { ...init, headers, credentials: "same-origin" });
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
  return (await response.json()) as T;
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
  overview: () => request<Overview>("/api/v1/overview"),
  auditEvents: (params?: ListParams) => request<ListResponse<AuditRecord>>(`/api/v1/audit-events${query(params)}`),
  nodes: (params?: ListParams) => request<ListResponse<NodeSummary>>(`/api/v1/nodes${query(params)}`),
  node: async (name: string) => normalizeNodeDetail(await request<NodeDetail>(`/api/v1/nodes/${encodeURIComponent(name)}`)),
  cordonNode: (name: string) => request<NodeSummary>(`/api/v1/nodes/${encodeURIComponent(name)}/cordon`, { method: "POST" }),
  uncordonNode: (name: string) => request<NodeSummary>(`/api/v1/nodes/${encodeURIComponent(name)}/uncordon`, { method: "POST" }),
  namespaces: (params?: ListParams) => request<ListResponse<NamespaceItem>>(`/api/v1/namespaces${query(params)}`),
  namespace: async (name: string) => normalizeNamespaceDetail(await request<NamespaceDetail>(`/api/v1/namespaces/${encodeURIComponent(name)}`)),
  workloads: (params?: ListParams) => request<ListResponse<WorkloadSummary>>(`/api/v1/workloads${query(params)}`),
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
  setCronJobSuspend: (kind: string, namespace: string, name: string, input: CronJobSuspendInput) =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/suspend`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteWorkload: (kind: string, namespace: string, name: string, propagation = "Background") =>
    request<{ status: string }>(`/api/v1/workloads/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}?propagation=${encodeURIComponent(propagation)}`, {
      method: "DELETE",
    }),
  pods: (params?: ListParams) => request<ListResponse<PodSummary>>(`/api/v1/pods${query(params)}`),
  pod: async (namespace: string, name: string) => normalizePodDetail(await request<PodDetail>(`/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  events: (params?: ListParams) => request<ListResponse<EventItem>>(`/api/v1/events${query(params)}`),
  services: (params?: ListParams) => request<ListResponse<ServiceSummary>>(`/api/v1/services${query(params)}`),
  service: async (namespace: string, name: string) => normalizeServiceDetail(await request<ServiceDetail>(`/api/v1/services/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  ingresses: (params?: ListParams) => request<ListResponse<IngressSummary>>(`/api/v1/ingresses${query(params)}`),
  ingress: async (namespace: string, name: string) => normalizeIngressDetail(await request<IngressDetail>(`/api/v1/ingresses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  ingressClasses: (params?: ListParams) => request<ListResponse<IngressClassSummary>>(`/api/v1/ingressclasses${query(params)}`),
  configmaps: (params?: ListParams) => request<ListResponse<ConfigMapSummary>>(`/api/v1/configmaps${query(params)}`),
  configmap: async (namespace: string, name: string) => normalizeConfigMapDetail(await request<ConfigMapDetail>(`/api/v1/configmaps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
  serverCertificate: () => request<ServerCertificate>("/api/v1/certificate"),
  reloadServerCertificate: () => request<ServerCertificate>("/api/v1/certificate/reload", { method: "POST" }),
  secrets: (params?: ListParams) => request<ListResponse<SecretSummary>>(`/api/v1/secrets${query(params)}`),
  revealSecret: async (namespace: string, name: string) =>
    normalizeSecretReveal(await request<SecretReveal>(`/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/reveal`, { method: "POST" })),
  storageclasses: (params?: ListParams) => request<ListResponse<StorageClass>>(`/api/v1/storageclasses${query(params)}`),
  storageclass: async (name: string) => normalizeStorageClassDetail(await request<StorageClassDetail>(`/api/v1/storageclasses/${encodeURIComponent(name)}`)),
  persistentvolumes: (params?: ListParams) => request<ListResponse<PersistentVolume>>(`/api/v1/persistentvolumes${query(params)}`),
  persistentvolume: async (name: string) => normalizePersistentVolumeDetail(await request<PersistentVolumeDetail>(`/api/v1/persistentvolumes/${encodeURIComponent(name)}`)),
  persistentvolumeclaims: (params?: ListParams) =>
    request<ListResponse<PersistentVolumeClaim>>(`/api/v1/persistentvolumeclaims${query(params)}`),
  persistentvolumeclaim: async (namespace: string, name: string) =>
    normalizePersistentVolumeClaimDetail(await request<PersistentVolumeClaimDetail>(`/api/v1/persistentvolumeclaims/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`)),
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

