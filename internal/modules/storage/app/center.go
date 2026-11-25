package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"
)

const defaultStorageSmokeImage = "registry.k8s.io/pause:3.10"

var (
	storageSmokeImagePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,511}$`)
	storageReleaseNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// HelmCatalog is the read-only portion of the existing Helm service. Keeping
// this narrow lets the Storage Center reuse trusted repository resolution and
// RBAC without duplicating Helm networking, cache or credential logic.
type HelmCatalog interface {
	ListRepositories(ctx context.Context, actor domain.Actor) ([]domain.HelmRepository, error)
	Charts(ctx context.Context, actor domain.Actor, repositoryID, search string) ([]domain.HelmChartSummary, []string, error)
}

// CenterRepository is implemented by the Kubernetes client. It is intentionally
// separate from Repository so existing storage tests and adapters remain valid.
type CenterRepository interface {
	ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error)
	ListNodes(ctx context.Context, query domain.ListQuery) ([]domain.NodeSummary, error)
	ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error)
	ListCSIDrivers(ctx context.Context) ([]domain.CSIDriverInfo, error)
}

type HelmReleaseReader interface {
	ListHelmReleases(ctx context.Context, namespace string) ([]domain.HelmReleaseSummary, []string, error)
}

type StorageSmokeTester interface {
	RunStorageSmokeTest(ctx context.Context, spec domain.StorageSmokeTestSpec) (domain.StorageSmokeTestResult, error)
}

func (s *Service) SetHelmCatalog(catalog HelmCatalog) {
	s.helmCatalog = catalog
}

func (s *Service) StorageCenter(ctx context.Context, actor domain.Actor) (domain.StorageCenter, error) {
	center := domain.StorageCenter{GeneratedAt: time.Now().UTC(), Catalog: []domain.StorageCatalogItem{}, Integrations: []domain.StorageIntegration{}, Diagnostics: []domain.StorageDiagnostic{}}
	centerRepo, ok := s.repository.(CenterRepository)
	if !ok {
		return center, fmt.Errorf("%w: storage center discovery is unavailable", domain.ErrUnavailable)
	}
	classes, err := s.repository.ListStorageClasses(ctx, domain.ListQuery{})
	if err != nil {
		s.auditStorage(ctx, actor, "read-storage-center", "storagecenter", "", "", err)
		return center, err
	}
	center.Summary.StorageClassCount = len(classes)
	center.Summary.DefaultStorageClassCount = countDefaultStorageClasses(classes)
	center.Summary.NodeCount, center.Summary.ReadyNodeCount = s.storageNodeSummary(ctx, centerRepo, &center.Warnings)
	center.Summary.PersistentVolumeCount = s.storageVolumeCount(ctx, &center.Warnings)
	claims, claimErr := s.repository.ListPersistentVolumeClaims(ctx, domain.ListQuery{})
	if claimErr != nil {
		center.Warnings = append(center.Warnings, "无法读取 PVC："+claimErr.Error())
	} else {
		center.Summary.PersistentVolumeClaimCount = len(claims)
		center.Summary.PendingClaimCount = countPendingClaims(claims)
	}
	if feature, err := s.repository.VolumeSnapshotFeature(ctx); err == nil {
		center.Summary.VolumeSnapshotAvailable = feature.Available
	}
	drivers, driverErr := centerRepo.ListCSIDrivers(ctx)
	if driverErr != nil {
		center.Warnings = append(center.Warnings, "无法读取 CSIDriver："+driverErr.Error())
	}
	pods, podErr := centerRepo.ListPods(ctx, domain.ListQuery{})
	if podErr != nil {
		center.Warnings = append(center.Warnings, "无法读取驱动工作负载："+podErr.Error())
	}
	releaseNamespace := make(map[string]struct{})
	for _, definition := range builtinStorageCatalog {
		releaseNamespace[definition.Item.DefaultNamespace] = struct{}{}
	}
	releasesByNamespace, releaseWarnings := s.storageHelmReleases(ctx, releaseNamespace)
	center.Warnings = append(center.Warnings, releaseWarnings...)
	catalog, catalogWarnings := s.storageCatalog(ctx, actor, classes, releasesByNamespace)
	center.Catalog = catalog
	center.Warnings = append(center.Warnings, catalogWarnings...)
	center.Integrations = buildStorageIntegrations(catalog, classes, drivers, pods, releasesByNamespace)
	for _, integration := range center.Integrations {
		if integration.Source == domain.StorageIntegrationManaged {
			center.Summary.ManagedIntegrationCount++
		} else {
			center.Summary.AdoptedIntegrationCount++
		}
	}
	center.Diagnostics = buildStorageDiagnostics(center, classes, claims)
	center.Warnings = uniqueStrings(center.Warnings)
	center.Diagnostics = uniqueDiagnostics(center.Diagnostics)
	return center, nil
}

func (s *Service) storageNodeSummary(ctx context.Context, repository CenterRepository, warnings *[]string) (int, int) {
	nodes, err := repository.ListNodes(ctx, domain.ListQuery{})
	if err != nil {
		*warnings = append(*warnings, "无法读取节点："+err.Error())
		return 0, 0
	}
	ready := 0
	for _, node := range nodes {
		if strings.EqualFold(node.Status, "Ready") {
			ready++
		}
	}
	return len(nodes), ready
}

func (s *Service) storageVolumeCount(ctx context.Context, warnings *[]string) int {
	volumes, err := s.repository.ListPersistentVolumes(ctx, domain.ListQuery{})
	if err != nil {
		*warnings = append(*warnings, "无法读取 PV："+err.Error())
		return 0
	}
	return len(volumes)
}

func (s *Service) storageHelmReleases(ctx context.Context, namespaces map[string]struct{}) (map[string][]domain.HelmReleaseSummary, []string) {
	releases := make(map[string][]domain.HelmReleaseSummary)
	reader, ok := s.repository.(HelmReleaseReader)
	if !ok {
		return releases, nil
	}
	warnings := make([]string, 0)
	for namespace := range namespaces {
		items, itemWarnings, err := reader.ListHelmReleases(ctx, namespace)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("无法读取命名空间 %s 的 Helm Release：%s", namespace, err.Error()))
			continue
		}
		releases[namespace] = items
		warnings = append(warnings, itemWarnings...)
	}
	return releases, warnings
}

func (s *Service) storageCatalog(ctx context.Context, actor domain.Actor, classes []domain.StorageClass, releases map[string][]domain.HelmReleaseSummary) ([]domain.StorageCatalogItem, []string) {
	items := make([]domain.StorageCatalogItem, 0, len(builtinStorageCatalog))
	chartByRepository := make(map[string]map[string]domain.HelmChartSummary)
	warnings := make([]string, 0)
	if s.helmCatalog == nil {
		for _, definition := range builtinStorageCatalog {
			item := definition.Item
			item.Status = domain.StorageCatalogMissingRepository
			item.StatusMessage = "尚未配置 Helm Chart 仓库，无法解析该方案。"
			items = append(items, decorateCatalogItem(item, classes, releases))
		}
		return items, []string{"存储目录需要管理员先在 Helm 仓库管理中登记可信 Chart 仓库。"}
	}
	repositories, err := s.helmCatalog.ListRepositories(ctx, actor)
	if err != nil {
		for _, definition := range builtinStorageCatalog {
			item := definition.Item
			item.Status = domain.StorageCatalogForbidden
			item.StatusMessage = "无法读取 Helm 仓库：" + err.Error()
			items = append(items, decorateCatalogItem(item, classes, releases))
		}
		if errors.Is(err, domain.ErrForbidden) {
			return items, []string{"当前身份无权读取 Helm 仓库，存储目录只展示内置方案信息。"}
		}
		return items, []string{"读取 Helm 仓库失败：" + err.Error()}
	}
	sort.SliceStable(repositories, func(i, j int) bool {
		if repositories[i].Default != repositories[j].Default {
			return repositories[i].Default
		}
		return strings.ToLower(repositories[i].Name) < strings.ToLower(repositories[j].Name)
	})
	for _, repository := range repositories {
		if !repository.Enabled {
			continue
		}
		charts, chartWarnings, chartErr := s.helmCatalog.Charts(ctx, actor, repository.ID, "")
		warnings = append(warnings, chartWarnings...)
		if chartErr != nil {
			warnings = append(warnings, fmt.Sprintf("无法读取 Helm 仓库 %s：%s", repository.Name, chartErr.Error()))
			continue
		}
		byName := make(map[string]domain.HelmChartSummary, len(charts))
		for _, chart := range charts {
			key := strings.ToLower(chart.Name)
			if current, exists := byName[key]; !exists || chart.Version > current.Version {
				byName[key] = chart
			}
		}
		chartByRepository[repository.ID] = byName
	}
	for _, definition := range builtinStorageCatalog {
		item := definition.Item
		for _, repository := range repositories {
			if !repository.Enabled {
				continue
			}
			chart, found := chartByRepository[repository.ID][strings.ToLower(definition.ChartName)]
			if !found {
				continue
			}
			item.Status = domain.StorageCatalogAvailable
			item.Chart = &domain.StorageCatalogChart{
				RepositoryID: repository.ID, RepositoryName: repository.Name,
				ChartName: chart.Name, Version: chart.Version, AppVersion: chart.AppVersion,
				Digest: chart.Digest, Deprecated: chart.Deprecated,
			}
			if chart.Deprecated {
				item.Warnings = append(item.Warnings, "该 Chart 版本已被上游标记为弃用，请评估替代方案。")
			}
			break
		}
		if item.Status != domain.StorageCatalogAvailable {
			item.Status = domain.StorageCatalogMissingChart
			item.StatusMessage = "已配置 Helm 仓库，但未找到 Chart " + definition.ChartName + "。请先同步包含该 Chart 的可信仓库。"
		}
		items = append(items, decorateCatalogItem(item, classes, releases))
	}
	return items, uniqueStrings(warnings)
}

func decorateCatalogItem(item domain.StorageCatalogItem, classes []domain.StorageClass, releases map[string][]domain.HelmReleaseSummary) domain.StorageCatalogItem {
	item.Capabilities = append([]string(nil), item.Capabilities...)
	item.Prerequisites = append([]string(nil), item.Prerequisites...)
	item.Warnings = append([]string(nil), item.Warnings...)
	item.Provisioners = append([]string(nil), item.Provisioners...)
	item.ExpectedStorageClasses = append([]string(nil), item.ExpectedStorageClasses...)
	for _, class := range classes {
		if provisionerMatches(class.Provisioner, item.Provisioners) || containsString(item.ExpectedStorageClasses, class.Name) {
			item.Installed = true
			item.IntegrationID = storageIntegrationID("provisioner", class.Provisioner)
			break
		}
	}
	for _, release := range releases[item.DefaultNamespace] {
		if strings.EqualFold(release.Chart, chartNameForCatalog(item)) || strings.EqualFold(release.Name, item.DefaultReleaseName) {
			item.Installed = true
			item.IntegrationID = storageIntegrationID("release", release.Namespace+"/"+release.Name)
			break
		}
	}
	return item
}

func chartNameForCatalog(item domain.StorageCatalogItem) string {
	for _, definition := range builtinStorageCatalog {
		if definition.Item.ID == item.ID {
			return definition.ChartName
		}
	}
	return ""
}

func buildStorageIntegrations(catalog []domain.StorageCatalogItem, classes []domain.StorageClass, drivers []domain.CSIDriverInfo, pods []domain.PodSummary, releases map[string][]domain.HelmReleaseSummary) []domain.StorageIntegration {
	byProvisioner := make(map[string][]domain.StorageClass)
	for _, class := range classes {
		byProvisioner[class.Provisioner] = append(byProvisioner[class.Provisioner], class)
	}
	managed := make(map[string]domain.HelmReleaseSummary)
	for _, releaseItems := range releases {
		for _, release := range releaseItems {
			for _, definition := range builtinStorageCatalog {
				if strings.EqualFold(release.Chart, definition.ChartName) || strings.EqualFold(release.Name, definition.Item.DefaultReleaseName) {
					managed[release.Namespace+"/"+release.Chart] = release
				}
			}
		}
	}
	result := make([]domain.StorageIntegration, 0, len(byProvisioner)+len(drivers)+len(managed))
	seen := make(map[string]struct{})
	for provisioner, provisionerClasses := range byProvisioner {
		definition, hasDefinition := catalogForProvisioner(provisioner)
		integration := domain.StorageIntegration{
			ID: storageIntegrationID("provisioner", provisioner), Name: provisioner, Kind: storageIntegrationKind(provisioner),
			Provisioner: provisioner, Source: domain.StorageIntegrationAdopted,
			StorageClasses: storageClassNames(provisionerClasses), DriverRegistered: driverRegistered(provisioner, drivers),
		}
		if hasDefinition {
			integration.Name = definition.Item.Name
			if definition.Item.Category == "csi-network-file" {
				integration.Kind = "csi"
			} else {
				integration.Kind = "provisioner"
			}
			if release, found := findManagedRelease(releases, definition); found {
				integration.Source = domain.StorageIntegrationManaged
				integration.ReleaseName = release.Name
				integration.ReleaseNamespace = release.Namespace
				seen[storageIntegrationID("release", release.Namespace+"/"+release.Name)] = struct{}{}
			}
		}
		applyIntegrationHealth(&integration, pods, provisioner)
		applyDefaultClass(&integration, provisionerClasses)
		result = append(result, integration)
		seen[integration.ID] = struct{}{}
	}
	for _, release := range managed {
		definition, found := catalogForRelease(release)
		if !found {
			continue
		}
		id := storageIntegrationID("release", release.Namespace+"/"+release.Name)
		if _, exists := seen[id]; exists {
			continue
		}
		provisioner := ""
		if len(definition.Item.Provisioners) > 0 {
			provisioner = definition.Item.Provisioners[0]
		}
		integration := domain.StorageIntegration{
			ID: id, Name: definition.Item.Name, Kind: storageIntegrationKind(provisioner), Provisioner: provisioner,
			Source: domain.StorageIntegrationManaged, ReleaseName: release.Name, ReleaseNamespace: release.Namespace,
			StorageClasses: []string{}, DriverRegistered: driverRegistered(provisioner, drivers),
			Warnings: []string{"已检测到 Helm Release，但尚未发现该方案创建的 StorageClass。"},
		}
		applyIntegrationHealth(&integration, pods, provisioner)
		result = append(result, integration)
		seen[id] = struct{}{}
	}
	for _, driver := range drivers {
		if _, exists := byProvisioner[driver.Name]; exists {
			continue
		}
		integration := domain.StorageIntegration{
			ID: storageIntegrationID("csi", driver.Name), Name: driver.Name, Kind: "csi", Provisioner: driver.Name,
			Source: domain.StorageIntegrationAdopted, DriverRegistered: true, StorageClasses: []string{},
		}
		applyIntegrationHealth(&integration, pods, driver.Name)
		result = append(result, integration)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Source != result[j].Source {
			return result[i].Source == domain.StorageIntegrationManaged
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func catalogForProvisioner(provisioner string) (storageCatalogDefinition, bool) {
	for _, definition := range builtinStorageCatalog {
		if provisionerMatches(provisioner, definition.Item.Provisioners) || provisionerMatches(provisioner, []string{definition.ChartName}) {
			return definition, true
		}
	}
	return storageCatalogDefinition{}, false
}

func catalogForRelease(release domain.HelmReleaseSummary) (storageCatalogDefinition, bool) {
	for _, definition := range builtinStorageCatalog {
		if strings.EqualFold(release.Chart, definition.ChartName) || strings.EqualFold(release.Name, definition.Item.DefaultReleaseName) {
			return definition, true
		}
	}
	return storageCatalogDefinition{}, false
}

func findManagedRelease(releases map[string][]domain.HelmReleaseSummary, definition storageCatalogDefinition) (domain.HelmReleaseSummary, bool) {
	for _, release := range releases[definition.Item.DefaultNamespace] {
		if strings.EqualFold(release.Chart, definition.ChartName) || strings.EqualFold(release.Name, definition.Item.DefaultReleaseName) {
			return release, true
		}
	}
	return domain.HelmReleaseSummary{}, false
}

func applyIntegrationHealth(integration *domain.StorageIntegration, pods []domain.PodSummary, provisioner string) {
	tokens := integrationPodTokens(provisioner)
	for _, pod := range pods {
		if !podMatchesTokens(pod, tokens) {
			continue
		}
		integration.PodCount++
		if strings.EqualFold(pod.Status, "Running") {
			integration.ReadyPodCount++
		}
	}
	switch {
	case integration.PodCount == 0:
		integration.Status = domain.StorageIntegrationUnknown
		integration.Message = "未发现可关联的驱动工作负载；可能是命名差异、命名空间权限限制或驱动未运行。"
	case integration.ReadyPodCount == integration.PodCount:
		integration.Status = domain.StorageIntegrationHealthy
		integration.Message = "驱动工作负载全部处于 Running 状态。"
	case integration.ReadyPodCount > 0:
		integration.Status = domain.StorageIntegrationDegraded
		integration.Message = fmt.Sprintf("仅 %d/%d 个驱动工作负载处于 Running 状态。", integration.ReadyPodCount, integration.PodCount)
	default:
		integration.Status = domain.StorageIntegrationDegraded
		integration.Message = "已发现驱动工作负载，但没有任何实例运行。"
	}
}

func integrationPodTokens(provisioner string) []string {
	for _, definition := range builtinStorageCatalog {
		if provisionerMatches(provisioner, definition.Item.Provisioners) || provisionerMatches(provisioner, []string{definition.ChartName}) {
			return definition.PodTokens
		}
	}
	tokens := []string{strings.ToLower(provisioner)}
	last := provisioner
	if index := strings.LastIndex(last, "/"); index >= 0 {
		last = last[index+1:]
	}
	if first := strings.Index(last, "."); first > 0 {
		last = last[:first]
	}
	if len(last) >= 4 {
		tokens = append(tokens, strings.ToLower(last))
	}
	return tokens
}

func podMatchesTokens(pod domain.PodSummary, tokens []string) bool {
	values := []string{pod.Name, pod.OwnerName}
	for key, value := range pod.Labels {
		values = append(values, key, value)
	}
	joined := strings.ToLower(strings.Join(values, " "))
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" && strings.Contains(joined, token) {
			return true
		}
	}
	return false
}

func applyDefaultClass(integration *domain.StorageIntegration, classes []domain.StorageClass) {
	for _, class := range classes {
		if class.Default {
			integration.DefaultClass = class.Name
			return
		}
	}
}

func storageClassNames(classes []domain.StorageClass) []string {
	names := make([]string, 0, len(classes))
	for _, class := range classes {
		names = append(names, class.Name)
	}
	sort.Strings(names)
	return names
}

func driverRegistered(provisioner string, drivers []domain.CSIDriverInfo) bool {
	for _, driver := range drivers {
		if driver.Name == provisioner {
			return true
		}
	}
	return false
}

func storageIntegrationKind(provisioner string) string {
	if strings.Contains(provisioner, ".") || strings.Contains(provisioner, "/") {
		return "csi"
	}
	return "provisioner"
}

func buildStorageDiagnostics(center domain.StorageCenter, classes []domain.StorageClass, claims []domain.PersistentVolumeClaim) []domain.StorageDiagnostic {
	diagnostics := make([]domain.StorageDiagnostic, 0)
	if center.Summary.ReadyNodeCount == 0 {
		diagnostics = append(diagnostics, domain.StorageDiagnostic{
			ID: "storage.no-ready-node", Severity: "critical", Title: "没有 Ready 节点",
			Message:    "当前没有可用节点，存储驱动和 PVC 均无法被正常调度。",
			Suggestion: "先修复节点状态与 kubelet 连接，再安装或验证存储方案。",
		})
	}
	if len(classes) == 0 {
		diagnostics = append(diagnostics, domain.StorageDiagnostic{
			ID: "storage.no-class", Severity: "warning", Title: "没有 StorageClass",
			Message:    "集群当前没有可用的动态存储类，新建 PVC 只能等待静态 PV 绑定。",
			Suggestion: "从存储方案目录安装驱动，或纳管已有 CSI Driver 后创建 StorageClass。",
		})
	}
	if center.Summary.DefaultStorageClassCount == 0 && len(classes) > 0 {
		diagnostics = append(diagnostics, domain.StorageDiagnostic{
			ID: "storage.no-default-class", Severity: "info", Title: "没有默认 StorageClass",
			Message:    "未指定 StorageClass 的 PVC 不会自动使用默认类。",
			Suggestion: "按业务需要为其中一个 StorageClass 设置默认类，避免隐式绑定到错误存储。",
		})
	}
	if center.Summary.DefaultStorageClassCount > 1 {
		diagnostics = append(diagnostics, domain.StorageDiagnostic{
			ID: "storage.default-class-conflict", Severity: "warning", Title: "存在多个默认 StorageClass",
			Message:    "Kubernetes 对多个默认类的选择行为不确定，可能导致 PVC 绑定到非预期存储。",
			Suggestion: "只保留一个默认 StorageClass，并显式指定需要特殊存储的 PVC。",
		})
	}
	if center.Summary.PendingClaimCount > 0 {
		samples := make([]string, 0, 3)
		for _, claim := range claims {
			if strings.EqualFold(claim.Status, "Pending") && len(samples) < 3 {
				samples = append(samples, claim.Namespace+"/"+claim.Name)
			}
		}
		diagnostics = append(diagnostics, domain.StorageDiagnostic{
			ID: "storage.pending-claim", Severity: "warning", Title: "存在 Pending PVC",
			Message:    fmt.Sprintf("%d 个 PVC 处于 Pending：%s", center.Summary.PendingClaimCount, strings.Join(samples, "、")),
			Suggestion: "检查 StorageClass、Provisioner、节点拓扑、访问模式和绑定事件；必要时运行存储验证。",
		})
	}
	for _, integration := range center.Integrations {
		if integration.Status == domain.StorageIntegrationDegraded {
			diagnostics = append(diagnostics, domain.StorageDiagnostic{
				ID: "storage.integration-degraded-" + integration.ID, Severity: "warning", Title: integration.Name + " 运行异常",
				Message: integration.Message, Resource: integration.Provisioner,
				Suggestion: "检查驱动 Controller/Node Plugin Pod、镜像拉取、节点挂载能力和相关事件。",
			})
		}
	}
	return diagnostics
}

func countDefaultStorageClasses(classes []domain.StorageClass) int {
	count := 0
	for _, class := range classes {
		if class.Default {
			count++
		}
	}
	return count
}

func countPendingClaims(claims []domain.PersistentVolumeClaim) int {
	count := 0
	for _, claim := range claims {
		if strings.EqualFold(claim.Status, "Pending") {
			count++
		}
	}
	return count
}

func storageIntegrationID(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return "storage-" + hex.EncodeToString(sum[:6])
}

func provisionerMatches(provisioner string, expected []string) bool {
	provisioner = strings.ToLower(strings.TrimSpace(provisioner))
	if provisioner == "" {
		return false
	}
	for _, candidate := range expected {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if provisioner == candidate || strings.Contains(provisioner, candidate) || strings.Contains(candidate, provisioner) {
			return true
		}
	}
	return false
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueDiagnostics(values []domain.StorageDiagnostic) []domain.StorageDiagnostic {
	seen := make(map[string]struct{}, len(values))
	out := make([]domain.StorageDiagnostic, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value.ID]; exists {
			continue
		}
		seen[value.ID] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *Service) StoragePreflight(ctx context.Context, actor domain.Actor, request domain.StoragePreflightRequest) (domain.StoragePreflight, error) {
	if strings.TrimSpace(request.CatalogID) == "" {
		return domain.StoragePreflight{}, storageValidationError("catalogId", "catalogId is required")
	}
	definition, found := findStorageCatalogDefinition(request.CatalogID)
	if !found {
		return domain.StoragePreflight{}, fmt.Errorf("%w: storage catalog item %s", domain.ErrNotFound, request.CatalogID)
	}
	namespace := strings.TrimSpace(request.Namespace)
	if namespace == "" {
		namespace = definition.Item.DefaultNamespace
	}
	releaseName := strings.TrimSpace(request.ReleaseName)
	if releaseName == "" {
		releaseName = definition.Item.DefaultReleaseName
	}
	result := domain.StoragePreflight{CatalogID: request.CatalogID, Status: domain.StorageCheckPass, Checks: []domain.StorageCheck{}, Warnings: []string{}}
	classes, err := s.repository.ListStorageClasses(ctx, domain.ListQuery{})
	if err != nil {
		return result, err
	}
	releases, releaseWarnings := s.storageHelmReleases(ctx, map[string]struct{}{namespace: {}})
	result.Warnings = append(result.Warnings, releaseWarnings...)
	catalog, catalogWarnings := s.storageCatalog(ctx, actor, classes, releases)
	result.Warnings = append(result.Warnings, catalogWarnings...)
	for _, item := range catalog {
		if item.ID == request.CatalogID {
			result.Catalog = &item
			break
		}
	}
	if result.Catalog == nil || result.Catalog.Chart == nil || result.Catalog.Status != domain.StorageCatalogAvailable {
		message := "未找到可安装的固定版本 Chart。"
		if result.Catalog != nil && result.Catalog.StatusMessage != "" {
			message = result.Catalog.StatusMessage
		}
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "chart", Title: "Chart 来源", Status: domain.StorageCheckFail, Message: message, Suggestion: "在 Helm 仓库管理中登记可信仓库并同步包含该 Chart 的索引。"})
	} else {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "chart", Title: "Chart 来源", Status: domain.StorageCheckPass, Message: fmt.Sprintf("%s / %s v%s 已固定，包摘要将在安装计划中再次校验。", result.Catalog.Chart.RepositoryName, result.Catalog.Chart.ChartName, result.Catalog.Chart.Version)})
	}
	if err := validateStorageName(namespace, "namespace"); err != nil {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "namespace", Title: "目标命名空间", Status: domain.StorageCheckFail, Message: err.Error()})
	} else {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "namespace", Title: "目标命名空间", Status: domain.StorageCheckPass, Message: "安装目标为 " + namespace})
	}
	if centerRepo, ok := s.repository.(CenterRepository); ok {
		namespaces, namespaceErr := centerRepo.ListNamespaces(ctx, domain.ListQuery{})
		if namespaceErr != nil {
			result.Warnings = append(result.Warnings, "无法读取命名空间列表："+namespaceErr.Error())
		} else {
			foundNamespace := false
			for _, item := range namespaces {
				if item.Name == namespace {
					foundNamespace = true
					break
				}
			}
			if foundNamespace {
				result.Checks = append(result.Checks, domain.StorageCheck{ID: "namespace-exists", Title: "命名空间状态", Status: domain.StorageCheckPass, Message: "命名空间 " + namespace + " 已存在。"})
			} else {
				result.Checks = append(result.Checks, domain.StorageCheck{ID: "namespace-exists", Title: "命名空间状态", Status: domain.StorageCheckWarning, Message: "命名空间 " + namespace + " 不存在。安装时必须启用“自动创建命名空间”，或先创建命名空间。", Suggestion: "在命名空间页面创建目标命名空间后再安装。"})
			}
		}
	}
	if len(releaseName) > 53 || !storageReleaseNamePattern.MatchString(releaseName) {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "release", Title: "Release 名称", Status: domain.StorageCheckFail, Message: "Release 名称必须是长度不超过 53 的 DNS-1123 小写标识。"})
	} else {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "release", Title: "Release 名称", Status: domain.StorageCheckPass, Message: namespace + "/" + releaseName})
	}
	if existing, exists := releaseInNamespace(releases[namespace], releaseName); exists {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "release-conflict", Title: "Release 冲突", Status: domain.StorageCheckFail, Message: fmt.Sprintf("Release %s 已存在（revision %d，状态 %s）。安装会被 Helm 拒绝。", existing.Name, existing.Revision, existing.Status), Suggestion: "请在 Helm 安装向导中改为升级已有 Release。"})
	}
	for _, class := range classes {
		if containsString(definition.Item.ExpectedStorageClasses, class.Name) {
			status := domain.StorageCheckWarning
			message := fmt.Sprintf("StorageClass %s 已存在，Provisioner=%s。", class.Name, class.Provisioner)
			suggestion := "验证现有 StorageClass 是否可用；无需重复安装。"
			if provisionerMatches(class.Provisioner, definition.Item.Provisioners) {
				status = domain.StorageCheckPass
			}
			result.Checks = append(result.Checks, domain.StorageCheck{ID: "expected-class-" + class.Name, Title: "StorageClass 预检", Status: status, Message: message, Suggestion: suggestion})
		}
	}
	if centerRepo, ok := s.repository.(CenterRepository); ok {
		nodes, nodeErr := centerRepo.ListNodes(ctx, domain.ListQuery{})
		if nodeErr != nil {
			result.Checks = append(result.Checks, domain.StorageCheck{ID: "nodes", Title: "节点可用性", Status: domain.StorageCheckWarning, Message: "无法读取节点：" + nodeErr.Error()})
		} else {
			ready := 0
			for _, node := range nodes {
				if strings.EqualFold(node.Status, "Ready") {
					ready++
				}
			}
			status := domain.StorageCheckPass
			message := fmt.Sprintf("%d/%d 个节点 Ready。", ready, len(nodes))
			if ready == 0 {
				status = domain.StorageCheckFail
				message = "没有 Ready 节点，无法调度 CSI Controller/Node Plugin。"
			}
			result.Checks = append(result.Checks, domain.StorageCheck{ID: "nodes", Title: "节点可用性", Status: status, Message: message})
			if definition.Item.ID == "local-path-provisioner" && ready > 1 {
				result.Checks = append(result.Checks, domain.StorageCheck{ID: "local-path-ha", Title: "Local Path 限制", Status: domain.StorageCheckWarning, Message: "当前有多个 Ready 节点。Local Path 数据与节点绑定，Pod 迁移后无法自动跨节点访问原数据。", Suggestion: "生产有状态业务优先选择网络存储或具备副本能力的 CSI 驱动。"})
			}
		}
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "create", Resource: "secrets", Namespace: namespace}); err != nil {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "helm-rbac", Title: "Helm 存储权限", Status: domain.StorageCheckFail, Message: "当前身份无法在目标命名空间创建 Helm Release Secret：" + err.Error(), Suggestion: "授予目标命名空间 Secret 的 create/update 权限后重试。"})
	} else {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "helm-rbac", Title: "Helm 存储权限", Status: domain.StorageCheckPass, Message: "已具备目标命名空间 Helm Release Secret 写入权限。"})
	}
	if s.repository != nil {
		if classes, err := s.repository.ListStorageClasses(ctx, domain.ListQuery{}); err == nil && countDefaultStorageClasses(classes) > 1 {
			result.Checks = append(result.Checks, domain.StorageCheck{ID: "default-class", Title: "默认 StorageClass", Status: domain.StorageCheckWarning, Message: "存在多个默认 StorageClass，动态供应结果可能不确定。"})
		}
	}
	result.Status = aggregateStorageChecks(result.Checks)
	result.Warnings = uniqueStrings(result.Warnings)
	return result, nil
}

func (s *Service) VerifyStorage(ctx context.Context, actor domain.Actor, request domain.StorageVerifyRequest) (domain.StorageVerification, error) {
	namespace := strings.TrimSpace(request.Namespace)
	className := strings.TrimSpace(request.StorageClass)
	if err := validateStorageName(namespace, "namespace"); err != nil {
		return domain.StorageVerification{}, err
	}
	if err := validateStorageName(className, "storageClass"); err != nil {
		return domain.StorageVerification{}, err
	}
	result := domain.StorageVerification{Namespace: namespace, StorageClass: className, Status: domain.StorageCheckPass, Checks: []domain.StorageCheck{}, Warnings: []string{}}
	class, classErr := s.repository.GetStorageClass(ctx, className)
	if classErr != nil {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "storage-class", Title: "StorageClass", Status: domain.StorageCheckFail, Message: "无法读取 StorageClass：" + classErr.Error(), Suggestion: "确认名称拼写和 StorageClass 读取权限。"})
		result.Status = domain.StorageCheckFail
		_ = s.auditStorageResult(ctx, actor, "verify-storage", "storageclass", namespace, className, classErr)
		return result, nil
	}
	result.Checks = append(result.Checks, domain.StorageCheck{ID: "storage-class", Title: "StorageClass", Status: domain.StorageCheckPass, Message: fmt.Sprintf("%s 使用 Provisioner %s，绑定模式 %s。", class.Name, class.Provisioner, class.VolumeBinding)})
	var definition *storageCatalogDefinition
	if request.CatalogID != "" {
		if found, ok := findStorageCatalogDefinition(request.CatalogID); ok {
			definition = &found
		}
	}
	if definition == nil {
		if found, ok := catalogForProvisioner(class.Provisioner); ok {
			definition = &found
		}
	}
	centerRepo, hasCenterRepo := s.repository.(CenterRepository)
	if hasCenterRepo {
		if drivers, err := centerRepo.ListCSIDrivers(ctx); err == nil {
			if driverRegistered(class.Provisioner, drivers) {
				result.Checks = append(result.Checks, domain.StorageCheck{ID: "csi-registration", Title: "CSI 注册", Status: domain.StorageCheckPass, Message: "CSIDriver " + class.Provisioner + " 已注册。"})
			} else if definition == nil || definition.Item.Category == "csi-network-file" {
				result.Checks = append(result.Checks, domain.StorageCheck{ID: "csi-registration", Title: "CSI 注册", Status: domain.StorageCheckWarning, Message: "未发现 CSIDriver " + class.Provisioner + "，可能尚未安装、权限不足或该 Provisioner 不是 CSI。", Suggestion: "确认驱动安装和 CSIDriver 注册状态。"})
			}
		} else {
			result.Warnings = append(result.Warnings, "无法读取 CSIDriver："+err.Error())
		}
		if pods, err := centerRepo.ListPods(ctx, domain.ListQuery{}); err == nil {
			tokens := integrationPodTokens(class.Provisioner)
			total, ready := 0, 0
			for _, pod := range pods {
				if podMatchesTokens(pod, tokens) {
					total++
					if strings.EqualFold(pod.Status, "Running") {
						ready++
					}
				}
			}
			status := domain.StorageCheckPass
			message := fmt.Sprintf("发现 %d/%d 个驱动工作负载 Running。", ready, total)
			if total == 0 {
				status = domain.StorageCheckWarning
				message = "未发现可关联的驱动工作负载；PVC 绑定结果仍是最终判据。"
			} else if ready < total {
				status = domain.StorageCheckWarning
			}
			result.Checks = append(result.Checks, domain.StorageCheck{ID: "driver-pods", Title: "驱动工作负载", Status: status, Message: message})
		} else {
			result.Warnings = append(result.Warnings, "无法读取驱动工作负载："+err.Error())
		}
		if classes, err := s.repository.ListStorageClasses(ctx, domain.ListQuery{}); err == nil && countDefaultStorageClasses(classes) > 1 {
			result.Checks = append(result.Checks, domain.StorageCheck{ID: "default-class", Title: "默认 StorageClass", Status: domain.StorageCheckWarning, Message: "存在多个默认 StorageClass，请确认 PVC 的显式选择。"})
		}
	}
	if definition != nil {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "profile", Title: "方案识别", Status: domain.StorageCheckPass, Message: "已匹配内置方案 " + definition.Item.Name + "。"})
	} else {
		result.Checks = append(result.Checks, domain.StorageCheck{ID: "profile", Title: "方案识别", Status: domain.StorageCheckInfo, Message: "该 StorageClass 未被内置目录标记，按纳管资源进行通用验证。"})
	}
	if request.SmokeTest {
		smoke, check := s.verifyStorageSmokeTest(ctx, actor, namespace, request)
		result.SmokeTest = &smoke
		result.Checks = append(result.Checks, check)
	}
	result.Status = aggregateStorageChecks(result.Checks)
	result.Warnings = uniqueStrings(result.Warnings)
	auditErr := error(nil)
	if result.Status == domain.StorageCheckFail {
		auditErr = errors.New("storage verification failed")
	}
	s.auditStorage(ctx, actor, "verify-storage", "storageclass", namespace, className, auditErr)
	return result, nil
}

func (s *Service) verifyStorageSmokeTest(ctx context.Context, actor domain.Actor, namespace string, request domain.StorageVerifyRequest) (domain.StorageSmokeTestResult, domain.StorageCheck) {
	image := strings.TrimSpace(request.SmokeTestImage)
	if image == "" {
		image = defaultStorageSmokeImage
	}
	if !storageSmokeImagePattern.MatchString(image) {
		return domain.StorageSmokeTestResult{Namespace: namespace, StorageClass: request.StorageClass}, domain.StorageCheck{ID: "smoke-test", Title: "Smoke Test", Status: domain.StorageCheckFail, Message: "探测镜像名称不合法。"}
	}
	for _, access := range []domain.Access{
		{Verb: "create", Resource: "pods", Namespace: namespace},
		{Verb: "delete", Resource: "pods", Namespace: namespace},
		{Verb: "create", Resource: "persistentvolumeclaims", Namespace: namespace},
		{Verb: "delete", Resource: "persistentvolumeclaims", Namespace: namespace},
	} {
		if err := s.authorizer.Require(ctx, actor, access); err != nil {
			return domain.StorageSmokeTestResult{Namespace: namespace, StorageClass: request.StorageClass}, domain.StorageCheck{ID: "smoke-test", Title: "Smoke Test", Status: domain.StorageCheckFail, Message: "缺少 Smoke Test 所需权限：" + err.Error(), Suggestion: "允许该身份在目标命名空间创建/删除 Pod 与 PVC，或关闭 Smoke Test 仅做只读验证。"}
		}
	}
	tester, ok := s.repository.(StorageSmokeTester)
	if !ok {
		return domain.StorageSmokeTestResult{Namespace: namespace, StorageClass: request.StorageClass}, domain.StorageCheck{ID: "smoke-test", Title: "Smoke Test", Status: domain.StorageCheckFail, Message: "当前存储适配器不支持运行 Smoke Test。"}
	}
	result, err := tester.RunStorageSmokeTest(ctx, domain.StorageSmokeTestSpec{Namespace: namespace, StorageClass: request.StorageClass, Image: image, TimeoutSeconds: 90})
	if err != nil {
		return result, domain.StorageCheck{ID: "smoke-test", Title: "Smoke Test", Status: domain.StorageCheckFail, Message: err.Error(), Suggestion: "检查 StorageClass、驱动工作负载、镜像拉取和命名空间事件。"}
	}
	status := domain.StorageCheckPass
	if !result.Bound || !result.PodRunning {
		status = domain.StorageCheckFail
	}
	if !result.CleanedUp {
		status = domain.StorageCheckWarning
		result.Message = strings.TrimSpace(result.Message + " 临时资源未能完全清理，请手动检查。")
	}
	return result, domain.StorageCheck{ID: "smoke-test", Title: "Smoke Test", Status: status, Message: result.Message, Suggestion: "如失败，请检查 PVC/Pod 事件、存储后端连通性、镜像拉取和拓扑约束。"}
}

func (s *Service) auditStorageResult(ctx context.Context, actor domain.Actor, action, resource, namespace, name string, err error) error {
	s.auditStorage(ctx, actor, action, resource, namespace, name, err)
	return err
}

func releaseInNamespace(releases []domain.HelmReleaseSummary, name string) (domain.HelmReleaseSummary, bool) {
	for _, release := range releases {
		if release.Name == name {
			return release, true
		}
	}
	return domain.HelmReleaseSummary{}, false
}

func aggregateStorageChecks(checks []domain.StorageCheck) string {
	status := domain.StorageCheckPass
	for _, check := range checks {
		switch check.Status {
		case domain.StorageCheckFail:
			return domain.StorageCheckFail
		case domain.StorageCheckWarning:
			if status != domain.StorageCheckFail {
				status = domain.StorageCheckWarning
			}
		}
	}
	return status
}
