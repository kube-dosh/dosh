package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// GetNamespaceMetadata returns only the namespace object. It deliberately does
// not fetch quotas and limit ranges, so connectivity analysis does not require
// permissions unrelated to network diagnostics.
func (c *Client) GetNamespaceMetadata(ctx context.Context, name string) (domain.Namespace, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.Namespace{}, mapKubeError(err)
	}
	return domain.Namespace{
		Name: item.Name, Status: string(item.Status.Phase), ResourceVersion: item.ResourceVersion,
		Labels: item.Labels, Annotations: item.Annotations, CreatedAt: item.CreationTimestamp.Time,
	}, nil
}

// ListNetworkPolicies returns policy rules in a transport-neutral form. When a
// namespace is selected, matching pod counts are calculated in the same
// request. A missing list-pods permission degrades counts to a warning rather
// than hiding the policies themselves.
func (c *Client) ListNetworkPolicies(ctx context.Context, namespace string) ([]domain.NetworkPolicySummary, []string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	scope := namespace
	if scope == "" {
		scope = metav1.NamespaceAll
	}
	list, err := c.clientset.NetworkingV1().NetworkPolicies(scope).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, mapKubeError(err)
	}
	var warnings []string
	podsByNamespace := map[string][]corev1.Pod{}
	if namespace != "" {
		pods, podErr := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if podErr != nil {
			warnings = append(warnings, "无法读取 Pod，策略匹配数量未计算："+mapKubeError(podErr).Error())
		} else {
			podsByNamespace[namespace] = pods.Items
		}
	} else if len(list.Items) > 0 {
		warnings = append(warnings, "选择命名空间后可计算每条策略实际匹配的 Pod 数量")
	}

	out := make([]domain.NetworkPolicySummary, 0, len(list.Items))
	for _, policy := range list.Items {
		item := networkPolicySummary(policy, podsByNamespace[policy.Namespace])
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, warnings, nil
}

func networkPolicySummary(policy networkingv1.NetworkPolicy, pods []corev1.Pod) domain.NetworkPolicySummary {
	item := domain.NetworkPolicySummary{
		Name: policy.Name, Namespace: policy.Namespace, PodSelector: cloneStringMap(policy.Spec.PodSelector.MatchLabels),
		PodSelectorExpressions: labelSelectorRequirements(policy.Spec.PodSelector.MatchExpressions),
		PolicyTypes:            effectiveNetworkPolicyTypes(policy), Ingress: networkPolicyIngressRules(policy.Spec.Ingress),
		Egress: networkPolicyEgressRules(policy.Spec.Egress), ResourceVersion: policy.ResourceVersion,
		CreatedAt: policy.CreationTimestamp.Time, APIVersion: "networking.k8s.io/v1", Resource: "networkpolicies",
	}
	if item.PodSelector == nil {
		item.PodSelector = map[string]string{}
	}
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if err != nil {
		item.Warnings = append(item.Warnings, "Pod selector 无效："+err.Error())
	} else {
		for _, pod := range pods {
			if selector.Matches(labels.Set(pod.Labels)) {
				item.MatchedPods++
			}
		}
	}
	if item.MatchedPods == 0 && len(pods) > 0 {
		item.Warnings = append(item.Warnings, "当前命名空间中没有 Pod 匹配该 selector")
	}
	if selector != nil && selector.Empty() {
		item.Warnings = append(item.Warnings, "空 Pod selector 会选择命名空间内全部 Pod")
	}
	if containsString(item.PolicyTypes, "Ingress") && len(item.Ingress) == 0 {
		item.Warnings = append(item.Warnings, "已启用入站隔离但没有允许规则，入站流量将被默认拒绝")
	}
	if containsString(item.PolicyTypes, "Egress") && len(item.Egress) == 0 {
		item.Warnings = append(item.Warnings, "已启用出站隔离但没有允许规则，出站流量将被默认拒绝")
	}
	for index, rule := range item.Ingress {
		if len(rule.Peers) == 0 && len(rule.Ports) == 0 {
			item.Warnings = append(item.Warnings, fmt.Sprintf("入站规则 %d 允许所有来源和端口", index+1))
		}
	}
	for index, rule := range item.Egress {
		if len(rule.Peers) == 0 && len(rule.Ports) == 0 {
			item.Warnings = append(item.Warnings, fmt.Sprintf("出站规则 %d 允许访问所有目标和端口", index+1))
		}
	}
	if containsString(item.PolicyTypes, "Egress") && !networkPolicyAllowsDNS(item.Egress) {
		item.Warnings = append(item.Warnings, "出站策略未明确放行 DNS 53/TCP 或 UDP；源 Pod 的域名解析可能失败")
	}
	return item
}

func effectiveNetworkPolicyTypes(policy networkingv1.NetworkPolicy) []string {
	if len(policy.Spec.PolicyTypes) > 0 {
		out := make([]string, 0, len(policy.Spec.PolicyTypes))
		for _, policyType := range policy.Spec.PolicyTypes {
			out = append(out, string(policyType))
		}
		return out
	}
	out := []string{"Ingress"}
	if len(policy.Spec.Egress) > 0 {
		out = append(out, "Egress")
	}
	return out
}

func networkPolicyIngressRules(rules []networkingv1.NetworkPolicyIngressRule) []domain.NetworkPolicyRule {
	out := make([]domain.NetworkPolicyRule, 0, len(rules))
	for _, rule := range rules {
		item := domain.NetworkPolicyRule{Peers: make([]domain.NetworkPolicyPeer, 0, len(rule.From)), Ports: networkPolicyPorts(rule.Ports)}
		for _, peer := range rule.From {
			item.Peers = append(item.Peers, networkPolicyPeer(peer))
		}
		out = append(out, item)
	}
	return out
}

func networkPolicyEgressRules(rules []networkingv1.NetworkPolicyEgressRule) []domain.NetworkPolicyRule {
	out := make([]domain.NetworkPolicyRule, 0, len(rules))
	for _, rule := range rules {
		item := domain.NetworkPolicyRule{Peers: make([]domain.NetworkPolicyPeer, 0, len(rule.To)), Ports: networkPolicyPorts(rule.Ports)}
		for _, peer := range rule.To {
			item.Peers = append(item.Peers, networkPolicyPeer(peer))
		}
		out = append(out, item)
	}
	return out
}

func networkPolicyPeer(peer networkingv1.NetworkPolicyPeer) domain.NetworkPolicyPeer {
	out := domain.NetworkPolicyPeer{}
	if peer.PodSelector != nil {
		out.PodSelector = cloneStringMap(peer.PodSelector.MatchLabels)
		out.PodSelectorExpressions = labelSelectorRequirements(peer.PodSelector.MatchExpressions)
		if out.PodSelector == nil {
			out.PodSelector = map[string]string{}
		}
	}
	if peer.NamespaceSelector != nil {
		out.NamespaceSelector = cloneStringMap(peer.NamespaceSelector.MatchLabels)
		out.NamespaceSelectorExpressions = labelSelectorRequirements(peer.NamespaceSelector.MatchExpressions)
		if out.NamespaceSelector == nil {
			out.NamespaceSelector = map[string]string{}
		}
	}
	if peer.IPBlock != nil {
		out.IPBlock = &domain.NetworkPolicyIPBlock{CIDR: peer.IPBlock.CIDR, Except: append([]string(nil), peer.IPBlock.Except...)}
	}
	return out
}

func labelSelectorRequirements(expressions []metav1.LabelSelectorRequirement) []domain.LabelSelectorRequirement {
	out := make([]domain.LabelSelectorRequirement, 0, len(expressions))
	for _, expression := range expressions {
		out = append(out, domain.LabelSelectorRequirement{
			Key: expression.Key, Operator: string(expression.Operator), Values: append([]string(nil), expression.Values...),
		})
	}
	return out
}

func networkPolicyPorts(ports []networkingv1.NetworkPolicyPort) []domain.NetworkPolicyPort {
	out := make([]domain.NetworkPolicyPort, 0, len(ports))
	for _, port := range ports {
		item := domain.NetworkPolicyPort{Protocol: string(corev1.ProtocolTCP), Port: intstrString(port.Port)}
		if port.Protocol != nil {
			item.Protocol = string(*port.Protocol)
		}
		if port.EndPort != nil {
			item.EndPort = *port.EndPort
		}
		out = append(out, item)
	}
	return out
}

func intstrString(value *intstr.IntOrString) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func networkPolicyAllowsDNS(rules []domain.NetworkPolicyRule) bool {
	for _, rule := range rules {
		if len(rule.Peers) == 0 {
			if len(rule.Ports) == 0 || portsAllowDNS(rule.Ports) {
				return true
			}
			continue
		}
		for _, peer := range rule.Peers {
			// An IPBlock or a podSelector matching another namespace is not
			// sufficient on its own to prove DNS is reachable. Cluster DNS is
			// usually in a separate namespace, so a same-namespace pod selector
			// must not suppress this warning.
			if peer.IPBlock != nil {
				// A CIDR-specific rule without a DNS port does not prove that
				// cluster DNS is reachable. Without ports only an unrestricted
				// CIDR covers every possible DNS address.
				if portsAllowDNS(rule.Ports) || (len(rule.Ports) == 0 && ipBlockIsUnrestricted(peer.IPBlock)) {
					return true
				}
			}
			// A namespaceSelector without a podSelector selects every Pod in
			// matching namespaces. With no port restriction it also reaches DNS.
			if peer.NamespaceSelector != nil && peer.PodSelector == nil && len(rule.Ports) == 0 {
				return true
			}
			// Explicit DNS ports combined with a cross-namespace selector are a
			// strong enough signal for the static policy explanation.
			if peer.NamespaceSelector != nil && peer.PodSelector != nil && portsAllowDNS(rule.Ports) {
				return true
			}
		}
	}
	return false
}

func ipBlockIsUnrestricted(block *domain.NetworkPolicyIPBlock) bool {
	if block == nil || len(block.Except) != 0 {
		return false
	}
	return block.CIDR == "0.0.0.0/0" || block.CIDR == "::/0"
}

func portsAllowDNS(ports []domain.NetworkPolicyPort) bool {
	for _, port := range ports {
		if strings.EqualFold(port.Protocol, "UDP") && (port.Port == "53" || port.Port == "domain") {
			return true
		}
		if strings.EqualFold(port.Protocol, "TCP") && (port.Port == "53" || port.Port == "domain") {
			return true
		}
	}
	return false
}
