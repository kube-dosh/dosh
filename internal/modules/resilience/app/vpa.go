package app

import (
	"context"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

// VpaRepository is implemented by the Kubernetes adapter. Keeping it separate
// from the HPA/PDB interface preserves focused test doubles while allowing the
// VPA workflow to remain optional on clusters without the CRD installed.
type VpaRepository interface {
	ListVerticalAutoscalers(ctx context.Context, namespace string) ([]domain.VerticalAutoscalerSummary, error)
	UpsertVerticalAutoscaler(ctx context.Context, spec domain.VerticalAutoscalerSpec, dryRun bool) (domain.VerticalAutoscalerSummary, error)
	DeleteVerticalAutoscaler(ctx context.Context, namespace, name, resourceVersion string) error
}

// VPAQuantityValidator keeps Kubernetes quantity parsing in the infrastructure
// adapter. The application layer owns field and policy validation but must not
// depend on k8s.io/apimachinery solely to parse resource.Quantity values.
type VPAQuantityValidator interface {
	ValidateVPAQuantities(spec domain.VerticalAutoscalerSpec) error
}

var vpaUpdateModes = map[string]struct{}{
	"Off": {}, "Initial": {}, "Recreate": {}, "Auto": {}, "InPlaceOrRecreate": {},
}

var vpaContainerModes = map[string]struct{}{
	"": {}, "Off": {}, "Auto": {},
}

var vpaControlledValues = map[string]struct{}{
	"": {}, "RequestsAndLimits": {}, "RequestsOnly": {},
}

func (s *Service) ListVerticalAutoscalers(ctx context.Context, actor domain.Actor, namespace string) ([]domain.VerticalAutoscalerSummary, error) {
	namespace = strings.TrimSpace(namespace)
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, actor, "list-vertical-autoscalers", "verticalpodautoscaler", namespace, "", "list", "autoscaling.k8s.io", "verticalpodautoscalers"); err != nil {
		return nil, err
	}
	repository, ok := s.repository.(VpaRepository)
	if !ok {
		return nil, fmt.Errorf("%w: VPA repository is unavailable", domain.ErrUnsupported)
	}
	return repository.ListVerticalAutoscalers(ctx, namespace)
}

func (s *Service) UpsertVerticalAutoscaler(ctx context.Context, actor domain.Actor, spec domain.VerticalAutoscalerSpec) (domain.VerticalAutoscalerSummary, error) {
	action := "update-vertical-autoscaler"
	if strings.TrimSpace(spec.ResourceVersion) == "" {
		action = "create-vertical-autoscaler"
	}
	if err := normalizeVerticalAutoscalerSpec(&spec); err != nil {
		s.audit(ctx, actor, action, "verticalpodautoscaler", spec.Namespace, spec.Name, err)
		return domain.VerticalAutoscalerSummary{}, err
	}
	if validator, ok := s.repository.(VPAQuantityValidator); ok {
		if err := validator.ValidateVPAQuantities(spec); err != nil {
			s.audit(ctx, actor, action, "verticalpodautoscaler", spec.Namespace, spec.Name, err)
			return domain.VerticalAutoscalerSummary{}, err
		}
	}
	verb := "update"
	if spec.ResourceVersion == "" {
		verb = "create"
	}
	if err := s.authorize(ctx, actor, action, "verticalpodautoscaler", spec.Namespace, spec.Name, verb, "autoscaling.k8s.io", "verticalpodautoscalers"); err != nil {
		return domain.VerticalAutoscalerSummary{}, err
	}
	repository, ok := s.repository.(VpaRepository)
	if !ok {
		return domain.VerticalAutoscalerSummary{}, fmt.Errorf("%w: VPA repository is unavailable", domain.ErrUnsupported)
	}
	if _, err := repository.UpsertVerticalAutoscaler(ctx, spec, true); err != nil {
		s.audit(ctx, actor, action, "verticalpodautoscaler", spec.Namespace, spec.Name, err)
		return domain.VerticalAutoscalerSummary{}, err
	}
	item, err := repository.UpsertVerticalAutoscaler(ctx, spec, false)
	s.audit(ctx, actor, action, "verticalpodautoscaler", spec.Namespace, spec.Name, err)
	return item, err
}

func (s *Service) DeleteVerticalAutoscaler(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	resourceVersion = strings.TrimSpace(resourceVersion)
	if err := validateIdentity(namespace, name); err != nil {
		s.audit(ctx, actor, "delete-vertical-autoscaler", "verticalpodautoscaler", namespace, name, err)
		return err
	}
	if resourceVersion == "" {
		err := validationError("resourceVersion", "resourceVersion is required to delete a VerticalPodAutoscaler")
		s.audit(ctx, actor, "delete-vertical-autoscaler", "verticalpodautoscaler", namespace, name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.audit(ctx, actor, "delete-vertical-autoscaler", "verticalpodautoscaler", namespace, name, err)
		return err
	}
	if err := s.authorize(ctx, actor, "delete-vertical-autoscaler", "verticalpodautoscaler", namespace, name, "delete", "autoscaling.k8s.io", "verticalpodautoscalers"); err != nil {
		return err
	}
	repository, ok := s.repository.(VpaRepository)
	if !ok {
		return fmt.Errorf("%w: VPA repository is unavailable", domain.ErrUnsupported)
	}
	err := repository.DeleteVerticalAutoscaler(ctx, namespace, name, resourceVersion)
	s.audit(ctx, actor, "delete-vertical-autoscaler", "verticalpodautoscaler", namespace, name, err)
	return err
}

func normalizeVerticalAutoscalerSpec(spec *domain.VerticalAutoscalerSpec) error {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Namespace = strings.TrimSpace(spec.Namespace)
	spec.TargetAPIVersion = strings.TrimSpace(spec.TargetAPIVersion)
	spec.TargetKind = strings.TrimSpace(spec.TargetKind)
	spec.TargetName = strings.TrimSpace(spec.TargetName)
	spec.UpdatePolicy = strings.TrimSpace(spec.UpdatePolicy)
	spec.ResourceVersion = strings.TrimSpace(spec.ResourceVersion)
	if err := validateIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	if err := validateResourceVersion(spec.ResourceVersion); err != nil {
		return err
	}
	if spec.TargetAPIVersion == "" {
		spec.TargetAPIVersion = "apps/v1"
	}
	switch spec.TargetKind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
	default:
		return validationError("targetKind", "targetKind must be Deployment, StatefulSet, DaemonSet or ReplicaSet")
	}
	if spec.TargetAPIVersion != "apps/v1" {
		return validationError("targetApiVersion", "targetApiVersion must be apps/v1 for the supported workload targets")
	}
	if err := validateDNSLabel("targetName", spec.TargetName); err != nil {
		return err
	}
	if spec.UpdatePolicy == "" {
		spec.UpdatePolicy = "Off"
	}
	if _, ok := vpaUpdateModes[spec.UpdatePolicy]; !ok {
		return validationError("updatePolicy", "updatePolicy must be Off, Initial, Recreate, Auto or InPlaceOrRecreate")
	}
	if spec.MinReplicas != nil && (*spec.MinReplicas < 1 || *spec.MinReplicas > 1000) {
		return validationError("minReplicas", "minReplicas must be between 1 and 1000")
	}
	if len(spec.ContainerPolicies) > 16 {
		return validationError("containerPolicies", "at most 16 container policies are supported")
	}
	seen := map[string]struct{}{}
	for index := range spec.ContainerPolicies {
		policy := &spec.ContainerPolicies[index]
		policy.ContainerName = strings.TrimSpace(policy.ContainerName)
		policy.Mode = strings.TrimSpace(policy.Mode)
		policy.ControlledValues = strings.TrimSpace(policy.ControlledValues)
		if policy.ContainerName == "" {
			policy.ContainerName = "*"
		}
		if policy.ContainerName != "*" {
			if err := validateDNSLabel("containerPolicies", policy.ContainerName); err != nil {
				return err
			}
		}
		if _, ok := seen[policy.ContainerName]; ok {
			return validationError("containerPolicies", "container %q appears more than once", policy.ContainerName)
		}
		seen[policy.ContainerName] = struct{}{}
		if _, ok := vpaContainerModes[policy.Mode]; !ok {
			return validationError("containerPolicies", "container mode must be Off or Auto")
		}
		if _, ok := vpaControlledValues[policy.ControlledValues]; !ok {
			return validationError("containerPolicies", "controlledValues must be RequestsAndLimits or RequestsOnly")
		}
		policy.MinAllowed, policy.MaxAllowed = normalizeVPAResources(policy.MinAllowed), normalizeVPAResources(policy.MaxAllowed)
		policy.ControlledResources = normalizeControlledResources(policy.ControlledResources)
	}
	return nil
}

func normalizeVPAResources(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, 2)
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if (key != "cpu" && key != "memory") || value == "" {
			continue
		}
		result[key] = value
	}
	return result
}

func normalizeControlledResources(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 2)
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "cpu" && value != "memory" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
