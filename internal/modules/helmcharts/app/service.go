package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"

	"sigs.k8s.io/yaml"
)

const (
	maxRepositoryNameLength       = 128
	maxRepositoryURLSize          = 2048
	maxCredentialSize             = 16 << 10
	maxCASize                     = 64 << 10
	maxChartValuesSize            = 1024 * 1024
	helmOperationTimeout          = 12 * time.Minute
	helmProgressInterval          = 3 * time.Second
	helmOperationHeartbeat        = 30 * time.Second
	helmOperationStaleAfter       = 2 * time.Minute
	helmOperationLegacyStaleAfter = helmOperationTimeout + 2*time.Minute
	helmSyncRetryBaseDelay        = 5 * time.Minute
	helmCacheMaintenance          = 10 * time.Minute
	helmOperationListLimit        = 500
	maxOperationResources         = 300
)

var (
	repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,127}$`)
	chartNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)
	sensitiveKey          = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[-_]?key|private[-_]?key|client[-_]?secret|access[-_]?key|credential)`)
)

type Repository interface {
	ListHelmRepositories(ctx context.Context) ([]domain.HelmRepository, error)
	GetHelmRepository(ctx context.Context, id string) (domain.HelmRepository, error)
	SaveHelmRepository(ctx context.Context, repository domain.HelmRepository) (domain.HelmRepository, error)
	DeleteHelmRepository(ctx context.Context, id, expectedRevision string) error
	SaveHelmChartOperation(ctx context.Context, operation domain.HelmChartOperation) error
	GetHelmChartOperation(ctx context.Context, id string) (domain.HelmChartOperation, error)
}

type Source interface {
	Charts(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error)
	Detail(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error)
	Download(ctx context.Context, repository domain.HelmRepository, chartName, version, expectedDigest string) ([]byte, string, error)
}

type RepositoryRefresher interface {
	Refresh(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error)
}

type CacheManager interface {
	CacheStats() (domain.HelmCacheStats, error)
	CleanupCache(maxBytes int64) (domain.HelmCacheCleanupResult, error)
	RemoveRepositoryCache(repository domain.HelmRepository) error
}

type OperationLister interface {
	ListHelmChartOperations(ctx context.Context, filter domain.HelmOperationFilter) ([]domain.HelmChartOperation, error)
}

type OperationLocker interface {
	AcquireHelmOperationLock(ctx context.Context, namespace, releaseName, operationID string, ttl time.Duration) error
	ReleaseHelmOperationLock(ctx context.Context, namespace, releaseName, operationID string) error
}

type OperationLockCleaner interface {
	CleanupHelmOperationLocks(ctx context.Context) (int, error)
}

type ChartRefresher interface {
	RefreshChart(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error)
}

// SupplyChainVerifier validates an OCI Chart against the trust policy stored on
// its repository. Verification is enforced before a plan or Release mutation;
// callers never receive an installable plan for an untrusted signature.
type SupplyChainVerifier interface {
	VerifySupplyChain(ctx context.Context, repository domain.HelmRepository, chartName, version string) (domain.HelmSupplyChainStatus, error)
}

type Executor interface {
	PlanHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error)
	InstallHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest, options domain.HelmExecutionOptions) (int, error)
	UpgradeHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest, options domain.HelmExecutionOptions) (int, error)
	UninstallHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, options domain.HelmExecutionOptions) error
	RollbackHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, targetRevision int, options domain.HelmExecutionOptions) (int, error)
}

type ReleaseUpgradeExecutor interface {
	PlanHelmReleaseUpgrade(ctx context.Context, actor domain.Actor, namespace, releaseName, values string) (domain.HelmChartPlanTemplate, error)
	UpgradeHelmReleaseWithHelm(ctx context.Context, actor domain.Actor, namespace, releaseName, values string, options domain.HelmExecutionOptions) (int, error)
}

// HelmResourceObserver exposes live object status for an operation. Executors
// that cannot observe resources still work; runOperation then keeps the plan
// snapshot without attempting progress reads.
type HelmResourceObserver interface {
	ObserveHelmOperationResources(ctx context.Context, resources []domain.HelmChartPlanResource) []domain.HelmOperationResource
}

type HelmReleaseTester interface {
	TestHelmRelease(ctx context.Context, actor domain.Actor, namespace, releaseName string) (domain.HelmReleaseTestResult, error)
}

type Service struct {
	repository Repository
	source     Source
	executor   Executor
	authorizer domain.Authorizer
	auditor    domain.Auditor

	syncMu       sync.Mutex
	syncInFlight map[string]bool
	cacheLimit   int64

	operationMu      sync.Mutex
	operationCancels map[string]context.CancelFunc
	operationLocks   map[string]string

	idempotencyMu    sync.Mutex
	idempotencyLocks map[string]*idempotencyMutex
}

func New(repository Repository, source Source, executor Executor, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{
		repository: repository, source: source, executor: executor, authorizer: authorizer, auditor: auditor,
		syncInFlight: map[string]bool{}, operationCancels: map[string]context.CancelFunc{}, operationLocks: map[string]string{},
		idempotencyLocks: map[string]*idempotencyMutex{},
	}
}

type idempotencyMutex struct {
	mu   sync.Mutex
	refs int
}

func (s *Service) SetCacheLimit(maxBytes int64) {
	s.cacheLimit = maxBytes
}

func (s *Service) ListRepositories(ctx context.Context, actor domain.Actor) ([]domain.HelmRepository, error) {
	if err := s.require(ctx, actor, "list"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListHelmRepositories(ctx)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = decorateRepository(items[index])
	}
	return stripRepositorySecrets(items), nil
}

func (s *Service) CreateRepository(ctx context.Context, actor domain.Actor, input domain.HelmRepositoryUpsert) (domain.HelmRepository, error) {
	if err := s.require(ctx, actor, "create"); err != nil {
		s.audit(ctx, actor, "create-helm-repository", "", err)
		return domain.HelmRepository{}, err
	}
	repository, err := normalizeRepository(input, domain.HelmRepository{})
	if err != nil {
		s.audit(ctx, actor, "create-helm-repository", "", err)
		return domain.HelmRepository{}, err
	}
	repository.ID, err = newID()
	if err != nil {
		return domain.HelmRepository{}, err
	}
	saved, err := s.repository.SaveHelmRepository(ctx, repository)
	if err != nil {
		s.audit(ctx, actor, "create-helm-repository", "", err)
		return domain.HelmRepository{}, err
	}
	saved, _ = s.refreshRepository(ctx, saved, domain.HelmRepositorySyncTriggerCreate)
	s.audit(ctx, actor, "create-helm-repository", saved.ID, nil)
	return stripRepositorySecret(saved), nil
}

func (s *Service) UpdateRepository(ctx context.Context, actor domain.Actor, id string, input domain.HelmRepositoryUpsert) (domain.HelmRepository, error) {
	if err := s.require(ctx, actor, "update"); err != nil {
		s.audit(ctx, actor, "update-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	current, err := s.repository.GetHelmRepository(ctx, id)
	if err != nil {
		s.audit(ctx, actor, "update-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	if input.Revision != "" && input.Revision != current.Revision {
		err := fmt.Errorf("%w: expected Helm repository revision %s, got %s", domain.ErrConflict, input.Revision, current.Revision)
		s.audit(ctx, actor, "update-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	repository, err := normalizeRepository(input, current)
	if err != nil {
		s.audit(ctx, actor, "update-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	repository.ID = current.ID
	repository.Revision = current.Revision
	repository.SyncHistory = current.SyncHistory
	repository.SyncFailureCount = current.SyncFailureCount
	if repository.URL == current.URL && repository.InsecureSkipTLS == current.InsecureSkipTLS && credentialsEqual(repository.Credentials, current.Credentials) {
		repository.LastSync = current.LastSync
		repository.ChartCount = current.ChartCount
	} else {
		repository.SyncFailureCount = 0
	}
	saved, err := s.repository.SaveHelmRepository(ctx, repository)
	if err != nil {
		s.audit(ctx, actor, "update-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	saved, _ = s.refreshRepository(ctx, saved, domain.HelmRepositorySyncTriggerUpdate)
	s.audit(ctx, actor, "update-helm-repository", id, nil)
	return stripRepositorySecret(saved), nil
}

func (s *Service) DeleteRepository(ctx context.Context, actor domain.Actor, id, revision string) error {
	if err := s.require(ctx, actor, "delete"); err != nil {
		s.audit(ctx, actor, "delete-helm-repository", id, err)
		return err
	}
	repository, getErr := s.repository.GetHelmRepository(ctx, id)
	if getErr != nil {
		s.audit(ctx, actor, "delete-helm-repository", id, getErr)
		return getErr
	}
	err := s.repository.DeleteHelmRepository(ctx, id, revision)
	if err == nil {
		if cache, ok := s.source.(CacheManager); ok {
			_ = cache.RemoveRepositoryCache(repository)
		}
	}
	s.audit(ctx, actor, "delete-helm-repository", id, err)
	return err
}

func (s *Service) SyncRepository(ctx context.Context, actor domain.Actor, id string) (domain.HelmRepository, error) {
	if err := s.require(ctx, actor, "update"); err != nil {
		s.audit(ctx, actor, "sync-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	repository, err := s.repository.GetHelmRepository(ctx, id)
	if err != nil {
		s.audit(ctx, actor, "sync-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	repository, syncErr := s.refreshRepository(ctx, repository, domain.HelmRepositorySyncTriggerManual)
	s.audit(ctx, actor, "sync-helm-repository", id, syncErr)
	return stripRepositorySecret(repository), nil
}

func (s *Service) Charts(ctx context.Context, actor domain.Actor, repositoryID, search string) ([]domain.HelmChartSummary, []string, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return nil, nil, err
	}
	repositories, err := s.repository.ListHelmRepositories(ctx)
	if err != nil {
		return nil, nil, err
	}
	query := strings.ToLower(strings.TrimSpace(search))
	items := make([]domain.HelmChartSummary, 0)
	warnings := make([]string, 0)
	for _, repository := range repositories {
		if !repository.Enabled || (repositoryID != "" && repository.ID != repositoryID) {
			continue
		}
		charts, fetchErr := s.source.Charts(ctx, repository)
		if fetchErr != nil {
			warnings = append(warnings, fmt.Sprintf("仓库 %s 同步失败：%s", repository.Name, fetchErr.Error()))
			continue
		}
		for _, chart := range charts {
			if query != "" && !strings.Contains(strings.ToLower(chart.Name), query) && !strings.Contains(strings.ToLower(chart.Description), query) {
				continue
			}
			items = append(items, chart)
		}
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].Name != items[right].Name {
			return strings.ToLower(items[left].Name) < strings.ToLower(items[right].Name)
		}
		return items[left].RepositoryName < items[right].RepositoryName
	})
	s.audit(ctx, actor, "list-helm-charts", repositoryID, nil)
	return items, warnings, nil
}

func (s *Service) Chart(ctx context.Context, actor domain.Actor, repositoryID, chartName string) (domain.HelmChartDetail, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return domain.HelmChartDetail{}, err
	}
	repository, chartName, err := s.repositoryAndChart(ctx, repositoryID, chartName)
	if err != nil {
		return domain.HelmChartDetail{}, err
	}
	detail, err := s.source.Detail(ctx, repository, chartName)
	if err != nil {
		s.audit(ctx, actor, "get-helm-chart", chartName, err)
		return domain.HelmChartDetail{}, err
	}
	if strings.HasPrefix(strings.ToLower(repository.URL), "http://") {
		detail.Warnings = append(detail.Warnings, "该仓库使用明文 HTTP，传输内容可能被篡改；生产环境请使用 HTTPS")
	}
	if verifier, ok := s.source.(SupplyChainVerifier); ok {
		status, verifyErr := verifier.VerifySupplyChain(ctx, repository, chartName, detail.Version)
		if verifyErr != nil {
			detail.Warnings = append(detail.Warnings, "OCI Chart 供应链校验失败："+verifyErr.Error())
		} else if status.Policy != "" && status.Policy != domain.HelmSupplyChainDisabled {
			detail.SupplyChain = &status
			if err := verifyChartDigestBinding(status, detail.Digest); err != nil {
				detail.Warnings = append(detail.Warnings, err.Error())
			}
		}
	}
	s.audit(ctx, actor, "get-helm-chart", chartName, nil)
	return detail, nil
}

// SyncChart forces a fresh Chart package download and refreshes the metadata
// extracted from that package. It does not re-synchronize the repository index.
// This is a cache mutation and therefore requires repository update permission.
func (s *Service) SyncChart(ctx context.Context, actor domain.Actor, repositoryID, chartName string) (domain.HelmChartDetail, error) {
	if err := s.require(ctx, actor, "update"); err != nil {
		s.audit(ctx, actor, "sync-helm-chart", chartName, err)
		return domain.HelmChartDetail{}, err
	}
	repository, chartName, err := s.repositoryAndChart(ctx, repositoryID, chartName)
	if err != nil {
		return domain.HelmChartDetail{}, err
	}
	lockName := "helm-chart-sync-" + repository.ID + "-" + chartName
	lockID := newOperationID()
	if err := s.acquireOperationLock(ctx, "dosh-internal", lockName, lockID); err != nil {
		s.audit(ctx, actor, "sync-helm-chart", chartName, err)
		return domain.HelmChartDetail{}, err
	}
	locked := true
	defer func() {
		if !locked {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		s.releaseOperationLock(releaseCtx, "dosh-internal", lockName, lockID)
	}()
	var detail domain.HelmChartDetail
	if refresher, ok := s.source.(ChartRefresher); ok {
		detail, err = refresher.RefreshChart(ctx, repository, chartName)
	} else {
		detail, err = s.source.Detail(ctx, repository, chartName)
	}
	if err != nil {
		s.audit(ctx, actor, "sync-helm-chart", chartName, err)
		return domain.HelmChartDetail{}, err
	}
	if strings.HasPrefix(strings.ToLower(repository.URL), "http://") {
		detail.Warnings = append(detail.Warnings, "该仓库使用明文 HTTP，传输内容可能被篡改；生产环境请使用 HTTPS")
	}
	if verifier, ok := s.source.(SupplyChainVerifier); ok {
		status, verifyErr := verifier.VerifySupplyChain(ctx, repository, chartName, detail.Version)
		if verifyErr != nil {
			detail.Warnings = append(detail.Warnings, "OCI Chart 供应链校验失败："+verifyErr.Error())
		} else if status.Policy != "" && status.Policy != domain.HelmSupplyChainDisabled {
			detail.SupplyChain = &status
			if err := verifyChartDigestBinding(status, detail.Digest); err != nil {
				detail.Warnings = append(detail.Warnings, err.Error())
			}
		}
	}
	locked = false
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	s.releaseOperationLock(releaseCtx, "dosh-internal", lockName, lockID)
	s.audit(ctx, actor, "sync-helm-chart", chartName, nil)
	return detail, nil
}

func (s *Service) Plan(ctx context.Context, actor domain.Actor, input domain.HelmChartPlanRequest) (domain.HelmChartPlan, error) {
	plan, _, err := s.plan(ctx, actor, input)
	return plan, err
}

// VerifyChartSupplyChain performs an on-demand verification for a specific
// Chart version. Install and upgrade plans run the same check internally.
func (s *Service) VerifyChartSupplyChain(ctx context.Context, actor domain.Actor, repositoryID, chartName, version string) (domain.HelmSupplyChainStatus, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return domain.HelmSupplyChainStatus{}, err
	}
	repository, chartName, err := s.repositoryAndChart(ctx, repositoryID, chartName)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, err
	}
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 128 || strings.ContainsAny(version, "/ \t\r\n") {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: a valid Chart version is required", domain.ErrInvalid)
	}
	verifier, ok := s.source.(SupplyChainVerifier)
	if !ok {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: OCI supply-chain verification is unavailable", domain.ErrUnsupported)
	}
	status, err := verifier.VerifySupplyChain(ctx, repository, chartName, version)
	s.audit(ctx, actor, "verify-helm-chart-supply-chain", chartName, err)
	return status, err
}

func (s *Service) Install(ctx context.Context, actor domain.Actor, input domain.HelmChartOperationRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	input.Mode = "install"
	normalized, err := normalizePlanRequest(input.HelmChartPlanRequest)
	if err != nil {
		s.audit(ctx, actor, "install-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	input.HelmChartPlanRequest = normalized
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		s.audit(ctx, actor, "install-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	requestDigest, err := operationRequestDigest("install", map[string]any{
		"repositoryId": normalized.RepositoryID, "chartName": normalized.ChartName, "version": normalized.Version,
		"namespace": normalized.Namespace, "releaseName": normalized.ReleaseName, "planDigest": input.PlanDigest,
	})
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = idempotencyInput("install", "", requestDigest, normalized.Namespace, normalized.ReleaseName)
	}
	unlockIdempotency := s.beginIdempotentRequest(idempotencyKey)
	defer unlockIdempotency()
	if existing, lookupErr := s.idempotentOperation(ctx, "install", normalized.Namespace, normalized.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		if err := s.requireReleaseRead(ctx, actor, existing.Namespace); err != nil {
			return domain.HelmChartOperation{}, err
		}
		if err := s.requireReleaseWrite(ctx, actor, existing.Namespace, "create"); err != nil {
			return domain.HelmChartOperation{}, err
		}
		return *existing, nil
	}
	operationID := newOperationID()
	if err := s.acquireOperationLock(ctx, normalized.Namespace, normalized.ReleaseName, operationID); err != nil {
		if existing, waitErr := s.waitForIdempotentOperation(ctx, "install", normalized.Namespace, normalized.ReleaseName, idempotencyKey, requestDigest); waitErr != nil {
			return domain.HelmChartOperation{}, waitErr
		} else if existing != nil {
			if err := s.requireReleaseRead(ctx, actor, existing.Namespace); err != nil {
				return domain.HelmChartOperation{}, err
			}
			if err := s.requireReleaseWrite(ctx, actor, existing.Namespace, "create"); err != nil {
				return domain.HelmChartOperation{}, err
			}
			return *existing, nil
		}
		s.audit(ctx, actor, "install-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	locked := true
	defer func() {
		if !locked {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		s.releaseOperationLock(releaseCtx, normalized.Namespace, normalized.ReleaseName, operationID)
	}()
	// Re-check after obtaining the Release lease. A concurrent request may have
	// completed and released the lease between the first lookup and acquisition;
	// without this second check the same idempotency key could execute twice.
	if existing, lookupErr := s.idempotentOperation(ctx, "install", normalized.Namespace, normalized.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		if err := s.requireReleaseRead(ctx, actor, existing.Namespace); err != nil {
			return domain.HelmChartOperation{}, err
		}
		if err := s.requireReleaseWrite(ctx, actor, existing.Namespace, "create"); err != nil {
			return domain.HelmChartOperation{}, err
		}
		return *existing, nil
	}
	plan, archive, err := s.plan(ctx, actor, input.HelmChartPlanRequest)
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if err := comparePlanDigest(input.PlanDigest, plan.PlanDigest); err != nil {
		s.audit(ctx, actor, "install-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	input.ExpectedRevision = plan.CurrentRevision
	operation := s.newOperation("install", input.HelmChartPlanRequest, plan)
	operation.ID = operationID
	operation.IdempotencyKey = idempotencyKey
	operation.RequestDigest = requestDigest
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	locked = false
	s.runOperation(ctx, operation, actor, "install-helm-chart", func(runCtx context.Context) (int, error) {
		return s.executor.InstallHelmChart(runCtx, actor, archive, input.HelmChartPlanRequest, plan.Options)
	})
	return operation, nil
}

func (s *Service) Upgrade(ctx context.Context, actor domain.Actor, input domain.HelmChartOperationRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	input.Mode = "upgrade"
	normalized, err := normalizePlanRequest(input.HelmChartPlanRequest)
	if err != nil {
		s.audit(ctx, actor, "upgrade-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	input.HelmChartPlanRequest = normalized
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		s.audit(ctx, actor, "upgrade-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	requestDigest, err := operationRequestDigest("upgrade", map[string]any{
		"repositoryId": normalized.RepositoryID, "chartName": normalized.ChartName, "version": normalized.Version,
		"namespace": normalized.Namespace, "releaseName": normalized.ReleaseName, "planDigest": input.PlanDigest,
	})
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = idempotencyInput("upgrade", "", requestDigest, normalized.Namespace, normalized.ReleaseName)
	}
	unlockIdempotency := s.beginIdempotentRequest(idempotencyKey)
	defer unlockIdempotency()
	if existing, lookupErr := s.idempotentOperation(ctx, "upgrade", normalized.Namespace, normalized.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		if err := s.requireReleaseRead(ctx, actor, existing.Namespace); err != nil {
			return domain.HelmChartOperation{}, err
		}
		if err := s.requireReleaseWrite(ctx, actor, existing.Namespace, "update"); err != nil {
			return domain.HelmChartOperation{}, err
		}
		return *existing, nil
	}
	operationID := newOperationID()
	if err := s.acquireOperationLock(ctx, normalized.Namespace, normalized.ReleaseName, operationID); err != nil {
		if existing, waitErr := s.waitForIdempotentOperation(ctx, "upgrade", normalized.Namespace, normalized.ReleaseName, idempotencyKey, requestDigest); waitErr != nil {
			return domain.HelmChartOperation{}, waitErr
		} else if existing != nil {
			if err := s.requireReleaseRead(ctx, actor, existing.Namespace); err != nil {
				return domain.HelmChartOperation{}, err
			}
			if err := s.requireReleaseWrite(ctx, actor, existing.Namespace, "update"); err != nil {
				return domain.HelmChartOperation{}, err
			}
			return *existing, nil
		}
		s.audit(ctx, actor, "upgrade-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	locked := true
	defer func() {
		if !locked {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		s.releaseOperationLock(releaseCtx, normalized.Namespace, normalized.ReleaseName, operationID)
	}()
	if existing, lookupErr := s.idempotentOperation(ctx, "upgrade", normalized.Namespace, normalized.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		if err := s.requireReleaseRead(ctx, actor, existing.Namespace); err != nil {
			return domain.HelmChartOperation{}, err
		}
		if err := s.requireReleaseWrite(ctx, actor, existing.Namespace, "update"); err != nil {
			return domain.HelmChartOperation{}, err
		}
		return *existing, nil
	}
	plan, archive, err := s.plan(ctx, actor, input.HelmChartPlanRequest)
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if err := comparePlanDigest(input.PlanDigest, plan.PlanDigest); err != nil {
		s.audit(ctx, actor, "upgrade-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	input.ExpectedRevision = plan.CurrentRevision
	operation := s.newOperation("upgrade", input.HelmChartPlanRequest, plan)
	operation.ID = operationID
	operation.IdempotencyKey = idempotencyKey
	operation.RequestDigest = requestDigest
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	locked = false
	s.runOperation(ctx, operation, actor, "upgrade-helm-chart", func(runCtx context.Context) (int, error) {
		return s.executor.UpgradeHelmChart(runCtx, actor, archive, input.HelmChartPlanRequest, plan.Options)
	})
	return operation, nil
}

// StartHelmReleaseUpgrade executes an installed Release's stored Chart through
// the same operation, locking, progress and cancellation engine used by Chart
// catalog operations. Values are an override over the deployed configuration.
func (s *Service) StartHelmReleaseUpgrade(ctx context.Context, actor domain.Actor, input domain.HelmReleaseUpgradeOperationRequest) (domain.HelmChartOperation, error) {
	if strings.TrimSpace(input.Namespace) == "" || strings.TrimSpace(input.ReleaseName) == "" {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	if len(input.Values) > maxChartValuesSize {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: values must not exceed 1 MiB", domain.ErrInvalid)
	}
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	executionOptions, err := domain.ResolveHelmExecutionOptions(input.Options)
	if err != nil {
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	upgrader, ok := s.executor.(ReleaseUpgradeExecutor)
	if !ok {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: native Helm release upgrade is unavailable", domain.ErrUnsupported)
	}
	if err := s.requireReleaseRead(ctx, actor, input.Namespace); err != nil {
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if err := s.requireReleaseWrite(ctx, actor, input.Namespace, "create"); err != nil {
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	requestDigest, err := operationRequestDigest("upgrade-release", map[string]any{
		"namespace": input.Namespace, "releaseName": input.ReleaseName, "values": input.Values,
		"currentRevision": input.CurrentRevision, "options": input.Options,
	})
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = idempotencyInput("upgrade", "", requestDigest, input.Namespace, input.ReleaseName)
	}
	unlockIdempotency := s.beginIdempotentRequest(idempotencyKey)
	defer unlockIdempotency()
	if existing, lookupErr := s.idempotentOperation(ctx, "upgrade", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		return *existing, nil
	}

	operationID := newOperationID()
	if err := s.acquireOperationLock(ctx, input.Namespace, input.ReleaseName, operationID); err != nil {
		if existing, waitErr := s.waitForIdempotentOperation(ctx, "upgrade", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); waitErr != nil {
			return domain.HelmChartOperation{}, waitErr
		} else if existing != nil {
			return *existing, nil
		}
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	locked := true
	defer func() {
		if locked {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			s.releaseOperationLock(releaseCtx, input.Namespace, input.ReleaseName, operationID)
		}
	}()
	if existing, lookupErr := s.idempotentOperation(ctx, "upgrade", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		return *existing, nil
	}

	plan, err := upgrader.PlanHelmReleaseUpgrade(ctx, actor, input.Namespace, input.ReleaseName, input.Values)
	if err != nil {
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if input.CurrentRevision > 0 && plan.CurrentRevision != input.CurrentRevision {
		err := fmt.Errorf("%w: Helm release revision changed; refresh the upgrade preview", domain.ErrConflict)
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if err := s.authorizeRenderedResources(ctx, actor, plan.Resources); err != nil {
		s.audit(ctx, actor, "upgrade-helm-release", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}

	operation := s.newOperationFromTemplate("upgrade", domain.HelmChartPlanRequest{
		HelmChartRef: domain.HelmChartRef{ChartName: plan.ChartName, Version: plan.ChartVersion},
		Namespace:    input.Namespace, ReleaseName: input.ReleaseName,
	}, plan)
	operation.ID = operationID
	operation.ChartName = plan.ChartName
	operation.ChartVersion = plan.ChartVersion
	operation.IdempotencyKey = idempotencyKey
	operation.RequestDigest = requestDigest
	operation.Options = &executionOptions
	operation.Warnings = append(operation.Warnings, "使用 Helm 原生升级执行：将运行 Hook、清理目标清单中已移除的对象，并保留原子回滚语义")
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	locked = false
	s.runOperation(ctx, operation, actor, "upgrade-helm-release", func(runCtx context.Context) (int, error) {
		return upgrader.UpgradeHelmReleaseWithHelm(runCtx, actor, input.Namespace, input.ReleaseName, input.Values, executionOptions)
	})
	return operation, nil
}

func (s *Service) Uninstall(ctx context.Context, actor domain.Actor, input domain.HelmChartUninstallRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	executionOptions, err := domain.ResolveHelmExecutionOptions(input.Options)
	if err != nil {
		s.audit(ctx, actor, "uninstall-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if err := s.requireReleaseWrite(ctx, actor, input.Namespace, "delete"); err != nil {
		s.audit(ctx, actor, "uninstall-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		s.audit(ctx, actor, "uninstall-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	requestDigest, err := operationRequestDigest("uninstall", map[string]any{
		"namespace": input.Namespace, "releaseName": input.ReleaseName, "options": input.Options,
	})
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = idempotencyInput("uninstall", "", requestDigest, input.Namespace, input.ReleaseName)
	}
	unlockIdempotency := s.beginIdempotentRequest(idempotencyKey)
	defer unlockIdempotency()
	if existing, lookupErr := s.idempotentOperation(ctx, "uninstall", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		return *existing, nil
	}
	operation := domain.HelmChartOperation{
		ID: newOperationID(), Type: "uninstall", Status: domain.HelmOperationRunning,
		Namespace: input.Namespace, ReleaseName: input.ReleaseName, StartedAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC(),
		Options: &executionOptions, IdempotencyKey: idempotencyKey, RequestDigest: requestDigest,
		Warnings: []string{"卸载会删除 Helm 管理的资源；默认保留的 PVC、CRD 和外部资源需要单独清理"},
	}
	if executionOptions.KeepHistory {
		operation.Warnings = append(operation.Warnings, "保留 Helm 历史：Release 记录会以 uninstalled 状态保留，后续可审计和恢复")
	}
	if err := s.acquireOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID); err != nil {
		if existing, waitErr := s.waitForIdempotentOperation(ctx, "uninstall", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); waitErr != nil {
			return domain.HelmChartOperation{}, waitErr
		} else if existing != nil {
			return *existing, nil
		}
		s.audit(ctx, actor, "uninstall-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if existing, lookupErr := s.idempotentOperation(ctx, "uninstall", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		s.releaseOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID)
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		s.releaseOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID)
		return *existing, nil
	}
	if err := s.saveOperation(ctx, &operation); err != nil {
		s.releaseOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID)
		return domain.HelmChartOperation{}, err
	}
	s.runOperation(ctx, operation, actor, "uninstall-helm-chart", func(runCtx context.Context) (int, error) {
		return 0, s.executor.UninstallHelmChart(runCtx, actor, input.Namespace, input.ReleaseName, executionOptions)
	})
	return operation, nil
}

func (s *Service) Rollback(ctx context.Context, actor domain.Actor, input domain.HelmChartRollbackRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	if input.TargetRevision <= 0 {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: target revision must be positive", domain.ErrInvalid)
	}
	executionOptions, err := domain.ResolveHelmExecutionOptions(input.Options)
	if err != nil {
		s.audit(ctx, actor, "rollback-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if err := s.requireReleaseWrite(ctx, actor, input.Namespace, "update"); err != nil {
		s.audit(ctx, actor, "rollback-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		s.audit(ctx, actor, "rollback-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	requestDigest, err := operationRequestDigest("rollback", map[string]any{
		"namespace": input.Namespace, "releaseName": input.ReleaseName, "targetRevision": input.TargetRevision, "options": input.Options,
	})
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = idempotencyInput("rollback", "", requestDigest, input.Namespace, input.ReleaseName)
	}
	unlockIdempotency := s.beginIdempotentRequest(idempotencyKey)
	defer unlockIdempotency()
	if existing, lookupErr := s.idempotentOperation(ctx, "rollback", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		return *existing, nil
	}
	operation := domain.HelmChartOperation{
		ID: newOperationID(), Type: "rollback", Status: domain.HelmOperationRunning,
		Namespace: input.Namespace, ReleaseName: input.ReleaseName, StartedAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC(),
		Options: &executionOptions, IdempotencyKey: idempotencyKey, RequestDigest: requestDigest,
	}
	if err := s.acquireOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID); err != nil {
		if existing, waitErr := s.waitForIdempotentOperation(ctx, "rollback", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); waitErr != nil {
			return domain.HelmChartOperation{}, waitErr
		} else if existing != nil {
			return *existing, nil
		}
		s.audit(ctx, actor, "rollback-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	if existing, lookupErr := s.idempotentOperation(ctx, "rollback", input.Namespace, input.ReleaseName, idempotencyKey, requestDigest); lookupErr != nil {
		s.releaseOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID)
		return domain.HelmChartOperation{}, lookupErr
	} else if existing != nil {
		s.releaseOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID)
		return *existing, nil
	}
	if err := s.saveOperation(ctx, &operation); err != nil {
		s.releaseOperationLock(ctx, operation.Namespace, operation.ReleaseName, operation.ID)
		return domain.HelmChartOperation{}, err
	}
	s.runOperation(ctx, operation, actor, "rollback-helm-chart", func(runCtx context.Context) (int, error) {
		return s.executor.RollbackHelmChart(runCtx, actor, input.Namespace, input.ReleaseName, input.TargetRevision, executionOptions)
	})
	return operation, nil
}

func (s *Service) Operation(ctx context.Context, actor domain.Actor, id string) (domain.HelmChartOperation, error) {
	if strings.TrimSpace(id) == "" {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: operation id is required", domain.ErrInvalid)
	}
	operation, err := s.repository.GetHelmChartOperation(ctx, id)
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if err := s.requireReleaseRead(ctx, actor, operation.Namespace); err != nil {
		return domain.HelmChartOperation{}, err
	}
	return operation, nil
}

func (s *Service) ListOperations(ctx context.Context, actor domain.Actor, filter domain.HelmOperationFilter) ([]domain.HelmChartOperation, error) {
	lister, ok := s.repository.(OperationLister)
	if !ok {
		return nil, fmt.Errorf("%w: Helm operation history is unavailable", domain.ErrUnsupported)
	}
	items, err := lister.ListHelmChartOperations(ctx, filter)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	filtered := make([]domain.HelmChartOperation, 0, len(items))
	for _, operation := range items {
		if operation.Namespace == "" {
			continue
		}
		if _, checked := allowed[operation.Namespace]; !checked {
			allowed[operation.Namespace] = s.requireReleaseRead(ctx, actor, operation.Namespace) == nil
		}
		if allowed[operation.Namespace] {
			filtered = append(filtered, operation)
		}
	}
	return filtered, nil
}

func (s *Service) CancelOperation(ctx context.Context, actor domain.Actor, id string) (domain.HelmChartOperation, error) {
	operation, err := s.repository.GetHelmChartOperation(ctx, id)
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if operation.Status != domain.HelmOperationRunning && operation.Status != domain.HelmOperationPending {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: operation %s is already finished", domain.ErrConflict, id)
	}
	if err := s.requireReleaseWrite(ctx, actor, operation.Namespace, "update"); err != nil {
		return domain.HelmChartOperation{}, err
	}
	s.operationMu.Lock()
	cancel := s.operationCancels[id]
	s.operationMu.Unlock()
	if cancel != nil {
		cancel()
	}
	operation.CancelRequested = true
	operation.Message = "已请求取消，等待 Helm 操作安全退出"
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	s.audit(ctx, actor, "cancel-helm-operation", operation.ReleaseName, nil)
	return operation, nil
}

// TestHelmReleaseWithLock serializes Helm test with every other mutation of the
// same Release. Helm test updates Release history and creates/deletes hook
// resources, so it must not overlap an upgrade, rollback or uninstall.
func (s *Service) TestHelmReleaseWithLock(ctx context.Context, actor domain.Actor, namespace, releaseName string) (domain.HelmReleaseTestResult, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(releaseName) == "" {
		return domain.HelmReleaseTestResult{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	tester, ok := s.executor.(HelmReleaseTester)
	if !ok {
		return domain.HelmReleaseTestResult{}, fmt.Errorf("%w: Helm test execution is unavailable", domain.ErrUnsupported)
	}
	operationID := newOperationID()
	if err := s.acquireOperationLock(ctx, namespace, releaseName, operationID); err != nil {
		return domain.HelmReleaseTestResult{}, err
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	defer s.releaseOperationLock(releaseCtx, namespace, releaseName, operationID)
	return tester.TestHelmRelease(ctx, actor, namespace, releaseName)
}

func (s *Service) acquireOperationLock(ctx context.Context, namespace, releaseName, operationID string) error {
	if locker, ok := s.repository.(OperationLocker); ok {
		return locker.AcquireHelmOperationLock(ctx, namespace, releaseName, operationID, 20*time.Minute)
	}
	key := namespace + "/" + releaseName
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if existing := s.operationLocks[key]; existing != "" && existing != operationID {
		return fmt.Errorf("%w: release %s already has operation %s in progress", domain.ErrConflict, key, existing)
	}
	s.operationLocks[key] = operationID
	return nil
}

func (s *Service) renewOperationLock(ctx context.Context, namespace, releaseName, operationID string) {
	if locker, ok := s.repository.(OperationLocker); ok {
		_ = locker.AcquireHelmOperationLock(ctx, namespace, releaseName, operationID, 20*time.Minute)
	}
}

func (s *Service) releaseOperationLock(ctx context.Context, namespace, releaseName, operationID string) {
	if locker, ok := s.repository.(OperationLocker); ok {
		_ = locker.ReleaseHelmOperationLock(ctx, namespace, releaseName, operationID)
		return
	}
	key := namespace + "/" + releaseName
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.operationLocks[key] == operationID {
		delete(s.operationLocks, key)
	}
}

func (s *Service) plan(ctx context.Context, actor domain.Actor, input domain.HelmChartPlanRequest) (domain.HelmChartPlan, []byte, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return domain.HelmChartPlan{}, nil, err
	}
	normalized, err := normalizePlanRequest(input)
	if err != nil {
		s.audit(ctx, actor, "plan-helm-chart", input.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	executionOptions, err := domain.ResolveHelmExecutionOptions(input.Options)
	if err != nil {
		s.audit(ctx, actor, "plan-helm-chart", input.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	repository, err := s.repository.GetHelmRepository(ctx, normalized.RepositoryID)
	if err != nil {
		s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	if !repository.Enabled {
		err := fmt.Errorf("%w: Helm repository %s is disabled", domain.ErrInvalid, repository.Name)
		s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	archive, actualDigest, err := s.source.Download(ctx, repository, normalized.ChartName, normalized.Version, normalized.Digest)
	if err != nil {
		s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	normalized.Digest = actualDigest
	var supplyChain *domain.HelmSupplyChainStatus
	if verifier, ok := s.source.(SupplyChainVerifier); ok {
		status, verifyErr := verifier.VerifySupplyChain(ctx, repository, normalized.ChartName, normalized.Version)
		if verifyErr != nil {
			s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, verifyErr)
			return domain.HelmChartPlan{}, nil, verifyErr
		}
		if status.Policy != "" && status.Policy != domain.HelmSupplyChainDisabled {
			if err := verifyChartDigestBinding(status, actualDigest); err != nil {
				s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
				return domain.HelmChartPlan{}, nil, err
			}
			supplyChain = &status
		}
	}
	if err := s.authorizeReleasePlan(ctx, actor, normalized, repository); err != nil {
		s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	template, err := s.executor.PlanHelmChart(ctx, actor, archive, normalized)
	if err != nil {
		s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	if err := s.authorizeRenderedResources(ctx, actor, template.Resources); err != nil {
		s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, err)
		return domain.HelmChartPlan{}, nil, err
	}
	plan := domain.HelmChartPlan{
		Action: normalized.Mode, RepositoryID: repository.ID, RepositoryName: repository.Name,
		ChartName: normalized.ChartName, ChartVersion: normalized.Version, Digest: actualDigest,
		Namespace: normalized.Namespace, ReleaseName: normalized.ReleaseName,
		CurrentRevision: template.CurrentRevision, Manifest: redactManifest(template.Manifest),
		Values: redactValues(template.Values), Resources: template.Resources,
		Warnings: append([]string(nil), template.Warnings...), PlanDigest: computePlanDigest(normalized, repository, actualDigest, template, executionOptions), Options: executionOptions,
		SupplyChain: supplyChain,
	}
	for _, resource := range template.Resources {
		if resource.Hook {
			plan.HookCount++
		}
		if resource.CRD {
			plan.CRDCount++
		}
	}
	if plan.HookCount > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("该 Chart 包含 %d 个 Helm Hook，安装或升级时会按 Helm 生命周期执行", plan.HookCount))
	}
	if plan.CRDCount > 0 {
		plan.Warnings = append(plan.Warnings, "Chart 包含 CRD；Helm 不会自动升级已存在的 CRD，请单独评估 CRD 变更")
	}
	if strings.HasPrefix(strings.ToLower(repository.URL), "http://") {
		plan.Warnings = append(plan.Warnings, "该仓库使用明文 HTTP，Chart 与凭据在传输过程中可能被篡改")
	}
	s.audit(ctx, actor, "plan-helm-chart", normalized.ReleaseName, nil)
	return plan, archive, nil
}

func (s *Service) repositoryAndChart(ctx context.Context, repositoryID, chartName string) (domain.HelmRepository, string, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	chartName = strings.TrimSpace(chartName)
	if repositoryID == "" || chartName == "" || !chartNamePattern.MatchString(chartName) {
		return domain.HelmRepository{}, "", fmt.Errorf("%w: repository and chart name are required", domain.ErrInvalid)
	}
	repository, err := s.repository.GetHelmRepository(ctx, repositoryID)
	if err != nil {
		return domain.HelmRepository{}, "", err
	}
	if !repository.Enabled {
		return domain.HelmRepository{}, "", fmt.Errorf("%w: Helm repository %s is disabled", domain.ErrInvalid, repository.Name)
	}
	return repository, chartName, nil
}

func (s *Service) refreshRepository(ctx context.Context, repository domain.HelmRepository, trigger string) (domain.HelmRepository, error) {
	if !s.beginRepositorySync(repository.ID) {
		return decorateRepository(repository), fmt.Errorf("%w: repository %s is already synchronizing", domain.ErrConflict, repository.Name)
	}
	lockID := newOperationID()
	lockName := helmRepositorySyncLockName(repository.ID)
	if err := s.acquireOperationLock(ctx, "dosh-internal", lockName, lockID); err != nil {
		s.endRepositorySync(repository.ID)
		return decorateRepository(repository), fmt.Errorf("%w: repository %s synchronization is owned by another Dosh replica", domain.ErrConflict, repository.Name)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		s.releaseOperationLock(releaseCtx, "dosh-internal", lockName, lockID)
	}()
	return s.refreshRepositoryClaimed(ctx, repository, trigger)
}

func helmRepositorySyncLockName(repositoryID string) string {
	return "helm-repository-sync-" + repositoryID
}

func (s *Service) refreshRepositoryClaimed(ctx context.Context, repository domain.HelmRepository, trigger string) (domain.HelmRepository, error) {
	defer s.endRepositorySync(repository.ID)
	started := time.Now()
	var charts []domain.HelmChartSummary
	var err error
	if refresher, ok := s.source.(RepositoryRefresher); ok {
		charts, err = refresher.Refresh(ctx, repository)
	} else {
		if invalidator, ok := s.source.(interface{ Invalidate(domain.HelmRepository) }); ok {
			invalidator.Invalidate(repository)
		}
		charts, err = s.source.Charts(ctx, repository)
	}
	finished := time.Now().UTC()
	attempt := repository.SyncFailureCount + 1
	sync := domain.HelmRepositorySync{
		OK: err == nil, Trigger: trigger, Attempt: attempt, DurationMS: time.Since(started).Milliseconds(), SyncedAt: finished,
	}
	if err != nil {
		repository.SyncFailureCount = attempt
		sync.Message = truncate(err.Error(), 300)
	} else {
		repository.SyncFailureCount = 0
		sync.Message = "同步成功"
		sync.ChartCount = len(charts)
		repository.ChartCount = len(charts)
	}
	repository.LastSync = &sync
	repository.SyncHistory = append([]domain.HelmRepositorySync{sync}, repository.SyncHistory...)
	if len(repository.SyncHistory) > domain.MaxHelmRepositorySyncHistory {
		repository.SyncHistory = repository.SyncHistory[:domain.MaxHelmRepositorySyncHistory]
	}
	repository = decorateRepository(repository)
	saved, saveErr := s.repository.SaveHelmRepository(ctx, repository)
	if saveErr != nil {
		return repository, errors.Join(err, saveErr)
	}
	return saved, err
}

func (s *Service) beginRepositorySync(id string) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncInFlight[id] {
		return false
	}
	s.syncInFlight[id] = true
	return true
}

func (s *Service) endRepositorySync(id string) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	delete(s.syncInFlight, id)
}

// RunScheduler periodically refreshes repositories whose administrator-enabled
// interval has elapsed. It is safe to call once per Dosh replica; the per-repo
// in-flight guard prevents duplicate work within a process.
func (s *Service) RunScheduler(ctx context.Context) {
	const tick = 30 * time.Second
	lastReconcile := time.Time{}
	lastCacheMaintenance := time.Time{}
	lastLockCleanup := time.Time{}
	maintain := func() {
		now := time.Now()
		if lastReconcile.IsZero() || now.Sub(lastReconcile) >= helmOperationStaleAfter {
			_, _ = s.ReconcileOperations(ctx)
			lastReconcile = now
		}
		if lastCacheMaintenance.IsZero() || now.Sub(lastCacheMaintenance) >= helmCacheMaintenance {
			s.maintainCache()
			lastCacheMaintenance = now
		}
		if lastLockCleanup.IsZero() || now.Sub(lastLockCleanup) >= helmCacheMaintenance {
			if cleaner, ok := s.repository.(OperationLockCleaner); ok {
				_, _ = cleaner.CleanupHelmOperationLocks(ctx)
			}
			lastLockCleanup = now
		}
	}
	maintain()
	s.syncDueRepositories(ctx)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintain()
			s.syncDueRepositories(ctx)
		}
	}
}

// ReconcileOperations marks operations whose owning Dosh process stopped
// heartbeating as failed. It is safe to run on every replica: live operations
// refresh their heartbeat every 30 seconds, well inside the stale threshold.
func (s *Service) ReconcileOperations(ctx context.Context) (int, error) {
	lister, ok := s.repository.(OperationLister)
	if !ok {
		return 0, nil
	}
	now := time.Now().UTC()
	reconciled := 0
	var resultErr error
	for _, status := range []string{domain.HelmOperationPending, domain.HelmOperationRunning} {
		items, err := lister.ListHelmChartOperations(ctx, domain.HelmOperationFilter{Status: status, Limit: helmOperationListLimit})
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		for index := range items {
			operation := items[index]
			lastSeen := operation.HeartbeatAt
			staleAfter := helmOperationStaleAfter
			if lastSeen.IsZero() {
				lastSeen = operation.StartedAt
				// Older Dosh versions did not write heartbeats. During a rolling
				// update, use the full operation timeout before declaring them
				// orphaned so a live previous-version replica is not interrupted.
				staleAfter = helmOperationLegacyStaleAfter
			}
			if !lastSeen.IsZero() && now.Sub(lastSeen) < staleAfter {
				continue
			}
			operation.FinishedAt = now
			if operation.CancelRequested {
				operation.Status = domain.HelmOperationCancelled
				operation.Message = "取消请求已生效；原 Dosh 执行进程已停止心跳"
			} else {
				operation.Status = domain.HelmOperationFailed
				operation.Message = "Helm 操作执行进程已停止心跳，可能因 Dosh 重启、Pod 驱逐或执行器崩溃而中断；请核对 Release 与资源状态后重试"
			}
			if err := s.repository.SaveHelmChartOperation(ctx, operation); err != nil {
				resultErr = errors.Join(resultErr, err)
				continue
			}
			reconciled++
		}
	}
	return reconciled, resultErr
}

func (s *Service) maintainCache() {
	if s.cacheLimit <= 0 {
		return
	}
	cache, ok := s.source.(CacheManager)
	if !ok {
		return
	}
	stats, err := cache.CacheStats()
	if err != nil || stats.TotalBytes <= s.cacheLimit {
		return
	}
	_, _ = cache.CleanupCache(s.cacheLimit)
}

// SyncDueRepositories is exported for deterministic tests and controlled
// operational invocations. It returns the number of repositories attempted.
func (s *Service) SyncDueRepositories(ctx context.Context) int {
	return s.syncDueRepositories(ctx)
}

func (s *Service) syncDueRepositories(ctx context.Context) int {
	items, err := s.repository.ListHelmRepositories(ctx)
	if err != nil {
		return 0
	}
	attempted := 0
	for _, repository := range items {
		repository = decorateRepository(repository)
		if !repository.Enabled || !repository.SyncEnabled || repository.NextSyncAt == nil || time.Now().Before(*repository.NextSyncAt) {
			continue
		}
		if locker, ok := s.repository.(OperationLocker); ok {
			token := newOperationID()
			lockName := helmRepositorySyncLockName(repository.ID)
			if err := locker.AcquireHelmOperationLock(ctx, "dosh-internal", lockName, token, 10*time.Minute); err != nil {
				continue
			}
			attempted++
			go func(item domain.HelmRepository, lockToken, syncLockName string) {
				runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				defer func() {
					releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
					defer releaseCancel()
					_ = locker.ReleaseHelmOperationLock(releaseCtx, "dosh-internal", syncLockName, lockToken)
				}()
				if s.beginRepositorySync(item.ID) {
					_, _ = s.refreshRepositoryClaimed(runCtx, item, domain.HelmRepositorySyncTriggerScheduled)
				}
			}(repository, token, lockName)
			continue
		}
		if s.beginRepositorySync(repository.ID) {
			attempted++
			go func(item domain.HelmRepository) {
				runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				_, _ = s.refreshRepositoryClaimed(runCtx, item, domain.HelmRepositorySyncTriggerScheduled)
			}(repository)
		}
	}
	return attempted
}

func (s *Service) CacheStats(ctx context.Context, actor domain.Actor) (domain.HelmCacheStats, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return domain.HelmCacheStats{}, err
	}
	cache, ok := s.source.(CacheManager)
	if !ok {
		return domain.HelmCacheStats{}, fmt.Errorf("%w: Helm cache management is unavailable", domain.ErrUnsupported)
	}
	stats, err := cache.CacheStats()
	if err == nil {
		stats.LimitBytes = s.cacheLimit
	}
	return stats, err
}

func (s *Service) CleanupCache(ctx context.Context, actor domain.Actor) (domain.HelmCacheCleanupResult, error) {
	if err := s.require(ctx, actor, "update"); err != nil {
		return domain.HelmCacheCleanupResult{}, err
	}
	cache, ok := s.source.(CacheManager)
	if !ok {
		return domain.HelmCacheCleanupResult{}, fmt.Errorf("%w: Helm cache management is unavailable", domain.ErrUnsupported)
	}
	result, err := cache.CleanupCache(s.cacheLimit)
	s.audit(ctx, actor, "cleanup-helm-cache", "", err)
	return result, err
}

func decorateRepository(repository domain.HelmRepository) domain.HelmRepository {
	if repository.SyncIntervalSec <= 0 {
		repository.SyncIntervalSec = int(domain.DefaultHelmRepositorySyncInterval / time.Second)
	}
	if !repository.SyncEnabled {
		repository.NextSyncAt = nil
		return repository
	}
	base := repository.UpdatedAt
	if repository.LastSync != nil && !repository.LastSync.SyncedAt.IsZero() {
		base = repository.LastSync.SyncedAt
	}
	if !base.IsZero() {
		interval := time.Duration(repository.SyncIntervalSec) * time.Second
		delay := interval
		if repository.LastSync != nil && !repository.LastSync.OK && repository.SyncFailureCount > 0 {
			delay = helmSyncRetryDelay(repository.SyncFailureCount, interval)
		}
		next := base.Add(delay).UTC()
		repository.NextSyncAt = &next
	}
	return repository
}

// helmSyncRetryDelay applies exponential backoff to failed scheduled syncs,
// capped by the administrator-configured interval. The first retry is five
// minutes after failure so transient registry or proxy outages recover without
// waiting for the normal multi-hour cadence.
func helmSyncRetryDelay(failureCount int, interval time.Duration) time.Duration {
	if failureCount <= 0 || interval <= 0 {
		return interval
	}
	delay := helmSyncRetryBaseDelay
	if delay > interval {
		return interval
	}
	for attempt := 1; attempt < failureCount && delay < interval; attempt++ {
		if delay > interval/2 {
			return interval
		}
		delay *= 2
	}
	if delay > interval {
		return interval
	}
	return delay
}

func normalizeRepository(input domain.HelmRepositoryUpsert, current domain.HelmRepository) (domain.HelmRepository, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || !repositoryNamePattern.MatchString(name) {
		return domain.HelmRepository{}, fmt.Errorf("%w: name is required and must use letters, numbers, spaces, dots, dashes or underscores", domain.ErrInvalid)
	}
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" || len(rawURL) > maxRepositoryURLSize {
		return domain.HelmRepository{}, fmt.Errorf("%w: repository URL is required and must not exceed %d bytes", domain.ErrInvalid, maxRepositoryURLSize)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return domain.HelmRepository{}, fmt.Errorf("%w: repository URL is invalid", domain.ErrInvalid)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" && parsed.Scheme != "http" && parsed.Scheme != "oci" {
		return domain.HelmRepository{}, fmt.Errorf("%w: repository URL must use HTTP, HTTPS or OCI", domain.ErrInvalid)
	}
	if parsed.Host == "" {
		return domain.HelmRepository{}, fmt.Errorf("%w: repository URL must include a host", domain.ErrInvalid)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return domain.HelmRepository{}, fmt.Errorf("%w: repository URL must not contain credentials, a query or a fragment", domain.ErrInvalid)
	}
	if parsed.Scheme == "oci" {
		cleanPath := strings.Trim(parsed.Path, "/")
		if cleanPath == "" || strings.ContainsAny(cleanPath, " \t\r\n@") {
			return domain.HelmRepository{}, fmt.Errorf("%w: OCI repository URL must point to one Chart path", domain.ErrInvalid)
		}
		chartName := path.Base(cleanPath)
		if !chartNamePattern.MatchString(chartName) || strings.Contains(chartName, ":") {
			return domain.HelmRepository{}, fmt.Errorf("%w: OCI repository URL must end with a valid Chart name and must not include a tag", domain.ErrInvalid)
		}
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return domain.HelmRepository{}, fmt.Errorf("%w: loopback Helm repository endpoints are not allowed", domain.ErrInvalid)
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return domain.HelmRepository{}, fmt.Errorf("%w: loopback and link-local Helm repository endpoints are not allowed", domain.ErrInvalid)
	}
	parsed.RawPath = ""
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if current.ID != "" && !strings.EqualFold(parsed.Hostname(), repositoryHost(current.URL)) && helmCredentialsStored(current.Credentials) && !input.ClearCredentials && !helmCredentialInputProvided(input) {
		return domain.HelmRepository{}, fmt.Errorf("%w: changing the repository host requires re-entering credentials or explicitly clearing them", domain.ErrInvalid)
	}
	enabled := true
	if current.ID != "" {
		enabled = current.Enabled
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	syncEnabled := current.SyncEnabled
	if input.SyncEnabled != nil {
		syncEnabled = *input.SyncEnabled
	}
	syncIntervalSec := current.SyncIntervalSec
	if syncIntervalSec <= 0 {
		syncIntervalSec = int(domain.DefaultHelmRepositorySyncInterval / time.Second)
	}
	if input.SyncIntervalSec > 0 {
		syncIntervalSec = input.SyncIntervalSec
	}
	if syncIntervalSec < int(domain.MinHelmRepositorySyncInterval/time.Second) || syncIntervalSec > int(domain.MaxHelmRepositorySyncInterval/time.Second) {
		return domain.HelmRepository{}, fmt.Errorf("%w: sync interval must be between %s and %s", domain.ErrInvalid, domain.MinHelmRepositorySyncInterval, domain.MaxHelmRepositorySyncInterval)
	}
	authType := strings.ToLower(strings.TrimSpace(input.AuthType))
	if authType == "" {
		if current.ID != "" {
			authType = current.AuthType
		}
		if authType == "" {
			authType = inferHelmAuthType(current.Credentials)
		}
	}
	if authType == "" {
		authType = domain.HelmRepositoryAuthAnonymous
	}
	if authType != domain.HelmRepositoryAuthAnonymous && authType != domain.HelmRepositoryAuthBasic && authType != domain.HelmRepositoryAuthBearer {
		return domain.HelmRepository{}, fmt.Errorf("%w: authType must be anonymous, basic or bearer", domain.ErrInvalid)
	}
	credentials := current.Credentials
	if current.ID != "" && !strings.EqualFold(parsed.Hostname(), repositoryHost(current.URL)) {
		credentials = domain.HelmRepositoryCredentials{}
	}
	if input.ClearCredentials {
		credentials = domain.HelmRepositoryCredentials{}
		authType = domain.HelmRepositoryAuthAnonymous
	} else {
		if strings.TrimSpace(input.Username) != "" {
			credentials.Username = strings.TrimSpace(input.Username)
		}
		if input.Password != "" {
			credentials.Password = input.Password
		}
		if strings.TrimSpace(input.Token) != "" {
			credentials.Token = strings.TrimSpace(input.Token)
		}
		if strings.TrimSpace(input.CAPEM) != "" {
			credentials.CAPEM = strings.TrimSpace(input.CAPEM)
		}
		switch authType {
		case domain.HelmRepositoryAuthAnonymous:
			credentials.Username = ""
			credentials.Password = ""
			credentials.Token = ""
		case domain.HelmRepositoryAuthBasic:
			credentials.Token = ""
			if credentials.Username == "" || credentials.Password == "" {
				return domain.HelmRepository{}, fmt.Errorf("%w: basic authentication requires username and password", domain.ErrInvalid)
			}
		case domain.HelmRepositoryAuthBearer:
			credentials.Username = ""
			credentials.Password = ""
			if credentials.Token == "" {
				return domain.HelmRepository{}, fmt.Errorf("%w: bearer authentication requires a token", domain.ErrInvalid)
			}
		}
	}
	if err := validateHelmCredentials(credentials); err != nil {
		return domain.HelmRepository{}, err
	}
	supplyChain, err := normalizeSupplyChainPolicy(parsed.Scheme == "oci", current.SupplyChain, input.SupplyChain)
	if err != nil {
		return domain.HelmRepository{}, err
	}
	return domain.HelmRepository{
		Name: name, URL: parsed.String(), Description: strings.TrimSpace(input.Description), Default: input.Default,
		Enabled: enabled, SyncEnabled: syncEnabled, SyncIntervalSec: syncIntervalSec,
		InsecureSkipTLS: input.InsecureSkipTLS, AuthType: authType, Credentials: credentials,
		SupplyChain: supplyChain,
		CreatedAt:   current.CreatedAt,
	}, nil
}

func repositoryHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func helmCredentialInputProvided(input domain.HelmRepositoryUpsert) bool {
	return strings.TrimSpace(input.Username) != "" || input.Password != "" || strings.TrimSpace(input.Token) != "" || strings.TrimSpace(input.CAPEM) != ""
}

func helmCredentialsStored(credentials domain.HelmRepositoryCredentials) bool {
	return strings.TrimSpace(credentials.Username) != "" || strings.TrimSpace(credentials.Password) != "" || strings.TrimSpace(credentials.Token) != "" || strings.TrimSpace(credentials.CAPEM) != ""
}

func inferHelmAuthType(credentials domain.HelmRepositoryCredentials) string {
	switch {
	case strings.TrimSpace(credentials.Token) != "":
		return domain.HelmRepositoryAuthBearer
	case strings.TrimSpace(credentials.Username) != "" || credentials.Password != "":
		return domain.HelmRepositoryAuthBasic
	default:
		return domain.HelmRepositoryAuthAnonymous
	}
}

func normalizePlanRequest(input domain.HelmChartPlanRequest) (domain.HelmChartPlanRequest, error) {
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode != "install" && input.Mode != "upgrade" {
		return domain.HelmChartPlanRequest{}, fmt.Errorf("%w: mode must be install or upgrade", domain.ErrInvalid)
	}
	input.RepositoryID = strings.TrimSpace(input.RepositoryID)
	input.ChartName = strings.TrimSpace(input.ChartName)
	input.Version = strings.TrimSpace(input.Version)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.ReleaseName = strings.TrimSpace(input.ReleaseName)
	if input.RepositoryID == "" || input.ChartName == "" || input.Version == "" || input.Namespace == "" || input.ReleaseName == "" {
		return domain.HelmChartPlanRequest{}, fmt.Errorf("%w: repository, chart, version, namespace and release name are required", domain.ErrInvalid)
	}
	if !chartNamePattern.MatchString(input.ChartName) || len(input.Version) > 128 || strings.ContainsAny(input.Version, "/ \t\r\n") {
		return domain.HelmChartPlanRequest{}, fmt.Errorf("%w: chart name or version is invalid", domain.ErrInvalid)
	}
	if len(input.ReleaseName) > 53 || len(input.Namespace) > 63 {
		return domain.HelmChartPlanRequest{}, fmt.Errorf("%w: release name or namespace is too long", domain.ErrInvalid)
	}
	if len(input.Values) > maxChartValuesSize {
		return domain.HelmChartPlanRequest{}, fmt.Errorf("%w: values must not exceed 1 MiB", domain.ErrInvalid)
	}
	return input, nil
}

func validateHelmCredentials(credentials domain.HelmRepositoryCredentials) error {
	for _, value := range []string{credentials.Username, credentials.Password, credentials.Token} {
		if len(value) > maxCredentialSize {
			return fmt.Errorf("%w: Helm repository credential fields are too large", domain.ErrInvalid)
		}
	}
	if len(credentials.CAPEM) > maxCASize {
		return fmt.Errorf("%w: Helm repository custom CA is too large", domain.ErrInvalid)
	}
	return nil
}

func (s *Service) authorizeReleasePlan(ctx context.Context, actor domain.Actor, request domain.HelmChartPlanRequest, repository domain.HelmRepository) error {
	if err := s.requireReleaseRead(ctx, actor, request.Namespace); err != nil {
		return err
	}
	if err := s.requireReleaseWrite(ctx, actor, request.Namespace, "create"); err != nil {
		return err
	}
	if err := s.requireReleaseWrite(ctx, actor, request.Namespace, "update"); err != nil {
		return err
	}
	if strings.HasPrefix(strings.ToLower(repository.URL), "http://") && !actor.Admin {
		return fmt.Errorf("%w: only administrators may use an insecure HTTP Helm repository", domain.ErrForbidden)
	}
	return nil
}

func (s *Service) authorizeRenderedResources(ctx context.Context, actor domain.Actor, resources []domain.HelmChartPlanResource) error {
	for _, resource := range resources {
		access := domain.Access{APIGroup: resource.APIGroup, Resource: resource.Resource, Namespace: resource.Namespace, Name: resource.Name, Verb: resource.Action}
		err := s.authorizer.Require(ctx, actor, access)
		if errors.Is(err, domain.ErrForbidden) && resource.Action == "update" {
			access.Verb = "patch"
			err = s.authorizer.Require(ctx, actor, access)
		}
		if err != nil {
			return fmt.Errorf("%w: %s %s/%s: %v", domain.ErrForbidden, resource.Kind, resource.Namespace, resource.Name, err)
		}
	}
	return nil
}

func (s *Service) requireReleaseRead(ctx context.Context, actor domain.Actor, namespace string) error {
	return s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", Resource: "secrets", Namespace: namespace})
}

func (s *Service) requireReleaseWrite(ctx context.Context, actor domain.Actor, namespace, verb string) error {
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: verb, Resource: "secrets", Namespace: namespace}); err != nil {
		return err
	}
	return s.authorizer.Require(ctx, actor, domain.Access{Verb: "update", Resource: "secrets", Namespace: namespace})
}

func (s *Service) require(ctx context.Context, actor domain.Actor, verb string) error {
	if s.authorizer == nil {
		return domain.ErrForbidden
	}
	return s.authorizer.Require(ctx, actor, domain.Access{Verb: verb, APIGroup: "dosh.io", Resource: "helm-repositories"})
}

func (s *Service) requireRead(ctx context.Context, actor domain.Actor) error {
	return s.require(ctx, actor, "list")
}

func (s *Service) newOperation(action string, request domain.HelmChartPlanRequest, plan domain.HelmChartPlan) domain.HelmChartOperation {
	operation := newHelmOperation(action, request, plan.Digest, plan.Resources, plan.Warnings)
	operation.Options = &plan.Options
	return operation
}

func (s *Service) newOperationFromTemplate(action string, request domain.HelmChartPlanRequest, plan domain.HelmChartPlanTemplate) domain.HelmChartOperation {
	return newHelmOperation(action, request, "", plan.Resources, plan.Warnings)
}

func newHelmOperation(action string, request domain.HelmChartPlanRequest, digest string, planResources []domain.HelmChartPlanResource, planWarnings []string) domain.HelmChartOperation {
	resources := make([]domain.HelmOperationResource, 0, len(planResources))
	for _, resource := range planResources {
		if len(resources) >= maxOperationResources {
			break
		}
		message := "等待 Helm 创建资源"
		switch resource.Action {
		case "update":
			message = "等待 Helm 更新资源"
		case "delete":
			message = "等待 Helm 删除资源"
		}
		if resource.Hook {
			message = "等待 Helm 执行 Hook"
		}
		resources = append(resources, domain.HelmOperationResource{
			APIVersion: resource.APIVersion, Kind: resource.Kind, Namespace: resource.Namespace,
			Name: resource.Name, Hook: resource.Hook, CRD: resource.CRD, Action: resource.Action,
			APIGroup: resource.APIGroup, Resource: resource.Resource, Status: "pending", Message: message,
		})
	}
	warnings := append([]string(nil), planWarnings...)
	if len(planResources) > maxOperationResources {
		warnings = append(warnings, fmt.Sprintf("该操作包含 %d 个资源，进度列表仅展示前 %d 个", len(planResources), maxOperationResources))
	}
	return domain.HelmChartOperation{
		ID: newOperationID(), Type: action, Status: domain.HelmOperationRunning,
		RepositoryID: request.RepositoryID, ChartName: request.ChartName, ChartVersion: request.Version,
		Digest: digest, Namespace: request.Namespace, ReleaseName: request.ReleaseName,
		StartedAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC(), Resources: resources, Warnings: warnings,
	}
}

func (s *Service) saveOperation(ctx context.Context, operation *domain.HelmChartOperation) error {
	return s.repository.SaveHelmChartOperation(ctx, *operation)
}

func (s *Service) finishOperation(ctx context.Context, operation *domain.HelmChartOperation, actionErr error) {
	operation.FinishedAt = time.Now().UTC()
	operation.HeartbeatAt = operation.FinishedAt
	if actionErr != nil {
		operation.Status = domain.HelmOperationFailed
		if errors.Is(actionErr, context.Canceled) {
			operation.Status = domain.HelmOperationCancelled
		}
		if operation.CancelRequested {
			operation.Message = "操作已响应取消请求并停止"
		} else {
			operation.Message = truncate(actionErr.Error(), 1000)
		}
	} else {
		operation.Status = domain.HelmOperationSuccess
		operation.Message = "操作成功"
	}
	if err := s.repository.SaveHelmChartOperation(ctx, *operation); err != nil {
		operation.Warnings = append(operation.Warnings, "操作已执行，但操作记录保存失败："+truncate(err.Error(), 200))
	}
}

func (s *Service) runOperation(parent context.Context, operation domain.HelmChartOperation, actor domain.Actor, action string, execute func(context.Context) (int, error)) {
	timeout := helmOperationTimeout
	if operation.Options != nil && operation.Options.TimeoutSeconds > 0 {
		timeout = time.Duration(operation.Options.TimeoutSeconds)*time.Second + 2*time.Minute
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	s.operationMu.Lock()
	s.operationCancels[operation.ID] = cancel
	s.operationMu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.operationMu.Lock()
			delete(s.operationCancels, operation.ID)
			s.operationMu.Unlock()
			releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
			defer releaseCancel()
			s.releaseOperationLock(releaseCtx, operation.Namespace, operation.ReleaseName, operation.ID)
		}()

		var operationMu sync.Mutex
		var saveMu sync.Mutex
		persist := func(saveCtx context.Context, mutate func()) {
			saveMu.Lock()
			defer saveMu.Unlock()
			operationMu.Lock()
			mutate()
			snapshot := operation
			operationMu.Unlock()
			_ = s.saveOperation(saveCtx, &snapshot)
		}

		stopHeartbeat := make(chan struct{})
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			ticker := time.NewTicker(helmOperationHeartbeat)
			defer ticker.Stop()
			for {
				select {
				case <-stopHeartbeat:
					return
				case <-ticker.C:
					heartbeatCtx, heartbeatCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
					stored, getErr := s.repository.GetHelmChartOperation(heartbeatCtx, operation.ID)
					cancelRequested := getErr == nil && stored.CancelRequested
					persist(heartbeatCtx, func() {
						operation.HeartbeatAt = time.Now().UTC()
						if cancelRequested {
							operation.CancelRequested = true
						}
					})
					renewCtx, renewCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
					s.renewOperationLock(renewCtx, operation.Namespace, operation.ReleaseName, operation.ID)
					renewCancel()
					heartbeatCancel()
					if cancelRequested {
						cancel()
					}
				}
			}
		}()

		stopProgress := make(chan struct{})
		progressDone := make(chan struct{})
		observer, canObserve := s.executor.(HelmResourceObserver)
		if canObserve && len(operation.Resources) > 0 {
			planned := operationPlanResources(operation.Resources)
			go func() {
				defer close(progressDone)
				ticker := time.NewTicker(helmProgressInterval)
				defer ticker.Stop()
				observe := func() {
					resources := observer.ObserveHelmOperationResources(runCtx, planned)
					if len(resources) == 0 {
						return
					}
					persist(runCtx, func() { operation.Resources = resources })
				}
				observe()
				for {
					select {
					case <-ticker.C:
						observe()
					case <-stopProgress:
						return
					}
				}
			}()
		} else {
			close(progressDone)
		}

		revision, err := execute(runCtx)
		close(stopHeartbeat)
		<-heartbeatDone
		close(stopProgress)
		<-progressDone

		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
		defer finishCancel()
		if stored, getErr := s.repository.GetHelmChartOperation(finishCtx, operation.ID); getErr == nil && stored.CancelRequested {
			operationMu.Lock()
			operation.CancelRequested = true
			operationMu.Unlock()
		}
		saveMu.Lock()
		defer saveMu.Unlock()
		operationMu.Lock()
		operation.Revision = revision
		finished := operation
		operationMu.Unlock()

		s.finishOperation(finishCtx, &finished, err)
		s.audit(finishCtx, actor, action, operation.ReleaseName, err)
	}()
}

func operationPlanResources(resources []domain.HelmOperationResource) []domain.HelmChartPlanResource {
	planned := make([]domain.HelmChartPlanResource, 0, len(resources))
	for _, resource := range resources {
		planned = append(planned, domain.HelmChartPlanResource{
			APIVersion: resource.APIVersion, Kind: resource.Kind, Namespace: resource.Namespace,
			Name: resource.Name, Hook: resource.Hook, CRD: resource.CRD, Action: resource.Action,
			APIGroup: resource.APIGroup, Resource: resource.Resource,
		})
	}
	return planned
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action string, name string, err error) {
	if s.auditor == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) || errors.Is(err, domain.ErrInvalid) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "helm-chart", Name: name, Result: result, Err: err})
}

func computePlanDigest(request domain.HelmChartPlanRequest, repository domain.HelmRepository, actualDigest string, template domain.HelmChartPlanTemplate, options domain.HelmExecutionOptions) string {
	payload := struct {
		Action          string                         `json:"action"`
		RepositoryID    string                         `json:"repositoryId"`
		RepositoryURL   string                         `json:"repositoryUrl"`
		ChartName       string                         `json:"chartName"`
		ChartVersion    string                         `json:"chartVersion"`
		Digest          string                         `json:"digest"`
		Namespace       string                         `json:"namespace"`
		ReleaseName     string                         `json:"releaseName"`
		CurrentRevision int                            `json:"currentRevision"`
		Values          string                         `json:"values"`
		Resources       []domain.HelmChartPlanResource `json:"resources"`
		Options         domain.HelmExecutionOptions    `json:"options"`
	}{
		Action: request.Mode, RepositoryID: repository.ID, RepositoryURL: repository.URL,
		ChartName: request.ChartName, ChartVersion: request.Version, Digest: actualDigest,
		Namespace: request.Namespace, ReleaseName: request.ReleaseName, Options: options,
		CurrentRevision: template.CurrentRevision, Values: template.Values, Resources: template.Resources,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func comparePlanDigest(requested, actual string) error {
	if strings.TrimSpace(requested) == "" {
		return fmt.Errorf("%w: planDigest is required; refresh the install plan", domain.ErrInvalid)
	}
	if requested != actual {
		return fmt.Errorf("%w: install plan changed or expired; review the refreshed plan before retrying", domain.ErrConflict)
	}
	return nil
}

func requireConfirmation(confirmation, releaseName string) error {
	if strings.TrimSpace(releaseName) == "" || strings.TrimSpace(confirmation) != strings.TrimSpace(releaseName) {
		return fmt.Errorf("%w: type the release name to confirm this operation", domain.ErrInvalid)
	}
	return nil
}

func stripRepositorySecret(repository domain.HelmRepository) domain.HelmRepository {
	repository.Credentials = domain.HelmRepositoryCredentials{}
	return repository
}

func stripRepositorySecrets(items []domain.HelmRepository) []domain.HelmRepository {
	for index := range items {
		items[index] = stripRepositorySecret(items[index])
	}
	return items
}

func credentialsEqual(left, right domain.HelmRepositoryCredentials) bool {
	return left.Username == right.Username && left.Password == right.Password && left.Token == right.Token && left.CAPEM == right.CAPEM
}

func newID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func newOperationID() string {
	id, err := newID()
	if err != nil {
		return fmt.Sprintf("op-%d", time.Now().UnixNano())
	}
	return id
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func redactValues(raw string) string {
	var object map[string]any
	if err := yaml.Unmarshal([]byte(raw), &object); err != nil {
		return "# values 解析失败，内容已隐藏"
	}
	maskSensitive(object)
	encoded, err := yaml.Marshal(object)
	if err != nil {
		return "# values 无法安全序列化，内容已隐藏"
	}
	return string(encoded)
}

func redactManifest(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	documents := strings.Split(raw, "\n---")
	redacted := make([]string, 0, len(documents))
	for _, document := range documents {
		document = strings.TrimSpace(strings.TrimPrefix(document, "---"))
		if document == "" {
			continue
		}
		var object map[string]any
		if err := yaml.Unmarshal([]byte(document), &object); err != nil {
			return "# manifest 无法安全解析，内容已隐藏"
		}
		maskSensitive(object)
		if strings.EqualFold(stringValue(object["kind"]), "Secret") {
			for _, key := range []string{"data", "stringData"} {
				if values, ok := object[key].(map[string]any); ok {
					for name := range values {
						values[name] = "<redacted>"
					}
				}
			}
		}
		encoded, err := yaml.Marshal(object)
		if err != nil {
			return "# manifest 无法安全序列化，内容已隐藏"
		}
		redacted = append(redacted, strings.TrimSpace(string(encoded)))
	}
	return strings.Join(redacted, "\n---\n")
}

func maskSensitive(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if name, ok := typed["name"].(string); ok && sensitiveKey.MatchString(name) {
			if _, hasValue := typed["value"]; hasValue {
				typed["value"] = "<redacted>"
			}
		}
		for key, nested := range typed {
			if sensitiveKey.MatchString(key) {
				typed[key] = "<redacted>"
				continue
			}
			maskSensitive(nested)
		}
	case []any:
		for _, nested := range typed {
			maskSensitive(nested)
		}
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
