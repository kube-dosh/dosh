package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

type reviewAuthorizer struct{ err error }

func (a reviewAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return a.err }

func TestSessionRoundTrip(t *testing.T) {
	t.Parallel()
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 8*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	session, err := manager.New("admin", []string{"dosh-admins"}, true)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encoded, err := manager.Encode(session)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := manager.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Username != "admin" || decoded.CSRFToken != session.CSRFToken || !decoded.Admin {
		t.Fatalf("decoded session mismatch: %#v", decoded)
	}
}

func TestSessionRejectsTamperingAndExpiry(t *testing.T) {
	t.Parallel()
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Nanosecond)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	session, err := manager.New("admin", nil, true)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encoded, err := manager.Encode(session)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if _, err := manager.Decode(encoded + "x"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("tampered Decode() error = %v, want ErrUnauthorized", err)
	}
	time.Sleep(time.Millisecond)
	if _, err := manager.Decode(encoded); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expired Decode() error = %v, want ErrUnauthorized", err)
	}
}

func TestSessionManagerRequiresStrongKey(t *testing.T) {
	t.Parallel()
	if _, err := NewSessionManager([]byte("short"), time.Hour); err == nil {
		t.Fatal("NewSessionManager() accepted a short key")
	}
}

func TestAccessReviewExplainsAuthorizationOutcomes(t *testing.T) {
	t.Parallel()
	access := domain.Access{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: "prod", Name: "api-0"}
	tests := []struct {
		name     string
		err      error
		allowed  bool
		reason   string
		guidance string
	}{
		{name: "allowed", reason: "allowed", allowed: true, guidance: "拥有该操作权限"},
		{name: "read only", err: domain.ErrReadOnly, reason: "read_only", guidance: "只读模式"},
		{name: "rbac denied", err: domain.ErrForbidden, reason: "rbac_denied", guidance: "RoleBinding"},
		{name: "unavailable", err: domain.ErrUnavailable, reason: "authorization_unavailable", guidance: "连通性"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewSessionService(nil, reviewAuthorizer{err: tt.err}, "local")
			review := service.Review(context.Background(), domain.Actor{Username: "alice"}, access)
			if review.Allowed != tt.allowed || review.Reason != tt.reason || !strings.Contains(review.Guidance, tt.guidance) {
				t.Fatalf("review = %#v", review)
			}
			if review.KubectlCommand != "kubectl auth can-i create pods/exec --namespace prod --resource-name api-0" {
				t.Fatalf("command = %q", review.KubectlCommand)
			}
		})
	}
	service := NewSessionService(nil, reviewAuthorizer{}, "local")
	review := service.Review(context.Background(), domain.Actor{}, access)
	if !strings.Contains(review.Guidance, "没有有效登录身份") {
		t.Fatalf("anonymous guidance = %q", review.Guidance)
	}
}
