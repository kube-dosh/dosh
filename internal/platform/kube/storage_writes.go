package kube

import (
	"context"
	"fmt"
	"sort"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"

func (c *Client) CreateStorageClass(ctx context.Context, spec domain.StorageClassWriteSpec, dryRun bool) (domain.StorageClassDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{
		Name: spec.Name, Labels: cloneStringMap(spec.Labels), Annotations: defaultStorageClassAnnotations(spec.Annotations, spec.Default),
	}, Provisioner: spec.Provisioner, Parameters: cloneStringMap(spec.Parameters), MountOptions: append([]string(nil), spec.MountOptions...)}
	applyStorageClassPolicy(item, spec)
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	created, err := c.clientset.StorageV1().StorageClasses().Create(ctx, item, options)
	if err != nil {
		return domain.StorageClassDetail{}, mapKubeError(err)
	}
	return storageClassDetail(*created), nil
}

func (c *Client) UpdateStorageClass(ctx context.Context, name string, spec domain.StorageClassWriteSpec, dryRun bool) (domain.StorageClassDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.StorageClassDetail{}, mapKubeError(err)
	}
	if spec.ResourceVersion != "" && item.ResourceVersion != spec.ResourceVersion {
		return domain.StorageClassDetail{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, spec.ResourceVersion, item.ResourceVersion)
	}
	if item.Provisioner != spec.Provisioner {
		return domain.StorageClassDetail{}, fmt.Errorf("%w: provisioner cannot be changed after creation", domain.ErrInvalid)
	}
	item.ReclaimPolicy = storageClassReclaimPolicyPointer(spec.ReclaimPolicy)
	item.VolumeBindingMode = bindingModePointer(spec.VolumeBinding)
	item.AllowVolumeExpansion = &spec.AllowVolumeExpansion
	item.Parameters = cloneStringMap(spec.Parameters)
	item.MountOptions = append([]string(nil), spec.MountOptions...)
	item.Labels = cloneStringMap(spec.Labels)
	item.Annotations = defaultStorageClassAnnotations(spec.Annotations, spec.Default)
	options := metav1.UpdateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	updated, err := c.clientset.StorageV1().StorageClasses().Update(ctx, item, options)
	if err != nil {
		return domain.StorageClassDetail{}, mapKubeError(err)
	}
	return storageClassDetail(*updated), nil
}

func (c *Client) DeleteStorageClass(ctx context.Context, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.StorageV1().StorageClasses().Delete(ctx, name, options))
}

func applyStorageClassPolicy(item *storagev1.StorageClass, spec domain.StorageClassWriteSpec) {
	item.ReclaimPolicy = storageClassReclaimPolicyPointer(spec.ReclaimPolicy)
	item.VolumeBindingMode = bindingModePointer(spec.VolumeBinding)
	item.AllowVolumeExpansion = &spec.AllowVolumeExpansion
}

func defaultStorageClassAnnotations(source map[string]string, isDefault bool) map[string]string {
	annotations := cloneStringMap(source)
	if annotations == nil {
		annotations = map[string]string{}
	}
	if isDefault {
		annotations[defaultStorageClassAnnotation] = "true"
	} else {
		delete(annotations, defaultStorageClassAnnotation)
	}
	if len(annotations) == 0 {
		return nil
	}
	return annotations
}

func storageClassReclaimPolicyPointer(value string) *corev1.PersistentVolumeReclaimPolicy {
	policy := corev1.PersistentVolumeReclaimPolicy(value)
	return &policy
}

func bindingModePointer(value string) *storagev1.VolumeBindingMode {
	mode := storagev1.VolumeBindingMode(value)
	return &mode
}

func (c *Client) CreatePersistentVolume(ctx context.Context, spec domain.PersistentVolumeCreateSpec, dryRun bool) (domain.PersistentVolumeDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	mode := corev1.PersistentVolumeMode(spec.VolumeMode)
	item := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Labels: cloneStringMap(spec.Labels), Annotations: cloneStringMap(spec.Annotations)},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: *resource.NewQuantity(spec.CapacityBytes, resource.BinarySI)},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimPolicy(spec.ReclaimPolicy),
			StorageClassName:              spec.StorageClass,
			MountOptions:                  append([]string(nil), spec.MountOptions...),
			VolumeMode:                    &mode,
		},
	}
	for _, accessMode := range spec.AccessModes {
		item.Spec.AccessModes = append(item.Spec.AccessModes, corev1.PersistentVolumeAccessMode(accessMode))
	}
	if spec.CSI != nil {
		item.Spec.CSI = &corev1.CSIPersistentVolumeSource{
			Driver: spec.CSI.Driver, VolumeHandle: spec.CSI.VolumeHandle, FSType: spec.CSI.FSType, ReadOnly: spec.CSI.ReadOnly,
		}
	}
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	created, err := c.clientset.CoreV1().PersistentVolumes().Create(ctx, item, options)
	if err != nil {
		return domain.PersistentVolumeDetail{}, mapKubeError(err)
	}
	detail := persistentVolumeDetail(*created)
	detail.Impact = c.storageImpactForClaim(ctx, detail.ClaimNamespace, detail.ClaimName)
	return detail, nil
}

func (c *Client) UpdatePersistentVolume(ctx context.Context, name string, update domain.PersistentVolumeUpdateSpec, dryRun bool) (domain.PersistentVolumeDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.PersistentVolumeDetail{}, mapKubeError(err)
	}
	if update.ResourceVersion != "" && item.ResourceVersion != update.ResourceVersion {
		return domain.PersistentVolumeDetail{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, item.ResourceVersion)
	}
	current := item.Spec.Capacity.Storage().Value()
	if update.CapacityBytes < current {
		return domain.PersistentVolumeDetail{}, fmt.Errorf("%w: PersistentVolume capacity cannot be reduced from %d to %d bytes", domain.ErrInvalid, current, update.CapacityBytes)
	}
	item.Spec.Capacity[corev1.ResourceStorage] = *resource.NewQuantity(update.CapacityBytes, resource.BinarySI)
	item.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimPolicy(update.ReclaimPolicy)
	item.Spec.MountOptions = append([]string(nil), update.MountOptions...)
	item.Labels = cloneStringMap(update.Labels)
	item.Annotations = cloneStringMap(update.Annotations)
	options := metav1.UpdateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	updated, err := c.clientset.CoreV1().PersistentVolumes().Update(ctx, item, options)
	if err != nil {
		return domain.PersistentVolumeDetail{}, mapKubeError(err)
	}
	detail := persistentVolumeDetail(*updated)
	detail.Impact = c.storageImpactForClaim(ctx, detail.ClaimNamespace, detail.ClaimName)
	return detail, nil
}

func (c *Client) DeletePersistentVolume(ctx context.Context, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.CoreV1().PersistentVolumes().Delete(ctx, name, options))
}

func (c *Client) storageImpactForClaim(ctx context.Context, namespace, claimName string) domain.StorageImpact {
	impact := domain.StorageImpact{Mounts: []domain.StorageMount{}, Warnings: []string{}}
	if namespace == "" || claimName == "" {
		return impact
	}
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		impact.Warnings = append(impact.Warnings, fmt.Sprintf("无法读取命名空间 %s 的 Pod 挂载关系：%v", namespace, mapKubeError(err)))
		return impact
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		volumeNames := make(map[string]struct{})
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == claimName {
				volumeNames[volume.Name] = struct{}{}
			}
		}
		if len(volumeNames) == 0 {
			continue
		}
		containers := make([]string, 0)
		mountPaths := make([]string, 0)
		collect := func(name string, mounts []corev1.VolumeMount) {
			matched := false
			for _, mount := range mounts {
				if _, ok := volumeNames[mount.Name]; !ok {
					continue
				}
				matched = true
				if !containsString(mountPaths, mount.MountPath) {
					mountPaths = append(mountPaths, mount.MountPath)
				}
			}
			if matched && !containsString(containers, name) {
				containers = append(containers, name)
			}
		}
		for _, container := range pod.Spec.InitContainers {
			collect(container.Name, container.VolumeMounts)
		}
		for _, container := range pod.Spec.Containers {
			collect(container.Name, container.VolumeMounts)
		}
		sort.Strings(containers)
		sort.Strings(mountPaths)
		workloadKind, workloadName := owner(pod)
		impact.Mounts = append(impact.Mounts, domain.StorageMount{
			Pod: pod.Name, Namespace: pod.Namespace, WorkloadKind: workloadKind, WorkloadName: workloadName,
			Containers: containers, MountPaths: mountPaths,
		})
	}
	sort.Slice(impact.Mounts, func(i, j int) bool {
		if impact.Mounts[i].Namespace != impact.Mounts[j].Namespace {
			return impact.Mounts[i].Namespace < impact.Mounts[j].Namespace
		}
		return impact.Mounts[i].Pod < impact.Mounts[j].Pod
	})
	impact.MountCount = len(impact.Mounts)
	return impact
}
