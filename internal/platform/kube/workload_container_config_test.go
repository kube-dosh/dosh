package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestGetWorkloadConvertsContainerConfiguration(t *testing.T) {
	deployment := testDeployment()
	optional := true
	deployment.Spec.Template.Spec = corev1.PodSpec{
		Volumes: []corev1.Volume{
			{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}}},
			{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "app-data"}}},
		},
		Containers: []corev1.Container{{
			Name:  "api",
			Image: "registry.test/api:1",
			Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("100m"), corev1.ResourceMemory: resourceQuantity("64Mi")},
				Limits:   corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("500m"), corev1.ResourceMemory: resourceQuantity("256Mi")},
			},
			Env: []corev1.EnvVar{
				{Name: "MODE", Value: "production"},
				{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"}, Key: "token", Optional: &optional,
				}}},
			},
			VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/app", ReadOnly: true}},
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path: "/ready", Port: probePort("8080"), Scheme: corev1.URISchemeHTTP,
			}}, InitialDelaySeconds: 5, PeriodSeconds: 10, FailureThreshold: 3},
			LivenessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"cat", "/tmp/healthy"}}}, PeriodSeconds: 20},
		}},
	}
	client, _ := newFakeKubeClient(deployment)
	detail, err := client.GetWorkload(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkload() error = %v", err)
	}
	if len(detail.Volumes) != 2 || detail.Volumes[0].Type != "configMap" || detail.Volumes[0].Source != "app-config" || detail.Volumes[1].Source != "app-data" {
		t.Fatalf("volumes = %#v", detail.Volumes)
	}
	container := detail.Containers[0]
	if len(container.Ports) != 1 || container.Ports[0].ContainerPort != 8080 || container.Resources.CPULimitMilli != 500 {
		t.Fatalf("container ports/resources = %#v", container)
	}
	if len(container.Env) != 2 || container.Env[0].Value != "production" || container.Env[1].ValueFrom == nil || container.Env[1].ValueFrom.Kind != "secretKeyRef" || !container.Env[1].ValueFrom.Optional {
		t.Fatalf("environment = %#v", container.Env)
	}
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != "/etc/app" || !container.VolumeMounts[0].ReadOnly {
		t.Fatalf("volume mounts = %#v", container.VolumeMounts)
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.Type != "httpGet" || container.ReadinessProbe.Path != "/ready" || container.ReadinessProbe.Port != "8080" {
		t.Fatalf("readiness probe = %#v", container.ReadinessProbe)
	}
	if container.LivenessProbe == nil || container.LivenessProbe.Type != "exec" || len(container.LivenessProbe.Command) != 2 {
		t.Fatalf("liveness probe = %#v", container.LivenessProbe)
	}
}

func TestUpdateWorkloadContainerConfigAppliesEditableFields(t *testing.T) {
	deployment := testDeployment()
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}}},
	}
	client, clientset := newFakeKubeClient(deployment)
	update := domain.WorkloadContainerConfigUpdate{
		ContainerName: "api",
		Env: []domain.WorkloadEnvVar{
			{Name: "MODE", Value: "staging"},
			{Name: "TOKEN", ValueFrom: &domain.WorkloadEnvValueFrom{Kind: "secretKeyRef", Name: "api-secret", Key: "token"}},
		},
		VolumeMounts: []domain.WorkloadVolumeMount{{Name: "config", MountPath: "/etc/app", ReadOnly: true}},
		ReadinessProbe: &domain.WorkloadProbe{
			Type: "httpGet", Path: "/healthz", Port: "8080", Scheme: "HTTP", PeriodSeconds: 10, FailureThreshold: 3,
		},
		LivenessProbe:   &domain.WorkloadProbe{Type: "exec", Command: []string{"cat", "/tmp/healthy"}},
		ResourceVersion: "10",
	}
	if err := client.UpdateWorkloadContainerConfig(context.Background(), "deployment", "prod", "api", update); err != nil {
		t.Fatalf("UpdateWorkloadContainerConfig() error = %v", err)
	}
	updated, err := clientset.AppsV1().Deployments("prod").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	container := updated.Spec.Template.Spec.Containers[0]
	if len(container.Env) != 2 || container.Env[0].Value != "staging" || container.Env[1].ValueFrom == nil || container.Env[1].ValueFrom.SecretKeyRef.Name != "api-secret" {
		t.Fatalf("environment = %#v", container.Env)
	}
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != "/etc/app" || !container.VolumeMounts[0].ReadOnly {
		t.Fatalf("volume mounts = %#v", container.VolumeMounts)
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet.Path != "/healthz" || container.ReadinessProbe.HTTPGet.Port.String() != "8080" {
		t.Fatalf("readiness probe = %#v", container.ReadinessProbe)
	}
	if container.ReadinessProbe.HTTPGet.Port.Type != intstr.Int || container.ReadinessProbe.HTTPGet.Port.IntVal != 8080 {
		t.Fatalf("readiness probe port must be numeric: %#v", container.ReadinessProbe.HTTPGet.Port)
	}
	if container.LivenessProbe == nil || container.LivenessProbe.Exec == nil || len(container.LivenessProbe.Exec.Command) != 2 {
		t.Fatalf("liveness probe = %#v", container.LivenessProbe)
	}
	if container.StartupProbe != nil {
		t.Fatalf("startup probe should be cleared: %#v", container.StartupProbe)
	}
}

func TestUpdateWorkloadContainerConfigRejectsStaleVersionAndMissingVolume(t *testing.T) {
	deployment := testDeployment()
	client, clientset := newFakeKubeClient(deployment)
	update := domain.WorkloadContainerConfigUpdate{ContainerName: "api", ResourceVersion: "9"}
	if err := client.UpdateWorkloadContainerConfig(context.Background(), "deployment", "prod", "api", update); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update error = %v, want ErrConflict", err)
	}
	update.ResourceVersion = "10"
	update.VolumeMounts = []domain.WorkloadVolumeMount{{Name: "missing", MountPath: "/data"}}
	if err := client.UpdateWorkloadContainerConfig(context.Background(), "deployment", "prod", "api", update); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing volume error = %v, want ErrInvalid", err)
	}
	updated, err := clientset.AppsV1().Deployments("prod").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if len(updated.Spec.Template.Spec.Containers[0].VolumeMounts) != 0 {
		t.Fatalf("deployment mutated on validation failure: %#v", updated)
	}
}

func probePort(value string) intstr.IntOrString {
	return intstr.FromString(value)
}
