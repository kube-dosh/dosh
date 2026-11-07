package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
)

func testService() *corev1.Service {
	policy := corev1.IPFamilyPolicyPreferDualStack
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web", Namespace: "prod",
			Labels:      map[string]string{"app": "web"},
			Annotations: map[string]string{"team": "platform"},
		},
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			ClusterIP:       "10.96.0.20",
			SessionAffinity: corev1.ServiceAffinityClientIP,
			IPFamilies:      []corev1.IPFamily{corev1.IPv4Protocol},
			IPFamilyPolicy:  &policy,
			Selector:        map[string]string{"app": "web"},
			Ports:           []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt32(8080), NodePort: 30080}},
		},
	}
}

func testServiceEndpointSlice() *discoveryv1.EndpointSlice {
	ready := true
	notReady := false
	node := "worker-1"
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-abc", Namespace: "prod",
			Labels: map[string]string{discoveryv1.LabelServiceName: "web"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.244.1.10"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "web-0", Namespace: "prod"},
				NodeName:   &node,
			},
			{Addresses: []string{"10.244.1.11"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
		},
	}
}

func testIngress() *networkingv1.Ingress {
	prefix := networkingv1.PathTypePrefix
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web", Namespace: "prod",
			Labels:      map[string]string{"app": "web"},
			Annotations: map[string]string{"nginx.ingress.kubernetes.io/rewrite-target": "/"},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ptr("nginx"),
			DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
				Name: "web", Port: networkingv1.ServiceBackendPort{Number: 80},
			}},
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"web.example.test"}, SecretName: "web-tls"}},
			Rules: []networkingv1.IngressRule{{Host: "web.example.test", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &prefix, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
					Name: "web", Port: networkingv1.ServiceBackendPort{Name: "http"},
				}}}},
			}}}},
		},
	}
}

func ptr[T any](value T) *T { return &value }

func TestGetServiceIncludesEndpointReadinessAndMetadata(t *testing.T) {
	client, _ := newFakeKubeClient(testService(), testServiceEndpointSlice())
	detail, err := client.GetService(context.Background(), "prod", "web")
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if detail.Name != "web" || detail.ClusterIP != "10.96.0.20" || detail.Type != "ClusterIP" {
		t.Fatalf("summary = %#v", detail.ServiceSummary)
	}
	if detail.SessionAffinity != "ClientIP" || detail.IPFamilyPolicy != "PreferDualStack" || len(detail.IPFamilies) != 1 || detail.IPFamilies[0] != "IPv4" {
		t.Fatalf("service policy = %#v", detail)
	}
	if detail.Labels["app"] != "web" || detail.Annotations["team"] != "platform" {
		t.Fatalf("metadata = %#v %#v", detail.Labels, detail.Annotations)
	}
	if detail.EndpointCount != 1 || len(detail.Endpoints) != 2 {
		t.Fatalf("endpoints = %#v count = %d", detail.Endpoints, detail.EndpointCount)
	}
	if detail.Endpoints[0].Addresses[0] != "10.244.1.10" || !detail.Endpoints[0].Ready || detail.Endpoints[0].TargetName != "web-0" || detail.Endpoints[0].NodeName != "worker-1" {
		t.Fatalf("ready endpoint = %#v", detail.Endpoints[0])
	}
	if detail.Endpoints[1].Ready {
		t.Fatalf("unready endpoint = %#v", detail.Endpoints[1])
	}
}

func TestGetServiceMapsNotFound(t *testing.T) {
	client, _ := newFakeKubeClient()
	_, err := client.GetService(context.Background(), "prod", "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetService() error = %v, want ErrNotFound", err)
	}
}

func TestGetIngressIncludesDefaultBackendTLSAndTargetStatus(t *testing.T) {
	client, _ := newFakeKubeClient(testIngress(), testService(), testServiceEndpointSlice())
	detail, err := client.GetIngress(context.Background(), "prod", "web")
	if err != nil {
		t.Fatalf("GetIngress() error = %v", err)
	}
	if detail.Name != "web" || detail.Class != "nginx" || len(detail.Paths) != 1 {
		t.Fatalf("summary = %#v", detail.IngressSummary)
	}
	if detail.Paths[0].TargetStatus != domain.IngressTargetReady || detail.DefaultBackend == nil || detail.DefaultBackend.TargetStatus != domain.IngressTargetReady {
		t.Fatalf("route statuses = %#v default = %#v", detail.Paths, detail.DefaultBackend)
	}
	if len(detail.TLS) != 1 || detail.TLS[0].SecretName != "web-tls" || len(detail.TLS[0].Hosts) != 1 {
		t.Fatalf("tls = %#v", detail.TLS)
	}
	if detail.Annotations["nginx.ingress.kubernetes.io/rewrite-target"] != "/" || detail.Labels["app"] != "web" {
		t.Fatalf("metadata = %#v %#v", detail.Labels, detail.Annotations)
	}
}

func TestGetIngressMapsNotFound(t *testing.T) {
	client, _ := newFakeKubeClient()
	_, err := client.GetIngress(context.Background(), "prod", "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetIngress() error = %v, want ErrNotFound", err)
	}
}

func TestCreateUpdateAndDeleteService(t *testing.T) {
	client, clientset := newFakeKubeClient()
	created, err := client.CreateService(context.Background(), domain.ServiceCreateSpec{
		Name: "api", Namespace: "prod", Type: "ClusterIP",
		Selector: map[string]string{"app": "api"},
		Ports:    []domain.ServicePortInput{{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "8080"}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if created.Name != "api" || created.Namespace != "prod" || created.Type != "ClusterIP" || created.Selector["app"] != "api" || len(created.Ports) != 1 || created.Ports[0].TargetPort != "8080" {
		t.Fatalf("created service = %#v", created)
	}
	headless, err := client.CreateService(context.Background(), domain.ServiceCreateSpec{
		Name: "db", Namespace: "prod", Type: "ClusterIP", Headless: true,
		Selector: map[string]string{"app": "db"},
		Ports:    []domain.ServicePortInput{{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "8080"}},
	})
	if err != nil {
		t.Fatalf("CreateService() headless error = %v", err)
	}
	stored, err := clientset.CoreV1().Services("prod").Get(context.Background(), "db", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("headless service lookup error = %v", err)
	}
	if !headless.Headless || headless.ClusterIP != corev1.ClusterIPNone || stored.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Fatalf("headless service = %#v stored ClusterIP = %q", headless, stored.Spec.ClusterIP)
	}

	service := testService()
	service.ResourceVersion = "7"
	updateClient, _ := newFakeKubeClient(service)
	updated, err := updateClient.UpdateService(context.Background(), "prod", "web", domain.ServiceUpdateSpec{
		Type: "NodePort", Selector: map[string]string{"app": "web"},
		Ports: []domain.ServicePortInput{{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "8080"}}, ResourceVersion: "7",
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updated.Type != "NodePort" || len(updated.Ports) != 1 || updated.Ports[0].NodePort != 30080 {
		t.Fatalf("updated service = %#v", updated)
	}
	_, err = updateClient.UpdateService(context.Background(), "prod", "web", domain.ServiceUpdateSpec{
		Type: "ClusterIP", Selector: map[string]string{"app": "web"},
		Ports: []domain.ServicePortInput{{Port: 80}}, ResourceVersion: "stale",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	service.ResourceVersion = "8"
	deleteClient, _ := newFakeKubeClient(service)
	if err := deleteClient.DeleteService(context.Background(), "prod", "web", "8"); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if _, err := deleteClient.GetService(context.Background(), "prod", "web"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted service lookup error = %v, want ErrNotFound", err)
	}
}

func TestCreateServiceMapsAlreadyExists(t *testing.T) {
	client, _ := newFakeKubeClient(testService())
	_, err := client.CreateService(context.Background(), domain.ServiceCreateSpec{
		Name: "web", Namespace: "prod", Type: "ClusterIP",
		Selector: map[string]string{"app": "web"},
		Ports:    []domain.ServicePortInput{{Port: 80}},
	})
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateService() error = %v, want ErrAlreadyExists", err)
	}
}

func TestDeleteServiceUsesResourceVersionPrecondition(t *testing.T) {
	service := testService()
	service.ResourceVersion = "9"
	client, clientset := newFakeKubeClient(service)
	var options metav1.DeleteOptions
	clientset.PrependReactor("delete", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if deleteAction, ok := action.(k8stesting.DeleteAction); ok {
			options = deleteAction.GetDeleteOptions()
		}
		return false, nil, nil
	})
	if err := client.DeleteService(context.Background(), "prod", "web", "9"); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if options.Preconditions == nil || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "9" {
		t.Fatalf("delete options = %#v", options)
	}
}
