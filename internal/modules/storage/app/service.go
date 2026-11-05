package app

import (
	"context"

	"dosh/internal/domain"
)

type Repository interface {
	ListStorageClasses(ctx context.Context, query domain.ListQuery) ([]domain.StorageClass, error)
	ListPersistentVolumes(ctx context.Context, query domain.ListQuery) ([]domain.PersistentVolume, error)
	ListPersistentVolumeClaims(ctx context.Context, query domain.ListQuery) ([]domain.PersistentVolumeClaim, error)
	GetStorageClass(ctx context.Context, name string) (domain.StorageClassDetail, error)
	GetPersistentVolume(ctx context.Context, name string) (domain.PersistentVolumeDetail, error)
	GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (domain.PersistentVolumeClaimDetail, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) ListStorageClasses(ctx context.Context, query domain.ListQuery) ([]domain.StorageClass, error) {
	return s.repository.ListStorageClasses(ctx, query)
}

func (s *Service) ListPersistentVolumes(ctx context.Context, query domain.ListQuery) ([]domain.PersistentVolume, error) {
	return s.repository.ListPersistentVolumes(ctx, query)
}

func (s *Service) ListPersistentVolumeClaims(ctx context.Context, query domain.ListQuery) ([]domain.PersistentVolumeClaim, error) {
	return s.repository.ListPersistentVolumeClaims(ctx, query)
}

func (s *Service) GetStorageClass(ctx context.Context, name string) (domain.StorageClassDetail, error) {
	return s.repository.GetStorageClass(ctx, name)
}

func (s *Service) GetPersistentVolume(ctx context.Context, name string) (domain.PersistentVolumeDetail, error) {
	return s.repository.GetPersistentVolume(ctx, name)
}

func (s *Service) GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (domain.PersistentVolumeClaimDetail, error) {
	return s.repository.GetPersistentVolumeClaim(ctx, namespace, name)
}
