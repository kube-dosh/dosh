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
	helmRepositoryConfigMapName  = "dosh-helm-repositories"
	helmRepositorySecretName     = "dosh-helm-repository-credentials"
	helmRepositoryCatalogKey     = "repositories.json"
	helmRepositoryCredentialsKey = "credentials.json"
	helmChartOperationsConfigMap = "dosh-helm-chart-operations"
	helmOperationMaxRecords      = 100
)

type helmRepositoryCatalog struct {
	Repositories []domain.HelmRepository `json:"repositories"`
}

func (c *Client) ListHelmRepositories(ctx context.Context) ([]domain.HelmRepository, error) {
	if c.preferencesNamespace == "" {
		return []domain.HelmRepository{}, nil
	}
	if c.clientset == nil {
		return nil, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	catalog, revision, err := c.readHelmRepositoryCatalog(ctx)
	if err != nil {
		return nil, err
	}
	credentials, err := c.readHelmRepositoryCredentials(ctx)
	if err != nil {
		return nil, err
	}
	for index := range catalog.Repositories {
		item := &catalog.Repositories[index]
		item.Revision = revision
		item.Credentials = credentials[item.ID]
		if item.AuthType == "" {
			item.AuthType = inferHelmRepositoryAuthType(item.Credentials)
		}
		item.HasCredentials = helmCredentialsConfigured(item.Credentials)
		item.HasCustomCA = strings.TrimSpace(item.Credentials.CAPEM) != ""
	}
	sort.SliceStable(catalog.Repositories, func(left, right int) bool {
		if catalog.Repositories[left].Default != catalog.Repositories[right].Default {
			return catalog.Repositories[left].Default
		}
		return strings.ToLower(catalog.Repositories[left].Name) < strings.ToLower(catalog.Repositories[right].Name)
	})
	return catalog.Repositories, nil
}

func (c *Client) GetHelmRepository(ctx context.Context, id string) (domain.HelmRepository, error) {
	items, err := c.ListHelmRepositories(ctx)
	if err != nil {
		return domain.HelmRepository{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.HelmRepository{}, fmt.Errorf("%w: Helm repository %q", domain.ErrNotFound, id)
}

func (c *Client) SaveHelmRepository(ctx context.Context, repository domain.HelmRepository) (domain.HelmRepository, error) {
	if c.preferencesNamespace == "" {
		return domain.HelmRepository{}, fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return domain.HelmRepository{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	c.helmStoreMu.Lock()
	defer c.helmStoreMu.Unlock()

	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	expectedRevision := repository.Revision
	updating := expectedRevision != ""
	now := time.Now().UTC()
	repository.Revision = ""
	if repository.AuthType == "" {
		repository.AuthType = inferHelmRepositoryAuthType(repository.Credentials)
	}
	repository.HasCredentials = helmCredentialsConfigured(repository.Credentials)
	repository.HasCustomCA = strings.TrimSpace(repository.Credentials.CAPEM) != ""

	for attempt := 0; attempt < 4; attempt++ {
		catalog, revision, err := c.readHelmRepositoryCatalog(ctx)
		if err != nil {
			return domain.HelmRepository{}, err
		}
		if attempt == 0 && updating && expectedRevision != "" && revision != expectedRevision {
			return domain.HelmRepository{}, fmt.Errorf("%w: expected Helm repository revision %s, got %s", domain.ErrConflict, expectedRevision, revision)
		}
		index := -1
		for i := range catalog.Repositories {
			if catalog.Repositories[i].ID == repository.ID {
				index = i
				break
			}
		}
		if index < 0 && updating {
			return domain.HelmRepository{}, fmt.Errorf("%w: Helm repository %q", domain.ErrNotFound, repository.ID)
		}
		if index >= 0 && !updating {
			return domain.HelmRepository{}, fmt.Errorf("%w: Helm repository %q", domain.ErrAlreadyExists, repository.ID)
		}
		if index < 0 {
			repository.CreatedAt = now
			catalog.Repositories = append(catalog.Repositories, repository)
		} else {
			repository.CreatedAt = catalog.Repositories[index].CreatedAt
			if repository.CreatedAt.IsZero() {
				repository.CreatedAt = now
			}
			catalog.Repositories[index] = repository
		}
		repository.UpdatedAt = now
		for i := range catalog.Repositories {
			if catalog.Repositories[i].ID == repository.ID {
				catalog.Repositories[i].UpdatedAt = now
				repository = catalog.Repositories[i]
			}
		}
		catalog.Repositories = uniqueHelmRepositoryDefaults(catalog.Repositories, repository.ID)

		credentials, err := c.readHelmRepositoryCredentials(ctx)
		if err != nil {
			return domain.HelmRepository{}, err
		}
		if helmCredentialsStored(repository.Credentials) {
			credentials[repository.ID] = repository.Credentials
		} else {
			delete(credentials, repository.ID)
		}
		if err := c.writeHelmRepositoryCredentials(ctx, credentials); err != nil {
			return domain.HelmRepository{}, err
		}
		saved, err := c.writeHelmRepositoryCatalog(ctx, catalog, expectedRevision)
		if err == nil {
			for _, item := range saved.Repositories {
				if item.ID == repository.ID {
					item.Credentials = repository.Credentials
					item.AuthType = repository.AuthType
					item.HasCredentials = helmCredentialsConfigured(repository.Credentials)
					item.HasCustomCA = strings.TrimSpace(repository.Credentials.CAPEM) != ""
					return item, nil
				}
			}
			return domain.HelmRepository{}, fmt.Errorf("%w: saved Helm repository is missing", domain.ErrUnavailable)
		}
		if !apierrors.IsConflict(err) && !isHelmRepositoryConflict(err) {
			return domain.HelmRepository{}, err
		}
		expectedRevision = ""
		updating = true
	}
	return domain.HelmRepository{}, fmt.Errorf("%w: Helm repository update conflict", domain.ErrConflict)
}

func (c *Client) DeleteHelmRepository(ctx context.Context, id, expectedRevision string) error {
	if c.preferencesNamespace == "" {
		return fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	c.helmStoreMu.Lock()
	defer c.helmStoreMu.Unlock()

	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	catalog, revision, err := c.readHelmRepositoryCatalog(ctx)
	if err != nil {
		return err
	}
	if expectedRevision != "" && expectedRevision != revision {
		return fmt.Errorf("%w: expected Helm repository revision %s, got %s", domain.ErrConflict, expectedRevision, revision)
	}
	next := catalog.Repositories[:0]
	found := false
	for _, item := range catalog.Repositories {
		if item.ID == id {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return fmt.Errorf("%w: Helm repository %q", domain.ErrNotFound, id)
	}
	catalog.Repositories = next
	credentials, err := c.readHelmRepositoryCredentials(ctx)
	if err != nil {
		return err
	}
	delete(credentials, id)
	if err := c.writeHelmRepositoryCredentials(ctx, credentials); err != nil {
		return err
	}
	_, err = c.writeHelmRepositoryCatalog(ctx, catalog, expectedRevision)
	return err
}

func (c *Client) SaveHelmChartOperation(ctx context.Context, operation domain.HelmChartOperation) error {
	if c.preferencesNamespace == "" {
		return fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	if strings.TrimSpace(operation.ID) == "" {
		return fmt.Errorf("%w: Helm chart operation id is required", domain.ErrInvalid)
	}
	c.helmOperationMu.Lock()
	defer c.helmOperationMu.Unlock()
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	encoded, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("%w: encode Helm chart operation: %v", domain.ErrInvalid, err)
	}
	client := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace)
	for attempt := 0; attempt < 3; attempt++ {
		current, err := client.Get(ctx, helmChartOperationsConfigMap, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, createErr := client.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: helmChartOperationsConfigMap, Namespace: c.preferencesNamespace,
					Labels: helmOperationLabels(),
				},
				Data: map[string]string{operation.ID + ".json": string(encoded)},
			}, metav1.CreateOptions{FieldManager: "dosh"})
			return mapKubeError(createErr)
		}
		if err != nil {
			return mapKubeError(err)
		}
		if current.Data == nil {
			current.Data = map[string]string{}
		}
		current.Data[operation.ID+".json"] = string(encoded)
		current.Data = pruneHelmOperations(current.Data, helmOperationMaxRecords)
		if current.Labels == nil {
			current.Labels = map[string]string{}
		}
		for key, value := range helmOperationLabels() {
			current.Labels[key] = value
		}
		_, err = client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
		if err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			return mapKubeError(err)
		}
	}
	return fmt.Errorf("%w: Helm chart operation update conflict", domain.ErrConflict)
}

func (c *Client) GetHelmChartOperation(ctx context.Context, id string) (domain.HelmChartOperation, error) {
	if c.preferencesNamespace == "" {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: Helm chart operation %q", domain.ErrNotFound, id)
	}
	if c.clientset == nil {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	configMap, err := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace).Get(ctx, helmChartOperationsConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: Helm chart operation %q", domain.ErrNotFound, id)
	}
	if err != nil {
		return domain.HelmChartOperation{}, mapKubeError(err)
	}
	raw := strings.TrimSpace(configMap.Data[id+".json"])
	if raw == "" {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: Helm chart operation %q", domain.ErrNotFound, id)
	}
	var operation domain.HelmChartOperation
	if err := json.Unmarshal([]byte(raw), &operation); err != nil {
		return domain.HelmChartOperation{}, fmt.Errorf("%w: decode Helm chart operation: %v", domain.ErrUnavailable, err)
	}
	return operation, nil
}

func (c *Client) readHelmRepositoryCatalog(ctx context.Context) (helmRepositoryCatalog, string, error) {
	configMap, err := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace).Get(ctx, helmRepositoryConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return helmRepositoryCatalog{Repositories: []domain.HelmRepository{}}, "", nil
	}
	if err != nil {
		return helmRepositoryCatalog{}, "", mapKubeError(err)
	}
	catalog := helmRepositoryCatalog{Repositories: []domain.HelmRepository{}}
	if raw := strings.TrimSpace(configMap.Data[helmRepositoryCatalogKey]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
			return helmRepositoryCatalog{}, "", fmt.Errorf("%w: decode Helm repository catalog: %v", domain.ErrUnavailable, err)
		}
	}
	if catalog.Repositories == nil {
		catalog.Repositories = []domain.HelmRepository{}
	}
	return catalog, configMap.ResourceVersion, nil
}

func (c *Client) writeHelmRepositoryCatalog(ctx context.Context, catalog helmRepositoryCatalog, expectedRevision string) (helmRepositoryCatalog, error) {
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return helmRepositoryCatalog{}, fmt.Errorf("%w: encode Helm repository catalog: %v", domain.ErrInvalid, err)
	}
	client := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace)
	current, err := client.Get(ctx, helmRepositoryConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if expectedRevision != "" {
			return helmRepositoryCatalog{}, fmt.Errorf("%w: Helm repository catalog was removed", domain.ErrConflict)
		}
		created, createErr := client.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: helmRepositoryConfigMapName, Namespace: c.preferencesNamespace,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "helm-repositories"},
			},
			Data: map[string]string{helmRepositoryCatalogKey: string(encoded)},
		}, metav1.CreateOptions{FieldManager: "dosh"})
		if createErr != nil {
			return helmRepositoryCatalog{}, mapKubeError(createErr)
		}
		catalog.Repositories = attachHelmRepositoryRevision(catalog.Repositories, created.ResourceVersion)
		return catalog, nil
	}
	if err != nil {
		return helmRepositoryCatalog{}, mapKubeError(err)
	}
	if expectedRevision != "" && current.ResourceVersion != expectedRevision {
		return helmRepositoryCatalog{}, fmt.Errorf("%w: expected Helm repository revision %s, got %s", domain.ErrConflict, expectedRevision, current.ResourceVersion)
	}
	if current.Data == nil {
		current.Data = map[string]string{}
	}
	current.Data[helmRepositoryCatalogKey] = string(encoded)
	if current.Labels == nil {
		current.Labels = map[string]string{}
	}
	current.Labels["app.kubernetes.io/managed-by"] = "dosh"
	current.Labels["dosh.io/component"] = "helm-repositories"
	updated, err := client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
	if err != nil {
		return helmRepositoryCatalog{}, mapKubeError(err)
	}
	catalog.Repositories = attachHelmRepositoryRevision(catalog.Repositories, updated.ResourceVersion)
	return catalog, nil
}

func (c *Client) readHelmRepositoryCredentials(ctx context.Context) (map[string]domain.HelmRepositoryCredentials, error) {
	secret, err := c.clientset.CoreV1().Secrets(c.preferencesNamespace).Get(ctx, helmRepositorySecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]domain.HelmRepositoryCredentials{}, nil
	}
	if err != nil {
		return nil, mapKubeError(err)
	}
	result := map[string]domain.HelmRepositoryCredentials{}
	if raw := strings.TrimSpace(string(secret.Data[helmRepositoryCredentialsKey])); raw != "" {
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("%w: decode Helm repository credentials: %v", domain.ErrUnavailable, err)
		}
	}
	if result == nil {
		result = map[string]domain.HelmRepositoryCredentials{}
	}
	return result, nil
}

func (c *Client) writeHelmRepositoryCredentials(ctx context.Context, credentials map[string]domain.HelmRepositoryCredentials) error {
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("%w: encode Helm repository credentials: %v", domain.ErrInvalid, err)
	}
	client := c.clientset.CoreV1().Secrets(c.preferencesNamespace)
	current, err := client.Get(ctx, helmRepositorySecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := client.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: helmRepositorySecretName, Namespace: c.preferencesNamespace,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "helm-repositories"},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{helmRepositoryCredentialsKey: encoded},
		}, metav1.CreateOptions{FieldManager: "dosh"})
		return mapKubeError(createErr)
	}
	if err != nil {
		return mapKubeError(err)
	}
	if current.Data == nil {
		current.Data = map[string][]byte{}
	}
	current.Data[helmRepositoryCredentialsKey] = encoded
	if current.Labels == nil {
		current.Labels = map[string]string{}
	}
	current.Labels["app.kubernetes.io/managed-by"] = "dosh"
	current.Labels["dosh.io/component"] = "helm-repositories"
	_, err = client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
	return mapKubeError(err)
}

func attachHelmRepositoryRevision(items []domain.HelmRepository, revision string) []domain.HelmRepository {
	for index := range items {
		items[index].Revision = revision
	}
	return items
}

func uniqueHelmRepositoryDefaults(items []domain.HelmRepository, selectedID string) []domain.HelmRepository {
	for index := range items {
		if items[index].ID != selectedID {
			items[index].Default = false
		}
	}
	return items
}

func helmCredentialsStored(credentials domain.HelmRepositoryCredentials) bool {
	return strings.TrimSpace(credentials.Username) != "" || strings.TrimSpace(credentials.Password) != "" || strings.TrimSpace(credentials.Token) != "" || strings.TrimSpace(credentials.CAPEM) != ""
}

func helmCredentialsConfigured(credentials domain.HelmRepositoryCredentials) bool {
	return strings.TrimSpace(credentials.Username) != "" || strings.TrimSpace(credentials.Password) != "" || strings.TrimSpace(credentials.Token) != ""
}

func inferHelmRepositoryAuthType(credentials domain.HelmRepositoryCredentials) string {
	switch {
	case strings.TrimSpace(credentials.Token) != "":
		return domain.HelmRepositoryAuthBearer
	case strings.TrimSpace(credentials.Username) != "" || strings.TrimSpace(credentials.Password) != "":
		return domain.HelmRepositoryAuthBasic
	default:
		return domain.HelmRepositoryAuthAnonymous
	}
}

func isHelmRepositoryConflict(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "revision")
}

func helmOperationLabels() map[string]string {
	return map[string]string{"app.kubernetes.io/managed-by": "dosh", "dosh.io/component": "helm-chart-operations"}
}

func pruneHelmOperations(data map[string]string, limit int) map[string]string {
	if len(data) <= limit {
		return data
	}
	type record struct {
		key       string
		startedAt time.Time
	}
	records := make([]record, 0, len(data))
	for key, raw := range data {
		var operation domain.HelmChartOperation
		_ = json.Unmarshal([]byte(raw), &operation)
		records = append(records, record{key: key, startedAt: operation.StartedAt})
	}
	sort.Slice(records, func(left, right int) bool { return records[left].startedAt.Before(records[right].startedAt) })
	for _, item := range records[:len(records)-limit] {
		delete(data, item.key)
	}
	return data
}
