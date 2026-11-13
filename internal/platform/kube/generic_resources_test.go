package kube

import (
	"context"
	"testing"

	"dosh/internal/domain"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newGenericResourceClient(objects ...runtime.Object) *Client {
	widgetGVR := schema.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{widgetGVR: "WidgetList"},
		objects...,
	)
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "pods", SingularName: "pod", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}},
				{Name: "pods/log", SingularName: "", Kind: "Pod", Namespaced: true, Verbs: []string{"get"}},
				{Name: "bindings", SingularName: "binding", Kind: "Binding", Namespaced: true, Verbs: []string{"create"}},
				{Name: "nodes", SingularName: "node", Kind: "Node", Verbs: []string{"get", "list"}},
			},
		},
		{
			GroupVersion: "example.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "widgets", SingularName: "widget", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list", "watch"}, ShortNames: []string{"wdg"}, Categories: []string{"all"}},
			},
		},
		{
			GroupVersion: "batch.example.io/v1alpha1",
			APIResources: []metav1.APIResource{
				{Name: "gadgets", SingularName: "gadget", Kind: "Gadget", Namespaced: true, Verbs: []string{"get"}},
			},
		},
	}
	return NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, discovery, testConfig())
}

func TestDiscoverResourcesUsesPreferredVersionsAndFiltersUnreadableVerbs(t *testing.T) {
	client := newGenericResourceClient()

	items, warnings, err := client.DiscoverResources(context.Background())
	if err != nil {
		t.Fatalf("DiscoverResources() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("DiscoverResources() warnings = %#v", warnings)
	}
	if len(items) != 4 {
		t.Fatalf("DiscoverResources() items = %#v", items)
	}
	byResource := make(map[string]domain.ResourceDescriptor, len(items))
	for _, item := range items {
		byResource[item.Group+"/"+item.Resource] = item
	}
	pods, ok := byResource["/pods"]
	if !ok || pods.Kind != "Pod" || pods.Singular != "pod" || pods.Version != "v1" || !pods.Namespaced {
		t.Fatalf("pods descriptor = %#v", pods)
	}
	widgets, ok := byResource["example.io/widgets"]
	if !ok || widgets.Kind != "Widget" || widgets.Version != "v1" || len(widgets.ShortNames) != 1 || widgets.ShortNames[0] != "wdg" || len(widgets.Categories) != 1 {
		t.Fatalf("widgets descriptor = %#v", widgets)
	}
	if _, ok := byResource["/bindings"]; ok {
		t.Fatal("create-only binding resource was included")
	}
	if _, ok := byResource["/pods/log"]; ok {
		t.Fatal("subresource was included")
	}
}

func TestListAndGetGenericResources(t *testing.T) {
	client := newGenericResourceClient(
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.io/v1", "kind": "Widget",
			"metadata": map[string]any{"name": "api-widget", "namespace": "prod", "uid": "uid-1", "resourceVersion": "7", "creationTimestamp": "2026-11-01T00:00:00Z", "labels": map[string]any{"app": "api"}},
			"spec":     map[string]any{"size": "large"},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.io/v1", "kind": "Widget",
			"metadata": map[string]any{"name": "web-widget", "namespace": "dev"},
			"spec":     map[string]any{"size": "small"},
		}},
	)
	descriptor := domain.ResourceDescriptor{Group: "example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true}
	result, err := client.ListGenericResources(context.Background(), descriptor, domain.ListQuery{Namespace: "prod", Search: "api"}, domain.PageRequest{Size: 20})
	if err != nil {
		t.Fatalf("ListGenericResources() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("ListGenericResources() items = %#v", result.Items)
	}
	item := result.Items[0]
	if item.Name != "api-widget" || item.Namespace != "prod" || item.Kind != "Widget" || item.APIVersion != "example.io/v1" || item.UID != "uid-1" || item.ResourceVersion != "7" || item.Labels["app"] != "api" || item.CreatedAt.IsZero() {
		t.Fatalf("item = %#v", item)
	}

	object, err := client.GetGenericResource(context.Background(), descriptor, "prod", "api-widget")
	if err != nil {
		t.Fatalf("GetGenericResource() error = %v", err)
	}
	spec, ok := object["spec"].(map[string]any)
	if !ok || spec["size"] != "large" {
		t.Fatalf("object = %#v", object)
	}
}

func TestGenericResourceInterfaceRequiresDynamicClient(t *testing.T) {
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, testConfig())
	_, err := client.ListGenericResources(context.Background(), domain.ResourceDescriptor{Version: "v1", Resource: "pods"}, domain.ListQuery{}, domain.PageRequest{})
	if err == nil {
		t.Fatal("ListGenericResources() error = nil, want unavailable dynamic client")
	}
}
