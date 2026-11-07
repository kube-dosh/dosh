package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type storageRepository struct {
	classes      []domain.StorageClass
	volumes      []domain.PersistentVolume
	claims       []domain.PersistentVolumeClaim
	classDetail  domain.StorageClassDetail
	volumeDetail domain.PersistentVolumeDetail
	claimDetail  domain.PersistentVolumeClaimDetail
	err          error
	createCalls  int
	updateCalls  int
	deleteCalls  int
	lastCreate   domain.PersistentVolumeClaimCreateSpec
	lastUpdate   domain.PersistentVolumeClaimUpdateSpec
	lastDeleteRV string
}

func (r *storageRepository) ListStorageClasses(context.Context, domain.ListQuery) ([]domain.StorageClass, error) {
	return r.classes, r.err
}
func (r *storageRepository) ListPersistentVolumes(context.Context, domain.ListQuery) ([]domain.PersistentVolume, error) {
	return r.volumes, r.err
}
func (r *storageRepository) ListPersistentVolumeClaims(context.Context, domain.ListQuery) ([]domain.PersistentVolumeClaim, error) {
	return r.claims, r.err
}
func (r *storageRepository) GetStorageClass(context.Context, string) (domain.StorageClassDetail, error) {
	return r.classDetail, r.err
}
func (r *storageRepository) GetPersistentVolume(context.Context, string) (domain.PersistentVolumeDetail, error) {
	return r.volumeDetail, r.err
}
func (r *storageRepository) GetPersistentVolumeClaim(context.Context, string, string) (domain.PersistentVolumeClaimDetail, error) {
	return r.claimDetail, r.err
}
func (r *storageRepository) CreatePersistentVolumeClaim(_ context.Context, spec domain.PersistentVolumeClaimCreateSpec) (domain.PersistentVolumeClaimDetail, error) {
	r.createCalls++
	r.lastCreate = spec
	return r.claimDetail, r.err
}
func (r *storageRepository) UpdatePersistentVolumeClaim(_ context.Context, _, _ string, update domain.PersistentVolumeClaimUpdateSpec) (domain.PersistentVolumeClaimDetail, error) {
	r.updateCalls++
	r.lastUpdate = update
	return r.claimDetail, r.err
}
func (r *storageRepository) DeletePersistentVolumeClaim(_ context.Context, _, _ string, resourceVersion string) error {
	r.deleteCalls++
	r.lastDeleteRV = resourceVersion
	return r.err
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
	repo := &storageRepository{claimDetail: domain.PersistentVolumeClaimDetail{PersistentVolumeClaim: domain.PersistentVolumeClaim{Name: "data", Namespace: "prod"}}}
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
	if repo.createCalls != 1 || repo.lastCreate.Name != "data" || repo.lastCreate.StorageClass != "standard" || repo.lastCreate.AccessModes[0] != "ReadWriteOnce" {
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
	if repo.updateCalls != 1 || repo.lastUpdate.RequestedCapacityBytes != 2<<30 || repo.lastUpdate.ResourceVersion != "41" {
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
