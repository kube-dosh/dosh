package kube

import (
	"context"
	"fmt"
	"sort"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type kubeadmClusterConfiguration struct {
	ClusterName          string `json:"clusterName"`
	KubernetesVersion    string `json:"kubernetesVersion"`
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint"`
	Networking           struct {
		DNSDomain     string `json:"dnsDomain"`
		PodSubnet     string `json:"podSubnet"`
		ServiceSubnet string `json:"serviceSubnet"`
	} `json:"networking"`
}

func (c *Client) GetClusterConfiguration(ctx context.Context) (domain.ClusterConfiguration, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	configuration := domain.ClusterConfiguration{
		Name:      c.clusterName,
		CNI:       c.DetectCNI(ctx),
		NodeCIDRs: []domain.ClusterNodeCIDR{},
	}
	warnings := make([]string, 0)
	loaded := 0

	if info, err := c.discovery.ServerVersion(); err != nil {
		warnings = append(warnings, "Kubernetes version is unavailable")
	} else {
		configuration.Version = info.GitVersion
		configuration.Platform = info.Platform
		loaded++
	}

	var kubeadm kubeadmClusterConfiguration
	configMap, err := c.clientset.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(ctx, "kubeadm-config", metav1.GetOptions{})
	if err != nil {
		warnings = append(warnings, "kubeadm-config is unavailable; pod and service CIDRs could not be read")
	} else if raw := configMap.Data["ClusterConfiguration"]; raw == "" {
		warnings = append(warnings, "kubeadm-config does not contain ClusterConfiguration")
	} else if err := yaml.Unmarshal([]byte(raw), &kubeadm); err != nil {
		warnings = append(warnings, "kubeadm ClusterConfiguration could not be parsed")
	} else {
		configuration.Source = "kubeadm-config"
		configuration.ControlPlaneEndpoint = kubeadm.ControlPlaneEndpoint
		configuration.PodCIDR = kubeadm.Networking.PodSubnet
		configuration.ServiceCIDR = kubeadm.Networking.ServiceSubnet
		configuration.DNSDomain = kubeadm.Networking.DNSDomain
		if configuration.Name == "" {
			configuration.Name = kubeadm.ClusterName
		}
		if configuration.Version == "" {
			configuration.Version = kubeadm.KubernetesVersion
		}
		loaded++
	}

	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		warnings = append(warnings, "node Pod CIDRs are unavailable")
	} else {
		for _, node := range nodes.Items {
			cidrs := append([]string(nil), node.Spec.PodCIDRs...)
			if len(cidrs) == 0 && node.Spec.PodCIDR != "" {
				cidrs = []string{node.Spec.PodCIDR}
			}
			configuration.NodeCIDRs = append(configuration.NodeCIDRs, domain.ClusterNodeCIDR{
				Name:       node.Name,
				InternalIP: nodeInternalIP(node),
				PodCIDRs:   cidrs,
			})
		}
		sort.Slice(configuration.NodeCIDRs, func(i, j int) bool {
			return configuration.NodeCIDRs[i].Name < configuration.NodeCIDRs[j].Name
		})
		loaded++
	}

	for _, target := range []struct {
		namespace string
		name      string
		assign    func(string)
		warning   string
	}{
		{namespace: metav1.NamespaceDefault, name: "kubernetes", assign: func(ip string) { configuration.KubernetesServiceIP = ip }, warning: "Kubernetes Service IP is unavailable"},
		{namespace: metav1.NamespaceSystem, name: "kube-dns", assign: func(ip string) { configuration.DNSServiceIP = ip }, warning: "cluster DNS Service IP is unavailable"},
	} {
		service, getErr := c.clientset.CoreV1().Services(target.namespace).Get(ctx, target.name, metav1.GetOptions{})
		if getErr != nil {
			warnings = append(warnings, target.warning)
			continue
		}
		target.assign(service.Spec.ClusterIP)
		loaded++
	}

	if loaded == 0 {
		return domain.ClusterConfiguration{}, fmt.Errorf("%w: cluster configuration could not be loaded", domain.ErrUnavailable)
	}
	if configuration.Name == "" {
		configuration.Name = "Kubernetes"
	}
	configuration.Warnings = warnings
	return configuration, nil
}

func nodeInternalIP(node corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}
