import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { Toaster } from "sonner";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { routeTree } from "@/router";
import type { IngressDetail, NodeDetail, Overview, PersistentVolumeClaimDetail, PersistentVolumeDetail, PodDetail, ServiceDetail, SessionResponse, StorageClassDetail, WorkloadSummary } from "@/api/types";
import { ThemeProvider } from "@/lib/theme";

const session: SessionResponse = {
  user: { username: "admin", admin: true, groups: ["dosh-admins"] },
  csrfToken: "test-csrf-token",
  expiresAt: "2099-01-01T00:00:00Z",
  capabilities: { read: true, scale: true, restart: true, updateImage: true, suspendCronJob: true, delete: true, create: true, revealSecrets: true, editYAML: true, nodeCordon: true, reloadCertificate: true },
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

const nodeDetail: NodeDetail = {
  name: "worker-0001", status: "Ready", roles: ["worker"], internalIP: "192.168.122.200", version: "v1.36.5",
  osImage: "Fedora", kernelVersion: "6.19", containerRuntime: "containerd://2.3.6", cpuCapacityMilli: 2000,
  memoryCapacityBytes: 4294967296, podCapacity: 110, podCount: 1, cpuRequestsMilli: 800, memoryRequestsBytes: 1073741824,
  hasMetrics: true, cpuUsageMilli: 350, memoryUsageBytes: 1073741824, unschedulable: false, createdAt: "2026-11-02T00:00:00Z",
  conditions: [{ type: "Ready", status: "True", reason: "KubeletReady" }], taints: [],
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
  name: "web", namespace: "default", type: "ClusterIP", clusterIP: "10.96.0.10", externalIP: "",
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
  http.get("/api/v1/ingresses/default/web", () => HttpResponse.json(ingressDetail)),
  http.get("/api/v1/storageclasses/data-standard", () => HttpResponse.json(storageClassDetail)),
  http.get("/api/v1/persistentvolumes/pv-data", () => HttpResponse.json(persistentVolumeDetail)),
  http.get("/api/v1/persistentvolumeclaims/production/data-claim", () => HttpResponse.json(persistentVolumeClaimDetail)),
  http.get("/api/v1/events", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 1, totalPages: 0, filters: {} } })),
  http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/secrets", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/certificate", () => HttpResponse.json({ mode: "https", enabled: true, configured: true, status: "valid", subject: "CN=dosh.example.test", issuer: "CN=dosh.example.test", dnsNames: ["dosh.example.test"], ipAddresses: [], daysRemaining: 90, fingerprintSha256: "A".repeat(64), keyAlgorithm: "ECDSA", autoReload: true, reloadIntervalSeconds: 30, lastReloadedAt: "2026-11-05T00:00:00Z" })),
  http.get("/api/v1/audit-events", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/yaml", () => HttpResponse.json({ kind: "Pod", namespace: "default", name: "web-abcdef-12345", yaml: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-abcdef-12345\n" })),
);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
beforeEach(() => localStorage.clear());

function renderRoute(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createRouter({ routeTree, history, defaultPreload: false });
  render(<ThemeProvider><QueryClientProvider client={queryClient}><RouterProvider router={router} /><Toaster /></QueryClientProvider></ThemeProvider>);
  return router;
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
    expect(await screen.findByRole("link", { name: "worker-0001" })).toBeInTheDocument();
    expect(screen.getByText("192.168.122.200")).toBeInTheDocument();
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
    fireEvent.click(await screen.findByRole("button", { name: "查看明文" }));

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

    fireEvent.click(await screen.findByRole("button", { name: "查看明文" }));

    const value = await screen.findByLabelText("payload 值");
    expect(value).toHaveClass("soft-wrap");
    expect(value).toHaveTextContent(longValue);
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

    expect(await screen.findByRole("link", { name: "worker-0001" })).toBeInTheDocument();
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

    expect(await screen.findByRole("link", { name: "worker-0001" })).toBeInTheDocument();
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
    fireEvent.click(await screen.findByRole("button", { name: "创建 Deployment" }));
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
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities: { ...session.capabilities, create: false } })),
      http.get("/api/v1/capabilities", () => HttpResponse.json({ ...session.capabilities, create: false })),
    );
    renderRoute("/workloads?namespace=default");
    expect(await screen.findByRole("link", { name: "web" })).toBeInTheDocument();
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

    fireEvent.click(await screen.findByRole("button", { name: "停止调度" }));
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
