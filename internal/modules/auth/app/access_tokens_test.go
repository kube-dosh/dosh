package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

type accessReviewerFunc func(context.Context, domain.Actor, domain.Access) (bool, error)

func (f accessReviewerFunc) ReviewAccess(ctx context.Context, actor domain.Actor, access domain.Access) (bool, error) {
	return f(ctx, actor, access)
}

type accessTokenIssuerStub struct {
	request          domain.AccessTokenIssueRequest
	result           domain.IssuedAccessToken
	err              error
	cleanupNamespace string
	cleanupDeleted   int
	cleanupErr       error
	called           bool
}

func (s *accessTokenIssuerStub) IssueAccessToken(_ context.Context, request domain.AccessTokenIssueRequest) (domain.IssuedAccessToken, error) {
	s.called = true
	s.request = request
	return s.result, s.err
}

func (s *accessTokenIssuerStub) CleanupExpiredAccessGrants(_ context.Context, namespace string) (int, error) {
	s.called = true
	s.cleanupNamespace = namespace
	return s.cleanupDeleted, s.cleanupErr
}

func TestAccessTokenServiceReviewTokenUsesTrimmedTokenAndActor(t *testing.T) {
	t.Parallel()
	var receivedToken string
	var receivedActor domain.Actor
	var receivedAccess domain.Access
	service := NewAccessTokenService(
		tokenAuthenticatorFunc(func(_ context.Context, token string) (domain.Actor, error) {
			receivedToken = token
			return domain.Actor{Username: "alice@example.test", Groups: []string{"viewers"}}, nil
		}),
		accessReviewerFunc(func(_ context.Context, actor domain.Actor, access domain.Access) (bool, error) {
			receivedActor = actor
			receivedAccess = access
			return true, nil
		}),
		nil, false,
	)
	access := domain.Access{Verb: "get", Resource: "pods", Namespace: "prod", Name: "api-0"}
	result, err := service.ReviewToken(context.Background(), "  token-value  ", access)
	if err != nil {
		t.Fatalf("ReviewToken() error = %v", err)
	}
	if receivedToken != "token-value" || receivedActor.Username != "alice@example.test" || receivedAccess != access {
		t.Fatalf("review inputs = token %q actor %#v access %#v", receivedToken, receivedActor, receivedAccess)
	}
	if result.Identity.Username != "alice@example.test" || !result.Review.Allowed || result.Review.Reason != "allowed" {
		t.Fatalf("review result = %#v", result)
	}
}

func TestAccessTokenServiceReviewTokenFailsClosed(t *testing.T) {
	t.Parallel()
	service := NewAccessTokenService(
		tokenAuthenticatorFunc(func(context.Context, string) (domain.Actor, error) {
			return domain.Actor{}, domain.ErrUnauthorized
		}),
		accessReviewerFunc(func(context.Context, domain.Actor, domain.Access) (bool, error) {
			return false, domain.ErrUnavailable
		}),
		nil, false,
	)
	for _, token := range []string{"", "   ", strings.Repeat("x", maxKubernetesTokenLength+1)} {
		if _, err := service.ReviewToken(context.Background(), token, domain.Access{Verb: "get", Resource: "pods"}); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("ReviewToken(%d bytes) error = %v, want unauthorized", len(token), err)
		}
	}

	service = NewAccessTokenService(
		tokenAuthenticatorFunc(func(context.Context, string) (domain.Actor, error) {
			return domain.Actor{Username: "alice@example.test"}, nil
		}),
		accessReviewerFunc(func(context.Context, domain.Actor, domain.Access) (bool, error) {
			return false, domain.ErrUnavailable
		}),
		nil, false,
	)
	result, err := service.ReviewToken(context.Background(), "valid-token", domain.Access{Verb: "get", Resource: "pods"})
	if err != nil {
		t.Fatalf("ReviewToken() error = %v", err)
	}
	if result.Review.Allowed || result.Review.Reason != "authorization_unavailable" {
		t.Fatalf("unavailable review = %#v", result.Review)
	}
}

func TestAccessTokenServiceIssueNormalizesExplicitRules(t *testing.T) {
	t.Parallel()
	issuer := &accessTokenIssuerStub{result: domain.IssuedAccessToken{Token: "issued-token"}}
	service := NewAccessTokenService(nil, nil, issuer, false)
	result, err := service.Issue(context.Background(), domain.Actor{Username: "admin", Admin: true}, domain.AccessTokenIssueRequest{
		Name:       "  oncall  ",
		Namespace:  " default ",
		TTLSeconds: 60,
		Rules: []domain.AccessTokenRule{{
			Verbs:         []string{" get ", "get", "list"},
			APIGroups:     []string{" ", ""},
			Resources:     []string{" pods ", "pods"},
			ResourceNames: []string{" api-0 ", "api-0"},
		}},
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !issuer.called || result.Token != "issued-token" {
		t.Fatalf("issuer result = %#v, called = %v", result, issuer.called)
	}
	request := issuer.request
	if request.Name != "oncall" || request.Namespace != "default" || request.TTLSeconds != 300 {
		t.Fatalf("normalized request = %#v", request)
	}
	if len(request.Rules) != 1 || len(request.Rules[0].Verbs) != 2 || len(request.Rules[0].APIGroups) != 1 || request.Rules[0].APIGroups[0] != "" {
		t.Fatalf("normalized rules = %#v", request.Rules)
	}
	if len(request.Rules[0].Resources) != 1 || request.Rules[0].Resources[0] != "pods" || len(request.Rules[0].ResourceNames) != 1 || request.Rules[0].ResourceNames[0] != "api-0" {
		t.Fatalf("normalized resources = %#v", request.Rules[0])
	}
}

func TestAccessTokenServiceRejectsUnsafeIssuance(t *testing.T) {
	t.Parallel()
	issuer := &accessTokenIssuerStub{}
	service := NewAccessTokenService(nil, nil, issuer, false)
	admin := domain.Actor{Username: "admin", Admin: true}
	validRule := domain.AccessTokenRule{Verbs: []string{"get"}, Resources: []string{"pods"}}
	tests := []struct {
		name    string
		actor   domain.Actor
		request domain.AccessTokenIssueRequest
	}{
		{name: "non admin", actor: domain.Actor{Username: "viewer"}, request: domain.AccessTokenIssueRequest{Namespace: "default", Rules: []domain.AccessTokenRule{validRule}}},
		{name: "invalid namespace", actor: admin, request: domain.AccessTokenIssueRequest{Namespace: "Default", Rules: []domain.AccessTokenRule{validRule}}},
		{name: "missing rules", actor: admin, request: domain.AccessTokenIssueRequest{Namespace: "default"}},
		{name: "wildcard resource", actor: admin, request: domain.AccessTokenIssueRequest{Namespace: "default", Rules: []domain.AccessTokenRule{{Verbs: []string{"get"}, Resources: []string{"*"}}}}},
		{name: "wildcard group", actor: admin, request: domain.AccessTokenIssueRequest{Namespace: "default", Rules: []domain.AccessTokenRule{{Verbs: []string{"get"}, APIGroups: []string{"*"}, Resources: []string{"pods"}}}}},
		{name: "wildcard resource name", actor: admin, request: domain.AccessTokenIssueRequest{Namespace: "default", Rules: []domain.AccessTokenRule{{Verbs: []string{"get"}, Resources: []string{"pods"}, ResourceNames: []string{"*"}}}}},
		{name: "invalid verb", actor: admin, request: domain.AccessTokenIssueRequest{Namespace: "default", Rules: []domain.AccessTokenRule{{Verbs: []string{"*"}, Resources: []string{"pods"}}}}},
		{name: "oversized name", actor: admin, request: domain.AccessTokenIssueRequest{Name: strings.Repeat("x", maxAccessTokenNameLen+1), Namespace: "default", Rules: []domain.AccessTokenRule{validRule}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Issue(context.Background(), tt.actor, tt.request)
			if tt.actor.Admin {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("Issue() error = %v, want invalid", err)
				}
			} else if !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("Issue() error = %v, want forbidden", err)
			}
		})
	}
	if issuer.called {
		t.Fatal("issuer was called for an unsafe request")
	}
}

func TestAccessTokenServiceCleanupRequiresAdminAndValidNamespace(t *testing.T) {
	t.Parallel()
	issuer := &accessTokenIssuerStub{cleanupDeleted: 2}
	service := NewAccessTokenService(nil, nil, issuer, false)
	if _, err := service.Cleanup(context.Background(), domain.Actor{Username: "viewer"}, "default"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin Cleanup() error = %v, want forbidden", err)
	}
	if _, err := service.Cleanup(context.Background(), domain.Actor{Username: "admin", Admin: true}, "Bad_Namespace"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid namespace Cleanup() error = %v, want invalid", err)
	}
	result, err := service.Cleanup(context.Background(), domain.Actor{Username: "admin", Admin: true}, " default ")
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.Deleted != 2 || issuer.cleanupNamespace != "default" {
		t.Fatalf("Cleanup() result = %#v, namespace = %q", result, issuer.cleanupNamespace)
	}
}

func TestAccessTokenServiceClampsTTL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		input int64
		want  int64
	}{
		{name: "default", input: 0, want: int64(defaultAccessTokenTTL / time.Second)},
		{name: "minimum", input: 1, want: int64(minAccessTokenTTL / time.Second)},
		{name: "maximum", input: 999999, want: int64(maxAccessTokenTTL / time.Second)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAccessTokenTTL(tt.input); got != tt.want {
				t.Fatalf("normalizeAccessTokenTTL(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestAccessTokenServiceBlocksMutationInReadOnlyMode(t *testing.T) {
	t.Parallel()
	issuer := &accessTokenIssuerStub{}
	service := NewAccessTokenService(nil, nil, issuer, true)
	admin := domain.Actor{Username: "admin", Admin: true}
	_, err := service.Issue(context.Background(), admin, domain.AccessTokenIssueRequest{
		Namespace: "default", Rules: []domain.AccessTokenRule{{Verbs: []string{"get"}, Resources: []string{"pods"}}},
	})
	if !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("Issue() error = %v, want read-only", err)
	}
	if _, err := service.Cleanup(context.Background(), admin, "default"); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("Cleanup() error = %v, want read-only", err)
	}
	if issuer.called {
		t.Fatal("issuer mutated Kubernetes in read-only mode")
	}
}
