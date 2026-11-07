package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	GetClusterConfiguration(ctx context.Context) (domain.ClusterConfiguration, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Get(ctx context.Context) (domain.ClusterConfiguration, error) {
	return s.repository.GetClusterConfiguration(ctx)
}
