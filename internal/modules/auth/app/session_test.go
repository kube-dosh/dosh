package app

import (
	"errors"
	"testing"
	"time"

	"dosh/internal/domain"
)

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
