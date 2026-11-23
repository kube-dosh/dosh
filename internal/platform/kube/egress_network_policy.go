package kube

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"dosh/internal/domain"
	"dosh/internal/platform/config"
	"dosh/internal/platform/egress"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	egressRuntimePolicyComponent = "egress-runtime"
	egressRuntimePolicyLabel     = "dosh.io/egress-policy"
)

// EgressNetworkPolicyManager owns the optional, dedicated runtime NetworkPolicy
// that lets Dosh reach the exact proxy endpoints selected in the UI. It never
// mutates the Helm-managed policy and never permits a default route.
type EgressNetworkPolicyManager struct {
	client        *Client
	namespace     string
	enabled       bool
	runtime       bool
	policyName    string
	helmPolicy    string
	podLabels     labels.Set
	selectorValid bool
}

func NewEgressNetworkPolicyManager(client *Client, cfg config.Config) (*EgressNetworkPolicyManager, error) {
	manager := &EgressNetworkPolicyManager{
		client: client, namespace: cfg.PreferencesNamespace, enabled: cfg.NetworkPolicyEnabled,
		runtime: cfg.NetworkPolicyRuntime, policyName: cfg.NetworkPolicyName, helmPolicy: cfg.NetworkPolicyHelmName,
	}
	if !manager.runtime {
		return manager, nil
	}
	if client == nil || client.clientset == nil {
		return nil, fmt.Errorf("egress NetworkPolicy runtime management requires a Kubernetes client")
	}
	if manager.namespace == "" {
		return nil, fmt.Errorf("egress NetworkPolicy runtime management requires a control-plane namespace")
	}
	if manager.policyName == "" {
		return nil, fmt.Errorf("egress NetworkPolicy runtime management requires a policy name")
	}
	selector, err := labels.ConvertSelectorToLabelsMap(cfg.NetworkPolicyPodLabels)
	if err != nil {
		return nil, fmt.Errorf("parse egress NetworkPolicy pod selector: %w", err)
	}
	if len(selector) == 0 {
		return nil, fmt.Errorf("egress NetworkPolicy runtime management requires a non-empty pod selector")
	}
	manager.podLabels = selector
	manager.selectorValid = true
	return manager, nil
}

func (m *EgressNetworkPolicyManager) Status() egress.NetworkPolicyStatus {
	if m == nil || !m.enabled {
		return egress.NetworkPolicyStatus{Mode: egress.NetworkPolicyModeDisabled, Message: "Helm NetworkPolicy 未启用"}
	}
	status := egress.NetworkPolicyStatus{
		Enabled: true, PolicyName: m.policyName, HelmPolicyName: m.helmPolicy,
	}
	if m.runtime {
		status.Mode = egress.NetworkPolicyModeRuntime
		status.AutoManage = true
		status.CanAutoManage = m.selectorValid && m.client != nil && m.namespace != ""
		status.Message = "Dosh 自动维护专用代理 NetworkPolicy"
		return status
	}
	status.Mode = egress.NetworkPolicyModeHelm
	status.Message = "Helm 控制 NetworkPolicy；保存代理前需在 values 中放行目标 CIDR 和端口"
	return status
}

func (m *EgressNetworkPolicyManager) CheckTargets(ctx context.Context, targets []egress.ProxyTarget) ([]egress.ProxyTarget, error) {
	result := append([]egress.ProxyTarget(nil), targets...)
	if len(result) == 0 || m == nil || !m.enabled || m.client == nil {
		for index := range result {
			result[index].Allowed = true
			result[index].PolicySource = "unmanaged"
		}
		return result, nil
	}
	if m.runtime {
		for index := range result {
			result[index].Allowed = true
			result[index].PolicySource = "runtime:" + m.policyName
		}
		return result, nil
	}

	policies, err := m.selectedEgressPolicies(ctx)
	if err != nil {
		return targets, err
	}
	if len(policies) == 0 {
		for index := range result {
			result[index].Allowed = true
			result[index].PolicySource = "none"
		}
		return result, nil
	}
	for index := range result {
		allowed := true
		sources := map[string]struct{}{}
		for _, address := range result[index].Addresses {
			matched := false
			for _, policy := range policies {
				if policyAllowsAddressPort(policy, address, result[index].Port) {
					matched = true
					sources[policy.Name] = struct{}{}
				}
			}
			if !matched {
				allowed = false
				break
			}
		}
		result[index].Allowed = allowed
		result[index].PolicySource = strings.Join(sortedKeys(sources), ",")
	}
	return result, nil
}

func (m *EgressNetworkPolicyManager) Ensure(ctx context.Context, targets []egress.ProxyTarget) error {
	if m == nil || !m.runtime || !m.enabled || m.client == nil {
		return nil
	}
	if len(targets) == 0 {
		return m.Cleanup(ctx)
	}
	desired, err := m.desiredPolicy(targets)
	if err != nil {
		return err
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := m.client.withTimeout(ctx)
	defer cancel()
	policies := m.client.clientset.NetworkingV1().NetworkPolicies(m.namespace)
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		current, err := policies.Get(ctx, m.policyName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			created, createErr := policies.Create(ctx, desired, metav1.CreateOptions{FieldManager: "dosh"})
			if createErr == nil {
				_ = created
				return nil
			}
			if apierrors.IsAlreadyExists(createErr) || apierrors.IsConflict(createErr) {
				lastErr = createErr
				continue
			}
			return mapKubeError(createErr)
		}
		if err != nil {
			return mapKubeError(err)
		}
		if !ownedByDosh(current) {
			return fmt.Errorf("%w: NetworkPolicy %s/%s exists but is not managed by Dosh", domain.ErrConflict, m.namespace, m.policyName)
		}
		updated := desired.DeepCopy()
		updated.ResourceVersion = current.ResourceVersion
		_, updateErr := policies.Update(ctx, updated, metav1.UpdateOptions{FieldManager: "dosh"})
		if updateErr == nil {
			return nil
		}
		if apierrors.IsConflict(updateErr) {
			lastErr = updateErr
			continue
		}
		return mapKubeError(updateErr)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("NetworkPolicy update did not converge")
	}
	return mapKubeError(lastErr)
}

func (m *EgressNetworkPolicyManager) Cleanup(ctx context.Context) error {
	if m == nil || !m.runtime || !m.enabled || m.client == nil {
		return nil
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := m.client.withTimeout(ctx)
	defer cancel()
	err := m.client.clientset.NetworkingV1().NetworkPolicies(m.namespace).Delete(ctx, m.policyName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return mapKubeError(err)
}

func (m *EgressNetworkPolicyManager) desiredPolicy(targets []egress.ProxyTarget) (*networkingv1.NetworkPolicy, error) {
	cidrsByPort := map[int][]string{}
	for _, target := range targets {
		if target.Port < 1 || target.Port > 65535 {
			return nil, fmt.Errorf("%w: proxy port must be between 1 and 65535", domain.ErrInvalid)
		}
		for _, cidr := range target.CIDRs {
			if err := validateExactCIDR(cidr); err != nil {
				return nil, err
			}
			cidrsByPort[target.Port] = append(cidrsByPort[target.Port], cidr)
		}
	}
	ports := make([]int, 0, len(cidrsByPort))
	for port := range cidrsByPort {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	rules := make([]networkingv1.NetworkPolicyEgressRule, 0, len(ports))
	for _, port := range ports {
		cidrs := uniqueSorted(cidrsByPort[port])
		if len(cidrs) == 0 {
			continue
		}
		peers := make([]networkingv1.NetworkPolicyPeer, 0, len(cidrs))
		for _, cidr := range cidrs {
			peers = append(peers, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: cidr}})
		}
		protocol := corev1.ProtocolTCP
		targetPort := intstr.FromInt(port)
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To: peers,
			Ports: []networkingv1.NetworkPolicyPort{{
				Protocol: &protocol, Port: &targetPort,
			}},
		})
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("%w: no exact proxy targets were resolved", domain.ErrInvalid)
	}
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: m.policyName, Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "dosh",
				"app.kubernetes.io/name":       "dosh",
				"dosh.io/component":            egressRuntimePolicyComponent,
				egressRuntimePolicyLabel:       "true",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: m.podLabels},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      rules,
		},
	}, nil
}

func (m *EgressNetworkPolicyManager) selectedEgressPolicies(ctx context.Context) ([]networkingv1.NetworkPolicy, error) {
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := m.client.withTimeout(ctx)
	defer cancel()
	list, err := m.client.clientset.NetworkingV1().NetworkPolicies(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	result := make([]networkingv1.NetworkPolicy, 0)
	for _, policy := range list.Items {
		if m.helmPolicy != "" && policy.Name == m.helmPolicy {
			if policyHasEgress(policy) {
				result = append(result, policy)
			}
			continue
		}
		if !m.selectorValid {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		if err != nil || !selector.Matches(m.podLabels) || !policyHasEgress(policy) {
			continue
		}
		result = append(result, policy)
	}
	return result, nil
}

func policyHasEgress(policy networkingv1.NetworkPolicy) bool {
	if len(policy.Spec.Egress) > 0 {
		return true
	}
	for _, policyType := range policy.Spec.PolicyTypes {
		if policyType == networkingv1.PolicyTypeEgress {
			return true
		}
	}
	return false
}

func policyAllowsAddressPort(policy networkingv1.NetworkPolicy, address string, port int) bool {
	ip := net.ParseIP(address)
	if ip == nil {
		return false
	}
	for _, rule := range policy.Spec.Egress {
		if !policyRuleAllowsPort(rule, port) {
			continue
		}
		if len(rule.To) == 0 {
			return true
		}
		for _, peer := range rule.To {
			if peer.IPBlock != nil && ipBlockContains(peer.IPBlock, ip) {
				return true
			}
		}
	}
	return false
}

func policyRuleAllowsPort(rule networkingv1.NetworkPolicyEgressRule, port int) bool {
	if len(rule.Ports) == 0 {
		return true
	}
	for _, candidate := range rule.Ports {
		if candidate.Protocol != nil && *candidate.Protocol != corev1.ProtocolTCP {
			continue
		}
		if candidate.Port == nil {
			return true
		}
		if candidate.Port.Type != intstr.Int {
			continue
		}
		if candidate.Port.IntVal == int32(port) {
			return true
		}
		if candidate.EndPort != nil && int32(port) >= candidate.Port.IntVal && int32(port) <= *candidate.EndPort {
			return true
		}
	}
	return false
}

func ipBlockContains(block *networkingv1.IPBlock, ip net.IP) bool {
	_, network, err := net.ParseCIDR(block.CIDR)
	if err != nil || !network.Contains(ip) {
		return false
	}
	for _, excluded := range block.Except {
		_, exceptNetwork, err := net.ParseCIDR(excluded)
		if err == nil && exceptNetwork.Contains(ip) {
			return false
		}
	}
	return true
}

func validateExactCIDR(cidr string) error {
	ip, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil || ip == nil || network == nil {
		return fmt.Errorf("%w: invalid proxy IP CIDR %q", domain.ErrInvalid, cidr)
	}
	ones, bits := network.Mask.Size()
	if (bits == 32 && ones != 32) || (bits == 128 && ones != 128) {
		return fmt.Errorf("%w: runtime proxy CIDR must resolve to a single host: %s", domain.ErrInvalid, cidr)
	}
	if network.IP.IsUnspecified() {
		return fmt.Errorf("%w: runtime proxy CIDR must not be a default route", domain.ErrInvalid)
	}
	return nil
}

func ownedByDosh(policy *networkingv1.NetworkPolicy) bool {
	if policy == nil {
		return false
	}
	return policy.Labels["app.kubernetes.io/managed-by"] == "dosh" &&
		policy.Labels["dosh.io/component"] == egressRuntimePolicyComponent
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

var _ egress.NetworkPolicyManager = (*EgressNetworkPolicyManager)(nil)
