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
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	values := "{}"
	if len(selected.Config) > 0 {
		encoded, err := yaml.Marshal(selected.Config)
		if err != nil {
			warnings = append(warnings, "Helm values 无法序列化为 YAML")
		} else {
			values = string(encoded)
		}
	}
	detail := domain.HelmReleaseDetail{
		HelmReleaseSummary: helmReleaseSummary(*selected),
		FirstDeployed:      selected.Info.FirstDeployed,
		LastDeployed:       selected.Info.LastDeployed,
		Notes:              selected.Info.Notes,
		Values:             values,
		Manifest:           selected.Manifest,
		History:            history,
		Warnings:           warnings,
	}
	return detail, nil
}

type decodedHelmRelease struct {
	payload helmReleasePayload
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
		payload, err := decodeHelmReleaseSecret(secret)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("无法解析 Helm Secret %s：%v", secret.Name, err))
			continue
		}
		if payload.Namespace == "" {
			payload.Namespace = namespace
		}
		result = append(result, decodedHelmRelease{payload: payload})
	}
	return result, warnings, nil
}

func decodeHelmReleaseSecret(secret corev1.Secret) (helmReleasePayload, error) {
	raw, ok := secret.Data["release"]
	if !ok {
		return helmReleasePayload{}, fmt.Errorf("release data is missing")
	}
	decoded, err := decodeHelmReleasePayload(raw)
	if err != nil {
		return helmReleasePayload{}, err
	}
	var release helmReleasePayload
	if err := json.Unmarshal(decoded, &release); err != nil {
		return helmReleasePayload{}, fmt.Errorf("decode JSON: %w", err)
	}
	if release.Name == "" || release.Version <= 0 {
		return helmReleasePayload{}, fmt.Errorf("release identity is incomplete")
	}
	return release, nil
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
