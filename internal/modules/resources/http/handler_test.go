package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	resourcesapp "dosh/internal/modules/resources/app"

	"github.com/go-chi/chi/v5"
)

type handlerRepository struct {
	descriptors []domain.ResourceDescriptor
	warnings    []string
	listResult  domain.GenericResourcePage
	rules       domain.ResourceRules
	object      map[string]any
	err         error

	lastQuery     domain.ListQuery
	lastNamespace string
	lastName      string
}

func (r *handlerRepository) DiscoverResources(context.Context) ([]domain.ResourceDescriptor, []string, error) {
	return r.descriptors, r.warnings, r.err
}

func (r *handlerRepository) ListResourceRules(context.Context, domain.Actor, string) (domain.ResourceRules, []string, error) {
	return r.rules, nil, nil
}

func (r *handlerRepository) ListGenericResources(_ context.Context, _ domain.ResourceDescriptor, query domain.ListQuery, _ domain.PageRequest) (domain.GenericResourcePage, error) {
	r.lastQuery = query
	return r.listResult, r.err
}

func (r *handlerRepository) GetGenericResource(_ context.Context, _ domain.ResourceDescriptor, namespace, name string) (map[string]any, error) {
	r.lastNamespace, r.lastName = namespace, name
	return r.object, r.err
}

type handlerAuthorizer struct{ err error }

func (a handlerAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return a.err }

type handlerAuditor struct{}

func (handlerAuditor) Record(context.Context, domain.Actor, domain.AuditEvent) {}

func resourceTestRouter(repository *handlerRepository, authorizer domain.Authorizer, actor domain.Actor) stdhttp.Handler {
	router := chi.NewRouter()
	router.Use(func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			next.ServeHTTP(w, r.WithContext(domain.WithActor(r.Context(), actor)))
		})
	})
	New(resourcesapp.New(repository, authorizer, handlerAuditor{})).Mount(router)
	return router
}

func resourceRequest(router stdhttp.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, path, nil))
	return response
}

func TestResourceDiscoveryRoute(t *testing.T) {
	repository := &handlerRepository{
		descriptors: []domain.ResourceDescriptor{{Group: "example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}}},
		warnings:    []string{"one API group is unavailable"},
	}
	response := resourceRequest(resourceTestRouter(repository, handlerAuthorizer{}, domain.Actor{Username: "alice"}), "/resources/discovery")
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Items    []domain.ResourceDescriptor `json:"items"`
		Warnings []string                    `json:"warnings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Resource != "widgets" || len(body.Warnings) != 1 {
		t.Fatalf("body = %#v", body)
	}
}

func TestResourceListRouteUsesGVRFiltersAndPagination(t *testing.T) {
	repository := &handlerRepository{
		descriptors: []domain.ResourceDescriptor{{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}},
		listResult: domain.GenericResourcePage{
			PageResult: domain.PageResult[domain.GenericResourceSummary]{
				Items: []domain.GenericResourceSummary{
					{APIVersion: "v1", Kind: "Pod", Name: "api-0", Namespace: "prod"},
				},
				Total:      1,
				TotalKnown: true,
			},
		},
	}
	response := resourceRequest(resourceTestRouter(repository, handlerAuthorizer{}, domain.Actor{Username: "alice"}), "/resources/_core/v1/pods?namespace=prod&q=api&pageSize=1&sort=name&order=desc")
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if repository.lastQuery.Namespace != "prod" || repository.lastQuery.Search != "api" {
		t.Fatalf("query = %#v", repository.lastQuery)
	}
	var body struct {
		Items []domain.GenericResourceSummary `json:"items"`
		Meta  struct {
			Page     int                     `json:"page"`
			PageSize int                     `json:"pageSize"`
			Sort     string                  `json:"sort"`
			Order    string                  `json:"order"`
			Columns  []domain.ResourceColumn `json:"columns"`
			Filters  struct {
				Namespace string `json:"namespace"`
				Search    string `json:"search"`
			} `json:"filters"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Name != "api-0" {
		t.Fatalf("items = %#v", body.Items)
	}
	if body.Meta.PageSize != 1 || body.Meta.Sort != "name" || body.Meta.Order != "desc" || body.Meta.Filters.Namespace != "prod" || body.Meta.Filters.Search != "api" {
		t.Fatalf("meta = %#v", body.Meta)
	}
}

func TestResourceSearchRouteReturnsBoundedGenericResults(t *testing.T) {
	repository := &handlerRepository{
		descriptors: []domain.ResourceDescriptor{{Group: "", Version: "v1", Resource: "nodes", Kind: "Node", Verbs: []string{"get", "list"}}},
		rules:       domain.ResourceRules{Cluster: []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"nodes"}}}},
		listResult: domain.GenericResourcePage{PageResult: domain.PageResult[domain.GenericResourceSummary]{
			Items: []domain.GenericResourceSummary{{APIVersion: "v1", Kind: "Node", Resource: "nodes", Name: "worker-1"}}, Total: 1, TotalKnown: true,
		}},
	}
	response := resourceRequest(resourceTestRouter(repository, handlerAuthorizer{}, domain.Actor{Username: "alice"}), "/resources/search?q=worker&limit=5")
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Items    []domain.GenericResourceSummary `json:"items"`
		Warnings []string                        `json:"warnings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Name != "worker-1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestResourceDetailRoute(t *testing.T) {
	repository := &handlerRepository{
		descriptors: []domain.ResourceDescriptor{{Group: "example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true}},
		object: map[string]any{
			"apiVersion": "example.io/v1", "kind": "Widget",
			"metadata": map[string]any{"name": "sample", "namespace": "prod"},
			"spec":     map[string]any{"size": "large"},
		},
	}
	response := resourceRequest(resourceTestRouter(repository, handlerAuthorizer{}, domain.Actor{Username: "alice"}), "/resources/example.io/v1/widgets/sample?namespace=prod")
	if response.Code != stdhttp.StatusOK || !strings.Contains(response.Body.String(), `"name":"sample"`) || !strings.Contains(response.Body.String(), `"yaml"`) {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if repository.lastNamespace != "prod" || repository.lastName != "sample" {
		t.Fatalf("repository params = %q, %q", repository.lastNamespace, repository.lastName)
	}
}

func TestResourceRoutesMapAuthorizationAndDiscoveryErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
		repo *handlerRepository
		auth domain.Authorizer
		want int
	}{
		{
			name: "list forbidden",
			path: "/resources/_core/v1/pods?namespace=prod",
			repo: &handlerRepository{descriptors: []domain.ResourceDescriptor{{Version: "v1", Resource: "pods", Namespaced: true}}},
			auth: handlerAuthorizer{err: domain.ErrForbidden},
			want: stdhttp.StatusForbidden,
		},
		{
			name: "unknown resource",
			path: "/resources/_core/v1/missing?namespace=prod",
			repo: &handlerRepository{descriptors: []domain.ResourceDescriptor{{Version: "v1", Resource: "pods", Namespaced: true}}},
			auth: handlerAuthorizer{},
			want: stdhttp.StatusNotFound,
		},
		{
			name: "discovery unavailable",
			path: "/resources/discovery",
			repo: &handlerRepository{err: domain.ErrUnavailable},
			auth: handlerAuthorizer{},
			want: stdhttp.StatusServiceUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := resourceRequest(resourceTestRouter(test.repo, test.auth, domain.Actor{Username: "alice"}), test.path)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
