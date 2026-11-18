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
)

var (
	dnsLabelPattern    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	envNamePattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	imagePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*$`)
	labelNamePattern   = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
	labelValuePattern  = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)
	labelPrefixPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
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
	if err := s.applyImagePullSecret(ctx, actor, spec.RegistryID, spec.Image, &spec.ImagePullSecrets); err != nil {
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
	if err := s.applyImagePullSecret(ctx, actor, spec.RegistryID, spec.Image, &spec.ImagePullSecrets); err != nil {
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
	if spec.ContainerName == "" {
		spec.ContainerName = spec.Name
	}
	if err := validateDNSLabel("name", spec.Name); err != nil {
		return err
	}
	if err := validateDNSLabel("namespace", spec.Namespace); err != nil {
		return err
	}
	if err := validateDNSLabel("containerName", spec.ContainerName); err != nil {
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
	if spec.Image == "" || len(spec.Image) > 512 || !imagePattern.MatchString(spec.Image) {
		return validationError("image", "image must be a valid container image reference (maximum 512 characters)")
	}
	if spec.ContainerPort < 1 || spec.ContainerPort > 65535 {
		return validationError("containerPort", "containerPort must be between 1 and 65535")
	}
	if err := validateResources(*spec); err != nil {
		return err
	}
	if len(spec.Env) > maxEnvVars {
		return validationError("env", "at most %d environment variables are allowed", maxEnvVars)
	}
	seenEnv := make(map[string]struct{}, len(spec.Env))
	for i, env := range spec.Env {
		env.Name = strings.TrimSpace(env.Name)
		if !envNamePattern.MatchString(env.Name) || len(env.Name) > 253 {
			return validationError(fmt.Sprintf("env.%d.name", i), "environment variable name is invalid")
		}
		if _, exists := seenEnv[env.Name]; exists {
			return validationError(fmt.Sprintf("env.%d.name", i), "environment variable name must be unique")
		}
		seenEnv[env.Name] = struct{}{}
		if len(env.Value) > 1<<20 {
			return validationError(fmt.Sprintf("env.%d.value", i), "environment variable value exceeds 1MiB")
		}
		spec.Env[i] = env
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

func validateResources(spec domain.WorkloadCreateSpec) error {
	if spec.CPURequestMilli < 0 || spec.CPULimitMilli < 0 || spec.CPURequestMilli > maxCPURequestMilli || spec.CPULimitMilli > maxCPURequestMilli {
		return validationError("cpu", "CPU request and limit must be between 0 and %dm", maxCPURequestMilli)
	}
	if spec.MemoryRequestBytes < 0 || spec.MemoryLimitBytes < 0 || spec.MemoryRequestBytes > maxMemoryBytes || spec.MemoryLimitBytes > maxMemoryBytes {
		return validationError("memory", "memory request and limit must be between 0 and 1TiB")
	}
	if spec.CPULimitMilli > 0 && spec.CPULimitMilli < spec.CPURequestMilli {
		return validationError("cpuLimitMilli", "CPU limit must be greater than or equal to the request")
	}
	if spec.MemoryLimitBytes > 0 && spec.MemoryLimitBytes < spec.MemoryRequestBytes {
		return validationError("memoryLimitBytes", "memory limit must be greater than or equal to the request")
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
