package kube

import (
	"context"
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

var verticalPodAutoscalerGVR = schema.GroupVersionResource{
	Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers",
}

// ValidateVPAQuantities parses and compares resource quantities with the exact
// Kubernetes grammar. Invalid input is rejected before an API Server dry-run,
// while the application layer remains independent of Kubernetes libraries.
func (c *Client) ValidateVPAQuantities(spec domain.VerticalAutoscalerSpec) error {
	for _, policy := range spec.ContainerPolicies {
		for field, values := range map[string]map[string]string{"minAllowed": policy.MinAllowed, "maxAllowed": policy.MaxAllowed} {
			for name, raw := range values {
				quantity, err := resource.ParseQuantity(raw)
				if err != nil || quantity.Sign() < 0 {
					return &domain.ValidationError{Field: "containerPolicies", Message: fmt.Sprintf("%s.%s must be a non-negative Kubernetes quantity", field, name)}
				}
				if field == "maxAllowed" {
					if minRaw, ok := policy.MinAllowed[name]; ok {
						minimum, minErr := resource.ParseQuantity(minRaw)
						if minErr == nil && minimum.Cmp(quantity) > 0 {
							return &domain.ValidationError{Field: "containerPolicies", Message: fmt.Sprintf("minAllowed for %s must not exceed maxAllowed", name)}
						}
					}
				}
			}
		}
	}
	return nil
}

func (c *Client) ListVerticalAutoscalers(ctx context.Context, namespace string) ([]domain.VerticalAutoscalerSummary, error) {
	if c.dynamic == nil {
		return nil, fmt.Errorf("%w: dynamic Kubernetes client is unavailable", domain.ErrUnavailable)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.dynamic.Resource(verticalPodAutoscalerGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, vpaCRDUnavailable(err)
		}
		return nil, mapKubeError(err)
	}
	out := make([]domain.VerticalAutoscalerSummary, 0, len(items.Items))
	for index := range items.Items {
		out = append(out, verticalAutoscalerSummary(&items.Items[index]))
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Namespace == out[right].Namespace {
			return out[left].Name < out[right].Name
		}
		return out[left].Namespace < out[right].Namespace
	})
	return out, nil
}

func (c *Client) UpsertVerticalAutoscaler(ctx context.Context, spec domain.VerticalAutoscalerSpec, dryRun bool) (domain.VerticalAutoscalerSummary, error) {
	if c.dynamic == nil {
		return domain.VerticalAutoscalerSummary{}, fmt.Errorf("%w: dynamic Kubernetes client is unavailable", domain.ErrUnavailable)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resource := c.dynamic.Resource(verticalPodAutoscalerGVR).Namespace(spec.Namespace)
	desired := verticalAutoscalerObject(spec)
	if spec.ResourceVersion == "" {
		options := metav1.CreateOptions{FieldManager: "dosh"}
		if dryRun {
			options.DryRun = []string{metav1.DryRunAll}
		}
		created, err := resource.Create(ctx, desired, options)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return domain.VerticalAutoscalerSummary{}, vpaCRDUnavailable(err)
			}
			return domain.VerticalAutoscalerSummary{}, mapKubeError(err)
		}
		return verticalAutoscalerSummary(created), nil
	}

	current, err := resource.Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) && !dryRun {
			return domain.VerticalAutoscalerSummary{}, fmt.Errorf("%w: VerticalPodAutoscaler %s/%s", domain.ErrNotFound, spec.Namespace, spec.Name)
		}
		return domain.VerticalAutoscalerSummary{}, mapKubeError(err)
	}
	if current.GetResourceVersion() != spec.ResourceVersion {
		return domain.VerticalAutoscalerSummary{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, spec.ResourceVersion, current.GetResourceVersion())
	}
	current.Object["spec"] = desired.Object["spec"]
	if current.GetLabels() == nil {
		current.SetLabels(map[string]string{})
	}
	labels := current.GetLabels()
	labels[managedByLabel] = "dosh"
	current.SetLabels(labels)
	options := metav1.UpdateOptions{FieldManager: "dosh"}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	updated, err := resource.Update(ctx, current, options)
	if err != nil {
		return domain.VerticalAutoscalerSummary{}, mapKubeError(err)
	}
	return verticalAutoscalerSummary(updated), nil
}

func (c *Client) DeleteVerticalAutoscaler(ctx context.Context, namespace, name, resourceVersion string) error {
	if c.dynamic == nil {
		return fmt.Errorf("%w: dynamic Kubernetes client is unavailable", domain.ErrUnavailable)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	err := c.dynamic.Resource(verticalPodAutoscalerGVR).Namespace(namespace).Delete(ctx, name, options)
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%w: VerticalPodAutoscaler %s/%s", domain.ErrNotFound, namespace, name)
	}
	return mapKubeError(err)
}

func verticalAutoscalerObject(spec domain.VerticalAutoscalerSpec) *unstructured.Unstructured {
	targetAPIVersion := strings.TrimSpace(spec.TargetAPIVersion)
	if targetAPIVersion == "" {
		targetAPIVersion = "apps/v1"
	}
	updateMode := strings.TrimSpace(spec.UpdatePolicy)
	if updateMode == "" {
		updateMode = "Off"
	}
	vpaSpec := map[string]any{
		"targetRef": map[string]any{
			"apiVersion": targetAPIVersion, "kind": spec.TargetKind, "name": spec.TargetName,
		},
		"updatePolicy": map[string]any{"updateMode": updateMode},
	}
	if spec.MinReplicas != nil {
		vpaSpec["minReplicas"] = int64(*spec.MinReplicas)
	}
	if len(spec.ContainerPolicies) > 0 {
		policies := make([]any, 0, len(spec.ContainerPolicies))
		for _, policy := range spec.ContainerPolicies {
			item := map[string]any{"containerName": policy.ContainerName}
			if policy.Mode != "" {
				item["mode"] = policy.Mode
			}
			if len(policy.MinAllowed) > 0 {
				item["minAllowed"] = quantityMapToAny(policy.MinAllowed)
			}
			if len(policy.MaxAllowed) > 0 {
				item["maxAllowed"] = quantityMapToAny(policy.MaxAllowed)
			}
			if policy.ControlledValues != "" {
				item["controlledValues"] = policy.ControlledValues
			}
			if len(policy.ControlledResources) > 0 {
				resources := make([]any, 0, len(policy.ControlledResources))
				for _, value := range policy.ControlledResources {
					resources = append(resources, value)
				}
				item["controlledResources"] = resources
			}
			policies = append(policies, item)
		}
		vpaSpec["resourcePolicy"] = map[string]any{"containerPolicies": policies}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.k8s.io/v1", "kind": "VerticalPodAutoscaler",
		"metadata": map[string]any{
			"name": spec.Name, "namespace": spec.Namespace, "labels": map[string]any{managedByLabel: "dosh"},
		},
		"spec": vpaSpec,
	}}
}

func quantityMapToAny(values map[string]string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func verticalAutoscalerSummary(item *unstructured.Unstructured) domain.VerticalAutoscalerSummary {
	spec, _ := item.Object["spec"].(map[string]any)
	status, _ := item.Object["status"].(map[string]any)
	target, _ := spec["targetRef"].(map[string]any)
	updatePolicy, _ := spec["updatePolicy"].(map[string]any)
	summary := domain.VerticalAutoscalerSummary{
		Name: item.GetName(), Namespace: item.GetNamespace(), ResourceVersion: item.GetResourceVersion(),
		TargetAPIVersion: vpaStringValue(target, "apiVersion"), TargetKind: vpaStringValue(target, "kind"),
		TargetName: vpaStringValue(target, "name"), UpdatePolicy: vpaStringValue(updatePolicy, "updateMode"),
		ContainerPolicies: []domain.VPAContainerPolicy{}, Recommendation: []domain.VPARecommendation{},
		Conditions: []domain.Condition{}, CreatedAt: item.GetCreationTimestamp().Time,
	}
	if summary.TargetAPIVersion == "" {
		summary.TargetAPIVersion = "apps/v1"
	}
	if summary.UpdatePolicy == "" {
		summary.UpdatePolicy = "Off"
	}
	if value, ok := vpaIntValue(spec["minReplicas"]); ok {
		minReplicas := int32(value)
		summary.MinReplicas = &minReplicas
	}
	if resourcePolicy, ok := spec["resourcePolicy"].(map[string]any); ok {
		if policies, ok := resourcePolicy["containerPolicies"].([]any); ok {
			for _, raw := range policies {
				policy, _ := raw.(map[string]any)
				if policy == nil {
					continue
				}
				summary.ContainerPolicies = append(summary.ContainerPolicies, domain.VPAContainerPolicy{
					ContainerName:       vpaStringValue(policy, "containerName"),
					Mode:                vpaStringValue(policy, "mode"),
					MinAllowed:          vpaStringMap(policy["minAllowed"]),
					MaxAllowed:          vpaStringMap(policy["maxAllowed"]),
					ControlledValues:    vpaStringValue(policy, "controlledValues"),
					ControlledResources: vpaStringList(policy["controlledResources"]),
				})
			}
		}
	}
	if conditions, ok := status["conditions"].([]any); ok {
		for _, raw := range conditions {
			condition, _ := raw.(map[string]any)
			if condition == nil {
				continue
			}
			summary.Conditions = append(summary.Conditions, domain.Condition{
				Type: vpaStringValue(condition, "type"), Status: vpaStringValue(condition, "status"),
				Reason: vpaStringValue(condition, "reason"), Message: vpaStringValue(condition, "message"),
				LastTransitionTime: vpaTimeValue(condition["lastTransitionTime"]),
			})
		}
	}
	if recommendation, ok := status["recommendation"].(map[string]any); ok {
		if recommendations, ok := recommendation["containerRecommendations"].([]any); ok {
			for _, raw := range recommendations {
				entry, _ := raw.(map[string]any)
				if entry == nil {
					continue
				}
				summary.Recommendation = append(summary.Recommendation, domain.VPARecommendation{
					ContainerName: vpaStringValue(entry, "containerName"), LowerBound: vpaStringMap(entry["lowerBound"]),
					Target: vpaStringMap(entry["target"]), UpperBound: vpaStringMap(entry["upperBound"]),
					UncappedTarget: vpaStringMap(entry["uncappedTarget"]),
				})
			}
		}
	}
	summary.Status, summary.Message = verticalAutoscalerStatus(summary)
	for _, condition := range summary.Conditions {
		if condition.Type == "RecommendationProvided" && condition.Status == "True" && !condition.LastTransitionTime.IsZero() {
			age := condition.LastTransitionTime
			summary.RecommendationAge = &age
			break
		}
	}
	if len(summary.Recommendation) > 0 && summary.Status == "Pending" {
		summary.Status = "Ready"
	}
	return summary
}

func verticalAutoscalerStatus(summary domain.VerticalAutoscalerSummary) (string, string) {
	for _, condition := range summary.Conditions {
		if condition.Status == "False" && (condition.Type == "RecommendationProvided" || condition.Type == "NoPodsMatched" || condition.Type == "FetchingHistory") {
			message := strings.TrimSpace(condition.Message)
			if message == "" {
				message = strings.TrimSpace(condition.Reason)
			}
			if condition.Type == "FetchingHistory" && condition.Reason == "Loading" {
				if message == "" {
					message = "VPA 正在聚合历史资源用量"
				}
				return "Pending", message
			}
			if condition.Type == "RecommendationProvided" {
				if message == "" {
					message = "VPA 无法生成资源建议"
				}
				return "Degraded", message
			}
		}
	}
	if len(summary.Recommendation) == 0 {
		return "Pending", "等待 VPA 生成资源建议"
	}
	return "Ready", "VPA 已生成资源建议"
}

func vpaStringValue(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func vpaIntValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}

func vpaStringMap(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	result := make(map[string]string, len(raw))
	for key, item := range raw {
		if text, ok := item.(string); ok {
			result[key] = text
		}
	}
	return result
}

func vpaStringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func vpaTimeValue(value any) time.Time {
	text, _ := value.(string)
	if text == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, text)
	return parsed
}

func vpaCRDUnavailable(err error) error {
	return domain.WithPublicMessage(
		fmt.Errorf("%w: autoscaling.k8s.io/v1 VerticalPodAutoscaler CRD is not installed: %v", domain.ErrUnsupported, err),
		"集群未安装 autoscaling.k8s.io/v1 VerticalPodAutoscaler CRD，请先安装 VPA 控制器与 CRD",
	)
}
