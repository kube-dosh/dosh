package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type yamlRepository struct {
	resource domain.YAMLResource
	err      error
	requests []domain.ApplyRequest
}

func (r *yamlRepository) GetYAML(context.Context, domain.ResourceType, string, string) (domain.YAMLResource, error) {
	return r.resource, r.err
}
func (r *yamlRepository) ApplyYAML(_ context.Context, request domain.ApplyRequest) (domain.YAMLResource, error) {
	r.requests = append(r.requests, request)
	return r.resource, r.err
}

type yamlAuthorizer struct {
	err    error
	access domain.Access
	calls  int
}

func (a *yamlAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.calls++
	a.access = access
	return a.err
}

type yamlAuditor struct{ events []domain.AuditEvent }

func (a *yamlAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestYAMLApplyDryRunAndCommit(t *testing.T) {
	repo := &yamlRepository{resource: domain.YAMLResource{Name: "api", Namespace: "prod"}}
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
	if authorizer.access != (domain.Access{Verb: "patch", APIGroup: "apps", Resource: "deployments", Namespace: "prod", Name: "api"}) {
		t.Fatalf("authorization access = %#v", authorizer.access)
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
	repo := &yamlRepository{resource: domain.YAMLResource{Name: "api", Namespace: "prod"}}
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

func TestAccessForYAMLValidationAndResourceMapping(t *testing.T) {
	valid := map[string]domain.Access{
		"apiVersion: v1\nkind: Pod\nmetadata: {name: api, namespace: prod}\n":                       {Verb: "patch", Resource: "pods", Namespace: "prod", Name: "api"},
		"apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: job, namespace: prod}\n":             {Verb: "patch", APIGroup: "batch", Resource: "cronjobs", Namespace: "prod", Name: "job"},
		"apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata: {name: api, namespace: prod}\n": {Verb: "patch", APIGroup: "networking.k8s.io", Resource: "ingresses", Namespace: "prod", Name: "api"},
		"apiVersion: storage.k8s.io/v1\nkind: StorageClass\nmetadata: {name: standard}\n":           {Verb: "patch", APIGroup: "storage.k8s.io", Resource: "storageclasses", Name: "standard"},
		"apiVersion: v1\nkind: PersistentVolume\nmetadata: {name: pv-a}\n":                          {Verb: "patch", Resource: "persistentvolumes", Name: "pv-a"},
	}
	for body, want := range valid {
		got, err := accessForYAML(body)
		if err != nil || got != want {
			t.Fatalf("accessForYAML(%q) = (%#v, %v), want %#v", body, got, err, want)
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
		if _, err := accessForYAML(body); !errors.Is(err, wantErr) {
			t.Fatalf("accessForYAML(%q) error = %v, want %v", body, err, wantErr)
		}
	}
}
