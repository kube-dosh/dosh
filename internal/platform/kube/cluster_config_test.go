package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestGetClusterConfigurationReadsKubeadmNetworkAndServiceEndpoints(t *testing.T) {
	clusterConfiguration := `apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
clusterName: cluster-a
kubernetesVersion: v1.36.5
controlPlaneEndpoint: 192.168.122.50:16443
networking:
  dnsDomain: cluster.local
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
`
	clientset := k8sfake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kubeadm-config", Namespace: metav1.NamespaceSystem}, Data: map[string]string{"ClusterConfiguration": clusterConfiguration}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}, Spec: corev1.NodeSpec{PodCIDR: "10.244.1.0/24", PodCIDRs: []string{"10.244.1.0/24"}}, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.168.122.200"}}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.1"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kube-dns", Namespace: metav1.NamespaceSystem}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.10"}},
	)
	discovery := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}, FakedServerVersion: &version.Info{GitVersion: "v1.36.5", Platform: "linux/amd64"}}
	client := NewForClients(clientset, nil, discovery, testConfig())

	got, err := client.GetClusterConfiguration(context.Background())
	if err != nil {
		t.Fatalf("GetClusterConfiguration() error = %v", err)
	}
	if got.Name != "test" || got.Version != "v1.36.5" || got.Platform != "linux/amd64" || got.Source != "kubeadm-config" {
		t.Fatalf("identity = %#v", got)
	}
	if got.ControlPlaneEndpoint != "192.168.122.50:16443" || got.PodCIDR != "10.244.0.0/16" || got.ServiceCIDR != "10.96.0.0/12" || got.DNSDomain != "cluster.local" {
		t.Fatalf("network = %#v", got)
	}
	if got.KubernetesServiceIP != "10.96.0.1" || got.DNSServiceIP != "10.96.0.10" {
		t.Fatalf("service IPs = %#v", got)
	}
	if len(got.NodeCIDRs) != 1 || got.NodeCIDRs[0].Name != "worker-a" || got.NodeCIDRs[0].InternalIP != "192.168.122.200" || got.NodeCIDRs[0].PodCIDRs[0] != "10.244.1.0/24" {
		t.Fatalf("node CIDRs = %#v", got.NodeCIDRs)
	}
}

func TestGetClusterConfigurationDegradesWhenKubeadmConfigIsUnavailable(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}, Spec: corev1.NodeSpec{PodCIDRs: []string{"10.244.1.0/24"}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.1"}},
	)
	discovery := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}, FakedServerVersion: &version.Info{GitVersion: "v1.36.5"}}
	client := NewForClients(clientset, nil, discovery, testConfig())

	got, err := client.GetClusterConfiguration(context.Background())
	if err != nil {
		t.Fatalf("GetClusterConfiguration() error = %v", err)
	}
	if got.PodCIDR != "" || got.ServiceCIDR != "" || len(got.NodeCIDRs) != 1 || got.KubernetesServiceIP != "10.96.0.1" {
		t.Fatalf("partial configuration = %#v", got)
	}
	if len(got.Warnings) == 0 || !strings.Contains(strings.Join(got.Warnings, " "), "kubeadm-config") {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}
