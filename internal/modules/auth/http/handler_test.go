package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	authapp "dosh/internal/modules/auth/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const testPassword = "a-valid-test-password"

type loginResponse struct {
	CSRFToken    string              `json:"csrfToken"`
	Capabilities domain.Capabilities `json:"capabilities"`
}

type authTestServer struct {
	router  http.Handler
	handler *Handler
	logs    *bytes.Buffer
}

func newAuthTestServer(t *testing.T, readOnly bool) authTestServer {
	t.Helper()
	return newAuthTestServerWithAuthorizer(t, authz.NewLocal(readOnly))
}

func newAuthTestServerWithAuthorizer(t *testing.T, authorizer domain.Authorizer) authTestServer {
	t.Helper()
	hash, err := authapp.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	manager, err := authapp.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 8*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := authapp.NewSessionService(manager, authorizer, "local")
	service := authapp.NewService(authapp.Config{Username: "admin", PasswordHash: hash}, sessions)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(Options{Mode: "local", Local: service, Sessions: sessions, Audit: audit.New(logger), Logger: logger, CookieSecure: true})
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.RequestIDContext)
	router.Route("/api/v1", func(r chi.Router) {
		MountPublic(r, handler)
		r.Group(func(r chi.Router) {
			r.Use(handler.RequireSession)
			r.Use(handler.RequireCSRF)
			MountProtected(r, handler)
			r.Post("/probe", func(w http.ResponseWriter, r *http.Request) {
				actor, _ := domain.ActorFromContext(r.Context())
				httpx.JSON(w, http.StatusOK, actor)
			})
		})
	})
	return authTestServer{router: router, handler: handler, logs: &logs}
}

func performJSON(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func login(t *testing.T, server authTestServer) (*http.Cookie, loginResponse) {
	t.Helper()
	response := performJSON(server.router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"`+testPassword+`"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload loginResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	return cookies[0], payload
}

func TestLoginSetsHardenedCookieAndCapabilities(t *testing.T) {
	server := newAuthTestServer(t, false)
	cookie, payload := login(t, server)
	if cookie.Name != authapp.CookieName {
		t.Fatalf("cookie name = %q", cookie.Name)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie attributes are not hardened: %#v", cookie)
	}
	if cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("cookie scope/expiry = path %q maxAge %d", cookie.Path, cookie.MaxAge)
	}
	if payload.CSRFToken == "" {
		t.Fatal("csrf token is empty")
	}
	if !payload.Capabilities.Scale || !payload.Capabilities.UpdateImage || !payload.Capabilities.RevealSecrets || !payload.Capabilities.EditYAML || !payload.Capabilities.NodeCordon {
		t.Fatalf("admin capabilities = %#v", payload.Capabilities)
	}
	if !strings.Contains(server.logs.String(), `"action":"login"`) || !strings.Contains(server.logs.String(), `"result":"success"`) || !strings.Contains(server.logs.String(), `"groups":["dosh-admins"]`) {
		t.Fatalf("login audit event missing: %s", server.logs.String())
	}
}

func TestLoginFailureDoesNotRevealCauseOrSetCookie(t *testing.T) {
	server := newAuthTestServer(t, false)
	for _, body := range []string{
		`{"username":"admin","password":"wrong password"}`,
		`{"username":"missing","password":"wrong password"}`,
	} {
		response := performJSON(server.router, http.MethodPost, "/api/v1/session", body, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
		if len(response.Result().Cookies()) != 0 {
			t.Fatal("failed login set a session cookie")
		}
		if !strings.Contains(response.Body.String(), `"message":"authentication required"`) {
			t.Fatalf("unexpected login error body: %s", response.Body.String())
		}
	}
}

func TestLoginValidationAndRateLimit(t *testing.T) {
	server := newAuthTestServer(t, false)
	response := performJSON(server.router, http.MethodPost, "/api/v1/session", `{"username":"admin"}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing password status = %d, want 400", response.Code)
	}
	response = performJSON(server.router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"x","extra":true}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", response.Code)
	}
	for range 5 {
		_ = performJSON(server.router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"wrong password"}`, nil)
	}
	response = performJSON(server.router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"wrong password"}`, nil)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status = %d, want 429", response.Code)
	}
}

func TestRequireSessionAndCSRF(t *testing.T) {
	server := newAuthTestServer(t, false)
	response := performJSON(server.router, http.MethodGet, "/api/v1/session", "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", response.Code)
	}

	cookie, payload := login(t, server)
	cookieHeader := map[string]string{"Cookie": cookie.String()}
	response = performJSON(server.router, http.MethodGet, "/api/v1/session", "", cookieHeader)
	if response.Code != http.StatusOK {
		t.Fatalf("valid session status = %d, body = %s", response.Code, response.Body.String())
	}

	response = performJSON(server.router, http.MethodPost, "/api/v1/probe", "", cookieHeader)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.Code)
	}
	response = performJSON(server.router, http.MethodPost, "/api/v1/probe", "", map[string]string{
		"Cookie": cookie.String(), "X-CSRF-Token": "wrong",
	})
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong CSRF status = %d, want 403", response.Code)
	}
	response = performJSON(server.router, http.MethodPost, "/api/v1/probe", "", map[string]string{
		"Cookie": cookie.String(), "X-CSRF-Token": payload.CSRFToken,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("valid CSRF status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"username":"admin"`) {
		t.Fatalf("actor was not propagated: %s", response.Body.String())
	}
}

func TestLogoutClearsCookieAndAudits(t *testing.T) {
	server := newAuthTestServer(t, false)
	cookie, payload := login(t, server)
	response := performJSON(server.router, http.MethodDelete, "/api/v1/session", "", map[string]string{
		"Cookie": cookie.String(), "X-CSRF-Token": payload.CSRFToken,
	})
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 || cookies[0].Value != "" {
		t.Fatalf("logout did not clear cookie: %#v", cookies)
	}
	if !strings.Contains(server.logs.String(), `"action":"logout"`) {
		t.Fatalf("logout audit missing: %s", server.logs.String())
	}
}

func TestReadOnlyCapabilitiesAndSessionTampering(t *testing.T) {
	server := newAuthTestServer(t, true)
	cookie, payload := login(t, server)
	if payload.Capabilities.Scale || payload.Capabilities.UpdateImage || payload.Capabilities.Delete || payload.Capabilities.RevealSecrets || payload.Capabilities.NodeCordon {
		t.Fatalf("read-only capabilities = %#v", payload.Capabilities)
	}
	response := performJSON(server.router, http.MethodGet, "/api/v1/session", "", map[string]string{
		"Cookie": authapp.CookieName + "=" + cookie.Value + "x",
	})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("tampered session status = %d, want 401", response.Code)
	}
}

type namespaceCapabilityAuthorizer struct{ namespace string }

func (a namespaceCapabilityAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	if access.Namespace == a.namespace {
		return nil
	}
	return domain.ErrForbidden
}

func TestCapabilitiesRouteUsesSessionActorAndNamespace(t *testing.T) {
	server := newAuthTestServerWithAuthorizer(t, namespaceCapabilityAuthorizer{namespace: "team-a"})
	cookie, _ := login(t, server)
	headers := map[string]string{"Cookie": cookie.String()}

	scoped := performJSON(server.router, http.MethodGet, "/api/v1/capabilities?namespace=team-a", "", headers)
	if scoped.Code != http.StatusOK {
		t.Fatalf("scoped capabilities status = %d, body = %s", scoped.Code, scoped.Body.String())
	}
	var capabilities domain.Capabilities
	if err := json.Unmarshal(scoped.Body.Bytes(), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if !capabilities.Read || !capabilities.Scale || !capabilities.Restart || !capabilities.UpdateImage || !capabilities.Delete ||
		!capabilities.Create || !capabilities.RevealSecrets || !capabilities.EditYAML {
		t.Fatalf("scoped capabilities = %#v", capabilities)
	}

	unscoped := performJSON(server.router, http.MethodGet, "/api/v1/capabilities?namespace=team-b", "", headers)
	if unscoped.Code != http.StatusOK {
		t.Fatalf("unscoped capabilities status = %d, body = %s", unscoped.Code, unscoped.Body.String())
	}
	if err := json.Unmarshal(unscoped.Body.Bytes(), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if capabilities.Read || capabilities.Scale || capabilities.Delete || capabilities.RevealSecrets {
		t.Fatalf("outer-namespace capabilities = %#v", capabilities)
	}
}
