package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/releaseutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestRemoveHelmNotes(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"valkey/templates/NOTES.txt":       "NOTES:\nGet the password with:\n  export PASSWORD=$(kubectl get secret ...)\n",
		"NOTES.txt":                        "root notes",
		"valkey/templates/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\n",
	}
	removeHelmNotes(files)
	if _, ok := files["valkey/templates/NOTES.txt"]; ok {
		t.Fatal("namespaced NOTES.txt was not removed")
	}
	if _, ok := files["NOTES.txt"]; ok {
		t.Fatal("root NOTES.txt was not removed")
	}
	if files["valkey/templates/deployment.yaml"] == "" {
		t.Fatal("regular rendered template was removed")
	}
}

func TestRenderHelmChartProcessesDependencyConditions(t *testing.T) {
	t.Parallel()
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}, FakedServerVersion: &version.Info{GitVersion: "v1.36.5"}}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery, testConfig())

	disabled := dependencyChart()
	_, manifest, _, _, _, err := client.renderHelmChart(context.Background(), domain.Actor{}, disabled, "apps", "web", 1, map[string]any{
		"child": map[string]any{"enabled": false},
	})
	if err != nil {
		t.Fatalf("render disabled dependency: %v", err)
	}
	if strings.Contains(manifest, "name: child-config") {
		t.Fatalf("disabled dependency was rendered:\n%s", manifest)
	}
	if !strings.Contains(manifest, "name: parent-config") {
		t.Fatalf("parent resource missing from render:\n%s", manifest)
	}

	enabled := dependencyChart()
	_, manifest, _, _, _, err = client.renderHelmChart(context.Background(), domain.Actor{}, enabled, "apps", "web", 1, map[string]any{
		"child": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("render enabled dependency: %v", err)
	}
	if !strings.Contains(manifest, "name: child-config") {
		t.Fatalf("enabled dependency was not rendered:\n%s", manifest)
	}
}

func TestRenderHelmChartLookupUsesCurrentActorIdentity(t *testing.T) {
	t.Parallel()
	var seenImpersonation atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Impersonate-User") == "alice" {
			seenImpersonation.Store(true)
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1":
			_ = json.NewEncoder(response).Encode(metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: []string{"get", "list"}}},
			})
		case "/api/v1/namespaces/apps/configmaps/existing":
			_ = json.NewEncoder(response).Encode(map[string]any{
				"apiVersion": "v1", "kind": "ConfigMap",
				"metadata": map[string]any{"name": "existing", "namespace": "apps"},
				"data":     map[string]any{"value": "found"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}, FakedServerVersion: &version.Info{GitVersion: "v1.36.5"}}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery, testConfig())
	client.restConfig = &rest.Config{Host: server.URL}
	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "lookup", Version: "1.0.0"},
		Templates: []*chart.File{{Name: "templates/configmap.yaml", Data: []byte(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: lookup-result\ndata:\n  value: {{ (lookup \"v1\" \"ConfigMap\" \"apps\" \"existing\").data.value | default \"missing\" | quote }}\n",
		)}},
	}
	_, manifest, _, _, _, err := client.renderHelmChart(context.Background(), domain.Actor{Username: "alice", Groups: []string{"developers"}}, ch, "apps", "web", 1, nil)
	if err != nil {
		t.Fatalf("render lookup chart: %v", err)
	}
	if !strings.Contains(manifest, `value: "found"`) {
		t.Fatalf("lookup result missing from manifest:\n%s", manifest)
	}
	if !seenImpersonation.Load() {
		t.Fatal("lookup requests did not carry the current actor identity")
	}
}

func TestRenderHelmChartUsesDiscoveryAPIVersions(t *testing.T) {
	t.Parallel()
	discovery := &fakediscovery.FakeDiscovery{
		Fake:               &k8stesting.Fake{},
		FakedServerVersion: &version.Info{GitVersion: "v1.36.5"},
	}
	discovery.Resources = []*metav1.APIResourceList{{
		GroupVersion: "example.io/v1",
		APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}},
	}}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery, testConfig())
	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "capabilities", Version: "1.0.0"},
		Templates: []*chart.File{{Name: "templates/configmap.yaml", Data: []byte(
			"{{- if .Capabilities.APIVersions.Has \"example.io/v1/Widget\" }}\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: discovered-api\n{{- else }}\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: missing-api\n{{- end }}\n",
		)}},
	}
	_, manifest, _, _, _, err := client.renderHelmChart(context.Background(), domain.Actor{}, ch, "apps", "web", 1, nil)
	if err != nil {
		t.Fatalf("render chart: %v", err)
	}
	if !strings.Contains(manifest, "name: discovered-api") {
		t.Fatalf("discovery API version was not exposed to the chart:\n%s", manifest)
	}
}

func TestPreflightHelmPlanObjectsUsesServerDryRun(t *testing.T) {
	t.Parallel()
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "ConfigMapList"})
	var seenDryRun atomic.Bool
	dynamicClient.PrependReactor("patch", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch, ok := action.(interface {
			GetPatchType() types.PatchType
			GetPatchOptions() metav1.PatchOptions
		})
		if !ok || patch.GetPatchType() != types.ApplyPatchType {
			t.Fatalf("unexpected patch action: %#v", action)
		}
		options := patch.GetPatchOptions()
		if len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
			t.Fatalf("dry-run options = %#v", options.DryRun)
		}
		seenDryRun.Store(true)
		return true, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": "demo", "namespace": "apps"},
		}}, nil
	})
	client := NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, nil, testConfig())
	descriptor := domain.ResourceDescriptor{Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true}
	byGVK := map[string]domain.ResourceDescriptor{"v1/ConfigMap": descriptor}
	warnings, err := client.preflightHelmPlanObjects(context.Background(), "apps", []releaseutil.Manifest{{
		Name: "templates/configmap.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	}}, nil, byGVK, byGVK, nil)
	if err != nil {
		t.Fatalf("preflightHelmPlanObjects() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("preflightHelmPlanObjects() warnings = %#v", warnings)
	}
	if !seenDryRun.Load() {
		t.Fatal("server-side dry-run was not requested")
	}
}

func dependencyChart() *chart.Chart {
	child := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "child", Version: "1.0.0"},
		Templates: []*chart.File{{Name: "templates/configmap.yaml", Data: []byte(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: child-config\n",
		)}},
	}
	condition := "child.enabled"
	parent := &chart.Chart{
		Metadata: &chart.Metadata{
			APIVersion: "v2", Name: "parent", Version: "1.0.0",
			Dependencies: []*chart.Dependency{{Name: "child", Version: "1.0.0", Condition: condition}},
		},
		Values: map[string]any{"child": map[string]any{"enabled": true}},
		Templates: []*chart.File{{Name: "templates/configmap.yaml", Data: []byte(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: parent-config\n",
		)}},
	}
	parent.SetDependencies(child)
	return parent
}
