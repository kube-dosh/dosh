package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	registryCatalogConfigMapName  = "dosh-container-registries"
	registryCredentialsSecretName = "dosh-container-registry-credentials"
	registryCatalogKey            = "registries.json"
	registryCredentialsKey        = "credentials.json"
)

type registryCatalog struct {
	Registries []domain.ContainerRegistry `json:"registries"`
}

func (c *Client) ListContainerRegistries(ctx context.Context) ([]domain.ContainerRegistry, error) {
	if c.preferencesNamespace == "" {
		return []domain.ContainerRegistry{}, nil
	}
	if c.clientset == nil {
		return nil, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	catalog, revision, err := c.readRegistryCatalog(ctx)
	if err != nil {
		return nil, err
	}
	credentials, err := c.readRegistryCredentials(ctx)
	if err != nil {
		return nil, err
	}
	for index := range catalog.Registries {
		item := &catalog.Registries[index]
		item.Revision = revision
		item.Credentials = credentials[item.ID]
		item.HasCredentials = credentialsConfigured(item.Credentials)
		item.HasCustomCA = strings.TrimSpace(item.Credentials.CAPEM) != ""
	}
	sort.SliceStable(catalog.Registries, func(left, right int) bool {
		if catalog.Registries[left].Default != catalog.Registries[right].Default {
			return catalog.Registries[left].Default
		}
		return strings.ToLower(catalog.Registries[left].Name) < strings.ToLower(catalog.Registries[right].Name)
	})
	return catalog.Registries, nil
}

func (c *Client) GetContainerRegistry(ctx context.Context, id string) (domain.ContainerRegistry, error) {
	items, err := c.ListContainerRegistries(ctx)
	if err != nil {
		return domain.ContainerRegistry{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.ContainerRegistry{}, fmt.Errorf("%w: container registry %q", domain.ErrNotFound, id)
}

func (c *Client) SaveContainerRegistry(ctx context.Context, registry domain.ContainerRegistry) (domain.ContainerRegistry, error) {
	if c.preferencesNamespace == "" {
		return domain.ContainerRegistry{}, fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return domain.ContainerRegistry{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	c.registryStoreMu.Lock()
	defer c.registryStoreMu.Unlock()

	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	expectedRevision := registry.Revision
	updating := expectedRevision != ""
	now := time.Now().UTC()
	registry.Revision = ""
	registry.HasCredentials = credentialsConfigured(registry.Credentials)
	registry.HasCustomCA = strings.TrimSpace(registry.Credentials.CAPEM) != ""

	for attempt := 0; attempt < 3; attempt++ {
		catalog, revision, err := c.readRegistryCatalog(ctx)
		if err != nil {
			return domain.ContainerRegistry{}, err
		}
		if attempt == 0 && updating && expectedRevision != "" && revision != expectedRevision {
			return domain.ContainerRegistry{}, fmt.Errorf("%w: expected registry revision %s, got %s", domain.ErrConflict, expectedRevision, revision)
		}
		index := -1
		for i := range catalog.Registries {
			if catalog.Registries[i].ID == registry.ID {
				index = i
				break
			}
		}
		if index < 0 && updating {
			return domain.ContainerRegistry{}, fmt.Errorf("%w: container registry %q", domain.ErrNotFound, registry.ID)
		}
		if index >= 0 && !updating {
			return domain.ContainerRegistry{}, fmt.Errorf("%w: container registry %q", domain.ErrAlreadyExists, registry.ID)
		}
		if index < 0 {
			registry.CreatedAt = now
			catalog.Registries = append(catalog.Registries, registry)
		} else {
			registry.CreatedAt = catalog.Registries[index].CreatedAt
			if registry.CreatedAt.IsZero() {
				registry.CreatedAt = now
			}
			catalog.Registries[index] = registry
		}
		registry.UpdatedAt = now
		for i := range catalog.Registries {
			if catalog.Registries[i].ID == registry.ID {
				catalog.Registries[i].UpdatedAt = now
				registry = catalog.Registries[i]
			}
		}
		catalog.Registries = uniqueRegistryDefaults(catalog.Registries, registry.ID)

		credentials, err := c.readRegistryCredentials(ctx)
		if err != nil {
			return domain.ContainerRegistry{}, err
		}
		if registryCredentialsStored(registry.Credentials) {
			credentials[registry.ID] = registry.Credentials
		} else {
			delete(credentials, registry.ID)
		}
		if err := c.writeRegistryCredentials(ctx, credentials); err != nil {
			return domain.ContainerRegistry{}, err
		}
		savedCatalog, err := c.writeRegistryCatalog(ctx, catalog, expectedRevision)
		if err == nil {
			for _, item := range savedCatalog.Registries {
				if item.ID == registry.ID {
					item.Credentials = registry.Credentials
					item.HasCredentials = credentialsConfigured(registry.Credentials)
					item.HasCustomCA = strings.TrimSpace(registry.Credentials.CAPEM) != ""
					return item, nil
				}
			}
			return domain.ContainerRegistry{}, fmt.Errorf("%w: saved container registry is missing", domain.ErrUnavailable)
		}
		if !apierrors.IsConflict(err) && !isRegistryConflict(err) {
			return domain.ContainerRegistry{}, err
		}
		expectedRevision = ""
		updating = true
	}
	return domain.ContainerRegistry{}, fmt.Errorf("%w: container registry update conflict", domain.ErrConflict)
}

func (c *Client) DeleteContainerRegistry(ctx context.Context, id, expectedRevision string) error {
	if c.preferencesNamespace == "" {
		return fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	c.registryStoreMu.Lock()
	defer c.registryStoreMu.Unlock()

	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	catalog, revision, err := c.readRegistryCatalog(ctx)
	if err != nil {
		return err
	}
	if expectedRevision != "" && expectedRevision != revision {
		return fmt.Errorf("%w: expected registry revision %s, got %s", domain.ErrConflict, expectedRevision, revision)
	}
	next := catalog.Registries[:0]
	found := false
	for _, item := range catalog.Registries {
		if item.ID == id {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return fmt.Errorf("%w: container registry %q", domain.ErrNotFound, id)
	}
	catalog.Registries = next
	credentials, err := c.readRegistryCredentials(ctx)
	if err != nil {
		return err
	}
	delete(credentials, id)
	if err := c.writeRegistryCredentials(ctx, credentials); err != nil {
		return err
	}
	_, err = c.writeRegistryCatalog(ctx, catalog, expectedRevision)
	return err
}

func (c *Client) readRegistryCatalog(ctx context.Context) (registryCatalog, string, error) {
	configMap, err := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace).Get(ctx, registryCatalogConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return registryCatalog{Registries: []domain.ContainerRegistry{}}, "", nil
	}
	if err != nil {
		return registryCatalog{}, "", mapKubeError(err)
	}
	catalog := registryCatalog{Registries: []domain.ContainerRegistry{}}
	if raw := strings.TrimSpace(configMap.Data[registryCatalogKey]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
			return registryCatalog{}, "", fmt.Errorf("%w: decode container registry catalog: %v", domain.ErrUnavailable, err)
		}
	}
	if catalog.Registries == nil {
		catalog.Registries = []domain.ContainerRegistry{}
	}
	return catalog, configMap.ResourceVersion, nil
}

func (c *Client) writeRegistryCatalog(ctx context.Context, catalog registryCatalog, expectedRevision string) (registryCatalog, error) {
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return registryCatalog{}, fmt.Errorf("%w: encode container registry catalog: %v", domain.ErrInvalid, err)
	}
	client := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace)
	current, err := client.Get(ctx, registryCatalogConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if expectedRevision != "" {
			return registryCatalog{}, fmt.Errorf("%w: container registry catalog was removed", domain.ErrConflict)
		}
		created, createErr := client.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: registryCatalogConfigMapName, Namespace: c.preferencesNamespace,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "container-registries"},
			},
			Data: map[string]string{registryCatalogKey: string(encoded)},
		}, metav1.CreateOptions{FieldManager: "dosh"})
		if createErr != nil {
			return registryCatalog{}, mapKubeError(createErr)
		}
		catalog.Registries = attachRegistryRevision(catalog.Registries, created.ResourceVersion)
		return catalog, nil
	}
	if err != nil {
		return registryCatalog{}, mapKubeError(err)
	}
	if expectedRevision != "" && current.ResourceVersion != expectedRevision {
		return registryCatalog{}, fmt.Errorf("%w: expected registry revision %s, got %s", domain.ErrConflict, expectedRevision, current.ResourceVersion)
	}
	if current.Data == nil {
		current.Data = map[string]string{}
	}
	current.Data[registryCatalogKey] = string(encoded)
	if current.Labels == nil {
		current.Labels = map[string]string{}
	}
	current.Labels["app.kubernetes.io/managed-by"] = "dosh"
	current.Labels["dosh.io/component"] = "container-registries"
	updated, err := client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
	if err != nil {
		return registryCatalog{}, mapKubeError(err)
	}
	catalog.Registries = attachRegistryRevision(catalog.Registries, updated.ResourceVersion)
	return catalog, nil
}

func (c *Client) readRegistryCredentials(ctx context.Context) (map[string]domain.ContainerRegistryCredentials, error) {
	secret, err := c.clientset.CoreV1().Secrets(c.preferencesNamespace).Get(ctx, registryCredentialsSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]domain.ContainerRegistryCredentials{}, nil
	}
	if err != nil {
		return nil, mapKubeError(err)
	}
	credentials := map[string]domain.ContainerRegistryCredentials{}
	if raw := secret.Data[registryCredentialsKey]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &credentials); err != nil {
			return nil, fmt.Errorf("%w: decode container registry credentials: %v", domain.ErrUnavailable, err)
		}
	}
	return credentials, nil
}

func (c *Client) writeRegistryCredentials(ctx context.Context, credentials map[string]domain.ContainerRegistryCredentials) error {
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("%w: encode container registry credentials: %v", domain.ErrInvalid, err)
	}
	client := c.clientset.CoreV1().Secrets(c.preferencesNamespace)
	current, err := client.Get(ctx, registryCredentialsSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := client.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: registryCredentialsSecretName, Namespace: c.preferencesNamespace,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "container-registry-credentials"},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{registryCredentialsKey: encoded},
		}, metav1.CreateOptions{FieldManager: "dosh"})
		return mapKubeError(createErr)
	}
	if err != nil {
		return mapKubeError(err)
	}
	if current.Data == nil {
		current.Data = map[string][]byte{}
	}
	current.Data[registryCredentialsKey] = encoded
	if current.Labels == nil {
		current.Labels = map[string]string{}
	}
	current.Labels["app.kubernetes.io/managed-by"] = "dosh"
	current.Labels["dosh.io/component"] = "container-registry-credentials"
	_, err = client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
	return mapKubeError(err)
}

func credentialsConfigured(credentials domain.ContainerRegistryCredentials) bool {
	return strings.TrimSpace(credentials.Username) != "" || strings.TrimSpace(credentials.Password) != "" || strings.TrimSpace(credentials.Token) != ""
}

func registryCredentialsStored(credentials domain.ContainerRegistryCredentials) bool {
	return credentialsConfigured(credentials) || strings.TrimSpace(credentials.CAPEM) != ""
}

func uniqueRegistryDefaults(items []domain.ContainerRegistry, selectedID string) []domain.ContainerRegistry {
	hasDefault := false
	for _, item := range items {
		if item.ID == selectedID && item.Default {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return items
	}
	for index := range items {
		items[index].Default = items[index].ID == selectedID
	}
	return items
}

func attachRegistryRevision(items []domain.ContainerRegistry, revision string) []domain.ContainerRegistry {
	for index := range items {
		items[index].Revision = revision
	}
	return items
}

func isRegistryConflict(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "conflict")
}
