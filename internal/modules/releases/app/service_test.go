package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"
)

type fakeReleaseRepository struct {
	items              []domain.HelmReleaseSummary
	warnings           []string
	detail             domain.HelmReleaseDetail
	revision           map[int]domain.HelmReleaseDetail
	listErr            error
	getErr             error
	revisionErr        error
	rollbackResult     domain.HelmReleaseRollbackResult
	rollbackErr        error
	lastNS             string
	lastName           string
	lastRevision       int
	lastTargetRevision int
	upgradeRender      domain.HelmReleaseUpgradeRender
	upgradeResult      domain.HelmReleaseUpgradeResult
	upgradeErr         error
	lastValues         string
}

func (f *fakeReleaseRepository) ListHelmReleases(_ context.Context, namespace string) ([]domain.HelmReleaseSummary, []string, error) {
	f.lastNS = namespace
	return f.items, f.warnings, f.listErr
}

func (f *fakeReleaseRepository) GetHelmRelease(_ context.Context, namespace, name string) (domain.HelmReleaseDetail, error) {
	f.lastNS, f.lastName = namespace, name
	return f.detail, f.getErr
}

func (f *fakeReleaseRepository) GetHelmReleaseRevision(_ context.Context, namespace, name string, revision int) (domain.HelmReleaseDetail, error) {
	f.lastNS, f.lastName, f.lastRevision = namespace, name, revision
	if f.revisionErr != nil {
		return domain.HelmReleaseDetail{}, f.revisionErr
	}
	if f.revision != nil {
		return f.revision[revision], nil
	}
	return f.detail, nil
}

func (f *fakeReleaseRepository) RollbackHelmRelease(_ context.Context, namespace, name string, targetRevision int) (domain.HelmReleaseRollbackResult, error) {
	f.lastNS, f.lastName, f.lastTargetRevision = namespace, name, targetRevision
	return f.rollbackResult, f.rollbackErr
}

func (f *fakeReleaseRepository) PreviewHelmUpgrade(_ context.Context, namespace, name, values string) (domain.HelmReleaseUpgradeRender, error) {
	f.lastNS, f.lastName, f.lastValues = namespace, name, values
	return f.upgradeRender, f.upgradeErr
}

func (f *fakeReleaseRepository) UpgradeHelmRelease(_ context.Context, namespace, name, values string) (domain.HelmReleaseUpgradeResult, error) {
	f.lastNS, f.lastName, f.lastValues = namespace, name, values
	return f.upgradeResult, f.upgradeErr
}

type releaseGuard struct {
	err    error
	access domain.Access
}

func (g *releaseGuard) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	g.access = access
	return g.err
}

type releaseAuditor struct {
	actions []string
	results []string
}

func (a *releaseAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.actions = append(a.actions, event.Action)
	a.results = append(a.results, event.Result)
}

func TestListRequiresNamespaceAndSecretReadPermission(t *testing.T) {
	repo := &fakeReleaseRepository{}
	guard := &releaseGuard{err: domain.ErrForbidden}
	auditor := &releaseAuditor{}
	service := New(repo, guard, auditor)
	if _, _, err := service.List(context.Background(), domain.Actor{Username: "viewer"}, " "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank namespace error = %v", err)
	}
	if _, _, err := service.List(context.Background(), domain.Actor{Username: "viewer"}, "apps"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("List() error = %v", err)
	}
	if guard.access != (domain.Access{Verb: "list", Resource: "secrets", Namespace: "apps"}) {
		t.Fatalf("access = %#v", guard.access)
	}
	if repo.lastNS != "" || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("repository calls=%q audit=%#v", repo.lastNS, auditor.results)
	}
}

func TestGetRedactsValuesAndManifestSecrets(t *testing.T) {
	repo := &fakeReleaseRepository{detail: domain.HelmReleaseDetail{
		Values: "password: s3cr3t-value\ntoken: abc-token\nreplicaCount: 2\nnested:\n  apiKey: xyz-key\n",
		Manifest: `apiVersion: v1
kind: Secret
metadata:
  name: app
data:
  password: c2VjcmV0
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      containers:
        - name: app
          image: example/app:1
          env:
            - name: API_TOKEN
              value: plain-token
`,
	}}
	guard := &releaseGuard{}
	auditor := &releaseAuditor{}
	service := New(repo, guard, auditor)
	detail, err := service.Get(context.Background(), domain.Actor{Username: "admin"}, "apps", "web")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	for _, secret := range []string{"s3cr3t-value", "abc-token", "xyz-key", "c2VjcmV0", "plain-token"} {
		if strings.Contains(detail.Values, secret) || strings.Contains(detail.Manifest, secret) {
			t.Fatalf("response leaked %q: values=%q manifest=%q", secret, detail.Values, detail.Manifest)
		}
	}
	if !strings.Contains(detail.Values, "<redacted>") || !strings.Contains(detail.Manifest, "kind: Secret") || !strings.Contains(detail.Manifest, "kind: Deployment") {
		t.Fatalf("redacted response lost structure: %#v", detail)
	}
	if guard.access != (domain.Access{Verb: "list", Resource: "secrets", Namespace: "apps"}) {
		t.Fatalf("access = %#v", guard.access)
	}
	if repo.lastName != "web" || len(auditor.actions) != 1 || auditor.actions[0] != "get-helm-release" || auditor.results[0] != "success" {
		t.Fatalf("repo/audit = %q %#v %#v", repo.lastName, auditor.actions, auditor.results)
	}
}

func TestGetHidesUnparseableManifest(t *testing.T) {
	repo := &fakeReleaseRepository{detail: domain.HelmReleaseDetail{Manifest: "not: [valid"}}
	service := New(repo, &releaseGuard{}, &releaseAuditor{})
	detail, err := service.Get(context.Background(), domain.Actor{Username: "admin"}, "apps", "web")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if strings.Contains(detail.Manifest, "not:") || len(detail.Warnings) != 1 || !strings.Contains(detail.Manifest, "内容已隐藏") {
		t.Fatalf("unsafe manifest = %#v", detail)
	}
}

func TestDiffRedactsBothRevisions(t *testing.T) {
	repo := &fakeReleaseRepository{revision: map[int]domain.HelmReleaseDetail{
		1: {HelmReleaseSummary: domain.HelmReleaseSummary{Revision: 1}, Values: "password: old-secret\n", Manifest: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app-old\ndata:\n  token: b2xk\n"},
		2: {HelmReleaseSummary: domain.HelmReleaseSummary{Revision: 2}, Values: "password: new-secret\nreplicaCount: 3\n", Manifest: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app-new\ndata:\n  token: bmV3\n"},
	}}
	guard := &releaseGuard{}
	auditor := &releaseAuditor{}
	service := New(repo, guard, auditor)
	diff, err := service.Diff(context.Background(), domain.Actor{Username: "admin"}, "apps", "web", 1, 2)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	for _, secret := range []string{"old-secret", "new-secret", "b2xk", "bmV3"} {
		if strings.Contains(diff.ValuesDiff, secret) || strings.Contains(diff.ManifestDiff, secret) {
			t.Fatalf("diff leaked %q: %#v", secret, diff)
		}
	}
	if !strings.Contains(diff.ValuesDiff, "replicaCount") || !strings.Contains(diff.ManifestDiff, "kind: Secret") || len(auditor.actions) != 1 || auditor.actions[0] != "diff-helm-release" {
		t.Fatalf("diff/audit = %#v %#v", diff, auditor.actions)
	}
}

func TestRollbackRequiresConfirmationAndSecretWritePermission(t *testing.T) {
	repo := &fakeReleaseRepository{rollbackResult: domain.HelmReleaseRollbackResult{Revision: 4, AppliedObjects: 2}}
	guard := &releaseGuard{err: domain.ErrForbidden}
	auditor := &releaseAuditor{}
	service := New(repo, guard, auditor)
	if _, err := service.Rollback(context.Background(), domain.Actor{Username: "viewer"}, "apps", "web", 2, "wrong"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("confirmation error = %v", err)
	}
	if _, err := service.Rollback(context.Background(), domain.Actor{Username: "viewer"}, "apps", "web", 2, "web"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Rollback() error = %v", err)
	}
	if guard.access != (domain.Access{Verb: "update", Resource: "secrets", Namespace: "apps"}) || repo.lastTargetRevision != 0 {
		t.Fatalf("access/repository = %#v / %d", guard.access, repo.lastTargetRevision)
	}
	if len(auditor.actions) != 2 || auditor.results[1] != "denied" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}
}

func TestRollbackAuditsSuccessfulRevision(t *testing.T) {
	repo := &fakeReleaseRepository{rollbackResult: domain.HelmReleaseRollbackResult{Namespace: "apps", Name: "web", TargetRevision: 1, Revision: 4, AppliedObjects: 3}}
	guard := &releaseGuard{}
	auditor := &releaseAuditor{}
	service := New(repo, guard, auditor)
	result, err := service.Rollback(context.Background(), domain.Actor{Username: "admin"}, "apps", "web", 1, "web")
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.Revision != 4 || repo.lastTargetRevision != 1 || len(auditor.actions) != 1 || auditor.actions[0] != "rollback-helm-release" || auditor.results[0] != "success" {
		t.Fatalf("result/repository/audit = %#v %d %#v %#v", result, repo.lastTargetRevision, auditor.actions, auditor.results)
	}
}
