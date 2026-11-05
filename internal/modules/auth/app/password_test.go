package app

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	valid, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !valid {
		t.Fatalf("VerifyPassword() = %v, %v", valid, err)
	}
	valid, err = VerifyPassword("wrong password", hash)
	if err != nil || valid {
		t.Fatalf("wrong password VerifyPassword() = %v, %v", valid, err)
	}
}

func TestHashPasswordRejectsShortInput(t *testing.T) {
	t.Parallel()
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("HashPassword() accepted a short password")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	t.Parallel()
	for _, encoded := range []string{"", "plain-text", "$argon2id$v=19$m=999999,t=3,p=2$bad$bad"} {
		if valid, err := VerifyPassword("password", encoded); err == nil || valid {
			t.Fatalf("VerifyPassword(%q) = %v, %v; want error", encoded, valid, err)
		}
	}
}
