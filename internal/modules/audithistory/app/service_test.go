package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type fakeRepository struct {
	records []domain.AuditRecord
	err     error
	query   domain.AuditQuery
}

type auditingRepository struct {
	fakeRepository
	events []domain.AuditEvent
}

func (f *auditingRepository) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	f.events = append(f.events, event)
}

type fakeAuthorizer struct {
	access domain.Access
	err    error
}

func (f *fakeAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	f.access = access
	return f.err
}

func (f *fakeRepository) ListHistory(_ context.Context, query domain.AuditQuery) ([]domain.AuditRecord, error) {
	f.query = query
	return f.records, f.err
}

func TestListRequiresAuthenticatedAdmin(t *testing.T) {
	service := New(&fakeRepository{}, &fakeAuthorizer{err: domain.ErrForbidden})
	if _, err := service.List(context.Background(), domain.AuditQuery{}); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("List() error = %v, want unauthorized", err)
	}
	nonAdmin := domain.WithActor(context.Background(), domain.Actor{Username: "viewer"})
	if _, err := service.List(nonAdmin, domain.AuditQuery{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("List(non-admin) error = %v, want forbidden", err)
	}
}

func TestListDelegatesQueryForAdmin(t *testing.T) {
	repository := &fakeRepository{records: []domain.AuditRecord{{ID: 1, Action: "scale"}}}
	authorizer := &fakeAuthorizer{}
	service := New(repository, authorizer)
	query := domain.AuditQuery{Namespace: "prod", Search: "api", Action: "scale", Result: "success"}
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "admin", Admin: true})

	records, err := service.List(ctx, query)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || repository.query != query {
		t.Fatalf("records = %#v, query = %#v", records, repository.query)
	}
	if authorizer.access.APIGroup != "dosh.io" || authorizer.access.Resource != "audit-events" || authorizer.access.Verb != "get" {
		t.Fatalf("authorization access = %#v", authorizer.access)
	}
}

func TestListPropagatesAuthorizationFailure(t *testing.T) {
	service := New(&fakeRepository{}, &fakeAuthorizer{err: domain.ErrForbidden})
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "viewer"})
	if _, err := service.List(ctx, domain.AuditQuery{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("List() error = %v, want forbidden", err)
	}
}

func TestActionsReturnsSortedDistinctDirectory(t *testing.T) {
	repository := &fakeRepository{records: []domain.AuditRecord{
		{ID: 1, Action: "scale"}, {ID: 2, Action: "diagnose"}, {ID: 3, Action: "scale"}, {ID: 4, Action: " "},
	}}
	service := New(repository, &fakeAuthorizer{})
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "admin", Admin: true})

	actions, err := service.Actions(ctx)
	if err != nil {
		t.Fatalf("Actions() error = %v", err)
	}
	if len(actions) != 2 || actions[0] != "diagnose" || actions[1] != "scale" {
		t.Fatalf("actions = %#v", actions)
	}
	if repository.query != (domain.AuditQuery{}) {
		t.Fatalf("query = %#v, want unfiltered", repository.query)
	}
}

func TestActionsRequiresAuthenticationAndPermission(t *testing.T) {
	service := New(&fakeRepository{}, &fakeAuthorizer{err: domain.ErrForbidden})
	if _, err := service.Actions(context.Background()); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Actions() error = %v, want unauthorized", err)
	}
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "viewer"})
	if _, err := service.Actions(ctx); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Actions(non-admin) error = %v, want forbidden", err)
	}
}

func TestExportRecordsSuccessfulAndDeniedAuditEvents(t *testing.T) {
	repository := &auditingRepository{fakeRepository: fakeRepository{records: []domain.AuditRecord{{ID: 1, Action: "scale"}}}}
	service := New(repository, &fakeAuthorizer{})
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "admin", Admin: true})

	records, err := service.Export(ctx, domain.AuditQuery{Namespace: "prod", Action: "scale"}, "csv")
	if err != nil || len(records) != 1 {
		t.Fatalf("Export() = (%#v, %v)", records, err)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %#v", repository.events)
	}
	event := repository.events[0]
	if event.Action != "export-audit" || event.Resource != "audit-events" || event.Namespace != "prod" || event.Result != "success" || event.Details["format"] != "csv" || event.Details["count"] != 1 {
		t.Fatalf("event = %#v", event)
	}

	deniedRepository := &auditingRepository{}
	denied := New(deniedRepository, &fakeAuthorizer{err: domain.ErrForbidden})
	if _, err := denied.Export(ctx, domain.AuditQuery{}, "json"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Export(denied) error = %v", err)
	}
	if len(deniedRepository.events) != 1 || deniedRepository.events[0].Result != "denied" || deniedRepository.events[0].Details["format"] != "json" {
		t.Fatalf("denied events = %#v", deniedRepository.events)
	}
}
