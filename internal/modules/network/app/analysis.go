package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"
)

var networkDNSSubdomainPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// GatewayAPI returns the productized Gateway API view. The repository reports
// unavailable CRDs as an empty view rather than a transport error so callers can
// explain the installation prerequisite in place.
func (s *Service) GatewayAPI(ctx context.Context, namespace, search string) (domain.GatewayAPIOverview, error) {
	if namespace != "" {
		if err := validateNetworkNamespace(namespace); err != nil {
			return domain.GatewayAPIOverview{}, err
		}
	}
	overview, err := s.repository.GatewayAPIOverview(ctx, namespace)
	if err != nil {
		return domain.GatewayAPIOverview{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle != "" {
		overview.Classes = filterGatewayClasses(overview.Classes, needle)
		overview.Gateways = filterGateways(overview.Gateways, needle)
		overview.Routes = filterGatewayRoutes(overview.Routes, needle)
	}
	if overview.Classes == nil {
		overview.Classes = []domain.GatewayClassSummary{}
	}
	if overview.Gateways == nil {
		overview.Gateways = []domain.GatewaySummary{}
	}
	if overview.Routes == nil {
		overview.Routes = []domain.GatewayRouteSummary{}
	}
	return overview, nil
}

func (s *Service) NetworkPolicies(ctx context.Context, namespace, search string) (domain.NetworkPolicyOverview, error) {
	if namespace != "" {
		if err := validateNetworkNamespace(namespace); err != nil {
			return domain.NetworkPolicyOverview{}, err
		}
	}
	items, warnings, err := s.repository.ListNetworkPolicies(ctx, namespace)
	if err != nil {
		return domain.NetworkPolicyOverview{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	filtered := make([]domain.NetworkPolicySummary, 0, len(items))
	for _, item := range items {
		if needle == "" || strings.Contains(networkPolicySearchText(item), needle) {
			filtered = append(filtered, item)
		}
	}
	if filtered == nil {
		filtered = []domain.NetworkPolicySummary{}
	}
	return domain.NetworkPolicyOverview{Items: filtered, Warnings: warnings, GeneratedAt: time.Now().UTC()}, nil
}

func (s *Service) DiagnoseConnectivity(ctx context.Context, actor domain.Actor, request domain.ConnectivityRequest) (domain.ConnectivityDiagnosis, error) {
	if err := normalizeConnectivityRequest(&request); err != nil {
		s.auditConnectivity(ctx, actor, request, err)
		return domain.ConnectivityDiagnosis{}, err
	}
	source, err := s.repository.GetPod(ctx, request.SourceNamespace, request.SourcePod)
	if err != nil {
		s.auditConnectivity(ctx, actor, request, err)
		return domain.ConnectivityDiagnosis{}, err
	}
	service, err := s.repository.GetService(ctx, request.DestinationNamespace, request.DestinationService)
	if err != nil {
		s.auditConnectivity(ctx, actor, request, err)
		return domain.ConnectivityDiagnosis{}, err
	}
	report := buildConnectivityBase(source, service, request)

	sourceNamespaceLabels, sourceNamespaceKnown := s.namespaceLabels(ctx, request.SourceNamespace, &report)
	destNamespaceLabels, destNamespaceKnown := sourceNamespaceLabels, sourceNamespaceKnown
	if request.DestinationNamespace != request.SourceNamespace {
		destNamespaceLabels, destNamespaceKnown = s.namespaceLabels(ctx, request.DestinationNamespace, &report)
	}

	sourcePolicies, sourceWarnings, sourceErr := s.repository.ListNetworkPolicies(ctx, request.SourceNamespace)
	if sourceErr != nil {
		report.Warnings = append(report.Warnings, "无法读取源命名空间 NetworkPolicy："+sourceErr.Error())
		sourcePolicies = nil
	}
	report.Warnings = append(report.Warnings, sourceWarnings...)
	destPolicies := sourcePolicies
	if request.DestinationNamespace != request.SourceNamespace {
		destPolicies, sourceWarnings, sourceErr = s.repository.ListNetworkPolicies(ctx, request.DestinationNamespace)
		if sourceErr != nil {
			report.Warnings = append(report.Warnings, "无法读取目标命名空间 NetworkPolicy："+sourceErr.Error())
			destPolicies = nil
		}
		report.Warnings = append(report.Warnings, sourceWarnings...)
	}

	port, ok := findConnectivityServicePort(service, request.Port, request.Protocol)
	if !ok {
		report.Verdict = "blocked"
		report.Summary = "目标 Service 没有匹配的端口"
		report.Checks = append(report.Checks, domain.ConnectivityCheck{
			ID: "service-port", Label: "Service 端口映射", Status: "critical",
			Message: fmt.Sprintf("Service %s/%s 未声明 %s/%d", service.Namespace, service.Name, request.Protocol, request.Port),
		})
		report.Findings = append(report.Findings, connectivityFinding(
			"connectivity.service-port-missing", domain.DiagnosisCritical, "Service 端口不存在",
			"请求的协议和端口没有在 Service spec.ports 中声明，流量不会到达后端 Pod。",
			[]domain.DiagnosisEvidence{{Label: "请求端口", Value: fmt.Sprintf("%s/%d", request.Protocol, request.Port)}},
			"/network/services/"+service.Namespace+"/"+service.Name,
		))
		report.GeneratedAt = time.Now().UTC()
		s.auditConnectivity(ctx, actor, request, nil)
		return report, nil
	}
	report.Port = domain.ConnectivityPortMapping{ServicePort: port.Port, TargetPort: port.TargetPort, Protocol: port.Protocol}
	report.Checks = append(report.Checks, domain.ConnectivityCheck{
		ID: "service-port", Label: "Service 端口映射", Status: "pass",
		Message: fmt.Sprintf("%d/%s → targetPort %s/%s", port.Port, port.Protocol, port.TargetPort, port.Protocol),
	})

	if service.Type == "ExternalName" {
		report.Verdict = "unknown"
		report.Summary = "ExternalName Service 由集群 DNS 返回外部 CNAME，无法静态判断外部 L4 路径"
		report.Checks = append(report.Checks, domain.ConnectivityCheck{
			ID: "endpoints", Label: "Service 后端", Status: "info", Message: "ExternalName 不使用 EndpointSlice",
		})
		report.GeneratedAt = time.Now().UTC()
		s.auditConnectivity(ctx, actor, request, nil)
		return report, nil
	}

	allowed := 0
	blocked := 0
	unknown := 0
	egressDecisionSeen := map[string]domain.ConnectivityPolicyDecision{}
	ingressDecisionSeen := map[string]domain.ConnectivityPolicyDecision{}
	for _, endpoint := range service.Endpoints {
		item := domain.ConnectivityEndpoint{PodIP: firstAddress(endpoint.Addresses), Ready: endpoint.Ready, Pod: endpoint.TargetName}
		if !endpoint.Ready {
			item.Allowed = "blocked"
			item.Reasons = append(item.Reasons, "EndpointSlice 地址未就绪")
			blocked++
			report.Endpoints = append(report.Endpoints, item)
			continue
		}
		if endpoint.TargetKind != "Pod" || endpoint.TargetName == "" {
			item.Allowed = "unknown"
			item.Reasons = append(item.Reasons, "Endpoint 没有 Pod 引用，无法按 Pod 标签评估 NetworkPolicy")
			unknown++
			report.Endpoints = append(report.Endpoints, item)
			continue
		}
		targetNamespace := endpoint.TargetNamespace
		if targetNamespace == "" {
			targetNamespace = service.Namespace
		}
		targetPod, podErr := s.repository.GetPod(ctx, targetNamespace, endpoint.TargetName)
		if podErr != nil {
			item.Allowed = "unknown"
			item.Reasons = append(item.Reasons, "无法读取后端 Pod："+podErr.Error())
			unknown++
			report.Endpoints = append(report.Endpoints, item)
			continue
		}
		targetPort, targetPortName, portKnown := resolveConnectivityTargetPort(targetPod, port.TargetPort, port.Protocol)
		if !portKnown {
			item.Allowed = "blocked"
			item.Reasons = append(item.Reasons, fmt.Sprintf("后端 Pod 未声明命名端口 %s/%s", port.TargetPort, port.Protocol))
			blocked++
			report.Endpoints = append(report.Endpoints, item)
			continue
		}
		egressAllowed, egressUnknown, egressDecisions := evaluateNetworkPolicyDirection(
			sourcePolicies, source.Labels,
			targetPod.Namespace, targetPod.Labels, destNamespaceLabels, destNamespaceKnown, targetPod.IP,
			targetPort, targetPortName, port.Protocol, "Egress",
		)
		ingressAllowed, ingressUnknown, ingressDecisions := evaluateNetworkPolicyDirection(
			destPolicies, targetPod.Labels,
			source.Namespace, source.Labels, sourceNamespaceLabels, sourceNamespaceKnown, source.IP,
			targetPort, targetPortName, port.Protocol, "Ingress",
		)
		for _, decision := range egressDecisions {
			egressDecisionSeen[decision.Namespace+"/"+decision.Name] = decision
		}
		for _, decision := range ingressDecisions {
			ingressDecisionSeen[decision.Namespace+"/"+decision.Name] = decision
		}
		if !egressUnknown {
			item.EgressAllowed = boolPointer(egressAllowed)
		}
		if !ingressUnknown {
			item.IngressAllowed = boolPointer(ingressAllowed)
		}
		switch {
		case egressUnknown || ingressUnknown:
			item.Allowed = "unknown"
			unknown++
			if egressUnknown {
				item.Reasons = append(item.Reasons, "源 Pod 出站策略包含无法静态判定的条件")
			}
			if ingressUnknown {
				item.Reasons = append(item.Reasons, "后端 Pod 入站策略包含无法静态判定的条件")
			}
		case egressAllowed && ingressAllowed:
			item.Allowed = "allowed"
			allowed++
		default:
			item.Allowed = "blocked"
			blocked++
			if !egressAllowed {
				item.Reasons = append(item.Reasons, "源 Pod 的出站 NetworkPolicy 未允许该目标")
			}
			if !ingressAllowed {
				item.Reasons = append(item.Reasons, "后端 Pod 的入站 NetworkPolicy 未允许该来源")
			}
		}
		report.Endpoints = append(report.Endpoints, item)
	}

	report.EgressPolicies = sortedPolicyDecisions(egressDecisionSeen)
	report.IngressPolicies = sortedPolicyDecisions(ingressDecisionSeen)
	report.Checks = append(report.Checks,
		connectivityEndpointCheck(len(service.Endpoints), allowed+blocked+unknown),
		connectivityPolicyCheck("egress", "源 Pod 出站策略", report.EgressPolicies),
		connectivityPolicyCheck("ingress", "后端 Pod 入站策略", report.IngressPolicies),
	)

	switch {
	case len(service.Endpoints) == 0:
		report.Verdict = "blocked"
		report.Summary = "Service 没有 EndpointSlice 后端，DNS 可能解析成功但 L4 流量无目标"
		report.Checks = append(report.Checks, domain.ConnectivityCheck{ID: "endpoints", Label: "Endpoint 就绪状态", Status: "critical", Message: "没有可用的 EndpointSlice 地址"})
		report.Findings = append(report.Findings, connectivityFinding(
			"connectivity.no-endpoints", domain.DiagnosisCritical, "Service 没有后端 Endpoint",
			"Endpoint 控制器没有为该 Service 生成地址，流量在 Service 层就无法转发。",
			[]domain.DiagnosisEvidence{{Label: "选择器", Value: selectorText(service.Selector)}},
			"/network/services/"+service.Namespace+"/"+service.Name,
		))
	case allowed > 0 && blocked == 0 && unknown == 0:
		report.Verdict = "allowed"
		report.Summary = fmt.Sprintf("静态分析允许从 %s/%s 访问 %s/%s:%d，%d 个就绪后端均通过策略检查", source.Namespace, source.Name, service.Namespace, service.Name, port.Port, allowed)
	case allowed > 0:
		report.Verdict = "degraded"
		report.Summary = fmt.Sprintf("部分后端可达：%d 个允许，%d 个被阻断，%d 个无法判定", allowed, blocked, unknown)
	case blocked > 0 && unknown == 0:
		report.Verdict = "blocked"
		report.Summary = "NetworkPolicy 或 Endpoint 状态阻断了全部后端"
		report.Findings = append(report.Findings, connectivityFinding(
			"connectivity.policy-blocked", domain.DiagnosisCritical, "NetworkPolicy 阻断访问",
			"源 Pod 的出站策略或后端 Pod 的入站策略没有允许该目标端口。",
			[]domain.DiagnosisEvidence{{Label: "目标", Value: fmt.Sprintf("%s/%s:%d", service.Namespace, service.Name, port.Port)}},
			"/network?namespace="+service.Namespace+"&tab=policies",
		))
	default:
		report.Verdict = "unknown"
		report.Summary = "当前信息不足以确定 L4 连通性，请检查页面中的策略与 Endpoint 证据"
	}
	report.Warnings = append(report.Warnings,
		"本诊断基于 Kubernetes 对象进行静态推演，不发送探测包；实际执行还取决于 CNI 是否正确执行 NetworkPolicy。",
	)
	if source.IP == "" {
		report.Warnings = append(report.Warnings, "源 Pod 没有状态 IP，基于 ipBlock 的策略无法精确匹配。")
	}
	report.GeneratedAt = time.Now().UTC()
	s.auditConnectivity(ctx, actor, request, nil)
	return report, nil
}

func buildConnectivityBase(source domain.PodDetail, service domain.ServiceDetail, request domain.ConnectivityRequest) domain.ConnectivityDiagnosis {
	dnsName := service.Name + "." + service.Namespace + ".svc"
	dnsExplanation := "集群 DNS 应返回 Service ClusterIP；Pod 再通过 kube-proxy 或 eBPF 转发到 Endpoint。"
	if service.Headless {
		dnsExplanation = "Headless Service 的 DNS 直接返回就绪 Pod 地址，不经过 ClusterIP。"
	}
	if service.Type == "ExternalName" {
		dnsExplanation = "ExternalName Service 的 DNS 返回外部 CNAME，集群内不会创建 EndpointSlice。"
	}
	report := domain.ConnectivityDiagnosis{
		Verdict: "unknown",
		Source:  domain.ConnectivitySource{Name: source.Name, Namespace: source.Namespace, IP: source.IP, Labels: source.Labels, Status: source.Status},
		Destination: domain.ConnectivityDestination{
			Name: service.Name, Namespace: service.Namespace, Type: service.Type, ClusterIP: service.ClusterIP,
			Headless: service.Headless, ExternalName: service.ExternalName, Selector: service.Selector,
		},
		DNS:       domain.ConnectivityDNS{Name: dnsName, ClusterIP: service.ClusterIP, ExternalName: service.ExternalName, Headless: service.Headless, Explanation: dnsExplanation},
		Port:      domain.ConnectivityPortMapping{ServicePort: request.Port, Protocol: request.Protocol},
		Endpoints: []domain.ConnectivityEndpoint{}, Checks: []domain.ConnectivityCheck{},
		EgressPolicies: []domain.ConnectivityPolicyDecision{}, IngressPolicies: []domain.ConnectivityPolicyDecision{}, Findings: []domain.DiagnosisFinding{},
	}
	return report
}

func (s *Service) namespaceLabels(ctx context.Context, namespace string, report *domain.ConnectivityDiagnosis) (map[string]string, bool) {
	item, err := s.repository.GetNamespaceMetadata(ctx, namespace)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("无法读取命名空间 %s 的标签：%s", namespace, err.Error()))
		if errors.Is(err, domain.ErrForbidden) {
			return nil, false
		}
		return nil, false
	}
	return item.Labels, true
}

func normalizeConnectivityRequest(request *domain.ConnectivityRequest) error {
	request.SourceNamespace = strings.TrimSpace(request.SourceNamespace)
	request.SourcePod = strings.TrimSpace(request.SourcePod)
	request.DestinationNamespace = strings.TrimSpace(request.DestinationNamespace)
	request.DestinationService = strings.TrimSpace(request.DestinationService)
	request.Protocol = strings.ToUpper(strings.TrimSpace(request.Protocol))
	if request.Protocol == "" {
		request.Protocol = "TCP"
	}
	if err := validateNetworkNamespace(request.SourceNamespace); err != nil {
		return err
	}
	if err := validateNetworkNamespace(request.DestinationNamespace); err != nil {
		return err
	}
	if request.SourcePod == "" || len(request.SourcePod) > 253 || !networkDNSSubdomainPattern.MatchString(request.SourcePod) {
		return serviceValidationError("sourcePod", "sourcePod must be a valid DNS-1123 subdomain")
	}
	if err := validateServiceIdentity(request.DestinationNamespace, request.DestinationService); err != nil {
		return err
	}
	if request.Port < 1 || request.Port > 65535 {
		return serviceValidationError("port", "port must be between 1 and 65535")
	}
	if _, ok := serviceProtocols[request.Protocol]; !ok {
		return serviceValidationError("protocol", "protocol must be TCP, UDP, or SCTP")
	}
	return nil
}

func validateNetworkNamespace(namespace string) error {
	if namespace == "" || len(namespace) > 63 || !dnsLabelPattern.MatchString(namespace) {
		return serviceValidationError("namespace", "namespace must be a valid DNS-1123 label")
	}
	return nil
}

func findConnectivityServicePort(service domain.ServiceDetail, port int32, protocol string) (domain.ServicePort, bool) {
	for _, candidate := range service.Ports {
		if candidate.Port == port && strings.EqualFold(candidate.Protocol, protocol) {
			return candidate, true
		}
	}
	return domain.ServicePort{}, false
}

func resolveConnectivityTargetPort(pod domain.PodDetail, targetPort, protocol string) (int32, string, bool) {
	if numeric, err := strconv.Atoi(targetPort); err == nil {
		if numeric < 1 || numeric > 65535 {
			return 0, "", false
		}
		for _, container := range pod.Containers {
			for _, port := range container.Ports {
				if port.Port == int32(numeric) && strings.EqualFold(port.Protocol, protocol) && port.Name != "" {
					return int32(numeric), port.Name, true
				}
			}
		}
		return int32(numeric), "", true
	}
	for _, container := range pod.Containers {
		for _, port := range container.Ports {
			if port.Name == targetPort && strings.EqualFold(port.Protocol, protocol) {
				return port.Port, port.Name, true
			}
		}
	}
	return 0, "", false
}

func evaluateNetworkPolicyDirection(
	policies []domain.NetworkPolicySummary,
	selectedLabels map[string]string,
	peerNamespace string,
	peerLabels map[string]string,
	peerNamespaceLabels map[string]string,
	peerNamespaceKnown bool,
	peerIP string,
	port int32,
	portName string,
	protocol string,
	direction string,
) (bool, bool, []domain.ConnectivityPolicyDecision) {
	selected := make([]domain.NetworkPolicySummary, 0)
	for _, policy := range policies {
		if !containsString(policy.PolicyTypes, direction) {
			continue
		}
		if !selectorMatches(selectedLabels, policy.PodSelector, policy.PodSelectorExpressions) {
			continue
		}
		selected = append(selected, policy)
	}
	if len(selected) == 0 {
		return true, false, nil
	}
	anyAllowed := false
	anyUnknown := false
	decisions := make([]domain.ConnectivityPolicyDecision, 0, len(selected))
	for _, policy := range selected {
		allowed := false
		unknown := false
		rules := policy.Ingress
		if direction == "Egress" {
			rules = policy.Egress
		}
		for index, rule := range rules {
			ruleUnknown := false
			peerAllowed := len(rule.Peers) == 0
			for _, peer := range rule.Peers {
				matched, peerUnknown := networkPolicyPeerMatches(
					policy.Namespace, peer, peerNamespace, peerLabels, peerNamespaceLabels, peerNamespaceKnown, peerIP,
				)
				if peerUnknown {
					ruleUnknown = true
				}
				if matched {
					peerAllowed = true
					break
				}
			}
			if peerAllowed && networkPolicyPortsMatch(rule.Ports, port, portName, protocol) {
				allowed = true
				decisions = append(decisions, domain.ConnectivityPolicyDecision{
					Name: policy.Name, Namespace: policy.Namespace, Direction: direction, Selected: true, Allowed: true,
					Reason: fmt.Sprintf("规则 %d 允许该流量", index+1),
				})
				break
			}
			if ruleUnknown {
				unknown = true
			}
		}
		if allowed {
			anyAllowed = true
			continue
		}
		if unknown {
			anyUnknown = true
		}
		decisions = append(decisions, domain.ConnectivityPolicyDecision{
			Name: policy.Name, Namespace: policy.Namespace, Direction: direction, Selected: true, Allowed: false,
			Reason: "策略选择了 Pod，但没有规则允许该来源/目标与端口",
		})
	}
	if anyAllowed {
		return true, false, decisions
	}
	return false, anyUnknown, decisions
}

func networkPolicyPeerMatches(
	policyNamespace string,
	peer domain.NetworkPolicyPeer,
	namespace string,
	podLabels map[string]string,
	namespaceLabels map[string]string,
	namespaceKnown bool,
	ip string,
) (bool, bool) {
	if peer.IPBlock != nil {
		if ip == "" {
			return false, false
		}
		return ipInBlock(ip, peer.IPBlock.CIDR, peer.IPBlock.Except), false
	}
	if peer.PodSelector == nil && peer.NamespaceSelector == nil {
		return true, false
	}
	namespaceMatched := namespace == policyNamespace
	unknown := false
	if peer.NamespaceSelector != nil {
		if !namespaceKnown && !selectorIsEmpty(peer.NamespaceSelector, peer.NamespaceSelectorExpressions) {
			unknown = true
		}
		namespaceMatched = selectorMatches(namespaceLabels, peer.NamespaceSelector, peer.NamespaceSelectorExpressions)
	}
	if !namespaceMatched {
		return false, unknown
	}
	if peer.PodSelector == nil {
		return true, unknown
	}
	return selectorMatches(podLabels, peer.PodSelector, peer.PodSelectorExpressions), unknown
}

func networkPolicyPortsMatch(ports []domain.NetworkPolicyPort, port int32, portName, protocol string) bool {
	if len(ports) == 0 {
		return true
	}
	for _, candidate := range ports {
		if !strings.EqualFold(candidate.Protocol, protocol) {
			continue
		}
		if candidate.Port == "" {
			return true
		}
		numeric, err := strconv.Atoi(candidate.Port)
		if err != nil {
			if portName != "" && candidate.Port == portName {
				return true
			}
			continue
		}
		end := int32(numeric)
		if candidate.EndPort >= end {
			end = candidate.EndPort
		}
		if int32(numeric) == port || (candidate.EndPort > 0 && port >= int32(numeric) && port <= end) {
			return true
		}
	}
	return false
}

func selectorIsEmpty(matchLabels map[string]string, expressions []domain.LabelSelectorRequirement) bool {
	return len(matchLabels) == 0 && len(expressions) == 0
}

func selectorMatches(labels map[string]string, matchLabels map[string]string, expressions []domain.LabelSelectorRequirement) bool {
	for key, value := range matchLabels {
		if labels[key] != value {
			return false
		}
	}
	for _, expression := range expressions {
		value, exists := labels[expression.Key]
		switch expression.Operator {
		case "In":
			if !exists || !containsString(expression.Values, value) {
				return false
			}
		case "NotIn":
			if exists && containsString(expression.Values, value) {
				return false
			}
		case "Exists":
			if !exists {
				return false
			}
		case "DoesNotExist":
			if exists {
				return false
			}
		case "Gt", "Lt":
			if !exists || len(expression.Values) != 1 {
				return false
			}
			left, leftErr := strconv.ParseInt(value, 10, 64)
			right, rightErr := strconv.ParseInt(expression.Values[0], 10, 64)
			if leftErr != nil || rightErr != nil {
				return false
			}
			if expression.Operator == "Gt" && left <= right || expression.Operator == "Lt" && left >= right {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func ipInBlock(ipValue, cidr string, except []string) bool {
	ip := net.ParseIP(ipValue)
	_, network, err := net.ParseCIDR(cidr)
	if ip == nil || err != nil || !network.Contains(ip) {
		return false
	}
	for _, item := range except {
		_, excluded, err := net.ParseCIDR(item)
		if err == nil && excluded.Contains(ip) {
			return false
		}
	}
	return true
}

func connectivityEndpointCheck(total, evaluated int) domain.ConnectivityCheck {
	if total == 0 {
		return domain.ConnectivityCheck{ID: "endpoints", Label: "Endpoint 就绪状态", Status: "critical", Message: "没有 EndpointSlice 地址"}
	}
	if evaluated < total {
		return domain.ConnectivityCheck{ID: "endpoints", Label: "Endpoint 就绪状态", Status: "warning", Message: fmt.Sprintf("共 %d 个地址，其中 %d 个可进入 Pod 策略评估", total, evaluated)}
	}
	return domain.ConnectivityCheck{ID: "endpoints", Label: "Endpoint 就绪状态", Status: "pass", Message: fmt.Sprintf("%d 个 Endpoint 已进入策略评估", total)}
}

func connectivityPolicyCheck(id, label string, decisions []domain.ConnectivityPolicyDecision) domain.ConnectivityCheck {
	if len(decisions) == 0 {
		return domain.ConnectivityCheck{ID: id, Label: label, Status: "pass", Message: "没有选择该 Pod 的相关隔离策略，默认允许"}
	}
	for _, decision := range decisions {
		if decision.Allowed {
			return domain.ConnectivityCheck{ID: id, Label: label, Status: "pass", Message: "至少一条策略规则允许该流量"}
		}
	}
	return domain.ConnectivityCheck{ID: id, Label: label, Status: "critical", Message: "策略选择了 Pod，但没有任何允许规则"}
}

func connectivityFinding(id string, severity string, title, cause string, evidence []domain.DiagnosisEvidence, path string) domain.DiagnosisFinding {
	return domain.DiagnosisFinding{
		ID: id, Severity: severity, Title: title, Cause: cause, Evidence: evidence,
		Recommendations: []domain.DiagnosisRecommendation{{Label: "检查网络策略与 Service", Path: path}},
	}
}

func sortedPolicyDecisions(values map[string]domain.ConnectivityPolicyDecision) []domain.ConnectivityPolicyDecision {
	out := make([]domain.ConnectivityPolicyDecision, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}

func boolPointer(value bool) *bool { return &value }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstAddress(addresses []string) string {
	if len(addresses) == 0 {
		return ""
	}
	return addresses[0]
}

func selectorText(selector map[string]string) string {
	if len(selector) == 0 {
		return "全部 Pod"
	}
	keys := make([]string, 0, len(selector))
	for key := range selector {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+selector[key])
	}
	return strings.Join(values, ",")
}

func networkPolicySearchText(policy domain.NetworkPolicySummary) string {
	parts := []string{policy.Name, policy.Namespace, strings.Join(policy.PolicyTypes, " "), selectorText(policy.PodSelector)}
	for _, rule := range append(append([]domain.NetworkPolicyRule{}, policy.Ingress...), policy.Egress...) {
		for _, peer := range rule.Peers {
			parts = append(parts, selectorText(peer.PodSelector), selectorText(peer.NamespaceSelector))
			if peer.IPBlock != nil {
				parts = append(parts, peer.IPBlock.CIDR)
			}
		}
		for _, port := range rule.Ports {
			parts = append(parts, port.Protocol+"/"+port.Port)
		}
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func filterGatewayClasses(items []domain.GatewayClassSummary, search string) []domain.GatewayClassSummary {
	out := make([]domain.GatewayClassSummary, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name+" "+item.Controller+" "+item.Status), search) {
			out = append(out, item)
		}
	}
	return out
}

func filterGateways(items []domain.GatewaySummary, search string) []domain.GatewaySummary {
	out := make([]domain.GatewaySummary, 0, len(items))
	for _, item := range items {
		text := item.Name + " " + item.Namespace + " " + item.ClassName + " " + item.Status
		for _, address := range item.Addresses {
			text += " " + address.Type + " " + address.Value
		}
		for _, listener := range item.Listeners {
			text += " " + listener.Name + " " + listener.Protocol + " " + listener.Hostname
		}
		if strings.Contains(strings.ToLower(text), search) {
			out = append(out, item)
		}
	}
	return out
}

func filterGatewayRoutes(items []domain.GatewayRouteSummary, search string) []domain.GatewayRouteSummary {
	out := make([]domain.GatewayRouteSummary, 0, len(items))
	for _, item := range items {
		text := item.Kind + " " + item.Name + " " + item.Namespace + " " + strings.Join(item.Hostnames, " ")
		for _, parent := range item.ParentRefs {
			text += " " + parent.Namespace + "/" + parent.Name
		}
		for _, rule := range item.Rules {
			for _, backend := range rule.Backends {
				text += " " + backend.Namespace + "/" + backend.Name + ":" + backend.Port
			}
		}
		if strings.Contains(strings.ToLower(text), search) {
			out = append(out, item)
		}
	}
	return out
}

func (s *Service) auditConnectivity(ctx context.Context, actor domain.Actor, request domain.ConnectivityRequest, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: "diagnose-connectivity", Resource: "service", Namespace: request.DestinationNamespace,
		Name: request.DestinationService, Result: result, Err: err,
		Details: map[string]any{"sourceNamespace": request.SourceNamespace, "sourcePod": request.SourcePod, "port": request.Port, "protocol": request.Protocol},
	})
}
