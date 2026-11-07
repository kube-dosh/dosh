package kube

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Client) ListNodes(ctx context.Context, query domain.ListQuery) ([]domain.NodeSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	pods, err := c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	usageByNode := map[string]nodeUsage{}
	actualByNode := c.nodeMetrics(ctx)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		current := usageByNode[pod.Spec.NodeName]
		current.count++
		for _, container := range pod.Spec.Containers {
			if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				current.cpuRequestsMilli += q.MilliValue()
			}
			if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				current.memRequestsBytes += q.Value()
			}
		}
		usageByNode[pod.Spec.NodeName] = current
	}
	out := make([]domain.NodeSummary, 0, len(nodes.Items))
	for i := range nodes.Items {
		node := &nodes.Items[i]
		summary := nodeSummary(*node, usageByNode[node.Name])
		if usage, ok := actualByNode[node.Name]; ok {
			summary.CPUUsageMilli = usage.CPUUsageMilli
			summary.MemoryUsageBytes = usage.MemoryUsageBytes
			summary.HasMetrics = true
		}
		if query.Search != "" && !matchesSearch([]string{summary.Name, summary.InternalIP, summary.OSImage}, query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Client) GetNode(ctx context.Context, name string) (domain.NodeDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeDetail{}, mapKubeError(err)
	}
	return c.nodeDetail(ctx, node)
}

func (c *Client) nodeDetail(ctx context.Context, node *corev1.Node) (domain.NodeDetail, error) {
	pods, err := c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node.Name})
	if err != nil {
		return domain.NodeDetail{}, mapKubeError(err)
	}
	summary := nodeSummary(*node, nodeUsageFromPods(pods.Items))
	if usage, ok := c.nodeMetrics(ctx)[node.Name]; ok {
		summary.CPUUsageMilli = usage.CPUUsageMilli
		summary.MemoryUsageBytes = usage.MemoryUsageBytes
		summary.HasMetrics = true
	}
	detail := domain.NodeDetail{
		NodeSummary:     summary,
		ResourceVersion: node.ResourceVersion,
		Conditions:      nodeConditionList(node.Status.Conditions),
		Taints:          make([]domain.Taint, 0, len(node.Spec.Taints)),
		Labels:          node.Labels,
		Annotations:     node.Annotations,
		Pods:            make([]domain.PodSummary, 0, len(pods.Items)),
	}
	for _, taint := range node.Spec.Taints {
		detail.Taints = append(detail.Taints, domain.Taint{Key: taint.Key, Value: taint.Value, Effect: string(taint.Effect)})
	}
	for _, pod := range pods.Items {
		detail.Pods = append(detail.Pods, podSummary(pod))
	}
	sortPods(detail.Pods)
	return detail, nil
}

func (c *Client) SetNodeLabels(ctx context.Context, name string, update domain.NodeLabelUpdate) (domain.NodeDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeDetail{}, mapKubeError(err)
	}
	desired := update.Labels
	if desired == nil {
		desired = map[string]string{}
	}
	if update.ResourceVersion != "" && node.ResourceVersion != update.ResourceVersion {
		if maps.Equal(node.Labels, desired) {
			return c.nodeDetail(ctx, node)
		}
		return domain.NodeDetail{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, node.ResourceVersion)
	}
	if maps.Equal(node.Labels, desired) {
		return c.nodeDetail(ctx, node)
	}
	node.Labels = desired
	updated, err := c.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{FieldManager: "dosh"})
	if err != nil {
		return domain.NodeDetail{}, mapKubeError(err)
	}
	return c.nodeDetail(ctx, updated)
}

func (c *Client) SetNodeTaints(ctx context.Context, name string, update domain.NodeTaintUpdate) (domain.NodeDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeDetail{}, mapKubeError(err)
	}
	if update.ResourceVersion != "" && node.ResourceVersion != update.ResourceVersion {
		if nodeTaintsEqual(node.Spec.Taints, update.Taints) {
			return c.nodeDetail(ctx, node)
		}
		return domain.NodeDetail{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, node.ResourceVersion)
	}
	if nodeTaintsEqual(node.Spec.Taints, update.Taints) {
		return c.nodeDetail(ctx, node)
	}
	node.Spec.Taints = nodeTaintsFromDomain(update.Taints, node.Spec.Taints)
	updated, err := c.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{FieldManager: "dosh"})
	if err != nil {
		return domain.NodeDetail{}, mapKubeError(err)
	}
	return c.nodeDetail(ctx, updated)
}

func nodeTaintsFromDomain(desired []domain.Taint, existing []corev1.Taint) []corev1.Taint {
	now := metav1.Now()
	existingTimes := make(map[string]*metav1.Time, len(existing))
	for _, taint := range existing {
		if taint.TimeAdded != nil {
			existingTimes[taint.Key+"\x00"+string(taint.Effect)] = taint.TimeAdded
		}
	}
	result := make([]corev1.Taint, 0, len(desired))
	for _, taint := range desired {
		item := corev1.Taint{Key: taint.Key, Value: taint.Value, Effect: corev1.TaintEffect(taint.Effect)}
		if added := existingTimes[taint.Key+"\x00"+taint.Effect]; added != nil {
			item.TimeAdded = added
		} else {
			added := now
			item.TimeAdded = &added
		}
		result = append(result, item)
	}
	return result
}

func nodeTaintsEqual(current []corev1.Taint, desired []domain.Taint) bool {
	if len(current) != len(desired) {
		return false
	}
	for index, taint := range current {
		if taint.Key != desired[index].Key || taint.Value != desired[index].Value || string(taint.Effect) != desired[index].Effect {
			return false
		}
	}
	return true
}

func (c *Client) SetNodeScheduling(ctx context.Context, name string, unschedulable bool) (domain.NodeSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeSummary{}, mapKubeError(err)
	}
	pods, err := c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: nodeFieldSelector(name)})
	if err != nil {
		return domain.NodeSummary{}, mapKubeError(err)
	}
	if node.Spec.Unschedulable != unschedulable {
		patch := []byte(fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, unschedulable))
		node, err = c.clientset.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			return domain.NodeSummary{}, mapKubeError(err)
		}
	}
	summary := nodeSummary(*node, nodeUsageFromPods(pods.Items))
	if usage, ok := c.nodeMetrics(ctx)[node.Name]; ok {
		summary.CPUUsageMilli = usage.CPUUsageMilli
		summary.MemoryUsageBytes = usage.MemoryUsageBytes
		summary.HasMetrics = true
	}
	return summary, nil
}

type nodeUsage struct {
	count            int
	cpuRequestsMilli int64
	memRequestsBytes int64
}

func nodeUsageFromPods(pods []corev1.Pod) nodeUsage {
	var result nodeUsage
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		result.count++
		for _, container := range pod.Spec.Containers {
			if q, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				result.cpuRequestsMilli += q.MilliValue()
			}
			if q, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				result.memRequestsBytes += q.Value()
			}
		}
	}
	return result
}

func nodeSummary(node corev1.Node, usage nodeUsage) domain.NodeSummary {
	status := "NotReady"
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			status = "Ready"
			break
		}
	}
	roles := make([]string, 0, 3)
	roleKeys := []struct{ key, label string }{
		{"node-role.kubernetes.io/control-plane", "control-plane"},
		{"node-role.kubernetes.io/master", "master"},
		{"node-role.kubernetes.io/worker", "worker"},
	}
	for _, role := range roleKeys {
		if _, ok := node.Labels[role.key]; ok {
			roles = append(roles, role.label)
		}
	}
	if len(roles) == 0 {
		roles = append(roles, "worker")
	}
	addr := ""
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			addr = address.Address
			break
		}
	}
	cpu := node.Status.Allocatable[corev1.ResourceCPU]
	mem := node.Status.Allocatable[corev1.ResourceMemory]
	podsCapacity := node.Status.Allocatable[corev1.ResourcePods]
	return domain.NodeSummary{
		Name:                node.Name,
		Status:              status,
		Roles:               roles,
		InternalIP:          addr,
		Version:             node.Status.NodeInfo.KubeletVersion,
		OSImage:             node.Status.NodeInfo.OSImage,
		KernelVersion:       node.Status.NodeInfo.KernelVersion,
		ContainerRuntime:    node.Status.NodeInfo.ContainerRuntimeVersion,
		CPUCapacityMilli:    cpu.MilliValue(),
		MemoryCapacityBytes: mem.Value(),
		PodCapacity:         podsCapacity.Value(),
		PodCount:            usage.count,
		CPURequestsMilli:    usage.cpuRequestsMilli,
		MemoryRequestsBytes: usage.memRequestsBytes,
		Unschedulable:       node.Spec.Unschedulable,
		CreatedAt:           node.CreationTimestamp.Time,
	}
}

func (c *Client) ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	items, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.Namespace, 0, len(items.Items))
	for _, item := range items.Items {
		if query.Search != "" && !matchesSearch([]string{item.Name, string(item.Status.Phase)}, query.Search) {
			continue
		}
		out = append(out, domain.Namespace{
			Name: item.Name, Status: string(item.Status.Phase), ResourceVersion: item.ResourceVersion, Labels: item.Labels,
			Annotations: item.Annotations, CreatedAt: item.CreationTimestamp.Time,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Client) CreateNamespace(ctx context.Context, spec domain.NamespaceCreateSpec) (domain.Namespace, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	created, err := c.clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Labels: spec.Labels, Annotations: spec.Annotations},
	}, metav1.CreateOptions{})
	if err != nil {
		return domain.Namespace{}, mapKubeError(err)
	}
	return domain.Namespace{
		Name: created.Name, Status: string(created.Status.Phase), ResourceVersion: created.ResourceVersion,
		Labels: created.Labels, Annotations: created.Annotations, CreatedAt: created.CreationTimestamp.Time,
	}, nil
}

func (c *Client) GetNamespace(ctx context.Context, name string) (domain.NamespaceDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NamespaceDetail{}, mapKubeError(err)
	}
	quotas, err := c.clientset.CoreV1().ResourceQuotas(name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.NamespaceDetail{}, mapKubeError(err)
	}
	limitRanges, err := c.clientset.CoreV1().LimitRanges(name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.NamespaceDetail{}, mapKubeError(err)
	}
	detail := domain.NamespaceDetail{
		Namespace: domain.Namespace{
			Name: item.Name, Status: string(item.Status.Phase), ResourceVersion: item.ResourceVersion, Labels: item.Labels,
			Annotations: item.Annotations, CreatedAt: item.CreationTimestamp.Time,
		},
		ResourceQuotas: make([]domain.ResourceQuota, 0, len(quotas.Items)),
		LimitRanges:    make([]domain.LimitRange, 0, len(limitRanges.Items)),
	}
	for _, quota := range quotas.Items {
		detail.ResourceQuotas = append(detail.ResourceQuotas, domain.ResourceQuota{
			Name: quota.Name, Hard: resourceListStrings(quota.Status.Hard),
			Used: resourceListStrings(quota.Status.Used), CreatedAt: quota.CreationTimestamp.Time,
		})
	}
	for _, limitRange := range limitRanges.Items {
		limits := make([]domain.LimitRangeItem, 0, len(limitRange.Spec.Limits))
		for _, limit := range limitRange.Spec.Limits {
			limits = append(limits, domain.LimitRangeItem{
				Type: string(limit.Type), Min: resourceListStrings(limit.Min), Max: resourceListStrings(limit.Max),
				Default: resourceListStrings(limit.Default), DefaultRequest: resourceListStrings(limit.DefaultRequest),
			})
		}
		detail.LimitRanges = append(detail.LimitRanges, domain.LimitRange{
			Name: limitRange.Name, Limits: limits, CreatedAt: limitRange.CreationTimestamp.Time,
		})
	}
	sort.Slice(detail.ResourceQuotas, func(i, j int) bool { return detail.ResourceQuotas[i].Name < detail.ResourceQuotas[j].Name })
	sort.Slice(detail.LimitRanges, func(i, j int) bool { return detail.LimitRanges[i].Name < detail.LimitRanges[j].Name })
	return detail, nil
}

func (c *Client) DeleteNamespace(ctx context.Context, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return mapKubeError(err)
	}
	if resourceVersion != "" && item.ResourceVersion != resourceVersion {
		return fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, resourceVersion, item.ResourceVersion)
	}
	policy := metav1.DeletePropagationForeground
	options := metav1.DeleteOptions{PropagationPolicy: &policy}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.CoreV1().Namespaces().Delete(ctx, name, options))
}

func resourceListStrings(items corev1.ResourceList) map[string]string {
	out := make(map[string]string, len(items))
	for name, quantity := range items {
		out[string(name)] = quantity.String()
	}
	return out
}

func (c *Client) ListPodsForNode(ctx context.Context, node string) ([]domain.PodSummary, error) {
	pods, err := c.ListPods(ctx, domain.ListQuery{})
	if err != nil {
		return nil, err
	}
	out := make([]domain.PodSummary, 0)
	for _, pod := range pods {
		if pod.Node == node {
			out = append(out, pod)
		}
	}
	if len(out) == 0 {
		return []domain.PodSummary{}, nil
	}
	return out, nil
}

func nodeFieldSelector(node string) string {
	return fmt.Sprintf("spec.nodeName=%s", node)
}
