package app

import (
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"
)

func TestValidateDeploymentSpecNormalizesSafeWhitespace(t *testing.T) {
	t.Parallel()
	spec := validDeploymentSpec()
	spec.Name = "  api  "
	spec.Namespace = "  prod  "
	spec.ContainerName = "  "
	spec.Image = "  registry.test/api:1.0  "
	spec.Env[0].Name = "  MODE  "
	if err := ValidateDeploymentSpec(&spec); err != nil {
		t.Fatalf("ValidateDeploymentSpec() error = %v", err)
	}
	if spec.Name != "api" || spec.Namespace != "prod" || spec.ContainerName != "api" || spec.Image != "registry.test/api:1.0" || spec.Env[0].Name != "MODE" {
		t.Fatalf("normalized spec = %#v", spec)
	}
}

func TestValidateDeploymentSpecRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		apply func(*domain.DeploymentCreateSpec)
	}{
		{name: "empty name", field: "name", apply: func(spec *domain.DeploymentCreateSpec) { spec.Name = "" }},
		{name: "long name", field: "name", apply: func(spec *domain.DeploymentCreateSpec) { spec.Name = strings.Repeat("a", 64) }},
		{name: "uppercase name", field: "name", apply: func(spec *domain.DeploymentCreateSpec) { spec.Name = "API" }},
		{name: "invalid namespace", field: "namespace", apply: func(spec *domain.DeploymentCreateSpec) { spec.Namespace = "Prod_NS" }},
		{name: "invalid container", field: "containerName", apply: func(spec *domain.DeploymentCreateSpec) { spec.ContainerName = "Bad_Name" }},
		{name: "negative replicas", field: "replicas", apply: func(spec *domain.DeploymentCreateSpec) { spec.Replicas = -1 }},
		{name: "too many replicas", field: "replicas", apply: func(spec *domain.DeploymentCreateSpec) { spec.Replicas = maxReplicas + 1 }},
		{name: "empty image", field: "image", apply: func(spec *domain.DeploymentCreateSpec) { spec.Image = "" }},
		{name: "long image", field: "image", apply: func(spec *domain.DeploymentCreateSpec) { spec.Image = strings.Repeat("a", 513) }},
		{name: "invalid image", field: "image", apply: func(spec *domain.DeploymentCreateSpec) { spec.Image = "bad image" }},
		{name: "zero port", field: "containerPort", apply: func(spec *domain.DeploymentCreateSpec) { spec.ContainerPort = 0 }},
		{name: "large port", field: "containerPort", apply: func(spec *domain.DeploymentCreateSpec) { spec.ContainerPort = 65536 }},
		{name: "negative cpu request", field: "cpu", apply: func(spec *domain.DeploymentCreateSpec) { spec.CPURequestMilli = -1 }},
		{name: "large cpu limit", field: "cpu", apply: func(spec *domain.DeploymentCreateSpec) { spec.CPULimitMilli = maxCPURequestMilli + 1 }},
		{name: "negative memory request", field: "memory", apply: func(spec *domain.DeploymentCreateSpec) { spec.MemoryRequestBytes = -1 }},
		{name: "large memory limit", field: "memory", apply: func(spec *domain.DeploymentCreateSpec) { spec.MemoryLimitBytes = maxMemoryBytes + 1 }},
		{name: "cpu limit below request", field: "cpuLimitMilli", apply: func(spec *domain.DeploymentCreateSpec) { spec.CPULimitMilli = spec.CPURequestMilli - 1 }},
		{name: "memory limit below request", field: "memoryLimitBytes", apply: func(spec *domain.DeploymentCreateSpec) { spec.MemoryLimitBytes = spec.MemoryRequestBytes - 1 }},
		{name: "too many env", field: "env", apply: func(spec *domain.DeploymentCreateSpec) {
			spec.Env = make([]domain.DeploymentEnvVar, maxEnvVars+1)
			for i := range spec.Env {
				spec.Env[i] = domain.DeploymentEnvVar{Name: "ENV_" + strings.Repeat("A", i), Value: "value"}
			}
		}},
		{name: "invalid env name", field: "env.0.name", apply: func(spec *domain.DeploymentCreateSpec) { spec.Env[0].Name = "BAD-NAME" }},
		{name: "duplicate env", field: "env.1.name", apply: func(spec *domain.DeploymentCreateSpec) {
			spec.Env = append(spec.Env, domain.DeploymentEnvVar{Name: " MODE ", Value: "other"})
		}},
		{name: "large env value", field: "env.0.value", apply: func(spec *domain.DeploymentCreateSpec) { spec.Env[0].Value = strings.Repeat("x", (1<<20)+1) }},
		{name: "too many labels", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) {
			spec.Labels = make(map[string]string, maxLabels+1)
			for i := 0; i <= maxLabels; i++ {
				spec.Labels["label-"+strings.Repeat("a", i)] = "value"
			}
		}},
		{name: "reserved name label", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) {
			spec.Labels = map[string]string{"app.kubernetes.io/name": "api"}
		}},
		{name: "reserved managed label", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) {
			spec.Labels = map[string]string{"app.kubernetes.io/managed-by": "dosh"}
		}},
		{name: "empty label key", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) { spec.Labels = map[string]string{"": "value"} }},
		{name: "long label value", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) {
			spec.Labels = map[string]string{"team": strings.Repeat("a", 64)}
		}},
		{name: "multiple label slashes", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) { spec.Labels = map[string]string{"a/b/c": "value"} }},
		{name: "invalid label prefix", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) { spec.Labels = map[string]string{"Bad_Prefix/name": "value"} }},
		{name: "invalid label name", field: "labels", apply: func(spec *domain.DeploymentCreateSpec) { spec.Labels = map[string]string{"prefix/-bad": "value"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validDeploymentSpec()
			test.apply(&spec)
			err := ValidateDeploymentSpec(&spec)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("ValidateDeploymentSpec() error = %v, want ErrInvalid", err)
			}
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || validation.Field != test.field {
				t.Fatalf("validation = %#v, want field %q", validation, test.field)
			}
		})
	}
}
