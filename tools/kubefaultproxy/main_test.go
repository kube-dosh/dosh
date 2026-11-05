package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFaultProxyInjectsThenPassesThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream"))
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	proxy := newFaultProxy(target)
	proxy.fault = faultConfig{
		Method:       http.MethodGet,
		PathContains: "/api/v1/nodes",
		Status:       http.StatusTooManyRequests,
		Remaining:    2,
		RetryAfter:   "1",
	}

	statuses := make([]int, 0, 3)
	for range 3 {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		statuses = append(statuses, response.Code)
	}
	if statuses[0] != http.StatusTooManyRequests || statuses[1] != http.StatusTooManyRequests || statuses[2] != http.StatusOK {
		t.Fatalf("statuses = %v", statuses)
	}
	proxy.mu.Lock()
	got := proxy.counts
	proxy.mu.Unlock()
	if got[statKey{Method: http.MethodGet, Path: "/api/v1/nodes", Status: http.StatusTooManyRequests}] != 2 {
		t.Fatalf("injected 429 count = %d", got[statKey{Method: http.MethodGet, Path: "/api/v1/nodes", Status: http.StatusTooManyRequests}])
	}
	if got[statKey{Method: http.MethodGet, Path: "/api/v1/nodes", Status: http.StatusOK}] != 1 {
		t.Fatalf("upstream 200 count = %d", got[statKey{Method: http.MethodGet, Path: "/api/v1/nodes", Status: http.StatusOK}])
	}
}

func TestFaultProxyOnlyMatchesConfiguredMethod(t *testing.T) {
	proxy := &faultProxy{counts: make(map[statKey]int)}
	proxy.fault = faultConfig{Method: http.MethodPut, PathContains: "/scale", Status: http.StatusServiceUnavailable, Remaining: 1}
	if _, ok := proxy.takeFault(http.MethodGet, "/apis/apps/v1/deployments/demo/scale"); ok {
		t.Fatal("GET matched a PUT-only fault")
	}
	if _, ok := proxy.takeFault(http.MethodPut, "/apis/apps/v1/deployments/demo/scale"); !ok {
		t.Fatal("PUT did not match configured fault")
	}
	if _, ok := proxy.takeFault(http.MethodPut, "/apis/apps/v1/deployments/demo/scale"); ok {
		t.Fatal("fault was applied after Remaining reached zero")
	}
}
