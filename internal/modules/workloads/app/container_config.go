package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

const (
	maxContainerEnvVars    = 128
	maxContainerMounts     = 64
	maxProbeCommandLength  = 64
	maxProbeArgumentLength = 4096
)

// UpdateContainerConfig validates and authorizes one complete container
// configuration update before the Kubernetes adapter performs the write.
func (s *Service) UpdateContainerConfig(ctx context.Context, actor domain.Actor, kind, namespace, name string, update domain.WorkloadContainerConfigUpdate) error {
	kind = strings.TrimSpace(kind)
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "update-container-config", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, "update-container-config", kind, namespace, name, access); err != nil {
		return err
	}
	if !isApplicationWorkload(kind) {
		err := fmt.Errorf("%w: container configuration is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "update-container-config", kind, namespace, name, err)
		return err
	}
	if err := validateContainerConfigUpdate(namespace, name, &update); err != nil {
		s.audit(ctx, actor, "update-container-config", kind, namespace, name, err)
		return err
	}
	err := s.repository.UpdateWorkloadContainerConfig(ctx, kind, namespace, name, update)
	s.audit(ctx, actor, "update-container-config", kind, namespace, name, err)
	return err
}

func validateContainerConfigUpdate(namespace, name string, update *domain.WorkloadContainerConfigUpdate) error {
	update.ContainerName = strings.TrimSpace(update.ContainerName)
	update.ResourceVersion = strings.TrimSpace(update.ResourceVersion)
	if err := validateWorkloadIdentity(namespace, name); err != nil {
		return err
	}
	if err := validateDNSLabel("container", update.ContainerName); err != nil {
		return err
	}
	if len(update.ResourceVersion) > 256 {
		return validationError("resourceVersion", "resourceVersion is too long")
	}
	if len(update.Env) > maxContainerEnvVars {
		return validationError("env", "at most %d environment variables are allowed", maxContainerEnvVars)
	}
	seenEnv := make(map[string]struct{}, len(update.Env))
	for i := range update.Env {
		variable := &update.Env[i]
		variable.Name = strings.TrimSpace(variable.Name)
		if !envNamePattern.MatchString(variable.Name) || len(variable.Name) > 253 {
			return validationError(fmt.Sprintf("env.%d.name", i), "environment variable name is invalid")
		}
		if _, exists := seenEnv[variable.Name]; exists {
			return validationError(fmt.Sprintf("env.%d.name", i), "environment variable name must be unique")
		}
		seenEnv[variable.Name] = struct{}{}
		if len(variable.Value) > 1<<20 {
			return validationError(fmt.Sprintf("env.%d.value", i), "environment variable value exceeds 1MiB")
		}
		if variable.ValueFrom != nil {
			if err := validateEnvValueFrom(i, variable); err != nil {
				return err
			}
		}
	}
	if len(update.VolumeMounts) > maxContainerMounts {
		return validationError("volumeMounts", "at most %d volume mounts are allowed", maxContainerMounts)
	}
	seenMountNames := make(map[string]struct{}, len(update.VolumeMounts))
	seenMountPaths := make(map[string]struct{}, len(update.VolumeMounts))
	for i := range update.VolumeMounts {
		mount := &update.VolumeMounts[i]
		mount.Name = strings.TrimSpace(mount.Name)
		mount.MountPath = strings.TrimSpace(mount.MountPath)
		mount.SubPath = strings.TrimSpace(mount.SubPath)
		if err := validateDNSLabel(fmt.Sprintf("volumeMounts.%d.name", i), mount.Name); err != nil {
			return err
		}
		if _, exists := seenMountNames[mount.Name]; exists {
			return validationError(fmt.Sprintf("volumeMounts.%d.name", i), "each volume may be mounted only once")
		}
		seenMountNames[mount.Name] = struct{}{}
		if !strings.HasPrefix(mount.MountPath, "/") || strings.Contains(mount.MountPath, "..") || len(mount.MountPath) > 4096 {
			return validationError(fmt.Sprintf("volumeMounts.%d.mountPath", i), "mountPath must be an absolute path without '..'")
		}
		if _, exists := seenMountPaths[mount.MountPath]; exists {
			return validationError(fmt.Sprintf("volumeMounts.%d.mountPath", i), "mountPath must be unique within a container")
		}
		seenMountPaths[mount.MountPath] = struct{}{}
		if strings.HasPrefix(mount.SubPath, "/") || strings.Contains(mount.SubPath, "..") || len(mount.SubPath) > 4096 {
			return validationError(fmt.Sprintf("volumeMounts.%d.subPath", i), "subPath must be relative and must not contain '..'")
		}
	}
	for field, probe := range map[string]**domain.WorkloadProbe{
		"livenessProbe":  &update.LivenessProbe,
		"readinessProbe": &update.ReadinessProbe,
		"startupProbe":   &update.StartupProbe,
	} {
		normalized, err := normalizeWorkloadProbe(field, *probe)
		if err != nil {
			return err
		}
		*probe = normalized
	}
	return nil
}

func validateEnvValueFrom(index int, variable *domain.WorkloadEnvVar) error {
	source := variable.ValueFrom
	source.Kind = strings.TrimSpace(source.Kind)
	source.Name = strings.TrimSpace(source.Name)
	source.Key = strings.TrimSpace(source.Key)
	variable.Value = ""
	field := fmt.Sprintf("env.%d.valueFrom", index)
	switch source.Kind {
	case "configMapKeyRef", "secretKeyRef":
		if len(source.Name) == 0 || len(source.Name) > 253 || !dnsSubdomainPattern.MatchString(source.Name) {
			return validationError(field+".name", "ConfigMap/Secret name must be a DNS-1123 subdomain")
		}
		if source.Key == "" || len(source.Key) > 253 {
			return validationError(field+".key", "key is required and must not exceed 253 characters")
		}
	case "fieldRef":
		if source.Name == "" || len(source.Name) > 253 {
			return validationError(field+".name", "fieldPath is required")
		}
	case "resourceFieldRef":
		if source.Name == "" || len(source.Name) > 253 {
			return validationError(field+".name", "resource name is required")
		}
		if source.Key != "" && !dnsLabelPattern.MatchString(source.Key) {
			return validationError(field+".key", "containerName must be a DNS-1123 label")
		}
	default:
		return validationError(field+".kind", "unsupported environment variable source")
	}
	return nil
}

func normalizeWorkloadProbe(field string, probe *domain.WorkloadProbe) (*domain.WorkloadProbe, error) {
	if probe == nil {
		return nil, nil
	}
	probe.Type = strings.TrimSpace(probe.Type)
	if probe.Type == "" || probe.Type == "disabled" {
		return nil, nil
	}
	probe.Path = strings.TrimSpace(probe.Path)
	probe.Host = strings.TrimSpace(probe.Host)
	probe.Scheme = strings.ToUpper(strings.TrimSpace(probe.Scheme))
	probe.Port = strings.TrimSpace(probe.Port)
	probe.Service = strings.TrimSpace(probe.Service)
	for i := range probe.Command {
		probe.Command[i] = strings.TrimSpace(probe.Command[i])
		if probe.Command[i] == "" || len(probe.Command[i]) > maxProbeArgumentLength {
			return nil, validationError(field+".command", "probe command arguments must be non-empty and at most 4096 characters")
		}
	}
	if probe.InitialDelaySeconds < 0 || probe.InitialDelaySeconds > 3600 {
		return nil, validationError(field+".initialDelaySeconds", "initialDelaySeconds must be between 0 and 3600")
	}
	if probe.PeriodSeconds != 0 && (probe.PeriodSeconds < 1 || probe.PeriodSeconds > 3600) {
		return nil, validationError(field+".periodSeconds", "periodSeconds must be between 1 and 3600")
	}
	if probe.TimeoutSeconds != 0 && (probe.TimeoutSeconds < 1 || probe.TimeoutSeconds > 3600) {
		return nil, validationError(field+".timeoutSeconds", "timeoutSeconds must be between 1 and 3600")
	}
	if probe.SuccessThreshold != 0 && (probe.SuccessThreshold < 1 || probe.SuccessThreshold > 10) {
		return nil, validationError(field+".successThreshold", "successThreshold must be between 1 and 10")
	}
	if probe.FailureThreshold != 0 && (probe.FailureThreshold < 1 || probe.FailureThreshold > 10) {
		return nil, validationError(field+".failureThreshold", "failureThreshold must be between 1 and 10")
	}
	switch probe.Type {
	case "httpGet":
		if !strings.HasPrefix(probe.Path, "/") || len(probe.Path) > 1024 {
			return nil, validationError(field+".path", "HTTP probe path must start with '/' and be at most 1024 characters")
		}
		if err := validateProbePort(field+".port", probe.Port); err != nil {
			return nil, err
		}
		if probe.Scheme == "" {
			probe.Scheme = "HTTP"
		}
		if probe.Scheme != "HTTP" && probe.Scheme != "HTTPS" {
			return nil, validationError(field+".scheme", "scheme must be HTTP or HTTPS")
		}
	case "tcpSocket":
		if err := validateProbePort(field+".port", probe.Port); err != nil {
			return nil, err
		}
	case "exec":
		if len(probe.Command) == 0 || len(probe.Command) > maxProbeCommandLength {
			return nil, validationError(field+".command", "exec probe requires between 1 and 64 command arguments")
		}
	case "grpc":
		port, err := strconv.Atoi(probe.Port)
		if err != nil || port < 1 || port > 65535 {
			return nil, validationError(field+".port", "gRPC probe port must be between 1 and 65535")
		}
		if len(probe.Service) > 253 {
			return nil, validationError(field+".service", "gRPC service must not exceed 253 characters")
		}
	default:
		return nil, validationError(field+".type", "unsupported probe type")
	}
	return probe, nil
}

func validateProbePort(field, value string) error {
	if value == "" || len(value) > 253 {
		return validationError(field, "probe port is required")
	}
	if port, err := strconv.Atoi(value); err == nil {
		if port < 1 || port > 65535 {
			return validationError(field, "probe port must be between 1 and 65535")
		}
		return nil
	}
	if !dnsLabelPattern.MatchString(value) {
		return validationError(field, "probe port must be a number or a DNS-1123 port name")
	}
	return nil
}
