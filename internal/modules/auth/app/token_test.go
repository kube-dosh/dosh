package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"dosh/internal/domain"
)

type tokenAuthenticatorFunc func(context.Context, string) (domain.Actor, error)

func (f tokenAuthenticatorFunc) AuthenticateToken(ctx context.Context, token string) (domain.Actor, error) {
	return f(ctx, token)
}

func TestKubernetesTokenLoginCreatesSession(t *testing.T) {
	t.Parallel()
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := NewSessionService(manager, tokenTestAuthorizer{}, "token")
	var received string
	service := NewKubernetesTokenService(tokenAuthenticatorFunc(func(_ context.Context, token string) (domain.Actor, error) {
		received = token
		return domain.Actor{Username: "alice@example.test", Groups: []string{"viewers"}}, nil
	}), sessions, nil)
	session, err := service.LoginContext(context.Background(), "192.0.2.10", "  token-value  ")
	if err != nil {
		t.Fatalf("LoginContext() error = %v", err)
	}
	if received != "token-value" {
		t.Fatalf("authenticator token = %q", received)
	}
	if session.Username != "alice@example.test" || len(session.Groups) != 1 || session.CSRFToken == "" {
		t.Fatalf("session = %#v", session)
	}
}

func TestKubernetesTokenLoginRejectsInvalidToken(t *testing.T) {
	t.Parallel()
	manager, _ := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	sessions := NewSessionService(manager, tokenTestAuthorizer{}, "token")
	service := NewKubernetesTokenService(tokenAuthenticatorFunc(func(context.Context, string) (domain.Actor, error) {
		return domain.Actor{}, domain.ErrUnauthorized
	}), sessions, nil)
	if _, err := service.LoginContext(context.Background(), "192.0.2.10", "invalid"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("LoginContext() error = %v, want unauthorized", err)
	}
	if _, err := service.LoginContext(context.Background(), "192.0.2.10", " "); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("blank token error = %v, want unauthorized", err)
	}
}

type tokenTestAuthorizer struct{}

func (tokenTestAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return nil }
