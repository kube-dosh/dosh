import { devices, expect, test, type Page, type Route } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type {
  AccessReview,
  ContainerRegistry,
  ContainerRegistryUpsertInput,
  Diagnosis,
  HelmReleaseDetail,
  HelmReleaseDiff,
  HelmReleaseRollbackResult,
  HelmReleaseSummary,
  HelmReleaseUpgradePreview,
  HelmReleaseUpgradeResult,
  ListResponse,
  ClusterConfiguration,
  IngressDetail,
  ImageReferenceResolution,
  NodeDetail,
  NodeDrainPlan,
  NodeDrainResult,
  NodeSummary,
  Overview,
  PodDetail,
  PodSummary,
  PortForwardSession,
  PersistentVolumeClaimDetail,
  PersistentVolumeDetail,
  ResourceDescriptor,
  ResourceRelations,
  ServerCertificate,
  ServiceDetail,
  SessionResponse,
  StorageClassDetail,
  WorkloadDetail,
  WorkloadRevision,
  WorkloadSummary,
  UserPreferences,
} from "../src/api/types";

const csrfToken = "e2e-csrf-token";

function logFrames(...lines: string[]): string {
  return `${lines.map((line) => JSON.stringify({ type: "log", data: `${line}\n` })).join("\n")}\n{"type":"end"}\n`;
}

function workloadLogFrames(...lines: Array<{ pod: string; container: string; data: string }>): string {
  return `${lines.map((line) => JSON.stringify({ type: "log", data: `${line.data}\n`, pod: line.pod, container: line.container })).join("\n")}\n{"type":"end"}\n`;
}

const session: SessionResponse = {
  user: { username: "admin", admin: true, groups: ["dosh-admins"] },
  csrfToken,
  expiresAt: "2099-01-01T00:00:00Z",
  authMode: "local",
  capabilities: {
    read: true,
    scale: true,
    restart: true,
    updateImage: true,
    suspendCronJob: true,
    delete: true,
    create: true,
    createService: true,
    updateService: true,
    deleteService: true,
    deleteNamespace: true,
    revealSecrets: true,
    editYAML: true,
    nodeCordon: true,
    nodeDrain: true,
    podExec: true,
    podEphemeralDebug: true,
    podPortForward: true,
    helmReleases: true,
    helmUpgrade: true,
    reloadCertificate: true,
    resourceActions: {
      pods: { create: false, update: true, delete: true, scale: false },
      deployments: { create: true, update: true, delete: true, scale: true },
      statefulsets: { create: false, update: true, delete: true, scale: true },
      daemonsets: { create: false, update: true, delete: true, scale: false },
      jobs: { create: false, update: true, delete: true, scale: false },
      cronjobs: { create: false, update: true, delete: true, scale: false },
      services: { create: true, update: true, delete: true, scale: false },
      ingresses: { create: true, update: true, delete: true, scale: false },
      configmaps: { create: true, update: true, delete: true, scale: false },
      secrets: { create: true, update: true, delete: true, scale: false },
      persistentvolumeclaims: { create: true, update: true, delete: true, scale: false },
      persistentvolumes: { create: false, update: false, delete: false, scale: false },
      storageclasses: { create: false, update: false, delete: false, scale: false },
      nodes: { create: false, update: true, delete: false, scale: false },
      namespaces: { create: true, update: false, delete: true, scale: false },
    },
  },
};

const podSummary: PodSummary = {
  name: "web-abcdef-12345",
  namespace: "default",
  status: "Running",
  ready: "1/1",
  restarts: 0,
  ip: "10.244.1.10",
  node: "worker-0001",
  ownerKind: "ReplicaSet",
  ownerName: "web-abcdef",
  createdAt: "2026-11-02T00:00:00Z",
  hasMetrics: true,
  cpuUsageMilli: 25,
  memoryUsageBytes: 33554432,
};

const nodeSummary: NodeSummary = {
  name: "worker-0001",
  status: "Ready",
  roles: ["worker"],
  internalIP: "192.168.122.200",
  version: "v1.36.5",
  osImage: "Fedora",
  kernelVersion: "6.19",
  containerRuntime: "containerd://2.3.6",
  cpuCapacityMilli: 2000,
  memoryCapacityBytes: 4294967296,
  podCapacity: 110,
  podCount: 12,
  cpuRequestsMilli: 800,
  memoryRequestsBytes: 1073741824,
  hasMetrics: true,
  cpuUsageMilli: 350,
  memoryUsageBytes: 1073741824,
  unschedulable: false,
  createdAt: "2026-11-02T00:00:00Z",
};

const nodeDetail: NodeDetail = {
  ...nodeSummary,
  conditions: [{ type: "Ready", status: "True", reason: "KubeletReady" }],
  taints: [{
    key: "node-role.kubernetes.io/control-plane",
    value: "",
    effect: "NoSchedule",
    timeAdded: "2026-11-02T00:00:00Z",
  }],
  resourceVersion: "1",
  labels: { "kubernetes.io/hostname": nodeSummary.name },
  annotations: { "node.alpha.kubernetes.io/ttl": "0" },
  pods: [podSummary],
};

const workloadSummary: WorkloadSummary = {
  kind: "Deployment",
  name: "web",
  namespace: "default",
  resourceVersion: "42",
  status: "Running",
  ready: "2/2",
  desired: 2,
  restarts: 0,
  images: ["nginx:alpine"],
  createdAt: "2026-11-02T00:00:00Z",
};

const workloadDetail: WorkloadDetail = {
  ...workloadSummary,
  conditions: [{ type: "Available", status: "True" }],
  selector: { app: "web" },
  pods: [podSummary],
  volumes: [
    { name: "config", type: "configMap", source: "web-config", readOnly: true },
    { name: "data", type: "persistentVolumeClaim", source: "web-data", readOnly: false },
  ],
  containers: [{
    name: "web",
    image: "nginx:alpine",
    ports: [{ name: "http", containerPort: 80, protocol: "TCP" }],
    resources: { cpuRequestMilli: 100, cpuLimitMilli: 500, memoryRequestBytes: 134217728, memoryLimitBytes: 536870912 },
    env: [
      { name: "MODE", value: "production" },
      { name: "TOKEN", value: "", valueFrom: { kind: "secretKeyRef", name: "web-secret", key: "token" } },
    ],
    volumeMounts: [{ name: "config", mountPath: "/etc/web", readOnly: true }],
    readinessProbe: { type: "httpGet", path: "/ready", port: "80", scheme: "HTTP", command: [], initialDelaySeconds: 5, periodSeconds: 10, timeoutSeconds: 1, successThreshold: 1, failureThreshold: 3 },
  }],
};

const mockContainerRegistries: ContainerRegistry[] = [
  {
    id: "primary",
    name: "Primary Registry",
    endpoint: "https://registry.example.com",
    repositoryPrefix: "platform",
    authType: "basic",
    hasCredentials: true,
    hasCustomCa: false,
    insecureSkipTlsVerify: false,
    pullSecretName: "registry-pull",
    default: true,
    capabilities: { catalog: true, tags: true },
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
    revision: "registry-revision-1",
  },
  {
    id: "harbor",
    name: "Harbor",
    endpoint: "https://harbor.example.com",
    repositoryPrefix: "",
    authType: "anonymous",
    hasCredentials: false,
    hasCustomCa: false,
    insecureSkipTlsVerify: false,
    pullSecretName: "",
    default: false,
    capabilities: { catalog: true, tags: true },
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
    revision: "registry-revision-1",
  },
];

const mockRegistryRepositories: Record<string, string[]> = {
  primary: ["platform/api", "platform/worker"],
  harbor: ["library/nginx", "team/toolbox"],
};

const mockRegistryTags: Record<string, string[]> = {
  "primary/platform/api": ["1.0.0", "1.1.0", "2.0.0"],
  "primary/platform/worker": ["latest", "nightly"],
  "harbor/library/nginx": ["1.25", "1.27"],
  "harbor/team/toolbox": ["debug", "latest"],
};

function resolveMockRegistryImage(image: string, registries: ContainerRegistry[]): ImageReferenceResolution {
  let name = image.trim();
  let digest = "";
  if (name.includes("@")) {
    const parts = name.split("@");
    name = parts[0];
    digest = parts[1] ?? "";
  }
  const lastSlash = name.lastIndexOf("/");
  let tag = "";
  const colon = name.lastIndexOf(":");
  if (colon > lastSlash) {
    tag = name.slice(colon + 1);
    name = name.slice(0, colon);
  }
  const components = name.split("/");
  const explicitRegistry = components.length > 1 && (components[0].includes(".") || components[0].includes(":") || components[0] === "localhost");
  const fallback = { image, explicitRegistry, repository: explicitRegistry ? components.slice(1).join("/") : name, tag: tag || (digest ? "" : "latest"), digest, matched: false };
  if (!explicitRegistry) return fallback;
  for (const registry of registries) {
    const host = new URL(registry.endpoint).host;
    if (components[0].toLowerCase() !== host.toLowerCase()) continue;
    const repository = components.slice(1).join("/");
    const prefix = registry.repositoryPrefix?.replace(/^\/+|\/+$/g, "") ?? "";
    if (prefix && repository !== prefix && !repository.startsWith(`${prefix}/`)) continue;
    return {
      image,
      explicitRegistry: true,
      registryId: registry.id,
      registryName: registry.name,
      registryEndpoint: registry.endpoint,
      repository,
      tag: tag || (digest ? "" : "latest"),
      digest,
      matched: true,
    };
  }
  return fallback;
}

const cronJobDetail: WorkloadDetail = {
  ...workloadSummary,
  kind: "CronJob",
  name: "backup",
  resourceVersion: "77",
  status: "Idle",
  ready: "0/0",
  desired: 0,
  conditions: [],
  selector: {},
  pods: [],
  volumes: [],
  containers: [],
};

const podDetail: PodDetail = {
  ...podSummary,
  qosClass: "BestEffort",
  serviceAccount: "default",
  conditions: [],
  containers: [
    {
      name: "migrate",
      image: "busybox:1.36",
      init: true,
      ready: false,
      restartCount: 0,
      state: "Terminated",
      reason: "Completed",
      cpuRequestMilli: 10,
      cpuLimitMilli: 50,
      memoryRequestBytes: 16777216,
      memoryLimitBytes: 33554432,
    },
    {
      name: "web",
      image: "nginx:alpine",
      ports: [{ name: "http", protocol: "TCP", port: 80 }],
      ready: true,
      restartCount: 0,
      state: "Running",
      cpuRequestMilli: 100,
      cpuLimitMilli: 500,
      memoryRequestBytes: 134217728,
      memoryLimitBytes: 536870912,
    },
  ],
};

const diagnosisFixture: Diagnosis = {
  target: { kind: "Pod", namespace: "default", name: "web-abcdef-12345", status: "CrashLoopBackOff" },
  severity: "critical",
  summary: "发现 1 个需要立即处理的问题",
  generatedAt: "2026-11-17T08:00:00Z",
  checks: [
    { id: "phase", label: "Pod 阶段", status: "critical", message: "当前状态为 Running" },
    { id: "containers", label: "容器状态", status: "critical", message: "1 个容器尚未就绪" },
    { id: "events", label: "相关事件", status: "warning", message: "发现 2 条 Warning 事件" },
    { id: "logs", label: "容器日志", status: "warning", message: "日志中发现值得关注的错误模式" },
  ],
  findings: [
    {
      id: "web.crashloop", severity: "critical", title: "容器处于 CrashLoopBackOff",
      cause: "容器启动后反复退出，Kubernetes 正在指数退避重启。应结合上次退出码和容器日志定位启动失败原因。",
      evidence: [
        { label: "容器", value: "web" },
        { label: "镜像", value: "nginx:alpine" },
        { label: "上次终止", value: "OOMKilled（退出码 137）" },
      ],
      recommendations: [
        { label: "查看容器日志", path: "/pods/default/web-abcdef-12345?tab=logs" },
        { label: "复制排查命令", command: "kubectl describe pod web-abcdef-12345 -n default" },
      ],
    },
    {
      id: "logs.out-of-memory", severity: "warning", title: "日志显示内存耗尽",
      cause: "进程可能超过了容器内存限制，或被运行时终止。",
      evidence: [{ label: "web", value: "fatal: out of memory while serving request" }],
      recommendations: [{ label: "建议", description: "结合 OOMKilled 状态提高内存 Limit，或排查内存泄漏。" }],
    },
  ],
  related: [
    { kind: "Pod", namespace: "default", name: "web-abcdef-12345", status: "CrashLoopBackOff", relationship: "当前对象", path: "/pods/default/web-abcdef-12345" },
    { kind: "Node", name: "worker-0001", relationship: "运行于节点", path: "/nodes/worker-0001" },
  ],
  warnings: ["当前身份缺少部分日志权限，日志检查已跳过"],
};

const helmReleaseSummary: HelmReleaseSummary = {
  name: "web",
  namespace: "default",
  chart: "web",
  chartVersion: "1.2.3",
  appVersion: "2.0.0",
  revision: 2,
  status: "deployed",
  description: "upgrade complete",
  updatedAt: "2026-11-06T08:00:00Z",
};

const helmReleaseDetail: HelmReleaseDetail = {
  ...helmReleaseSummary,
  firstDeployed: "2026-11-01T08:00:00Z",
  lastDeployed: "2026-11-06T08:00:00Z",
  notes: "Visit https://web.example.test after rollout.",
  values: "replicaCount: 2\nimage:\n  repository: nginx\n  tag: alpine\npassword: <redacted>\n",
  manifest: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 2\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: web-secret\ndata:\n  token: <redacted>\n",
  history: [
    helmReleaseSummary,
    { ...helmReleaseSummary, revision: 1, status: "superseded", description: "install complete", appVersion: "1.0.0", updatedAt: "2026-11-01T08:00:00Z" },
  ],
  warnings: [],
};

const helmReleaseDiff: HelmReleaseDiff = {
  namespace: "default",
  name: "web",
  from: { ...helmReleaseSummary, revision: 1, status: "superseded", appVersion: "1.0.0" },
  to: helmReleaseSummary,
  valuesDiff: "--- values rev 1\n+++ values rev 2\n@@\n-image:\n-  tag: 1.0.0\n+image:\n+  tag: alpine\n",
  manifestDiff: "--- manifest rev 1\n+++ manifest rev 2\n@@\n-  replicas: 1\n+  replicas: 2\n",
  warnings: [],
};

const helmRollbackResult: HelmReleaseRollbackResult = {
  namespace: "default",
  name: "web",
  targetRevision: 1,
  revision: 3,
  appliedObjects: 2,
  warnings: ["已跳过 1 个 Helm Hook"],
};

const helmUpgradePreview: HelmReleaseUpgradePreview = {
  namespace: "default",
  name: "web",
  currentRevision: 2,
  nextRevision: 3,
  valuesDiff: "--- current values\n+++ next values\n@@\n-replicas: 2\n+replicaCount: 3\n",
  manifestDiff: "--- current manifest\n+++ next manifest\n@@\n-  replicas: 2\n+  replicas: 3\n",
  warnings: [],
};

const helmUpgradeResult: HelmReleaseUpgradeResult = {
  namespace: "default",
  name: "web",
  previousRevision: 2,
  revision: 3,
  appliedObjects: 3,
  warnings: [],
};

const resourceRelations: ResourceRelations = {
  items: [
    { apiVersion: "apps/v1", kind: "ReplicaSet", resource: "replicasets", name: "web-abcdef", namespace: "default", relationship: "owner", direction: "upstream" },
    { apiVersion: "v1", kind: "Service", resource: "services", name: "web", namespace: "default", relationship: "selects", direction: "downstream", status: "Running" },
    { apiVersion: "v1", kind: "Pod", resource: "pods", name: "web-broken", namespace: "default", relationship: "selects", direction: "downstream", status: "CrashLoopBackOff" },
    { apiVersion: "v1", kind: "Node", resource: "nodes", name: "worker-0001", relationship: "scheduled on", direction: "upstream" },
  ],
  warnings: [],
};

const emptyPreferences: UserPreferences = {
  favorites: [],
  recent: [],
  savedViews: {},
  hiddenColumns: {},
  updatedAt: "2026-11-16T00:00:00Z",
};

const nodeDrainPlan: NodeDrainPlan = {
  node: "worker-0001",
  unschedulable: false,
  evictCount: 1,
  skippedCount: 1,
  pods: [
    { name: "web-abcdef-12345", namespace: "default", status: "Running", ownerKind: "ReplicaSet", ownerName: "web-abcdef", action: "evict", pdbNames: ["web-pdb"] },
    { name: "node-agent", namespace: "kube-system", status: "Running", ownerKind: "DaemonSet", ownerName: "node-agent", action: "block", reason: "存在 DaemonSet Pod，需要显式忽略 DaemonSet" },
  ],
};

const nodeDrainResult: NodeDrainResult = {
  node: "worker-0001",
  cordoned: true,
  evictedCount: 1,
  blockedCount: 1,
  failedCount: 0,
  skippedCount: 0,
  warnings: ["Pod 已被 PodDisruptionBudget 阻止，后续 Pod 未驱逐"],
  pods: [
    { ...nodeDrainPlan.pods[0], action: "evict" },
    { ...nodeDrainPlan.pods[1], action: "blocked", evictionError: "Cannot evict pod as it would violate the pod's disruption budget." },
  ],
};

const workloadRevisions: WorkloadRevision[] = [
  {
    kind: "Deployment", name: "web", revision: 2, current: true, changeCause: "kubectl set image", status: "active", replicas: 2, readyReplicas: 2,
    images: ["nginx:alpine"], createdAt: "2026-11-06T08:00:00Z",
    template: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n        - name: web\n          image: nginx:alpine\n",
  },
  {
    kind: "Deployment", name: "web", revision: 1, current: false, changeCause: "initial rollout", status: "superseded", replicas: 0, readyReplicas: 0,
    images: ["nginx:1.25"], createdAt: "2026-11-01T08:00:00Z",
    template: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n        - name: web\n          image: nginx:1.25\n",
  },
];

const serviceDetail: ServiceDetail = {
  name: "web",
  namespace: "default",
  type: "ClusterIP",
  headless: false,
  clusterIP: "10.96.0.10",
  externalIP: "",
  ports: [{ name: "http", protocol: "TCP", port: 80, targetPort: "8080" }],
  selector: { app: "web" },
  endpointCount: 1,
  resourceVersion: "42",
  createdAt: "2026-11-02T00:00:00Z",
  labels: { app: "web" },
  annotations: { team: "platform" },
  sessionAffinity: "None",
  ipFamilies: ["IPv4"],
  ipFamilyPolicy: "SingleStack",
  endpoints: [{ addresses: ["10.244.1.10"], ready: true, targetKind: "Pod", targetName: "web-abcdef-12345", targetNamespace: "default", nodeName: "worker-0001" }],
};

const ingressDetail: IngressDetail = {
  name: "web",
  namespace: "default",
  class: "nginx",
  createdAt: "2026-11-02T00:00:00Z",
  paths: [{ host: "web.example.test", path: "/", pathType: "Prefix", service: "web", port: "80", targetStatus: "Ready" }],
  tlsHosts: ["web.example.test"],
  labels: { app: "web" },
  annotations: { "nginx.ingress.kubernetes.io/rewrite-target": "/" },
  defaultBackend: { host: "", path: "", pathType: "", service: "web", port: "80", targetStatus: "Ready" },
  tls: [{ hosts: ["web.example.test"], secretName: "web-tls" }],
};

const storageClassDetail: StorageClassDetail = {
  name: "data-standard", provisioner: "csi.example.test", reclaimPolicy: "Delete", volumeBinding: "WaitForFirstConsumer",
  default: true, createdAt: "2026-11-02T00:00:00Z", labels: { tier: "gold" }, annotations: { owner: "platform" },
  parameters: { type: "premium-rwo", encrypted: "true" }, mountOptions: ["discard"], allowVolumeExpansion: true,
};

const persistentVolumeDetail: PersistentVolumeDetail = {
  name: "pv-data", capacityBytes: 10 * 1024 * 1024 * 1024, accessModes: ["ReadWriteOnce"], reclaimPolicy: "Retain",
  status: "Bound", claim: "production/data-claim", storageClass: "data-standard", createdAt: "2026-11-02T00:00:00Z",
  labels: { tier: "gold" }, annotations: { owner: "platform" }, volumeMode: "Filesystem", mountOptions: ["discard"],
  claimNamespace: "production", claimName: "data-claim", csiDriver: "csi.example.test",
};

const persistentVolumeClaimDetail: PersistentVolumeClaimDetail = {
  name: "data-claim", namespace: "production", status: "Bound", volumeName: "pv-data", capacityBytes: 10 * 1024 * 1024 * 1024,
  accessModes: ["ReadWriteOnce"], storageClass: "data-standard", createdAt: "2026-11-02T00:00:00Z",
  labels: { app: "database" }, annotations: { backup: "daily" }, volumeMode: "Filesystem", requestedCapacityBytes: 5 * 1024 * 1024 * 1024,
  conditions: [{ type: "Resizing", status: "True", reason: "Expanding", message: "volume expansion in progress", lastTransitionTime: "2026-11-02T01:00:00Z" }],
};

const overview: Overview = {
  cluster: {
    name: "kubedo-lab",
    version: "v1.36.5",
    cni: "Cilium",
    status: "Healthy",
    metricsAvailable: true,
  },
  counts: {
    nodes: 8,
    readyNodes: 8,
    pods: 42,
    runningPods: 40,
    workloads: 12,
    namespaces: 9,
    unhealthyPods: 2,
    warningEvents: 1,
  },
  capacity: {
    cpuAllocatableMilli: 16000,
    memoryAllocatableBytes: 34359738368,
    podCapacity: 880,
    cpuRequestsMilli: 4200,
    memoryRequestsBytes: 8589934592,
    cpuLimitsMilli: 8000,
    memoryLimitsBytes: 17179869184,
    cpuUsageMilli: 2800,
    memoryUsageBytes: 5368709120,
  },
  recentEvents: [{
    uid: "event-1",
    namespace: "default",
    type: "Warning",
    reason: "BackOff",
    message: "Back-off restarting failed container",
    objectKind: "Pod",
    objectName: "web-abcdef-12345",
    count: 3,
    firstSeen: "2026-11-02T00:00:00Z",
    lastSeen: "2026-11-02T00:00:00Z",
  }],
};

const clusterConfiguration: ClusterConfiguration = {
  name: "kubedo-lab",
  version: "v1.36.5",
  platform: "linux/amd64",
  cni: "Cilium",
  controlPlaneEndpoint: "192.168.122.50:16443",
  podCIDR: "10.244.0.0/16",
  serviceCIDR: "10.96.0.0/12",
  dnsDomain: "cluster.local",
  kubernetesServiceIP: "10.96.0.1",
  dnsServiceIP: "10.96.0.10",
  source: "kubeadm-config",
  nodeCIDRs: [{ name: "worker-0001", internalIP: "192.168.122.200", podCIDRs: ["10.244.1.0/24"] }],
  warnings: [],
};

const serverCertificate: ServerCertificate = {
  mode: "https",
  enabled: true,
  configured: true,
  status: "valid",
  subject: "CN=dosh.xuthus.cc",
  issuer: "CN=Cloudflare Inc ECC CA-3, O=Cloudflare, Inc., C=US",
  serialNumber: "05a1b2c3d4e5f60718293a4b5c6d7e8f",
  dnsNames: ["dosh.xuthus.cc"],
  ipAddresses: ["10.244.2.211"],
  notBefore: "2026-10-01T00:00:00Z",
  notAfter: "2027-01-01T00:00:00Z",
  daysRemaining: 56,
  fingerprintSha256: "8F:40:69:8B:64:50:A5:69:B0:31:1D:E3:A6:70:68:6A:86:90:63:0C:99:59:45:66:8F:62:52:C6:45:AE:DA:50",
  keyAlgorithm: "ECDSA P-256",
  autoReload: true,
  reloadIntervalSeconds: 30,
  lastReloadedAt: "2026-11-06T06:30:00Z",
};

function json(route: Route, value: unknown, status = 200) {
  return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(value) });
}

function list<T>(items: T[]): ListResponse<T> {
  return { items, meta: { total: items.length, totalKnown: true, page: 1, pageSize: Math.max(items.length, 1), totalPages: items.length ? 1 : 0, hasMore: false, warnings: [], filters: {} } };
}

async function openResourceMenu(page: Page, resource: string) {
  await page.getByRole("button", { name: `操作 ${resource}` }).click();
}

type NodeSchedulingRequest = { method: string; path: string; csrf: string | null };
type NodeMetadataRequest = { method: string; path: string; csrf: string | null; body: Record<string, unknown> };
type PreferenceRequest = { method: string; body: UserPreferences };
type RegistryRequest = { method: string; path: string; body?: ContainerRegistryUpsertInput };

async function installApiMocks(page: Page, options: { authenticated?: boolean; authMode?: "local" | "token"; overview?: Overview; overviewStatus?: number; nodeSchedulingRequests?: NodeSchedulingRequest[]; nodeMetadataRequests?: NodeMetadataRequest[]; preferenceRequests?: PreferenceRequest[]; registryRequests?: RegistryRequest[]; containerRegistries?: ContainerRegistry[]; registryFailures?: { repositories?: boolean; tags?: boolean }; expireSessionOnNodes?: boolean; noIngressController?: boolean; podName?: string; podExec?: boolean; podEphemeralDebug?: boolean; emptyDiscovery?: boolean; ephemeralRequests?: Array<{ image: string; targetContainerName?: string; shell?: string }>; diagnosis?: Diagnosis } = {}) {
  const authenticated = options.authenticated ?? true;
  const authMode = options.authMode ?? "local";
  const sessionResponse: SessionResponse = { ...session, authMode, capabilities: { ...session.capabilities, podExec: options.podExec ?? session.capabilities.podExec, podEphemeralDebug: options.podEphemeralDebug ?? session.capabilities.podEphemeralDebug } };
  const listedPod = options.podName ? { ...podSummary, name: options.podName } : podSummary;
  let signedIn = authenticated;
  let nodeSchedulingDisabled = false;
  let nodeLabels = { ...nodeDetail.labels };
  let nodeTaints = [...nodeDetail.taints];
  let nodeResourceVersion = 1;
  let services: ServiceDetail[] = [{ ...serviceDetail }];
  let tunnelSession: PortForwardSession | null = null;
  let widgetRevision = 1;
  let preferences = { ...emptyPreferences };
  let containerRegistries = structuredClone(options.containerRegistries ?? mockContainerRegistries);
  const currentNodeDetail = () => ({
    ...nodeDetail,
    unschedulable: nodeSchedulingDisabled,
    resourceVersion: String(nodeResourceVersion),
    labels: nodeLabels,
    taints: nodeTaints,
  });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();

    if (path === "/api/v1/auth/config" && method === "GET") {
      return json(route, { mode: authMode, providerName: authMode === "token" ? "Kubernetes" : "" });
    }
    if (path === "/api/v1/session" && method === "GET") {
      return signedIn ? json(route, sessionResponse) : json(route, { code: "unauthorized", message: "authentication required" }, 401);
    }
    if (path === "/api/v1/session" && method === "POST") {
      if (authMode === "token" && request.postDataJSON()?.token !== "e2e-kubernetes-token") {
        return json(route, { code: "unauthorized", message: "authentication required" }, 401);
      }
      signedIn = true;
      return json(route, sessionResponse);
    }
    if (path === "/api/v1/session" && method === "DELETE") {
      signedIn = false;
      return route.fulfill({ status: 204, body: "" });
    }
    if (!signedIn) {
      return json(route, { code: "unauthorized", message: "authentication required" }, 401);
    }
    if (options.expireSessionOnNodes && path === "/api/v1/nodes") {
      signedIn = false;
      return json(route, { code: "unauthorized", message: "authentication required" }, 401);
    }
    if (path.startsWith("/api/v1/diagnostics/") && method === "GET") {
      const parts = path.split("/").filter(Boolean).slice(3);
      const kind = decodeURIComponent(parts[0] ?? "pod");
      const name = decodeURIComponent(parts.at(-1) ?? "unknown");
      const namespace = parts.length >= 3 ? decodeURIComponent(parts[1]) : undefined;
      return json(route, options.diagnosis ? structuredClone(options.diagnosis) : {
        ...diagnosisFixture,
        target: { ...diagnosisFixture.target, kind, namespace, name },
      });
    }
    if (path === "/api/v1/preferences" && method === "GET") return json(route, preferences);
    if (path === "/api/v1/preferences" && method === "PUT") {
      const body = request.postDataJSON() as UserPreferences;
      options.preferenceRequests?.push({ method, body });
      preferences = { ...body, updatedAt: "2026-11-16T00:00:00Z" };
      return json(route, preferences);
    }
    if (path === "/api/v1/container-registries" && method === "GET") return json(route, list(containerRegistries));
    if (path === "/api/v1/container-registries/resolve" && method === "GET") {
      const image = new URL(request.url()).searchParams.get("image") ?? "";
      return json(route, resolveMockRegistryImage(image, containerRegistries));
    }
    if (path === "/api/v1/container-registries" && method === "POST") {
      const body = request.postDataJSON() as ContainerRegistryUpsertInput;
      options.registryRequests?.push({ method, path, body });
      const saved: ContainerRegistry = {
        id: `registry-${containerRegistries.length + 1}`,
        name: body.name,
        endpoint: body.endpoint,
        repositoryPrefix: body.repositoryPrefix,
        authType: body.authType,
        hasCredentials: Boolean(body.credentials?.username || body.credentials?.password || body.credentials?.token),
        hasCustomCa: Boolean(body.credentials?.caPem),
        insecureSkipTlsVerify: Boolean(body.insecureSkipTlsVerify),
        pullSecretName: body.pullSecretName,
        default: Boolean(body.default),
        capabilities: { catalog: true, tags: true },
        createdAt: "2026-11-16T00:00:00Z",
        updatedAt: "2026-11-16T00:00:00Z",
        revision: `registry-revision-${containerRegistries.length + 1}`,
      };
      if (saved.default) containerRegistries = containerRegistries.map((item) => ({ ...item, default: false }));
      containerRegistries = [...containerRegistries, saved];
      return json(route, saved, 201);
    }
    const registryAction = path.match(/^\/api\/v1\/container-registries\/([^/]+)\/(test|repositories|tags)$/);
    if (registryAction) {
      const registryID = decodeURIComponent(registryAction[1]);
      const action = registryAction[2];
      const registry = containerRegistries.find((item) => item.id === registryID);
      if (!registry) return json(route, { code: "not_found", message: "container registry not found" }, 404);
      if (action === "test" && method === "POST") {
        const tested: ContainerRegistry = {
          ...registry,
          lastTest: { ok: true, message: "连接成功", testedAt: "2026-11-16T00:00:00Z", capabilities: { catalog: true, tags: true } },
        };
        containerRegistries = containerRegistries.map((item) => item.id === registryID ? tested : item);
        return json(route, tested);
      }
      if (action === "repositories" && method === "GET") {
        if (options.registryFailures?.repositories) return json(route, { code: "dependency_unavailable", message: "cluster API is unavailable" }, 503);
        const params = new URL(request.url()).searchParams;
        const query = params.get("q")?.trim().toLowerCase() ?? "";
        const host = new URL(registry.endpoint).host;
        const prefix = registry.repositoryPrefix?.replace(/^\/+|\/+$/g, "") ?? "";
        const items = (mockRegistryRepositories[registryID] ?? [])
          .filter((repository) => !prefix || repository === prefix || repository.startsWith(`${prefix}/`))
          .filter((repository) => !query || repository.toLowerCase().includes(query))
          .map((repository) => ({ name: repository, image: `${host}/${repository}` }));
        return json(route, list(items));
      }
      if (action === "tags" && method === "GET") {
        if (options.registryFailures?.tags) return json(route, { code: "dependency_unavailable", message: "cluster API is unavailable" }, 503);
        const params = new URL(request.url()).searchParams;
        const repository = params.get("repository") ?? "";
        const query = params.get("q")?.trim().toLowerCase() ?? "";
        const host = new URL(registry.endpoint).host;
        const items = (mockRegistryTags[`${registryID}/${repository}`] ?? [])
          .filter((tag) => !query || tag.toLowerCase().includes(query))
          .map((tag) => ({ name: tag, image: `${host}/${repository}:${tag}` }));
        return json(route, list(items));
      }
    }
    const registryItemPath = path.match(/^\/api\/v1\/container-registries\/([^/]+)$/);
    if (registryItemPath && (method === "PUT" || method === "DELETE")) {
      const registryID = decodeURIComponent(registryItemPath[1]);
      const registry = containerRegistries.find((item) => item.id === registryID);
      if (!registry) return json(route, { code: "not_found", message: "container registry not found" }, 404);
      if (method === "DELETE") {
        options.registryRequests?.push({ method, path });
        containerRegistries = containerRegistries.filter((item) => item.id !== registryID);
        return json(route, { status: "deleted" });
      }
      const body = request.postDataJSON() as ContainerRegistryUpsertInput;
      const updated: ContainerRegistry = {
        ...registry,
        name: body.name,
        endpoint: body.endpoint,
        repositoryPrefix: body.repositoryPrefix,
        authType: body.authType,
        hasCredentials: body.clearCredentials ? false : Boolean(body.credentials?.username || body.credentials?.password || body.credentials?.token || registry.hasCredentials),
        hasCustomCa: body.clearCredentials ? false : Boolean(body.credentials?.caPem || registry.hasCustomCa),
        insecureSkipTlsVerify: Boolean(body.insecureSkipTlsVerify),
        pullSecretName: body.pullSecretName,
        default: Boolean(body.default),
        updatedAt: "2026-11-16T00:00:00Z",
      };
      options.registryRequests?.push({ method, path, body });
      if (updated.default) containerRegistries = containerRegistries.map((item) => item.id === registryID ? updated : { ...item, default: false });
      else containerRegistries = containerRegistries.map((item) => item.id === registryID ? updated : item);
      return json(route, updated);
    }
    if (path === "/api/v1/overview") return json(route, options.overview ?? overview, options.overviewStatus ?? 200);
    if (path === "/api/v1/resources/discovery") {
      const resources: ResourceDescriptor[] = options.emptyDiscovery ? [] : [
        { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true, preferred: true, verbs: ["get", "list", "watch", "create", "delete"], subresources: ["exec", "log", "status"] },
        { group: "", version: "v1", resource: "secrets", kind: "Secret", namespaced: true, preferred: true, verbs: ["get", "list"] },
        { group: "apps", version: "v1", resource: "deployments", kind: "Deployment", namespaced: true, preferred: true, verbs: ["get", "list", "update", "patch"], subresources: ["scale", "status"] },
        { group: "apps", version: "v1", resource: "statefulsets", kind: "StatefulSet", namespaced: true, preferred: true, verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { group: "", version: "v1", resource: "nodes", kind: "Node", namespaced: false, preferred: true, verbs: ["get", "list", "update", "patch"], subresources: ["proxy", "status"] },
        { group: "", version: "v1", resource: "serviceaccounts", kind: "ServiceAccount", namespaced: true, verbs: ["get", "list"] },
        { group: "rbac.authorization.k8s.io", version: "v1", resource: "roles", kind: "Role", namespaced: true, verbs: ["get", "list"] },
        { group: "rbac.authorization.k8s.io", version: "v1", resource: "rolebindings", kind: "RoleBinding", namespaced: true, verbs: ["get", "list"] },
        { group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterroles", kind: "ClusterRole", namespaced: false, verbs: ["get", "list"] },
        { group: "rbac.authorization.k8s.io", version: "v1", resource: "clusterrolebindings", kind: "ClusterRoleBinding", namespaced: false, verbs: ["get", "list"] },
        { group: "networking.k8s.io", version: "v1", resource: "networkpolicies", kind: "NetworkPolicy", namespaced: true, verbs: ["get", "list"] },
        { group: "policy", version: "v1", resource: "poddisruptionbudgets", kind: "PodDisruptionBudget", namespaced: true, verbs: ["get", "list"] },
        { group: "autoscaling", version: "v2", resource: "horizontalpodautoscalers", kind: "HorizontalPodAutoscaler", namespaced: true, verbs: ["get", "list"] },
        { group: "", version: "v1", resource: "resourcequotas", kind: "ResourceQuota", namespaced: true, verbs: ["get", "list"] },
        { group: "", version: "v1", resource: "limitranges", kind: "LimitRange", namespaced: true, verbs: ["get", "list"] },
        { group: "scheduling.k8s.io", version: "v1", resource: "priorityclasses", kind: "PriorityClass", namespaced: false, verbs: ["get", "list"] },
        { group: "example.io", version: "v1", resource: "widgets", kind: "Widget", namespaced: true, preferred: true, verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { group: "example.io", version: "v1beta1", resource: "widgets", kind: "Widget", namespaced: true, verbs: ["get", "list"] },
        { group: "example.io", version: "v1", resource: "clusterwidgets", kind: "ClusterWidget", namespaced: false, preferred: true, verbs: ["get", "list", "create", "update", "patch", "delete"] },
      ];
      return json(route, { items: resources });
    }
    if (path === "/api/v1/resources/search" && method === "GET") {
      const q = new URL(request.url()).searchParams.get("q")?.trim().toLowerCase() ?? "";
      return json(route, {
        items: q.length >= 2 && "sample-widget".includes(q) ? [{
          apiVersion: "example.io/v1", kind: "Widget", resource: "widgets", name: "sample-widget", namespace: "default",
          resourceVersion: String(widgetRevision), createdAt: "2026-11-02T00:00:00Z",
        }] : [],
        warnings: [],
        truncated: false,
      });
    }
    if (path === "/api/v1/resources/_core/v1/pods" && method === "GET") {
      return json(route, list([{ apiVersion: "v1", kind: "Pod", resource: "pods", name: "web-abcdef-12345", namespace: "default", resourceVersion: "1", createdAt: "2026-11-02T00:00:00Z" }]));
    }
    if (path === "/api/v1/resources/_core/v1/secrets" && method === "GET") {
      return json(route, list([{ apiVersion: "v1", kind: "Secret", resource: "secrets", name: "app-secret", namespace: "default", resourceVersion: "1", createdAt: "2026-11-02T00:00:00Z" }]));
    }
    if (path === "/api/v1/resources/capabilities") {
      const resource = new URL(request.url()).searchParams.get("resource");
      if (resource === "clusterwidgets") {
        return json(route, {
          create: true, update: true, delete: true, scale: false,
          riskLevel: "critical", requireConfirmation: true,
          riskReasons: ["这是测试中的集群级高风险资源", "写入会影响全部命名空间"],
        });
      }
      return json(route, { create: true, update: true, delete: true, scale: false, riskLevel: "normal", requireConfirmation: false });
    }
    if (path === "/api/v1/resources/schema") {
      const params = new URL(request.url()).searchParams;
      const resource = params.get("resource") ?? "widgets";
      if (resource === "statefulsets") {
        return json(route, {
          group: "apps", version: "v1", resource, kind: "StatefulSet", source: "openapi-v2",
          schema: { type: "object", properties: { apiVersion: { type: "string" }, kind: { type: "string" }, metadata: { $ref: "#/definitions/meta" }, spec: { $ref: "#/definitions/spec" } } },
          definitions: {
            meta: { type: "object", required: ["name"], properties: { name: { type: "string" }, namespace: { type: "string" } } },
            spec: { type: "object", properties: { serviceName: { type: "string" }, replicas: { type: "integer" }, selector: { type: "object" }, template: { type: "object" } } },
          },
        });
      }
      const kind = resource === "clusterwidgets" ? "ClusterWidget" : "Widget";
      return json(route, {
        group: "example.io", version: params.get("version") ?? "v1", resource, kind, source: "openapi-v2",
        schema: {
          type: "object", required: ["metadata", "spec"],
          properties: {
            apiVersion: { type: "string" }, kind: { type: "string" },
            metadata: { $ref: "#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta" },
            spec: { $ref: "#/definitions/example.io.v1.WidgetSpec" },
          },
        },
        definitions: {
          "io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": { type: "object", required: ["name"], properties: { name: { type: "string" }, namespace: { type: "string" } } },
          "example.io.v1.WidgetSpec": { type: "object", required: ["size"], properties: { size: { type: "string", enum: ["small", "large"], title: "尺寸" }, replicas: { type: "integer", title: "副本数" } } },
        },
      });
    }
    if (path === "/api/v1/resources/example.io/v1/widgets" && method === "GET") {
      return json(route, list([{
        apiVersion: "example.io/v1", kind: "Widget", resource: "widgets", name: "sample-widget", namespace: "default",
        resourceVersion: String(widgetRevision), createdAt: "2026-11-02T00:00:00Z", labels: { app: "demo" },
      }]));
    }
    if (path === "/api/v1/resources/example.io/v1beta1/widgets" && method === "GET") {
      return json(route, list([{
        apiVersion: "example.io/v1beta1", kind: "Widget", resource: "widgets", name: "sample-widget", namespace: "default",
        resourceVersion: String(widgetRevision), createdAt: "2026-11-02T00:00:00Z", labels: { app: "demo" },
      }]));
    }
    if (path === "/api/v1/resources/example.io/v1/clusterwidgets" && method === "GET") {
      return json(route, list([{
        apiVersion: "example.io/v1", kind: "ClusterWidget", resource: "clusterwidgets", name: "sample-cluster-widget",
        resourceVersion: String(widgetRevision), createdAt: "2026-11-02T00:00:00Z",
      }]));
    }
    if (path === "/api/v1/resources/apps/v1/statefulsets" && method === "GET") return json(route, list([]));
    if (/^\/api\/v1\/resources\/example\.io\/v1(?:beta1)?\/widgets\/sample-widget$/.test(path) && method === "GET") {
      const version = path.includes("v1beta1") ? "v1beta1" : "v1";
      return json(route, {
        apiVersion: `example.io/${version}`, kind: "Widget", name: "sample-widget", namespace: "default",
        resourceVersion: String(widgetRevision), createdAt: "2026-11-02T00:00:00Z", labels: { app: "demo" }, annotations: {},
        object: { apiVersion: `example.io/${version}`, kind: "Widget", metadata: { name: "sample-widget", namespace: "default" }, spec: { size: "small" } },
        yaml: `apiVersion: example.io/${version}\nkind: Widget\nmetadata:\n  name: sample-widget\n  namespace: default\nspec:\n  size: small\n`,
      });
    }
    if (path === "/api/v1/resources/example.io/v1/widgets" && method === "POST") {
      widgetRevision++;
      return json(route, { apiVersion: "example.io/v1", kind: "Widget", name: "created-widget", namespace: "default", object: {}, yaml: "" }, 201);
    }
    if (path === "/api/v1/resources/example.io/v1/widgets/sample-widget" && method === "PUT") {
      widgetRevision++;
      return json(route, { apiVersion: "example.io/v1", kind: "Widget", name: "sample-widget", namespace: "default", object: {}, yaml: "" });
    }
    if (path === "/api/v1/resources/example.io/v1/widgets/sample-widget" && method === "DELETE") {
      return json(route, { status: "deleting" }, 202);
    }
    if (path === "/api/v1/cluster/config") return json(route, clusterConfiguration);
    if (path === "/api/v1/capabilities") return json(route, sessionResponse.capabilities);
    if (path === "/api/v1/capabilities/review" && method === "GET") {
      const params = new URL(request.url()).searchParams;
      const resource = params.get("resource") ?? "pods";
      const allowed = resource !== "secrets";
      const review: AccessReview = {
        verb: params.get("verb") ?? "get",
        apiGroup: params.get("apiGroup") ?? "",
        resource,
        subresource: params.get("subresource") ?? "",
        namespace: params.get("namespace") ?? "",
        name: params.get("name") ?? "",
        allowed,
        reason: allowed ? "allowed" : "rbac_denied",
        guidance: allowed ? "当前身份拥有该操作权限。" : "Kubernetes RBAC 未授权当前身份执行该操作，请检查 RoleBinding。",
        kubectlCommand: `kubectl auth can-i ${params.get("verb") ?? "get"} ${resource}${params.get("namespace") ? ` --namespace ${params.get("namespace")}` : ""}`,
      };
      return json(route, review);
    }
    if (path === "/api/v1/nodes") return json(route, list([{ ...nodeSummary, unschedulable: nodeSchedulingDisabled }]));
    if (path === "/api/v1/nodes/worker-0001") return json(route, currentNodeDetail());
    if (path === "/api/v1/nodes/worker-0001/drain-plan" && method === "GET") return json(route, nodeDrainPlan);
    if (path === "/api/v1/nodes/worker-0001/drain" && method === "POST") return json(route, nodeDrainResult);
    if (path === "/api/v1/nodes/worker-0001/labels" && method === "PUT") {
      const body = request.postDataJSON() as { labels: Record<string, string>; resourceVersion: string };
      options.nodeMetadataRequests?.push({ method, path, csrf: request.headers()["x-csrf-token"] ?? null, body });
      if (body.resourceVersion !== String(nodeResourceVersion)) return json(route, { code: "conflict", message: "resource changed since it was loaded" }, 409);
      nodeLabels = { ...body.labels };
      nodeResourceVersion++;
      return json(route, currentNodeDetail());
    }
    if (path === "/api/v1/nodes/worker-0001/taints" && method === "PUT") {
      const body = request.postDataJSON() as { taints: NodeDetail["taints"]; resourceVersion: string };
      options.nodeMetadataRequests?.push({ method, path, csrf: request.headers()["x-csrf-token"] ?? null, body });
      if (body.resourceVersion !== String(nodeResourceVersion)) return json(route, { code: "conflict", message: "resource changed since it was loaded" }, 409);
      nodeTaints = body.taints.map((taint) => ({ ...taint, timeAdded: taint.timeAdded ?? "2026-11-06T08:00:00Z" }));
      nodeResourceVersion++;
      return json(route, currentNodeDetail());
    }
    if (path === "/api/v1/nodes/worker-0001/cordon" && method === "POST") {
      options.nodeSchedulingRequests?.push({ method, path, csrf: request.headers()["x-csrf-token"] ?? null });
      nodeSchedulingDisabled = true;
      return json(route, { ...nodeSummary, unschedulable: true });
    }
    if (path === "/api/v1/nodes/worker-0001/uncordon" && method === "POST") {
      options.nodeSchedulingRequests?.push({ method, path, csrf: request.headers()["x-csrf-token"] ?? null });
      nodeSchedulingDisabled = false;
      return json(route, { ...nodeSummary, unschedulable: false });
    }
    if (path === "/api/v1/workloads") {
      const params = new URL(request.url()).searchParams;
      const q = params.get("q")?.trim() ?? "";
      const namespace = params.get("namespace")?.trim() ?? "";
      const kind = params.get("kind")?.trim().toLowerCase() ?? "";
      const status = params.get("status")?.trim().toLowerCase() ?? "";
      const items = [workloadSummary].filter((item) => (
        (!q || item.name.includes(q))
        && (!namespace || item.namespace === namespace)
        && (!kind || item.kind.toLowerCase() === kind)
        && (!status || item.status.toLowerCase() === status)
      ));
      return json(route, list(items));
    }
    if (path === "/api/v1/namespaces") {
      const query = new URL(request.url()).searchParams.get("q")?.trim() ?? "";
      return json(route, list([
        { name: "default", status: "Active", createdAt: "2026-11-02T00:00:00Z" },
        { name: "production", status: "Active", createdAt: "2026-11-02T00:00:00Z" },
      ].filter((item) => item.name.includes(query))));
    }
    if (path === "/api/v1/namespaces/default") return json(route, {
      name: "default",
      status: "Active",
      labels: { "kubernetes.io/metadata.name": "default" },
      annotations: { owner: "platform" },
      createdAt: "2026-11-02T00:00:00Z",
      resourceQuotas: [{
        name: "compute",
        hard: { "requests.cpu": "2", "requests.memory": "2Gi" },
        used: { "requests.cpu": "1", "requests.memory": "1Gi" },
        createdAt: "2026-11-02T00:00:00Z",
      }],
      limitRanges: [{
        name: "container-defaults",
        limits: [{
          type: "Container",
          min: { cpu: "50m" },
          max: { cpu: "1" },
          default: { cpu: "500m" },
          defaultRequest: { cpu: "100m" },
        }],
        createdAt: "2026-11-02T00:00:00Z",
      }],
    });
    if (path === "/api/v1/namespaces/production" && method === "GET") return json(route, {
      name: "production",
      status: "Active",
      resourceVersion: "17",
      labels: { "kubernetes.io/metadata.name": "production" },
      annotations: {},
      createdAt: "2026-11-02T00:00:00Z",
      resourceQuotas: [],
      limitRanges: [],
    });
    if (path === "/api/v1/namespaces/production" && method === "DELETE") {
      return json(route, { status: "deleting" }, 202);
    }
    if (path === "/api/v1/workloads/deployment/default/web") return json(route, workloadDetail);
    if (path === "/api/v1/workloads/deployment/default/web/logs") {
      return route.fulfill({
        status: 200,
        contentType: "application/x-ndjson; charset=utf-8",
        body: workloadLogFrames(
          { pod: "web-abcdef-12345", container: "web", data: "aggregate log from web instance" },
          { pod: "web-abcdef-67890", container: "web", data: "aggregate log from second instance" },
        ),
      });
    }
    if (path === "/api/v1/workloads/deployment/default/web/scale" && method === "POST") {
      return json(route, { status: "ok" });
    }
    if (path === "/api/v1/workloads/deployment/default/web/image" && method === "POST") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/workloads/deployment/default/web/containers/web/config" && method === "PUT") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/workloads/deployment/default/web/revisions" && method === "GET") return json(route, { items: workloadRevisions });
    if (path === "/api/v1/workloads/deployment/default/web/rollback" && method === "POST") return json(route, { status: "updated" });
    if (path === "/api/v1/workloads/cronjob/default/backup" && method === "GET") return json(route, cronJobDetail);
    if (path === "/api/v1/workloads/cronjob/default/backup/suspend" && method === "POST") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/pods") return json(route, list([listedPod]));
    if (path === "/api/v1/pods/default/web-abcdef-12345/ephemeral-containers" && method === "POST") {
      options.ephemeralRequests?.push(request.postDataJSON() as { image: string; targetContainerName?: string; shell?: string });
      return json(route, { name: "debug-e2e01", status: "created" }, 201);
    }
    if (path === "/api/v1/pods/default/web-abcdef-12345") return json(route, podDetail);
    if (path === "/api/v1/pods/default/web-abcdef-12345/logs") {
      return route.fulfill({
        status: 200,
        contentType: "application/x-ndjson; charset=utf-8",
        body: logFrames(
          "hello from dosh log stream",
          "request served by web-abcdef-12345",
          '<img src=x onerror="window.__doshXss=true"> DOSH_E2E_HTML_MARKER',
          "\u001b[31mANSI_RED\u001b[0m",
          "\u001b]8;;javascript:alert(1)\u0007ANSI_LINK\u001b]8;;\u0007",
        ),
      });
    }
    if (path === "/api/v1/services/default/web" && method === "GET") return json(route, services.find((item) => item.name === "web") ?? serviceDetail);
    if (path === "/api/v1/services" && method === "POST") {
      const body = request.postDataJSON() as { name: string; namespace: string; type: string; headless?: boolean; selector: Record<string, string>; ports: Array<{ name?: string; protocol?: string; port: number; targetPort?: number }> };
      const created: ServiceDetail = {
        ...serviceDetail,
        name: body.name,
        namespace: body.namespace,
        resourceVersion: "50",
        type: body.type,
        headless: Boolean(body.headless),
        clusterIP: body.headless ? "None" : "10.96.0.50",
        selector: body.selector,
        ports: body.ports.map((port) => ({ name: port.name, protocol: port.protocol ?? "TCP", port: port.port, targetPort: String(port.targetPort ?? port.port) })),
        endpointCount: 0,
        endpoints: [],
      };
      services.push(created);
      return json(route, created, 201);
    }
    if (path === "/api/v1/services/default/web" && method === "PUT") {
      const body = request.postDataJSON() as { type: string; selector: Record<string, string>; ports: Array<{ name?: string; protocol?: string; port: number; targetPort?: number }>; resourceVersion?: string };
      const current = services.find((item) => item.name === "web") ?? serviceDetail;
      const updated: ServiceDetail = {
        ...current,
        resourceVersion: "43",
        type: body.type,
        selector: body.selector,
        ports: body.ports.map((port) => ({ name: port.name, protocol: port.protocol ?? "TCP", port: port.port, targetPort: String(port.targetPort ?? port.port) })),
      };
      services = services.map((item) => item.name === "web" ? updated : item);
      return json(route, updated);
    }
    if (path === "/api/v1/services/default/web" && method === "DELETE") {
      services = services.filter((item) => item.name !== "web");
      return json(route, { status: "deleting" }, 202);
    }
    if (path === "/api/v1/ingresses/default/web") return json(route, ingressDetail);
    if (path === "/api/v1/events") return json(route, list([]));
    if (path === "/api/v1/audit-events") {
      const params = new URL(request.url()).searchParams;
      const records = [{
        id: 42,
        timestamp: "2026-11-04T06:00:00Z",
        user: "alice@example.test",
        groups: ["dosh-admins"],
        action: "scale",
        resource: "deployment",
        namespace: "production",
        name: "web-api",
        result: "success",
        requestId: "req-audit-42",
      }].filter((record) => (
        (!params.get("namespace") || record.namespace === params.get("namespace"))
        && (!params.get("action") || record.action === params.get("action"))
        && (!params.get("result") || record.result === params.get("result"))
        && (!params.get("q") || JSON.stringify(record).includes(params.get("q") ?? ""))
      ));
      return json(route, list(records));
    }
    if (path === "/api/v1/yaml") {
      const params = new URL(request.url()).searchParams;
      if (params.get("type") === "secret") return json(route, {
        kind: "Secret",
        namespace: "default",
        name: "app-secret",
        yaml: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app-secret\n  namespace: default\ndata:\n  token: dG9rZW49YWJjCg==\n",
      });
      return json(route, {
        kind: "Pod",
        namespace: "default",
        name: "web-abcdef-12345",
        yaml: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-abcdef-12345\n",
      });
    }
    if (path === "/api/v1/yaml/apply" && method === "POST") {
      const body = request.postDataJSON() as { yaml: string };
      const kind = body.yaml.match(/^kind:\s*(\S+)/m)?.[1] ?? "Deployment";
      const name = body.yaml.match(/^  name:\s*(\S+)/m)?.[1] ?? "web";
      return json(route, { kind, namespace: "default", name, yaml: body.yaml });
    }
    if (path === "/api/v1/storageclasses") {
      const q = new URL(request.url()).searchParams.get("q") ?? "";
      return json(route, list([
        { name: "data-standard", provisioner: "csi.example.test", reclaimPolicy: "Delete", volumeBinding: "Immediate", default: true, createdAt: "2026-11-02T00:00:00Z" },
        { name: "logs-fast", provisioner: "csi.example.test", reclaimPolicy: "Retain", volumeBinding: "WaitForFirstConsumer", default: false, createdAt: "2026-11-02T00:00:00Z" },
      ].filter((item) => item.name.includes(q))));
    }
    if (path === "/api/v1/storageclasses/data-standard") return json(route, storageClassDetail);
    if (path === "/api/v1/persistentvolumes") return json(route, list([]));
    if (path === "/api/v1/persistentvolumes/pv-data") return json(route, persistentVolumeDetail);
    if (path === "/api/v1/persistentvolumeclaims") return json(route, list([]));
    if (path === "/api/v1/persistentvolumeclaims/production/data-claim") return json(route, persistentVolumeClaimDetail);
    if (path === "/api/v1/services" && method === "GET") return json(route, list(services));
    if (path === "/api/v1/ingresses") return json(route, list([{
      name: "web", namespace: "default", class: "nginx", createdAt: "2026-11-02T00:00:00Z",
      paths: [{ host: "web.example.test", path: "/", pathType: "Prefix", service: "web", port: "80", targetStatus: "Ready" }], tlsHosts: ["web.example.test"],
    }]));
    if (path === "/api/v1/ingressclasses") {
      const q = new URL(request.url()).searchParams.get("q") ?? "";
      const items = options.noIngressController ? [] : [{
        name: "nginx", controller: "k8s.io/ingress-nginx", default: true, createdAt: "2026-11-02T00:00:00Z",
      }].filter((item) => item.name.includes(q) || item.controller.includes(q));
      return json(route, list(items));
    }
    if (path === "/api/v1/configmaps") return json(route, list([{
      name: "app-config", namespace: "default", keys: ["mode"], dataBytes: 12, createdAt: "2026-11-02T00:00:00Z",
    }]));
    if (path === "/api/v1/secrets") return json(route, list([{
      name: "app-secret", namespace: "default", type: "Opaque", keys: [{ key: "token", size: 16 }], createdAt: "2026-11-02T00:00:00Z",
    }]));
    if (path === "/api/v1/secrets/default/app-secret/reveal" && method === "POST") return json(route, {
      name: "app-secret", namespace: "default", type: "Opaque",
      keys: [{ key: "token", size: 10 }, { key: "blob", size: 3 }], createdAt: "2026-11-02T00:00:00Z",
      data: { token: "dG9rZW49YWJjCg==", blob: "AP8K" },
    });
    if (path === "/api/v1/helm/releases" && method === "GET") return json(route, { items: [helmReleaseSummary], warnings: [] });
    if (path === "/api/v1/helm/releases/default/web/diff" && method === "GET") return json(route, helmReleaseDiff);
    if (path === "/api/v1/helm/releases/default/web/upgrade/preview" && method === "POST") return json(route, helmUpgradePreview);
    if (path === "/api/v1/helm/releases/default/web/upgrade" && method === "POST") return json(route, helmUpgradeResult, 202);
    if (path === "/api/v1/helm/releases/default/web/rollback" && method === "POST") return json(route, helmRollbackResult, 202);
    if (path === "/api/v1/helm/releases/default/web" && method === "GET") return json(route, helmReleaseDetail);
    if (/^\/api\/v1\/(?:pods|services)\/[^/]+\/[^/]+\/tunnels$/.test(path) && method === "POST") {
      const body = request.postDataJSON() as { port?: number };
      const pod = path.startsWith("/api/v1/pods/") ? decodeURIComponent(path.split("/")[5]) : "web-abcdef-12345";
      tunnelSession = {
        id: "e2e-tunnel", namespace: "default", pod, port: body.port ?? 80,
        path: "/api/v1/tunnels/e2e-tunnel/", expiresAt: "2099-01-01T00:00:00Z",
        ...(path.startsWith("/api/v1/services/") ? { service: "web" } : {}),
      };
      return json(route, tunnelSession, 201);
    }
    if (path === "/api/v1/tunnels/e2e-tunnel" && method === "DELETE") {
      tunnelSession = null;
      return route.fulfill({ status: 204, body: "" });
    }
    if (path.endsWith("/relations") && method === "GET") return json(route, resourceRelations);
    if (path === "/api/v1/certificate") return json(route, serverCertificate);
    if (path === "/api/v1/certificate/reload" && method === "POST") return json(route, serverCertificate);
    return json(route, { code: "not_found", message: `unmocked API route: ${method} ${path}` }, 404);
  });
}

async function waitForVisualStability(page: Page) {
  await page.evaluate(async () => {
    await document.fonts?.ready;
    const finiteAnimations = document.getAnimations().filter((animation) => {
      const endTime = animation.effect?.getComputedTiming().endTime;
      return typeof endTime === "number" && Number.isFinite(endTime);
    });
    await Promise.all(finiteAnimations.map((animation) => animation.finished.catch(() => undefined)));
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
  });
}

async function expectNoPageOverflow(page: Page, context: string) {
  const overflow = await page.evaluate(() => {
    const viewportWidth = window.visualViewport?.width ?? window.innerWidth;
    return {
      documentWidth: document.documentElement.scrollWidth,
      bodyWidth: document.body.scrollWidth,
      viewportWidth,
      innerWidth: window.innerWidth,
      visualViewportWidth: window.visualViewport?.width,
      screenWidth: window.screen.width,
      offenders: Array.from(document.querySelectorAll<HTMLElement>("body *"))
        .map((element) => {
          const rect = element.getBoundingClientRect();
          return { tag: element.tagName, className: String(element.className), text: element.textContent?.trim().slice(0, 80), left: rect.left, right: rect.right, width: rect.width };
        })
        .filter((item) => item.width > 0 && item.right > viewportWidth + 1)
        .sort((left, right) => right.right - left.right)
        .slice(0, 12),
    };
  });
  expect(
    Math.max(overflow.documentWidth, overflow.bodyWidth),
    `${context} overflows horizontally: ${JSON.stringify(overflow)}`,
  ).toBeLessThanOrEqual(overflow.viewportWidth + 1);
}

async function expectElementInsideViewport(page: Page, locator: ReturnType<Page["locator"]>, context: string) {
  await waitForVisualStability(page);
  const box = await locator.boundingBox();
  expect(box, `${context} must have a layout box`).not.toBeNull();
  const viewport = page.viewportSize();
  expect(viewport, `${context} requires a configured viewport`).not.toBeNull();
  expect(box!.x, `${context} must not start left of the viewport`).toBeGreaterThanOrEqual(-1);
  expect(box!.y, `${context} must not start above the viewport`).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, `${context} must not extend right of the viewport`).toBeLessThanOrEqual(viewport!.width + 1);
  expect(box!.y + box!.height, `${context} must not extend below the viewport`).toBeLessThanOrEqual(viewport!.height + 1);
}

async function expectNoSeriousAccessibilityViolations(page: Page, context: string) {
  await waitForVisualStability(page);
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  const blocking = results.violations
    .filter((violation) => violation.impact === "serious" || violation.impact === "critical")
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      targets: violation.nodes.map((node) => node.target),
    }));
  expect(blocking, `${context} has accessibility violations:\n${JSON.stringify(blocking, null, 2)}`).toEqual([]);
}

async function expectThemeApplied(page: Page, theme: "light" | "dark") {
  const expectedBackground = theme === "dark" ? "222 28% 9%" : "216 33% 97%";
  await expect
    .poll(async () =>
      page.evaluate(() => ({
        dark: document.documentElement.classList.contains("dark"),
        background: getComputedStyle(document.documentElement).getPropertyValue("--background").trim(),
        colorScheme: document.documentElement.style.colorScheme,
      })),
    )
    .toEqual({ dark: theme === "dark", background: expectedBackground, colorScheme: theme });
}

type FocusDescriptor = {
  tag: string;
  role: string;
  text: string;
  id: string;
  placeholder: string;
};

async function describeActiveElement(page: Page): Promise<FocusDescriptor> {
  return page.evaluate(() => {
    const active = document.activeElement as HTMLElement | null;
    return {
      tag: active?.tagName ?? "",
      role: active?.getAttribute("role") ?? "",
      text: (active?.innerText ?? "").replace(/\s+/g, " ").trim(),
      id: active?.id ?? "",
      placeholder: active?.getAttribute("placeholder") ?? "",
    };
  });
}

async function pressTabUntil(
  page: Page,
  description: string,
  predicate: (focus: FocusDescriptor) => boolean,
  maxTabs = 200,
) {
  for (let index = 0; index <= maxTabs; index += 1) {
    if (predicate(await describeActiveElement(page))) return;
    await page.keyboard.press("Tab");
  }
  throw new Error(`Unable to focus ${description} with the keyboard after ${maxTabs} tab presses`);
}

async function expectVisibleFocusRing(page: Page) {
  const styles = await page.evaluate(() => {
    const active = document.activeElement as HTMLElement | null;
    if (!active) return null;
    const style = getComputedStyle(active);
    return { boxShadow: style.boxShadow, outlineStyle: style.outlineStyle, outlineWidth: style.outlineWidth };
  });
  expect(styles, "keyboard focus must land on an interactive element").not.toBeNull();
  expect(
    styles!.boxShadow !== "none" || (styles!.outlineStyle !== "none" && parseFloat(styles!.outlineWidth) > 0),
    "keyboard focus must have a visible focus ring",
  ).toBe(true);
}

test("登录后进入集群总览", async ({ page }) => {
  await installApiMocks(page, { authenticated: false });
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
  await page.getByLabel("用户名").fill("admin");
  await page.getByLabel("密码").fill("e2e-password");
  await page.getByRole("button", { name: "登录" }).click();

  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByText("8/8", { exact: true })).toBeVisible();
  await expect(page.getByText("40/42", { exact: true })).toBeVisible();
  await expect(page.getByText("Available", { exact: true })).toBeVisible();
  await expect(page.getByText("2.8 cores 实际", { exact: false })).toBeVisible();
});

test("Kubernetes Token 登录后进入总览且不持久化 Token", async ({ page }) => {
  await installApiMocks(page, { authenticated: false, authMode: "token" });
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Kubernetes Token 登录" })).toBeVisible();
  await page.getByLabel("Bearer Token").fill("e2e-kubernetes-token");
  await page.getByRole("button", { name: "登录" }).click();

  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  expect(await page.evaluate(() => ({
    local: Object.values(localStorage).join(""),
    session: Object.values(sessionStorage).join(""),
  }))).toEqual({ local: "", session: "" });
});

test("审计日志保留过滤条件并展示请求详情", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/audit?namespace=production&action=scale&result=success&q=web");

  await expect(page.getByRole("heading", { name: "审计日志" })).toBeVisible();
  await expect(page.getByText("仅展示当前实例最近 1000 条记录", { exact: false })).toBeVisible();
  await expect(page.getByRole("cell", { name: "alice@example.test" })).toBeVisible();
  await expect(page.getByRole("cell", { name: /web-api/ })).toBeVisible();

  await page.getByRole("cell", { name: "alice@example.test" }).click();
  const dialog = page.getByRole("dialog", { name: "审计记录 #42" });
  await expect(dialog).toContainText("req-audit-42");
  await expect(dialog).toContainText("dosh-admins");
});

test("节点列表可进入节点详情", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/nodes");
  await page.getByRole("link", { name: "worker-0001" }).click();

  await expect(page).toHaveURL(/\/nodes\/worker-0001$/);
  await expect(page.getByRole("heading", { name: "worker-0001" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "概览", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: /实例/ })).toBeVisible();
  await expect(page.getByRole("tab", { name: "事件", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "日志", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "YAML", exact: true })).toBeVisible();
  await expect(page.getByText("192.168.122.200").first()).toBeVisible();
  await expect(page.getByText("资源使用", { exact: true })).toBeVisible();
  await expect(page.getByText("CPU 实际使用", { exact: true })).toBeVisible();
  await expect(page.getByText(/350m \/ 2 cores/)).toBeVisible();

  await page.getByRole("tab", { name: /实例/ }).click();
  await expect(page.getByRole("link", { name: "web-abcdef-12345" })).toBeVisible();

  await page.getByRole("tab", { name: "日志", exact: true }).click();
  await expect(page.getByLabel("选择实例查看日志")).toBeVisible();
  await expect(page.getByText("hello from dosh log stream", { exact: false })).toBeVisible();

  await page.getByRole("tab", { name: "事件", exact: true }).click();
  await expect(page).toHaveURL(/\?tab=events$/);
  await expect(page.getByText("该资源暂无事件")).toBeVisible();

  const yamlRequest = page.waitForRequest((request) => new URL(request.url()).pathname === "/api/v1/yaml");
  await page.getByRole("tab", { name: "YAML", exact: true }).click();
  await yamlRequest;
  await expect(page).toHaveURL(/\?tab=yaml$/);
  await expect(page.getByText("apiVersion: v1", { exact: false })).toBeVisible();
});

test("资源搜索具有可访问名称且可一键清空", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/nodes?q=worker");

  const search = page.getByRole("textbox", { name: "搜索节点名称或 IP" });
  await expect(search).toHaveValue("worker");
  await page.getByRole("button", { name: "清空搜索" }).click();

  await expect(search).toHaveValue("");
  await expect(page).not.toHaveURL(/[?&]q=/);
});

test("集群配置展示网络规划与节点 Pod 网段", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/cluster/config");

  await expect(page.getByRole("heading", { name: "集群配置" })).toBeVisible();
  await expect(page.getByText("10.244.0.0/16", { exact: true })).toBeVisible();
  await expect(page.getByText("10.96.0.0/12", { exact: true })).toBeVisible();
  await expect(page.getByText("192.168.122.50:16443", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "worker-0001" })).toHaveAttribute("href", "/nodes/worker-0001");
  await expectNoPageOverflow(page, "cluster-config");
});

test("YAML 创建支持 Dry Run 并应用资源", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/yaml/create");

  await expect(page.getByRole("heading", { name: "YAML 创建" })).toBeVisible();
  const dryRunRequest = page.waitForRequest((request) => {
    if (new URL(request.url()).pathname !== "/api/v1/yaml/apply") return false;
    return (request.postDataJSON() as { dryRun?: boolean }).dryRun === true;
  });
  await page.getByRole("button", { name: "Dry Run" }).click();
  const dryRunPayload = (await dryRunRequest).postDataJSON() as { yaml: string; dryRun: boolean };
  expect(dryRunPayload.dryRun).toBe(true);
  expect(dryRunPayload.yaml).toContain("kind: Deployment");
  await expect(page.getByRole("status", { name: "Dry Run 校验结果" })).toBeVisible();

  const applyRequest = page.waitForRequest((request) => {
    if (new URL(request.url()).pathname !== "/api/v1/yaml/apply") return false;
    return (request.postDataJSON() as { dryRun?: boolean }).dryRun === false;
  });
  await page.getByRole("button", { name: "创建 / 应用" }).click();
  await applyRequest;
  await expect(page.getByRole("status", { name: "YAML 应用结果" })).toBeVisible();
  await expect(page.getByRole("link", { name: "查看工作负载" })).toHaveAttribute("href", "/workloads/deployment/default/web");
});

test("YAML 创建支持多个文档顺序 Dry Run 与应用", async ({ page }) => {
  await installApiMocks(page);
  const requests: Array<{ yaml: string; dryRun: boolean }> = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname !== "/api/v1/yaml/apply") return;
    requests.push(request.postDataJSON() as { yaml: string; dryRun: boolean });
  });
  await page.goto("/yaml/create");

  await expect(page.getByRole("heading", { name: "YAML 创建" })).toBeVisible();
  await page.getByRole("button", { name: "组合示例" }).click();
  await expect(page.getByText("3 个有效文档", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Dry Run" }).click();
  await expect.poll(() => requests.filter((request) => request.dryRun).length).toBe(3);
  await expect(page.getByRole("status", { name: "Dry Run 校验结果" })).toContainText("3/3 成功");

  await page.getByRole("button", { name: "创建 / 应用 3 个资源" }).click();
  await expect.poll(() => requests.filter((request) => !request.dryRun).length).toBe(3);
  await expect(page.getByRole("status", { name: "YAML 应用结果" })).toContainText("3/3 成功");
  await page.getByRole("button", { name: "返回 YAML" }).first().click();
  await expect(page.getByRole("dialog", { name: /ConfigMap default\/web-config/ })).toBeVisible();
});

test("节点详情确认后停止并恢复调度", async ({ page }) => {
  const schedulingRequests: NodeSchedulingRequest[] = [];
  await installApiMocks(page, { nodeSchedulingRequests: schedulingRequests });
  await page.goto("/nodes/worker-0001");

  await page.getByRole("button", { name: "停止调度" }).click();
  await expect(page.getByRole("alertdialog")).toContainText("停止节点调度？");
  await expect(page.getByRole("alertdialog")).toContainText("已有 Pod 不会被驱逐");
  expect(schedulingRequests).toHaveLength(0);

  await page.getByRole("button", { name: "确认停止调度" }).click();
  await expect(page.getByRole("button", { name: "恢复调度" })).toBeVisible();
  await expect(page.getByText("SchedulingDisabled")).toBeVisible();

  await page.getByRole("button", { name: "恢复调度" }).click();
  await expect(page.getByRole("alertdialog")).toContainText("恢复节点调度？");
  await page.getByRole("button", { name: "确认恢复" }).click();
  await expect(page.getByRole("button", { name: "停止调度" })).toBeVisible();

  expect(schedulingRequests.map((request) => request.path)).toEqual([
    "/api/v1/nodes/worker-0001/cordon",
    "/api/v1/nodes/worker-0001/uncordon",
  ]);
  for (const request of schedulingRequests) {
    expect(request.method).toBe("POST");
    expect(request.csrf).toBe(csrfToken);
  }
});

test("节点排空先展示 PDB 计划并在确认后展示阻塞结果", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/nodes/worker-0001");
  await page.getByRole("button", { name: "排空节点" }).click();
  const dialog = page.getByRole("dialog", { name: "排空节点 worker-0001" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("将驱逐 1")).toBeVisible();
  await expect(dialog.getByText("关联 PDB：web-pdb")).toBeVisible();
  await expect(dialog.getByText("存在 DaemonSet Pod，需要显式忽略 DaemonSet")).toBeVisible();
  await expectElementInsideViewport(page, dialog, "node drain dialog");
  await expectNoPageOverflow(page, "node drain dialog");

  await dialog.getByLabel("输入节点名称确认风险操作").fill("worker-0001");
  const drainRequest = page.waitForRequest((request) => request.url().endsWith("/api/v1/nodes/worker-0001/drain") && request.method() === "POST");
  await dialog.getByRole("button", { name: "确认排空" }).click();
  const request = await drainRequest;
  expect(request.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(request.postDataJSON()).toMatchObject({ ignoreDaemonSets: false, force: false, timeoutSeconds: 90, confirmation: "worker-0001" });
  await expect(dialog.getByText("排空未完全完成")).toBeVisible();
  await expect(dialog.getByText(/Pod 已被 PodDisruptionBudget 阻止/)).toBeVisible();
  await expectElementInsideViewport(page, dialog, "node drain result dialog");
});

test("Helm Release 列表、详情和历史版本可读且敏感值已遮蔽", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/helm/releases?namespace=default");
  await expect(page.getByRole("heading", { name: "Helm Releases" })).toBeVisible();
  await expect(page.getByRole("link", { name: "web", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "web", exact: true }).click();
  await expect(page).toHaveURL(/\/helm\/releases\/default\/web/);
  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();
  await expect(page.getByText("revision #2")).toBeVisible();
  await expect(page.getByText("部署说明")).toBeVisible();
  await page.getByRole("tab", { name: /Values/ }).click();
  await expect(page.getByText("password: <redacted>")).toBeVisible();
  await page.getByRole("tab", { name: /Manifest/ }).click();
  await expect(page.getByText("token: <redacted>")).toBeVisible();
  await page.getByRole("tab", { name: /Diff/ }).click();
  await expect(page.getByRole("heading", { name: "版本对比" })).toBeVisible();
  await page.getByRole("button", { name: "生成 Diff" }).click();
  await expect(page.getByText("Values Diff")).toBeVisible();
  await expect(page.getByText("Manifest Diff")).toBeVisible();
  await expect(page.getByText("+  replicas: 2", { exact: false })).toBeVisible();
  await page.getByRole("tab", { name: /历史/ }).click();
  await expect(page.getByRole("row").filter({ hasText: "#1" })).toBeVisible();
  await page.getByRole("button", { name: "回滚到此版本" }).click();
  const rollbackDialog = page.getByRole("dialog", { name: "受控回滚 Helm Release" });
  await expect(rollbackDialog).toBeVisible();
  await rollbackDialog.getByLabel(/输入 Release 名称/).fill("web");
  const rollbackRequest = page.waitForRequest((request) => request.url().endsWith("/api/v1/helm/releases/default/web/rollback") && request.method() === "POST");
  await rollbackDialog.getByRole("button", { name: "确认回滚" }).click();
  expect((await rollbackRequest).postDataJSON()).toEqual({ targetRevision: 1, confirmation: "web" });
  await expect(page.getByText(/已创建 Helm revision #3/)).toBeVisible();
  await expectNoPageOverflow(page, "helm release detail");
});

test("权限检查联动选择资源、子资源和对象，并保留手动输入回退", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/access");
  const resource = page.getByRole("combobox", { name: "Resource", exact: true });
  const group = page.getByRole("combobox", { name: "API Group", exact: true });
  const subresource = page.getByRole("combobox", { name: "Subresource", exact: true });
  const namespace = page.getByRole("combobox", { name: "Namespace", exact: true });
  const name = page.getByRole("combobox", { name: "资源名称", exact: true });

  await page.getByRole("button", { name: "查看 Pod", exact: true }).click();
  await expect(resource).toContainText("Pod · pods · core/v1");
  await expect(group).toContainText("core（留空）");
  await expect(namespace).toContainText("default");

  await resource.click();
  const resourceSearch = page.getByRole("searchbox", { name: "搜索 Resource", exact: true });
  await expect(resourceSearch).toBeVisible();
  await expect(page.getByRole("option", { name: "Pod · pods · core/v1", exact: true })).toBeVisible();
  await expect(page.getByRole("option").last()).toHaveText("手动输入");
  await resourceSearch.fill("deploy");
  await expect(page.getByRole("option", { name: "Deployment · deployments · apps/v1", exact: true })).toBeVisible();
  await expect(page.getByRole("option", { name: "Pod · pods · core/v1", exact: true })).toHaveCount(0);
  await resourceSearch.fill("does-not-exist");
  await expect(page.getByText("没有匹配项", { exact: true })).toBeVisible();
  await expect(page.getByRole("option", { name: "手动输入", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");

  await subresource.click();
  await expect(page.getByRole("option", { name: "log（日志）", exact: true })).toBeVisible();
  await expect(page.getByRole("option", { name: "exec（终端）", exact: true })).toBeVisible();
  await expect(page.getByRole("option").last()).toHaveText("手动输入");
  await page.getByRole("option", { name: "log（日志）", exact: true }).click();
  await expect(subresource).toContainText("log（日志）");

  await expect(name).toBeEnabled();
  await name.click();
  const nameSearch = page.getByRole("searchbox", { name: "搜索 资源名称", exact: true });
  await nameSearch.fill("web");
  await expect(page.getByRole("option", { name: "web-abcdef-12345", exact: true })).toBeVisible();
  await expect(page.getByRole("option").last()).toHaveText("手动输入");
  await page.getByRole("option", { name: "web-abcdef-12345", exact: true }).click();

  await namespace.click();
  await expect(page.getByRole("option", { name: "集群级资源（留空）", exact: true })).toBeVisible();
  await expect(page.getByRole("option", { name: "production", exact: true })).toBeVisible();
  await expect(page.getByRole("option").last()).toHaveText("手动输入");
  await page.getByRole("option", { name: "default", exact: true }).click();

  await page.getByRole("button", { name: "检查权限", exact: true }).click();
  await expect(page.getByText("允许执行")).toBeVisible();
  await expect(page.getByText(/kubectl auth can-i list pods --namespace default/)).toBeVisible();
  await expect(page.getByText("list pods/log · default · web-abcdef-12345")).toBeVisible();

  await resource.click();
  await page.getByRole("searchbox", { name: "搜索 Resource", exact: true }).fill("secret");
  await expect(page.getByRole("option", { name: "Pod · pods · core/v1", exact: true })).toHaveCount(0);
  await page.getByRole("option", { name: "Secret · secrets · core/v1", exact: true }).click();
  await expect(resource).toContainText("Secret · secrets · core/v1");
  await expect(group).toContainText("core（留空）");
  await expect(subresource).toContainText("无子资源（留空）");
  await page.getByRole("button", { name: "检查权限", exact: true }).click();
  await expect(page.getByText("不允许执行")).toBeVisible();
  await expect(page.getByText("RBAC denied")).toBeVisible();
  await expect(page.getByText("Kubernetes RBAC 未授权当前身份执行该操作，请检查 RoleBinding。")).toBeVisible();

  await subresource.click();
  await page.getByRole("option", { name: "手动输入", exact: true }).click();
  const subresourceInput = page.getByRole("textbox", { name: "Subresource", exact: true });
  await expect(subresourceInput).toBeVisible();
  await subresourceInput.fill("logs");
  await page.getByRole("button", { name: "返回 Subresource 列表", exact: true }).click();
  await expect(subresource).toBeVisible();

  await resource.click();
  await page.getByRole("option", { name: "手动输入", exact: true }).click();
  const resourceInput = page.getByRole("textbox", { name: "Resource", exact: true });
  await expect(resourceInput).toBeVisible();
  await resourceInput.fill("customresources.example.io");
  await page.getByRole("button", { name: "返回 Resource 列表", exact: true }).click();
  await expect(resource).toBeVisible();
  await expectNoPageOverflow(page, "access review");
});

test("权限检查联动选择器在 320px 与 390px 视口内完整可用", async ({ page }) => {
  test.setTimeout(90_000);
  await installApiMocks(page);

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/access");
    await expect(page.getByRole("heading", { name: "权限检查" })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px access review`);

    for (const label of ["Resource", "API Group", "Subresource", "Namespace", "资源名称"]) {
      const trigger = page.getByRole("combobox", { name: label, exact: true });
      await expect(trigger).toBeEnabled();
      await trigger.click();
      const listbox = page.getByRole("listbox");
      await expect(listbox).toBeVisible();
      await expectElementInsideViewport(page, listbox, `${viewport.width}px ${label} selector`);
      await expectNoPageOverflow(page, `${viewport.width}px ${label} selector`);
      await page.keyboard.press("Escape");
    }

    await page.getByRole("combobox", { name: "Resource", exact: true }).click();
    await page.getByRole("searchbox", { name: "搜索 Resource", exact: true }).fill("pod");
    await expect(page.getByRole("option", { name: "Pod · pods · core/v1", exact: true })).toBeVisible();
    await expect(page.getByRole("option", { name: "Deployment · deployments · apps/v1", exact: true })).toHaveCount(0);
    await expect(page.getByRole("option").last()).toHaveText("手动输入");
    await expectNoPageOverflow(page, `${viewport.width}px access resource menu`);
    await page.screenshot({ path: `test-results/mobile-interactions/access-review-${viewport.width}.png`, fullPage: true });
  }
});

test("Discovery 无可用资源时权限检查自动退回手动输入", async ({ page }) => {
  await installApiMocks(page, { emptyDiscovery: true });
  await page.goto("/access");
  await expect(page.getByRole("textbox", { name: "Resource", exact: true })).toHaveValue("pods");
  await expect(page.getByRole("textbox", { name: "API Group", exact: true })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Subresource", exact: true })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "资源名称", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Namespace", exact: true })).toContainText("default");
  await expectNoPageOverflow(page, "empty discovery access review");
});

test("通用资源收藏、视图、列偏好和本地模板可持久化", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/resources/example.io/v1/widgets?namespace=default");
  await page.getByRole("button", { name: "收藏类型" }).click();
  await expect(page.getByRole("button", { name: "已收藏" })).toBeVisible();
  await page.getByRole("button", { name: "视图" }).click();
  await page.getByRole("menuitem", { name: "保存当前视图" }).click();
  await expect(page.getByText("已保存视图“default · 默认视图”")).toBeVisible();
  await page.getByRole("button", { name: "视图" }).click();
  await expect(page.getByRole("menuitem", { name: "default · 默认视图", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "列", exact: true }).click();
  await page.getByRole("menuitem", { name: "Labels" }).click();
  await expect(page.getByRole("columnheader", { name: "Labels" })).toHaveCount(0);
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "创建 YAML" }).click();
  const dialog = page.getByRole("dialog", { name: "创建 Widget" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("tab", { name: "表单" }).click();
  await dialog.getByLabel("搜索 Schema 字段").fill("尺寸");
  await expect(dialog.getByText("匹配 1 个顶层字段；嵌套字段会按名称和说明过滤。")).toBeVisible();
  await dialog.getByLabel("搜索 Schema 字段").fill("");
  await dialog.getByRole("button", { name: "本地模板" }).click();
  await page.getByRole("menuitem", { name: "从当前 YAML 另存模板" }).click();
  await expect(page.getByText(/已保存本地模板/)).toBeVisible();
  await dialog.getByRole("button", { name: "全屏编辑" }).click();
  await expect(dialog.getByRole("button", { name: "退出全屏" })).toBeVisible();
  await expectElementInsideViewport(page, dialog, "fullscreen generic editor");
  await expectNoPageOverflow(page, "fullscreen generic editor");
  await dialog.getByRole("button", { name: "本地模板" }).click();
  await expect(page.getByRole("menuitem", { name: /example · Widget/ })).toBeVisible();
});

test("Pod 日志支持时间范围和 Init Container，终端可通过同源 WebSocket 交互", async ({ page }) => {
  await installApiMocks(page);
  let terminalInput = "";
  await page.routeWebSocket("**/api/v1/pods/**/terminal?**", (ws) => {
    ws.onMessage((message) => {
      const frame = JSON.parse(String(message)) as { type: string; data?: string };
      if (frame.type === "input") {
        terminalInput += atob(frame.data ?? "");
        ws.send(JSON.stringify({ type: "output", data: btoa("hello from terminal\r\n") }));
      }
    });
    setTimeout(() => ws.send(JSON.stringify({ type: "ready" })), 20);
  });

  await page.goto("/pods/default/web-abcdef-12345?tab=logs");
  const timeRange = page.getByLabel("日志时间范围");
  await expect(timeRange).toBeVisible();
  const sinceRequest = page.waitForRequest((request) => request.url().includes("/logs") && request.url().includes("sinceSeconds=900"));
  await timeRange.click();
  await page.getByRole("option", { name: "最近 15 分钟" }).click();
  await sinceRequest;
  await expect(page.getByText(/时间范围 15 分钟/)).toBeVisible();

  await page.goto("/pods");
  const podRow = page.getByRole("row").filter({ hasText: "web-abcdef-12345" });
  await podRow.getByRole("button", { name: /操作 Pod/ }).click();
  await page.getByRole("menuitem", { name: "进入终端" }).click();
  await expect(page).toHaveURL(/tab=terminal/);
  await expect(page.getByRole("tab", { name: /终端/ })).toHaveAttribute("aria-selected", "true");

  const containerSelect = page.getByLabel("终端容器");
  await expect(containerSelect).toContainText("web · Running");
  await containerSelect.click();
  await expect(page.getByRole("option", { name: /migrate · Init\/Sidecar · Terminated/ })).toBeVisible();
  await page.getByRole("option", { name: /migrate · Init\/Sidecar · Terminated/ }).click();
  await expect(page.getByRole("alert").filter({ hasText: "容器当前状态为 Terminated" })).toBeVisible();
  await expect(page.getByRole("button", { name: "连接终端" })).toBeDisabled();
  await containerSelect.click();
  await page.getByRole("option", { name: /web · Running/ }).click();
  await expect(page.getByRole("button", { name: "连接终端" })).toBeEnabled();

  await page.getByRole("button", { name: "连接终端" }).click();
  await expect(page.getByRole("status").filter({ hasText: "已连接" })).toBeVisible();
  await expect(page.getByRole("application", { name: "Pod 容器终端" })).toBeVisible();
  await page.getByRole("application", { name: "Pod 容器终端" }).click();
  await page.keyboard.type("echo hello");
  await page.keyboard.press("Enter");
  await expect.poll(() => terminalInput).toContain("echo hello\r");
  await expect(page.locator(".xterm-accessibility")).toContainText("hello from terminal");
  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoPageOverflow(page, "pod terminal");
});

test("一键诊断入口覆盖 Pod、工作负载、Service、Ingress、PVC 与 Node", async ({ page }) => {
  await installApiMocks(page);
  const targets = [
    "/pods/default/web-abcdef-12345",
    "/workloads/deployment/default/web",
    "/network/services/default/web",
    "/network/ingresses/default/web",
    "/storage/claims/production/data-claim",
    "/nodes/worker-0001",
  ];
  for (const target of targets) {
    await page.goto(target);
    await page.getByRole("button", { name: "一键诊断" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("容器处于 CrashLoopBackOff")).toBeVisible();
    await expectNoPageOverflow(page, `diagnosis entry ${target}`);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
  }
});

test("一键诊断展示检查、证据与建议，并适配桌面及移动端", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }, { width: 320, height: 720 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/pods/default/web-abcdef-12345");
    await page.getByRole("button", { name: "一键诊断" }).click();
    const dialog = page.getByRole("dialog", { name: "诊断 pod web-abcdef-12345" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("发现 1 个需要立即处理的问题")).toBeVisible();
    await expect(dialog.getByText("容器处于 CrashLoopBackOff")).toBeVisible();
    await expect(dialog.getByText("上次终止")).toBeVisible();
    await expect(dialog.getByText("OOMKilled（退出码 137）")).toBeVisible();
    await expect(dialog.getByText("kubectl describe pod web-abcdef-12345 -n default")).toBeVisible();
    await expect(dialog.getByRole("button", { name: /复制排查命令/ })).toBeVisible();
    await expect(dialog.getByRole("link", { name: /worker-0001/ })).toBeVisible();
    await expect(dialog.getByText("当前身份缺少部分日志权限，日志检查已跳过")).toBeVisible();
    await expectElementInsideViewport(page, dialog, `${viewport.width}px diagnosis dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px diagnosis dialog`);
    await page.screenshot({ path: `test-results/mobile-interactions/diagnosis-${viewport.width}.png`, fullPage: true });
    await page.keyboard.press("Escape");
  }
});

test("Pod 终端在无 pods/exec 权限时保留入口并解释原因", async ({ page }) => {
  await installApiMocks(page, { podExec: false });
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto("/pods/default/web-abcdef-12345?tab=terminal");
  await expect(page.getByRole("tab", { name: /终端/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("heading", { name: "当前身份不能进入容器终端" })).toBeVisible();
  await expect(page.getByText(/create pods\/exec/).first()).toBeVisible();
  await expect(page.getByRole("link", { name: "前往权限检查" })).toBeVisible();
  await expect(page.getByRole("application", { name: "Pod 容器终端" })).toHaveCount(0);
  await expectNoPageOverflow(page, "pod terminal permission state");
});

test("Pod 终端可创建受审计的 Ephemeral Container 调试容器", async ({ page }) => {
  const ephemeralRequests: Array<{ image: string; targetContainerName?: string; shell?: string }> = [];
  await installApiMocks(page, { ephemeralRequests });
  await page.goto("/pods/default/web-abcdef-12345?tab=terminal");
  await page.getByRole("button", { name: "调试容器", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "创建 Ephemeral Container" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("调试目标容器")).toContainText("web");
  await expect(dialog.getByText(/update pods\/ephemeralcontainers/)).toBeVisible();
  await dialog.getByRole("button", { name: "创建调试容器" }).click();

  await expect.poll(() => ephemeralRequests).toEqual([{ image: "busybox:1.36", targetContainerName: "web", shell: "sh" }]);
  await expect(dialog).toBeHidden();
  await expect(page.getByText(/调试容器 debug-e2e01 已创建/)).toBeVisible();
  await expectNoPageOverflow(page, "ephemeral container desktop");
});

test("Ephemeral Container 弹窗在 320px 与 390px 视口内完整可用", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/pods/default/web-abcdef-12345?tab=terminal");

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.getByRole("button", { name: "调试容器", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "创建 Ephemeral Container" });
    await expect(dialog).toBeVisible();
    await expectElementInsideViewport(page, dialog, `${viewport.width}px ephemeral dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px ephemeral dialog`);

    await dialog.getByLabel("调试目标容器").click();
    await expect(page.getByRole("option", { name: /web · Running/ })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px ephemeral target menu`);
    await page.keyboard.press("Escape");
    await expectNoSeriousAccessibilityViolations(page, `${viewport.width}px ephemeral dialog`);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  }
});

test("发布历史回滚确认展示 Pod 模板 Diff", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web?tab=history");
  await expect(page.getByText("Pod 模板变更 Preview")).toHaveCount(0);
  const historyRow = page.getByRole("row").filter({ hasText: "#1" });
  await historyRow.getByRole("button", { name: "回滚" }).click();
  const dialog = page.getByRole("alertdialog", { name: "回滚到 revision 1？" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Pod 模板变更 Preview")).toBeVisible();
  await expect(dialog.getByLabel("回滚变更预览")).toContainText("nginx:1.25");
  await expectElementInsideViewport(page, dialog, "rollback preview dialog");
  await expectNoPageOverflow(page, "rollback preview dialog");
});

test("节点标签和污点支持新增、编辑与删除", async ({ page }) => {
  const metadataRequests: NodeMetadataRequest[] = [];
  await installApiMocks(page, { nodeMetadataRequests: metadataRequests });
  await page.goto("/nodes/worker-0001?tab=labels");

  await expect(page.getByRole("heading", { name: /节点标签/ })).toBeVisible();
  await page.getByRole("button", { name: "添加标签" }).click();
  let dialog = page.getByRole("dialog", { name: "添加节点标签" });
  await dialog.getByLabel("标签键").fill("topology.kubernetes.io/zone");
  await dialog.getByLabel("标签值").fill("az-1");
  await dialog.getByRole("button", { name: "添加标签" }).click();
  await expect(page.getByText("topology.kubernetes.io/zone", { exact: true })).toBeVisible();
  await expect(page.getByText("az-1", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "编辑标签 topology.kubernetes.io/zone" }).click();
  dialog = page.getByRole("dialog", { name: "编辑节点标签" });
  await dialog.getByLabel("标签值").fill("az-2");
  await dialog.getByRole("button", { name: "保存修改" }).click();
  await expect(page.getByText("az-2", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "删除标签 topology.kubernetes.io/zone" }).click();
  await expect(page.getByRole("alertdialog")).toContainText("删除节点标签？");
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByText("topology.kubernetes.io/zone", { exact: true })).not.toBeVisible();

  await page.getByRole("tab", { name: /污点/ }).click();
  await expect(page.getByRole("heading", { name: /节点污点/ })).toBeVisible();
  await page.getByRole("button", { name: "添加污点" }).click();
  dialog = page.getByRole("dialog", { name: "添加节点污点" });
  await dialog.getByLabel("污点键").fill("dedicated");
  await dialog.getByLabel("污点值").fill("gpu");
  await dialog.getByLabel("污点效果").click();
  await page.getByRole("option", { name: "NoExecute" }).click();
  await dialog.getByRole("button", { name: "添加污点" }).click();
  await expect(page.getByText("dedicated=gpu", { exact: true })).toBeVisible();
  await expect(page.getByText("NoExecute", { exact: true })).toBeVisible();
  await expect(page.getByText(/驱逐不 toleration 该污点的已有 Pod/)).toBeVisible();

  await page.getByRole("button", { name: "编辑污点 dedicated NoExecute" }).click();
  dialog = page.getByRole("dialog", { name: "编辑节点污点" });
  await dialog.getByLabel("污点值").fill("gpu-a");
  await dialog.getByRole("button", { name: "保存修改" }).click();
  await expect(page.getByText("dedicated=gpu-a", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "删除污点 dedicated NoExecute" }).click();
  await expect(page.getByRole("alertdialog")).toContainText("删除节点污点？");
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByText("dedicated=gpu-a", { exact: true })).not.toBeVisible();

  expect(metadataRequests.map((request) => request.path)).toEqual([
    "/api/v1/nodes/worker-0001/labels",
    "/api/v1/nodes/worker-0001/labels",
    "/api/v1/nodes/worker-0001/labels",
    "/api/v1/nodes/worker-0001/taints",
    "/api/v1/nodes/worker-0001/taints",
    "/api/v1/nodes/worker-0001/taints",
  ]);
  expect(metadataRequests.every((request) => request.method === "PUT" && request.csrf === csrfToken)).toBe(true);
  expect(metadataRequests.map((request) => request.body.resourceVersion)).toEqual(["1", "2", "3", "4", "5", "6"]);
});

test("顶部栏与侧栏读取真实集群上下文", async ({ page }) => {
  await installApiMocks(page, {
    overview: {
      ...overview,
      cluster: { name: "commercial-e2e", version: "v9.9.9", cni: "TelemetryCNI", status: "Degraded", metricsAvailable: true },
    },
  });
  await page.goto("/nodes");

  const currentCluster = page.getByLabel("当前集群");
  await expect(currentCluster).toContainText("commercial-e2e");
  await expect(currentCluster).toContainText("部分异常");
  const connection = page.getByRole("status", { name: "集群连接状态" });
  await expect(connection).toContainText("集群部分异常");
  await expect(connection).toContainText("Kubernetes v9.9.9 · TelemetryCNI");
  await expect(page.getByText("kubedo-lab")).toHaveCount(0);
});

test("其他功能默认折叠并可展开访问低频导航", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/");

  const other = page.getByRole("button", { name: "其他功能", exact: true });
  await expect(other).toHaveAttribute("aria-expanded", "false");
  await expect(page.getByRole("link", { name: "治理与策略", exact: true })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "权限检查", exact: true })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "镜像仓库", exact: true })).toHaveCount(0);

  await other.click();
  await expect(other).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("link", { name: "镜像仓库", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "治理与策略", exact: true }).click();
  await expect(page).toHaveURL(/\/governance$/);
  await expect(page.getByRole("heading", { name: "治理与策略", exact: true })).toBeVisible();
  await expect(other).toHaveAttribute("aria-expanded", "true");

  await page.goto("/access");
  await expect(page.getByRole("heading", { name: "权限检查", exact: true })).toBeVisible();
  await expect(other).toHaveAttribute("aria-expanded", "true");
  await other.click();
  await expect(other).toHaveAttribute("aria-expanded", "false");
  await expect(page.getByRole("link", { name: "权限检查", exact: true })).toHaveCount(0);
  await expectNoPageOverflow(page, "other navigation");
});

test("总览页面包屑读取真实集群上下文", async ({ page }) => {
  await installApiMocks(page, {
    overview: {
      ...overview,
      cluster: { name: "commercial-overview", version: "v9.9.9", cni: "TelemetryCNI", status: "Healthy", metricsAvailable: true },
    },
  });
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await expect(page.getByText("总览 / commercial-overview")).toBeVisible();
  await expect(page.getByText("总览 / kubedo-lab")).toHaveCount(0);
});

test("总览不可用时显示真实集群降级状态", async ({ page }) => {
  await installApiMocks(page, { overviewStatus: 503 });
  await page.goto("/nodes");

  const connection = page.getByRole("status", { name: "集群连接状态" });
  await expect(connection).toContainText("集群状态不可用");
  await expect(connection.getByRole("button", { name: "重试" })).toBeVisible();
  await expect(page.getByText("集群连接正常")).toHaveCount(0);
  await expect(page.getByText("kubedo-lab")).toHaveCount(0);
});

test("工作负载筛选条件可刷新、分享并随前进后退恢复", async ({ page, context }) => {
  test.setTimeout(150_000);
  await installApiMocks(page);
  await page.goto("/workloads");
  const search = page.getByPlaceholder("搜索工作负载名称");
  const namespace = page.getByRole("combobox", { name: "命名空间筛选" });
  const kind = page.getByRole("combobox", { name: "工作负载类型筛选" });
  const status = page.getByRole("combobox", { name: "工作负载状态筛选" });

  await search.fill("web");
  await expect(search).toHaveValue("web");
  await expect(page).toHaveURL(/q=web/);
  await namespace.click();
  await page.getByRole("option", { name: "default" }).click();
  await expect(page).toHaveURL(/namespace=default/);
  await kind.click();
  await page.getByRole("option", { name: "deployment" }).click();
  await expect(page).toHaveURL(/kind=deployment/);
  await status.click();
  await page.getByRole("option", { name: "Running" }).click();
  await expect(page).toHaveURL(/status=Running/);
  await expect(page.getByRole("link", { name: "web", exact: true })).toBeVisible();

  await page.reload();
  await expect(search).toHaveValue("web");
  await expect(namespace).toContainText("default");
  await expect(kind).toContainText("deployment");
  await expect(status).toContainText("Running");
  await expect(page.getByRole("link", { name: "web", exact: true })).toBeVisible();

  const sharedURL = page.url();
  const sharedPage = await context.newPage();
  await installApiMocks(sharedPage);
  await sharedPage.goto(sharedURL);
  await expect(sharedPage.getByPlaceholder("搜索工作负载名称")).toHaveValue("web");
  await expect(sharedPage.getByRole("combobox", { name: "命名空间筛选" })).toContainText("default");
  await expect(sharedPage.getByRole("combobox", { name: "工作负载类型筛选" })).toContainText("deployment");
  await expect(sharedPage.getByRole("combobox", { name: "工作负载状态筛选" })).toContainText("Running");
  await expect(sharedPage.getByRole("link", { name: "web", exact: true })).toBeVisible();

  await search.fill("api");
  await expect(page).toHaveURL(/q=api/);
  await expect(page.getByText("没有匹配的工作负载")).toBeVisible();
  await search.fill("");
  await expect(page).not.toHaveURL(/[?&]q=/);
  await status.click();
  await page.getByRole("option", { name: "Failed" }).click();
  await expect(page).toHaveURL(/status=Failed/);
  await expect(page.getByText("没有匹配的工作负载")).toBeVisible();
  await status.click();
  await page.getByRole("option", { name: "Running" }).click();
  await expect(page.getByRole("link", { name: "web", exact: true })).toBeVisible();

  await page.goto("/nodes");
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/kind=deployment/);
  await expect(page).toHaveURL(/status=Running/);
  await expect(search).toHaveValue("");
  await expect(namespace).toContainText("default");
  await page.goForward();
  await expect(page).toHaveURL(/\/nodes$/);
  await page.goBack();
  await expect(page).toHaveURL(/namespace=default/);
});

test("工作负载扩容发送带 CSRF 的受控写请求", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads");
  await page.getByRole("link", { name: "web", exact: true }).click();
  await expect(page.getByRole("heading", { name: "web" })).toBeVisible();

  await page.getByRole("button", { name: "扩缩容" }).click();
  await expect(page.getByRole("dialog", { name: "调整副本数" })).toBeVisible();
  await page.getByLabel("期望副本数").fill("3");

  const scaleRequest = page.waitForRequest((request) =>
    request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/workloads/deployment/default/web/scale",
  );
  await page.getByRole("button", { name: "确认调整" }).click();
  const request = await scaleRequest;

  expect(request.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(request.postDataJSON()).toEqual({ replicas: 3 });
  await expect(page.getByText("副本数已调整为 3")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "调整副本数" })).toBeHidden();
});

test("工作负载镜像更新携带容器和资源版本并发送 CSRF", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web");
  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "更新镜像" }).click();
  await expect(page.getByRole("dialog", { name: "更新容器镜像" })).toBeVisible();
  await page.getByLabel("镜像地址").fill("registry.example.test/web:2");

  const imageRequest = page.waitForRequest((request) =>
    request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/workloads/deployment/default/web/image",
  );
  await page.getByRole("button", { name: "确认更新" }).click();
  const request = await imageRequest;

  expect(request.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(request.postDataJSON()).toEqual({
    containerName: "web",
    image: "registry.example.test/web:2",
    resourceVersion: "42",
  });
  await expect(page.getByText("镜像已更新，滚动发布已触发")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "更新容器镜像" })).toBeHidden();
});

test("镜像仓支持多源配置、连接测试、默认切换和删除", async ({ page }) => {
  const registryRequests: RegistryRequest[] = [];
  await installApiMocks(page, { registryRequests });
  await page.goto("/registries");

  await expect(page.getByRole("heading", { name: "镜像仓库", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: /Primary Registry/ })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Harbor", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "测试连接", exact: true }).first().click();
  await expect(page.getByText("连接正常", { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "设为默认", exact: true }).click();
  await expect(page.getByText("Harbor 已设为默认镜像仓")).toBeVisible();
  expect(registryRequests.some((request) => request.method === "PUT" && request.path === "/api/v1/container-registries/harbor" && request.body?.default === true)).toBe(true);

  await page.getByRole("button", { name: "添加镜像仓", exact: true }).click();
  const editor = page.getByRole("dialog", { name: "添加镜像仓" });
  await editor.getByLabel("名称").fill("Staging Registry");
  await editor.getByLabel("镜像仓地址").fill("https://staging.example.com");
  await editor.getByLabel("仓库范围（可选）").fill("staging");
  await editor.getByLabel("拉取 Secret（可选）").fill("staging-pull");
  await editor.getByRole("button", { name: "添加镜像仓", exact: true }).click();
  await expect(page.getByText("镜像仓 Staging Registry 已创建")).toBeVisible();

  const created = registryRequests.find((request) => request.method === "POST");
  expect(created?.body).toMatchObject({
    name: "Staging Registry",
    endpoint: "https://staging.example.com",
    repositoryPrefix: "staging",
    pullSecretName: "staging-pull",
    authType: "anonymous",
  });

  const card = page.locator("div.rounded-lg.border.bg-card").filter({ hasText: "Staging Registry" });
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "删除", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(page.getByText("镜像仓 Staging Registry 已删除")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Staging Registry", exact: true })).toHaveCount(0);
});

test("镜像选择器可搜索仓库与 Tag 并默认锁定镜像所属仓库", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web");
  await page.getByRole("button", { name: "更新镜像", exact: true }).click();
  const updateDialog = page.getByRole("dialog", { name: "更新容器镜像" });
  await updateDialog.getByLabel("镜像地址").fill("registry.example.com/platform/api:1.0.0");
  await updateDialog.getByRole("button", { name: "浏览镜像仓" }).click();

  const picker = page.getByRole("dialog", { name: "选择容器镜像" });
  await expect(picker).toBeVisible();
  await expect(picker.getByRole("button", { name: "切换镜像仓" })).toBeVisible();
  await expect(picker.getByRole("combobox", { name: "镜像仓" })).toContainText("Primary Registry");
  await picker.getByRole("combobox", { name: "镜像仓" }).click();
  await expect(page.getByRole("option", { name: /Primary Registry/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Harbor/ })).toHaveCount(0);
  await picker.getByRole("combobox", { name: "镜像仓" }).click();

  await picker.getByRole("button", { name: "切换镜像仓" }).click();
  await picker.getByRole("combobox", { name: "镜像仓" }).click();
  await expect(page.getByRole("option", { name: /Harbor/ })).toBeVisible();
  await picker.getByRole("combobox", { name: "镜像仓" }).click();

  const repositoryList = picker.getByRole("listbox", { name: "仓库列表" });
  const tagList = picker.getByRole("listbox", { name: "Tag 列表" });
  await expect(repositoryList).toBeHidden();
  await expect(tagList).toBeHidden();
  await picker.getByRole("button", { name: "展开仓库列表" }).click();
  await expect(repositoryList).toBeVisible();
  await picker.getByLabel("搜索仓库").fill("api");
  await picker.getByRole("option", { name: "platform/api", exact: true }).click();
  await expect(repositoryList).toBeHidden();
  await expect(tagList).toBeVisible();
  await picker.getByLabel("搜索 Tag").fill("2.0");
  await picker.getByRole("option", { name: "2.0.0", exact: true }).click();
  await expect(tagList).toBeHidden();
  await expect(picker.getByText("registry.example.com/platform/api:2.0.0", { exact: true })).toBeVisible();
  await picker.getByRole("button", { name: "使用此镜像", exact: true }).click();
  await expect(picker).toBeHidden();
  await expect(updateDialog.getByLabel("镜像地址")).toHaveValue("registry.example.com/platform/api:2.0.0");

  const imageRequest = page.waitForRequest((request) => request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/workloads/deployment/default/web/image");
  await updateDialog.getByRole("button", { name: "确认更新", exact: true }).click();
  expect((await imageRequest).postDataJSON()).toEqual({
    containerName: "web",
    image: "registry.example.com/platform/api:2.0.0",
    registryId: "primary",
    resourceVersion: "42",
  });
});

test("镜像仓页面与镜像选择器在 320px 和 390px 视口内无横向溢出", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/registries");
    await expect(page.getByRole("heading", { name: "镜像仓库", exact: true })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px container registries`);
    await page.screenshot({ path: `test-results/mobile-registries/page-${viewport.width}.png`, fullPage: true });

    await page.goto("/workloads/deployment/default/web");
    await page.getByRole("button", { name: "更新镜像", exact: true }).click();
    await page.getByRole("button", { name: "浏览镜像仓" }).click();
    const picker = page.getByRole("dialog", { name: "选择容器镜像" });
    await expect(picker).toBeVisible();
    await expectElementInsideViewport(page, picker, `${viewport.width}px image picker`);
    await expectNoPageOverflow(page, `${viewport.width}px image picker`);

    await picker.getByRole("combobox", { name: "镜像仓" }).click();
    const registryOptions = page.getByRole("listbox", { name: "镜像仓选项列表" });
    await expect(registryOptions).toBeVisible();
    await expectElementInsideViewport(page, registryOptions, `${viewport.width}px registry options`);
    await expectNoPageOverflow(page, `${viewport.width}px registry options`);
    await page.screenshot({ path: `test-results/mobile-registries/picker-${viewport.width}.png`, fullPage: false });
  }
});

test("镜像选择器显示仓库与 Tag 接口失败的诊断信息", async ({ page }) => {
  const unavailableRegistry: ContainerRegistry = {
    ...mockContainerRegistries[0],
    lastTest: {
      ok: false,
      message: "registry request failed: context deadline exceeded",
      testedAt: "2026-11-17T06:44:54Z",
      capabilities: { catalog: false, tags: false },
    },
  };
  await installApiMocks(page, {
    containerRegistries: [unavailableRegistry],
    registryFailures: { repositories: true, tags: true },
  });
  await page.goto("/workloads/deployment/default/web");
  await page.getByRole("button", { name: "更新镜像", exact: true }).click();
  await page.getByRole("button", { name: "浏览镜像仓" }).click();

  const picker = page.getByRole("dialog", { name: "选择容器镜像" });
  await picker.getByRole("button", { name: "展开仓库列表" }).click();
  await expect(picker.getByRole("alert").filter({ hasText: "仓库列表不可用" })).toContainText("networkPolicy.containerRegistries");
  await expect(picker.getByRole("alert").filter({ hasText: "仓库列表不可用" })).toContainText("registry request failed: context deadline exceeded");

  await picker.getByLabel("仓库路径").fill("platform/api");
  await picker.getByRole("button", { name: "展开 Tag 列表" }).click();
  await expect(picker.getByRole("alert").filter({ hasText: "Tag 列表不可用" })).toContainText("networkPolicy.containerRegistries");
  await expect(picker.getByRole("alert").filter({ hasText: "Tag 列表不可用" })).toContainText("registry request failed: context deadline exceeded");
});

test("工作负载容器配置可查看并更新环境变量", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web?tab=config");
  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();

  await expect(page.getByText("环境变量", { exact: true })).toBeVisible();
  await expect(page.getByText("MODE", { exact: true })).toBeVisible();
  await expect(page.getByText("production", { exact: true })).toBeVisible();
  await expect(page.getByText("/etc/web", { exact: true })).toBeVisible();
  await expect(page.getByText("HTTP GET http://Pod/ready:80", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "编辑环境变量" }).click();
  const dialog = page.getByRole("dialog", { name: "编辑环境变量" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "添加环境变量" }).click();
  await dialog.getByLabel("变量 3 名称").fill("FEATURE_FLAG");
  await dialog.getByLabel("变量 3 值").fill("enabled");

  const configRequest = page.waitForRequest((request) =>
    request.method() === "PUT" && new URL(request.url()).pathname === "/api/v1/workloads/deployment/default/web/containers/web/config",
  );
  await dialog.getByRole("button", { name: "保存配置" }).click();
  const request = await configRequest;
  expect(request.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(request.postDataJSON()).toMatchObject({
    resourceVersion: "42",
    volumeMounts: [{ name: "config", mountPath: "/etc/web", readOnly: true }],
    env: [
      { name: "MODE", value: "production" },
      { name: "TOKEN", value: "", valueFrom: { kind: "secretKeyRef", name: "web-secret", key: "token" } },
      { name: "FEATURE_FLAG", value: "enabled" },
    ],
    readinessProbe: { type: "httpGet", path: "/ready", port: "80" },
  });
  await expect(page.getByText("容器配置已更新，滚动发布已触发")).toBeVisible();
  await expect(dialog).toBeHidden();
});

test("工作负载实例中的节点名称可跳转到节点详情", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web?tab=pods");
  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();

  await page.getByRole("link", { name: "worker-0001", exact: true }).click();
  await expect(page).toHaveURL(/\/nodes\/worker-0001$/);
  await expect(page.getByRole("heading", { name: "worker-0001", exact: true })).toBeVisible();
});

test("工作负载访问方式可创建、编辑和删除关联 Service", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web?tab=services");
  await expect(page.getByRole("heading", { name: "关联 Service" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "类型" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "访问地址" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Endpoint" })).toBeVisible();

  await page.getByRole("button", { name: "配置访问方式" }).click();
  const createDialog = page.getByRole("dialog", { name: "配置访问方式" });
  await expect(createDialog).toBeVisible();
  await createDialog.getByLabel("Service 名称").fill("web-public");
  await createDialog.getByRole("combobox", { name: "访问类型" }).click();
  await page.getByRole("option", { name: "Headless（无 ClusterIP）" }).click();
  await createDialog.getByLabel("Service 端口").fill("8080");
  await createDialog.getByLabel("目标端口").fill("8080");
  const createRequest = page.waitForRequest((request) => request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/services");
  await createDialog.getByRole("button", { name: "创建 Service" }).click();
  const created = await createRequest;
  expect(created.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(created.postDataJSON()).toMatchObject({ name: "web-public", namespace: "default", type: "ClusterIP", headless: true, selector: { app: "web" } });
  await expect(page.getByText("Headless", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "web-public", exact: true })).toBeVisible();

  const webRow = page.getByRole("row").filter({ has: page.getByRole("link", { name: "web", exact: true }) });
  await webRow.getByRole("button", { name: "操作 Service default/web" }).click();
  await page.getByRole("menuitem", { name: "编辑" }).click();
  const editDialog = page.getByRole("dialog", { name: "编辑 Service" });
  await expect(editDialog).toBeVisible();
  const updateRequest = page.waitForRequest((request) => request.method() === "PUT" && new URL(request.url()).pathname === "/api/v1/services/default/web");
  await editDialog.getByRole("button", { name: "保存修改" }).click();
  const updated = await updateRequest;
  expect(updated.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(updated.postDataJSON()).toMatchObject({ resourceVersion: "42", selector: { app: "web" } });

  await webRow.getByRole("button", { name: "操作 Service default/web" }).click();
  await page.getByRole("menuitem", { name: "删除" }).click();
  const deleteDialog = page.getByRole("alertdialog", { name: /删除 Service web/ });
  await deleteDialog.getByLabel("Service 名称").fill("web");
  const deleteRequest = page.waitForRequest((request) => request.method() === "DELETE" && new URL(request.url()).pathname === "/api/v1/services/default/web");
  await deleteDialog.getByRole("button", { name: "确认删除" }).click();
  const deleted = await deleteRequest;
  expect(deleted.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(new URL(deleted.url()).searchParams.get("resourceVersion")).toBe("43");
  await expect(page.getByRole("link", { name: "web", exact: true })).toHaveCount(0);
});

test("未选择命名空间时创建 Service 按钮提示原因", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/network");

  const button = page.getByRole("button", { name: "创建 Service" });
  await expect(button).toBeDisabled();
  await button.locator("..").hover();
  await expect(page.getByRole("tooltip")).toContainText("请先选择命名空间，再创建 Service");
});

test("CronJob 暂停携带资源版本并发送 CSRF", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/cronjob/default/backup");
  await expect(page.getByRole("heading", { name: "backup", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "暂停调度" }).click();
  await expect(page.getByRole("alertdialog", { name: "暂停 CronJob 调度？" })).toBeVisible();

  const suspendRequest = page.waitForRequest((request) =>
    request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/workloads/cronjob/default/backup/suspend",
  );
  await page.getByRole("button", { name: "确认暂停" }).click();
  const request = await suspendRequest;

  expect(request.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(request.postDataJSON()).toEqual({ suspend: true, resourceVersion: "77" });
  await expect(page.getByText("CronJob 已暂停调度")).toBeVisible();
});

test("Pod 日志通过流式接口加载", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/pods/default/web-abcdef-12345");
  await expect(page.getByRole("heading", { name: "web-abcdef-12345" })).toBeVisible();
  await expect(page.getByText("25m", { exact: true })).toBeVisible();
  await expect(page.getByText("32 MiB", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "日志" }).click();
  await expect(page.getByText("hello from dosh log stream", { exact: false })).toBeVisible();
  await expect(page.getByText("request served by web-abcdef-12345", { exact: false })).toBeVisible();
});

test("工作负载日志聚合多个 Pod 并标注来源", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web?tab=logs");
  await expect(page.getByText("多实例聚合", { exact: true })).toBeVisible();
  await expect(page.getByText("[web-abcdef-12345/web] aggregate log from web instance", { exact: false })).toBeVisible();
  await expect(page.getByText("[web-abcdef-67890/web] aggregate log from second instance", { exact: false })).toBeVisible();
  await expectNoPageOverflow(page, "workload aggregate logs");
});

test("资源详情展示直接关系与故障拓扑", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/resources/example.io/v1/widgets/sample-widget?namespace=default");
  await expect(page.getByRole("heading", { name: "故障拓扑与直接关系" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "上游依赖" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "下游对象" })).toBeVisible();
  await expect(page.getByText("worker-0001", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "web", exact: true }).click();
  await expect(page).toHaveURL(/\/network\/services\/default\/web/);
  await page.goto("/network/ingresses/default/web");
  await expect(page.getByRole("heading", { name: "流量拓扑与直接关系" })).toBeVisible();
  await expectNoPageOverflow(page, "resource relations");
});

test("资源关系支持摘要、方向和类型筛选", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/resources/example.io/v1/widgets/sample-widget?namespace=default");
  const summary = page.getByLabel("关系摘要");
  await expect(summary.getByText("异常关联")).toBeVisible();
  await expect(summary.getByText("1", { exact: true })).toBeVisible();
  await expect(page.getByText("显示 4 / 4 条直接关系")).toBeVisible();

  await page.getByRole("combobox", { name: "关系方向" }).click();
  await page.getByRole("option", { name: "下游对象" }).click();
  await expect(page.getByText("显示 2 / 4 条直接关系")).toBeVisible();
  await expect(page.getByText("web-broken", { exact: true })).toBeVisible();

  await page.getByRole("combobox", { name: "资源类型" }).click();
  await page.getByRole("option", { name: "Pod" }).click();
  await expect(page.getByText("显示 1 / 4 条直接关系")).toBeVisible();
  await expect(page.getByText("web", { exact: true })).toHaveCount(0);
  await expectNoPageOverflow(page, "resource relation filters");
});

test("Pod 与 Service 端口转发在桌面和移动端可用且无横向溢出", async ({ page }) => {
  await installApiMocks(page);
  for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }, { width: 320, height: 720 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/pods/default/web-abcdef-12345?tab=port-forward");
    await expect(page.getByRole("heading", { name: "新建临时隧道" })).toBeVisible();
    await expect(page.getByText("80", { exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "建立隧道" }).click();
    await expect(page.getByText("临时访问地址")).toBeVisible();
    await expect(page.getByText("web-abcdef-12345", { exact: true }).last()).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px pod port-forward`);
    await page.screenshot({ path: `test-results/mobile-interactions/pod-port-forward-${viewport.width}.png`, fullPage: true });
    await page.getByRole("button", { name: "关闭隧道" }).click();
    await expect(page.getByText("尚未创建端口转发。创建后会显示临时访问地址、后端 Pod 和到期时间。")).toBeVisible();

    await page.goto("/network/services/default/web?tab=port-forward");
    await expect(page.getByRole("heading", { name: "新建临时隧道" })).toBeVisible();
    await page.getByRole("button", { name: "建立隧道" }).click();
    await expect(page.getByText("临时访问地址")).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px service port-forward`);
  }
});

test("Helm 升级预览与执行在桌面和移动端无横向溢出", async ({ page }) => {
  await installApiMocks(page);
  for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }, { width: 320, height: 720 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/helm/releases/default/web?namespace=default&tab=upgrade");
    await page.getByLabel("升级 Values（YAML）").fill("replicaCount: 3\n");
    await page.getByRole("button", { name: "生成升级预览" }).click();
    await expect(page.getByRole("heading", { name: "升级后 Values Diff" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "升级后 Manifest Diff" })).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px helm upgrade preview`);

    await page.getByRole("button", { name: "执行升级" }).click();
    const dialog = page.getByRole("dialog", { name: "执行 Helm 升级" });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel(/输入 Release 名称/).fill("web");
    await dialog.getByRole("button", { name: "确认升级" }).click();
    await expect(page.getByText(/已创建 Helm revision #3/)).toBeVisible();
    await expectNoPageOverflow(page, `${viewport.width}px helm upgrade result`);
  }
});

test("StatefulSet 推荐模板可直接进入结构化表单", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/resources/apps/v1/statefulsets?namespace=default");
  await page.getByRole("button", { name: "创建 YAML" }).click();
  const dialog = page.getByRole("dialog", { name: "创建 StatefulSet" });
  await expect(dialog.locator(".cm-content")).toContainText("statefulset-example");
  await dialog.getByRole("tab", { name: "表单" }).click();
  await expect(dialog.getByLabel("名称")).toHaveValue("statefulset-example");
  await expect(dialog.getByText("资源字段")).toBeVisible();
  await expectNoPageOverflow(page, "statefulset starter template form");
});

test("全局搜索默认跨命名空间检索并记录最近资源偏好", async ({ page }) => {
  const preferenceRequests: PreferenceRequest[] = [];
  await installApiMocks(page, { preferenceRequests });
  await page.goto("/");
  await page.keyboard.press("Control+K");
  const dialog = page.getByRole("dialog", { name: "搜索集群资源" });
  await dialog.getByPlaceholder("输入资源名称…").fill("sample");
  await expect(dialog.getByText("sample-widget", { exact: true })).toBeVisible();
  await dialog.getByText("sample-widget", { exact: true }).click();
  await expect(page).toHaveURL(/\/resources\/example\.io\/v1\/widgets\/sample-widget/);
  await expect.poll(() => preferenceRequests.some((request) => request.body.recent?.some((item) => item.path.includes("/resources/example.io/v1/widgets/sample-widget"))), { timeout: 5_000 }).toBe(true);
  await expectNoPageOverflow(page, "global search and recent preferences");
});

test("Pod 日志中的 HTML 与 ANSI 控制字符仅作为纯文本渲染", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/pods/default/web-abcdef-12345?tab=logs");

  const output = page.locator("pre");
  await expect(output).toContainText('<img src=x onerror="window.__doshXss=true"> DOSH_E2E_HTML_MARKER');
  await expect(output).toContainText("ANSI_RED");
  await expect(output).toContainText("ANSI_LINK");
  await expect(output.locator("img")).toHaveCount(0);
  await expect(page.getByRole("link", { name: "ANSI_LINK" })).toHaveCount(0);
  expect(await page.evaluate(() => (window as Window & { __doshXss?: boolean }).__doshXss)).toBeUndefined();
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
  await expect(output).not.toContainText("\u001b");
});

test("事件消息中的 HTML 不会创建可执行 DOM 节点", async ({ page }) => {
  await installApiMocks(page);
  await page.route("**/api/v1/events**", (route) => json(route, list([{
    uid: "event-xss",
    namespace: "default",
    type: "Warning",
    reason: "InjectedMessage",
    message: '<img src=x onerror="window.__doshEventXss=true"> DOSH_EVENT_MARKER',
    objectKind: "Pod",
    objectName: "web-abcdef-12345",
    count: 1,
    firstSeen: "2026-11-02T00:00:00Z",
    lastSeen: "2026-11-02T00:00:00Z",
  }])));

  await page.goto("/events");
  await expect(page.getByText("DOSH_EVENT_MARKER", { exact: false })).toBeVisible();
  await expect(page.locator("img")).toHaveCount(0);
  expect(await page.evaluate(() => (window as Window & { __doshEventXss?: boolean }).__doshEventXss)).toBeUndefined();
});

test("集群事件长消息限制为两行并可查看全文", async ({ page }) => {
  const message = `Long event message ${"with detailed context ".repeat(40)}`;
  await installApiMocks(page);
  await page.route("**/api/v1/events**", (route) => json(route, list([{
    uid: "event-long-message",
    namespace: "default",
    type: "Warning",
    reason: "LongMessage",
    message,
    objectKind: "Pod",
    objectName: "web-abcdef-12345",
    count: 1,
    firstSeen: "2026-11-02T00:00:00Z",
    lastSeen: "2026-11-02T00:00:00Z",
  }])));

  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/events");
    const trigger = page.getByRole("button", { name: message });
    await expect(trigger).toBeVisible();
    expect(await trigger.locator("span").evaluate((element) => getComputedStyle(element).webkitLineClamp)).toBe("2");
    expect(await trigger.locator("xpath=ancestor::td").evaluate((element) => element.getBoundingClientRect().width)).toBeGreaterThanOrEqual(420);
    expect(await trigger.locator("xpath=ancestor::tr").evaluate((element) => element.getBoundingClientRect().height)).toBeLessThan(72);
  }

  const trigger = page.getByRole("button", { name: message });
  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "事件消息" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(message, { exact: true })).toBeVisible();
});

test("集群事件对象可跳转到资源详情并显示完整总数", async ({ page }) => {
  await installApiMocks(page);
  const events = [
    {
      uid: "event-deployment-link",
      namespace: "default",
      type: "Normal",
      reason: "ScalingReplicaSet",
      message: "Scaled deployment",
      objectKind: "Deployment",
      objectName: "web",
      count: 1,
      firstSeen: "2026-11-02T00:00:00Z",
      lastSeen: "2026-11-02T00:01:00Z",
    },
    ...Array.from({ length: 11 }, (_, index) => ({
      uid: `event-pod-link-${index}`,
      namespace: "default",
      type: "Normal",
      reason: "Started",
      message: `Started pod ${index}`,
      objectKind: "Pod",
      objectName: `web-event-${index}`,
      count: 1,
      firstSeen: "2026-11-02T00:00:00Z",
      lastSeen: `2026-11-02T00:${10 + index}:00Z`,
    })),
  ];
  await page.route("**/api/v1/events**", (route) => json(route, list(events)));
  await page.goto("/events");

  await expect(page.getByText("共 12 条事件")).toBeVisible();
  const deployment = page.getByRole("link", { name: "Deployment/web", exact: true });
  await expect(deployment).toHaveAttribute("href", "/workloads/deployment/default/web");
  await expect(page.getByRole("link", { name: "Pod/web-event-10", exact: true })).toBeVisible();
  await deployment.click();
  await expect(page).toHaveURL(/\/workloads\/deployment\/default\/web$/);
  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();
});

test("Pod 日志可切换时间戳参数", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/pods/default/web-abcdef-12345?tab=logs");
  await expect(page.getByText("hello from dosh log stream", { exact: false })).toBeVisible();

  const request = page.waitForRequest((candidate) => {
    const url = new URL(candidate.url());
    return url.pathname === "/api/v1/pods/default/web-abcdef-12345/logs" && url.searchParams.get("timestamps") === "true";
  });
  await page.getByRole("button", { name: "显示时间戳" }).click();
  await request;
  await expect(page.getByRole("button", { name: "隐藏时间戳" })).toHaveAttribute("aria-pressed", "true");
});

test("Pod 详情标签可深链并在刷新后保持", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/pods/default/web-abcdef-12345?tab=logs");

  await expect(page.getByRole("tab", { name: "日志" })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText("hello from dosh log stream", { exact: false })).toBeVisible();

  await page.getByRole("tab", { name: /实例/ }).click();
  await expect(page).toHaveURL(/\?tab=containers$/);
  await page.reload();
  await expect(page.getByRole("tab", { name: /实例/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText("nginx:alpine", { exact: true })).toBeVisible();
});

test("详情页多标签可深链并加载事件与 YAML", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web");

  await expect(page.getByRole("tab", { name: "概览", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "容器配置", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: /实例/ })).toBeVisible();
  await expect(page.getByRole("tab", { name: "事件", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "日志", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "YAML", exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "事件", exact: true }).click();
  await expect(page).toHaveURL(/\?tab=events$/);
  await expect(page.getByText("该资源暂无事件")).toBeVisible();

  const yamlRequest = page.waitForRequest((request) => new URL(request.url()).pathname === "/api/v1/yaml");
  await page.getByRole("tab", { name: "YAML", exact: true }).click();
  await yamlRequest;
  await expect(page).toHaveURL(/\?tab=yaml$/);
  await expect(page.getByText("apiVersion: v1", { exact: false })).toBeVisible();
});

test("未认证访问受保护页面跳转登录", async ({ page }) => {
  await installApiMocks(page, { authenticated: false });
  await page.goto("/nodes");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
});

test("受保护 API 会话过期后跳转登录", async ({ page }) => {
  await installApiMocks(page, { expireSessionOnNodes: true });
  await page.goto("/nodes");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
});

test("资源浏览器支持多版本、Schema 表单、Diff 与高风险确认", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);
  await page.goto("/resources");
  await expect(page.getByRole("heading", { name: "资源浏览器" })).toBeVisible();
  const widgetRow = page.getByRole("row").filter({ has: page.getByRole("link", { name: "Widget", exact: true }) });
  await expect(widgetRow).toContainText("Preferred");
  await widgetRow.getByRole("link", { name: "Widget", exact: true }).click();
  await expect(page).toHaveURL(/\/resources\/example\.io\/v1\/widgets/);
  await page.getByLabel("API 版本").click();
  await page.getByRole("option", { name: /v1beta1/ }).click();
  await expect(page).toHaveURL(/\/resources\/example\.io\/v1beta1\/widgets/);
  await page.getByRole("link", { name: "sample-widget" }).click();
  await expect(page.getByRole("heading", { name: "sample-widget", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "编辑资源" }).click();
  const editDialog = page.getByRole("dialog");
  await expect(editDialog).toContainText("Schema 表单 / YAML 双模式");
  await editDialog.getByRole("tab", { name: "表单" }).click();
  await expect(editDialog.getByRole("heading", { name: "基本信息" })).toBeVisible();
  await editDialog.getByLabel("尺寸").selectOption("large");
  await editDialog.getByRole("tab", { name: /变更预览/ }).click();
  const diff = editDialog.getByLabel("YAML 变更预览");
  await expect(diff.getByText("size: large", { exact: true })).toBeVisible();
  await expect(diff.getByText("size: small", { exact: true })).toBeVisible();
  await editDialog.getByRole("button", { name: "取消" }).click();

  await page.goto("/resources/example.io/v1/clusterwidgets");
  await expect(page.getByRole("heading", { name: "ClusterWidget", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "创建 YAML" }).click();
  const riskyDialog = page.getByRole("dialog");
  await expect(riskyDialog).toContainText("高风险资源");
  await riskyDialog.getByRole("tab", { name: "表单" }).click();
  await riskyDialog.getByLabel(/^名称/).fill("demo-cluster-widget");
  await riskyDialog.getByLabel(/^尺寸/).selectOption("large");
  const createButton = riskyDialog.getByRole("button", { name: "创建", exact: true });
  await expect(createButton).toBeDisabled();
  await riskyDialog.getByLabel("输入资源名称确认高风险写入").fill("demo-cluster-widget");
  await expect(createButton).toBeEnabled();
});

test("移动端可通过导航抽屉访问节点", async ({ page }) => {
  await installApiMocks(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("link", { name: "节点管理" }).click();

  await expect(page).toHaveURL(/\/nodes$/);
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();
  await expect(page.getByRole("link", { name: "worker-0001" })).toBeVisible();

  const tableRegion = page.getByRole("region", { name: "可滚动表格" });
  await expect(tableRegion).toBeVisible();
  await expect(page.getByText("左右滑动查看完整列")).toBeVisible();
  expect(await tableRegion.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true);
  expect(await page.getByRole("columnheader", { name: /节点名称/ }).evaluate((element) => getComputedStyle(element).whiteSpace)).toBe("nowrap");
  expect(await page.getByRole("row").nth(1).evaluate((element) => element.getBoundingClientRect().height)).toBeLessThan(64);
});

test("移动端其他功能默认折叠且可从抽屉进入权限检查", async ({ page }) => {
  test.setTimeout(60_000);
  await installApiMocks(page);

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/");
    await page.getByRole("button", { name: "打开导航" }).click();
    const drawer = page.locator("aside");
    const other = drawer.getByRole("button", { name: "其他功能", exact: true });
    await expect(other).toHaveAttribute("aria-expanded", "false");
    await expect(drawer.getByRole("link", { name: "治理与策略", exact: true })).toHaveCount(0);
    await expectNoPageOverflow(page, `${viewport.width}px collapsed other navigation`);

    await other.click();
    await expect(other).toHaveAttribute("aria-expanded", "true");
    await expect(drawer.getByRole("link", { name: "治理与策略", exact: true })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "权限检查", exact: true })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "镜像仓库", exact: true })).toBeVisible();
    await expectElementInsideViewport(page, drawer, `${viewport.width}px expanded other navigation`);
    await expectElementInsideViewport(page, drawer.getByRole("link", { name: "权限检查", exact: true }), `${viewport.width}px other navigation access link`);
    await expectNoPageOverflow(page, `${viewport.width}px expanded other navigation`);
    await page.screenshot({ path: `test-results/mobile-interactions/other-navigation-${viewport.width}.png`, fullPage: false });
    await drawer.getByRole("link", { name: "权限检查", exact: true }).click();
    await expect(page).toHaveURL(/\/access$/);
    await expect(page.getByRole("heading", { name: "权限检查", exact: true })).toBeVisible();
  }
});

test("移动端超长固定列不会遮挡其他列", async ({ page }) => {
  const longName = `web-${"a".repeat(96)}`;
  await installApiMocks(page, { podName: longName });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/pods");

  const region = page.getByRole("region", { name: "可滚动表格" });
  const firstRow = page.getByRole("row").nth(1);
  const firstCell = firstRow.getByRole("cell").first();
  const lastCell = firstRow.getByRole("cell").last();

  const firstCellWidth = await firstCell.evaluate((element) => element.getBoundingClientRect().width);
  expect(firstCellWidth).toBeLessThanOrEqual(150);
  const truncation = await firstCell.evaluate((element) => {
    const content = element.firstElementChild as HTMLElement;
    const style = getComputedStyle(content);
    return { overflow: style.overflow, textOverflow: style.textOverflow, scrollWidth: content.scrollWidth, clientWidth: content.clientWidth };
  });
  expect(truncation.overflow).toBe("hidden");
  expect(truncation.textOverflow).toBe("ellipsis");
  expect(truncation.scrollWidth).toBeGreaterThan(truncation.clientWidth);

  await region.evaluate((element) => { element.scrollLeft = element.scrollWidth; });
  const uncoveredWidth = await lastCell.evaluate((element, stickyWidth) => {
    const scrollRegion = element.closest("[role=region]")!;
    const cellRect = element.getBoundingClientRect();
    const regionRect = scrollRegion.getBoundingClientRect();
    return Math.min(cellRect.right, regionRect.right) - Math.max(cellRect.left, regionRect.left + stickyWidth);
  }, firstCellWidth);

  expect(uncoveredWidth).toBeGreaterThan(20);
  expect(await firstRow.evaluate((element) => element.getBoundingClientRect().height)).toBeLessThan(64);
});

test("移动端逐页布局无页面级横向溢出", async ({ page }) => {
  test.setTimeout(360_000);
  await installApiMocks(page);
  await page.setViewportSize({ width: 390, height: 844 });
  const routes = [
    { path: "/", name: "overview" },
    { path: "/cluster/config", name: "cluster-config" },
    { path: "/yaml/create", name: "yaml-create" },
    { path: "/nodes", name: "nodes" },
    { path: "/nodes/worker-0001", name: "node-detail" },
    { path: "/nodes/worker-0001?tab=labels", name: "node-labels" },
    { path: "/nodes/worker-0001?tab=taints", name: "node-taints" },
    { path: "/workloads", name: "workloads" },
    { path: "/workloads/deployment/default/web", name: "workload-detail" },
    { path: "/workloads/deployment/default/web?tab=services", name: "workload-services" },
    { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
    { path: "/workloads/deployment/default/web?tab=logs", name: "workload-logs" },
    { path: "/workloads/deployment/default/web?tab=history", name: "workload-history" },
    { path: "/workloads/deployment/default/web?tab=yaml", name: "workload-yaml" },
    { path: "/pods", name: "pods" },
    { path: "/pods/default/web-abcdef-12345", name: "pod-detail" },
    { path: "/pods/default/web-abcdef-12345?tab=logs", name: "pod-logs" },
    { path: "/pods/default/web-abcdef-12345?tab=terminal", name: "pod-terminal" },
    { path: "/pods/default/web-abcdef-12345?tab=yaml", name: "pod-yaml" },
    { path: "/network", name: "network" },
    { path: "/network?tab=ingresses", name: "network-ingresses" },
    { path: "/network/services/default/web", name: "service-detail" },
    { path: "/network/ingresses/default/web", name: "ingress-detail" },
    { path: "/configuration", name: "configuration" },
    { path: "/configuration?tab=secrets", name: "configuration-secrets" },
    { path: "/configuration?tab=certificate", name: "configuration-certificate" },
    { path: "/storage", name: "storage" },
    { path: "/storage?tab=volumes", name: "storage-volumes" },
    { path: "/storage?tab=claims", name: "storage-claims" },
    { path: "/storage/classes/data-standard", name: "storage-class-detail" },
    { path: "/storage/classes/data-standard?tab=events", name: "storage-class-events" },
    { path: "/storage/volumes/pv-data", name: "pv-detail" },
    { path: "/storage/volumes/pv-data?tab=yaml", name: "pv-yaml" },
    { path: "/storage/claims/production/data-claim", name: "pvc-detail" },
    { path: "/storage/claims/production/data-claim?tab=events", name: "pvc-events" },
    { path: "/namespaces", name: "namespaces" },
    { path: "/namespaces/default", name: "namespace-detail" },
    { path: "/namespaces/default?tab=quotas", name: "namespace-quotas" },
    { path: "/namespaces/default?tab=limits", name: "namespace-limits" },
    { path: "/namespaces/default?tab=events", name: "namespace-events" },
    { path: "/namespaces/default?tab=yaml", name: "namespace-yaml" },
    { path: "/events", name: "events" },
    { path: "/audit", name: "audit" },
    { path: "/resources", name: "resources" },
    { path: "/resources/example.io/v1/widgets", name: "resource-list" },
    { path: "/resources/example.io/v1/widgets/sample-widget?namespace=default", name: "resource-detail" },
    { path: "/governance", name: "governance" },
    { path: "/governance?namespace=default", name: "governance-namespace" },
    { path: "/access", name: "access-review" },
    { path: "/registries", name: "container-registries" },
    { path: "/helm/releases?namespace=default", name: "helm-releases" },
    { path: "/helm/releases/default/web?namespace=default", name: "helm-release-detail" },
    { path: "/helm/releases/default/web?namespace=default&tab=values", name: "helm-release-values" },
    { path: "/helm/releases/default/web?namespace=default&tab=manifest", name: "helm-release-manifest" },
    { path: "/helm/releases/default/web?namespace=default&tab=history", name: "helm-release-history" },
    { path: "/helm/releases/default/web?namespace=default&tab=diff", name: "helm-release-diff" },
  ];

  for (const route of routes) {
    await page.goto(route.path);
    await page.locator("main h1").waitFor({ state: "visible" });
    await page.waitForTimeout(250);
    await expectNoPageOverflow(page, route.name);
    await page.screenshot({ path: `test-results/mobile-audit/${route.name}.png`, fullPage: true });
  }
});

test("移动端登录、抽屉导航和全局搜索可用", async ({ page }) => {
  test.setTimeout(90_000);
  await installApiMocks(page, { authenticated: false });
  await page.setViewportSize({ width: 390, height: 844 });

  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
  await expectNoPageOverflow(page, "login");
  await page.screenshot({ path: "test-results/mobile-interactions/login.png", fullPage: true });

  await page.getByLabel("用户名").fill("admin");
  await page.getByLabel("密码").fill("e2e-password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();

  const menuButton = page.getByRole("button", { name: "打开导航" });
  await expect(menuButton).toHaveAttribute("aria-expanded", "false");
  await menuButton.click();
  await expect(menuButton).toHaveAttribute("aria-expanded", "true");
  const drawer = page.locator("aside");
  await expect(drawer.getByRole("link", { name: "节点管理" })).toBeVisible();
  await expectElementInsideViewport(page, drawer, "mobile navigation drawer");
  await page.screenshot({ path: "test-results/mobile-interactions/navigation.png", fullPage: true });

  await drawer.getByRole("link", { name: "节点管理" }).click();
  await expect(page).toHaveURL(/\/nodes$/);
  await expect(page.getByRole("heading", { name: "节点管理" })).toBeVisible();
  await expect(menuButton).toHaveAttribute("aria-expanded", "false");
  await page.waitForTimeout(300);
  const closedDrawer = await drawer.boundingBox();
  expect(closedDrawer).not.toBeNull();
  expect(closedDrawer!.x + closedDrawer!.width).toBeLessThanOrEqual(1);
  await expect(drawer.locator("a", { hasText: "节点管理" })).toBeHidden();

  await page.getByRole("button", { name: "搜索" }).click();
  const searchDialog = page.getByRole("dialog", { name: "搜索集群资源" });
  await expect(searchDialog).toBeVisible();
  await searchDialog.getByPlaceholder("输入资源名称…").fill("web");
  await expect(searchDialog.getByRole("button", { name: /web-abcdef-12345/ })).toBeVisible();
  await expectElementInsideViewport(page, searchDialog, "global search dialog");
  await expectNoPageOverflow(page, "global search dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/global-search.png", fullPage: true });
  await page.keyboard.press("Escape");
  await expect(searchDialog).toBeHidden();
  await page.keyboard.press("Control+K");
  await expect(searchDialog).toBeVisible();
  await expect(searchDialog.getByPlaceholder("输入资源名称…")).toHaveValue("");
  await page.keyboard.press("Escape");
});

test("320px 窄屏关键页面无横向溢出", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);
  await page.setViewportSize({ width: 320, height: 720 });
  for (const route of [
    { path: "/", name: "overview" },
    { path: "/nodes", name: "nodes" },
    { path: "/nodes/worker-0001?tab=labels", name: "node-labels" },
    { path: "/nodes/worker-0001?tab=taints", name: "node-taints" },
    { path: "/workloads/deployment/default/web", name: "workload-detail" },
    { path: "/workloads/deployment/default/web?tab=services", name: "workload-services" },
    { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
    { path: "/workloads/deployment/default/web?tab=history", name: "workload-history" },
    { path: "/pods/default/web-abcdef-12345?tab=logs", name: "pod-logs" },
    { path: "/pods/default/web-abcdef-12345?tab=terminal", name: "pod-terminal" },
    { path: "/configuration?tab=certificate", name: "certificate" },
    { path: "/events", name: "events" },
    { path: "/audit", name: "audit" },
    { path: "/governance", name: "governance" },
    { path: "/governance?namespace=default", name: "governance-namespace" },
    { path: "/access", name: "access-review" },
    { path: "/registries", name: "container-registries" },
    { path: "/helm/releases?namespace=default", name: "helm-releases" },
    { path: "/helm/releases/default/web?namespace=default", name: "helm-release-detail" },
    { path: "/helm/releases/default/web?namespace=default&tab=history", name: "helm-release-history" },
    { path: "/helm/releases/default/web?namespace=default&tab=diff", name: "helm-release-diff" },
  ]) {
    await page.goto(route.path);
    await page.locator("main h1").waitFor({ state: "visible" });
    await expectNoPageOverflow(page, `320px ${route.name}`);
    await page.screenshot({ path: `test-results/mobile-320/${route.name}.png`, fullPage: true });
  }
});

test("治理页移动端卡片不超出主内容区域", async ({ page }) => {
  test.setTimeout(90_000);
  await installApiMocks(page);

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/governance");
    await page.locator("main h1").waitFor({ state: "visible" });
    await page.waitForTimeout(250);

    const layout = await page.evaluate(() => {
      const main = document.querySelector("main")!;
      const mainRect = main.getBoundingClientRect();
      const mainStyle = getComputedStyle(main);
      const contentLeft = mainRect.left + Number.parseFloat(mainStyle.paddingLeft);
      const contentRight = mainRect.right - Number.parseFloat(mainStyle.paddingRight);
      const cards = Array.from(document.querySelectorAll<HTMLAnchorElement>('section[aria-labelledby^="governance-"] a[href^="/resources/"]'));
      const outside = cards.filter((card) => {
        const rect = card.getBoundingClientRect();
        return rect.left < contentLeft - 1 || rect.right > contentRight + 1;
      }).map((card) => ({ text: card.textContent?.trim().slice(0, 80), rect: card.getBoundingClientRect().toJSON(), contentLeft, contentRight }));
      const clipped = cards.flatMap((card) => {
        const cardRect = card.getBoundingClientRect();
        return Array.from(card.querySelectorAll<HTMLElement>("*")).filter((child) => {
          const rect = child.getBoundingClientRect();
          return rect.width > 0 && (rect.left < cardRect.left - 1 || rect.right > cardRect.right + 1);
        }).map((child) => ({ card: card.textContent?.trim().slice(0, 80), tag: child.tagName, className: child.className, text: child.textContent?.trim().slice(0, 80) }));
      });
      return { cardCount: cards.length, outside, clipped };
    });

    expect(layout.cardCount, `${viewport.width}px governance cards`).toBeGreaterThan(0);
    expect(layout.outside, `${viewport.width}px governance cards outside main content`).toEqual([]);
    expect(layout.clipped, `${viewport.width}px governance card descendants clipped`).toEqual([]);
    await page.screenshot({ path: `test-results/mobile-governance/${viewport.width}.png`, fullPage: true });
  }
});

test("320px 登录页无横向溢出", async ({ page }) => {
  test.setTimeout(30_000);
  await installApiMocks(page, { authenticated: false });
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
  await expectNoPageOverflow(page, "320px login");
  await page.screenshot({ path: "test-results/mobile-320/login.png", fullPage: true });
});

test("320px 大字体缩放时页面不产生横向溢出", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);
  await page.setViewportSize({ width: 320, height: 720 });
  await page.addInitScript(() => {
    document.documentElement.style.fontSize = "20px";
  });

  for (const route of [
    { path: "/", name: "overview" },
    { path: "/nodes", name: "nodes" },
    { path: "/workloads/deployment/default/web", name: "workload-detail" },
    { path: "/workloads/deployment/default/web?tab=services", name: "workload-services" },
    { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
    { path: "/pods/default/web-abcdef-12345?tab=logs", name: "pod-logs" },
    { path: "/configuration?tab=secrets", name: "secrets" },
    { path: "/events", name: "events" },
    { path: "/resources", name: "resources" },
    { path: "/resources/example.io/v1/widgets", name: "resource-list" },
    { path: "/resources/example.io/v1/widgets/sample-widget?namespace=default", name: "resource-detail" },
    { path: "/governance", name: "governance" },
  ]) {
    await page.goto(route.path);
    await page.locator("main h1").waitFor({ state: "visible" });
    await expectNoPageOverflow(page, `320px large font ${route.name}`);
    await page.screenshot({ path: `test-results/mobile-320-large-font/${route.name}.png`, fullPage: true });
  }
});

test.describe("Android Chrome 视口", () => {
  test.skip(({ browserName }) => browserName !== "chromium", "Android Chrome viewport checks run in Chromium only");
  const { defaultBrowserType: _defaultBrowserType, ...pixel7 } = devices["Pixel 7"];
  test.use({
    ...pixel7,
    viewport: { width: 360, height: 800 },
  });

  test("整页宽度不超过手机视口，滚动只发生在资源表格内部", async ({ page }) => {
    test.setTimeout(120_000);
    await installApiMocks(page);

    for (const route of [
      { path: "/", name: "overview" },
      { path: "/nodes", name: "nodes" },
      { path: "/nodes/worker-0001?tab=labels", name: "node-labels" },
      { path: "/workloads/deployment/default/web", name: "workload-detail" },
      { path: "/workloads/deployment/default/web?tab=services", name: "workload-services" },
      { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
      { path: "/configuration?tab=secrets", name: "secrets" },
      { path: "/events", name: "events" },
      { path: "/audit", name: "audit" },
      { path: "/resources", name: "resources" },
      { path: "/resources/example.io/v1/widgets", name: "resource-list" },
      { path: "/resources/example.io/v1/widgets/sample-widget?namespace=default", name: "resource-detail" },
      { path: "/governance", name: "governance" },
    ]) {
      await page.goto(route.path);
      await page.locator("main h1").waitFor({ state: "visible" });
      await expectNoPageOverflow(page, `Android 360px ${route.name}`);
      await page.screenshot({ path: `test-results/mobile-android/${route.name}.png`, fullPage: true });
    }

    await page.goto("/nodes");
    await page.locator("main h1").waitFor({ state: "visible" });
    const layout = await page.evaluate(() => {
      const header = document.querySelector("header")!.getBoundingClientRect();
      const main = document.querySelector("main")!.getBoundingClientRect();
      const bodyStyle = getComputedStyle(document.body);
      return {
        viewportWidth: window.visualViewport?.width ?? window.innerWidth,
        headerRight: header.right,
        mainRight: main.right,
        textSizeAdjust: bodyStyle.getPropertyValue("text-size-adjust") || bodyStyle.getPropertyValue("-webkit-text-size-adjust"),
        bodyOverflowX: bodyStyle.overflowX,
      };
    });
    expect(layout.headerRight).toBeLessThanOrEqual(layout.viewportWidth + 1);
    expect(layout.mainRight).toBeLessThanOrEqual(layout.viewportWidth + 1);
    expect(layout.textSizeAdjust).toBe("100%");
    expect(layout.bodyOverflowX).toBe("clip");
  });

  test("平板横屏使用移动抽屉，顶部栏和污点摘要不溢出", async ({ page }) => {
    test.setTimeout(120_000);
    await installApiMocks(page);
    await page.setViewportSize({ width: 782, height: 361 });

    for (const path of ["/nodes/worker-0001", "/nodes/worker-0001?tab=labels", "/nodes/worker-0001?tab=taints"]) {
      await page.goto(path);
      await page.locator("main h1").waitFor({ state: "visible" });
      await expectNoPageOverflow(page, `Android landscape ${path}`);
    }

    await page.goto("/nodes/worker-0001");
    await page.locator("main h1").waitFor({ state: "visible" });
    const headerOverflow = await page.locator("header").evaluate((header) => {
      const bounds = header.getBoundingClientRect();
      return Array.from(header.querySelectorAll<HTMLElement>("*"))
        .map((element) => ({ element, rect: element.getBoundingClientRect() }))
        .filter(({ rect }) => rect.width > 0 && rect.right > bounds.right + 1)
        .map(({ element, rect }) => ({ tag: element.tagName, right: rect.right, className: String(element.className).slice(0, 80) }));
    });
    expect(headerOverflow).toEqual([]);

    const summary = page.getByTestId("node-taints-summary");
    const summaryOverflow = await summary.evaluate((element) => ({ clientWidth: element.clientWidth, scrollWidth: element.scrollWidth }));
    expect(summaryOverflow.scrollWidth).toBeLessThanOrEqual(summaryOverflow.clientWidth + 1);

    await expect(page.getByRole("button", { name: "打开导航" })).toBeVisible();
    const aside = await page.locator("aside").boundingBox();
    expect(aside).not.toBeNull();
    expect(aside!.x + aside!.width).toBeLessThanOrEqual(1);
    await page.screenshot({ path: "test-results/mobile-android/tablet-landscape.png", fullPage: false });
  });
});

test("移动端关键弹窗与确认框保持在视口内", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);
  await page.setViewportSize({ width: 390, height: 844 });

  await page.goto("/pods/default/web-abcdef-12345");
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  let dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "YAML dialog");
  await expectNoPageOverflow(page, "YAML dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/yaml-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/configuration");
  await page.getByRole("button", { name: "app-config", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "app-config" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "ConfigMap dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/configmap-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/configuration?tab=secrets");
  await openResourceMenu(page, "Secret default/app-secret");
  await page.getByRole("menuitem", { name: "查看数据" }).click();
  dialog = page.getByRole("dialog", { name: "app-secret" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "Secret reveal dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/secret-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/audit");
  await page.getByRole("cell", { name: "alice@example.test" }).click();
  dialog = page.getByRole("dialog", { name: "审计记录 #42" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "audit detail dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/audit-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/nodes/worker-0001?tab=labels");
  await page.getByRole("button", { name: "添加标签" }).click();
  dialog = page.getByRole("dialog", { name: "添加节点标签" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "node label dialog");
  await expectNoPageOverflow(page, "node label dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/node-label-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/workloads");
  await page.getByRole("button", { name: "创建 Deployment" }).click();
  dialog = page.getByRole("dialog", { name: "创建 Deployment" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "deployment create dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/deployment-create-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/resources/example.io/v1/widgets?namespace=default");
  await page.getByRole("button", { name: "创建 YAML" }).click();
  dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("tab", { name: "表单" }).click();
  await expect(dialog.getByRole("heading", { name: "基本信息" })).toBeVisible();
  await expectElementInsideViewport(page, dialog, "generic resource form dialog");
  await expectNoPageOverflow(page, "generic resource form dialog");
  const clippedFormContent = await dialog.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    return Array.from(element.querySelectorAll<HTMLElement>("*"))
      .map((child) => ({ child, rect: child.getBoundingClientRect() }))
      .filter(({ child, rect }) => rect.width > 0 && getComputedStyle(child).visibility !== "hidden" && (rect.left < bounds.left - 1 || rect.right > bounds.right + 1))
      .map(({ child, rect }) => ({ tag: child.tagName, className: String(child.className).slice(0, 100), left: rect.left, right: rect.right, dialogLeft: bounds.left, dialogRight: bounds.right }));
  });
  expect(clippedFormContent).toEqual([]);
  await page.screenshot({ path: "test-results/mobile-interactions/generic-resource-form-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.goto("/workloads/deployment/default/web");
  await page.getByRole("button", { name: "扩缩容", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "调整副本数" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "workload scale dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/workload-scale-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "更新镜像", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "更新容器镜像" });
  await expect(dialog).toBeVisible();
  await expectElementInsideViewport(page, dialog, "workload image dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/workload-image-dialog.png", fullPage: true });
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "删除", exact: true }).click();
  const deleteAlert = page.getByRole("alertdialog", { name: "删除 web？" });
  await expect(deleteAlert).toBeVisible();
  await expectElementInsideViewport(page, deleteAlert, "workload delete dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/workload-delete-dialog.png", fullPage: true });
  await deleteAlert.getByRole("button", { name: "取消" }).click();

  await page.goto("/nodes/worker-0001");
  await page.getByRole("button", { name: "停止调度" }).click();
  const alert = page.getByRole("alertdialog", { name: "停止节点调度？" });
  await expect(alert).toBeVisible();
  await expectElementInsideViewport(page, alert, "node scheduling dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/node-scheduling-dialog.png", fullPage: true });
  await page.getByRole("button", { name: "取消" }).click();
});

test("移动端节点排空与回滚预览不产生横向溢出", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/nodes/worker-0001");
    await page.getByRole("button", { name: "排空节点" }).click();
    let dialog = page.getByRole("dialog", { name: "排空节点 worker-0001" });
    await expect(dialog).toBeVisible();
    await expectElementInsideViewport(page, dialog, `${viewport.width}px node drain dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px node drain dialog`);
    await page.screenshot({ path: `test-results/mobile-interactions/node-drain-${viewport.width}.png`, fullPage: true });
    await page.keyboard.press("Escape");

    await page.goto("/workloads/deployment/default/web?tab=history");
    await page.getByRole("row").filter({ hasText: "#1" }).getByRole("button", { name: "回滚" }).click();
    dialog = page.getByRole("alertdialog", { name: "回滚到 revision 1？" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel("回滚变更预览")).toBeVisible();
    await expectElementInsideViewport(page, dialog, `${viewport.width}px rollback dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px rollback dialog`);
    await page.screenshot({ path: `test-results/mobile-interactions/rollback-${viewport.width}.png`, fullPage: true });
    await page.keyboard.press("Escape");

    await page.goto("/helm/releases/default/web?namespace=default&tab=history");
    await page.getByRole("button", { name: "回滚到此版本" }).click();
    dialog = page.getByRole("dialog", { name: "受控回滚 Helm Release" });
    await expect(dialog).toBeVisible();
    await expectElementInsideViewport(page, dialog, `${viewport.width}px Helm rollback dialog`);
    await expectNoPageOverflow(page, `${viewport.width}px Helm rollback dialog`);
    await page.screenshot({ path: `test-results/mobile-interactions/helm-rollback-${viewport.width}.png`, fullPage: true });
    await page.keyboard.press("Escape");
  }
});

test("命名空间筛选写入 URL 并在刷新后恢复", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/namespaces?q=production");

  await expect(page.getByRole("cell", { name: "production", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "default", exact: true })).toHaveCount(0);
  await expect(page.getByPlaceholder("搜索命名空间")).toHaveValue("production");

  await page.getByPlaceholder("搜索命名空间").fill("default");
  await expect(page).toHaveURL(/\/namespaces\?q=default$/);
  await page.reload();
  await expect(page.getByRole("cell", { name: "default", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "production", exact: true })).toHaveCount(0);
});

test("命名空间详情展示资源配额与限制范围", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/namespaces?q=default");
  await page.getByRole("link", { name: "default", exact: true }).click();

  await expect(page).toHaveURL(/\/namespaces\/default$/);
  await expect(page.getByRole("heading", { name: "default" })).toBeVisible();
  await expect(page.getByText("1 个 ResourceQuota")).toBeVisible();
  await expect(page.getByText("platform")).toBeVisible();

  await page.getByRole("tab", { name: "资源配额 (1)" }).click();
  await expect(page.getByText("requests.cpu", { exact: true })).toBeVisible();
  await expect(page.getByText("compute", { exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "2", exact: true })).toBeVisible();

  await page.getByRole("tab", { name: "限制范围 (1)" }).click();
  await expect(page.getByText("container-defaults", { exact: true })).toBeVisible();
  await expect(page.getByText("cpu=100m", { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("tab", { name: "限制范围 (1)" })).toHaveAttribute("aria-selected", "true");
});

test("命名空间支持输入名称确认后级联删除", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/namespaces/production");
  await expect(page.getByRole("heading", { name: "production", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "删除命名空间" }).click();
  const dialog = page.getByRole("alertdialog", { name: /删除命名空间 production/ });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "确认级联删除" })).toBeDisabled();
  await dialog.getByLabel("请输入命名空间名称确认").fill("production");
  const deleteRequest = page.waitForRequest((request) => request.method() === "DELETE" && new URL(request.url()).pathname === "/api/v1/namespaces/production");
  await dialog.getByRole("button", { name: "确认级联删除" }).click();
  const request = await deleteRequest;
  expect(request.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(new URL(request.url()).searchParams.get("resourceVersion")).toBe("17");
  await expect(page).toHaveURL(/\/namespaces$/);
});

test("存储筛选写入 URL 并在刷新后恢复", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/storage?q=logs");

  await expect(page.getByRole("cell", { name: "logs-fast", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "data-standard", exact: true })).toHaveCount(0);
  await expect(page.getByPlaceholder("搜索存储资源")).toHaveValue("logs");

  await page.getByPlaceholder("搜索存储资源").fill("data");
  await expect(page).toHaveURL(/\/storage\?q=data$/);
  await page.reload();
  await expect(page.getByRole("cell", { name: "data-standard", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "logs-fast", exact: true })).toHaveCount(0);
});

test("存储标签可深链并在刷新后保持", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/storage?tab=claims");

  await expect(page.getByRole("tab", { name: /PVC/ })).toHaveAttribute("aria-selected", "true");
  await page.reload();
  await expect(page.getByRole("tab", { name: /PVC/ })).toHaveAttribute("aria-selected", "true");
});

test("StorageClass 详情展示存储参数并支持事件深链", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/storage/classes/data-standard?tab=events");

  await expect(page.getByRole("heading", { name: "data-standard", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "事件", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText("该资源暂无事件")).toBeVisible();

  await page.getByRole("tab", { name: "概览", exact: true }).click();
  await expect(page).toHaveURL(/\/storage\/classes\/data-standard$/);
  await expect(page.getByText("premium-rwo", { exact: true })).toBeVisible();
  await expect(page.getByText("discard", { exact: true })).toBeVisible();
});

test("PersistentVolume 详情展示容量与绑定 PVC", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/storage/volumes/pv-data");

  await expect(page.getByRole("heading", { name: "pv-data", exact: true })).toBeVisible();
  await expect(page.getByText("10 GiB").first()).toBeVisible();
  await expect(page.getByRole("link", { name: "production/data-claim", exact: true })).toHaveAttribute("href", "/storage/claims/production/data-claim");
  await expect(page.getByText("csi.example.test", { exact: true })).toBeVisible();
});

test("PersistentVolumeClaim 详情展示申请容量、PV 和状态条件", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/storage/claims/production/data-claim");

  await expect(page.getByRole("heading", { name: "data-claim", exact: true })).toBeVisible();
  await expect(page.getByText("5.0 GiB").first()).toBeVisible();
  await expect(page.getByRole("link", { name: "pv-data", exact: true })).toHaveAttribute("href", "/storage/volumes/pv-data");
  await expect(page.getByText(/volume expansion in progress/)).toBeVisible();

  await page.getByRole("tab", { name: "YAML", exact: true }).click();
  await expect(page).toHaveURL(/tab=yaml/);
});

test("网络标签写入 URL 并在刷新后保持", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/network");

  await expect(page.getByRole("columnheader", { name: "Endpoint" })).toBeVisible();
  await expect(page.getByRole("cell", { name: "1", exact: true })).toBeVisible();
  await page.getByRole("tab", { name: /Ingress/ }).click();
  await expect(page).toHaveURL(/tab=ingresses/);
  await expect(page.getByRole("tab", { name: /Ingress/ })).toHaveAttribute("aria-selected", "true");
  await page.reload();
  await expect(page.getByRole("tab", { name: /Ingress/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("cell", { name: "web", exact: true }).first()).toBeVisible();
  await expect(page.getByText("Ready", { exact: true })).toBeVisible();
});

test("Service 详情展示 Endpoint 就绪状态并支持事件深链", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/network/services/default/web?tab=endpoints");

  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: /Endpoint/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("cell", { name: "10.244.1.10", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "web-abcdef-12345", exact: true })).toBeVisible();

  await page.getByRole("tab", { name: "事件", exact: true }).click();
  await expect(page).toHaveURL(/tab=events/);
  await expect(page.getByText("该资源暂无事件")).toBeVisible();
});

test("Ingress 详情展示默认后端与 TLS Secret", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/network/ingresses/default/web?tab=routes");

  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "默认后端", exact: true })).toBeVisible();
  await expect(page.getByText("web.example.test").first()).toBeVisible();

  await page.getByRole("tab", { name: "概览", exact: true }).click();
  await expect(page.getByText("web-tls", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "YAML", exact: true }).click();
  await expect(page).toHaveURL(/tab=yaml/);
});

test("没有 IngressClass 时显示未安装 Ingress Controller", async ({ page }) => {
  await installApiMocks(page, { noIngressController: true });
  await page.goto("/network?tab=ingresses");

  await expect(page.getByText("未安装 Ingress Controller", { exact: true })).toBeVisible();
  await expect(page.getByText(/当前集群没有 IngressClass/)).toBeVisible();
});

test("配置标签写入 URL 并在刷新后保持", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/configuration");

  await page.getByRole("tab", { name: /Secret/ }).click();
  await expect(page).toHaveURL(/tab=secrets/);
  await expect(page.getByRole("tab", { name: /Secret/ })).toHaveAttribute("aria-selected", "true");
  await page.reload();
  await expect(page.getByRole("tab", { name: /Secret/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("cell", { name: "app-secret", exact: true })).toBeVisible();
});

test("Secret 数据查看解码文本并保留二进制 Base64", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/configuration?tab=secrets");

  await openResourceMenu(page, "Secret default/app-secret");
  await page.getByRole("menuitem", { name: "查看数据" }).click();

  const token = page.locator('pre[aria-label="token 值"]');
  await expect(token).toContainText("token=abc");
  await expect(token).not.toContainText("dG9rZW49YWJjCg==");
  await expect(page.getByText("Base64（二进制）")).toBeVisible();
  await expect(page.locator('pre[aria-label="blob 值"]')).toHaveText("AP8K");
});

test("Secret YAML 展示原始数据并允许编辑", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/configuration?tab=secrets");

  await openResourceMenu(page, "Secret default/app-secret");
  await page.getByRole("menuitem", { name: "查看 YAML" }).click();

  const dialog = page.getByRole("dialog", { name: "app-secret" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("可编辑 YAML")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "编辑" })).toBeVisible();
  await expect(dialog.getByText("dG9rZW49YWJjCg==", { exact: false })).toBeVisible();
  await expect(dialog.getByText(/脱敏/)).toHaveCount(0);
});

test("暗色主题可切换、刷新后保持并使用暗色语义 token", async ({ page }) => {
  await installApiMocks(page);
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/nodes");

  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await page.getByRole("button", { name: "切换为深色主题" }).click();

  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(page.getByRole("button", { name: "切换为浅色主题" })).toHaveAttribute("aria-pressed", "true");
  await expect.poll(() => page.evaluate(() => localStorage.getItem("dosh-theme"))).toBe("dark");
  await expect.poll(() => page.evaluate(() => document.documentElement.style.colorScheme)).toBe("dark");
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--background").trim())).toBe("222 28% 9%");

  await page.reload();

  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(page.getByRole("button", { name: "切换为浅色主题" })).toBeVisible();
});

test("未保存主题选择时跟随系统暗色偏好", async ({ page }) => {
  await installApiMocks(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto("/nodes");

  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(page.getByRole("button", { name: "切换为浅色主题" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem("dosh-theme"))).toBeNull();
});

test("登录页在亮暗主题下无严重可访问性问题", async ({ page }) => {
  await installApiMocks(page, { authenticated: false });
  for (const theme of ["light", "dark"] as const) {
    await page.goto("/login");
    await page.evaluate((value) => localStorage.setItem("dosh-theme", value), theme);
    await page.reload();
    await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
    await expectThemeApplied(page, theme);
    await expectNoSeriousAccessibilityViolations(page, `login/${theme}`);
  }
});

test("核心运维页面在亮暗主题下无严重可访问性问题", async ({ page }) => {
  test.setTimeout(480_000);
  await installApiMocks(page);
  for (const theme of ["light", "dark"] as const) {
    for (const path of ["/", "/nodes", "/workloads/deployment/default/web", "/workloads/deployment/default/web?tab=config", "/pods/default/web-abcdef-12345", "/pods/default/web-abcdef-12345?tab=terminal", "/audit", "/access", "/helm/releases?namespace=default", "/helm/releases/default/web?namespace=default", "/resources", "/resources/example.io/v1/widgets", "/resources/example.io/v1/widgets/sample-widget?namespace=default"]) {
      await page.goto(path);
      await page.evaluate((value) => localStorage.setItem("dosh-theme", value), theme);
      await page.reload();
      await expect(page.getByRole("heading").first()).toBeVisible();
      await expectThemeApplied(page, theme);
      await expectNoSeriousAccessibilityViolations(page, `${path}/${theme}`);
    }
  }
});

test("通用资源动态表单在亮暗主题下无严重可访问性问题", async ({ page }) => {
  test.setTimeout(120_000);
  await installApiMocks(page);
  for (const theme of ["light", "dark"] as const) {
    await page.goto("/resources/example.io/v1/widgets?namespace=default");
    await page.evaluate((value) => localStorage.setItem("dosh-theme", value), theme);
    await page.reload();
    await expect(page.getByRole("heading", { name: "Widget", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "创建 YAML" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("tab", { name: "表单" }).click();
    await expect(dialog.getByRole("heading", { name: "基本信息" })).toBeVisible();
    await expectThemeApplied(page, theme);
    await expectNoSeriousAccessibilityViolations(page, `generic-resource-form/${theme}`);
    await page.keyboard.press("Escape");
  }
});

test("仅用键盘完成登录、导航、扩容确认和退出", async ({ page }) => {
  await installApiMocks(page, { authenticated: false });
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
  await expect(page.getByLabel("用户名")).toBeFocused();
  await page.keyboard.type("admin");
  await page.keyboard.press("Tab");
  await page.keyboard.type("e2e-password");
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "登录", exact: true })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();

  await page.keyboard.press("Control+K");
  const searchDialog = page.getByRole("dialog", { name: "搜索集群资源" });
  await expect(searchDialog).toBeVisible();
  await expect(searchDialog.getByPlaceholder("输入资源名称…")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(searchDialog).toBeHidden();

  await pressTabUntil(page, "workload navigation link", (focus) => focus.tag === "A" && focus.text === "工作负载");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "工作负载" })).toBeVisible();

  await pressTabUntil(
    page,
    "workload search input",
    (focus) => focus.tag === "INPUT" && focus.placeholder === "搜索工作负载名称",
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.type("web");
  await expect(page).toHaveURL(/[?&]q=web(?:&|$)/);
  const webLink = page.getByRole("link", { name: "web", exact: true });
  await expect(webLink).toBeVisible();
  await pressTabUntil(page, "web workload link", (focus) => focus.tag === "A" && focus.text === "web");
  await expectVisibleFocusRing(page);
  await expect(webLink).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "web", exact: true })).toBeVisible();

  await pressTabUntil(
    page,
    "scale button",
    (focus) => (focus.tag === "BUTTON" || focus.role === "button") && focus.text === "扩缩容",
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "调整副本数" })).toBeVisible();
  await pressTabUntil(page, "replica input", (focus) => focus.tag === "INPUT" && focus.id === "replicas");
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Control+A");
  await page.keyboard.type("3");
  await pressTabUntil(
    page,
    "scale confirm button",
    (focus) => (focus.tag === "BUTTON" || focus.role === "button") && focus.text === "确认调整",
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await expect(page.getByText("副本数已调整为 3", { exact: true })).toBeVisible();

  await pressTabUntil(
    page,
    "admin user menu",
    (focus) => (focus.tag === "BUTTON" || focus.role === "button") && focus.text.includes("admin"),
  );
  await expectVisibleFocusRing(page);
  await page.keyboard.press("Enter");
  await pressTabUntil(page, "logout menu item", (focus) => focus.role === "menuitem" && focus.text === "退出登录");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "登录控制台" })).toBeVisible();
});
