package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

const maxKubernetesTokenLength = 16 * 1024

// TokenAuthenticator validates an external bearer token and returns the
// Kubernetes identity it represents. Implementations must not retain the raw
// token or include it in errors.
type TokenAuthenticator interface {
	AuthenticateToken(ctx context.Context, token string) (domain.Actor, error)
}

type KubernetesTokenService struct {
	authenticator TokenAuthenticator
	sessions      *SessionService
	attempts      LoginAttemptGate
}

func NewKubernetesTokenService(authenticator TokenAuthenticator, sessions *SessionService, attempts LoginAttemptGate) *KubernetesTokenService {
	if attempts == nil {
		attempts = memoryLoginAttempts{limiter: NewLoginLimiter()}
	}
	return &KubernetesTokenService{authenticator: authenticator, sessions: sessions, attempts: attempts}
}

func (s *KubernetesTokenService) LoginContext(ctx context.Context, ip, token string) (Session, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxKubernetesTokenLength {
		return Session{}, domain.ErrUnauthorized
	}
	allowed, err := s.attempts.Begin(ctx, ip)
	if err != nil {
		return Session{}, fmt.Errorf("%w: login rate limiter unavailable", domain.ErrUnavailable)
	}
	if !allowed {
		return Session{}, domain.ErrRateLimited
	}
	actor, err := s.authenticator.AuthenticateToken(domain.WithoutActor(ctx), token)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) || errors.Is(err, domain.ErrForbidden) {
			_ = s.attempts.Fail(ctx, ip)
		}
		return Session{}, err
	}
	if strings.TrimSpace(actor.Username) == "" {
		_ = s.attempts.Fail(ctx, ip)
		return Session{}, domain.ErrUnauthorized
	}
	_ = s.attempts.Reset(ctx, ip)
	return s.sessions.Manager().New(actor.Username, actor.Groups, actor.Admin)
}
