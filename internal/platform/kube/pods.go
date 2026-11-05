package kube

import (
	"context"
	"io"
	"sort"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	actual := c.podMetrics(ctx, namespace)
	out := make([]domain.PodSummary, 0, len(items.Items))
	for _, pod := range items.Items {
		usage, hasMetrics := actual[pod.Namespace+"/"+pod.Name]
		summary := podSummaryWithUsage(pod, usage, hasMetrics)
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.Namespace, summary.Status, summary.Node, summary.OwnerName}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sortPods(out)
	return out, nil
}

func (c *Client) GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.PodDetail{}, mapKubeError(err)
	}
	summary := podSummary(*pod)
	if usage, ok := c.podMetric(ctx, namespace, name); ok {
		summary.CPUUsageMilli = usage.CPUUsageMilli
		summary.MemoryUsageBytes = usage.MemoryUsageBytes
		summary.HasMetrics = true
	}
	detail := domain.PodDetail{
		PodSummary:     summary,
		QOSClass:       string(pod.Status.QOSClass),
		ServiceAccount: pod.Spec.ServiceAccountName,
	}
	for _, condition := range pod.Status.Conditions {
		detail.Conditions = append(detail.Conditions, domain.Condition{
			Type: string(condition.Type), Status: string(condition.Status), LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	for i, container := range pod.Spec.Containers {
		item := domain.Container{Name: container.Name, Image: container.Image}
		if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			item.CPURequestMilli = q.MilliValue()
		}
		if q, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
			item.CPULimitMilli = q.MilliValue()
		}
		if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			item.MemoryRequestBytes = q.Value()
		}
		if q, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
			item.MemoryLimitBytes = q.Value()
		}
		if i < len(pod.Status.ContainerStatuses) {
			status := pod.Status.ContainerStatuses[i]
			item.Ready = status.Ready
			item.RestartCount = status.RestartCount
			switch {
			case status.State.Running != nil:
				item.State = "Running"
			case status.State.Waiting != nil:
				item.State = "Waiting"
				item.Reason = status.State.Waiting.Reason
			case status.State.Terminated != nil:
				item.State = "Terminated"
				item.Reason = status.State.Terminated.Reason
			default:
				item.State = "Unknown"
			}
		}
		detail.Containers = append(detail.Containers, item)
	}
	return detail, nil
}

func (c *Client) StreamPodLogs(ctx context.Context, namespace, name, container string, follow bool, tailLines int64, timestamps bool, limitBytes int64) (io.ReadCloser, error) {
	options := &corev1.PodLogOptions{
		Container:  container,
		Follow:     follow,
		TailLines:  &tailLines,
		Timestamps: timestamps,
	}
	if limitBytes > 0 {
		options.LimitBytes = &limitBytes
	}
	stream, err := c.clientset.CoreV1().Pods(namespace).GetLogs(name, options).Stream(ctx)
	if err != nil {
		return nil, mapKubeError(err)
	}
	return stream, nil
}

func (c *Client) ListPodsBySelector(ctx context.Context, namespace string, selector map[string]string, search string) ([]domain.PodSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	items, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selectorString(selector)})
	if err != nil {
		return nil, mapKubeError(err)
	}
	actual := c.podMetrics(ctx, namespace)
	out := make([]domain.PodSummary, 0, len(items.Items))
	for _, pod := range items.Items {
		usage, hasMetrics := actual[pod.Namespace+"/"+pod.Name]
		summary := podSummaryWithUsage(pod, usage, hasMetrics)
		if search != "" && !matchesSearch([]string{summary.Name, summary.Status}, search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
