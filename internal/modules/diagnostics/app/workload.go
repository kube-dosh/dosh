package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

const maxWorkloadPodDiagnoses = 2

func (s *Service) diagnoseWorkload(ctx context.Context, actor domain.Actor, kind, namespace, name string) (*reportBuilder, error) {
	workload, err := s.repository.GetWorkload(ctx, kind, namespace, name)
	if err != nil {
		return nil, err
	}
	report := newReport(workload.Kind, namespace, name, workload.Status)
	report.relatedResource(workload.Kind, namespace, name, workload.Status, "当前对象", workloadPath(kind, namespace, name))
	report.check("status", "工作负载状态", workloadStatusCheck(workload), "当前状态为 "+fallback(workload.Status, "Unknown")+"，就绪 "+fallback(workload.Ready, "0/0"))

	ready, desired, hasReplicaCounts := parseReadyCounts(workload.Ready)
	switch kind {
	case "cronjob":
		s.diagnoseCronJobStatus(workload, report)
	case "job":
		s.diagnoseJobStatus(workload, ready, desired, hasReplicaCounts, report)
	default:
		s.diagnoseReplicaStatus(workload, ready, desired, hasReplicaCounts, report)
	}

	s.collectWorkloadConditions(workload, report)
	s.collectWorkloadPods(ctx, actor, workload, report)
	s.collectEvents(ctx, actor, namespace, workload.Kind, name, workloadPath(kind, namespace, name)+"?tab=events", report)
	return report, nil
}

func workloadStatusCheck(workload domain.WorkloadDetail) string {
	switch workload.Status {
	case "Running", "Complete", "Succeeded":
		return domain.DiagnosisCheckPass
	case "Failed":
		return domain.DiagnosisCheckCritical
	case "Pending", "Updating", "Progressing":
		return domain.DiagnosisCheckWarning
	default:
		return domain.DiagnosisCheckInfo
	}
}

func (s *Service) diagnoseReplicaStatus(workload domain.WorkloadDetail, ready, desired int, hasReplicaCounts bool, report *reportBuilder) {
	if workload.Status == "Failed" {
		report.finding(domain.DiagnosisFinding{
			ID: "workload.failed", Severity: domain.DiagnosisCritical, Title: "工作负载处于 Failed 状态",
			Cause: "控制器报告副本创建或 Pod 调度持续失败，当前工作负载可能没有可用实例。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "状态", Value: workload.Status},
				{Label: "就绪副本", Value: fallback(workload.Ready, fmt.Sprintf("%d/%d", ready, desired))},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看实例与事件", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=pods"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl rollout status %s/%s -n %s", strings.ToLower(workload.Kind), workload.Name, workload.Namespace)},
			},
		})
	}
	if !hasReplicaCounts || desired == 0 {
		if desired == 0 {
			report.check("availability", "副本可用性", domain.DiagnosisCheckInfo, "工作负载已缩容到 0，不期望运行实例")
		}
		return
	}
	if ready >= desired {
		report.check("availability", "副本可用性", domain.DiagnosisCheckPass, fmt.Sprintf("%d/%d 个副本已就绪", ready, desired))
		return
	}
	status := domain.DiagnosisCheckWarning
	severity := domain.DiagnosisWarning
	if ready == 0 || workload.Status == "Failed" {
		status = domain.DiagnosisCheckCritical
		severity = domain.DiagnosisCritical
	}
	report.check("availability", "副本可用性", status, fmt.Sprintf("仅 %d/%d 个副本就绪", ready, desired))
	report.finding(domain.DiagnosisFinding{
		ID: "workload.availability", Severity: severity, Title: "可用副本少于期望数量",
		Cause: "控制器尚未达到期望副本数。常见原因包括镜像拉取失败、调度受限、探针失败、资源不足或滚动更新尚未完成。",
		Evidence: []domain.DiagnosisEvidence{
			{Label: "就绪副本", Value: fmt.Sprintf("%d/%d", ready, desired)},
			{Label: "工作负载状态", Value: fallback(workload.Status, "Unknown")},
			{Label: "关联 Pod", Value: strconv.Itoa(len(workload.Pods))},
		},
		Recommendations: []domain.DiagnosisRecommendation{
			{Label: "查看实例列表", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=pods"},
			{Label: "查看事件", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=events"},
			{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe %s %s -n %s", strings.ToLower(workload.Kind), workload.Name, workload.Namespace)},
		},
	})
}

func (s *Service) diagnoseJobStatus(workload domain.WorkloadDetail, ready, desired int, hasReplicaCounts bool, report *reportBuilder) {
	if workload.Status == "Failed" {
		report.finding(domain.DiagnosisFinding{
			ID: "job.failed", Severity: domain.DiagnosisCritical, Title: "Job 未成功完成",
			Cause: "Job 已达到失败条件，控制器不会继续创建新的 Pod 重试。请检查失败 Pod 的退出码、日志和 backoffLimit。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "完成数", Value: fallback(workload.Ready, fmt.Sprintf("%d/%d", ready, desired))},
				{Label: "关联 Pod", Value: strconv.Itoa(len(workload.Pods))},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看实例", Path: workloadPath("job", workload.Namespace, workload.Name) + "?tab=pods"},
				{Label: "查看 Pod 日志", Path: workloadPath("job", workload.Namespace, workload.Name) + "?tab=logs"},
			},
		})
		return
	}
	if workload.Status == "Complete" {
		report.check("completion", "Job 完成状态", domain.DiagnosisCheckPass, "Job 已达到成功完成条件")
		return
	}
	if hasReplicaCounts && desired > 0 && ready >= desired {
		report.check("completion", "Job 完成状态", domain.DiagnosisCheckPass, fmt.Sprintf("%d/%d 个任务已成功完成", ready, desired))
		return
	}
	report.check("completion", "Job 完成状态", domain.DiagnosisCheckInfo, "Job 仍在运行，尚未达到成功完成条件")
}

func (s *Service) diagnoseCronJobStatus(workload domain.WorkloadDetail, report *reportBuilder) {
	switch workload.Status {
	case "Suspended":
		report.check("schedule", "CronJob 调度", domain.DiagnosisCheckInfo, "CronJob 已暂停，不会创建新的 Job")
	case "Running":
		report.check("schedule", "CronJob 调度", domain.DiagnosisCheckPass, "当前存在活动的 Job")
	case "Idle":
		report.check("schedule", "CronJob 调度", domain.DiagnosisCheckPass, "当前没有活动 Job，等待下一次调度")
	default:
		report.check("schedule", "CronJob 调度", domain.DiagnosisCheckInfo, "当前状态为 "+fallback(workload.Status, "Unknown"))
	}
}

func (s *Service) collectWorkloadConditions(workload domain.WorkloadDetail, report *reportBuilder) {
	if len(workload.Conditions) == 0 {
		report.check("conditions", "控制器 Conditions", domain.DiagnosisCheckInfo, "控制器未报告 Condition")
		return
	}
	status := domain.DiagnosisCheckPass
	message := "控制器 Conditions 未报告异常"
	for _, condition := range workload.Conditions {
		severity := ""
		checkStatus := domain.DiagnosisCheckInfo
		switch {
		case strings.EqualFold(condition.Type, "Failed") && condition.Status == "True",
			strings.EqualFold(condition.Type, "ReplicaFailure") && condition.Status == "True":
			severity = domain.DiagnosisCritical
			checkStatus = domain.DiagnosisCheckCritical
		case condition.Status == "False" && (strings.EqualFold(condition.Type, "Available") || strings.EqualFold(condition.Type, "Ready") || strings.EqualFold(condition.Type, "Progressing")):
			severity = domain.DiagnosisWarning
			checkStatus = domain.DiagnosisCheckWarning
		case condition.Status == "True" && (strings.EqualFold(condition.Type, "Complete") || strings.EqualFold(condition.Type, "Available") || strings.EqualFold(condition.Type, "Progressing")):
			continue
		}
		if severity == "" {
			continue
		}
		if severityRank(severity) < severityRank(severityFromCheck(status)) {
			status = checkStatus
		}
		message = "至少一个控制器 Condition 报告异常"
		detail := fallback(condition.Message, fallback(condition.Reason, condition.Type+"="+condition.Status))
		report.finding(domain.DiagnosisFinding{
			ID: "workload.condition." + strings.ToLower(condition.Type), Severity: severity,
			Title: "控制器报告 " + condition.Type + " 异常", Cause: detail,
			Evidence: []domain.DiagnosisEvidence{
				{Label: "Condition", Value: condition.Type + "=" + condition.Status},
				{Label: "原因", Value: fallback(condition.Reason, "Unknown")},
				{Label: "消息", Value: fallback(condition.Message, "未提供")},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看事件", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=events"},
				{Label: "查看实例状态", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=pods"},
			},
		})
	}
	report.check("conditions", "控制器 Conditions", status, message)
}

func (s *Service) collectWorkloadPods(ctx context.Context, actor domain.Actor, workload domain.WorkloadDetail, report *reportBuilder) {
	if len(workload.Pods) == 0 {
		if strings.EqualFold(workload.Kind, "CronJob") {
			report.check("pods", "关联 Pod", domain.DiagnosisCheckInfo, "CronJob 当前没有可供诊断的 Pod")
		} else if workload.Desired > 0 {
			if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", Resource: "pods", Namespace: workload.Namespace}); err != nil {
				report.warn("无法确认关联 Pod：当前身份缺少 list pods 权限")
				report.check("pods", "关联 Pod", domain.DiagnosisCheckSkipped, "未验证工作负载是否有关联 Pod")
				return
			}
			report.check("pods", "关联 Pod", domain.DiagnosisCheckCritical, "工作负载期望运行副本，但选择器没有匹配到 Pod")
			report.finding(domain.DiagnosisFinding{
				ID: "workload.no-pods", Severity: domain.DiagnosisCritical, Title: "选择器没有匹配到 Pod",
				Cause: "工作负载期望实例，但当前没有关联 Pod。请检查 Pod 模板、控制器事件、命名空间配额和调度约束。",
				Evidence: []domain.DiagnosisEvidence{
					{Label: "期望副本", Value: strconv.Itoa(int(workload.Desired))},
					{Label: "选择器", Value: selectorLabel(workload.Selector)},
				},
				Recommendations: []domain.DiagnosisRecommendation{
					{Label: "查看工作负载事件", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=events"},
					{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe %s %s -n %s", strings.ToLower(workload.Kind), workload.Name, workload.Namespace)},
				},
			})
		} else {
			report.check("pods", "关联 Pod", domain.DiagnosisCheckPass, "当前没有需要运行的 Pod")
		}
		return
	}
	for index, pod := range workload.Pods {
		if index >= maxRelatedPods {
			break
		}
		report.relatedResource("Pod", pod.Namespace, pod.Name, pod.Status, "由工作负载管理", podPath(pod.Namespace, pod.Name))
	}
	unhealthy := make([]domain.PodSummary, 0)
	for _, pod := range workload.Pods {
		if podNeedsAttention(pod) {
			unhealthy = append(unhealthy, pod)
		}
	}
	if len(unhealthy) == 0 {
		report.check("pods", "关联 Pod", domain.DiagnosisCheckPass, fmt.Sprintf("%d 个关联 Pod 未发现失败、未就绪或重启异常", len(workload.Pods)))
	} else {
		status := domain.DiagnosisCheckWarning
		for _, pod := range unhealthy {
			if criticalPodStatus(pod.Status) {
				status = domain.DiagnosisCheckCritical
				break
			}
		}
		report.check("pods", "关联 Pod", status, fmt.Sprintf("%d/%d 个关联 Pod 需要关注", len(unhealthy), len(workload.Pods)))
	}
	if workload.Restarts > 0 {
		evidence := make([]domain.DiagnosisEvidence, 0, 3)
		for _, pod := range workload.Pods {
			if pod.Restarts == 0 || len(evidence) >= 3 {
				continue
			}
			evidence = append(evidence, domain.DiagnosisEvidence{Label: pod.Name, Value: fmt.Sprintf("重启 %d 次 · %s", pod.Restarts, pod.Status)})
		}
		report.finding(domain.DiagnosisFinding{
			ID: "workload.restarts", Severity: domain.DiagnosisWarning, Title: "关联 Pod 发生过重启",
			Cause:    "容器重启可能来自应用崩溃、OOMKilled、启动探针失败或被节点驱逐。请优先检查重启次数最高的 Pod。",
			Evidence: evidence,
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看实例列表", Path: workloadPath(strings.ToLower(workload.Kind), workload.Namespace, workload.Name) + "?tab=pods"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl get pods -n %s -l %s --sort-by=.status.containerStatuses[0].restartCount", workload.Namespace, selectorLabel(workload.Selector))},
			},
		})
	}
	if len(unhealthy) == 0 {
		return
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", Resource: "pods", Namespace: workload.Namespace}); err != nil {
		report.warn("无法读取关联 Pod 详情：当前身份缺少 list pods 权限")
		report.check("pod-details", "异常 Pod 详情", domain.DiagnosisCheckSkipped, "未执行深入容器检查")
		return
	}
	sort.SliceStable(unhealthy, func(i, j int) bool {
		return podAttentionScore(unhealthy[i]) > podAttentionScore(unhealthy[j])
	})
	if len(unhealthy) > maxWorkloadPodDiagnoses {
		unhealthy = unhealthy[:maxWorkloadPodDiagnoses]
		report.warn(fmt.Sprintf("关联 Pod 异常较多，诊断仅深入检查前 %d 个 Pod", maxWorkloadPodDiagnoses))
	}
	report.check("pod-details", "异常 Pod 详情", domain.DiagnosisCheckInfo, fmt.Sprintf("深入检查 %d 个异常 Pod", len(unhealthy)))
	for _, summary := range unhealthy {
		detail, err := s.repository.GetPod(ctx, summary.Namespace, summary.Name)
		if err != nil {
			report.warn(fmt.Sprintf("读取 Pod %s 详情失败：%s", summary.Name, err.Error()))
			continue
		}
		for _, finding := range podContainerFindings(detail, "workload."+detail.Name) {
			report.finding(finding)
		}
		for _, finding := range podMetricFindings(detail) {
			finding.ID = "workload." + detail.Name + "." + finding.ID
			report.finding(finding)
		}
		s.collectPodLogsFor(ctx, actor, detail, report, detail.Name+".", "logs-"+detail.Name, "日志 · "+detail.Name)
	}
}

func podNeedsAttention(pod domain.PodSummary) bool {
	if pod.Status == "Succeeded" || pod.Status == "Complete" {
		return false
	}
	if pod.Restarts > 0 {
		return true
	}
	switch pod.Status {
	case "Running":
	default:
		return true
	}
	ready, desired, ok := parseReadyCounts(pod.Ready)
	return ok && desired > 0 && ready < desired
}

func criticalPodStatus(status string) bool {
	switch status {
	case "Failed", "Error", "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "ContainerCannotRun", "Evicted", "Unknown":
		return true
	default:
		return false
	}
}

func podAttentionScore(pod domain.PodSummary) int {
	score := int(pod.Restarts)
	if criticalPodStatus(pod.Status) {
		score += 1000
	}
	if ready, desired, ok := parseReadyCounts(pod.Ready); ok && desired > 0 && ready == 0 {
		score += 500
	}
	return score
}

func parseReadyCounts(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	ready, readyErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	desired, desiredErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if readyErr != nil || desiredErr != nil || ready < 0 || desired < 0 {
		return 0, 0, false
	}
	return ready, desired, true
}

func selectorLabel(selector map[string]string) string {
	if len(selector) == 0 {
		return "<none>"
	}
	keys := make([]string, 0, len(selector))
	for key := range selector {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+selector[key])
	}
	return strings.Join(parts, ",")
}
