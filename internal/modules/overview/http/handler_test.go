package overviewhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	overviewapp "dosh/internal/modules/overview/app"

	"github.com/go-chi/chi/v5"
)

type httpOverviewRepository struct {
	version    string
	versionErr error
	metrics    bool
	cni        string
	nodes      []domain.NodeSummary
	nodesErr   error
	pods       []domain.PodSummary
	podsErr    error
	workloads  []domain.WorkloadSummary
	workErr    error
	namespaces []domain.Namespace
	nsErr      error
	events     []domain.Event
	eventsErr  error
}

func (r *httpOverviewRepository) Version(context.Context) (string, error) {
	return r.version, r.versionErr
}
func (r *httpOverviewRepository) MetricsAvailable(context.Context) bool { return r.metrics }
func (r *httpOverviewRepository) DetectCNI(context.Context) string      { return r.cni }
func (r *httpOverviewRepository) NodeOverview(context.Context) (domain.NodeOverview, error) {
	result := domain.NodeOverview{Total: len(r.nodes)}
	for _, node := range r.nodes {
		if node.Status == "Ready" {
			result.Ready++
		}
	}
	return result, r.nodesErr
}
func (r *httpOverviewRepository) PodOverview(context.Context) (domain.PodOverview, error) {
	result := domain.PodOverview{Total: len(r.pods)}
	for _, pod := range r.pods {
		if pod.Status == "Running" {
			result.Running++
		}
	}
	return result, r.podsErr
}
func (r *httpOverviewRepository) CountWorkloads(context.Context) (int, error) {
	return len(r.workloads), r.workErr
}
func (r *httpOverviewRepository) CountNamespaces(context.Context) (int, error) {
	return len(r.namespaces), r.nsErr
}
func (r *httpOverviewRepository) ListEvents(context.Context, domain.ListQuery) ([]domain.Event, error) {
	return r.events, r.eventsErr
}

func overviewTestRouter(repo *httpOverviewRepository) http.Handler {
	router := chi.NewRouter()
	New(overviewapp.New(repo, "cluster-a")).Mount(router)
	return router
}

func overviewRequest(handler http.Handler) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/overview", nil))
	return response
}

func TestOverviewRouteReturnsAggregatedContract(t *testing.T) {
	repo := &httpOverviewRepository{
		version: "v1.36.5", metrics: true, cni: "Cilium",
		nodes:      []domain.NodeSummary{{Name: "worker-a", Status: "Ready"}},
		pods:       []domain.PodSummary{{Name: "api-1", Status: "Running"}},
		workloads:  []domain.WorkloadSummary{{Name: "api"}},
		namespaces: []domain.Namespace{{Name: "prod"}},
		events:     []domain.Event{{Type: "Warning", Reason: "BackOff"}},
	}
	response := overviewRequest(overviewTestRouter(repo))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, want := range []string{
		`"name":"cluster-a"`, `"version":"v1.36.5"`, `"cni":"Cilium"`, `"metricsAvailable":true`,
		`"status":"Healthy"`, `"readyNodes":1`, `"runningPods":1`, `"warningEvents":1`, `"reason":"BackOff"`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, response.Body.String())
		}
	}
}

func TestOverviewRouteReturnsPartialDataWithWarnings(t *testing.T) {
	repo := &httpOverviewRepository{
		nodes:      []domain.NodeSummary{{Name: "worker-a", Status: "Ready"}},
		versionErr: errors.New("version unavailable"), podsErr: errors.New("pods unavailable"),
		workErr: errors.New("workloads unavailable"), nsErr: errors.New("namespaces unavailable"),
		eventsErr: errors.New("events unavailable"),
	}
	response := overviewRequest(overviewTestRouter(repo))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"warnings":[`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestOverviewRouteMapsUnavailableDependency(t *testing.T) {
	repo := &httpOverviewRepository{
		versionErr: errors.New("version"), nodesErr: errors.New("nodes"), podsErr: errors.New("pods"), workErr: errors.New("workloads"),
	}
	response := overviewRequest(overviewTestRouter(repo))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"dependency_unavailable"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestOverviewRouteMapsForbiddenCoreSources(t *testing.T) {
	repo := &httpOverviewRepository{
		nodesErr: domain.ErrForbidden,
		podsErr:  domain.ErrForbidden,
		workErr:  domain.ErrForbidden,
	}
	response := overviewRequest(overviewTestRouter(repo))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
