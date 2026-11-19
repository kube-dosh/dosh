package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const volumeSnapshotUnavailableReason = "未检测到 snapshot.storage.k8s.io VolumeSnapshot CRD；安装 CSI snapshot-controller、CRD 和 VolumeSnapshotClass 后会自动启用。"

func (c *Client) VolumeSnapshotFeature(ctx context.Context) (domain.VolumeSnapshotFeature, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	gvr, reason := c.volumeSnapshotResource(ctx)
	feature := domain.VolumeSnapshotFeature{Available: false, Reason: reason, Classes: []string{}}
	if gvr.Resource == "" {
		return feature, nil
	}
	feature.Available = true
	feature.APIVersion = gvr.Group + "/" + gvr.Version
	feature.Reason = ""
	classes, defaultClass, classReason := c.listVolumeSnapshotClasses(ctx, gvr.Version)
	feature.Classes = classes
	feature.DefaultClass = defaultClass
	if classReason != "" {
		feature.Reason = classReason
	}
	return feature, nil
}

func (c *Client) ListVolumeSnapshots(ctx context.Context, query domain.ListQuery) ([]domain.VolumeSnapshot, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	gvr, reason := c.volumeSnapshotResource(ctx)
	if gvr.Resource == "" {
		return nil, fmt.Errorf("%w: %s", domain.ErrUnsupported, reason)
	}
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	list, err := c.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	items := make([]domain.VolumeSnapshot, 0, len(list.Items))
	for _, raw := range list.Items {
		item := volumeSnapshotSummary(raw)
		if query.Search != "" && !matchesSearch([]string{item.Name, item.Namespace, item.SourcePVC, item.Status, item.VolumeSnapshotClassName}, query.Search) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].CreationTime, items[j].CreationTime
		if left.IsZero() || right.IsZero() {
			left, right = items[i].CreatedAt, items[j].CreatedAt
		}
		if !left.Equal(right) {
			return left.After(right)
		}
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (c *Client) CreateVolumeSnapshot(ctx context.Context, spec domain.VolumeSnapshotCreateSpec, dryRun bool) (domain.VolumeSnapshot, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	gvr, reason := c.volumeSnapshotResource(ctx)
	if gvr.Resource == "" {
		return domain.VolumeSnapshot{}, fmt.Errorf("%w: %s", domain.ErrUnsupported, reason)
	}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gvr.Group + "/" + gvr.Version,
		"kind":       "VolumeSnapshot",
		"metadata": map[string]any{
			"name":      spec.Name,
			"namespace": spec.Namespace,
		},
		"spec": map[string]any{
			"source": map[string]any{"persistentVolumeClaimName": spec.SourcePVC},
		},
	}}
	if spec.VolumeSnapshotClassName != "" {
		object.Object["spec"].(map[string]any)["volumeSnapshotClassName"] = spec.VolumeSnapshotClassName
	}
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	created, err := c.dynamic.Resource(gvr).Namespace(spec.Namespace).Create(ctx, object, options)
	if err != nil {
		return domain.VolumeSnapshot{}, mapKubeError(err)
	}
	return volumeSnapshotSummary(*created), nil
}

func (c *Client) DeleteVolumeSnapshot(ctx context.Context, namespace, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	gvr, reason := c.volumeSnapshotResource(ctx)
	if gvr.Resource == "" {
		return fmt.Errorf("%w: %s", domain.ErrUnsupported, reason)
	}
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.dynamic.Resource(gvr).Namespace(namespace).Delete(ctx, name, options))
}

func (c *Client) volumeSnapshotCountForPVC(ctx context.Context, namespace, claimName string) int {
	gvr, _ := c.volumeSnapshotResource(ctx)
	if gvr.Resource == "" || c.dynamic == nil {
		return 0
	}
	list, err := c.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0
	}
	count := 0
	for _, item := range list.Items {
		if nestedString(item.Object, "spec", "source", "persistentVolumeClaimName") == claimName {
			count++
		}
	}
	return count
}

func (c *Client) volumeSnapshotResource(ctx context.Context) (schema.GroupVersionResource, string) {
	if c.dynamic == nil || c.discovery == nil {
		return schema.GroupVersionResource{}, "VolumeSnapshot dynamic client 不可用。"
	}
	for _, version := range []string{"v1", "v1beta1"} {
		resources, err := c.serverResourcesForGroupVersion(ctx, "snapshot.storage.k8s.io/"+version)
		if err != nil {
			if apierrors.IsNotFound(err) || strings.Contains(strings.ToLower(err.Error()), "not found") {
				continue
			}
			return schema.GroupVersionResource{}, fmt.Sprintf("无法探测 VolumeSnapshot API：%v", err)
		}
		for _, resource := range resources.APIResources {
			if resource.Name == "volumesnapshots" && resource.Kind == "VolumeSnapshot" {
				return schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: version, Resource: "volumesnapshots"}, ""
			}
		}
	}
	return schema.GroupVersionResource{}, volumeSnapshotUnavailableReason
}

func (c *Client) listVolumeSnapshotClasses(ctx context.Context, version string) ([]string, string, string) {
	gvr := schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: version, Resource: "volumesnapshotclasses"}
	list, err := c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		if errors.Is(mapKubeError(err), domain.ErrForbidden) {
			return []string{}, "", "已检测到 VolumeSnapshot API，但当前身份无权列出 VolumeSnapshotClass；创建快照时可手动输入名称。"
		}
		return []string{}, "", "已检测到 VolumeSnapshot API，但暂时无法列出 VolumeSnapshotClass；创建快照时可直接使用默认类或手动输入名称。"
	}
	classes := make([]string, 0, len(list.Items))
	defaultClass := ""
	for _, item := range list.Items {
		classes = append(classes, item.GetName())
		if item.GetAnnotations()["snapshot.storage.kubernetes.io/is-default-class"] == "true" || item.GetAnnotations()["snapshot.storage.kubernetes.io/is-default-class"] == "True" {
			defaultClass = item.GetName()
		}
	}
	sort.Strings(classes)
	return classes, defaultClass, ""
}

func volumeSnapshotSummary(item unstructured.Unstructured) domain.VolumeSnapshot {
	ready, _, _ := unstructured.NestedBool(item.Object, "status", "readyToUse")
	restoreSize := int64(0)
	if value := nestedString(item.Object, "status", "restoreSize"); value != "" {
		if quantity, err := resource.ParseQuantity(value); err == nil {
			restoreSize = quantity.Value()
		}
	}
	creationTime := time.Time{}
	if value := nestedString(item.Object, "status", "creationTime"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			creationTime = parsed
		}
	}
	errorMessage := nestedString(item.Object, "status", "error", "message")
	status := "Pending"
	if ready {
		status = "Ready"
	} else if errorMessage != "" {
		status = "Error"
	}
	return domain.VolumeSnapshot{
		Name: item.GetName(), Namespace: item.GetNamespace(), ResourceVersion: item.GetResourceVersion(),
		SourcePVC:               nestedString(item.Object, "spec", "source", "persistentVolumeClaimName"),
		VolumeSnapshotClassName: nestedString(item.Object, "spec", "volumeSnapshotClassName"),
		VolumeSnapshotContent:   nestedString(item.Object, "status", "boundVolumeSnapshotContentName"),
		Status:                  status, ReadyToUse: ready, RestoreSizeBytes: restoreSize, ErrorMessage: errorMessage,
		CreationTime: creationTime, CreatedAt: item.GetCreationTimestamp().Time,
	}
}

func nestedString(object map[string]any, fields ...string) string {
	value, _, _ := unstructured.NestedString(object, fields...)
	return value
}
