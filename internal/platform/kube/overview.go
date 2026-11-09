package kube

import (
	"context"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeOverview streams nodes and pods in bounded chunks and returns only the
// aggregate values needed by the overview page. The node count is normally
// small, but pod requests can be large, so neither collection is materialized.
func (c *Client) NodeOverview(ctx context.Context) (domain.NodeOverview, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	usageByNode := make(map[string]nodeUsage)
	err := scanChunks(ctx, func(options metav1.ListOptions) ([]corev1.Pod, metav1.ListMeta, error) {
		items, err := c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return items.Items, items.ListMeta, nil
	}, func(pods []corev1.Pod) error {
		for _, pod := range pods {
			if pod.Spec.NodeName == "" {
				continue
			}
			usage := nodeUsageFromPods([]corev1.Pod{pod})
			current := usageByNode[pod.Spec.NodeName]
			current.count += usage.count
			current.cpuRequestsMilli += usage.cpuRequestsMilli
			current.memRequestsBytes += usage.memRequestsBytes
			usageByNode[pod.Spec.NodeName] = current
		}
		return nil
	})
	if err != nil {
		return domain.NodeOverview{}, err
	}

	actualByNode := c.nodeMetrics(ctx)
	result := domain.NodeOverview{}
	err = scanChunks(ctx, func(options metav1.ListOptions) ([]corev1.Node, metav1.ListMeta, error) {
		items, err := c.clientset.CoreV1().Nodes().List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return items.Items, items.ListMeta, nil
	}, func(nodes []corev1.Node) error {
		for _, node := range nodes {
			summary := nodeSummary(node, usageByNode[node.Name])
			if usage, ok := actualByNode[node.Name]; ok {
				summary.CPUUsageMilli = usage.CPUUsageMilli
				summary.MemoryUsageBytes = usage.MemoryUsageBytes
			}
			result.Total++
			if summary.Status == "Ready" {
				result.Ready++
			}
			result.Capacity.CPUAllocatableMilli += summary.CPUCapacityMilli
			result.Capacity.MemoryAllocatableBytes += summary.MemoryCapacityBytes
			result.Capacity.PodCapacity += summary.PodCapacity
			result.Capacity.CPURequestsMilli += summary.CPURequestsMilli
			result.Capacity.MemoryRequestsBytes += summary.MemoryRequestsBytes
			result.Capacity.CPUUsageMilli += summary.CPUUsageMilli
			result.Capacity.MemoryUsageBytes += summary.MemoryUsageBytes
		}
		return nil
	})
	if err != nil {
		return domain.NodeOverview{}, err
	}
	return result, nil
}

// PodOverview streams pod status in bounded chunks without fetching metrics for
// every pod, which keeps the overview responsive on large clusters.
func (c *Client) PodOverview(ctx context.Context) (domain.PodOverview, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	result := domain.PodOverview{}
	err := scanChunks(ctx, func(options metav1.ListOptions) ([]corev1.Pod, metav1.ListMeta, error) {
		items, err := c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return items.Items, items.ListMeta, nil
	}, func(pods []corev1.Pod) error {
		for _, pod := range pods {
			result.Total++
			status := string(pod.Status.Phase)
			if pod.DeletionTimestamp != nil {
				status = "Terminating"
			}
			switch status {
			case "Running":
				result.Running++
			case "Succeeded", "Completed", "Terminating":
			default:
				result.Unhealthy++
			}
		}
		return nil
	})
	if err != nil {
		return domain.PodOverview{}, err
	}
	return result, nil
}

func (c *Client) CountWorkloads(ctx context.Context) (int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	deployments, err := countCollection(ctx, func(options metav1.ListOptions) ([]string, metav1.ListMeta, error) {
		items, err := c.clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return make([]string, len(items.Items)), items.ListMeta, nil
	})
	if err != nil {
		return 0, err
	}
	statefulSets, err := countCollection(ctx, func(options metav1.ListOptions) ([]string, metav1.ListMeta, error) {
		items, err := c.clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return make([]string, len(items.Items)), items.ListMeta, nil
	})
	if err != nil {
		return 0, err
	}
	daemonSets, err := countCollection(ctx, func(options metav1.ListOptions) ([]string, metav1.ListMeta, error) {
		items, err := c.clientset.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return make([]string, len(items.Items)), items.ListMeta, nil
	})
	if err != nil {
		return 0, err
	}
	jobs, err := countCollection(ctx, func(options metav1.ListOptions) ([]string, metav1.ListMeta, error) {
		items, err := c.clientset.BatchV1().Jobs(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return make([]string, len(items.Items)), items.ListMeta, nil
	})
	if err != nil {
		return 0, err
	}
	cronJobs, err := countCollection(ctx, func(options metav1.ListOptions) ([]string, metav1.ListMeta, error) {
		items, err := c.clientset.BatchV1().CronJobs(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return make([]string, len(items.Items)), items.ListMeta, nil
	})
	if err != nil {
		return 0, err
	}
	return deployments + statefulSets + daemonSets + jobs + cronJobs, nil
}

func (c *Client) CountNamespaces(ctx context.Context) (int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return countCollection(ctx, func(options metav1.ListOptions) ([]corev1.Namespace, metav1.ListMeta, error) {
		items, err := c.clientset.CoreV1().Namespaces().List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return items.Items, items.ListMeta, nil
	})
}
