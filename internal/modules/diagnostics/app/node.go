package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

func (s *Service) diagnoseNode(ctx context.Context, actor domain.Actor, name string) (*reportBuilder, error) {
	node, err := s.repository.GetNode(ctx, name)
	if err != nil {
		return nil, err
	}
	report := newReport("Node", "", name, node.Status)
	report.relatedResource("Node", "", name, node.Status, "当前对象", nodePath(name))

	if node.Status == "Ready" {
		report.check("ready", "节点就绪", domain.DiagnosisCheckPass, "Kubelet 报告节点 Ready")
	} else {
		report.check("ready", "节点就绪", domain.DiagnosisCheckCritical, "节点当前为 "+fallback(node.Status, "Unknown"))
		message := "节点未通过 Ready Condition，工作负载可能被驱逐或无法调度到该节点。"
		if condition, ok := podCondition(node.Conditions, "Ready"); ok {
			message = fallback(condition.Message, fallback(condition.Reason, message))
		}
		report.finding(domain.DiagnosisFinding{
			ID: "node.not-ready", Severity: domain.DiagnosisCritical, Title: "节点未就绪",
			Cause: message,
			Evidence: []domain.DiagnosisEvidence{
				{Label: "状态", Value: fallback(node.Status, "Unknown")},
				{Label: "Kubelet", Value: fallback(node.Version, "Unknown")},
				{Label: "运行时", Value: fallback(node.ContainerRuntime, "Unknown")},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看节点条件", Path: nodePath(name)},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe node %s", name)},
			},
		})
	}

	s.collectNodeConditions(node, report)
	s.collectNodePods(node, report)
	s.collectNodeCapacity(node, report)
	if len(node.Taints) > 0 {
		evidence := make([]domain.DiagnosisEvidence, 0, len(node.Taints))
		for index, taint := range node.Taints {
			if index >= 8 {
				break
			}
			value := taint.Key
			if taint.Value != "" {
				value += "=" + taint.Value
			}
			evidence = append(evidence, domain.DiagnosisEvidence{Label: taint.Effect, Value: value})
		}
		report.check("taints", "节点 Taints", domain.DiagnosisCheckInfo, fmt.Sprintf("节点包含 %d 个 Taint", len(node.Taints)))
		report.finding(domain.DiagnosisFinding{
			ID: "node.taints", Severity: domain.DiagnosisInfo, Title: "节点包含可能影响调度的 Taint",
			Cause:           "NoSchedule 与 NoExecute 会影响新 Pod 调度或驱逐现有 Pod；请确认这些限制是预期配置。",
			Evidence:        evidence,
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看与编辑 Taints", Path: nodePath(name) + "?tab=taints"}},
		})
	} else {
		report.check("taints", "节点 Taints", domain.DiagnosisCheckPass, "节点没有 Taint")
	}

	if node.Unschedulable {
		report.check("scheduling", "调度状态", domain.DiagnosisCheckInfo, "节点已 Cordon，新 Pod 不会被调度到该节点")
		report.finding(domain.DiagnosisFinding{
			ID: "node.cordoned", Severity: domain.DiagnosisInfo, Title: "节点已停止调度",
			Cause:           "节点处于 Cordon 状态。如果这是维护中的预期状态则无需处理，否则新工作负载将无法调度到该节点。",
			Evidence:        []domain.DiagnosisEvidence{{Label: "Unschedulable", Value: "true"}},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "节点详情", Path: nodePath(name)}},
		})
	} else {
		report.check("scheduling", "调度状态", domain.DiagnosisCheckPass, "节点允许新 Pod 调度")
	}

	s.collectEvents(ctx, actor, "", "Node", name, nodePath(name)+"?tab=events", report)
	return report, nil
}

func (s *Service) collectNodeConditions(node domain.NodeDetail, report *reportBuilder) {
	if len(node.Conditions) == 0 {
		report.check("pressure", "节点健康 Condition", domain.DiagnosisCheckInfo, "节点未报告额外 Condition")
		return
	}
	status := domain.DiagnosisCheckPass
	message := "未发现 MemoryPressure、DiskPressure、PIDPressure 或网络异常"
	for _, condition := range node.Conditions {
		lowerType := strings.ToLower(condition.Type)
		if lowerType == "ready" {
			continue
		}
		abnormal := false
		severity := domain.DiagnosisWarning
		switch condition.Type {
		case "MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable":
			abnormal = condition.Status == "True"
			if condition.Type == "NetworkUnavailable" && abnormal {
				severity = domain.DiagnosisCritical
			}
		default:
			abnormal = condition.Status == "False" && strings.Contains(lowerType, "pressure")
		}
		if !abnormal {
			continue
		}
		candidate := domain.DiagnosisCheckWarning
		if severity == domain.DiagnosisCritical {
			candidate = domain.DiagnosisCheckCritical
		}
		if severityRank(severityFromCheck(candidate)) < severityRank(severityFromCheck(status)) {
			status = candidate
		}
		message = "至少一个节点健康 Condition 报告异常"
		detail := fallback(condition.Message, fallback(condition.Reason, condition.Type+"="+condition.Status))
		report.finding(domain.DiagnosisFinding{
			ID: "node.condition." + strings.ToLower(condition.Type), Severity: severity,
			Title: "节点报告 " + condition.Type,
			Cause: detail,
			Evidence: []domain.DiagnosisEvidence{
				{Label: "Condition", Value: condition.Type + "=" + condition.Status},
				{Label: "原因", Value: fallback(condition.Reason, "Unknown")},
				{Label: "消息", Value: fallback(condition.Message, "未提供")},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看节点详情", Path: nodePath(node.Name)},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe node %s", node.Name)},
			},
		})
	}
	report.check("pressure", "节点健康 Condition", status, message)
}

func (s *Service) collectNodePods(node domain.NodeDetail, report *reportBuilder) {
	if len(node.Pods) == 0 {
		report.check("pods", "节点上的 Pod", domain.DiagnosisCheckInfo, "节点当前没有运行 Pod")
		return
	}
	unhealthy := make([]domain.PodSummary, 0)
	restarted := make([]domain.PodSummary, 0)
	for _, pod := range node.Pods {
		if podNeedsAttention(pod) {
			unhealthy = append(unhealthy, pod)
		}
		if pod.Restarts > 0 {
			restarted = append(restarted, pod)
		}
	}
	status := domain.DiagnosisCheckPass
	message := fmt.Sprintf("%d 个 Pod 未发现失败或未就绪状态", len(node.Pods))
	if len(unhealthy) > 0 {
		status = domain.DiagnosisCheckWarning
		for _, pod := range unhealthy {
			if criticalPodStatus(pod.Status) {
				status = domain.DiagnosisCheckCritical
				break
			}
		}
		message = fmt.Sprintf("%d/%d 个 Pod 需要关注", len(unhealthy), len(node.Pods))
	}
	report.check("pods", "节点上的 Pod", status, message)
	if len(unhealthy) > 0 {
		evidence := make([]domain.DiagnosisEvidence, 0, 5)
		for index, pod := range unhealthy {
			if index >= 5 {
				break
			}
			evidence = append(evidence, domain.DiagnosisEvidence{Label: pod.Namespace + "/" + pod.Name, Value: pod.Status + " · " + pod.Ready + " ready"})
		}
		report.finding(domain.DiagnosisFinding{
			ID: "node.unhealthy-pods", Severity: severityFromCheck(status), Title: "节点上存在异常 Pod",
			Cause:    "节点压力、运行时或网络异常可能同时影响多个工作负载。请结合 Pod 状态与节点 Condition 判断是否为节点级问题。",
			Evidence: evidence,
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看节点上的 Pod", Path: nodePath(node.Name) + "?tab=pods"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl get pods --all-namespaces --field-selector spec.nodeName=%s", node.Name)},
			},
		})
	}
	if len(restarted) > 0 {
		evidence := make([]domain.DiagnosisEvidence, 0, 3)
		for index, pod := range restarted {
			if index >= 3 {
				break
			}
			evidence = append(evidence, domain.DiagnosisEvidence{Label: pod.Namespace + "/" + pod.Name, Value: strconv.Itoa(int(pod.Restarts)) + " 次重启"})
		}
		report.finding(domain.DiagnosisFinding{
			ID: "node.restarts", Severity: domain.DiagnosisWarning, Title: "节点上的 Pod 发生过重启",
			Cause:           "多个 Pod 重启可能意味着节点压力、运行时异常或共享依赖故障。",
			Evidence:        evidence,
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看节点上的 Pod", Path: nodePath(node.Name) + "?tab=pods"}},
		})
	}
	for index, pod := range node.Pods {
		if index >= maxRelatedPods {
			break
		}
		if podNeedsAttention(pod) {
			report.relatedResource("Pod", pod.Namespace, pod.Name, pod.Status, "运行于该节点且需要关注", podPath(pod.Namespace, pod.Name))
		}
	}
}

func (s *Service) collectNodeCapacity(node domain.NodeDetail, report *reportBuilder) {
	if node.CPUCapacityMilli > 0 && node.CPURequestsMilli > node.CPUCapacityMilli {
		report.finding(domain.DiagnosisFinding{
			ID: "node.cpu-overcommit", Severity: domain.DiagnosisWarning, Title: "节点 CPU Requests 超过可分配容量",
			Cause: "Pod 的 CPU Requests 总和已超过节点可分配 CPU，新 Pod 可能无法调度。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "Requests", Value: formatMilliCPU(node.CPURequestsMilli)},
				{Label: "可分配", Value: formatMilliCPU(node.CPUCapacityMilli)},
			},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看节点容量", Path: nodePath(node.Name)}},
		})
	}
	if node.MemoryCapacityBytes > 0 && node.MemoryRequestsBytes > node.MemoryCapacityBytes {
		report.finding(domain.DiagnosisFinding{
			ID: "node.memory-overcommit", Severity: domain.DiagnosisWarning, Title: "节点内存 Requests 超过可分配容量",
			Cause: "Pod 的内存 Requests 总和已超过节点可分配内存，新 Pod 可能无法调度。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "Requests", Value: formatBytes(node.MemoryRequestsBytes)},
				{Label: "可分配", Value: formatBytes(node.MemoryCapacityBytes)},
			},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看节点容量", Path: nodePath(node.Name)}},
		})
	}
	if node.PodCapacity > 0 && int64(node.PodCount) >= node.PodCapacity {
		report.finding(domain.DiagnosisFinding{
			ID: "node.pod-capacity", Severity: domain.DiagnosisWarning, Title: "节点 Pod 数量已达到容量上限",
			Cause: "节点已达到可分配 Pod 数量上限，新 Pod 无法调度到该节点。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "当前 Pod", Value: strconv.Itoa(node.PodCount)},
				{Label: "容量", Value: strconv.FormatInt(node.PodCapacity, 10)},
			},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看节点容量", Path: nodePath(node.Name)}},
		})
	}
	if !node.HasMetrics {
		report.check("usage", "资源使用率", domain.DiagnosisCheckSkipped, "Metrics Server 未提供节点实时使用率")
		return
	}
	status := domain.DiagnosisCheckPass
	message := "CPU 与内存使用率低于 90% 阈值"
	if node.CPUCapacityMilli > 0 && node.CPUUsageMilli*100 >= node.CPUCapacityMilli*90 {
		status = domain.DiagnosisCheckWarning
		message = "CPU 使用率已达到或超过 90%"
		report.finding(domain.DiagnosisFinding{
			ID: "node.cpu-pressure", Severity: domain.DiagnosisWarning, Title: "节点 CPU 使用率较高",
			Cause: "CPU 使用率接近容量上限时，Pod 可能被限流并出现请求延迟。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "当前使用", Value: formatMilliCPU(node.CPUUsageMilli)},
				{Label: "可分配", Value: formatMilliCPU(node.CPUCapacityMilli)},
			},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看节点详情", Path: nodePath(node.Name)}},
		})
	}
	if node.MemoryCapacityBytes > 0 && node.MemoryUsageBytes*100 >= node.MemoryCapacityBytes*90 {
		status = domain.DiagnosisCheckWarning
		message = "内存使用率已达到或超过 90%"
		report.finding(domain.DiagnosisFinding{
			ID: "node.memory-pressure", Severity: domain.DiagnosisWarning, Title: "节点内存使用率较高",
			Cause: "内存使用率接近容量上限时，Pod 可能被 OOMKilled 或触发节点驱逐。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "当前使用", Value: formatBytes(node.MemoryUsageBytes)},
				{Label: "可分配", Value: formatBytes(node.MemoryCapacityBytes)},
			},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "查看节点详情", Path: nodePath(node.Name)}},
		})
	}
	report.check("usage", "资源使用率", status, message)
}
