package registryclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

func TestClientSupportsAnonymousBearerAndPagination(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("scope") == "" {
				http.Error(w, "missing scope", http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprint(w, `{"token":"registry-token","expires_in":300}`)
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/_catalog":
			if r.Header.Get("Authorization") != "Bearer registry-token" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, server.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("last") == "" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/v2/_catalog?n=1000&last=team/app>; rel="next"`, server.URL))
				_, _ = fmt.Fprint(w, `{"repositories":["team/app","team/worker"]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"repositories":["team/zeta"]}`)
		case "/v2/team/app/tags/list":
			if r.Header.Get("Authorization") != "Bearer registry-token" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, server.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = fmt.Fprint(w, `{"name":"team/app","tags":["1.0.0","1.1.0"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := New(5 * time.Second)
	registry := domain.ContainerRegistry{Endpoint: server.URL, AuthType: domain.RegistryAuthAnonymous}
	capabilities, err := client.Test(context.Background(), registry)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if !capabilities.Catalog || !capabilities.Tags {
		t.Fatalf("Test() capabilities = %#v", capabilities)
	}
	repositories, err := client.Repositories(context.Background(), registry)
	if err != nil {
		t.Fatalf("Repositories() error = %v", err)
	}
	if strings.Join(repositories, ",") != "team/app,team/worker,team/zeta" {
		t.Fatalf("Repositories() = %#v", repositories)
	}
	tags, err := client.Tags(context.Background(), registry, "team/app")
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}
	if strings.Join(tags, ",") != "1.0.0,1.1.0" {
		t.Fatalf("Tags() = %#v", tags)
	}
}

func TestClientUsesBasicAuthentication(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/_catalog" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"repositories":[]}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := New(5 * time.Second)
	registry := domain.ContainerRegistry{
		Endpoint: server.URL, AuthType: domain.RegistryAuthBasic,
		Credentials: domain.ContainerRegistryCredentials{Username: "user", Password: "secret"},
	}
	if _, err := client.Test(context.Background(), registry); err != nil {
		t.Fatalf("Test() error = %v", err)
	}
}

func TestClientTestFailsWhenCatalogIsUnavailable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "catalog unavailable", http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(5 * time.Second)
	registry := domain.ContainerRegistry{Endpoint: server.URL, AuthType: domain.RegistryAuthAnonymous}
	capabilities, err := client.Test(context.Background(), registry)
	if err == nil {
		t.Fatal("Test() succeeded although /v2/_catalog is unavailable")
	}
	if capabilities.Catalog || capabilities.Tags {
		t.Fatalf("Test() capabilities = %#v, want both unavailable", capabilities)
	}
	if !strings.Contains(err.Error(), "https://") {
		t.Fatalf("Test() error = %q, want actionable protocol guidance", err)
	}
	if !strings.Contains(domain.PublicDetail(err), "HTTP 400") {
		t.Fatalf("Test() public detail = %q, want HTTP status", domain.PublicDetail(err))
	}
}
