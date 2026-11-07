import { devices, expect, test, type Page, type Route } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type {
  ListResponse,
  IngressDetail,
  NodeDetail,
  NodeSummary,
  Overview,
  PodDetail,
  PodSummary,
  PersistentVolumeClaimDetail,
  PersistentVolumeDetail,
  ServerCertificate,
  ServiceDetail,
  SessionResponse,
  StorageClassDetail,
  WorkloadDetail,
  WorkloadSummary,
} from "../src/api/types";

const csrfToken = "e2e-csrf-token";

function logFrames(...lines: string[]): string {
  return `${lines.map((line) => JSON.stringify({ type: "log", data: `${line}\n` })).join("\n")}\n{"type":"end"}\n`;
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
      name: "web",
      image: "nginx:alpine",
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
  return { items, meta: { total: items.length, page: 1, pageSize: Math.max(items.length, 1), totalPages: items.length ? 1 : 0, filters: {} } };
}

async function openResourceMenu(page: Page, resource: string) {
  await page.getByRole("button", { name: `操作 ${resource}` }).click();
}

type NodeSchedulingRequest = { method: string; path: string; csrf: string | null };
type NodeMetadataRequest = { method: string; path: string; csrf: string | null; body: Record<string, unknown> };

async function installApiMocks(page: Page, options: { authenticated?: boolean; authMode?: "local" | "token"; overview?: Overview; overviewStatus?: number; nodeSchedulingRequests?: NodeSchedulingRequest[]; nodeMetadataRequests?: NodeMetadataRequest[]; expireSessionOnNodes?: boolean; noIngressController?: boolean; podName?: string } = {}) {
  const authenticated = options.authenticated ?? true;
  const authMode = options.authMode ?? "local";
  const sessionResponse: SessionResponse = { ...session, authMode };
  const listedPod = options.podName ? { ...podSummary, name: options.podName } : podSummary;
  let signedIn = authenticated;
  let nodeSchedulingDisabled = false;
  let nodeLabels = { ...nodeDetail.labels };
  let nodeTaints = [...nodeDetail.taints];
  let nodeResourceVersion = 1;
  let services: ServiceDetail[] = [{ ...serviceDetail }];
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
    if (path === "/api/v1/overview") return json(route, options.overview ?? overview, options.overviewStatus ?? 200);
    if (path === "/api/v1/capabilities") return json(route, session.capabilities);
    if (path === "/api/v1/nodes") return json(route, list([{ ...nodeSummary, unschedulable: nodeSchedulingDisabled }]));
    if (path === "/api/v1/nodes/worker-0001") return json(route, currentNodeDetail());
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
    if (path === "/api/v1/workloads/deployment/default/web/scale" && method === "POST") {
      return json(route, { status: "ok" });
    }
    if (path === "/api/v1/workloads/deployment/default/web/image" && method === "POST") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/workloads/deployment/default/web/containers/web/config" && method === "PUT") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/workloads/cronjob/default/backup" && method === "GET") return json(route, cronJobDetail);
    if (path === "/api/v1/workloads/cronjob/default/backup/suspend" && method === "POST") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/pods") return json(route, list([listedPod]));
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
    if (path === "/api/v1/yaml") return json(route, {
      kind: "Pod",
      namespace: "default",
      name: "web-abcdef-12345",
      yaml: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-abcdef-12345\n",
    });
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

  const webCard = page.locator(".rounded-lg.border.bg-card").filter({ has: page.getByRole("link", { name: "web", exact: true }) });
  await webCard.getByRole("button", { name: "编辑" }).click();
  const editDialog = page.getByRole("dialog", { name: "编辑 Service" });
  await expect(editDialog).toBeVisible();
  const updateRequest = page.waitForRequest((request) => request.method() === "PUT" && new URL(request.url()).pathname === "/api/v1/services/default/web");
  await editDialog.getByRole("button", { name: "保存修改" }).click();
  const updated = await updateRequest;
  expect(updated.headers()["x-csrf-token"]).toBe(csrfToken);
  expect(updated.postDataJSON()).toMatchObject({ resourceVersion: "42", selector: { app: "web" } });

  await webCard.getByRole("button", { name: "删除" }).click();
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
  test.setTimeout(180_000);
  await installApiMocks(page);
  await page.setViewportSize({ width: 390, height: 844 });
  const routes = [
    { path: "/", name: "overview" },
    { path: "/nodes", name: "nodes" },
    { path: "/nodes/worker-0001", name: "node-detail" },
    { path: "/nodes/worker-0001?tab=labels", name: "node-labels" },
    { path: "/nodes/worker-0001?tab=taints", name: "node-taints" },
    { path: "/workloads", name: "workloads" },
    { path: "/workloads/deployment/default/web", name: "workload-detail" },
    { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
    { path: "/workloads/deployment/default/web?tab=logs", name: "workload-logs" },
    { path: "/workloads/deployment/default/web?tab=yaml", name: "workload-yaml" },
    { path: "/pods", name: "pods" },
    { path: "/pods/default/web-abcdef-12345", name: "pod-detail" },
    { path: "/pods/default/web-abcdef-12345?tab=logs", name: "pod-logs" },
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

  await page.getByRole("button", { name: "搜索" }).click();
  const searchDialog = page.getByRole("dialog", { name: "搜索集群资源" });
  await expect(searchDialog).toBeVisible();
  await searchDialog.getByPlaceholder("输入资源名称…").fill("web");
  await expect(searchDialog.getByRole("button", { name: /web-abcdef-12345/ })).toBeVisible();
  await expectElementInsideViewport(page, searchDialog, "global search dialog");
  await expectNoPageOverflow(page, "global search dialog");
  await page.screenshot({ path: "test-results/mobile-interactions/global-search.png", fullPage: true });
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
    { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
    { path: "/pods/default/web-abcdef-12345?tab=logs", name: "pod-logs" },
    { path: "/configuration?tab=certificate", name: "certificate" },
    { path: "/events", name: "events" },
    { path: "/audit", name: "audit" },
  ]) {
    await page.goto(route.path);
    await page.locator("main h1").waitFor({ state: "visible" });
    await expectNoPageOverflow(page, `320px ${route.name}`);
    await page.screenshot({ path: `test-results/mobile-320/${route.name}.png`, fullPage: true });
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
    { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
    { path: "/pods/default/web-abcdef-12345?tab=logs", name: "pod-logs" },
    { path: "/configuration?tab=secrets", name: "secrets" },
    { path: "/events", name: "events" },
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
      { path: "/workloads/deployment/default/web?tab=config", name: "workload-config" },
      { path: "/configuration?tab=secrets", name: "secrets" },
      { path: "/events", name: "events" },
      { path: "/audit", name: "audit" },
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
  await page.getByRole("menuitem", { name: "查看明文" }).click();
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

test("Secret 明文查看解码文本并保留二进制 Base64", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/configuration?tab=secrets");

  await openResourceMenu(page, "Secret default/app-secret");
  await page.getByRole("menuitem", { name: "查看明文" }).click();

  const token = page.locator('pre[aria-label="token 值"]');
  await expect(token).toContainText("token=abc");
  await expect(token).not.toContainText("dG9rZW49YWJjCg==");
  await expect(page.getByText("Base64（二进制）")).toBeVisible();
  await expect(page.locator('pre[aria-label="blob 值"]')).toHaveText("AP8K");
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
  test.setTimeout(120_000);
  await installApiMocks(page);
  for (const theme of ["light", "dark"] as const) {
    for (const path of ["/", "/nodes", "/workloads/deployment/default/web", "/workloads/deployment/default/web?tab=config", "/pods/default/web-abcdef-12345", "/audit"]) {
      await page.goto(path);
      await page.evaluate((value) => localStorage.setItem("dosh-theme", value), theme);
      await page.reload();
      await expect(page.getByRole("heading").first()).toBeVisible();
      await expectThemeApplied(page, theme);
      await expectNoSeriousAccessibilityViolations(page, `${path}/${theme}`);
    }
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
  await pressTabUntil(page, "web workload link", (focus) => focus.tag === "A" && focus.text === "web");
  await expectVisibleFocusRing(page);
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
