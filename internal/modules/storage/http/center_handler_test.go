package storagehttp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"dosh/internal/domain"
	storageapp "dosh/internal/modules/storage/app"

	"github.com/go-chi/chi/v5"
)

type centerHTTPRepository struct {
	*httpStorageRepository
	pods     []domain.PodSummary
	nodes    []domain.NodeSummary
	drivers  []domain.CSIDriverInfo
	releases []domain.HelmReleaseSummary
	smoke    domain.StorageSmokeTestResult
}

func (r *centerHTTPRepository) ListPods(context.Context, domain.ListQuery) ([]domain.PodSummary, error) {
	return r.pods, nil
}
func (r *centerHTTPRepository) ListNodes(context.Context, domain.ListQuery) ([]domain.NodeSummary, error) {
	return r.nodes, nil
}
func (r *centerHTTPRepository) ListNamespaces(context.Context, domain.ListQuery) ([]domain.Namespace, error) {
	return []domain.Namespace{{Name: "local-path-storage"}}, nil
}
func (r *centerHTTPRepository) ListCSIDrivers(context.Context) ([]domain.CSIDriverInfo, error) {
	return r.drivers, nil
}
func (r *centerHTTPRepository) ListHelmReleases(context.Context, string) ([]domain.HelmReleaseSummary, []string, error) {
	return r.releases, nil, nil
}
func (r *centerHTTPRepository) RunStorageSmokeTest(context.Context, domain.StorageSmokeTestSpec) (domain.StorageSmokeTestResult, error) {
	return r.smoke, nil
}

type centerHTTPHelmCatalog struct{}

func (centerHTTPHelmCatalog) ListRepositories(context.Context, domain.Actor) ([]domain.HelmRepository, error) {
	return []domain.HelmRepository{{ID: "repo-1", Name: "trusted", Enabled: true, Default: true}}, nil
}
func (centerHTTPHelmCatalog) Charts(context.Context, domain.Actor, string, string) ([]domain.HelmChartSummary, []string, error) {
	return []domain.HelmChartSummary{{RepositoryID: "repo-1", RepositoryName: "trusted", Name: "local-path-provisioner", Version: "0.0.30", Digest: "sha256:abc"}}, nil, nil
}

func centerHTTPRouter(repo *centerHTTPRepository) http.Handler {
	service := storageapp.New(repo, storageHTTPAuthorizer{}, storageHTTPAuditor{})
	service.SetHelmCatalog(centerHTTPHelmCatalog{})
	router := chi.NewRouter()
	New(service).Mount(router)
	return router
}

func TestStorageCenterRoutesExposeCatalogPreflightAndVerification(t *testing.T) {
	repo := &centerHTTPRepository{
		httpStorageRepository: &httpStorageRepository{
			classes:     []domain.StorageClass{{Name: "local-path", Provisioner: "rancher.io/local-path"}},
			classDetail: domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "local-path", Provisioner: "rancher.io/local-path"}},
		},
		nodes:    []domain.NodeSummary{{Name: "worker-1", Status: "Ready"}},
		pods:     []domain.PodSummary{{Name: "local-path-provisioner-abc", Namespace: "local-path-storage", Status: "Running"}},
		releases: []domain.HelmReleaseSummary{{Name: "local-path-provisioner", Namespace: "local-path-storage", Chart: "local-path-provisioner", Status: "deployed", Revision: 1}},
		smoke:    domain.StorageSmokeTestResult{Name: "dosh-smoke", Namespace: "prod", StorageClass: "local-path", Bound: true, PodRunning: true, CleanedUp: true, Message: "PVC 已绑定且 Pod 已成功挂载存储。"},
	}
	router := centerHTTPRouter(repo)

	response := storageRequest(router, "/storage/center")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"catalog"`) || !strings.Contains(response.Body.String(), `"Local Path Provisioner"`) || !strings.Contains(response.Body.String(), `"managed"`) {
		t.Fatalf("storage center status=%d body=%s", response.Code, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodPost, "/storage/center/preflight", `{"catalogId":"local-path-provisioner"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"pass"`) || !strings.Contains(response.Body.String(), `"chart"`) {
		t.Fatalf("storage preflight status=%d body=%s", response.Code, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodPost, "/storage/center/verify", `{"namespace":"prod","storageClass":"local-path","smokeTest":true}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"smokeTest"`) || !strings.Contains(response.Body.String(), `"bound":true`) {
		t.Fatalf("storage verify status=%d body=%s", response.Code, response.Body.String())
	}
}
