package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"dosh/internal/domain"
)

type oidcTestAuthorizer struct{}

func (oidcTestAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return nil }

type oidcTestProvider struct {
	state       string
	nonce       string
	verifier    string
	identity    OIDCIdentity
	exchangeErr error
}

func (p *oidcTestProvider) AuthCodeURL(state, nonce, verifier string) string {
	p.state, p.nonce, p.verifier = state, nonce, verifier
	return "https://idp.example.test/authorize?state=" + state
}

func (p *oidcTestProvider) Exchange(_ context.Context, _ string, verifier, nonce string) (OIDCIdentity, error) {
	if verifier != p.verifier || nonce != p.nonce {
		return OIDCIdentity{}, errors.New("PKCE or nonce mismatch")
	}
	return p.identity, p.exchangeErr
}

func (p *oidcTestProvider) EndSessionURL(redirect string) string {
	return "https://idp.example.test/logout?redirect=" + redirect
}

func newOIDCTestService(t *testing.T, provider *oidcTestProvider) *OIDCService {
	t.Helper()
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := NewSessionService(manager, oidcTestAuthorizer{}, "oidc")
	states, err := NewOIDCStateManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewOIDCStateManager() error = %v", err)
	}
	return NewOIDCService(provider, sessions, states, OIDCConfig{
		AdminGroups: []string{"dosh-admins"}, SessionTTL: time.Hour, StateTTL: 5 * time.Minute,
		PostLogoutRedirectURL: "https://dosh.example.test/login",
	})
}

func TestOIDCFlowCreatesShortLivedSessionAndValidatesState(t *testing.T) {
	t.Parallel()
	provider := &oidcTestProvider{identity: OIDCIdentity{
		Subject: "subject-1", Username: "alice@example.test", Groups: []string{"dosh-admins", "viewers"},
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}}
	service := newOIDCTestService(t, provider)
	_, sealedState, _, err := service.Begin("/pods?namespace=default")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if provider.state == "" || provider.nonce == "" || provider.verifier == "" {
		t.Fatalf("provider did not receive state/nonce/verifier: %#v", provider)
	}
	if got := service.ReturnTo(sealedState); got != "/pods?namespace=default" {
		t.Fatalf("ReturnTo() = %q", got)
	}
	session, err := service.Complete(context.Background(), "code-1", provider.state, sealedState)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if session.Username != "alice@example.test" || !session.Admin || session.CSRFToken == "" {
		t.Fatalf("session = %#v", session)
	}
	if session.ExpiresAt.After(time.Now().Add(31 * time.Minute)) {
		t.Fatalf("session expiry was not capped by ID token: %s", session.ExpiresAt)
	}
	if _, err := service.Complete(context.Background(), "code-1", "wrong-state", sealedState); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong state error = %v", err)
	}
}

func TestOIDCReturnToRejectsOpenRedirect(t *testing.T) {
	for _, returnTo := range []string{
		"//evil.example.test/steal",
		"/\\evil.example.test/steal",
		"/%0d%0aLocation:%20https://evil.example.test",
		"https://evil.example.test/steal",
		"javascript:alert(1)",
		"pods",
	} {
		returnTo := returnTo
		t.Run(returnTo, func(t *testing.T) {
			t.Parallel()
			service := newOIDCTestService(t, &oidcTestProvider{})
			_, sealedState, _, err := service.Begin(returnTo)
			if err != nil {
				t.Fatalf("Begin() error = %v", err)
			}
			if got := service.ReturnTo(sealedState); got != "/" {
				t.Fatalf("ReturnTo(%q) = %q, want /", returnTo, got)
			}
		})
	}
}

func TestOIDCReturnToAllowsOnlySafeSameOriginPath(t *testing.T) {
	t.Parallel()
	service := newOIDCTestService(t, &oidcTestProvider{})
	_, sealedState, _, err := service.Begin("/pods?namespace=default#top")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if got := service.ReturnTo(sealedState); got != "/pods?namespace=default#top" {
		t.Fatalf("ReturnTo() = %q", got)
	}
}

func TestOIDCStateCookieRejectsTampering(t *testing.T) {
	t.Parallel()
	manager, err := NewOIDCStateManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewOIDCStateManager() error = %v", err)
	}
	sealed, err := manager.Encode(oidcState{State: "state", Nonce: "nonce", Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if _, err := manager.Decode(sealed + "x"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("tampered state error = %v", err)
	}
}

func TestOIDCRejectsExpiredState(t *testing.T) {
	t.Parallel()
	provider := &oidcTestProvider{}
	service := newOIDCTestService(t, provider)
	service.config.StateTTL = -time.Second
	_, sealedState, expiresAt, err := service.Begin("/pods")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if !expiresAt.Before(time.Now()) {
		t.Fatalf("expiresAt = %s, want an already-expired state", expiresAt)
	}
	if _, err := service.Complete(context.Background(), "code-1", provider.state, sealedState); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expired state error = %v", err)
	}
}

func TestOIDCLogoutURLUsesAbsolutePostLogoutRedirect(t *testing.T) {
	t.Parallel()
	service := newOIDCTestService(t, &oidcTestProvider{})
	got := service.LogoutURL()
	want := "https://idp.example.test/logout?redirect=https://dosh.example.test/login"
	if got != want {
		t.Fatalf("LogoutURL() = %q, want %q", got, want)
	}
}
