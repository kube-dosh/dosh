package authhttp

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	authapp "dosh/internal/modules/auth/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type oidcHTTPProvider struct {
	state    string
	identity authapp.OIDCIdentity
}

func (p *oidcHTTPProvider) AuthCodeURL(state, _, _ string) string {
	p.state = state
	return "https://idp.example.test/authorize?state=" + url.QueryEscape(state)
}

func (p *oidcHTTPProvider) Exchange(context.Context, string, string, string) (authapp.OIDCIdentity, error) {
	return p.identity, nil
}

func (p *oidcHTTPProvider) EndSessionURL(redirect string) string {
	return "https://idp.example.test/logout?redirect=" + url.QueryEscape(redirect)
}

type oidcHTTPAuthorizer struct{}

func (oidcHTTPAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return nil }

type oidcTestServer struct {
	router   http.Handler
	provider *oidcHTTPProvider
	logs     *bytes.Buffer
}

func newOIDCHTTPTestServer(t *testing.T) oidcTestServer {
	t.Helper()
	manager, err := authapp.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := authapp.NewSessionService(manager, oidcHTTPAuthorizer{}, "oidc")
	states, err := authapp.NewOIDCStateManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewOIDCStateManager() error = %v", err)
	}
	provider := &oidcHTTPProvider{identity: authapp.OIDCIdentity{
		Subject: "subject-1", Username: "alice@example.test", Groups: []string{"viewers"}, ExpiresAt: time.Now().Add(time.Hour),
	}}
	oidcService := authapp.NewOIDCService(provider, sessions, states, authapp.OIDCConfig{SessionTTL: time.Hour})
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(Options{
		Mode: "oidc", ProviderName: "idp.example.test", OIDC: oidcService, Sessions: sessions,
		Audit: audit.New(logger), Logger: logger, CookieSecure: true,
	})
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.RequestIDContext)
	router.Route("/api/v1", func(r chi.Router) {
		MountPublic(r, handler)
		r.Group(func(r chi.Router) {
			r.Use(handler.RequireSession)
			r.Use(handler.RequireCSRF)
			MountProtected(r, handler)
		})
	})
	return oidcTestServer{router: router, provider: provider, logs: &logs}
}

func TestOIDCHTTPFlowSetsSessionAndPreservesReturnTo(t *testing.T) {
	server := newOIDCHTTPTestServer(t)
	configResponse := performJSON(server.router, http.MethodGet, "/api/v1/auth/config", "", nil)
	if configResponse.Code != http.StatusOK || !strings.Contains(configResponse.Body.String(), `"mode":"oidc"`) {
		t.Fatalf("config status = %d body = %s", configResponse.Code, configResponse.Body.String())
	}

	login := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?returnTo=%2Fpods%3Fnamespace%3Ddefault", nil)
	loginResponse := httptest.NewRecorder()
	server.router.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusFound {
		t.Fatalf("login status = %d body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	var stateCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == authapp.OIDCStateCookieName {
			stateCookie = cookie
		}
	}
	if stateCookie == nil || !stateCookie.HttpOnly || stateCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie = %#v", stateCookie)
	}

	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=code-1&state="+url.QueryEscape(server.provider.state), nil)
	callback.AddCookie(stateCookie)
	callbackResponse := httptest.NewRecorder()
	server.router.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusFound || callbackResponse.Header().Get("Location") != "/pods?namespace=default" {
		t.Fatalf("callback status = %d location = %q", callbackResponse.Code, callbackResponse.Header().Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == authapp.CookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", sessionCookie)
	}

	sessionResponse := performJSON(server.router, http.MethodGet, "/api/v1/session", "", map[string]string{"Cookie": sessionCookie.String()})
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status = %d body = %s", sessionResponse.Code, sessionResponse.Body.String())
	}
	body := sessionResponse.Body.String()
	if !strings.Contains(body, `"authMode":"oidc"`) || !strings.Contains(body, `"username":"alice@example.test"`) || !strings.Contains(body, `"scale":true`) {
		t.Fatalf("session body = %s", body)
	}
	if !strings.Contains(server.logs.String(), `"user":"alice@example.test","groups":["viewers"]`) {
		t.Fatalf("OIDC login audit is missing actor groups: %s", server.logs.String())
	}
}

func TestOIDCCallbackRejectsMismatchedState(t *testing.T) {
	server := newOIDCHTTPTestServer(t)
	login := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
	loginResponse := httptest.NewRecorder()
	server.router.ServeHTTP(loginResponse, login)
	var stateCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == authapp.OIDCStateCookieName {
			stateCookie = cookie
		}
	}
	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=code-1&state=wrong", nil)
	callback.AddCookie(stateCookie)
	callbackResponse := httptest.NewRecorder()
	server.router.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusFound || callbackResponse.Header().Get("Location") != "/login?error=oidc" {
		t.Fatalf("callback status = %d location = %q", callbackResponse.Code, callbackResponse.Header().Get("Location"))
	}
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == authapp.CookieName && cookie.Value != "" {
			t.Fatal("failed OIDC callback created a session cookie")
		}
	}
}

func TestOIDCLogoutRequiresAuthenticatedSession(t *testing.T) {
	server := newOIDCHTTPTestServer(t)
	response := performJSON(server.router, http.MethodGet, "/api/v1/auth/oidc/logout", "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("logout without session status = %d, want 401; body = %s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("unauthenticated logout must not clear cookies: %#v", cookies)
	}
}
