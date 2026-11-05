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

func TestStorageServiceListsResourcesAndFailures(t *testing.T) {
	repo := &storageRepository{
		classes: []domain.StorageClass{{Name: "standard"}},
		volumes: []domain.PersistentVolume{{Name: "pv-a"}},
		claims:  []domain.PersistentVolumeClaim{{Name: "pvc-a"}},
	}
	service := New(repo)
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
