import { describe, expect, it } from "vitest";
import type { PodSummary, WorkloadSummary } from "@/api/types";
import { summarizeWorkspace } from "@/features/workspaces/workspace-model";

const workload: WorkloadSummary = {
  kind: "Deployment",
  name: "web",
  namespace: "default",
  status: "Running",
  ready: "2/2",
  desired: 2,
  restarts: 0,
  images: ["nginx:alpine"],
  createdAt: "2026-11-19T00:00:00Z",
};

const pod: PodSummary = {
  name: "web-1",
  namespace: "default",
  status: "Running",
  ready: "1/1",
  restarts: 0,
  ip: "10.0.0.10",
  node: "worker-a",
  createdAt: "2026-11-19T00:00:00Z",
  hasMetrics: false,
};

describe("workspace model", () => {
  it("reports a healthy namespace from ready workloads and pods", () => {
    const health = summarizeWorkspace({
      workloads: [workload],
      pods: [pod, { ...pod, name: "completed", status: "Succeeded", ready: "0/1" }],
      services: [],
      ingresses: [],
      configMaps: [],
      claims: [],
      autoscalers: [],
      disruptionBudgets: [],
      warningEvents: [],
    });

    expect(health.status).toBe("healthy");
    expect(health.counts.readyWorkloads).toBe(1);
    expect(health.counts.pods).toBe(1);
    expect(health.counts.readyPods).toBe(1);
    expect(health.issues).toEqual([]);
  });

  it("treats completed and intentionally idle workloads as healthy", () => {
    const health = summarizeWorkspace({
      workloads: [
        { ...workload, kind: "Job", name: "migration", status: "Complete", ready: "1/1", desired: 1 },
        { ...workload, kind: "CronJob", name: "backup", status: "Suspended", ready: "0", desired: 1 },
      ],
      pods: [],
      services: [],
      ingresses: [],
      configMaps: [],
      claims: [],
      autoscalers: [],
      disruptionBudgets: [],
      warningEvents: [],
    });

    expect(health.status).toBe("healthy");
    expect(health.counts.readyWorkloads).toBe(2);
    expect(health.issues).toEqual([]);
  });

  it("aggregates workload, pod, storage, PDB and event issues", () => {
    const health = summarizeWorkspace({
      workloads: [{ ...workload, status: "Updating", ready: "1/2" }],
      pods: [{ ...pod, status: "CrashLoopBackOff", ready: "0/1", restarts: 4 }],
      services: [],
      ingresses: [],
      configMaps: [],
      claims: [{ name: "data", namespace: "default", status: "Pending", accessModes: ["ReadWriteOnce"], storageClass: "", capacityBytes: 0, createdAt: "2026-11-19T00:00:00Z" }],
      autoscalers: [],
      disruptionBudgets: [{ name: "web-pdb", namespace: "default", selector: { app: "web" }, currentHealthy: 1, desiredHealthy: 2, disruptionsAllowed: 0, expectedPods: 2, observedGeneration: 1, status: "Blocked", conditions: [], createdAt: "2026-11-19T00:00:00Z" }],
      warningEvents: [{ uid: "event-1", namespace: "default", type: "Warning", reason: "BackOff", message: "retrying", objectKind: "Pod", objectName: "web-1", count: 3, firstSeen: "2026-11-19T00:00:00Z", lastSeen: "2026-11-19T00:01:00Z" }],
    });

    expect(health.status).toBe("degraded");
    expect(health.counts.restarts).toBe(4);
    expect(health.issues).toHaveLength(5);
    expect(health.issues.join(" ")).toContain("CrashLoopBackOff");
  });

  it("distinguishes an empty application workspace", () => {
    const health = summarizeWorkspace({
      workloads: [], pods: [], services: [], ingresses: [], configMaps: [], claims: [], autoscalers: [], disruptionBudgets: [], warningEvents: [],
    });
    expect(health.status).toBe("empty");
    expect(health.summary).toContain("尚未运行工作负载");
  });

  it("degrades the conclusion when a data section is unavailable", () => {
    const health = summarizeWorkspace({
      workloads: [workload], pods: [pod], services: [], ingresses: [], configMaps: [], claims: [], autoscalers: [], disruptionBudgets: [], warningEvents: [],
      errors: ["Pod 数据加载失败"],
    });
    expect(health.status).toBe("degraded");
    expect(health.issues[0]).toContain("健康结论可能不完整");
  });
});
