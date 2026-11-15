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
	ListNodesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.NodeSummary], error)
	GetNode(ctx context.Context, name string) (domain.NodeDetail, error)
	SetNodeScheduling(ctx context.Context, name string, unschedulable bool) (domain.NodeSummary, error)
	PlanNodeDrain(ctx context.Context, name string, options domain.NodeDrainOptions) (domain.NodeDrainPlan, error)
	DrainNode(ctx context.Context, name string, options domain.NodeDrainOptions) (domain.NodeDrainResult, error)
	SetNodeLabels(ctx context.Context, name string, update domain.NodeLabelUpdate) (domain.NodeDetail, error)
	SetNodeTaints(ctx context.Context, name string, update domain.NodeTaintUpdate) (domain.NodeDetail, error)
	ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error)
	ListNamespacesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.Namespace], error)
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

func (s *Service) ListPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.NodeSummary], error) {
	return s.repository.ListNodesPage(ctx, query, page)
}

func (s *Service) Get(ctx context.Context, name string) (domain.NodeDetail, error) {
	return s.repository.GetNode(ctx, name)
}

func (s *Service) Cordon(ctx context.Context, actor domain.Actor, name string) (domain.NodeSummary, error) {
	return s.setScheduling(ctx, actor, name, "cordon", true)
}

func (s *Service) Uncordon(ctx context.Context, actor domain.Actor, name string) (domain.NodeSummary, error) {
	return s.setScheduling(ctx, actor, name, "uncordon", false)
}

// PlanDrain previews node eviction without mutating the cluster. Pod names are
// only returned after the caller's list access is verified in each namespace.
func (s *Service) PlanDrain(ctx context.Context, actor domain.Actor, name string, options domain.NodeDrainOptions) (domain.NodeDrainPlan, error) {
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "get", Resource: "nodes", Name: name}); err != nil {
		return domain.NodeDrainPlan{}, err
	}
	plan, err := s.repository.PlanNodeDrain(ctx, name, options)
	if err != nil {
		return domain.NodeDrainPlan{}, err
	}
	if err := s.authorizeDrainPods(ctx, actor, plan.Pods); err != nil {
		return domain.NodeDrainPlan{}, err
	}
	return plan, nil
}

func (s *Service) Drain(ctx context.Context, actor domain.Actor, name string, options domain.NodeDrainOptions) (domain.NodeDrainResult, error) {
	if options.Confirmation != name {
		return domain.NodeDrainResult{}, fmt.Errorf("%w: 排空节点需要输入节点名称 %q 进行二次确认", domain.ErrInvalid, name)
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "update", Resource: "nodes", Name: name}); err != nil {
		s.audit(ctx, actor, "drain", name, err)
		return domain.NodeDrainResult{}, err
	}
	plan, err := s.repository.PlanNodeDrain(ctx, name, options)
	if err != nil {
		s.audit(ctx, actor, "drain", name, err)
		return domain.NodeDrainResult{}, err
	}
	if err := s.authorizeDrainPods(ctx, actor, plan.Pods); err != nil {
		s.audit(ctx, actor, "drain", name, err)
		return domain.NodeDrainResult{}, err
	}
	for _, pod := range plan.Pods {
		if pod.Action != "evict" {
			continue
		}
		access := domain.Access{Verb: "create", Resource: "pods", Subresource: "eviction", Namespace: pod.Namespace, Name: pod.Name}
		if err := s.authorizer.Require(ctx, actor, access); err != nil {
			s.audit(ctx, actor, "drain", name, err)
			return domain.NodeDrainResult{}, err
		}
	}
	result, err := s.repository.DrainNode(ctx, name, options)
	s.audit(ctx, actor, "drain", name, err)
	return result, err
}

func (s *Service) authorizeDrainPods(ctx context.Context, actor domain.Actor, pods []domain.NodeDrainPod) error {
	checked := make(map[string]struct{})
	for _, pod := range pods {
		if _, ok := checked[pod.Namespace]; ok {
			continue
		}
		checked[pod.Namespace] = struct{}{}
		if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", Resource: "pods", Namespace: pod.Namespace}); err != nil {
			return err
		}
	}
	return nil
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

func (s *Service) ListNamespacesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.Namespace], error) {
	return s.repository.ListNamespacesPage(ctx, query, page)
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
