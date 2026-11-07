package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"
)

type yamlRepository struct {
	resource   domain.YAMLResource
	err        error
	exists     bool
	existsErr  error
	existsArgs []domain.ResourceType
	requests   []domain.ApplyRequest
}

func (r *yamlRepository) GetYAML(context.Context, domain.ResourceType, string, string) (domain.YAMLResource, error) {
	return r.resource, r.err
}
func (r *yamlRepository) Exists(_ context.Context, resourceType domain.ResourceType, _, _ string) (bool, error) {
	r.existsArgs = append(r.existsArgs, resourceType)
	return r.exists, r.existsErr
}
func (r *yamlRepository) ApplyYAML(_ context.Context, request domain.ApplyRequest) (domain.YAMLResource, error) {
	r.requests = append(r.requests, request)
	return r.resource, r.err
}

type yamlAuthorizer struct {
	err       error
	access    domain.Access
	accesses  []domain.Access
	denyVerbs map[string]bool
	calls     int
}

func (a *yamlAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.calls++
	a.access = access
	a.accesses = append(a.accesses, access)
	if a.denyVerbs[access.Verb] {
		return domain.ErrForbidden
	}
	return a.err
}

type yamlAuditor struct{ events []domain.AuditEvent }

func (a *yamlAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestYAMLApplyDryRunAndCommit(t *testing.T) {
	repo := &yamlRepository{resource: domain.YAMLResource{Name: "api", Namespace: "prod"}, exists: true}
	authorizer := &yamlAuthorizer{}
	auditor := &yamlAuditor{}
	service := New(repo, authorizer, auditor)
	body := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: prod\n"

	result, err := service.Apply(context.Background(), domain.Actor{Username: "operator"}, domain.ApplyRequest{YAML: body, DryRun: true})
	if err != nil || result.Name != "api" {
		t.Fatalf("dry-run Apply() = (%#v, %v)", result, err)
	}
	if len(repo.requests) != 1 || !repo.requests[0].DryRun {
		t.Fatalf("dry-run requests = %#v", repo.requests)
	}
	if authorizer.access != (domain.Access{Verb: "update", APIGroup: "apps", Resource: "deployments", Namespace: "prod", Name: "api"}) {
		t.Fatalf("authorization access = %#v", authorizer.access)
	}
	if len(repo.existsArgs) != 1 || repo.existsArgs[0] != domain.ResourceDeployment {
		t.Fatalf("existence checks = %#v", repo.existsArgs)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "dry-run" || auditor.events[0].Result != "success" {
		t.Fatalf("dry-run audit = %#v", auditor.events)
	}

	repo.requests = nil
	auditor.events = nil
	_, err = service.Apply(context.Background(), domain.Actor{Username: "operator"}, domain.ApplyRequest{YAML: body, DryRun: false})
	if err != nil {
		t.Fatalf("commit Apply() error = %v", err)
	}
	if len(repo.requests) != 2 || !repo.requests[0].DryRun || repo.requests[1].DryRun {
		t.Fatalf("commit requests = %#v", repo.requests)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "apply" || auditor.events[0].Result != "success" {
		t.Fatalf("commit audit = %#v", auditor.events)
	}
}

func TestYAMLApplyDeniedAndDryRunFailureStopsCommit(t *testing.T) {
	repo := &yamlRepository{resource: domain.YAMLResource{Name: "api", Namespace: "prod"}, exists: true}
	authorizer := &yamlAuthorizer{err: domain.ErrForbidden}
	auditor := &yamlAuditor{}
	service := New(repo, authorizer, auditor)
	body := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api\n  namespace: prod\n"

	if _, err := service.Apply(context.Background(), domain.Actor{}, domain.ApplyRequest{YAML: body}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Apply() error = %v, want forbidden", err)
	}
	if len(repo.requests) != 0 || len(auditor.events) != 1 || auditor.events[0].Result != "denied" {
		t.Fatalf("denied requests = %#v audit = %#v", repo.requests, auditor.events)
	}
	if len(authorizer.accesses) != 2 || authorizer.accesses[0].Verb != "update" || authorizer.accesses[1].Verb != "patch" {
		t.Fatalf("denied authorization attempts = %#v", authorizer.accesses)
	}

	authorizer.err = nil
	repo.err = domain.ErrUnavailable
	auditor.events = nil
	if _, err := service.Apply(context.Background(), domain.Actor{}, domain.ApplyRequest{YAML: body}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Apply() error = %v, want unavailable", err)
	}
	if len(repo.requests) != 1 || !repo.requests[0].DryRun {
		t.Fatalf("requests after failed dry-run = %#v", repo.requests)
	}
}

func TestYAMLApplyUsesCreateForNewAndPatchFallbackForExisting(t *testing.T) {
	body := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n"

	createRepo := &yamlRepository{resource: domain.YAMLResource{Name: "app", Namespace: "prod"}}
	createAuthorizer := &yamlAuthorizer{}
	createService := New(createRepo, createAuthorizer, &yamlAuditor{})
	if _, err := createService.Apply(context.Background(), domain.Actor{Username: "operator"}, domain.ApplyRequest{YAML: body, DryRun: true}); err != nil {
		t.Fatalf("create Apply() error = %v", err)
	}
	if len(createAuthorizer.accesses) != 1 || createAuthorizer.accesses[0].Verb != "create" {
		t.Fatalf("create authorization = %#v", createAuthorizer.accesses)
	}

	patchRepo := &yamlRepository{resource: domain.YAMLResource{Name: "app", Namespace: "prod"}, exists: true}
	patchAuthorizer := &yamlAuthorizer{denyVerbs: map[string]bool{"update": true}}
	patchService := New(patchRepo, patchAuthorizer, &yamlAuditor{})
	if _, err := patchService.Apply(context.Background(), domain.Actor{Username: "operator"}, domain.ApplyRequest{YAML: body, DryRun: true}); err != nil {
		t.Fatalf("patch Apply() error = %v", err)
	}
	if len(patchAuthorizer.accesses) != 2 || patchAuthorizer.accesses[0].Verb != "update" || patchAuthorizer.accesses[1].Verb != "patch" {
		t.Fatalf("existing authorization = %#v", patchAuthorizer.accesses)
	}
}

func TestYAMLApplyFailsClosedWhenExistenceCheckFails(t *testing.T) {
	repo := &yamlRepository{existsErr: domain.ErrUnavailable}
	authorizer := &yamlAuthorizer{}
	auditor := &yamlAuditor{}
	service := New(repo, authorizer, auditor)
	body := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n"

	if _, err := service.Apply(context.Background(), domain.Actor{Username: "operator"}, domain.ApplyRequest{YAML: body}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Apply() error = %v, want unavailable", err)
	}
	if authorizer.calls != 0 || len(repo.requests) != 0 {
		t.Fatalf("authorization calls = %d requests = %#v", authorizer.calls, repo.requests)
	}
	if len(auditor.events) != 1 || auditor.events[0].Result != "failure" {
		t.Fatalf("audit = %#v", auditor.events)
	}
}

func TestTargetForYAMLValidationAndResourceMapping(t *testing.T) {
	valid := map[string]struct {
		resourceType domain.ResourceType
		access       domain.Access
	}{
		"apiVersion: v1\nkind: Pod\nmetadata: {name: api, namespace: prod}\n":                       {domain.ResourcePod, domain.Access{Resource: "pods", Namespace: "prod", Name: "api"}},
		"apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: job, namespace: prod}\n":             {domain.ResourceCronJob, domain.Access{APIGroup: "batch", Resource: "cronjobs", Namespace: "prod", Name: "job"}},
		"apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata: {name: api, namespace: prod}\n": {domain.ResourceIngress, domain.Access{APIGroup: "networking.k8s.io", Resource: "ingresses", Namespace: "prod", Name: "api"}},
		"apiVersion: storage.k8s.io/v1\nkind: StorageClass\nmetadata: {name: standard}\n":           {domain.ResourceStorageClass, domain.Access{APIGroup: "storage.k8s.io", Resource: "storageclasses", Name: "standard"}},
		"apiVersion: v1\nkind: PersistentVolume\nmetadata: {name: pv-a}\n":                          {domain.ResourcePersistentVolume, domain.Access{Resource: "persistentvolumes", Name: "pv-a"}},
	}
	for body, want := range valid {
		got, err := targetForYAML(body)
		if err != nil || got.ResourceType != want.resourceType || got.Access != want.access {
			t.Fatalf("targetForYAML(%q) = (%#v, %v), want %#v", body, got, err, want)
		}
	}

	for body, wantErr := range map[string]error{
		"":            domain.ErrInvalid,
		"---\n":       domain.ErrInvalid,
		"not: [valid": domain.ErrInvalid,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: one\n  namespace: two\n":                                                          domain.ErrInvalid,
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: app, namespace: prod}\n---\napiVersion: v1\nkind: Secret\nmetadata: {name: other, namespace: prod}\n": domain.ErrInvalid,
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {namespace: prod}\n":                                                                                         domain.ErrInvalid,
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: app}\n":                                                                                               domain.ErrInvalid,
		"apiVersion: v1\nkind: Node\nmetadata: {name: worker-a}\n":                                                                                               domain.ErrUnsupported,
		"apiVersion: example.com/v1\nkind: Widget\nmetadata: {name: app, namespace: prod}\n":                                                                     domain.ErrUnsupported,
	} {
		if _, err := targetForYAML(body); !errors.Is(err, wantErr) {
			t.Fatalf("targetForYAML(%q) error = %v, want %v", body, err, wantErr)
		}
	}
}

func TestTargetForYAMLRejectsOversizedDocument(t *testing.T) {
	if _, err := targetForYAML(strings.Repeat("a", maxYAMLBytes+1)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("targetForYAML(oversized) error = %v, want ErrInvalid", err)
	}
}
