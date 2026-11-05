package app

import (
	"context"
	"crypto/subtle"
	"fmt"
	"sync"
	"time"

	"dosh/internal/domain"
)

type LoginLimiter struct {
	mu          sync.Mutex
	attempts    map[string]*loginAttempt
	maxAttempts int
	maxEntries  int
	window      time.Duration
	blockFor    time.Duration
	cleanup     time.Duration
	nextCleanup time.Time
	now         func() time.Time
}

type loginAttempt struct {
	count       int
	first       time.Time
	lastSeen    time.Time
	blockedTill time.Time
}

const loginLimiterMaxEntries = 4096

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		attempts:    map[string]*loginAttempt{},
		maxAttempts: 5,
		maxEntries:  loginLimiterMaxEntries,
		window:      5 * time.Minute,
		blockFor:    5 * time.Minute,
		cleanup:     time.Minute,
		now:         time.Now,
	}
}

// Begin reserves one login attempt before password verification. Reserving
// before the expensive hash check prevents concurrent requests from all
// passing the threshold before failures are recorded.
func (l *LoginLimiter) Begin(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.cleanupLocked(now)
	item := l.attempts[key]
	if item != nil && now.Before(item.blockedTill) {
		return false
	}
	if item != nil && now.Sub(item.first) >= l.window {
		delete(l.attempts, key)
		item = nil
	}
	if item == nil {
		if !l.reserveCapacityLocked(now) {
			return false
		}
		item = &loginAttempt{first: now}
		l.attempts[key] = item
	}
	item.lastSeen = now
	if item.count >= l.maxAttempts {
		item.blockedTill = now.Add(l.blockFor)
		return false
	}
	item.count++
	return true
}

func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	item := l.attempts[key]
	if item == nil {
		return
	}
	item.lastSeen = now
	if item.count >= l.maxAttempts {
		item.blockedTill = now.Add(l.blockFor)
	}
}

func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *LoginLimiter) cleanupLocked(now time.Time) {
	if !l.nextCleanup.IsZero() && now.Before(l.nextCleanup) {
		return
	}
	for key, item := range l.attempts {
		if now.Before(item.blockedTill) {
			continue
		}
		if now.Sub(item.lastSeen) > l.window {
			delete(l.attempts, key)
		}
	}
	l.nextCleanup = now.Add(l.cleanup)
}

func (l *LoginLimiter) reserveCapacityLocked(now time.Time) bool {
	if len(l.attempts) < l.maxEntries {
		return true
	}
	for key, item := range l.attempts {
		if now.Before(item.blockedTill) {
			continue
		}
		if now.Sub(item.lastSeen) > l.window {
			delete(l.attempts, key)
		}
	}
	if len(l.attempts) < l.maxEntries {
		return true
	}
	var oldestKey string
	var oldest time.Time
	for key, item := range l.attempts {
		if now.Before(item.blockedTill) {
			continue
		}
		if oldestKey == "" || item.lastSeen.Before(oldest) {
			oldestKey = key
			oldest = item.lastSeen
		}
	}
	if oldestKey == "" {
		return false
	}
	delete(l.attempts, oldestKey)
	return true
}

type SessionResponse struct {
	User         domain.Actor        `json:"user"`
	CSRFToken    string              `json:"csrfToken"`
	ExpiresAt    time.Time           `json:"expiresAt"`
	Capabilities domain.Capabilities `json:"capabilities"`
	AuthMode     string              `json:"authMode"`
}

type Config struct {
	Username     string
	PasswordHash string
	ReadOnly     bool
}

// LoginAttemptGate reserves, records, and clears per-client login attempts.
// Implementations may share state across replicas; errors must fail closed.
type LoginAttemptGate interface {
	Begin(ctx context.Context, key string) (bool, error)
	Fail(ctx context.Context, key string) error
	Reset(ctx context.Context, key string) error
}

type memoryLoginAttempts struct {
	limiter *LoginLimiter
}

func (m memoryLoginAttempts) Begin(_ context.Context, key string) (bool, error) {
	return m.limiter.Begin(key), nil
}

func (m memoryLoginAttempts) Fail(_ context.Context, key string) error {
	m.limiter.Fail(key)
	return nil
}

func (m memoryLoginAttempts) Reset(_ context.Context, key string) error {
	m.limiter.Reset(key)
	return nil
}

type Service struct {
	config   Config
	sessions *SessionService
	attempts LoginAttemptGate
}

func NewService(config Config, sessions *SessionService) *Service {
	return NewServiceWithLoginAttempts(config, sessions, nil)
}

func NewServiceWithLoginAttempts(config Config, sessions *SessionService, attempts LoginAttemptGate) *Service {
	if attempts == nil {
		attempts = memoryLoginAttempts{limiter: NewLoginLimiter()}
	}
	return &Service{config: config, sessions: sessions, attempts: attempts}
}

func (s *Service) Sessions() *SessionService { return s.sessions }

func (s *Service) Login(ip, username, password string) (Session, error) {
	return s.LoginContext(context.Background(), ip, username, password)
}

func (s *Service) LoginContext(ctx context.Context, ip, username, password string) (Session, error) {
	allowed, err := s.attempts.Begin(ctx, ip)
	if err != nil {
		return Session{}, fmt.Errorf("%w: login rate limiter unavailable", domain.ErrUnavailable)
	}
	if !allowed {
		return Session{}, domain.ErrRateLimited
	}
	usernameOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.config.Username)) == 1
	passwordOK, err := VerifyPassword(password, s.config.PasswordHash)
	if err != nil {
		return Session{}, s.failed(ctx, ip)
	}
	if !usernameOK || !passwordOK {
		return Session{}, s.failed(ctx, ip)
	}
	_ = s.attempts.Reset(ctx, ip)
	return s.sessions.Manager().New(s.config.Username, []string{"dosh-admins"}, true)
}

func (s *Service) failed(ctx context.Context, ip string) error {
	if err := s.attempts.Fail(ctx, ip); err != nil {
		return fmt.Errorf("%w: login rate limiter unavailable", domain.ErrUnavailable)
	}
	return domain.ErrUnauthorized
}
