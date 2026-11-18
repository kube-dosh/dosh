package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

type Repository interface {
	ListAutoscalers(ctx context.Context, namespace string) ([]domain.AutoscalerSummary, error)
	UpsertAutoscaler(ctx context.Context, spec domain.AutoscalerSpec, dryRun bool) (domain.AutoscalerSummary, error)
	DeleteAutoscaler(ctx context.Context, namespace, name, resourceVersion string) error
	ListDisruptionBudgets(ctx context.Context, namespace string) ([]domain.DisruptionBudgetSummary, error)
	UpsertDisruptionBudget(ctx context.Context, spec domain.DisruptionBudgetSpec, dryRun bool) (domain.DisruptionBudgetSummary, error)
	DeleteDisruptionBudget(ctx context.Context, namespace, name, resourceVersion string) error
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

var (
	dnsLabelPattern   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	labelValuePattern = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)
	quantityPattern   = regexp.MustCompile(`^([0-9]+)(%?)$`)
)

func (s *Service) ListAutoscalers(ctx context.Context, actor domain.Actor, namespace string) ([]domain.AutoscalerSummary, error) {
	namespace = strings.TrimSpace(namespace)
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, actor, "list-autoscalers", "horizontalpodautoscaler", namespace, "", "list", "autoscaling", "horizontalpodautoscalers"); err != nil {
		return nil, err
	}
	return s.repository.ListAutoscalers(ctx, namespace)
}

func (s *Service) UpsertAutoscaler(ctx context.Context, actor domain.Actor, spec domain.AutoscalerSpec) (domain.AutoscalerSummary, error) {
	action := "update-autoscaler"
	if spec.ResourceVersion == "" {
		action = "create-autoscaler"
	}
	if err := normalizeAutoscalerSpec(&spec); err != nil {
		s.audit(ctx, actor, action, "horizontalpodautoscaler", spec.Namespace, spec.Name, err)
		return domain.AutoscalerSummary{}, err
	}
	verb := "create"
	if spec.ResourceVersion != "" {
		verb = "update"
	}
	if err := s.authorize(ctx, actor, action, "horizontalpodautoscaler", spec.Namespace, spec.Name, verb, "autoscaling", "horizontalpodautoscalers"); err != nil {
		return domain.AutoscalerSummary{}, err
	}
	if spec.ResourceVersion == "" {
		if _, err := s.repository.UpsertAutoscaler(ctx, spec, true); err != nil {
			s.audit(ctx, actor, action, "horizontalpodautoscaler", spec.Namespace, spec.Name, err)
			return domain.AutoscalerSummary{}, err
		}
	}
	item, err := s.repository.UpsertAutoscaler(ctx, spec, false)
	s.audit(ctx, actor, action, "horizontalpodautoscaler", spec.Namespace, spec.Name, err)
	return item, err
}

func (s *Service) DeleteAutoscaler(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	if err := validateIdentity(namespace, name); err != nil {
		s.audit(ctx, actor, "delete-autoscaler", "horizontalpodautoscaler", namespace, name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.audit(ctx, actor, "delete-autoscaler", "horizontalpodautoscaler", namespace, name, err)
		return err
	}
	if err := s.authorize(ctx, actor, "delete-autoscaler", "horizontalpodautoscaler", namespace, name, "delete", "autoscaling", "horizontalpodautoscalers"); err != nil {
		return err
	}
	err := s.repository.DeleteAutoscaler(ctx, namespace, name, resourceVersion)
	s.audit(ctx, actor, "delete-autoscaler", "horizontalpodautoscaler", namespace, name, err)
	return err
}

func (s *Service) ListDisruptionBudgets(ctx context.Context, actor domain.Actor, namespace string) ([]domain.DisruptionBudgetSummary, error) {
	namespace = strings.TrimSpace(namespace)
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, actor, "list-disruption-budgets", "poddisruptionbudget", namespace, "", "list", "policy", "poddisruptionbudgets"); err != nil {
		return nil, err
	}
	return s.repository.ListDisruptionBudgets(ctx, namespace)
}

func (s *Service) UpsertDisruptionBudget(ctx context.Context, actor domain.Actor, spec domain.DisruptionBudgetSpec) (domain.DisruptionBudgetSummary, error) {
	action := "update-disruption-budget"
	if spec.ResourceVersion == "" {
		action = "create-disruption-budget"
	}
	if err := normalizeDisruptionBudgetSpec(&spec); err != nil {
		s.audit(ctx, actor, action, "poddisruptionbudget", spec.Namespace, spec.Name, err)
		return domain.DisruptionBudgetSummary{}, err
	}
	verb := "create"
	if spec.ResourceVersion != "" {
		verb = "update"
	}
	if err := s.authorize(ctx, actor, action, "poddisruptionbudget", spec.Namespace, spec.Name, verb, "policy", "poddisruptionbudgets"); err != nil {
		return domain.DisruptionBudgetSummary{}, err
	}
	if spec.ResourceVersion == "" {
		if _, err := s.repository.UpsertDisruptionBudget(ctx, spec, true); err != nil {
			s.audit(ctx, actor, action, "poddisruptionbudget", spec.Namespace, spec.Name, err)
			return domain.DisruptionBudgetSummary{}, err
		}
	}
	item, err := s.repository.UpsertDisruptionBudget(ctx, spec, false)
	s.audit(ctx, actor, action, "poddisruptionbudget", spec.Namespace, spec.Name, err)
	return item, err
}

func (s *Service) DeleteDisruptionBudget(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	if err := validateIdentity(namespace, name); err != nil {
		s.audit(ctx, actor, "delete-disruption-budget", "poddisruptionbudget", namespace, name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.audit(ctx, actor, "delete-disruption-budget", "poddisruptionbudget", namespace, name, err)
		return err
	}
	if err := s.authorize(ctx, actor, "delete-disruption-budget", "poddisruptionbudget", namespace, name, "delete", "policy", "poddisruptionbudgets"); err != nil {
		return err
	}
	err := s.repository.DeleteDisruptionBudget(ctx, namespace, name, resourceVersion)
	s.audit(ctx, actor, "delete-disruption-budget", "poddisruptionbudget", namespace, name, err)
	return err
}

func normalizeAutoscalerSpec(spec *domain.AutoscalerSpec) error {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Namespace = strings.TrimSpace(spec.Namespace)
	spec.TargetName = strings.TrimSpace(spec.TargetName)
	spec.TargetKind = strings.TrimSpace(spec.TargetKind)
	spec.ResourceVersion = strings.TrimSpace(spec.ResourceVersion)
	if err := validateIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	if err := validateResourceVersion(spec.ResourceVersion); err != nil {
		return err
	}
	if spec.TargetKind == "" {
		spec.TargetKind = "Deployment"
	}
	switch spec.TargetKind {
	case "Deployment", "StatefulSet":
	default:
		return validationError("targetKind", "targetKind must be Deployment or StatefulSet")
	}
	if err := validateDNSLabel("targetName", spec.TargetName); err != nil {
		return err
	}
	if spec.MinReplicas < 1 || spec.MinReplicas > 1000 {
		return validationError("minReplicas", "minReplicas must be between 1 and 1000")
	}
	if spec.MaxReplicas < spec.MinReplicas || spec.MaxReplicas > 1000 {
		return validationError("maxReplicas", "maxReplicas must be at least minReplicas and at most 1000")
	}
	if spec.CPUUtilization == nil && spec.MemoryUtilization == nil {
		return validationError("metrics", "at least one CPU or memory utilization target is required")
	}
	for field, value := range map[string]*int32{"cpuUtilization": spec.CPUUtilization, "memoryUtilization": spec.MemoryUtilization} {
		if value != nil && (*value < 1 || *value > 100) {
			return validationError(field, "%s must be between 1 and 100", field)
		}
	}
	return nil
}

func normalizeDisruptionBudgetSpec(spec *domain.DisruptionBudgetSpec) error {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Namespace = strings.TrimSpace(spec.Namespace)
	spec.MinAvailable = strings.TrimSpace(spec.MinAvailable)
	spec.MaxUnavailable = strings.TrimSpace(spec.MaxUnavailable)
	spec.ResourceVersion = strings.TrimSpace(spec.ResourceVersion)
	if err := validateIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	if err := validateResourceVersion(spec.ResourceVersion); err != nil {
		return err
	}
	if (spec.MinAvailable == "") == (spec.MaxUnavailable == "") {
		return validationError("availability", "exactly one of minAvailable and maxUnavailable is required")
	}
	for field, value := range map[string]string{"minAvailable": spec.MinAvailable, "maxUnavailable": spec.MaxUnavailable} {
		if value != "" {
			if err := validateDisruptionValue(field, value); err != nil {
				return err
			}
		}
	}
	if len(spec.Selector) == 0 || len(spec.Selector) > 16 {
		return validationError("selector", "selector must contain between 1 and 16 labels")
	}
	for key, value := range spec.Selector {
		if err := validateLabel(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateDisruptionValue(field, value string) error {
	matches := quantityPattern.FindStringSubmatch(value)
	if matches == nil {
		return validationError(field, "%s must be a non-negative integer or percentage", field)
	}
	number, err := strconv.Atoi(matches[1])
	if err != nil || number < 0 || (matches[2] == "%" && number > 100) {
		return validationError(field, "%s is out of range", field)
	}
	return nil
}

func validateIdentity(namespace, name string) error {
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	return validateDNSLabel("name", name)
}

func validateNamespace(namespace string) error {
	if namespace == "" {
		return validationError("namespace", "namespace is required")
	}
	return validateDNSLabel("namespace", namespace)
}

func validateDNSLabel(field, value string) error {
	if value == "" || len(value) > 63 || !dnsLabelPattern.MatchString(value) {
		return validationError(field, "%s must be a DNS-1123 label", field)
	}
	return nil
}

func validateResourceVersion(value string) error {
	if len(value) > 256 {
		return validationError("resourceVersion", "resourceVersion is too long")
	}
	return nil
}

func validateLabel(key, value string) error {
	if key == "" || len(key) > 253 || strings.Count(key, "/") > 1 {
		return validationError("selector", "label key %q is invalid", key)
	}
	parts := strings.SplitN(key, "/", 2)
	name := parts[len(parts)-1]
	if name == "" || len(name) > 63 || !labelValuePattern.MatchString(name) {
		return validationError("selector", "selector label name %q is invalid", name)
	}
	if len(value) > 63 || !labelValuePattern.MatchString(value) {
		return validationError("selector", "selector label value for %q is invalid", key)
	}
	return nil
}

func validationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func (s *Service) authorize(ctx context.Context, actor domain.Actor, action, resource, namespace, name, verb, group, apiResource string) error {
	access := domain.Access{Verb: verb, APIGroup: group, Resource: apiResource, Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, resource, namespace, name, err)
		return err
	}
	return nil
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action, resource, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: resource, Namespace: namespace, Name: name, Result: result, Err: err})
}
