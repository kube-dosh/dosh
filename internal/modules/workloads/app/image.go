package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"dosh/internal/domain"
)

var dnsSubdomainPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// UpdateImage validates and authorizes one pod-template image change before
// invoking the Kubernetes adapter. The adapter remains responsible for
// resolving the workload and enforcing optimistic concurrency.
func (s *Service) UpdateImage(ctx context.Context, actor domain.Actor, kind, namespace, name string, update domain.WorkloadImageUpdate) error {
	kind = strings.TrimSpace(kind)
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "update-image", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, "update-image", kind, namespace, name, access); err != nil {
		return err
	}
	if !isApplicationWorkload(kind) {
		err := fmt.Errorf("%w: image update is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "update-image", kind, namespace, name, err)
		return err
	}
	if err := validateImageUpdate(namespace, name, &update); err != nil {
		s.audit(ctx, actor, "update-image", kind, namespace, name, err)
		return err
	}
	err := s.repository.UpdateWorkloadImage(ctx, kind, namespace, name, update)
	s.audit(ctx, actor, "update-image", kind, namespace, name, err)
	return err
}

func isApplicationWorkload(kind string) bool {
	switch strings.ToLower(kind) {
	case "deployment", "statefulset", "daemonset":
		return true
	default:
		return false
	}
}

func validateImageUpdate(namespace, name string, update *domain.WorkloadImageUpdate) error {
	update.ContainerName = strings.TrimSpace(update.ContainerName)
	update.Image = strings.TrimSpace(update.Image)
	update.ResourceVersion = strings.TrimSpace(update.ResourceVersion)
	if err := validateWorkloadIdentity(namespace, name); err != nil {
		return err
	}
	if err := validateDNSLabel("containerName", update.ContainerName); err != nil {
		return err
	}
	if update.Image == "" || len(update.Image) > 512 || !imagePattern.MatchString(update.Image) {
		return validationError("image", "image must be a valid container image reference (maximum 512 characters)")
	}
	if len(update.ResourceVersion) > 256 {
		return validationError("resourceVersion", "resourceVersion is too long")
	}
	return nil
}

func validateWorkloadIdentity(namespace, name string) error {
	if err := validateDNSLabel("namespace", namespace); err != nil {
		return err
	}
	if len(name) == 0 || len(name) > 253 || !dnsSubdomainPattern.MatchString(name) {
		return validationError("name", "name must be a DNS-1123 subdomain (lowercase letters, digits, dots, or hyphens; maximum 253 characters)")
	}
	return nil
}
