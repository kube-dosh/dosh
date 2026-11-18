package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
)

func TestAutoscalerLifecycleIncludesSpecMetricsAndDryRun(t *testing.T) {
	t.Parallel()
	client, clientset := newFakeKubeClient()
	cpu, memory := int32(65), int32(75)
	created, err := client.UpsertAutoscaler(context.Background(), domain.AutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api",
		MinReplicas: 2, MaxReplicas: 8, CPUUtilization: &cpu, MemoryUtilization: &memory,
	}, true)
	if err != nil {
		t.Fatalf("UpsertAutoscaler(dryRun) error = %v", err)
	}
	if created.Name != "api" || created.TargetName != "api" || len(created.Metrics) != 2 {
		t.Fatalf("created = %#v", created)
	}
	actions := clientset.Actions()
	if len(actions) != 1 {
		t.Fatalf("actions = %#v", actions)
	}
	createAction, ok := actions[0].(k8stesting.CreateActionImpl)
	if !ok || len(createAction.GetCreateOptions().DryRun) != 1 || createAction.GetCreateOptions().DryRun[0] != metav1.DryRunAll {
		t.Fatalf("dry-run action = %#v", actions[0])
	}
	if err := client.DeleteAutoscaler(context.Background(), "prod", "api", ""); err != nil {
		t.Fatalf("DeleteAutoscaler(after fake dry-run) error = %v", err)
	}
	if _, err := client.UpsertAutoscaler(context.Background(), domain.AutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api",
		MinReplicas: 2, MaxReplicas: 8, CPUUtilization: &cpu,
	}, false); err != nil {
		t.Fatalf("UpsertAutoscaler(create) error = %v", err)
	}
	listed, err := client.ListAutoscalers(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListAutoscalers() error = %v", err)
	}
	if len(listed) != 1 || listed[0].MinReplicas != 2 || listed[0].MaxReplicas != 8 || len(listed[0].Metrics) != 1 || listed[0].Metrics[0].Target != "65%" {
		t.Fatalf("listed = %#v", listed)
	}
}

func TestAutoscalerUpdateRequiresCurrentResourceVersion(t *testing.T) {
	t.Parallel()
	item := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: "9"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"},
			MaxReplicas:    4,
		},
	}
	client, _ := newFakeKubeClient(item)
	cpu := int32(70)
	_, err := client.UpsertAutoscaler(context.Background(), domain.AutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api",
		MinReplicas: 1, MaxReplicas: 4, CPUUtilization: &cpu, ResourceVersion: "8",
	}, false)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("update error = %v, want ErrConflict", err)
	}
}

func TestDisruptionBudgetLifecycleAndStatus(t *testing.T) {
	t.Parallel()
	existing := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: "11"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: pointerIntOrString(intstr.FromString("50%")),
			Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
		},
		Status: policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 1, DesiredHealthy: 2, DisruptionsAllowed: 0, ExpectedPods: 2, ObservedGeneration: 4},
	}
	client, clientset := newFakeKubeClient(existing)
	listed, err := client.ListDisruptionBudgets(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListDisruptionBudgets() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Status != "Blocked" || listed[0].MinAvailable != "50%" || listed[0].Selector["app"] != "api" {
		t.Fatalf("listed = %#v", listed)
	}
	created, err := client.UpsertDisruptionBudget(context.Background(), domain.DisruptionBudgetSpec{
		Name: "worker", Namespace: "prod", MaxUnavailable: "1", Selector: map[string]string{"app": "worker"},
	}, true)
	if err != nil {
		t.Fatalf("UpsertDisruptionBudget(dryRun) error = %v", err)
	}
	if created.Name != "worker" || created.MaxUnavailable != "1" {
		t.Fatalf("created = %#v", created)
	}
	actions := clientset.Actions()
	last := actions[len(actions)-1]
	createAction, ok := last.(k8stesting.CreateActionImpl)
	if !ok || len(createAction.GetCreateOptions().DryRun) != 1 {
		t.Fatalf("last action = %#v", last)
	}
	if err := client.DeleteDisruptionBudget(context.Background(), "prod", "api", "11"); err != nil {
		t.Fatalf("DeleteDisruptionBudget() error = %v", err)
	}
}

func pointerIntOrString(value intstr.IntOrString) *intstr.IntOrString { return &value }

func TestAutoscalerSummaryPreservesMetricTargetsWithoutCurrentMetrics(t *testing.T) {
	t.Parallel()
	cpu := int32(80)
	item := autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"},
			MinReplicas:    int32Pointer(1),
			MaxReplicas:    6,
			Metrics: []autoscalingv2.MetricSpec{{
				Type:     autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &cpu}},
			}},
		},
	}
	summary := autoscalerSummary(item)
	if len(summary.Metrics) != 1 || summary.Metrics[0].Target != "80%" || summary.Metrics[0].TargetUtilization == nil || *summary.Metrics[0].TargetUtilization != 80 {
		t.Fatalf("summary metrics = %#v", summary.Metrics)
	}
}
