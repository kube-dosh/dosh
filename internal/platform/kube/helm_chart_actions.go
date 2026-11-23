package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

const helmActionTimeout = 10 * time.Minute

const (
	helmManagedByLabel             = "app.kubernetes.io/managed-by"
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"
)

func helmExecutionTimeout(options domain.HelmExecutionOptions) time.Duration {
	if options.TimeoutSeconds > 0 {
		return time.Duration(options.TimeoutSeconds) * time.Second
	}
	return helmActionTimeout
}

func helmMaxHistory(options domain.HelmExecutionOptions) int {
	if options.MaxHistory > 0 {
		return options.MaxHistory
	}
	return domain.DefaultHelmMaxHistory
}

// PlanHelmChart renders a downloaded chart and resolves every object to a
// Kubernetes resource. It does not install anything. The caller performs RBAC
// checks against the returned resources before allowing an operation.
func (c *Client) PlanHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error) {
	executionOptions, err := domain.ResolveHelmExecutionOptions(request.Options)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	namespaceExists, err := c.validateHelmTargetNamespace(ctx, request.Namespace, request.ReleaseName, mode == "install" && executionOptions.CreateNamespace)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	ch, err := loadHelmChartArchive(archive, request.ChartName, request.Version)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	configuration, err := c.helmConfiguration(actor, request.Namespace)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	currentRevision := 0
	var latest *release.Release
	if namespaceExists {
		currentRevision, latest, err = helmReleaseState(configuration, request.ReleaseName)
		if err != nil {
			return domain.HelmChartPlanTemplate{}, err
		}
	}
	switch mode {
	case "install":
		if latest != nil {
			return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: Helm release %s/%s already exists", domain.ErrAlreadyExists, request.Namespace, request.ReleaseName)
		}
	case "upgrade":
		if latest == nil {
			return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: Helm release %s/%s", domain.ErrNotFound, request.Namespace, request.ReleaseName)
		}
		if latest.Info != nil && !helmReleaseCanUpgrade(latest.Info.Status) {
			return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: only a deployed, failed or superseded Helm release can be upgraded (current status %s)", domain.ErrInvalid, latest.Info.Status)
		}
	default:
		return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: plan mode must be install or upgrade", domain.ErrInvalid)
	}

	values, err := parseHelmValues(request.Values)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	if mode == "upgrade" {
		values = mergeHelmUpgradeValues(latest, values)
	}
	currentManifest := ""
	if mode == "upgrade" && latest != nil {
		currentManifest = latest.Manifest
	}
	return c.helmPlanTemplateForChart(ctx, actor, request.Namespace, request.ReleaseName, currentRevision, currentManifest, ch, values, namespaceExists, nil)
}

func (c *Client) helmPlanTemplateForChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, currentRevision int, currentManifest string, ch *chart.Chart, values map[string]any, allowServerDryRun bool, warnings []string) (domain.HelmChartPlanTemplate, error) {
	if ch == nil || ch.Metadata == nil {
		return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: Helm Chart metadata is unavailable", domain.ErrInvalid)
	}
	mergedValues, manifest, hooks, generic, renderWarnings, err := c.renderHelmChart(ctx, actor, ch, namespace, releaseName, currentRevision+1, values)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	descriptors, discoveryWarnings, err := c.DiscoverResources(ctx)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	warnings = append(append([]string(nil), warnings...), renderWarnings...)
	warnings = append(warnings, discoveryWarnings...)
	byGVK := helmDescriptorsByGVK(descriptors)
	servedByGVK := make(map[string]domain.ResourceDescriptor, len(byGVK))
	for key, descriptor := range byGVK {
		servedByGVK[key] = descriptor
	}
	chartCRDs := chartCRDDescriptors(ch)
	for key, descriptor := range chartCRDs {
		if _, exists := byGVK[key]; !exists {
			byGVK[key] = descriptor
		}
	}
	resources := make([]domain.HelmChartPlanResource, 0, len(generic)+len(hooks)+len(ch.CRDObjects()))
	for _, item := range generic {
		resource, err := c.helmPlanResource(ctx, namespace, releaseName, item, false, byGVK, chartCRDs)
		if err != nil {
			return domain.HelmChartPlanTemplate{}, err
		}
		resources = append(resources, resource)
	}
	for _, hook := range hooks {
		resource, err := c.helmPlanResource(ctx, namespace, releaseName, releaseutil.Manifest{Name: hook.Path, Content: hook.Manifest}, true, byGVK, chartCRDs)
		if err != nil {
			return domain.HelmChartPlanTemplate{}, err
		}
		resources = append(resources, resource)
	}
	resources = uniqueHelmPlanResources(resources)
	if strings.TrimSpace(currentManifest) != "" {
		removed, removedWarnings := c.helmPlanRemovedResources(currentManifest, namespace, resources, byGVK)
		resources = uniqueHelmPlanResources(append(resources, removed...))
		warnings = append(warnings, removedWarnings...)
	}
	if allowServerDryRun {
		preflightWarnings, err := c.preflightHelmPlanObjects(ctx, namespace, generic, hooks, servedByGVK, byGVK, chartCRDs)
		if err != nil {
			return domain.HelmChartPlanTemplate{}, err
		}
		warnings = append(warnings, preflightWarnings...)
	} else {
		warnings = append(warnings, "目标命名空间尚不存在，计划阶段无法执行 server-side dry-run；Helm 创建命名空间后仍会在执行时接受准入校验")
	}
	encodedValues, err := yaml.Marshal(mergedValues)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: encode Helm values: %v", domain.ErrInvalid, err)
	}
	if len(mergedValues) == 0 {
		encodedValues = []byte("{}\n")
	}
	return domain.HelmChartPlanTemplate{
		ChartName:       ch.Metadata.Name,
		ChartVersion:    ch.Metadata.Version,
		Manifest:        manifest,
		Values:          string(encodedValues),
		Resources:       resources,
		CurrentRevision: currentRevision,
		Warnings:        warnings,
	}, nil
}

func (c *Client) InstallHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest, options domain.HelmExecutionOptions) (int, error) {
	namespaceExists, err := c.validateHelmTargetNamespace(ctx, request.Namespace, request.ReleaseName, options.CreateNamespace)
	if err != nil {
		return 0, err
	}
	ch, err := loadHelmChartArchive(archive, request.ChartName, request.Version)
	if err != nil {
		return 0, err
	}
	values, err := parseHelmValues(request.Values)
	if err != nil {
		return 0, err
	}
	configuration, err := c.helmConfiguration(actor, request.Namespace)
	if err != nil {
		return 0, err
	}
	if namespaceExists {
		if currentRevision, latest, stateErr := helmReleaseState(configuration, request.ReleaseName); stateErr != nil {
			return 0, stateErr
		} else if latest != nil {
			return 0, fmt.Errorf("%w: Helm release %s/%s already exists at revision %d", domain.ErrAlreadyExists, request.Namespace, request.ReleaseName, currentRevision)
		}
	}
	install := action.NewInstall(configuration)
	install.ReleaseName = request.ReleaseName
	install.Namespace = request.Namespace
	install.Wait = options.Wait
	install.WaitForJobs = options.WaitForJobs
	install.Atomic = options.Atomic
	install.DisableHooks = options.DisableHooks
	install.SkipCRDs = options.SkipCRDs
	install.Timeout = helmExecutionTimeout(options)
	install.CreateNamespace = options.CreateNamespace
	install.Description = "Installed by Dosh from an approved Helm repository"
	configuration.Releases.MaxHistory = helmMaxHistory(options)
	result, err := install.RunWithContext(ctx, ch, values)
	if err != nil {
		return 0, mapHelmActionError(err)
	}
	if result == nil {
		return 0, fmt.Errorf("%w: Helm install returned no release", domain.ErrUnavailable)
	}
	return result.Version, nil
}

func (c *Client) UpgradeHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest, options domain.HelmExecutionOptions) (int, error) {
	if err := c.validateHelmTarget(ctx, request.Namespace, request.ReleaseName); err != nil {
		return 0, err
	}
	ch, err := loadHelmChartArchive(archive, request.ChartName, request.Version)
	if err != nil {
		return 0, err
	}
	values, err := parseHelmValues(request.Values)
	if err != nil {
		return 0, err
	}
	configuration, err := c.helmConfiguration(actor, request.Namespace)
	if err != nil {
		return 0, err
	}
	currentRevision, latest, err := helmReleaseState(configuration, request.ReleaseName)
	if err != nil {
		return 0, err
	}
	if latest == nil {
		return 0, fmt.Errorf("%w: Helm release %s/%s", domain.ErrNotFound, request.Namespace, request.ReleaseName)
	}
	if err := validateHelmExpectedRevision(currentRevision, request.ExpectedRevision); err != nil {
		return 0, err
	}
	values = mergeHelmUpgradeValues(latest, values)
	upgrade := action.NewUpgrade(configuration)
	upgrade.Namespace = request.Namespace
	upgrade.Wait = options.Wait
	upgrade.WaitForJobs = options.WaitForJobs
	upgrade.Atomic = options.Atomic
	upgrade.CleanupOnFail = options.CleanupOnFail
	upgrade.DisableHooks = options.DisableHooks
	upgrade.SkipCRDs = options.SkipCRDs
	upgrade.MaxHistory = helmMaxHistory(options)
	upgrade.Timeout = helmExecutionTimeout(options)
	upgrade.Description = "Upgraded by Dosh from an approved Helm repository"
	result, err := upgrade.RunWithContext(ctx, request.ReleaseName, ch, values)
	if err != nil {
		return 0, mapHelmActionError(err)
	}
	if result == nil {
		return 0, fmt.Errorf("%w: Helm upgrade returned no release", domain.ErrUnavailable)
	}
	return result.Version, nil
}

func validateHelmExpectedRevision(currentRevision, expectedRevision int) error {
	if expectedRevision <= 0 || currentRevision == expectedRevision {
		return nil
	}
	return fmt.Errorf("%w: Helm release revision changed from %d to %d; refresh the reviewed plan before retrying", domain.ErrConflict, expectedRevision, currentRevision)
}

func (c *Client) UninstallHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, options domain.HelmExecutionOptions) error {
	if err := c.validateHelmTarget(ctx, namespace, releaseName); err != nil {
		return err
	}
	configuration, err := c.helmConfiguration(actor, namespace)
	if err != nil {
		return err
	}
	uninstall := action.NewUninstall(configuration)
	uninstall.Wait = options.Wait
	uninstall.DisableHooks = options.DisableHooks
	uninstall.Timeout = helmExecutionTimeout(options)
	uninstall.KeepHistory = options.KeepHistory
	uninstall.Description = "Uninstalled by Dosh"
	_, err = uninstall.Run(releaseName)
	if err != nil {
		return mapHelmActionError(err)
	}
	return nil
}

func (c *Client) RollbackHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, targetRevision int, options domain.HelmExecutionOptions) (int, error) {
	if targetRevision <= 0 {
		return 0, fmt.Errorf("%w: target revision must be positive", domain.ErrInvalid)
	}
	if err := c.validateHelmTarget(ctx, namespace, releaseName); err != nil {
		return 0, err
	}
	configuration, err := c.helmConfiguration(actor, namespace)
	if err != nil {
		return 0, err
	}
	rollback := action.NewRollback(configuration)
	rollback.Version = targetRevision
	rollback.Wait = options.Wait
	rollback.WaitForJobs = options.WaitForJobs
	rollback.DisableHooks = options.DisableHooks
	rollback.CleanupOnFail = options.CleanupOnFail
	rollback.Timeout = helmExecutionTimeout(options)
	rollback.MaxHistory = helmMaxHistory(options)
	if err := rollback.Run(releaseName); err != nil {
		return 0, mapHelmActionError(err)
	}
	revision, latest, err := helmReleaseState(configuration, releaseName)
	if err != nil {
		return 0, err
	}
	if latest == nil {
		return 0, fmt.Errorf("%w: Helm release %s/%s after rollback", domain.ErrUnavailable, namespace, releaseName)
	}
	return revision, nil
}

func (c *Client) validateHelmTarget(ctx context.Context, namespace, releaseName string) error {
	_, err := c.validateHelmTargetNamespace(ctx, namespace, releaseName, false)
	return err
}

func (c *Client) validateHelmTargetNamespace(ctx context.Context, namespace, releaseName string, allowMissingNamespace bool) (bool, error) {
	namespace = strings.TrimSpace(namespace)
	releaseName = strings.TrimSpace(releaseName)
	if namespace == "" || releaseName == "" {
		return false, fmt.Errorf("%w: namespace and release name are required", domain.ErrInvalid)
	}
	if c.ControlPlaneNamespace() != "" && namespace == c.ControlPlaneNamespace() {
		return false, fmt.Errorf("%w: Helm releases cannot be installed in the Dosh control-plane namespace", domain.ErrForbidden)
	}
	if err := chartutil.ValidateReleaseName(releaseName); err != nil {
		return false, fmt.Errorf("%w: release name is invalid", domain.ErrInvalid)
	}
	if err := chartutil.ValidateMetadataName(namespace); err != nil {
		return false, fmt.Errorf("%w: namespace is invalid", domain.ErrInvalid)
	}
	if c.clientset == nil {
		return false, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if _, err := c.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) && allowMissingNamespace {
			return false, nil
		}
		return false, mapKubeError(err)
	}
	return true, nil
}

func helmReleaseCanUpgrade(status release.Status) bool {
	return status == release.StatusDeployed || status == release.StatusFailed || status == release.StatusSuperseded
}

func (c *Client) renderHelmChart(ctx context.Context, actor domain.Actor, ch *chart.Chart, namespace, releaseName string, revision int, values map[string]any) (map[string]any, string, []*release.Hook, []releaseutil.Manifest, []string, error) {
	if err := chartutil.ProcessDependenciesWithMerge(ch, values); err != nil {
		return nil, "", nil, nil, nil, fmt.Errorf("%w: process Helm chart dependencies: %v", domain.ErrInvalid, err)
	}
	caps, warnings := c.helmRenderCapabilities(ctx)
	renderValues, err := chartutil.ToRenderValues(ch, values, chartutil.ReleaseOptions{
		Name: releaseName, Namespace: namespace, Revision: revision, IsInstall: revision <= 1, IsUpgrade: revision > 1,
	}, &caps)
	if err != nil {
		return nil, "", nil, nil, nil, fmt.Errorf("%w: prepare Helm render values: %v", domain.ErrInvalid, err)
	}
	merged, _ := renderValues["Values"].(map[string]any)
	if merged == nil {
		merged = values
	}
	files, err := c.renderHelmFiles(ch, renderValues, actor)
	if err != nil {
		return nil, "", nil, nil, nil, fmt.Errorf("%w: render Helm chart: %v", domain.ErrInvalid, err)
	}
	removeHelmNotes(files)
	for _, crd := range ch.CRDObjects() {
		files[crd.Filename] = string(crd.File.Data)
	}
	hooks, generic, err := releaseutil.SortManifests(files, caps.APIVersions, releaseutil.InstallOrder)
	if err != nil {
		return nil, "", nil, nil, nil, fmt.Errorf("%w: parse rendered Helm chart: %v", domain.ErrInvalid, err)
	}
	manifest := helmPlanManifest(generic, hooks)
	return merged, manifest, hooks, generic, warnings, nil
}

// helmPlanRemovedResources mirrors Helm's upgrade pruning so the reviewed plan
// includes resources that exist in the current release but not in the target
// manifest. Helm matches old and new objects by group, kind, namespace and
// name, so the preview uses the same identity instead of the full apiVersion.
func (c *Client) helmPlanRemovedResources(currentManifest, defaultNamespace string, target []domain.HelmChartPlanResource, byGVK map[string]domain.ResourceDescriptor) ([]domain.HelmChartPlanResource, []string) {
	targetKeys := make(map[string]bool, len(target))
	for _, resource := range target {
		targetKeys[helmPlanResourceIdentity(resource.APIGroup, resource.Kind, resource.Namespace, resource.Name)] = true
	}
	warnings := make([]string, 0)
	removed := make([]domain.HelmChartPlanResource, 0)
	seen := make(map[string]bool)
	documents := releaseutil.SplitManifests(currentManifest)
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		object := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(documents[name]), object); err != nil || object.GetKind() == "" || object.GetName() == "" {
			warnings = append(warnings, fmt.Sprintf("无法解析当前 Release 清单中的资源 %s，未纳入删除预览", name))
			continue
		}
		annotations := object.GetAnnotations()
		if strings.TrimSpace(annotations["helm.sh/hook"]) != "" {
			continue
		}
		descriptor, ok := byGVK[object.GetAPIVersion()+"/"+object.GetKind()]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("当前 Release 中的 %s %s 已不在集群 API 发现结果中，未纳入删除预览", object.GetAPIVersion(), object.GetName()))
			continue
		}
		namespace := object.GetNamespace()
		if descriptor.Namespaced && namespace == "" {
			namespace = defaultNamespace
		}
		if !descriptor.Namespaced {
			namespace = ""
		}
		identity := helmPlanResourceIdentity(descriptor.Group, descriptor.Kind, namespace, object.GetName())
		if targetKeys[identity] || seen[identity] {
			continue
		}
		if annotations["helm.sh/resource-policy"] == "keep" {
			warnings = append(warnings, fmt.Sprintf("资源 %s %s/%s 标记了 helm.sh/resource-policy=keep，升级不会删除", descriptor.Kind, namespace, object.GetName()))
			continue
		}
		seen[identity] = true
		removed = append(removed, domain.HelmChartPlanResource{
			APIVersion: object.GetAPIVersion(), Kind: descriptor.Kind, Namespace: namespace, Name: object.GetName(),
			Action: "delete", APIGroup: descriptor.Group, Resource: descriptor.Resource,
			Exists: true, CRD: strings.EqualFold(descriptor.Kind, "CustomResourceDefinition"),
		})
	}
	if len(removed) > 0 {
		warnings = append(warnings, fmt.Sprintf("本次升级将删除 %d 个目标清单中已不存在的资源", len(removed)))
	}
	return removed, warnings
}

func helmPlanResourceIdentity(group, kind, namespace, name string) string {
	return strings.Join([]string{group, kind, namespace, name}, "\x00")
}

// helmRenderCapabilities mirrors the capabilities Helm's native install and
// upgrade actions build from cluster discovery. Using only Helm's default
// VersionSet during planning makes charts that branch on
// .Capabilities.APIVersions render differently from the actual operation.
func (c *Client) helmRenderCapabilities(ctx context.Context) (chartutil.Capabilities, []string) {
	caps := *chartutil.DefaultCapabilities.Copy()
	warnings := make([]string, 0, 2)
	if c.discovery == nil {
		warnings = append(warnings, "无法连接 Kubernetes Discovery，模板 Capabilities.APIVersions 使用 Helm 默认值")
	} else if versions, err := action.GetVersionSet(c.discovery); err == nil && len(versions) > 0 {
		caps.APIVersions = versions
	} else {
		warnings = append(warnings, "无法读取 Kubernetes API 版本集合，模板 Capabilities.APIVersions 使用 Helm 默认值")
	}
	if info, err := c.serverVersion(ctx); err == nil && info != nil {
		caps.KubeVersion = chartutil.KubeVersion{Version: info.GitVersion, Major: info.Major, Minor: info.Minor}
	} else {
		warnings = append(warnings, "无法读取 Kubernetes 版本，模板 Capabilities 使用 Helm 默认值")
	}
	return caps, warnings
}

// preflightHelmPlanObjects asks the API server to validate each rendered
// object with dryRun=All. Custom resources whose CRD is introduced by the same
// chart cannot be validated until that CRD exists, so they are skipped with an
// explicit warning instead of producing a false failure.
func (c *Client) preflightHelmPlanObjects(ctx context.Context, defaultNamespace string, generic []releaseutil.Manifest, hooks []*release.Hook, servedByGVK, byGVK, chartCRDs map[string]domain.ResourceDescriptor) ([]string, error) {
	if c.dynamic == nil {
		return []string{"动态 Kubernetes 客户端不可用，已跳过 server-side dry-run"}, nil
	}
	warnings := make([]string, 0)
	seenWarnings := make(map[string]bool)
	preflight := func(item releaseutil.Manifest) error {
		object := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(item.Content), object); err != nil {
			return fmt.Errorf("%w: parse rendered %s for preflight: %v", domain.ErrInvalid, item.Name, err)
		}
		key := object.GetAPIVersion() + "/" + object.GetKind()
		descriptor, ok := byGVK[key]
		if !ok {
			return fmt.Errorf("%w: rendered resource %s is not served by this cluster", domain.ErrUnsupported, key)
		}
		if _, chartOwned := chartCRDs[descriptor.Group+"/"+descriptor.Version+"/"+descriptor.Kind]; chartOwned {
			if _, served := servedByGVK[key]; !served {
				warning := fmt.Sprintf("跳过 %s 的 server-side dry-run：其 CRD 由当前 Chart 同时创建，需先创建 CRD 才能校验", key)
				if !seenWarnings[warning] {
					seenWarnings[warning] = true
					warnings = append(warnings, warning)
				}
				return nil
			}
		}
		if descriptor.Namespaced && object.GetNamespace() == "" {
			object.SetNamespace(defaultNamespace)
		}
		if !descriptor.Namespaced {
			object.SetNamespace("")
		}
		if err := c.applyHelmObject(ctx, descriptor, object, true); err != nil {
			return fmt.Errorf("server-side dry-run %s %s/%s: %w", key, object.GetNamespace(), object.GetName(), err)
		}
		return nil
	}
	for _, item := range generic {
		if err := preflight(item); err != nil {
			return nil, err
		}
	}
	for _, hook := range hooks {
		if err := preflight(releaseutil.Manifest{Name: hook.Path, Content: hook.Manifest}); err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

// renderHelmFiles uses the same client-aware lookup semantics as Helm's native
// install and upgrade actions when an in-cluster REST configuration is
// available. Tests and degraded clients still retain the non-mutating fallback.
func (c *Client) renderHelmFiles(ch *chart.Chart, renderValues chartutil.Values, actor domain.Actor) (map[string]string, error) {
	if config := c.helmRenderRESTConfig(actor); config != nil {
		return engine.RenderWithClient(ch, renderValues, config)
	}
	return engine.Render(ch, renderValues)
}

func (c *Client) helmRenderRESTConfig(actor domain.Actor) *rest.Config {
	if c.restConfig == nil || strings.TrimSpace(c.restConfig.Host) == "" {
		return nil
	}
	config := rest.CopyConfig(c.restConfig)
	if c.requestTimeout > 0 {
		config.Timeout = c.requestTimeout
	}
	previousWrap := config.WrapTransport
	if strings.TrimSpace(actor.Username) == "" {
		return config
	}
	config.WrapTransport = func(base http.RoundTripper) http.RoundTripper {
		var transport http.RoundTripper = base
		if previousWrap != nil {
			transport = previousWrap(base)
		}
		return &fixedImpersonationTransport{base: transport, actor: actor}
	}
	return config
}

// Helm renders NOTES.txt as plain installation notes, not Kubernetes YAML.
// Sorting all rendered files without excluding it makes ordinary Charts fail
// planning when their notes contain prose or template comments.
func removeHelmNotes(files map[string]string) {
	for name := range files {
		if name == "NOTES.txt" || strings.HasSuffix(name, "/NOTES.txt") {
			delete(files, name)
		}
	}
}

func helmPlanManifest(generic []releaseutil.Manifest, hooks []*release.Hook) string {
	documents := make([]string, 0, len(generic)+len(hooks))
	for _, item := range generic {
		if strings.TrimSpace(item.Content) != "" {
			documents = append(documents, fmt.Sprintf("# Source: %s\n%s", item.Name, strings.TrimSpace(item.Content)))
		}
	}
	for _, hook := range hooks {
		if strings.TrimSpace(hook.Manifest) != "" {
			documents = append(documents, fmt.Sprintf("# Source: %s (hook: %s)\n%s", hook.Path, hookEvents(hook), strings.TrimSpace(hook.Manifest)))
		}
	}
	return strings.Join(documents, "\n---\n")
}

func hookEvents(hook *release.Hook) string {
	events := make([]string, 0, len(hook.Events))
	for _, event := range hook.Events {
		events = append(events, string(event))
	}
	return strings.Join(events, ",")
}

func (c *Client) helmPlanResource(ctx context.Context, defaultNamespace, releaseName string, item releaseutil.Manifest, hook bool, byGVK map[string]domain.ResourceDescriptor, chartCRDs map[string]domain.ResourceDescriptor) (domain.HelmChartPlanResource, error) {
	var object struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(item.Content), &object); err != nil {
		return domain.HelmChartPlanResource{}, fmt.Errorf("%w: parse rendered %s: %v", domain.ErrInvalid, item.Name, err)
	}
	if object.APIVersion == "" || object.Kind == "" || object.Metadata.Name == "" {
		return domain.HelmChartPlanResource{}, fmt.Errorf("%w: rendered object %s has incomplete identity", domain.ErrInvalid, item.Name)
	}
	key := object.APIVersion + "/" + object.Kind
	descriptor, ok := byGVK[key]
	if !ok {
		return domain.HelmChartPlanResource{}, fmt.Errorf("%w: rendered resource %s is not served by this cluster", domain.ErrUnsupported, key)
	}
	namespace := object.Metadata.Namespace
	if descriptor.Namespaced && namespace == "" {
		namespace = defaultNamespace
	}
	if !descriptor.Namespaced {
		namespace = ""
	}
	exists := false
	if _, chartOwnedCRD := chartCRDs[key]; !chartOwnedCRD {
		existing, err := c.GetGenericResource(ctx, descriptor, namespace, object.Metadata.Name)
		switch {
		case err == nil:
			exists = true
			if !hook {
				if ownershipErr := validateHelmPlanOwnership(existing, releaseName, namespace); ownershipErr != nil {
					return domain.HelmChartPlanResource{}, fmt.Errorf("%w: existing %s %s/%s cannot be managed by Helm release %s/%s: %v", domain.ErrConflict, object.Kind, namespace, object.Metadata.Name, namespace, releaseName, ownershipErr)
				}
			}
		case errors.Is(err, domain.ErrNotFound):
			exists = false
		case err != nil:
			return domain.HelmChartPlanResource{}, err
		}
	}
	actionName := "create"
	if exists {
		actionName = "update"
	}
	return domain.HelmChartPlanResource{
		APIVersion: object.APIVersion, Kind: object.Kind, Namespace: namespace, Name: object.Metadata.Name,
		Hook: hook, CRD: strings.EqualFold(object.Kind, "CustomResourceDefinition"), Action: actionName,
		APIGroup: descriptor.Group, Resource: descriptor.Resource, Exists: exists,
	}, nil
}

func validateHelmPlanOwnership(object map[string]any, releaseName, namespace string) error {
	metadata := objectMapLike(object, "metadata")
	labels := objectMapLike(metadata, "labels")
	annotations := objectMapLike(metadata, "annotations")
	if labels[helmManagedByLabel] == "Helm" && annotations[helmReleaseNameAnnotation] == releaseName && annotations[helmReleaseNamespaceAnnotation] == namespace {
		return nil
	}
	actualManager := helmMetadataValue(labels[helmManagedByLabel])
	actualRelease := helmMetadataValue(annotations[helmReleaseNameAnnotation])
	actualNamespace := helmMetadataValue(annotations[helmReleaseNamespaceAnnotation])
	return fmt.Errorf("resource is managed by %q release %q in namespace %q", actualManager, actualRelease, actualNamespace)
}

func helmMetadataValue(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "unset"
	}
	return text
}

func uniqueHelmPlanResources(items []domain.HelmChartPlanResource) []domain.HelmChartPlanResource {
	byKey := make(map[string]int, len(items))
	result := make([]domain.HelmChartPlanResource, 0, len(items))
	for _, item := range items {
		key := strings.Join([]string{item.APIVersion, item.Kind, item.Namespace, item.Name}, "\x00")
		if index, ok := byKey[key]; ok {
			result[index].Hook = result[index].Hook || item.Hook
			continue
		}
		byKey[key] = len(result)
		result = append(result, item)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		if result[left].Namespace != result[right].Namespace {
			return result[left].Namespace < result[right].Namespace
		}
		return result[left].Name < result[right].Name
	})
	return result
}

func chartCRDDescriptors(ch *chart.Chart) map[string]domain.ResourceDescriptor {
	result := map[string]domain.ResourceDescriptor{}
	for _, crd := range ch.CRDObjects() {
		var object struct {
			APIVersion string `json:"apiVersion"`
			Spec       struct {
				Group string `json:"group"`
				Names struct {
					Kind   string `json:"kind"`
					Plural string `json:"plural"`
				} `json:"names"`
				Scope string `json:"scope"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal(crd.File.Data, &object); err != nil || object.Spec.Group == "" || object.Spec.Names.Kind == "" || object.Spec.Names.Plural == "" {
			continue
		}
		version := "v1"
		if parts := strings.Split(object.APIVersion, "/"); len(parts) == 2 {
			version = parts[1]
		}
		descriptor := domain.ResourceDescriptor{
			Group: object.Spec.Group, Version: version, Resource: object.Spec.Names.Plural,
			Kind: object.Spec.Names.Kind, Namespaced: !strings.EqualFold(object.Spec.Scope, "Cluster"),
		}
		result[object.Spec.Group+"/"+version+"/"+object.Spec.Names.Kind] = descriptor
	}
	return result
}

func loadHelmChartArchive(archive []byte, chartName, version string) (*chart.Chart, error) {
	if len(archive) == 0 {
		return nil, fmt.Errorf("%w: chart package is empty", domain.ErrInvalid)
	}
	ch, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("%w: load chart package: %v", domain.ErrInvalid, err)
	}
	if ch.Metadata == nil || ch.Metadata.Name == "" || ch.Metadata.Version == "" {
		return nil, fmt.Errorf("%w: chart metadata is incomplete", domain.ErrInvalid)
	}
	if ch.Metadata.Name != chartName || ch.Metadata.Version != version {
		return nil, fmt.Errorf("%w: downloaded chart identity %s@%s does not match %s@%s", domain.ErrConflict, ch.Metadata.Name, ch.Metadata.Version, chartName, version)
	}
	if ch.Metadata.Type == "library" {
		return nil, fmt.Errorf("%w: library charts cannot be installed as releases", domain.ErrInvalid)
	}
	return ch, nil
}

func parseHelmValues(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	if len(raw) > 1024*1024 {
		return nil, fmt.Errorf("%w: values must not exceed 1 MiB", domain.ErrInvalid)
	}
	values, err := chartutil.ReadValues([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: parse values YAML: %v", domain.ErrInvalid, err)
	}
	return map[string]any(values), nil
}

func helmReleaseState(configuration *action.Configuration, name string) (int, *release.Release, error) {
	history, err := configuration.Releases.History(name)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return 0, nil, nil
		}
		return 0, nil, mapHelmActionError(err)
	}
	if len(history) == 0 {
		return 0, nil, nil
	}
	releaseutil.SortByRevision(history)
	latest := history[len(history)-1]
	if latest.Info != nil && latest.Info.Status == release.StatusUninstalled {
		return latest.Version, nil, nil
	}
	return latest.Version, latest, nil
}

func (c *Client) helmConfiguration(actor domain.Actor, namespace string) (*action.Configuration, error) {
	if c.clientset == nil || c.discovery == nil || c.restConfig == nil {
		return nil, fmt.Errorf("%w: Helm operations require an in-cluster Kubernetes client", domain.ErrUnsupported)
	}
	restConfig := rest.CopyConfig(c.restConfig)
	previousWrap := restConfig.WrapTransport
	restConfig.WrapTransport = func(base http.RoundTripper) http.RoundTripper {
		var transport http.RoundTripper = base
		if previousWrap != nil {
			transport = previousWrap(base)
		}
		return &fixedImpersonationTransport{base: transport, actor: actor}
	}
	getter := &helmRESTClientGetter{
		restConfig: restConfig,
		discovery:  memory.NewMemCacheClient(c.discovery),
	}
	configuration := new(action.Configuration)
	if err := configuration.Init(getter, namespace, "secret", func(string, ...any) {}); err != nil {
		return nil, mapHelmActionError(err)
	}
	return configuration, nil
}

type fixedImpersonationTransport struct {
	base  http.RoundTripper
	actor domain.Actor
}

func (t *fixedImpersonationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Impersonate-User", t.actor.Username)
	for _, group := range t.actor.Groups {
		if group != "" {
			clone.Header.Add("Impersonate-Group", group)
		}
	}
	return t.base.RoundTrip(clone)
}

type helmRESTClientGetter struct {
	restConfig *rest.Config
	discovery  discovery.CachedDiscoveryInterface
}

func (g *helmRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return rest.CopyConfig(g.restConfig), nil
}

func (g *helmRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return g.discovery, nil
}

func (g *helmRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return restmapper.NewDeferredDiscoveryRESTMapper(g.discovery), nil
}

func (g *helmRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewDefaultClientConfig(clientcmdapi.Config{}, &clientcmd.ConfigOverrides{})
}

var _ genericclioptions.RESTClientGetter = (*helmRESTClientGetter)(nil)

func mapHelmActionError(err error) error {
	if err == nil {
		return nil
	}
	if status := apierrors.APIStatus(nil); errors.As(err, &status) {
		return mapKubeError(err)
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "forbidden"):
		return fmt.Errorf("%w: %v", domain.ErrForbidden, err)
	case strings.Contains(message, "not found"), errors.Is(err, driver.ErrReleaseNotFound):
		return fmt.Errorf("%w: %v", domain.ErrNotFound, err)
	case strings.Contains(message, "exist"), errors.Is(err, driver.ErrReleaseExists):
		return fmt.Errorf("%w: %v", domain.ErrAlreadyExists, err)
	case strings.Contains(message, "invalid"), strings.Contains(message, "failed render"), strings.Contains(message, "validation"):
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	default:
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
}
