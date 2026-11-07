package yamlhttp

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
	yamlapp "dosh/internal/modules/yaml/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const yamlTestPassword = "a-valid-test-password"

type yamlRepo struct {
	getCalls   int
	exists     bool
	existsErr  error
	applyCalls int
	lastApply  domain.ApplyRequest
	getErr     error
	applyErr   error
}

func (r *yamlRepo) Exists(context.Context, domain.ResourceType, string, string) (bool, error) {
	return r.exists, r.existsErr
}

func (r *yamlRepo) GetYAML(_ context.Context, resourceType domain.ResourceType, namespace, name string) (domain.YAMLResource, error) {
	r.getCalls++
	if r.getErr != nil {
		return domain.YAMLResource{}, r.getErr
	}
	return domain.YAMLResource{Kind: string(resourceType), Namespace: namespace, Name: name, YAML: "apiVersion: v1\nkind: ConfigMap\n"}, nil
}

func (r *yamlRepo) ApplyYAML(_ context.Context, request domain.ApplyRequest) (domain.YAMLResource, error) {
	r.applyCalls++
	r.lastApply = request
	if r.applyErr != nil {
		return domain.YAMLResource{}, r.applyErr
	}
	return domain.YAMLResource{Kind: "ConfigMap", Namespace: "prod", Name: "app", YAML: request.YAML}, nil
}

type yamlTestServer struct {
	router http.Handler
	repo   *yamlRepo
	logs   *bytes.Buffer
	cookie *http.Cookie
	csrf   string
}

func newYAMLTestServer(t *testing.T, readOnly bool) *yamlTestServer {
	t.Helper()
	hash, err := authapp.HashPassword(yamlTestPassword)
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
	repo := &yamlRepo{}
	service := yamlapp.New(repo, authorizer, audit.New(logger))
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

	server := &yamlTestServer{router: router, repo: repo, logs: &logs}
	response := yamlRequest(router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"`+yamlTestPassword+`"}`, nil)
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

func yamlRequest(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func (s *yamlTestServer) authHeaders() map[string]string {
	return map[string]string{"Cookie": s.cookie.String(), "X-CSRF-Token": s.csrf}
}

func TestYAMLGetValidatesResourceType(t *testing.T) {
	server := newYAMLTestServer(t, false)
	response := yamlRequest(server.router, http.MethodGet, "/api/v1/yaml?type=not-real&namespace=prod&name=app", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusBadRequest || server.repo.getCalls != 0 {
		t.Fatalf("status = %d calls = %d", response.Code, server.repo.getCalls)
	}
	response = yamlRequest(server.router, http.MethodGet, "/api/v1/yaml?type=configmap&namespace=prod&name=app", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusOK || server.repo.getCalls != 1 {
		t.Fatalf("status = %d calls = %d body = %s", response.Code, server.repo.getCalls, response.Body.String())
	}
}

func TestYAMLApplyDryRunAndCommit(t *testing.T) {
	server := newYAMLTestServer(t, false)
	dryRunBody := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n","dryRun":true}`
	response := yamlRequest(server.router, http.MethodPost, "/api/v1/yaml/apply", dryRunBody, server.authHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("dry-run status = %d, body = %s", response.Code, response.Body.String())
	}
	if server.repo.applyCalls != 1 || !server.repo.lastApply.DryRun {
		t.Fatalf("dry-run calls = %d request = %#v", server.repo.applyCalls, server.repo.lastApply)
	}
	commitBody := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n","dryRun":false,"resourceVersion":"7"}`
	response = yamlRequest(server.router, http.MethodPost, "/api/v1/yaml/apply", commitBody, server.authHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("commit status = %d, body = %s", response.Code, response.Body.String())
	}
	if server.repo.applyCalls != 3 || server.repo.lastApply.DryRun {
		t.Fatalf("commit calls = %d request = %#v", server.repo.applyCalls, server.repo.lastApply)
	}
	if server.repo.lastApply.ResourceVersion != "7" {
		t.Fatalf("resourceVersion = %q", server.repo.lastApply.ResourceVersion)
	}
	if !strings.Contains(server.logs.String(), `"action":"apply"`) || !strings.Contains(server.logs.String(), `"action":"dry-run"`) {
		t.Fatalf("apply audit events missing: %s", server.logs.String())
	}
}

func TestYAMLApplyGuardsAndErrors(t *testing.T) {
	server := newYAMLTestServer(t, false)
	body := `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n","dryRun":true}`
	response := yamlRequest(server.router, http.MethodPost, "/api/v1/yaml/apply", body, map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusForbidden || server.repo.applyCalls != 0 {
		t.Fatalf("missing CSRF status = %d calls = %d", response.Code, server.repo.applyCalls)
	}
	server.repo.applyErr = domain.ErrUnsupported
	response = yamlRequest(server.router, http.MethodPost, "/api/v1/yaml/apply", body, server.authHeaders())
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported status = %d, body = %s", response.Code, response.Body.String())
	}
	response = yamlRequest(server.router, http.MethodPost, "/api/v1/yaml/apply", `{"yaml":`, server.authHeaders())
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", response.Code)
	}

	readOnly := newYAMLTestServer(t, true)
	response = yamlRequest(readOnly.router, http.MethodPost, "/api/v1/yaml/apply", body, readOnly.authHeaders())
	if response.Code != http.StatusForbidden || readOnly.repo.applyCalls != 0 {
		t.Fatalf("read-only status = %d calls = %d", response.Code, readOnly.repo.applyCalls)
	}
}
