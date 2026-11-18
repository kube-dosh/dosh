package kube

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dosh/internal/domain"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

const (
	managedByNameLabel = "app.kubernetes.io/name"
	managedByLabel     = "app.kubernetes.io/managed-by"
)

// CreateWorkload validates through API Server dry-run and creates one of the
// supported workload kinds with application and optional init containers.
func (c *Client) CreateWorkload(ctx context.Context, spec domain.WorkloadCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	normalizeCreateContainers(&spec)
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
	normalizeCreateContainers(&spec)
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
func (c *Client) RunCronJobOnce(ctx context.Context, namespace, name string, input domain.CronJobRunInput) (domain.WorkloadSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	cronJob, err := c.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.WorkloadSummary{}, mapKubeError(err)
	}
	job := jobFromCronJobTemplate(*cronJob)
	if len(job.Spec.Template.Spec.Containers) == 0 {
		return domain.WorkloadSummary{}, fmt.Errorf("%w: cronjob pod template has no application container", domain.ErrInvalid)
	}
	index := 0
	if input.ContainerName != "" {
		index = -1
		for i := range job.Spec.Template.Spec.Containers {
			if job.Spec.Template.Spec.Containers[i].Name == input.ContainerName {
				index = i
				break
			}
		}
		if index < 0 {
			return domain.WorkloadSummary{}, fmt.Errorf("%w: container %q not found", domain.ErrInvalid, input.ContainerName)
		}
	}
	if len(input.Command) > 0 {
		job.Spec.Template.Spec.Containers[index].Command = append([]string(nil), input.Command...)
	}
	if len(input.Args) > 0 {
		job.Spec.Template.Spec.Containers[index].Args = append([]string(nil), input.Args...)
	}
	created, err := c.clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return domain.WorkloadSummary{}, mapKubeError(err)
	}
	return jobSummary(*created), nil
}

func normalizeWorkloadCreateKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func normalizeCreateContainers(spec *domain.WorkloadCreateSpec) {
	if len(spec.Containers) == 0 && strings.TrimSpace(spec.Image) != "" {
		spec.Containers = []domain.WorkloadContainerSpec{{
			Name: spec.ContainerName, Image: spec.Image, RegistryID: spec.RegistryID, ContainerPort: spec.ContainerPort,
			CPURequestMilli: spec.CPURequestMilli, CPULimitMilli: spec.CPULimitMilli,
			MemoryRequestBytes: spec.MemoryRequestBytes, MemoryLimitBytes: spec.MemoryLimitBytes,
			Env: spec.Env, Command: spec.Command, Args: spec.Args,
		}}
	}
	for i := range spec.Containers {
		spec.Containers[i].Name = strings.TrimSpace(spec.Containers[i].Name)
		if spec.Containers[i].Name == "" && i == 0 {
			spec.Containers[i].Name = spec.Name
		}
	}
}

func podTemplateForCreate(spec domain.WorkloadCreateSpec) corev1.PodTemplateSpec {
	normalizeCreateContainers(&spec)
	labels := mergedWorkloadLabels(spec.Name, spec.Labels)
	initContainers := make([]corev1.Container, 0, len(spec.InitContainers))
	for _, container := range spec.InitContainers {
		initContainers = append(initContainers, containerForCreate(container))
	}
	containers := make([]corev1.Container, 0, len(spec.Containers))
	for _, container := range spec.Containers {
		containers = append(containers, containerForCreate(container))
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			ImagePullSecrets:          workloadImagePullSecrets(spec.ImagePullSecrets),
			InitContainers:            initContainers,
			Containers:                containers,
			NodeSelector:              spec.NodeSelector,
			Tolerations:               workloadTolerations(spec.Tolerations),
			Affinity:                  workloadAffinity(spec.NodeAffinity),
			TopologySpreadConstraints: workloadTopologySpread(spec),
			PriorityClassName:         spec.PriorityClassName,
			ServiceAccountName:        spec.ServiceAccountName,
			SecurityContext:           workloadPodSecurityContext(spec.PodSecurityContext),
		},
	}
}

func containerForCreate(spec domain.WorkloadContainerSpec) corev1.Container {
	container := corev1.Container{
		Name: spec.Name, Image: spec.Image, Env: workloadEnv(spec.Env),
		Command: append([]string(nil), spec.Command...), Args: append([]string(nil), spec.Args...),
		Resources: resourcesForContainer(spec), SecurityContext: workloadContainerSecurityContext(spec.SecurityContext),
	}
	if spec.ContainerPort > 0 {
		container.Ports = []corev1.ContainerPort{{ContainerPort: spec.ContainerPort, Protocol: corev1.ProtocolTCP}}
	}
	return container
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
		Spec:       jobSpecForCreate(spec, template),
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
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: mergedWorkloadLabels(spec.Name, spec.Labels)},
				Spec:       jobSpecForCreate(spec, template),
			},
		},
	}
}

func jobSpecForCreate(spec domain.WorkloadCreateSpec, template corev1.PodTemplateSpec) batchv1.JobSpec {
	return batchv1.JobSpec{
		Template:                template,
		Completions:             spec.JobCompletions,
		Parallelism:             spec.JobParallelism,
		BackoffLimit:            spec.JobBackoffLimit,
		ActiveDeadlineSeconds:   spec.JobActiveDeadlineSeconds,
		TTLSecondsAfterFinished: spec.JobTTLSecondsAfterFinished,
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

func resourcesForContainer(spec domain.WorkloadContainerSpec) corev1.ResourceRequirements {
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

func workloadTolerations(items []domain.WorkloadToleration) []corev1.Toleration {
	if len(items) == 0 {
		return nil
	}
	out := make([]corev1.Toleration, 0, len(items))
	for _, item := range items {
		out = append(out, corev1.Toleration{
			Key: item.Key, Operator: corev1.TolerationOperator(item.Operator), Value: item.Value,
			Effect: corev1.TaintEffect(item.Effect), TolerationSeconds: item.TolerationSeconds,
		})
	}
	return out
}

func workloadAffinity(items []domain.WorkloadNodeSelectorRequirement) *corev1.Affinity {
	if len(items) == 0 {
		return nil
	}
	expressions := make([]corev1.NodeSelectorRequirement, 0, len(items))
	for _, item := range items {
		expressions = append(expressions, corev1.NodeSelectorRequirement{
			Key: item.Key, Operator: corev1.NodeSelectorOperator(item.Operator), Values: append([]string(nil), item.Values...),
		})
	}
	return &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: expressions}},
		},
	}}
}

func workloadTopologySpread(spec domain.WorkloadCreateSpec) []corev1.TopologySpreadConstraint {
	if len(spec.TopologySpreadConstraints) == 0 {
		return nil
	}
	out := make([]corev1.TopologySpreadConstraint, 0, len(spec.TopologySpreadConstraints))
	for _, item := range spec.TopologySpreadConstraints {
		out = append(out, corev1.TopologySpreadConstraint{
			MaxSkew: item.MaxSkew, TopologyKey: item.TopologyKey,
			WhenUnsatisfiable: corev1.UnsatisfiableConstraintAction(item.WhenUnsatisfiable),
			LabelSelector:     &metav1.LabelSelector{MatchLabels: workloadCreateSelector(spec.Name)},
		})
	}
	return out
}

func workloadPodSecurityContext(spec *domain.WorkloadPodSecurityContext) *corev1.PodSecurityContext {
	if spec == nil {
		return nil
	}
	out := &corev1.PodSecurityContext{RunAsUser: spec.RunAsUser, RunAsGroup: spec.RunAsGroup, FSGroup: spec.FSGroup, RunAsNonRoot: spec.RunAsNonRoot}
	if spec.SeccompProfile != "" {
		out.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileType(spec.SeccompProfile)}
	}
	return out
}

func workloadContainerSecurityContext(spec *domain.WorkloadContainerSecurityContext) *corev1.SecurityContext {
	if spec == nil {
		return nil
	}
	out := &corev1.SecurityContext{
		RunAsUser: spec.RunAsUser, RunAsGroup: spec.RunAsGroup, RunAsNonRoot: spec.RunAsNonRoot,
		ReadOnlyRootFilesystem: spec.ReadOnlyRootFilesystem, AllowPrivilegeEscalation: spec.AllowPrivilegeEscalation,
	}
	if len(spec.CapabilitiesAdd) > 0 || len(spec.CapabilitiesDrop) > 0 {
		out.Capabilities = &corev1.Capabilities{}
		for _, capability := range spec.CapabilitiesAdd {
			out.Capabilities.Add = append(out.Capabilities.Add, corev1.Capability(capability))
		}
		for _, capability := range spec.CapabilitiesDrop {
			out.Capabilities.Drop = append(out.Capabilities.Drop, corev1.Capability(capability))
		}
	}
	return out
}

func parseRolloutValue(value string) (*intstr.IntOrString, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed := intstr.Parse(value)
	if parsed.Type == intstr.Int && parsed.IntVal < 0 {
		return nil, fmt.Errorf("%w: rollout value must not be negative", domain.ErrInvalid)
	}
	if parsed.Type == intstr.String {
		text := strings.TrimSuffix(parsed.StrVal, "%")
		if !strings.HasSuffix(parsed.StrVal, "%") || text == "" {
			return nil, fmt.Errorf("%w: rollout percentage must use the form 25%%", domain.ErrInvalid)
		}
		percentage, err := strconv.Atoi(text)
		if err != nil || percentage < 0 || percentage > 100 {
			return nil, fmt.Errorf("%w: rollout percentage must be between 0%% and 100%%", domain.ErrInvalid)
		}
	}
	return &parsed, nil
}
