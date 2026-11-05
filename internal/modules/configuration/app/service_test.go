package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type configurationRepository struct {
	configMaps []domain.ConfigMapSummary
	configMap  domain.ConfigMapDetail
	secrets    []domain.SecretSummary
	reveal     domain.SecretReveal
	calls      int
	err        error
}

func (r *configurationRepository) ListConfigMaps(context.Context, domain.ListQuery) ([]domain.ConfigMapSummary, error) {
	return r.configMaps, r.err
}
func (r *configurationRepository) GetConfigMap(context.Context, string, string) (domain.ConfigMapDetail, error) {
	return r.configMap, r.err
}
func (r *configurationRepository) ListSecrets(context.Context, domain.ListQuery) ([]domain.SecretSummary, error) {
	return r.secrets, r.err
}
func (r *configurationRepository) RevealSecret(context.Context, string, string) (domain.SecretReveal, error) {
	r.calls++
	return r.reveal, r.err
}

type configurationAuthorizer struct {
	err    error
	access domain.Access
	calls  int
}

func (a *configurationAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.calls++
	a.access = access
	return a.err
}

type configurationAuditor struct{ events []domain.AuditEvent }

func (a *configurationAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestConfigurationListsAndRevealsSecret(t *testing.T) {
	repo := &configurationRepository{
		configMaps: []domain.ConfigMapSummary{{Name: "settings"}},
		secrets:    []domain.SecretSummary{{Name: "credentials"}},
		reveal:     domain.SecretReveal{Data: map[string]string{"token": "redacted-in-test"}},
	}
	authorizer := &configurationAuthorizer{}
	auditor := &configurationAuditor{}
	service := New(repo, authorizer, auditor)

	configMaps, err := service.ListConfigMaps(context.Background(), domain.ListQuery{})
	if err != nil || len(configMaps) != 1 {
		t.Fatalf("ListConfigMaps() = (%#v, %v)", configMaps, err)
	}
	secrets, err := service.ListSecrets(context.Background(), domain.ListQuery{})
	if err != nil || len(secrets) != 1 {
		t.Fatalf("ListSecrets() = (%#v, %v)", secrets, err)
	}
	reveal, err := service.RevealSecret(context.Background(), domain.Actor{Username: "admin"}, "prod", "credentials")
	if err != nil || reveal.Data["token"] != "redacted-in-test" {
		t.Fatalf("RevealSecret() = (%#v, %v)", reveal, err)
	}
	wantAccess := domain.Access{Verb: "get", Resource: "secrets", Namespace: "prod", Name: "credentials"}
	if authorizer.access != wantAccess {
		t.Fatalf("authorization access = %#v, want %#v", authorizer.access, wantAccess)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "reveal" || auditor.events[0].Result != "success" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestConfigurationRevealDeniedAndFailure(t *testing.T) {
	repo := &configurationRepository{}
	authorizer := &configurationAuthorizer{err: domain.ErrForbidden}
	auditor := &configurationAuditor{}
	service := New(repo, authorizer, auditor)
	if _, err := service.RevealSecret(context.Background(), domain.Actor{}, "prod", "credentials"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("RevealSecret() error = %v, want ErrForbidden", err)
	}
	if repo.calls != 0 {
		t.Fatalf("repository calls = %d after denial, want 0", repo.calls)
	}
	if len(auditor.events) != 1 || auditor.events[0].Result != "denied" {
		t.Fatalf("denied audit events = %#v", auditor.events)
	}

	repo.err = domain.ErrUnavailable
	authorizer.err = nil
	auditor.events = nil
	if _, err := service.RevealSecret(context.Background(), domain.Actor{}, "prod", "credentials"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("RevealSecret() error = %v, want ErrUnavailable", err)
	}
	if len(auditor.events) != 1 || auditor.events[0].Result != "failure" {
		t.Fatalf("failure audit events = %#v", auditor.events)
	}
}
