package kube

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/client-go/rest"
)

func TestReviewTokenRulesUsesTokenIdentityAndReturnsBoundedRules(t *testing.T) {
	t.Parallel()
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		authorization = r.Header.Get("Authorization")
		review := authv1.SelfSubjectRulesReview{Status: authv1.SubjectRulesReviewStatus{
			Incomplete: true,
			ResourceRules: []authv1.ResourceRule{{
				Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"},
			}},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(review)
	}))
	defer server.Close()

	client, err := NewFromRESTConfig(&rest.Config{Host: server.URL}, testConfig())
	if err != nil {
		t.Fatalf("NewFromRESTConfig() error = %v", err)
	}
	snapshot, err := client.ReviewTokenRules(context.Background(), " raw-token ", "prod")
	if err != nil {
		t.Fatalf("ReviewTokenRules() error = %v", err)
	}
	if authorization != "Bearer raw-token" {
		t.Fatalf("Authorization = %q, want token identity", authorization)
	}
	if snapshot.Namespace != "prod" || !snapshot.Incomplete || len(snapshot.Rules) != 1 || snapshot.Rules[0].Resources[0] != "pods" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestReviewTokenRulesRejectsMissingConfigurationAndNamespace(t *testing.T) {
	t.Parallel()
	client := NewForClients(nil, nil, nil, testConfig())
	if _, err := client.ReviewTokenRules(context.Background(), "token", "default"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("missing configuration error = %v, want unsupported", err)
	}
	client, err := NewFromRESTConfig(&rest.Config{Host: "https://kubernetes.example.test"}, testConfig())
	if err != nil {
		t.Fatalf("NewFromRESTConfig() error = %v", err)
	}
	if _, err := client.ReviewTokenRules(context.Background(), "token", "bad namespace"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid namespace error = %v, want invalid", err)
	}
	if _, err := client.ReviewTokenRules(context.Background(), "", "default"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("empty token error = %v, want unauthorized", err)
	}
}
