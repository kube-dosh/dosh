package networkhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	networkapp "dosh/internal/modules/network/app"

	"github.com/go-chi/chi/v5"
)

type httpNetworkRepository struct {
	services            []domain.ServiceSummary
	service             domain.ServiceDetail
	ingresses           []domain.IngressSummary
	ingress             domain.IngressDetail
	ingressClasses      []domain.IngressClassSummary
	servicesErr         error
	serviceErr          error
	ingressesErr        error
	ingressErr          error
	ingressClassesErr   error
	lastServiceQry      domain.ListQuery
	lastServiceParams   [2]string
	lastIngressQry      domain.ListQuery
	lastIngressParams   [2]string
	lastIngressClassQry domain.ListQuery
	createCalls         int
	updateCalls         int
	deleteCalls         int
	createErr           error
	updateErr           error
	deleteErr           error
	lastCreate          domain.ServiceCreateSpec
	lastUpdate          domain.ServiceUpdateSpec
	lastDeleteResource  string
}

func (r *httpNetworkRepository) ListServices(_ context.Context, query domain.ListQuery) ([]domain.ServiceSummary, error) {
	r.lastServiceQry = query
	return r.services, r.servicesErr
}

func (r *httpNetworkRepository) GetService(_ context.Context, namespace, name string) (domain.ServiceDetail, error) {
	r.lastServiceParams = [2]string{namespace, name}
	return r.service, r.serviceErr
}

func (r *httpNetworkRepository) CreateService(_ context.Context, spec domain.ServiceCreateSpec) (domain.ServiceDetail, error) {
	r.createCalls++
	r.lastCreate = spec
	return r.service, r.createErr
}

func (r *httpNetworkRepository) UpdateService(_ context.Context, namespace, name string, update domain.ServiceUpdateSpec) (domain.ServiceDetail, error) {
	r.updateCalls++
	r.lastServiceParams = [2]string{namespace, name}
	r.lastUpdate = update
	return r.service, r.updateErr
}

func (r *httpNetworkRepository) DeleteService(_ context.Context, namespace, name, resourceVersion string) error {
	r.deleteCalls++
	r.lastServiceParams = [2]string{namespace, name}
	r.lastDeleteResource = resourceVersion
	return r.deleteErr
}

func (r *httpNetworkRepository) ListIngresses(_ context.Context, query domain.ListQuery) ([]domain.IngressSummary, error) {
	r.lastIngressQry = query
	return r.ingresses, r.ingressesErr
}

func (r *httpNetworkRepository) GetIngress(_ context.Context, namespace, name string) (domain.IngressDetail, error) {
	r.lastIngressParams = [2]string{namespace, name}
	return r.ingress, r.ingressErr
}

func (r *httpNetworkRepository) ListIngressClasses(_ context.Context, query domain.ListQuery) ([]domain.IngressClassSummary, error) {
	r.lastIngressClassQry = query
	return r.ingressClasses, r.ingressClassesErr
}

func networkTestRouter(repo *httpNetworkRepository) http.Handler {
	return networkTestRouterWithAuthorizer(repo, networkHTTPAuthorizer{})
}

func networkTestRouterWithAuthorizer(repo *httpNetworkRepository, authorizer domain.Authorizer) http.Handler {
	router := chi.NewRouter()
	New(networkapp.New(repo, authorizer, networkHTTPAuditor{})).Mount(router)
	return router
}

type networkHTTPAuthorizer struct{ err error }

func (a networkHTTPAuthorizer) Require(context.Context, domain.Actor, domain.Access) error {
	return a.err
}

type networkHTTPAuditor struct{}

func (networkHTTPAuditor) Record(context.Context, domain.Actor, domain.AuditEvent) {}

func networkRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	return networkRequestMethod(handler, http.MethodGet, path, "")
}

func networkRequestMethod(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	handler.ServeHTTP(response, request)
	return response
}

func TestNetworkRoutesReturnFilteredLists(t *testing.T) {
	repo := &httpNetworkRepository{
		services:       []domain.ServiceSummary{{Name: "api", Namespace: "prod", Type: "ClusterIP", Ports: []domain.ServicePort{{Name: "http", Port: 80}}, EndpointCount: 2}},
		service:        domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api", Namespace: "prod", EndpointCount: 1}, SessionAffinity: "None", Endpoints: []domain.ServiceEndpoint{{Addresses: []string{"10.0.0.10"}, Ready: true, TargetKind: "Pod", TargetName: "api-0"}}},
		ingresses:      []domain.IngressSummary{{Name: "web", Namespace: "prod", Class: "nginx", Paths: []domain.IngressPath{{Host: "app.example.test", Path: "/", Service: "web", Port: "80", TargetStatus: domain.IngressTargetReady}}}},
		ingress:        domain.IngressDetail{IngressSummary: domain.IngressSummary{Name: "web", Namespace: "prod", Class: "nginx"}, TLS: []domain.IngressTLS{{Hosts: []string{"app.example.test"}, SecretName: "web-tls"}}},
		ingressClasses: []domain.IngressClassSummary{{Name: "nginx", Controller: "k8s.io/ingress-nginx", Default: true}},
	}
	router := networkTestRouter(repo)
	response := networkRequest(router, "/services?namespace=%20prod%20&q=%20api%20")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"api"`) ||
		!strings.Contains(response.Body.String(), `"namespace":"prod"`) || !strings.Contains(response.Body.String(), `"endpointCount":2`) || !strings.Contains(response.Body.String(), `"search":"api"`) {
		t.Fatalf("services status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastServiceQry.Namespace != "prod" || repo.lastServiceQry.Search != "api" {
		t.Fatalf("services query = %#v", repo.lastServiceQry)
	}
	response = networkRequest(router, "/services/prod/api")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"targetName":"api-0"`) || !strings.Contains(response.Body.String(), `"sessionAffinity":"None"`) {
		t.Fatalf("service detail status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastServiceParams != [2]string{"prod", "api"} {
		t.Fatalf("service detail params = %#v", repo.lastServiceParams)
	}

	response = networkRequest(router, "/ingresses?namespace=prod&q=web")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"web"`) ||
		!strings.Contains(response.Body.String(), `"host":"app.example.test"`) || !strings.Contains(response.Body.String(), `"targetStatus":"Ready"`) || !strings.Contains(response.Body.String(), `"search":"web"`) {
		t.Fatalf("ingresses status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastIngressQry.Namespace != "prod" || repo.lastIngressQry.Search != "web" {
		t.Fatalf("ingresses query = %#v", repo.lastIngressQry)
	}
	response = networkRequest(router, "/ingresses/prod/web")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"secretName":"web-tls"`) || !strings.Contains(response.Body.String(), `"class":"nginx"`) {
		t.Fatalf("ingress detail status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastIngressParams != [2]string{"prod", "web"} {
		t.Fatalf("ingress detail params = %#v", repo.lastIngressParams)
	}

	response = networkRequest(router, "/ingressclasses?q=nginx")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"nginx"`) ||
		!strings.Contains(response.Body.String(), `"controller":"k8s.io/ingress-nginx"`) || !strings.Contains(response.Body.String(), `"default":true`) {
		t.Fatalf("ingressclasses status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastIngressClassQry.Namespace != "" || repo.lastIngressClassQry.Search != "nginx" {
		t.Fatalf("ingressclasses query = %#v", repo.lastIngressClassQry)
	}
}

func TestNetworkRoutesMapDependencyFailures(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		apply func(*httpNetworkRepository)
		want  int
	}{
		{name: "services unavailable", path: "/services", apply: func(r *httpNetworkRepository) { r.servicesErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "services not found", path: "/services", apply: func(r *httpNetworkRepository) { r.servicesErr = domain.ErrNotFound }, want: http.StatusNotFound},
		{name: "service detail unavailable", path: "/services/prod/api", apply: func(r *httpNetworkRepository) { r.serviceErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "service detail not found", path: "/services/prod/missing", apply: func(r *httpNetworkRepository) { r.serviceErr = domain.ErrNotFound }, want: http.StatusNotFound},
		{name: "ingresses unavailable", path: "/ingresses", apply: func(r *httpNetworkRepository) { r.ingressesErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "ingresses unsupported", path: "/ingresses", apply: func(r *httpNetworkRepository) { r.ingressesErr = domain.ErrUnsupported }, want: http.StatusNotImplemented},
		{name: "ingress detail unavailable", path: "/ingresses/prod/web", apply: func(r *httpNetworkRepository) { r.ingressErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "ingress detail unsupported", path: "/ingresses/prod/web", apply: func(r *httpNetworkRepository) { r.ingressErr = domain.ErrUnsupported }, want: http.StatusNotImplemented},
		{name: "ingressclasses unavailable", path: "/ingressclasses", apply: func(r *httpNetworkRepository) { r.ingressClassesErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "ingressclasses unsupported", path: "/ingressclasses", apply: func(r *httpNetworkRepository) { r.ingressClassesErr = domain.ErrUnsupported }, want: http.StatusNotImplemented},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &httpNetworkRepository{}
			test.apply(repo)
			response := networkRequest(networkTestRouter(repo), test.path)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestNetworkServiceWriteRoutes(t *testing.T) {
	t.Parallel()
	repo := &httpNetworkRepository{service: domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api", Namespace: "prod", ResourceVersion: "11", Type: "ClusterIP"}}}
	router := networkTestRouter(repo)

	response := networkRequestMethod(router, http.MethodPost, "/services", `{"name":"api","namespace":"prod","type":"ClusterIP","selector":{"app":"web"},"ports":[{"name":"http","port":80,"targetPort":"8080"}]}`)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"resourceVersion":"11"`) {
		t.Fatalf("create status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.createCalls != 1 || repo.lastCreate.Name != "api" || repo.lastCreate.Ports[0].TargetPort != "8080" {
		t.Fatalf("create call = %d spec = %#v", repo.createCalls, repo.lastCreate)
	}

	response = networkRequestMethod(router, http.MethodPut, "/services/prod/api", `{"type":"NodePort","selector":{"app":"web"},"ports":[{"name":"http","port":80}],"resourceVersion":"11"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"api"`) {
		t.Fatalf("update status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.updateCalls != 1 || repo.lastServiceParams != [2]string{"prod", "api"} || repo.lastUpdate.ResourceVersion != "11" {
		t.Fatalf("update call = %d params = %#v update = %#v", repo.updateCalls, repo.lastServiceParams, repo.lastUpdate)
	}

	response = networkRequestMethod(router, http.MethodDelete, "/services/prod/api?resourceVersion=11", "")
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"status":"deleting"`) {
		t.Fatalf("delete status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.deleteCalls != 1 || repo.lastDeleteResource != "11" {
		t.Fatalf("delete call = %d resourceVersion = %q", repo.deleteCalls, repo.lastDeleteResource)
	}
}

func TestNetworkServiceWriteRoutesValidateAndRequirePermission(t *testing.T) {
	t.Parallel()
	repo := &httpNetworkRepository{}
	response := networkRequestMethod(networkTestRouter(repo), http.MethodPost, "/services", `{"name":"api","namespace":"prod","selector":{},"ports":[{"port":80}]}`)
	if response.Code != http.StatusBadRequest || repo.createCalls != 0 {
		t.Fatalf("invalid create status = %d calls = %d body = %s", response.Code, repo.createCalls, response.Body.String())
	}
	router := networkTestRouterWithAuthorizer(repo, networkHTTPAuthorizer{err: domain.ErrForbidden})
	response = networkRequestMethod(router, http.MethodPut, "/services/prod/api", `{"type":"ClusterIP","selector":{"app":"web"},"ports":[{"port":80}]}`)
	if response.Code != http.StatusForbidden || repo.updateCalls != 0 {
		t.Fatalf("forbidden update status = %d calls = %d body = %s", response.Code, repo.updateCalls, response.Body.String())
	}
	response = networkRequestMethod(router, http.MethodDelete, "/services/prod/api", "")
	if response.Code != http.StatusForbidden || repo.deleteCalls != 0 {
		t.Fatalf("forbidden delete status = %d calls = %d body = %s", response.Code, repo.deleteCalls, response.Body.String())
	}
}
