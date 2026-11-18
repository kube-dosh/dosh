package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
)

type fakeRepository struct {
	autoscalers       []domain.AutoscalerSummary
	disruptionBudgets []domain.DisruptionBudgetSummary
	dryRuns           []bool
	lastHPA           domain.AutoscalerSpec
	lastPDB           domain.DisruptionBudgetSpec
	deleteHPACalls    int
	deletePDBCalls    int
}

func (r *fakeRepository) ListAutoscalers(context.Context, string) ([]domain.AutoscalerSummary, error) {
	return r.autoscalers, nil
}

func (r *fakeRepository) UpsertAutoscaler(_ context.Context, spec domain.AutoscalerSpec, dryRun bool) (domain.AutoscalerSummary, error) {
	r.dryRuns = append(r.dryRuns, dryRun)
	r.lastHPA = spec
	return domain.AutoscalerSummary{Name: spec.Name, Namespace: spec.Namespace, TargetKind: spec.TargetKind, TargetName: spec.TargetName, MinReplicas: spec.MinReplicas, MaxReplicas: spec.MaxReplicas}, nil
}

func (r *fakeRepository) DeleteAutoscaler(context.Context, string, string, string) error {
	r.deleteHPACalls++
	return nil
}

func (r *fakeRepository) ListDisruptionBudgets(context.Context, string) ([]domain.DisruptionBudgetSummary, error) {
	return r.disruptionBudgets, nil
}

func (r *fakeRepository) UpsertDisruptionBudget(_ context.Context, spec domain.DisruptionBudgetSpec, dryRun bool) (domain.DisruptionBudgetSummary, error) {
	r.dryRuns = append(r.dryRuns, dryRun)
	r.lastPDB = spec
	return domain.DisruptionBudgetSummary{Name: spec.Name, Namespace: spec.Namespace, MinAvailable: spec.MinAvailable, MaxUnavailable: spec.MaxUnavailable, Selector: spec.Selector}, nil
}

func (r *fakeRepository) DeleteDisruptionBudget(context.Context, string, string, string) error {
	r.deletePDBCalls++
	return nil
}

type recordingAuditor struct {
	actions []string
	results []string
}

func (a *recordingAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.actions = append(a.actions, event.Action)
	a.results = append(a.results, event.Result)
}

type recordingAuthorizer struct {
	accesses []domain.Access
	err      error
}

func (a *recordingAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.accesses = append(a.accesses, access)
	return a.err
}

func TestUpsertAutoscalerDryRunsAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	service := New(repository, authorizer, auditor)
	cpu, memory := int32(70), int32(80)
	item, err := service.UpsertAutoscaler(context.Background(), domain.Actor{Username: "alice"}, domain.AutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api",
		MinReplicas: 2, MaxReplicas: 8, CPUUtilization: &cpu, MemoryUtilization: &memory,
	})
	if err != nil {
		t.Fatalf("UpsertAutoscaler() error = %v", err)
	}
	if item.Name != "api" || len(repository.dryRuns) != 2 || !repository.dryRuns[0] || repository.dryRuns[1] {
		t.Fatalf("item = %#v dryRuns = %#v", item, repository.dryRuns)
	}
	wantAccess := domain.Access{Verb: "create", APIGroup: "autoscaling", Resource: "horizontalpodautoscalers", Namespace: "prod", Name: "api"}
	if len(authorizer.accesses) != 1 || authorizer.accesses[0] != wantAccess {
		t.Fatalf("authorization = %#v, want %#v", authorizer.accesses, wantAccess)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "create-autoscaler" || auditor.results[0] != "success" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}
}

func TestUpsertAutoscalerRejectsMissingMetricBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	service := New(repository, authorizer, auditor)
	_, err := service.UpsertAutoscaler(context.Background(), domain.Actor{Username: "alice"}, domain.AutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", MinReplicas: 1, MaxReplicas: 4,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Field != "metrics" {
		t.Fatalf("validation = %#v", validation)
	}
	if len(repository.dryRuns) != 0 || len(authorizer.accesses) != 0 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("dryRuns=%#v accesses=%#v audits=%#v", repository.dryRuns, authorizer.accesses, auditor.results)
	}
}

func TestUpsertDisruptionBudgetValidatesAvailabilityAndSelector(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	service := New(repository, authorizer, auditor)
	item, err := service.UpsertDisruptionBudget(context.Background(), domain.Actor{Username: "alice"}, domain.DisruptionBudgetSpec{
		Name: "api", Namespace: "prod", MinAvailable: "50%", Selector: map[string]string{"app": "api"},
	})
	if err != nil {
		t.Fatalf("UpsertDisruptionBudget() error = %v", err)
	}
	if item.MinAvailable != "50%" || len(repository.dryRuns) != 2 || !repository.dryRuns[0] {
		t.Fatalf("item=%#v dryRuns=%#v", item, repository.dryRuns)
	}
	wantAccess := domain.Access{Verb: "create", APIGroup: "policy", Resource: "poddisruptionbudgets", Namespace: "prod", Name: "api"}
	if len(authorizer.accesses) != 1 || authorizer.accesses[0] != wantAccess {
		t.Fatalf("authorization = %#v", authorizer.accesses)
	}

	denied := New(&fakeRepository{}, &recordingAuthorizer{err: domain.ErrForbidden}, &recordingAuditor{})
	if _, err := denied.UpsertDisruptionBudget(context.Background(), domain.Actor{Username: "bob"}, domain.DisruptionBudgetSpec{
		Name: "api", Namespace: "prod", MinAvailable: "1", Selector: map[string]string{"app": "api"},
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied error = %v, want ErrForbidden", err)
	}

	invalid := New(&fakeRepository{}, authz.NewLocal(false), &recordingAuditor{})
	for _, spec := range []domain.DisruptionBudgetSpec{
		{Name: "api", Namespace: "prod", Selector: map[string]string{"app": "api"}},
		{Name: "api", Namespace: "prod", MinAvailable: "1", MaxUnavailable: "1", Selector: map[string]string{"app": "api"}},
		{Name: "api", Namespace: "prod", MinAvailable: "101%", Selector: map[string]string{"app": "api"}},
		{Name: "api", Namespace: "prod", MinAvailable: "1", Selector: map[string]string{}},
	} {
		if _, err := invalid.UpsertDisruptionBudget(context.Background(), domain.Actor{Username: "alice", Admin: true}, spec); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("spec %#v error = %v, want ErrInvalid", spec, err)
		}
	}
}

func TestDeleteAutoscalerAndDisruptionBudgetAreAudited(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	auditor := &recordingAuditor{}
	service := New(repository, authz.NewLocal(false), auditor)
	if err := service.DeleteAutoscaler(context.Background(), domain.Actor{Username: "alice", Admin: true}, "prod", "api", "12"); err != nil {
		t.Fatalf("DeleteAutoscaler() error = %v", err)
	}
	if err := service.DeleteDisruptionBudget(context.Background(), domain.Actor{Username: "alice", Admin: true}, "prod", "api", "7"); err != nil {
		t.Fatalf("DeleteDisruptionBudget() error = %v", err)
	}
	if repository.deleteHPACalls != 1 || repository.deletePDBCalls != 1 {
		t.Fatalf("delete calls = %d %d", repository.deleteHPACalls, repository.deletePDBCalls)
	}
	if len(auditor.actions) != 2 || auditor.actions[0] != "delete-autoscaler" || auditor.actions[1] != "delete-disruption-budget" {
		t.Fatalf("audit = %#v", auditor.actions)
	}
}
