package kube

import (
	"context"
	"strings"

	"dosh/internal/domain"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	managedByNameLabel = "app.kubernetes.io/name"
	managedByLabel     = "app.kubernetes.io/managed-by"
)

// CreateWorkload validates through API Server dry-run and creates one of the
// supported workload kinds with a single application container.
func (c *Client) CreateWorkload(ctx context.Context, spec domain.WorkloadCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	switch normalizeWorkloadCreateKind(spec.Kind) {
	case "deployment":
		created, err := c.clientset.AppsV1().Deployments(spec.Namespace).Create(ctx, deploymentForCreate(spec), options)
		if err != nil {
			return domain.WorkloadSummary{}, mapKubeError(err)
		}
		return deploymentSummary(*created), nil
	case "statefulset":
		created, err := c.clientset.AppsV1().StatefulSets(spec.Namespace).Create(ctx, statefulSetForCreate(spec), options)
		if err != nil {
			return domain.WorkloadSummary{}, mapKubeError(err)
		}
		return statefulSetSummary(*created), nil
	case "daemonset":
		created, err := c.clientset.AppsV1().DaemonSets(spec.Namespace).Create(ctx, daemonSetForCreate(spec), options)
		if err != nil {
			return domain.WorkloadSummary{}, mapKubeError(err)
		}
		return daemonSetSummary(*created), nil
	case "job":
		created, err := c.clientset.BatchV1().Jobs(spec.Namespace).Create(ctx, jobForCreate(spec), options)
		if err != nil {
			return domain.WorkloadSummary{}, mapKubeError(err)
		}
		return jobSummary(*created), nil
	case "cronjob":
		created, err := c.clientset.BatchV1().CronJobs(spec.Namespace).Create(ctx, cronJobForCreate(spec), options)
		if err != nil {
			return domain.WorkloadSummary{}, mapKubeError(err)
		}
		return cronJobSummary(*created), nil
	default:
		return domain.WorkloadSummary{}, domain.ErrUnsupported
	}
}

// PreviewWorkload returns the exact manifest the create path would submit,
// without contacting the Kubernetes API Server.
func (c *Client) PreviewWorkload(_ context.Context, spec domain.WorkloadCreateSpec) (string, error) {
	var object any
	switch normalizeWorkloadCreateKind(spec.Kind) {
	case "deployment":
		object = deploymentForCreate(spec)
	case "statefulset":
		object = statefulSetForCreate(spec)
	case "daemonset":
		object = daemonSetForCreate(spec)
	case "job":
		object = jobForCreate(spec)
	case "cronjob":
		object = cronJobForCreate(spec)
	default:
		return "", domain.ErrUnsupported
	}
	data, err := yaml.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RunCronJobOnce creates a Job from the current CronJob job template. It is the
// dashboard equivalent of `kubectl create job --from=cronjob/<name>` and does
// not alter the CronJob schedule.
func (c *Client) RunCronJobOnce(ctx context.Context, namespace, name string) (domain.WorkloadSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	cronJob, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.WorkloadSummary{}, mapKubeError(err)
	}
	job := jobFromCronJobTemplate(*cronJob)
	created, err := c.clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return domain.WorkloadSummary{}, mapKubeError(err)
	}
	return jobSummary(*created), nil
}

func normalizeWorkloadCreateKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func podTemplateForCreate(spec domain.WorkloadCreateSpec) corev1.PodTemplateSpec {
	labels := mergedWorkloadLabels(spec.Name, spec.Labels)
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			ImagePullSecrets: workloadImagePullSecrets(spec.ImagePullSecrets),
			Containers: []corev1.Container{{
				Name: spec.ContainerName, Image: spec.Image,
				Ports:     []corev1.ContainerPort{{Name: "http", ContainerPort: spec.ContainerPort, Protocol: corev1.ProtocolTCP}},
				Env:       workloadEnv(spec.Env),
				Resources: workloadResources(spec),
			}},
		},
	}
}

func deploymentForCreate(spec domain.WorkloadCreateSpec) *appsv1.Deployment {
	replicas := spec.Replicas
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Labels: mergedWorkloadLabels(spec.Name, spec.Labels)},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: workloadCreateSelector(spec.Name)},
			Template: podTemplateForCreate(spec),
		},
	}
}

func statefulSetForCreate(spec domain.WorkloadCreateSpec) *appsv1.StatefulSet {
	replicas := spec.Replicas
	return &appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Labels: mergedWorkloadLabels(spec.Name, spec.Labels)},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: spec.ServiceName,
			Selector:    &metav1.LabelSelector{MatchLabels: workloadCreateSelector(spec.Name)},
			Template:    podTemplateForCreate(spec),
		},
	}
}

func daemonSetForCreate(spec domain.WorkloadCreateSpec) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DaemonSet"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Labels: mergedWorkloadLabels(spec.Name, spec.Labels)},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: workloadCreateSelector(spec.Name)},
			Template: podTemplateForCreate(spec),
		},
	}
}

func jobForCreate(spec domain.WorkloadCreateSpec) *batchv1.Job {
	template := podTemplateForCreate(spec)
	template.Spec.RestartPolicy = corev1.RestartPolicyNever
	return &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Labels: mergedWorkloadLabels(spec.Name, spec.Labels)},
		Spec: batchv1.JobSpec{
			Template: template,
		},
	}
}

func cronJobForCreate(spec domain.WorkloadCreateSpec) *batchv1.CronJob {
	template := podTemplateForCreate(spec)
	template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
	suspend := spec.Suspend
	policy := batchv1.AllowConcurrent
	switch strings.TrimSpace(spec.ConcurrencyPolicy) {
	case "Forbid":
		policy = batchv1.ForbidConcurrent
	case "Replace":
		policy = batchv1.ReplaceConcurrent
	}
	return &batchv1.CronJob{
		TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Labels: mergedWorkloadLabels(spec.Name, spec.Labels)},
		Spec: batchv1.CronJobSpec{
			Schedule:          spec.Schedule,
			ConcurrencyPolicy: policy,
			Suspend:           &suspend,
			JobTemplate:       batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: template}},
		},
	}
}

func jobFromCronJobTemplate(cronJob batchv1.CronJob) *batchv1.Job {
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cronJob.Name + "-manual-",
			Namespace:    cronJob.Namespace,
			Labels:       cronJob.Spec.JobTemplate.ObjectMeta.Labels,
			Annotations:  map[string]string{"dosh.io/run-from-cronjob": cronJob.Name},
		},
		Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
	}
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	job.Labels[managedByLabel] = "dosh"
	return job
}

func workloadCreateSelector(name string) map[string]string {
	return map[string]string{managedByNameLabel: name, managedByLabel: "dosh"}
}

func (c *Client) CreateDeployment(ctx context.Context, spec domain.DeploymentCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	spec.Kind = "deployment"
	return c.CreateWorkload(ctx, spec, dryRun)
}

func (c *Client) PreviewDeployment(ctx context.Context, spec domain.DeploymentCreateSpec) (string, error) {
	spec.Kind = "deployment"
	return c.PreviewWorkload(ctx, spec)
}

func workloadImagePullSecrets(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	result := make([]corev1.LocalObjectReference, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, corev1.LocalObjectReference{Name: name})
	}
	return result
}

func mergedWorkloadLabels(name string, custom map[string]string) map[string]string {
	labels := make(map[string]string, len(custom)+2)
	for key, value := range custom {
		labels[key] = value
	}
	labels[managedByNameLabel] = name
	labels[managedByLabel] = "dosh"
	return labels
}

func workloadEnv(env []domain.DeploymentEnvVar) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	out := make([]corev1.EnvVar, 0, len(env))
	for _, item := range env {
		out = append(out, corev1.EnvVar{Name: item.Name, Value: item.Value})
	}
	return out
}

func workloadResources(spec domain.WorkloadCreateSpec) corev1.ResourceRequirements {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	if spec.CPURequestMilli > 0 {
		requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(spec.CPURequestMilli, resource.DecimalSI)
	}
	if spec.MemoryRequestBytes > 0 {
		requests[corev1.ResourceMemory] = *resource.NewQuantity(spec.MemoryRequestBytes, resource.BinarySI)
	}
	if spec.CPULimitMilli > 0 {
		limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(spec.CPULimitMilli, resource.DecimalSI)
	}
	if spec.MemoryLimitBytes > 0 {
		limits[corev1.ResourceMemory] = *resource.NewQuantity(spec.MemoryLimitBytes, resource.BinarySI)
	}
	return corev1.ResourceRequirements{Requests: requests, Limits: limits}
}
