package storagehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dosh/internal/domain"
	storageapp "dosh/internal/modules/storage/app"

	"github.com/go-chi/chi/v5"
)

type httpStorageRepository struct {
	classes      []domain.StorageClass
	volumes      []domain.PersistentVolume
	claims       []domain.PersistentVolumeClaim
	classDetail  domain.StorageClassDetail
	volumeDetail domain.PersistentVolumeDetail
	claimDetail  domain.PersistentVolumeClaimDetail
	classesErr   error
	volumesErr   error
	claimsErr    error
	detailErr    error
	lastPVCQry   domain.ListQuery
}

func (r *httpStorageRepository) ListStorageClasses(context.Context, domain.ListQuery) ([]domain.StorageClass, error) {
	return r.classes, r.classesErr
}

func (r *httpStorageRepository) ListPersistentVolumes(context.Context, domain.ListQuery) ([]domain.PersistentVolume, error) {
	return r.volumes, r.volumesErr
}

func (r *httpStorageRepository) ListPersistentVolumeClaims(_ context.Context, query domain.ListQuery) ([]domain.PersistentVolumeClaim, error) {
	r.lastPVCQry = query
	return r.claims, r.claimsErr
}
func (r *httpStorageRepository) GetStorageClass(context.Context, string) (domain.StorageClassDetail, error) {
	return r.classDetail, r.detailErr
}
func (r *httpStorageRepository) GetPersistentVolume(context.Context, string) (domain.PersistentVolumeDetail, error) {
	return r.volumeDetail, r.detailErr
}
func (r *httpStorageRepository) GetPersistentVolumeClaim(context.Context, string, string) (domain.PersistentVolumeClaimDetail, error) {
	return r.claimDetail, r.detailErr
}
func (r *httpStorageRepository) CreatePersistentVolumeClaim(context.Context, domain.PersistentVolumeClaimCreateSpec) (domain.PersistentVolumeClaimDetail, error) {
	return r.claimDetail, r.detailErr
}
func (r *httpStorageRepository) UpdatePersistentVolumeClaim(context.Context, string, string, domain.PersistentVolumeClaimUpdateSpec) (domain.PersistentVolumeClaimDetail, error) {
	return r.claimDetail, r.detailErr
}
func (r *httpStorageRepository) DeletePersistentVolumeClaim(context.Context, string, string, string) error {
	return r.detailErr
}

type storageHTTPAuthorizer struct{}

func (storageHTTPAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return nil }

type storageHTTPAuditor struct{}

func (storageHTTPAuditor) Record(context.Context, domain.Actor, domain.AuditEvent) {}

func storageTestRouter(repo *httpStorageRepository) http.Handler {
	router := chi.NewRouter()
	New(storageapp.New(repo, storageHTTPAuthorizer{}, storageHTTPAuditor{})).Mount(router)
	return router
}

func storageRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestStorageRoutesReturnResourceContracts(t *testing.T) {
	repo := &httpStorageRepository{
		classes: []domain.StorageClass{{Name: "standard", Provisioner: "csi.example.test", Default: true}},
		volumes: []domain.PersistentVolume{{Name: "pv-a", CapacityBytes: 1024, Status: "Bound", Claim: "prod/pvc-a"}},
		claims:  []domain.PersistentVolumeClaim{{Name: "pvc-a", Namespace: "prod", Status: "Bound", VolumeName: "pv-a", CapacityBytes: 1024}},
	}
	router := storageTestRouter(repo)

	response := storageRequest(router, "/storageclasses?q=standard")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"standard"`) ||
		!strings.Contains(response.Body.String(), `"default":true`) || !strings.Contains(response.Body.String(), `"search":"standard"`) {
		t.Fatalf("storage classes status = %d body = %s", response.Code, response.Body.String())
	}
	response = storageRequest(router, "/persistentvolumes")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"pv-a"`) ||
		!strings.Contains(response.Body.String(), `"claim":"prod/pvc-a"`) {
		t.Fatalf("persistent volumes status = %d body = %s", response.Code, response.Body.String())
	}
	response = storageRequest(router, "/persistentvolumeclaims?namespace=%20prod%20&q=pvc")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"pvc-a"`) ||
		!strings.Contains(response.Body.String(), `"namespace":"prod"`) || !strings.Contains(response.Body.String(), `"search":"pvc"`) {
		t.Fatalf("persistent volume claims status = %d body = %s", response.Code, response.Body.String())
	}
	if repo.lastPVCQry.Namespace != "prod" || repo.lastPVCQry.Search != "pvc" {
		t.Fatalf("pvc query = %#v", repo.lastPVCQry)
	}
}

func TestStorageRoutesMapDependencyFailures(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		apply func(*httpStorageRepository)
		want  int
	}{
		{name: "storage classes unavailable", path: "/storageclasses", apply: func(r *httpStorageRepository) { r.classesErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "storage classes rate limited", path: "/storageclasses", apply: func(r *httpStorageRepository) { r.classesErr = domain.ErrRateLimited }, want: http.StatusTooManyRequests},
		{name: "persistent volumes unavailable", path: "/persistentvolumes", apply: func(r *httpStorageRepository) { r.volumesErr = domain.ErrUnavailable }, want: http.StatusServiceUnavailable},
		{name: "persistent volume claims forbidden", path: "/persistentvolumeclaims", apply: func(r *httpStorageRepository) { r.claimsErr = domain.ErrForbidden }, want: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &httpStorageRepository{}
			test.apply(repo)
			response := storageRequest(storageTestRouter(repo), test.path)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestStorageDetailRoutesReturnResourceContracts(t *testing.T) {
	repo := &httpStorageRepository{
		classDetail:  domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "standard"}, AllowVolumeExpansion: true},
		volumeDetail: domain.PersistentVolumeDetail{PersistentVolume: domain.PersistentVolume{Name: "pv-a", Status: "Bound"}, VolumeMode: "Filesystem"},
		claimDetail:  domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{Name: "pvc-a", Namespace: "prod"}, RequestedCapacityBytes: 1024},
	}
	router := storageTestRouter(repo)
	tests := []struct {
		path string
		want string
	}{
		{path: "/storageclasses/standard", want: `"allowVolumeExpansion":true`},
		{path: "/persistentvolumes/pv-a", want: `"volumeMode":"Filesystem"`},
		{path: "/persistentvolumeclaims/prod/pvc-a", want: `"requestedCapacityBytes":1024`},
	}
	for _, test := range tests {
		response := storageRequest(router, test.path)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s status = %d body = %s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestStorageDetailRoutesMapErrors(t *testing.T) {
	repo := &httpStorageRepository{detailErr: domain.ErrNotFound}
	router := storageTestRouter(repo)
	for _, path := range []string{"/storageclasses/missing", "/persistentvolumes/missing", "/persistentvolumeclaims/prod/missing"} {
		response := storageRequest(router, path)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d, body = %s", path, response.Code, http.StatusNotFound, response.Body.String())
		}
	}
}
