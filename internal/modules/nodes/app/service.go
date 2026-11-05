package app

import (
	"context"
	"errors"

	"dosh/internal/domain"
)

type Repository interface {
	ListNodes(ctx context.Context, query domain.ListQuery) ([]domain.NodeSummary, error)
	GetNode(ctx context.Context, name string) (domain.NodeDetail, error)
	SetNodeScheduling(ctx context.Context, name string, unschedulable bool) (domain.NodeSummary, error)
	ListPodsForNode(ctx context.Context, node string) ([]domain.PodSummary, error)
	ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error)
	GetNamespace(ctx context.Context, name string) (domain.NamespaceDetail, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) List(ctx context.Context, query domain.ListQuery) ([]domain.NodeSummary, error) {
	return s.repository.ListNodes(ctx, query)
}

func (s *Service) Get(ctx context.Context, name string) (domain.NodeDetail, error) {
	detail, err := s.repository.GetNode(ctx, name)
	if err != nil {
		return domain.NodeDetail{}, err
	}
	pods, err := s.repository.ListPodsForNode(ctx, name)
	if err != nil {
		return domain.NodeDetail{}, err
	}
	detail.Pods = pods
	return detail, nil
}

func (s *Service) Cordon(ctx context.Context, actor domain.Actor, name string) (domain.NodeSummary, error) {
	return s.setScheduling(ctx, actor, name, "cordon", true)
}

func (s *Service) Uncordon(ctx context.Context, actor domain.Actor, name string) (domain.NodeSummary, error) {
	return s.setScheduling(ctx, actor, name, "uncordon", false)
}

func (s *Service) setScheduling(ctx context.Context, actor domain.Actor, name, action string, unschedulable bool) (domain.NodeSummary, error) {
	access := domain.Access{Verb: "update", Resource: "nodes", Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, name, err)
		return domain.NodeSummary{}, err
	}
	summary, err := s.repository.SetNodeScheduling(ctx, name, unschedulable)
	s.audit(ctx, actor, action, name, err)
	return summary, err
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "node", Name: name, Result: result, Err: err})
}

func (s *Service) ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error) {
	return s.repository.ListNamespaces(ctx, query)
}

func (s *Service) GetNamespace(ctx context.Context, name string) (domain.NamespaceDetail, error) {
	return s.repository.GetNamespace(ctx, name)
}
