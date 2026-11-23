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

// PlanHelmChart renders a downloaded chart and resolves every object to a
// Kubernetes resource. It does not install anything. The caller performs RBAC
// checks against the returned resources before allowing an operation.
func (c *Client) PlanHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error) {
	if err := c.validateHelmTarget(ctx, request.Namespace, request.ReleaseName); err != nil {
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
	currentRevision, latest, err := helmReleaseState(configuration, request.ReleaseName)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	switch mode {
	case "install":
		if latest != nil {
			return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: Helm release %s/%s already exists", domain.ErrAlreadyExists, request.Namespace, request.ReleaseName)
		}
	case "upgrade":
		if latest == nil {
			return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: Helm release %s/%s", domain.ErrNotFound, request.Namespace, request.ReleaseName)
		}
		if latest.Info != nil && latest.Info.Status != release.StatusDeployed {
			return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: only a deployed Helm release can be upgraded (current status %s)", domain.ErrInvalid, latest.Info.Status)
		}
	default:
		return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: plan mode must be install or upgrade", domain.ErrInvalid)
	}

	values, err := parseHelmValues(request.Values)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	mergedValues, manifest, hooks, generic, renderWarnings, err := c.renderHelmChart(ctx, ch, request.Namespace, request.ReleaseName, currentRevision+1, values)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	descriptors, warnings, err := c.DiscoverResources(ctx)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, err
	}
	warnings = append(renderWarnings, warnings...)
	byGVK := helmDescriptorsByGVK(descriptors)
	chartCRDs := chartCRDDescriptors(ch)
	for key, descriptor := range chartCRDs {
		if _, exists := byGVK[key]; !exists {
			byGVK[key] = descriptor
		}
	}
	resources := make([]domain.HelmChartPlanResource, 0, len(generic)+len(hooks)+len(ch.CRDObjects()))
	for _, item := range generic {
		resource, err := c.helmPlanResource(ctx, request.Namespace, item, false, byGVK, chartCRDs)
		if err != nil {
			return domain.HelmChartPlanTemplate{}, err
		}
		resources = append(resources, resource)
	}
	for _, hook := range hooks {
		resource, err := c.helmPlanResource(ctx, request.Namespace, releaseutil.Manifest{Name: hook.Path, Content: hook.Manifest}, true, byGVK, chartCRDs)
		if err != nil {
			return domain.HelmChartPlanTemplate{}, err
		}
		resources = append(resources, resource)
	}
	resources = uniqueHelmPlanResources(resources)
	encodedValues, err := yaml.Marshal(mergedValues)
	if err != nil {
		return domain.HelmChartPlanTemplate{}, fmt.Errorf("%w: encode Helm values: %v", domain.ErrInvalid, err)
	}
	if len(mergedValues) == 0 {
		encodedValues = []byte("{}\n")
	}
	return domain.HelmChartPlanTemplate{
		Manifest:        manifest,
		Values:          string(encodedValues),
		Resources:       resources,
		CurrentRevision: currentRevision,
		Warnings:        warnings,
	}, nil
}

func (c *Client) InstallHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (int, error) {
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
	if currentRevision, latest, stateErr := helmReleaseState(configuration, request.ReleaseName); stateErr != nil {
		return 0, stateErr
	} else if latest != nil {
		return 0, fmt.Errorf("%w: Helm release %s/%s already exists at revision %d", domain.ErrAlreadyExists, request.Namespace, request.ReleaseName, currentRevision)
	}
	install := action.NewInstall(configuration)
	install.ReleaseName = request.ReleaseName
	install.Namespace = request.Namespace
	install.Wait = true
	install.WaitForJobs = true
	install.Atomic = true
	install.Timeout = helmActionTimeout
	install.CreateNamespace = false
	install.Description = "Installed by Dosh from an approved Helm repository"
	result, err := install.RunWithContext(ctx, ch, values)
	if err != nil {
		return 0, mapHelmActionError(err)
	}
	if result == nil {
		return 0, fmt.Errorf("%w: Helm install returned no release", domain.ErrUnavailable)
	}
	return result.Version, nil
}

func (c *Client) UpgradeHelmChart(ctx context.Context, actor domain.Actor, archive []byte, request domain.HelmChartPlanRequest) (int, error) {
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
	_, latest, err := helmReleaseState(configuration, request.ReleaseName)
	if err != nil {
		return 0, err
	}
	if latest == nil {
		return 0, fmt.Errorf("%w: Helm release %s/%s", domain.ErrNotFound, request.Namespace, request.ReleaseName)
	}
	upgrade := action.NewUpgrade(configuration)
	upgrade.Namespace = request.Namespace
	upgrade.Wait = true
	upgrade.WaitForJobs = true
	upgrade.Atomic = true
	upgrade.CleanupOnFail = true
	upgrade.MaxHistory = 10
	upgrade.Timeout = helmActionTimeout
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

func (c *Client) UninstallHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string) error {
	if err := c.validateHelmTarget(ctx, namespace, releaseName); err != nil {
		return err
	}
	configuration, err := c.helmConfiguration(actor, namespace)
	if err != nil {
		return err
	}
	uninstall := action.NewUninstall(configuration)
	uninstall.Wait = true
	uninstall.Timeout = helmActionTimeout
	uninstall.KeepHistory = false
	uninstall.Description = "Uninstalled by Dosh"
	_, err = uninstall.Run(releaseName)
	if err != nil {
		return mapHelmActionError(err)
	}
	return nil
}

func (c *Client) RollbackHelmChart(ctx context.Context, actor domain.Actor, namespace, releaseName string, targetRevision int) (int, error) {
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
	rollback.Wait = true
	rollback.WaitForJobs = true
	rollback.CleanupOnFail = true
	rollback.Timeout = helmActionTimeout
	rollback.MaxHistory = 10
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
	namespace = strings.TrimSpace(namespace)
	releaseName = strings.TrimSpace(releaseName)
	if namespace == "" || releaseName == "" {
		return fmt.Errorf("%w: namespace and release name are required", domain.ErrInvalid)
	}
	if c.ControlPlaneNamespace() != "" && namespace == c.ControlPlaneNamespace() {
		return fmt.Errorf("%w: Helm releases cannot be installed in the Dosh control-plane namespace", domain.ErrForbidden)
	}
	if err := chartutil.ValidateReleaseName(releaseName); err != nil {
		return fmt.Errorf("%w: release name is invalid", domain.ErrInvalid)
	}
	if err := chartutil.ValidateMetadataName(namespace); err != nil {
		return fmt.Errorf("%w: namespace is invalid", domain.ErrInvalid)
	}
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if _, err := c.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		return mapKubeError(err)
	}
	return nil
}

func (c *Client) renderHelmChart(ctx context.Context, ch *chart.Chart, namespace, releaseName string, revision int, values map[string]any) (map[string]any, string, []*release.Hook, []releaseutil.Manifest, []string, error) {
	caps := chartutil.DefaultCapabilities.Copy()
	warnings := []string{}
	if info, err := c.serverVersion(ctx); err == nil && info != nil {
		caps.KubeVersion = chartutil.KubeVersion{Version: info.GitVersion, Major: info.Major, Minor: info.Minor}
	} else {
		warnings = append(warnings, "无法读取 Kubernetes 版本，模板 Capabilities 使用 Helm 默认值")
	}
	renderValues, err := chartutil.ToRenderValues(ch, values, chartutil.ReleaseOptions{
		Name: releaseName, Namespace: namespace, Revision: revision, IsInstall: revision <= 1, IsUpgrade: revision > 1,
	}, caps)
	if err != nil {
		return nil, "", nil, nil, nil, fmt.Errorf("%w: prepare Helm render values: %v", domain.ErrInvalid, err)
	}
	merged, _ := renderValues["Values"].(map[string]any)
	if merged == nil {
		merged = values
	}
	files, err := engine.Render(ch, renderValues)
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

func (c *Client) helmPlanResource(ctx context.Context, defaultNamespace string, item releaseutil.Manifest, hook bool, byGVK map[string]domain.ResourceDescriptor, chartCRDs map[string]domain.ResourceDescriptor) (domain.HelmChartPlanResource, error) {
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
		_, err := c.GetGenericResource(ctx, descriptor, namespace, object.Metadata.Name)
		switch {
		case err == nil:
			exists = true
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
