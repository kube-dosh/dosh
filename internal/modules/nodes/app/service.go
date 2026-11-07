package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"dosh/internal/domain"
)

type Repository interface {
	ListNodes(ctx context.Context, query domain.ListQuery) ([]domain.NodeSummary, error)
	GetNode(ctx context.Context, name string) (domain.NodeDetail, error)
	SetNodeScheduling(ctx context.Context, name string, unschedulable bool) (domain.NodeSummary, error)
	SetNodeLabels(ctx context.Context, name string, update domain.NodeLabelUpdate) (domain.NodeDetail, error)
	SetNodeTaints(ctx context.Context, name string, update domain.NodeTaintUpdate) (domain.NodeDetail, error)
	ListPodsForNode(ctx context.Context, node string) ([]domain.PodSummary, error)
	ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error)
	CreateNamespace(ctx context.Context, spec domain.NamespaceCreateSpec) (domain.Namespace, error)
	GetNamespace(ctx context.Context, name string) (domain.NamespaceDetail, error)
	DeleteNamespace(ctx context.Context, name, resourceVersion string) error
}

var namespaceNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

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

func (s *Service) SetLabels(ctx context.Context, actor domain.Actor, name string, update domain.NodeLabelUpdate) (domain.NodeDetail, error) {
	return s.setMetadata(ctx, actor, name, "update-labels", func() (domain.NodeDetail, error) {
		labels, err := normalizeNodeLabels(update.Labels)
		if err != nil {
			return domain.NodeDetail{}, err
		}
		update.Labels = labels
		return s.repository.SetNodeLabels(ctx, name, update)
	})
}

func (s *Service) SetTaints(ctx context.Context, actor domain.Actor, name string, update domain.NodeTaintUpdate) (domain.NodeDetail, error) {
	return s.setMetadata(ctx, actor, name, "update-taints", func() (domain.NodeDetail, error) {
		taints, err := normalizeNodeTaints(update.Taints)
		if err != nil {
			return domain.NodeDetail{}, err
		}
		update.Taints = taints
		return s.repository.SetNodeTaints(ctx, name, update)
	})
}

func (s *Service) setMetadata(ctx context.Context, actor domain.Actor, name, action string, update func() (domain.NodeDetail, error)) (domain.NodeDetail, error) {
	access := domain.Access{Verb: "update", Resource: "nodes", Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, name, err)
		return domain.NodeDetail{}, err
	}
	detail, err := update()
	s.audit(ctx, actor, action, name, err)
	return detail, err
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

func (s *Service) CreateNamespace(ctx context.Context, actor domain.Actor, spec domain.NamespaceCreateSpec) (domain.Namespace, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" || len(spec.Name) > 63 || !namespaceNamePattern.MatchString(spec.Name) {
		err := &domain.ValidationError{Field: "name", Message: "namespace name must be a valid DNS-1123 label of at most 63 characters"}
		s.auditNamespace(ctx, actor, "create-namespace", spec.Name, err)
		return domain.Namespace{}, err
	}
	access := domain.Access{Verb: "create", Resource: "namespaces", Name: spec.Name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditNamespace(ctx, actor, "create-namespace", spec.Name, err)
		return domain.Namespace{}, err
	}
	created, err := s.repository.CreateNamespace(ctx, spec)
	s.auditNamespace(ctx, actor, "create-namespace", spec.Name, err)
	return created, err
}

func (s *Service) GetNamespace(ctx context.Context, name string) (domain.NamespaceDetail, error) {
	return s.repository.GetNamespace(ctx, name)
}

func (s *Service) DeleteNamespace(ctx context.Context, actor domain.Actor, name, resourceVersion string) error {
	if name == "" || strings.HasPrefix(name, "kube-") || name == "default" {
		err := &domain.ValidationError{Field: "name", Message: fmt.Sprintf("namespace %q is protected and cannot be deleted", name)}
		s.auditNamespace(ctx, actor, "delete-namespace", name, err)
		return err
	}
	access := domain.Access{Verb: "delete", Resource: "namespaces", Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditNamespace(ctx, actor, "delete-namespace", name, err)
		return err
	}
	err := s.repository.DeleteNamespace(ctx, name, resourceVersion)
	s.auditNamespace(ctx, actor, "delete-namespace", name, err)
	return err
}

func (s *Service) auditNamespace(ctx context.Context, actor domain.Actor, action, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "namespace", Name: name, Result: result, Err: err})
}
