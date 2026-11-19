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
	storageNamePattern        = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	storageProvisionerPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?(\/[a-z0-9]([-a-z0-9.]*[a-z0-9])?)?$`)
	volumeBindingValues       = map[string]struct{}{"Immediate": {}, "WaitForFirstConsumer": {}}
	accessModeValues          = map[string]struct{}{
		"ReadWriteOnce": {}, "ReadOnlyMany": {}, "ReadWriteMany": {}, "ReadWriteOncePod": {},
	}
	volumeModeValues = map[string]struct{}{"Filesystem": {}, "Block": {}}
)

func (s *Service) CreateStorageClass(ctx context.Context, actor domain.Actor, spec domain.StorageClassWriteSpec) (domain.StorageClassDetail, error) {
	if err := normalizeStorageClassSpec(&spec, true); err != nil {
		s.auditStorage(ctx, actor, "create-storageclass", "storageclass", "", spec.Name, err)
		return domain.StorageClassDetail{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "create-storageclass", "storageclass", "storage.k8s.io", "storageclasses", "", spec.Name, "create"); err != nil {
		return domain.StorageClassDetail{}, err
	}
	if _, err := s.repository.CreateStorageClass(ctx, spec, true); err != nil {
		s.auditStorage(ctx, actor, "create-storageclass", "storageclass", "", spec.Name, err)
		return domain.StorageClassDetail{}, err
	}
	detail, err := s.repository.CreateStorageClass(ctx, spec, false)
	s.auditStorage(ctx, actor, "create-storageclass", "storageclass", "", spec.Name, err)
	return detail, err
}

func (s *Service) UpdateStorageClass(ctx context.Context, actor domain.Actor, name string, spec domain.StorageClassWriteSpec) (domain.StorageClassDetail, error) {
	spec.Name = strings.TrimSpace(name)
	if err := normalizeStorageClassSpec(&spec, false); err != nil {
		s.auditStorage(ctx, actor, "update-storageclass", "storageclass", "", spec.Name, err)
		return domain.StorageClassDetail{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "update-storageclass", "storageclass", "storage.k8s.io", "storageclasses", "", spec.Name, "update"); err != nil {
		return domain.StorageClassDetail{}, err
	}
	current, err := s.repository.GetStorageClass(ctx, spec.Name)
	if err != nil {
		s.auditStorage(ctx, actor, "update-storageclass", "storageclass", "", spec.Name, err)
		return domain.StorageClassDetail{}, err
	}
	if current.Provisioner != spec.Provisioner {
		err := storageValidationError("provisioner", "provisioner cannot be changed after StorageClass creation")
		s.auditStorage(ctx, actor, "update-storageclass", "storageclass", "", spec.Name, err)
		return domain.StorageClassDetail{}, err
	}
	if _, err := s.repository.UpdateStorageClass(ctx, spec.Name, spec, true); err != nil {
		s.auditStorage(ctx, actor, "update-storageclass", "storageclass", "", spec.Name, err)
		return domain.StorageClassDetail{}, err
	}
	detail, err := s.repository.UpdateStorageClass(ctx, spec.Name, spec, false)
	s.auditStorage(ctx, actor, "update-storageclass", "storageclass", "", spec.Name, err)
	return detail, err
}

func (s *Service) DeleteStorageClass(ctx context.Context, actor domain.Actor, name, resourceVersion string) error {
	name = strings.TrimSpace(name)
	if err := validateStorageName(name, "name"); err != nil {
		s.auditStorage(ctx, actor, "delete-storageclass", "storageclass", "", name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.auditStorage(ctx, actor, "delete-storageclass", "storageclass", "", name, err)
		return err
	}
	if err := s.authorizeStorage(ctx, actor, "delete-storageclass", "storageclass", "storage.k8s.io", "storageclasses", "", name, "delete"); err != nil {
		return err
	}
	current, err := s.repository.GetStorageClass(ctx, name)
	if err != nil {
		s.auditStorage(ctx, actor, "delete-storageclass", "storageclass", "", name, err)
		return err
	}
	if current.Default {
		err := storageValidationError("default", "the default StorageClass cannot be deleted; select another default class first")
		s.auditStorage(ctx, actor, "delete-storageclass", "storageclass", "", name, err)
		return err
	}
	if strings.HasPrefix(name, "kube-") {
		err := storageValidationError("name", "Kubernetes-managed kube-* StorageClasses are protected and cannot be deleted")
		s.auditStorage(ctx, actor, "delete-storageclass", "storageclass", "", name, err)
		return err
	}
	err = s.repository.DeleteStorageClass(ctx, name, resourceVersion)
	s.auditStorage(ctx, actor, "delete-storageclass", "storageclass", "", name, err)
	return err
}

func (s *Service) CreatePersistentVolume(ctx context.Context, actor domain.Actor, spec domain.PersistentVolumeCreateSpec) (domain.PersistentVolumeDetail, error) {
	if err := normalizePersistentVolumeCreateSpec(&spec); err != nil {
		s.auditStorage(ctx, actor, "create-persistentvolume", "persistentvolume", "", spec.Name, err)
		return domain.PersistentVolumeDetail{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "create-persistentvolume", "persistentvolume", "", "persistentvolumes", "", spec.Name, "create"); err != nil {
		return domain.PersistentVolumeDetail{}, err
	}
	if _, err := s.repository.CreatePersistentVolume(ctx, spec, true); err != nil {
		s.auditStorage(ctx, actor, "create-persistentvolume", "persistentvolume", "", spec.Name, err)
		return domain.PersistentVolumeDetail{}, err
	}
	detail, err := s.repository.CreatePersistentVolume(ctx, spec, false)
	s.auditStorage(ctx, actor, "create-persistentvolume", "persistentvolume", "", spec.Name, err)
	return detail, err
}

func (s *Service) UpdatePersistentVolume(ctx context.Context, actor domain.Actor, name string, update domain.PersistentVolumeUpdateSpec) (domain.PersistentVolumeDetail, error) {
	name = strings.TrimSpace(name)
	if err := normalizePersistentVolumeUpdateSpec(name, &update); err != nil {
		s.auditStorage(ctx, actor, "update-persistentvolume", "persistentvolume", "", name, err)
		return domain.PersistentVolumeDetail{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "update-persistentvolume", "persistentvolume", "", "persistentvolumes", "", name, "update"); err != nil {
		return domain.PersistentVolumeDetail{}, err
	}
	current, err := s.repository.GetPersistentVolume(ctx, name)
	if err != nil {
		s.auditStorage(ctx, actor, "update-persistentvolume", "persistentvolume", "", name, err)
		return domain.PersistentVolumeDetail{}, err
	}
	if update.CapacityBytes < current.CapacityBytes {
		err := storageValidationError("capacityBytes", "PersistentVolume capacity cannot be reduced from %d to %d bytes", current.CapacityBytes, update.CapacityBytes)
		s.auditStorage(ctx, actor, "update-persistentvolume", "persistentvolume", "", name, err)
		return domain.PersistentVolumeDetail{}, err
	}
	if _, err := s.repository.UpdatePersistentVolume(ctx, name, update, true); err != nil {
		s.auditStorage(ctx, actor, "update-persistentvolume", "persistentvolume", "", name, err)
		return domain.PersistentVolumeDetail{}, err
	}
	detail, err := s.repository.UpdatePersistentVolume(ctx, name, update, false)
	s.auditStorage(ctx, actor, "update-persistentvolume", "persistentvolume", "", name, err)
	return detail, err
}

func (s *Service) DeletePersistentVolume(ctx context.Context, actor domain.Actor, name, resourceVersion string) error {
	name = strings.TrimSpace(name)
	if err := validateStorageName(name, "name"); err != nil {
		s.auditStorage(ctx, actor, "delete-persistentvolume", "persistentvolume", "", name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.auditStorage(ctx, actor, "delete-persistentvolume", "persistentvolume", "", name, err)
		return err
	}
	if err := s.authorizeStorage(ctx, actor, "delete-persistentvolume", "persistentvolume", "", "persistentvolumes", "", name, "delete"); err != nil {
		return err
	}
	if strings.HasPrefix(name, "kube-") {
		err := storageValidationError("name", "Kubernetes-managed kube-* PersistentVolumes are protected and cannot be deleted")
		s.auditStorage(ctx, actor, "delete-persistentvolume", "persistentvolume", "", name, err)
		return err
	}
	err := s.repository.DeletePersistentVolume(ctx, name, resourceVersion)
	s.auditStorage(ctx, actor, "delete-persistentvolume", "persistentvolume", "", name, err)
	return err
}

func (s *Service) CreatePersistentVolumeClaim(ctx context.Context, actor domain.Actor, spec domain.PersistentVolumeClaimCreateSpec) (domain.PersistentVolumeClaimDetail, error) {
	if err := normalizePVCCreateSpec(&spec); err != nil {
		s.auditStorage(ctx, actor, "create-persistentvolumeclaim", "persistentvolumeclaim", spec.Namespace, spec.Name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "create-persistentvolumeclaim", "persistentvolumeclaim", "", "persistentvolumeclaims", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if _, err := s.repository.CreatePersistentVolumeClaim(ctx, spec, true); err != nil {
		s.auditStorage(ctx, actor, "create-persistentvolumeclaim", "persistentvolumeclaim", spec.Namespace, spec.Name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	detail, err := s.repository.CreatePersistentVolumeClaim(ctx, spec, false)
	s.auditStorage(ctx, actor, "create-persistentvolumeclaim", "persistentvolumeclaim", spec.Namespace, spec.Name, err)
	return detail, err
}

func (s *Service) UpdatePersistentVolumeClaim(ctx context.Context, actor domain.Actor, namespace, name string, update domain.PersistentVolumeClaimUpdateSpec) (domain.PersistentVolumeClaimDetail, error) {
	if err := normalizePVCUpdateSpec(namespace, name, &update); err != nil {
		s.auditStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", "", "persistentvolumeclaims", namespace, name, "update"); err != nil {
		return domain.PersistentVolumeClaimDetail{}, err
	}
	current, err := s.repository.GetPersistentVolumeClaim(ctx, namespace, name)
	if err != nil {
		s.auditStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if update.RequestedCapacityBytes < current.RequestedCapacityBytes {
		err := storageValidationError("requestedCapacityBytes", "PVC capacity cannot be reduced from %d to %d bytes", current.RequestedCapacityBytes, update.RequestedCapacityBytes)
		s.auditStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	if update.RequestedCapacityBytes > current.RequestedCapacityBytes {
		if err := s.validatePVCExpansion(ctx, current); err != nil {
			s.auditStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
			return domain.PersistentVolumeClaimDetail{}, err
		}
	}
	if _, err := s.repository.UpdatePersistentVolumeClaim(ctx, namespace, name, update, true); err != nil {
		s.auditStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
		return domain.PersistentVolumeClaimDetail{}, err
	}
	detail, err := s.repository.UpdatePersistentVolumeClaim(ctx, namespace, name, update, false)
	s.auditStorage(ctx, actor, "update-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
	return detail, err
}

func (s *Service) validatePVCExpansion(ctx context.Context, current domain.PersistentVolumeClaimDetail) error {
	if current.StorageClass == "" {
		return storageValidationError("storageClass", "PVC has no StorageClass and cannot request automatic volume expansion")
	}
	class, err := s.repository.GetStorageClass(ctx, current.StorageClass)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return storageValidationError("storageClass", "StorageClass %q no longer exists; expansion cannot be requested", current.StorageClass)
		}
		return fmt.Errorf("%w: cannot verify StorageClass %q expansion policy: %v", domain.ErrUnavailable, current.StorageClass, err)
	}
	if !class.AllowVolumeExpansion {
		return storageValidationError("storageClass", "StorageClass %q does not set allowVolumeExpansion=true; this volume cannot be expanded", current.StorageClass)
	}
	return nil
}

func (s *Service) DeletePersistentVolumeClaim(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validatePVCIdentity(namespace, name); err != nil {
		s.auditStorage(ctx, actor, "delete-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.auditStorage(ctx, actor, "delete-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
		return err
	}
	if err := s.authorizeStorage(ctx, actor, "delete-persistentvolumeclaim", "persistentvolumeclaim", "", "persistentvolumeclaims", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeletePersistentVolumeClaim(ctx, namespace, name, resourceVersion)
	s.auditStorage(ctx, actor, "delete-persistentvolumeclaim", "persistentvolumeclaim", namespace, name, err)
	return err
}

func (s *Service) VolumeSnapshotFeature(ctx context.Context) (domain.VolumeSnapshotFeature, error) {
	return s.repository.VolumeSnapshotFeature(ctx)
}

func (s *Service) ListVolumeSnapshots(ctx context.Context, actor domain.Actor, query domain.ListQuery) ([]domain.VolumeSnapshot, error) {
	query.Namespace = strings.TrimSpace(query.Namespace)
	if query.Namespace != "" {
		if err := validateStorageName(query.Namespace, "namespace"); err != nil {
			return nil, err
		}
	}
	if err := s.authorizeStorage(ctx, actor, "list-volumesnapshots", "volumesnapshot", "snapshot.storage.k8s.io", "volumesnapshots", query.Namespace, "", "list"); err != nil {
		return nil, err
	}
	return s.repository.ListVolumeSnapshots(ctx, query)
}

func (s *Service) CreateVolumeSnapshot(ctx context.Context, actor domain.Actor, spec domain.VolumeSnapshotCreateSpec) (domain.VolumeSnapshot, error) {
	if err := normalizeVolumeSnapshotCreateSpec(&spec); err != nil {
		s.auditStorage(ctx, actor, "create-volumesnapshot", "volumesnapshot", spec.Namespace, spec.Name, err)
		return domain.VolumeSnapshot{}, err
	}
	if err := s.authorizeStorage(ctx, actor, "create-volumesnapshot", "volumesnapshot", "snapshot.storage.k8s.io", "volumesnapshots", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.VolumeSnapshot{}, err
	}
	if _, err := s.repository.CreateVolumeSnapshot(ctx, spec, true); err != nil {
		s.auditStorage(ctx, actor, "create-volumesnapshot", "volumesnapshot", spec.Namespace, spec.Name, err)
		return domain.VolumeSnapshot{}, err
	}
	item, err := s.repository.CreateVolumeSnapshot(ctx, spec, false)
	s.auditStorage(ctx, actor, "create-volumesnapshot", "volumesnapshot", spec.Namespace, spec.Name, err)
	return item, err
}

func (s *Service) DeleteVolumeSnapshot(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validatePVCIdentity(namespace, name); err != nil {
		s.auditStorage(ctx, actor, "delete-volumesnapshot", "volumesnapshot", namespace, name, err)
		return err
	}
	if err := validateResourceVersion(resourceVersion); err != nil {
		s.auditStorage(ctx, actor, "delete-volumesnapshot", "volumesnapshot", namespace, name, err)
		return err
	}
	if err := s.authorizeStorage(ctx, actor, "delete-volumesnapshot", "volumesnapshot", "snapshot.storage.k8s.io", "volumesnapshots", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeleteVolumeSnapshot(ctx, namespace, name, resourceVersion)
	s.auditStorage(ctx, actor, "delete-volumesnapshot", "volumesnapshot", namespace, name, err)
	return err
}

func (s *Service) authorizeStorage(ctx context.Context, actor domain.Actor, action, auditResource, apiGroup, resource, namespace, name, verb string) error {
	access := domain.Access{Verb: verb, APIGroup: apiGroup, Resource: resource, Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditStorage(ctx, actor, action, auditResource, namespace, name, err)
		return err
	}
	return nil
}

func (s *Service) auditStorage(ctx context.Context, actor domain.Actor, action, resource, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: resource, Namespace: namespace, Name: name, Result: result, Err: err})
}

func normalizeStorageClassSpec(spec *domain.StorageClassWriteSpec, creating bool) error {
	spec.Name = strings.TrimSpace(spec.Name)
	if err := validateStorageName(spec.Name, "name"); err != nil {
		return err
	}
	spec.Provisioner = strings.TrimSpace(spec.Provisioner)
	if len(spec.Provisioner) > 253 || !storageProvisionerPattern.MatchString(spec.Provisioner) {
		return storageValidationError("provisioner", "provisioner must be a valid CSI driver or provisioner name")
	}
	spec.ReclaimPolicy = strings.TrimSpace(spec.ReclaimPolicy)
	if spec.ReclaimPolicy == "" {
		spec.ReclaimPolicy = "Delete"
	}
	if spec.ReclaimPolicy != "Delete" && spec.ReclaimPolicy != "Retain" {
		return storageValidationError("reclaimPolicy", "reclaimPolicy must be Delete or Retain")
	}
	spec.VolumeBinding = strings.TrimSpace(spec.VolumeBinding)
	if spec.VolumeBinding == "" {
		spec.VolumeBinding = "Immediate"
	}
	if _, ok := volumeBindingValues[spec.VolumeBinding]; !ok {
		return storageValidationError("volumeBinding", "volumeBinding must be Immediate or WaitForFirstConsumer")
	}
	if !creating && strings.TrimSpace(spec.ResourceVersion) == "" {
		return storageValidationError("resourceVersion", "resourceVersion is required to prevent overwriting a newer StorageClass")
	}
	spec.Parameters = normalizeStringMap(spec.Parameters)
	spec.MountOptions = normalizeStringSlice(spec.MountOptions)
	normalizeMetadata(&spec.Labels, &spec.Annotations)
	return nil
}

func normalizePersistentVolumeCreateSpec(spec *domain.PersistentVolumeCreateSpec) error {
	spec.Name = strings.TrimSpace(spec.Name)
	if err := validateStorageName(spec.Name, "name"); err != nil {
		return err
	}
	if err := normalizePersistentVolumeCapacity(spec.CapacityBytes); err != nil {
		return err
	}
	spec.AccessModes = normalizeAccessModes(spec.AccessModes)
	if len(spec.AccessModes) == 0 {
		return storageValidationError("accessModes", "at least one access mode is required")
	}
	spec.ReclaimPolicy = strings.TrimSpace(spec.ReclaimPolicy)
	if spec.ReclaimPolicy == "" {
		spec.ReclaimPolicy = "Retain"
	}
	if spec.ReclaimPolicy != "Retain" && spec.ReclaimPolicy != "Delete" && spec.ReclaimPolicy != "Recycle" {
		return storageValidationError("reclaimPolicy", "reclaimPolicy must be Retain, Delete or Recycle")
	}
	spec.VolumeMode = strings.TrimSpace(spec.VolumeMode)
	if spec.VolumeMode == "" {
		spec.VolumeMode = "Filesystem"
	}
	if _, ok := volumeModeValues[spec.VolumeMode]; !ok {
		return storageValidationError("volumeMode", "volumeMode must be Filesystem or Block")
	}
	spec.StorageClass = strings.TrimSpace(spec.StorageClass)
	if spec.StorageClass != "" {
		if err := validateStorageName(spec.StorageClass, "storageClass"); err != nil {
			return err
		}
	}
	if spec.CSI == nil {
		return storageValidationError("csi", "a CSI volume source is required for structured PersistentVolume creation")
	}
	spec.CSI.Driver = strings.TrimSpace(spec.CSI.Driver)
	spec.CSI.VolumeHandle = strings.TrimSpace(spec.CSI.VolumeHandle)
	if spec.CSI.Driver == "" || len(spec.CSI.Driver) > 253 {
		return storageValidationError("csi.driver", "CSI driver is required")
	}
	if spec.CSI.VolumeHandle == "" || len(spec.CSI.VolumeHandle) > 512 {
		return storageValidationError("csi.volumeHandle", "CSI volumeHandle is required")
	}
	spec.CSI.FSType = strings.TrimSpace(spec.CSI.FSType)
	spec.MountOptions = normalizeStringSlice(spec.MountOptions)
	normalizeMetadata(&spec.Labels, &spec.Annotations)
	return nil
}

func normalizePersistentVolumeUpdateSpec(name string, update *domain.PersistentVolumeUpdateSpec) error {
	if err := validateStorageName(name, "name"); err != nil {
		return err
	}
	if err := normalizePersistentVolumeCapacity(update.CapacityBytes); err != nil {
		return err
	}
	update.ReclaimPolicy = strings.TrimSpace(update.ReclaimPolicy)
	if update.ReclaimPolicy != "Retain" && update.ReclaimPolicy != "Delete" && update.ReclaimPolicy != "Recycle" {
		return storageValidationError("reclaimPolicy", "reclaimPolicy must be Retain, Delete or Recycle")
	}
	if strings.TrimSpace(update.ResourceVersion) == "" {
		return storageValidationError("resourceVersion", "resourceVersion is required to prevent overwriting a newer PersistentVolume")
	}
	update.MountOptions = normalizeStringSlice(update.MountOptions)
	normalizeMetadata(&update.Labels, &update.Annotations)
	return nil
}

func normalizeVolumeSnapshotCreateSpec(spec *domain.VolumeSnapshotCreateSpec) error {
	if err := validatePVCIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	if err := validateStorageName(strings.TrimSpace(spec.SourcePVC), "sourcePvc"); err != nil {
		return err
	}
	spec.SourcePVC = strings.TrimSpace(spec.SourcePVC)
	spec.VolumeSnapshotClassName = strings.TrimSpace(spec.VolumeSnapshotClassName)
	if spec.VolumeSnapshotClassName != "" {
		if err := validateStorageName(spec.VolumeSnapshotClassName, "volumeSnapshotClassName"); err != nil {
			return err
		}
	}
	return nil
}

func normalizePVCCreateSpec(spec *domain.PersistentVolumeClaimCreateSpec) error {
	if err := validatePVCIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	spec.StorageClass = strings.TrimSpace(spec.StorageClass)
	if spec.StorageClass != "" {
		if err := validateStorageName(spec.StorageClass, "storageClass"); err != nil {
			return err
		}
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
	spec.SnapshotName = strings.TrimSpace(spec.SnapshotName)
	if spec.SnapshotName != "" {
		if err := validateStorageName(spec.SnapshotName, "snapshotName"); err != nil {
			return err
		}
	}
	normalizeMetadata(&spec.Labels, &spec.Annotations)
	return nil
}

func normalizePVCUpdateSpec(namespace, name string, update *domain.PersistentVolumeClaimUpdateSpec) error {
	if err := validatePVCIdentity(namespace, name); err != nil {
		return err
	}
	if err := normalizePVCCapacity(update.RequestedCapacityBytes); err != nil {
		return err
	}
	if strings.TrimSpace(update.ResourceVersion) == "" {
		return storageValidationError("resourceVersion", "resourceVersion is required to prevent overwriting a newer PVC")
	}
	normalizeMetadata(&update.Labels, &update.Annotations)
	return nil
}

func validatePVCIdentity(namespace, name string) error {
	if namespace == "" || len(namespace) > 63 || !storageNamePattern.MatchString(namespace) {
		return storageValidationError("namespace", "namespace must be a valid DNS-1123 label")
	}
	if err := validateStorageName(name, "name"); err != nil {
		return err
	}
	return nil
}

func validateStorageName(name, field string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 253 || !storageNamePattern.MatchString(name) {
		return storageValidationError(field, "%s must be a valid DNS-1123 label", field)
	}
	return nil
}

func validateResourceVersion(resourceVersion string) error {
	resourceVersion = strings.TrimSpace(resourceVersion)
	if resourceVersion == "" || len(resourceVersion) > 64 {
		return storageValidationError("resourceVersion", "resourceVersion is required")
	}
	return nil
}

func normalizePVCCapacity(capacity int64) error {
	if capacity < 1 || capacity > maxPersistentVolumeClaimBytes {
		return storageValidationError("requestedCapacityBytes", "requested capacity must be between 1 byte and 1 PiB")
	}
	return nil
}

func normalizePersistentVolumeCapacity(capacity int64) error {
	if capacity < 1 || capacity > maxPersistentVolumeClaimBytes {
		return storageValidationError("capacityBytes", "capacity must be between 1 byte and 1 PiB")
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

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeMetadata(labels, annotations *map[string]string) {
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
