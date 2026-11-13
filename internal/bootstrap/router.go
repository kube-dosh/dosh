package bootstrap

import (
	"log/slog"
	stdhttp "net/http"
	"strings"

	"dosh/internal/domain"
	audithistoryhttp "dosh/internal/modules/audithistory/http"
	authhttp "dosh/internal/modules/auth/http"
	certificateshttp "dosh/internal/modules/certificates/http"
	clusterconfighttp "dosh/internal/modules/clusterconfig/http"
	configurationhttp "dosh/internal/modules/configuration/http"
	eventshttp "dosh/internal/modules/events/http"
	networkhttp "dosh/internal/modules/network/http"
	nodeshttp "dosh/internal/modules/nodes/http"
	overviewhttp "dosh/internal/modules/overview/http"
	resourceshttp "dosh/internal/modules/resources/http"
	storagehttp "dosh/internal/modules/storage/http"
	workloadshttp "dosh/internal/modules/workloads/http"
	yamlhttp "dosh/internal/modules/yaml/http"
	"dosh/internal/platform/config"
	"dosh/internal/platform/httpx"
	"dosh/internal/platform/kube"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type routerDependencies struct {
	logger        *slog.Logger
	cfg           config.Config
	kube          *kube.Client
	auth          *authhttp.Handler
	overview      *overviewhttp.Handler
	resources     *resourceshttp.Handler
	clusterConfig *clusterconfighttp.Handler
	nodes         *nodeshttp.Handler
	workloads     *workloadshttp.Handler
	events        *eventshttp.Handler
	network       *networkhttp.Handler
	configuration *configurationhttp.Handler
	certificates  *certificateshttp.Handler
	storage       *storagehttp.Handler
	yaml          *yamlhttp.Handler
	auditHistory  *audithistoryhttp.Handler
}

func buildRouter(deps routerDependencies) (stdhttp.Handler, error) {
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.RequestIDContext)
	router.Use(middleware.Recoverer)
	router.Use(httpx.SecurityHeaders)
	router.Use(httpx.NoStoreAPI)
	router.Use(httpx.SameOrigin)
	router.Use(httpx.AccessLog(deps.logger))
	router.Use(middleware.Compress(5, "text/html", "text/css", "application/json", "application/javascript", "text/plain"))

	router.Get("/healthz", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		status, body := readiness(r.Context(), deps.kube)
		httpx.JSON(w, status, body)
	})

	router.Route("/api/v1", func(r chi.Router) {
		authhttp.MountPublic(r, deps.auth)
		r.Group(func(r chi.Router) {
			r.Use(deps.auth.RequireSession)
			r.Use(deps.auth.RequireCSRF)
			authhttp.MountProtected(r, deps.auth)
			deps.overview.Mount(r)
			deps.resources.Mount(r)
			deps.clusterConfig.Mount(r)
			deps.nodes.Mount(r)
			deps.workloads.Mount(r)
			deps.events.Mount(r)
			deps.network.Mount(r)
			deps.configuration.Mount(r)
			deps.certificates.Mount(r)
			deps.storage.Mount(r)
			deps.yaml.Mount(r)
			deps.auditHistory.Mount(r)
		})
	})

	spa, err := staticHandler()
	if err != nil {
		return nil, err
	}
	router.NotFound(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			httpx.Error(w, r, domain.ErrNotFound)
			return
		}
		spa.ServeHTTP(w, r)
	})
	return router, nil
}

func isAsset(path string) bool {
	return strings.HasPrefix(path, "/assets/")
}

func cacheControl(asset bool) string {
	if asset {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}
