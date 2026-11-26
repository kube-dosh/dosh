import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { Toaster } from "sonner";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { routeTree } from "@/router";
import type { AutoscalerSummary, DisruptionBudgetSummary, IngressDetail, NodeDetail, Overview, PersistentVolumeClaimDetail, PersistentVolumeDetail, PodDetail, ServiceDetail, SessionResponse, StorageClassDetail, WorkloadContainerInventory, WorkloadDetail, WorkloadPodTemplate, WorkloadPodTemplateResult, WorkloadPodTemplateUpdateInput, WorkloadSummary } from "@/api/types";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ThemeProvider } from "@/lib/theme";

const session: SessionResponse = {
  user: { username: "admin", admin: true, groups: ["dosh-admins"] },
  csrfToken: "test-csrf-token",
  expiresAt: "2099-01-01T00:00:00Z",
  capabilities: {
    read: true, scale: true, restart: true, updateImage: true, suspendCronJob: true, delete: true, create: true,
    createService: true, updateService: true, deleteService: true, deleteNamespace: true, revealSecrets: true,
    editYAML: true, nodeCordon: true, nodeDrain: true, podExec: false, podEphemeralDebug: false, podPortForward: false, helmReleases: true, helmUpgrade: false, helmChartRepositories: true, helmChartWrite: true, reloadCertificate: true,
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
      persistentvolumes: { create: true, update: true, delete: true, scale: false },
      storageclasses: { create: true, update: true, delete: true, scale: false },
      volumesnapshots: { create: true, update: false, delete: true, scale: false },
      nodes: { create: false, update: true, delete: false, scale: false },
      namespaces: { create: true, update: false, delete: true, scale: false },
      horizontalpodautoscalers: { create: true, update: true, delete: true, scale: false },
      poddisruptionbudgets: { create: true, update: true, delete: true, scale: false },
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

function configuredWorkloadTemplate(): WorkloadPodTemplate {
  const detail = configuredWorkload();
  return {
    kind: "Deployment",
    namespace: "default",
    name: "web-api",
    resourceVersion: detail.resourceVersion,
    selector: detail.selector,
    protectedLabels: ["app"],
    editable: true,
    warnings: [],
    template: {
      metadata: { labels: { app: "web" }, annotations: {} },
      spec: {
        containers: [{
          name: "web",
          image: "nginx:alpine",
          ports: [{ name: "http", containerPort: 80, protocol: "TCP" }],
          resources: { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "500m", memory: "512Mi" } },
          env: [
            { name: "MODE", value: "production" },
            { name: "TOKEN", valueFrom: { secretKeyRef: { name: "web-secret", key: "token" } } },
          ],
          volumeMounts: [{ name: "config", mountPath: "/etc/web", readOnly: true }],
          readinessProbe: { httpGet: { path: "/ready", port: 80, scheme: "HTTP" }, initialDelaySeconds: 5, periodSeconds: 10, timeoutSeconds: 1, successThreshold: 1, failureThreshold: 3 },
        }],
        volumes: [{ name: "config", configMap: { name: "web-config" } }],
      },
    },
    yaml: "",
  };
}

function configuredContainerInventory(): WorkloadContainerInventory {
  const detail = configuredWorkload();
  return {
    kind: "Deployment",
    namespace: "default",
    name: "web-api",
    resourceVersion: detail.resourceVersion,
    selector: detail.selector,
    templateEditable: true,
    metricsAvailable: true,
    warnings: [],
    refreshedAt: "2026-11-25T10:00:00Z",
    containers: [{
      ...detail.containers[0],
      type: "container",
      init: false,
      sidecar: false,
      runtime: { pods: 1, running: 1, ready: 1, terminated: 0, restarts: 0, metricsAvailable: true, cpuUsageMilli: 25, memoryUsageBytes: 33554432 },
      instances: [{
        pod: "web-api-abcdef-12345", namespace: "default", node: "worker-0001", image: "nginx:alpine", state: "Running",
        ready: true, restartCount: 0, metricsAvailable: true, cpuUsageMilli: 25, memoryUsageBytes: 33554432,
      }],
    }],
  };
}

function templateResult(body: WorkloadPodTemplateUpdateInput): WorkloadPodTemplateResult {
  return {
    ...configuredWorkloadTemplate(),
    resourceVersion: "317755",
    template: body.template,
    changed: true,
    diff: [{ path: "spec.containers[0]", before: "old", after: "new" }],
    rollout: { effect: "rollingUpdate", message: "模板已保存，将触发滚动发布" },
    newWarnings: [],
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
  source: { type: "CSI", csiDriver: "csi.example.test", csiVolumeHandle: "vol-1", csiFsType: "ext4" },
  impact: { mountCount: 1, volumeSnapshotCount: 1, mounts: [{ pod: "db-0", namespace: "production", workloadKind: "StatefulSet", workloadName: "db", containers: ["postgres", "metrics"], mountPaths: ["/var/lib/postgresql/data"] }] },
};

const persistentVolumeClaimDetail: PersistentVolumeClaimDetail = {
  name: "data-claim", namespace: "production", status: "Bound", volumeName: "pv-data", capacityBytes: 10 * 1024 * 1024 * 1024,
  accessModes: ["ReadWriteOnce"], storageClass: "data-standard", createdAt: "2026-11-02T00:00:00Z",
  labels: { app: "database" }, annotations: { backup: "daily" }, volumeMode: "Filesystem", requestedCapacityBytes: 5 * 1024 * 1024 * 1024,
  conditions: [{ type: "Resizing", status: "True", reason: "Expanding", message: "volume expansion in progress", lastTransitionTime: "2026-11-02T01:00:00Z" }],
  storageClassAvailable: true, allowVolumeExpansion: true,
  impact: { mountCount: 1, volumeSnapshotCount: 1, mounts: [{ pod: "db-0", namespace: "production", workloadKind: "StatefulSet", workloadName: "db", containers: ["postgres"], mountPaths: ["/var/lib/postgresql/data"] }] },
};

const server = setupServer(
  http.get("/api/v1/auth/config", () => HttpResponse.json({ mode: "local", providerName: "", loginURL: "" })),
  http.get("/api/v1/session", () => HttpResponse.json(session)),
  http.get("/api/v1/capabilities", () => HttpResponse.json(session.capabilities)),
  http.get("/api/v1/preferences", () => HttpResponse.json({ favorites: [], recent: [], savedViews: {}, hiddenColumns: {} })),
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
  http.get("/api/v1/resilience/autoscalers", () => HttpResponse.json({ items: [{
    name: "web-api", namespace: "default", resourceVersion: "31", targetKind: "Deployment", targetName: "web-api",
    minReplicas: 2, maxReplicas: 8, currentReplicas: 3, desiredReplicas: 3, status: "Ready",
    metrics: [{ type: "Resource", resource: "cpu", current: "45%", target: "80%", currentUtilization: 45, targetUtilization: 80 }],
    conditions: [{ type: "AbleToScale", status: "True", reason: "ReadyForNewScale" }], createdAt: "2026-11-02T00:00:00Z",
  } satisfies AutoscalerSummary], meta: { total: 1, totalKnown: true, page: 1, pageSize: 1, totalPages: 1, hasMore: false } })),
  http.get("/api/v1/resilience/disruption-budgets", () => HttpResponse.json({ items: [{
    name: "web-api", namespace: "default", resourceVersion: "41", minAvailable: "2", selector: { app: "web" },
    currentHealthy: 3, desiredHealthy: 2, disruptionsAllowed: 1, expectedPods: 3, observedGeneration: 2, status: "Ready",
    conditions: [{ type: "DisruptionAllowed", status: "True", reason: "SufficientPods" }], createdAt: "2026-11-02T00:00:00Z",
  } satisfies DisruptionBudgetSummary], meta: { total: 1, totalKnown: true, page: 1, pageSize: 1, totalPages: 1, hasMore: false } })),
  http.get("/api/v1/workloads/deployment/default/web-api", () => HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"], resourceVersion: "317754", conditions: [], selector: {}, pods: [], containers: [{ name: "web", image: "nginx:alpine" }] })),
  http.get("/api/v1/workloads/deployment/default/web-api/pod-template", () => HttpResponse.json(configuredWorkloadTemplate())),
  http.post("/api/v1/workloads/deployment/default/web-api/pod-template", async ({ request }) => HttpResponse.json(templateResult(await request.json() as WorkloadPodTemplateUpdateInput))),
  http.get("/api/v1/workloads/deployment/default/web-api/containers", () => HttpResponse.json(configuredContainerInventory())),
  http.get("/api/v1/container-registries", () => HttpResponse.json({ items: [] })),
  http.post("/api/v1/workloads/deployments/preview", () => HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web-api\n" })),
  http.post("/api/v1/workloads/deployments", () => HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"] }, { status: 201 })),
  http.post("/api/v1/workloads/create/preview", () => HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web-api\n" })),
  http.post("/api/v1/workloads/create", () => HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"] }, { status: 201 })),
  http.get("/api/v1/namespaces", () => HttpResponse.json({ items: [{ name: "default", status: "Active", createdAt: "2026-11-02T00:00:00Z" }, { name: "production", status: "Active", createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 2 } })),
  http.get("/api/v1/pods", () => HttpResponse.json({ items: [pod], meta: { total: 1 } })),
  http.get("/api/v1/pods/default/web-abcdef-12345", () => HttpResponse.json(pod)),
  http.get("/api/v1/pods/default/web-abcdef-12345/logs", () => HttpResponse.text(logFrames("hello from dosh log stream"), { headers: { "Content-Type": "application/x-ndjson; charset=utf-8" } })),
  http.get("/api/v1/resources/discovery", () => HttpResponse.json({
    items: [
      { group: "", version: "v1", resource: "pods", kind: "Pod", singularName: "pod", namespaced: true, verbs: ["get", "list"], shortNames: ["po"] },
      { group: "example.io", version: "v1", resource: "widgets", kind: "Widget", singularName: "widget", namespaced: true, verbs: ["get", "list"] },
    ],
    warnings: [],
  })),
  http.get("/api/v1/resources/_core/v1/pods", () => HttpResponse.json({
    items: [{ apiVersion: "v1", kind: "Pod", resource: "pods", name: "web-abcdef-12345", namespace: "default", createdAt: "2026-11-02T00:00:00Z", labels: { app: "web" }, cells: { Status: "Running", Age: "2d" } }],
    meta: { total: 1, totalKnown: true, page: 1, pageSize: 20, totalPages: 1, hasMore: false, columns: [{ name: "Name", type: "string" }, { name: "Status", type: "string" }, { name: "Age", type: "string" }] },
  })),
  http.get("/api/v1/resources/_core/v1/pods/web-abcdef-12345", () => HttpResponse.json({
    apiVersion: "v1", kind: "Pod", name: "web-abcdef-12345", namespace: "default",
    uid: "pod-uid", resourceVersion: "42", createdAt: "2026-11-02T00:00:00Z",
    labels: { app: "web" }, annotations: { owner: "platform" },
    object: { apiVersion: "v1", kind: "Pod", metadata: { name: "web-abcdef-12345", namespace: "default" }, spec: { containers: [{ name: "web", image: "nginx:alpine" }] } },
    yaml: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-abcdef-12345\n",
    redacted: false,
  })),
  http.get("/api/v1/services/default/web", () => HttpResponse.json(serviceDetail)),
  http.get("/api/v1/services", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
  http.get("/api/v1/ingresses", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/ingresses/default/web", () => HttpResponse.json(ingressDetail)),
  http.get("/api/v1/ingressclasses", () => HttpResponse.json({ items: [{ name: "nginx", controller: "k8s.io/ingress-nginx", default: true, createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1 } })),
  http.get("/api/v1/network/gateway-api", () => HttpResponse.json({
    available: true,
    version: "v1",
    classes: [{ name: "public", controller: "example.net/gateway", status: "Ready", conditions: [{ type: "Accepted", status: "True" }], createdAt: "2026-11-02T00:00:00Z", apiVersion: "gateway.networking.k8s.io/v1", resource: "gatewayclasses" }],
    gateways: [{ name: "edge", namespace: "default", className: "public", addresses: [{ type: "IPAddress", value: "192.0.2.10" }], listeners: [{ name: "http", protocol: "HTTP", port: 80, hostname: "web.example.test" }], status: "Ready", createdAt: "2026-11-02T00:00:00Z", apiVersion: "gateway.networking.k8s.io/v1", resource: "gateways" }],
    routes: [{ kind: "HTTPRoute", name: "web", namespace: "default", hostnames: ["web.example.test"], parentRefs: [{ name: "edge", namespace: "default", kind: "Gateway" }], rules: [{ matches: [{ path: "/", pathType: "PathPrefix" }], backends: [{ name: "web", port: "80" }] }], status: "Ready", createdAt: "2026-11-02T00:00:00Z", apiVersion: "gateway.networking.k8s.io/v1", resource: "httproutes" }],
    generatedAt: "2026-11-02T00:00:00Z",
  })),
  http.get("/api/v1/network/policies", () => HttpResponse.json({
    items: [{
      name: "web-policy", namespace: "default", podSelector: { app: "web" }, policyTypes: ["Ingress", "Egress"], matchedPods: 1,
      ingress: [{ peers: [{ namespaceSelector: { team: "platform" } }], ports: [{ protocol: "TCP", port: "80" }] }],
      egress: [{ peers: [{ podSelector: { tier: "db" } }], ports: [] }],
      warnings: ["出站策略未明确放行 DNS 53/TCP 或 UDP；源 Pod 的域名解析可能失败"],
      createdAt: "2026-11-02T00:00:00Z", apiVersion: "networking.k8s.io/v1", resource: "networkpolicies",
    }],
    generatedAt: "2026-11-02T00:00:00Z",
  })),
  http.get("/api/v1/network/connectivity", () => HttpResponse.json({
    verdict: "blocked", summary: "目标 Pod Ingress 策略拒绝该流量",
    source: { name: "web-abcdef-12345", namespace: "default", ip: "10.244.1.10", status: "Running" },
    destination: { name: "web", namespace: "default", type: "ClusterIP", clusterIP: "10.96.0.10", headless: false },
    dns: { name: "web.default.svc.cluster.local", clusterIP: "10.96.0.10", headless: false, explanation: "ClusterIP Service 通过 kube-proxy 转发" },
    port: { servicePort: 80, targetPort: "8080", protocol: "TCP" },
    endpoints: [{ pod: "web-abcdef-12345", podIP: "10.244.1.10", ready: true, egressAllowed: true, ingressAllowed: false, allowed: "blocked", reasons: ["ingress policy web-policy did not allow the peer"] }],
    checks: [{ id: "service-port", label: "Service 端口映射", status: "pass", message: "TCP/80 → 8080" }],
    egressPolicies: [], ingressPolicies: [{ name: "web-policy", namespace: "default", direction: "Ingress", selected: true, allowed: false, reason: "没有匹配的允许规则" }],
    findings: [], warnings: [], generatedAt: "2026-11-02T00:00:00Z",
  })),
  http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/storageclasses/data-standard", () => HttpResponse.json(storageClassDetail)),
  http.get("/api/v1/persistentvolumes/pv-data", () => HttpResponse.json(persistentVolumeDetail)),
  http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/persistentvolumeclaims/production/data-claim", () => HttpResponse.json(persistentVolumeClaimDetail)),
  http.get("/api/v1/volume-snapshots/feature", () => HttpResponse.json({ available: false, reason: "VolumeSnapshot CRD 未安装", classes: [] })),
  http.get("/api/v1/volume-snapshots", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/events", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 1, totalPages: 0, filters: {} } })),
  http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/secrets", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/certificate", () => HttpResponse.json({ mode: "https", enabled: true, configured: true, status: "valid", subject: "CN=dosh.example.test", issuer: "CN=dosh.example.test", dnsNames: ["dosh.example.test"], ipAddresses: [], daysRemaining: 90, fingerprintSha256: "A".repeat(64), keyAlgorithm: "ECDSA", autoReload: true, reloadIntervalSeconds: 30, lastReloadedAt: "2026-11-05T00:00:00Z" })),
  http.get("/api/v1/audit-events", () => HttpResponse.json({ items: [], meta: { total: 0, page: 1, pageSize: 20, totalPages: 0, filters: {} } })),
  http.get("/api/v1/audit-events/actions", () => HttpResponse.json({ items: [] })),
  http.get("/api/v1/yaml", () => HttpResponse.json({ kind: "Pod", namespace: "default", name: "web-abcdef-12345", yaml: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-abcdef-12345\n" })),
  http.post("/api/v1/yaml/apply", () => HttpResponse.json({ kind: "Deployment", namespace: "default", name: "web", yaml: "" })),
);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
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

  it("keeps the session when local logout fails", async () => {
    server.use(
      http.delete("/api/v1/session", () => HttpResponse.json({ code: "internal_error", message: "logout failed" }, { status: 500 })),
    );
    renderRoute("/");

    expect(await screen.findByRole("heading", { name: "集群总览" })).toBeInTheDocument();
    const userMenu = screen.getByRole("button", { name: /admin/i });
    fireEvent.pointerDown(userMenu, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.pointerUp(userMenu, { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(userMenu);
    fireEvent.click(await screen.findByRole("menuitem", { name: "退出登录" }));

    expect(await screen.findByText(/退出登录失败/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "集群总览" })).toBeInTheDocument();
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

  it("renders autoscaler and disruption budget explanations", async () => {
    renderRoute("/resilience");

    expect(await screen.findByRole("heading", { name: "弹性与可用性" })).toBeInTheDocument();
    expect(await screen.findByText("web-api", { selector: "h3" })).toBeInTheDocument();
    expect(screen.getByText("当前 45% · 目标 80%")).toBeInTheDocument();
    const pdbTab = screen.getByRole("tab", { name: /PDB 中断预算/ });
    fireEvent.mouseDown(pdbTab);
    fireEvent.click(pdbTab);
    expect(await screen.findByText("健康 / 期望")).toBeInTheDocument();
    expect(screen.getByText("当前允许驱逐 1 个 Pod；节点维护会遵守该上限逐个处理。")).toBeInTheDocument();
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

  it("aggregates an application workspace and scales a workload in place", async () => {
    let scaleBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads", () => HttpResponse.json({ items: [workload], meta: { total: 1, totalKnown: true, page: 1, pageSize: 200, totalPages: 1, hasMore: false } })),
      http.get("/api/v1/pods", () => HttpResponse.json({ items: [{ ...pod, status: "CrashLoopBackOff", ready: "0/1", restarts: 4 }], meta: { total: 1, totalKnown: true, page: 1, pageSize: 200, totalPages: 1, hasMore: false } })),
      http.get("/api/v1/services", () => HttpResponse.json({ items: [{ name: "web", namespace: "default", type: "ClusterIP", headless: false, clusterIP: "10.96.0.10", externalIP: "", ports: serviceDetail.ports, endpointCount: 1, createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1, totalKnown: true, page: 1, pageSize: 200, totalPages: 1, hasMore: false } })),
      http.get("/api/v1/ingresses", () => HttpResponse.json({ items: [{ name: "web", namespace: "default", class: "nginx", paths: ingressDetail.paths, tlsHosts: ["web.example.test"], createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1, totalKnown: true, page: 1, pageSize: 200, totalPages: 1, hasMore: false } })),
      http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [{ name: "app-config", namespace: "default", keys: ["mode"], dataBytes: 12, createdAt: "2026-11-02T00:00:00Z" }], meta: { total: 1 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [{ ...persistentVolumeClaimDetail, namespace: "default" }], meta: { total: 1 } })),
      http.get("/api/v1/events", () => HttpResponse.json({ items: [{ uid: "event-warning", namespace: "default", type: "Warning", reason: "BackOff", message: "container is restarting", objectKind: "Pod", objectName: "web-abcdef-12345", count: 3, firstSeen: "2026-11-02T00:00:00Z", lastSeen: "2026-11-02T00:05:00Z" }], meta: { total: 1 } })),
      http.post("/api/v1/workloads/deployment/default/web/scale", async ({ request }) => {
        scaleBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "ok" });
      }),
    );
    renderRoute("/applications?namespace=default");

    expect(await screen.findByRole("heading", { name: "应用工作台" })).toBeInTheDocument();
    expect(await screen.findByText("需要关注")).toBeInTheDocument();
    expect(screen.getByText(/container is restarting/)).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "web" }).some((link) => link.getAttribute("href") === "/workloads/deployment/default/web")).toBe(true);
    expect(screen.getByRole("link", { name: "app-config" })).toHaveAttribute("href", "/configuration?namespace=default&tab=configmaps&q=app-config");

    openResourceMenu("Deployment default/web");
    fireEvent.click(await screen.findByRole("menuitem", { name: "扩缩容" }));
    const dialog = await screen.findByRole("dialog", { name: "调整副本数" });
    fireEvent.change(within(dialog).getByLabelText("期望副本数"), { target: { value: "3" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认调整" }));
    await waitFor(() => expect(scaleBody).toEqual({ replicas: 3 }));
  });

  it("keeps application workspace sections visible when one data source is forbidden", async () => {
    server.use(
      http.get("/api/v1/workloads", () => HttpResponse.json({ items: [workload], meta: { total: 1, totalKnown: true, page: 1, pageSize: 200, totalPages: 1, hasMore: false } })),
      http.get("/api/v1/pods", () => HttpResponse.json({ code: "forbidden", message: "pods are forbidden" }, { status: 403 })),
    );
    renderRoute("/applications?namespace=default");

    expect(await screen.findByRole("heading", { name: "应用工作台" })).toBeInTheDocument();
    expect(await screen.findByText(/部分数据加载失败：Pod/)).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "web" }).some((link) => link.getAttribute("href") === "/workloads/deployment/default/web")).toBe(true);
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
    renderRoute("/storage?tab=classes&namespace=production&q=data");

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

  it("keeps PVC data visible when StorageClass access is forbidden", async () => {
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ code: "forbidden", message: "denied" }, { status: 403 })),
      http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [{
        name: "data-claim", namespace: "production", status: "Bound", capacityBytes: 1073741824,
        accessModes: ["ReadWriteOnce"], storageClass: "data-standard", createdAt: "2026-11-02T00:00:00Z",
      }], meta: { total: 1 } })),
    );
    renderRoute("/storage?tab=claims");

    expect(await screen.findByRole("cell", { name: "data-claim" })).toBeInTheDocument();
    expect(screen.queryByText("没有访问权限")).not.toBeInTheDocument();

    const classesTab = screen.getByRole("tab", { name: /StorageClass/ });
    fireEvent.mouseDown(classesTab);
    fireEvent.click(classesTab);
    expect(await screen.findByText("没有访问权限")).toBeInTheDocument();
  });

  it("degrades the snapshot tab to an explained unavailable state without CSI snapshot CRDs", async () => {
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/volume-snapshots/feature", () => HttpResponse.json({ available: false, reason: "未检测到 VolumeSnapshot CRD", classes: [] })),
    );
    renderRoute("/storage?tab=snapshots");

    expect(await screen.findByRole("tab", { name: /快照 \(不可用\)/ })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText("CSI 快照能力不可用")).toBeInTheDocument();
    expect(screen.getByText("未检测到 VolumeSnapshot CRD")).toBeInTheDocument();
  });

  it("creates a StorageClass through the structured form with dry-run-backed API contract", async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.post("/api/v1/storageclasses", async ({ request }) => {
        body = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...storageClassDetail, name: "fast-ssd", provisioner: "csi.example.test", allowVolumeExpansion: true }, { status: 201 });
      }),
    );
    renderRoute("/storage?tab=classes");

    const createButton = await screen.findByRole("button", { name: "创建 StorageClass" });
    await waitFor(() => expect(createButton).toBeEnabled());
    fireEvent.click(createButton);
    const dialog = await screen.findByRole("dialog", { name: "创建 StorageClass" });
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "fast-ssd" } });
    fireEvent.change(within(dialog).getByLabelText("Provisioner"), { target: { value: "csi.example.test" } });
    fireEvent.click(within(dialog).getByRole("button", { name: /允许在线扩容/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 StorageClass" }));

    await waitFor(() => expect(body).toMatchObject({
      name: "fast-ssd", provisioner: "csi.example.test", reclaimPolicy: "Delete", volumeBinding: "Immediate", allowVolumeExpansion: true,
    }));
    expect(await screen.findByText("StorageClass fast-ssd 已创建")).toBeInTheDocument();
  });

  it("creates a structured CSI PersistentVolume", async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.post("/api/v1/persistentvolumes", async ({ request }) => {
        body = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...persistentVolumeDetail, name: "pv-new", status: "Available", claim: undefined }, { status: 201 });
      }),
    );
    renderRoute("/storage?tab=volumes");

    const createButton = await screen.findByRole("button", { name: "创建 PV" });
    await waitFor(() => expect(createButton).toBeEnabled());
    fireEvent.click(createButton);
    const dialog = await screen.findByRole("dialog", { name: "创建 PersistentVolume" });
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "pv-new" } });
    fireEvent.change(within(dialog).getByLabelText("CSI Driver"), { target: { value: "csi.example.test" } });
    fireEvent.change(within(dialog).getByLabelText("VolumeHandle"), { target: { value: "vol-new" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 PersistentVolume" }));

    await waitFor(() => expect(body).toMatchObject({
      name: "pv-new", capacityBytes: 10 * 1024 ** 3, reclaimPolicy: "Retain", volumeMode: "Filesystem",
      csi: { driver: "csi.example.test", volumeHandle: "vol-new" },
    }));
    expect(await screen.findByText("PersistentVolume pv-new 已创建")).toBeInTheDocument();
  });

  it("restores a PVC from a ready VolumeSnapshot with a minimum restore size", async () => {
    const snapshot = {
      name: "snap-daily", namespace: "production", resourceVersion: "21", sourcePvc: "data-claim", volumeSnapshotClassName: "csi-snap",
      status: "Ready" as const, readyToUse: true, restoreSizeBytes: 5 * 1024 ** 3, createdAt: "2026-11-19T01:00:00Z",
    };
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/storageclasses", () => HttpResponse.json({ items: [storageClassDetail], meta: { total: 1 } })),
      http.get("/api/v1/persistentvolumes", () => HttpResponse.json({ items: [], meta: { total: 0 } })),
      http.get("/api/v1/persistentvolumeclaims", () => HttpResponse.json({ items: [persistentVolumeClaimDetail], meta: { total: 1 } })),
      http.get("/api/v1/volume-snapshots/feature", () => HttpResponse.json({ available: true, apiVersion: "snapshot.storage.k8s.io/v1", classes: ["csi-snap"], defaultClass: "csi-snap" })),
      http.get("/api/v1/volume-snapshots", () => HttpResponse.json({ items: [snapshot], meta: { total: 1, totalKnown: true, page: 1, pageSize: 20, totalPages: 1, hasMore: false } })),
      http.post("/api/v1/persistentvolumeclaims", async ({ request }) => {
        body = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...persistentVolumeClaimDetail, name: "snap-daily-restore", requestedCapacityBytes: 6 * 1024 ** 3 }, { status: 201 });
      }),
    );
    renderRoute("/storage?tab=snapshots&namespace=production");

    expect(await screen.findByRole("cell", { name: "snap-daily" })).toBeInTheDocument();
    openResourceMenu("VolumeSnapshot production/snap-daily");
    fireEvent.click(await screen.findByRole("menuitem", { name: "从快照恢复 PVC" }));
    const dialog = await screen.findByRole("dialog", { name: "从快照恢复 PersistentVolumeClaim" });
    expect(dialog).toHaveTextContent("production/snap-daily");
    expect(dialog).toHaveTextContent("恢复大小 5.0 GiB");
    fireEvent.change(within(dialog).getByLabelText("申请容量"), { target: { value: "6" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "从快照恢复" }));

    await waitFor(() => expect(body).toMatchObject({
      name: "snap-daily-restore", namespace: "production", requestedCapacityBytes: 6 * 1024 ** 3, snapshotName: "snap-daily",
    }));
    expect(await screen.findByText("PVC snap-daily-restore 已从快照发起恢复")).toBeInTheDocument();
  });

  it("explains blocked PVC expansion and shows live mount impact", async () => {
    server.use(
      http.get("/api/v1/persistentvolumeclaims/production/data-claim", () => HttpResponse.json({
        ...persistentVolumeClaimDetail,
        storageClassAvailable: false, allowVolumeExpansion: false, expansionStatus: "blocked",
        expansionMessage: "StorageClass data-standard 未启用 allowVolumeExpansion，PVC 无法扩容。",
      })),
    );
    renderRoute("/storage/claims/production/data-claim");

    expect(await screen.findByRole("heading", { name: "data-claim" })).toBeInTheDocument();
    expect(screen.getByText("扩容受阻")).toBeInTheDocument();
    expect(screen.getByText(/未启用 allowVolumeExpansion/)).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "扩容" })).toBeDisabled();
    expect(screen.getByText("PVC 使用者与影响")).toBeInTheDocument();
    expect(screen.getByText("production/db-0")).toBeInTheDocument();
    expect(screen.getByText(/容器：postgres/)).toBeInTheDocument();
    expect(screen.getByText(/挂载：\/var\/lib\/postgresql\/data/)).toBeInTheDocument();
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
    fireEvent.click(await screen.findByRole("button", { name: "app-secret" }));

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

  it("keeps ConfigMaps visible when Secret access is forbidden", async () => {
    server.use(
      http.get("/api/v1/configmaps", () => HttpResponse.json({ items: [{
        name: "app-config", namespace: "default", keys: ["mode"], dataBytes: 12, createdAt: "2026-11-02T00:00:00Z",
      }], meta: { total: 1 } })),
      http.get("/api/v1/secrets", () => HttpResponse.json({ code: "forbidden", message: "denied" }, { status: 403 })),
    );
    renderRoute("/configuration");

    expect(await screen.findByRole("cell", { name: "app-config" })).toBeInTheDocument();
    expect(screen.queryByText("没有访问权限")).not.toBeInTheDocument();

    const secretsTab = screen.getByRole("tab", { name: /Secret/ });
    fireEvent.mouseDown(secretsTab);
    fireEvent.click(secretsTab);
    expect(await screen.findByText("没有访问权限")).toBeInTheDocument();
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

  it("updates a workload image atomically with the current resource version", async () => {
    let imageBody: Record<string, unknown> | undefined;
    let csrf: string | null = null;
    server.use(http.post("/api/v1/workloads/deployment/default/web-api/pod-template", async ({ request }) => {
      imageBody = await request.json() as Record<string, unknown>;
      csrf = request.headers.get("x-csrf-token");
      return HttpResponse.json(templateResult(imageBody as unknown as WorkloadPodTemplateUpdateInput));
    }));
    renderRoute("/workloads/deployment/default/web-api");
    fireEvent.click(await screen.findByRole("button", { name: "更新镜像" }));
    const dialog = await screen.findByRole("dialog", { name: "更新容器镜像" });
    expect(await within(dialog).findByLabelText("镜像地址")).toHaveValue("nginx:alpine");
    fireEvent.change(within(dialog).getByLabelText("镜像地址"), { target: { value: "registry.example.test/web@sha256:abc123" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认更新" }));
    await waitFor(() => expect(imageBody).toMatchObject({
      resourceVersion: "317754",
      source: "form",
      template: { spec: { containers: [{ name: "web", image: "registry.example.test/web@sha256:abc123" }] } },
    }));
    expect(csrf).toBe("test-csrf-token");
    expect(await screen.findByText("已更新 1 个容器镜像，滚动发布已触发")).toBeInTheDocument();
  });

  it("keeps the image draft when an atomic template update conflicts", async () => {
    server.use(http.post("/api/v1/workloads/deployment/default/web-api/pod-template", () => HttpResponse.json(
      { code: "conflict", message: "resource changed since it was loaded" },
      { status: 409 },
    )));
    renderRoute("/workloads/deployment/default/web-api");
    fireEvent.click(await screen.findByRole("button", { name: "更新镜像" }));
    const dialog = await screen.findByRole("dialog", { name: "更新容器镜像" });
    const imageInput = await within(dialog).findByLabelText("镜像地址");
    fireEvent.change(imageInput, { target: { value: "registry.example.test/web:2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认更新" }));
    expect(await screen.findByText(/你的镜像草稿仍然保留/)).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "更新容器镜像" })).toBeInTheDocument();
    expect(within(dialog).getByLabelText("镜像地址")).toHaveValue("registry.example.test/web:2");
  });

  it("shows all workload containers, instances and Pod template entry points", async () => {
    renderRoute("/workloads/deployment/default/web-api?tab=config");
    const center = await screen.findByTestId("workload-container-center");
    const container = within(center).getByTestId("container-web");
    expect(container).toHaveTextContent("业务容器");
    expect(container).toHaveTextContent("1 个实例");
    fireEvent.click(within(container).getByRole("button", { name: "详情与实例" }));
    expect(within(container).getByText("MODE=production")).toBeInTheDocument();
    expect(within(container).getByText("config → /etc/web")).toBeInTheDocument();
    expect(within(container).getByText("Readiness · httpGet")).toBeInTheDocument();
    expect(within(container).getByTestId("container-instance")).toHaveTextContent("web-api-abcdef-12345");

    fireEvent.click(within(container).getByRole("button", { name: "编辑配置" }));
    const editor = await screen.findByRole("dialog", { name: "Pod 模板 · web-api" });
    expect(await within(editor).findByText("结构化表单")).toBeInTheDocument();
    expect(within(editor).getByText("完整 YAML")).toBeInTheDocument();
  });

  it("saves environment and volume changes through one Pod template update", async () => {
    let updateBody: WorkloadPodTemplateUpdateInput | undefined;
    server.use(http.post("/api/v1/workloads/deployment/default/web-api/pod-template", async ({ request }) => {
      updateBody = await request.json() as WorkloadPodTemplateUpdateInput;
      return HttpResponse.json(templateResult(updateBody));
    }));
    renderRoute("/workloads/deployment/default/web-api?tab=config");
    const center = await screen.findByTestId("workload-container-center");
    const container = within(center).getByTestId("container-web");
    fireEvent.click(within(container).getByRole("button", { name: "编辑配置" }));
    const editor = await screen.findByRole("dialog", { name: "Pod 模板 · web-api" });
    fireEvent.click(await within(editor).findByText("环境变量（2）"));
    fireEvent.click(within(editor).getByRole("button", { name: "添加环境变量" }));
    const names = within(editor).getAllByPlaceholderText("变量名");
    const values = within(editor).getAllByPlaceholderText("变量值");
    fireEvent.change(names[names.length - 1], { target: { value: "FEATURE_FLAG" } });
    fireEvent.change(values[values.length - 1], { target: { value: "enabled" } });
    fireEvent.click(within(editor).getByText("存储挂载（1）"));
    fireEvent.change(within(editor).getByPlaceholderText("/data"), { target: { value: "/srv/web-config" } });

    fireEvent.click(within(editor).getByRole("button", { name: "保存并发布" }));
    await waitFor(() => expect(updateBody?.dryRun).toBe(false));
    expect(updateBody).toMatchObject({
      resourceVersion: "317754",
      source: "form",
      template: {
        spec: {
          containers: [{
            name: "web",
            env: [
              { name: "MODE", value: "production" },
              { name: "TOKEN", valueFrom: { secretKeyRef: { name: "web-secret", key: "token" } } },
              { name: "FEATURE_FLAG", value: "enabled" },
            ],
            volumeMounts: [{ name: "config", mountPath: "/srv/web-config", readOnly: true }],
            readinessProbe: { httpGet: { path: "/ready", port: 80, scheme: "HTTP" } },
          }],
        },
      },
    });
    expect(await screen.findByText("Pod 模板已保存，发布变更已触发")).toBeInTheDocument();
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

  it("runs a CronJob once and opens the created Job", async () => {
    const capabilities = {
      ...session.capabilities,
      resourceActions: {
        ...session.capabilities.resourceActions!,
        jobs: { ...session.capabilities.resourceActions!.jobs, create: true },
      },
    };
    const cronJob = {
      ...workload, kind: "CronJob", name: "backup", namespace: "default", status: "Idle", ready: "0/0", desired: 0,
      resourceVersion: "99", conditions: [], selector: {}, pods: [], containers: [],
    };
    let runCalled = false;
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities })),
      http.get("/api/v1/capabilities", () => HttpResponse.json(capabilities)),
      http.get("/api/v1/workloads/cronjob/default/backup", () => HttpResponse.json(cronJob)),
      http.post("/api/v1/workloads/cronjob/default/backup/run", () => {
        runCalled = true;
        return HttpResponse.json({ ...workload, kind: "Job", name: "backup-manual-12345", namespace: "default" }, { status: 201 });
      }),
      http.get("/api/v1/workloads/job/default/backup-manual-12345", () => HttpResponse.json({
        ...workload, kind: "Job", name: "backup-manual-12345", namespace: "default",
        conditions: [], selector: {}, pods: [], containers: [],
      })),
    );
    renderRoute("/workloads/cronjob/default/backup");

    fireEvent.click(await screen.findByRole("button", { name: "立即执行" }));
    const dialog = await screen.findByRole("alertdialog", { name: "立即执行 backup？" });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认执行" }));

    expect(await screen.findByRole("heading", { name: "backup-manual-12345" })).toBeInTheDocument();
    expect(runCalled).toBe(true);
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

  it("creates a workload through preview and confirmation", async () => {
    let previewBody: Record<string, unknown> | undefined;
    let createBody: Record<string, unknown> | undefined;
    server.use(
      http.post("/api/v1/workloads/create/preview", async ({ request }) => {
        previewBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web-api\n" });
      }),
      http.post("/api/v1/workloads/create", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, name: "web-api", images: ["nginx:alpine"] }, { status: 201 });
      }),
    );
    renderRoute("/workloads?namespace=default");
    fireEvent.click(await screen.findByRole("button", { name: "创建工作负载" }, { timeout: 5000 }));
    const dialog = await screen.findByRole("dialog", { name: "创建工作负载" });
    fireEvent.change(within(dialog).getByLabelText("Deployment 名称"), { target: { value: "web-api" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    await within(dialog).findByLabelText("容器镜像");
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    expect(await within(dialog).findByText(/kind: Deployment/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 Deployment" }));
    expect(await screen.findByRole("heading", { name: "web-api" })).toBeInTheDocument();
    expect(previewBody).toMatchObject({ kind: "deployment", name: "web-api", namespace: "default", image: "nginx:alpine", containerPort: 80, cpuRequestMilli: 100, memoryRequestBytes: 134217728 });
    expect(createBody).toMatchObject({ name: "web-api", namespace: "default", replicas: 1 });
  });

  it("creates a StatefulSet with its headless service", async () => {
    const writableCapabilities = {
      ...session.capabilities,
      resourceActions: Object.fromEntries(Object.entries(session.capabilities.resourceActions!).map(([resource, actions]) => [
        resource,
        ["statefulsets", "daemonsets", "jobs", "cronjobs"].includes(resource) ? { ...actions, create: true } : actions,
      ])),
    };
    let statefulSetBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities: writableCapabilities })),
      http.get("/api/v1/capabilities", () => HttpResponse.json(writableCapabilities)),
      http.post("/api/v1/workloads/create/preview", async ({ request }) => {
        statefulSetBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n  name: db\n" });
      }),
      http.post("/api/v1/workloads/create", async ({ request }) => {
        statefulSetBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, kind: "StatefulSet", name: "db", images: ["postgres:17"] }, { status: 201 });
      }),
      http.get("/api/v1/workloads/statefulset/default/db", () => HttpResponse.json({ ...workload, kind: "StatefulSet", name: "db", images: ["postgres:17"], conditions: [], selector: {}, pods: [], containers: [] })),
    );
    renderRoute("/workloads?namespace=default&kind=statefulset");

    fireEvent.click(await screen.findByRole("button", { name: "创建工作负载" }));
    const dialog = await screen.findByRole("dialog", { name: "创建工作负载" });
    fireEvent.change(within(dialog).getByLabelText("StatefulSet 名称"), { target: { value: "db" } });
    fireEvent.change(within(dialog).getByLabelText("Headless Service"), { target: { value: "db-headless" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    const image = await within(dialog).findByLabelText("容器镜像");
    fireEvent.change(image, { target: { value: "postgres:17" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    expect(await within(dialog).findByText(/kind: StatefulSet/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 StatefulSet" }));

    expect(await screen.findByRole("heading", { name: "db" })).toBeInTheDocument();
    expect(statefulSetBody).toMatchObject({ kind: "statefulset", name: "db", serviceName: "db-headless", replicas: 1 });
  });

  it("creates a CronJob with schedule and concurrency policy", async () => {
    const writableCapabilities = {
      ...session.capabilities,
      resourceActions: {
        ...session.capabilities.resourceActions!,
        cronjobs: { ...session.capabilities.resourceActions!.cronjobs, create: true },
      },
    };
    let createBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities: writableCapabilities })),
      http.get("/api/v1/capabilities", () => HttpResponse.json(writableCapabilities)),
      http.post("/api/v1/workloads/create/preview", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ yaml: "apiVersion: batch/v1\nkind: CronJob\nmetadata:\n  name: backup\n" });
      }),
      http.post("/api/v1/workloads/create", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, kind: "CronJob", name: "backup", status: "Idle", ready: "0/0", desired: 0 }, { status: 201 });
      }),
      http.get("/api/v1/workloads/cronjob/default/backup", () => HttpResponse.json({
        ...workload, kind: "CronJob", name: "backup", status: "Idle", ready: "0/0", desired: 0,
        conditions: [], selector: {}, pods: [], containers: [],
      })),
    );
    renderRoute("/workloads?namespace=default&kind=cronjob");

    fireEvent.click(await screen.findByRole("button", { name: "创建工作负载" }));
    const dialog = await screen.findByRole("dialog", { name: "创建工作负载" });
    fireEvent.change(within(dialog).getByLabelText("CronJob 名称"), { target: { value: "backup" } });
    fireEvent.change(within(dialog).getByLabelText("Cron 表达式"), { target: { value: "0 * * * *" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    await within(dialog).findByLabelText("容器镜像");
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    expect(await within(dialog).findByText(/kind: CronJob/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 CronJob" }));

    expect(await screen.findByRole("heading", { name: "backup" })).toBeInTheDocument();
    expect(createBody).toMatchObject({ kind: "cronjob", name: "backup", schedule: "0 * * * *", concurrencyPolicy: "Allow", suspend: false });
  });

  it("creates a workload with multiple containers, init containers and scheduling controls", async () => {
    let createBody: Record<string, unknown> | undefined;
    server.use(
      http.post("/api/v1/workloads/create/preview", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: multi\n" });
      }),
      http.post("/api/v1/workloads/create", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, name: "multi", images: ["nginx:alpine", "envoy:1"] }, { status: 201 });
      }),
      http.get("/api/v1/workloads/deployment/default/multi", () => HttpResponse.json({
        ...configuredWorkload(), name: "multi", images: ["nginx:alpine", "envoy:1"],
      })),
    );
    renderRoute("/workloads?namespace=default&kind=deployment");

    fireEvent.click(await screen.findByRole("button", { name: "创建工作负载" }));
    const dialog = await screen.findByRole("dialog", { name: "创建工作负载" });
    fireEvent.change(within(dialog).getByLabelText("Deployment 名称"), { target: { value: "multi" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    await within(dialog).findByLabelText("容器镜像");

    fireEvent.click(within(dialog).getByRole("button", { name: "添加应用容器" }));
    fireEvent.change(await within(dialog).findByLabelText("容器镜像 2"), { target: { value: "envoy:1" } });
    const sidecarName = dialog.querySelector<HTMLInputElement>("#workload-1-container-name");
    expect(sidecarName).not.toBeNull();
    fireEvent.change(sidecarName!, { target: { value: "sidecar" } });

    fireEvent.click(within(dialog).getByRole("button", { name: "添加 Init Container" }));
    fireEvent.change(await within(dialog).findByLabelText("Init Container 容器镜像"), { target: { value: "busybox:1.37" } });
    const initName = dialog.querySelector<HTMLInputElement>("#workload-init-0-container-name");
    expect(initName).not.toBeNull();
    fireEvent.change(initName!, { target: { value: "init-config" } });
    const initCommand = dialog.querySelector<HTMLTextAreaElement>("#workload-init-0-command");
    expect(initCommand).not.toBeNull();
    fireEvent.change(initCommand!, { target: { value: "sh\n-c\nwait-for-db" } });

    fireEvent.click(within(dialog).getByText("高级调度与 Pod 安全上下文"));
    fireEvent.change(within(dialog).getByLabelText("PriorityClass"), { target: { value: "high-priority" } });
    fireEvent.change(within(dialog).getByLabelText("ServiceAccount"), { target: { value: "multi-sa" } });
    fireEvent.change(within(dialog).getByLabelText("NodeSelector"), { target: { value: "node-pool=gpu" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "添加规则" }));
    fireEvent.change(within(dialog).getByLabelText("标签键"), { target: { value: "topology.kubernetes.io/zone" } });
    fireEvent.change(within(dialog).getByLabelText("值"), { target: { value: "az-a,az-b" } });
    fireEvent.change(within(dialog).getByLabelText("自定义标签"), { target: { value: "team=platform" } });

    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    expect(await within(dialog).findByText(/kind: Deployment/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 Deployment" }));
    expect(await screen.findByRole("heading", { name: "multi" })).toBeInTheDocument();
    expect(createBody).toMatchObject({
      kind: "deployment",
      name: "multi",
      labels: { team: "platform" },
      nodeSelector: { "node-pool": "gpu" },
      priorityClassName: "high-priority",
      serviceAccountName: "multi-sa",
      nodeAffinity: [{ key: "topology.kubernetes.io/zone", operator: "In", values: ["az-a", "az-b"] }],
    });
    const containers = createBody?.containers as Array<Record<string, unknown>>;
    const initContainers = createBody?.initContainers as Array<Record<string, unknown>>;
    expect(containers).toHaveLength(2);
    expect(containers[1]).toMatchObject({ name: "sidecar", image: "envoy:1" });
    expect(initContainers[0]).toMatchObject({ name: "init-config", image: "busybox:1.37", command: ["sh", "-c", "wait-for-db"] });
  });

  it("creates a workload with volumes, env sources, probes and runtime settings", async () => {
    let createBody: Record<string, unknown> | undefined;
    server.use(
      http.post("/api/v1/workloads/create/preview", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ yaml: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: advanced\n" });
      }),
      http.post("/api/v1/workloads/create", async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, name: "advanced", images: ["nginx:alpine"] }, { status: 201 });
      }),
      http.get("/api/v1/workloads/deployment/default/advanced", () => HttpResponse.json({
        ...configuredWorkload(), name: "advanced", images: ["nginx:alpine"],
      })),
    );
    renderRoute("/workloads?namespace=default&kind=deployment");

    fireEvent.click(await screen.findByRole("button", { name: "创建工作负载" }));
    const dialog = await screen.findByRole("dialog", { name: "创建工作负载" });
    fireEvent.change(within(dialog).getByLabelText("Deployment 名称"), { target: { value: "advanced" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    await within(dialog).findByLabelText("容器镜像");

    fireEvent.click(within(dialog).getByRole("button", { name: "添加存储卷" }));
    fireEvent.change(within(dialog).getByLabelText("卷名称"), { target: { value: "app-config" } });
    fireEvent.change(within(dialog).getByLabelText("ConfigMap 名称"), { target: { value: "web-config" } });

    fireEvent.click(within(dialog).getByRole("button", { name: "添加挂载" }));
    fireEvent.change(within(dialog).getByLabelText("挂载路径"), { target: { value: "/etc/app" } });
    fireEvent.click(within(dialog).getByLabelText("只读挂载"));

    fireEvent.click(within(dialog).getByRole("button", { name: "添加来源" }));
    fireEvent.change(within(dialog).getByLabelText("变量名称"), { target: { value: "TOKEN" } });
    fireEvent.change(within(dialog).getByLabelText("来源名称"), { target: { value: "app-secret" } });
    fireEvent.change(within(dialog).getByLabelText("Key"), { target: { value: "token" } });

    const readinessKind = dialog.querySelector<HTMLButtonElement>('[id="containers.0-readinessProbe-kind"]');
    expect(readinessKind).not.toBeNull();
    fireEvent.click(readinessKind!);
    fireEvent.click(await screen.findByRole("option", { name: "HTTP GET" }));
    const readinessPath = dialog.querySelector<HTMLInputElement>('[id="containers.0-readinessProbe-path"]');
    const readinessPort = dialog.querySelector<HTMLInputElement>('[id="containers.0-readinessProbe-port"]');
    expect(readinessPath).not.toBeNull();
    expect(readinessPort).not.toBeNull();
    fireEvent.change(readinessPath!, { target: { value: "/ready" } });
    fireEvent.change(readinessPort!, { target: { value: "8080" } });

    fireEvent.click(within(dialog).getByText("高级调度与 Pod 安全上下文"));
    fireEvent.change(within(dialog).getByLabelText("RuntimeClass"), { target: { value: "gvisor" } });
    fireEvent.change(within(dialog).getByLabelText("优雅终止（秒）"), { target: { value: "30" } });
    fireEvent.change(within(dialog).getByLabelText("ImagePullSecrets"), { target: { value: "registry-pull-secret" } });

    fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
    expect(await within(dialog).findByText(/kind: Deployment/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 Deployment" }));
    expect(await screen.findByRole("heading", { name: "advanced" })).toBeInTheDocument();

    expect(createBody).toMatchObject({
      kind: "deployment",
      name: "advanced",
      imagePullSecrets: ["registry-pull-secret"],
      runtimeClassName: "gvisor",
      terminationGracePeriodSeconds: 30,
      volumes: [{ name: "app-config", type: "configMap", source: "web-config" }],
    });
    const containers = createBody?.containers as Array<Record<string, unknown>>;
    expect(containers[0]).toMatchObject({
      volumeMounts: [{ name: "app-config", mountPath: "/etc/app", readOnly: true }],
      env: [{ name: "TOKEN", value: "", valueFrom: { kind: "secretKeyRef", name: "app-secret", key: "token" } }],
      readinessProbe: { type: "httpGet", path: "/ready", port: "8080", scheme: "HTTP" },
    });
  });

  it("shows rollout progress and sends deployment rollout controls", async () => {
    let rolloutBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/deployment/default/web", () => HttpResponse.json({
        ...configuredWorkload(),
        rollout: { paused: false, strategy: "RollingUpdate", maxSurge: "25%", maxUnavailable: "0", observedGeneration: 8, updatedReplicas: 1, readyReplicas: 1, availableReplicas: 1, unavailableReplicas: 1 },
        replicaSets: [{ name: "web-7d8f", revision: 2, current: true, desired: 2, ready: 1, available: 1, images: ["nginx:alpine"], createdAt: "2026-11-02T00:00:00Z" }],
      })),
      http.post("/api/v1/workloads/deployment/default/web/rollout", async ({ request }) => {
        rolloutBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/deployment/default/web");

    expect(await screen.findByText("发布进度")).toBeInTheDocument();
    expect(screen.getByText("web-7d8f")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "发布控制" }));
    const dialog = await screen.findByRole("dialog", { name: "发布控制" });
    fireEvent.click(within(dialog).getByRole("combobox", { name: "发布状态" }));
    fireEvent.click(await screen.findByRole("option", { name: "暂停发布" }));
    fireEvent.change(within(dialog).getByLabelText("maxSurge"), { target: { value: "50%" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存发布设置" }));

    await waitFor(() => expect(rolloutBody).toMatchObject({ paused: true, maxSurge: "50%", maxUnavailable: "0", resourceVersion: "317754" }));
    expect(await screen.findByText("Deployment 发布已暂停")).toBeInTheDocument();
  });

  it("updates StatefulSet partition rollout control", async () => {
    let rolloutBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/statefulset/default/db", () => HttpResponse.json({
        ...configuredWorkload(),
        kind: "StatefulSet",
        name: "db",
        images: ["postgres:17"],
        rollout: {
          strategy: "RollingUpdate", partition: 1, observedGeneration: 5,
          updatedReplicas: 1, readyReplicas: 3, availableReplicas: 3, unavailableReplicas: 0,
        },
      })),
      http.post("/api/v1/workloads/statefulset/default/db/rollout", async ({ request }) => {
        rolloutBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/statefulset/default/db");

    fireEvent.click(await screen.findByRole("button", { name: "发布控制" }));
    const dialog = await screen.findByRole("dialog", { name: "发布控制" });
    expect(within(dialog).getByLabelText("Partition")).toHaveValue(1);
    fireEvent.change(within(dialog).getByLabelText("Partition"), { target: { value: "2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存发布设置" }));

    await waitFor(() => expect(rolloutBody).toEqual({ partition: 2, resourceVersion: "317754" }));
    expect(await screen.findByText("StatefulSet 发布控制已更新")).toBeInTheDocument();
  });

  it("updates DaemonSet rollout strategy", async () => {
    let rolloutBody: Record<string, unknown> | undefined;
    server.use(
      http.get("/api/v1/workloads/daemonset/default/agent", () => HttpResponse.json({
        ...configuredWorkload(),
        kind: "DaemonSet",
        name: "agent",
        images: ["agent:2"],
        rollout: {
          strategy: "RollingUpdate", maxSurge: "0", maxUnavailable: "1", observedGeneration: 7,
          updatedReplicas: 5, readyReplicas: 5, availableReplicas: 5, unavailableReplicas: 0,
        },
      })),
      http.post("/api/v1/workloads/daemonset/default/agent/rollout", async ({ request }) => {
        rolloutBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ status: "updated" });
      }),
    );
    renderRoute("/workloads/daemonset/default/agent");

    fireEvent.click(await screen.findByRole("button", { name: "发布控制" }));
    const dialog = await screen.findByRole("dialog", { name: "发布控制" });
    fireEvent.change(within(dialog).getByLabelText("maxSurge"), { target: { value: "10%" } });
    fireEvent.change(within(dialog).getByLabelText("maxUnavailable"), { target: { value: "2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存发布设置" }));

    await waitFor(() => expect(rolloutBody).toMatchObject({ maxSurge: "10%", maxUnavailable: "2", resourceVersion: "317754" }));
    expect(await screen.findByText("DaemonSet 发布控制已更新")).toBeInTheDocument();
  });

  it("creates, edits and deletes autoscalers and disruption budgets", async () => {
    let hpaCreateBody: Record<string, unknown> | undefined;
    let hpaUpdateBody: Record<string, unknown> | undefined;
    let hpaDeleteVersion: string | null = null;
    let pdbCreateBody: Record<string, unknown> | undefined;
    let pdbUpdateBody: Record<string, unknown> | undefined;
    let pdbDeleteVersion: string | null = null;
    let autoscalers: AutoscalerSummary[] = [{
      name: "web-api", namespace: "default", resourceVersion: "31", targetKind: "Deployment", targetName: "web-api",
      minReplicas: 2, maxReplicas: 8, currentReplicas: 3, desiredReplicas: 3, status: "Ready",
      metrics: [{ type: "Resource", resource: "cpu", current: "45%", target: "80%", currentUtilization: 45, targetUtilization: 80 }],
      conditions: [{ type: "AbleToScale", status: "True", reason: "ReadyForNewScale" }], createdAt: "2026-11-02T00:00:00Z",
    }];
    let disruptionBudgets: DisruptionBudgetSummary[] = [{
      name: "web-api", namespace: "default", resourceVersion: "41", minAvailable: "2", selector: { app: "web" },
      currentHealthy: 3, desiredHealthy: 2, disruptionsAllowed: 1, expectedPods: 3, observedGeneration: 2, status: "Ready",
      conditions: [{ type: "DisruptionAllowed", status: "True", reason: "SufficientPods" }], createdAt: "2026-11-02T00:00:00Z",
    }];
    server.use(
      http.get("/api/v1/resilience/autoscalers", () => HttpResponse.json({ items: autoscalers, meta: { total: autoscalers.length } })),
      http.post("/api/v1/resilience/autoscalers", async ({ request }) => {
        hpaCreateBody = await request.json() as Record<string, unknown>;
        const created = {
          ...autoscalers[0], name: String(hpaCreateBody.name), resourceVersion: "32", targetName: String(hpaCreateBody.targetName),
        };
        autoscalers = [...autoscalers, created];
        return HttpResponse.json(created, { status: 201 });
      }),
      http.put("/api/v1/resilience/autoscalers/default/web-api", async ({ request }) => {
        hpaUpdateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...autoscalers[0], maxReplicas: 12, resourceVersion: "33" });
      }),
      http.delete("/api/v1/resilience/autoscalers/default/web-api", ({ request }) => {
        hpaDeleteVersion = new URL(request.url).searchParams.get("resourceVersion");
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
      http.get("/api/v1/resilience/disruption-budgets", () => HttpResponse.json({ items: disruptionBudgets, meta: { total: disruptionBudgets.length } })),
      http.post("/api/v1/resilience/disruption-budgets", async ({ request }) => {
        pdbCreateBody = await request.json() as Record<string, unknown>;
        const created = {
          ...disruptionBudgets[0], name: String(pdbCreateBody.name), resourceVersion: "42",
          minAvailable: String(pdbCreateBody.minAvailable), selector: pdbCreateBody.selector as Record<string, string>,
        };
        disruptionBudgets = [...disruptionBudgets, created];
        return HttpResponse.json(created, { status: 201 });
      }),
      http.put("/api/v1/resilience/disruption-budgets/default/web-api", async ({ request }) => {
        pdbUpdateBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...disruptionBudgets[0], minAvailable: "80%", resourceVersion: "43" });
      }),
      http.delete("/api/v1/resilience/disruption-budgets/default/web-api", ({ request }) => {
        pdbDeleteVersion = new URL(request.url).searchParams.get("resourceVersion");
        return HttpResponse.json({ status: "deleting" }, { status: 202 });
      }),
    );
    renderRoute("/resilience?namespace=default");

    expect(await screen.findByText("web-api", { selector: "h3" })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "创建 HPA" }));
    let dialog = await screen.findByRole("dialog", { name: "创建 HPA" });
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "worker-hpa" } });
    fireEvent.click(within(dialog).getByRole("combobox", { name: "选择目标工作负载" }));
    fireEvent.click(await screen.findByRole("option", { name: "手动输入" }));
    fireEvent.change(within(dialog).getByLabelText("目标工作负载"), { target: { value: "worker" } });
    fireEvent.change(within(dialog).getByLabelText("最小副本"), { target: { value: "2" } });
    fireEvent.change(within(dialog).getByLabelText("最大副本"), { target: { value: "10" } });
    fireEvent.change(within(dialog).getByLabelText("CPU 利用率目标（%）"), { target: { value: "70" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 HPA" }));
    await waitFor(() => expect(hpaCreateBody).toMatchObject({
      name: "worker-hpa", namespace: "default", targetKind: "Deployment", targetName: "worker",
      minReplicas: 2, maxReplicas: 10, cpuUtilization: 70,
    }));

    fireEvent.click(screen.getAllByRole("button", { name: "编辑" })[0]);
    dialog = await screen.findByRole("dialog", { name: "编辑 HPA" });
    fireEvent.change(within(dialog).getByLabelText("最大副本"), { target: { value: "12" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存 HPA" }));
    await waitFor(() => expect(hpaUpdateBody).toMatchObject({ maxReplicas: 12, resourceVersion: "31" }));

    fireEvent.click(screen.getAllByRole("button", { name: "删除" })[0]);
    dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(hpaDeleteVersion).toBe("31"));

    const pdbTab = screen.getByRole("tab", { name: /PDB 中断预算/ });
    fireEvent.mouseDown(pdbTab);
    fireEvent.click(pdbTab);
    expect(await screen.findByText("健康 / 期望")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "创建 PDB" }));
    dialog = await screen.findByRole("dialog", { name: "创建 PDB" });
    fireEvent.change(within(dialog).getByLabelText("名称"), { target: { value: "worker-pdb" } });
    fireEvent.change(within(dialog).getByLabelText("约束值"), { target: { value: "75%" } });
    fireEvent.change(within(dialog).getByLabelText("Pod 选择器"), { target: { value: "app=worker" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建 PDB" }));
    await waitFor(() => expect(pdbCreateBody).toMatchObject({
      name: "worker-pdb", namespace: "default", minAvailable: "75%", selector: { app: "worker" },
    }));

    fireEvent.click(screen.getAllByRole("button", { name: "编辑" })[0]);
    dialog = await screen.findByRole("dialog", { name: "编辑 PDB" });
    fireEvent.change(within(dialog).getByLabelText("约束值"), { target: { value: "80%" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存 PDB" }));
    await waitFor(() => expect(pdbUpdateBody).toMatchObject({
      name: "web-api", namespace: "default", minAvailable: "80%", selector: { app: "web" }, resourceVersion: "41",
    }));

    fireEvent.click(screen.getAllByRole("button", { name: "删除" })[0]);
    dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(pdbDeleteVersion).toBe("41"));
  });

  it("shows batch status and runs a selected CronJob container with overrides", async () => {
    const writableCapabilities = {
      ...session.capabilities,
      resourceActions: {
        ...session.capabilities.resourceActions!,
        jobs: { ...session.capabilities.resourceActions!.jobs, create: true },
      },
    };
    let runBody: Record<string, unknown> | undefined;
    const containers = [
      { ...configuredWorkload().containers[0], name: "app", image: "backup:1" },
      { ...configuredWorkload().containers[0], name: "worker", image: "worker:1" },
    ];
    server.use(
      http.get("/api/v1/session", () => HttpResponse.json({ ...session, capabilities: writableCapabilities })),
      http.get("/api/v1/capabilities", () => HttpResponse.json(writableCapabilities)),
      http.get("/api/v1/workloads/cronjob/default/backup", () => HttpResponse.json({
        ...workload,
        kind: "CronJob", name: "backup", status: "Idle", ready: "0/0", desired: 0,
        resourceVersion: "77", conditions: [], selector: {}, pods: [], volumes: [], containers,
        batch: {
          active: 1, succeeded: 4, failed: 1, completions: 1, parallelism: 1, backoffLimit: 3, ttlSecondsAfterFinished: 120,
          failureReason: "BackoffLimitExceeded", failureMessage: "backup-2: job has reached the specified backoff limit",
          activeJobs: [{ name: "backup-1", status: "Running", succeeded: 0, failed: 0, startTime: "2026-11-02T01:00:00Z" }],
        },
      })),
      http.post("/api/v1/workloads/cronjob/default/backup/run", async ({ request }) => {
        runBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ ...workload, kind: "Job", name: "backup-manual-test", namespace: "default" }, { status: 201 });
      }),
      http.get("/api/v1/workloads/job/default/backup-manual-test", () => HttpResponse.json({
        ...workload, kind: "Job", name: "backup-manual-test", conditions: [], selector: {}, pods: [], volumes: [], containers,
      })),
    );
    renderRoute("/workloads/cronjob/default/backup");

    expect(await screen.findByText("批处理状态")).toBeInTheDocument();
    expect(screen.getByText("BackoffLimitExceeded")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "backup-1" })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "立即执行" }));
    const dialog = await screen.findByRole("alertdialog", { name: "立即执行 backup？" });
    fireEvent.click(within(dialog).getByRole("combobox", { name: "目标容器" }));
    fireEvent.click(await screen.findByRole("option", { name: /worker/ }));
    fireEvent.change(within(dialog).getByLabelText("Command 覆盖（可选）"), { target: { value: "/bin/sh\n-c" } });
    fireEvent.change(within(dialog).getByLabelText("Args 覆盖（可选）"), { target: { value: "worker --once" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "确认执行" }));

    await waitFor(() => expect(runBody).toMatchObject({ containerName: "worker", command: ["/bin/sh", "-c"], args: ["worker --once"] }));
    expect(await screen.findByRole("heading", { name: "backup-manual-test" })).toBeInTheDocument();
  });

  it("exports the filtered audit snapshot as CSV", async () => {
    let actionsRequested = false;
    let exportRequest: URL | undefined;
    const createObjectURL = vi.fn(() => "blob:audit-export");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", class extends URL {
      static createObjectURL = createObjectURL;
      static revokeObjectURL = revokeObjectURL;
    });
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    server.use(
      http.get("/api/v1/audit-events/actions", () => {
        actionsRequested = true;
        return HttpResponse.json({ items: ["diagnose", "run-cronjob", "scale"] });
      }),
      http.get("/api/v1/audit-events/export", ({ request }) => {
        exportRequest = new URL(request.url);
        return new HttpResponse("id,timestamp,action\n42,2026-11-04T06:00:00Z,scale\n", {
          headers: {
            "Content-Type": "text/csv; charset=utf-8",
            "Content-Disposition": 'attachment; filename="dosh-audit-test.csv"',
          },
        });
      }),
    );
    renderRoute("/audit?namespace=production&result=success&action=scale&q=web");

    await waitFor(() => expect(actionsRequested).toBe(true));
    await waitFor(() => expect(exportRequest).toBeUndefined());
    fireEvent.pointerDown(await screen.findByRole("button", { name: "导出" }), { button: 0, ctrlKey: false, pointerType: "mouse" });
    fireEvent.click(await screen.findByRole("menuitem", { name: "导出 CSV" }));
    await waitFor(() => expect(exportRequest?.searchParams.get("format")).toBe("csv"));
    expect(exportRequest?.searchParams.get("namespace")).toBe("production");
    expect(exportRequest?.searchParams.get("result")).toBe("success");
    expect(exportRequest?.searchParams.get("action")).toBe("scale");
    expect(exportRequest?.searchParams.get("q")).toBe("web");
    expect(createObjectURL).toHaveBeenCalledTimes(1);
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
    await waitFor(() => expect(screen.queryByRole("button", { name: "创建工作负载" })).not.toBeInTheDocument());
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
      return HttpResponse.json({ items: events, meta: { total: events.length, totalKnown: true, page: 1, pageSize: 50, totalPages: 1, hasMore: false, filters: {} } });
    }));
    renderRoute("/events");

    expect(await screen.findByText("共 12 条事件")).toBeInTheDocument();
    await waitFor(() => expect(eventQuery?.get("pageSize")).toBe("50"));
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

  it("keeps the active network tab usable when another resource is forbidden", async () => {
    server.use(
      http.get("/api/v1/services", () => HttpResponse.json({ items: [{
        name: "web", namespace: "default", type: "ClusterIP", clusterIP: "10.96.0.10",
        ports: [{ name: "http", port: 80, targetPort: "80", protocol: "TCP" }],
        selector: { app: "web" }, endpointCount: 1, createdAt: "2026-11-02T00:00:00Z",
      }], meta: { total: 1 } })),
      http.get("/api/v1/ingresses", () => HttpResponse.json({ code: "forbidden", message: "denied" }, { status: 403 })),
      http.get("/api/v1/ingressclasses", () => HttpResponse.json({ code: "forbidden", message: "denied" }, { status: 403 })),
    );
    renderRoute("/network");

    expect(await screen.findByRole("cell", { name: "web" })).toBeInTheDocument();
    expect(screen.queryByText("没有访问权限")).not.toBeInTheDocument();

    const ingressTab = screen.getByRole("tab", { name: /Ingress/ });
    fireEvent.mouseDown(ingressTab);
    fireEvent.click(ingressTab);
    expect(await screen.findByText("没有访问权限")).toBeInTheDocument();
  });

  it("explains NetworkPolicy, discovers Gateway API and diagnoses Service connectivity", async () => {
    server.use(http.get("/api/v1/services", () => HttpResponse.json({
      items: [{ name: "web", namespace: "default", type: "ClusterIP", headless: false, clusterIP: "10.96.0.10", externalIP: "", ports: serviceDetail.ports, selector: { app: "web" }, endpointCount: 1, createdAt: "2026-11-02T00:00:00Z" }],
      meta: { total: 1, totalKnown: true, page: 1, pageSize: 200, totalPages: 1, hasMore: false },
    })));
    renderRoute("/network?namespace=default&tab=policies");

    expect(await screen.findByText("default/web-policy")).toBeInTheDocument();
    expect(screen.getByText(/DNS 53\/TCP/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "编辑 YAML" })).toHaveAttribute("href", "/resources/networking.k8s.io/v1/networkpolicies/web-policy?namespace=default");

    const gatewayTab = screen.getByRole("tab", { name: "Gateway API" });
    fireEvent.mouseDown(gatewayTab);
    fireEvent.click(gatewayTab);
    expect((await screen.findAllByText("public")).length).toBeGreaterThan(0);
    expect(screen.getByText("default/edge")).toBeInTheDocument();
    expect(screen.getByText("HTTPRoute default/web")).toBeInTheDocument();

    const connectivityTab = screen.getByRole("tab", { name: "连通性诊断" });
    fireEvent.mouseDown(connectivityTab);
    fireEvent.click(connectivityTab);
    const sourcePodSelect = await screen.findByRole("combobox", { name: "源 Pod" });
    await waitFor(() => expect(sourcePodSelect).toBeEnabled());
    fireEvent.click(sourcePodSelect);
    fireEvent.click(await screen.findByRole("option", { name: "web-abcdef-12345" }));
    const targetServiceSelect = screen.getByRole("combobox", { name: "目标 Service" });
    await waitFor(() => expect(targetServiceSelect).toBeEnabled());
    fireEvent.click(targetServiceSelect);
    fireEvent.click(await screen.findByRole("option", { name: "web" }));
    fireEvent.click(screen.getByRole("button", { name: "开始诊断" }));

    expect(await screen.findByText("目标 Pod Ingress 策略拒绝该流量")).toBeInTheDocument();
    expect(screen.getByText("被阻断")).toBeInTheDocument();
    expect(screen.getByText(/ingress policy web-policy/)).toBeInTheDocument();
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

  it("browses a discovered resource type and opens generic metadata and YAML detail", async () => {
    const router = renderRoute("/resources");

    expect(await screen.findByRole("heading", { name: "资源浏览器" })).toBeInTheDocument();
    const podLink = await screen.findByRole("link", { name: "Pod" });
    expect(screen.getByText("example.io/v1")).toBeInTheDocument();
    fireEvent.click(podLink);
    await waitFor(() => expect(router.state.location.pathname).toBe("/resources/_core/v1/pods"));

    expect(await screen.findByRole("heading", { name: "Pod" })).toBeInTheDocument();
    expect(await screen.findByText("Namespaced · core/v1 · 当前读取受 Kubernetes RBAC 约束。")).toBeInTheDocument();
    const resourceLink = await screen.findByRole("link", { name: "web-abcdef-12345" });
    expect(resourceLink).toHaveAttribute("href", "/resources/_core/v1/pods/web-abcdef-12345?namespace=default");
    expect(await screen.findByText("Running")).toBeInTheDocument();
    fireEvent.click(resourceLink);

    expect(await screen.findByRole("heading", { name: "web-abcdef-12345" })).toBeInTheDocument();
    expect(screen.getByText("pod-uid")).toBeInTheDocument();
    expect(screen.getByText("owner")).toBeInTheDocument();
    const yamlTab = screen.getByRole("tab", { name: /YAML/ });
    fireEvent.mouseDown(yamlTab);
    fireEvent.click(yamlTab);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ tab: "yaml" }));
    expect(await screen.findByText(/只读 YAML/)).toBeInTheDocument();
  });
});
