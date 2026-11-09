package kube

import (
	"context"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func (c *Client) nodeMetrics(ctx context.Context) map[string]domain.ResourceUsage {
	if !c.MetricsAvailable(ctx) {
		return nil
	}
	items, err := c.metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	out := make(map[string]domain.ResourceUsage, len(items.Items))
	for _, item := range items.Items {
		out[item.Name] = usageFromQuantity(item.Usage, nil)
	}
	return out
}

func (c *Client) nodeMetric(ctx context.Context, name string) (domain.ResourceUsage, bool) {
	if !c.MetricsAvailable(ctx) {
		return domain.ResourceUsage{}, false
	}
	item, err := c.metrics.MetricsV1beta1().NodeMetricses().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.ResourceUsage{}, false
	}
	return usageFromQuantity(item.Usage, nil), true
}

func (c *Client) podMetrics(ctx context.Context, namespace string) map[string]domain.ResourceUsage {
	if !c.MetricsAvailable(ctx) {
		return nil
	}
	items, err := c.metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	out := make(map[string]domain.ResourceUsage, len(items.Items))
	for _, item := range items.Items {
		out[item.Namespace+"/"+item.Name] = usageFromQuantity(nil, item.Containers)
	}
	return out
}

func (c *Client) podMetric(ctx context.Context, namespace, name string) (domain.ResourceUsage, bool) {
	if !c.MetricsAvailable(ctx) {
		return domain.ResourceUsage{}, false
	}
	item, err := c.metrics.MetricsV1beta1().PodMetricses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.ResourceUsage{}, false
	}
	return usageFromQuantity(nil, item.Containers), true
}

func usageFromQuantity(nodeUsage corev1.ResourceList, containers []metricsv1beta1.ContainerMetrics) domain.ResourceUsage {
	var result domain.ResourceUsage
	if q, ok := nodeUsage[corev1.ResourceCPU]; ok {
		result.CPUUsageMilli += q.MilliValue()
	}
	if q, ok := nodeUsage[corev1.ResourceMemory]; ok {
		result.MemoryUsageBytes += q.Value()
	}
	for _, container := range containers {
		if q, ok := container.Usage[corev1.ResourceCPU]; ok {
			result.CPUUsageMilli += q.MilliValue()
		}
		if q, ok := container.Usage[corev1.ResourceMemory]; ok {
			result.MemoryUsageBytes += q.Value()
		}
	}
	return result
}
