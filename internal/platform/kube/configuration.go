package kube

import (
	"context"
	"encoding/base64"
	"fmt"
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

func (c *Client) ListConfigMapsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.ConfigMapSummary], error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	return listPage(ctx, page, query.Search != "", func(options metav1.ListOptions) ([]corev1.ConfigMap, metav1.ListMeta, error) {
		items, err := c.clientset.CoreV1().ConfigMaps(namespace).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return items.Items, items.ListMeta, nil
	}, configMapSummary, func(summary domain.ConfigMapSummary) bool {
		return query.Search == "" || matchesSearch(append([]string{summary.Name, summary.Namespace}, summary.Keys...), query.Search)
	})
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
	return domain.ConfigMapSummary{
		Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Keys: keys, DataBytes: bytes, CreatedAt: item.CreationTimestamp.Time,
	}
}

func (c *Client) GetConfigMap(ctx context.Context, namespace, name string) (domain.ConfigMapDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.ConfigMapDetail{}, mapKubeError(err)
	}
	data := make(map[string]string, len(item.Data))
	for key, value := range item.Data {
		data[key] = value
	}
	return domain.ConfigMapDetail{ConfigMapSummary: configMapSummary(*item), Data: data}, nil
}

func (c *Client) CreateConfigMap(ctx context.Context, spec domain.ConfigMapCreateSpec) (domain.ConfigMapDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Annotations: cloneStringMap(spec.Annotations)},
		Data:       cloneStringMap(spec.Data),
	}
	if _, err := c.clientset.CoreV1().ConfigMaps(spec.Namespace).Create(ctx, item, metav1.CreateOptions{}); err != nil {
		return domain.ConfigMapDetail{}, mapKubeError(err)
	}
	return c.GetConfigMap(ctx, spec.Namespace, spec.Name)
}

func (c *Client) UpdateConfigMap(ctx context.Context, namespace, name string, update domain.ConfigMapUpdateSpec) (domain.ConfigMapDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.ConfigMapDetail{}, mapKubeError(err)
	}
	if update.ResourceVersion != "" && item.ResourceVersion != update.ResourceVersion {
		return domain.ConfigMapDetail{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, item.ResourceVersion)
	}
	item.Data = cloneStringMap(update.Data)
	if update.Annotations != nil {
		item.Annotations = cloneStringMap(update.Annotations)
	}
	if _, err := c.clientset.CoreV1().ConfigMaps(namespace).Update(ctx, item, metav1.UpdateOptions{}); err != nil {
		return domain.ConfigMapDetail{}, mapKubeError(err)
	}
	return c.GetConfigMap(ctx, namespace, name)
}

func (c *Client) DeleteConfigMap(ctx context.Context, namespace, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, options))
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

func (c *Client) ListSecretsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.SecretSummary], error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	namespace := query.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	return listPage(ctx, page, query.Search != "", func(options metav1.ListOptions) ([]corev1.Secret, metav1.ListMeta, error) {
		items, err := c.clientset.CoreV1().Secrets(namespace).List(ctx, options)
		if err != nil {
			return nil, metav1.ListMeta{}, mapKubeError(err)
		}
		return items.Items, items.ListMeta, nil
	}, secretSummary, func(summary domain.SecretSummary) bool {
		keyNames := make([]string, 0, len(summary.Keys))
		for _, key := range summary.Keys {
			keyNames = append(keyNames, key.Key)
		}
		return query.Search == "" || matchesSearch(append([]string{summary.Name, summary.Namespace, summary.Type}, keyNames...), query.Search)
	})
}

func secretSummary(item corev1.Secret) domain.SecretSummary {
	keys := make([]domain.SecretKey, 0, len(item.Data))
	for key, value := range item.Data {
		keys = append(keys, domain.SecretKey{Key: key, Size: len(value)})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })
	return domain.SecretSummary{
		Name: item.Name, Namespace: item.Namespace, ResourceVersion: item.ResourceVersion,
		Type: string(item.Type), Keys: keys, CreatedAt: item.CreationTimestamp.Time,
	}
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

func (c *Client) CreateSecret(ctx context.Context, spec domain.SecretWriteSpec) (domain.SecretSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	data, err := decodeSecretWriteData(spec.Data)
	if err != nil {
		return domain.SecretSummary{}, err
	}
	item := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: spec.Namespace, Annotations: cloneStringMap(spec.Annotations)},
		Type:       corev1.SecretType(spec.Type),
		Data:       data,
	}
	created, err := c.clientset.CoreV1().Secrets(spec.Namespace).Create(ctx, item, metav1.CreateOptions{})
	if err != nil {
		return domain.SecretSummary{}, mapKubeError(err)
	}
	return secretSummary(*created), nil
}

func (c *Client) UpdateSecret(ctx context.Context, namespace, name string, update domain.SecretWriteSpec) (domain.SecretSummary, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.SecretSummary{}, mapKubeError(err)
	}
	if update.ResourceVersion != "" && item.ResourceVersion != update.ResourceVersion {
		return domain.SecretSummary{}, fmt.Errorf("%w: expected %s, got %s", domain.ErrConflict, update.ResourceVersion, item.ResourceVersion)
	}
	data, err := decodeSecretWriteData(update.Data)
	if err != nil {
		return domain.SecretSummary{}, err
	}
	item.Type = corev1.SecretType(update.Type)
	item.Data = data
	if update.Annotations != nil {
		item.Annotations = cloneStringMap(update.Annotations)
	}
	updated, err := c.clientset.CoreV1().Secrets(namespace).Update(ctx, item, metav1.UpdateOptions{})
	if err != nil {
		return domain.SecretSummary{}, mapKubeError(err)
	}
	return secretSummary(*updated), nil
}

func (c *Client) DeleteSecret(ctx context.Context, namespace, name, resourceVersion string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	options := metav1.DeleteOptions{}
	if resourceVersion != "" {
		options.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return mapKubeError(c.clientset.CoreV1().Secrets(namespace).Delete(ctx, name, options))
}

func decodeSecretWriteData(encoded map[string]string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(encoded))
	for key, value := range encoded {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("%w: Secret value %q must be valid Base64", domain.ErrInvalid, key)
		}
		out[key] = decoded
	}
	return out, nil
}
