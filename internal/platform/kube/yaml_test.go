package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newFakeYAMLClient(objects ...runtime.Object) (*Client, *dynamicfake.FakeDynamicClient) {
	listKinds := map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Version: "v1", Resource: "secrets"}:                    "SecretList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
	return NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, nil, testConfig()), dynamicClient
}

func unstructuredConfigMap() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{
			"name": "app", "namespace": "prod", "managedFields": []any{map[string]any{"manager": "someone"}},
		},
		"data": map[string]any{"mode": "debug"},
	}}
}

func TestGetYAMLStripsManagedFieldsAndReturnsSecretData(t *testing.T) {
	secret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "app", "namespace": "prod", "managedFields": []any{map[string]any{"manager": "someone"}}},
		"data":     map[string]any{"token": "c2Vuc2l0aXZl"},
	}}
	client, _ := newFakeYAMLClient(unstructuredConfigMap(), secret)
	configMap, err := client.GetYAML(context.Background(), domain.ResourceConfigMap, "prod", "app")
	if err != nil {
		t.Fatalf("GetYAML(configmap) error = %v", err)
	}
	if strings.Contains(configMap.YAML, "managedFields") || !strings.Contains(configMap.YAML, "mode: debug") {
		t.Fatalf("configmap yaml = %s", configMap.YAML)
	}
	secretYAML, err := client.GetYAML(context.Background(), domain.ResourceSecret, "prod", "app")
	if err != nil {
		t.Fatalf("GetYAML(secret) error = %v", err)
	}
	if !strings.Contains(secretYAML.YAML, "c2Vuc2l0aXZl") || strings.Contains(secretYAML.YAML, "<redacted>") {
		t.Fatalf("secret data wasn't returned: %s", secretYAML.YAML)
	}
}

func TestGetYAMLRejectsUnsupportedAndMissingNamespace(t *testing.T) {
	client, _ := newFakeYAMLClient()
	_, err := client.GetYAML(context.Background(), domain.ResourceType("unknown"), "prod", "app")
	if !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("unsupported error = %v, want ErrUnsupported", err)
	}
	_, err = client.GetYAML(context.Background(), domain.ResourceConfigMap, "", "app")
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing namespace error = %v, want ErrInvalid", err)
	}
}

func TestExistsDistinguishesPresentMissingAndInvalidTargets(t *testing.T) {
	storageClass := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "storage.k8s.io/v1", "kind": "StorageClass",
		"metadata": map[string]any{"name": "fast"},
	}}
	client, _ := newFakeYAMLClient(unstructuredConfigMap(), storageClass)
	exists, err := client.Exists(context.Background(), domain.ResourceConfigMap, "prod", "app")
	if err != nil || !exists {
		t.Fatalf("Exists(present) = (%v, %v), want (true, nil)", exists, err)
	}
	exists, err = client.Exists(context.Background(), domain.ResourceConfigMap, "prod", "missing")
	if err != nil || exists {
		t.Fatalf("Exists(missing) = (%v, %v), want (false, nil)", exists, err)
	}
	if _, err := client.Exists(context.Background(), domain.ResourceType("unknown"), "prod", "app"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Exists(unsupported) error = %v, want ErrUnsupported", err)
	}
	if _, err := client.Exists(context.Background(), domain.ResourceConfigMap, "", "app"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Exists(missing namespace) error = %v, want ErrInvalid", err)
	}
	exists, err = client.Exists(context.Background(), domain.ResourceStorageClass, "", "fast")
	if err != nil || !exists {
		t.Fatalf("Exists(cluster-scoped) = (%v, %v), want (true, nil)", exists, err)
	}
}

func TestApplyYAMLValidation(t *testing.T) {
	client, _ := newFakeYAMLClient()
	tests := []struct {
		name    string
		request domain.ApplyRequest
		target  error
	}{
		{name: "empty", request: domain.ApplyRequest{}, target: domain.ErrInvalid},
		{name: "malformed", request: domain.ApplyRequest{YAML: "metadata: ["}, target: domain.ErrInvalid},
		{name: "unsupported", request: domain.ApplyRequest{YAML: "apiVersion: apps/v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n"}, target: domain.ErrUnsupported},
		{name: "missing name", request: domain.ApplyRequest{YAML: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  namespace: prod\n"}, target: domain.ErrInvalid},
		{name: "missing namespace", request: domain.ApplyRequest{YAML: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n"}, target: domain.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ApplyYAML(context.Background(), tt.request)
			if !errors.Is(err, tt.target) {
				t.Fatalf("ApplyYAML() error = %v, want %v", err, tt.target)
			}
		})
	}
}

func TestApplyYAMLPreservesResourceVersionAndReturnsSecretData(t *testing.T) {
	client, dynamicClient := newFakeYAMLClient()
	var captured *unstructured.Unstructured
	var dryRun bool
	dynamicClient.PrependReactor("patch", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		switch patchAction := action.(type) {
		case k8stesting.PatchActionImpl:
			dryRun = len(patchAction.GetPatchOptions().DryRun) > 0
		case *k8stesting.PatchActionImpl:
			dryRun = len(patchAction.GetPatchOptions().DryRun) > 0
		default:
			t.Fatalf("unexpected patch action type %T", action)
		}
		captured = &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "Secret",
			"metadata": map[string]any{"name": "app", "namespace": "prod", "resourceVersion": "9"},
			"data":     map[string]any{"token": "c2Vuc2l0aXZl"},
		}}
		return true, captured.DeepCopy(), nil
	})
	body := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app\n  namespace: prod\ndata:\n  token: c2Vuc2l0aXZl\n"
	result, err := client.ApplyYAML(context.Background(), domain.ApplyRequest{YAML: body, ResourceVersion: "9", DryRun: true})
	if err != nil {
		t.Fatalf("ApplyYAML() error = %v", err)
	}
	if !dryRun || captured == nil {
		t.Fatalf("dryRun = %v captured = %#v", dryRun, captured)
	}
	if !strings.Contains(result.YAML, "c2Vuc2l0aXZl") || strings.Contains(result.YAML, "<redacted>") {
		t.Fatalf("applied secret data wasn't returned: %s", result.YAML)
	}
}

func TestGVRForKnownAndUnknownResources(t *testing.T) {
	gvr, namespaced, ok := gvrFor(domain.ResourceDeployment)
	if !ok || !namespaced || gvr.Resource != "deployments" || gvr.Group != "apps" {
		t.Fatalf("deployment gvr = %#v namespaced=%v ok=%v", gvr, namespaced, ok)
	}
	if _, _, ok := gvrFor(domain.ResourceType("unknown")); ok {
		t.Fatal("unknown resource was accepted")
	}
}

func TestGetYAMLMissingResourceMapsNotFound(t *testing.T) {
	client, _ := newFakeYAMLClient()
	_, err := client.GetYAML(context.Background(), domain.ResourceConfigMap, "prod", "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetYAML() error = %v, want ErrNotFound", err)
	}
}
