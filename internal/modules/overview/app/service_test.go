package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

type overviewRepository struct {
	version       string
	versionErr    error
	metrics       bool
	cni           string
	nodes         []domain.NodeSummary
	nodesErr      error
	pods          []domain.PodSummary
	podsErr       error
	workloads     []domain.WorkloadSummary
	workloadsErr  error
	namespaces    []domain.Namespace
	namespacesErr error
	events        []domain.Event
	eventsErr     error
}

func (r *overviewRepository) Version(context.Context) (string, error) { return r.version, r.versionErr }
func (r *overviewRepository) MetricsAvailable(context.Context) bool   { return r.metrics }
func (r *overviewRepository) DetectCNI(context.Context) string        { return r.cni }
func (r *overviewRepository) ListNodes(context.Context, domain.ListQuery) ([]domain.NodeSummary, error) {
	return r.nodes, r.nodesErr
}
func (r *overviewRepository) ListPods(context.Context, domain.ListQuery) ([]domain.PodSummary, error) {
	return r.pods, r.podsErr
}
func (r *overviewRepository) ListWorkloads(context.Context, domain.ListQuery) ([]domain.WorkloadSummary, error) {
	return r.workloads, r.workloadsErr
}
func (r *overviewRepository) ListNamespaces(context.Context, domain.ListQuery) ([]domain.Namespace, error) {
	return r.namespaces, r.namespacesErr
}
func (r *overviewRepository) ListEvents(context.Context, domain.ListQuery) ([]domain.Event, error) {
	return r.events, r.eventsErr
}

func TestOverviewAggregatesClusterState(t *testing.T) {
	events := make([]domain.Event, 0, 10)
	for i := 0; i < 9; i++ {
		eventType := "Normal"
		if i%3 == 0 {
			eventType = "Warning"
		}
		events = append(events, domain.Event{Type: eventType, Reason: "reason-" + strings.Repeat("x", i+1), LastSeen: time.Unix(int64(i), 0)})
	}
	repo := &overviewRepository{
		version: "v1.36.5", metrics: true, cni: "Cilium",
		nodes: []domain.NodeSummary{
			{Name: "worker-a", Status: "Ready", CPUCapacityMilli: 2000, MemoryCapacityBytes: 4096, PodCapacity: 110, CPURequestsMilli: 500, MemoryRequestsBytes: 1024, CPUUsageMilli: 250, MemoryUsageBytes: 512},
			{Name: "worker-b", Status: "NotReady", CPUCapacityMilli: 4000, MemoryCapacityBytes: 8192, PodCapacity: 220, CPURequestsMilli: 1000, MemoryRequestsBytes: 2048, CPUUsageMilli: 500, MemoryUsageBytes: 1024},
		},
		pods: []domain.PodSummary{
			{Name: "running", Status: "Running"},
			{Name: "pending", Status: "Pending"},
			{Name: "complete", Status: "Succeeded"},
			{Name: "terminating", Status: "Terminating"},
		},
		workloads:  []domain.WorkloadSummary{{Name: "api"}},
		namespaces: []domain.Namespace{{Name: "default"}, {Name: "prod"}},
		events:     events,
	}

	overview, err := New(repo, "cluster-a").Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if overview.Cluster.Name != "cluster-a" || overview.Cluster.Version != "v1.36.5" || overview.Cluster.CNI != "Cilium" || !overview.Cluster.MetricsAvailable {
		t.Fatalf("cluster = %#v", overview.Cluster)
	}
	if overview.Cluster.Status != "Degraded" {
		t.Fatalf("status = %q, want Degraded", overview.Cluster.Status)
	}
	wantCounts := domain.OverviewCounts{Nodes: 2, ReadyNodes: 1, Pods: 4, RunningPods: 1, Workloads: 1, Namespaces: 2, UnhealthyPods: 1, WarningEvents: 3}
	if overview.Counts != wantCounts {
		t.Fatalf("counts = %#v, want %#v", overview.Counts, wantCounts)
	}
	wantCapacity := domain.ClusterCapacity{
		CPUAllocatableMilli: 6000, MemoryAllocatableBytes: 12288, PodCapacity: 330,
		CPURequestsMilli: 1500, MemoryRequestsBytes: 3072, CPUUsageMilli: 750, MemoryUsageBytes: 1536,
	}
	if overview.Capacity != wantCapacity {
		t.Fatalf("capacity = %#v, want %#v", overview.Capacity, wantCapacity)
	}
	if len(overview.Events) != 8 {
		t.Fatalf("recent events = %d, want 8", len(overview.Events))
	}
	if len(overview.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", overview.Warnings)
	}
}

func TestOverviewReturnsPartialWarnings(t *testing.T) {
	repo := &overviewRepository{
		versionErr: errors.New("version unavailable"), nodes: []domain.NodeSummary{{Name: "worker-a", Status: "Ready"}},
		podsErr: errors.New("pods unavailable"), workloadsErr: errors.New("workloads unavailable"),
		namespacesErr: errors.New("namespaces unavailable"), eventsErr: errors.New("events unavailable"),
	}
	overview, err := New(repo, "cluster-a").Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if overview.Cluster.Status != "Healthy" {
		t.Fatalf("status = %q, want Healthy", overview.Cluster.Status)
	}
	if len(overview.Warnings) != 5 {
		t.Fatalf("warnings = %#v, want 5 entries", overview.Warnings)
	}
}

func TestOverviewFailsClosedWhenNoCoreDataLoads(t *testing.T) {
	repo := &overviewRepository{
		versionErr: errors.New("version"), nodesErr: errors.New("nodes"), podsErr: errors.New("pods"), workloadsErr: errors.New("workloads"),
	}
	_, err := New(repo, "cluster-a").Get(context.Background())
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Get() error = %v, want ErrUnavailable", err)
	}
}

func TestOverviewReturnsForbiddenWhenCoreDataAccessIsDenied(t *testing.T) {
	repo := &overviewRepository{
		versionErr:   errors.New("version"),
		nodesErr:     domain.ErrForbidden,
		podsErr:      fmt.Errorf("%w: pods are forbidden", domain.ErrForbidden),
		workloadsErr: domain.ErrForbidden,
	}
	_, err := New(repo, "cluster-a").Get(context.Background())
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Get() error = %v, want ErrForbidden", err)
	}
	if errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Get() error = %v, must not also match ErrUnavailable", err)
	}
}

func TestOverviewKeepsPartialDataWhenAnotherCoreSourceIsForbidden(t *testing.T) {
	repo := &overviewRepository{
		nodes:     []domain.NodeSummary{{Name: "worker-a", Status: "Ready"}},
		podsErr:   domain.ErrForbidden,
		workloads: []domain.WorkloadSummary{{Name: "api"}},
	}
	overview, err := New(repo, "cluster-a").Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if overview.Cluster.Status != "Healthy" {
		t.Fatalf("status = %q, want Healthy", overview.Cluster.Status)
	}
	if overview.Counts.Nodes != 1 || overview.Counts.Workloads != 1 {
		t.Fatalf("counts = %#v", overview.Counts)
	}
	if len(overview.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one entry", overview.Warnings)
	}
}

func TestOverviewReturnsHealthyEmptyClusterWhenCoreSourcesSucceed(t *testing.T) {
	repo := &overviewRepository{
		nodes: []domain.NodeSummary{{Name: "worker-a", Status: "Ready"}},
		pods:  []domain.PodSummary{},
	}
	overview, err := New(repo, "cluster-a").Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if overview.Cluster.Status != "Healthy" {
		t.Fatalf("status = %q, want Healthy", overview.Cluster.Status)
	}
	if overview.Counts.Pods != 0 {
		t.Fatalf("pods = %d, want 0", overview.Counts.Pods)
	}
}
