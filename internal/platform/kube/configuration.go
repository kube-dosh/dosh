package kube

import (
	"context"
	"encoding/base64"
	"sort"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) ListConfigMaps(ctx context.Context, query domain.ListQuery) ([]domain.ConfigMapSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.ConfigMapSummary, 0, len(items.Items))
	for _, item := range items.Items {
		summary := configMapSummary(item)
		if query.Search != "" && !matchesSearch(append([]string{summary.Name, summary.Namespace}, summary.Keys...), query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func configMapSummary(item corev1.ConfigMap) domain.ConfigMapSummary {
	keys := make([]string, 0, len(item.Data)+len(item.BinaryData))
	bytes := 0
	for key, value := range item.Data {
		keys = append(keys, key)
		bytes += len(value)
	}
	for key, value := range item.BinaryData {
		keys = append(keys, key)
		bytes += len(value)
	}
	sort.Strings(keys)
	return domain.ConfigMapSummary{Name: item.Name, Namespace: item.Namespace, Keys: keys, DataBytes: bytes, CreatedAt: item.CreationTimestamp.Time}
}

func (c *Client) GetConfigMap(ctx context.Context, namespace, name string) (domain.ConfigMapDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.ConfigMapDetail{}, mapKubeError(err)
	}
	return domain.ConfigMapDetail{ConfigMapSummary: configMapSummary(*item), Data: item.Data}, nil
}

func (c *Client) ListSecrets(ctx context.Context, query domain.ListQuery) ([]domain.SecretSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	items, err := c.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	out := make([]domain.SecretSummary, 0, len(items.Items))
	for _, item := range items.Items {
		summary := secretSummary(item)
		keyNames := make([]string, 0, len(summary.Keys))
		for _, key := range summary.Keys {
			keyNames = append(keyNames, key.Key)
		}
		if query.Search != "" && !matchesSearch(append([]string{summary.Name, summary.Namespace, summary.Type}, keyNames...), query.Search) {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func secretSummary(item corev1.Secret) domain.SecretSummary {
	keys := make([]domain.SecretKey, 0, len(item.Data))
	for key, value := range item.Data {
		keys = append(keys, domain.SecretKey{Key: key, Size: len(value)})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })
	return domain.SecretSummary{Name: item.Name, Namespace: item.Namespace, Type: string(item.Type), Keys: keys, CreatedAt: item.CreationTimestamp.Time}
}

func (c *Client) RevealSecret(ctx context.Context, namespace, name string) (domain.SecretReveal, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.SecretReveal{}, mapKubeError(err)
	}
	data := make(map[string]string, len(item.Data))
	for key, value := range item.Data {
		data[key] = base64.StdEncoding.EncodeToString(value)
	}
	return domain.SecretReveal{SecretSummary: secretSummary(*item), Data: data}, nil
}
