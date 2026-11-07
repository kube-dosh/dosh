package bootstrap

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	audithistoryapp "dosh/internal/modules/audithistory/app"
	audithistoryhttp "dosh/internal/modules/audithistory/http"
	"dosh/internal/modules/auth/app"
	authhttp "dosh/internal/modules/auth/http"
	certificatesapp "dosh/internal/modules/certificates/app"
	certificateshttp "dosh/internal/modules/certificates/http"
	clusterconfighttp "dosh/internal/modules/clusterconfig/http"
	configurationhttp "dosh/internal/modules/configuration/http"
	eventshttp "dosh/internal/modules/events/http"
	networkhttp "dosh/internal/modules/network/http"
	nodeshttp "dosh/internal/modules/nodes/http"
	overviewhttp "dosh/internal/modules/overview/http"
	storagehttp "dosh/internal/modules/storage/http"
	workloadshttp "dosh/internal/modules/workloads/http"
	yamlhttp "dosh/internal/modules/yaml/http"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"
	"dosh/internal/platform/config"

	"github.com/go-chi/chi/v5"
	"sigs.k8s.io/yaml"
)

type routeKey struct {
	method  string
	pattern string
}

func (r routeKey) String() string { return r.method + " " + r.pattern }

func TestRouteInventoryMatchesOpenAPI(t *testing.T) {
	t.Parallel()

	router, err := buildRouter(newRouteTestDependencies(t))
	if err != nil {
		t.Fatalf("buildRouter() error = %v", err)
	}
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router type %T does not implement chi.Routes", router)
	}

	actual := make(map[routeKey]struct{})
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := routeKey{method: strings.ToUpper(method), pattern: route}
		if _, exists := actual[key]; exists {
			t.Errorf("duplicate route registration: %s", key)
		}
		if !isNonAPIRoute(key.pattern) {
			actual[key] = struct{}{}
		}
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk() error = %v", err)
	}

	expected, err := openAPIRoutes(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("openAPIRoutes() error = %v", err)
	}

	for route := range expected {
		if _, ok := actual[route]; !ok {
			t.Errorf("OpenAPI route is not mounted: %s", route)
		}
	}
	for route := range actual {
		if _, ok := expected[route]; !ok {
			t.Errorf("mounted route is missing from OpenAPI: %s", route)
		}
	}
}

func TestModuleRoutesAreScoped(t *testing.T) {
	t.Parallel()

	deps := newRouteTestDependencies(t)
	tests := []struct {
		name     string
		mount    func(chi.Router)
		prefixes []string
	}{
		{name: "auth", mount: func(r chi.Router) {
			authhttp.MountPublic(r, deps.auth)
			authhttp.MountProtected(r, deps.auth)
		}, prefixes: []string{"/auth", "/session", "/capabilities"}},
		{name: "overview", mount: deps.overview.Mount, prefixes: []string{"/overview"}},
		{name: "cluster-config", mount: deps.clusterConfig.Mount, prefixes: []string{"/cluster"}},
		{name: "nodes", mount: deps.nodes.Mount, prefixes: []string{"/nodes", "/namespaces"}},
		{name: "workloads", mount: deps.workloads.Mount, prefixes: []string{"/workloads", "/pods"}},
		{name: "events", mount: deps.events.Mount, prefixes: []string{"/events"}},
		{name: "network", mount: deps.network.Mount, prefixes: []string{"/services", "/ingresses", "/ingressclasses"}},
		{name: "configuration", mount: deps.configuration.Mount, prefixes: []string{"/configmaps", "/secrets"}},
		{name: "certificates", mount: deps.certificates.Mount, prefixes: []string{"/certificate"}},
		{name: "storage", mount: deps.storage.Mount, prefixes: []string{"/storageclasses", "/persistentvolumes", "/persistentvolumeclaims"}},
		{name: "yaml", mount: deps.yaml.Mount, prefixes: []string{"/yaml"}},
		{name: "audit-history", mount: deps.auditHistory.Mount, prefixes: []string{"/audit-events"}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := chi.NewRouter()
			test.mount(router)
			err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				if !hasAnyPrefix(route, test.prefixes) {
					t.Errorf("%s registered out-of-scope route %s %s; allowed prefixes: %v", test.name, method, route, test.prefixes)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("chi.Walk() error = %v", err)
			}
		})
	}
}

func newRouteTestDependencies(t *testing.T) routerDependencies {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	manager, err := app.NewSessionManager(bytes.Repeat([]byte("k"), 32), 8*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	stateManager, err := app.NewOIDCStateManager(bytes.Repeat([]byte("s"), 32))
	if err != nil {
		t.Fatalf("NewOIDCStateManager() error = %v", err)
	}
	authorizer := authz.NewLocal(false)
	sessions := app.NewSessionService(manager, authorizer, "local")
	local := app.NewService(app.Config{}, sessions)
	oidc := app.NewOIDCService(nil, sessions, stateManager, app.OIDCConfig{})
	authHandler := authhttp.New(authhttp.Options{
		Mode: "local", Local: local, OIDC: oidc, Sessions: sessions,
		Audit: audit.New(logger), Logger: logger, CookieSecure: true,
	})

	return routerDependencies{
		logger:        logger,
		cfg:           config.Config{},
		auth:          authHandler,
		overview:      overviewhttp.New(nil),
		clusterConfig: clusterconfighttp.New(nil),
		nodes:         nodeshttp.New(nil),
		workloads:     workloadshttp.New(nil),
		events:        eventshttp.New(nil),
		network:       networkhttp.New(nil),
		configuration: configurationhttp.New(nil),
		certificates:  certificateshttp.New(certificatesapp.New(nil, authz.NewLocal(false), audit.New(logger))),
		storage:       storagehttp.New(nil),
		yaml:          yamlhttp.New(nil),
		auditHistory:  audithistoryhttp.New(audithistoryapp.New(audit.New(logger), authz.NewLocal(false))),
	}
}

func openAPIRoutes(path string) (map[routeKey]struct{}, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	routes := make(map[routeKey]struct{})
	for pattern, operations := range document.Paths {
		for method := range operations {
			switch strings.ToLower(method) {
			case "get", "post", "put", "patch", "delete", "head", "options", "trace":
				routes[routeKey{method: strings.ToUpper(method), pattern: pattern}] = struct{}{}
			}
		}
	}
	return routes, nil
}

func isNonAPIRoute(pattern string) bool {
	switch pattern {
	case "/healthz", "/readyz":
		return true
	default:
		return false
	}
}

func hasAnyPrefix(route string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if route == prefix || strings.HasPrefix(route, prefix+"/") {
			return true
		}
	}
	return false
}
