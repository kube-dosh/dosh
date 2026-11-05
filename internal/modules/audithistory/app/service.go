package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	ListHistory(ctx context.Context, query domain.AuditQuery) ([]domain.AuditRecord, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
}

func New(repository Repository, authorizer domain.Authorizer) *Service {
	return &Service{repository: repository, authorizer: authorizer}
}

func (s *Service) List(ctx context.Context, query domain.AuditQuery) ([]domain.AuditRecord, error) {
	actor, ok := domain.ActorFromContext(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "get", APIGroup: "dosh.io", Resource: "audit-events"}); err != nil {
		return nil, err
	}
	return s.repository.ListHistory(ctx, query)
}
