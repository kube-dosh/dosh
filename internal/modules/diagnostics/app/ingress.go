package app

import (
	"context"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

func (s *Service) diagnoseIngress(ctx context.Context, actor domain.Actor, namespace, name string) (*reportBuilder, error) {
	ingress, err := s.repository.GetIngress(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	report := newReport("Ingress", namespace, name, "Configured")
	report.relatedResource("Ingress", namespace, name, "Configured", "当前对象", ingressPath(namespace, name))

	if ingress.Class == "" {
		report.check("class", "IngressClass", domain.DiagnosisCheckWarning, "未显式指定 IngressClass，将依赖集群默认 Controller")
		report.finding(domain.DiagnosisFinding{
			ID: "ingress.default-class", Severity: domain.DiagnosisWarning, Title: "未显式设置 IngressClass",
			Cause:    "如果集群没有默认 IngressClass，或默认 Controller 未运行，该 Ingress 可能不会被任何控制器处理。",
			Evidence: []domain.DiagnosisEvidence{{Label: "IngressClass", Value: "未设置"}},
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "查看 IngressClass", Path: "/network/ingresses"},
				{Label: "复制排查命令", Command: "kubectl get ingressclass"},
			},
		})
	} else {
		report.check("class", "IngressClass", domain.DiagnosisCheckPass, "IngressClass 为 "+ingress.Class)
	}

	routes := append([]domain.IngressPath{}, ingress.Paths...)
	if ingress.DefaultBackend != nil {
		routes = append(routes, *ingress.DefaultBackend)
	}
	if len(routes) == 0 {
		report.check("routes", "路由后端", domain.DiagnosisCheckWarning, "Ingress 没有配置路由或默认后端")
		report.finding(domain.DiagnosisFinding{
			ID: "ingress.no-routes", Severity: domain.DiagnosisWarning, Title: "Ingress 没有配置转发规则",
			Cause:           "没有规则或默认后端的 Ingress 不会转发任何请求。",
			Evidence:        []domain.DiagnosisEvidence{{Label: "路由数量", Value: "0"}},
			Recommendations: []domain.DiagnosisRecommendation{{Label: "编辑 Ingress", Path: ingressPath(namespace, name)}},
		})
	} else {
		s.collectIngressRoutes(ingress, routes, report)
	}

	if len(ingress.TLS) == 0 {
		report.check("tls", "TLS 配置", domain.DiagnosisCheckInfo, "Ingress 未声明 TLS")
	} else {
		issues := 0
		evidence := make([]domain.DiagnosisEvidence, 0, len(ingress.TLS))
		for index, tls := range ingress.TLS {
			hosts := strings.Join(tls.Hosts, ", ")
			if hosts == "" {
				hosts = "未声明 hosts"
			}
			if strings.TrimSpace(tls.SecretName) == "" {
				issues++
				evidence = append(evidence, domain.DiagnosisEvidence{Label: fmt.Sprintf("TLS %d", index+1), Value: hosts + " · Secret 未设置"})
			} else {
				evidence = append(evidence, domain.DiagnosisEvidence{Label: fmt.Sprintf("TLS %d", index+1), Value: hosts + " · Secret " + tls.SecretName})
			}
		}
		if issues > 0 {
			report.check("tls", "TLS 配置", domain.DiagnosisCheckWarning, fmt.Sprintf("%d 个 TLS 条目未指定 Secret", issues))
			report.finding(domain.DiagnosisFinding{
				ID: "ingress.tls-secret", Severity: domain.DiagnosisWarning, Title: "TLS 条目缺少 Secret 名称",
				Cause:           "没有 Secret 的 TLS 条目无法为对应 Host 提供证书，Controller 通常会拒绝或忽略该配置。",
				Evidence:        evidence,
				Recommendations: []domain.DiagnosisRecommendation{{Label: "编辑 Ingress TLS", Path: ingressPath(namespace, name)}},
			})
		} else {
			report.check("tls", "TLS 配置", domain.DiagnosisCheckInfo, "TLS 已引用 Secret；证书有效期与内容仍需在配置与密钥页确认")
		}
	}

	s.collectEvents(ctx, actor, namespace, "Ingress", name, ingressPath(namespace, name)+"?tab=events", report)
	return report, nil
}

func (s *Service) collectIngressRoutes(ingress domain.IngressDetail, routes []domain.IngressPath, report *reportBuilder) {
	impacted := make([]domain.DiagnosisEvidence, 0)
	relatedServices := make(map[string]struct{})
	critical := false
	for _, route := range routes {
		routeLabel := ingressRouteLabel(route)
		switch route.TargetStatus {
		case domain.IngressTargetReady:
			relatedServices[route.Service] = struct{}{}
		case domain.IngressTargetServiceNotFound:
			critical = true
			impacted = append(impacted, domain.DiagnosisEvidence{Label: routeLabel, Value: "后端 Service 不存在"})
		case domain.IngressTargetPortNotFound:
			critical = true
			impacted = append(impacted, domain.DiagnosisEvidence{Label: routeLabel, Value: "后端 Service 缺少端口 " + fallback(route.Port, "未指定")})
			relatedServices[route.Service] = struct{}{}
		case domain.IngressTargetNoReadyEndpoints:
			critical = true
			impacted = append(impacted, domain.DiagnosisEvidence{Label: routeLabel, Value: "后端 Service 没有就绪 Endpoint"})
			relatedServices[route.Service] = struct{}{}
		default:
			impacted = append(impacted, domain.DiagnosisEvidence{Label: routeLabel, Value: "后端状态未知"})
			relatedServices[route.Service] = struct{}{}
		}
	}
	if critical {
		status := domain.DiagnosisCheckCritical
		if len(impacted) == 0 {
			status = domain.DiagnosisCheckWarning
		}
		report.check("routes", "路由后端", status, fmt.Sprintf("%d/%d 条路由后端不可用", len(impacted), len(routes)))
		report.finding(domain.DiagnosisFinding{
			ID: "ingress.backend-unavailable", Severity: domain.DiagnosisCritical, Title: "Ingress 后端不可用",
			Cause:    "至少一条路由引用的 Service、端口或就绪 Endpoint 不存在，匹配该 Host/Path 的请求会返回错误。",
			Evidence: impacted,
			Recommendations: []domain.DiagnosisRecommendation{
				{Label: "检查路由", Path: ingressPath(ingress.Namespace, ingress.Name) + "?tab=routes"},
				{Label: "复制排查命令", Command: fmt.Sprintf("kubectl describe ingress %s -n %s", ingress.Name, ingress.Namespace)},
			},
		})
	} else if len(impacted) > 0 {
		report.check("routes", "路由后端", domain.DiagnosisCheckWarning, fmt.Sprintf("%d/%d 条路由后端状态未知", len(impacted), len(routes)))
		report.finding(domain.DiagnosisFinding{
			ID: "ingress.backend-unknown", Severity: domain.DiagnosisWarning, Title: "部分 Ingress 后端状态未知",
			Cause:           "Dosh 无法从当前对象状态确认这些后端是否可用。",
			Evidence:        impacted,
			Recommendations: []domain.DiagnosisRecommendation{{Label: "检查路由", Path: ingressPath(ingress.Namespace, ingress.Name) + "?tab=routes"}},
		})
	} else {
		report.check("routes", "路由后端", domain.DiagnosisCheckPass, fmt.Sprintf("%d 条路由后端均已就绪", len(routes)))
	}
	for _, serviceName := range sortedKeys(relatedServices) {
		report.relatedResource("Service", ingress.Namespace, serviceName, "", "Ingress 后端", servicePath(ingress.Namespace, serviceName))
	}
}

func ingressRouteLabel(route domain.IngressPath) string {
	host := route.Host
	if host == "" {
		host = "default"
	}
	path := route.Path
	if path == "" {
		path = "/"
	}
	return host + path
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
