package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"dosh/internal/domain"
)

const podLogReadTimeout = 8 * time.Second

var podLogPatterns = []struct {
	ID             string
	Needle         string
	Severity       string
	Title          string
	Cause          string
	Recommendation string
}{
	{ID: "out-of-memory", Needle: "out of memory", Severity: domain.DiagnosisCritical, Title: "日志显示内存耗尽", Cause: "进程可能超过了容器内存限制，或被运行时终止。", Recommendation: "结合 OOMKilled 状态提高内存 Limit，或排查内存泄漏。"},
	{ID: "no-space", Needle: "no space left on device", Severity: domain.DiagnosisCritical, Title: "日志显示磁盘空间不足", Cause: "容器可写层、挂载卷或节点磁盘可能已耗尽。", Recommendation: "检查节点磁盘、emptyDir 用量和持久卷容量。"},
	{ID: "panic", Needle: "panic:", Severity: domain.DiagnosisCritical, Title: "应用发生 panic", Cause: "进程在日志中记录了不可恢复的运行时错误。", Recommendation: "查看异常堆栈和最近一次配置或镜像变更。"},
	{ID: "fatal", Needle: "fatal error:", Severity: domain.DiagnosisCritical, Title: "应用记录 fatal error", Cause: "应用主动终止并记录了致命错误。", Recommendation: "检查启动配置、依赖连接和最近变更。"},
	{ID: "exec-format", Needle: "exec format error", Severity: domain.DiagnosisCritical, Title: "启动命令或镜像架构不兼容", Cause: "可执行文件格式与节点架构不匹配，或入口命令不是有效可执行文件。", Recommendation: "确认镜像多架构与目标节点架构一致，并检查 command/entrypoint。"},
	{ID: "connection-refused", Needle: "connection refused", Severity: domain.DiagnosisWarning, Title: "日志显示连接被拒绝", Cause: "应用尝试连接的依赖服务或端口未监听。", Recommendation: "检查依赖 Service、Endpoint、端口和 NetworkPolicy。"},
	{ID: "dns-failure", Needle: "no such host", Severity: domain.DiagnosisWarning, Title: "日志显示 DNS 解析失败", Cause: "服务名无法解析，可能是名称错误、DNS 配置或网络策略问题。", Recommendation: "检查 Service 名称、CoreDNS 状态和 DNS 出口策略。"},
	{ID: "deadline", Needle: "deadline exceeded", Severity: domain.DiagnosisWarning, Title: "依赖调用发生超时", Cause: "上游依赖响应过慢或网络链路异常。", Recommendation: "检查依赖服务负载、超时配置和网络延迟。"},
	{ID: "permission", Needle: "permission denied", Severity: domain.DiagnosisWarning, Title: "日志显示权限不足", Cause: "进程访问文件、挂载卷或系统资源时被拒绝。", Recommendation: "检查 securityContext、文件权限和挂载的只读设置。"},
}

func (s *Service) diagnosePod(ctx context.Context, actor domain.Actor, namespace, name string) (*reportBuilder, error) {
	pod, err := s.repository.GetPod(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	report := newReport("Pod", namespace, name, pod.Status)
	if pod.Node != "" {
		report.relatedResource("Node", "", pod.Node, "", "运行于节点", nodePath(pod.Node))
	}
	if pod.OwnerKind != "" && pod.OwnerName != "" {
		ownerKind := strings.ToLower(pod.OwnerKind)
		path := ""
		if ownerKind == "deployment" || ownerKind == "statefulset" || ownerKind == "daemonset" || ownerKind == "job" || ownerKind == "cronjob" {
			path = workloadPath(ownerKind, namespace, pod.OwnerName)
		}
		report.relatedResource(pod.OwnerKind, namespace, pod.OwnerName, "", "归属控制器", path)
	}
	report.check("phase", "Pod 阶段", phaseCheck(pod.Status), "当前状态为 "+pod.Status)
	if pod.Status == "Failed" {
		report.finding(domain.DiagnosisFinding{
			ID: "pod.failed", Severity: domain.DiagnosisCritical, Title: "Pod 已进入 Failed 状态",
			Cause: "Pod 中的至少一个容器以失败状态终止，Kubernetes 不会自动重启该 Pod。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "阶段", Value: pod.Status},
				{Label: "容器状态", Value: containerCheckMessage(pod)},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看容器日志", Path: podTabPath(namespace, name, "logs")},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe pod %s -n %s", name, namespace)},
			},
		})
	} else if pod.Status == "Pending" || pod.Status == "Unknown" {
		severity := domain.DiagnosisWarning
		if pod.Status == "Unknown" {
			severity = domain.DiagnosisCritical
		}
		report.finding(domain.DiagnosisFinding{
			ID: "pod.phase", Severity: severity, Title: "Pod 处于 " + pod.Status + " 状态",
			Cause:    "Pod 还没有进入可服务状态。请结合调度 Condition、容器状态和事件确认阻塞原因。",
			Evidence: []domain.DiagnosisEvidence{{Label: "阶段", Value: pod.Status}, {Label: "就绪", Value: fallback(pod.Ready, "0/0")}},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看 Pod 事件", Path: podTabPath(namespace, name, "events")},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe pod %s -n %s", name, namespace)},
			},
		})
	}
	readyCondition, hasReady := podCondition(pod.Conditions, "Ready")
	containersReady, hasContainersReady := podCondition(pod.Conditions, "ContainersReady")
	if hasReady {
		if readyCondition.Status == "True" {
			report.check("ready", "Pod 就绪", domain.DiagnosisCheckPass, "Pod 已通过 Ready Condition")
		} else {
			message := fallback(readyCondition.Message, fallback(readyCondition.Reason, "Pod 尚未就绪"))
			report.check("ready", "Pod 就绪", domain.DiagnosisCheckWarning, message)
		}
	} else if hasContainersReady {
		status := domain.DiagnosisCheckWarning
		if containersReady.Status == "True" {
			status = domain.DiagnosisCheckPass
		}
		report.check("ready", "Pod 就绪", status, fallback(containersReady.Message, fallback(containersReady.Reason, "仅取得 ContainersReady Condition")))
	}
	if scheduled, ok := podCondition(pod.Conditions, "PodScheduled"); ok && scheduled.Status == "False" {
		message := fallback(scheduled.Message, fallback(scheduled.Reason, "Pod 尚未完成调度"))
		report.finding(domain.DiagnosisFinding{
			ID: "pod-unscheduled", Severity: domain.DiagnosisCritical, Title: "Pod 尚未完成调度", Cause: message,
			Evidence: []domain.DiagnosisEvidence{{Label: "Condition", Value: "PodScheduled=False"}, {Label: "原因", Value: fallback(scheduled.Reason, "Unknown")}},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看节点与资源状态", Path: "/nodes"},
				{Label: "查看调度事件", Path: podTabPath(namespace, name, "events")},
			},
		})
	}
	for _, finding := range podContainerFindings(pod, "pod") {
		report.finding(finding)
	}
	report.check("containers", "容器状态", containerCheckStatus(pod), containerCheckMessage(pod))
	for _, finding := range podMetricFindings(pod) {
		report.finding(finding)
	}
	report.relatedResource("Pod", namespace, name, pod.Status, "当前对象", podPath(namespace, name))
	s.collectPodLogs(ctx, actor, pod, report)
	s.collectEvents(ctx, actor, namespace, "Pod", name, podTabPath(namespace, name, "events"), report)
	return report, nil
}

func podCondition(conditions []domain.Condition, conditionType string) (domain.Condition, bool) {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition, true
		}
	}
	return domain.Condition{}, false
}

func phaseCheck(status string) string {
	switch status {
	case "Running", "Succeeded":
		return domain.DiagnosisCheckPass
	case "Pending":
		return domain.DiagnosisCheckWarning
	case "Failed", "Unknown":
		return domain.DiagnosisCheckCritical
	default:
		return domain.DiagnosisCheckInfo
	}
}

func containerCheckStatus(pod domain.PodDetail) string {
	for _, container := range pod.Containers {
		switch container.Reason {
		case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "RunContainerError", "ContainerCannotRun", "InvalidImageName":
			return domain.DiagnosisCheckCritical
		}
		if !container.Ready && !container.Init && container.State == "Running" {
			return domain.DiagnosisCheckWarning
		}
	}
	return domain.DiagnosisCheckPass
}

func containerCheckMessage(pod domain.PodDetail) string {
	notReady := 0
	for _, container := range pod.Containers {
		if !container.Ready && !container.Init && !container.Ephemeral {
			notReady++
		}
	}
	if notReady == 0 {
		return "所有常规容器均已就绪"
	}
	return fmt.Sprintf("%d 个容器尚未就绪", notReady)
}

func podContainerFindings(pod domain.PodDetail, scope string) []domain.DiagnosisFinding {
	findings := make([]domain.DiagnosisFinding, 0)
	add := func(id, severity, title, cause string, evidence []domain.DiagnosisEvidence, recommendations []domain.DiagnosisRecommendation) {
		findings = append(findings, domain.DiagnosisFinding{
			ID: scope + "." + id, Severity: severity, Title: title, Cause: cause,
			Evidence: evidence, Recommendations: recommendations,
		})
	}
	for _, container := range pod.Containers {
		if container.Init || container.Ephemeral {
			continue
		}
		prefix := container.Name
		evidence := []domain.DiagnosisEvidence{
			{Label: "容器", Value: container.Name},
			{Label: "镜像", Value: container.Image},
			{Label: "重启次数", Value: fmt.Sprintf("%d", container.RestartCount)},
		}
		if scope != "pod" {
			evidence = append([]domain.DiagnosisEvidence{{Label: "Pod", Value: pod.Name}}, evidence...)
		}
		recommendations := []domain.DiagnosisRecommendation{
			{Label: "查看容器日志", Path: podTabPath(pod.Namespace, pod.Name, "logs")},
			{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace)},
		}
		switch container.Reason {
		case "CrashLoopBackOff":
			if container.LastReason != "" || container.LastExitCode != 0 {
				evidence = append(evidence, domain.DiagnosisEvidence{Label: "上次终止", Value: fmt.Sprintf("%s（退出码 %d）", fallback(container.LastReason, "Unknown"), container.LastExitCode)})
			}
			add(prefix+".crashloop", domain.DiagnosisCritical, "容器处于 CrashLoopBackOff", "容器启动后反复退出，Kubernetes 正在指数退避重启。应结合上次退出码和容器日志定位启动失败原因。", evidence, recommendations)
		case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
			add(prefix+".imagepull", domain.DiagnosisCritical, "容器镜像拉取失败", "镜像地址无效、镜像不存在、认证失败、Registry 证书不受信任或网络出口被阻断。", evidence, []domain.DiagnosisRecommendation{
				{Label: "查看 Pod 事件", Path: podTabPath(pod.Namespace, pod.Name, "events")},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace)},
			})
		case "CreateContainerConfigError", "CreateContainerError":
			add(prefix+".config", domain.DiagnosisCritical, "容器配置无法创建", "引用的 ConfigMap、Secret 或 Key 可能缺失，或容器配置被 API Server 拒绝。", evidence, []domain.DiagnosisRecommendation{
				{Label: "查看 Pod YAML", Path: podTabPath(pod.Namespace, pod.Name, "yaml")},
				{Label: "查看 Pod 事件", Path: podTabPath(pod.Namespace, pod.Name, "events")},
			})
		case "RunContainerError", "ContainerCannotRun":
			add(prefix+".run", domain.DiagnosisCritical, "容器运行命令启动失败", "入口命令、可执行权限、动态链接库或容器运行时可能存在问题。", evidence, recommendations)
		}
		if container.Reason == "OOMKilled" || container.LastReason == "OOMKilled" {
			if container.MemoryLimitBytes > 0 {
				evidence = append(evidence, domain.DiagnosisEvidence{Label: "内存 Limit", Value: formatBytes(container.MemoryLimitBytes)})
			}
			add(prefix+".oom", domain.DiagnosisCritical, "容器曾因内存不足被终止", "容器被运行时以 OOMKilled 终止，通常由内存 Limit 过低或进程内存增长导致。", evidence, []domain.DiagnosisRecommendation{
				{Label: "查看容器配置", Path: podTabPath(pod.Namespace, pod.Name, "containers")},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl top pod %s -n %s --containers", pod.Name, pod.Namespace)},
			})
		}
		if !container.Ready && container.State == "Running" {
			add(prefix+".notready", domain.DiagnosisWarning, "容器正在运行但尚未就绪", "容器可能仍在启动，或 Readiness Probe 持续失败。", evidence, []domain.DiagnosisRecommendation{
				{Label: "查看容器配置与探针", Path: podTabPath(pod.Namespace, pod.Name, "containers")},
				{Label: "查看容器日志", Path: podTabPath(pod.Namespace, pod.Name, "logs")},
			})
		}
	}
	return findings
}

func podMetricFindings(pod domain.PodDetail) []domain.DiagnosisFinding {
	if !pod.HasMetrics {
		return nil
	}
	var memoryLimit int64
	var cpuLimit int64
	for _, container := range pod.Containers {
		if container.Ephemeral {
			continue
		}
		memoryLimit += container.MemoryLimitBytes
		cpuLimit += container.CPULimitMilli
	}
	findings := make([]domain.DiagnosisFinding, 0, 2)
	if memoryLimit > 0 && pod.MemoryUsageBytes*100 >= memoryLimit*90 {
		severity := domain.DiagnosisWarning
		if pod.MemoryUsageBytes >= memoryLimit {
			severity = domain.DiagnosisCritical
		}
		findings = append(findings, domain.DiagnosisFinding{
			ID: "pod.memory-pressure", Severity: severity, Title: "Pod 内存使用接近或超过 Limit",
			Cause: "当前内存使用已接近容器内存 Limit，存在 OOMKilled 和重启风险。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "当前使用", Value: formatBytes(pod.MemoryUsageBytes)},
				{Label: "Limit 合计", Value: formatBytes(memoryLimit)},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "调整容器资源", Path: podTabPath(pod.Namespace, pod.Name, "containers")},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl top pod %s -n %s --containers", pod.Name, pod.Namespace)},
			},
		})
	}
	if cpuLimit > 0 && pod.CPUUsageMilli*100 >= cpuLimit*90 {
		findings = append(findings, domain.DiagnosisFinding{
			ID: "pod.cpu-pressure", Severity: domain.DiagnosisWarning, Title: "Pod CPU 使用接近 Limit",
			Cause: "CPU 使用率已接近 Limit，可能发生限流并导致请求延迟。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "当前使用", Value: formatMilliCPU(pod.CPUUsageMilli)},
				{Label: "Limit 合计", Value: formatMilliCPU(cpuLimit)},
			},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "调整容器资源", Path: podTabPath(pod.Namespace, pod.Name, "containers")}},
		})
	}
	return findings
}

func (s *Service) collectPodLogs(ctx context.Context, actor domain.Actor, pod domain.PodDetail, report *reportBuilder) {
	s.collectPodLogsFor(ctx, actor, pod, report, "", "logs", "容器日志")
}

func (s *Service) collectPodLogsFor(ctx context.Context, actor domain.Actor, pod domain.PodDetail, report *reportBuilder, findingPrefix, checkID, label string) {
	candidates := make([]domain.Container, 0, len(pod.Containers))
	for _, container := range pod.Containers {
		if container.Ephemeral || container.Reason == "ImagePullBackOff" || container.Reason == "ErrImagePull" || container.Reason == "InvalidImageName" {
			continue
		}
		if container.RestartCount > 0 || !container.Ready || container.State == "Terminated" || container.State == "Waiting" {
			candidates = append(candidates, container)
		}
	}
	if len(candidates) == 0 {
		report.check(checkID, label, domain.DiagnosisCheckPass, pod.Name+" 的容器状态正常，无需读取异常日志")
		return
	}
	if len(candidates) > maxDiagnosisLogContainers {
		candidates = candidates[:maxDiagnosisLogContainers]
		report.warn(fmt.Sprintf("%s 的异常容器较多，诊断仅读取前 %d 个容器的有界日志", pod.Name, maxDiagnosisLogContainers))
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "get", Resource: "pods", Subresource: "log", Namespace: pod.Namespace, Name: pod.Name}); err != nil {
		report.warn("无法读取 " + pod.Name + " 的容器日志：当前身份缺少 get pods/log 权限")
		report.check(checkID, label, domain.DiagnosisCheckSkipped, "未执行日志检查")
		return
	}
	type match struct {
		pattern  int
		evidence []domain.DiagnosisEvidence
		seen     map[string]struct{}
	}
	matches := make(map[int]*match)
	healthyStreams := 0
	for _, container := range candidates {
		previous := container.RestartCount > 0 && container.LastState == "Terminated"
		streamCtx, cancel := context.WithTimeout(ctx, podLogReadTimeout)
		stream, err := s.repository.StreamPodLogs(streamCtx, pod.Namespace, pod.Name, container.Name, false, previous, maxDiagnosisLogLines, true, 1800, maxDiagnosisLogBytes)
		if err != nil {
			cancel()
			if errors.Is(err, domain.ErrForbidden) {
				report.warn("读取容器日志时被拒绝：" + container.Name)
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, maxDiagnosisLogBytes))
		_ = stream.Close()
		cancel()
		if len(data) == 0 {
			continue
		}
		healthyStreams++
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			lower := strings.ToLower(line)
			for index, pattern := range podLogPatterns {
				if !strings.Contains(lower, pattern.Needle) {
					continue
				}
				item, ok := matches[index]
				if !ok {
					item = &match{pattern: index, seen: make(map[string]struct{})}
					matches[index] = item
				}
				value := truncateText(line, 320)
				if _, exists := item.seen[value]; exists {
					continue
				}
				item.seen[value] = struct{}{}
				if len(item.evidence) < 3 {
					item.evidence = append(item.evidence, domain.DiagnosisEvidence{Label: container.Name, Value: value})
				}
				break
			}
		}
		_ = readErr
	}
	if len(matches) == 0 {
		if healthyStreams > 0 {
			report.check(checkID, label, domain.DiagnosisCheckPass, pod.Name+" 的有界日志中未发现已知异常模式")
		} else {
			report.check(checkID, label, domain.DiagnosisCheckSkipped, "当前容器没有可读取的历史日志")
		}
		return
	}
	report.check(checkID, label, domain.DiagnosisCheckWarning, pod.Name+" 的日志中发现值得关注的错误模式")
	indexes := make([]int, 0, len(matches))
	for index := range matches {
		indexes = append(indexes, index)
	}
	sortInts(indexes)
	for _, index := range indexes {
		pattern := podLogPatterns[index]
		report.finding(domain.DiagnosisFinding{
			ID: findingPrefix + "logs." + pattern.ID, Severity: pattern.Severity, Title: pattern.Title, Cause: pattern.Cause,
			Evidence: matches[index].evidence,
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看完整日志", Path: podTabPath(pod.Namespace, pod.Name, "logs")},
				{Label: "建议", Description: pattern.Recommendation},
			},
		})
	}
}

func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func formatBytes(value int64) string {
	const mib = 1024 * 1024
	const gib = 1024 * mib
	if value >= gib {
		return fmt.Sprintf("%.1f GiB", float64(value)/float64(gib))
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/float64(mib))
}

func formatMilliCPU(value int64) string {
	return fmt.Sprintf("%dm", value)
}
