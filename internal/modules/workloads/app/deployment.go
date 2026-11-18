package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"dosh/internal/domain"
)

const (
	maxReplicas        = 1000
	maxCPURequestMilli = 128_000
	maxMemoryBytes     = 1 << 40
	maxEnvVars         = 64
	maxLabels          = 32
	maxContainers      = 32
	maxCommandArgs     = 128
	maxCommandArgBytes = 4096
)

var (
	dnsLabelPattern    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	envNamePattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	imagePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*$`)
	labelNamePattern   = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
	labelValuePattern  = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)
	labelPrefixPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
	capabilityPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// CreateWorkload validates the complete request before any Kubernetes write,
// performs a server-side dry run, and only then creates the workload. A failed
// dry run therefore never leaves a partial resource behind.
func (s *Service) CreateWorkload(ctx context.Context, actor domain.Actor, spec domain.WorkloadCreateSpec) (domain.WorkloadSummary, error) {
	spec.Kind = normalizeCreateKind(spec.Kind)
	if err := ValidateKind(spec.Kind); err != nil {
		validationErr := validationError("kind", "unsupported workload kind %q", spec.Kind)
		s.audit(ctx, actor, "create", spec.Kind, spec.Namespace, spec.Name, validationErr)
		return domain.WorkloadSummary{}, validationErr
	}
	access := workloadAccess(spec.Kind, "create", strings.TrimSpace(spec.Namespace), "", "")
	if err := s.authorize(ctx, actor, "create", spec.Kind, spec.Namespace, spec.Name, access); err != nil {
		return domain.WorkloadSummary{}, err
	}
	if err := ValidateWorkloadSpec(&spec); err != nil {
		s.audit(ctx, actor, "create", spec.Kind, spec.Namespace, spec.Name, err)
		return domain.WorkloadSummary{}, err
	}
	if err := s.applyWorkloadImagePullSecrets(ctx, actor, &spec); err != nil {
		s.audit(ctx, actor, "create", spec.Kind, spec.Namespace, spec.Name, err)
		return domain.WorkloadSummary{}, err
	}
	if _, err := s.repository.CreateWorkload(ctx, spec, true); err != nil {
		s.audit(ctx, actor, "create", spec.Kind, spec.Namespace, spec.Name, err)
		return domain.WorkloadSummary{}, err
	}
	summary, err := s.repository.CreateWorkload(ctx, spec, false)
	s.audit(ctx, actor, "create", spec.Kind, spec.Namespace, spec.Name, err)
	return summary, err
}

// PreviewWorkload returns the exact object the create path would submit,
// without writing to Kubernetes. It enforces the same create permission and
// validation rules as the real request.
func (s *Service) PreviewWorkload(ctx context.Context, actor domain.Actor, spec domain.WorkloadCreateSpec) (string, error) {
	spec.Kind = normalizeCreateKind(spec.Kind)
	if err := ValidateKind(spec.Kind); err != nil {
		return "", validationError("kind", "unsupported workload kind %q", spec.Kind)
	}
	access := workloadAccess(spec.Kind, "create", strings.TrimSpace(spec.Namespace), "", "")
	if err := s.authorize(ctx, actor, "preview", spec.Kind, spec.Namespace, spec.Name, access); err != nil {
		return "", err
	}
	if err := ValidateWorkloadSpec(&spec); err != nil {
		return "", err
	}
	if err := s.applyWorkloadImagePullSecrets(ctx, actor, &spec); err != nil {
		return "", err
	}
	return s.repository.PreviewWorkload(ctx, spec)
}

func (s *Service) CreateDeployment(ctx context.Context, actor domain.Actor, spec domain.DeploymentCreateSpec) (domain.WorkloadSummary, error) {
	spec.Kind = "deployment"
	return s.CreateWorkload(ctx, actor, spec)
}

func (s *Service) PreviewDeployment(ctx context.Context, actor domain.Actor, spec domain.DeploymentCreateSpec) (string, error) {
	spec.Kind = "deployment"
	return s.PreviewWorkload(ctx, actor, spec)
}

// ValidateWorkloadSpec normalizes safe whitespace and rejects inputs that are
// invalid or unsafe before the Kubernetes dry-run boundary. API Server dry-run
// remains authoritative for schema checks outside the supported wizard fields.
func ValidateWorkloadSpec(spec *domain.WorkloadCreateSpec) error {
	spec.Kind = normalizeCreateKind(spec.Kind)
	if err := ValidateKind(spec.Kind); err != nil {
		return validationError("kind", "unsupported workload kind %q", spec.Kind)
	}
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Namespace = strings.TrimSpace(spec.Namespace)
	spec.ServiceName = strings.TrimSpace(spec.ServiceName)
	spec.Schedule = strings.TrimSpace(spec.Schedule)
	spec.ConcurrencyPolicy = strings.TrimSpace(spec.ConcurrencyPolicy)
	spec.ContainerName = strings.TrimSpace(spec.ContainerName)
	spec.Image = strings.TrimSpace(spec.Image)
	spec.RegistryID = strings.TrimSpace(spec.RegistryID)
	spec.PriorityClassName = strings.TrimSpace(spec.PriorityClassName)
	spec.ServiceAccountName = strings.TrimSpace(spec.ServiceAccountName)
	if err := validateDNSLabel("name", spec.Name); err != nil {
		return err
	}
	if err := validateDNSLabel("namespace", spec.Namespace); err != nil {
		return err
	}
	if spec.Kind == "daemonset" || spec.Kind == "job" || spec.Kind == "cronjob" {
		spec.Replicas = 0
	} else if spec.Replicas < 0 || spec.Replicas > maxReplicas {
		return validationError("replicas", "replicas must be between 0 and %d", maxReplicas)
	}
	if spec.Kind == "statefulset" {
		if spec.ServiceName == "" {
			spec.ServiceName = spec.Name
		}
		if err := validateDNSLabel("serviceName", spec.ServiceName); err != nil {
			return err
		}
	}
	if spec.Kind == "cronjob" {
		if spec.Schedule == "" || len(spec.Schedule) > 100 || strings.ContainsAny(spec.Schedule, "\r\n\t") {
			return validationError("schedule", "schedule is required and must be a single-line cron expression")
		}
		switch spec.ConcurrencyPolicy {
		case "", "Allow":
			spec.ConcurrencyPolicy = "Allow"
		case "Forbid", "Replace":
		default:
			return validationError("concurrencyPolicy", "concurrencyPolicy must be Allow, Forbid or Replace")
		}
	}
	legacySingleContainer := len(spec.Containers) == 0
	if err := normalizeWorkloadContainers(spec); err != nil {
		return err
	}
	if err := validateContainers(spec, legacySingleContainer); err != nil {
		return err
	}
	if err := validateScheduling(spec); err != nil {
		return err
	}
	if err := validateBatchSpec(spec); err != nil {
		return err
	}
	if len(spec.Labels) > maxLabels {
		return validationError("labels", "at most %d labels are allowed", maxLabels)
	}
	for key, value := range spec.Labels {
		if key == "app.kubernetes.io/name" || key == "app.kubernetes.io/managed-by" {
			return validationError("labels", "label %q is reserved by dosh", key)
		}
		if err := validateLabel(key, value); err != nil {
			return err
		}
	}
	return nil
}

func ValidateDeploymentSpec(spec *domain.DeploymentCreateSpec) error {
	spec.Kind = "deployment"
	return ValidateWorkloadSpec(spec)
}

func normalizeCreateKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func normalizeWorkloadContainers(spec *domain.WorkloadCreateSpec) error {
	if len(spec.Containers) == 0 {
		spec.Containers = []domain.WorkloadContainerSpec{{
			Name: spec.ContainerName, Image: spec.Image, RegistryID: spec.RegistryID, ContainerPort: spec.ContainerPort,
			CPURequestMilli: spec.CPURequestMilli, CPULimitMilli: spec.CPULimitMilli,
			MemoryRequestBytes: spec.MemoryRequestBytes, MemoryLimitBytes: spec.MemoryLimitBytes,
			Env: spec.Env, Command: spec.Command, Args: spec.Args,
		}}
	}
	if len(spec.InitContainers) > maxContainers || len(spec.Containers) > maxContainers {
		return validationError("containers", "at most %d application and %d init containers are allowed", maxContainers, maxContainers)
	}
	if len(spec.Containers) == 0 {
		return validationError("containers", "at least one application container is required")
	}
	for i := range spec.Containers {
		container := &spec.Containers[i]
		container.Name = strings.TrimSpace(container.Name)
		container.Image = strings.TrimSpace(container.Image)
		container.RegistryID = strings.TrimSpace(container.RegistryID)
		if container.Name == "" && i == 0 {
			container.Name = spec.Name
		}
	}
	for i := range spec.InitContainers {
		container := &spec.InitContainers[i]
		container.Name = strings.TrimSpace(container.Name)
		container.Image = strings.TrimSpace(container.Image)
		container.RegistryID = strings.TrimSpace(container.RegistryID)
	}
	first := spec.Containers[0]
	spec.ContainerName, spec.Image, spec.RegistryID = first.Name, first.Image, first.RegistryID
	spec.ContainerPort = first.ContainerPort
	spec.CPURequestMilli, spec.CPULimitMilli = first.CPURequestMilli, first.CPULimitMilli
	spec.MemoryRequestBytes, spec.MemoryLimitBytes = first.MemoryRequestBytes, first.MemoryLimitBytes
	spec.Env = first.Env
	return nil
}

func validateContainers(spec *domain.WorkloadCreateSpec, legacySingleContainer bool) error {
	seen := make(map[string]struct{}, len(spec.Containers)+len(spec.InitContainers))
	validate := func(container domain.WorkloadContainerSpec, field string, legacy bool) error {
		nameField, imageField, portField := field+".name", field+".image", field+".containerPort"
		if legacy {
			nameField, imageField, portField = "containerName", "image", "containerPort"
		}
		if err := validateDNSLabel(nameField, container.Name); err != nil {
			return err
		}
		if _, exists := seen[container.Name]; exists {
			return validationError(nameField, "container name %q must be unique across application and init containers", container.Name)
		}
		seen[container.Name] = struct{}{}
		if container.Image == "" || len(container.Image) > 512 || !imagePattern.MatchString(container.Image) {
			return validationError(imageField, "image must be a valid container image reference (maximum 512 characters)")
		}
		if container.ContainerPort < 0 || container.ContainerPort > 65535 {
			return validationError(portField, "containerPort must be between 0 and 65535")
		}
		if legacy && (container.ContainerPort < 1 || container.ContainerPort > 65535) {
			return validationError(portField, "containerPort must be between 1 and 65535")
		}
		if err := validateContainerResources(container, field, legacy); err != nil {
			return err
		}
		if len(container.Env) > maxEnvVars {
			envField := field + ".env"
			if legacy {
				envField = "env"
			}
			return validationError(envField, "at most %d environment variables are allowed", maxEnvVars)
		}
		seenEnv := make(map[string]struct{}, len(container.Env))
		for i, env := range container.Env {
			env.Name = strings.TrimSpace(env.Name)
			if !envNamePattern.MatchString(env.Name) || len(env.Name) > 253 {
				envField := fmt.Sprintf("%s.env.%d.name", field, i)
				if legacy {
					envField = fmt.Sprintf("env.%d.name", i)
				}
				return validationError(envField, "environment variable name is invalid")
			}
			if _, exists := seenEnv[env.Name]; exists {
				envField := fmt.Sprintf("%s.env.%d.name", field, i)
				if legacy {
					envField = fmt.Sprintf("env.%d.name", i)
				}
				return validationError(envField, "environment variable name must be unique")
			}
			seenEnv[env.Name] = struct{}{}
			if len(env.Value) > 1<<20 {
				envField := fmt.Sprintf("%s.env.%d.value", field, i)
				if legacy {
					envField = fmt.Sprintf("env.%d.value", i)
				}
				return validationError(envField, "environment variable value exceeds 1MiB")
			}
			container.Env[i] = env
		}
		if err := validateCommandArgs(container.Command, field+".command"); err != nil {
			return err
		}
		if err := validateCommandArgs(container.Args, field+".args"); err != nil {
			return err
		}
		return validateContainerSecurityContext(container.SecurityContext, field+".securityContext")
	}
	for i := range spec.Containers {
		container := spec.Containers[i]
		if err := validate(container, fmt.Sprintf("containers.%d", i), i == 0 && legacySingleContainer); err != nil {
			return err
		}
		spec.Containers[i] = container
	}
	for i := range spec.InitContainers {
		container := spec.InitContainers[i]
		if err := validate(container, fmt.Sprintf("initContainers.%d", i), false); err != nil {
			return err
		}
		spec.InitContainers[i] = container
	}
	return nil
}

func validateContainerResources(container domain.WorkloadContainerSpec, field string, legacy bool) error {
	cpuField, memoryField := field+".cpu", field+".memory"
	cpuLimitField, memoryLimitField := field+".cpuLimitMilli", field+".memoryLimitBytes"
	if legacy {
		cpuField, memoryField = "cpu", "memory"
		cpuLimitField, memoryLimitField = "cpuLimitMilli", "memoryLimitBytes"
	}
	if container.CPURequestMilli < 0 || container.CPULimitMilli < 0 || container.CPURequestMilli > maxCPURequestMilli || container.CPULimitMilli > maxCPURequestMilli {
		return validationError(cpuField, "CPU request and limit must be between 0 and %dm", maxCPURequestMilli)
	}
	if container.MemoryRequestBytes < 0 || container.MemoryLimitBytes < 0 || container.MemoryRequestBytes > maxMemoryBytes || container.MemoryLimitBytes > maxMemoryBytes {
		return validationError(memoryField, "memory request and limit must be between 0 and 1TiB")
	}
	if container.CPULimitMilli > 0 && container.CPULimitMilli < container.CPURequestMilli {
		return validationError(cpuLimitField, "CPU limit must be greater than or equal to the request")
	}
	if container.MemoryLimitBytes > 0 && container.MemoryLimitBytes < container.MemoryRequestBytes {
		return validationError(memoryLimitField, "memory limit must be greater than or equal to the request")
	}
	return nil
}

func validateCommandArgs(values []string, field string) error {
	if len(values) > maxCommandArgs {
		return validationError(field, "at most %d command or argument values are allowed", maxCommandArgs)
	}
	for i, value := range values {
		if len(value) > maxCommandArgBytes {
			return validationError(fmt.Sprintf("%s.%d", field, i), "command or argument exceeds %d bytes", maxCommandArgBytes)
		}
	}
	return nil
}

func validateScheduling(spec *domain.WorkloadCreateSpec) error {
	if spec.PriorityClassName != "" {
		if err := validateDNSLabel("priorityClassName", spec.PriorityClassName); err != nil {
			return err
		}
	}
	if spec.ServiceAccountName != "" {
		if err := validateDNSLabel("serviceAccountName", spec.ServiceAccountName); err != nil {
			return err
		}
	}
	if len(spec.NodeSelector) > maxLabels {
		return validationError("nodeSelector", "at most %d node selector labels are allowed", maxLabels)
	}
	for key, value := range spec.NodeSelector {
		if err := validateLabel(key, value); err != nil {
			return err
		}
	}
	if len(spec.Tolerations) > maxLabels {
		return validationError("tolerations", "at most %d tolerations are allowed", maxLabels)
	}
	for i := range spec.Tolerations {
		toleration := &spec.Tolerations[i]
		toleration.Key = strings.TrimSpace(toleration.Key)
		toleration.Operator = strings.TrimSpace(toleration.Operator)
		toleration.Value = strings.TrimSpace(toleration.Value)
		toleration.Effect = strings.TrimSpace(toleration.Effect)
		if toleration.Key != "" {
			if err := validateLabel(toleration.Key, toleration.Value); err != nil {
				return validationError(fmt.Sprintf("tolerations.%d.key", i), "toleration key or value is invalid")
			}
		}
		if toleration.Operator == "" {
			toleration.Operator = "Equal"
		}
		switch toleration.Operator {
		case "Equal":
			if toleration.Key == "" {
				return validationError(fmt.Sprintf("tolerations.%d.key", i), "toleration key is required for Equal")
			}
		case "Exists":
			if toleration.Value != "" {
				return validationError(fmt.Sprintf("tolerations.%d.value", i), "toleration value must be empty for Exists")
			}
		default:
			return validationError(fmt.Sprintf("tolerations.%d.operator", i), "toleration operator must be Equal or Exists")
		}
		switch toleration.Effect {
		case "", "NoSchedule", "PreferNoSchedule", "NoExecute":
		default:
			return validationError(fmt.Sprintf("tolerations.%d.effect", i), "toleration effect is invalid")
		}
		if toleration.TolerationSeconds != nil && (*toleration.TolerationSeconds < 0 || toleration.Effect != "NoExecute") {
			return validationError(fmt.Sprintf("tolerations.%d.tolerationSeconds", i), "tolerationSeconds requires NoExecute and must not be negative")
		}
	}
	if len(spec.NodeAffinity) > maxLabels {
		return validationError("nodeAffinity", "at most %d node affinity rules are allowed", maxLabels)
	}
	for i, requirement := range spec.NodeAffinity {
		requirement.Key = strings.TrimSpace(requirement.Key)
		requirement.Operator = strings.TrimSpace(requirement.Operator)
		if err := validateLabel(requirement.Key, ""); err != nil {
			return validationError(fmt.Sprintf("nodeAffinity.%d.key", i), "node affinity key is invalid")
		}
		switch requirement.Operator {
		case "In", "NotIn":
			if len(requirement.Values) == 0 {
				return validationError(fmt.Sprintf("nodeAffinity.%d.values", i), "node affinity values are required for %s", requirement.Operator)
			}
		case "Exists", "DoesNotExist":
			if len(requirement.Values) != 0 {
				return validationError(fmt.Sprintf("nodeAffinity.%d.values", i), "node affinity values must be empty for %s", requirement.Operator)
			}
		default:
			return validationError(fmt.Sprintf("nodeAffinity.%d.operator", i), "node affinity operator is invalid")
		}
		for j, value := range requirement.Values {
			if err := validateLabel(requirement.Key, strings.TrimSpace(value)); err != nil {
				return validationError(fmt.Sprintf("nodeAffinity.%d.values.%d", i, j), "node affinity value is invalid")
			}
		}
	}
	if len(spec.TopologySpreadConstraints) > 8 {
		return validationError("topologySpreadConstraints", "at most 8 topology spread constraints are allowed")
	}
	for i, constraint := range spec.TopologySpreadConstraints {
		constraint.TopologyKey = strings.TrimSpace(constraint.TopologyKey)
		if constraint.MaxSkew < 1 || constraint.MaxSkew > 100 {
			return validationError(fmt.Sprintf("topologySpreadConstraints.%d.maxSkew", i), "maxSkew must be between 1 and 100")
		}
		if err := validateLabel(constraint.TopologyKey, ""); err != nil {
			return validationError(fmt.Sprintf("topologySpreadConstraints.%d.topologyKey", i), "topologyKey is invalid")
		}
		switch constraint.WhenUnsatisfiable {
		case "DoNotSchedule", "ScheduleAnyway":
		default:
			return validationError(fmt.Sprintf("topologySpreadConstraints.%d.whenUnsatisfiable", i), "whenUnsatisfiable must be DoNotSchedule or ScheduleAnyway")
		}
	}
	return validatePodSecurityContext(spec.PodSecurityContext)
}

func validatePodSecurityContext(context *domain.WorkloadPodSecurityContext) error {
	if context == nil {
		return nil
	}
	if context.RunAsUser != nil && *context.RunAsUser < 0 {
		return validationError("podSecurityContext.runAsUser", "runAsUser must not be negative")
	}
	if context.RunAsGroup != nil && *context.RunAsGroup < 0 {
		return validationError("podSecurityContext.runAsGroup", "runAsGroup must not be negative")
	}
	if context.FSGroup != nil && *context.FSGroup < 0 {
		return validationError("podSecurityContext.fsGroup", "fsGroup must not be negative")
	}
	switch context.SeccompProfile {
	case "", "RuntimeDefault", "Localhost", "Unconfined":
	default:
		return validationError("podSecurityContext.seccompProfile", "seccompProfile must be RuntimeDefault, Localhost or Unconfined")
	}
	return nil
}

func validateContainerSecurityContext(context *domain.WorkloadContainerSecurityContext, field string) error {
	if context == nil {
		return nil
	}
	if context.RunAsUser != nil && *context.RunAsUser < 0 {
		return validationError(field+".runAsUser", "runAsUser must not be negative")
	}
	if context.RunAsGroup != nil && *context.RunAsGroup < 0 {
		return validationError(field+".runAsGroup", "runAsGroup must not be negative")
	}
	for i, capability := range append(append([]string{}, context.CapabilitiesAdd...), context.CapabilitiesDrop...) {
		if !capabilityPattern.MatchString(capability) || len(capability) > 64 {
			return validationError(fmt.Sprintf("%s.capabilities.%d", field, i), "capability name is invalid")
		}
	}
	return nil
}

func validateBatchSpec(spec *domain.WorkloadCreateSpec) error {
	if spec.Kind != "job" && spec.Kind != "cronjob" {
		spec.JobCompletions, spec.JobParallelism, spec.JobBackoffLimit = nil, nil, nil
		spec.JobActiveDeadlineSeconds, spec.JobTTLSecondsAfterFinished = nil, nil
		return nil
	}
	if spec.JobCompletions != nil && (*spec.JobCompletions < 1 || *spec.JobCompletions > 10000) {
		return validationError("jobCompletions", "jobCompletions must be between 1 and 10000")
	}
	if spec.JobParallelism != nil && (*spec.JobParallelism < 0 || *spec.JobParallelism > 10000) {
		return validationError("jobParallelism", "jobParallelism must be between 0 and 10000")
	}
	if spec.JobBackoffLimit != nil && (*spec.JobBackoffLimit < 0 || *spec.JobBackoffLimit > 10000) {
		return validationError("jobBackoffLimit", "jobBackoffLimit must be between 0 and 10000")
	}
	if spec.JobActiveDeadlineSeconds != nil && (*spec.JobActiveDeadlineSeconds < 1 || *spec.JobActiveDeadlineSeconds > 7*24*60*60) {
		return validationError("jobActiveDeadlineSeconds", "jobActiveDeadlineSeconds must be between 1 and 604800")
	}
	if spec.JobTTLSecondsAfterFinished != nil && (*spec.JobTTLSecondsAfterFinished < 0 || *spec.JobTTLSecondsAfterFinished > 30*24*60*60) {
		return validationError("jobTtlSecondsAfterFinished", "jobTtlSecondsAfterFinished must be between 0 and 2592000")
	}
	return nil
}

func validateDNSLabel(field, value string) error {
	if len(value) == 0 || len(value) > 63 || !dnsLabelPattern.MatchString(value) {
		return validationError(field, "%s must be a DNS-1123 label (lowercase letters, digits, or hyphens; maximum 63 characters)", field)
	}
	return nil
}

func validateLabel(key, value string) error {
	if key == "" || len(key) > 253 || len(value) > 63 || !labelValuePattern.MatchString(value) {
		return validationError("labels", "label %q is invalid", key)
	}
	parts := strings.Split(key, "/")
	if len(parts) > 2 {
		return validationError("labels", "label key %q contains more than one slash", key)
	}
	name := parts[0]
	if len(parts) == 2 {
		if len(parts[0]) == 0 || len(parts[0]) > 253 || !labelPrefixPattern.MatchString(parts[0]) {
			return validationError("labels", "label prefix %q is invalid", parts[0])
		}
		name = parts[1]
	}
	if name == "" || len(name) > 63 || !labelNamePattern.MatchString(name) {
		return validationError("labels", "label name %q is invalid", name)
	}
	return nil
}

func validationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
