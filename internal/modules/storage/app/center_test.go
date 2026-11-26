package app

import (
	"context"
	"testing"

	"dosh/internal/domain"
)

type centerTestRepository struct {
	*storageRepository
	pods       []domain.PodSummary
	nodes      []domain.NodeSummary
	namespaces []domain.Namespace
	drivers    []domain.CSIDriverInfo
	releases   map[string][]domain.HelmReleaseSummary
	smoke      domain.StorageSmokeTestResult
	smokeErr   error
	lastSmoke  domain.StorageSmokeTestSpec
}

func (r *centerTestRepository) ListPods(context.Context, domain.ListQuery) ([]domain.PodSummary, error) {
	return r.pods, nil
}
func (r *centerTestRepository) ListNodes(context.Context, domain.ListQuery) ([]domain.NodeSummary, error) {
	return r.nodes, nil
}
func (r *centerTestRepository) ListNamespaces(context.Context, domain.ListQuery) ([]domain.Namespace, error) {
	return r.namespaces, nil
}
func (r *centerTestRepository) ListCSIDrivers(context.Context) ([]domain.CSIDriverInfo, error) {
	return r.drivers, nil
}
func (r *centerTestRepository) ListHelmReleases(_ context.Context, namespace string) ([]domain.HelmReleaseSummary, []string, error) {
	return r.releases[namespace], nil, nil
}
func (r *centerTestRepository) RunStorageSmokeTest(_ context.Context, spec domain.StorageSmokeTestSpec) (domain.StorageSmokeTestResult, error) {
	r.lastSmoke = spec
	return r.smoke, r.smokeErr
}

type centerTestHelmCatalog struct {
	repositories []domain.HelmRepository
	charts       map[string][]domain.HelmChartSummary
	err          error
}

func (c *centerTestHelmCatalog) ListRepositories(context.Context, domain.Actor) ([]domain.HelmRepository, error) {
	return c.repositories, c.err
}
func (c *centerTestHelmCatalog) Charts(_ context.Context, _ domain.Actor, repositoryID, _ string) ([]domain.HelmChartSummary, []string, error) {
	return c.charts[repositoryID], nil, c.err
}

func TestStorageCenterDiscoversCatalogManagedAndAdoptedIntegrations(t *testing.T) {
	t.Parallel()
	repo := &centerTestRepository{
		storageRepository: &storageRepository{
			classes: []domain.StorageClass{
				{Name: "local-path", Provisioner: "rancher.io/local-path", Default: true},
				{Name: "data-standard", Provisioner: "csi.example.test"},
			},
			claims: []domain.PersistentVolumeClaim{{Name: "pending", Namespace: "prod", Status: "Pending", StorageClass: "data-standard"}},
		},
		nodes:   []domain.NodeSummary{{Name: "worker-1", Status: "Ready"}, {Name: "worker-2", Status: "Ready"}},
		pods:    []domain.PodSummary{{Name: "local-path-provisioner-abc", Namespace: "local-path-storage", Status: "Running"}, {Name: "csi-example-controller", Namespace: "kube-system", Status: "Running"}},
		drivers: []domain.CSIDriverInfo{{Name: "csi.example.test"}},
		releases: map[string][]domain.HelmReleaseSummary{
			"local-path-storage": {{Name: "local-path-provisioner", Namespace: "local-path-storage", Chart: "local-path-provisioner", Status: "deployed", Revision: 1}},
		},
	}
	service := New(repo, &storageTestAuthorizer{}, &storageTestAuditor{})
	service.SetHelmCatalog(&centerTestHelmCatalog{
		repositories: []domain.HelmRepository{{ID: "repo-1", Name: "trusted", Enabled: true, Default: true}},
		charts: map[string][]domain.HelmChartSummary{
			"repo-1": {{RepositoryID: "repo-1", RepositoryName: "trusted", Name: "local-path-provisioner", Version: "0.0.30", Digest: "sha256:abc"}},
		},
	})
	center, err := service.StorageCenter(context.Background(), domain.Actor{Username: "admin", Admin: true})
	if err != nil {
		t.Fatalf("StorageCenter() error = %v", err)
	}
	if center.Summary.StorageClassCount != 2 || center.Summary.ManagedIntegrationCount != 1 || center.Summary.AdoptedIntegrationCount != 1 {
		t.Fatalf("summary = %#v", center.Summary)
	}
	if len(center.Catalog) != 2 || center.Catalog[0].Status != domain.StorageCatalogAvailable {
		t.Fatalf("catalog = %#v", center.Catalog)
	}
	if center.Catalog[0].Project != "rancher/local-path-provisioner" {
		t.Fatalf("local path project = %q", center.Catalog[0].Project)
	}
	if len(center.Catalog[0].Provisioners) != 1 || center.Catalog[0].Provisioners[0] != "rancher.io/local-path" {
		t.Fatalf("local path provisioner = %#v", center.Catalog[0].Provisioners)
	}
	if center.Catalog[1].ID != "csi-driver-nfs" {
		t.Fatalf("NFS catalog entry = %#v", center.Catalog[1])
	}
	for _, item := range center.Catalog {
		if item.ID == "nfs-subdir-external-provisioner" {
			t.Fatal("retired NFS Subdir External Provisioner must not be recommended")
		}
	}
	foundManaged, foundAdopted := false, false
	for _, integration := range center.Integrations {
		if integration.Source == domain.StorageIntegrationManaged && integration.ReleaseName == "local-path-provisioner" {
			foundManaged = true
		}
		if integration.Source == domain.StorageIntegrationAdopted && integration.Provisioner == "csi.example.test" {
			foundAdopted = true
		}
	}
	if !foundManaged || !foundAdopted {
		t.Fatalf("integrations = %#v", center.Integrations)
	}
	if len(center.Diagnostics) == 0 {
		t.Fatal("expected diagnostics for pending claim and local-path multi-node limitation")
	}
}

func TestStoragePreflightFailsOnMissingChartAndReleaseConflict(t *testing.T) {
	t.Parallel()
	repo := &centerTestRepository{
		storageRepository: &storageRepository{classes: []domain.StorageClass{{Name: "local-path", Provisioner: "rancher.io/local-path"}}},
		nodes:             []domain.NodeSummary{{Name: "worker-1", Status: "Ready"}},
		releases: map[string][]domain.HelmReleaseSummary{
			"local-path-storage": {{Name: "local-path-provisioner", Namespace: "local-path-storage", Chart: "local-path-provisioner", Status: "deployed", Revision: 1}},
		},
	}
	service := New(repo, &storageTestAuthorizer{}, &storageTestAuditor{})
	service.SetHelmCatalog(&centerTestHelmCatalog{
		repositories: []domain.HelmRepository{{ID: "repo-1", Name: "trusted", Enabled: true}},
		charts:       map[string][]domain.HelmChartSummary{},
	})
	result, err := service.StoragePreflight(context.Background(), domain.Actor{Username: "admin", Admin: true}, domain.StoragePreflightRequest{CatalogID: "local-path-provisioner"})
	if err != nil {
		t.Fatalf("StoragePreflight() error = %v", err)
	}
	if result.Status != domain.StorageCheckFail {
		t.Fatalf("preflight status = %q, checks = %#v", result.Status, result.Checks)
	}
	messages := ""
	for _, check := range result.Checks {
		messages += check.ID + ":" + check.Status + " "
	}
	if !containsSubstring(messages, "chart:fail") || !containsSubstring(messages, "release-conflict:fail") {
		t.Fatalf("checks = %#v", result.Checks)
	}
}

func TestVerifyStorageRunsSmokeTestAndAggregatesChecks(t *testing.T) {
	t.Parallel()
	repo := &centerTestRepository{
		storageRepository: &storageRepository{classDetail: domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "local-path", Provisioner: "rancher.io/local-path", VolumeBinding: "WaitForFirstConsumer"}}},
		pods:              []domain.PodSummary{{Name: "local-path-provisioner-abc", Namespace: "local-path-storage", Status: "Running"}},
		smoke:             domain.StorageSmokeTestResult{Name: "dosh-smoke", Namespace: "prod", StorageClass: "local-path", Bound: true, PodRunning: true, CleanedUp: true, Message: "PVC 已绑定且 Pod 已成功挂载存储。"},
	}
	auditor := &storageTestAuditor{}
	service := New(repo, &storageTestAuthorizer{}, auditor)
	result, err := service.VerifyStorage(context.Background(), domain.Actor{Username: "admin", Admin: true}, domain.StorageVerifyRequest{
		Namespace: "prod", StorageClass: "local-path", SmokeTest: true,
	})
	if err != nil {
		t.Fatalf("VerifyStorage() error = %v", err)
	}
	if result.Status != domain.StorageCheckPass || result.SmokeTest == nil || !result.SmokeTest.Bound {
		t.Fatalf("verification = %#v", result)
	}
	if repo.lastSmoke.Namespace != "prod" || repo.lastSmoke.StorageClass != "local-path" || repo.lastSmoke.Image != defaultStorageSmokeImage {
		t.Fatalf("smoke spec = %#v", repo.lastSmoke)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "verify-storage" || auditor.events[0].Result != "success" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func containsSubstring(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
