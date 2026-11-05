package kube

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"dosh/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestImpersonationTransportAddsUserAndGroups(t *testing.T) {
	t.Parallel()
	var captured *http.Request
	transport := &impersonationTransport{base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(http.NoBody), Header: make(http.Header)}, nil
	})}
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "alice@example.test", Groups: []string{"viewers", "dosh-admins"}})
	request := httptest.NewRequest(http.MethodGet, "https://kubernetes.example.test/api/v1/pods", nil).WithContext(ctx)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if got := captured.Header.Get("Impersonate-User"); got != "alice@example.test" {
		t.Fatalf("Impersonate-User = %q", got)
	}
	if got := captured.Header.Values("Impersonate-Group"); len(got) != 2 || got[0] != "viewers" || got[1] != "dosh-admins" {
		t.Fatalf("Impersonate-Group = %#v", got)
	}
	if captured == request {
		t.Fatal("request was mutated instead of cloned")
	}
}

func TestImpersonationTransportUsesServiceAccountWithoutActor(t *testing.T) {
	t.Parallel()
	var captured *http.Request
	transport := &impersonationTransport{base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(http.NoBody), Header: make(http.Header)}, nil
	})}
	request := httptest.NewRequest(http.MethodGet, "https://kubernetes.example.test/version", nil)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if got := captured.Header.Get("Impersonate-User"); got != "" {
		t.Fatalf("unexpected Impersonate-User = %q", got)
	}
}

func TestImpersonationTransportIgnoresActorAfterWithoutActor(t *testing.T) {
	t.Parallel()
	var captured *http.Request
	transport := &impersonationTransport{base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(http.NoBody), Header: make(http.Header)}, nil
	})}
	ctx := domain.WithoutActor(domain.WithActor(context.Background(), domain.Actor{Username: "alice@example.test"}))
	request := httptest.NewRequest(http.MethodPost, "https://kubernetes.example.test/apis/authorization.k8s.io/v1/subjectaccessreviews", nil).WithContext(ctx)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if got := captured.Header.Get("Impersonate-User"); got != "" {
		t.Fatalf("unexpected Impersonate-User = %q", got)
	}
	if got := captured.Header.Values("Impersonate-Group"); len(got) != 0 {
		t.Fatalf("unexpected Impersonate-Group = %#v", got)
	}
}
