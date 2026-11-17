package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dosh/internal/domain"
)

func (s *Service) diagnoseService(ctx context.Context, actor domain.Actor, namespace, name string) (*reportBuilder, error) {
	service, err := s.repository.GetService(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	report := newReport("Service", namespace, name, service.Type)
	report.relatedResource("Service", namespace, name, service.Type, "当前对象", servicePath(namespace, name))

	if service.ExternalName != "" {
		report.check("type", "Service 类型", domain.DiagnosisCheckInfo, "ExternalName 指向集群外部 DNS 名称 "+service.ExternalName)
		report.check("endpoints", "就绪 Endpoint", domain.DiagnosisCheckInfo, "ExternalName Service 不使用 EndpointSlice")
		s.collectEvents(ctx, actor, namespace, "Service", name, servicePath(namespace, name)+"?tab=events", report)
		return report, nil
	}

	if len(service.Ports) == 0 {
		report.check("ports", "端口配置", domain.DiagnosisCheckCritical, "Service 没有配置端口")
		report.finding(domain.DiagnosisFinding{
			ID: "service.no-ports", Severity: domain.DiagnosisCritical, Title: "Service 没有可用端口",
			Cause:    "没有端口的 Service 无法把流量转发到后端 Pod。",
			Evidence: []domain.DiagnosisEvidence{{Label: "类型", Value: service.Type}},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "编辑 Service", Path: servicePath(namespace, name)},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe service %s -n %s", name, namespace)},
			},
		})
	} else {
		report.check("ports", "端口配置", domain.DiagnosisCheckPass, fmt.Sprintf("已配置 %d 个服务端口", len(service.Ports)))
		s.collectServicePortWarnings(service, report)
	}

	if len(service.Selector) == 0 {
		report.check("selector", "Pod 选择器", domain.DiagnosisCheckInfo, "Service 未配置 selector，Endpoint 需要由外部控制器或手工管理")
	} else {
		report.check("selector", "Pod 选择器", domain.DiagnosisCheckPass, "选择器为 "+selectorLabel(service.Selector))
	}

	readyAddresses := serviceReadyAddressCount(service.Endpoints)
	hasEndpointRecords := len(service.Endpoints) > 0 || service.EndpointCount > 0
	switch {
	case readyAddresses > 0:
		report.check("endpoints", "就绪 Endpoint", domain.DiagnosisCheckPass, fmt.Sprintf("%d 个后端地址就绪", maxInt(readyAddresses, service.EndpointCount)))
	case hasEndpointRecords:
		report.check("endpoints", "就绪 Endpoint", domain.DiagnosisCheckCritical, "Endpoint 已创建但没有就绪地址")
		report.finding(domain.DiagnosisFinding{
			ID: "service.no-ready-endpoints", Severity: domain.DiagnosisCritical, Title: "Service 没有就绪的后端 Endpoint",
			Cause:    "Endpoint 可能已创建，但目标 Pod 尚未通过 Readiness Probe，导致 Service 无法转发流量。",
			Evidence: serviceEndpointEvidence(service),
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看 Endpoint", Path: servicePath(namespace, name) + "?tab=endpoints"},
				{Label: "查看目标 Pod", Description: "从 Endpoint 的目标 Pod 开始检查 Readiness 与事件。"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl get endpointslices -n %s -l kubernetes.io/service-name=%s", namespace, name)},
			},
		})
	default:
		severity := domain.DiagnosisCritical
		status := domain.DiagnosisCheckCritical
		if len(service.Selector) == 0 {
			severity = domain.DiagnosisWarning
			status = domain.DiagnosisCheckWarning
		}
		report.check("endpoints", "就绪 Endpoint", status, "Service 当前没有就绪后端")
		report.finding(domain.DiagnosisFinding{
			ID: "service.no-endpoints", Severity: severity, Title: "Service 当前没有就绪后端",
			Cause:    "没有就绪 Endpoint 时，Service 请求会失败或超时。常见原因是 selector 不匹配、Pod 未就绪或 Endpoint 控制器异常。",
			Evidence: serviceEndpointEvidence(service),
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看 Endpoint", Path: servicePath(namespace, name) + "?tab=endpoints"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl get endpointslices -n %s -l kubernetes.io/service-name=%s", namespace, name)},
			},
		})
	}

	if len(service.Selector) > 0 {
		s.collectServiceSelectorPods(ctx, actor, service, report)
	}
	s.collectEvents(ctx, actor, namespace, "Service", name, servicePath(namespace, name)+"?tab=events", report)
	return report, nil
}

func (s *Service) collectServicePortWarnings(service domain.ServiceDetail, report *reportBuilder) {
	seen := make(map[string]struct{}, len(service.Ports))
	for _, port := range service.Ports {
		key := strings.ToLower(port.Protocol) + "/" + fmt.Sprintf("%d", port.Port)
		if _, ok := seen[key]; ok {
			report.finding(domain.DiagnosisFinding{
				ID: "service.duplicate-port", Severity: domain.DiagnosisWarning, Title: "Service 端口配置存在重复项",
				Cause:           "相同协议和端口被声明多次，可能导致配置难以维护或流量行为不明确。",
				Evidence:        []domain.DiagnosisEvidence{{Label: "端口", Value: port.Protocol + "/" + fmt.Sprintf("%d", port.Port)}},
				Recommendations: []domain.DiagnosisRecommendation{{Label: "编辑 Service", Path: servicePath(service.Namespace, service.Name)}},
			})
			break
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(port.TargetPort) == "" {
			report.finding(domain.DiagnosisFinding{
				ID: "service.missing-target-port", Severity: domain.DiagnosisWarning, Title: "Service 端口缺少 targetPort",
				Cause:           "Kubernetes 通常会把 targetPort 默认为 port，但显式声明可以避免 Service 与容器端口不一致。",
				Evidence:        []domain.DiagnosisEvidence{{Label: "服务端口", Value: fmt.Sprintf("%s/%d", port.Protocol, port.Port)}},
				Recommendations: []domain.DiagnosisRecommendation{{Label: "检查端口映射", Path: servicePath(service.Namespace, service.Name)}},
			})
			break
		}
	}
}

func (s *Service) collectServiceSelectorPods(ctx context.Context, actor domain.Actor, service domain.ServiceDetail, report *reportBuilder) {
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", Resource: "pods", Namespace: service.Namespace}); err != nil {
		report.warn("无法读取 Pod 选择结果：当前身份缺少 list pods 权限")
		report.check("selector-pods", "Pod 选择结果", domain.DiagnosisCheckSkipped, "未验证 selector 是否匹配 Pod")
		return
	}
	pods, err := s.repository.ListPodsBySelector(ctx, service.Namespace, service.Selector, "")
	if err != nil {
		report.warn("读取 selector 匹配 Pod 失败：" + err.Error())
		report.check("selector-pods", "Pod 选择结果", domain.DiagnosisCheckSkipped, "未能验证 selector")
		return
	}
	if len(pods) == 0 {
		report.check("selector-pods", "Pod 选择结果", domain.DiagnosisCheckCritical, "selector 没有匹配到 Pod")
		report.finding(domain.DiagnosisFinding{
			ID: "service.selector-no-pods", Severity: domain.DiagnosisCritical, Title: "Service selector 没有匹配到 Pod",
			Cause: "Service 的 selector 与命名空间内 Pod 标签不匹配，Endpoint 控制器无法创建后端地址。",
			Evidence: []domain.DiagnosisEvidence{
				{Label: "选择器", Value: selectorLabel(service.Selector)},
				{Label: "发现 Pod", Value: "0"},
			},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "检查 Pod 标签", Path: "/pods?namespace=" + service.Namespace},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl get pods -n %s -l %s --show-labels", service.Namespace, selectorLabel(service.Selector))},
			},
		})
		return
	}
	report.check("selector-pods", "Pod 选择结果", domain.DiagnosisCheckPass, fmt.Sprintf("selector 匹配到 %d 个 Pod", len(pods)))
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	for index, pod := range pods {
		if index >= maxRelatedPods {
			break
		}
		report.relatedResource("Pod", pod.Namespace, pod.Name, pod.Status, "由 Service selector 匹配", podPath(pod.Namespace, pod.Name))
	}
}

func serviceReadyAddressCount(endpoints []domain.ServiceEndpoint) int {
	count := 0
	for _, endpoint := range endpoints {
		if endpoint.Ready {
			count += len(endpoint.Addresses)
		}
	}
	return count
}

func serviceEndpointEvidence(service domain.ServiceDetail) []domain.DiagnosisEvidence {
	ready := 0
	notReady := 0
	for _, endpoint := range service.Endpoints {
		if endpoint.Ready {
			ready += len(endpoint.Addresses)
		} else {
			notReady += len(endpoint.Addresses)
		}
	}
	return []domain.DiagnosisEvidence{
		{Label: "就绪地址", Value: fmt.Sprintf("%d", ready)},
		{Label: "未就绪地址", Value: fmt.Sprintf("%d", notReady)},
		{Label: "选择器", Value: selectorLabel(service.Selector)},
	}
}
