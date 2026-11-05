package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"dosh/internal/domain"
)

const CookieName = "dosh_session"

type Session struct {
	Username  string    `json:"username"`
	Groups    []string  `json:"groups,omitempty"`
	Admin     bool      `json:"admin"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type SessionManager struct {
	aead cipher.AEAD
	ttl  time.Duration
}

func NewSessionManager(secret []byte, ttl time.Duration) (*SessionManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("session secret must contain at least 32 bytes")
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SessionManager{aead: aead, ttl: ttl}, nil
}

func (m *SessionManager) New(username string, groups []string, admin bool) (Session, error) {
	return m.NewWithExpiry(username, groups, admin, time.Now().Add(m.ttl).UTC())
}

func (m *SessionManager) NewWithExpiry(username string, groups []string, admin bool, expiresAt time.Time) (Session, error) {
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	return Session{Username: username, Groups: groups, Admin: admin, CSRFToken: csrf, ExpiresAt: expiresAt.UTC()}, nil
}

// Encode serializes and seals a session for transport in an HTTP cookie. Cookie
// attributes and transport policy belong to the HTTP adapter.
func (m *SessionManager) Encode(session Session) (string, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := m.aead.Seal(nonce, nonce, payload, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decode authenticates and validates a sealed session value.
func (m *SessionManager) Decode(value string) (Session, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(sealed) < m.aead.NonceSize() {
		return Session{}, domain.ErrUnauthorized
	}
	nonce, ciphertext := sealed[:m.aead.NonceSize()], sealed[m.aead.NonceSize():]
	payload, err := m.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return Session{}, domain.ErrUnauthorized
	}
	var session Session
	if err := json.Unmarshal(payload, &session); err != nil {
		return Session{}, domain.ErrUnauthorized
	}
	if session.Username == "" || session.CSRFToken == "" || time.Now().After(session.ExpiresAt) {
		return Session{}, domain.ErrUnauthorized
	}
	return session, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
