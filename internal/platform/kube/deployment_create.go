package kube

import (
	"context"

	"dosh/internal/domain"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	managedByNameLabel = "app.kubernetes.io/name"
	managedByLabel     = "app.kubernetes.io/managed-by"
)

func (c *Client) CreateDeployment(ctx context.Context, spec domain.DeploymentCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	created, err := c.clientset.AppsV1().Deployments(spec.Namespace).Create(ctx, deploymentForCreate(spec), options)
	if err != nil {
		return domain.WorkloadSummary{}, mapKubeError(err)
	}
	return deploymentSummary(*created), nil
}

func (c *Client) PreviewDeployment(_ context.Context, spec domain.DeploymentCreateSpec) (string, error) {
	data, err := yaml.Marshal(deploymentForCreate(spec))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func deploymentForCreate(spec domain.DeploymentCreateSpec) *appsv1.Deployment {
	labels := mergedDeploymentLabels(spec.Name, spec.Labels)
	selector := map[string]string{
		managedByNameLabel: spec.Name,
		managedByLabel:     "dosh",
	}
	replicas := spec.Replicas
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name, Namespace: spec.Namespace, Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: spec.ContainerName, Image: spec.Image,
					Ports:     []corev1.ContainerPort{{Name: "http", ContainerPort: spec.ContainerPort, Protocol: corev1.ProtocolTCP}},
					Env:       deploymentEnv(spec.Env),
					Resources: deploymentResources(spec),
				}}},
			},
		},
	}
}

func mergedDeploymentLabels(name string, custom map[string]string) map[string]string {
	labels := make(map[string]string, len(custom)+2)
	for key, value := range custom {
		labels[key] = value
	}
	labels[managedByNameLabel] = name
	labels[managedByLabel] = "dosh"
	return labels
}

func deploymentEnv(env []domain.DeploymentEnvVar) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	out := make([]corev1.EnvVar, 0, len(env))
	for _, item := range env {
		out = append(out, corev1.EnvVar{Name: item.Name, Value: item.Value})
	}
	return out
}

func deploymentResources(spec domain.DeploymentCreateSpec) corev1.ResourceRequirements {
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
