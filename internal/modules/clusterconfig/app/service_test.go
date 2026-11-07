package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type repository struct {
	configuration domain.ClusterConfiguration
	err           error
	calls         int
}

func (r *repository) GetClusterConfiguration(context.Context) (domain.ClusterConfiguration, error) {
	r.calls++
	return r.configuration, r.err
}

func TestGetReturnsClusterConfiguration(t *testing.T) {
	want := domain.ClusterConfiguration{Name: "cluster-a", PodCIDR: "10.244.0.0/16"}
	repo := &repository{configuration: want}
	got, err := New(repo).Get(context.Background())
	if err != nil || got.Name != want.Name || got.PodCIDR != want.PodCIDR || repo.calls != 1 {
		t.Fatalf("Get() = (%#v, %v), calls = %d", got, err, repo.calls)
	}
}

func TestGetPreservesRepositoryError(t *testing.T) {
	repo := &repository{err: errors.New("unavailable")}
	if _, err := New(repo).Get(context.Background()); !errors.Is(err, repo.err) {
		t.Fatalf("Get() error = %v", err)
	}
}
