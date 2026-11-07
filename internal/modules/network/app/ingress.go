package app

import (
	"context"
	"errors"

	"dosh/internal/domain"
)

func (s *Service) CreateIngress(ctx context.Context, actor domain.Actor, spec domain.IngressCreateSpec) (domain.IngressDetail, error) {
	if err := normalizeIngressCreateSpec(&spec); err != nil {
		s.auditIngress(ctx, actor, "create-ingress", spec.Namespace, spec.Name, err)
		return domain.IngressDetail{}, err
	}
	if err := s.authorizeIngress(ctx, actor, "create-ingress", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.IngressDetail{}, err
	}
	detail, err := s.repository.CreateIngress(ctx, spec)
	s.auditIngress(ctx, actor, "create-ingress", spec.Namespace, spec.Name, err)
	return detail, err
}

func (s *Service) UpdateIngress(ctx context.Context, actor domain.Actor, namespace, name string, update domain.IngressUpdateSpec) (domain.IngressDetail, error) {
	if err := normalizeIngressUpdateSpec(namespace, name, &update); err != nil {
		s.auditIngress(ctx, actor, "update-ingress", namespace, name, err)
		return domain.IngressDetail{}, err
	}
	if err := s.authorizeIngress(ctx, actor, "update-ingress", namespace, name, "update"); err != nil {
		return domain.IngressDetail{}, err
	}
	detail, err := s.repository.UpdateIngress(ctx, namespace, name, update)
	s.auditIngress(ctx, actor, "update-ingress", namespace, name, err)
	return detail, err
}

func (s *Service) DeleteIngress(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validateIngressIdentity(namespace, name); err != nil {
		s.auditIngress(ctx, actor, "delete-ingress", namespace, name, err)
		return err
	}
	if err := s.authorizeIngress(ctx, actor, "delete-ingress", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeleteIngress(ctx, namespace, name, resourceVersion)
	s.auditIngress(ctx, actor, "delete-ingress", namespace, name, err)
	return err
}

func (s *Service) authorizeIngress(ctx context.Context, actor domain.Actor, action, namespace, name, verb string) error {
	access := domain.Access{
		Verb: verb, APIGroup: "networking.k8s.io", Resource: "ingresses", Namespace: namespace, Name: name,
	}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditIngress(ctx, actor, action, namespace, name, err)
		return err
	}
	return nil
}

func (s *Service) auditIngress(ctx context.Context, actor domain.Actor, action, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "ingress", Namespace: namespace, Name: name, Result: result, Err: err})
}
