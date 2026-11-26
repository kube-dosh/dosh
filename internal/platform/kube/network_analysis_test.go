package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestListNetworkPoliciesExplainsSelectorsRulesAndWarnings(t *testing.T) {
	t.Parallel()
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "web-policy", Namespace: "prod", ResourceVersion: "12"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}}}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPointer(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.String, StrVal: "http"}}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tier", Operator: metav1.LabelSelectorOpIn, Values: []string{"db"}}}}}},
			}},
		},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "prod", Labels: map[string]string{"app": "web"}}}
	client, _ := newFakeKubeClient(policy, pod)
	items, warnings, err := client.ListNetworkPolicies(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListNetworkPolicies() error = %v", err)
	}
	if len(warnings) != 0 || len(items) != 1 {
		t.Fatalf("items = %#v warnings = %#v", items, warnings)
	}
	item := items[0]
	if item.MatchedPods != 1 || len(item.Ingress) != 1 || len(item.Ingress[0].Peers) != 1 || item.Ingress[0].Peers[0].NamespaceSelector["team"] != "platform" || item.Ingress[0].Ports[0].Port != "http" {
		t.Fatalf("policy = %#v", item)
	}
	if len(item.Egress) != 1 || len(item.Egress[0].Peers) != 1 || len(item.Egress[0].Peers[0].PodSelectorExpressions) != 1 || item.Egress[0].Peers[0].PodSelectorExpressions[0].Key != "tier" {
		t.Fatalf("egress = %#v", item.Egress)
	}
	foundDNSWarning := false
	for _, warning := range item.Warnings {
		if warning == "出站策略未明确放行 DNS 53/TCP 或 UDP；源 Pod 的域名解析可能失败" {
			foundDNSWarning = true
		}
	}
	if !foundDNSWarning {
		t.Fatalf("warnings = %#v", item.Warnings)
	}
}

func TestGatewayAPIOverviewDiscoversCommonResources(t *testing.T) {
	t.Parallel()
	gatewayGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	classGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
	routeGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			gatewayGVR: "GatewayList", classGVR: "GatewayClassList", routeGVR: "HTTPRouteList",
			{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}: "GRPCRouteList",
		},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1", "kind": "GatewayClass",
			"metadata": map[string]any{"name": "public", "resourceVersion": "1"},
			"spec":     map[string]any{"controllerName": "example.net/gateway"},
			"status":   map[string]any{"conditions": []any{map[string]any{"type": "Accepted", "status": "True", "reason": "Accepted"}}},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
			"metadata": map[string]any{"name": "web", "namespace": "prod", "resourceVersion": "3"},
			"spec": map[string]any{
				"hostnames":  []any{"app.example.test"},
				"parentRefs": []any{map[string]any{"name": "edge"}},
				"rules": []any{map[string]any{
					"matches":     []any{map[string]any{"path": map[string]any{"type": "PathPrefix", "value": "/"}}},
					"backendRefs": []any{map[string]any{"name": "web", "port": int64(80)}},
				}},
			},
			"status": map[string]any{"parents": []any{map[string]any{"conditions": []any{map[string]any{"type": "Accepted", "status": "True"}, map[string]any{"type": "ResolvedRefs", "status": "True"}}}}},
		}},
	)
	// The fake dynamic tracker mechanically pluralizes Gateway as "gatewaies".
	// Create the object through the explicit GVR used by the real API server so
	// the test exercises the same resource path as production.
	gatewayObject := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway",
		"metadata": map[string]any{"name": "edge", "namespace": "prod", "resourceVersion": "2"},
		"spec":     map[string]any{"gatewayClassName": "public", "listeners": []any{map[string]any{"name": "http", "protocol": "HTTP", "port": int64(80), "hostname": "app.example.test"}}},
		"status": map[string]any{
			"addresses":  []any{map[string]any{"type": "IPAddress", "value": "192.0.2.10"}},
			"conditions": []any{map[string]any{"type": "Accepted", "status": "True"}, map[string]any{"type": "Programmed", "status": "True"}},
		},
	}}
	if err := dynamicClient.Tracker().Create(gatewayGVR, gatewayObject, "prod"); err != nil {
		t.Fatalf("create Gateway fixture: %v", err)
	}
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}}}},
		{GroupVersion: "gateway.networking.k8s.io/v1", APIResources: []metav1.APIResource{
			{Name: "gatewayclasses", Kind: "GatewayClass", Verbs: []string{"get", "list"}},
			{Name: "gateways", Kind: "Gateway", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "httproutes", Kind: "HTTPRoute", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "grpcroutes", Kind: "GRPCRoute", Namespaced: true, Verbs: []string{"get", "list"}},
		}},
	}
	client := NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, discovery, testConfig())
	overview, err := client.GatewayAPIOverview(context.Background(), "prod")
	if err != nil {
		t.Fatalf("GatewayAPIOverview() error = %v", err)
	}
	if !overview.Available || len(overview.Classes) != 1 || overview.Classes[0].Controller != "example.net/gateway" || overview.Classes[0].Status != "Ready" {
		t.Fatalf("classes = %#v", overview)
	}
	if len(overview.Gateways) != 1 || overview.Gateways[0].Status != "Ready" || len(overview.Gateways[0].Addresses) != 1 || len(overview.Gateways[0].Listeners) != 1 {
		t.Fatalf("gateways = %#v", overview.Gateways)
	}
	if len(overview.Routes) != 1 || overview.Routes[0].Status != "Ready" || len(overview.Routes[0].Rules) != 1 || overview.Routes[0].Rules[0].Backends[0].Name != "web" || overview.Routes[0].ParentRefs[0].Name != "edge" {
		t.Fatalf("routes = %#v", overview.Routes)
	}
}

func protocolPointer(protocol corev1.Protocol) *corev1.Protocol { return &protocol }
