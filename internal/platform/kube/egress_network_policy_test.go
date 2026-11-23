package kube

import (
	"context"
	"testing"

	"dosh/internal/platform/config"
	"dosh/internal/platform/egress"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func runtimePolicyManager(t *testing.T, objects ...*networkingv1.NetworkPolicy) (*EgressNetworkPolicyManager, *fake.Clientset) {
	t.Helper()
	clientset := fake.NewSimpleClientset()
	for _, object := range objects {
		if _, err := clientset.NetworkingV1().NetworkPolicies("dosh").Create(context.Background(), object, metav1.CreateOptions{}); err != nil {
			t.Fatalf("seed NetworkPolicy: %v", err)
		}
	}
	client := NewForClients(clientset, nil, nil, config.Config{PreferencesNamespace: "dosh"})
	manager, err := NewEgressNetworkPolicyManager(client, config.Config{
		PreferencesNamespace: "dosh", NetworkPolicyEnabled: true, NetworkPolicyRuntime: true,
		NetworkPolicyName: "dosh-egress-runtime", NetworkPolicyPodLabels: "app.kubernetes.io/name=dosh,app.kubernetes.io/instance=dosh",
	})
	if err != nil {
		t.Fatalf("NewEgressNetworkPolicyManager() error = %v", err)
	}
	return manager, clientset
}

func TestEgressNetworkPolicyRuntimeEnsureAndCleanup(t *testing.T) {
	manager, clientset := runtimePolicyManager(t)
	targets := []egress.ProxyTarget{
		{Kind: "http", Host: "proxy.example.test", Port: 1080, CIDRs: []string{"192.0.2.10/32", "2001:db8::10/128"}},
	}
	if err := manager.Ensure(context.Background(), targets); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	policy, err := clientset.NetworkingV1().NetworkPolicies("dosh").Get(context.Background(), "dosh-egress-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(NetworkPolicy) error = %v", err)
	}
	if !ownedByDosh(policy) || len(policy.Spec.Egress) != 1 {
		t.Fatalf("policy = %#v", policy)
	}
	rule := policy.Spec.Egress[0]
	if len(rule.To) != 2 || rule.Ports[0].Port.IntVal != 1080 {
		t.Fatalf("egress rule = %#v", rule)
	}
	for _, peer := range rule.To {
		if peer.IPBlock == nil || peer.IPBlock.CIDR == "0.0.0.0/0" || peer.IPBlock.CIDR == "::/0" {
			t.Fatalf("unsafe peer = %#v", peer)
		}
	}
	if err := manager.Ensure(context.Background(), []egress.ProxyTarget{{Kind: "https", Port: 8443, CIDRs: []string{"198.51.100.20/32"}}}); err != nil {
		t.Fatalf("Ensure(update) error = %v", err)
	}
	policy, err = clientset.NetworkingV1().NetworkPolicies("dosh").Get(context.Background(), "dosh-egress-runtime", metav1.GetOptions{})
	if err != nil || policy.Spec.Egress[0].To[0].IPBlock.CIDR != "198.51.100.20/32" {
		t.Fatalf("updated policy = %#v, err = %v", policy, err)
	}
	if err := manager.Cleanup(context.Background()); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := clientset.NetworkingV1().NetworkPolicies("dosh").Get(context.Background(), "dosh-egress-runtime", metav1.GetOptions{}); err == nil {
		t.Fatal("runtime NetworkPolicy still exists after Cleanup")
	}
}

func TestEgressNetworkPolicyRejectsBroadRuntimeCIDR(t *testing.T) {
	manager, _ := runtimePolicyManager(t)
	if err := manager.Ensure(context.Background(), []egress.ProxyTarget{{Kind: "http", Port: 8080, CIDRs: []string{"0.0.0.0/0"}}}); err == nil {
		t.Fatal("Ensure() accepted a default route")
	}
}

func TestEgressNetworkPolicyHelmPreflightInspectsSelectedPolicies(t *testing.T) {
	protocol := corev1.ProtocolTCP
	port := intstr.FromInt(1080)
	namespaceClient := fake.NewSimpleClientset(
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "dosh", Namespace: "dosh"},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "dosh"}},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress: []networkingv1.NetworkPolicyEgressRule{{
					To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "192.0.2.10/32"}}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
				}},
			},
		},
	)
	client := NewForClients(namespaceClient, nil, nil, config.Config{PreferencesNamespace: "dosh"})
	manager, err := NewEgressNetworkPolicyManager(client, config.Config{
		PreferencesNamespace: "dosh", NetworkPolicyEnabled: true,
		NetworkPolicyHelmName: "dosh", NetworkPolicyPodLabels: "app.kubernetes.io/name=dosh,app.kubernetes.io/instance=dosh",
	})
	if err != nil {
		t.Fatalf("NewEgressNetworkPolicyManager() error = %v", err)
	}
	targets, err := manager.CheckTargets(context.Background(), []egress.ProxyTarget{
		{Kind: "http", Port: 1080, Addresses: []string{"192.0.2.10"}},
		{Kind: "https", Port: 1080, Addresses: []string{"192.0.2.11"}},
	})
	if err != nil {
		t.Fatalf("CheckTargets() error = %v", err)
	}
	if !targets[0].Allowed || targets[0].PolicySource != "dosh" {
		t.Fatalf("allowed target = %#v", targets[0])
	}
	if targets[1].Allowed {
		t.Fatalf("unexpectedly allowed target = %#v", targets[1])
	}
}

func TestEgressNetworkPolicyDoesNotOverwriteForeignPolicy(t *testing.T) {
	foreign := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "dosh-egress-runtime", Namespace: "dosh"}}
	manager, _ := runtimePolicyManager(t, foreign)
	if err := manager.Ensure(context.Background(), []egress.ProxyTarget{{Kind: "http", Port: 8080, CIDRs: []string{"192.0.2.10/32"}}}); err == nil {
		t.Fatal("Ensure() overwrote a foreign NetworkPolicy")
	}
}
