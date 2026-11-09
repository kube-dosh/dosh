package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	ListConfigMaps(ctx context.Context, query domain.ListQuery) ([]domain.ConfigMapSummary, error)
	ListConfigMapsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.ConfigMapSummary], error)
	GetConfigMap(ctx context.Context, namespace, name string) (domain.ConfigMapDetail, error)
	CreateConfigMap(ctx context.Context, spec domain.ConfigMapCreateSpec) (domain.ConfigMapDetail, error)
	UpdateConfigMap(ctx context.Context, namespace, name string, update domain.ConfigMapUpdateSpec) (domain.ConfigMapDetail, error)
	DeleteConfigMap(ctx context.Context, namespace, name, resourceVersion string) error
	ListSecrets(ctx context.Context, query domain.ListQuery) ([]domain.SecretSummary, error)
	ListSecretsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.SecretSummary], error)
	RevealSecret(ctx context.Context, namespace, name string) (domain.SecretReveal, error)
	CreateSecret(ctx context.Context, spec domain.SecretWriteSpec) (domain.SecretSummary, error)
	UpdateSecret(ctx context.Context, namespace, name string, update domain.SecretWriteSpec) (domain.SecretSummary, error)
	DeleteSecret(ctx context.Context, namespace, name, resourceVersion string) error
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) ListConfigMaps(ctx context.Context, query domain.ListQuery) ([]domain.ConfigMapSummary, error) {
	return s.repository.ListConfigMaps(ctx, query)
}

func (s *Service) ListConfigMapsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.ConfigMapSummary], error) {
	return s.repository.ListConfigMapsPage(ctx, query, page)
}

func (s *Service) GetConfigMap(ctx context.Context, namespace, name string) (domain.ConfigMapDetail, error) {
	return s.repository.GetConfigMap(ctx, namespace, name)
}

func (s *Service) ListSecrets(ctx context.Context, query domain.ListQuery) ([]domain.SecretSummary, error) {
	return s.repository.ListSecrets(ctx, query)
}

func (s *Service) ListSecretsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.SecretSummary], error) {
	return s.repository.ListSecretsPage(ctx, query, page)
}

func (s *Service) RevealSecret(ctx context.Context, actor domain.Actor, namespace, name string) (domain.SecretReveal, error) {
	access := domain.Access{Verb: "get", Resource: "secrets", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "reveal", Resource: "secret", Namespace: namespace, Name: name, Result: "denied", Err: err})
		return domain.SecretReveal{}, err
	}
	reveal, err := s.repository.RevealSecret(ctx, namespace, name)
	result := "success"
	if err != nil {
		result = "failure"
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: "reveal", Resource: "secret", Namespace: namespace, Name: name, Result: result, Err: err})
	return reveal, err
}
