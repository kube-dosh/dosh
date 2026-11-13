package kube

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	k8stesting "k8s.io/client-go/testing"
)

type discoveryWithREST struct {
	*fakediscovery.FakeDiscovery
	restClient rest.Interface
}

func (d discoveryWithREST) RESTClient() rest.Interface { return d.restClient }

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
	if len(result.Columns) != 3 || result.Columns[0].Name != "Name" {
		t.Fatalf("fallback columns = %#v", result.Columns)
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

func TestListGenericResourcesUsesTableAPI(t *testing.T) {
	body := `{
  "kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"resourceVersion":"9"},
  "columnDefinitions":[
    {"name":"Name","type":"string","format":"name"},
    {"name":"Status","type":"string"},
    {"name":"Age","type":"string"},
    {"name":"Wide","type":"string","priority":1}
  ],
  "rows":[{"cells":["api-0","Running","2d"]},{"cells":["api-1","Pending","3h"]}]
}`
	restClient := &restfake.RESTClient{
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		Resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}
	discovery := discoveryWithREST{FakeDiscovery: &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}, restClient: restClient}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery, testConfig())
	descriptor := domain.ResourceDescriptor{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}

	result, err := client.ListGenericResources(context.Background(), descriptor, domain.ListQuery{Namespace: "prod"}, domain.PageRequest{Size: 20})
	if err != nil {
		t.Fatalf("ListGenericResources() error = %v", err)
	}
	if restClient.Req == nil || restClient.Req.URL.Path != "/api/v1/namespaces/prod/pods" {
		t.Fatalf("table request path = %#v", restClient.Req)
	}
	if got := restClient.Req.Header.Get("Accept"); got != "application/json;as=Table;v=v1;g=meta.k8s.io" {
		t.Fatalf("Accept = %q", got)
	}
	if len(result.Columns) != 3 || result.Columns[2].Name != "Age" {
		t.Fatalf("columns = %#v", result.Columns)
	}
	if len(result.Items) != 2 || result.Items[0].Name != "api-0" || result.Items[0].Resource != "pods" || result.Items[0].Cells["Status"] != "Running" {
		t.Fatalf("items = %#v", result.Items)
	}
	if result.Items[0].Namespace != "prod" {
		t.Fatalf("namespace fallback = %q, want prod", result.Items[0].Namespace)
	}
}

func TestTableResourceSummaryReadsRowObjectMetadata(t *testing.T) {
	descriptor := domain.ResourceDescriptor{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}
	columns := []domain.ResourceColumn{{Name: "Name", Type: "string"}, {Name: "Status", Type: "string"}, {Name: "Age", Type: "string"}}
	row := metav1.TableRow{
		Cells: []any{"api-0", "Running", "2d"},
		Object: runtime.RawExtension{Raw: []byte(`{
			"apiVersion":"v1","kind":"Pod",
			"metadata":{"name":"api-0","namespace":"prod","uid":"uid-1","resourceVersion":"9","creationTimestamp":"2026-11-01T00:00:00Z","labels":{"app":"api"}}
		}`)},
	}

	summary := tableResourceSummary(descriptor, columns, row)
	if summary.Namespace != "prod" || summary.UID != "uid-1" || summary.ResourceVersion != "9" || summary.Labels["app"] != "api" || summary.CreatedAt.IsZero() {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestListResourceRulesUsesSelfSubjectRulesReviewAndCaches(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	calls := 0
	var namespaces []string
	clientset.PrependReactor("create", "selfsubjectrulesreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		review := action.(k8stesting.CreateAction).GetObject().(*authv1.SelfSubjectRulesReview)
		namespaces = append(namespaces, review.Spec.Namespace)
		review.Status.ResourceRules = []authv1.ResourceRule{{Verbs: []string{"list", "get"}, APIGroups: []string{""}, Resources: []string{"pods", "nodes"}}}
		return true, review, nil
	})
	client := NewForClients(clientset, nil, nil, testConfig())
	actor := domain.Actor{Username: "alice", Groups: []string{"viewers"}}

	rules, warnings, err := client.ListResourceRules(context.Background(), actor, "prod")
	if err != nil {
		t.Fatalf("ListResourceRules() error = %v", err)
	}
	if len(warnings) != 0 || len(rules.Namespaced) != 1 || len(rules.Cluster) != 1 {
		t.Fatalf("rules = %#v warnings = %#v", rules, warnings)
	}
	if calls != 2 {
		t.Fatalf("SelfSubjectRulesReview calls = %d, want 2", calls)
	}
	if len(namespaces) != 2 || namespaces[0] != "prod" || namespaces[1] != "" {
		t.Fatalf("review namespaces = %#v", namespaces)
	}
	if _, _, err := client.ListResourceRules(context.Background(), actor, "prod"); err != nil {
		t.Fatalf("cached ListResourceRules() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("cache miss: SelfSubjectRulesReview calls = %d", calls)
	}
}

func TestGenericResourceInterfaceRequiresDynamicClient(t *testing.T) {
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, testConfig())
	_, err := client.ListGenericResources(context.Background(), domain.ResourceDescriptor{Version: "v1", Resource: "pods"}, domain.ListQuery{}, domain.PageRequest{})
	if err == nil {
		t.Fatal("ListGenericResources() error = nil, want unavailable dynamic client")
	}
}
