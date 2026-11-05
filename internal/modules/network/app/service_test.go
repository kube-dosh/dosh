package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type networkRepository struct {
	services       []domain.ServiceSummary
	service        domain.ServiceDetail
	ingresses      []domain.IngressSummary
	ingress        domain.IngressDetail
	ingressClasses []domain.IngressClassSummary
	err            error
}

func (r *networkRepository) ListServices(context.Context, domain.ListQuery) ([]domain.ServiceSummary, error) {
	return r.services, r.err
}
func (r *networkRepository) GetService(context.Context, string, string) (domain.ServiceDetail, error) {
	return r.service, r.err
}
func (r *networkRepository) ListIngresses(context.Context, domain.ListQuery) ([]domain.IngressSummary, error) {
	return r.ingresses, r.err
}
func (r *networkRepository) GetIngress(context.Context, string, string) (domain.IngressDetail, error) {
	return r.ingress, r.err
}

func (r *networkRepository) ListIngressClasses(context.Context, domain.ListQuery) ([]domain.IngressClassSummary, error) {
	return r.ingressClasses, r.err
}

func TestNetworkServiceListsResourcesAndFailures(t *testing.T) {
	repo := &networkRepository{
		services:       []domain.ServiceSummary{{Name: "api"}},
		service:        domain.ServiceDetail{ServiceSummary: domain.ServiceSummary{Name: "api"}, Endpoints: []domain.ServiceEndpoint{{Addresses: []string{"10.0.0.10"}, Ready: true}}},
		ingresses:      []domain.IngressSummary{{Name: "api"}},
		ingress:        domain.IngressDetail{IngressSummary: domain.IngressSummary{Name: "api"}, TLS: []domain.IngressTLS{{SecretName: "api-tls"}}},
		ingressClasses: []domain.IngressClassSummary{{Name: "nginx"}},
	}
	service := New(repo)
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
