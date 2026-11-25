package kube

import (
	"context"
	"sort"

	"dosh/internal/domain"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListCSIDrivers exposes the registration surface of CSI drivers. It is a
// cluster-scoped read and therefore participates in the caller's impersonated
// Kubernetes authorization.
func (c *Client) ListCSIDrivers(ctx context.Context) ([]domain.CSIDriverInfo, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	items, err := c.clientset.StorageV1().CSIDrivers().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.CSIDriverInfo, 0, len(items.Items))
	for _, item := range items.Items {
		out = append(out, csiDriverInfo(item))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func csiDriverInfo(item storagev1.CSIDriver) domain.CSIDriverInfo {
	modes := make([]string, 0, len(item.Spec.VolumeLifecycleModes))
	for _, mode := range item.Spec.VolumeLifecycleModes {
		modes = append(modes, string(mode))
	}
	return domain.CSIDriverInfo{
		Name:                 item.Name,
		AttachRequired:       item.Spec.AttachRequired != nil && *item.Spec.AttachRequired,
		PodInfoOnMount:       item.Spec.PodInfoOnMount != nil && *item.Spec.PodInfoOnMount,
		StorageCapacity:      item.Spec.StorageCapacity != nil && *item.Spec.StorageCapacity,
		VolumeLifecycleModes: modes,
	}
}
