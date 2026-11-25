package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

const changeCauseAnnotation = "kubernetes.io/change-cause"

// GetWorkloadPodTemplate returns only the workload's PodTemplateSpec. Workload
// level fields such as replicas, selectors and rollout strategy are deliberately
// excluded so a template edit cannot overwrite unrelated concurrent changes.
func (c *Client) GetWorkloadPodTemplate(ctx context.Context, kind, namespace, name string) (domain.WorkloadPodTemplate, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	template, resourceVersion, selector, editable, restrictions, err := c.readWorkloadPodTemplate(ctx, kind, namespace, name)
	if err != nil {
		return domain.WorkloadPodTemplate{}, err
	}
	document := podTemplateDocument(template)
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return domain.WorkloadPodTemplate{}, fmt.Errorf("marshal pod template yaml: %w", err)
	}
	protected := make([]string, 0, len(selector))
	for key := range selector {
		protected = append(protected, key)
	}
	sort.Strings(protected)
	return domain.WorkloadPodTemplate{
		Kind: kindDisplayName(kind), Namespace: namespace, Name: name,
		ResourceVersion: resourceVersion, Template: document, YAML: string(encoded),
		Selector: selector, ProtectedLabels: protected, Editable: editable,
		Restrictions: restrictions, Warnings: podTemplateWarnings(template),
	}, nil
}

// UpdateWorkloadPodTemplate performs a targeted replacement of only the
// workload's PodTemplateSpec. A dry-run is always executed before the real
// write; both operations include the caller-supplied resourceVersion.
func (c *Client) UpdateWorkloadPodTemplate(ctx context.Context, kind, namespace, name string, update domain.WorkloadPodTemplateUpdate) (domain.WorkloadPodTemplateResult, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if !podTemplateEditable(kind) {
		return domain.WorkloadPodTemplateResult{}, fmt.Errorf("%w: Kubernetes Job Pod templates are immutable", domain.ErrUnsupported)
	}
	current, resourceVersion, selector, _, restrictions, err := c.readWorkloadPodTemplate(ctx, kind, namespace, name)
	if err != nil {
		return domain.WorkloadPodTemplateResult{}, err
	}
	if strings.TrimSpace(update.ResourceVersion) == "" {
		return domain.WorkloadPodTemplateResult{}, fmt.Errorf("%w: resourceVersion is required", domain.ErrInvalid)
	}
	if update.ResourceVersion != resourceVersion {
		return domain.WorkloadPodTemplateResult{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, resourceVersion)
	}
	if err := validateTemplateSelector(current, selector, update.Template); err != nil {
		return domain.WorkloadPodTemplateResult{}, err
	}
	desired, err := decodePodTemplateDocument(update.Template)
	if err != nil {
		return domain.WorkloadPodTemplateResult{}, err
	}
	if len(desired.Spec.Containers) == 0 {
		return domain.WorkloadPodTemplateResult{}, fmt.Errorf("%w: pod template requires at least one container", domain.ErrInvalid)
	}
	before := podTemplateDocument(current)
	afterWithoutCause := podTemplateDocument(desired)
	changed := !reflect.DeepEqual(before, afterWithoutCause)
	if changed && strings.TrimSpace(update.ChangeCause) != "" {
		if desired.Annotations == nil {
			desired.Annotations = map[string]string{}
		}
		desired.Annotations[changeCauseAnnotation] = strings.TrimSpace(update.ChangeCause)
	}
	beforeWarnings := podTemplateWarnings(current)
	warnings := podTemplateWarnings(desired)
	newWarnings := differenceStrings(warnings, beforeWarnings)
	if len(newWarnings) > 0 && !update.AcknowledgeRisks {
		return domain.WorkloadPodTemplateResult{}, fmt.Errorf("%w: high-risk pod template changes require acknowledgement", domain.ErrInvalid)
	}
	result := domain.WorkloadPodTemplateResult{
		WorkloadPodTemplate: domain.WorkloadPodTemplate{
			Kind: kindDisplayName(kind), Namespace: namespace, Name: name,
			ResourceVersion: resourceVersion, Selector: selector, Editable: true,
			Restrictions: restrictions, Warnings: warnings,
		},
		Changed: changed, Diff: diffPodTemplateDocuments(before, podTemplateDocument(desired)),
		Rollout: podTemplateRolloutEffect(kind, changed), NewWarnings: newWarnings,
	}
	if !changed {
		result.Template = before
		result.YAML = marshalPodTemplateYAML(before)
		return result, nil
	}
	if err := c.commitWorkloadPodTemplate(ctx, kind, namespace, name, desired, update.ResourceVersion, true); err != nil {
		return domain.WorkloadPodTemplateResult{}, err
	}
	if update.DryRun {
		result.ResourceVersion = resourceVersion
		result.Template = podTemplateDocument(desired)
		result.YAML = marshalPodTemplateYAML(result.Template)
		return result, nil
	}
	committed, nextVersion, err := c.commitWorkloadPodTemplateAndRead(ctx, kind, namespace, name, desired, update.ResourceVersion, false)
	if err != nil {
		return domain.WorkloadPodTemplateResult{}, err
	}
	result.ResourceVersion = nextVersion
	result.Template = podTemplateDocument(committed)
	result.YAML = marshalPodTemplateYAML(result.Template)
	result.Diff = diffPodTemplateDocuments(before, result.Template)
	return result, nil
}

func (c *Client) readWorkloadPodTemplate(ctx context.Context, kind, namespace, name string) (corev1.PodTemplateSpec, string, map[string]string, bool, []string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "deployment":
		item, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", nil, true, nil, mapKubeError(err)
		}
		return item.Spec.Template, item.ResourceVersion, selectorLabels(item.Spec.Selector), true, nil, nil
	case "statefulset":
		item, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", nil, true, nil, mapKubeError(err)
		}
		return item.Spec.Template, item.ResourceVersion, selectorLabels(item.Spec.Selector), true, nil, nil
	case "daemonset":
		item, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", nil, true, nil, mapKubeError(err)
		}
		return item.Spec.Template, item.ResourceVersion, selectorLabels(item.Spec.Selector), true, nil, nil
	case "cronjob":
		item, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", nil, true, nil, mapKubeError(err)
		}
		return item.Spec.JobTemplate.Spec.Template, item.ResourceVersion, nil, true, nil, nil
	case "job":
		item, err := c.clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", nil, false, nil, mapKubeError(err)
		}
		return item.Spec.Template, item.ResourceVersion, selectorLabels(item.Spec.Selector), false,
			[]string{"Kubernetes Job 的 Pod 模板创建后不可变，仅支持查看和运行时运维"}, nil
	default:
		return corev1.PodTemplateSpec{}, "", nil, false, nil, fmt.Errorf("%w: workload kind %q", domain.ErrUnsupported, kind)
	}
}

func (c *Client) commitWorkloadPodTemplate(ctx context.Context, kind, namespace, name string, template corev1.PodTemplateSpec, resourceVersion string, dryRun bool) error {
	_, _, err := c.commitWorkloadPodTemplateAndRead(ctx, kind, namespace, name, template, resourceVersion, dryRun)
	return err
}

func (c *Client) commitWorkloadPodTemplateAndRead(ctx context.Context, kind, namespace, name string, template corev1.PodTemplateSpec, resourceVersion string, dryRun bool) (corev1.PodTemplateSpec, string, error) {
	options := metav1.UpdateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment":
		item, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		item.ResourceVersion = resourceVersion
		item.Spec.Template = template
		updated, err := c.clientset.AppsV1().Deployments(namespace).Update(ctx, item, options)
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		return updated.Spec.Template, updated.ResourceVersion, nil
	case "statefulset":
		item, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		item.ResourceVersion = resourceVersion
		item.Spec.Template = template
		updated, err := c.clientset.AppsV1().StatefulSets(namespace).Update(ctx, item, options)
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		return updated.Spec.Template, updated.ResourceVersion, nil
	case "daemonset":
		item, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		item.ResourceVersion = resourceVersion
		item.Spec.Template = template
		updated, err := c.clientset.AppsV1().DaemonSets(namespace).Update(ctx, item, options)
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		return updated.Spec.Template, updated.ResourceVersion, nil
	case "cronjob":
		item, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		item.ResourceVersion = resourceVersion
		item.Spec.JobTemplate.Spec.Template = template
		updated, err := c.clientset.BatchV1().CronJobs(namespace).Update(ctx, item, options)
		if err != nil {
			return corev1.PodTemplateSpec{}, "", mapKubeError(err)
		}
		return updated.Spec.JobTemplate.Spec.Template, updated.ResourceVersion, nil
	default:
		return corev1.PodTemplateSpec{}, "", fmt.Errorf("%w: pod template editing is not supported for %s", domain.ErrUnsupported, kind)
	}
}

func podTemplateEditable(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment", "statefulset", "daemonset", "cronjob":
		return true
	default:
		return false
	}
}

func selectorLabels(selector *metav1.LabelSelector) map[string]string {
	if selector == nil || len(selector.MatchLabels) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(selector.MatchLabels))
	for key, value := range selector.MatchLabels {
		result[key] = value
	}
	return result
}

func validateTemplateSelector(current corev1.PodTemplateSpec, selector map[string]string, document map[string]any) error {
	desired, err := decodePodTemplateDocument(document)
	if err != nil {
		return err
	}
	for key, value := range selector {
		if desired.Labels[key] != value {
			return fmt.Errorf("%w: pod template label %q must remain %q to match the workload selector", domain.ErrInvalid, key, value)
		}
	}
	_ = current
	return nil
}

func podTemplateDocument(template corev1.PodTemplateSpec) map[string]any {
	metadata := map[string]any{}
	if len(template.Labels) > 0 {
		metadata["labels"] = stringMapAny(template.Labels)
	}
	if len(template.Annotations) > 0 {
		metadata["annotations"] = stringMapAny(template.Annotations)
	}
	spec := map[string]any{}
	encoded, err := json.Marshal(template.Spec)
	if err == nil {
		_ = json.Unmarshal(encoded, &spec)
	}
	return map[string]any{"metadata": metadata, "spec": spec}
}

func decodePodTemplateDocument(document map[string]any) (corev1.PodTemplateSpec, error) {
	if document == nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("%w: template is required", domain.ErrInvalid)
	}
	for key := range document {
		if key != "metadata" && key != "spec" {
			return corev1.PodTemplateSpec{}, fmt.Errorf("%w: template.%s is not editable", domain.ErrInvalid, key)
		}
	}
	metadata, _ := document["metadata"].(map[string]any)
	for key := range metadata {
		if key != "labels" && key != "annotations" {
			return corev1.PodTemplateSpec{}, fmt.Errorf("%w: template.metadata.%s is not editable", domain.ErrInvalid, key)
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("%w: encode pod template: %v", domain.ErrInvalid, err)
	}
	var template corev1.PodTemplateSpec
	if err := json.Unmarshal(encoded, &template); err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("%w: decode pod template: %v", domain.ErrInvalid, err)
	}
	seen := map[string]string{}
	for _, container := range append(append([]corev1.Container{}, template.Spec.Containers...), template.Spec.InitContainers...) {
		if _, exists := seen[container.Name]; exists {
			return corev1.PodTemplateSpec{}, fmt.Errorf("%w: container name %q must be unique", domain.ErrInvalid, container.Name)
		}
		seen[container.Name] = container.Name
	}
	return template, nil
}

func marshalPodTemplateYAML(document map[string]any) string {
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func stringMapAny(values map[string]string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func diffPodTemplateDocuments(before, after map[string]any) []domain.WorkloadTemplateDiff {
	diffs := make([]domain.WorkloadTemplateDiff, 0)
	diffValue("", before, after, &diffs)
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs
}

func diffValue(path string, before, after any, diffs *[]domain.WorkloadTemplateDiff) {
	beforeMap, beforeOK := before.(map[string]any)
	afterMap, afterOK := after.(map[string]any)
	if beforeOK && afterOK {
		keys := map[string]struct{}{}
		for key := range beforeMap {
			keys[key] = struct{}{}
		}
		for key := range afterMap {
			keys[key] = struct{}{}
		}
		for key := range keys {
			diffValue(joinJSONPath(path, key), beforeMap[key], afterMap[key], diffs)
		}
		return
	}
	beforeSlice, beforeSliceOK := before.([]any)
	afterSlice, afterSliceOK := after.([]any)
	if beforeSliceOK && afterSliceOK {
		max := len(beforeSlice)
		if len(afterSlice) > max {
			max = len(afterSlice)
		}
		for index := 0; index < max; index++ {
			var left, right any
			if index < len(beforeSlice) {
				left = beforeSlice[index]
			}
			if index < len(afterSlice) {
				right = afterSlice[index]
			}
			diffValue(fmt.Sprintf("%s[%d]", path, index), left, right, diffs)
		}
		return
	}
	if !reflect.DeepEqual(before, after) {
		*diffs = append(*diffs, domain.WorkloadTemplateDiff{Path: path, Before: before, After: after})
	}
}

func joinJSONPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func podTemplateWarnings(template corev1.PodTemplateSpec) []string {
	warnings := make([]string, 0)
	if template.Spec.HostNetwork {
		warnings = append(warnings, "spec.hostNetwork=true 将共享节点网络")
	}
	if template.Spec.HostPID {
		warnings = append(warnings, "spec.hostPID=true 将共享节点进程命名空间")
	}
	if template.Spec.HostIPC {
		warnings = append(warnings, "spec.hostIPC=true 将共享节点 IPC 命名空间")
	}
	for _, volume := range template.Spec.Volumes {
		if volume.HostPath != nil {
			warnings = append(warnings, fmt.Sprintf("卷 %s 使用 hostPath=%s", volume.Name, volume.HostPath.Path))
		}
	}
	for _, container := range append(append([]corev1.Container{}, template.Spec.InitContainers...), template.Spec.Containers...) {
		for _, port := range container.Ports {
			if port.HostPort != 0 {
				warnings = append(warnings, fmt.Sprintf("容器 %s 使用 hostPort=%d", container.Name, port.HostPort))
			}
		}
		security := container.SecurityContext
		if security == nil {
			continue
		}
		if security.Privileged != nil && *security.Privileged {
			warnings = append(warnings, fmt.Sprintf("容器 %s 以 privileged 模式运行", container.Name))
		}
		if security.RunAsUser != nil && *security.RunAsUser == 0 {
			warnings = append(warnings, fmt.Sprintf("容器 %s 以 root 用户运行", container.Name))
		}
		if security.AllowPrivilegeEscalation != nil && *security.AllowPrivilegeEscalation {
			warnings = append(warnings, fmt.Sprintf("容器 %s 允许权限提升", container.Name))
		}
		if security.Capabilities != nil && len(security.Capabilities.Add) > 0 {
			capabilities := make([]string, 0, len(security.Capabilities.Add))
			for _, capability := range security.Capabilities.Add {
				capabilities = append(capabilities, string(capability))
			}
			warnings = append(warnings, fmt.Sprintf("容器 %s 新增 Linux capabilities：%s", container.Name, strings.Join(capabilities, ", ")))
		}
	}
	sort.Strings(warnings)
	return warnings
}

func differenceStrings(values, existing []string) []string {
	seen := make(map[string]struct{}, len(existing))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		result = append(result, value)
	}
	return result
}

func podTemplateRolloutEffect(kind string, changed bool) domain.WorkloadTemplateRollout {
	if !changed {
		return domain.WorkloadTemplateRollout{Effect: "none", Message: "模板没有实际变化"}
	}
	switch strings.ToLower(kind) {
	case "cronjob":
		return domain.WorkloadTemplateRollout{Effect: "futureJobs", Message: "模板已保存，仅影响后续创建的 Job，正在运行和已完成的 Job 不受影响"}
	case "statefulset":
		return domain.WorkloadTemplateRollout{Effect: "rollingUpdate", Message: "模板已保存，将创建新的 ControllerRevision，并按 Partition 和更新策略滚动更新 Pod"}
	case "daemonset":
		return domain.WorkloadTemplateRollout{Effect: "rollingUpdate", Message: "模板已保存，将按当前更新策略滚动更新节点上的 Pod"}
	default:
		return domain.WorkloadTemplateRollout{Effect: "rollingUpdate", Message: "模板已保存，将创建新的 ReplicaSet 并触发滚动发布"}
	}
}

func kindDisplayName(kind string) string {
	switch strings.ToLower(kind) {
	case "deployment":
		return "Deployment"
	case "statefulset":
		return "StatefulSet"
	case "daemonset":
		return "DaemonSet"
	case "job":
		return "Job"
	case "cronjob":
		return "CronJob"
	default:
		return kind
	}
}

type containerMetricUsage struct {
	cpuMilli int64
	memory   int64
}

func (c *Client) GetWorkloadContainerInventory(ctx context.Context, kind, namespace, name string) (domain.WorkloadContainerInventory, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	template, resourceVersion, selector, editable, restrictions, err := c.readWorkloadPodTemplate(ctx, kind, namespace, name)
	if err != nil {
		return domain.WorkloadContainerInventory{}, err
	}
	pods, warnings, err := c.inventoryPods(ctx, kind, namespace, name, selector)
	if err != nil {
		return domain.WorkloadContainerInventory{}, err
	}
	metrics, metricsAvailable := c.containerMetricsByPod(ctx, namespace)
	regular := workloadContainers(template.Spec.Containers)
	initContainers := workloadContainers(template.Spec.InitContainers)
	result := domain.WorkloadContainerInventory{
		Kind: kindDisplayName(kind), Namespace: namespace, Name: name,
		ResourceVersion: resourceVersion, Selector: selector,
		Containers:       make([]domain.WorkloadContainerState, 0, len(regular)+len(initContainers)),
		TemplateEditable: editable, MetricsAvailable: metricsAvailable,
		Warnings:    append(append([]string{}, restrictions...), append(podTemplateWarnings(template), warnings...)...),
		RefreshedAt: time.Now().UTC(),
	}
	for _, container := range initContainers {
		result.Containers = append(result.Containers, buildContainerState(container, "initContainer", true, template.Spec.InitContainers, pods, metrics))
	}
	for _, container := range regular {
		result.Containers = append(result.Containers, buildContainerState(container, "container", false, template.Spec.Containers, pods, metrics))
	}
	return result, nil
}

type inventoryPod struct {
	pod *corev1.Pod
	job string
}

func (c *Client) inventoryPods(ctx context.Context, kind, namespace, name string, selector map[string]string) ([]inventoryPod, []string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "cronjob" {
		return c.cronJobInventoryPods(ctx, namespace, name)
	}
	items, err := c.podsByTemplateSelector(ctx, namespace, selector)
	if err != nil {
		return nil, nil, err
	}
	return items, nil, nil
}

func (c *Client) podsByTemplateSelector(ctx context.Context, namespace string, selector map[string]string) ([]inventoryPod, error) {
	if len(selector) == 0 {
		return []inventoryPod{}, nil
	}
	options := metav1.ListOptions{LabelSelector: labels.SelectorFromSet(selector).String()}
	items, err := c.clientset.CoreV1().Pods(namespace).List(ctx, options)
	if err != nil {
		return nil, mapKubeError(err)
	}
	result := make([]inventoryPod, 0, len(items.Items))
	for index := range items.Items {
		result = append(result, inventoryPod{pod: &items.Items[index]})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].pod.Name < result[j].pod.Name })
	return result, nil
}

func (c *Client) cronJobInventoryPods(ctx context.Context, namespace, name string) ([]inventoryPod, []string, error) {
	cronJob, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, mapKubeError(err)
	}
	result := make([]inventoryPod, 0)
	warnings := []string{"CronJob 仅展示当前活跃 Job 的容器运行实例；历史 Job 请从 Job 资源查看"}
	for _, reference := range cronJob.Status.Active {
		jobNamespace := reference.Namespace
		if jobNamespace == "" {
			jobNamespace = namespace
		}
		job, err := c.clientset.BatchV1().Jobs(jobNamespace).Get(ctx, reference.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, nil, mapKubeError(err)
		}
		selector := selectorLabels(job.Spec.Selector)
		items, err := c.podsByTemplateSelector(ctx, job.Namespace, selector)
		if err != nil {
			return nil, nil, err
		}
		for index := range items {
			items[index].job = job.Name
		}
		result = append(result, items...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].job != result[j].job {
			return result[i].job < result[j].job
		}
		return result[i].pod.Name < result[j].pod.Name
	})
	return result, warnings, nil
}

func (c *Client) containerMetricsByPod(ctx context.Context, namespace string) (map[string]map[string]containerMetricUsage, bool) {
	result := make(map[string]map[string]containerMetricUsage)
	if c.metrics == nil || !c.MetricsAvailable(ctx) {
		return result, false
	}
	items, err := c.metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return result, false
	}
	for _, item := range items.Items {
		byContainer := make(map[string]containerMetricUsage, len(item.Containers))
		for _, container := range item.Containers {
			usage := containerMetricUsage{}
			if quantity, ok := container.Usage[corev1.ResourceCPU]; ok {
				usage.cpuMilli = quantity.MilliValue()
			}
			if quantity, ok := container.Usage[corev1.ResourceMemory]; ok {
				usage.memory = quantity.Value()
			}
			byContainer[container.Name] = usage
		}
		result[item.Name] = byContainer
	}
	return result, true
}

func buildContainerState(config domain.WorkloadContainer, containerType string, init bool, specs []corev1.Container, pods []inventoryPod, metrics map[string]map[string]containerMetricUsage) domain.WorkloadContainerState {
	state := domain.WorkloadContainerState{
		WorkloadContainer: config, Type: containerType, Init: init,
		Instances: make([]domain.WorkloadContainerInstance, 0),
	}
	var sidecar bool
	for index := range specs {
		if specs[index].Name != config.Name {
			continue
		}
		if specs[index].RestartPolicy != nil && *specs[index].RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sidecar = true
		}
		break
	}
	state.Sidecar = sidecar
	if sidecar {
		state.Type = "sidecar"
	}
	for _, item := range pods {
		pod := item.pod
		instance, exists := containerInstanceFromPod(pod, config.Name, init, item.job)
		if !exists {
			continue
		}
		if usage, ok := metrics[pod.Name][config.Name]; ok {
			instance.MetricsAvailable = true
			instance.CPUUsageMilli = usage.cpuMilli
			instance.MemoryUsageBytes = usage.memory
		}
		state.Instances = append(state.Instances, instance)
		state.Runtime.Pods++
		state.Runtime.Restarts += instance.RestartCount
		if instance.State == "Running" {
			state.Runtime.Running++
		}
		if instance.State == "Terminated" {
			state.Runtime.Terminated++
		}
		if instance.Ready {
			state.Runtime.Ready++
		}
		if instance.MetricsAvailable {
			state.Runtime.MetricsAvailable = true
			state.Runtime.CPUUsageMilli += instance.CPUUsageMilli
			state.Runtime.MemoryUsageBytes += instance.MemoryUsageBytes
		}
	}
	return state
}

func containerInstanceFromPod(pod *corev1.Pod, name string, init bool, job string) (domain.WorkloadContainerInstance, bool) {
	specs := pod.Spec.Containers
	statuses := pod.Status.ContainerStatuses
	if init {
		specs = pod.Spec.InitContainers
		statuses = pod.Status.InitContainerStatuses
	}
	image := ""
	for _, spec := range specs {
		if spec.Name == name {
			image = spec.Image
			break
		}
	}
	if image == "" {
		return domain.WorkloadContainerInstance{}, false
	}
	instance := domain.WorkloadContainerInstance{Pod: pod.Name, Namespace: pod.Namespace, Node: pod.Spec.NodeName, Job: job, Image: image, State: "Pending"}
	for _, status := range statuses {
		if status.Name != name {
			continue
		}
		if status.Image != "" {
			instance.Image = status.Image
		}
		instance.Ready = status.Ready
		instance.RestartCount = status.RestartCount
		switch {
		case status.State.Running != nil:
			instance.State = "Running"
			started := status.State.Running.StartedAt.Time
			instance.StartedAt = &started
		case status.State.Waiting != nil:
			instance.State = "Waiting"
			instance.Reason = status.State.Waiting.Reason
			instance.Message = status.State.Waiting.Message
		case status.State.Terminated != nil:
			instance.State = "Terminated"
			instance.Reason = status.State.Terminated.Reason
			instance.Message = status.State.Terminated.Message
			started := status.State.Terminated.StartedAt.Time
			instance.StartedAt = &started
		}
		break
	}
	return instance, true
}

// DecodePodTemplateDocument is exported for focused tests and other adapters
// that need to validate a document without reaching Kubernetes.
func DecodePodTemplateDocument(document map[string]any) (map[string]any, error) {
	template, err := decodePodTemplateDocument(document)
	if err != nil {
		return nil, err
	}
	return podTemplateDocument(template), nil
}
