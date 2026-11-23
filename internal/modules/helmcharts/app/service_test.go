package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
)

type memoryHelmRepository struct {
	items      []domain.HelmRepository
	operations map[string]domain.HelmChartOperation
	mu         sync.Mutex
}

type lockingMemoryHelmRepository struct {
	*memoryHelmRepository
	acquireCalls int
	lastTTL      time.Duration
}

func (r *lockingMemoryHelmRepository) AcquireHelmOperationLock(_ context.Context, _, _, _ string, ttl time.Duration) error {
	r.acquireCalls++
	r.lastTTL = ttl
	return nil
}

func (r *lockingMemoryHelmRepository) ReleaseHelmOperationLock(context.Context, string, string, string) error {
	return nil
}

func (r *memoryHelmRepository) ListHelmRepositories(context.Context) ([]domain.HelmRepository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.HelmRepository(nil), r.items...), nil
}

func (r *memoryHelmRepository) GetHelmRepository(_ context.Context, id string) (domain.HelmRepository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.HelmRepository{}, domain.ErrNotFound
}

func (r *memoryHelmRepository) SaveHelmRepository(_ context.Context, repository domain.HelmRepository) (domain.HelmRepository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repository.Revision = "revision-1"
	repository.UpdatedAt = time.Now().UTC()
	repository.HasCredentials = repository.Credentials.Username != "" || repository.Credentials.Password != "" || repository.Credentials.Token != ""
	repository.HasCustomCA = repository.Credentials.CAPEM != ""
	for index := range r.items {
		if r.items[index].ID == repository.ID {
			r.items[index] = repository
			return repository, nil
		}
	}
	r.items = append(r.items, repository)
	return repository, nil
}

func (r *memoryHelmRepository) DeleteHelmRepository(_ context.Context, id, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *memoryHelmRepository) SaveHelmChartOperation(_ context.Context, operation domain.HelmChartOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.operations == nil {
		r.operations = map[string]domain.HelmChartOperation{}
	}
	r.operations[operation.ID] = operation
	return nil
}

func (r *memoryHelmRepository) GetHelmChartOperation(_ context.Context, id string) (domain.HelmChartOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	operation, ok := r.operations[id]
	if !ok {
		return domain.HelmChartOperation{}, domain.ErrNotFound
	}
	return operation, nil
}

func (r *memoryHelmRepository) ListHelmChartOperations(_ context.Context, filter domain.HelmOperationFilter) ([]domain.HelmChartOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domain.HelmChartOperation, 0, len(r.operations))
	for _, operation := range r.operations {
		if filter.Status != "" && operation.Status != filter.Status {
			continue
		}
		if filter.Namespace != "" && operation.Namespace != filter.Namespace {
			continue
		}
		if filter.ReleaseName != "" && operation.ReleaseName != filter.ReleaseName {
			continue
		}
		items = append(items, operation)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].StartedAt.After(items[right].StartedAt) })
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

type fakeHelmSource struct {
	archive []byte
	digest  string
}

type invalidatingHelmSource struct {
	fakeHelmSource
	invalidations int
}

type refreshingHelmSource struct {
	fakeHelmSource
	refreshes int
}

type countingHelmSource struct {
	fakeHelmSource
	chartsCalls atomic.Int32
}

type flakyHelmSource struct {
	fakeHelmSource
	failuresRemaining atomic.Int32
	chartsCalls       atomic.Int32
}

type fakeHelmCache struct {
	fakeHelmSource
	stats   domain.HelmCacheStats
	cleaned int
}

func (s *invalidatingHelmSource) Invalidate(domain.HelmRepository) {
	s.invalidations++
}

func (s *refreshingHelmSource) RefreshChart(_ context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
	s.refreshes++
	return domain.HelmChartDetail{HelmChartSummary: domain.HelmChartSummary{RepositoryID: repository.ID, RepositoryName: repository.Name, Name: chartName, Version: "1.0.0"}, Readme: "refreshed", Values: "{}"}, nil
}

func (s *countingHelmSource) Charts(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	s.chartsCalls.Add(1)
	return s.fakeHelmSource.Charts(ctx, repository)
}

func (s *flakyHelmSource) Charts(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	s.chartsCalls.Add(1)
	if s.failuresRemaining.Add(-1) >= 0 {
		return nil, errors.New("upstream unavailable")
	}
	return s.fakeHelmSource.Charts(ctx, repository)
}

func (s *fakeHelmCache) CacheStats() (domain.HelmCacheStats, error) {
	return s.stats, nil
}

func (s *fakeHelmCache) CleanupCache(maxBytes int64) (domain.HelmCacheCleanupResult, error) {
	s.cleaned++
	s.stats.TotalBytes = maxBytes
	return domain.HelmCacheCleanupResult{Stats: s.stats}, nil
}

func (s *fakeHelmCache) RemoveRepositoryCache(domain.HelmRepository) error { return nil }

func (s fakeHelmSource) Charts(_ context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	return []domain.HelmChartSummary{{RepositoryID: repository.ID, RepositoryName: repository.Name, Name: "demo", Version: "1.0.0"}}, nil
}

func (s fakeHelmSource) Detail(_ context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
	return domain.HelmChartDetail{HelmChartSummary: domain.HelmChartSummary{RepositoryID: repository.ID, RepositoryName: repository.Name, Name: chartName, Version: "1.0.0"}, Values: "{}"}, nil
}

func (s fakeHelmSource) Download(context.Context, domain.HelmRepository, string, string, string) ([]byte, string, error) {
	return s.archive, s.digest, nil
}

type fakeHelmExecutor struct {
	template           domain.HelmChartPlanTemplate
	installs           int
	lastInstallRequest domain.HelmChartPlanRequest
	lastUpgradeRequest domain.HelmChartPlanRequest
	onPlan             func()
	testCalls          int
	testResult         domain.HelmReleaseTestResult
}

type fakeReleaseUpgradeExecutor struct {
	fakeHelmExecutor
	mu                  sync.Mutex
	releaseTemplate     domain.HelmChartPlanTemplate
	releaseRevision     int
	releasePlanCalls    int
	releaseUpgradeCalls int
	lastValues          string
}

type rotatingHelmExecutor struct {
	fakeHelmExecutor
	planCalls int
}

func (e *rotatingHelmExecutor) PlanHelmChart(context.Context, domain.Actor, []byte, domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error) {
	e.planCalls++
	e.template.Manifest = fmt.Sprintf("generated-secret-data-%d", e.planCalls)
	return e.template, nil
}

func (e *fakeHelmExecutor) PlanHelmChart(context.Context, domain.Actor, []byte, domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error) {
	if e.onPlan != nil {
		e.onPlan()
	}
	return e.template, nil
}

func (e *fakeHelmExecutor) InstallHelmChart(_ context.Context, _ domain.Actor, _ []byte, request domain.HelmChartPlanRequest, _ domain.HelmExecutionOptions) (int, error) {
	e.installs++
	e.lastInstallRequest = request
	return 1, nil
}

func (e *fakeHelmExecutor) UpgradeHelmChart(_ context.Context, _ domain.Actor, _ []byte, request domain.HelmChartPlanRequest, _ domain.HelmExecutionOptions) (int, error) {
	e.lastUpgradeRequest = request
	return 2, nil
}

func (e *fakeHelmExecutor) UninstallHelmChart(context.Context, domain.Actor, string, string, domain.HelmExecutionOptions) error {
	return nil
}

func (e *fakeHelmExecutor) RollbackHelmChart(context.Context, domain.Actor, string, string, int, domain.HelmExecutionOptions) (int, error) {
	return 2, nil
}

func (e *fakeHelmExecutor) TestHelmRelease(context.Context, domain.Actor, string, string) (domain.HelmReleaseTestResult, error) {
	e.testCalls++
	return e.testResult, nil
}

func (e *fakeReleaseUpgradeExecutor) PlanHelmReleaseUpgrade(_ context.Context, _ domain.Actor, _ string, _ string, values string) (domain.HelmChartPlanTemplate, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releasePlanCalls++
	e.lastValues = values
	return e.releaseTemplate, nil
}

func (e *fakeReleaseUpgradeExecutor) UpgradeHelmReleaseWithHelm(_ context.Context, _ domain.Actor, _ string, _ string, values string, _ domain.HelmExecutionOptions) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releaseUpgradeCalls++
	e.lastValues = values
	return e.releaseRevision, nil
}

func testHelmActor() domain.Actor { return domain.Actor{Username: "admin", Admin: true} }

func waitForHelmOperation(t *testing.T, repository *memoryHelmRepository, id string) domain.HelmChartOperation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		operation, err := repository.GetHelmChartOperation(context.Background(), id)
		if err != nil {
			t.Fatalf("GetHelmChartOperation() error = %v", err)
		}
		if operation.Status != domain.HelmOperationRunning && operation.Status != domain.HelmOperationPending {
			return operation
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not finish: %#v", id, operation)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCreateRepositoryStripsSecretsAndRecordsAuthType(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	created, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "Private", URL: "https://charts.example.com", AuthType: domain.HelmRepositoryAuthBasic,
		Username: "robot", Password: "secret", CAPEM: "ca-data",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if created.AuthType != domain.HelmRepositoryAuthBasic || !created.HasCredentials || !created.HasCustomCA {
		t.Fatalf("CreateRepository() flags = %#v", created)
	}
	if created.Credentials.Username != "" || created.Credentials.Password != "" || created.Credentials.CAPEM != "" {
		t.Fatalf("CreateRepository() leaked credentials: %#v", created.Credentials)
	}
	stored := repository.items[0]
	if stored.Credentials.Username != "robot" || stored.Credentials.Password != "secret" || stored.Credentials.CAPEM != "ca-data" {
		t.Fatalf("stored credentials = %#v", stored.Credentials)
	}
}

func TestSyncRepositoryInvalidatesSourceCache(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthAnonymous, Revision: "revision-1",
	}}}
	source := &invalidatingHelmSource{}
	service := New(repository, source, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	if _, err := service.SyncRepository(context.Background(), testHelmActor(), "repo-1"); err != nil {
		t.Fatalf("SyncRepository() error = %v", err)
	}
	if source.invalidations != 1 {
		t.Fatalf("SyncRepository() invalidations = %d", source.invalidations)
	}
}

func TestRepositorySyncSettingsDefaultOffAndThreeHours(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	created, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "Public", URL: "https://charts.example.com",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if created.SyncEnabled {
		t.Fatal("CreateRepository() enabled scheduled synchronization by default")
	}
	if created.SyncIntervalSec != int((3 * time.Hour).Seconds()) {
		t.Fatalf("CreateRepository() interval = %d", created.SyncIntervalSec)
	}
	stored := repository.items[0]
	if stored.SyncEnabled || stored.SyncIntervalSec != int((3*time.Hour).Seconds()) {
		t.Fatalf("stored sync settings = %#v", stored)
	}
}

func TestScheduledSyncRunsOnlyDueRepositories(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	old := now.Add(-4 * time.Hour)
	recent := now.Add(-time.Hour)
	repository := &memoryHelmRepository{items: []domain.HelmRepository{
		{ID: "due", Name: "Due", URL: "https://due.example.com", Enabled: true, SyncEnabled: true, SyncIntervalSec: 10800, LastSync: &domain.HelmRepositorySync{OK: true, SyncedAt: old}},
		{ID: "recent", Name: "Recent", URL: "https://recent.example.com", Enabled: true, SyncEnabled: true, SyncIntervalSec: 10800, LastSync: &domain.HelmRepositorySync{OK: true, SyncedAt: recent}},
		{ID: "disabled", Name: "Disabled", URL: "https://disabled.example.com", Enabled: true, SyncEnabled: false, SyncIntervalSec: 10800, LastSync: &domain.HelmRepositorySync{OK: true, SyncedAt: old}},
	}}
	source := &countingHelmSource{}
	service := New(repository, source, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	if attempted := service.SyncDueRepositories(context.Background()); attempted != 1 {
		t.Fatalf("SyncDueRepositories() attempted = %d", attempted)
	}
	deadline := time.Now().Add(2 * time.Second)
	for source.chartsCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if source.chartsCalls.Load() != 1 {
		t.Fatalf("scheduled source calls = %d", source.chartsCalls.Load())
	}
	due, _ := repository.GetHelmRepository(context.Background(), "due")
	if due.LastSync == nil || due.LastSync.Trigger != domain.HelmRepositorySyncTriggerScheduled {
		t.Fatalf("due repository last sync = %#v", due.LastSync)
	}
	if due.NextSyncAt == nil || !due.NextSyncAt.After(now) {
		t.Fatalf("due repository next sync = %v", due.NextSyncAt)
	}
}

func TestScheduledSyncFailureUsesBackoffAndResetsAfterSuccess(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true,
		SyncEnabled: true, SyncIntervalSec: int((3 * time.Hour).Seconds()), LastSync: &domain.HelmRepositorySync{OK: true, SyncedAt: now.Add(-4 * time.Hour), Attempt: 1},
	}}}
	source := &flakyHelmSource{}
	source.failuresRemaining.Store(2)
	service := New(repository, source, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	if _, err := service.refreshRepository(context.Background(), repository.items[0], domain.HelmRepositorySyncTriggerScheduled); err == nil {
		t.Fatal("first refresh unexpectedly succeeded")
	}
	first, _ := repository.GetHelmRepository(context.Background(), "repo-1")
	if first.SyncFailureCount != 1 || first.LastSync == nil || first.LastSync.OK || first.LastSync.Attempt != 1 {
		t.Fatalf("first failure state = %#v", first)
	}
	if first.NextSyncAt == nil || first.NextSyncAt.Sub(first.LastSync.SyncedAt) != helmSyncRetryBaseDelay {
		t.Fatalf("first retry at = %v, last sync = %v", first.NextSyncAt, first.LastSync)
	}

	if _, err := service.refreshRepository(context.Background(), first, domain.HelmRepositorySyncTriggerScheduled); err == nil {
		t.Fatal("second refresh unexpectedly succeeded")
	}
	second, _ := repository.GetHelmRepository(context.Background(), "repo-1")
	if second.SyncFailureCount != 2 || second.NextSyncAt == nil || second.NextSyncAt.Sub(second.LastSync.SyncedAt) != 2*helmSyncRetryBaseDelay {
		t.Fatalf("second retry state = %#v", second)
	}

	if _, err := service.refreshRepository(context.Background(), second, domain.HelmRepositorySyncTriggerScheduled); err != nil {
		t.Fatalf("recovery refresh error = %v", err)
	}
	recovered, _ := repository.GetHelmRepository(context.Background(), "repo-1")
	if recovered.SyncFailureCount != 0 || recovered.LastSync == nil || !recovered.LastSync.OK {
		t.Fatalf("recovered sync state = %#v", recovered)
	}
	if recovered.NextSyncAt == nil || recovered.NextSyncAt.Sub(recovered.LastSync.SyncedAt) != 3*time.Hour {
		t.Fatalf("recovered next sync = %v, last sync = %v", recovered.NextSyncAt, recovered.LastSync)
	}
}

func TestReconcileOperationsFailsOnlyStaleHeartbeats(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	repository := &memoryHelmRepository{operations: map[string]domain.HelmChartOperation{
		"stale":    {ID: "stale", Status: domain.HelmOperationRunning, Namespace: "default", ReleaseName: "demo", StartedAt: now.Add(-10 * time.Minute), HeartbeatAt: now.Add(-5 * time.Minute)},
		"fresh":    {ID: "fresh", Status: domain.HelmOperationRunning, Namespace: "default", ReleaseName: "api", StartedAt: now.Add(-10 * time.Minute), HeartbeatAt: now.Add(-10 * time.Second)},
		"canceled": {ID: "canceled", Status: domain.HelmOperationRunning, Namespace: "default", ReleaseName: "worker", StartedAt: now.Add(-10 * time.Minute), HeartbeatAt: now.Add(-5 * time.Minute), CancelRequested: true},
	}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	reconciled, err := service.ReconcileOperations(context.Background())
	if err != nil || reconciled != 2 {
		t.Fatalf("ReconcileOperations() = %d, %v", reconciled, err)
	}
	stale, _ := repository.GetHelmChartOperation(context.Background(), "stale")
	fresh, _ := repository.GetHelmChartOperation(context.Background(), "fresh")
	canceled, _ := repository.GetHelmChartOperation(context.Background(), "canceled")
	if stale.Status != domain.HelmOperationFailed || fresh.Status != domain.HelmOperationRunning || canceled.Status != domain.HelmOperationCancelled {
		t.Fatalf("reconciled statuses = stale %s, fresh %s, canceled %s", stale.Status, fresh.Status, canceled.Status)
	}
}

func TestCancelOperationPersistsRequestForRemoteOwner(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	repository := &memoryHelmRepository{operations: map[string]domain.HelmChartOperation{
		"remote": {ID: "remote", Type: "upgrade", Status: domain.HelmOperationRunning, Namespace: "default", ReleaseName: "demo", StartedAt: now, HeartbeatAt: now},
	}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	operation, err := service.CancelOperation(context.Background(), testHelmActor(), "remote")
	if err != nil {
		t.Fatalf("CancelOperation() error = %v", err)
	}
	if !operation.CancelRequested || operation.Message == "" {
		t.Fatalf("CancelOperation() = %#v", operation)
	}
	stored, _ := repository.GetHelmChartOperation(context.Background(), "remote")
	if !stored.CancelRequested {
		t.Fatalf("stored cancellation request = %#v", stored)
	}
}

func TestCacheMaintenanceRunsOnlyAboveConfiguredLimit(t *testing.T) {
	t.Parallel()
	cache := &fakeHelmCache{stats: domain.HelmCacheStats{TotalBytes: 2048}}
	service := New(&memoryHelmRepository{}, cache, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	service.SetCacheLimit(1024)
	service.maintainCache()
	if cache.cleaned != 1 {
		t.Fatalf("cache cleanup calls = %d", cache.cleaned)
	}
	cache.stats.TotalBytes = 512
	service.maintainCache()
	if cache.cleaned != 1 {
		t.Fatalf("cache cleanup ran below limit: %d", cache.cleaned)
	}
}

func TestOperationLockRejectsSameRelease(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{ID: "repo-1", Name: "Demo", URL: "https://charts.example.com", Enabled: true}}}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{Values: "{}"}}, authz.NewLocal(false), nil)
	if err := service.acquireOperationLock(context.Background(), "default", "demo", "first"); err != nil {
		t.Fatalf("first lock error = %v", err)
	}
	if err := service.acquireOperationLock(context.Background(), "default", "demo", "second"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second lock error = %v", err)
	}
	service.releaseOperationLock(context.Background(), "default", "demo", "first")
	if err := service.acquireOperationLock(context.Background(), "default", "demo", "second"); err != nil {
		t.Fatalf("lock after release error = %v", err)
	}
}

func TestStartHelmReleaseUpgradeUsesOperationEngineAndRevisionGuard(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{}
	executor := &fakeReleaseUpgradeExecutor{
		releaseTemplate: domain.HelmChartPlanTemplate{
			ChartName: "web", ChartVersion: "1.2.3", CurrentRevision: 2,
			Resources: []domain.HelmChartPlanResource{{
				APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "web",
				Action: "update", APIGroup: "apps", Resource: "deployments",
			}},
			Warnings: []string{"upgrade warning"},
		},
		releaseRevision: 3,
	}
	service := New(repository, fakeHelmSource{}, executor, authz.NewLocal(false), nil)
	operation, err := service.StartHelmReleaseUpgrade(context.Background(), testHelmActor(), domain.HelmReleaseUpgradeOperationRequest{
		Namespace: "default", ReleaseName: "web", Values: "replicaCount: 3\n", Confirmation: "web", CurrentRevision: 2,
	})
	if err != nil {
		t.Fatalf("StartHelmReleaseUpgrade() error = %v", err)
	}
	if operation.Type != "upgrade" || operation.ChartName != "web" || operation.ChartVersion != "1.2.3" || len(operation.Resources) != 1 {
		t.Fatalf("operation = %#v", operation)
	}
	finished := waitForHelmOperation(t, repository, operation.ID)
	if finished.Status != domain.HelmOperationSuccess || finished.Revision != 3 {
		t.Fatalf("finished operation = %#v", finished)
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.releasePlanCalls != 1 || executor.releaseUpgradeCalls != 1 || executor.lastValues != "replicaCount: 3\n" {
		t.Fatalf("executor calls/values = %d/%d/%q", executor.releasePlanCalls, executor.releaseUpgradeCalls, executor.lastValues)
	}

	staleExecutor := &fakeReleaseUpgradeExecutor{releaseTemplate: domain.HelmChartPlanTemplate{CurrentRevision: 1}}
	staleService := New(&memoryHelmRepository{}, fakeHelmSource{}, staleExecutor, authz.NewLocal(false), nil)
	if _, err := staleService.StartHelmReleaseUpgrade(context.Background(), testHelmActor(), domain.HelmReleaseUpgradeOperationRequest{
		Namespace: "default", ReleaseName: "web", Confirmation: "web", CurrentRevision: 2,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale currentRevision error = %v", err)
	}
}

type namespaceHelmAuthorizer struct {
	denied string
}

func (a namespaceHelmAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	if access.Resource == "secrets" && access.Namespace == a.denied {
		return domain.ErrForbidden
	}
	return nil
}

func TestOperationHistoryFiltersNamespacesWithoutSecretAccess(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{operations: map[string]domain.HelmChartOperation{
		"allowed": {ID: "allowed", Type: "install", Status: domain.HelmOperationSuccess, Namespace: "apps", ReleaseName: "web", StartedAt: time.Now().UTC()},
		"denied":  {ID: "denied", Type: "upgrade", Status: domain.HelmOperationRunning, Namespace: "restricted", ReleaseName: "hidden", StartedAt: time.Now().UTC()},
	}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, namespaceHelmAuthorizer{denied: "restricted"}, nil)
	items, err := service.ListOperations(context.Background(), domain.Actor{Username: "admin"}, domain.HelmOperationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "allowed" {
		t.Fatalf("ListOperations() = %#v", items)
	}
	if _, err := service.Operation(context.Background(), domain.Actor{Username: "admin"}, "denied"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Operation(denied) error = %v", err)
	}
	if _, err := service.CancelOperation(context.Background(), domain.Actor{Username: "admin"}, "denied"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("CancelOperation(denied) error = %v", err)
	}
}

func TestSyncChartRefreshesCachedPackageDetail(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{ID: "repo-1", Name: "Demo", URL: "https://charts.example.com", Enabled: true}}}
	lockingRepository := &lockingMemoryHelmRepository{memoryHelmRepository: repository}
	source := &refreshingHelmSource{}
	service := New(lockingRepository, source, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	detail, err := service.SyncChart(context.Background(), testHelmActor(), "repo-1", "demo")
	if err != nil {
		t.Fatalf("SyncChart() error = %v", err)
	}
	if source.refreshes != 1 || detail.Readme != "refreshed" || detail.Name != "demo" || lockingRepository.acquireCalls != 1 {
		t.Fatalf("SyncChart() refreshes = %d detail = %#v acquireCalls=%d", source.refreshes, detail, lockingRepository.acquireCalls)
	}
}

func TestSyncChartRequiresRepositoryWritePermission(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{ID: "repo-1", Name: "Demo", URL: "https://charts.example.com", Enabled: true}}}
	source := &refreshingHelmSource{}
	service := New(repository, source, &fakeHelmExecutor{}, authz.NewLocal(true), nil)
	if _, err := service.SyncChart(context.Background(), testHelmActor(), "repo-1", "demo"); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("SyncChart(read-only) error = %v", err)
	}
	if source.refreshes != 0 {
		t.Fatalf("SyncChart(read-only) refreshes = %d", source.refreshes)
	}
}

func TestUpgradePassesReviewedRevisionToExecutor(t *testing.T) {
	t.Parallel()
	repository := &lockingMemoryHelmRepository{memoryHelmRepository: &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}}
	executor := &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{
		Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n", Values: "replicas: 1\n", CurrentRevision: 7,
		Resources: []domain.HelmChartPlanResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "demo", Resource: "configmaps", Action: "update"}},
	}}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{
		RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0",
	}, Mode: "upgrade", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1\n"}
	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	operation, err := service.Upgrade(context.Background(), testHelmActor(), domain.HelmChartOperationRequest{
		HelmChartPlanRequest: request, PlanDigest: plan.PlanDigest, Confirmation: "demo",
	})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	finished := waitForHelmOperation(t, repository.memoryHelmRepository, operation.ID)
	if finished.Status != domain.HelmOperationSuccess {
		t.Fatalf("Upgrade() operation = %#v", finished)
	}
	if executor.lastUpgradeRequest.ExpectedRevision != 7 || repository.acquireCalls != 1 {
		t.Fatalf("executor revision = %d, lock calls = %d", executor.lastUpgradeRequest.ExpectedRevision, repository.acquireCalls)
	}
}

func TestHelmReleaseTestUsesReleaseOperationLock(t *testing.T) {
	t.Parallel()
	repository := &lockingMemoryHelmRepository{memoryHelmRepository: &memoryHelmRepository{}}
	executor := &fakeHelmExecutor{testResult: domain.HelmReleaseTestResult{Namespace: "apps", Name: "web", Status: "succeeded"}}
	service := New(repository, fakeHelmSource{}, executor, authz.NewLocal(false), nil)
	result, err := service.TestHelmReleaseWithLock(context.Background(), testHelmActor(), "apps", "web")
	if err != nil {
		t.Fatalf("TestHelmReleaseWithLock() error = %v", err)
	}
	if result.Status != "succeeded" || executor.testCalls != 1 || repository.acquireCalls != 1 {
		t.Fatalf("result=%#v testCalls=%d acquireCalls=%d", result, executor.testCalls, repository.acquireCalls)
	}
}

func TestCreateRepositoryAcceptsOCIPathAndRejectsTag(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	created, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "OCI", URL: "oci://registry.example.com/team/charts/demo", AuthType: domain.HelmRepositoryAuthAnonymous,
	})
	if err != nil {
		t.Fatalf("CreateRepository() OCI error = %v", err)
	}
	if created.URL != "oci://registry.example.com/team/charts/demo" || created.ChartCount != 1 || created.LastSync == nil || !created.LastSync.OK {
		t.Fatalf("CreateRepository() OCI = %#v", created)
	}

	_, err = service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "Invalid OCI", URL: "oci://registry.example.com/team/charts/demo:1.2.3",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("CreateRepository() tagged OCI error = %v", err)
	}
}

func TestUpdateRepositoryAnonymousClearsAuthButKeepsCA(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Private", URL: "https://charts.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthBasic, Revision: "revision-1",
		Credentials: domain.HelmRepositoryCredentials{Username: "robot", Password: "secret", CAPEM: "ca-data"},
	}}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	updated, err := service.UpdateRepository(context.Background(), testHelmActor(), "repo-1", domain.HelmRepositoryUpsert{
		Name: "Private", URL: "https://charts.example.com", AuthType: domain.HelmRepositoryAuthAnonymous, CAPEM: "ca-data",
	})
	if err != nil {
		t.Fatalf("UpdateRepository() error = %v", err)
	}
	stored := repository.items[0]
	if updated.AuthType != domain.HelmRepositoryAuthAnonymous || stored.Credentials.Username != "" || stored.Credentials.Password != "" {
		t.Fatalf("anonymous update kept auth credentials: %#v", stored.Credentials)
	}
	if stored.Credentials.CAPEM != "ca-data" || !updated.HasCustomCA {
		t.Fatalf("anonymous update did not retain CA: %#v", stored.Credentials)
	}
}

func TestUpdateRepositoryHostChangeRequiresExplicitCredentialHandling(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Private", URL: "https://old.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthBasic, Revision: "revision-1",
		Credentials: domain.HelmRepositoryCredentials{Username: "robot", Password: "secret"},
	}}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	_, err := service.UpdateRepository(context.Background(), testHelmActor(), "repo-1", domain.HelmRepositoryUpsert{Name: "Private", URL: "https://new.example.com"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "host") {
		t.Fatalf("UpdateRepository() host change error = %v", err)
	}

	updated, err := service.UpdateRepository(context.Background(), testHelmActor(), "repo-1", domain.HelmRepositoryUpsert{
		Name: "Private", URL: "https://new.example.com", AuthType: domain.HelmRepositoryAuthBasic, Username: "new-robot", Password: "new-secret",
	})
	if err != nil {
		t.Fatalf("UpdateRepository() explicit credential error = %v", err)
	}
	stored := repository.items[0]
	if stored.Credentials.Username != "new-robot" || stored.Credentials.Password != "new-secret" || !updated.HasCredentials {
		t.Fatalf("explicit credential update = %#v", stored.Credentials)
	}
}

func TestInstallRejectsStalePlanDigestBeforeExecuting(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}
	executor := &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{
		Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
		Values:   "replicas: 1\n", CurrentRevision: 0,
		Resources: []domain.HelmChartPlanResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "demo", Resource: "configmaps", Action: "create"}},
	}}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{
		RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0",
	}, Mode: "install", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1\n"}

	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.PlanDigest == "" || plan.Digest != "sha256:abc" || len(plan.Resources) != 1 {
		t.Fatalf("Plan() = %#v", plan)
	}

	_, err = service.Install(context.Background(), testHelmActor(), domain.HelmChartOperationRequest{
		HelmChartPlanRequest: request, PlanDigest: "sha256:stale", Confirmation: "demo",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Install() stale digest error = %v", err)
	}
	if executor.installs != 0 {
		t.Fatalf("Install() executed %d times after stale plan", executor.installs)
	}
}

func TestPlanDigestChangesWithReviewedValues(t *testing.T) {
	t.Parallel()
	repository := domain.HelmRepository{ID: "repo-1", URL: "https://charts.example.com"}
	template := domain.HelmChartPlanTemplate{Manifest: "manifest", Values: "replicas: 1", CurrentRevision: 1}
	changedTemplate := template
	changedTemplate.Values = "replicas: 2"
	base := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{ChartName: "demo", Version: "1.0.0"}, Mode: "upgrade", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1"}
	changed := base
	changed.Values = "replicas: 2"
	options, err := domain.ResolveHelmExecutionOptions(nil)
	if err != nil {
		t.Fatalf("ResolveHelmExecutionOptions() error = %v", err)
	}
	if computePlanDigest(base, repository, "sha256:abc", template, options) == computePlanDigest(changed, repository, "sha256:abc", changedTemplate, options) {
		t.Fatal("computePlanDigest() did not change with reviewed values")
	}
}

func TestRenewOperationLockUsesRepositoryLease(t *testing.T) {
	t.Parallel()
	repository := &lockingMemoryHelmRepository{memoryHelmRepository: &memoryHelmRepository{}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)
	service.renewOperationLock(context.Background(), "default", "demo", "operation-1")
	if repository.acquireCalls != 1 || repository.lastTTL != 20*time.Minute {
		t.Fatalf("lock renew calls/ttl = %d/%s", repository.acquireCalls, repository.lastTTL)
	}
}

func TestPlanDigestIgnoresGeneratedManifestButTracksResourceSet(t *testing.T) {
	t.Parallel()
	repository := domain.HelmRepository{ID: "repo-1", URL: "https://charts.example.com"}
	base := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{ChartName: "demo", Version: "1.0.0"}, Mode: "install", Namespace: "default", ReleaseName: "demo", Values: "{}"}
	options, err := domain.ResolveHelmExecutionOptions(nil)
	if err != nil {
		t.Fatalf("ResolveHelmExecutionOptions() error = %v", err)
	}
	first := domain.HelmChartPlanTemplate{
		Manifest: "data: random-one", Values: "{}",
		Resources: []domain.HelmChartPlanResource{{APIVersion: "v1", Kind: "Secret", Namespace: "default", Name: "demo", Action: "create"}},
	}
	second := first
	second.Manifest = "data: random-two"
	if computePlanDigest(base, repository, "sha256:abc", first, options) != computePlanDigest(base, repository, "sha256:abc", second, options) {
		t.Fatal("computePlanDigest() changed with generated manifest data")
	}
	second.Resources = append([]domain.HelmChartPlanResource(nil), first.Resources...)
	second.Resources[0].Action = "update"
	if computePlanDigest(base, repository, "sha256:abc", first, options) == computePlanDigest(base, repository, "sha256:abc", second, options) {
		t.Fatal("computePlanDigest() did not change with the reviewed resource set")
	}
}

func TestPlanDigestChangesWithExecutionOptions(t *testing.T) {
	t.Parallel()
	repository := domain.HelmRepository{ID: "repo-1", URL: "https://charts.example.com"}
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{ChartName: "demo", Version: "1.0.0"}, Mode: "install", Namespace: "default", ReleaseName: "demo", Values: "{}"}
	template := domain.HelmChartPlanTemplate{Values: "{}", Resources: []domain.HelmChartPlanResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "demo", Action: "create"}}}
	first, err := domain.ResolveHelmExecutionOptions(nil)
	if err != nil {
		t.Fatalf("ResolveHelmExecutionOptions() error = %v", err)
	}
	wait := false
	second, err := domain.ResolveHelmExecutionOptions(&domain.HelmExecutionOptionsInput{Wait: &wait, Atomic: &wait, CleanupOnFail: &wait})
	if err != nil {
		t.Fatalf("ResolveHelmExecutionOptions() error = %v", err)
	}
	if computePlanDigest(request, repository, "sha256:abc", template, first) == computePlanDigest(request, repository, "sha256:abc", template, second) {
		t.Fatal("computePlanDigest() did not change with execution options")
	}
}

func TestInstallAcceptsPlanWhenGeneratedManifestChanges(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}
	executor := &rotatingHelmExecutor{fakeHelmExecutor: fakeHelmExecutor{template: domain.HelmChartPlanTemplate{
		Values: "replicas: 1\n",
		Resources: []domain.HelmChartPlanResource{{
			APIVersion: "v1", Kind: "Secret", Namespace: "default", Name: "demo", Resource: "secrets", Action: "create",
		}},
	}}}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{
		RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0",
	}, Mode: "install", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1\n"}

	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	operation, err := service.Install(context.Background(), testHelmActor(), domain.HelmChartOperationRequest{
		HelmChartPlanRequest: request, PlanDigest: plan.PlanDigest, Confirmation: "demo",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	operation = waitForHelmOperation(t, repository, operation.ID)
	if operation.Status != domain.HelmOperationSuccess || operation.Revision != 1 {
		t.Fatalf("Install() operation = %#v", operation)
	}
	if len(operation.Resources) != 1 || operation.Resources[0].Kind != "Secret" || operation.Resources[0].Status != "pending" {
		t.Fatalf("Install() resource progress = %#v", operation.Resources)
	}
	if executor.planCalls != 2 || executor.installs != 1 {
		t.Fatalf("executor calls = plan %d, install %d", executor.planCalls, executor.installs)
	}
}
