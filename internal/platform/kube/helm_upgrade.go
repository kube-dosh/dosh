package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type renderedHelmUpgrade struct {
	domain.HelmReleaseUpgradeRender
	release  *decodedHelmRelease
	config   map[string]any
	manifest string
}

// PreviewHelmUpgrade renders values over the release's server-side current
// config. The values argument is treated as an override, never as a replacement
// for redacted values that were shown in the browser.
func (c *Client) PreviewHelmUpgrade(ctx context.Context, namespace, name, values string) (domain.HelmReleaseUpgradeRender, error) {
	rendered, err := c.renderHelmUpgrade(ctx, namespace, name, values)
	if err != nil {
		return domain.HelmReleaseUpgradeRender{}, err
	}
	return rendered.HelmReleaseUpgradeRender, nil
}

// UpgradeHelmRelease renders the chart stored in the current revision, applies
// every rendered object with a full dry-run preflight, then records a new Helm
// compatible revision. Hooks and removed-object pruning are intentionally out
// of scope and reported as warnings.
func (c *Client) UpgradeHelmRelease(ctx context.Context, namespace, name, values string) (domain.HelmReleaseUpgradeResult, error) {
	rendered, err := c.renderHelmUpgrade(ctx, namespace, name, values)
	if err != nil {
		return domain.HelmReleaseUpgradeResult{}, err
	}
	objects, hookCount, objectWarnings, err := c.helmManifestObjects(rendered.manifest, namespace)
	if err != nil {
		return domain.HelmReleaseUpgradeResult{}, err
	}
	warnings := append([]string(nil), rendered.Warnings...)
	warnings = append(warnings, objectWarnings...)
	if hookCount > 0 {
		warnings = append(warnings, fmt.Sprintf("已跳过 %d 个 Helm Hook，升级不会执行 pre/post hook", hookCount))
	}
	warnings = append(warnings, "升级只会应用新清单；目标清单中不存在、但当前仍存在的对象不会被删除")
	if len(objects) == 0 {
		warnings = append(warnings, "渲染后的清单为空，仅创建新的 Helm 历史记录")
	}
	descriptors, discoveryWarnings, err := c.DiscoverResources(ctx)
	if err != nil {
		return domain.HelmReleaseUpgradeResult{}, err
	}
	warnings = append(warnings, discoveryWarnings...)
	byGVK := helmDescriptorsByGVK(descriptors)
	for index := range objects {
		key := objects[index].GetAPIVersion() + "/" + objects[index].GetKind()
		descriptor, ok := byGVK[key]
		if !ok {
			return domain.HelmReleaseUpgradeResult{}, fmt.Errorf("%w: resource type %s is not discoverable", domain.ErrUnsupported, key)
		}
		if descriptor.Namespaced && objects[index].GetNamespace() == "" {
			objects[index].SetNamespace(namespace)
		}
		if err := c.applyHelmObject(ctx, descriptor, objects[index], true); err != nil {
			return domain.HelmReleaseUpgradeResult{}, fmt.Errorf("preflight %s %s: %w", key, objects[index].GetName(), err)
		}
	}
	for index := range objects {
		descriptor := byGVK[objects[index].GetAPIVersion()+"/"+objects[index].GetKind()]
		if err := c.applyHelmObject(ctx, descriptor, objects[index], false); err != nil {
			return domain.HelmReleaseUpgradeResult{}, fmt.Errorf("apply %s %s: %w", descriptor.Kind, objects[index].GetName(), err)
		}
	}
	if err := c.recordHelmUpgrade(ctx, namespace, name, rendered, len(objects)); err != nil {
		return domain.HelmReleaseUpgradeResult{}, err
	}
	return domain.HelmReleaseUpgradeResult{
		Namespace: namespace, Name: name, PreviousRevision: rendered.CurrentRevision,
		Revision: rendered.NextRevision, AppliedObjects: len(objects), Warnings: warnings,
	}, nil
}

func (c *Client) renderHelmUpgrade(ctx context.Context, namespace, name, values string) (renderedHelmUpgrade, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return renderedHelmUpgrade{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	releases, warnings, err := c.readHelmReleases(ctx, namespace)
	if err != nil {
		return renderedHelmUpgrade{}, err
	}
	var current *decodedHelmRelease
	maxRevision := 0
	for index := range releases {
		item := &releases[index]
		if item.payload.Name == name && item.payload.Version > maxRevision {
			current = item
			maxRevision = item.payload.Version
		}
	}
	if current == nil {
		return renderedHelmUpgrade{}, fmt.Errorf("%w: helm release %s/%s", domain.ErrNotFound, namespace, name)
	}
	if current.payload.Info.Status != "" && current.payload.Info.Status != "deployed" {
		return renderedHelmUpgrade{}, fmt.Errorf("%w: only a deployed Helm release can be upgraded (current status %s)", domain.ErrInvalid, current.payload.Info.Status)
	}
	ch, err := decodeHelmChart(current.raw)
	if err != nil {
		return renderedHelmUpgrade{}, err
	}
	nextConfig, err := deepCopyMap(current.payload.Config)
	if err != nil {
		return renderedHelmUpgrade{}, fmt.Errorf("%w: copy current Helm values: %v", domain.ErrInvalid, err)
	}
	if strings.TrimSpace(values) != "" {
		override, err := chartutil.ReadValues([]byte(values))
		if err != nil {
			return renderedHelmUpgrade{}, fmt.Errorf("%w: parse values YAML: %v", domain.ErrInvalid, err)
		}
		nextConfig = mergeHelmValues(nextConfig, map[string]any(override))
	}
	caps := chartutil.DefaultCapabilities.Copy()
	if info, versionErr := c.serverVersion(ctx); versionErr == nil && info != nil {
		caps.KubeVersion = chartutil.KubeVersion{Version: info.GitVersion, Major: info.Major, Minor: info.Minor}
	} else {
		warnings = append(warnings, "无法读取 Kubernetes 版本，模板 Capabilities 使用 Helm 默认值")
	}
	renderValues, err := chartutil.ToRenderValues(ch, nextConfig, chartutil.ReleaseOptions{
		Name: name, Namespace: namespace, Revision: maxRevision + 1, IsUpgrade: true,
	}, caps)
	if err != nil {
		return renderedHelmUpgrade{}, fmt.Errorf("%w: prepare Helm render values: %v", domain.ErrInvalid, err)
	}
	files, err := engine.Render(ch, renderValues)
	if err != nil {
		return renderedHelmUpgrade{}, fmt.Errorf("%w: render Helm chart: %v", domain.ErrInvalid, err)
	}
	nextManifest := helmRenderedManifest(files)
	currentValues, err := marshalHelmValues(current.payload.Config)
	if err != nil {
		return renderedHelmUpgrade{}, err
	}
	nextValues, err := marshalHelmValues(nextConfig)
	if err != nil {
		return renderedHelmUpgrade{}, err
	}
	return renderedHelmUpgrade{
		HelmReleaseUpgradeRender: domain.HelmReleaseUpgradeRender{
			Namespace: namespace, Name: name, CurrentRevision: maxRevision, NextRevision: maxRevision + 1,
			CurrentValues: currentValues, NextValues: nextValues,
			CurrentManifest: current.payload.Manifest, NextManifest: nextManifest,
			Warnings: warnings,
		},
		release: current, config: nextConfig, manifest: nextManifest,
	}, nil
}

func decodeHelmChart(raw map[string]any) (*chart.Chart, error) {
	encoded, err := json.Marshal(raw["chart"])
	if err != nil {
		return nil, fmt.Errorf("%w: encode stored Helm chart: %v", domain.ErrInvalid, err)
	}
	var ch chart.Chart
	if err := json.Unmarshal(encoded, &ch); err != nil {
		return nil, fmt.Errorf("%w: decode stored Helm chart: %v", domain.ErrInvalid, err)
	}
	if ch.Metadata == nil || ch.Metadata.Name == "" || len(ch.Templates) == 0 {
		return nil, fmt.Errorf("%w: Helm release does not contain a renderable chart", domain.ErrUnsupported)
	}
	if len(ch.Metadata.Dependencies) > 0 {
		return nil, fmt.Errorf("%w: Helm release declares sub-chart dependencies, but Helm release secrets do not retain their templates; upgrade this release with Helm to avoid dropping sub-chart objects", domain.ErrUnsupported)
	}
	return &ch, nil
}

func helmRenderedManifest(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for key := range files {
		if strings.HasSuffix(key, "/NOTES.txt") || key == "NOTES.txt" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	documents := make([]string, 0, len(keys))
	for _, key := range keys {
		if content := strings.TrimSpace(files[key]); content != "" {
			documents = append(documents, content)
		}
	}
	return strings.Join(documents, "\n---\n")
}

func marshalHelmValues(values map[string]any) (string, error) {
	if len(values) == 0 {
		return "{}\n", nil
	}
	encoded, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("%w: encode Helm values: %v", domain.ErrInvalid, err)
	}
	return string(encoded), nil
}

func deepCopyMap(value map[string]any) (map[string]any, error) {
	if value == nil {
		return make(map[string]any), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any)
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = make(map[string]any)
	}
	return result, nil
}

func mergeHelmValues(base, override map[string]any) map[string]any {
	out, err := deepCopyMap(base)
	if err != nil {
		out = make(map[string]any)
	}
	for key, value := range override {
		if nested, ok := value.(map[string]any); ok {
			if existing, ok := out[key].(map[string]any); ok {
				out[key] = mergeHelmValues(existing, nested)
				continue
			}
		}
		out[key] = value
	}
	return out
}

func helmDescriptorsByGVK(descriptors []domain.ResourceDescriptor) map[string]domain.ResourceDescriptor {
	byGVK := make(map[string]domain.ResourceDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		apiVersion := descriptor.Version
		if descriptor.Group != "" {
			apiVersion = descriptor.Group + "/" + descriptor.Version
		}
		key := apiVersion + "/" + descriptor.Kind
		existing, exists := byGVK[key]
		if !exists || (!existing.Preferred && descriptor.Preferred) {
			byGVK[key] = descriptor
		}
	}
	return byGVK
}

func (c *Client) recordHelmUpgrade(ctx context.Context, namespace, name string, rendered renderedHelmUpgrade, appliedObjects int) error {
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	raw, err := cloneJSONMap(rendered.release.raw)
	if err != nil {
		return fmt.Errorf("%w: encode Helm history: %v", domain.ErrInvalid, err)
	}
	now := time.Now().UTC()
	raw["name"] = name
	raw["namespace"] = namespace
	raw["version"] = rendered.NextRevision
	raw["config"] = rendered.config
	raw["manifest"] = rendered.manifest
	info, _ := raw["info"].(map[string]any)
	if info == nil {
		info = make(map[string]any)
		raw["info"] = info
	}
	info["last_deployed"] = now
	info["status"] = "deployed"
	info["description"] = fmt.Sprintf("Upgrade complete: applied %d objects", appliedObjects)
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("%w: encode Helm release payload: %v", domain.ErrInvalid, err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		return fmt.Errorf("%w: compress Helm release payload: %v", domain.ErrUnavailable, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("%w: compress Helm release payload: %v", domain.ErrUnavailable, err)
	}
	secretName := fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, rendered.NextRevision)
	labels := map[string]string{
		"owner": "helm", "name": name, "status": "deployed",
		"version": strconv.Itoa(rendered.NextRevision), "createdAt": strconv.FormatInt(now.Unix(), 10),
	}
	if _, err := c.clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace, Labels: labels},
		Type:       corev1.SecretType(helmReleaseSecretType),
		Data:       map[string][]byte{"release": []byte(base64.StdEncoding.EncodeToString(compressed.Bytes()))},
	}, metav1.CreateOptions{FieldManager: "dosh"}); err != nil {
		return mapKubeError(err)
	}
	if rendered.release.secretName == "" {
		return nil
	}
	current, err := c.clientset.CoreV1().Secrets(namespace).Get(ctx, rendered.release.secretName, metav1.GetOptions{})
	if err != nil {
		return mapKubeError(err)
	}
	if current.Labels == nil {
		current.Labels = make(map[string]string)
	}
	current.Labels["status"] = "superseded"
	if _, err := c.clientset.CoreV1().Secrets(namespace).Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"}); err != nil {
		return mapKubeError(err)
	}
	return nil
}
