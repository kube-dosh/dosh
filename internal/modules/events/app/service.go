package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	ListEvents(ctx context.Context, query domain.ListQuery) ([]domain.Event, error)
	ListEventsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.Event], error)
	WatchEvents(ctx context.Context, namespace string) (<-chan domain.Event, <-chan error, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, query domain.ListQuery) ([]domain.Event, error) {
	return s.repository.ListEvents(ctx, query)
}

func (s *Service) ListPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.Event], error) {
	return s.repository.ListEventsPage(ctx, query, page)
}

func (s *Service) Watch(ctx context.Context, namespace string) (<-chan domain.Event, <-chan error, error) {
	return s.repository.WatchEvents(ctx, namespace)
}
