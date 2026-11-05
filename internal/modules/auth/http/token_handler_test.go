package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	authapp "dosh/internal/modules/auth/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type tokenHTTPAuthenticator struct {
	actor domain.Actor
	token string
}

func (a *tokenHTTPAuthenticator) AuthenticateToken(_ context.Context, token string) (domain.Actor, error) {
	if token != a.token {
		return domain.Actor{}, domain.ErrUnauthorized
	}
	return a.actor, nil
}

type tokenHTTPAuthorizer struct{}

func (tokenHTTPAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return nil }

func newTokenHTTPTestServer(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	manager, err := authapp.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	sessions := authapp.NewSessionService(manager, tokenHTTPAuthorizer{}, "token")
	authenticator := &tokenHTTPAuthenticator{
		actor: domain.Actor{Username: "alice@example.test", Groups: []string{"dosh-operators"}},
		token: "valid-kubernetes-token",
	}
	tokenService := authapp.NewKubernetesTokenService(authenticator, sessions, nil)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(Options{
		Mode: "token", ProviderName: "Kubernetes", Token: tokenService, Sessions: sessions,
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
	return router, &logs
}

func TestTokenLoginSetsSessionAndReturnsIdentity(t *testing.T) {
	router, logs := newTokenHTTPTestServer(t)
	configResponse := performJSON(router, http.MethodGet, "/api/v1/auth/config", "", nil)
	if configResponse.Code != http.StatusOK || !strings.Contains(configResponse.Body.String(), `"mode":"token"`) {
		t.Fatalf("config status = %d body = %s", configResponse.Code, configResponse.Body.String())
	}
	response := performJSON(router, http.MethodPost, "/api/v1/session", `{"token":"valid-kubernetes-token"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("token login status = %d body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"authMode":"token"`) || !strings.Contains(response.Body.String(), `"username":"alice@example.test"`) {
		t.Fatalf("token login body = %s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("token login cookies = %#v", cookies)
	}
	var payload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.CSRFToken == "" {
		t.Fatalf("decode CSRF token: %v body=%s", err, response.Body.String())
	}
	session := performJSON(router, http.MethodGet, "/api/v1/session", "", map[string]string{"Cookie": cookies[0].String()})
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"groups":["dosh-operators"]`) {
		t.Fatalf("session status = %d body = %s", session.Code, session.Body.String())
	}
	if !strings.Contains(logs.String(), `"user":"alice@example.test"`) || strings.Contains(logs.String(), "valid-kubernetes-token") {
		t.Fatalf("token login audit is incorrect: %s", logs.String())
	}
}

func TestTokenLoginRejectsInvalidAndBlankTokens(t *testing.T) {
	router, _ := newTokenHTTPTestServer(t)
	for _, body := range []string{`{"token":"wrong"}`, `{"token":""}`, `{}`} {
		response := performJSON(router, http.MethodPost, "/api/v1/session", body, nil)
		if response.Code != http.StatusUnauthorized && response.Code != http.StatusBadRequest {
			t.Fatalf("token login status = %d body = %s", response.Code, response.Body.String())
		}
		if len(response.Result().Cookies()) != 0 {
			t.Fatalf("failed token login set cookies for %s", body)
		}
	}
}
