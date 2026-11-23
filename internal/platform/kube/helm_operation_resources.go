package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const helmResourceMessageLimit = 320

type helmResourceEvents struct {
	latestWarning *corev1.Event
	latestNormal  *corev1.Event
}

// ObserveHelmOperationResources reads the live state of the resources in a
// reviewed Helm plan. It is intentionally best-effort: one unreadable object
// must not stop the operation record from showing progress for the others.
func (c *Client) ObserveHelmOperationResources(ctx context.Context, resources []domain.HelmChartPlanResource) []domain.HelmOperationResource {
	items := make([]domain.HelmOperationResource, len(resources))
	needsEvent := make([]bool, len(resources))
	uids := make([]string, len(resources))
	for index, resource := range resources {
		item := domain.HelmOperationResource{
			APIVersion: resource.APIVersion, Kind: resource.Kind, Namespace: resource.Namespace,
			Name: resource.Name, Hook: resource.Hook, CRD: resource.CRD, Action: resource.Action,
			APIGroup: resource.APIGroup, Resource: resource.Resource, Status: "pending",
		}
		if resource.Hook {
			item.Message = "等待 Helm 执行 Hook"
		} else if resource.Action == "update" {
			item.Message = "等待 Helm 更新资源"
		} else {
			item.Message = "等待 Helm 创建资源"
		}

		object, err := c.GetGenericResource(ctx, helmPlanDescriptor(resource), resource.Namespace, resource.Name)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			// Helm has not created this object yet.
		case err != nil:
			item.Status = "unknown"
			item.Message = "读取资源状态失败：" + truncateHelmMessage(err.Error())
		default:
			item.Status, item.Message = helmResourceObjectStatus(resource, object)
			needsEvent[index] = item.Status == "waiting" || item.Status == "failed"
			if metadata := objectMapLike(object, "metadata"); metadata != nil {
				uids[index], _ = metadata["uid"].(string)
			}
		}
		items[index] = item
	}

	messages := c.helmResourceEventMessages(ctx, resources, needsEvent, uids)
	for index := range items {
		key := helmResourceEventKey(items[index].Kind, items[index].Namespace, items[index].Name)
		message := ""
		if uids[index] != "" {
			message = messages["uid:"+uids[index]]
		}
		if message == "" {
			message = messages[key]
		}
		if message != "" {
			items[index].Message = message
		}
	}
	return items
}

func helmPlanDescriptor(resource domain.HelmChartPlanResource) domain.ResourceDescriptor {
	group := resource.APIGroup
	version := resource.APIVersion
	if parts := strings.SplitN(resource.APIVersion, "/", 2); len(parts) == 2 {
		group = parts[0]
		version = parts[1]
	}
	return domain.ResourceDescriptor{
		Group: group, Version: version, Resource: resource.Resource, Kind: resource.Kind,
		Namespaced: resource.Namespace != "",
	}
}

func (c *Client) helmResourceEventMessages(ctx context.Context, resources []domain.HelmChartPlanResource, needsEvent []bool, uids []string) map[string]string {
	result := map[string]string{}
	if c.clientset == nil {
		return result
	}
	namespaces := map[string]struct{}{}
	for index, resource := range resources {
		if resource.Namespace != "" && needsEvent[index] {
			namespaces[resource.Namespace] = struct{}{}
		}
	}
	for namespace := range namespaces {
		events, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: 500})
		if err != nil {
			continue
		}
		byUID := map[string]*helmResourceEvents{}
		byName := map[string]*helmResourceEvents{}
		for index := range events.Items {
			event := &events.Items[index]
			entry := byUID[string(event.InvolvedObject.UID)]
			if entry == nil {
				key := helmResourceEventKey(event.InvolvedObject.Kind, event.InvolvedObject.Namespace, event.InvolvedObject.Name)
				entry = byName[key]
				if entry == nil {
					entry = &helmResourceEvents{}
					byName[key] = entry
				}
				if event.InvolvedObject.UID != "" {
					byUID[string(event.InvolvedObject.UID)] = entry
				}
			}
			if event.Type == corev1.EventTypeWarning {
				if entry.latestWarning == nil || helmEventTime(event).After(helmEventTime(entry.latestWarning)) {
					entry.latestWarning = event
				}
			} else if entry.latestNormal == nil || helmEventTime(event).After(helmEventTime(entry.latestNormal)) {
				entry.latestNormal = event
			}
		}
		for index, resource := range resources {
			if resource.Namespace != namespace || !needsEvent[index] {
				continue
			}
			var entry *helmResourceEvents
			if uids[index] != "" {
				entry = byUID[uids[index]]
			}
			if entry == nil {
				entry = byName[helmResourceEventKey(resource.Kind, resource.Namespace, resource.Name)]
			}
			if entry == nil {
				continue
			}
			event := entry.latestWarning
			if event == nil {
				event = entry.latestNormal
			}
			if event != nil {
				message := helmEventMessage(event)
				key := helmResourceEventKey(resource.Kind, resource.Namespace, resource.Name)
				result[key] = message
				if uids[index] != "" {
					result["uid:"+uids[index]] = message
				}
			}
		}
	}
	return result
}

func helmEventTime(event *corev1.Event) time.Time {
	if event == nil {
		return time.Time{}
	}
	for _, value := range []time.Time{event.EventTime.Time, event.LastTimestamp.Time, event.CreationTimestamp.Time} {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func helmEventMessage(event *corev1.Event) string {
	message := strings.TrimSpace(event.Message)
	if message == "" {
		message = strings.TrimSpace(event.Reason)
	} else if event.Reason != "" && !strings.HasPrefix(message, event.Reason) {
		message = event.Reason + ": " + message
	}
	if event.Count > 1 {
		message = fmt.Sprintf("%s（重复 %d 次）", message, event.Count)
	}
	return truncateHelmMessage(message)
}

func helmResourceEventKey(kind, namespace, name string) string {
	return kind + "\x00" + namespace + "\x00" + name
}

func helmResourceObjectStatus(resource domain.HelmChartPlanResource, object map[string]any) (string, string) {
	statusObject := objectMapLike(object, "status")
	phase, _ := statusObject["phase"].(string)
	kind := strings.ToLower(resource.Kind)

	switch kind {
	case "persistentvolumeclaim":
		switch phase {
		case "Bound":
			return "ready", "PVC 已绑定"
		case "Lost":
			return "failed", "PVC 已丢失"
		default:
			return "waiting", "PVC 等待绑定"
		}
	case "pod":
		return helmPodStatus(object)
	case "deployment", "statefulset", "daemonset", "replicaset":
		return helmWorkloadStatus(object, kind)
	case "job":
		if helmInt(statusObject["failed"]) > 0 {
			return "failed", "Job 已失败"
		}
		if helmInt(statusObject["succeeded"]) > 0 {
			return "ready", "Job 已完成"
		}
		if active := helmInt(statusObject["active"]); active > 0 {
			return "waiting", fmt.Sprintf("Job 正在运行（%d 个活动 Pod）", active)
		}
		return "waiting", "等待 Job 启动"
	case "cronjob":
		return "created", "CronJob 已创建"
	case "customresourcedefinition":
		if message := helmConditionMessage(object, "Established", "NamesAccepted"); message != "" {
			if message == "condition-true" {
				return "ready", "CRD 已生效"
			}
			return "waiting", message
		}
		return "created", "CRD 已创建"
	case "namespace":
		if phase == "Active" {
			return "ready", "Namespace 已就绪"
		}
		if phase == "Terminating" {
			return "waiting", "Namespace 正在终止"
		}
	case "service":
		return "ready", "Service 已创建"
	}

	if condition := helmReadyConditionMessage(object); condition.message != "" {
		return condition.status, condition.message
	}
	if resource.Hook {
		return "created", "Hook 资源已创建"
	}
	return "created", "资源已创建"
}

func helmPodStatus(object map[string]any) (string, string) {
	statusObject := objectMapLike(object, "status")
	phase, _ := statusObject["phase"].(string)
	if issue := helmContainerIssue(object); issue != "" {
		lower := strings.ToLower(issue)
		if strings.Contains(lower, "crashloopbackoff") || strings.Contains(lower, "errimagepull") || strings.Contains(lower, "imagepullbackoff") || strings.Contains(lower, "createcontainererror") || strings.Contains(lower, "runcontainererror") {
			return "failed", issue
		}
		return "waiting", issue
	}
	switch phase {
	case "Running":
		if condition := helmConditionByType(object, "Ready"); condition != nil && condition["status"] == "True" {
			return "ready", "Pod 已就绪"
		}
		if message := helmConditionMessage(object, "Ready", "ContainersReady"); message != "" && message != "condition-true" {
			return "waiting", message
		}
		return "waiting", "Pod 正在启动"
	case "Pending":
		if message := helmConditionMessage(object, "PodScheduled"); message != "" && message != "condition-true" {
			return "waiting", message
		}
		return "waiting", "Pod 等待调度"
	case "Failed":
		return "failed", "Pod 已失败"
	case "Succeeded":
		return "ready", "Pod 已成功完成"
	default:
		return "waiting", "等待 Pod 启动"
	}
}

func helmContainerIssue(object map[string]any) string {
	statusObject := objectMapLike(object, "status")
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		statuses, _ := statusObject[field].([]any)
		for _, raw := range statuses {
			container, _ := raw.(map[string]any)
			name, _ := container["name"].(string)
			state := objectMapLike(container, "state")
			if waiting := objectMapLike(state, "waiting"); waiting != nil {
				reason, _ := waiting["reason"].(string)
				message, _ := waiting["message"].(string)
				if reason == "" {
					reason = "等待容器启动"
				}
				if name != "" {
					reason = name + ": " + reason
				}
				if message != "" {
					reason += " - " + message
				}
				return truncateHelmMessage(reason)
			}
			if terminated := objectMapLike(state, "terminated"); terminated != nil {
				reason, _ := terminated["reason"].(string)
				message, _ := terminated["message"].(string)
				if reason == "" || reason == "Completed" {
					continue
				}
				if name != "" {
					reason = name + ": " + reason
				}
				if message != "" {
					reason += " - " + message
				}
				return truncateHelmMessage(reason)
			}
		}
	}
	return ""
}

func helmWorkloadStatus(object map[string]any, kind string) (string, string) {
	spec := objectMapLike(object, "spec")
	statusObject := objectMapLike(object, "status")
	desired := helmInt(spec["replicas"])
	if desired == 0 && kind != "daemonset" {
		desired = 1
	}
	ready := helmInt(statusObject["readyReplicas"])
	if kind == "daemonset" {
		desired = helmInt(statusObject["desiredNumberScheduled"])
		ready = helmInt(statusObject["numberReady"])
	}
	generation := helmInt(objectMapLike(object, "metadata")["generation"])
	observed := helmInt(statusObject["observedGeneration"])
	if generation > 0 && observed > 0 && observed < generation {
		return "waiting", "等待控制器处理最新版本"
	}
	if kind == "statefulset" {
		current, _ := statusObject["currentRevision"].(string)
		update, _ := statusObject["updateRevision"].(string)
		if current != "" && update != "" && current != update {
			return "waiting", fmt.Sprintf("等待滚动更新完成（就绪 %d/%d）", ready, desired)
		}
	}
	if condition := helmConditionByType(object, "Progressing"); condition != nil && condition["status"] == "False" {
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		if reason == "ProgressDeadlineExceeded" {
			return "failed", joinHelmConditionMessage(reason, message)
		}
	}
	if desired > 0 && ready >= desired {
		return "ready", fmt.Sprintf("就绪 %d/%d", ready, desired)
	}
	if desired == 0 {
		return "ready", "期望副本数为 0"
	}
	return "waiting", fmt.Sprintf("就绪 %d/%d", ready, desired)
}

type helmConditionResult struct {
	status  string
	message string
}

func helmReadyConditionMessage(object map[string]any) helmConditionResult {
	for _, conditionType := range []string{"Ready", "Available", "Established", "Complete", "Succeeded"} {
		condition := helmConditionByType(object, conditionType)
		if condition == nil {
			continue
		}
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		if condition["status"] == "True" {
			return helmConditionResult{status: "ready", message: helmResourceConditionText(conditionType, reason, message, true)}
		}
		if condition["status"] == "False" {
			return helmConditionResult{status: "waiting", message: helmResourceConditionText(conditionType, reason, message, false)}
		}
	}
	return helmConditionResult{}
}

func helmConditionMessage(object map[string]any, types ...string) string {
	for _, conditionType := range types {
		condition := helmConditionByType(object, conditionType)
		if condition == nil {
			continue
		}
		if condition["status"] == "True" {
			return "condition-true"
		}
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		return helmResourceConditionText(conditionType, reason, message, false)
	}
	return ""
}

func helmResourceConditionText(conditionType, reason, message string, trueCondition bool) string {
	if message != "" {
		if reason != "" {
			return truncateHelmMessage(reason + ": " + message)
		}
		return truncateHelmMessage(message)
	}
	if reason != "" {
		return truncateHelmMessage(reason)
	}
	if trueCondition {
		return conditionType + " 已满足"
	}
	return conditionType + " 未满足"
}

func joinHelmConditionMessage(reason, message string) string {
	if reason == "" {
		return truncateHelmMessage(message)
	}
	if message == "" {
		return truncateHelmMessage(reason)
	}
	return truncateHelmMessage(reason + ": " + message)
}

func helmConditionByType(object map[string]any, conditionType string) map[string]any {
	statusObject := objectMapLike(object, "status")
	conditions, _ := statusObject["conditions"].([]any)
	for _, raw := range conditions {
		condition, _ := raw.(map[string]any)
		if condition["type"] == conditionType {
			return condition
		}
	}
	return nil
}

func helmInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func truncateHelmMessage(message string) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) <= helmResourceMessageLimit {
		return message
	}
	return string(runes[:helmResourceMessageLimit]) + "…"
}
