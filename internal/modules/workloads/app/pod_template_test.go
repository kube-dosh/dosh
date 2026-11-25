package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type templateFakeRepository struct {
	*fakeRepository
	getCalls    int
	updateCalls int
	lastUpdate  domain.WorkloadPodTemplateUpdate
	template    domain.WorkloadPodTemplate
	inventory   domain.WorkloadContainerInventory
	result      domain.WorkloadPodTemplateResult
	err         error
}

func (r *templateFakeRepository) GetWorkloadPodTemplate(context.Context, string, string, string) (domain.WorkloadPodTemplate, error) {
	r.getCalls++
	return r.template, r.err
}

func (r *templateFakeRepository) UpdateWorkloadPodTemplate(_ context.Context, _ string, _ string, _ string, update domain.WorkloadPodTemplateUpdate) (domain.WorkloadPodTemplateResult, error) {
	r.updateCalls++
	r.lastUpdate = update
	return r.result, r.err
}

func (r *templateFakeRepository) GetWorkloadContainerInventory(context.Context, string, string, string) (domain.WorkloadContainerInventory, error) {
	return r.inventory, r.err
}

func TestGetPodTemplateAndInventoryRequireReadAccess(t *testing.T) {
	repo := &templateFakeRepository{fakeRepository: &fakeRepository{}, template: domain.WorkloadPodTemplate{Name: "api"}}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: false}, auditor)
	if _, err := service.GetPodTemplate(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("GetPodTemplate() error = %v, want ErrForbidden", err)
	}
	if _, err := service.GetContainerInventory(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("GetContainerInventory() error = %v, want ErrForbidden", err)
	}
	if repo.getCalls != 0 || len(auditor.results) != 2 || auditor.results[0] != "denied" {
		t.Fatalf("repo calls = %d, audits = %#v", repo.getCalls, auditor.results)
	}

	service = New(repo, fakeGuard{allow: true}, auditor)
	if _, err := service.GetPodTemplate(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api"); err != nil {
		t.Fatalf("GetPodTemplate(allowed) error = %v", err)
	}
	if repo.getCalls != 1 {
		t.Fatalf("get calls = %d, want 1", repo.getCalls)
	}
}

func TestUpdatePodTemplateValidatesAuthorizesAndAudits(t *testing.T) {
	template := map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "api"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "api", "image": "registry.test/api:2"}}}}
	repo := &templateFakeRepository{fakeRepository: &fakeRepository{}, result: domain.WorkloadPodTemplateResult{Changed: true}}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: false}, auditor)
	update := domain.WorkloadPodTemplateUpdate{ResourceVersion: "10", Template: template, ChangeCause: "  release  ", Source: "form"}
	if _, err := service.UpdatePodTemplate(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied update error = %v, want ErrForbidden", err)
	}
	if repo.updateCalls != 0 || auditor.results[0] != "denied" {
		t.Fatalf("denied update calls = %d, audits = %#v", repo.updateCalls, auditor.results)
	}

	service = New(repo, fakeGuard{allow: true}, auditor)
	if _, err := service.UpdatePodTemplate(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api", update); err != nil {
		t.Fatalf("UpdatePodTemplate() error = %v", err)
	}
	if repo.updateCalls != 1 || repo.lastUpdate.ChangeCause != "release" || repo.lastUpdate.Source != "form" {
		t.Fatalf("update = %#v", repo.lastUpdate)
	}
	if len(auditor.actions) < 2 || auditor.actions[len(auditor.actions)-1] != "update-pod-template" {
		t.Fatalf("audit actions = %#v", auditor.actions)
	}

	if _, err := service.UpdatePodTemplate(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{Template: template}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing resourceVersion error = %v, want ErrInvalid", err)
	}
	if _, err := service.UpdatePodTemplate(context.Background(), domain.Actor{Username: "dev"}, "job", "prod", "api", update); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("job update error = %v, want ErrUnsupported", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1", repo.updateCalls)
	}
}

func TestUpdatePodTemplateRejectsOversizedDocument(t *testing.T) {
	repo := &templateFakeRepository{fakeRepository: &fakeRepository{}}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	large := map[string]any{"metadata": map[string]any{"annotations": map[string]any{"large": make([]byte, 1<<20)}}}
	_, err := service.UpdatePodTemplate(context.Background(), domain.Actor{Username: "dev"}, "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: "10", Template: large,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized template error = %v, want ErrInvalid", err)
	}
	if repo.updateCalls != 0 {
		t.Fatal("oversized template reached repository")
	}
}
