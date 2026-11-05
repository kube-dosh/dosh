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
