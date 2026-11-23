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
	maxRepositoryNameLength = 128
	maxRepositoryURLSize    = 2048
	maxCredentialSize       = 16 << 10
	maxCASize               = 64 << 10
	maxChartValuesSize      = 1024 * 1024
	helmOperationTimeout    = 12 * time.Minute
	helmProgressInterval    = 3 * time.Second
	maxOperationResources   = 300
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

type ChartRefresher interface {
	RefreshChart(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error)
}

type Executor interface {
	PlanHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error)
	InstallHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (int, error)
	UpgradeHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (int, error)
	UninstallHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string) error
	RollbackHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, targetRevision int) (int, error)
}

// HelmResourceObserver exposes live object status for an operation. Executors
// that cannot observe resources still work; runOperation then keeps the plan
// snapshot without attempting progress reads.
type HelmResourceObserver interface {
	ObserveHelmOperationResources(ctx context.Context, resources []domain.HelmChartPlanResource) []domain.HelmOperationResource
}

type Service struct {
	repository Repository
	source     Source
	executor   Executor
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, source Source, executor Executor, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, source: source, executor: executor, authorizer: authorizer, auditor: auditor}
}

func (s *Service) ListRepositories(ctx context.Context, actor domain.Actor) ([]domain.HelmRepository, error) {
	if err := s.require(ctx, actor, "list"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListHelmRepositories(ctx)
	if err != nil {
		return nil, err
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
	saved = s.refreshBestEffort(ctx, saved)
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
	if repository.URL == current.URL && repository.InsecureSkipTLS == current.InsecureSkipTLS && credentialsEqual(repository.Credentials, current.Credentials) {
		repository.LastSync = current.LastSync
		repository.ChartCount = current.ChartCount
	}
	saved, err := s.repository.SaveHelmRepository(ctx, repository)
	if err != nil {
		s.audit(ctx, actor, "update-helm-repository", id, err)
		return domain.HelmRepository{}, err
	}
	saved = s.refreshBestEffort(ctx, saved)
	s.audit(ctx, actor, "update-helm-repository", id, nil)
	return stripRepositorySecret(saved), nil
}

func (s *Service) DeleteRepository(ctx context.Context, actor domain.Actor, id, revision string) error {
	if err := s.require(ctx, actor, "delete"); err != nil {
		s.audit(ctx, actor, "delete-helm-repository", id, err)
		return err
	}
	err := s.repository.DeleteHelmRepository(ctx, id, revision)
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
	repository = s.refreshBestEffort(ctx, repository)
	s.audit(ctx, actor, "sync-helm-repository", id, nil)
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
	s.audit(ctx, actor, "get-helm-chart", chartName, nil)
	return detail, nil
}

// SyncChart forces a fresh Chart package download and refreshes the metadata
// extracted from that package. It does not re-synchronize the repository index.
func (s *Service) SyncChart(ctx context.Context, actor domain.Actor, repositoryID, chartName string) (domain.HelmChartDetail, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return domain.HelmChartDetail{}, err
	}
	repository, chartName, err := s.repositoryAndChart(ctx, repositoryID, chartName)
	if err != nil {
		return domain.HelmChartDetail{}, err
	}
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
	s.audit(ctx, actor, "sync-helm-chart", chartName, nil)
	return detail, nil
}

func (s *Service) Plan(ctx context.Context, actor domain.Actor, input domain.HelmChartPlanRequest) (domain.HelmChartPlan, error) {
	plan, _, err := s.plan(ctx, actor, input)
	return plan, err
}

func (s *Service) Install(ctx context.Context, actor domain.Actor, input domain.HelmChartOperationRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	input.Mode = "install"
	plan, archive, err := s.plan(ctx, actor, input.HelmChartPlanRequest)
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if err := comparePlanDigest(input.PlanDigest, plan.PlanDigest); err != nil {
		s.audit(ctx, actor, "install-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	operation := s.newOperation("install", input.HelmChartPlanRequest, plan)
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	s.runOperation(ctx, operation, actor, "install-helm-chart", func(runCtx context.Context) (int, error) {
		return s.executor.InstallHelmChart(runCtx, actor, archive, input.HelmChartPlanRequest)
	})
	return operation, nil
}

func (s *Service) Upgrade(ctx context.Context, actor domain.Actor, input domain.HelmChartOperationRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	input.Mode = "upgrade"
	plan, archive, err := s.plan(ctx, actor, input.HelmChartPlanRequest)
	if err != nil {
		return domain.HelmChartOperation{}, err
	}
	if err := comparePlanDigest(input.PlanDigest, plan.PlanDigest); err != nil {
		s.audit(ctx, actor, "upgrade-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	operation := s.newOperation("upgrade", input.HelmChartPlanRequest, plan)
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	s.runOperation(ctx, operation, actor, "upgrade-helm-chart", func(runCtx context.Context) (int, error) {
		return s.executor.UpgradeHelmChart(runCtx, actor, archive, input.HelmChartPlanRequest)
	})
	return operation, nil
}

func (s *Service) Uninstall(ctx context.Context, actor domain.Actor, input domain.HelmChartUninstallRequest) (domain.HelmChartOperation, error) {
	if err := requireConfirmation(input.Confirmation, input.ReleaseName); err != nil {
		return domain.HelmChartOperation{}, err
	}
	if err := s.requireReleaseWrite(ctx, actor, input.Namespace, "delete"); err != nil {
		s.audit(ctx, actor, "uninstall-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	operation := domain.HelmChartOperation{
		ID: newOperationID(), Type: "uninstall", Status: domain.HelmOperationRunning,
		Namespace: input.Namespace, ReleaseName: input.ReleaseName, StartedAt: time.Now().UTC(),
		Warnings: []string{"卸载会删除 Helm 管理的资源；默认保留的 PVC、CRD 和外部资源需要单独清理"},
	}
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	s.runOperation(ctx, operation, actor, "uninstall-helm-chart", func(runCtx context.Context) (int, error) {
		return 0, s.executor.UninstallHelmChart(runCtx, actor, input.Namespace, input.ReleaseName)
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
	if err := s.requireReleaseWrite(ctx, actor, input.Namespace, "update"); err != nil {
		s.audit(ctx, actor, "rollback-helm-chart", input.ReleaseName, err)
		return domain.HelmChartOperation{}, err
	}
	operation := domain.HelmChartOperation{
		ID: newOperationID(), Type: "rollback", Status: domain.HelmOperationRunning,
		Namespace: input.Namespace, ReleaseName: input.ReleaseName, StartedAt: time.Now().UTC(),
	}
	if err := s.saveOperation(ctx, &operation); err != nil {
		return domain.HelmChartOperation{}, err
	}
	s.runOperation(ctx, operation, actor, "rollback-helm-chart", func(runCtx context.Context) (int, error) {
		return s.executor.RollbackHelmChart(runCtx, actor, input.Namespace, input.ReleaseName, input.TargetRevision)
	})
	return operation, nil
}

func (s *Service) Operation(ctx context.Context, actor domain.Actor, id string) (domain.HelmChartOperation, error) {
	if err := s.requireRead(ctx, actor); err != nil {
		return domain.HelmChartOperation{}, err
	}
	if strings.TrimSpace(id) == "" {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: operation id is required", domain.ErrInvalid)
	}
	return s.repository.GetHelmChartOperation(ctx, id)
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
		Warnings: append([]string(nil), template.Warnings...), PlanDigest: computePlanDigest(normalized, repository, actualDigest, template),
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

func (s *Service) refreshBestEffort(ctx context.Context, repository domain.HelmRepository) domain.HelmRepository {
	if invalidator, ok := s.source.(interface{ Invalidate(domain.HelmRepository) }); ok {
		invalidator.Invalidate(repository)
	}
	started := time.Now().UTC()
	charts, err := s.source.Charts(ctx, repository)
	sync := &domain.HelmRepositorySync{OK: err == nil, SyncedAt: started}
	if err != nil {
		sync.Message = truncate(err.Error(), 300)
	} else {
		sync.Message = "同步成功"
		sync.ChartCount = len(charts)
		repository.ChartCount = len(charts)
	}
	repository.LastSync = sync
	saved, saveErr := s.repository.SaveHelmRepository(ctx, repository)
	if saveErr != nil {
		return repository
	}
	return saved
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
	return domain.HelmRepository{
		Name: name, URL: parsed.String(), Description: strings.TrimSpace(input.Description), Default: input.Default,
		Enabled: enabled, InsecureSkipTLS: input.InsecureSkipTLS, AuthType: authType, Credentials: credentials,
		CreatedAt: current.CreatedAt,
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
	resources := make([]domain.HelmOperationResource, 0, len(plan.Resources))
	for _, resource := range plan.Resources {
		if len(resources) >= maxOperationResources {
			break
		}
		message := "等待 Helm 创建资源"
		if resource.Action == "update" {
			message = "等待 Helm 更新资源"
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
	warnings := append([]string(nil), plan.Warnings...)
	if len(plan.Resources) > maxOperationResources {
		warnings = append(warnings, fmt.Sprintf("该操作包含 %d 个资源，进度列表仅展示前 %d 个", len(plan.Resources), maxOperationResources))
	}
	return domain.HelmChartOperation{
		ID: newOperationID(), Type: action, Status: domain.HelmOperationRunning,
		RepositoryID: request.RepositoryID, ChartName: request.ChartName, ChartVersion: request.Version,
		Digest: plan.Digest, Namespace: request.Namespace, ReleaseName: request.ReleaseName,
		StartedAt: time.Now().UTC(), Resources: resources, Warnings: warnings,
	}
}

func (s *Service) saveOperation(ctx context.Context, operation *domain.HelmChartOperation) error {
	return s.repository.SaveHelmChartOperation(ctx, *operation)
}

func (s *Service) finishOperation(ctx context.Context, operation *domain.HelmChartOperation, actionErr error) {
	operation.FinishedAt = time.Now().UTC()
	if actionErr != nil {
		operation.Status = domain.HelmOperationFailed
		operation.Message = truncate(actionErr.Error(), 1000)
	} else {
		operation.Status = domain.HelmOperationSuccess
		operation.Message = "操作成功"
	}
	if err := s.repository.SaveHelmChartOperation(ctx, *operation); err != nil {
		operation.Warnings = append(operation.Warnings, "操作已执行，但操作记录保存失败："+truncate(err.Error(), 200))
	}
}

func (s *Service) runOperation(parent context.Context, operation domain.HelmChartOperation, actor domain.Actor, action string, execute func(context.Context) (int, error)) {
	go func() {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), helmOperationTimeout)
		defer cancel()

		var operationMu sync.Mutex
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
					operationMu.Lock()
					operation.Resources = resources
					snapshot := operation
					operationMu.Unlock()
					_ = s.saveOperation(runCtx, &snapshot)
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
		close(stopProgress)
		<-progressDone

		operationMu.Lock()
		operation.Revision = revision
		finished := operation
		operationMu.Unlock()

		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
		defer finishCancel()
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

func computePlanDigest(request domain.HelmChartPlanRequest, repository domain.HelmRepository, actualDigest string, template domain.HelmChartPlanTemplate) string {
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
	}{
		Action: request.Mode, RepositoryID: repository.ID, RepositoryURL: repository.URL,
		ChartName: request.ChartName, ChartVersion: request.Version, Digest: actualDigest,
		Namespace: request.Namespace, ReleaseName: request.ReleaseName,
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
