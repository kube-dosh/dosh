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
	classes          []domain.StorageClass
	volumes          []domain.PersistentVolume
	claims           []domain.PersistentVolumeClaim
	snapshots        []domain.VolumeSnapshot
	feature          domain.VolumeSnapshotFeature
	classDetail      domain.StorageClassDetail
	volumeDetail     domain.PersistentVolumeDetail
	claimDetail      domain.PersistentVolumeClaimDetail
	classesErr       error
	volumesErr       error
	claimsErr        error
	detailErr        error
	lastPVCQry       domain.ListQuery
	lastSnapshotQry  domain.ListQuery
	lastClassSpec    domain.StorageClassWriteSpec
	lastVolumeCreate domain.PersistentVolumeCreateSpec
	lastVolumeUpdate domain.PersistentVolumeUpdateSpec
	lastSnapshotSpec domain.VolumeSnapshotCreateSpec
	lastDeleteRV     string
	lastDryRun       bool
	writeCalls       int
}

func (r *httpStorageRepository) ListStorageClasses(context.Context, domain.ListQuery) ([]domain.StorageClass, error) {
	return r.classes, r.classesErr
}
func (r *httpStorageRepository) ListStorageClassesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.StorageClass], error) {
	items, err := r.ListStorageClasses(ctx, query)
	return domain.PaginateSlice(items, page), err
}

func (r *httpStorageRepository) ListPersistentVolumes(context.Context, domain.ListQuery) ([]domain.PersistentVolume, error) {
	return r.volumes, r.volumesErr
}
func (r *httpStorageRepository) ListPersistentVolumesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PersistentVolume], error) {
	items, err := r.ListPersistentVolumes(ctx, query)
	return domain.PaginateSlice(items, page), err
}

func (r *httpStorageRepository) ListPersistentVolumeClaims(_ context.Context, query domain.ListQuery) ([]domain.PersistentVolumeClaim, error) {
	r.lastPVCQry = query
	return r.claims, r.claimsErr
}
func (r *httpStorageRepository) ListPersistentVolumeClaimsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PersistentVolumeClaim], error) {
	items, err := r.ListPersistentVolumeClaims(ctx, query)
	return domain.PaginateSlice(items, page), err
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
func (r *httpStorageRepository) CreateStorageClass(_ context.Context, spec domain.StorageClassWriteSpec, dryRun bool) (domain.StorageClassDetail, error) {
	r.lastClassSpec, r.lastDryRun = spec, dryRun
	r.writeCalls++
	return r.classDetail, r.detailErr
}
func (r *httpStorageRepository) UpdateStorageClass(_ context.Context, _ string, spec domain.StorageClassWriteSpec, dryRun bool) (domain.StorageClassDetail, error) {
	r.lastClassSpec, r.lastDryRun = spec, dryRun
	r.writeCalls++
	return r.classDetail, r.detailErr
}
func (r *httpStorageRepository) DeleteStorageClass(_ context.Context, _, resourceVersion string) error {
	r.lastDeleteRV = resourceVersion
	r.writeCalls++
	return r.detailErr
}
func (r *httpStorageRepository) CreatePersistentVolume(_ context.Context, spec domain.PersistentVolumeCreateSpec, dryRun bool) (domain.PersistentVolumeDetail, error) {
	r.lastVolumeCreate, r.lastDryRun = spec, dryRun
	r.writeCalls++
	return r.volumeDetail, r.detailErr
}
func (r *httpStorageRepository) UpdatePersistentVolume(_ context.Context, _ string, update domain.PersistentVolumeUpdateSpec, dryRun bool) (domain.PersistentVolumeDetail, error) {
	r.lastVolumeUpdate, r.lastDryRun = update, dryRun
	r.writeCalls++
	return r.volumeDetail, r.detailErr
}
func (r *httpStorageRepository) DeletePersistentVolume(_ context.Context, _, resourceVersion string) error {
	r.lastDeleteRV = resourceVersion
	r.writeCalls++
	return r.detailErr
}
func (r *httpStorageRepository) CreatePersistentVolumeClaim(context.Context, domain.PersistentVolumeClaimCreateSpec, bool) (domain.PersistentVolumeClaimDetail, error) {
	r.writeCalls++
	return r.claimDetail, r.detailErr
}
func (r *httpStorageRepository) UpdatePersistentVolumeClaim(context.Context, string, string, domain.PersistentVolumeClaimUpdateSpec, bool) (domain.PersistentVolumeClaimDetail, error) {
	return r.claimDetail, r.detailErr
}
func (r *httpStorageRepository) DeletePersistentVolumeClaim(context.Context, string, string, string) error {
	return r.detailErr
}
func (r *httpStorageRepository) VolumeSnapshotFeature(context.Context) (domain.VolumeSnapshotFeature, error) {
	if r.feature.Reason != "" || r.feature.Available || r.feature.Classes != nil {
		return r.feature, r.detailErr
	}
	return domain.VolumeSnapshotFeature{Available: false, Reason: "CRD unavailable", Classes: []string{}}, r.detailErr
}
func (r *httpStorageRepository) ListVolumeSnapshots(_ context.Context, query domain.ListQuery) ([]domain.VolumeSnapshot, error) {
	r.lastSnapshotQry = query
	return r.snapshots, r.detailErr
}
func (r *httpStorageRepository) CreateVolumeSnapshot(_ context.Context, spec domain.VolumeSnapshotCreateSpec, dryRun bool) (domain.VolumeSnapshot, error) {
	r.lastSnapshotSpec, r.lastDryRun = spec, dryRun
	r.writeCalls++
	return domain.VolumeSnapshot{Name: spec.Name, Namespace: spec.Namespace, SourcePVC: spec.SourcePVC}, r.detailErr
}
func (r *httpStorageRepository) DeleteVolumeSnapshot(_ context.Context, _, _ string, resourceVersion string) error {
	r.lastDeleteRV = resourceVersion
	r.writeCalls++
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

func storageWriteRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
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

func TestStorageWriteRoutesEnforceContractsAndResourceVersions(t *testing.T) {
	repo := &httpStorageRepository{
		classDetail:  domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "fast", Provisioner: "csi.example.test"}},
		volumeDetail: domain.PersistentVolumeDetail{PersistentVolume: domain.PersistentVolume{Name: "pv-new", CapacityBytes: 1 << 30}},
	}
	router := storageTestRouter(repo)

	response := storageWriteRequest(router, http.MethodPost, "/storageclasses", `{
		"name":"fast","provisioner":"csi.example.test","reclaimPolicy":"Delete","volumeBinding":"Immediate","allowVolumeExpansion":true
	}`)
	if response.Code != http.StatusCreated || repo.writeCalls != 2 || repo.lastDryRun || repo.lastClassSpec.Name != "fast" || !repo.lastClassSpec.AllowVolumeExpansion {
		t.Fatalf("create StorageClass status=%d calls=%d spec=%#v body=%s", response.Code, repo.writeCalls, repo.lastClassSpec, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodPut, "/storageclasses/fast", `{
		"provisioner":"csi.example.test","reclaimPolicy":"Retain","volumeBinding":"WaitForFirstConsumer","allowVolumeExpansion":true,"resourceVersion":"7"
	}`)
	if response.Code != http.StatusOK || repo.writeCalls != 4 || repo.lastClassSpec.ResourceVersion != "7" || repo.lastClassSpec.ReclaimPolicy != "Retain" {
		t.Fatalf("update StorageClass status=%d calls=%d spec=%#v body=%s", response.Code, repo.writeCalls, repo.lastClassSpec, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodDelete, "/storageclasses/fast?resourceVersion=7", "")
	if response.Code != http.StatusAccepted || repo.writeCalls != 5 || repo.lastDeleteRV != "7" {
		t.Fatalf("delete StorageClass status=%d calls=%d rv=%q body=%s", response.Code, repo.writeCalls, repo.lastDeleteRV, response.Body.String())
	}

	response = storageWriteRequest(router, http.MethodPost, "/persistentvolumes", `{
		"name":"pv-new","capacityBytes":1073741824,"accessModes":["ReadWriteOnce"],"reclaimPolicy":"Retain",
		"csi":{"driver":"csi.example.test","volumeHandle":"vol-1"}
	}`)
	if response.Code != http.StatusCreated || repo.writeCalls != 7 || repo.lastVolumeCreate.Name != "pv-new" || repo.lastVolumeCreate.CSI.VolumeHandle != "vol-1" {
		t.Fatalf("create PV status=%d calls=%d spec=%#v body=%s", response.Code, repo.writeCalls, repo.lastVolumeCreate, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodPut, "/persistentvolumes/pv-new", `{
		"capacityBytes":2147483648,"reclaimPolicy":"Delete","resourceVersion":"9"
	}`)
	if response.Code != http.StatusOK || repo.writeCalls != 9 || repo.lastVolumeUpdate.ResourceVersion != "9" || repo.lastVolumeUpdate.CapacityBytes != 2<<30 {
		t.Fatalf("update PV status=%d calls=%d spec=%#v body=%s", response.Code, repo.writeCalls, repo.lastVolumeUpdate, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodDelete, "/persistentvolumes/pv-new?resourceVersion=9", "")
	if response.Code != http.StatusAccepted || repo.writeCalls != 10 || repo.lastDeleteRV != "9" {
		t.Fatalf("delete PV status=%d calls=%d rv=%q body=%s", response.Code, repo.writeCalls, repo.lastDeleteRV, response.Body.String())
	}
}

func TestVolumeSnapshotRoutesExposeFeatureAndWriteContracts(t *testing.T) {
	repo := &httpStorageRepository{
		feature:   domain.VolumeSnapshotFeature{Available: true, APIVersion: "snapshot.storage.k8s.io/v1", Classes: []string{"csi-snap"}},
		snapshots: []domain.VolumeSnapshot{{Name: "snap-a", Namespace: "prod", SourcePVC: "data", Status: "Ready"}},
	}
	router := storageTestRouter(repo)

	response := storageRequest(router, "/volume-snapshots/feature")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"available":true`) || !strings.Contains(response.Body.String(), `"apiVersion":"snapshot.storage.k8s.io/v1"`) {
		t.Fatalf("snapshot feature status=%d body=%s", response.Code, response.Body.String())
	}
	response = storageRequest(router, "/volume-snapshots?namespace=%20prod%20&q=snap")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"snap-a"`) || repo.lastSnapshotQry.Namespace != "prod" || repo.lastSnapshotQry.Search != "snap" {
		t.Fatalf("snapshot list status=%d query=%#v body=%s", response.Code, repo.lastSnapshotQry, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodPost, "/volume-snapshots", `{"name":"snap-b","namespace":"prod","sourcePvc":"data","volumeSnapshotClassName":"csi-snap"}`)
	if response.Code != http.StatusCreated || repo.writeCalls != 2 || repo.lastSnapshotSpec.SourcePVC != "data" || repo.lastSnapshotSpec.VolumeSnapshotClassName != "csi-snap" {
		t.Fatalf("create snapshot status=%d calls=%d spec=%#v body=%s", response.Code, repo.writeCalls, repo.lastSnapshotSpec, response.Body.String())
	}
	response = storageWriteRequest(router, http.MethodDelete, "/volume-snapshots/prod/snap-b?resourceVersion=13", "")
	if response.Code != http.StatusAccepted || repo.writeCalls != 3 || repo.lastDeleteRV != "13" {
		t.Fatalf("delete snapshot status=%d calls=%d rv=%q body=%s", response.Code, repo.writeCalls, repo.lastDeleteRV, response.Body.String())
	}
}

func TestVolumeSnapshotRoutesReportUnsupportedCRD(t *testing.T) {
	repo := &httpStorageRepository{detailErr: domain.ErrUnsupported}
	router := storageTestRouter(repo)
	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/volume-snapshots"},
		{method: http.MethodPost, path: "/volume-snapshots", body: `{"name":"snap-a","namespace":"prod","sourcePvc":"data"}`},
		{method: http.MethodDelete, path: "/volume-snapshots/prod/snap-a?resourceVersion=1"},
	} {
		response := storageWriteRequest(router, test.method, test.path, test.body)
		if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "not_supported") {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}
