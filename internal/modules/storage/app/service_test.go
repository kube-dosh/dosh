package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type storageRepository struct {
	classes               []domain.StorageClass
	volumes               []domain.PersistentVolume
	claims                []domain.PersistentVolumeClaim
	snapshots             []domain.VolumeSnapshot
	classDetail           domain.StorageClassDetail
	volumeDetail          domain.PersistentVolumeDetail
	claimDetail           domain.PersistentVolumeClaimDetail
	feature               domain.VolumeSnapshotFeature
	err                   error
	classErr              error
	volumeErr             error
	claimErr              error
	featureErr            error
	snapshotListErr       error
	snapshotCreateErr     error
	snapshotDeleteErr     error
	createCalls           int
	updateCalls           int
	deleteCalls           int
	classCreateCalls      int
	classUpdateCalls      int
	classDeleteCalls      int
	volumeCreateCalls     int
	volumeUpdateCalls     int
	volumeDeleteCalls     int
	snapshotCreateCalls   int
	snapshotDeleteCalls   int
	lastCreate            domain.PersistentVolumeClaimCreateSpec
	lastUpdate            domain.PersistentVolumeClaimUpdateSpec
	lastDeleteRV          string
	lastClassCreate       domain.StorageClassWriteSpec
	lastClassUpdate       domain.StorageClassWriteSpec
	lastVolumeCreate      domain.PersistentVolumeCreateSpec
	lastVolumeUpdate      domain.PersistentVolumeUpdateSpec
	lastSnapshot          domain.VolumeSnapshotCreateSpec
	lastSnapshotQuery     domain.ListQuery
	lastSnapshotRV        string
	classCreateDryRuns    []bool
	classUpdateDryRuns    []bool
	volumeCreateDryRuns   []bool
	volumeUpdateDryRuns   []bool
	snapshotCreateDryRuns []bool
}

func (r *storageRepository) ListStorageClasses(context.Context, domain.ListQuery) ([]domain.StorageClass, error) {
	return r.classes, r.err
}
func (r *storageRepository) ListStorageClassesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.StorageClass], error) {
	items, err := r.ListStorageClasses(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *storageRepository) ListPersistentVolumes(context.Context, domain.ListQuery) ([]domain.PersistentVolume, error) {
	return r.volumes, r.err
}
func (r *storageRepository) ListPersistentVolumesPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PersistentVolume], error) {
	items, err := r.ListPersistentVolumes(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *storageRepository) ListPersistentVolumeClaims(context.Context, domain.ListQuery) ([]domain.PersistentVolumeClaim, error) {
	return r.claims, r.err
}
func (r *storageRepository) ListPersistentVolumeClaimsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PersistentVolumeClaim], error) {
	items, err := r.ListPersistentVolumeClaims(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (r *storageRepository) GetStorageClass(context.Context, string) (domain.StorageClassDetail, error) {
	return r.classDetail, firstError(r.classErr, r.err)
}
func (r *storageRepository) GetPersistentVolume(context.Context, string) (domain.PersistentVolumeDetail, error) {
	return r.volumeDetail, firstError(r.volumeErr, r.err)
}
func (r *storageRepository) GetPersistentVolumeClaim(context.Context, string, string) (domain.PersistentVolumeClaimDetail, error) {
	return r.claimDetail, firstError(r.claimErr, r.err)
}
func (r *storageRepository) CreateStorageClass(_ context.Context, spec domain.StorageClassWriteSpec, dryRun bool) (domain.StorageClassDetail, error) {
	r.classCreateCalls++
	r.lastClassCreate = spec
	r.classCreateDryRuns = append(r.classCreateDryRuns, dryRun)
	return r.classDetail, firstError(r.classErr, r.err)
}
func (r *storageRepository) UpdateStorageClass(_ context.Context, _ string, spec domain.StorageClassWriteSpec, dryRun bool) (domain.StorageClassDetail, error) {
	r.classUpdateCalls++
	r.lastClassUpdate = spec
	r.classUpdateDryRuns = append(r.classUpdateDryRuns, dryRun)
	return r.classDetail, firstError(r.classErr, r.err)
}
func (r *storageRepository) DeleteStorageClass(context.Context, string, string) error {
	r.classDeleteCalls++
	return firstError(r.classErr, r.err)
}
func (r *storageRepository) CreatePersistentVolume(_ context.Context, spec domain.PersistentVolumeCreateSpec, dryRun bool) (domain.PersistentVolumeDetail, error) {
	r.volumeCreateCalls++
	r.lastVolumeCreate = spec
	r.volumeCreateDryRuns = append(r.volumeCreateDryRuns, dryRun)
	return r.volumeDetail, firstError(r.volumeErr, r.err)
}
func (r *storageRepository) UpdatePersistentVolume(_ context.Context, _ string, update domain.PersistentVolumeUpdateSpec, dryRun bool) (domain.PersistentVolumeDetail, error) {
	r.volumeUpdateCalls++
	r.lastVolumeUpdate = update
	r.volumeUpdateDryRuns = append(r.volumeUpdateDryRuns, dryRun)
	return r.volumeDetail, firstError(r.volumeErr, r.err)
}
func (r *storageRepository) DeletePersistentVolume(context.Context, string, string) error {
	r.volumeDeleteCalls++
	return firstError(r.volumeErr, r.err)
}
func (r *storageRepository) CreatePersistentVolumeClaim(_ context.Context, spec domain.PersistentVolumeClaimCreateSpec, _ bool) (domain.PersistentVolumeClaimDetail, error) {
	r.createCalls++
	r.lastCreate = spec
	return r.claimDetail, firstError(r.claimErr, r.err)
}
func (r *storageRepository) UpdatePersistentVolumeClaim(_ context.Context, _, _ string, update domain.PersistentVolumeClaimUpdateSpec, _ bool) (domain.PersistentVolumeClaimDetail, error) {
	r.updateCalls++
	r.lastUpdate = update
	return r.claimDetail, firstError(r.claimErr, r.err)
}
func (r *storageRepository) DeletePersistentVolumeClaim(_ context.Context, _, _ string, resourceVersion string) error {
	r.deleteCalls++
	r.lastDeleteRV = resourceVersion
	return firstError(r.claimErr, r.err)
}
func (r *storageRepository) VolumeSnapshotFeature(context.Context) (domain.VolumeSnapshotFeature, error) {
	return r.feature, firstError(r.featureErr, r.err)
}
func (r *storageRepository) ListVolumeSnapshots(_ context.Context, query domain.ListQuery) ([]domain.VolumeSnapshot, error) {
	r.lastSnapshotQuery = query
	return r.snapshots, firstError(r.snapshotListErr, r.err)
}
func (r *storageRepository) CreateVolumeSnapshot(_ context.Context, spec domain.VolumeSnapshotCreateSpec, dryRun bool) (domain.VolumeSnapshot, error) {
	r.snapshotCreateCalls++
	r.lastSnapshot = spec
	r.snapshotCreateDryRuns = append(r.snapshotCreateDryRuns, dryRun)
	return domain.VolumeSnapshot{Name: spec.Name, Namespace: spec.Namespace}, firstError(r.snapshotCreateErr, r.err)
}
func (r *storageRepository) DeleteVolumeSnapshot(_ context.Context, _, _ string, resourceVersion string) error {
	r.snapshotDeleteCalls++
	r.lastSnapshotRV = resourceVersion
	return firstError(r.snapshotDeleteErr, r.err)
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

type storageTestAuthorizer struct {
	err    error
	access domain.Access
	calls  int
}

func (a *storageTestAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.calls++
	a.access = access
	return a.err
}

type storageTestAuditor struct{ events []domain.AuditEvent }

func (a *storageTestAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestStorageServiceListsResourcesAndFailures(t *testing.T) {
	repo := &storageRepository{
		classes: []domain.StorageClass{{Name: "standard"}},
		volumes: []domain.PersistentVolume{{Name: "pv-a"}},
		claims:  []domain.PersistentVolumeClaim{{Name: "pvc-a"}},
	}
	service := New(repo, &storageTestAuthorizer{}, &storageTestAuditor{})
	classes, err := service.ListStorageClasses(context.Background(), domain.ListQuery{})
	if err != nil || len(classes) != 1 {
		t.Fatalf("ListStorageClasses() = (%#v, %v)", classes, err)
	}
	volumes, err := service.ListPersistentVolumes(context.Background(), domain.ListQuery{})
	if err != nil || len(volumes) != 1 {
		t.Fatalf("ListPersistentVolumes() = (%#v, %v)", volumes, err)
	}
	claims, err := service.ListPersistentVolumeClaims(context.Background(), domain.ListQuery{})
	if err != nil || len(claims) != 1 {
		t.Fatalf("ListPersistentVolumeClaims() = (%#v, %v)", claims, err)
	}
	repo.classDetail = domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "standard"}, Parameters: map[string]string{"type": "ssd"}}
	repo.volumeDetail = domain.PersistentVolumeDetail{PersistentVolume: domain.PersistentVolume{Name: "pv-a"}, VolumeMode: "Filesystem"}
	repo.claimDetail = domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{Name: "pvc-a", Namespace: "prod"}, RequestedCapacityBytes: 1024}
	class, err := service.GetStorageClass(context.Background(), "standard")
	if err != nil || class.Parameters["type"] != "ssd" {
		t.Fatalf("GetStorageClass() = (%#v, %v)", class, err)
	}
	volume, err := service.GetPersistentVolume(context.Background(), "pv-a")
	if err != nil || volume.VolumeMode != "Filesystem" {
		t.Fatalf("GetPersistentVolume() = (%#v, %v)", volume, err)
	}
	claim, err := service.GetPersistentVolumeClaim(context.Background(), "prod", "pvc-a")
	if err != nil || claim.RequestedCapacityBytes != 1024 {
		t.Fatalf("GetPersistentVolumeClaim() = (%#v, %v)", claim, err)
	}

	dependencyErr := errors.New("kubernetes unavailable")
	repo.err = dependencyErr
	if _, err := service.ListStorageClasses(context.Background(), domain.ListQuery{}); !errors.Is(err, dependencyErr) {
		t.Fatalf("ListStorageClasses() error = %v", err)
	}
	if _, err := service.GetStorageClass(context.Background(), "standard"); !errors.Is(err, dependencyErr) {
		t.Fatalf("GetStorageClass() error = %v", err)
	}
}

func TestStorageServiceWritesPersistentVolumeClaims(t *testing.T) {
	t.Parallel()
	repo := &storageRepository{
		claimDetail: domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{Name: "data", Namespace: "prod", StorageClass: "standard"}},
		classDetail: domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "standard"}, AllowVolumeExpansion: true},
	}
	authorizer := &storageTestAuthorizer{}
	auditor := &storageTestAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}

	if _, err := service.CreatePersistentVolumeClaim(context.Background(), actor, domain.PersistentVolumeClaimCreateSpec{
		Namespace: "prod", Name: "data", StorageClass: "standard", RequestedCapacityBytes: 1 << 30,
		AccessModes: []string{"ReadWriteOnce"}, VolumeMode: "Filesystem", Labels: map[string]string{"app": "db"},
	}); err != nil {
		t.Fatalf("CreatePersistentVolumeClaim() error = %v", err)
	}
	if repo.createCalls != 2 || repo.lastCreate.Name != "data" || repo.lastCreate.StorageClass != "standard" || repo.lastCreate.AccessModes[0] != "ReadWriteOnce" {
		t.Fatalf("create PVC call = %d spec = %#v", repo.createCalls, repo.lastCreate)
	}
	if authorizer.access != (domain.Access{Verb: "create", Resource: "persistentvolumeclaims", Namespace: "prod", Name: "data"}) {
		t.Fatalf("create access = %#v", authorizer.access)
	}

	if _, err := service.UpdatePersistentVolumeClaim(context.Background(), actor, "prod", "data", domain.PersistentVolumeClaimUpdateSpec{
		RequestedCapacityBytes: 2 << 30, ResourceVersion: "41", Labels: map[string]string{"app": "db"},
	}); err != nil {
		t.Fatalf("UpdatePersistentVolumeClaim() error = %v", err)
	}
	if repo.updateCalls != 2 || repo.lastUpdate.RequestedCapacityBytes != 2<<30 || repo.lastUpdate.ResourceVersion != "41" {
		t.Fatalf("update PVC call = %d spec = %#v", repo.updateCalls, repo.lastUpdate)
	}
	if err := service.DeletePersistentVolumeClaim(context.Background(), actor, "prod", "data", "42"); err != nil {
		t.Fatalf("DeletePersistentVolumeClaim() error = %v", err)
	}
	if repo.deleteCalls != 1 || repo.lastDeleteRV != "42" {
		t.Fatalf("delete PVC calls = %d rv = %q", repo.deleteCalls, repo.lastDeleteRV)
	}
	if len(auditor.events) != 3 || auditor.events[0].Action != "create-persistentvolumeclaim" || auditor.events[1].Action != "update-persistentvolumeclaim" || auditor.events[2].Action != "delete-persistentvolumeclaim" {
		t.Fatalf("audit = %#v", auditor.events)
	}
}

func TestStorageServiceRejectsInvalidPVCAndPermissionFailures(t *testing.T) {
	t.Parallel()
	repo := &storageRepository{}
	authorizer := &storageTestAuthorizer{}
	auditor := &storageTestAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}
	if _, err := service.CreatePersistentVolumeClaim(context.Background(), actor, domain.PersistentVolumeClaimCreateSpec{
		Namespace: "prod", Name: "data", RequestedCapacityBytes: 0, AccessModes: []string{"ReadWriteOnce"},
	}); err == nil {
		t.Fatal("CreatePersistentVolumeClaim() accepted zero capacity")
	}
	if repo.createCalls != 0 || authorizer.calls != 0 {
		t.Fatalf("invalid PVC reached write path: repo=%d auth=%d", repo.createCalls, authorizer.calls)
	}

	authorizer.err = domain.ErrForbidden
	if _, err := service.CreatePersistentVolumeClaim(context.Background(), actor, domain.PersistentVolumeClaimCreateSpec{
		Namespace: "prod", Name: "data", RequestedCapacityBytes: 1 << 30, AccessModes: []string{"ReadWriteOnce"},
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("CreatePersistentVolumeClaim() error = %v", err)
	}
	if repo.createCalls != 0 || len(auditor.events) != 2 || auditor.events[0].Result != "failure" || auditor.events[1].Result != "denied" {
		t.Fatalf("denied PVC audit = %#v", auditor.events)
	}
}

func TestStorageServiceWritesStorageClassesWithDryRunAndProtections(t *testing.T) {
	t.Parallel()
	repo := &storageRepository{classDetail: domain.StorageClassDetail{
		StorageClass: domain.StorageClass{Name: "fast", Provisioner: "csi.example.test", ResourceVersion: "7"},
	}}
	authorizer := &storageTestAuthorizer{}
	auditor := &storageTestAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}
	spec := domain.StorageClassWriteSpec{
		Name: "fast", Provisioner: " csi.example.test ", ReclaimPolicy: "", VolumeBinding: "",
		AllowVolumeExpansion: true, Default: true, Parameters: map[string]string{" type ": "ssd", "": "ignored"},
		MountOptions: []string{" discard ", "discard", ""}, ResourceVersion: "7",
	}

	if _, err := service.CreateStorageClass(context.Background(), actor, spec); err != nil {
		t.Fatalf("CreateStorageClass() error = %v", err)
	}
	if repo.classCreateCalls != 2 || len(repo.classCreateDryRuns) != 2 || !repo.classCreateDryRuns[0] || repo.classCreateDryRuns[1] {
		t.Fatalf("StorageClass create calls=%d dryRuns=%#v", repo.classCreateCalls, repo.classCreateDryRuns)
	}
	if repo.lastClassCreate.Provisioner != "csi.example.test" || repo.lastClassCreate.ReclaimPolicy != "Delete" ||
		repo.lastClassCreate.VolumeBinding != "Immediate" || repo.lastClassCreate.Parameters["type"] != "ssd" ||
		len(repo.lastClassCreate.Parameters) != 1 || len(repo.lastClassCreate.MountOptions) != 1 {
		t.Fatalf("normalized StorageClass create spec = %#v", repo.lastClassCreate)
	}
	if authorizer.access != (domain.Access{Verb: "create", APIGroup: "storage.k8s.io", Resource: "storageclasses", Name: "fast"}) {
		t.Fatalf("StorageClass create access = %#v", authorizer.access)
	}

	if _, err := service.UpdateStorageClass(context.Background(), actor, "fast", spec); err != nil {
		t.Fatalf("UpdateStorageClass() error = %v", err)
	}
	if repo.classUpdateCalls != 2 || len(repo.classUpdateDryRuns) != 2 || !repo.classUpdateDryRuns[0] || repo.classUpdateDryRuns[1] {
		t.Fatalf("StorageClass update calls=%d dryRuns=%#v", repo.classUpdateCalls, repo.classUpdateDryRuns)
	}
	spec.Provisioner = "csi.other.test"
	if _, err := service.UpdateStorageClass(context.Background(), actor, "fast", spec); err == nil {
		t.Fatal("UpdateStorageClass() allowed a provisioner change")
	}
	if repo.classUpdateCalls != 2 {
		t.Fatalf("immutable provisioner reached repository: calls=%d", repo.classUpdateCalls)
	}

	repo.classDetail.Default = true
	if err := service.DeleteStorageClass(context.Background(), actor, "fast", "7"); err == nil || repo.classDeleteCalls != 0 {
		t.Fatalf("default StorageClass delete = (%v, calls=%d)", err, repo.classDeleteCalls)
	}
	repo.classDetail.Default = false
	if err := service.DeleteStorageClass(context.Background(), actor, "kube-fast", "7"); err == nil || repo.classDeleteCalls != 0 {
		t.Fatalf("kube-* StorageClass delete = (%v, calls=%d)", err, repo.classDeleteCalls)
	}
	if err := service.DeleteStorageClass(context.Background(), actor, "fast", "7"); err != nil || repo.classDeleteCalls != 1 {
		t.Fatalf("StorageClass delete = (%v, calls=%d)", err, repo.classDeleteCalls)
	}
	if len(auditor.events) != 6 || auditor.events[len(auditor.events)-1].Result != "success" {
		t.Fatalf("StorageClass audit = %#v", auditor.events)
	}
}

func TestStorageServiceWritesPersistentVolumesWithCapacityGuard(t *testing.T) {
	t.Parallel()
	repo := &storageRepository{
		volumeDetail: domain.PersistentVolumeDetail{PersistentVolume: domain.PersistentVolume{Name: "pv-a", CapacityBytes: 2 << 30, ResourceVersion: "9"}},
	}
	authorizer := &storageTestAuthorizer{}
	auditor := &storageTestAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}

	if _, err := service.CreatePersistentVolume(context.Background(), actor, domain.PersistentVolumeCreateSpec{
		Name: "pv-a", CapacityBytes: 2 << 30, AccessModes: []string{"ReadWriteOnce"}, VolumeMode: "Filesystem",
		CSI: &domain.CSIPersistentVolumeSource{Driver: " csi.example.test ", VolumeHandle: " vol-1 "},
	}); err != nil {
		t.Fatalf("CreatePersistentVolume() error = %v", err)
	}
	if repo.volumeCreateCalls != 2 || len(repo.volumeCreateDryRuns) != 2 || !repo.volumeCreateDryRuns[0] || repo.volumeCreateDryRuns[1] {
		t.Fatalf("PV create calls=%d dryRuns=%#v", repo.volumeCreateCalls, repo.volumeCreateDryRuns)
	}
	if repo.lastVolumeCreate.ReclaimPolicy != "Retain" || repo.lastVolumeCreate.CSI.Driver != "csi.example.test" || repo.lastVolumeCreate.CSI.VolumeHandle != "vol-1" {
		t.Fatalf("normalized PV create spec = %#v", repo.lastVolumeCreate)
	}
	if authorizer.access != (domain.Access{Verb: "create", Resource: "persistentvolumes", Name: "pv-a"}) {
		t.Fatalf("PV create access = %#v", authorizer.access)
	}

	if _, err := service.UpdatePersistentVolume(context.Background(), actor, "pv-a", domain.PersistentVolumeUpdateSpec{
		CapacityBytes: 1 << 30, ReclaimPolicy: "Retain", ResourceVersion: "9",
	}); err == nil || repo.volumeUpdateCalls != 0 {
		t.Fatalf("PV shrink = (%v, calls=%d)", err, repo.volumeUpdateCalls)
	}
	if _, err := service.UpdatePersistentVolume(context.Background(), actor, "pv-a", domain.PersistentVolumeUpdateSpec{
		CapacityBytes: 3 << 30, ReclaimPolicy: "Delete", MountOptions: []string{" discard ", "discard"}, ResourceVersion: "9",
	}); err != nil {
		t.Fatalf("UpdatePersistentVolume() error = %v", err)
	}
	if repo.volumeUpdateCalls != 2 || len(repo.volumeUpdateDryRuns) != 2 || !repo.volumeUpdateDryRuns[0] || repo.volumeUpdateDryRuns[1] ||
		repo.lastVolumeUpdate.CapacityBytes != 3<<30 || len(repo.lastVolumeUpdate.MountOptions) != 1 {
		t.Fatalf("PV update calls=%d dryRuns=%#v spec=%#v", repo.volumeUpdateCalls, repo.volumeUpdateDryRuns, repo.lastVolumeUpdate)
	}
	if err := service.DeletePersistentVolume(context.Background(), actor, "kube-pv", "9"); err == nil || repo.volumeDeleteCalls != 0 {
		t.Fatalf("kube-* PV delete = (%v, calls=%d)", err, repo.volumeDeleteCalls)
	}
	if err := service.DeletePersistentVolume(context.Background(), actor, "pv-a", "9"); err != nil || repo.volumeDeleteCalls != 1 {
		t.Fatalf("PV delete = (%v, calls=%d)", err, repo.volumeDeleteCalls)
	}
	if len(auditor.events) != 5 || auditor.events[len(auditor.events)-1].Result != "success" {
		t.Fatalf("PV audit = %#v", auditor.events)
	}
}

func TestStorageServiceRejectsPVCExpansionWithoutExpansionPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		claim     domain.PersistentVolumeClaimDetail
		classErr  error
		class     domain.StorageClassDetail
		wantCalls int
	}{
		{
			name: "missing StorageClass",
			claim: domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{
				Name: "data", Namespace: "prod", StorageClass: "gone",
			}, RequestedCapacityBytes: 1 << 30},
			classErr: domain.ErrNotFound,
		},
		{
			name: "expansion disabled",
			claim: domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{
				Name: "data", Namespace: "prod", StorageClass: "standard",
			}, RequestedCapacityBytes: 1 << 30},
			class: domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "standard"}},
		},
		{
			name: "no StorageClass",
			claim: domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{
				Name: "data", Namespace: "prod",
			}, RequestedCapacityBytes: 1 << 30},
		},
		{
			name: "expansion enabled",
			claim: domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{
				Name: "data", Namespace: "prod", StorageClass: "standard",
			}, RequestedCapacityBytes: 1 << 30},
			class:     domain.StorageClassDetail{StorageClass: domain.StorageClass{Name: "standard"}, AllowVolumeExpansion: true},
			wantCalls: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &storageRepository{claimDetail: test.claim, classDetail: test.class, classErr: test.classErr}
			auditor := &storageTestAuditor{}
			service := New(repo, &storageTestAuthorizer{}, auditor)
			_, err := service.UpdatePersistentVolumeClaim(context.Background(), domain.Actor{Username: "admin"}, "prod", "data", domain.PersistentVolumeClaimUpdateSpec{
				RequestedCapacityBytes: 2 << 30, ResourceVersion: "11",
			})
			if test.wantCalls == 0 && (err == nil || repo.updateCalls != 0) {
				t.Fatalf("UpdatePersistentVolumeClaim() = (%v, calls=%d), want a preflight rejection", err, repo.updateCalls)
			}
			if test.wantCalls > 0 && (err != nil || repo.updateCalls != test.wantCalls) {
				t.Fatalf("UpdatePersistentVolumeClaim() = (%v, calls=%d), want %d calls", err, repo.updateCalls, test.wantCalls)
			}
		})
	}
}

func TestStorageServiceManagesVolumeSnapshotsAndSurfacesUnsupportedFeature(t *testing.T) {
	t.Parallel()
	repo := &storageRepository{
		feature:   domain.VolumeSnapshotFeature{Available: false, Reason: "CRD unavailable", Classes: []string{}},
		snapshots: []domain.VolumeSnapshot{{Name: "snap-a", Namespace: "prod", SourcePVC: "data"}},
	}
	authorizer := &storageTestAuthorizer{}
	auditor := &storageTestAuditor{}
	service := New(repo, authorizer, auditor)
	actor := domain.Actor{Username: "admin"}

	feature, err := service.VolumeSnapshotFeature(context.Background())
	if err != nil || feature.Available || feature.Reason != "CRD unavailable" {
		t.Fatalf("VolumeSnapshotFeature() = (%#v, %v)", feature, err)
	}
	items, err := service.ListVolumeSnapshots(context.Background(), actor, domain.ListQuery{Namespace: " prod "})
	if err != nil || len(items) != 1 || repo.lastSnapshotQuery.Namespace != "prod" {
		t.Fatalf("ListVolumeSnapshots() = (%#v, %v), query=%#v", items, err, repo.lastSnapshotQuery)
	}
	if authorizer.access != (domain.Access{Verb: "list", APIGroup: "snapshot.storage.k8s.io", Resource: "volumesnapshots", Namespace: "prod"}) {
		t.Fatalf("snapshot list access = %#v", authorizer.access)
	}
	if _, err := service.CreateVolumeSnapshot(context.Background(), actor, domain.VolumeSnapshotCreateSpec{
		Namespace: "prod", Name: "snap-a", SourcePVC: " data ", VolumeSnapshotClassName: " csi-snap ",
	}); err != nil {
		t.Fatalf("CreateVolumeSnapshot() error = %v", err)
	}
	if repo.snapshotCreateCalls != 2 || len(repo.snapshotCreateDryRuns) != 2 || !repo.snapshotCreateDryRuns[0] || repo.snapshotCreateDryRuns[1] ||
		repo.lastSnapshot.SourcePVC != "data" || repo.lastSnapshot.VolumeSnapshotClassName != "csi-snap" {
		t.Fatalf("snapshot create calls=%d dryRuns=%#v spec=%#v", repo.snapshotCreateCalls, repo.snapshotCreateDryRuns, repo.lastSnapshot)
	}
	if err := service.DeleteVolumeSnapshot(context.Background(), actor, "prod", "snap-a", "13"); err != nil || repo.snapshotDeleteCalls != 1 || repo.lastSnapshotRV != "13" {
		t.Fatalf("DeleteVolumeSnapshot() = (%v, calls=%d rv=%q)", err, repo.snapshotDeleteCalls, repo.lastSnapshotRV)
	}
	repo.snapshotCreateErr = domain.ErrUnsupported
	if _, err := service.CreateVolumeSnapshot(context.Background(), actor, domain.VolumeSnapshotCreateSpec{
		Namespace: "prod", Name: "snap-b", SourcePVC: "data",
	}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("unsupported snapshot create error = %v", err)
	}
	if len(auditor.events) != 3 || auditor.events[1].Result != "success" || auditor.events[2].Result != "failure" {
		t.Fatalf("snapshot audit = %#v", auditor.events)
	}
}
