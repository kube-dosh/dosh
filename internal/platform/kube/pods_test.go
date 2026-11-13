package kube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"dosh/internal/domain"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestStreamPodLogsForwardsOptionsAndMapsFailures(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/prod/pods/api/log" {
			t.Fatalf("unexpected log path %q", r.URL.Path)
		}
		received = r.URL.Query()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "line one\nline two\n")
	}))
	defer server.Close()
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}
	client := NewForClients(clientset, nil, nil, testConfig())
	stream, err := client.StreamPodLogs(context.Background(), "prod", "api", "app", true, false, 25, true, 0)
	if err != nil {
		t.Fatalf("StreamPodLogs() error = %v", err)
	}
	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	if string(body) != "line one\nline two\n" {
		t.Fatalf("stream body = %q", body)
	}
	if received.Get("container") != "app" || received.Get("follow") != "true" || received.Get("tailLines") != "25" || received.Get("timestamps") != "true" || received.Has("limitBytes") {
		t.Fatalf("log query = %#v", received)
	}

	stream, err = client.StreamPodLogs(context.Background(), "prod", "api", "app", false, false, 25, false, 1<<20)
	if err != nil {
		t.Fatalf("StreamPodLogs(limit) error = %v", err)
	}
	_, _ = io.Copy(io.Discard, stream)
	_ = stream.Close()
	if received.Get("limitBytes") != "1048576" {
		t.Fatalf("non-stream log query = %#v", received)
	}

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"forbidden","reason":"Forbidden","code":403}`)
	}))
	defer forbidden.Close()
	clientset, err = kubernetes.NewForConfig(&rest.Config{Host: forbidden.URL})
	if err != nil {
		t.Fatalf("NewForConfig(forbidden) error = %v", err)
	}
	client = NewForClients(clientset, nil, nil, testConfig())
	if _, err := client.StreamPodLogs(context.Background(), "prod", "api", "app", false, false, 0, false, 1<<20); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("StreamPodLogs() forbidden error = %v", err)
	}
}
