package kube

import (
	"context"
	"fmt"
	"sort"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

const (
	drainActionEvict     = "evict"
	drainActionSkip      = "skip"
	drainActionBlock     = "block"
	drainActionBlocked   = "blocked"
	drainActionFailed    = "failed"
	drainActionRemaining = "remaining"
)

// PlanNodeDrain performs a read-only safety preview. PDB matching is advisory;
// the Eviction API remains the authority for whether a Pod can be displaced.
func (c *Client) PlanNodeDrain(ctx context.Context, name string, options domain.NodeDrainOptions) (domain.NodeDrainPlan, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeDrainPlan{}, mapKubeError(err)
	}
	pods, err := c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{FieldSelector: nodeFieldSelector(name)})
	if err != nil {
		return domain.NodeDrainPlan{}, mapKubeError(err)
	}
	plan := domain.NodeDrainPlan{Node: name, Unschedulable: node.Spec.Unschedulable}
	pdbsByNamespace, warnings := c.nodeDrainPDBs(ctx, pods.Items)
	plan.Warnings = append(plan.Warnings, warnings...)
	for _, pod := range pods.Items {
		item := classifyDrainPod(pod, options, pdbsByNamespace[pod.Namespace])
		plan.Pods = append(plan.Pods, item)
		switch item.Action {
		case drainActionEvict:
			plan.EvictCount++
		default:
			plan.SkippedCount++
		}
	}
	sort.Slice(plan.Pods, func(i, j int) bool {
		if plan.Pods[i].Namespace != plan.Pods[j].Namespace {
			return plan.Pods[i].Namespace < plan.Pods[j].Namespace
		}
		return plan.Pods[i].Name < plan.Pods[j].Name
	})
	return plan, nil
}

// DrainNode cordons a node and evicts eligible Pods one at a time. On a PDB
// rejection it stops immediately and returns the blocked/remaining set so the
// caller never claims a successful drain.
func (c *Client) DrainNode(ctx context.Context, name string, options domain.NodeDrainOptions) (domain.NodeDrainResult, error) {
	timeout := time.Duration(options.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeDrainResult{}, mapKubeError(err)
	}
	if !node.Spec.Unschedulable {
		_, err = c.clientset.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, []byte(`{"spec":{"unschedulable":true}}`), metav1.PatchOptions{FieldManager: "dosh"})
		if err != nil {
			return domain.NodeDrainResult{}, mapKubeError(err)
		}
	}
	plan, err := c.PlanNodeDrain(ctx, name, options)
	if err != nil {
		return domain.NodeDrainResult{}, err
	}
	result := domain.NodeDrainResult{Node: name, Cordoned: true, Warnings: append([]string(nil), plan.Warnings...)}
	stopped := false
	for _, item := range plan.Pods {
		if stopped {
			item.Action = drainActionRemaining
			result.Pods = append(result.Pods, item)
			result.SkippedCount++
			continue
		}
		switch item.Action {
		case drainActionEvict:
			eviction := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: item.Name, Namespace: item.Namespace}}
			err := c.clientset.PolicyV1().Evictions(item.Namespace).Evict(ctx, eviction)
			switch {
			case err == nil:
				item.Action = drainActionEvict
				result.EvictedCount++
			case apierrors.IsTooManyRequests(err):
				item.Action = drainActionBlocked
				item.EvictionErr = err.Error()
				result.BlockedCount++
				result.Warnings = append(result.Warnings, fmt.Sprintf("Pod %s/%s 被 PodDisruptionBudget 阻止，后续 Pod 未驱逐", item.Namespace, item.Name))
				stopped = true
			case apierrors.IsForbidden(err):
				item.Action = drainActionFailed
				item.EvictionErr = err.Error()
				result.FailedCount++
				result.Warnings = append(result.Warnings, fmt.Sprintf("Pod %s/%s 驱逐被拒绝，后续 Pod 未驱逐", item.Namespace, item.Name))
				stopped = true
			case apierrors.IsNotFound(err):
				item.Action = drainActionSkip
				item.Reason = "Pod 已不存在"
				result.SkippedCount++
			default:
				item.Action = drainActionFailed
				item.EvictionErr = err.Error()
				result.FailedCount++
			}
		case drainActionBlock:
			item.Action = drainActionBlocked
			result.BlockedCount++
		default:
			result.SkippedCount++
		}
		result.Pods = append(result.Pods, item)
	}
	return result, nil
}

func classifyDrainPod(pod corev1.Pod, options domain.NodeDrainOptions, pdbs []policyv1.PodDisruptionBudget) domain.NodeDrainPod {
	ownerKind, ownerName := owner(pod)
	item := domain.NodeDrainPod{
		Name: pod.Name, Namespace: pod.Namespace, Status: string(pod.Status.Phase),
		OwnerKind: ownerKind, OwnerName: ownerName, Action: drainActionEvict,
	}
	for _, pdb := range pdbs {
		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err == nil && selector.Matches(labels.Set(pod.Labels)) {
			item.PDBNames = append(item.PDBNames, pdb.Name)
		}
	}
	switch {
	case pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed:
		item.Action, item.Reason = drainActionSkip, "Pod 已结束"
	case pod.Annotations["kubernetes.io/config.mirror"] != "":
		item.Action, item.Reason = drainActionSkip, "镜像 Pod 由 kubelet 管理"
	case ownerKind == "DaemonSet" && options.IgnoreDaemonSets:
		item.Action, item.Reason = drainActionSkip, "已选择忽略 DaemonSet Pod"
	case ownerKind == "DaemonSet":
		item.Action, item.Reason = drainActionBlock, "存在 DaemonSet Pod，需要显式忽略 DaemonSet"
	case ownerKind == "" && !options.Force:
		item.Action, item.Reason = drainActionBlock, "Pod 没有控制器，需要显式允许强制驱逐"
	}
	return item
}

func (c *Client) nodeDrainPDBs(ctx context.Context, pods []corev1.Pod) (map[string][]policyv1.PodDisruptionBudget, []string) {
	namespaces := make(map[string]struct{})
	for _, pod := range pods {
		namespaces[pod.Namespace] = struct{}{}
	}
	result := make(map[string][]policyv1.PodDisruptionBudget)
	var warnings []string
	for namespace := range namespaces {
		list, err := c.clientset.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("无法读取命名空间 %s 的 PodDisruptionBudget：%s", namespace, err.Error()))
			continue
		}
		result[namespace] = list.Items
	}
	return result, warnings
}
