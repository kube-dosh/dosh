package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/requestid"
)

func TestWebhookAuditIsSignedFlushedAndRedacted(t *testing.T) {
	var attempts atomic.Int32
	received := make(chan *http.Request, 4)
	bodies := make(chan []byte, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if attempts.Add(1) < 3 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		received <- r
		bodies <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	logger, err := NewWithConfig(testLogger(), Config{
		WebhookURL: server.URL, BearerToken: "token", HMACKey: []byte("hmac-secret"), Timeout: time.Second, QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	ctx := requestid.With(context.Background(), "req-audit-1")
	logger.Record(ctx, domain.Actor{Username: "alice", Groups: []string{"platform"}}, domain.AuditEvent{
		Action: "scale", Resource: "deployment", Namespace: "prod", Name: "web", Result: "success",
	})
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := logger.Flush(flushCtx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
	req := <-received
	body := <-bodies
	if got := req.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("authorization = %q", got)
	}
	if got := req.Header.Get("X-Request-ID"); got != "req-audit-1" {
		t.Fatalf("request id = %q", got)
	}
	timestamp := req.Header.Get("X-Dosh-Timestamp")
	mac := hmac.New(sha256.New, []byte("hmac-secret"))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := req.Header.Get("X-Dosh-Signature"); got != wantSignature {
		t.Fatalf("signature = %q, want %q", got, wantSignature)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["user"] != "alice" || payload["action"] != "scale" || payload["requestId"] != "req-audit-1" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, exists := payload["secret"]; exists {
		t.Fatalf("payload unexpectedly contains secret: %#v", payload)
	}
}

func TestCloseFlushesQueuedRecordsAndRejectsNewRecords(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	logger, err := NewWithConfig(testLogger(), Config{
		WebhookURL: server.URL, BearerToken: "token", Timeout: time.Second, QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	logger.Record(context.Background(), domain.Actor{Username: "alice"}, domain.AuditEvent{
		Action: "scale", Resource: "deployment", Namespace: "prod", Name: "web", Result: "success",
	})

	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := logger.Close(closeCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := received.Load(); got != 1 {
		t.Fatalf("received = %d, want 1", got)
	}

	logger.Record(context.Background(), domain.Actor{Username: "alice"}, domain.AuditEvent{
		Action: "delete", Resource: "deployment", Namespace: "prod", Name: "web", Result: "success",
	})
	time.Sleep(50 * time.Millisecond)
	if got := received.Load(); got != 1 {
		t.Fatalf("received after Close = %d, want 1", got)
	}
}

func TestWebhookUsesConfiguredCAAndBearerToken(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer enterprise-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	logger, err := NewWithConfig(testLogger(), Config{
		WebhookURL: server.URL, BearerToken: "enterprise-token", CAFile: caFile,
		Timeout: time.Second, QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	logger.Record(context.Background(), domain.Actor{Username: "alice"}, domain.AuditEvent{
		Action: "reveal", Resource: "secret", Namespace: "prod", Name: "db", Result: "success",
	})
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := logger.Flush(flushCtx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if got := received.Load(); got != 1 {
		t.Fatalf("received = %d, want 1", got)
	}
}

func TestWebhookRejectsInvalidCA(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	_, err := NewWithConfig(testLogger(), Config{
		WebhookURL: "https://siem.example.test/audit", BearerToken: "token", CAFile: caFile,
		Timeout: time.Second, QueueSize: 8,
	})
	if err == nil {
		t.Fatal("NewWithConfig() error = nil, want invalid CA error")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
