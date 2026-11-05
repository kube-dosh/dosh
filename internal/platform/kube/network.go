package kube

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (c *Client) ListServices(ctx context.Context, query domain.ListQuery) ([]domain.ServiceSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	endpointSlices, err := c.clientset.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	endpointCounts := readyEndpointCounts(endpointSlices.Items)
	out := make([]domain.ServiceSummary, 0, len(items.Items))
	for _, item := range items.Items {
		summary := serviceSummary(item)
		summary.EndpointCount = endpointCounts[serviceEndpointKey(item.Namespace, item.Name)]
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.Namespace, summary.Type, summary.ClusterIP, summary.ExternalIP}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func (c *Client) GetService(ctx context.Context, namespace, name string) (domain.ServiceDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.ServiceDetail{}, mapKubeError(err)
	}
	slices, err := c.clientset.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + name,
	})
	if err != nil {
		return domain.ServiceDetail{}, mapKubeError(err)
	}
	detail := domain.ServiceDetail{
		ServiceSummary:    serviceSummary(*item),
		Labels:            item.Labels,
		Annotations:       item.Annotations,
		ExternalName:      item.Spec.ExternalName,
		SessionAffinity:   string(item.Spec.SessionAffinity),
		IPFamilies:        make([]string, 0, len(item.Spec.IPFamilies)),
		IPFamilyPolicy:    "",
		LoadBalancerClass: "",
		Endpoints:         serviceEndpoints(slices.Items),
	}
	for _, family := range item.Spec.IPFamilies {
		detail.IPFamilies = append(detail.IPFamilies, string(family))
	}
	if item.Spec.IPFamilyPolicy != nil {
		detail.IPFamilyPolicy = string(*item.Spec.IPFamilyPolicy)
	}
	if item.Spec.LoadBalancerClass != nil {
		detail.LoadBalancerClass = *item.Spec.LoadBalancerClass
	}
	for _, endpoint := range detail.Endpoints {
		if endpoint.Ready {
			detail.EndpointCount += len(endpoint.Addresses)
		}
	}
	return detail, nil
}

func serviceEndpoints(slices []discoveryv1.EndpointSlice) []domain.ServiceEndpoint {
	out := make([]domain.ServiceEndpoint, 0)
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			addresses := append([]string(nil), endpoint.Addresses...)
			if len(addresses) == 0 {
				continue
			}
			item := domain.ServiceEndpoint{
				Addresses: addresses,
				Ready:     endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready,
			}
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				item.Ready = false
			}
			if endpoint.TargetRef != nil {
				item.TargetKind = endpoint.TargetRef.Kind
				item.TargetName = endpoint.TargetRef.Name
				item.TargetNamespace = endpoint.TargetRef.Namespace
			}
			if endpoint.NodeName != nil {
				item.NodeName = *endpoint.NodeName
			}
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Addresses) == 0 || len(out[j].Addresses) == 0 {
			return len(out[i].Addresses) < len(out[j].Addresses)
		}
		return out[i].Addresses[0] < out[j].Addresses[0]
	})
	return out
}

func readyEndpointCounts(slices []discoveryv1.EndpointSlice) map[string]int {
	counts := make(map[string]int)
	for _, slice := range slices {
		serviceName := slice.Labels[discoveryv1.LabelServiceName]
		if serviceName == "" {
			continue
		}
		key := serviceEndpointKey(slice.Namespace, serviceName)
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				continue
			}
			counts[key] += len(endpoint.Addresses)
		}
	}
	return counts
}

func serviceEndpointKey(namespace, name string) string { return namespace + "/" + name }

func serviceSummary(item corev1.Service) domain.ServiceSummary {
	external := append([]string{}, item.Spec.ExternalIPs...)
	for _, ingress := range item.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			external = append(external, ingress.IP)
		}
		if ingress.Hostname != "" {
			external = append(external, ingress.Hostname)
		}
	}
	ports := make([]domain.ServicePort, 0, len(item.Spec.Ports))
	for _, port := range item.Spec.Ports {
		ports = append(ports, domain.ServicePort{
			Name: port.Name, Protocol: string(port.Protocol), Port: port.Port, TargetPort: intOrString(port.TargetPort), NodePort: port.NodePort,
		})
	}
	externalIP := strings.Join(external, ", ")
	if externalIP == "" && item.Spec.Type == corev1.ServiceTypeExternalName {
		externalIP = item.Spec.ExternalName
	}
	return domain.ServiceSummary{
		Name: item.Name, Namespace: item.Namespace, Type: string(item.Spec.Type), ClusterIP: item.Spec.ClusterIP,
		ExternalIP: externalIP, Ports: ports, Selector: item.Spec.Selector, CreatedAt: item.CreationTimestamp.Time,
	}
}

func intOrString(value intstr.IntOrString) string {
	if value.Type == intstr.String {
		return value.StrVal
	}
	return strconv.Itoa(int(value.IntVal))
}

func (c *Client) ListIngresses(ctx context.Context, query domain.ListQuery) ([]domain.IngressSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	services, err := c.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	endpointSlices, err := c.clientset.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	servicePorts := servicePortIndexes(services.Items)
	endpointCounts := readyEndpointCounts(endpointSlices.Items)
	out := make([]domain.IngressSummary, 0, len(items.Items))
	for _, item := range items.Items {
		summary := ingressSummary(item)
		for index := range summary.Paths {
			summary.Paths[index].TargetStatus = ingressTargetStatus(item.Namespace, summary.Paths[index], servicePorts, endpointCounts)
		}
		if query.Search != "" && !matchesSearch(append([]string{summary.Name, summary.Namespace, summary.Class}, ingressHosts(summary)...), query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func (c *Client) GetIngress(ctx context.Context, namespace, name string) (domain.IngressDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.IngressDetail{}, mapKubeError(err)
	}
	services, err := c.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.IngressDetail{}, mapKubeError(err)
	}
	endpointSlices, err := c.clientset.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.IngressDetail{}, mapKubeError(err)
	}
	detail := ingressDetail(*item, servicePortIndexes(services.Items), readyEndpointCounts(endpointSlices.Items))
	return detail, nil
}

func servicePortIndexes(services []corev1.Service) map[string]map[string]struct{} {
	servicePorts := make(map[string]map[string]struct{}, len(services))
	for _, item := range services {
		ports := make(map[string]struct{}, len(item.Spec.Ports)*2)
		for _, port := range item.Spec.Ports {
			ports[port.Name] = struct{}{}
			ports[strconv.Itoa(int(port.Port))] = struct{}{}
		}
		servicePorts[serviceEndpointKey(item.Namespace, item.Name)] = ports
	}
	return servicePorts
}

func ingressTargetStatus(namespace string, path domain.IngressPath, servicePorts map[string]map[string]struct{}, endpointCounts map[string]int) string {
	if path.Service == "" {
		return domain.IngressTargetUnknown
	}
	key := serviceEndpointKey(namespace, path.Service)
	ports, ok := servicePorts[key]
	if !ok {
		return domain.IngressTargetServiceNotFound
	}
	if _, ok := ports[path.Port]; !ok {
		return domain.IngressTargetPortNotFound
	}
	if endpointCounts[key] == 0 {
		return domain.IngressTargetNoReadyEndpoints
	}
	return domain.IngressTargetReady
}

func ingressSummary(item networkingv1.Ingress) domain.IngressSummary {
	class := ""
	if item.Spec.IngressClassName != nil {
		class = *item.Spec.IngressClassName
	}
	summary := domain.IngressSummary{Name: item.Name, Namespace: item.Namespace, Class: class, CreatedAt: item.CreationTimestamp.Time}
	for _, tls := range item.Spec.TLS {
		summary.TLSHosts = append(summary.TLSHosts, tls.Hosts...)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			summary.Paths = append(summary.Paths, ingressPath(rule.Host, path.Path, path.PathType, path.Backend))
		}
	}
	return summary
}

func ingressDetail(item networkingv1.Ingress, servicePorts map[string]map[string]struct{}, endpointCounts map[string]int) domain.IngressDetail {
	summary := ingressSummary(item)
	for index := range summary.Paths {
		summary.Paths[index].TargetStatus = ingressTargetStatus(item.Namespace, summary.Paths[index], servicePorts, endpointCounts)
	}
	detail := domain.IngressDetail{
		IngressSummary: summary,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		TLS:            make([]domain.IngressTLS, 0, len(item.Spec.TLS)),
	}
	if item.Spec.DefaultBackend != nil {
		backend := ingressPath("", "", nil, *item.Spec.DefaultBackend)
		backend.TargetStatus = ingressTargetStatus(item.Namespace, backend, servicePorts, endpointCounts)
		detail.DefaultBackend = &backend
	}
	for _, tls := range item.Spec.TLS {
		detail.TLS = append(detail.TLS, domain.IngressTLS{Hosts: append([]string(nil), tls.Hosts...), SecretName: tls.SecretName})
	}
	return detail
}

func ingressPath(host, path string, pathType *networkingv1.PathType, backend networkingv1.IngressBackend) domain.IngressPath {
	item := domain.IngressPath{Host: host, Path: path}
	if pathType != nil {
		item.PathType = string(*pathType)
	}
	if backend.Service != nil {
		item.Service = backend.Service.Name
		if backend.Service.Port.Name != "" {
			item.Port = backend.Service.Port.Name
		} else {
			item.Port = strconv.Itoa(int(backend.Service.Port.Number))
		}
	}
	return item
}

func ingressHosts(summary domain.IngressSummary) []string {
	out := make([]string, 0, len(summary.Paths))
	for _, path := range summary.Paths {
		out = append(out, path.Host, path.Path, path.Service)
	}
	return out
}

func (c *Client) ListIngressClasses(ctx context.Context, query domain.ListQuery) ([]domain.IngressClassSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	items, err := c.clientset.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.IngressClassSummary, 0, len(items.Items))
	for _, item := range items.Items {
		summary := domain.IngressClassSummary{
			Name:       item.Name,
			Controller: item.Spec.Controller,
			Default:    item.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true",
			CreatedAt:  item.CreationTimestamp.Time,
		}
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.Controller}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
