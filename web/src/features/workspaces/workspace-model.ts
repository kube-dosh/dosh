import type {
  AutoscalerSummary,
  ConfigMapSummary,
  DisruptionBudgetSummary,
  EventItem,
  IngressSummary,
  PersistentVolumeClaim,
  PodSummary,
  ServiceSummary,
  WorkloadSummary,
} from "@/api/types";

export const workspacePageSize = 200;

export type WorkspaceHealthStatus = "healthy" | "degraded" | "empty";

export interface WorkspaceCounts {
  workloads: number;
  readyWorkloads: number;
  pods: number;
  readyPods: number;
  restarts: number;
  services: number;
  ingresses: number;
  configMaps: number;
  claims: number;
  autoscalers: number;
  disruptionBudgets: number;
  warningEvents: number;
}

export interface WorkspaceHealth {
  status: WorkspaceHealthStatus;
  summary: string;
  issues: string[];
  counts: WorkspaceCounts;
}

export interface WorkspaceSnapshot {
  workloads: WorkloadSummary[];
  pods: PodSummary[];
  services: ServiceSummary[];
  ingresses: IngressSummary[];
  configMaps: ConfigMapSummary[];
  claims: PersistentVolumeClaim[];
  autoscalers: AutoscalerSummary[];
  disruptionBudgets: DisruptionBudgetSummary[];
  warningEvents: EventItem[];
  errors?: string[];
}

function readyFraction(value: string): { ready: number; total: number } | undefined {
  const match = /^(\d+)\s*\/\s*(\d+)$/.exec(value.trim());
  if (!match) return undefined;
  return { ready: Number(match[1]), total: Number(match[2]) };
}

function workloadReady(workload: WorkloadSummary): boolean {
  const status = workload.status.toLowerCase();
  if (status === "failed" || status === "pending" || status === "updating") return false;
  if (["complete", "completed", "succeeded", "idle", "suspended"].includes(status)) return true;
  if (workload.desired <= 0) return true;
  const ready = readyFraction(workload.ready);
  if (ready) return ready.total > 0 && ready.ready >= ready.total;
  return false;
}

function completedPod(pod: PodSummary): boolean {
  return pod.status === "Succeeded" || pod.status === "Completed";
}

function terminatingPod(pod: PodSummary): boolean {
  return pod.status === "Terminating";
}

function podReady(pod: PodSummary): boolean {
  if (completedPod(pod)) return true;
  const ready = readyFraction(pod.ready);
  return Boolean(ready && ready.total > 0 && ready.ready >= ready.total);
}

function podIssue(pod: PodSummary): boolean {
  if (completedPod(pod) || terminatingPod(pod)) return false;
  return !podReady(pod) || ["Pending", "Failed", "Unknown"].includes(pod.status);
}

function names(items: string[], limit = 3): string {
  const unique = Array.from(new Set(items));
  if (unique.length <= limit) return unique.join("、");
  return `${unique.slice(0, limit).join("、")} 等 ${unique.length} 项`;
}

export function summarizeWorkspace(snapshot: WorkspaceSnapshot): WorkspaceHealth {
  const activePods = snapshot.pods.filter((pod) => !completedPod(pod));
  const readyPods = activePods.filter(podReady).length;
  const readyWorkloads = snapshot.workloads.filter(workloadReady).length;
  const restarts = snapshot.pods.reduce((total, pod) => total + pod.restarts, 0);
  const unreadyWorkloads = snapshot.workloads.filter((workload) => !workloadReady(workload));
  const unreadyPods = activePods.filter(podIssue);
  const unhealthyClaims = snapshot.claims.filter((claim) => ["Pending", "Lost"].includes(claim.status));
  const blockedBudgets = snapshot.disruptionBudgets.filter((budget) => budget.expectedPods > 0 && budget.disruptionsAllowed === 0);
  const unavailableAutoscalers = snapshot.autoscalers.filter((autoscaler) => ["Failed", "Unavailable"].includes(autoscaler.status));
  const issues: string[] = [];

  if (unreadyWorkloads.length) {
    issues.push(`${unreadyWorkloads.length} 个工作负载未就绪：${names(unreadyWorkloads.map((item) => `${item.name} (${item.ready})`))}`);
  }
  if (unreadyPods.length) {
    issues.push(`${unreadyPods.length} 个 Pod 未就绪：${names(unreadyPods.map((item) => `${item.name} (${item.status})`))}`);
  }
  if (unhealthyClaims.length) {
    issues.push(`${unhealthyClaims.length} 个 PVC 处于 Pending 或 Lost：${names(unhealthyClaims.map((item) => item.name))}`);
  }
  if (blockedBudgets.length) {
    issues.push(`${blockedBudgets.length} 个 PDB 当前不允许驱逐：${names(blockedBudgets.map((item) => item.name))}`);
  }
  if (unavailableAutoscalers.length) {
    issues.push(`${unavailableAutoscalers.length} 个 HPA 状态不可用：${names(unavailableAutoscalers.map((item) => item.name))}`);
  }
  if (snapshot.warningEvents.length) {
    issues.push(`最近有 ${snapshot.warningEvents.length} 条 Warning 事件`);
  }
  if (snapshot.errors?.length) {
    issues.push(`部分数据不可用，健康结论可能不完整：${snapshot.errors.join("、")}`);
  }

  const counts: WorkspaceCounts = {
    workloads: snapshot.workloads.length,
    readyWorkloads,
    pods: activePods.length,
    readyPods,
    restarts,
    services: snapshot.services.length,
    ingresses: snapshot.ingresses.length,
    configMaps: snapshot.configMaps.length,
    claims: snapshot.claims.length,
    autoscalers: snapshot.autoscalers.length,
    disruptionBudgets: snapshot.disruptionBudgets.length,
    warningEvents: snapshot.warningEvents.length,
  };

  if (!snapshot.workloads.length && !activePods.length && !issues.length) {
    return {
      status: "empty",
      summary: "该命名空间尚未运行工作负载。可从工作负载页面创建应用，或切换到其他命名空间。",
      issues,
      counts,
    };
  }

  if (issues.length) {
    return {
      status: "degraded",
      summary: `${readyWorkloads}/${snapshot.workloads.length} 个工作负载就绪，${readyPods}/${activePods.length} 个 Pod 就绪；存在 ${issues.length} 项需要关注。`,
      issues,
      counts,
    };
  }

  return {
    status: "healthy",
    summary: `${readyWorkloads}/${snapshot.workloads.length} 个工作负载就绪，${readyPods}/${activePods.length} 个 Pod 就绪；未发现 Warning 事件或未就绪依赖。`,
    issues,
    counts,
  };
}
