package kube

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// UpdateWorkloadContainerConfig updates only the editable configuration of one
// regular container. The adapter keeps the read-modify-write atomic from the
// caller's perspective by pinning the workload resourceVersion.
func (c *Client) UpdateWorkloadContainerConfig(ctx context.Context, kind, namespace, name string, update domain.WorkloadContainerConfigUpdate) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	switch strings.ToLower(kind) {
	case "deployment":
		item, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mapKubeError(err)
		}
		if err := ensureWorkloadResourceVersion(item.ResourceVersion, update.ResourceVersion); err != nil {
			return err
		}
		if err := applyWorkloadContainerConfig(item.Spec.Template.Spec.Containers, item.Spec.Template.Spec.Volumes, update); err != nil {
			return err
		}
		_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, item, metav1.UpdateOptions{})
		return mapKubeError(err)
	case "statefulset":
		item, err := c.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mapKubeError(err)
		}
		if err := ensureWorkloadResourceVersion(item.ResourceVersion, update.ResourceVersion); err != nil {
			return err
		}
		if err := applyWorkloadContainerConfig(item.Spec.Template.Spec.Containers, item.Spec.Template.Spec.Volumes, update); err != nil {
			return err
		}
		_, err = c.clientset.AppsV1().StatefulSets(namespace).Update(ctx, item, metav1.UpdateOptions{})
		return mapKubeError(err)
	case "daemonset":
		item, err := c.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return mapKubeError(err)
		}
		if err := ensureWorkloadResourceVersion(item.ResourceVersion, update.ResourceVersion); err != nil {
			return err
		}
		if err := applyWorkloadContainerConfig(item.Spec.Template.Spec.Containers, item.Spec.Template.Spec.Volumes, update); err != nil {
			return err
		}
		_, err = c.clientset.AppsV1().DaemonSets(namespace).Update(ctx, item, metav1.UpdateOptions{})
		return mapKubeError(err)
	default:
		return fmt.Errorf("%w: container configuration is not supported for %s", domain.ErrUnsupported, kind)
	}
}

func ensureWorkloadResourceVersion(current, expected string) error {
	if expected == "" || current == expected {
		return nil
	}
	return fmt.Errorf("%w: expected resourceVersion %s, got %s", domain.ErrConflict, expected, current)
}

func applyWorkloadContainerConfig(containers []corev1.Container, volumes []corev1.Volume, update domain.WorkloadContainerConfigUpdate) error {
	index := -1
	for i := range containers {
		if containers[i].Name == update.ContainerName {
			index = i
			break
		}
	}
	if index < 0 {
		return &domain.ValidationError{Field: "container", Message: fmt.Sprintf("container %q was not found", update.ContainerName)}
	}
	availableVolumes := make(map[string]struct{}, len(volumes))
	for _, volume := range volumes {
		availableVolumes[volume.Name] = struct{}{}
	}
	for i, mount := range update.VolumeMounts {
		if _, ok := availableVolumes[mount.Name]; !ok {
			return &domain.ValidationError{Field: fmt.Sprintf("volumeMounts.%d.name", i), Message: fmt.Sprintf("volume %q was not found in the pod template", mount.Name)}
		}
	}
	containers[index].Env = coreEnvVars(update.Env)
	containers[index].VolumeMounts = coreVolumeMounts(update.VolumeMounts)
	containers[index].LivenessProbe = coreProbe(update.LivenessProbe)
	containers[index].ReadinessProbe = coreProbe(update.ReadinessProbe)
	containers[index].StartupProbe = coreProbe(update.StartupProbe)
	return nil
}

func coreEnvVars(env []domain.WorkloadEnvVar) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	out := make([]corev1.EnvVar, 0, len(env))
	for _, variable := range env {
		item := corev1.EnvVar{Name: variable.Name, Value: variable.Value}
		if source := variable.ValueFrom; source != nil {
			optional := source.Optional
			switch source.Kind {
			case "configMapKeyRef":
				item.Value = ""
				item.ValueFrom = &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: source.Name}, Key: source.Key, Optional: &optional,
				}}
			case "secretKeyRef":
				item.Value = ""
				item.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: source.Name}, Key: source.Key, Optional: &optional,
				}}
			case "fieldRef":
				item.Value = ""
				item.ValueFrom = &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: source.Name}}
			case "resourceFieldRef":
				item.Value = ""
				item.ValueFrom = &corev1.EnvVarSource{ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource: source.Name, ContainerName: source.Key,
				}}
			}
		}
		out = append(out, item)
	}
	return out
}

func coreVolumeMounts(mounts []domain.WorkloadVolumeMount) []corev1.VolumeMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]corev1.VolumeMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, corev1.VolumeMount{
			Name: mount.Name, MountPath: mount.MountPath, SubPath: mount.SubPath, ReadOnly: mount.ReadOnly,
		})
	}
	return out
}

func coreProbe(probe *domain.WorkloadProbe) *corev1.Probe {
	if probe == nil || probe.Type == "" || probe.Type == "disabled" {
		return nil
	}
	item := &corev1.Probe{
		InitialDelaySeconds: probe.InitialDelaySeconds,
		PeriodSeconds:       probe.PeriodSeconds,
		TimeoutSeconds:      probe.TimeoutSeconds,
		SuccessThreshold:    probe.SuccessThreshold,
		FailureThreshold:    probe.FailureThreshold,
	}
	port := probeIntOrString(probe.Port)
	switch probe.Type {
	case "httpGet":
		item.HTTPGet = &corev1.HTTPGetAction{Path: probe.Path, Port: port, Host: probe.Host, Scheme: corev1.URIScheme(probe.Scheme)}
	case "tcpSocket":
		item.TCPSocket = &corev1.TCPSocketAction{Port: port, Host: probe.Host}
	case "grpc":
		portNumber, _ := strconv.Atoi(probe.Port)
		service := probe.Service
		item.GRPC = &corev1.GRPCAction{Port: int32(portNumber), Service: &service}
	default:
		command := append([]string(nil), probe.Command...)
		item.Exec = &corev1.ExecAction{Command: command}
	}
	return item
}

func probeIntOrString(value string) intstr.IntOrString {
	if port, err := strconv.Atoi(value); err == nil {
		return intstr.FromInt32(int32(port))
	}
	return intstr.FromString(value)
}
