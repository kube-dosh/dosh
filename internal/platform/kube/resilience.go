package kube

import (
	"context"
	"fmt"
	"sort"

	"dosh/internal/domain"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (c *Client) ListAutoscalers(ctx context.Context, namespace string) ([]domain.AutoscalerSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.AutoscalerSummary, 0, len(items.Items))
	for _, item := range items.Items {
		out = append(out, autoscalerSummary(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func (c *Client) UpsertAutoscaler(ctx context.Context, spec domain.AutoscalerSpec, dryRun bool) (domain.AutoscalerSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	if spec.ResourceVersion == "" {
		item := autoscalerForSpec(spec)
		created, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(spec.Namespace).Create(ctx, item, options)
		if err != nil {
			return domain.AutoscalerSummary{}, mapKubeError(err)
		}
		return autoscalerSummary(*created), nil
	}
	item, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(spec.Namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		return domain.AutoscalerSummary{}, mapKubeError(err)
	}
	if item.ResourceVersion != spec.ResourceVersion {
		return domain.AutoscalerSummary{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, spec.ResourceVersion, item.ResourceVersion)
	}
	item.Spec = autoscalerForSpec(spec).Spec
	updated, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(spec.Namespace).Update(ctx, item, metav1.UpdateOptions{DryRun: options.DryRun})
	if err != nil {
		return domain.AutoscalerSummary{}, mapKubeError(err)
	}
	return autoscalerSummary(*updated), nil
}

func (c *Client) DeleteAutoscaler(ctx context.Context, namespace, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, name, options))
}

func (c *Client) ListDisruptionBudgets(ctx context.Context, namespace string) ([]domain.DisruptionBudgetSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.DisruptionBudgetSummary, 0, len(items.Items))
	for _, item := range items.Items {
		out = append(out, disruptionBudgetSummary(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func (c *Client) UpsertDisruptionBudget(ctx context.Context, spec domain.DisruptionBudgetSpec, dryRun bool) (domain.DisruptionBudgetSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	if spec.ResourceVersion == "" {
		item, err := disruptionBudgetForSpec(spec)
		if err != nil {
			return domain.DisruptionBudgetSummary{}, err
		}
		created, err := c.clientset.PolicyV1().PodDisruptionBudgets(spec.Namespace).Create(ctx, item, options)
		if err != nil {
			return domain.DisruptionBudgetSummary{}, mapKubeError(err)
		}
		return disruptionBudgetSummary(*created), nil
	}
	item, err := c.clientset.PolicyV1().PodDisruptionBudgets(spec.Namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		return domain.DisruptionBudgetSummary{}, mapKubeError(err)
	}
	if item.ResourceVersion != spec.ResourceVersion {
		return domain.DisruptionBudgetSummary{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, spec.ResourceVersion, item.ResourceVersion)
	}
	desired, err := disruptionBudgetForSpec(spec)
	if err != nil {
		return domain.DisruptionBudgetSummary{}, err
	}
	item.Spec = desired.Spec
	updated, err := c.clientset.PolicyV1().PodDisruptionBudgets(spec.Namespace).Update(ctx, item, metav1.UpdateOptions{DryRun: options.DryRun})
	if err != nil {
		return domain.DisruptionBudgetSummary{}, mapKubeError(err)
	}
	return disruptionBudgetSummary(*updated), nil
}

func (c *Client) DeleteDisruptionBudget(ctx context.Context, namespace, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.PolicyV1().PodDisruptionBudgets(namespace).Delete(ctx, name, options))
}

func autoscalerForSpec(spec domain.AutoscalerSpec) *autoscalingv2.HorizontalPodAutoscaler {
	minReplicas := spec.MinReplicas
	if minReplicas < 1 {
		minReplicas = 1
	}
	metrics := make([]autoscalingv2.MetricSpec, 0, 2)
	if spec.CPUUtilization != nil {
		metrics = append(metrics, resourceMetricSpec("cpu", *spec.CPUUtilization))
	}
	if spec.MemoryUtilization != nil {
		metrics = append(metrics, resourceMetricSpec("memory", *spec.MemoryUtilization))
	}
	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"},
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name, Namespace: spec.Namespace, Labels: map[string]string{managedByLabel: "dosh"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1", Kind: spec.TargetKind, Name: spec.TargetName,
			},
			MinReplicas: &minReplicas, MaxReplicas: spec.MaxReplicas, Metrics: metrics,
		},
	}
}

func resourceMetricSpec(resource string, utilization int32) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: coreResourceName(resource),
			Target: autoscalingv2.MetricTarget{
				Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &utilization,
			},
		},
	}
}

func coreResourceName(name string) corev1.ResourceName {
	return corev1.ResourceName(name)
}

func autoscalerSummary(item autoscalingv2.HorizontalPodAutoscaler) domain.AutoscalerSummary {
	status := &domain.AutoscalerSummary{
		Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		TargetKind: item.Spec.ScaleTargetRef.Kind, TargetName: item.Spec.ScaleTargetRef.Name,
		MinReplicas: 1, MaxReplicas: item.Spec.MaxReplicas,
		CurrentReplicas: item.Status.CurrentReplicas, DesiredReplicas: item.Status.DesiredReplicas,
		Metrics: []domain.AutoscalerMetric{}, Conditions: []domain.Condition{}, CreatedAt: item.CreationTimestamp.Time,
	}
	if item.Spec.MinReplicas != nil {
		status.MinReplicas = *item.Spec.MinReplicas
	}
	if item.Status.LastScaleTime != nil {
		value := item.Status.LastScaleTime.Time
		status.LastScaleTime = &value
	}
	status.Status = autoscalerStatus(item)
	metricIndex := make(map[string]int, len(item.Spec.Metrics)+len(item.Status.CurrentMetrics))
	upsertMetric := func(resource string) domain.AutoscalerMetric {
		if index, exists := metricIndex[resource]; exists {
			return status.Metrics[index]
		}
		entry := domain.AutoscalerMetric{Type: string(autoscalingv2.ResourceMetricSourceType), Resource: resource}
		status.Metrics = append(status.Metrics, entry)
		metricIndex[resource] = len(status.Metrics) - 1
		return entry
	}
	for _, desired := range item.Spec.Metrics {
		if desired.Type != autoscalingv2.ResourceMetricSourceType || desired.Resource == nil {
			continue
		}
		entry := upsertMetric(string(desired.Resource.Name))
		entry.Type = string(desired.Type)
		if desired.Resource.Target.AverageUtilization != nil {
			value := *desired.Resource.Target.AverageUtilization
			entry.TargetUtilization = &value
			entry.Target = fmt.Sprintf("%d%%", value)
		} else if desired.Resource.Target.AverageValue != nil {
			entry.Target = desired.Resource.Target.AverageValue.String()
		}
		status.Metrics[metricIndex[entry.Resource]] = entry
	}
	for _, metric := range item.Status.CurrentMetrics {
		if metric.Resource == nil {
			continue
		}
		entry := upsertMetric(string(metric.Resource.Name))
		entry.Type = string(metric.Type)
		if metric.Resource.Current.AverageUtilization != nil {
			value := *metric.Resource.Current.AverageUtilization
			entry.CurrentUtilization = &value
			entry.Current = fmt.Sprintf("%d%%", value)
		} else if metric.Resource.Current.AverageValue != nil {
			entry.Current = metric.Resource.Current.AverageValue.String()
		}
		status.Metrics[metricIndex[entry.Resource]] = entry
	}
	for _, condition := range item.Status.Conditions {
		status.Conditions = append(status.Conditions, domain.Condition{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	return *status
}

func autoscalerStatus(item autoscalingv2.HorizontalPodAutoscaler) string {
	for _, condition := range item.Status.Conditions {
		if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionUnknown {
			return "Degraded"
		}
		if condition.Type == autoscalingv2.AbleToScale && condition.Status == corev1.ConditionFalse {
			return "Degraded"
		}
	}
	if item.Status.DesiredReplicas != item.Status.CurrentReplicas {
		return "Scaling"
	}
	if item.Status.CurrentReplicas == 0 {
		return "Idle"
	}
	return "Ready"
}

func disruptionBudgetForSpec(spec domain.DisruptionBudgetSpec) (*policyv1.PodDisruptionBudget, error) {
	item := &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name, Namespace: spec.Namespace, Labels: map[string]string{managedByLabel: "dosh"},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: spec.Selector}},
	}
	if spec.MinAvailable != "" {
		value := intstr.Parse(spec.MinAvailable)
		item.Spec.MinAvailable = &value
	}
	if spec.MaxUnavailable != "" {
		value := intstr.Parse(spec.MaxUnavailable)
		item.Spec.MaxUnavailable = &value
	}
	return item, nil
}

func disruptionBudgetSummary(item policyv1.PodDisruptionBudget) domain.DisruptionBudgetSummary {
	summary := domain.DisruptionBudgetSummary{
		Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Selector: map[string]string{}, CurrentHealthy: item.Status.CurrentHealthy,
		DesiredHealthy: item.Status.DesiredHealthy, DisruptionsAllowed: item.Status.DisruptionsAllowed,
		ExpectedPods: item.Status.ExpectedPods, ObservedGeneration: item.Status.ObservedGeneration,
		Conditions: []domain.Condition{}, CreatedAt: item.CreationTimestamp.Time,
	}
	if item.Spec.MinAvailable != nil {
		summary.MinAvailable = item.Spec.MinAvailable.String()
	}
	if item.Spec.MaxUnavailable != nil {
		summary.MaxUnavailable = item.Spec.MaxUnavailable.String()
	}
	if item.Spec.Selector != nil {
		summary.Selector = item.Spec.Selector.MatchLabels
	}
	if summary.CurrentHealthy < summary.DesiredHealthy {
		summary.Status = "Blocked"
	} else if summary.DisruptionsAllowed == 0 {
		summary.Status = "Protected"
	} else {
		summary.Status = "Ready"
	}
	for _, condition := range item.Status.Conditions {
		summary.Conditions = append(summary.Conditions, domain.Condition{
			Type: condition.Type, Status: string(condition.Status), Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	return summary
}
