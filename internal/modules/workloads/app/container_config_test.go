package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

func TestUpdateContainerConfigValidatesAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	update := domain.WorkloadContainerConfigUpdate{
		ContainerName: "api",
		Env: []domain.WorkloadEnvVar{
			{Name: "MODE", Value: "staging"},
			{Name: "TOKEN", ValueFrom: &domain.WorkloadEnvValueFrom{Kind: "secretKeyRef", Name: "api-secret", Key: "token"}},
		},
		VolumeMounts: []domain.WorkloadVolumeMount{{Name: "config", MountPath: "/etc/app", ReadOnly: true}},
		ReadinessProbe: &domain.WorkloadProbe{
			Type: "httpGet", Path: "/healthz", Port: "8080", Scheme: "http", PeriodSeconds: 10, FailureThreshold: 3,
		},
		LivenessProbe:   &domain.WorkloadProbe{Type: "exec", Command: []string{"cat", "/tmp/healthy"}},
		ResourceVersion: "42",
	}
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	denied := New(repo, fakeGuard{allow: false}, auditor)
	if err := denied.UpdateContainerConfig(context.Background(), domain.Actor{Username: "viewer"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied UpdateContainerConfig() error = %v", err)
	}
	if repo.containerCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("denied calls = %d audit = %#v", repo.containerCalls, auditor.results)
	}

	service := New(repo, fakeGuard{allow: true}, auditor)
	if err := service.UpdateContainerConfig(context.Background(), domain.Actor{Username: "admin"}, "job", "prod", "migrate", update); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("job UpdateContainerConfig() error = %v", err)
	}
	invalidCases := []domain.WorkloadContainerConfigUpdate{
		{ContainerName: "Bad_Name"},
		{ContainerName: "api", Env: []domain.WorkloadEnvVar{{Name: "INVALID-NAME", Value: "x"}}},
		{ContainerName: "api", Env: []domain.WorkloadEnvVar{{Name: "TOKEN", ValueFrom: &domain.WorkloadEnvValueFrom{Kind: "secretKeyRef", Name: "secret"}}}},
		{ContainerName: "api", VolumeMounts: []domain.WorkloadVolumeMount{{Name: "config", MountPath: "relative"}}},
		{ContainerName: "api", ReadinessProbe: &domain.WorkloadProbe{Type: "httpGet", Path: "healthz", Port: "8080"}},
		{ContainerName: "api", LivenessProbe: &domain.WorkloadProbe{Type: "exec"}},
	}
	for _, invalid := range invalidCases {
		if err := service.UpdateContainerConfig(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", invalid); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid UpdateContainerConfig(%#v) error = %v", invalid, err)
		}
	}
	if repo.containerCalls != 0 || len(auditor.results) != 8 {
		t.Fatalf("invalid calls = %d audit = %#v", repo.containerCalls, auditor.results)
	}

	update.ContainerName = "  api  "
	update.Env[1].ValueFrom.Name = "  api-secret  "
	update.ReadinessProbe.Scheme = " http "
	if err := service.UpdateContainerConfig(context.Background(), domain.Actor{Username: "admin"}, "Deployment", " prod ", " api ", update); err != nil {
		t.Fatalf("UpdateContainerConfig() error = %v", err)
	}
	if repo.containerCalls != 1 || repo.lastKind != "Deployment" || repo.lastNamespace != "prod" || repo.lastName != "api" {
		t.Fatalf("container calls = %d last = %s/%s/%s", repo.containerCalls, repo.lastKind, repo.lastNamespace, repo.lastName)
	}
	if repo.lastContainer.ContainerName != "api" || repo.lastContainer.Env[1].ValueFrom.Name != "api-secret" || repo.lastContainer.ReadinessProbe.Scheme != "HTTP" {
		t.Fatalf("normalized update = %#v", repo.lastContainer)
	}
	repo.containerErr = domain.ErrConflict
	if err := service.UpdateContainerConfig(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflict UpdateContainerConfig() error = %v", err)
	}
	if repo.containerCalls != 2 || len(auditor.results) != 10 || auditor.results[8] != "success" || auditor.results[9] != "failure" {
		t.Fatalf("success/conflict calls = %d audit = %#v", repo.containerCalls, auditor.results)
	}
}
