package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newFakeVPAClient(objects ...runtime.Object) (*Client, *dynamicfake.FakeDynamicClient) {
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{verticalPodAutoscalerGVR: "VerticalPodAutoscalerList"},
		objects...,
	)
	return NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, nil, testConfig()), dynamicClient
}

func fakeVPAObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.k8s.io/v1", "kind": "VerticalPodAutoscaler",
		"metadata": map[string]any{"name": "api", "namespace": "prod", "resourceVersion": "9"},
		"spec": map[string]any{
			"targetRef":    map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "api"},
			"updatePolicy": map[string]any{"updateMode": "Auto"},
			"minReplicas":  int64(2),
			"resourcePolicy": map[string]any{"containerPolicies": []any{map[string]any{
				"containerName": "api", "mode": "Auto", "minAllowed": map[string]any{"cpu": "100m"},
				"maxAllowed": map[string]any{"cpu": "2"}, "controlledValues": "RequestsAndLimits",
				"controlledResources": []any{"cpu", "memory"},
			}}},
		},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "RecommendationProvided", "status": "True", "lastTransitionTime": "2026-01-02T03:04:05Z"}},
			"recommendation": map[string]any{"containerRecommendations": []any{map[string]any{
				"containerName": "api", "lowerBound": map[string]any{"cpu": "120m", "memory": "128Mi"},
				"target": map[string]any{"cpu": "180m", "memory": "192Mi"}, "upperBound": map[string]any{"cpu": "300m", "memory": "256Mi"},
				"uncappedTarget": map[string]any{"cpu": "190m", "memory": "200Mi"},
			}}},
		},
	}}
}

func TestVerticalAutoscalerLifecycleAndRecommendationStatus(t *testing.T) {
	client, dynamicClient := newFakeVPAClient(fakeVPAObject())
	listed, err := client.ListVerticalAutoscalers(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListVerticalAutoscalers() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Status != "Ready" || listed[0].TargetKind != "Deployment" || listed[0].MinReplicas == nil || *listed[0].MinReplicas != 2 {
		t.Fatalf("listed = %#v", listed)
	}
	if len(listed[0].Recommendation) != 1 || listed[0].Recommendation[0].Target["cpu"] != "180m" || listed[0].RecommendationAge == nil {
		t.Fatalf("recommendation = %#v", listed[0].Recommendation)
	}
	if len(listed[0].ContainerPolicies) != 1 || listed[0].ContainerPolicies[0].MaxAllowed["cpu"] != "2" || len(listed[0].ContainerPolicies[0].ControlledResources) != 2 {
		t.Fatalf("container policies = %#v", listed[0].ContainerPolicies)
	}

	created, err := client.UpsertVerticalAutoscaler(context.Background(), domain.VerticalAutoscalerSpec{
		Name: "worker", Namespace: "prod", TargetKind: "Deployment", TargetName: "worker", UpdatePolicy: "Off",
	}, true)
	if err != nil || created.Name != "worker" {
		t.Fatalf("dry-run create = %#v, %v", created, err)
	}
	actions := dynamicClient.Actions()
	last := actions[len(actions)-1]
	createAction, ok := last.(k8stesting.CreateActionImpl)
	if !ok || len(createAction.GetCreateOptions().DryRun) != 1 || createAction.GetCreateOptions().DryRun[0] != metav1.DryRunAll {
		t.Fatalf("dry-run create action = %#v", last)
	}

	if _, err := client.UpsertVerticalAutoscaler(context.Background(), domain.VerticalAutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", UpdatePolicy: "Auto", ResourceVersion: "8",
	}, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}
	updated, err := client.UpsertVerticalAutoscaler(context.Background(), domain.VerticalAutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", UpdatePolicy: "Initial", ResourceVersion: "9",
		ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "api", Mode: "Off"}},
	}, false)
	if err != nil || updated.UpdatePolicy != "Initial" || len(updated.ContainerPolicies) != 1 || updated.ContainerPolicies[0].Mode != "Off" {
		t.Fatalf("updated = %#v, %v", updated, err)
	}
	if err := client.DeleteVerticalAutoscaler(context.Background(), "prod", "api", "9"); err != nil {
		t.Fatalf("DeleteVerticalAutoscaler() error = %v", err)
	}
	deleteAction, ok := dynamicClient.Actions()[len(dynamicClient.Actions())-1].(k8stesting.DeleteActionImpl)
	if !ok || deleteAction.GetDeleteOptions().Preconditions == nil || deleteAction.GetDeleteOptions().Preconditions.ResourceVersion == nil || *deleteAction.GetDeleteOptions().Preconditions.ResourceVersion != "9" {
		t.Fatalf("delete action = %#v", dynamicClient.Actions()[len(dynamicClient.Actions())-1])
	}
}

func TestVerticalAutoscalerCRDNotFoundMapsToUnsupported(t *testing.T) {
	client, dynamicClient := newFakeVPAClient()
	dynamicClient.PrependReactor("list", "verticalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "autoscaling.k8s.io", Resource: "verticalpodautoscalers"}, "")
	})
	dynamicClient.PrependReactor("create", "verticalpodautoscalers", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "autoscaling.k8s.io", Resource: "verticalpodautoscalers"}, "")
	})
	if _, err := client.ListVerticalAutoscalers(context.Background(), "prod"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("ListVerticalAutoscalers() error = %v, want unsupported", err)
	}
	if _, err := client.UpsertVerticalAutoscaler(context.Background(), domain.VerticalAutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", UpdatePolicy: "Off",
	}, false); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("UpsertVerticalAutoscaler() error = %v, want unsupported", err)
	}
}

func TestVerticalAutoscalerRequiresDynamicClient(t *testing.T) {
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, testConfig())
	if _, err := client.ListVerticalAutoscalers(context.Background(), "prod"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("ListVerticalAutoscalers() error = %v, want unavailable", err)
	}
}

func TestVerticalAutoscalerWithoutRecommendationStaysPending(t *testing.T) {
	t.Parallel()
	item := fakeVPAObject().DeepCopy()
	item.Object["status"] = map[string]any{
		"conditions": []any{map[string]any{"type": "FetchingHistory", "status": "False", "reason": "Loading"}},
	}
	summary := verticalAutoscalerSummary(item)
	if summary.Status != "Pending" || summary.Message == "" || summary.RecommendationAge != nil || len(summary.Recommendation) != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestValidateVPAQuantitiesUsesKubernetesGrammarAndBounds(t *testing.T) {
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, testConfig())
	valid := domain.VerticalAutoscalerSpec{ContainerPolicies: []domain.VPAContainerPolicy{{
		ContainerName: "api", MinAllowed: map[string]string{"cpu": "100m", "memory": "1.5Gi"}, MaxAllowed: map[string]string{"cpu": "2", "memory": "4Gi"},
	}}}
	if err := client.ValidateVPAQuantities(valid); err != nil {
		t.Fatalf("ValidateVPAQuantities(valid) error = %v", err)
	}
	for name, spec := range map[string]domain.VerticalAutoscalerSpec{
		"invalid quantity":      {ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "api", MinAllowed: map[string]string{"memory": "nope"}}}},
		"negative quantity":     {ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "api", MaxAllowed: map[string]string{"cpu": "-1"}}}},
		"minimum above maximum": {ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "api", MinAllowed: map[string]string{"cpu": "2"}, MaxAllowed: map[string]string{"cpu": "1"}}}},
	} {
		if err := client.ValidateVPAQuantities(spec); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%s error = %v, want invalid", name, err)
		}
	}
}
