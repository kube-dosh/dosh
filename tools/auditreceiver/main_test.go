package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReceiverVerifiesSignatureAndDeduplicates(t *testing.T) {
	rec := &receiver{hmacKey: []byte("secret"), token: "token", requireToken: true, requireHMAC: true, seen: make(map[string]struct{})}
	body := []byte(`{"user":"alice","action":"scale","resource":"deployment","result":"success"}`)
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	mac := hmac.New(sha256.New, rec.hmacKey)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer token")
		request.Header.Set("X-Request-ID", "req-1")
		request.Header.Set("X-Dosh-Timestamp", timestamp)
		request.Header.Set("X-Dosh-Signature", signature)
		response := httptest.NewRecorder()
		rec.handleAudit(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	}
	statsRequest := httptest.NewRequest(http.MethodGet, "/stats", nil)
	statsRecorder := httptest.NewRecorder()
	rec.handleStats(statsRecorder, statsRequest)
	var stats statsResponse
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Received != 1 || stats.Verified != 2 || len(stats.RequestID) != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestReceiverRejectsBadSignatureAndToken(t *testing.T) {
	rec := &receiver{hmacKey: []byte("secret"), token: "token", requireToken: true, requireHMAC: true, seen: make(map[string]struct{})}
	body := []byte(`{"user":"alice"}`)
	request := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer wrong")
	request.Header.Set("X-Request-ID", "req-1")
	response := httptest.NewRecorder()
	rec.handleAudit(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Request-ID", "req-2")
	request.Header.Set("X-Dosh-Timestamp", time.Now().UTC().Format(time.RFC3339Nano))
	request.Header.Set("X-Dosh-Signature", "sha256=bad")
	response = httptest.NewRecorder()
	rec.handleAudit(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d, want 401", response.Code)
	}
}

func TestReceiverFaultInjectionRecoversAndRecordsEvent(t *testing.T) {
	rec := &receiver{token: "token", requireToken: true, seen: make(map[string]struct{})}
	control := httptest.NewRequest(http.MethodPost, "/control", bytes.NewBufferString(`{"failStatus":503,"failCount":2}`))
	controlResponse := httptest.NewRecorder()
	rec.handleControl(controlResponse, control)
	if controlResponse.Code != http.StatusOK {
		t.Fatalf("control status = %d, want 200", controlResponse.Code)
	}

	body := []byte(`{"user":"alice","groups":["ops"],"action":"scale","resource":"deployment","namespace":"default","name":"web","result":"success"}`)
	for attempt := 1; attempt <= 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer token")
		request.Header.Set("X-Request-ID", "req-fault")
		response := httptest.NewRecorder()
		rec.handleAudit(response, request)
		want := http.StatusNoContent
		if attempt <= 2 {
			want = http.StatusServiceUnavailable
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.Code, want)
		}
	}

	statsRequest := httptest.NewRequest(http.MethodGet, "/stats", nil)
	statsRecorder := httptest.NewRecorder()
	rec.handleStats(statsRecorder, statsRequest)
	var stats statsResponse
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Attempts != 3 || stats.Received != 1 || stats.Verified != 1 || stats.FaultRemaining != 0 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(stats.Events) != 1 || stats.Events[0].Namespace != "default" || stats.Events[0].Name != "web" {
		t.Fatalf("events = %#v", stats.Events)
	}
}

func TestReceiverControlValidatesFaultConfiguration(t *testing.T) {
	rec := &receiver{seen: make(map[string]struct{})}
	for _, body := range []string{
		`{"failStatus":200,"failCount":1}`,
		`{"failStatus":503,"failCount":-1}`,
		`{"delayMillis":60001}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/control", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		rec.handleControl(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("control body %s status = %d, want 400", body, response.Code)
		}
	}
}

func TestReceiverForwardsVerifiedEventToLoki(t *testing.T) {
	var pushed lokiPushRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/push" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Scope-OrgID"); got != "dosh" {
			t.Errorf("tenant = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&pushed); err != nil {
			t.Errorf("decode push request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := newLokiClient(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("newLokiClient() error = %v", err)
	}
	rec := &receiver{
		token: "token", requireToken: true, lokiURL: server.URL, lokiTenant: "dosh",
		lokiClient: client, seen: make(map[string]struct{}),
	}
	body := []byte(`{"timestamp":"2026-11-04T01:02:03.123456789Z","user":"alice","action":"reveal","resource":"secret","namespace":"prod","name":"db","result":"success","requestId":"req-loki"}`)
	request := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Request-ID", "req-loki")
	response := httptest.NewRecorder()
	rec.handleAudit(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(pushed.Streams) != 1 {
		t.Fatalf("streams = %#v", pushed.Streams)
	}
	stream := pushed.Streams[0]
	if stream.Stream["job"] != "dosh-audit" || stream.Stream["service"] != "dosh" || stream.Stream["result"] != "success" {
		t.Fatalf("labels = %#v", stream.Stream)
	}
	if len(stream.Values) != 1 || stream.Values[0][0] != "1793754123123456789" || stream.Values[0][1] != string(body) {
		t.Fatalf("values = %#v", stream.Values)
	}

	statsRecorder := httptest.NewRecorder()
	rec.handleStats(statsRecorder, httptest.NewRequest(http.MethodGet, "/stats", nil))
	var stats statsResponse
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Received != 1 || !stats.LokiConfigured {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestReceiverDoesNotAcknowledgeWhenLokiRejects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := newLokiClient(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("newLokiClient() error = %v", err)
	}
	rec := &receiver{
		token: "token", requireToken: true, lokiURL: server.URL, lokiTenant: "dosh",
		lokiClient: client, seen: make(map[string]struct{}),
	}
	request := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewBufferString(`{"user":"alice","action":"scale","result":"success","requestId":"req-rejected"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Request-ID", "req-rejected")
	response := httptest.NewRecorder()
	rec.handleAudit(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	statsRecorder := httptest.NewRecorder()
	rec.handleStats(statsRecorder, httptest.NewRequest(http.MethodGet, "/stats", nil))
	var stats statsResponse
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.Received != 0 || stats.Verified != 0 {
		t.Fatalf("stats = %#v", stats)
	}
}
