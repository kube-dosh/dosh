package releasecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckerReportsUpdateAvailableAndRedactsSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer update-token" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v1.2.4",
			"html_url":     "https://github.com/kube-dosh/dosh/releases/tag/v1.2.4?token=secret#notes",
			"published_at": "2026-11-20T10:00:00Z",
			"body":         "Important fixes",
		})
	}))
	defer server.Close()

	checker := New(Config{
		Enabled: true, URL: server.URL + "/latest?token=super-secret", Token: "update-token",
		Channel: "stable", CurrentVersion: "1.2.3", Timeout: time.Second,
	}, nil)
	status := checker.Status(context.Background(), false)
	if status.Status != "update_available" {
		t.Fatalf("Status = %q, want update_available: %#v", status.Status, status)
	}
	if status.LatestVersion != "1.2.4" {
		t.Fatalf("LatestVersion = %q, want 1.2.4", status.LatestVersion)
	}
	if status.LatestURL != "https://github.com/kube-dosh/dosh/releases/tag/v1.2.4" {
		t.Fatalf("LatestURL = %q, want sanitized URL", status.LatestURL)
	}
	if strings.Contains(status.Source, "super-secret") {
		t.Fatalf("Source leaked query token: %q", status.Source)
	}
	if status.Notes != "Important fixes" {
		t.Fatalf("Notes = %q", status.Notes)
	}
}

func TestCheckerStableChannelIgnoresPrerelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0-rc.1", "prerelease": true})
	}))
	defer server.Close()
	checker := New(Config{Enabled: true, URL: server.URL, Channel: "stable", CurrentVersion: "1.0.0", Timeout: time.Second}, nil)
	status := checker.Status(context.Background(), false)
	if status.Status != "current" {
		t.Fatalf("Status = %q, want current: %#v", status.Status, status)
	}
	if !strings.Contains(status.Message, "预发布") {
		t.Fatalf("Message = %q, want prerelease explanation", status.Message)
	}
}

func TestCheckerStableChannelIgnoresSemverPrereleaseWithoutFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0-rc.1"})
	}))
	defer server.Close()
	checker := New(Config{Enabled: true, URL: server.URL, Channel: "stable", CurrentVersion: "1.0.0", Timeout: time.Second}, nil)
	status := checker.Status(context.Background(), false)
	if status.Status != "current" {
		t.Fatalf("Status = %q, want current: %#v", status.Status, status)
	}
}

func TestCheckerReportsCurrentAndCachesResults(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.5.0"})
	}))
	defer server.Close()
	checker := New(Config{Enabled: true, URL: server.URL, Channel: "stable", CurrentVersion: "1.5.0", Timeout: time.Second, CacheTTL: time.Hour}, nil)
	for range 3 {
		if status := checker.Status(context.Background(), false); status.Status != "current" {
			t.Fatalf("Status = %q, want current", status.Status)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want one cached request", got)
	}
	_ = checker.Status(context.Background(), true)
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests after force = %d, want 2", got)
	}
}

func TestCheckerDisabledDoesNotCallSource(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	checker := New(Config{Enabled: false, URL: server.URL, CurrentVersion: "1.0.0"}, nil)
	status := checker.Status(context.Background(), false)
	if status.Status != "disabled" || status.Enabled {
		t.Fatalf("Status = %#v, want disabled", status)
	}
	if requests.Load() != 0 {
		t.Fatal("disabled update checker contacted the update source")
	}
}

func TestCheckerHandlesSourceFailuresAndOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error":
			w.WriteHeader(http.StatusBadGateway)
		case "/invalid":
			_, _ = w.Write([]byte("not-json"))
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat("x", maxReleaseBodyBytes+1)))
		case "/nosemver":
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "nightly"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "1.0.0"})
		}
	}))
	defer server.Close()
	for _, test := range []struct {
		path       string
		wantStatus string
	}{
		{path: "/error", wantStatus: "error"},
		{path: "/invalid", wantStatus: "error"},
		{path: "/large", wantStatus: "error"},
		{path: "/nosemver", wantStatus: "unknown"},
	} {
		t.Run(test.path, func(t *testing.T) {
			checker := New(Config{Enabled: true, URL: server.URL + test.path, Channel: "stable", CurrentVersion: "1.0.0", Timeout: time.Second}, nil)
			status := checker.Status(context.Background(), true)
			if status.Status != test.wantStatus {
				t.Fatalf("Status = %q, want %q: %#v", status.Status, test.wantStatus, status)
			}
			if status.Message == "" {
				t.Fatal("Message is empty, want operator-facing explanation")
			}
		})
	}
}
