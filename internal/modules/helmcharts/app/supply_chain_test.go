package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
)

func testPublicKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
}

func TestNormalizeSupplyChainPolicy(t *testing.T) {
	t.Parallel()
	publicKey := testPublicKeyPEM(t)
	if _, err := normalizeSupplyChainPolicy(false, nil, &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainKey}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("HTTP key policy error = %v, want invalid", err)
	}
	current := &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainKey, PublicKeys: []string{publicKey}}
	inherited, err := normalizeSupplyChainPolicy(true, current, nil)
	if err != nil || inherited == nil || inherited.PublicKeys[0] != publicKey {
		t.Fatalf("inherited policy = %#v, %v", inherited, err)
	}
	inherited.PublicKeys[0] = "mutated"
	if current.PublicKeys[0] != publicKey {
		t.Fatal("normalizeSupplyChainPolicy() reused caller-owned key slice")
	}
	policy, err := normalizeSupplyChainPolicy(true, nil, &domain.HelmSupplyChainPolicy{
		Mode: domain.HelmSupplyChainKey, PublicKeys: []string{"  " + publicKey + "  "},
	})
	if err != nil || strings.TrimSpace(policy.PublicKeys[0]) != strings.TrimSpace(publicKey) {
		t.Fatalf("valid key policy = %#v, %v", policy, err)
	}
	for name, input := range map[string]*domain.HelmSupplyChainPolicy{
		"key without key":          {Mode: domain.HelmSupplyChainKey},
		"invalid pem":              {Mode: domain.HelmSupplyChainKey, PublicKeys: []string{"not-pem"}},
		"key with identity":        {Mode: domain.HelmSupplyChainKey, PublicKeys: []string{publicKey}, Identities: []domain.HelmSupplyChainIdentity{{Subject: "user@example.com"}}},
		"keyless no identity":      {Mode: domain.HelmSupplyChainKeyless},
		"keyless bad regex":        {Mode: domain.HelmSupplyChainKeyless, Identities: []domain.HelmSupplyChainIdentity{{SubjectRegExp: "["}}},
		"attestation no predicate": {Mode: domain.HelmSupplyChainKey, PublicKeys: []string{publicKey}, RequireAttestation: true},
	} {
		if _, err := normalizeSupplyChainPolicy(true, nil, input); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%s error = %v, want invalid", name, err)
		}
	}
	keyless, err := normalizeSupplyChainPolicy(true, nil, &domain.HelmSupplyChainPolicy{
		Mode:       domain.HelmSupplyChainKeyless,
		Identities: []domain.HelmSupplyChainIdentity{{Issuer: "https://issuer.example.com", SubjectRegExp: "^repo:acme/.*$"}},
	})
	if err != nil || keyless == nil || keyless.Identities[0].Issuer != "https://issuer.example.com" {
		t.Fatalf("keyless policy = %#v, %v", keyless, err)
	}
	if disabled, err := normalizeSupplyChainPolicy(true, current, &domain.HelmSupplyChainPolicy{Mode: " DISABLED "}); err != nil || disabled != nil {
		t.Fatalf("disabled policy = %#v, %v", disabled, err)
	}
}

func TestNormalizeIdempotencyKey(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"abc", "request-123:retry", "A.b_c"} {
		if got, err := normalizeIdempotencyKey("  " + value + " "); err != nil || got != value {
			t.Fatalf("normalizeIdempotencyKey(%q) = %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"-leading", "contains space", strings.Repeat("a", 129)} {
		if _, err := normalizeIdempotencyKey(value); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("normalizeIdempotencyKey(%q) error = %v", value, err)
		}
	}
}

type blockingHelmExecutor struct {
	fakeHelmExecutor
	started atomic.Int32
	release chan struct{}
	entered chan struct{}
}

type supplyChainHelmSource struct {
	fakeHelmSource
	calls  atomic.Int32
	status domain.HelmSupplyChainStatus
	err    error
}

func (s *supplyChainHelmSource) VerifySupplyChain(context.Context, domain.HelmRepository, string, string) (domain.HelmSupplyChainStatus, error) {
	s.calls.Add(1)
	return s.status, s.err
}

func newBlockingHelmExecutor() *blockingHelmExecutor {
	return &blockingHelmExecutor{release: make(chan struct{}), entered: make(chan struct{}, 4)}
}

func (e *blockingHelmExecutor) InstallHelmChart(_ context.Context, _ domain.Actor, _ []byte, request domain.HelmChartPlanRequest, _ domain.HelmExecutionOptions) (int, error) {
	e.installs++
	e.lastInstallRequest = request
	e.started.Add(1)
	e.entered <- struct{}{}
	<-e.release
	return 1, nil
}

func TestInstallIdempotencyConcurrentSameKeyExecutesOnce(t *testing.T) {
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}
	executor := newBlockingHelmExecutor()
	executor.template = domain.HelmChartPlanTemplate{
		Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", Values: "replicas: 1\n",
		Resources: []domain.HelmChartPlanResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "demo", Resource: "configmaps", Action: "create"}},
	}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0"}, Namespace: "default", ReleaseName: "demo"}
	request.Mode = "install"
	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	operationRequest := domain.HelmChartOperationRequest{HelmChartPlanRequest: request, PlanDigest: plan.PlanDigest, Confirmation: "demo", IdempotencyKey: "install-concurrent-1"}
	start := make(chan struct{})
	results := make(chan domain.HelmChartOperation, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			operation, installErr := service.Install(context.Background(), testHelmActor(), operationRequest)
			results <- operation
			errs <- installErr
		}()
	}
	close(start)
	first, second := <-results, <-results
	if err := <-errs; err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("second Install() error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("concurrent idempotent operations = %s / %s", first.ID, second.ID)
	}
	if executor.started.Load() != 1 {
		t.Fatalf("executor installs = %d, want 1", executor.started.Load())
	}
	select {
	case <-executor.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("install executor did not start")
	}
	close(executor.release)
	finished := waitForHelmOperation(t, repository, first.ID)
	if finished.Status != domain.HelmOperationSuccess {
		t.Fatalf("operation = %#v", finished)
	}
}

func TestInstallIdempotencyRejectsReusedKeyWithDifferentDigest(t *testing.T) {
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}
	executor := &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", Values: "replicas: 1\n"}}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0"}, Mode: "install", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1\n"}
	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	first, err := service.Install(context.Background(), testHelmActor(), domain.HelmChartOperationRequest{HelmChartPlanRequest: request, PlanDigest: plan.PlanDigest, Confirmation: "demo", IdempotencyKey: "install-reused-1"})
	if err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	_ = waitForHelmOperation(t, repository, first.ID)
	changed := request
	changed.Version = "1.1.0"
	changedPlan, err := service.Plan(context.Background(), testHelmActor(), changed)
	if err != nil {
		t.Fatalf("Plan(changed) error = %v", err)
	}
	if _, err := service.Install(context.Background(), testHelmActor(), domain.HelmChartOperationRequest{HelmChartPlanRequest: changed, PlanDigest: changedPlan.PlanDigest, Confirmation: "demo", IdempotencyKey: "install-reused-1"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("reused key error = %v, want conflict", err)
	}
	if executor.installs != 1 {
		t.Fatalf("executor installs = %d, want 1", executor.installs)
	}
}

func TestGeneratedIdempotencyKeyRetriesAfterFailure(t *testing.T) {
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}
	executor := &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", Values: "replicas: 1\n"}, installErr: errors.New("temporary failure")}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0"}, Mode: "install", Namespace: "default", ReleaseName: "demo"}
	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	operationRequest := domain.HelmChartOperationRequest{HelmChartPlanRequest: request, PlanDigest: plan.PlanDigest, Confirmation: "demo"}
	first, err := service.Install(context.Background(), testHelmActor(), operationRequest)
	if err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	if failed := waitForHelmOperation(t, repository, first.ID); failed.Status != domain.HelmOperationFailed {
		t.Fatalf("first operation = %#v", failed)
	}
	executor.installErr = nil
	second, err := service.Install(context.Background(), testHelmActor(), operationRequest)
	if err != nil {
		t.Fatalf("retry Install() error = %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("retry reused failed operation %s", first.ID)
	}
	if finished := waitForHelmOperation(t, repository, second.ID); finished.Status != domain.HelmOperationSuccess {
		t.Fatalf("retry operation = %#v", finished)
	}
	if executor.installs != 2 {
		t.Fatalf("executor installs = %d, want 2", executor.installs)
	}
}

func TestFindIdempotentOperationRejectsDifferentOperationType(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{operations: map[string]domain.HelmChartOperation{
		"op-1": {ID: "op-1", Type: "install", Status: domain.HelmOperationSuccess, Namespace: "default", ReleaseName: "demo", IdempotencyKey: "same-key", RequestDigest: "sha256:digest", StartedAt: time.Now().UTC()},
	}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	if _, err := service.findIdempotentOperation(context.Background(), "uninstall", "default", "demo", "same-key", "sha256:digest"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("operation type conflict = %v, want conflict", err)
	}
}

func TestPlanEnforcesSupplyChainVerifierAndReturnsAttestationStatus(t *testing.T) {
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-oci", Name: "OCI", URL: "oci://registry.example.com/team/charts/demo", Enabled: true,
		AuthType: domain.HelmRepositoryAuthAnonymous, SupplyChain: &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainKey},
	}}}
	executor := &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", Values: "{}"}}
	source := &supplyChainHelmSource{fakeHelmSource: fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, status: domain.HelmSupplyChainStatus{
		Policy: domain.HelmSupplyChainKey, Verified: true, SignatureVerified: true, AttestationVerified: true,
		ManifestDigest: "sha256:manifest", ChartDigest: "sha256:abc", Signer: "public-key", KeyFingerprint: "sha256:key",
	}}
	service := New(repository, source, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{RepositoryID: "repo-oci", ChartName: "demo", Version: "1.0.0"}, Mode: "install", Namespace: "default", ReleaseName: "demo"}
	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if source.calls.Load() != 1 || plan.SupplyChain == nil || !plan.SupplyChain.AttestationVerified || plan.SupplyChain.KeyFingerprint != "sha256:key" {
		t.Fatalf("plan supply chain = %#v, calls = %d", plan.SupplyChain, source.calls.Load())
	}

	source.err = domain.ErrForbidden
	if _, err := service.Plan(context.Background(), testHelmActor(), request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Plan() untrusted Chart error = %v, want forbidden", err)
	}
	source.err = nil
	source.status.Verified = true
	source.status.ChartDigest = "sha256:different"
	if _, err := service.Plan(context.Background(), testHelmActor(), request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Plan() mutated Chart package error = %v, want forbidden", err)
	}
}

func TestRepositoryPersistsOCIOnlySupplyChainPolicy(t *testing.T) {
	repository := &memoryHelmRepository{}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	publicKey := testPublicKeyPEM(t)
	created, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "OCI", URL: "oci://registry.example.com/team/demo", AuthType: domain.HelmRepositoryAuthAnonymous,
		SupplyChain: &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainKey, PublicKeys: []string{publicKey}, RequireAttestation: true, AttestationPredicateType: "https://slsa.dev/provenance/v1"},
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if created.SupplyChain == nil || created.SupplyChain.Mode != domain.HelmSupplyChainKey || len(repository.items[0].SupplyChain.PublicKeys) != 1 {
		t.Fatalf("created supply chain = %#v", created.SupplyChain)
	}
	if _, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "HTTP", URL: "https://charts.example.com", SupplyChain: &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainKey, PublicKeys: []string{publicKey}},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("HTTP repository supply-chain error = %v, want invalid", err)
	}
}

func TestVerifyChartDigestBindingRejectsMissingOrMismatchedDigests(t *testing.T) {
	t.Parallel()
	status := domain.HelmSupplyChainStatus{Policy: domain.HelmSupplyChainKey, Verified: true, ChartDigest: "SHA256:ABC"}
	if err := verifyChartDigestBinding(status, "sha256:abc"); err != nil {
		t.Fatalf("case-insensitive digest binding error = %v", err)
	}
	for name, candidate := range map[string]domain.HelmSupplyChainStatus{
		"unverified":        {Policy: domain.HelmSupplyChainKey, ChartDigest: "sha256:abc"},
		"missing chart":     {Policy: domain.HelmSupplyChainKey, Verified: true},
		"missing package":   status,
		"different package": status,
	} {
		digest := "sha256:abc"
		if name == "missing package" {
			digest = ""
		}
		if name == "different package" {
			digest = "sha256:def"
		}
		if err := verifyChartDigestBinding(candidate, digest); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("%s binding error = %v, want forbidden", name, err)
		}
	}
	if err := verifyChartDigestBinding(domain.HelmSupplyChainStatus{Policy: domain.HelmSupplyChainDisabled}, ""); err != nil {
		t.Fatalf("disabled binding error = %v", err)
	}
}
