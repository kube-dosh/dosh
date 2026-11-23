package kube

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestHelmRepositoryStoreSeparatesCredentialsAndTracksAuthType(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	saved, err := client.SaveHelmRepository(context.Background(), domain.HelmRepository{
		ID: "repo-1", Name: "Private", URL: "https://charts.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthBasic,
		Credentials: domain.HelmRepositoryCredentials{
			Username: "robot", Password: "secret", CAPEM: "ca-data",
		},
		Default: true,
	})
	if err != nil {
		t.Fatalf("SaveHelmRepository() error = %v", err)
	}
	if saved.AuthType != domain.HelmRepositoryAuthBasic || !saved.HasCredentials || !saved.HasCustomCA || saved.Credentials.Password != "secret" {
		t.Fatalf("SaveHelmRepository() = %#v", saved)
	}

	items, err := client.ListHelmRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListHelmRepositories() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListHelmRepositories() count = %d", len(items))
	}
	item := items[0]
	if item.AuthType != domain.HelmRepositoryAuthBasic || !item.HasCredentials || !item.HasCustomCA {
		t.Fatalf("ListHelmRepositories() flags = %#v", item)
	}
	if item.Credentials.Username != "robot" || item.Credentials.Password != "secret" || item.Credentials.CAPEM != "ca-data" {
		t.Fatalf("ListHelmRepositories() credentials = %#v", item.Credentials)
	}
}

func TestHelmRepositoryStoreEnforcesSingleDefaultAndRevision(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	first, err := client.SaveHelmRepository(context.Background(), domain.HelmRepository{ID: "repo-1", Name: "First", URL: "https://one.example.com", Enabled: true, Default: true, AuthType: domain.HelmRepositoryAuthAnonymous})
	if err != nil {
		t.Fatalf("SaveHelmRepository(first) error = %v", err)
	}
	_, err = client.SaveHelmRepository(context.Background(), domain.HelmRepository{ID: "repo-2", Name: "Second", URL: "https://two.example.com", Enabled: true, Default: true, AuthType: domain.HelmRepositoryAuthAnonymous})
	if err != nil {
		t.Fatalf("SaveHelmRepository(second) error = %v", err)
	}
	items, err := client.ListHelmRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListHelmRepositories() error = %v", err)
	}
	for _, item := range items {
		if item.ID == first.ID && item.Default {
			t.Fatalf("first repository remained default: %#v", items)
		}
	}
}

func TestAttachHelmRepositoryRevision(t *testing.T) {
	t.Parallel()
	items := attachHelmRepositoryRevision([]domain.HelmRepository{{ID: "repo-1"}, {ID: "repo-2"}}, "42")
	for _, item := range items {
		if item.Revision != "42" {
			t.Fatalf("attachHelmRepositoryRevision() = %#v", items)
		}
	}
}

func TestHelmRepositoryStoreReportsAnonymousForCustomCAOnly(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	saved, err := client.SaveHelmRepository(context.Background(), domain.HelmRepository{
		ID: "repo-1", Name: "Internal TLS", URL: "https://charts.internal.example.com", Enabled: true,
		AuthType:    domain.HelmRepositoryAuthAnonymous,
		Credentials: domain.HelmRepositoryCredentials{CAPEM: "ca-data"},
	})
	if err != nil {
		t.Fatalf("SaveHelmRepository() error = %v", err)
	}
	if saved.HasCredentials || !saved.HasCustomCA || saved.AuthType != domain.HelmRepositoryAuthAnonymous {
		t.Fatalf("SaveHelmRepository() flags = %#v", saved)
	}
}

func TestHelmOperationLockIsSharedAndIdempotent(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	first := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	second := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	if err := first.AcquireHelmOperationLock(ctx, "default", "demo", "operation-a", time.Minute); err != nil {
		t.Fatalf("first AcquireHelmOperationLock() error = %v", err)
	}
	if err := second.AcquireHelmOperationLock(ctx, "default", "demo", "operation-b", time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second AcquireHelmOperationLock() error = %v", err)
	}
	if err := second.ReleaseHelmOperationLock(ctx, "default", "demo", "operation-a"); err != nil {
		t.Fatalf("ReleaseHelmOperationLock() error = %v", err)
	}
	if err := second.AcquireHelmOperationLock(ctx, "default", "demo", "operation-b", time.Minute); err != nil {
		t.Fatalf("AcquireHelmOperationLock() after release error = %v", err)
	}
}

func TestHelmOperationLocksAreIsolatedPerRelease(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	first := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	second := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	if err := first.AcquireHelmOperationLock(ctx, "default", "demo", "operation-a", time.Minute); err != nil {
		t.Fatalf("first release lock error = %v", err)
	}
	if err := second.AcquireHelmOperationLock(ctx, "default", "api", "operation-b", time.Minute); err != nil {
		t.Fatalf("second release lock was blocked by unrelated release: %v", err)
	}
	configMaps, err := clientset.CoreV1().ConfigMaps("dosh").List(ctx, metav1.ListOptions{LabelSelector: "dosh.io/component=helm-operation-locks"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(configMaps.Items) != 2 {
		t.Fatalf("isolated lock objects = %d", len(configMaps.Items))
	}
	for _, item := range configMaps.Items {
		if item.Name == "dosh-helm-operation-locks" {
			t.Fatalf("shared lock ConfigMap was created")
		}
	}
}

func TestReleaseHelmOperationLockRemovesIdleConfigMap(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	if err := client.AcquireHelmOperationLock(ctx, "default", "demo", "operation-a", time.Minute); err != nil {
		t.Fatalf("AcquireHelmOperationLock() error = %v", err)
	}
	if err := client.ReleaseHelmOperationLock(ctx, "default", "demo", "operation-a"); err != nil {
		t.Fatalf("ReleaseHelmOperationLock() error = %v", err)
	}
	configMaps, err := clientset.CoreV1().ConfigMaps("dosh").List(ctx, metav1.ListOptions{LabelSelector: "dosh.io/component=helm-operation-locks"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(configMaps.Items) != 0 {
		t.Fatalf("released lock objects = %d, want 0", len(configMaps.Items))
	}
}

func TestCleanupHelmOperationLocksRemovesIdleAndExpiredOnly(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	now := time.Now().UTC()
	records := map[string]helmOperationLockRecord{
		"idle":    {},
		"expired": {OperationID: "old", ExpiresAt: now.Add(-time.Minute)},
		"active":  {OperationID: "live", ExpiresAt: now.Add(time.Minute)},
	}
	for key, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal lock record: %v", err)
		}
		_, err = clientset.CoreV1().ConfigMaps("dosh").Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: helmOperationLockConfigMapName(key), Namespace: "dosh", Labels: helmOperationLockLabels()},
			Data:       map[string]string{helmOperationLockDataKey: string(encoded)},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create lock ConfigMap: %v", err)
		}
	}
	deleted, err := client.CleanupHelmOperationLocks(ctx)
	if err != nil {
		t.Fatalf("CleanupHelmOperationLocks() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("CleanupHelmOperationLocks() deleted = %d, want 2", deleted)
	}
	configMaps, err := clientset.CoreV1().ConfigMaps("dosh").List(ctx, metav1.ListOptions{LabelSelector: "dosh.io/component=helm-operation-locks"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(configMaps.Items) != 1 || configMaps.Items[0].Name != helmOperationLockConfigMapName("active") {
		t.Fatalf("remaining lock objects = %#v", configMaps.Items)
	}
}

func TestHelmChartOperationStoreUsesDedicatedConfigMaps(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	first := domain.HelmChartOperation{ID: "operation-a", Type: "install", Status: domain.HelmOperationRunning, Namespace: "apps", ReleaseName: "web", StartedAt: time.Now().UTC()}
	second := domain.HelmChartOperation{ID: "operation-b", Type: "upgrade", Status: domain.HelmOperationSuccess, Namespace: "apps", ReleaseName: "api", StartedAt: time.Now().UTC().Add(time.Second)}
	if err := client.SaveHelmChartOperation(ctx, first); err != nil {
		t.Fatalf("SaveHelmChartOperation(first) error = %v", err)
	}
	if err := client.SaveHelmChartOperation(ctx, second); err != nil {
		t.Fatalf("SaveHelmChartOperation(second) error = %v", err)
	}
	if _, err := clientset.CoreV1().ConfigMaps("dosh").Get(ctx, helmChartOperationsConfigMap, metav1.GetOptions{}); err == nil {
		t.Fatalf("legacy operation ConfigMap was created")
	}
	firstConfigMap, err := clientset.CoreV1().ConfigMaps("dosh").Get(ctx, helmOperationConfigMapName(first.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("dedicated operation ConfigMap error = %v", err)
	}
	if _, ok := firstConfigMap.Data[helmOperationDataKey]; !ok {
		t.Fatalf("dedicated operation ConfigMap data = %#v", firstConfigMap.Data)
	}
	first.Status = domain.HelmOperationSuccess
	first.FinishedAt = time.Now().UTC()
	if err := client.SaveHelmChartOperation(ctx, first); err != nil {
		t.Fatalf("SaveHelmChartOperation(update) error = %v", err)
	}
	items, err := client.ListHelmChartOperations(ctx, domain.HelmOperationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListHelmChartOperations() error = %v", err)
	}
	if len(items) != 2 || items[0].ID != second.ID || items[1].Status != domain.HelmOperationSuccess {
		t.Fatalf("ListHelmChartOperations() = %#v", items)
	}
}

func TestHelmChartOperationStoreReadsLegacyAggregate(t *testing.T) {
	t.Parallel()
	operation := domain.HelmChartOperation{ID: "legacy", Type: "rollback", Status: domain.HelmOperationFailed, Namespace: "apps", ReleaseName: "web", StartedAt: time.Now().UTC()}
	encoded, err := json.Marshal(operation)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	clientset := k8sfake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: helmChartOperationsConfigMap, Namespace: "dosh"},
		Data:       map[string]string{"legacy.json": string(encoded)},
	})
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	items, err := client.ListHelmChartOperations(context.Background(), domain.HelmOperationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListHelmChartOperations() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != operation.ID {
		t.Fatalf("ListHelmChartOperations() = %#v", items)
	}
}

func TestHelmChartOperationStorePreservesCancellationAndTerminalState(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	operation := domain.HelmChartOperation{ID: "operation-a", Type: "install", Status: domain.HelmOperationRunning, Namespace: "apps", ReleaseName: "web", StartedAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC()}
	if err := client.SaveHelmChartOperation(ctx, operation); err != nil {
		t.Fatalf("SaveHelmChartOperation(initial) error = %v", err)
	}
	operation.CancelRequested = true
	if err := client.SaveHelmChartOperation(ctx, operation); err != nil {
		t.Fatalf("SaveHelmChartOperation(cancel) error = %v", err)
	}
	operation.CancelRequested = false
	operation.HeartbeatAt = time.Now().UTC()
	if err := client.SaveHelmChartOperation(ctx, operation); err != nil {
		t.Fatalf("SaveHelmChartOperation(progress) error = %v", err)
	}
	stored, err := client.GetHelmChartOperation(ctx, operation.ID)
	if err != nil || !stored.CancelRequested {
		t.Fatalf("stored cancellation = %#v, err = %v", stored, err)
	}
	stored.Status = domain.HelmOperationSuccess
	stored.FinishedAt = time.Now().UTC()
	if err := client.SaveHelmChartOperation(ctx, stored); err != nil {
		t.Fatalf("SaveHelmChartOperation(terminal) error = %v", err)
	}
	stale := stored
	stale.Status = domain.HelmOperationRunning
	stale.FinishedAt = time.Time{}
	if err := client.SaveHelmChartOperation(ctx, stale); err != nil {
		t.Fatalf("SaveHelmChartOperation(stale progress) error = %v", err)
	}
	stored, err = client.GetHelmChartOperation(ctx, operation.ID)
	if err != nil || stored.Status != domain.HelmOperationSuccess {
		t.Fatalf("terminal state was overwritten = %#v, err = %v", stored, err)
	}
}
