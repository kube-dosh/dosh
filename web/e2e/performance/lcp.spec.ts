import { expect, test } from "@playwright/test";

const session = {
  user: { username: "admin", admin: true, groups: ["dosh-admins"] },
  csrfToken: "performance-csrf-token",
  expiresAt: "2099-01-01T00:00:00Z",
  authMode: "local",
  capabilities: { read: true, scale: true, restart: true, updateImage: true, suspendCronJob: true, delete: true, create: true, revealSecrets: true, editYAML: true, nodeCordon: true, reloadCertificate: true },
};

const node = {
  name: "worker-0001", status: "Ready", roles: ["worker"], internalIP: "192.168.122.200", version: "v1.36.5",
  osImage: "Fedora", kernelVersion: "6.19", containerRuntime: "containerd://2.3.6", cpuCapacityMilli: 2000,
  memoryCapacityBytes: 4294967296, podCapacity: 110, podCount: 12, cpuRequestsMilli: 800,
  memoryRequestsBytes: 1073741824, unschedulable: false, createdAt: "2026-11-02T00:00:00Z",
};

const overview = {
  cluster: { name: "kubedo-lab", version: "v1.36.5", cni: "Cilium", status: "Healthy", metricsAvailable: false },
  counts: { nodes: 8, readyNodes: 8, pods: 42, runningPods: 40, workloads: 12, namespaces: 9, unhealthyPods: 2, warningEvents: 1 },
  capacity: {
    cpuAllocatableMilli: 16000, memoryAllocatableBytes: 34359738368, podCapacity: 880,
    cpuRequestsMilli: 4200, memoryRequestsBytes: 8589934592, cpuLimitsMilli: 8000, memoryLimitsBytes: 17179869184,
  },
  recentEvents: [],
};

test("生产构建首屏 LCP 小于 2.5 秒", async ({ page }) => {
  const consoleErrors: string[] = [];
  await page.addInitScript(() => {
    const target = window as unknown as { __doshLCP: number };
    target.__doshLCP = 0;
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) target.__doshLCP = entry.startTime;
    }).observe({ type: "largest-contentful-paint", buffered: true });
  });
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/auth/config") return route.fulfill({ json: { mode: "local", providerName: "", loginURL: "" } });
    if (path === "/api/v1/session") return route.fulfill({ json: session });
    if (path === "/api/v1/overview") return route.fulfill({ json: overview });
    if (path === "/api/v1/nodes") return route.fulfill({ json: { items: [node], meta: { total: 1 } } });
    if (path === "/api/v1/events") return route.fulfill({ json: { items: [], meta: { total: 0 } } });
    return route.fulfill({ status: 404, json: { code: "not_found", message: path } });
  });

  await page.goto("/");
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await page.waitForTimeout(250);
  const lcp = await page.evaluate(() => {
    return (window as unknown as { __doshLCP: number }).__doshLCP;
  });
  test.info().annotations.push({ type: "lcp-ms", description: String(lcp) });
  console.log(`production LCP: ${lcp.toFixed(1)} ms`);
  expect(Number.isFinite(lcp)).toBe(true);
  expect(lcp).toBeLessThan(2500);
  expect(consoleErrors).toEqual([]);
});
