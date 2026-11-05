package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"dosh/internal/domain"
)

const OIDCStateCookieName = "dosh_oidc_state"

type OIDCIdentity struct {
	Subject   string
	Username  string
	Groups    []string
	ExpiresAt time.Time
}

// OIDCProvider is the outbound port implemented by the platform OIDC adapter.
// The app layer owns state/nonce/PKCE lifecycle and session policy.
type OIDCProvider interface {
	AuthCodeURL(state, nonce, codeVerifier string) string
	Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (OIDCIdentity, error)
	EndSessionURL(postLogoutRedirectURL string) string
}

type OIDCConfig struct {
	ProviderName          string
	AdminGroups           []string
	SessionTTL            time.Duration
	StateTTL              time.Duration
	PostLogoutRedirectURL string
}

type OIDCService struct {
	provider OIDCProvider
	sessions *SessionService
	states   *OIDCStateManager
	config   OIDCConfig
}

func NewOIDCService(provider OIDCProvider, sessions *SessionService, states *OIDCStateManager, config OIDCConfig) *OIDCService {
	if config.StateTTL <= 0 {
		config.StateTTL = 10 * time.Minute
	}
	return &OIDCService{provider: provider, sessions: sessions, states: states, config: config}
}

func (s *OIDCService) Begin(returnTo string) (authorizationURL, sealedState string, expiresAt time.Time, err error) {
	state, err := randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt = time.Now().Add(s.config.StateTTL).UTC()
	sealedState, err = s.states.Encode(oidcState{
		State: state, Nonce: nonce, Verifier: verifier, ReturnTo: safeReturnTo(returnTo), ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", "", time.Time{}, err
	}
	return s.provider.AuthCodeURL(state, nonce, verifier), sealedState, expiresAt, nil
}

func (s *OIDCService) Complete(ctx context.Context, code, state, sealedState string) (Session, error) {
	if code == "" || state == "" || sealedState == "" {
		return Session{}, domain.ErrUnauthorized
	}
	pending, err := s.states.Decode(sealedState)
	if err != nil || time.Now().After(pending.ExpiresAt) {
		return Session{}, domain.ErrUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(pending.State)) != 1 {
		return Session{}, domain.ErrUnauthorized
	}
	identity, err := s.provider.Exchange(ctx, code, pending.Verifier, pending.Nonce)
	if err != nil {
		return Session{}, fmt.Errorf("%w: OIDC exchange failed: %v", domain.ErrUnauthorized, err)
	}
	if identity.Username == "" || identity.Subject == "" {
		return Session{}, domain.ErrUnauthorized
	}
	expiresAt := time.Now().Add(s.config.SessionTTL)
	if !identity.ExpiresAt.IsZero() && identity.ExpiresAt.Before(expiresAt) {
		expiresAt = identity.ExpiresAt
	}
	actor := domain.Actor{
		Username: identity.Username,
		Groups:   uniqueStrings(identity.Groups),
		Admin:    containsAny(identity.Groups, s.config.AdminGroups),
	}
	session, err := s.sessions.New(ctx, actor, expiresAt)
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *OIDCService) ReturnTo(sealedState string) string {
	pending, err := s.states.Decode(sealedState)
	if err != nil {
		return "/"
	}
	return safeReturnTo(pending.ReturnTo)
}

func (s *OIDCService) LogoutURL() string {
	if strings.TrimSpace(s.config.PostLogoutRedirectURL) == "" {
		return ""
	}
	return s.provider.EndSessionURL(s.config.PostLogoutRedirectURL)
}

func (s *OIDCService) Sessions() *SessionService { return s.sessions }

type oidcState struct {
	State     string    `json:"state"`
	Nonce     string    `json:"nonce"`
	Verifier  string    `json:"verifier"`
	ReturnTo  string    `json:"returnTo"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type OIDCStateManager struct {
	aead cipher.AEAD
}

func NewOIDCStateManager(secret []byte) (*OIDCStateManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("OIDC state secret must contain at least 32 bytes")
	}
	digest := sha256.Sum256(append([]byte("dosh-oidc-state:"), secret...))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &OIDCStateManager{aead: aead}, nil
}

func (m *OIDCStateManager) Encode(state oidcState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(m.aead.Seal(nonce, nonce, payload, nil)), nil
}

func (m *OIDCStateManager) Decode(value string) (oidcState, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(sealed) < m.aead.NonceSize() {
		return oidcState{}, domain.ErrUnauthorized
	}
	nonce, ciphertext := sealed[:m.aead.NonceSize()], sealed[m.aead.NonceSize():]
	payload, err := m.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return oidcState{}, domain.ErrUnauthorized
	}
	var state oidcState
	if err := json.Unmarshal(payload, &state); err != nil {
		return oidcState{}, domain.ErrUnauthorized
	}
	if state.State == "" || state.Nonce == "" || state.Verifier == "" || state.ExpiresAt.IsZero() {
		return oidcState{}, domain.ErrUnauthorized
	}
	return state, nil
}

func safeReturnTo(value string) string {
	if value == "" || value[0] != '/' || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return "/"
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return "/"
		}
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "/"
	}
	for _, char := range parsed.Path {
		if char < 0x20 || char == 0x7f {
			return "/"
		}
	}
	return value
}

func containsAny(values, expected []string) bool {
	for _, value := range values {
		for _, wanted := range expected {
			if value == wanted {
				return true
			}
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
