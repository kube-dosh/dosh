package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

const maxPodTemplateBytes = 1 << 20

// PodTemplateRepository is kept separate from the broad workload repository
// interface so focused tests and installations can still use the image and
// container configuration paths independently.
type PodTemplateRepository interface {
	GetWorkloadPodTemplate(ctx context.Context, kind, namespace, name string) (domain.WorkloadPodTemplate, error)
	UpdateWorkloadPodTemplate(ctx context.Context, kind, namespace, name string, update domain.WorkloadPodTemplateUpdate) (domain.WorkloadPodTemplateResult, error)
	GetWorkloadContainerInventory(ctx context.Context, kind, namespace, name string) (domain.WorkloadContainerInventory, error)
}

func (s *Service) templateRepository() (PodTemplateRepository, error) {
	repository, ok := s.repository.(PodTemplateRepository)
	if !ok {
		return nil, fmt.Errorf("%w: pod template editing is unavailable", domain.ErrUnsupported)
	}
	return repository, nil
}

func (s *Service) GetPodTemplate(ctx context.Context, actor domain.Actor, kind, namespace, name string) (domain.WorkloadPodTemplate, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if err := ValidateKind(kind); err != nil {
		return domain.WorkloadPodTemplate{}, err
	}
	if err := s.authorize(ctx, actor, "read-pod-template", kind, namespace, name, workloadAccess(kind, "get", namespace, name, "")); err != nil {
		return domain.WorkloadPodTemplate{}, err
	}
	repository, err := s.templateRepository()
	if err != nil {
		s.audit(ctx, actor, "read-pod-template", kind, namespace, name, err)
		return domain.WorkloadPodTemplate{}, err
	}
	return repository.GetWorkloadPodTemplate(ctx, kind, namespace, name)
}

func (s *Service) GetContainerInventory(ctx context.Context, actor domain.Actor, kind, namespace, name string) (domain.WorkloadContainerInventory, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if err := ValidateKind(kind); err != nil {
		return domain.WorkloadContainerInventory{}, err
	}
	if err := s.authorize(ctx, actor, "read-container-inventory", kind, namespace, name, workloadAccess(kind, "get", namespace, name, "")); err != nil {
		return domain.WorkloadContainerInventory{}, err
	}
	repository, err := s.templateRepository()
	if err != nil {
		s.audit(ctx, actor, "read-container-inventory", kind, namespace, name, err)
		return domain.WorkloadContainerInventory{}, err
	}
	return repository.GetWorkloadContainerInventory(ctx, kind, namespace, name)
}

func (s *Service) UpdatePodTemplate(ctx context.Context, actor domain.Actor, kind, namespace, name string, update domain.WorkloadPodTemplateUpdate) (domain.WorkloadPodTemplateResult, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	action := "update-pod-template"
	if update.DryRun {
		action = "validate-pod-template"
	}
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return domain.WorkloadPodTemplateResult{}, err
	}
	if kind == "job" {
		err := fmt.Errorf("%w: Kubernetes Job Pod templates are immutable", domain.ErrUnsupported)
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return domain.WorkloadPodTemplateResult{}, err
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, action, kind, namespace, name, access); err != nil {
		return domain.WorkloadPodTemplateResult{}, err
	}
	if err := validatePodTemplateUpdate(namespace, name, &update); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return domain.WorkloadPodTemplateResult{}, err
	}
	repository, err := s.templateRepository()
	if err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return domain.WorkloadPodTemplateResult{}, err
	}
	result, err := repository.UpdateWorkloadPodTemplate(ctx, kind, namespace, name, update)
	s.audit(ctx, actor, action, kind, namespace, name, err)
	return result, err
}

func validatePodTemplateUpdate(namespace, name string, update *domain.WorkloadPodTemplateUpdate) error {
	if err := validateWorkloadIdentity(namespace, name); err != nil {
		return err
	}
	update.ResourceVersion = strings.TrimSpace(update.ResourceVersion)
	update.ChangeCause = strings.TrimSpace(update.ChangeCause)
	update.Source = strings.TrimSpace(update.Source)
	if update.ResourceVersion == "" {
		return validationError("resourceVersion", "resourceVersion is required")
	}
	if len(update.ResourceVersion) > 256 {
		return validationError("resourceVersion", "resourceVersion is too long")
	}
	if update.Template == nil {
		return validationError("template", "template is required")
	}
	encoded, err := json.Marshal(update.Template)
	if err != nil {
		return validationError("template", "template must be a JSON object")
	}
	if len(encoded) > maxPodTemplateBytes {
		return validationError("template", "template must not exceed 1 MiB")
	}
	if len(update.ChangeCause) > 256 {
		return validationError("changeCause", "changeCause must not exceed 256 characters")
	}
	if update.Source != "" && update.Source != "form" && update.Source != "yaml" {
		return validationError("source", "source must be form or yaml")
	}
	return nil
}
