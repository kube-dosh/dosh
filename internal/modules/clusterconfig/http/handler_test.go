package clusterconfighttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	clusterconfigapp "dosh/internal/modules/clusterconfig/app"

	"github.com/go-chi/chi/v5"
)

type configurationRepository struct {
	configuration domain.ClusterConfiguration
	err           error
}

func (r configurationRepository) GetClusterConfiguration(context.Context) (domain.ClusterConfiguration, error) {
	return r.configuration, r.err
}

func testRouter(repo configurationRepository) http.Handler {
	router := chi.NewRouter()
	New(clusterconfigapp.New(repo)).Mount(router)
	return router
}

func TestClusterConfigurationRoute(t *testing.T) {
	router := testRouter(configurationRepository{configuration: domain.ClusterConfiguration{
		Name: "cluster-a", Version: "v1.36.5", CNI: "Cilium", PodCIDR: "10.244.0.0/16", ServiceCIDR: "10.96.0.0/12",
		NodeCIDRs: []domain.ClusterNodeCIDR{{Name: "worker-a", InternalIP: "192.168.122.200", PodCIDRs: []string{"10.244.1.0/24"}}},
	}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/cluster/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, want := range []string{`"name":"cluster-a"`, `"podCIDR":"10.244.0.0/16"`, `"serviceCIDR":"10.96.0.0/12"`, `"podCIDRs":["10.244.1.0/24"]`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, response.Body.String())
		}
	}
}

func TestClusterConfigurationRouteMapsDependencyError(t *testing.T) {
	router := testRouter(configurationRepository{err: errors.New("unavailable")})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/cluster/config", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
