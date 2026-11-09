package app

import (
	"context"
	"encoding/base64"
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
	createCM   domain.ConfigMapCreateSpec
	updateCM   domain.ConfigMapUpdateSpec
	deleteCM   string
	createSec  domain.SecretWriteSpec
	updateSec  domain.SecretWriteSpec
	deleteSec  string
}

func (r *configurationRepository) ListConfigMaps(context.Context, domain.ListQuery) ([]domain.ConfigMapSummary, error) {
	return r.configMaps, r.err
}
func (r *configurationRepository) ListConfigMapsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.ConfigMapSummary], error) {
	items, err := r.ListConfigMaps(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *configurationRepository) GetConfigMap(context.Context, string, string) (domain.ConfigMapDetail, error) {
	return r.configMap, r.err
}
func (r *configurationRepository) CreateConfigMap(_ context.Context, spec domain.ConfigMapCreateSpec) (domain.ConfigMapDetail, error) {
	r.createCM = spec
	return r.configMap, r.err
}
func (r *configurationRepository) UpdateConfigMap(_ context.Context, _, _ string, update domain.ConfigMapUpdateSpec) (domain.ConfigMapDetail, error) {
	r.updateCM = update
	return r.configMap, r.err
}
func (r *configurationRepository) DeleteConfigMap(_ context.Context, _, _ string, resourceVersion string) error {
	r.deleteCM = resourceVersion
	return r.err
}
func (r *configurationRepository) ListSecrets(context.Context, domain.ListQuery) ([]domain.SecretSummary, error) {
	return r.secrets, r.err
}
func (r *configurationRepository) ListSecretsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.SecretSummary], error) {
	items, err := r.ListSecrets(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *configurationRepository) RevealSecret(context.Context, string, string) (domain.SecretReveal, error) {
	r.calls++
	return r.reveal, r.err
}
func (r *configurationRepository) CreateSecret(_ context.Context, spec domain.SecretWriteSpec) (domain.SecretSummary, error) {
	r.createSec = spec
	return domain.SecretSummary{Name: spec.Name, Namespace: spec.Namespace}, r.err
}
func (r *configurationRepository) UpdateSecret(_ context.Context, _, _ string, update domain.SecretWriteSpec) (domain.SecretSummary, error) {
	r.updateSec = update
	return domain.SecretSummary{Name: update.Name, Namespace: update.Namespace}, r.err
}
func (r *configurationRepository) DeleteSecret(_ context.Context, _, _ string, resourceVersion string) error {
	r.deleteSec = resourceVersion
	return r.err
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

func TestConfigurationConfigMapWritesValidateAuthAndAudit(t *testing.T) {
	t.Parallel()
	repo := &configurationRepository{configMap: domain.ConfigMapDetail{ConfigMapSummary: domain.ConfigMapSummary{Name: "settings", Namespace: "prod"}}}
	authorizer := &configurationAuthorizer{}
	auditor := &configurationAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}

	if _, err := service.CreateConfigMap(context.Background(), actor, domain.ConfigMapCreateSpec{
		Namespace: "prod", Name: "settings", Data: map[string]string{"mode": "safe"}, Annotations: map[string]string{"owner": "ops"},
	}); err != nil {
		t.Fatalf("CreateConfigMap() error = %v", err)
	}
	if repo.createCM.Name != "settings" || repo.createCM.Data["mode"] != "safe" || repo.createCM.Annotations["owner"] != "ops" {
		t.Fatalf("create configmap spec = %#v", repo.createCM)
	}
	if authorizer.access != (domain.Access{Verb: "create", Resource: "configmaps", Namespace: "prod", Name: "settings"}) {
		t.Fatalf("create access = %#v", authorizer.access)
	}

	if _, err := service.UpdateConfigMap(context.Background(), actor, "prod", "settings", domain.ConfigMapUpdateSpec{
		Data: map[string]string{"mode": "fast"}, ResourceVersion: "11",
	}); err != nil {
		t.Fatalf("UpdateConfigMap() error = %v", err)
	}
	if repo.updateCM.Data["mode"] != "fast" || repo.updateCM.ResourceVersion != "11" {
		t.Fatalf("update configmap spec = %#v", repo.updateCM)
	}
	if authorizer.access != (domain.Access{Verb: "update", Resource: "configmaps", Namespace: "prod", Name: "settings"}) {
		t.Fatalf("update access = %#v", authorizer.access)
	}

	if err := service.DeleteConfigMap(context.Background(), actor, "prod", "settings", "12"); err != nil {
		t.Fatalf("DeleteConfigMap() error = %v", err)
	}
	if repo.deleteCM != "12" {
		t.Fatalf("delete resourceVersion = %q", repo.deleteCM)
	}
	if len(auditor.events) != 3 || auditor.events[0].Action != "create-configmap" || auditor.events[1].Action != "update-configmap" || auditor.events[2].Action != "delete-configmap" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestConfigurationSecretWritesPreserveIdentityAndRejectInvalidData(t *testing.T) {
	t.Parallel()
	encoded := base64.StdEncoding.EncodeToString([]byte("secret-value"))
	repo := &configurationRepository{}
	authorizer := &configurationAuthorizer{}
	auditor := &configurationAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}

	created, err := service.CreateSecret(context.Background(), actor, domain.SecretWriteSpec{
		Namespace: "prod", Name: "credentials", Type: "Opaque", Data: map[string]string{"token": encoded},
	})
	if err != nil || created.Name != "credentials" || created.Namespace != "prod" {
		t.Fatalf("CreateSecret() = (%#v, %v)", created, err)
	}
	if repo.createSec.Namespace != "prod" || repo.createSec.Name != "credentials" || repo.createSec.Data["token"] != encoded {
		t.Fatalf("create secret spec = %#v", repo.createSec)
	}
	if authorizer.access != (domain.Access{Verb: "create", Resource: "secrets", Namespace: "prod", Name: "credentials"}) {
		t.Fatalf("create access = %#v", authorizer.access)
	}

	if _, err := service.UpdateSecret(context.Background(), actor, "prod", "credentials", domain.SecretWriteSpec{
		Type: "Opaque", Data: map[string]string{"token": encoded}, ResourceVersion: "21",
	}); err != nil {
		t.Fatalf("UpdateSecret() error = %v", err)
	}
	if repo.updateSec.Namespace != "prod" || repo.updateSec.Name != "credentials" || repo.updateSec.ResourceVersion != "21" {
		t.Fatalf("update secret spec = %#v", repo.updateSec)
	}

	if err := service.DeleteSecret(context.Background(), actor, "prod", "credentials", "22"); err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}
	if repo.deleteSec != "22" {
		t.Fatalf("delete secret resourceVersion = %q", repo.deleteSec)
	}

	before := authorizer.calls
	if _, err := service.CreateSecret(context.Background(), actor, domain.SecretWriteSpec{
		Namespace: "prod", Name: "invalid", Data: map[string]string{"token": "not-base64"},
	}); err == nil {
		t.Fatal("CreateSecret() accepted invalid base64")
	}
	if authorizer.calls != before {
		t.Fatalf("invalid secret reached authorization: %d -> %d", before, authorizer.calls)
	}
	if len(auditor.events) != 4 || auditor.events[3].Action != "create-secret" || auditor.events[3].Result != "failure" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}
