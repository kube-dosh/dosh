package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
)

type namespaceAuthorizer struct{ allowedNamespace string }

type loginAttemptGateStub struct {
	beginAllowed bool
	beginErr     error
	failErr      error
}

func (g loginAttemptGateStub) Begin(context.Context, string) (bool, error) {
	return g.beginAllowed, g.beginErr
}

func (g loginAttemptGateStub) Fail(context.Context, string) error { return g.failErr }

func (g loginAttemptGateStub) Reset(context.Context, string) error { return nil }

func (a namespaceAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	if access.Namespace == a.allowedNamespace {
		return nil
	}
	return domain.ErrForbidden
}

func TestCapabilitiesUseRequestedNamespace(t *testing.T) {
	t.Parallel()
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := NewSessionService(manager, namespaceAuthorizer{allowedNamespace: "dosh-e2e-target"}, "oidc")
	actor := domain.Actor{Username: "operator@example.test", Groups: []string{"dosh-e2e-operators"}}

	scoped := sessions.Capabilities(context.Background(), actor, "dosh-e2e-target")
	if !scoped.Read || !scoped.Scale || !scoped.Restart || !scoped.UpdateImage || !scoped.Delete {
		t.Fatalf("namespace capabilities = %#v", scoped)
	}
	global := sessions.Capabilities(context.Background(), actor, "")
	if global.Scale || global.Restart || global.UpdateImage || global.Delete {
		t.Fatalf("cluster-scoped capabilities = %#v", global)
	}
}

func newTestService(t *testing.T, readOnly bool) *Service {
	t.Helper()
	hash, err := HashPassword("a-valid-test-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	authorizer := authz.NewLocal(readOnly)
	sessions := NewSessionService(manager, authorizer, "local")
	return NewService(Config{Username: "admin", PasswordHash: hash, ReadOnly: readOnly}, sessions)
}

func TestServiceLoginAndCapabilities(t *testing.T) {
	t.Parallel()
	service := newTestService(t, false)
	session, err := service.Login("127.0.0.1", "admin", "a-valid-test-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Username != "admin" || !session.Admin {
		t.Fatalf("unexpected session: %#v", session)
	}
	if _, err := service.Login("127.0.0.2", "admin", "wrong-password"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong Login() error = %v, want ErrUnauthorized", err)
	}
	caps := service.Sessions().Capabilities(context.Background(), domain.Actor{Username: "admin", Admin: true}, "")
	if !caps.Scale || !caps.UpdateImage || !caps.SuspendCronJob || !caps.RevealSecrets || !caps.EditYAML || !caps.NodeCordon {
		t.Fatalf("admin capabilities = %#v", caps)
	}
}

func TestServiceFailsClosedWhenLoginAttemptGateFails(t *testing.T) {
	t.Parallel()
	manager, err := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := NewSessionService(manager, authz.NewLocal(false), "local")
	hash, err := HashPassword("a-valid-test-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	config := Config{Username: "admin", PasswordHash: hash}

	unavailable := NewServiceWithLoginAttempts(config, sessions, loginAttemptGateStub{beginErr: errors.New("lease unavailable")})
	if _, err := unavailable.Login("127.0.0.1", "admin", "a-valid-test-password"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Begin failure Login() error = %v, want ErrUnavailable", err)
	}

	failUnavailable := NewServiceWithLoginAttempts(config, sessions, loginAttemptGateStub{beginAllowed: true, failErr: errors.New("lease unavailable")})
	if _, err := failUnavailable.Login("127.0.0.1", "admin", "wrong-password"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Fail failure Login() error = %v, want ErrUnavailable", err)
	}
}

func TestServiceReadOnlyCapabilities(t *testing.T) {
	t.Parallel()
	service := newTestService(t, true)
	actor := domain.Actor{Username: "admin", Admin: true}
	caps := service.Sessions().Capabilities(context.Background(), actor, "")
	if caps.Scale || caps.Restart || caps.UpdateImage || caps.SuspendCronJob || caps.Delete || caps.RevealSecrets || caps.EditYAML || caps.NodeCordon {
		t.Fatalf("read-only capabilities = %#v", caps)
	}
	if caps := service.Sessions().Capabilities(context.Background(), actor, ""); !caps.Read {
		t.Fatalf("read-only capabilities = %#v", caps)
	}
	if caps := service.Sessions().Capabilities(context.Background(), domain.Actor{Username: "viewer"}, ""); caps.Scale || caps.UpdateImage || caps.SuspendCronJob || caps.Delete {
		t.Fatalf("non-admin capabilities = %#v", caps)
	}
}

func TestLoginLimiterBlocksAfterThreshold(t *testing.T) {
	t.Parallel()
	limiter := NewLoginLimiter()
	for range 5 {
		if !limiter.Begin("192.0.2.1") {
			t.Fatal("limiter rejected an in-window attempt")
		}
		limiter.Fail("192.0.2.1")
	}
	if limiter.Begin("192.0.2.1") {
		t.Fatal("limiter allowed a blocked IP")
	}
	if !limiter.Begin("192.0.2.2") {
		t.Fatal("limiter blocked an unrelated IP")
	}
	limiter.Reset("192.0.2.1")
	if !limiter.Begin("192.0.2.1") {
		t.Fatal("limiter did not clear the blocked IP")
	}
}

func TestLoginLimiterReservesAttemptsBeforeConcurrentVerification(t *testing.T) {
	t.Parallel()
	limiter := NewLoginLimiter()
	var allowed atomic.Int64
	var wait sync.WaitGroup
	for range 64 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if limiter.Begin("192.0.2.10") {
				allowed.Add(1)
			}
		}()
	}
	wait.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("allowed %d concurrent attempts, want 5", got)
	}
}

func TestLoginLimiterEvictsUnblockedEntriesButPreservesActiveBlocks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	limiter := NewLoginLimiter()
	limiter.maxEntries = 2
	limiter.now = func() time.Time { return now }

	if !limiter.Begin("192.0.2.20") || !limiter.Begin("192.0.2.21") {
		t.Fatal("limiter rejected attempts before reaching capacity")
	}
	if !limiter.Begin("192.0.2.22") {
		t.Fatal("limiter did not evict an unblocked entry at capacity")
	}
	if got := len(limiter.attempts); got != limiter.maxEntries {
		t.Fatalf("entry count = %d, want %d", got, limiter.maxEntries)
	}

	limiter.Reset("192.0.2.20")
	limiter.Reset("192.0.2.21")
	limiter.Reset("192.0.2.22")
	for _, key := range []string{"192.0.2.30", "192.0.2.31"} {
		for range 5 {
			if !limiter.Begin(key) {
				t.Fatalf("limiter rejected in-window attempt for %s", key)
			}
			limiter.Fail(key)
		}
	}
	if limiter.Begin("192.0.2.32") {
		t.Fatal("limiter evicted an active block at capacity")
	}

	now = now.Add(limiter.blockFor + limiter.window + time.Minute)
	if !limiter.Begin("192.0.2.32") {
		t.Fatal("limiter did not reclaim expired entries")
	}
}
