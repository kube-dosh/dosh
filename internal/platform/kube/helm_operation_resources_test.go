package kube

import (
	"context"
	"testing"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestObserveHelmOperationResourcesReadsLiveObjects(t *testing.T) {
	t.Parallel()
	client := newGenericResourceClient(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.io/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "ready-widget", "namespace": "default", "uid": "widget-1"},
		"status": map[string]any{"conditions": []any{map[string]any{
			"type": "Ready", "status": "True", "message": "controller reconciled",
		}}},
	}})
	resources := []domain.HelmChartPlanResource{
		{APIVersion: "example.io/v1", Kind: "Widget", Namespace: "default", Name: "ready-widget", APIGroup: "example.io", Resource: "widgets", Action: "create"},
		{APIVersion: "example.io/v1", Kind: "Widget", Namespace: "default", Name: "missing-widget", APIGroup: "example.io", Resource: "widgets", Action: "create"},
	}
	items := client.ObserveHelmOperationResources(context.Background(), resources)
	if len(items) != 2 || items[0].Status != "ready" || items[1].Status != "pending" {
		t.Fatalf("ObserveHelmOperationResources() = %#v", items)
	}
	if items[1].Message != "等待 Helm 创建资源" {
		t.Fatalf("missing resource message = %q", items[1].Message)
	}
}

func TestHelmResourceObjectStatusReportsPendingPVCAndPodFailure(t *testing.T) {
	t.Parallel()
	pvc := map[string]any{"status": map[string]any{"phase": "Pending"}}
	status, message := helmResourceObjectStatus(domain.HelmChartPlanResource{Kind: "PersistentVolumeClaim"}, pvc)
	if status != "waiting" || message != "PVC 等待绑定" {
		t.Fatalf("PVC status = %q, %q", status, message)
	}

	pod := map[string]any{
		"status": map[string]any{
			"phase": "Running",
			"containerStatuses": []any{map[string]any{
				"name":  "api",
				"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff", "message": "back-off restarting failed container"}},
			}},
		},
	}
	status, message = helmResourceObjectStatus(domain.HelmChartPlanResource{Kind: "Pod"}, pod)
	if status != "failed" || message == "" {
		t.Fatalf("Pod status = %q, %q", status, message)
	}
}

func TestHelmResourceObjectStatusReportsWorkloadReadiness(t *testing.T) {
	t.Parallel()
	deployment := map[string]any{
		"metadata": map[string]any{"generation": int64(2)},
		"spec":     map[string]any{"replicas": int64(2)},
		"status": map[string]any{
			"observedGeneration": int64(2), "readyReplicas": int64(1),
			"conditions": []any{map[string]any{
				"type": "Progressing", "status": "True", "reason": "ReplicaSetUpdated", "message": "rolling out",
			}},
		},
	}
	status, message := helmResourceObjectStatus(domain.HelmChartPlanResource{Kind: "Deployment"}, deployment)
	if status != "waiting" || message != "就绪 1/2" {
		t.Fatalf("Deployment status = %q, %q", status, message)
	}
	deployment["status"].(map[string]any)["readyReplicas"] = int64(2)
	status, message = helmResourceObjectStatus(domain.HelmChartPlanResource{Kind: "Deployment"}, deployment)
	if status != "ready" || message != "就绪 2/2" {
		t.Fatalf("ready Deployment status = %q, %q", status, message)
	}
}

func TestHelmResourceEventMessagesPrefersLatestMatchingWarning(t *testing.T) {
	t.Parallel()
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "consul-0.1", Namespace: "default", CreationTimestamp: metav1.NewTime(time.Now().UTC())},
		InvolvedObject: corev1.ObjectReference{
			Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data-consul-0", UID: "pvc-uid",
		},
		Type: corev1.EventTypeWarning, Reason: "ProvisioningFailed",
		Message: "no persistent volumes available for this claim and no storage class is set", Count: 3,
	}
	client := NewForClients(k8sfake.NewSimpleClientset(event), nil, nil, testConfig())
	resources := []domain.HelmChartPlanResource{{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data-consul-0"}}
	messages := client.helmResourceEventMessages(context.Background(), resources, []bool{true}, []string{"pvc-uid"})
	message := messages["uid:pvc-uid"]
	if message == "" || message != "ProvisioningFailed: no persistent volumes available for this claim and no storage class is set（重复 3 次）" {
		t.Fatalf("event message = %q", message)
	}
}
