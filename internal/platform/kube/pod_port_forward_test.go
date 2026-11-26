package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestResolvePortForwardServiceUsesReadyPodAndNamedTargetPort(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstrFromString("http"), Protocol: corev1.ProtocolTCP}},
		},
	}
	notReady := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-a", Namespace: "prod", Labels: map[string]string{"app": "web"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}}}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	ready := notReady.DeepCopy()
	ready.Name = "web-b"
	ready.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	client := NewForClients(k8sfake.NewSimpleClientset(service, notReady, ready), nil, nil, testConfig())

	target, err := client.ResolvePortForwardTarget(context.Background(), domain.PortForwardTarget{Kind: "service", Namespace: "prod", Name: "web", Port: 80})
	if err != nil {
		t.Fatalf("ResolvePortForwardTarget() error = %v", err)
	}
	if target.Pod != "web-b" || target.Service != "web" || target.Port != 8080 {
		t.Fatalf("target = %#v", target)
	}
}

func TestResolvePortForwardServiceRejectsNonTCPAndSelectorless(t *testing.T) {
	selectorless := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: "prod"}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}}}
	udp := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "dns", Namespace: "prod"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "dns"}, Ports: []corev1.ServicePort{{Port: 53, Protocol: corev1.ProtocolUDP}}},
	}
	client := NewForClients(k8sfake.NewSimpleClientset(selectorless, udp), nil, nil, testConfig())
	if _, err := client.ResolvePortForwardTarget(context.Background(), domain.PortForwardTarget{Kind: "service", Namespace: "prod", Name: "external", Port: 80}); err == nil || !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("selectorless error = %v, want unsupported", err)
	}
	if _, err := client.ResolvePortForwardTarget(context.Background(), domain.PortForwardTarget{Kind: "service", Namespace: "prod", Name: "dns", Port: 53}); err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UDP error = %v, want not found", err)
	}
}

func TestResolvePortForwardServiceValidatesSelectedBackendPod(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(8080), Protocol: corev1.ProtocolTCP}},
		},
	}
	ready := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-a", Namespace: "prod", Labels: map[string]string{"app": "web"}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	notBackend := ready.DeepCopy()
	notBackend.Name = "other"
	notBackend.Labels = map[string]string{"app": "other"}
	client := NewForClients(k8sfake.NewSimpleClientset(service, ready, notBackend), nil, nil, testConfig())

	target, err := client.ResolvePortForwardTarget(context.Background(), domain.PortForwardTarget{
		Kind: "service", Namespace: "prod", Name: "web", Port: 80, PodName: "web-a",
	})
	if err != nil || target.Pod != "web-a" || target.Port != 8080 {
		t.Fatalf("selected backend = %#v, %v", target, err)
	}
	if _, err := client.ResolvePortForwardTarget(context.Background(), domain.PortForwardTarget{
		Kind: "service", Namespace: "prod", Name: "web", Port: 80, PodName: "other",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("non-backend Pod error = %v, want invalid", err)
	}
}

func TestResolvePortForwardPodRequiresRunningPod(t *testing.T) {
	pending := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "prod"}, Status: corev1.PodStatus{Phase: corev1.PodPending}}
	client := NewForClients(k8sfake.NewSimpleClientset(pending), nil, nil, testConfig())
	if _, err := client.ResolvePortForwardTarget(context.Background(), domain.PortForwardTarget{Kind: "pod", Namespace: "prod", Name: "pending", Port: 80}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("pending Pod error = %v, want unavailable", err)
	}
}

func TestListPortForwardBackendPodsUsesServiceSelectorWithoutPageLimit(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
	}
	matching := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-b", Namespace: "prod", Labels: map[string]string{"app": "web"}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	other := matching.DeepCopy()
	other.Name = "other"
	other.Labels = map[string]string{"app": "other"}
	client := NewForClients(k8sfake.NewSimpleClientset(service, matching, other), nil, nil, testConfig())

	items, err := client.ListPortForwardBackendPods(context.Background(), "prod", "web")
	if err != nil || len(items) != 1 || items[0].Name != "web-b" {
		t.Fatalf("ListPortForwardBackendPods() = %#v, %v", items, err)
	}
	selectorless := service.DeepCopy()
	selectorless.Name = "external"
	selectorless.Spec.Selector = nil
	client = NewForClients(k8sfake.NewSimpleClientset(selectorless), nil, nil, testConfig())
	if _, err := client.ListPortForwardBackendPods(context.Background(), "prod", "external"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("selectorless Service error = %v, want unsupported", err)
	}
}

func intstrFromString(value string) intstr.IntOrString {
	return intstr.FromString(value)
}
