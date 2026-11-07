package app

import (
	"context"
	"errors"

	"dosh/internal/domain"
)

type Repository interface {
	ListServices(ctx context.Context, query domain.ListQuery) ([]domain.ServiceSummary, error)
	GetService(ctx context.Context, namespace, name string) (domain.ServiceDetail, error)
	CreateService(ctx context.Context, spec domain.ServiceCreateSpec) (domain.ServiceDetail, error)
	UpdateService(ctx context.Context, namespace, name string, update domain.ServiceUpdateSpec) (domain.ServiceDetail, error)
	DeleteService(ctx context.Context, namespace, name, resourceVersion string) error
	ListIngresses(ctx context.Context, query domain.ListQuery) ([]domain.IngressSummary, error)
	GetIngress(ctx context.Context, namespace, name string) (domain.IngressDetail, error)
	CreateIngress(ctx context.Context, spec domain.IngressCreateSpec) (domain.IngressDetail, error)
	UpdateIngress(ctx context.Context, namespace, name string, update domain.IngressUpdateSpec) (domain.IngressDetail, error)
	DeleteIngress(ctx context.Context, namespace, name, resourceVersion string) error
	ListIngressClasses(ctx context.Context, query domain.ListQuery) ([]domain.IngressClassSummary, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) ListServices(ctx context.Context, query domain.ListQuery) ([]domain.ServiceSummary, error) {
	return s.repository.ListServices(ctx, query)
}

func (s *Service) GetService(ctx context.Context, namespace, name string) (domain.ServiceDetail, error) {
	return s.repository.GetService(ctx, namespace, name)
}

func (s *Service) CreateService(ctx context.Context, actor domain.Actor, spec domain.ServiceCreateSpec) (domain.ServiceDetail, error) {
	if err := normalizeServiceCreateSpec(&spec); err != nil {
		s.audit(ctx, actor, "create-service", spec.Namespace, spec.Name, err)
		return domain.ServiceDetail{}, err
	}
	if err := s.authorize(ctx, actor, "create-service", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.ServiceDetail{}, err
	}
	detail, err := s.repository.CreateService(ctx, spec)
	s.audit(ctx, actor, "create-service", spec.Namespace, spec.Name, err)
	return detail, err
}

func (s *Service) UpdateService(ctx context.Context, actor domain.Actor, namespace, name string, update domain.ServiceUpdateSpec) (domain.ServiceDetail, error) {
	if err := normalizeServiceUpdateSpec(namespace, name, &update); err != nil {
		s.audit(ctx, actor, "update-service", namespace, name, err)
		return domain.ServiceDetail{}, err
	}
	if err := s.authorize(ctx, actor, "update-service", namespace, name, "update"); err != nil {
		return domain.ServiceDetail{}, err
	}
	current, err := s.repository.GetService(ctx, namespace, name)
	if err != nil {
		s.audit(ctx, actor, "update-service", namespace, name, err)
		return domain.ServiceDetail{}, err
	}
	if current.Headless != update.Headless {
		err := serviceValidationError("headless", "headless mode cannot be changed after Service creation")
		s.audit(ctx, actor, "update-service", namespace, name, err)
		return domain.ServiceDetail{}, err
	}
	detail, err := s.repository.UpdateService(ctx, namespace, name, update)
	s.audit(ctx, actor, "update-service", namespace, name, err)
	return detail, err
}

func (s *Service) DeleteService(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validateServiceIdentity(namespace, name); err != nil {
		s.audit(ctx, actor, "delete-service", namespace, name, err)
		return err
	}
	if err := s.authorize(ctx, actor, "delete-service", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeleteService(ctx, namespace, name, resourceVersion)
	s.audit(ctx, actor, "delete-service", namespace, name, err)
	return err
}

func (s *Service) authorize(ctx context.Context, actor domain.Actor, action, namespace, name, verb string) error {
	access := domain.Access{Verb: verb, Resource: "services", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, namespace, name, err)
		return err
	}
	return nil
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "service", Namespace: namespace, Name: name, Result: result, Err: err})
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
