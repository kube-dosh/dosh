package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
	"k8s.io/apimachinery/pkg/api/resource"
)

type fakeVPARepository struct {
	fakeRepository
	items     []domain.VerticalAutoscalerSummary
	listErr   error
	upsertErr error
	deleteErr error
	dryRuns   []bool
	lastSpec  domain.VerticalAutoscalerSpec
	deleteRV  string
}

func (r *fakeVPARepository) ListVerticalAutoscalers(context.Context, string) ([]domain.VerticalAutoscalerSummary, error) {
	return r.items, r.listErr
}

func (r *fakeVPARepository) UpsertVerticalAutoscaler(_ context.Context, spec domain.VerticalAutoscalerSpec, dryRun bool) (domain.VerticalAutoscalerSummary, error) {
	r.dryRuns = append(r.dryRuns, dryRun)
	r.lastSpec = spec
	if r.upsertErr != nil {
		return domain.VerticalAutoscalerSummary{}, r.upsertErr
	}
	return domain.VerticalAutoscalerSummary{
		Name: spec.Name, Namespace: spec.Namespace, ResourceVersion: "7", TargetKind: spec.TargetKind,
		TargetName: spec.TargetName, UpdatePolicy: spec.UpdatePolicy, ContainerPolicies: spec.ContainerPolicies,
	}, nil
}

func (r *fakeVPARepository) DeleteVerticalAutoscaler(_ context.Context, _, _, resourceVersion string) error {
	r.deleteRV = resourceVersion
	return r.deleteErr
}

func (r *fakeVPARepository) ValidateVPAQuantities(spec domain.VerticalAutoscalerSpec) error {
	for _, policy := range spec.ContainerPolicies {
		for field, values := range map[string]map[string]string{"minAllowed": policy.MinAllowed, "maxAllowed": policy.MaxAllowed} {
			for name, raw := range values {
				quantity, err := resource.ParseQuantity(raw)
				if err != nil || quantity.Sign() < 0 {
					return &domain.ValidationError{Field: "containerPolicies", Message: field + "." + name + " is invalid"}
				}
				if field == "maxAllowed" {
					if minimum, ok := policy.MinAllowed[name]; ok {
						parsed, err := resource.ParseQuantity(minimum)
						if err == nil && parsed.Cmp(quantity) > 0 {
							return &domain.ValidationError{Field: "containerPolicies", Message: "minAllowed exceeds maxAllowed"}
						}
					}
				}
			}
		}
	}
	return nil
}

func TestUpsertVerticalAutoscalerDryRunsAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	repository := &fakeVPARepository{}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	service := New(repository, authorizer, auditor)
	minReplicas := int32(2)
	item, err := service.UpsertVerticalAutoscaler(context.Background(), domain.Actor{Username: "alice"}, domain.VerticalAutoscalerSpec{
		Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", UpdatePolicy: "Auto", MinReplicas: &minReplicas,
		ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "api", Mode: "Auto", MinAllowed: map[string]string{"CPU": "100m"}, MaxAllowed: map[string]string{"cpu": "2"}, ControlledResources: []string{"memory", "CPU", "memory"}}},
	})
	if err != nil {
		t.Fatalf("UpsertVerticalAutoscaler() error = %v", err)
	}
	if item.Name != "api" || len(repository.dryRuns) != 2 || !repository.dryRuns[0] || repository.dryRuns[1] {
		t.Fatalf("item = %#v dryRuns = %#v", item, repository.dryRuns)
	}
	if repository.lastSpec.TargetAPIVersion != "apps/v1" || repository.lastSpec.ContainerPolicies[0].MinAllowed["cpu"] != "100m" || len(repository.lastSpec.ContainerPolicies[0].ControlledResources) != 2 {
		t.Fatalf("normalized spec = %#v", repository.lastSpec)
	}
	wantAccess := domain.Access{Verb: "create", APIGroup: "autoscaling.k8s.io", Resource: "verticalpodautoscalers", Namespace: "prod", Name: "api"}
	if len(authorizer.accesses) != 1 || authorizer.accesses[0] != wantAccess {
		t.Fatalf("authorization = %#v, want %#v", authorizer.accesses, wantAccess)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "create-vertical-autoscaler" || auditor.results[0] != "success" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}
}

func TestUpsertVerticalAutoscalerRejectsInvalidPolicyBeforeAuthorization(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]domain.VerticalAutoscalerSpec{
		"unsupported kind":       {Name: "api", Namespace: "prod", TargetKind: "Job", TargetName: "api"},
		"unsupported update":     {Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", UpdatePolicy: "Sometimes"},
		"min replicas below one": {Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", MinReplicas: int32Pointer(0)},
		"duplicate containers":   {Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "app"}, {ContainerName: "app"}}},
		"min above max":          {Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "app", MinAllowed: map[string]string{"cpu": "2"}, MaxAllowed: map[string]string{"cpu": "1"}}}},
		"invalid quantity":       {Name: "api", Namespace: "prod", TargetKind: "Deployment", TargetName: "api", ContainerPolicies: []domain.VPAContainerPolicy{{ContainerName: "app", MinAllowed: map[string]string{"memory": "nope"}}}},
	} {
		repository := &fakeVPARepository{}
		authorizer := &recordingAuthorizer{}
		auditor := &recordingAuditor{}
		service := New(repository, authorizer, auditor)
		if _, err := service.UpsertVerticalAutoscaler(context.Background(), domain.Actor{Username: "alice"}, spec); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%s error = %v, want invalid", name, err)
		}
		if len(repository.dryRuns) != 0 || len(authorizer.accesses) != 0 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
			t.Fatalf("%s dryRuns=%#v accesses=%#v audits=%#v", name, repository.dryRuns, authorizer.accesses, auditor.results)
		}
	}
}

func TestDeleteVerticalAutoscalerRequiresResourceVersion(t *testing.T) {
	t.Parallel()
	repository := &fakeVPARepository{}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	service := New(repository, authorizer, auditor)
	if err := service.DeleteVerticalAutoscaler(context.Background(), domain.Actor{Username: "alice"}, "prod", "api", "  "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty resourceVersion error = %v, want invalid", err)
	}
	if len(authorizer.accesses) != 0 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("empty resourceVersion reached authorization: %#v %#v", authorizer.accesses, auditor.results)
	}
	if err := service.DeleteVerticalAutoscaler(context.Background(), domain.Actor{Username: "alice"}, "prod", "api", "11"); err != nil {
		t.Fatalf("DeleteVerticalAutoscaler() error = %v", err)
	}
	if repository.deleteRV != "11" {
		t.Fatalf("delete resourceVersion = %q", repository.deleteRV)
	}
}

func TestVerticalAutoscalerCRDUnavailableIsPropagated(t *testing.T) {
	t.Parallel()
	repository := &fakeVPARepository{listErr: domain.ErrUnsupported}
	service := New(repository, authz.NewLocal(false), &recordingAuditor{})
	if _, err := service.ListVerticalAutoscalers(context.Background(), domain.Actor{Username: "admin", Admin: true}, "prod"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("ListVerticalAutoscalers() error = %v, want unsupported", err)
	}
	base := &fakeRepository{}
	unsupported := New(base, authz.NewLocal(false), &recordingAuditor{})
	if _, err := unsupported.ListVerticalAutoscalers(context.Background(), domain.Actor{Username: "admin", Admin: true}, "prod"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("repository without VPA support error = %v, want unsupported", err)
	}
}

func int32Pointer(value int32) *int32 { return &value }
