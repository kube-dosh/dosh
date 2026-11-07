import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { Toaster } from "sonner";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { routeTree } from "@/router";
import type { IngressDetail, NodeDetail, Overview, PersistentVolumeClaimDetail, PersistentVolumeDetail, PodDetail, ServiceDetail, SessionResponse, StorageClassDetail, WorkloadDetail, WorkloadSummary } from "@/api/types";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ThemeProvider } from "@/lib/theme";

const session: SessionResponse = {
  user: { username: "admin", admin: true, groups: ["dosh-admins"] },
  csrfToken: "test-csrf-token",
  expiresAt: "2099-01-01T00:00:00Z",
  capabilities: {
    read: true, scale: true, restart: true, updateImage: true, suspendCronJob: true, delete: true, create: true,
    createService: true, updateService: true, deleteService: true, deleteNamespace: true, revealSecrets: true,
    editYAML: true, nodeCordon: true, reloadCertificate: true,
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
  authMode: "local",
};

function logFrames(...lines: string[]): string {
  return `${lines.map((line) => JSON.stringify({ type: "log", data: `${line}\n` })).join("\n")}\n{"type":"end"}\n`;
}

const overview: Overview = {
  cluster: { name: "kubedo-lab", version: "v1.36.5", cni: "Cilium", status: "Healthy", metricsAvailable: false },
  counts: { nodes: 8, readyNodes: 8, pods: 42, runningPods: 40, workloads: 12, namespaces: 9, unhealthyPods: 2, warningEvents: 1 },
  capacity: { cpuAllocatableMilli: 16000, memoryAllocatableBytes: 34359738368, podCapacity: 880, cpuRequestsMilli: 4200, memoryRequestsBytes: 8589934592, cpuLimitsMilli: 8000, memoryLimitsBytes: 17179869184, cpuUsageMilli: 2800, memoryUsageBytes: 5368709120 },
  recentEvents: [],
};

const workload: WorkloadSummary = {
  kind: "Deployment", name: "web", namespace: "default", status: "Running", ready: "2/2", desired: 2,
  restarts: 0, images: ["nginx:alpine"], createdAt: "2026-11-02T00:00:00Z",
};

function configuredWorkload(): WorkloadDetail {
  return {
    ...workload,
    name: "web-api",
    resourceVersion: "317754",
    conditions: [],
    selector: { app: "web" },
    pods: [],
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
}

const nodeDetail: NodeDetail = {
  name: "worker-0001", status: "Ready", roles: ["worker"], internalIP: "192.168.122.200", version: "v1.36.5",
  osImage: "Fedora", kernelVersion: "6.19", containerRuntime: "containerd://2.3.6", cpuCapacityMilli: 2000,
  memoryCapacityBytes: 4294967296, podCapacity: 110, podCount: 1, cpuRequestsMilli: 800, memoryRequestsBytes: 1073741824,
  hasMetrics: true, cpuUsageMilli: 350, memoryUsageBytes: 1073741824, unschedulable: false, createdAt: "2026-11-02T00:00:00Z",
  conditions: [{ type: "Ready", status: "True", reason: "KubeletReady" }], taints: [],
  resourceVersion: "1",
  labels: { "kubernetes.io/hostname": "worker-0001" }, annotations: {}, pods: [],
};

const pod: PodDetail = {
  name: "web-abcdef-12345", namespace: "default", status: "Running", ready: "1/1", restarts: 0,
  ip: "10.244.1.10", node: "worker-0001", ownerKind: "ReplicaSet", ownerName: "web-abcdef",
  createdAt: "2026-11-02T00:00:00Z", qosClass: "BestEffort", serviceAccount: "default", conditions: [],
  hasMetrics: true, cpuUsageMilli: 25, memoryUsageBytes: 33554432,
  containers: [{ name: "web", image: "nginx:alpine", ready: true, restartCount: 0, state: "Running", cpuRequestMilli: 100, cpuLimitMilli: 500, memoryRequestBytes: 134217728, memoryLimitBytes: 536870912 }],
};

const serviceDetail: ServiceDetail = {
  name: "web", namespace: "default", type: "ClusterIP", headless: false, clusterIP: "10.96.0.10", externalIP: "",
  ports: [{ name: "http", protocol: "TCP", port: 80, targetPort: "8080" }], selector: { app: "web" },
  endpointCount: 1, createdAt: "2026-11-02T00:00:00Z", labels: { app: "web" }, annotations: { team: "platform" },
  sessionAffinity: "None", ipFamilies: ["IPv4"], ipFamilyPolicy: "SingleStack",
  endpoints: [{ addresses: ["10.244.1.10"], ready: true, targetKind: "Pod", targetName: "web-abcdef-12345", targetNamespace: "default", nodeName: "worker-0001" }],
};

const ingressDetail: IngressDetail = {
  name: "web", namespace: "default", class: "nginx", createdAt: "2026-11-02T00:00:00Z",
  paths: [{ host: "web.example.test", path: "/", pathType: "Prefix", service: "web", port: "80", targetStatus: "Ready" }],
  tlsHosts: ["web.example.test"], labels: { app: "web" }, annotations: { "nginx.ingress.kubernetes.io/rewrite-target": "/" },
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

const server = setupServer(
  http.get("/api/v1/auth/config", () => HttpResponse.json({ mode: "local", providerName: "", loginURL: "" })),
  http.get("/api/v1/session", () => HttpResponse.json(session)),
  http.get("/api/v1/capabilities", () => HttpResponse.json(session.capabilities)),
  http.post("/api/v1/session", () => HttpResponse.json(session)),
  http.delete("/api/v1/session", () => new HttpResponse(null, { status: 204 })),
  http.get("/api/v1/overview", () => HttpResponse.json(overview)),
  http.get("/api/v1/cluster/config", () => HttpResponse.json({
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
  })),
  http.get("/api/v1/nodes", () => HttpResponse.json({ items: [{ name: "worker-0001", status: "Ready", roles: ["worker"], internalIP: "192.168.122.200", version: "v1.36.5", osImage: "Fedora", kernelVersion: "6.19", containerRuntime: "containerd://2.3.6", cpuCapacityMilli: 2000, memoryCapacityBytes: 4294967296, podCapacity: 110, podCount: 12, cpuRequestsMilli: 800, memoryRequestsBytes: 1073741824, unschedulable: false, createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1 } })),
  http.get("/api/v1/nodes/worker-0001", () => HttpResponse.json(nodeDetail)),
  http.get("/api/v1/workloads", () => HttpResponse.json({ items: [workload], meta: { total: 1 } })),
  http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"], resourceVersion: "317754", conditions: [], selector: {}, pods: [], containers: [{ name: "web", image: "nginx:alpine" }] })),
  http.post("/api/v1/workloads/deployments/preview", () => HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web-api\n" })),
  http.post("/api/v1/workloads/deployments", () => HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"] }, { status: 201 })),
  http.get("/api/v1/namespaces", () => HttpResponse.json({ items: [{ name: "default", status: "Active", createdAt: "2026-11-02T00:00:00Z" }, { name: "production", status: "Active", createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 2 } })),
  http.get("/api/v1/pods", () => HttpResponse.json({ items: [pod], meta: { total: 1 } })),
  http.get("/api/v1/pods/default/web-abcdef-12345", () => HttpResponse.json(pod)),
  http.get("/api/v1/pods/default/web-abcdef-12345/logs", () => HttpResponse.text(logFrames("hello from dosh log stream"), { headers: { "Content-Type": "application/x-ndjson; charset=utf-8" } })),
  http.get("/api/v1/services/default/web", () => HttpResponse.json(serviceDetail)),
  http.get("/api/v1/services", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
  http.get("/api/v1/ingresses/default/web", () => HttpResponse.json(ingressDetail)),
  http.get("/api/v1/ingressclasses", () => HttpResponse.json({ items: [{ name: "nginx", controller: "k8s.io/ingress-nginx", default: true, createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1 } })),
  http.get("/api/v1/storageclasses/data-standard", () => HttpResponse.json(storageClassDetail)),
  http.get("/api/v1/persistentvolumes/pv-data", () => HttpResponse.json(persistentVolumeDetail)),
  http.get("/api/v1/persistentvolumeclaims/production/data-claim", () => HttpResponse.json(persistentVolumeClaimDetail)),
  http.get("/api/v1/events", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 1, totalPages: 0, filters: {} } })),
  http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/secrets", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/certificate", () => HttpResponse.json({ mode: "https", enabled: true, configured: true, status: "valid", subject: "CN=dosh.example.test", issuer: "CN=dosh.example.test", dnsNames: ["dosh.example.test"], ipAddresses: [], daysRemaining: 90, fingerprintSha256: "A".repeat(64), keyAlgorithm: "ECDSA", autoReload: true, reloadIntervalSeconds: 30, lastReloadedAt: "2026-11-05T00:00:00Z" })),
  http.get("/api/v1/audit-events", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/yaml", () => HttpResponse.json({ kind: "Pod", namespace: "default", name: "web-abcdef-12345", yaml: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-abcdef-12345\n" })),
  http.post("/api/v1/yaml/apply", () => HttpResponse.json({ kind: "Deployment", namespace: "default", name: "web", yaml: "" })),
);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
beforeEach(() => localStorage.clear());

function renderRoute(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createRouter({ routeTree, history, defaultPreload: false });
  render(<ThemeProvider><QueryClientProvider client={queryClient}><TooltipProvider><RouterProvider router={router} /><Toaster /></TooltipProvider></QueryClientProvider></ThemeProvider>);
  return router;
}

async function expectNodeLink() {
  await waitFor(() => expect(screen.getByRole("link", { name: "worker-0001" })).toBeInTheDocument(), { timeout: 10000 });
}

function openResourceMenu(name: string) {
  const trigger = screen.getByRole("button", { name: `操作 ${name}` });
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: "mouse" });
  fireEvent.pointerUp(trigger, { button: 0, ctrlKey: false, pointerType: "mouse" });
  fireEvent.click(trigger);
}

describe("dosh application", () => {
  it("uses the OIDC logout endpoint for enterprise sessions", async () => {
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, authMode: "oidc" })),
    );
    renderRoute("/");

    expect(await screen.findByRole("heading", { name: "集群总览" })).toBeInTheDocument();
    const userMenu = screen.getByRole("button", { name: /admin/i });
    fireEvent.pointerDown(userMenu, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.pointerUp(userMenu, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(userMenu);
    const logout = await screen.findByRole("menuitem", { name: "退出登录" });
    expect(logout.tagName).toBe("A");
    expect(logout).toHaveAttribute("href", "/api/v1/auth/oidc/logout");
    expect(screen.getByText("企业身份")).toBeInTheDocument();
  });

  it("logs in and renders the cluster overview", async () => {
    let authenticated = false;
    server.use(
      http.get("/api/v1/session", () => authenticated ? HttpResponse.json(session) : HttpResponse.json({ code: "unauthorized", message: "authentication required" }, { status: 401 })),
      http.post("/api/v1/session", () => { authenticated = true; return HttpResponse.json(session); }),
    );
    renderRoute("/login");
    expect(await screen.findByText("登录控制台")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("用户名"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "a-valid-test-password" } });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByText("集群总览")).toBeInTheDocument();
    expect(screen.getByText("8/8")).toBeInTheDocument();
  });

  it("logs in with a Kubernetes bearer token without persisting it", async () => {
    let authenticated = false;
    const tokenSession = { ...session, authMode: "token" as const };
    server.use(
      http.get("/api/v1/auth/config", () => HttpResponse.json({ mode: "token", providerName: "Kubernetes" })),
      http.get("/api/v1/session", () => authenticated ? HttpResponse.json(tokenSession) : HttpResponse.json({ code: "unauthorized", message: "authentication required" }, { status: 401 })),
      http.post("/api/v1/session", async ({ request }) => {
        const body = await request.json() as { token?: string };
        if (body.token !== "valid-kubernetes-token") return HttpResponse.json({ code: "unauthorized", message: "authentication required" }, { status: 401 });
        authenticated = true;
        return HttpResponse.json(tokenSession);
      }),
    );
    renderRoute("/login");

    expect(await screen.findByRole("heading", { name: "Kubernetes Token 登录" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Bearer Token"), { target: { value: "valid-kubernetes-token" } });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByText("集群总览")).toBeInTheDocument();
    expect(localStorage.getItem("valid-kubernetes-token")).toBeNull();
    const userMenu = screen.getByRole("button", { name: /admin/i });
    fireEvent.pointerDown(userMenu, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.pointerUp(userMenu, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(userMenu);
    expect(screen.getByText("Kubernetes Token")).toBeInTheDocument();
  });

  it("renders node inventory", async () => {
    renderRoute("/nodes");
    await expectNodeLink();
    expect(screen.getByText("192.168.122.200")).toBeInTheDocument();
  });

  it("exposes YAML creation from the quick entry navigation", async () => {
    renderRoute("/");

    expect(await screen.findByRole("heading", { name: "集群总览" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "YAML 创建" })).toHaveAttribute("href", "/yaml/create");
  });

  it("renders cluster network ranges and links node Pod CIDRs", async () => {
    renderRoute("/cluster/config");

    expect(await screen.findByRole("heading", { name: "集群配置" })).toBeInTheDocument();
    expect(screen.getByText("10.244.0.0/16")).toBeInTheDocument();
    expect(screen.getByText("10.96.0.0/12")).toBeInTheDocument();
    expect(screen.getByText("192.168.122.50:16443")).toBeInTheDocument();
    expect(screen.getByText("10.244.1.0/24")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "worker-0001" })).toHaveAttribute("href", "/nodes/worker-0001");
  });

  it("dry-runs and applies a YAML resource from the create page", async () => {
    const requests: Array<{ yaml: string; dryRun: boolean }> = [];
    server.use(http.post("/api/v1/yaml/apply", async ({ request }) => {
      const body = await request.json() as { yaml: string; dryRun: boolean };
      requests.push(body);
      return HttpResponse.json({ kind: "Deployment", namespace: "default", name: "web", yaml: "" });
    }));
    renderRoute("/yaml/create");

    expect(await screen.findByRole("heading", { name: "YAML 创建" })).toBeInTheDocument();
    const dryRun = screen.getByRole("button", { name: "Dry Run" });
    await waitFor(() => expect(dryRun).toBeEnabled());
    fireEvent.click(dryRun);

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0]).toMatchObject({ dryRun: true });
    expect(requests[0].yaml).toContain("kind: Deployment");
    expect(await screen.findByRole("status", { name: "Dry Run 校验结果" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "创建 / 应用" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[1]).toMatchObject({ dryRun: false });
    expect(await screen.findByRole("status", { name: "YAML 应用结果" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看工作负载" })).toHaveAttribute("href", "/workloads/deployment/default/web");
  });

  it("dry-runs and sequentially applies multiple YAML documents", async () => {
    const requests: Array<{ yaml: string; dryRun: boolean }> = [];
    server.use(http.post("/api/v1/yaml/apply", async ({ request }) => {
      const body = await request.json() as { yaml: string; dryRun: boolean };
      requests.push(body);
      const kind = body.yaml.match(/^kind:\s*(\S+)/m)?.[1] ?? "Resource";
      const name = body.yaml.match(/^  name:\s*(\S+)/m)?.[1] ?? "resource";
      return HttpResponse.json({ kind, namespace: "default", name, yaml: body.yaml });
    }));
    renderRoute("/yaml/create");

    expect(await screen.findByRole("heading", { name: "YAML 创建" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "组合示例" }));
    expect(await screen.findByText("3 个有效文档")).toBeInTheDocument();
    const dryRun = screen.getByRole("button", { name: "Dry Run" });
    await waitFor(() => expect(dryRun).toBeEnabled());
    fireEvent.click(dryRun);

    await waitFor(() => expect(requests).toHaveLength(3));
    expect(requests.map((request) => request.dryRun)).toEqual([true, true, true]);
    expect(requests.map((request) => request.yaml.match(/^kind:\s*(\S+)/m)?.[1])).toEqual(["ConfigMap", "Deployment", "Service"]);
    expect(await screen.findByRole("status", { name: "Dry Run 校验结果" })).toHaveTextContent("3/3 成功");

    fireEvent.click(screen.getByRole("button", { name: "创建 / 应用 3 个资源" }));
    await waitFor(() => expect(requests).toHaveLength(6));
    expect(requests.slice(3).map((request) => request.dryRun)).toEqual([false, false, false]);
    expect(await screen.findByRole("status", { name: "YAML 应用结果" })).toHaveTextContent("3/3 成功");
  });

  it("restores the namespace filter from the URL", async () => {
    server.use(http.get("/api/v1/namespaces", ({ request }) => {
      const q = new URL(request.url).searchParams.get("q") ?? "";
      const items = [
        { name: "default", status: "Active", createdAt: "2026-11-02T00:00:00Z" },
        { name: "production", status: "Active", createdAt: "2026-11-02T00:00:00Z" },
      ].filter((item) => item.name.includes(q));
      return HttpResponse.json({ items, meta: { total: items.length } });
    }));
    renderRoute("/namespaces?q=production");

    expect(await screen.findByRole("cell", { name: "production" })).toBeInTheDocument();
    expect(screen.queryByRole("cell", { name: "default" })).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText("搜索命名空间")).toHaveValue("production");
  });

  it("creates a namespace from the namespace table", async () => {
    let createBody: Record<string, unknown> | undefined;
    let csrf: string | null = null;
    server.use(http.post("/api/v1/namespaces", async ({ request }) => {
      createBody = await request.json() as Record<string, unknown>;
      csrf = request.headers.get("x-csrf-token");
      return HttpResponse.json({ name: "preview", status: "Active", resourceVersion: "8", labels: { team: "platform" }, annotations: { owner: "ops" }, createdAt: "2026-11-02T00:00:00Z" }, { status: 201 });
    }));
    renderRoute("/namespaces");

    fireEvent.click(await screen.findByRole("button", { name: "创建命名空间" }));
    const dialog = await screen.findByRole("dialog", { name: "创建命名空间" });
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "preview" } });
    fireEvent.change(within(dialog).getByLabelText("Labels"), { target: { value: "team=platform" } });
    fireEvent.change(within(dialog).getByLabelText("Annotations"), { target: { value: "owner=ops" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建命名空间" }));

    await waitFor(() => expect(createBody).toEqual({ name: "preview", labels: { team: "platform" }, annotations: { owner: "ops" } }));
    expect(csrf).toBe("test-csrf-token");
    expect(await screen.findByText("命名空间 preview 已创建")).toBeInTheDocument();
  });

  it("passes storage URL filters to every storage query", async () => {
    const requests: URL[] = [];
    server.use(
      http.get("/api/v1/storageclasses", ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ items: [{ name: "data-standard", provisioner: "csi.example.test", reclaimPolicy: "Delete", volumeBinding: "Immediate", default: true, createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1 } });
      }),
      http.get("/api/v1/persistentvolumes", ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ items: [], meta: { total: 0 } });
      }),
      http.get("/api/v1/persistentvolumeclaims", ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ items: [], meta: { total: 0 } });
      }),
    );
    renderRoute("/storage?namespace=production&q=data");

    expect(await screen.findByRole("cell", { name: "data-standard" })).toBeInTheDocument();
    expect(screen.getByPlaceholderText("搜索存储资源")).toHaveValue("data");
    await waitFor(() => expect(requests).toHaveLength(3));
    for (const url of requests) expect(url.searchParams.get("q")).toBe("data");
    expect(requests.filter((url) => url.pathname === "/api/v1/persistentvolumeclaims")[0]?.searchParams.get("namespace")).toBe("production");
  });

  it("restores storage tabs from the URL", async () => {
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [{ name: "data-claim", namespace: "production", status: "Bound", capacityBytes: 1073741824, accessModes: ["ReadWriteOnce"], storageClass: "data-standard", createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1 } })),
    );
    renderRoute("/storage?tab=claims");

    expect(await screen.findByRole("tab", { name: /PVC/ })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByRole("cell", { name: "data-claim" })).toBeInTheDocument();
  });

  it("shows Secret text as plaintext while retaining binary values as base64", async () => {
    const secret = { name: "app-secret", namespace: "default", type: "Opaque", keys: [{ key: "token", size: 10 }, { key: "blob", size: 3 }], createdAt: "2026-11-02T00:00:00Z" };
    const encodedToken = Buffer.from("token=abc\n").toString("base64");
    const encodedBinary = Buffer.from(new Uint8Array([0, 255, 10])).toString("base64");
    server.use(
      http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/secrets", () => HttpResponse.json({ items: [secret], meta: { total: 1 } })),
      http.post("/api/v1/secrets/default/app-secret/reveal", () => HttpResponse.json({ ...secret, data: { token: encodedToken, blob: encodedBinary } })),
    );
    renderRoute("/configuration?tab=secrets");

    expect(await screen.findByRole("cell", { name: "app-secret" })).toBeInTheDocument();
    expect(screen.queryByText("token · 10 B")).not.toBeInTheDocument();
    expect(screen.queryByText("blob · 3 B")).not.toBeInTheDocument();
    openResourceMenu("Secret default/app-secret");
    fireEvent.click(await screen.findByRole("menuitem", { name: "查看数据" }));

    expect(await screen.findByLabelText("token 值")).toHaveTextContent("token=abc");
    expect(screen.getByLabelText("token 值")).not.toHaveTextContent(encodedToken);
    expect(await screen.findByLabelText("blob 值")).toHaveTextContent(encodedBinary);
    expect(screen.getByText("Base64（二进制）")).toBeInTheDocument();
  });

  it("soft-wraps long Secret values inside the reveal dialog", async () => {
    const longValue = "x".repeat(4096);
    const secret = { name: "long-secret", namespace: "default", type: "Opaque", keys: [{ key: "payload", size: longValue.length }], createdAt: "2026-11-02T00:00:00Z" };
    server.use(
      http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/secrets", () => HttpResponse.json({ items: [secret], meta: { total: 1 } })),
      http.post("/api/v1/secrets/default/long-secret/reveal", () => HttpResponse.json({ ...secret, data: { payload: Buffer.from(longValue).toString("base64") } })),
    );
    renderRoute("/configuration?tab=secrets");

    expect(await screen.findByRole("cell", { name: "long-secret" })).toBeInTheDocument();
    openResourceMenu("Secret default/long-secret");
    fireEvent.click(await screen.findByRole("menuitem", { name: "查看数据" }));

    const value = await screen.findByLabelText("payload 值");
    expect(value).toHaveClass("soft-wrap");
    expect(value).toHaveTextContent(longValue);
  });

  it("shows Secret YAML with original data and allows editing", async () => {
    const secret = { name: "app-secret", namespace: "default", type: "Opaque", keys: [{ key: "token", size: 10 }], createdAt: "2026-11-02T00:00:00Z" };
    server.use(
      http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/secrets", () => HttpResponse.json({ items: [secret], meta: { total: 1 } })),
      http.get("/api/v1/yaml", ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get("type")).toBe("secret");
        expect(url.searchParams.get("namespace")).toBe("default");
        expect(url.searchParams.get("name")).toBe("app-secret");
        return HttpResponse.json({
          kind: "Secret",
          namespace: "default",
          name: "app-secret",
          yaml: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app-secret\n  namespace: default\ndata:\n  token: c2Vuc2l0aXZl\n",
        });
      }),
    );
    renderRoute("/configuration?tab=secrets");

    expect(await screen.findByRole("cell", { name: "app-secret" })).toBeInTheDocument();
    openResourceMenu("Secret default/app-secret");
    fireEvent.click(await screen.findByRole("menuitem", { name: "查看 YAML" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/可编辑 YAML/)).toBeInTheDocument();
    expect(await within(dialog).findByRole("button", { name: "编辑" })).toBeInTheDocument();
    expect(await within(dialog).findByText(/c2Vuc2l0aXZl/)).toBeInTheDocument();
    expect(within(dialog).queryByText(/脱敏/)).not.toBeInTheDocument();
  });

  it("creates, edits and deletes a ConfigMap with resource versions", async () => {
    const summary = { name: "settings", namespace: "default", resourceVersion: "51", keys: ["mode"], dataBytes: 4, createdAt: "2026-11-02T00:00:00Z" };
    const detail = { ...summary, data: { mode: "safe" } };
    let createBody: Record<string, unknown> | undefined;
    let updateBody: Record<string, unknown> | undefined;
    let deletedResourceVersion: string | null = null;
    server.use(
      http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [summary], meta: { total: 1, page: 1, pageSize: 20, totalPages: 1, filters: {} } })),
      http.get("/api/v1/configmaps/default/settings", () => HttpResponse.json(detail)),
      http.post("/api/v1/configmaps", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...detail, name: "feature-flags" }, { status: 201 });
      }),
      http.put("/api/v1/configmaps/default/settings", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json(detail);
      }),
      http.delete("/api/v1/configmaps/default/settings", ({ request }) => {
        deletedResourceVersion = new URL(request.url).searchParams.get("resourceVersion");
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    renderRoute("/configuration?namespace=default");

    const createButton = await screen.findByRole("button", { name: "创建 ConfigMap" });
    await waitFor(() => expect(createButton).toBeEnabled());
    expect(screen.queryByText("mode")).not.toBeInTheDocument();
    fireEvent.click(createButton);
    let dialog = await screen.findByRole("dialog", { name: "创建 ConfigMap" });
    fireEvent.change(within(dialog).getByLabelText("ConfigMap 名称"), { target: { value: "feature-flags" } });
    fireEvent.change(within(dialog).getByLabelText("键"), { target: { value: "checkout" } });
    fireEvent.change(within(dialog).getByLabelText("值"), { target: { value: "enabled" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 ConfigMap" }));
    await waitFor(() => expect(createBody).toEqual({ name: "feature-flags", namespace: "default", data: { checkout: "enabled" } }));

    openResourceMenu("ConfigMap default/settings");
    fireEvent.click(await screen.findByRole("menuitem", { name: "编辑 ConfigMap" }));
    dialog = await screen.findByRole("dialog", { name: "编辑 ConfigMap" });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(updateBody).toMatchObject({ data: { mode: "safe" }, resourceVersion: "51" }));

    openResourceMenu("ConfigMap default/settings");
    fireEvent.click(await screen.findByRole("menuitem", { name: "删除 ConfigMap" }));
    dialog = await screen.findByRole("alertdialog", { name: "删除 ConfigMap settings？" });
    fireEvent.change(within(dialog).getByLabelText("输入名称确认"), { target: { value: "settings" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(deletedResourceVersion).toBe("51"));
  });

  it("creates, edits and deletes a Secret with resource versions", async () => {
    const summary = { name: "api-token", namespace: "default", resourceVersion: "61", type: "Opaque", keys: [{ key: "password", size: 12 }], createdAt: "2026-11-02T00:00:00Z" };
    const revealed = { ...summary, data: { password: btoa("old-secret") } };
    let createBody: Record<string, unknown> | undefined;
    let updateBody: Record<string, unknown> | undefined;
    let deletedResourceVersion: string | null = null;
    server.use(
      http.get("/api/v1/secrets", () => HttpResponse.json({ items: [summary], meta: { total: 1, page: 1, pageSize: 20, totalPages: 1, filters: {} } })),
      http.post("/api/v1/secrets/default/api-token/reveal", () => HttpResponse.json(revealed)),
      http.post("/api/v1/secrets", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...summary, name: "service-token" }, { status: 201 });
      }),
      http.put("/api/v1/secrets/default/api-token", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json(summary);
      }),
      http.delete("/api/v1/secrets/default/api-token", ({ request }) => {
        deletedResourceVersion = new URL(request.url).searchParams.get("resourceVersion");
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    renderRoute("/configuration?tab=secrets&namespace=default");

    const createButton = await screen.findByRole("button", { name: "创建 Secret" });
    await waitFor(() => expect(createButton).toBeEnabled());
    fireEvent.click(createButton);
    let dialog = await screen.findByRole("dialog", { name: "创建 Secret" });
    fireEvent.change(within(dialog).getByLabelText("Secret 名称"), { target: { value: "service-token" } });
    fireEvent.change(within(dialog).getByLabelText("键"), { target: { value: "token" } });
    fireEvent.change(within(dialog).getByLabelText("Base64 值"), { target: { value: btoa("enabled") } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 Secret" }));
    await waitFor(() => expect(createBody).toEqual({ name: "service-token", namespace: "default", type: "Opaque", data: { token: btoa("enabled") } }));

    openResourceMenu("Secret default/api-token");
    fireEvent.click(await screen.findByRole("menuitem", { name: "编辑 Secret" }));
    dialog = await screen.findByRole("dialog", { name: "编辑 Secret" });
    fireEvent.change(within(dialog).getByLabelText("Base64 值"), { target: { value: btoa("new-secret") } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(updateBody).toEqual({ type: "Opaque", data: { password: btoa("new-secret") }, resourceVersion: "61" }));

    openResourceMenu("Secret default/api-token");
    fireEvent.click(await screen.findByRole("menuitem", { name: "删除 Secret" }));
    dialog = await screen.findByRole("alertdialog", { name: "删除 Secret api-token？" });
    fireEvent.change(within(dialog).getByLabelText("输入名称确认"), { target: { value: "api-token" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(deletedResourceVersion).toBe("61"));
  });

  it("shows server certificate metadata and reloads it", async () => {
    let reloads = 0;
    const certificate = {
      mode: "https", enabled: true, configured: true, status: "valid", subject: "CN=dosh.example.test", issuer: "CN=dosh.example.test",
      dnsNames: ["dosh.example.test"], ipAddresses: ["192.168.122.100"], daysRemaining: 90,
      fingerprintSha256: "A".repeat(64), keyAlgorithm: "ECDSA", autoReload: true, reloadIntervalSeconds: 30,
      lastReloadedAt: "2026-11-05T00:00:00Z",
    };
    server.use(
      http.get("/api/v1/certificate", () => HttpResponse.json(certificate)),
      http.post("/api/v1/certificate/reload", () => {
        reloads += 1;
        return HttpResponse.json(certificate);
      }),
    );
    renderRoute("/configuration?tab=certificate");

    expect(await screen.findByText("dosh.example.test")).toBeInTheDocument();
    expect(screen.getByText("192.168.122.100")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重载证书" }));
    await waitFor(() => expect(reloads).toBe(1));
    expect(await screen.findByText("服务端证书已重新加载")).toBeInTheDocument();
  });

  it("redirects to login when a protected API returns 401", async () => {
    let authenticated = true;
    server.use(
      http.get("/api/v1/session", () => authenticated ? HttpResponse.json(session) : HttpResponse.json({ code: "unauthorized", message: "authentication required" }, { status: 401 })),
      http.get("/api/v1/nodes", () => {
        authenticated = false;
        return HttpResponse.json({ code: "unauthorized", message: "authentication required" }, { status: 401 });
      }),
    );
    renderRoute("/nodes");

    expect(await screen.findByText("登录控制台")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "worker-0001" })).not.toBeInTheDocument();
  });

  it("derives cluster identity and health from the overview API", async () => {
    server.use(http.get("/api/v1/overview", () => HttpResponse.json({
      ...overview,
      cluster: { name: "commercial-test", version: "v9.9.9", cni: "TelemetryCNI", status: "Degraded", metricsAvailable: false },
    })));
    renderRoute("/nodes");

    await expectNodeLink();
    const currentCluster = await screen.findByLabelText("当前集群");
    expect(currentCluster).toHaveTextContent("commercial-test");
    expect(currentCluster).toHaveTextContent("/ 部分异常");
    const connection = screen.getByRole("status", { name: "集群连接状态" });
    expect(connection).toHaveTextContent("集群部分异常");
    expect(connection).toHaveTextContent("Kubernetes v9.9.9 · TelemetryCNI");
    expect(screen.queryByText("kubedo-lab")).not.toBeInTheDocument();
  });

  it("derives the overview breadcrumb from the overview API", async () => {
    server.use(http.get("/api/v1/overview", () => HttpResponse.json({
      ...overview,
      cluster: { name: "commercial-overview", version: "v9.9.9", cni: "TelemetryCNI", status: "Healthy", metricsAvailable: false },
    })));
    renderRoute("/");

    expect(await screen.findByRole("heading", { name: "集群总览" })).toBeInTheDocument();
    expect(await screen.findByText("总览 / commercial-overview")).toBeInTheDocument();
    expect(screen.queryByText("总览 / kubedo-lab")).not.toBeInTheDocument();
  });

  it("does not report a healthy cluster when the overview request fails", async () => {
    server.use(http.get("/api/v1/overview", () => HttpResponse.json(
      { code: "unavailable", message: "overview unavailable" },
      { status: 503 },
    )));
    renderRoute("/nodes");

    await expectNodeLink();
    const connection = await screen.findByRole("status", { name: "集群连接状态" });
    expect(connection).toHaveTextContent("集群状态不可用");
    expect(within(connection).getByRole("button", { name: "重试" })).toBeInTheDocument();
    expect(screen.queryByText("集群连接正常")).not.toBeInTheDocument();
    expect(screen.queryByText("kubedo-lab")).not.toBeInTheDocument();
  });

  it("renders workload inventory", async () => {
    renderRoute("/workloads");
    await waitFor(() => expect(screen.getByRole("link", { name: "web" })).toBeInTheDocument());
    expect(screen.getByText("nginx:alpine")).toBeInTheDocument();
  });

  it("shows recent audit records, applies URL filters and reveals request details", async () => {
    let requested: URL | undefined;
    server.use(http.get("/api/v1/audit-events", ({ request }) => {
      requested = new URL(request.url);
      return HttpResponse.json({
        items: [{
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
        }],
        meta: { total: 1, page: 1, pageSize: 20, totalPages: 1, filters: { namespace: "production", result: "success", action: "scale", search: "web" } },
      });
    }));

    renderRoute("/audit?namespace=production&result=success&action=scale&q=web");

    expect(await screen.findByRole("cell", { name: "alice@example.test" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: /web-api/ })).toBeInTheDocument();
    await waitFor(() => expect(requested?.searchParams.get("namespace")).toBe("production"));
    expect(requested?.searchParams.get("result")).toBe("success");
    expect(requested?.searchParams.get("action")).toBe("scale");
    expect(requested?.searchParams.get("q")).toBe("web");

    fireEvent.click(screen.getByRole("cell", { name: "alice@example.test" }));
    const dialog = await screen.findByRole("dialog", { name: "审计记录 #42" });
    expect(within(dialog).getByText("req-audit-42")).toBeInTheDocument();
    expect(within(dialog).getByText("dosh-admins")).toBeInTheDocument();
  });

  it("derives the workload status filter from the URL", async () => {
    let requestedStatus: string | null = null;
    server.use(http.get("/api/v1/workloads", ({ request }) => {
      requestedStatus = new URL(request.url).searchParams.get("status");
      return HttpResponse.json({ items: [workload], meta: { total: 1, status: "Running" } });
    }));

    renderRoute("/workloads?status=Running");

    await waitFor(() => expect(requestedStatus).toBe("Running"));
    expect(await screen.findByRole("combobox", { name: "工作负载状态筛选" })).toHaveTextContent("Running");
  });

  it("scales from desired state without pinning a stale resource version", async () => {
    let scaleBody: Record<string, unknown> | undefined;
    server.use(http.post("/api/v1/workloads/deployment/default/web-api/scale", async ({ request }) => {
      scaleBody = await request.json() as Record<string, unknown>;
      return HttpResponse.json({ status: "scaled" });
    }));
    renderRoute("/workloads/deployment/default/web-api");
    fireEvent.click(await screen.findByRole("button", { name: "扩缩容" }));
    const dialog = await screen.findByRole("dialog", { name: "调整副本数" });
    fireEvent.change(within(dialog).getByLabelText("期望副本数"), { target: { value: "3" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认调整" }));
    await waitFor(() => expect(scaleBody).toEqual({ replicas: 3 }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "调整副本数" })).not.toBeInTheDocument());
  });

  it("updates a workload image with the current resource version", async () => {
    let imageBody: Record<string, unknown> | undefined;
    let csrf: string | null = null;
    server.use(http.post("/api/v1/workloads/deployment/default/web-api/image", async ({ request }) => {
      imageBody = await request.json() as Record<string, unknown>;
      csrf = request.headers.get("x-csrf-token");
      return HttpResponse.json({ status: "updated" });
    }));
    renderRoute("/workloads/deployment/default/web-api");
    fireEvent.click(await screen.findByRole("button", { name: "更新镜像" }));
    const dialog = await screen.findByRole("dialog", { name: "更新容器镜像" });
    expect(within(dialog).getByLabelText("镜像地址")).toHaveValue("nginx:alpine");
    fireEvent.change(within(dialog).getByLabelText("镜像地址"), { target: { value: "registry.example.test/web@sha256:abc123" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认更新" }));
    await waitFor(() => expect(imageBody).toEqual({
      containerName: "web",
      image: "registry.example.test/web@sha256:abc123",
      resourceVersion: "317754",
    }));
    expect(csrf).toBe("test-csrf-token");
    expect(await screen.findByText("镜像已更新，滚动发布已触发")).toBeInTheDocument();
  });

  it("keeps the image dialog open and reports a resource conflict", async () => {
    server.use(http.post("/api/v1/workloads/deployment/default/web-api/image", () => HttpResponse.json(
      { code: "conflict", message: "resource changed since it was loaded" },
      { status: 409 },
    )));
    renderRoute("/workloads/deployment/default/web-api");
    fireEvent.click(await screen.findByRole("button", { name: "更新镜像" }));
    const dialog = await screen.findByRole("dialog", { name: "更新容器镜像" });
    fireEvent.change(within(dialog).getByLabelText("镜像地址"), { target: { value: "registry.example.test/web:2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认更新" }));
    expect(await screen.findByText("工作负载已被其他操作更新，请刷新后重试")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "更新容器镜像" })).toBeInTheDocument();
  });

  it("shows workload container environment, mounts and health checks", async () => {
    server.use(http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json(configuredWorkload())));
    renderRoute("/workloads/deployment/default/web-api?tab=config");

    expect(await screen.findByText("环境变量")).toBeInTheDocument();
    expect(screen.getByText("MODE")).toBeInTheDocument();
    expect(screen.getByText("production")).toBeInTheDocument();
    expect(screen.getByText("web-secret / token")).toBeInTheDocument();
    expect(screen.getByText("/etc/web")).toBeInTheDocument();
    expect(screen.getByText("web-config")).toBeInTheDocument();
    expect(screen.getByText("HTTP GET http://Pod/ready:80")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "编辑就绪检查" })).toBeInTheDocument();
  });

  it("updates workload environment variables with the current resource version", async () => {
    let updateBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json(configuredWorkload())),
      http.put("/api/v1/workloads/deployment/default/web-api/containers/web/config", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/deployment/default/web-api?tab=config");
    fireEvent.click(await screen.findByRole("button", { name: "编辑环境变量" }));
    const dialog = await screen.findByRole("dialog", { name: "编辑环境变量" });
    fireEvent.click(within(dialog).getByRole("button", { name: "添加环境变量" }));
    fireEvent.change(within(dialog).getByLabelText("变量 3 名称"), { target: { value: "FEATURE_FLAG" } });
    fireEvent.change(within(dialog).getByLabelText("变量 3 值"), { target: { value: "enabled" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存配置" }));

    await waitFor(() => expect(updateBody).toMatchObject({
      resourceVersion: "317754",
      env: [
        { name: "MODE", value: "production" },
        { name: "TOKEN", value: "", valueFrom: { kind: "secretKeyRef", name: "web-secret", key: "token" } },
        { name: "FEATURE_FLAG", value: "enabled" },
      ],
      volumeMounts: [{ name: "config", mountPath: "/etc/web", readOnly: true }],
      readinessProbe: { type: "httpGet", path: "/ready", port: "80" },
    }));
    expect(await screen.findByText("容器配置已更新，滚动发布已触发")).toBeInTheDocument();
  });

  it("updates workload volume mounts without dropping environment or probes", async () => {
    let updateBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json(configuredWorkload())),
      http.put("/api/v1/workloads/deployment/default/web-api/containers/web/config", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/deployment/default/web-api?tab=config");
    fireEvent.click(await screen.findByRole("button", { name: "编辑数据存储" }));
    const dialog = await screen.findByRole("dialog", { name: "编辑数据存储" });
    fireEvent.change(within(dialog).getByLabelText("挂载 1 路径"), { target: { value: "/srv/web-config" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存配置" }));

    await waitFor(() => expect(updateBody).toMatchObject({
      resourceVersion: "317754",
      env: [{ name: "MODE", value: "production" }, { name: "TOKEN", value: "", valueFrom: { kind: "secretKeyRef", name: "web-secret", key: "token" } }],
      volumeMounts: [{ name: "config", mountPath: "/srv/web-config", readOnly: true }],
      readinessProbe: { type: "httpGet", path: "/ready", port: "80" },
    }));
  });

  it("updates a readiness probe and keeps disabled probes unset", async () => {
    let updateBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json(configuredWorkload())),
      http.put("/api/v1/workloads/deployment/default/web-api/containers/web/config", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/deployment/default/web-api?tab=config");
    fireEvent.click(await screen.findByRole("button", { name: "编辑就绪检查" }));
    const dialog = await screen.findByRole("dialog", { name: "编辑就绪检查" });
    fireEvent.change(within(dialog).getByLabelText("请求路径"), { target: { value: "/healthz" } });
    fireEvent.change(within(dialog).getByLabelText("端口"), { target: { value: "8080" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存配置" }));

    await waitFor(() => expect(updateBody).toMatchObject({
      resourceVersion: "317754",
      readinessProbe: { type: "httpGet", path: "/healthz", port: "8080" },
    }));
    expect(updateBody).not.toHaveProperty("livenessProbe");
    expect(updateBody).not.toHaveProperty("startupProbe");
  });

  it("pauses a CronJob with the current resource version", async () => {
    const cronJob = {
      ...workload, kind: "CronJob", name: "backup", namespace: "default", status: "Idle", ready: "0/0", desired: 0,
      resourceVersion: "99", conditions: [], selector: {}, pods: [], containers: [],
    };
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/cronjob/default/backup", () => HttpResponse.json(cronJob)),
      http.post("/api/v1/workloads/cronjob/default/backup/suspend", async ({ request }) => {
        body = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/cronjob/default/backup");
    fireEvent.click(await screen.findByRole("button", { name: "暂停调度" }));
    const dialog = await screen.findByRole("alertdialog", { name: "暂停 CronJob 调度？" });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认暂停" }));
    await waitFor(() => expect(body).toEqual({ suspend: true, resourceVersion: "99" }));
    expect(await screen.findByText("CronJob 已暂停调度")).toBeInTheDocument();
  });

  it("keeps the CronJob suspend dialog open on a resource conflict", async () => {
    const cronJob = {
      ...workload, kind: "CronJob", name: "backup", namespace: "default", status: "Idle", ready: "0/0", desired: 0,
      resourceVersion: "99", conditions: [], selector: {}, pods: [], containers: [],
    };
    server.use(
      http.get("/api/v1/workloads/cronjob/default/backup", () => HttpResponse.json(cronJob)),
      http.post("/api/v1/workloads/cronjob/default/backup/suspend", () => HttpResponse.json(
        { code: "conflict", message: "resource changed since it was loaded" },
        { status: 409 },
      )),
    );
    renderRoute("/workloads/cronjob/default/backup");
    fireEvent.click(await screen.findByRole("button", { name: "暂停调度" }));
    const dialog = await screen.findByRole("alertdialog", { name: "暂停 CronJob 调度？" });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认暂停" }));
    expect(await screen.findByText("CronJob 已被其他操作更新，请刷新后重试")).toBeInTheDocument();
    expect(screen.getByRole("alertdialog", { name: "暂停 CronJob 调度？" })).toBeInTheDocument();
  });

  it("toggles the dark theme and persists the explicit preference", async () => {
    renderRoute("/nodes");
    const toggle = await screen.findByRole("button", { name: "切换为深色主题" });
    expect(document.documentElement).not.toHaveClass("dark");

    fireEvent.click(toggle);

    expect(document.documentElement).toHaveClass("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
    expect(localStorage.getItem("dosh-theme")).toBe("dark");
    expect(screen.getByRole("button", { name: "切换为浅色主题" })).toHaveAttribute("aria-pressed", "true");
  });

  it("creates a deployment through preview and confirmation", async () => {
    let previewBody: Record<string, unknown> | undefined;
    let createBody: Record<string, unknown> | undefined;
    server.use(
      http.post("/api/v1/workloads/deployments/preview", async ({ request }) => {
        previewBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web-api\n" });
      }),
      http.post("/api/v1/workloads/deployments", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"] }, { status: 201 });
      }),
    );
    renderRoute("/workloads?namespace=default");
    fireEvent.click(await screen.findByRole("button", { name: "创建 Deployment" }, { timeout: 5000 }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Deployment 名称"), { target: { value: "web-api" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    await within(dialog).findByLabelText("容器镜像");
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    expect(await within(dialog).findByText(/kind: Deployment/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 Deployment" }));
    expect(await screen.findByRole("heading", { name: "web-api" })).toBeInTheDocument();
    expect(previewBody).toMatchObject({ name: "web-api", namespace: "default", image: "nginx:alpine", containerPort: 80, cpuRequestMilli: 100, memoryRequestBytes: 134217728 });
    expect(createBody).toMatchObject({ name: "web-api", namespace: "default", replicas: 1 });
  });

  it("hides deployment creation for read-only capabilities", async () => {
    let capabilityRequests = 0;
    const readOnlyCapabilities = {
      ...session.capabilities,
      create: false,
      resourceActions: {
        ...session.capabilities.resourceActions,
        deployments: { ...session.capabilities.resourceActions!.deployments, create: false },
      },
    };
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities: readOnlyCapabilities })),
      http.get("/api/v1/capabilities", () => {
        capabilityRequests += 1;
        return HttpResponse.json(readOnlyCapabilities);
      }),
    );
    renderRoute("/workloads?namespace=default");
    expect(await screen.findByRole("link", { name: "web" })).toBeInTheDocument();
    await waitFor(() => expect(capabilityRequests).toBeGreaterThan(0));
    await waitFor(() => expect(screen.queryByRole("button", { name: "创建 Deployment" })).not.toBeInTheDocument());
  });

  it("streams pod logs in the detail page", async () => {
    let logsRequested = false;
    server.use(http.get("/api/v1/pods/default/web-abcdef-12345/logs", () => {
      logsRequested = true;
      return HttpResponse.text(logFrames("hello from dosh log stream"), { headers: { "Content-Type": "application/x-ndjson; charset=utf-8" } });
    }));
    renderRoute("/pods/default/web-abcdef-12345");
    expect(await screen.findByRole("heading", { name: "web-abcdef-12345" })).toBeInTheDocument();
    const logsTab = screen.getByRole("tab", { name: /日志/ });
    fireEvent.mouseDown(logsTab);
    fireEvent.click(logsTab);
    await waitFor(() => expect(logsRequested).toBe(true));
    expect(await screen.findByText(/hello from dosh log stream/)).toBeInTheDocument();
  });

  it("deletes a Pod from its detail page", async () => {
    let deleted = false;
    server.use(
      http.delete("/api/v1/pods/default/web-abcdef-12345", () => {
        deleted = true;
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    const router = renderRoute("/pods/default/web-abcdef-12345");

    fireEvent.click(await screen.findByRole("button", { name: "删除" }, { timeout: 5000 }));
    const dialog = await screen.findByRole("alertdialog", { name: "删除 Pod web-abcdef-12345？" });
    fireEvent.change(within(dialog).getByLabelText("输入名称确认"), { target: { value: "web-abcdef-12345" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));

    await waitFor(() => expect(deleted).toBe(true));
    await waitFor(() => expect(router.state.location.pathname).toBe("/pods"));
  });

  it("requests log timestamps when the viewer toggle is enabled", async () => {
    const logRequests: URL[] = [];
    server.use(http.get("/api/v1/pods/default/web-abcdef-12345/logs", ({ request }) => {
      logRequests.push(new URL(request.url));
      return HttpResponse.text(logFrames("2026-11-04T08:00:00Z hello with timestamp"), { headers: { "Content-Type": "application/x-ndjson; charset=utf-8" } });
    }));
    renderRoute("/pods/default/web-abcdef-12345?tab=logs");

    expect(await screen.findByText(/hello with timestamp/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "显示时间戳" }));
    await waitFor(() => expect(logRequests.some((url) => url.searchParams.get("timestamps") === "true")).toBe(true));
    expect(screen.getByRole("button", { name: "隐藏时间戳" })).toHaveAttribute("aria-pressed", "true");
  });

  it("restores and updates Pod detail tabs through the URL", async () => {
    const router = renderRoute("/pods/default/web-abcdef-12345?tab=containers");
    expect(await screen.findByRole("tab", { name: /实例/ })).toHaveAttribute("aria-selected", "true");

    const logsTab = screen.getByRole("tab", { name: /日志/ });
    fireEvent.mouseDown(logsTab);
    fireEvent.click(logsTab);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ tab: "logs" }));
    expect(await screen.findByText(/hello from dosh log stream/)).toBeInTheDocument();
  });

  it("restores and updates workload detail tabs through the URL", async () => {
    const router = renderRoute("/workloads/deployment/default/web-api?tab=events");
    expect(await screen.findByRole("tab", { name: "事件" })).toHaveAttribute("aria-selected", "true");

    const podsTab = screen.getByRole("tab", { name: /实例/ });
    fireEvent.mouseDown(podsTab);
    fireEvent.click(podsTab);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ tab: "pods" }));
    expect(screen.getByRole("tab", { name: /实例/ })).toHaveAttribute("aria-selected", "true");
  });

  it("links workload instance nodes to node detail", async () => {
    server.use(http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json({
      ...workload,
      name: "web-api",
      images: ["nginx:alpine"],
      resourceVersion: "317754",
      conditions: [],
      selector: { app: "web" },
      pods: [pod],
      containers: [{ name: "web", image: "nginx:alpine" }],
    })));
    renderRoute("/workloads/deployment/default/web-api?tab=pods");

    const nodeLink = await screen.findByRole("link", { name: "worker-0001" });
    expect(nodeLink).toHaveAttribute("href", "/nodes/worker-0001");
  });

  it("creates, edits and deletes a workload Service with resource versions", async () => {
    const services = [{
      name: "web-api", namespace: "default", resourceVersion: "21", type: "ClusterIP", headless: false, clusterIP: "10.96.0.21", externalIP: "",
      ports: [{ name: "http", protocol: "TCP", port: 80, targetPort: "8080" }], selector: { app: "web" }, endpointCount: 1,
      createdAt: "2026-11-02T00:00:00Z",
    }];
    let createBody: Record<string, unknown> | undefined;
    let updateBody: Record<string, unknown> | undefined;
    let deleteCSRF: string | null = null;
    let deleteResourceVersion: string | null = null;
    server.use(
      http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json({
        ...workload,
        name: "web-api",
        images: ["nginx:alpine"],
        resourceVersion: "317754",
        conditions: [],
        selector: { app: "web" },
        pods: [],
        containers: [{ name: "web", image: "nginx:alpine" }],
      })),
      http.get("/api/v1/services", () => HttpResponse.json({ items: services, meta: { total: services.length } })),
      http.post("/api/v1/services", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        const created = {
          name: String(createBody.name), namespace: "default", resourceVersion: "22", type: "NodePort", headless: false, clusterIP: "10.96.0.22", externalIP: "",
          ports: [{ name: "http", protocol: "TCP", port: 80, targetPort: "8080", nodePort: 31080 }], selector: { app: "web" }, endpointCount: 0,
          createdAt: "2026-11-02T00:00:00Z", labels: {}, annotations: {}, sessionAffinity: "None", ipFamilies: ["IPv4"], endpoints: [],
        };
        services.push(created);
        return HttpResponse.json(created, { status: 201 });
      }),
      http.put("/api/v1/services/default/web-api", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        const updated = { ...services[0], resourceVersion: "23", type: "LoadBalancer", endpointCount: 1 };
        services[0] = updated;
        return HttpResponse.json({ ...updated, labels: {}, annotations: {}, sessionAffinity: "None", ipFamilies: ["IPv4"], endpoints: [] });
      }),
      http.delete("/api/v1/services/default/web-api", ({ request }) => {
        deleteCSRF = request.headers.get("x-csrf-token");
        deleteResourceVersion = new URL(request.url).searchParams.get("resourceVersion");
        services.splice(0, 1);
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    renderRoute("/workloads/deployment/default/web-api?tab=services");

    expect(await screen.findByRole("heading", { name: "关联 Service" })).toBeInTheDocument();
    expect(await screen.findByRole("columnheader", { name: "类型" })).toBeInTheDocument();
    expect(await screen.findByRole("columnheader", { name: "访问地址" })).toBeInTheDocument();
    expect(await screen.findByRole("columnheader", { name: "Endpoint" })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "配置访问方式" }));
    let dialog = await screen.findByRole("dialog", { name: "配置访问方式" });
    fireEvent.change(within(dialog).getByLabelText("Service 名称"), { target: { value: "web-api-public" } });
    fireEvent.change(within(dialog).getByLabelText("Service 端口"), { target: { value: "8080" } });
    fireEvent.change(within(dialog).getByLabelText("目标端口"), { target: { value: "8080" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 Service" }));
    await waitFor(() => expect(createBody).toMatchObject({ name: "web-api-public", namespace: "default", selector: { app: "web" } }));
    expect(await screen.findByText("web-api-public")).toBeInTheDocument();

    openResourceMenu("Service default/web-api");
    fireEvent.click(await screen.findByRole("menuitem", { name: "编辑" }));
    dialog = await screen.findByRole("dialog", { name: "编辑 Service" });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(updateBody).toMatchObject({ resourceVersion: "21", selector: { app: "web" } }));

    openResourceMenu("Service default/web-api");
    fireEvent.click(await screen.findByRole("menuitem", { name: "删除" }));
    const alert = await screen.findByRole("alertdialog", { name: /删除 Service web-api/ });
    fireEvent.change(within(alert).getByLabelText("Service 名称"), { target: { value: "web-api" } });
    fireEvent.click(within(alert).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(deleteResourceVersion).toBe("23"));
    expect(deleteCSRF).toBe("test-csrf-token");
  });

  it("deletes a namespace after typed confirmation", async () => {
    let deleteRequest: Request | undefined;
    server.use(
      http.get("/api/v1/namespaces/production", () => HttpResponse.json({
        name: "production", status: "Active", resourceVersion: "41", labels: {}, annotations: {}, createdAt: "2026-11-02T00:00:00Z",
        resourceQuotas: [], limitRanges: [],
      })),
      http.delete("/api/v1/namespaces/production", ({ request }) => {
        deleteRequest = request;
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    const router = renderRoute("/namespaces/production");
    fireEvent.click(await screen.findByRole("button", { name: "删除命名空间" }, { timeout: 5000 }));
    const dialog = await screen.findByRole("alertdialog", { name: /删除命名空间 production/ });
    expect(within(dialog).getByRole("button", { name: "确认级联删除" })).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("请输入命名空间名称确认"), { target: { value: "production" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认级联删除" }));
    await waitFor(() => expect(deleteRequest).toBeDefined());
    expect(new URL(deleteRequest!.url).searchParams.get("resourceVersion")).toBe("41");
    expect(deleteRequest!.headers.get("x-csrf-token")).toBe("test-csrf-token");
    await waitFor(() => expect(router.state.location.pathname).toBe("/namespaces"));
  });

  it("renders workload detail when collection fields are null", async () => {
    server.use(http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json({
      ...workload,
      name: "web-api",
      images: null,
      conditions: null,
      selector: null,
      pods: null,
      containers: null,
    })));

    renderRoute("/workloads/deployment/default/web-api");

    expect(await screen.findByRole("heading", { name: "web-api" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "实例 (0)" })).toBeInTheDocument();
    expect(screen.getByText("暂无 Condition 数据")).toBeInTheDocument();
  });

  it("restores node detail tabs and scopes events to the node", async () => {
    let eventQuery: URLSearchParams | undefined;
    server.use(
      http.get("/api/v1/events", ({ request }) => {
        eventQuery = new URL(request.url).searchParams;
        return HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 100, totalPages: 0, filters: {} } });
      }),
    );
    const router = renderRoute("/nodes/worker-0001?tab=events");
    expect(await screen.findByRole("tab", { name: "事件" })).toHaveAttribute("aria-selected", "true");
    await waitFor(() => expect(eventQuery?.get("objectKind")).toBe("Node"));
    expect(eventQuery?.get("objectName")).toBe("worker-0001");
    expect(await screen.findByText("该资源暂无事件")).toBeInTheDocument();

    const yamlTab = screen.getByRole("tab", { name: "YAML" });
    fireEvent.mouseDown(yamlTab);
    fireEvent.click(yamlTab);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ tab: "yaml" }));
    expect(screen.getByRole("tab", { name: "YAML" })).toHaveAttribute("aria-selected", "true");
  });

  it("links event objects and renders more than ten events", async () => {
    let eventQuery: URLSearchParams | undefined;
    const events = [
      { uid: "event-deployment", namespace: "default", type: "Normal", reason: "ScalingReplicaSet", message: "Scaled deployment", objectKind: "Deployment", objectName: "web-api", count: 1, firstSeen: "2026-11-02T00:00:00Z", lastSeen: "2026-11-02T00:01:00Z" },
      { uid: "event-service", namespace: "default", type: "Normal", reason: "UpdatedLoadBalancer", message: "Updated service", objectKind: "Service", objectName: "web-api", count: 1, firstSeen: "2026-11-02T00:00:00Z", lastSeen: "2026-11-02T00:02:00Z" },
      { uid: "event-node", namespace: "", type: "Warning", reason: "NodeNotReady", message: "Node unavailable", objectKind: "Node", objectName: "worker-0001", count: 2, firstSeen: "2026-11-02T00:00:00Z", lastSeen: "2026-11-02T00:03:00Z" },
      ...Array.from({ length: 9 }, (_, index) => ({ uid: `event-pod-${index}`, namespace: "default", type: "Normal", reason: "Started", message: `Started pod ${index}`, objectKind: "Pod", objectName: `web-event-${index}`, count: 1, firstSeen: "2026-11-02T00:00:00Z", lastSeen: `2026-11-02T00:${10 + index}:00Z` })),
    ];
    server.use(http.get("/api/v1/events", ({ request }) => {
      eventQuery = new URL(request.url).searchParams;
      return HttpResponse.json({ items: events, meta: { total: events.length, page: 1, pageSize: 200, totalPages: 1, filters: {} } });
    }));
    renderRoute("/events");

    expect(await screen.findByText("共 12 条事件")).toBeInTheDocument();
    await waitFor(() => expect(eventQuery?.get("pageSize")).toBe("200"));
    expect(eventQuery?.get("sort")).toBe("lastSeen");
    expect(eventQuery?.get("order")).toBe("desc");
    expect(await screen.findByRole("link", { name: "Deployment/web-api" })).toHaveAttribute("href", "/workloads/deployment/default/web-api");
    expect(screen.getByRole("link", { name: "Service/web-api" })).toHaveAttribute("href", "/network/services/default/web-api");
    expect(screen.getByRole("link", { name: "Node/worker-0001" })).toHaveAttribute("href", "/nodes/worker-0001");
    expect(screen.getAllByRole("link", { name: /^Pod\/web-event-/ })).toHaveLength(9);
  });

  it("restores Service detail tabs and shows Endpoint readiness", async () => {
    const router = renderRoute("/network/services/default/web?tab=endpoints");
    expect(await screen.findByRole("heading", { name: "web" })).toBeInTheDocument();
    expect(await screen.findByRole("cell", { name: "10.244.1.10" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "web-abcdef-12345" })).toBeInTheDocument();
    expect(screen.getAllByText("Ready").length).toBeGreaterThan(0);

    const eventsTab = screen.getByRole("tab", { name: "事件" });
    fireEvent.mouseDown(eventsTab);
    fireEvent.click(eventsTab);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ tab: "events" }));
    expect(await screen.findByText("该资源暂无事件")).toBeInTheDocument();
  });

  it("edits and deletes a Service from its detail page", async () => {
    let updateBody: Record<string, unknown> | undefined;
    let deletedResourceVersion: string | null = null;
    server.use(
      http.get("/api/v1/services/default/web", () => HttpResponse.json({ ...serviceDetail, resourceVersion: "31" })),
      http.put("/api/v1/services/default/web", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...serviceDetail, resourceVersion: "31" });
      }),
      http.delete("/api/v1/services/default/web", ({ request }) => {
        deletedResourceVersion = new URL(request.url).searchParams.get("resourceVersion");
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    const router = renderRoute("/network/services/default/web");

    fireEvent.click(await screen.findByRole("button", { name: "编辑" }, { timeout: 5000 }));
    let dialog = await screen.findByRole("dialog", { name: "编辑 Service" });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(updateBody).toMatchObject({ resourceVersion: "31", selector: { app: "web" } }));

    fireEvent.click(await screen.findByRole("button", { name: "删除" }));
    dialog = await screen.findByRole("alertdialog", { name: "删除 Service web？" });
    fireEvent.change(within(dialog).getByLabelText("输入名称确认"), { target: { value: "web" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(deletedResourceVersion).toBe("31"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/network"));
  });

  it("restores Ingress detail tabs and shows TLS and backend status", async () => {
    const router = renderRoute("/network/ingresses/default/web?tab=routes");
    expect(await screen.findByRole("heading", { name: "web" })).toBeInTheDocument();
    expect(await screen.findByRole("cell", { name: "web.example.test" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "默认后端" })).toBeInTheDocument();

    const overviewTab = screen.getByRole("tab", { name: "概览" });
    fireEvent.mouseDown(overviewTab);
    fireEvent.click(overviewTab);
    await waitFor(() => expect(router.state.location.search).not.toMatchObject({ tab: "routes" }));
    expect(screen.getByText("web-tls")).toBeInTheDocument();
  });

  it("exposes Service and Ingress management actions directly from the list", async () => {
    let capabilityRequests = 0;
    server.use(
      http.get("/api/v1/capabilities", () => {
        capabilityRequests += 1;
        return HttpResponse.json(session.capabilities);
      }),
      http.get("/api/v1/services", () => HttpResponse.json({
        items: [{
          name: "web", namespace: "default", resourceVersion: "31", type: "ClusterIP", headless: false, clusterIP: "10.96.0.31",
          externalIP: "", ports: [{ protocol: "TCP", port: 80, targetPort: "8080" }], selector: { app: "web" },
          endpointCount: 2, createdAt: "2026-11-02T00:00:00Z",
        }],
        meta: { total: 1 },
      })),
      http.get("/api/v1/ingresses", () => HttpResponse.json({
        items: [{ ...ingressDetail, resourceVersion: "41" }],
        meta: { total: 1 },
      })),
    );
    renderRoute("/network?namespace=default");

    expect(await screen.findByRole("cell", { name: "web" })).toBeInTheDocument();
    await waitFor(() => expect(capabilityRequests).toBeGreaterThan(0));
    openResourceMenu("Service default/web");
    expect(await screen.findByRole("menuitem", { name: "编辑 Service" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "删除 Service" })).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());

    const ingressTab = screen.getByRole("tab", { name: /Ingress/ });
    fireEvent.mouseDown(ingressTab);
    fireEvent.click(ingressTab);
    await waitFor(() => expect(screen.getByRole("cell", { name: "web" })).toBeInTheDocument());
    openResourceMenu("Ingress default/web");
    expect(await screen.findByRole("menuitem", { name: "编辑 Ingress" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "删除 Ingress" })).toBeInTheDocument();
  });

  it("explains why Service creation is disabled without a namespace", async () => {
    renderRoute("/network");

    const createButton = await screen.findByRole("button", { name: "创建 Service" });
    expect(createButton).toBeDisabled();
    const trigger = createButton.parentElement!;
    expect(trigger).toHaveAttribute("tabindex", "0");

    fireEvent.focus(trigger);
    expect(await screen.findByRole("tooltip")).toHaveTextContent("请先选择命名空间，再创建 Service");
  });

  it("edits and deletes an Ingress from its detail page", async () => {
    let updateBody: Record<string, unknown> | undefined;
    let deletedResourceVersion: string | null = null;
    server.use(
      http.get("/api/v1/ingresses/default/web", () => HttpResponse.json({ ...ingressDetail, resourceVersion: "41" })),
      http.put("/api/v1/ingresses/default/web", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...ingressDetail, resourceVersion: "41" });
      }),
      http.delete("/api/v1/ingresses/default/web", ({ request }) => {
        deletedResourceVersion = new URL(request.url).searchParams.get("resourceVersion");
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    const router = renderRoute("/network/ingresses/default/web");

    fireEvent.click(await screen.findByRole("button", { name: "编辑" }, { timeout: 5000 }));
    let dialog = await screen.findByRole("dialog", { name: "编辑 Ingress" });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(updateBody).toMatchObject({ resourceVersion: "41", annotations: { "nginx.ingress.kubernetes.io/rewrite-target": "/" } }));

    fireEvent.click(await screen.findByRole("button", { name: "删除" }));
    dialog = await screen.findByRole("alertdialog", { name: "删除 Ingress web？" });
    fireEvent.change(within(dialog).getByLabelText("输入名称确认"), { target: { value: "web" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(deletedResourceVersion).toBe("41"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/network"));
  });

  it("shows StorageClass parameters and URL-driven event deep links", async () => {
    const router = renderRoute("/storage/classes/data-standard?tab=events");
    expect(await screen.findByRole("heading", { name: "data-standard" })).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "事件" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText("该资源暂无事件")).toBeInTheDocument();

    const overviewTab = screen.getByRole("tab", { name: "概览" });
    fireEvent.mouseDown(overviewTab);
    fireEvent.click(overviewTab);
    await waitFor(() => expect(router.state.location.search).not.toMatchObject({ tab: "events" }));
    expect(screen.getByText("premium-rwo")).toBeInTheDocument();
    expect(screen.getByText("discard")).toBeInTheDocument();
  });

  it("shows PersistentVolume capacity and bound claim links", async () => {
    renderRoute("/storage/volumes/pv-data");
    expect(await screen.findByRole("heading", { name: "pv-data" })).toBeInTheDocument();
    expect(screen.getAllByText("10 GiB").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "production/data-claim" })).toHaveAttribute("href", "/storage/claims/production/data-claim");
    expect(screen.getByText("csi.example.test")).toBeInTheDocument();
  });

  it("shows PersistentVolumeClaim request size, bound PV and conditions", async () => {
    renderRoute("/storage/claims/production/data-claim");
    expect(await screen.findByRole("heading", { name: "data-claim" })).toBeInTheDocument();
    expect(screen.getAllByText("5.0 GiB").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "pv-data" })).toHaveAttribute("href", "/storage/volumes/pv-data");
    expect(screen.getByText(/volume expansion in progress/)).toBeInTheDocument();
  });

  it("expands and deletes a PersistentVolumeClaim from its detail page", async () => {
    let updateBody: Record<string, unknown> | undefined;
    let deleted = false;
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ items: [storageClassDetail], meta: { total: 1 } })),
      http.put("/api/v1/persistentvolumeclaims/production/data-claim", async ({ request }) => {
        updateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json(persistentVolumeClaimDetail);
      }),
      http.delete("/api/v1/persistentvolumeclaims/production/data-claim", () => {
        deleted = true;
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    const router = renderRoute("/storage/claims/production/data-claim");

    fireEvent.click(await screen.findByRole("button", { name: "扩容" }, { timeout: 5000 }));
    let dialog = await screen.findByRole("dialog", { name: "扩容 PersistentVolumeClaim" });
    fireEvent.change(within(dialog).getByLabelText("申请容量"), { target: { value: "6" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "提交扩容" }));
    await waitFor(() => expect(updateBody).toMatchObject({ requestedCapacityBytes: 6 * 1024 ** 3 }));

    fireEvent.click(await screen.findByRole("button", { name: "删除" }));
    dialog = await screen.findByRole("alertdialog", { name: "删除 PersistentVolumeClaim data-claim？" });
    fireEvent.change(within(dialog).getByLabelText("输入名称确认"), { target: { value: "data-claim" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(deleted).toBe(true));
    await waitFor(() => expect(router.state.location.pathname).toBe("/storage"));
  });

  it("confirms node cordon with CSRF and refreshes scheduling state", async () => {
    let cordoned = false;
    let schedulingRequest: Request | undefined;
    server.use(
      http.get("/api/v1/nodes/worker-0001", () => HttpResponse.json({ ...nodeDetail, unschedulable: cordoned })),
      http.post("/api/v1/nodes/worker-0001/cordon", ({ request }) => {
        schedulingRequest = request;
        cordoned = true;
        return HttpResponse.json({ ...nodeDetail, pods: undefined, unschedulable: true });
      }),
    );
    renderRoute("/nodes/worker-0001");

    fireEvent.click(await screen.findByRole("button", { name: "停止调度" }, { timeout: 5000 }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("停止节点调度？");
    expect(schedulingRequest).toBeUndefined();
    fireEvent.click(within(dialog).getByRole("button", { name: "确认停止调度" }));

    await waitFor(() => expect(schedulingRequest?.headers.get("X-CSRF-Token")).toBe("test-csrf-token"));
    expect(await screen.findByRole("button", { name: "恢复调度" })).toBeInTheDocument();
  });

  it("hides node scheduling controls without nodeCordon capability", async () => {
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities: { ...session.capabilities, nodeCordon: false } })),
      http.get("/api/v1/capabilities", () => HttpResponse.json({ ...session.capabilities, nodeCordon: false })),
    );
    renderRoute("/nodes/worker-0001");
    expect(await screen.findByRole("heading", { name: "worker-0001" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "停止调度" })).not.toBeInTheDocument();
  });
});
