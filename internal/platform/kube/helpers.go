package kube

import (
	"sort"
	"strings"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
)

func matchesSearch(values []string, search string) bool {
	if search == "" {
		return true
	}
	needle := strings.ToLower(search)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func nodeConditionList(conditions []corev1.NodeCondition) []domain.Condition {
	out := make([]domain.Condition, 0, len(conditions))
	for _, condition := range conditions {
		out = append(out, domain.Condition{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	return out
}

func podReadyCount(pod corev1.Pod) (ready, total int) {
	total = len(pod.Status.ContainerStatuses)
	if total == 0 {
		total = len(pod.Spec.Containers)
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
	}
	return ready, total
}

func podRestarts(pod corev1.Pod) int32 {
	var restarts int32
	for _, status := range pod.Status.ContainerStatuses {
		restarts += status.RestartCount
	}
	for _, status := range pod.Status.InitContainerStatuses {
		restarts += status.RestartCount
	}
	return restarts
}

func podStatus(pod corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "Terminating"
	}
	if pod.Status.Reason != "" && pod.Status.Reason != "Completed" {
		return pod.Status.Reason
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			if pod.Status.Phase == corev1.PodPending {
				return status.State.Waiting.Reason
			}
		}
		if status.State.Terminated != nil && status.State.Terminated.Reason != "" && pod.Status.Phase != corev1.PodSucceeded {
			return status.State.Terminated.Reason
		}
	}
	if pod.Status.Phase == "" {
		return "Unknown"
	}
	return string(pod.Status.Phase)
}

func owner(pod corev1.Pod) (kind, name string) {
	if len(pod.OwnerReferences) == 0 {
		return "", ""
	}
	return pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name
}

func podSummary(pod corev1.Pod) domain.PodSummary {
	return podSummaryWithUsage(pod, domain.ResourceUsage{}, false)
}

func podSummaryWithUsage(pod corev1.Pod, usage domain.ResourceUsage, hasMetrics bool) domain.PodSummary {
	ready, total := podReadyCount(pod)
	ownerKind, ownerName := owner(pod)
	return domain.PodSummary{
		Name:             pod.Name,
		Namespace:        pod.Namespace,
		Status:           podStatus(pod),
		Ready:            itoa(ready) + "/" + itoa(total),
		Restarts:         podRestarts(pod),
		IP:               pod.Status.PodIP,
		Node:             pod.Spec.NodeName,
		OwnerKind:        ownerKind,
		OwnerName:        ownerName,
		CreatedAt:        pod.CreationTimestamp.Time,
		Labels:           pod.Labels,
		CPUUsageMilli:    usage.CPUUsageMilli,
		MemoryUsageBytes: usage.MemoryUsageBytes,
		HasMetrics:       hasMetrics,
	}
}

func sortPods(items []domain.PodSummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace == items[j].Namespace {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
