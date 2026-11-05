package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	ListServices(ctx context.Context, query domain.ListQuery) ([]domain.ServiceSummary, error)
	GetService(ctx context.Context, namespace, name string) (domain.ServiceDetail, error)
	ListIngresses(ctx context.Context, query domain.ListQuery) ([]domain.IngressSummary, error)
	GetIngress(ctx context.Context, namespace, name string) (domain.IngressDetail, error)
	ListIngressClasses(ctx context.Context, query domain.ListQuery) ([]domain.IngressClassSummary, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) ListServices(ctx context.Context, query domain.ListQuery) ([]domain.ServiceSummary, error) {
	return s.repository.ListServices(ctx, query)
}

func (s *Service) GetService(ctx context.Context, namespace, name string) (domain.ServiceDetail, error) {
	return s.repository.GetService(ctx, namespace, name)
}

func (s *Service) ListIngresses(ctx context.Context, query domain.ListQuery) ([]domain.IngressSummary, error) {
	return s.repository.ListIngresses(ctx, query)
}

func (s *Service) GetIngress(ctx context.Context, namespace, name string) (domain.IngressDetail, error) {
	return s.repository.GetIngress(ctx, namespace, name)
}

func (s *Service) ListIngressClasses(ctx context.Context, query domain.ListQuery) ([]domain.IngressClassSummary, error) {
	return s.repository.ListIngressClasses(ctx, query)
}
