package kube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"dosh/internal/domain"

	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/scheme"
	restfake "k8s.io/client-go/rest/fake"
	k8stesting "k8s.io/client-go/testing"
)

const widgetOpenAPIDocument = `{
  "swagger": "2.0",
  "definitions": {
    "example.io.v1.Widget": {
      "type": "object",
      "required": ["spec"],
      "properties": {
        "apiVersion": {"type": "string"},
        "kind": {"type": "string"},
        "metadata": {"$ref": "#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"},
        "spec": {"$ref": "#/definitions/example.io.v1.WidgetSpec"}
      }
    },
    "example.io.v1.WidgetSpec": {
      "type": "object",
      "required": ["size"],
      "properties": {"size": {"type": "string", "enum": ["small", "large"]}}
    },
    "io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": {
      "type": "object",
      "properties": {"name": {"type": "string"}, "namespace": {"type": "string"}}
    },
    "unrelated.io.v1.Noise": {"type": "object"}
  }
}`

func TestGetResourceSchemaMatchesCRDAndCachesDocument(t *testing.T) {
	calls := 0
	httpClient := restfake.CreateHTTPClient(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path != "/openapi/v2" {
			t.Fatalf("request path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		return schemaHTTPResponse(widgetOpenAPIDocument), nil
	})
	restClient := &restfake.RESTClient{NegotiatedSerializer: scheme.Codecs.WithoutConversion(), Client: httpClient}
	discovery := discoveryWithREST{FakeDiscovery: &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}, restClient: restClient}
	client := NewForClients(nil, nil, discovery, testConfig())
	descriptor := domain.ResourceDescriptor{Group: "example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true}

	schema, err := client.GetResourceSchema(context.Background(), descriptor)
	if err != nil {
		t.Fatalf("GetResourceSchema() error = %v", err)
	}
	if schema.Source != "openapi-v2" || schema.Kind != "Widget" || len(schema.Schema) == 0 {
		t.Fatalf("schema = %#v", schema)
	}
	if _, ok := schema.Definitions["example.io.v1.WidgetSpec"]; !ok {
		t.Fatalf("referenced WidgetSpec definition missing: %#v", schema.Definitions)
	}
	if _, ok := schema.Definitions["io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"]; !ok {
		t.Fatalf("referenced ObjectMeta definition missing: %#v", schema.Definitions)
	}
	if _, ok := schema.Definitions["unrelated.io.v1.Noise"]; ok {
		t.Fatalf("unreferenced definition leaked into response: %#v", schema.Definitions)
	}
	if _, err := client.GetResourceSchema(context.Background(), descriptor); err != nil {
		t.Fatalf("cached GetResourceSchema() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("OpenAPI requests = %d, want 1", calls)
	}
}

func TestGetResourceSchemaReturnsUnavailableForUnknownDefinition(t *testing.T) {
	httpClient := restfake.CreateHTTPClient(func(*http.Request) (*http.Response, error) {
		return schemaHTTPResponse(widgetOpenAPIDocument), nil
	})
	restClient := &restfake.RESTClient{NegotiatedSerializer: scheme.Codecs.WithoutConversion(), Client: httpClient}
	discovery := discoveryWithREST{FakeDiscovery: &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}, restClient: restClient}
	client := NewForClients(nil, nil, discovery, testConfig())

	schema, err := client.GetResourceSchema(context.Background(), domain.ResourceDescriptor{Group: "example.io", Version: "v1", Resource: "gadgets", Kind: "Gadget"})
	if err != nil {
		t.Fatalf("GetResourceSchema() error = %v", err)
	}
	if schema.Source != "unavailable" || len(schema.Warnings) == 0 {
		t.Fatalf("schema = %#v", schema)
	}
}

func TestGetResourceSchemaDegradesOnTransportAndDecodeErrors(t *testing.T) {
	transport := discoveryWithREST{
		FakeDiscovery: &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}},
		restClient:    &restfake.RESTClient{Err: errors.New("api unavailable")},
	}
	client := NewForClients(nil, nil, transport, testConfig())
	if _, err := client.GetResourceSchema(context.Background(), domain.ResourceDescriptor{Version: "v1", Resource: "pods", Kind: "Pod"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("transport error = %v, want ErrUnavailable", err)
	}

	httpClient := restfake.CreateHTTPClient(func(*http.Request) (*http.Response, error) {
		return schemaHTTPResponse("{not-json"), nil
	})
	malformed := discoveryWithREST{
		FakeDiscovery: &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}},
		restClient:    &restfake.RESTClient{NegotiatedSerializer: scheme.Codecs.WithoutConversion(), Client: httpClient},
	}
	client = NewForClients(nil, nil, malformed, testConfig())
	if _, err := client.GetResourceSchema(context.Background(), domain.ResourceDescriptor{Version: "v1", Resource: "pods", Kind: "Pod"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("decode error = %v, want ErrUnavailable", err)
	}
}

func TestMatchResourceSchemaSupportsBuiltInAndCoreGroups(t *testing.T) {
	definitions := map[string]map[string]any{
		"io.k8s.api.apps.v1.Deployment": {"type": "object"},
		"io.k8s.api.rbac.v1.Role":       {"type": "object"},
		"io.k8s.api.core.v1.Pod":        {"type": "object"},
		"example.io.v1.Widget":          {"type": "object"},
	}
	tests := []struct {
		descriptor domain.ResourceDescriptor
		want       string
	}{
		{domain.ResourceDescriptor{Group: "apps", Version: "v1", Kind: "Deployment"}, "io.k8s.api.apps.v1.Deployment"},
		{domain.ResourceDescriptor{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}, "io.k8s.api.rbac.v1.Role"},
		{domain.ResourceDescriptor{Version: "v1", Kind: "Pod"}, "io.k8s.api.core.v1.Pod"},
		{domain.ResourceDescriptor{Group: "example.io", Version: "v1", Kind: "Widget"}, "example.io.v1.Widget"},
	}
	for _, test := range tests {
		key, _ := matchResourceSchema(definitions, test.descriptor)
		if key != test.want {
			t.Fatalf("matchResourceSchema(%#v) = %q, want %q", test.descriptor, key, test.want)
		}
	}
}

func schemaHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
