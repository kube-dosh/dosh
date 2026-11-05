package httpx

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	"dosh/internal/platform/requestid"
)

type testPayload struct {
	Name string `json:"name"`
}

func TestDecodeJSONRejectsOversizedBodyWith413(t *testing.T) {
	SetMaxBodyBytes(16)
	t.Cleanup(func() { SetMaxBodyBytes(2 * 1024 * 1024) })

	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"name":"a value that is too large"}`))
	request = request.WithContext(requestid.With(request.Context(), "req-413"))
	response := httptest.NewRecorder()

	var payload testPayload
	if DecodeJSON(response, request, &payload) {
		t.Fatal("DecodeJSON() accepted an oversized body")
	}
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"request_too_large"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"requestId":"req-413"`) {
		t.Fatalf("request id missing from body: %s", response.Body.String())
	}
}

func TestDecodeJSONRejectsUnknownAndMultipleValues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"name":"ok","extra":true}`},
		{name: "multiple values", body: `{"name":"first"}{"name":"second"}`},
		{name: "empty body", body: ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			var payload testPayload
			if DecodeJSON(response, request, &payload) {
				t.Fatal("DecodeJSON() accepted an invalid body")
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", response.Code)
			}
			if !strings.Contains(response.Body.String(), `"code":"validation_failed"`) {
				t.Fatalf("body = %s", response.Body.String())
			}
		})
	}
}

func TestErrorMapsStableProblemCodes(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "validation", err: &domain.ValidationError{Field: "replicas", Message: "required"}, status: http.StatusBadRequest, code: "validation_failed"},
		{name: "invalid", err: domain.ErrInvalid, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "unauthorized", err: domain.ErrUnauthorized, status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "forbidden", err: domain.ErrForbidden, status: http.StatusForbidden, code: "forbidden"},
		{name: "read only", err: domain.ErrReadOnly, status: http.StatusForbidden, code: "forbidden"},
		{name: "not found", err: domain.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "conflict", err: domain.ErrConflict, status: http.StatusConflict, code: "conflict"},
		{name: "already exists", err: domain.ErrAlreadyExists, status: http.StatusConflict, code: "already_exists"},
		{name: "unavailable", err: domain.ErrUnavailable, status: http.StatusServiceUnavailable, code: "dependency_unavailable"},
		{name: "unsupported", err: domain.ErrUnsupported, status: http.StatusNotImplemented, code: "not_supported"},
		{name: "rate limited", err: domain.ErrRateLimited, status: http.StatusTooManyRequests, code: "rate_limited"},
		{name: "internal", err: errors.New("database details must not leak"), status: http.StatusInternalServerError, code: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
			request = request.WithContext(requestid.With(request.Context(), "req-error"))
			response := httptest.NewRecorder()
			Error(response, request, tt.err)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			if !strings.Contains(response.Body.String(), `"code":"`+tt.code+`"`) {
				t.Fatalf("body = %s, want code %s", response.Body.String(), tt.code)
			}
			if !strings.Contains(response.Body.String(), `"requestId":"req-error"`) {
				t.Fatalf("request id missing: %s", response.Body.String())
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	for _, header := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Cross-Origin-Opener-Policy",
		"Cross-Origin-Resource-Policy",
		"Referrer-Policy",
		"Permissions-Policy",
	} {
		if response.Header().Get(header) == "" {
			t.Fatalf("security header %s is missing", header)
		}
	}
	if response.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS must not be emitted for plain HTTP")
	}

	tlsRequest := httptest.NewRequest(http.MethodGet, "https://dosh.example.test/api/v1/overview", nil)
	tlsRequest.TLS = &tls.ConnectionState{}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, tlsRequest)
	if response.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS is missing for TLS requests")
	}
}

func TestNoStoreAPIOnlyAppliesToAPIRoutes(t *testing.T) {
	handler := NoStoreAPI(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/api/v1/session", want: "no-store"},
		{path: "/assets/app.js", want: ""},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if got := response.Header().Get("Cache-Control"); got != test.want {
			t.Fatalf("Cache-Control for %s = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestSameOriginRejectsCrossSiteUnsafeRequests(t *testing.T) {
	handler := SameOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		name   string
		method string
		origin string
		fetch  string
		status int
	}{
		{name: "same origin post", method: http.MethodPost, origin: "https://dosh.example.test", status: http.StatusNoContent},
		{name: "cross origin post", method: http.MethodPost, origin: "https://evil.example.test", status: http.StatusForbidden},
		{name: "opaque origin post", method: http.MethodPost, origin: "null", status: http.StatusForbidden},
		{name: "malformed origin post", method: http.MethodPost, origin: "://bad", status: http.StatusForbidden},
		{name: "cross-site fetch metadata", method: http.MethodPost, fetch: "cross-site", status: http.StatusForbidden},
		{name: "same-origin fetch metadata", method: http.MethodPost, origin: "https://dosh.example.test", fetch: "same-origin", status: http.StatusNoContent},
		{name: "cli post without origin metadata", method: http.MethodPost, status: http.StatusNoContent},
		{name: "cross-origin read remains allowed", method: http.MethodGet, origin: "https://evil.example.test", status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "https://dosh.example.test/api/v1/session", nil)
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			if tt.fetch != "" {
				request.Header.Set("Sec-Fetch-Site", tt.fetch)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.status, response.Body.String())
			}
		})
	}
}

func TestAccessLogIncludesRequestID(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(RequestIDContext(AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestid.From(r.Context()) == "" {
			t.Fatal("request id missing from context")
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID response header is missing")
	}
	for _, want := range []string{`"msg":"http_request"`, `"request_id"`, `"status":200`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("log output %q does not contain %q", output.String(), want)
		}
	}
}

func TestParseListOptionsValidatesPaginationAndSorting(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/pods?page=2&pageSize=2&sort=name&order=desc", nil)
	options, err := ParseListOptions(request, []string{"name"})
	if err != nil {
		t.Fatalf("ParseListOptions() error = %v", err)
	}
	if options.Page != 2 || options.PageSize != 2 || options.Sort != "name" || options.Order != "desc" {
		t.Fatalf("options = %#v", options)
	}

	for _, target := range []string{
		"/api/v1/pods?sort=unsupported",
		"/api/v1/pods?page=0",
		"/api/v1/pods?pageSize=201",
		"/api/v1/pods?order=desc",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		if _, err := ParseListOptions(request, []string{"name"}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ParseListOptions(%s) error = %v, want validation failure", target, err)
		}
	}
}

func TestPageItemsReturnsStablePaginationAndFilterMetadata(t *testing.T) {
	items := []testPayload{{Name: "zeta"}, {Name: "alpha"}, {Name: "middle"}}
	options := ListOptions{Page: 2, PageSize: 1, Sort: "name", Order: "asc"}
	page, meta := PageItems(items, options, map[string]string{"namespace": "prod"})
	if len(page) != 1 || page[0].Name != "middle" {
		t.Fatalf("page = %#v", page)
	}
	if meta.Total != 3 || meta.Page != 2 || meta.PageSize != 1 || meta.TotalPages != 3 || meta.Sort != "name" || meta.Order != "asc" {
		t.Fatalf("meta = %#v", meta)
	}
	if meta.Filters["namespace"] != "prod" {
		t.Fatalf("filters = %#v", meta.Filters)
	}
}
