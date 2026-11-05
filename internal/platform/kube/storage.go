package kube

import (
	"context"
	"sort"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) ListStorageClasses(ctx context.Context, query domain.ListQuery) ([]domain.StorageClass, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	items, err := c.clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.StorageClass, 0, len(items.Items))
	for _, item := range items.Items {
		summary := storageClassSummary(item)
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.Provisioner}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func storageClassSummary(item storagev1.StorageClass) domain.StorageClass {
	reclaim := "Delete"
	if item.ReclaimPolicy != nil {
		reclaim = string(*item.ReclaimPolicy)
	}
	binding := "Immediate"
	if item.VolumeBindingMode != nil {
		binding = string(*item.VolumeBindingMode)
	}
	_, defaultClass := item.Annotations["storageclass.kubernetes.io/is-default-class"]
	return domain.StorageClass{Name: item.Name, Provisioner: item.Provisioner, ReclaimPolicy: reclaim, VolumeBinding: binding, Default: defaultClass, CreatedAt: item.CreationTimestamp.Time}
}

func (c *Client) GetStorageClass(ctx context.Context, name string) (domain.StorageClassDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.StorageClassDetail{}, mapKubeError(err)
	}
	return storageClassDetail(*item), nil
}

func storageClassDetail(item storagev1.StorageClass) domain.StorageClassDetail {
	detail := domain.StorageClassDetail{
		StorageClass: storageClassSummary(item),
		Labels:       item.Labels,
		Annotations:  item.Annotations,
		Parameters:   item.Parameters,
		MountOptions: append([]string(nil), item.MountOptions...),
	}
	if item.AllowVolumeExpansion != nil {
		detail.AllowVolumeExpansion = *item.AllowVolumeExpansion
	}
	return detail
}

func (c *Client) ListPersistentVolumes(ctx context.Context, query domain.ListQuery) ([]domain.PersistentVolume, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	items, err := c.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.PersistentVolume, 0, len(items.Items))
	for _, item := range items.Items {
		summary := persistentVolumeSummary(item)
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.StorageClass, summary.Claim, summary.Status}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func persistentVolumeSummary(item corev1.PersistentVolume) domain.PersistentVolume {
	claim := ""
	if item.Spec.ClaimRef != nil {
		claim = item.Spec.ClaimRef.Namespace + "/" + item.Spec.ClaimRef.Name
	}
	return domain.PersistentVolume{
		Name: item.Name, CapacityBytes: item.Spec.Capacity.Storage().Value(), AccessModes: accessModes(item.Spec.AccessModes),
		ReclaimPolicy: string(item.Spec.PersistentVolumeReclaimPolicy), Status: string(item.Status.Phase), Claim: claim,
		StorageClass: item.Spec.StorageClassName, CreatedAt: item.CreationTimestamp.Time,
	}
}

func (c *Client) GetPersistentVolume(ctx context.Context, name string) (domain.PersistentVolumeDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.PersistentVolumeDetail{}, mapKubeError(err)
	}
	return persistentVolumeDetail(*item), nil
}

func persistentVolumeDetail(item corev1.PersistentVolume) domain.PersistentVolumeDetail {
	detail := domain.PersistentVolumeDetail{
		PersistentVolume: persistentVolumeSummary(item),
		Labels:           item.Labels,
		Annotations:      item.Annotations,
		VolumeMode:       volumeMode(item.Spec.VolumeMode),
		MountOptions:     append([]string(nil), item.Spec.MountOptions...),
	}
	if item.Spec.ClaimRef != nil {
		detail.ClaimNamespace = item.Spec.ClaimRef.Namespace
		detail.ClaimName = item.Spec.ClaimRef.Name
	}
	if item.Spec.CSI != nil {
		detail.CSIDriver = item.Spec.CSI.Driver
	}
	return detail
}

func (c *Client) ListPersistentVolumeClaims(ctx context.Context, query domain.ListQuery) ([]domain.PersistentVolumeClaim, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.PersistentVolumeClaim, 0, len(items.Items))
	for _, item := range items.Items {
		summary := persistentVolumeClaimSummary(item)
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.Namespace, summary.Status, summary.StorageClass, summary.VolumeName}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func persistentVolumeClaimSummary(item corev1.PersistentVolumeClaim) domain.PersistentVolumeClaim {
	return domain.PersistentVolumeClaim{
		Name: item.Name, Namespace: item.Namespace, Status: string(item.Status.Phase), VolumeName: item.Spec.VolumeName,
		CapacityBytes: item.Status.Capacity.Storage().Value(), AccessModes: accessModes(item.Spec.AccessModes),
		StorageClass: valueOrEmpty(item.Spec.StorageClassName), CreatedAt: item.CreationTimestamp.Time,
	}
}

func (c *Client) GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (domain.PersistentVolumeClaimDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.PersistentVolumeClaimDetail{}, mapKubeError(err)
	}
	return persistentVolumeClaimDetail(*item), nil
}

func persistentVolumeClaimDetail(item corev1.PersistentVolumeClaim) domain.PersistentVolumeClaimDetail {
	detail := domain.PersistentVolumeClaimDetail{
		PersistentVolumeClaim:  persistentVolumeClaimSummary(item),
		Labels:                 item.Labels,
		Annotations:            item.Annotations,
		VolumeMode:             volumeMode(item.Spec.VolumeMode),
		RequestedCapacityBytes: item.Spec.Resources.Requests.Storage().Value(),
		Conditions:             make([]domain.Condition, 0, len(item.Status.Conditions)),
	}
	for _, condition := range item.Status.Conditions {
		detail.Conditions = append(detail.Conditions, domain.Condition{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	return detail
}

func volumeMode(mode *corev1.PersistentVolumeMode) string {
	if mode == nil {
		return string(corev1.PersistentVolumeFilesystem)
	}
	return string(*mode)
}

func accessModes(modes []corev1.PersistentVolumeAccessMode) []string {
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		out = append(out, string(mode))
	}
	return out
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
