package kube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestMapKubeError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
	}{
		{name: "not found", err: apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "api"), target: domain.ErrNotFound},
		{name: "conflict", err: apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, "api", errors.New("stale")), target: domain.ErrConflict},
		{name: "already exists", err: apierrors.NewAlreadyExists(schema.GroupResource{Resource: "deployments"}, "api"), target: domain.ErrAlreadyExists},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "app", errors.New("denied")), target: domain.ErrForbidden},
		{name: "bad request", err: apierrors.NewBadRequest("invalid selector"), target: domain.ErrInvalid},
		{name: "unauthorized", err: apierrors.NewUnauthorized("expired"), target: domain.ErrForbidden},
		{name: "rate limited", err: apierrors.NewTooManyRequests("slow down", 1), target: domain.ErrRateLimited},
		{name: "service unavailable", err: apierrors.NewServiceUnavailable("api unavailable"), target: domain.ErrUnavailable},
		{name: "deadline", err: context.DeadlineExceeded, target: domain.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := mapKubeError(tt.err)
			if !errors.Is(mapped, tt.target) {
				t.Fatalf("mapKubeError() = %v, want %v", mapped, tt.target)
			}
		})
	}
	if mapped := mapKubeError(nil); mapped != nil {
		t.Fatalf("mapKubeError(nil) = %v", mapped)
	}
	if mapped := mapKubeError(context.Canceled); !errors.Is(mapped, context.Canceled) {
		t.Fatalf("canceled error = %v, want context.Canceled", mapped)
	}
	opaque := errors.New("transport failed")
	if mapped := mapKubeError(opaque); mapped != opaque {
		t.Fatalf("opaque error = %v, want original error", mapped)
	}
}

func TestNewFromRESTConfigAppliesTestAPIOverride(t *testing.T) {
	restConfig := &rest.Config{Host: "https://kubernetes.default.svc:443", Timeout: 15 * time.Second}
	cfg := testConfig()
	cfg.KubeAPIServer = "https://kube-fault-proxy.dosh-e2e.svc:443"
	cfg.InsecureSkipVerify = true
	prepared := prepareRESTConfig(restConfig, cfg)
	if prepared.Host != cfg.KubeAPIServer {
		t.Fatalf("host = %q, want %q", prepared.Host, cfg.KubeAPIServer)
	}
	if !prepared.TLSClientConfig.Insecure {
		t.Fatal("TLSClientConfig.Insecure was not enabled")
	}
	if prepared.TLSClientConfig.CAFile != "" || len(prepared.TLSClientConfig.CAData) != 0 {
		t.Fatal("CA material was not cleared for the explicit test endpoint")
	}
	if prepared.Timeout != 0 {
		t.Fatalf("global REST timeout = %s, want 0 so watches are not terminated", prepared.Timeout)
	}
	if restConfig.Timeout != 15*time.Second {
		t.Fatal("prepareRESTConfig mutated the caller's rest config")
	}
}

func testConfig() config.Config {
	return config.Config{RequestTimeout: 2 * time.Second, ClusterName: "test"}
}

func TestClusterContextVersionAndCNI(t *testing.T) {
	discovery := &fakediscovery.FakeDiscovery{
		Fake:               &k8stesting.Fake{},
		FakedServerVersion: &version.Info{GitVersion: "v1.36.5"},
	}
	discovery.Resources = []*metav1.APIResourceList{
		{GroupVersion: "cilium.io/v2", APIResources: []metav1.APIResource{{Name: "ciliumnetworkpolicies"}}},
		{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods"}}},
	}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery, config.Config{RequestTimeout: 2 * time.Second, ClusterName: "kubedo-lab"})
	if got := client.ClusterName(); got != "kubedo-lab" {
		t.Fatalf("ClusterName() = %q", got)
	}
	if got, err := client.Version(context.Background()); err != nil || got != "v1.36.5" {
		t.Fatalf("Version() = (%q, %v)", got, err)
	}
	if got := client.DetectCNI(context.Background()); got != "Cilium" {
		t.Fatalf("DetectCNI() = %q, want Cilium", got)
	}

	unknown := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}, FakedServerVersion: &version.Info{GitVersion: "v1.36.5"}}
	if got := NewForClients(k8sfake.NewSimpleClientset(), nil, unknown, testConfig()).DetectCNI(context.Background()); got != "Unknown" {
		t.Fatalf("DetectCNI() without Cilium = %q", got)
	}
	discovery.PrependReactor("get", "version", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("discovery unavailable")
	})
	if _, err := client.Version(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Version() error = %v, want ErrUnavailable", err)
	}
}

func TestDiscoveryRequestsUseCallerContext(t *testing.T) {
	restClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(func(request *http.Request) (*http.Response, error) {
			return nil, request.Context().Err()
		}),
	}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery.NewDiscoveryClient(restClient), testConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := []struct {
		name string
		call func() error
	}{
		{name: "version", call: func() error { _, err := client.serverVersion(ctx); return err }},
		{name: "groups", call: func() error { _, err := client.serverGroups(ctx); return err }},
		{name: "metrics resources", call: func() error {
			_, err := client.serverResourcesForGroupVersion(ctx, "metrics.k8s.io/v1beta1")
			return err
		}},
	}
	for _, tt := range calls {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestServerResourcesForCoreGroupUsesCoreAPIPath(t *testing.T) {
	var requestedPath string
	restClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(func(request *http.Request) (*http.Response, error) {
			requestedPath = request.URL.Path
			body := `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[{"name":"pods","singularName":"pod","namespaced":true,"kind":"Pod","verbs":["get","list"]}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		}),
	}
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, discovery.NewDiscoveryClient(restClient), testConfig())
	resources, err := client.serverResourcesForGroupVersion(context.Background(), "v1")
	if err != nil {
		t.Fatalf("serverResourcesForGroupVersion() error = %v", err)
	}
	if requestedPath != "/api/v1" {
		t.Fatalf("requested path = %q, want /api/v1", requestedPath)
	}
	if len(resources.APIResources) != 1 || resources.APIResources[0].SingularName != "pod" {
		t.Fatalf("resources = %#v", resources)
	}
}
