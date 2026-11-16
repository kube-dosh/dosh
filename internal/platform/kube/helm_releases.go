package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

const helmReleaseSecretType = "helm.sh/release.v1"

type helmReleasePayload struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		FirstDeployed time.Time `json:"first_deployed"`
		LastDeployed  time.Time `json:"last_deployed"`
		Status        string    `json:"status"`
		Description   string    `json:"description"`
		Notes         string    `json:"notes"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
	Config   map[string]any `json:"config"`
	Manifest string         `json:"manifest"`
}

// ListHelmReleases reads Helm's Secret storage in one namespace. Helm release
// payloads can contain credentials, so callers must require Kubernetes Secret
// read permission and redact before exposing the result.
func (c *Client) ListHelmReleases(ctx context.Context, namespace string) ([]domain.HelmReleaseSummary, []string, error) {
	releases, warnings, err := c.readHelmReleases(ctx, namespace)
	if err != nil {
		return nil, nil, err
	}
	latest := make(map[string]helmReleasePayload)
	for _, item := range releases {
		current, ok := latest[item.payload.Name]
		if !ok || item.payload.Version > current.Version {
			latest[item.payload.Name] = item.payload
		}
	}
	items := make([]domain.HelmReleaseSummary, 0, len(latest))
	for _, release := range latest {
		items = append(items, helmReleaseSummary(release))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, warnings, nil
}

// GetHelmRelease returns one release and its revision history, newest first.
func (c *Client) GetHelmRelease(ctx context.Context, namespace, name string) (domain.HelmReleaseDetail, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domain.HelmReleaseDetail{}, fmt.Errorf("%w: namespace and helm release name are required", domain.ErrInvalid)
	}
	releases, warnings, err := c.readHelmReleases(ctx, namespace)
	if err != nil {
		return domain.HelmReleaseDetail{}, err
	}
	var selected *helmReleasePayload
	history := make([]domain.HelmReleaseSummary, 0)
	for index := range releases {
		release := releases[index].payload
		if release.Name != name {
			continue
		}
		history = append(history, helmReleaseSummary(release))
		if selected == nil || release.Version > selected.Version {
			copy := release
			selected = &copy
		}
	}
	if selected == nil {
		return domain.HelmReleaseDetail{}, fmt.Errorf("%w: helm release %s/%s", domain.ErrNotFound, namespace, name)
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Revision > history[j].Revision })
	return helmReleaseDetail(*selected, history, warnings), nil
}

// GetHelmReleaseRevision returns one specific revision for diffing without
// exposing every release payload to the browser.
func (c *Client) GetHelmReleaseRevision(ctx context.Context, namespace, name string, revision int) (domain.HelmReleaseDetail, error) {
	if revision <= 0 {
		return domain.HelmReleaseDetail{}, fmt.Errorf("%w: revision must be positive", domain.ErrInvalid)
	}
	releases, warnings, err := c.readHelmReleases(ctx, namespace)
	if err != nil {
		return domain.HelmReleaseDetail{}, err
	}
	var selected *helmReleasePayload
	history := make([]domain.HelmReleaseSummary, 0)
	for index := range releases {
		release := releases[index].payload
		if release.Name != name {
			continue
		}
		history = append(history, helmReleaseSummary(release))
		if release.Version == revision {
			copy := release
			selected = &copy
		}
	}
	if selected == nil {
		return domain.HelmReleaseDetail{}, fmt.Errorf("%w: helm release %s/%s revision %d", domain.ErrNotFound, namespace, name, revision)
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Revision > history[j].Revision })
	return helmReleaseDetail(*selected, history, warnings), nil
}

func helmReleaseDetail(selected helmReleasePayload, history []domain.HelmReleaseSummary, warnings []string) domain.HelmReleaseDetail {
	values := "{}"
	if len(selected.Config) > 0 {
		encoded, err := yaml.Marshal(selected.Config)
		if err != nil {
			warnings = append(warnings, "Helm values 无法序列化为 YAML")
		} else {
			values = string(encoded)
		}
	}
	return domain.HelmReleaseDetail{
		HelmReleaseSummary: helmReleaseSummary(selected), FirstDeployed: selected.Info.FirstDeployed,
		LastDeployed: selected.Info.LastDeployed, Notes: selected.Info.Notes, Values: values,
		Manifest: selected.Manifest, History: history, Warnings: warnings,
	}
}

type decodedHelmRelease struct {
	payload    helmReleasePayload
	raw        map[string]any
	secretName string
}

func (c *Client) readHelmReleases(ctx context.Context, namespace string) ([]decodedHelmRelease, []string, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, nil, fmt.Errorf("%w: namespace is required for helm releases", domain.ErrInvalid)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	secrets, err := c.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, nil, mapKubeError(err)
	}
	result := make([]decodedHelmRelease, 0, len(secrets.Items))
	warnings := make([]string, 0)
	for _, secret := range secrets.Items {
		if secret.Type != corev1.SecretType(helmReleaseSecretType) && secret.Labels["owner"] != "helm" {
			continue
		}
		payload, raw, err := decodeHelmReleaseSecret(secret)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("无法解析 Helm Secret %s：%v", secret.Name, err))
			continue
		}
		if payload.Namespace == "" {
			payload.Namespace = namespace
		}
		result = append(result, decodedHelmRelease{payload: payload, raw: raw, secretName: secret.Name})
	}
	return result, warnings, nil
}

func decodeHelmReleaseSecret(secret corev1.Secret) (helmReleasePayload, map[string]any, error) {
	raw, ok := secret.Data["release"]
	if !ok {
		return helmReleasePayload{}, nil, fmt.Errorf("release data is missing")
	}
	decoded, err := decodeHelmReleasePayload(raw)
	if err != nil {
		return helmReleasePayload{}, nil, err
	}
	var release helmReleasePayload
	if err := json.Unmarshal(decoded, &release); err != nil {
		return helmReleasePayload{}, nil, fmt.Errorf("decode JSON: %w", err)
	}
	if release.Name == "" || release.Version <= 0 {
		return helmReleasePayload{}, nil, fmt.Errorf("release identity is incomplete")
	}
	var object map[string]any
	if err := json.Unmarshal(decoded, &object); err != nil {
		return helmReleasePayload{}, nil, fmt.Errorf("decode release object: %w", err)
	}
	return release, object, nil
}

func decodeHelmReleasePayload(raw []byte) ([]byte, error) {
	data := bytes.TrimSpace(raw)
	for attempt := 0; attempt < 2; attempt++ {
		if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
			break
		}
		if len(data) > 0 && data[0] == '{' {
			return data, nil
		}
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode release base64: %w", err)
		}
		data = decoded
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return nil, fmt.Errorf("release payload is not gzip or JSON")
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open release gzip: %w", err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("decompress release: %w", err)
	}
	return decoded, nil
}

func helmReleaseSummary(release helmReleasePayload) domain.HelmReleaseSummary {
	return domain.HelmReleaseSummary{
		Name: release.Name, Namespace: release.Namespace,
		Chart: release.Chart.Metadata.Name, ChartVersion: release.Chart.Metadata.Version,
		AppVersion: release.Chart.Metadata.AppVersion, Revision: release.Version,
		Status: release.Info.Status, Description: release.Info.Description,
		UpdatedAt: release.Info.LastDeployed,
	}
}

// RollbackHelmRelease applies the manifest stored in a previous Helm revision
// and records a new Helm-compatible release revision. Helm hooks are skipped,
// and objects removed from the target manifest are intentionally left in the
// cluster; both limitations are returned as warnings. Every object is dry-run
// applied before any live apply so schema and admission failures cannot leave a
// partially attempted rollout.
func (c *Client) RollbackHelmRelease(ctx context.Context, namespace, name string, targetRevision int) (domain.HelmReleaseRollbackResult, error) {
	releases, warnings, err := c.readHelmReleases(ctx, namespace)
	if err != nil {
		return domain.HelmReleaseRollbackResult{}, err
	}
	var current *decodedHelmRelease
	var target *decodedHelmRelease
	maxRevision := 0
	for index := range releases {
		item := &releases[index]
		if item.payload.Name != name {
			continue
		}
		if item.payload.Version > maxRevision {
			maxRevision = item.payload.Version
			current = item
		}
		if item.payload.Version == targetRevision {
			target = item
		}
	}
	if current == nil {
		return domain.HelmReleaseRollbackResult{}, fmt.Errorf("%w: helm release %s/%s", domain.ErrNotFound, namespace, name)
	}
	if target == nil {
		return domain.HelmReleaseRollbackResult{}, fmt.Errorf("%w: helm release %s/%s revision %d", domain.ErrNotFound, namespace, name, targetRevision)
	}
	if current.payload.Version == targetRevision {
		return domain.HelmReleaseRollbackResult{}, fmt.Errorf("%w: target revision is already the current revision", domain.ErrInvalid)
	}
	objects, hookCount, objectWarnings, err := c.helmManifestObjects(target.payload.Manifest, namespace)
	warnings = append(warnings, objectWarnings...)
	if err != nil {
		return domain.HelmReleaseRollbackResult{}, err
	}
	if hookCount > 0 {
		warnings = append(warnings, fmt.Sprintf("已跳过 %d 个 Helm Hook，回滚不会重新执行 pre/post hook", hookCount))
	}
	warnings = append(warnings, "回滚只会应用目标版本清单；目标清单中不存在、但当前仍存在的对象不会被删除")
	if len(objects) == 0 {
		warnings = append(warnings, "目标版本的渲染清单为空，仅创建新的 Helm 历史记录")
	}
	descriptors, discoveryWarnings, err := c.DiscoverResources(ctx)
	if err != nil {
		return domain.HelmReleaseRollbackResult{}, err
	}
	warnings = append(warnings, discoveryWarnings...)
	byGVK := make(map[string]domain.ResourceDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		apiVersion := descriptor.Version
		if descriptor.Group != "" {
			apiVersion = descriptor.Group + "/" + descriptor.Version
		}
		byGVK[apiVersion+"/"+descriptor.Kind] = descriptor
	}
	for index := range objects {
		key := objects[index].GetAPIVersion() + "/" + objects[index].GetKind()
		descriptor, ok := byGVK[key]
		if !ok {
			return domain.HelmReleaseRollbackResult{}, fmt.Errorf("%w: resource type %s is not discoverable", domain.ErrUnsupported, key)
		}
		if descriptor.Namespaced && objects[index].GetNamespace() == "" {
			objects[index].SetNamespace(namespace)
		}
		if err := c.applyHelmObject(ctx, descriptor, objects[index], true); err != nil {
			return domain.HelmReleaseRollbackResult{}, fmt.Errorf("preflight %s %s: %w", key, objects[index].GetName(), err)
		}
	}
	for index := range objects {
		descriptor := byGVK[objects[index].GetAPIVersion()+"/"+objects[index].GetKind()]
		if err := c.applyHelmObject(ctx, descriptor, objects[index], false); err != nil {
			return domain.HelmReleaseRollbackResult{}, fmt.Errorf("apply %s %s: %w", descriptor.Kind, objects[index].GetName(), err)
		}
	}
	nextRevision := maxRevision + 1
	if err := c.recordHelmRollback(ctx, namespace, name, nextRevision, targetRevision, target, current.secretName); err != nil {
		return domain.HelmReleaseRollbackResult{}, err
	}
	return domain.HelmReleaseRollbackResult{
		Namespace: namespace, Name: name, TargetRevision: targetRevision,
		Revision: nextRevision, AppliedObjects: len(objects), Warnings: warnings,
	}, nil
}

func (c *Client) helmManifestObjects(manifest, releaseNamespace string) ([]*unstructured.Unstructured, int, []string, error) {
	if strings.TrimSpace(manifest) == "" {
		return nil, 0, nil, nil
	}
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	result := make([]*unstructured.Unstructured, 0)
	warnings := make([]string, 0)
	hooks := 0
	for {
		var object map[string]any
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, warnings, fmt.Errorf("%w: parse Helm manifest: %v", domain.ErrInvalid, err)
		}
		if len(object) == 0 {
			continue
		}
		expanded, expandWarnings := expandHelmManifestObject(object, releaseNamespace)
		warnings = append(warnings, expandWarnings...)
		for _, item := range expanded {
			item := item
			if item.GetAPIVersion() == "" || item.GetKind() == "" || item.GetName() == "" {
				return nil, 0, warnings, fmt.Errorf("%w: Helm manifest object is missing apiVersion, kind or metadata.name", domain.ErrInvalid)
			}
			if strings.TrimSpace(item.GetAnnotations()["helm.sh/hook"]) != "" {
				hooks++
				continue
			}
			cleanHelmObject(item)
			result = append(result, item)
		}
	}
	return result, hooks, warnings, nil
}

func expandHelmManifestObject(object map[string]any, releaseNamespace string) ([]*unstructured.Unstructured, []string) {
	item := &unstructured.Unstructured{Object: object}
	if !strings.EqualFold(item.GetKind(), "List") {
		return []*unstructured.Unstructured{item}, nil
	}
	items, found, err := unstructured.NestedSlice(object, "items")
	if err != nil || !found {
		return nil, []string{"Helm Manifest 中的 List 对象不包含可解析的 items，已跳过"}
	}
	result := make([]*unstructured.Unstructured, 0, len(items))
	for _, raw := range items {
		nested, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		child := &unstructured.Unstructured{Object: nested}
		if child.GetNamespace() == "" {
			child.SetNamespace(releaseNamespace)
		}
		result = append(result, child)
	}
	return result, nil
}

func cleanHelmObject(item *unstructured.Unstructured) {
	item.SetResourceVersion("")
	item.SetUID("")
	item.SetGeneration(0)
	item.SetCreationTimestamp(metav1.Time{})
	item.SetDeletionTimestamp(nil)
	item.SetDeletionGracePeriodSeconds(nil)
	item.SetManagedFields(nil)
	unstructured.RemoveNestedField(item.Object, "status")
}

func (c *Client) applyHelmObject(ctx context.Context, descriptor domain.ResourceDescriptor, item *unstructured.Unstructured, dryRun bool) error {
	if c.dynamic == nil {
		return fmt.Errorf("%w: dynamic Kubernetes client is unavailable", domain.ErrUnavailable)
	}
	body, err := json.Marshal(item.Object)
	if err != nil {
		return fmt.Errorf("%w: encode Helm manifest object: %v", domain.ErrInvalid, err)
	}
	gvr := schema.GroupVersionResource{Group: descriptor.Group, Version: descriptor.Version, Resource: descriptor.Resource}
	resource := c.dynamic.Resource(gvr)
	var target dynamic.ResourceInterface = resource
	if descriptor.Namespaced && item.GetNamespace() != "" {
		target = resource.Namespace(item.GetNamespace())
	}
	force := true
	options := metav1.PatchOptions{FieldManager: "dosh-helm-rollback", Force: &force}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	_, err = target.Patch(ctx, item.GetName(), types.ApplyPatchType, body, options)
	return mapKubeError(err)
}

func (c *Client) recordHelmRollback(ctx context.Context, namespace, name string, revision, targetRevision int, target *decodedHelmRelease, currentSecret string) error {
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	raw, err := cloneJSONMap(target.raw)
	if err != nil {
		return fmt.Errorf("%w: encode Helm history: %v", domain.ErrInvalid, err)
	}
	now := time.Now().UTC()
	raw["name"] = name
	raw["namespace"] = namespace
	raw["version"] = revision
	info, _ := raw["info"].(map[string]any)
	if info == nil {
		info = make(map[string]any)
		raw["info"] = info
	}
	info["last_deployed"] = now
	info["status"] = "deployed"
	info["description"] = fmt.Sprintf("Rollback to revision %d", targetRevision)
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
	secretName := fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, revision)
	labels := map[string]string{
		"owner": "helm", "name": name, "status": "deployed",
		"version": strconv.Itoa(revision), "createdAt": strconv.FormatInt(now.Unix(), 10),
	}
	_, err = c.clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace, Labels: labels},
		Type:       corev1.SecretType(helmReleaseSecretType),
		Data:       map[string][]byte{"release": []byte(base64.StdEncoding.EncodeToString(compressed.Bytes()))},
	}, metav1.CreateOptions{FieldManager: "dosh"})
	if err != nil {
		return mapKubeError(err)
	}
	if currentSecret == "" {
		return nil
	}
	current, err := c.clientset.CoreV1().Secrets(namespace).Get(ctx, currentSecret, metav1.GetOptions{})
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

func cloneJSONMap(value map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}
