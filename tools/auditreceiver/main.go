package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxBodyBytes = 1 << 20

type receivedEvent struct {
	RequestID string    `json:"requestId"`
	User      string    `json:"user"`
	Groups    []string  `json:"groups"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Result    string    `json:"result"`
	Error     string    `json:"error,omitempty"`
	Verified  bool      `json:"verified"`
	Received  time.Time `json:"received"`
}

type receiver struct {
	mu             sync.Mutex
	hmacKey        []byte
	token          string
	requireToken   bool
	requireHMAC    bool
	lokiURL        string
	lokiTenant     string
	lokiClient     *http.Client
	events         []receivedEvent
	seen           map[string]struct{}
	attempts       int
	verified       int
	faultStatus    int
	faultRemaining int
	delay          time.Duration
}

type auditPayload struct {
	Timestamp string   `json:"timestamp"`
	User      string   `json:"user"`
	Groups    []string `json:"groups"`
	Action    string   `json:"action"`
	Resource  string   `json:"resource"`
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Result    string   `json:"result"`
	RequestID string   `json:"requestId"`
	Error     string   `json:"error,omitempty"`
}

type statsResponse struct {
	Attempts       int             `json:"attempts"`
	Received       int             `json:"received"`
	Verified       int             `json:"verified"`
	FaultRemaining int             `json:"faultRemaining"`
	AuthMode       string          `json:"authMode"`
	LokiConfigured bool            `json:"lokiConfigured"`
	RequestID      []string        `json:"requestIds"`
	Events         []receivedEvent `json:"events"`
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type controlRequest struct {
	FailStatus  int    `json:"failStatus"`
	FailCount   int    `json:"failCount"`
	DelayMillis int    `json:"delayMillis"`
	AuthMode    string `json:"authMode"`
	Reset       bool   `json:"reset"`
}

func main() {
	addr := flag.String("addr", ":8443", "listen address")
	certFile := flag.String("cert", "", "TLS certificate file")
	keyFile := flag.String("key", "", "TLS private key file")
	flag.Parse()

	rec := &receiver{
		hmacKey:      []byte(strings.TrimSpace(os.Getenv("AUDIT_HMAC_KEY"))),
		token:        strings.TrimSpace(os.Getenv("AUDIT_BEARER_TOKEN")),
		requireToken: strings.TrimSpace(os.Getenv("AUDIT_BEARER_TOKEN")) != "",
		requireHMAC:  strings.TrimSpace(os.Getenv("AUDIT_HMAC_KEY")) != "",
		lokiURL:      strings.TrimSpace(os.Getenv("AUDIT_LOKI_URL")),
		lokiTenant:   strings.TrimSpace(os.Getenv("AUDIT_LOKI_TENANT")),
		seen:         make(map[string]struct{}),
	}
	if rec.lokiTenant == "" {
		rec.lokiTenant = "dosh"
	}
	if rec.lokiURL != "" {
		client, err := newLokiClient(rec.lokiURL, strings.TrimSpace(os.Getenv("AUDIT_LOKI_CA_FILE")), envDuration("AUDIT_LOKI_TIMEOUT", 3*time.Second))
		if err != nil {
			log.Fatalf("configure Loki: %v", err)
		}
		rec.lokiClient = client
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/audit", rec.handleAudit)
	mux.HandleFunc("/stats", rec.handleStats)
	mux.HandleFunc("/control", rec.handleControl)

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		var err error
		if *certFile != "" || *keyFile != "" {
			err = server.ListenAndServeTLS(*certFile, *keyFile)
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	log.Printf("audit receiver started addr=%s tls=%t loki=%t", *addr, *certFile != "", rec.lokiURL != "")
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	}
}

func (r *receiver) handleAudit(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.mu.Lock()
	r.attempts++
	requireToken, requireHMAC := r.requireToken, r.requireHMAC
	r.mu.Unlock()
	if requireToken && (r.token == "" || request.Header.Get("Authorization") != "Bearer "+r.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxBodyBytes+1))
	if err != nil || len(body) > maxBodyBytes {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	verified := !requireHMAC || (len(r.hmacKey) > 0 && verifySignature(r.hmacKey, request, body))
	if !verified {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var payload auditPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
	if requestID == "" {
		http.Error(w, "missing request id", http.StatusBadRequest)
		return
	}
	if payload.RequestID != "" && payload.RequestID != requestID {
		http.Error(w, "request id mismatch", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	delay := r.delay
	fail := r.faultStatus > 0 && r.faultRemaining > 0
	if fail && r.faultStatus != 0 {
		r.faultRemaining--
	}
	status := r.faultStatus
	r.mu.Unlock()
	if delay > 0 {
		select {
		case <-request.Context().Done():
			http.Error(w, "request canceled", http.StatusRequestTimeout)
			return
		case <-time.After(delay):
		}
	}
	if fail {
		http.Error(w, http.StatusText(status), status)
		return
	}
	if r.lokiClient != nil {
		if err := r.pushLoki(request.Context(), payload, body); err != nil {
			log.Printf("audit_loki_push_failed request_id=%s error=%v", requestID, err)
			http.Error(w, "loki unavailable", http.StatusBadGateway)
			return
		}
	}
	r.mu.Lock()
	r.verified++
	if _, exists := r.seen[requestID]; !exists {
		r.seen[requestID] = struct{}{}
		r.events = append(r.events, receivedEvent{
			RequestID: requestID, User: payload.User, Groups: payload.Groups,
			Action: payload.Action, Resource: payload.Resource, Namespace: payload.Namespace,
			Name: payload.Name, Result: payload.Result, Error: payload.Error,
			Verified: verified, Received: time.Now().UTC(),
		})
	}
	r.mu.Unlock()
	log.Printf("audit_received request_id=%s user=%s action=%s resource=%s result=%s verified=%t",
		requestID, payload.User, payload.Action, payload.Resource, payload.Result, verified)
	w.WriteHeader(http.StatusNoContent)
}

func (r *receiver) handleStats(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.mu.Lock()
	ids := make([]string, 0, len(r.events))
	for _, event := range r.events {
		ids = append(ids, event.RequestID)
	}
	response := statsResponse{
		Attempts: r.attempts, Received: len(r.events), Verified: r.verified,
		FaultRemaining: r.faultRemaining, AuthMode: r.authModeLocked(),
		LokiConfigured: r.lokiClient != nil,
		RequestID:      ids, Events: append([]receivedEvent(nil), r.events...),
	}
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (r *receiver) handleControl(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var control controlRequest
	if err := json.NewDecoder(io.LimitReader(request.Body, maxBodyBytes)).Decode(&control); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if control.FailStatus != 0 && (control.FailStatus < 400 || control.FailStatus > 599) {
		http.Error(w, "failStatus must be 400-599", http.StatusBadRequest)
		return
	}
	if control.FailCount < 0 || control.DelayMillis < 0 || control.DelayMillis > 60000 {
		http.Error(w, "failCount and delayMillis out of range", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	switch control.AuthMode {
	case "":
	case "both":
		if r.token == "" || len(r.hmacKey) == 0 {
			r.mu.Unlock()
			http.Error(w, "both credentials are not configured", http.StatusBadRequest)
			return
		}
		r.requireToken, r.requireHMAC = true, true
	case "token":
		if r.token == "" {
			r.mu.Unlock()
			http.Error(w, "bearer token is not configured", http.StatusBadRequest)
			return
		}
		r.requireToken, r.requireHMAC = true, false
	case "hmac":
		if len(r.hmacKey) == 0 {
			r.mu.Unlock()
			http.Error(w, "HMAC key is not configured", http.StatusBadRequest)
			return
		}
		r.requireToken, r.requireHMAC = false, true
	default:
		r.mu.Unlock()
		http.Error(w, "authMode must be both, token, or hmac", http.StatusBadRequest)
		return
	}
	if control.Reset {
		r.attempts = 0
		r.verified = 0
		r.events = nil
		r.seen = make(map[string]struct{})
	}
	r.faultStatus = control.FailStatus
	r.faultRemaining = control.FailCount
	r.delay = time.Duration(control.DelayMillis) * time.Millisecond
	status, remaining, delay, authMode := r.faultStatus, r.faultRemaining, r.delay, r.authModeLocked()
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"failStatus": status, "faultRemaining": remaining, "delayMillis": delay.Milliseconds(), "authMode": authMode,
	})
}

func (r *receiver) authModeLocked() string {
	switch {
	case r.requireToken && r.requireHMAC:
		return "both"
	case r.requireToken:
		return "token"
	case r.requireHMAC:
		return "hmac"
	default:
		return "none"
	}
}

func verifySignature(key []byte, request *http.Request, body []byte) bool {
	timestamp := request.Header.Get("X-Dosh-Timestamp")
	signature := request.Header.Get("X-Dosh-Signature")
	if timestamp == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(signature))
}

func newLokiClient(rawURL, caFile string, timeout time.Duration) (*http.Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("loki URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("loki URL must include a host")
	}
	if timeout <= 0 || timeout > 30*time.Second {
		return nil, errors.New("loki timeout must be between 1ns and 30s")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no valid certificates")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func (r *receiver) pushLoki(ctx context.Context, payload auditPayload, body []byte) error {
	timestamp, err := time.Parse(time.RFC3339Nano, payload.Timestamp)
	if err != nil {
		timestamp = time.Now().UTC()
	}
	stream := map[string]string{"job": "dosh-audit", "service": "dosh"}
	if payload.Result != "" {
		stream["result"] = payload.Result
	}
	requestBody, err := json.Marshal(lokiPushRequest{Streams: []lokiStream{{
		Stream: stream,
		Values: [][2]string{{strconv.FormatInt(timestamp.UTC().UnixNano(), 10), string(body)}},
	}}})
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	target := strings.TrimRight(r.lokiURL, "/") + "/loki/api/v1/push"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("create push request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Scope-OrgID", r.lokiTenant)
	response, err := r.lokiClient.Do(request)
	if err != nil {
		return fmt.Errorf("send push request: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("loki returned status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
