package nodeshttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	nodesapp "dosh/internal/modules/nodes/app"

	"github.com/go-chi/chi/v5"
)

type httpNodesRepository struct {
	nodes              []domain.NodeSummary
	detail             domain.NodeDetail
	pods               []domain.PodSummary
	namespaces         []domain.Namespace
	namespace          domain.NamespaceDetail
	listErr            error
	detailErr          error
	podsErr            error
	namespacesErr      error
	namespaceErr       error
	lastNodeQuery      domain.ListQuery
	lastNSQuery        domain.ListQuery
	lastNodeName       string
	lastNSName         string
	scheduling         domain.NodeSummary
	schedulingErr      error
	schedulingCalls    int
	lastSchedulingName string
	lastUnschedulable  bool
}

func (r *httpNodesRepository) ListNodes(_ context.Context, query domain.ListQuery) ([]domain.NodeSummary, error) {
	r.lastNodeQuery = query
	return r.nodes, r.listErr
}

func (r *httpNodesRepository) GetNode(_ context.Context, name string) (domain.NodeDetail, error) {
	r.lastNodeName = name
	return r.detail, r.detailErr
}

func (r *httpNodesRepository) SetNodeScheduling(_ context.Context, name string, unschedulable bool) (domain.NodeSummary, error) {
	r.schedulingCalls++
	r.lastSchedulingName = name
	r.lastUnschedulable = unschedulable
	return r.scheduling, r.schedulingErr
}

func (r *httpNodesRepository) ListPodsForNode(_ context.Context, name string) ([]domain.PodSummary, error) {
	r.lastNodeName = name
	return r.pods, r.podsErr
}

func (r *httpNodesRepository) ListNamespaces(_ context.Context, query domain.ListQuery) ([]domain.Namespace, error) {
	r.lastNSQuery = query
	return r.namespaces, r.namespacesErr
}

func (r *httpNodesRepository) GetNamespace(_ context.Context, name string) (domain.NamespaceDetail, error) {
	r.lastNSName = name
	return r.namespace, r.namespaceErr
}

type httpNodesAuthorizer struct{ err error }

func (a httpNodesAuthorizer) Require(context.Context, domain.Actor, domain.Access) error {
	return a.err
}

type httpNodesAuditor struct{}

func (httpNodesAuditor) Record(context.Context, domain.Actor, domain.AuditEvent) {}

func nodesTestRouter(repo *httpNodesRepository) http.Handler {
	return nodesTestRouterWithAuthorizer(repo, httpNodesAuthorizer{})
}

func nodesTestRouterWithAuthorizer(repo *httpNodesRepository, authorizer domain.Authorizer) http.Handler {
	router := chi.NewRouter()
	New(nodesapp.New(repo, authorizer, httpNodesAuditor{})).Mount(router)
	return router
}

func nodesRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	return nodesRequestMethod(handler, http.MethodGet, path)
}

func nodesRequestMethod(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

func TestNodesRoutesReturnContracts(t *testing.T) {
	repo := &httpNodesRepository{
		nodes:      []domain.NodeSummary{{Name: "worker-a", Status: "Ready"}},
		detail:     domain.NodeDetail{NodeSummary: domain.NodeSummary{Name: "worker-a"}, Labels: map[string]string{"role": "worker"}},
		pods:       []domain.PodSummary{{Name: "api-1", Namespace: "prod"}},
		namespaces: []domain.Namespace{{Name: "prod", Status: "Active"}},
		namespace: domain.NamespaceDetail{
			Namespace:      domain.Namespace{Name: "prod", Status: "Active", Annotations: map[string]string{"owner": "platform"}},
			ResourceQuotas: []domain.ResourceQuota{{Name: "compute", Hard: map[string]string{"requests.cpu": "2"}, Used: map[string]string{"requests.cpu": "1"}}},
			LimitRanges:    []domain.LimitRange{{Name: "defaults", Limits: []domain.LimitRangeItem{{Type: "Container", DefaultRequest: map[string]string{"cpu": "100m"}}}}},
		},
	}
	router := nodesTestRouter(repo)

	response := nodesRequest(router, "/nodes?q=%20worker%20")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"worker-a"`) {
		t.Fatalf("nodes status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastNodeQuery.Search != "worker" || !strings.Contains(response.Body.String(), `"search":"worker"`) {
		t.Fatalf("nodes query = %#v body = %s", repo.lastNodeQuery, response.Body.String())
	}
	response = nodesRequest(router, "/nodes/worker-a")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"worker-a"`) ||
		!strings.Contains(response.Body.String(), `"name":"api-1"`) || !strings.Contains(response.Body.String(), `"role":"worker"`) {
		t.Fatalf("node detail status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastNodeName != "worker-a" {
		t.Fatalf("node detail name = %q", repo.lastNodeName)
	}

	response = nodesRequest(router, "/namespaces?q=prod")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"prod"`) {
		t.Fatalf("namespaces status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastNSQuery.Search != "prod" || !strings.Contains(response.Body.String(), `"search":"prod"`) {
		t.Fatalf("namespace query = %#v body = %s", repo.lastNSQuery, response.Body.String())
	}
	response = nodesRequest(router, "/namespaces/prod")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"prod"`) ||
		!strings.Contains(response.Body.String(), `"resourceQuotas"`) ||
		!strings.Contains(response.Body.String(), `"requests.cpu":"2"`) ||
		!strings.Contains(response.Body.String(), `"limitRanges"`) || repo.lastNSName != "prod" {
		t.Fatalf("namespace detail status = %d name = %q body = %s", response.Code, repo.lastNSName, response.Body.String())
	}
}

func TestNodesRoutesMapDependencyFailures(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		apply func(*httpNodesRepository)
		want  int
	}{
		{name: "list nodes", path: "/nodes", apply: func(r *httpNodesRepository) { r.listErr = errors.New("unavailable") }, want: http.StatusInternalServerError},
		{name: "list nodes unavailable", path: "/nodes", apply: func(r *httpNodesRepository) { r.listErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "node detail", path: "/nodes/worker-a", apply: func(r *httpNodesRepository) { r.detailErr = domain.ErrNotFound }, want: http.StatusNotFound},
		{name: "node pods", path: "/nodes/worker-a", apply: func(r *httpNodesRepository) { r.podsErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "list namespaces", path: "/namespaces", apply: func(r *httpNodesRepository) { r.namespacesErr = domain.ErrRateLimited }, want: http.StatusTooManyRequests},
		{name: "namespace detail", path: "/namespaces/prod", apply: func(r *httpNodesRepository) { r.namespaceErr = domain.ErrNotFound }, want: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &httpNodesRepository{}
			test.apply(repo)
			response := nodesRequest(nodesTestRouter(repo), test.path)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestNodesCordonAndUncordonRoutesReturnUpdatedSummary(t *testing.T) {
	repo := &httpNodesRepository{scheduling: domain.NodeSummary{Name: "worker-a", Status: "Ready", Unschedulable: true}}
	router := nodesTestRouter(repo)
	response := nodesRequestMethod(router, http.MethodPost, "/nodes/worker-a/cordon")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"unschedulable":true`) {
		t.Fatalf("cordon status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.schedulingCalls != 1 || repo.lastSchedulingName != "worker-a" || !repo.lastUnschedulable {
		t.Fatalf("cordon calls = %d, name = %q, unschedulable = %t", repo.schedulingCalls, repo.lastSchedulingName, repo.lastUnschedulable)
	}

	repo.scheduling = domain.NodeSummary{Name: "worker-a", Status: "Ready", Unschedulable: false}
	response = nodesRequestMethod(router, http.MethodPost, "/nodes/worker-a/uncordon")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"unschedulable":false`) {
		t.Fatalf("uncordon status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.schedulingCalls != 2 || repo.lastUnschedulable {
		t.Fatalf("uncordon calls = %d, unschedulable = %t", repo.schedulingCalls, repo.lastUnschedulable)
	}
}

func TestNodesSchedulingRoutesMapAuthorizationAndDependencyFailures(t *testing.T) {
	repo := &httpNodesRepository{schedulingErr: domain.ErrUnavailable}
	response := nodesRequestMethod(nodesTestRouterWithAuthorizer(repo, httpNodesAuthorizer{err: domain.ErrForbidden}), http.MethodPost, "/nodes/worker-a/cordon")
	if response.Code != http.StatusForbidden || repo.schedulingCalls != 0 {
		t.Fatalf("forbidden status = %d calls = %d body = %s", response.Code, repo.schedulingCalls, response.Body.String())
	}
	response = nodesRequestMethod(nodesTestRouter(repo), http.MethodPost, "/nodes/worker-a/uncordon")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d body = %s", response.Code, response.Body.String())
	}
}
