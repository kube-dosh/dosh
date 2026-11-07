package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Client) ListWorkloads(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error) {
	kind := strings.ToLower(query.Kind)
	if kind == "" {
		out := make([]domain.WorkloadSummary, 0)
		for _, k := range []string{"deployment", "statefulset", "daemonset", "job", "cronjob"} {
			q := query
			q.Kind = k
			items, err := c.ListWorkloads(ctx, q)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
		sortWorkloads(out)
		return out, nil
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	var out []domain.WorkloadSummary
	switch kind {
	case "deployment":
		items, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, mapKubeError(err)
		}
		for _, item := range items.Items {
			summary := deploymentSummary(item)
			if !matchesWorkloadQuery(summary, query) {
				continue
			}
			summary.Restarts = c.restartsForSelector(ctx, item.Namespace, item.Spec.Selector)
			out = append(out, summary)
		}
	case "statefulset":
		items, err := c.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, mapKubeError(err)
		}
		for _, item := range items.Items {
			summary := statefulSetSummary(item)
			if !matchesWorkloadQuery(summary, query) {
				continue
			}
			summary.Restarts = c.restartsForSelector(ctx, item.Namespace, item.Spec.Selector)
			out = append(out, summary)
		}
	case "daemonset":
		items, err := c.clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, mapKubeError(err)
		}
		for _, item := range items.Items {
			summary := daemonSetSummary(item)
			if !matchesWorkloadQuery(summary, query) {
				continue
			}
			summary.Restarts = c.restartsForSelector(ctx, item.Namespace, item.Spec.Selector)
			out = append(out, summary)
		}
	case "job":
		items, err := c.clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, mapKubeError(err)
		}
		for _, item := range items.Items {
			summary := jobSummary(item)
			if !matchesWorkloadQuery(summary, query) {
				continue
			}
			summary.Restarts = c.restartsForSelector(ctx, item.Namespace, item.Spec.Selector)
			out = append(out, summary)
		}
	case "cronjob":
		items, err := c.clientset.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, mapKubeError(err)
		}
		for _, item := range items.Items {
			summary := cronJobSummary(item)
			if matchesWorkloadQuery(summary, query) {
				out = append(out, summary)
			}
		}
	default:
		return nil, fmt.Errorf("%w: workload kind %q", domain.ErrUnsupported, query.Kind)
	}
	sortWorkloads(out)
	return out, nil
}

func (c *Client) GetWorkload(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var summary domain.WorkloadSummary
	var selector *metav1.LabelSelector
	conditions := make([]domain.Condition, 0)
	var containers []corev1.Container
	var volumes []corev1.Volume
	switch strings.ToLower(kind) {
	case "deployment":
		item, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return domain.WorkloadDetail{}, mapKubeError(err)
		}
		summary = deploymentSummary(*item)
		selector = item.Spec.Selector
		containers = item.Spec.Template.Spec.Containers
		volumes = item.Spec.Template.Spec.Volumes
		for _, condition := range item.Status.Conditions {
			conditions = append(conditions, domain.Condition{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time})
		}
	case "statefulset":
		item, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return domain.WorkloadDetail{}, mapKubeError(err)
		}
		summary = statefulSetSummary(*item)
		selector = item.Spec.Selector
		containers = item.Spec.Template.Spec.Containers
		volumes = item.Spec.Template.Spec.Volumes
		for _, condition := range item.Status.Conditions {
			conditions = append(conditions, domain.Condition{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time})
		}
	case "daemonset":
		item, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return domain.WorkloadDetail{}, mapKubeError(err)
		}
		summary = daemonSetSummary(*item)
		selector = item.Spec.Selector
		containers = item.Spec.Template.Spec.Containers
		volumes = item.Spec.Template.Spec.Volumes
		for _, condition := range item.Status.Conditions {
			conditions = append(conditions, domain.Condition{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time})
		}
	case "job":
		item, err := c.clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return domain.WorkloadDetail{}, mapKubeError(err)
		}
		summary = jobSummary(*item)
		selector = item.Spec.Selector
		containers = item.Spec.Template.Spec.Containers
		volumes = item.Spec.Template.Spec.Volumes
		for _, condition := range item.Status.Conditions {
			conditions = append(conditions, domain.Condition{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Time})
		}
	case "cronjob":
		item, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return domain.WorkloadDetail{}, mapKubeError(err)
		}
		summary = cronJobSummary(*item)
		containers = item.Spec.JobTemplate.Spec.Template.Spec.Containers
		volumes = item.Spec.JobTemplate.Spec.Template.Spec.Volumes
	default:
		return domain.WorkloadDetail{}, fmt.Errorf("%w: workload kind %q", domain.ErrUnsupported, kind)
	}
	pods := []domain.PodSummary{}
	selectorLabels := map[string]string{}
	if selector != nil {
		selectorLabels = selector.MatchLabels
		if podsBySelector, err := c.ListPodsBySelector(ctx, namespace, selector.MatchLabels, ""); err == nil {
			pods = podsBySelector
		}
		summary.Restarts = c.restartsForSelector(ctx, namespace, selector)
	}
	return domain.WorkloadDetail{
		WorkloadSummary: summary,
		Conditions:      conditions,
		Selector:        selectorLabels,
		Pods:            pods,
		Volumes:         workloadVolumes(volumes),
		Containers:      workloadContainers(containers),
	}, nil
}

func (c *Client) ScaleWorkload(ctx context.Context, kind, namespace, name string, replicas int32, expectedResourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if replicas < 0 || replicas > 10000 {
		return fmt.Errorf("%w: replicas must be between 0 and 10000", domain.ErrInvalid)
	}
	switch strings.ToLower(kind) {
	case "deployment":
		return scaleWithConflictRetry(replicas, expectedResourceVersion,
			func() (*autoscalingv1.Scale, error) {
				return c.clientset.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
			},
			func(scale *autoscalingv1.Scale) error {
				_, err := c.clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
				return err
			})
	case "statefulset":
		return scaleWithConflictRetry(replicas, expectedResourceVersion,
			func() (*autoscalingv1.Scale, error) {
				return c.clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, name, metav1.GetOptions{})
			},
			func(scale *autoscalingv1.Scale) error {
				_, err := c.clientset.AppsV1().StatefulSets(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
				return err
			})
	default:
		return fmt.Errorf("%w: scale is only supported for deployments and statefulsets", domain.ErrUnsupported)
	}
}

// scaleWithConflictRetry retries only optimistic-concurrency conflicts when the
// caller did not pin a resourceVersion. A stale pinned version is still accepted
// when the live scale subresource already has the requested replica count, which
// makes retries after a successful write idempotent without replaying a write.
// Timeouts and 5xx errors still return immediately.
func scaleWithConflictRetry(replicas int32, expectedResourceVersion string, get func() (*autoscalingv1.Scale, error), update func(*autoscalingv1.Scale) error) error {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		scale, err := get()
		if err != nil {
			return mapKubeError(err)
		}
		if expectedResourceVersion != "" && scale.ResourceVersion != expectedResourceVersion {
			if scale.Spec.Replicas == replicas {
				return nil
			}
			return fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, expectedResourceVersion, scale.ResourceVersion)
		}
		if scale.Spec.Replicas == replicas {
			return nil
		}
		scale.Spec.Replicas = replicas
		err = update(scale)
		if err == nil {
			return nil
		}
		if expectedResourceVersion == "" && apierrors.IsConflict(err) && attempt < maxAttempts-1 {
			continue
		}
		return mapKubeError(err)
	}
	return nil
}

func (c *Client) RestartWorkload(ctx context.Context, kind, namespace, name string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339)
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, now))
	switch strings.ToLower(kind) {
	case "deployment":
		_, err := c.clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		return mapKubeError(err)
	case "statefulset":
		_, err := c.clientset.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		return mapKubeError(err)
	case "daemonset":
		_, err := c.clientset.AppsV1().DaemonSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		return mapKubeError(err)
	default:
		return fmt.Errorf("%w: restart is not supported for %s", domain.ErrUnsupported, kind)
	}
}

// UpdateWorkloadImage performs a read-modify-write against the workload pod
// template. A caller-supplied resourceVersion protects against overwriting a
// concurrent change; when the requested image is already present a stale
// version is treated as an idempotent success.
func (c *Client) UpdateWorkloadImage(ctx context.Context, kind, namespace, name string, update domain.WorkloadImageUpdate) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	switch strings.ToLower(kind) {
	case "deployment":
		item, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mapKubeError(err)
		}
		changed, err := applyContainerImage(item.Spec.Template.Spec.Containers, item.ResourceVersion, update)
		if err != nil || !changed {
			return err
		}
		_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, item, metav1.UpdateOptions{})
		return mapKubeError(err)
	case "statefulset":
		item, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mapKubeError(err)
		}
		changed, err := applyContainerImage(item.Spec.Template.Spec.Containers, item.ResourceVersion, update)
		if err != nil || !changed {
			return err
		}
		_, err = c.clientset.AppsV1().StatefulSets(namespace).Update(ctx, item, metav1.UpdateOptions{})
		return mapKubeError(err)
	case "daemonset":
		item, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mapKubeError(err)
		}
		changed, err := applyContainerImage(item.Spec.Template.Spec.Containers, item.ResourceVersion, update)
		if err != nil || !changed {
			return err
		}
		_, err = c.clientset.AppsV1().DaemonSets(namespace).Update(ctx, item, metav1.UpdateOptions{})
		return mapKubeError(err)
	default:
		return fmt.Errorf("%w: image update is not supported for %s", domain.ErrUnsupported, kind)
	}
}

func applyContainerImage(containers []corev1.Container, resourceVersion string, update domain.WorkloadImageUpdate) (bool, error) {
	index := -1
	for i := range containers {
		if containers[i].Name == update.ContainerName {
			index = i
			break
		}
	}
	if index < 0 {
		return false, fmt.Errorf("%w: container %q not found", domain.ErrInvalid, update.ContainerName)
	}
	current := containers[index].Image
	if update.ResourceVersion != "" && resourceVersion != update.ResourceVersion {
		if current == update.Image {
			return false, nil
		}
		return false, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, resourceVersion)
	}
	if current == update.Image {
		return false, nil
	}
	containers[index].Image = update.Image
	return true, nil
}

// SetCronJobSuspend applies a read-modify-write to the CronJob suspend flag.
// A stale resourceVersion is accepted when the requested state is already in
// place, which keeps a retry after a successful write idempotent.
func (c *Client) SetCronJobSuspend(ctx context.Context, namespace, name string, update domain.CronJobSuspendUpdate) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return mapKubeError(err)
	}
	current := item.Spec.Suspend != nil && *item.Spec.Suspend
	if update.ResourceVersion != "" && item.ResourceVersion != update.ResourceVersion {
		if current == update.Suspend {
			return nil
		}
		return fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, item.ResourceVersion)
	}
	if current == update.Suspend {
		return nil
	}
	suspend := update.Suspend
	item.Spec.Suspend = &suspend
	_, err = c.clientset.BatchV1().CronJobs(namespace).Update(ctx, item, metav1.UpdateOptions{})
	return mapKubeError(err)
}

func (c *Client) DeleteWorkload(ctx context.Context, kind, namespace, name string, propagation domain.DeletionPropagation) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	policy := metav1.DeletePropagationBackground
	switch propagation {
	case domain.DeletePropagationForeground:
		policy = metav1.DeletePropagationForeground
	case domain.DeletePropagationOrphan:
		policy = metav1.DeletePropagationOrphan
	}
	options := metav1.DeleteOptions{PropagationPolicy: &policy}
	switch strings.ToLower(kind) {
	case "deployment":
		return mapKubeError(c.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, options))
	case "statefulset":
		return mapKubeError(c.clientset.AppsV1().StatefulSets(namespace).Delete(ctx, name, options))
	case "daemonset":
		return mapKubeError(c.clientset.AppsV1().DaemonSets(namespace).Delete(ctx, name, options))
	case "job":
		return mapKubeError(c.clientset.BatchV1().Jobs(namespace).Delete(ctx, name, options))
	case "cronjob":
		return mapKubeError(c.clientset.BatchV1().CronJobs(namespace).Delete(ctx, name, options))
	default:
		return fmt.Errorf("%w: delete is not supported for %s", domain.ErrUnsupported, kind)
	}
}

func deploymentSummary(item appsv1.Deployment) domain.WorkloadSummary {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	failed := false
	for _, condition := range item.Status.Conditions {
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			failed = true
		}
	}
	status := replicaStatus(desired, item.Status.ReadyReplicas, item.Status.AvailableReplicas, failed)
	return domain.WorkloadSummary{
		Kind: "Deployment", Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Status: status, Ready: fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, desired), Desired: desired,
		Images: podTemplateImages(item.Spec.Template.Spec.Containers), CreatedAt: item.CreationTimestamp.Time, Labels: item.Labels,
	}
}

func statefulSetSummary(item appsv1.StatefulSet) domain.WorkloadSummary {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	status := replicaStatus(desired, item.Status.ReadyReplicas, item.Status.CurrentReplicas, false)
	return domain.WorkloadSummary{
		Kind: "StatefulSet", Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Status: status, Ready: fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, desired), Desired: desired,
		Images: podTemplateImages(item.Spec.Template.Spec.Containers), CreatedAt: item.CreationTimestamp.Time, Labels: item.Labels,
	}
}

func daemonSetSummary(item appsv1.DaemonSet) domain.WorkloadSummary {
	desired := item.Status.DesiredNumberScheduled
	return domain.WorkloadSummary{
		Kind: "DaemonSet", Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Status: readyStatus(item.Status.NumberReady, desired), Ready: fmt.Sprintf("%d/%d", item.Status.NumberReady, desired), Desired: desired,
		Images: podTemplateImages(item.Spec.Template.Spec.Containers), CreatedAt: item.CreationTimestamp.Time, Labels: item.Labels,
	}
}

func jobSummary(item batchv1.Job) domain.WorkloadSummary {
	completions := int32(1)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	status := "Running"
	for _, condition := range item.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			status = "Complete"
		}
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			status = "Failed"
		}
	}
	return domain.WorkloadSummary{
		Kind: "Job", Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Status: status, Ready: fmt.Sprintf("%d/%d", item.Status.Succeeded, completions), Desired: completions,
		Images: podTemplateImages(item.Spec.Template.Spec.Containers), CreatedAt: item.CreationTimestamp.Time, Labels: item.Labels,
	}
}

func cronJobSummary(item batchv1.CronJob) domain.WorkloadSummary {
	status := "Idle"
	if item.Spec.Suspend != nil && *item.Spec.Suspend {
		status = "Suspended"
	} else if len(item.Status.Active) > 0 {
		status = "Running"
	}
	return domain.WorkloadSummary{
		Kind: "CronJob", Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Status: status, Ready: strconv.Itoa(len(item.Status.Active)), Desired: 1,
		Images: podTemplateImages(item.Spec.JobTemplate.Spec.Template.Spec.Containers), CreatedAt: item.CreationTimestamp.Time, Labels: item.Labels,
	}
}

func replicaStatus(desired, ready, available int32, failed bool) string {
	if desired == 0 {
		return "Stopped"
	}
	if failed {
		return "Failed"
	}
	if ready >= desired || available >= desired {
		return "Running"
	}
	if ready == 0 {
		return "Pending"
	}
	return "Updating"
}

func readyStatus(ready, desired int32) string {
	if desired == 0 {
		return "Stopped"
	}
	if ready >= desired {
		return "Running"
	}
	return "Updating"
}

func podTemplateImages(containers []corev1.Container) []string {
	out := make([]string, 0, len(containers))
	for _, container := range containers {
		out = append(out, container.Image)
	}
	return out
}

func workloadContainers(containers []corev1.Container) []domain.WorkloadContainer {
	out := make([]domain.WorkloadContainer, 0, len(containers))
	for _, container := range containers {
		item := domain.WorkloadContainer{
			Name:           container.Name,
			Image:          container.Image,
			Ports:          workloadContainerPorts(container.Ports),
			Resources:      workloadContainerResources(container.Resources),
			Env:            workloadEnvVars(container.Env),
			VolumeMounts:   workloadVolumeMounts(container.VolumeMounts),
			LivenessProbe:  workloadProbe(container.LivenessProbe),
			ReadinessProbe: workloadProbe(container.ReadinessProbe),
			StartupProbe:   workloadProbe(container.StartupProbe),
		}
		out = append(out, item)
	}
	return out
}

func workloadContainerPorts(ports []corev1.ContainerPort) []domain.WorkloadContainerPort {
	out := make([]domain.WorkloadContainerPort, 0, len(ports))
	for _, port := range ports {
		out = append(out, domain.WorkloadContainerPort{
			Name: port.Name, ContainerPort: port.ContainerPort, Protocol: string(port.Protocol),
		})
	}
	return out
}

func workloadContainerResources(resources corev1.ResourceRequirements) domain.WorkloadContainerResources {
	out := domain.WorkloadContainerResources{}
	if quantity, ok := resources.Requests[corev1.ResourceCPU]; ok {
		out.CPURequestMilli = quantity.MilliValue()
	}
	if quantity, ok := resources.Limits[corev1.ResourceCPU]; ok {
		out.CPULimitMilli = quantity.MilliValue()
	}
	if quantity, ok := resources.Requests[corev1.ResourceMemory]; ok {
		out.MemoryRequestBytes = quantity.Value()
	}
	if quantity, ok := resources.Limits[corev1.ResourceMemory]; ok {
		out.MemoryLimitBytes = quantity.Value()
	}
	return out
}

func workloadEnvVars(env []corev1.EnvVar) []domain.WorkloadEnvVar {
	out := make([]domain.WorkloadEnvVar, 0, len(env))
	for _, variable := range env {
		item := domain.WorkloadEnvVar{Name: variable.Name, Value: variable.Value}
		if source := variable.ValueFrom; source != nil {
			switch {
			case source.ConfigMapKeyRef != nil:
				item.ValueFrom = &domain.WorkloadEnvValueFrom{
					Kind: "configMapKeyRef", Name: source.ConfigMapKeyRef.Name, Key: source.ConfigMapKeyRef.Key,
					Optional: source.ConfigMapKeyRef.Optional != nil && *source.ConfigMapKeyRef.Optional,
				}
			case source.SecretKeyRef != nil:
				item.ValueFrom = &domain.WorkloadEnvValueFrom{
					Kind: "secretKeyRef", Name: source.SecretKeyRef.Name, Key: source.SecretKeyRef.Key,
					Optional: source.SecretKeyRef.Optional != nil && *source.SecretKeyRef.Optional,
				}
			case source.FieldRef != nil:
				item.ValueFrom = &domain.WorkloadEnvValueFrom{Kind: "fieldRef", Name: source.FieldRef.FieldPath}
			case source.ResourceFieldRef != nil:
				item.ValueFrom = &domain.WorkloadEnvValueFrom{
					Kind: "resourceFieldRef", Name: source.ResourceFieldRef.Resource, Key: source.ResourceFieldRef.ContainerName,
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func workloadVolumeMounts(mounts []corev1.VolumeMount) []domain.WorkloadVolumeMount {
	out := make([]domain.WorkloadVolumeMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, domain.WorkloadVolumeMount{
			Name: mount.Name, MountPath: mount.MountPath, SubPath: mount.SubPath, ReadOnly: mount.ReadOnly,
		})
	}
	return out
}

func workloadVolumes(volumes []corev1.Volume) []domain.WorkloadVolume {
	out := make([]domain.WorkloadVolume, 0, len(volumes))
	for _, volume := range volumes {
		item := domain.WorkloadVolume{Name: volume.Name, Type: "other"}
		switch {
		case volume.ConfigMap != nil:
			item.Type, item.Source, item.ReadOnly = "configMap", volume.ConfigMap.Name, true
		case volume.Secret != nil:
			item.Type, item.Source, item.ReadOnly = "secret", volume.Secret.SecretName, true
		case volume.PersistentVolumeClaim != nil:
			item.Type, item.Source = "persistentVolumeClaim", volume.PersistentVolumeClaim.ClaimName
		case volume.EmptyDir != nil:
			item.Type, item.Source = "emptyDir", string(volume.EmptyDir.Medium)
		case volume.HostPath != nil:
			item.Type, item.Source = "hostPath", volume.HostPath.Path
		case volume.NFS != nil:
			item.Type, item.Source = "nfs", volume.NFS.Server+":"+volume.NFS.Path
		case volume.CSI != nil:
			item.Type, item.Source = "csi", volume.CSI.Driver
		case volume.DownwardAPI != nil:
			item.Type, item.ReadOnly = "downwardAPI", true
		case volume.Projected != nil:
			item.Type, item.ReadOnly = "projected", true
		}
		out = append(out, item)
	}
	return out
}

func workloadProbe(probe *corev1.Probe) *domain.WorkloadProbe {
	if probe == nil {
		return nil
	}
	item := &domain.WorkloadProbe{
		Command:             make([]string, 0),
		InitialDelaySeconds: probe.InitialDelaySeconds,
		PeriodSeconds:       probe.PeriodSeconds,
		TimeoutSeconds:      probe.TimeoutSeconds,
		SuccessThreshold:    probe.SuccessThreshold,
		FailureThreshold:    probe.FailureThreshold,
	}
	switch {
	case probe.HTTPGet != nil:
		item.Type = "httpGet"
		item.Path = probe.HTTPGet.Path
		item.Host = probe.HTTPGet.Host
		item.Scheme = string(probe.HTTPGet.Scheme)
		item.Port = probe.HTTPGet.Port.String()
	case probe.TCPSocket != nil:
		item.Type = "tcpSocket"
		item.Host = probe.TCPSocket.Host
		item.Port = probe.TCPSocket.Port.String()
	case probe.GRPC != nil:
		item.Type = "grpc"
		item.Port = strconv.Itoa(int(probe.GRPC.Port))
		if probe.GRPC.Service != nil {
			item.Service = *probe.GRPC.Service
		}
	case probe.Exec != nil:
		item.Type = "exec"
		item.Command = append([]string(nil), probe.Exec.Command...)
	default:
		item.Type = "unknown"
	}
	return item
}

func (c *Client) restartsForSelector(ctx context.Context, namespace string, selector *metav1.LabelSelector) int32 {
	if selector == nil {
		return 0
	}
	pods, err := c.ListPodsBySelector(ctx, namespace, selector.MatchLabels, "")
	if err != nil {
		return 0
	}
	var restarts int32
	for _, pod := range pods {
		restarts += pod.Restarts
	}
	return restarts
}

func matchesWorkloadSearch(item domain.WorkloadSummary, search string) bool {
	if search == "" {
		return true
	}
	values := []string{item.Name, item.Namespace, item.Kind, item.Status}
	values = append(values, item.Images...)
	return matchesSearch(values, search)
}

func matchesWorkloadQuery(item domain.WorkloadSummary, query domain.ListQuery) bool {
	if !matchesWorkloadSearch(item, query.Search) {
		return false
	}
	status := strings.TrimSpace(query.Status)
	return status == "" || strings.EqualFold(item.Status, status)
}

func sortWorkloads(items []domain.WorkloadSummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
}

func selectorString(selector map[string]string) string {
	return labels.SelectorFromSet(selector).String()
}
