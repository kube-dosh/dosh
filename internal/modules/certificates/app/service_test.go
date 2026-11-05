package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type certificateRepository struct {
	status      domain.ServerCertificate
	reload      domain.ServerCertificate
	statusErr   error
	reloadErr   error
	reloadCalls int
}

func (r *certificateRepository) Status(context.Context) (domain.ServerCertificate, error) {
	return r.status, r.statusErr
}

func (r *certificateRepository) Reload(context.Context) (domain.ServerCertificate, error) {
	r.reloadCalls++
	return r.reload, r.reloadErr
}

type certificateAuthorizer struct {
	err    error
	access domain.Access
}

func (a *certificateAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.access = access
	return a.err
}

type certificateAuditor struct{ events []domain.AuditEvent }

func (a *certificateAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestServiceStatusAndReload(t *testing.T) {
	repo := &certificateRepository{
		status: domain.ServerCertificate{Mode: "https", Enabled: true, Status: "valid"},
		reload: domain.ServerCertificate{Mode: "https", Enabled: true, Status: "valid", FingerprintSHA256: "new"},
	}
	authorizer := &certificateAuthorizer{}
	auditor := &certificateAuditor{}
	service := New(repo, authorizer, auditor)

	status, err := service.Status(context.Background())
	if err != nil || status.Status != "valid" {
		t.Fatalf("Status() = (%#v, %v)", status, err)
	}
	reloaded, err := service.Reload(context.Background(), domain.Actor{Username: "admin", Admin: true})
	if err != nil || reloaded.FingerprintSHA256 != "new" || repo.reloadCalls != 1 {
		t.Fatalf("Reload() = (%#v, %v), calls=%d", reloaded, err, repo.reloadCalls)
	}
	if authorizer.access != (domain.Access{Verb: "update", APIGroup: "dosh.io", Resource: "server-certificates"}) {
		t.Fatalf("access = %#v", authorizer.access)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "reload" || auditor.events[0].Result != "success" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestServiceReloadDeniedAndFailure(t *testing.T) {
	repo := &certificateRepository{}
	authorizer := &certificateAuthorizer{err: domain.ErrForbidden}
	auditor := &certificateAuditor{}
	service := New(repo, authorizer, auditor)
	if _, err := service.Reload(context.Background(), domain.Actor{Username: "viewer"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Reload() error = %v, want ErrForbidden", err)
	}
	if repo.reloadCalls != 0 || len(auditor.events) != 1 || auditor.events[0].Result != "denied" {
		t.Fatalf("denied calls=%d events=%#v", repo.reloadCalls, auditor.events)
	}

	authorizer.err = nil
	repo.reloadErr = domain.ErrUnavailable
	auditor.events = nil
	if _, err := service.Reload(context.Background(), domain.Actor{Username: "admin", Admin: true}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Reload() error = %v, want ErrUnavailable", err)
	}
	if len(auditor.events) != 1 || auditor.events[0].Result != "failure" {
		t.Fatalf("failure events = %#v", auditor.events)
	}
}

func TestServiceWithoutRepositoryReportsHTTPAndRejectsReload(t *testing.T) {
	authorizer := &certificateAuthorizer{}
	auditor := &certificateAuditor{}
	service := New(nil, authorizer, auditor)
	status, err := service.Status(context.Background())
	if err != nil || status.Mode != "http" || status.Status != "disabled" || status.Configured {
		t.Fatalf("Status() = (%#v, %v)", status, err)
	}
	if _, err := service.Reload(context.Background(), domain.Actor{Username: "admin", Admin: true}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Reload() error = %v, want ErrUnsupported", err)
	}
}
