package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type networkRepository struct {
	services              []domain.ServiceSummary
	service               domain.ServiceDetail
	ingresses             []domain.IngressSummary
	ingress               domain.IngressDetail
	ingressClasses        []domain.IngressClassSummary
	err                   error
	createCalls           int
	updateCalls           int
	deleteCalls           int
	lastCreate            domain.ServiceCreateSpec
	lastUpdate            domain.ServiceUpdateSpec
	lastUpdateNS          string
	lastUpdateName        string
	lastDeleteNS          string
	lastDeleteName        string
	lastDeleteRV          string
	ingressCreateCalls    int
	ingressUpdateCalls    int
	ingressDeleteCalls    int
	lastIngressCreate     domain.IngressCreateSpec
	lastIngressUpdate     domain.IngressUpdateSpec
	lastIngressUpdateNS   string
	lastIngressUpdateName string
	lastIngressDeleteRV   string
}

func (r *networkRepository) ListServices(context.Context, domain.ListQuery) ([]domain.ServiceSummary, error) {
	return r.services, r.err
}
func (r *networkRepository) ListServicesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.ServiceSummary], error) {
	items, err := r.ListServices(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *networkRepository) GetService(context.Context, string, string) (domain.ServiceDetail, error) {
	return r.service, r.err
}
func (r *networkRepository) CreateService(_ context.Context, spec domain.ServiceCreateSpec) (domain.ServiceDetail, error) {
	r.createCalls++
	r.lastCreate = spec
	return r.service, r.err
}
func (r *networkRepository) UpdateService(_ context.Context, namespace, name string, update domain.ServiceUpdateSpec) (domain.ServiceDetail, error) {
	r.updateCalls++
	r.lastUpdateNS = namespace
	r.lastUpdateName = name
	r.lastUpdate = update
	return r.service, r.err
}
func (r *networkRepository) DeleteService(_ context.Context, namespace, name, resourceVersion string) error {
	r.deleteCalls++
	r.lastDeleteNS = namespace
	r.lastDeleteName = name
	r.lastDeleteRV = resourceVersion
	return r.err
}
func (r *networkRepository) ListIngresses(context.Context, domain.ListQuery) ([]domain.IngressSummary, error) {
	return r.ingresses, r.err
}
func (r *networkRepository) ListIngressesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.IngressSummary], error) {
	items, err := r.ListIngresses(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *networkRepository) GetIngress(context.Context, string, string) (domain.IngressDetail, error) {
	return r.ingress, r.err
}
func (r *networkRepository) CreateIngress(_ context.Context, spec domain.IngressCreateSpec) (domain.IngressDetail, error) {
	r.ingressCreateCalls++
	r.lastIngressCreate = spec
	return r.ingress, r.err
}
func (r *networkRepository) UpdateIngress(_ context.Context, namespace, name string, update domain.IngressUpdateSpec) (domain.IngressDetail, error) {
	r.ingressUpdateCalls++
	r.lastIngressUpdateNS = namespace
	r.lastIngressUpdateName = name
	r.lastIngressUpdate = update
	return r.ingress, r.err
}
func (r *networkRepository) DeleteIngress(_ context.Context, _, _ string, resourceVersion string) error {
	r.ingressDeleteCalls++
	r.lastIngressDeleteRV = resourceVersion
	return r.err
}

func (r *networkRepository) ListIngressClasses(context.Context, domain.ListQuery) ([]domain.IngressClassSummary, error) {
	return r.ingressClasses, r.err
}
func (r *networkRepository) ListIngressClassesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.IngressClassSummary], error) {
	items, err := r.ListIngressClasses(ctx, query)
	return domain.PaginateSlice(items, page), err
}

func TestNetworkServiceListsResourcesAndFailures(t *testing.T) {
	repo := &networkRepository{
		services:       []domain.ServiceSummary{{Name: "api"}},
		service:        domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api"}, Endpoints: []domain.ServiceEndpoint{{Addresses: []string{"10.0.0.10"}, Ready: true}}},
		ingresses:      []domain.IngressSummary{{Name: "api"}},
		ingress:        domain.IngressDetail{IngressSummary: domain.IngressSummary{Name: "api"}, TLS: []domain.IngressTLS{{SecretName: "api-tls"}}},
		ingressClasses: []domain.IngressClassSummary{{Name: "nginx"}},
	}
	service := New(repo, networkTestAuthorizer{}, networkTestAuditor{})
	services, err := service.ListServices(context.Background(), domain.ListQuery{})
	if err != nil || len(services) != 1 {
		t.Fatalf("ListServices() = (%#v, %v)", services, err)
	}
	serviceDetail, err := service.GetService(context.Background(), "prod", "api")
	if err != nil || len(serviceDetail.Endpoints) != 1 {
		t.Fatalf("GetService() = (%#v, %v)", serviceDetail, err)
	}
	ingresses, err := service.ListIngresses(context.Background(), domain.ListQuery{})
	if err != nil || len(ingresses) != 1 {
		t.Fatalf("ListIngresses() = (%#v, %v)", ingresses, err)
	}
	ingressDetail, err := service.GetIngress(context.Background(), "prod", "api")
	if err != nil || len(ingressDetail.TLS) != 1 {
		t.Fatalf("GetIngress() = (%#v, %v)", ingressDetail, err)
	}
	classes, err := service.ListIngressClasses(context.Background(), domain.ListQuery{})
	if err != nil || len(classes) != 1 {
		t.Fatalf("ListIngressClasses() = (%#v, %v)", classes, err)
	}

	dependencyErr := errors.New("kubernetes unavailable")
	repo.err = dependencyErr
	if _, err := service.ListServices(context.Background(), domain.ListQuery{}); !errors.Is(err, dependencyErr) {
		t.Fatalf("ListServices() error = %v", err)
	}
	if _, err := service.GetService(context.Background(), "prod", "api"); !errors.Is(err, dependencyErr) {
		t.Fatalf("GetService() error = %v", err)
	}
	if _, err := service.ListIngresses(context.Background(), domain.ListQuery{}); !errors.Is(err, dependencyErr) {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if _, err := service.GetIngress(context.Background(), "prod", "api"); !errors.Is(err, dependencyErr) {
		t.Fatalf("GetIngress() error = %v", err)
	}
	if _, err := service.ListIngressClasses(context.Background(), domain.ListQuery{}); !errors.Is(err, dependencyErr) {
		t.Fatalf("ListIngressClasses() error = %v", err)
	}
}

type networkTestAuthorizer struct{ err error }

func (a networkTestAuthorizer) Require(context.Context, domain.Actor, domain.Access) error {
	return a.err
}

type networkTestAuditor struct{}

func (networkTestAuditor) Record(context.Context, domain.Actor, domain.AuditEvent) {}

type networkCaptureAuthorizer struct {
	err    error
	access domain.Access
}

func (a *networkCaptureAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.access = access
	return a.err
}

type networkCaptureAuditor struct{ events []domain.AuditEvent }

func (a *networkCaptureAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestNetworkServiceCreatesNormalizedServiceAndAudits(t *testing.T) {
	t.Parallel()
	repo := &networkRepository{service: domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api", Namespace: "prod"}}}
	authorizer := &networkCaptureAuthorizer{}
	auditor := &networkCaptureAuditor{}
	service := New(repo, authorizer, auditor)

	detail, err := service.CreateService(context.Background(), domain.Actor{Username: "admin"}, domain.ServiceCreateSpec{
		Name: "api", Namespace: "prod", Selector: map[string]string{" app ": " web "},
		Ports: []domain.ServicePortInput{{Port: 80}},
	})
	if err != nil || detail.Name != "api" || repo.createCalls != 1 {
		t.Fatalf("CreateService() = (%#v, %v), calls = %d", detail, err, repo.createCalls)
	}
	if repo.lastCreate.Type != "ClusterIP" || repo.lastCreate.Selector["app"] != "web" {
		t.Fatalf("normalized create spec = %#v", repo.lastCreate)
	}
	if port := repo.lastCreate.Ports[0]; port.Protocol != "TCP" || port.TargetPort != "80" {
		t.Fatalf("normalized port = %#v", port)
	}
	if authorizer.access != (domain.Access{Verb: "create", Resource: "services", Namespace: "prod", Name: "api"}) {
		t.Fatalf("access = %#v", authorizer.access)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "create-service" || auditor.events[0].Resource != "service" || auditor.events[0].Result != "success" {
		t.Fatalf("audit = %#v", auditor.events)
	}
}

func TestNetworkServiceWritesRequirePermission(t *testing.T) {
	t.Parallel()
	repo := &networkRepository{}
	authorizer := &networkCaptureAuthorizer{err: domain.ErrForbidden}
	auditor := &networkCaptureAuditor{}
	service := New(repo, authorizer, auditor)
	spec := domain.ServiceCreateSpec{Name: "api", Namespace: "prod", Selector: map[string]string{"app": "web"}, Ports: []domain.ServicePortInput{{Port: 80}}}

	if _, err := service.CreateService(context.Background(), domain.Actor{Username: "viewer"}, spec); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("CreateService() error = %v", err)
	}
	if _, err := service.UpdateService(context.Background(), domain.Actor{Username: "viewer"}, "prod", "api", domain.ServiceUpdateSpec{Type: "ClusterIP", Selector: map[string]string{"app": "web"}, Ports: []domain.ServicePortInput{{Port: 80}}}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if err := service.DeleteService(context.Background(), domain.Actor{Username: "viewer"}, "prod", "api", "1"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if repo.createCalls != 0 || repo.updateCalls != 0 || repo.deleteCalls != 0 {
		t.Fatalf("repository writes without permission = %d/%d/%d", repo.createCalls, repo.updateCalls, repo.deleteCalls)
	}
	if len(auditor.events) != 3 || auditor.events[0].Result != "denied" || auditor.events[1].Result != "denied" || auditor.events[2].Result != "denied" {
		t.Fatalf("denied audit = %#v", auditor.events)
	}
}

func TestNetworkServiceRejectsInvalidServiceBeforeRepositoryWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		spec  domain.ServiceCreateSpec
	}{
		{name: "empty selector", field: "selector", spec: domain.ServiceCreateSpec{Name: "api", Namespace: "prod", Ports: []domain.ServicePortInput{{Port: 80}}}},
		{name: "unsupported type", field: "type", spec: domain.ServiceCreateSpec{Name: "api", Namespace: "prod", Type: "ExternalName", Selector: map[string]string{"app": "web"}, Ports: []domain.ServicePortInput{{Port: 80}}}},
		{name: "unnamed multiple ports", field: "ports", spec: domain.ServiceCreateSpec{Name: "api", Namespace: "prod", Selector: map[string]string{"app": "web"}, Ports: []domain.ServicePortInput{{Port: 80}, {Port: 443}}}},
		{name: "duplicate port", field: "ports", spec: domain.ServiceCreateSpec{Name: "api", Namespace: "prod", Selector: map[string]string{"app": "web"}, Ports: []domain.ServicePortInput{{Name: "http", Port: 80}, {Name: "web", Port: 80}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := &networkRepository{}
			auditor := &networkCaptureAuditor{}
			service := New(repo, &networkCaptureAuthorizer{}, auditor)
			if _, err := service.CreateService(context.Background(), domain.Actor{Username: "admin"}, test.spec); err == nil {
				t.Fatal("CreateService() accepted invalid spec")
			}
			if repo.createCalls != 0 {
				t.Fatalf("repository called for invalid spec: %d", repo.createCalls)
			}
			if len(auditor.events) != 1 || auditor.events[0].Result != "failure" {
				t.Fatalf("validation audit = %#v", auditor.events)
			}
			var validation *domain.ValidationError
			if !errors.As(auditor.events[0].Err, &validation) || validation.Field != test.field {
				t.Fatalf("validation error = %#v", auditor.events[0].Err)
			}
		})
	}
}

func TestNetworkServiceNormalizesAndLocksHeadlessMode(t *testing.T) {
	t.Parallel()
	repo := &networkRepository{}
	service := New(repo, &networkCaptureAuthorizer{}, &networkCaptureAuditor{})
	actor := domain.Actor{Username: "admin"}

	if _, err := service.CreateService(context.Background(), actor, domain.ServiceCreateSpec{
		Name: "db", Namespace: "prod", Headless: true, Selector: map[string]string{"app": "db"},
		Ports: []domain.ServicePortInput{{Name: "http", Port: 80}},
	}); err != nil {
		t.Fatalf("CreateService() headless error = %v", err)
	}
	if repo.lastCreate.Type != "ClusterIP" || !repo.lastCreate.Headless {
		t.Fatalf("headless create spec = %#v", repo.lastCreate)
	}

	if _, err := service.CreateService(context.Background(), actor, domain.ServiceCreateSpec{
		Name: "db-node", Namespace: "prod", Type: "NodePort", Headless: true, Selector: map[string]string{"app": "db"},
		Ports: []domain.ServicePortInput{{Name: "http", Port: 80}},
	}); err == nil {
		t.Fatal("CreateService() accepted Headless NodePort")
	} else {
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Field != "headless" {
			t.Fatalf("headless type validation = %#v", err)
		}
	}

	repo.service = domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api", Namespace: "prod"}}
	if _, err := service.UpdateService(context.Background(), actor, "prod", "api", domain.ServiceUpdateSpec{
		Type: "ClusterIP", Headless: true, Selector: map[string]string{"app": "web"},
		Ports: []domain.ServicePortInput{{Port: 80}},
	}); err == nil {
		t.Fatal("UpdateService() changed headless mode")
	} else {
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Field != "headless" {
			t.Fatalf("headless immutability validation = %#v", err)
		}
	}
	if repo.updateCalls != 0 {
		t.Fatalf("repository update called for immutable headless change: %d", repo.updateCalls)
	}

	repo.service.Headless = true
	if _, err := service.UpdateService(context.Background(), actor, "prod", "api", domain.ServiceUpdateSpec{
		Type: "ClusterIP", Headless: true, Selector: map[string]string{"app": "web"},
		Ports: []domain.ServicePortInput{{Port: 80}},
	}); err != nil {
		t.Fatalf("UpdateService() matching headless mode error = %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1", repo.updateCalls)
	}
}

func TestNetworkServiceUpdatesAndDeletesWithResourceVersion(t *testing.T) {
	t.Parallel()
	repo := &networkRepository{service: domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api", Namespace: "prod"}}}
	auditor := &networkCaptureAuditor{}
	service := New(repo, &networkCaptureAuthorizer{}, auditor)
	update := domain.ServiceUpdateSpec{Type: "NodePort", Selector: map[string]string{"app": "web"}, Ports: []domain.ServicePortInput{{Name: "http", Port: 80}}, ResourceVersion: "7"}
	if _, err := service.UpdateService(context.Background(), domain.Actor{Username: "admin"}, "prod", "api", update); err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if repo.updateCalls != 1 || repo.lastUpdateNS != "prod" || repo.lastUpdateName != "api" || repo.lastUpdate.ResourceVersion != "7" || repo.lastUpdate.Type != "NodePort" {
		t.Fatalf("update call = %#v", repo.lastUpdate)
	}
	if err := service.DeleteService(context.Background(), domain.Actor{Username: "admin"}, "prod", "api", "7"); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if repo.deleteCalls != 1 || repo.lastDeleteNS != "prod" || repo.lastDeleteName != "api" || repo.lastDeleteRV != "7" {
		t.Fatalf("delete call = %s/%s rv=%s", repo.lastDeleteNS, repo.lastDeleteName, repo.lastDeleteRV)
	}
	if len(auditor.events) != 2 || auditor.events[0].Action != "update-service" || auditor.events[1].Action != "delete-service" {
		t.Fatalf("audit = %#v", auditor.events)
	}
}

func TestNetworkIngressWritesValidateAuthAndResourceVersions(t *testing.T) {
	t.Parallel()
	repo := &networkRepository{ingress: domain.IngressDetail{IngressSummary: domain.IngressSummary{Name: "web", Namespace: "prod"}}}
	auditor := &networkCaptureAuditor{}
	service := New(repo, &networkCaptureAuthorizer{}, auditor)
	actor := domain.Actor{Username: "admin"}
	create := domain.IngressCreateSpec{
		Name: "web", Namespace: "prod", Class: "nginx",
		Paths: []domain.IngressPathInput{{Host: "app.example.test", Path: "/", PathType: "Prefix", Service: "web", Port: "80"}},
		TLS:   []domain.IngressTLS{{Hosts: []string{"app.example.test"}, SecretName: "web-tls"}},
	}
	if _, err := service.CreateIngress(context.Background(), actor, create); err != nil {
		t.Fatalf("CreateIngress() error = %v", err)
	}
	if repo.ingressCreateCalls != 1 || repo.lastIngressCreate.Name != "web" || repo.lastIngressCreate.TLS[0].SecretName != "web-tls" {
		t.Fatalf("create ingress call = %d spec = %#v", repo.ingressCreateCalls, repo.lastIngressCreate)
	}
	update := domain.IngressUpdateSpec{
		Class: "nginx", Paths: []domain.IngressPathInput{{Path: "/", PathType: "Prefix", Service: "web", Port: "8080"}}, ResourceVersion: "31",
	}
	if _, err := service.UpdateIngress(context.Background(), actor, "prod", "web", update); err != nil {
		t.Fatalf("UpdateIngress() error = %v", err)
	}
	if repo.ingressUpdateCalls != 1 || repo.lastIngressUpdateNS != "prod" || repo.lastIngressUpdateName != "web" || repo.lastIngressUpdate.ResourceVersion != "31" {
		t.Fatalf("update ingress call = %d %s/%s spec = %#v", repo.ingressUpdateCalls, repo.lastIngressUpdateNS, repo.lastIngressUpdateName, repo.lastIngressUpdate)
	}
	if err := service.DeleteIngress(context.Background(), actor, "prod", "web", "32"); err != nil {
		t.Fatalf("DeleteIngress() error = %v", err)
	}
	if repo.ingressDeleteCalls != 1 || repo.lastIngressDeleteRV != "32" {
		t.Fatalf("delete ingress calls = %d rv = %q", repo.ingressDeleteCalls, repo.lastIngressDeleteRV)
	}
	if len(auditor.events) != 3 || auditor.events[0].Action != "create-ingress" || auditor.events[1].Action != "update-ingress" || auditor.events[2].Action != "delete-ingress" {
		t.Fatalf("audit = %#v", auditor.events)
	}

	before := repo.ingressCreateCalls
	if _, err := service.CreateIngress(context.Background(), actor, domain.IngressCreateSpec{
		Name: "invalid", Namespace: "prod", Paths: []domain.IngressPathInput{{Host: "bad_host", Path: "/", Service: "web", Port: "80"}},
	}); err == nil {
		t.Fatal("CreateIngress() accepted invalid host")
	}
	if repo.ingressCreateCalls != before {
		t.Fatalf("invalid ingress reached repository: %d -> %d", before, repo.ingressCreateCalls)
	}
}
