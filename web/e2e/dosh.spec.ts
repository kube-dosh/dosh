import { expect, test, type Page, type Route } from "@playwright/test";
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
    revealSecrets: true,
    editYAML: true,
    nodeCordon: true,
    reloadCertificate: true,
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
  taints: [],
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
  containers: [{ name: "web", image: "nginx:alpine" }],
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
  clusterIP: "10.96.0.10",
  externalIP: "",
  ports: [{ name: "http", protocol: "TCP", port: 80, targetPort: "8080" }],
  selector: { app: "web" },
  endpointCount: 1,
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

function json(route: Route, value: unknown, status = 200) {
  return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(value) });
}

function list<T>(items: T[]): ListResponse<T> {
  return { items, meta: { total: items.length, page: 1, pageSize: Math.max(items.length, 1), totalPages: items.length ? 1 : 0, filters: {} } };
}

type NodeSchedulingRequest = { method: string; path: string; csrf: string | null };

async function installApiMocks(page: Page, options: { authenticated?: boolean; authMode?: "local" | "token"; overview?: Overview; overviewStatus?: number; nodeSchedulingRequests?: NodeSchedulingRequest[]; expireSessionOnNodes?: boolean; noIngressController?: boolean } = {}) {
  const authenticated = options.authenticated ?? true;
  const authMode = options.authMode ?? "local";
  const sessionResponse: SessionResponse = { ...session, authMode };
  let signedIn = authenticated;
  let nodeSchedulingDisabled = false;
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
    if (path === "/api/v1/nodes/worker-0001") return json(route, { ...nodeDetail, unschedulable: nodeSchedulingDisabled });
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
    if (path === "/api/v1/workloads/deployment/default/web") return json(route, workloadDetail);
    if (path === "/api/v1/workloads/deployment/default/web/scale" && method === "POST") {
      return json(route, { status: "ok" });
    }
    if (path === "/api/v1/workloads/deployment/default/web/image" && method === "POST") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/workloads/cronjob/default/backup" && method === "GET") return json(route, cronJobDetail);
    if (path === "/api/v1/workloads/cronjob/default/backup/suspend" && method === "POST") {
      return json(route, { status: "updated" });
    }
    if (path === "/api/v1/pods") return json(route, list([podSummary]));
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
    if (path === "/api/v1/services/default/web") return json(route, serviceDetail);
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
    if (path === "/api/v1/services") return json(route, list([{
      name: "web", namespace: "default", type: "ClusterIP", clusterIP: "10.96.0.10", externalIP: "",
      ports: [{ name: "http", protocol: "TCP", port: 80, targetPort: "8080" }], selector: { app: "web" }, endpointCount: 2, createdAt: "2026-11-02T00:00:00Z",
    }]));
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

test("详情页五标签可深链并加载事件与 YAML", async ({ page }) => {
  await installApiMocks(page);
  await page.goto("/workloads/deployment/default/web");

  await expect(page.getByRole("tab", { name: "概览", exact: true })).toBeVisible();
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
  await expect(page.getByRole("cell", { name: "2", exact: true })).toBeVisible();
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

  await page.getByRole("button", { name: "查看明文" }).click();
  await page.getByLabel("Secret 名称").fill("app-secret");
  await page.getByRole("button", { name: "确认查看" }).click();

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
    for (const path of ["/", "/nodes", "/workloads/deployment/default/web", "/pods/default/web-abcdef-12345", "/audit"]) {
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
