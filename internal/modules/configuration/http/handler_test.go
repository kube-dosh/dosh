package configurationhttp

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
	authhttp "dosh/internal/modules/auth/http"
	configurationapp "dosh/internal/modules/configuration/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const configurationTestPassword = "a-valid-test-password"

type configurationRepo struct {
	revealCalls int
	revealErr   error
	reveal      domain.SecretReveal
	configMaps  []domain.ConfigMapSummary
	secrets     []domain.SecretSummary
}

func (r *configurationRepo) ListConfigMaps(context.Context, domain.ListQuery) ([]domain.ConfigMapSummary, error) {
	return r.configMaps, nil
}

func (r *configurationRepo) GetConfigMap(context.Context, string, string) (domain.ConfigMapDetail, error) {
	return domain.ConfigMapDetail{}, nil
}
func (r *configurationRepo) CreateConfigMap(context.Context, domain.ConfigMapCreateSpec) (domain.ConfigMapDetail, error) {
	return domain.ConfigMapDetail{}, nil
}
func (r *configurationRepo) UpdateConfigMap(context.Context, string, string, domain.ConfigMapUpdateSpec) (domain.ConfigMapDetail, error) {
	return domain.ConfigMapDetail{}, nil
}
func (r *configurationRepo) DeleteConfigMap(context.Context, string, string, string) error {
	return nil
}

func (r *configurationRepo) ListSecrets(context.Context, domain.ListQuery) ([]domain.SecretSummary, error) {
	return r.secrets, nil
}

func (r *configurationRepo) RevealSecret(_ context.Context, namespace, name string) (domain.SecretReveal, error) {
	r.revealCalls++
	if r.revealErr != nil {
		return domain.SecretReveal{}, r.revealErr
	}
	value := r.reveal
	value.Namespace, value.Name = namespace, name
	return value, nil
}
func (r *configurationRepo) CreateSecret(context.Context, domain.SecretWriteSpec) (domain.SecretSummary, error) {
	return domain.SecretSummary{}, nil
}
func (r *configurationRepo) UpdateSecret(context.Context, string, string, domain.SecretWriteSpec) (domain.SecretSummary, error) {
	return domain.SecretSummary{}, nil
}
func (r *configurationRepo) DeleteSecret(context.Context, string, string, string) error {
	return nil
}

type configurationTestServer struct {
	router http.Handler
	repo   *configurationRepo
	logs   *bytes.Buffer
	cookie *http.Cookie
	csrf   string
}

func newConfigurationTestServer(t *testing.T, readOnly bool) *configurationTestServer {
	t.Helper()
	hash, err := authapp.HashPassword(configurationTestPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	manager, err := authapp.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 8*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	authorizer := authz.NewLocal(readOnly)
	sessions := authapp.NewSessionService(manager, authorizer, "local")
	authService := authapp.NewService(authapp.Config{Username: "admin", PasswordHash: hash, ReadOnly: readOnly}, sessions)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	authHandler := authhttp.New(authhttp.Options{Mode: "local", Local: authService, Sessions: sessions, Audit: audit.New(logger), Logger: logger, CookieSecure: true})
	repo := &configurationRepo{}
	service := configurationapp.New(repo, authorizer, audit.New(logger))
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.RequestIDContext)
	router.Route("/api/v1", func(r chi.Router) {
		authhttp.MountPublic(r, authHandler)
		r.Group(func(r chi.Router) {
			r.Use(authHandler.RequireSession)
			r.Use(authHandler.RequireCSRF)
			New(service).Mount(r)
		})
	})

	server := &configurationTestServer{router: router, repo: repo, logs: &logs}
	response := configurationRequest(router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"`+configurationTestPassword+`"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	server.cookie = response.Result().Cookies()[0]
	server.csrf = payload.CSRFToken
	return server
}

func configurationRequest(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func (s *configurationTestServer) authHeaders() map[string]string {
	return map[string]string{"Cookie": s.cookie.String(), "X-CSRF-Token": s.csrf}
}

func TestSecretRevealRequiresCSRFAndDisablesCaching(t *testing.T) {
	server := newConfigurationTestServer(t, false)
	server.repo.reveal = domain.SecretReveal{
		SecretSummary: domain.SecretSummary{Type: "Opaque", Keys: []domain.SecretKey{{Key: "token", Size: 6}}},
		Data:          map[string]string{"token": "secret"},
	}
	path := "/api/v1/secrets/prod/app/reveal"
	response := configurationRequest(server.router, http.MethodPost, path, "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", response.Code)
	}
	response = configurationRequest(server.router, http.MethodPost, path, "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.Code)
	}
	if server.repo.revealCalls != 0 {
		t.Fatal("repository was called without CSRF")
	}
	response = configurationRequest(server.router, http.MethodPost, path, "", server.authHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if !strings.Contains(response.Body.String(), `"token":"secret"`) {
		t.Fatalf("reveal body = %s", response.Body.String())
	}
	if server.repo.revealCalls != 1 {
		t.Fatalf("reveal calls = %d, want 1", server.repo.revealCalls)
	}
	for _, want := range []string{`"action":"reveal"`, `"result":"success"`, `"name":"app"`} {
		if !strings.Contains(server.logs.String(), want) {
			t.Fatalf("audit log %q does not contain %q", server.logs.String(), want)
		}
	}
}

func TestSecretRevealReadOnlyAndErrorMapping(t *testing.T) {
	readOnly := newConfigurationTestServer(t, true)
	response := configurationRequest(readOnly.router, http.MethodPost, "/api/v1/secrets/prod/app/reveal", "", readOnly.authHeaders())
	if response.Code != http.StatusForbidden {
		t.Fatalf("read-only reveal status = %d, want 403", response.Code)
	}
	if readOnly.repo.revealCalls != 0 {
		t.Fatal("read-only reveal reached the repository")
	}

	server := newConfigurationTestServer(t, false)
	server.repo.revealErr = domain.ErrNotFound
	response = configurationRequest(server.router, http.MethodPost, "/api/v1/secrets/prod/app/reveal", "", server.authHeaders())
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", response.Code)
	}
	if response.Header().Get("Cache-Control") != "" {
		t.Fatal("failed reveal must not add a success-only cache policy")
	}
}

func TestConfigurationListsReturnEmptyArrays(t *testing.T) {
	server := newConfigurationTestServer(t, false)
	for _, path := range []string{"/api/v1/configmaps", "/api/v1/secrets"} {
		response := configurationRequest(server.router, http.MethodGet, path, "", map[string]string{"Cookie": server.cookie.String()})
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"items":[]`) {
			t.Fatalf("%s body = %s, want empty array", path, response.Body.String())
		}
	}
}
