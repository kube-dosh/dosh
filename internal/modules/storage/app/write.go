package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"dosh/internal/domain"
)

const maxPersistentVolumeClaimBytes int64 = 1 << 50

var (
	storageNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	accessModeValues   = map[string]struct{}{
		"ReadWriteOnce": {}, "ReadOnlyMany": {}, "ReadWriteMany": {}, "ReadWriteOncePod": {},
	}
	volumeModeValues = map[string]struct{}{"Filesystem": {}, "Block": {}}
)

func (s *Service) CreatePersistentVolumeClaim(ctx context.Context, actor domain.Actor, spec domain.PersistentVolumeClaimCreateSpec) (domain.PersistentVolumeClaimDetail, error) {
	if err := normalizePVCCreateSpec(&spec); err != nil {
		s.auditPVC(ctx, actor, "create-persistentvolumeclaim", spec.Namespace, spec.Name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if err := s.authorizePVC(ctx, actor, "create-persistentvolumeclaim", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.PersistentVolumeClaimDetail{}, err
	}
	detail, err := s.repository.CreatePersistentVolumeClaim(ctx, spec)
	s.auditPVC(ctx, actor, "create-persistentvolumeclaim", spec.Namespace, spec.Name, err)
	return detail, err
}

func (s *Service) UpdatePersistentVolumeClaim(ctx context.Context, actor domain.Actor, namespace, name string, update domain.PersistentVolumeClaimUpdateSpec) (domain.PersistentVolumeClaimDetail, error) {
	if err := normalizePVCUpdateSpec(namespace, name, &update); err != nil {
		s.auditPVC(ctx, actor, "update-persistentvolumeclaim", namespace, name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if err := s.authorizePVC(ctx, actor, "update-persistentvolumeclaim", namespace, name, "update"); err != nil {
		return domain.PersistentVolumeClaimDetail{}, err
	}
	detail, err := s.repository.UpdatePersistentVolumeClaim(ctx, namespace, name, update)
	s.auditPVC(ctx, actor, "update-persistentvolumeclaim", namespace, name, err)
	return detail, err
}

func (s *Service) DeletePersistentVolumeClaim(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validatePVCIdentity(namespace, name); err != nil {
		s.auditPVC(ctx, actor, "delete-persistentvolumeclaim", namespace, name, err)
		return err
	}
	if err := s.authorizePVC(ctx, actor, "delete-persistentvolumeclaim", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeletePersistentVolumeClaim(ctx, namespace, name, resourceVersion)
	s.auditPVC(ctx, actor, "delete-persistentvolumeclaim", namespace, name, err)
	return err
}

func (s *Service) authorizePVC(ctx context.Context, actor domain.Actor, action, namespace, name, verb string) error {
	access := domain.Access{Verb: verb, Resource: "persistentvolumeclaims", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditPVC(ctx, actor, action, namespace, name, err)
		return err
	}
	return nil
}

func (s *Service) auditPVC(ctx context.Context, actor domain.Actor, action, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "persistentvolumeclaim", Namespace: namespace, Name: name, Result: result, Err: err})
}

func normalizePVCCreateSpec(spec *domain.PersistentVolumeClaimCreateSpec) error {
	if err := validatePVCIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	spec.StorageClass = strings.TrimSpace(spec.StorageClass)
	if spec.StorageClass != "" && (len(spec.StorageClass) > 253 || !storageNamePattern.MatchString(spec.StorageClass)) {
		return storageValidationError("storageClass", "storageClass must be a valid DNS-1123 label")
	}
	if err := normalizePVCCapacity(spec.RequestedCapacityBytes); err != nil {
		return err
	}
	spec.AccessModes = normalizeAccessModes(spec.AccessModes)
	if len(spec.AccessModes) == 0 {
		return storageValidationError("accessModes", "at least one access mode is required")
	}
	spec.VolumeMode = strings.TrimSpace(spec.VolumeMode)
	if spec.VolumeMode == "" {
		spec.VolumeMode = "Filesystem"
	}
	if _, ok := volumeModeValues[spec.VolumeMode]; !ok {
		return storageValidationError("volumeMode", "volumeMode must be Filesystem or Block")
	}
	normalizePVCMetadata(&spec.Labels, &spec.Annotations)
	return nil
}

func normalizePVCUpdateSpec(namespace, name string, update *domain.PersistentVolumeClaimUpdateSpec) error {
	if err := validatePVCIdentity(namespace, name); err != nil {
		return err
	}
	if err := normalizePVCCapacity(update.RequestedCapacityBytes); err != nil {
		return err
	}
	normalizePVCMetadata(&update.Labels, &update.Annotations)
	return nil
}

func validatePVCIdentity(namespace, name string) error {
	if namespace == "" || len(namespace) > 63 || !storageNamePattern.MatchString(namespace) {
		return storageValidationError("namespace", "namespace must be a valid DNS-1123 label")
	}
	if name == "" || len(name) > 253 || !storageNamePattern.MatchString(name) {
		return storageValidationError("name", "name must be a valid DNS-1123 label")
	}
	return nil
}

func normalizePVCCapacity(capacity int64) error {
	if capacity < 1 || capacity > maxPersistentVolumeClaimBytes {
		return storageValidationError("requestedCapacityBytes", "requested capacity must be between 1 byte and 1 PiB")
	}
	return nil
}

func normalizeAccessModes(modes []string) []string {
	out := make([]string, 0, len(modes))
	seen := make(map[string]struct{}, len(modes))
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		if _, ok := accessModeValues[mode]; !ok {
			continue
		}
		if _, duplicate := seen[mode]; duplicate {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	return out
}

func normalizePVCMetadata(labels, annotations *map[string]string) {
	*labels = normalizeStringMap(*labels)
	*annotations = normalizeStringMap(*annotations)
}

func normalizeStringMap(source map[string]string) map[string]string {
	out := make(map[string]string, len(source))
	for key, value := range source {
		key = strings.TrimSpace(key)
		if key != "" && len(key) <= 253 {
			out[key] = value
		}
	}
	return out
}

func storageValidationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
