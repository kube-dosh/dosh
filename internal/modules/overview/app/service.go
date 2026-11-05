package app

import (
	"context"
	"fmt"
	"sync"

	"dosh/internal/domain"

	"golang.org/x/sync/errgroup"
)

type Repository interface {
	Version(ctx context.Context) (string, error)
	MetricsAvailable(ctx context.Context) bool
	DetectCNI(ctx context.Context) string
	ListNodes(ctx context.Context, query domain.ListQuery) ([]domain.NodeSummary, error)
	ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error)
	ListWorkloads(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error)
	ListNamespaces(ctx context.Context, query domain.ListQuery) ([]domain.Namespace, error)
	ListEvents(ctx context.Context, query domain.ListQuery) ([]domain.Event, error)
}

type Service struct {
	repository  Repository
	clusterName string
}

func New(repository Repository, clusterName string) *Service {
	return &Service{repository: repository, clusterName: clusterName}
}

func (s *Service) Get(ctx context.Context) (domain.Overview, error) {
	var (
		version    string
		nodes      []domain.NodeSummary
		pods       []domain.PodSummary
		workloads  []domain.WorkloadSummary
		namespaces []domain.Namespace
		events     []domain.Event
	)
	var mu sync.Mutex
	warnings := make([]string, 0)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		value, err := s.repository.Version(groupCtx)
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "Kubernetes version is unavailable")
			mu.Unlock()
			return nil
		}
		version = value
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.ListNodes(groupCtx, domain.ListQuery{})
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "node data is unavailable")
			mu.Unlock()
			return nil
		}
		nodes = value
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.ListPods(groupCtx, domain.ListQuery{})
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "pod data is unavailable")
			mu.Unlock()
			return nil
		}
		pods = value
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.ListWorkloads(groupCtx, domain.ListQuery{})
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "workload data is unavailable")
			mu.Unlock()
			return nil
		}
		workloads = value
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.ListNamespaces(groupCtx, domain.ListQuery{})
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "namespace data is unavailable")
			mu.Unlock()
			return nil
		}
		namespaces = value
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.ListEvents(groupCtx, domain.ListQuery{})
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "event data is unavailable")
			mu.Unlock()
			return nil
		}
		events = value
		return nil
	})
	_ = group.Wait()
	if len(nodes) == 0 && len(pods) == 0 && len(workloads) == 0 {
		return domain.Overview{}, fmt.Errorf("%w: no cluster data could be loaded", domain.ErrUnavailable)
	}
	readyNodes := 0
	var capacity domain.ClusterCapacity
	for _, node := range nodes {
		if node.Status == "Ready" {
			readyNodes++
		}
		capacity.CPUAllocatableMilli += node.CPUCapacityMilli
		capacity.MemoryAllocatableBytes += node.MemoryCapacityBytes
		capacity.PodCapacity += node.PodCapacity
		capacity.CPURequestsMilli += node.CPURequestsMilli
		capacity.MemoryRequestsBytes += node.MemoryRequestsBytes
		capacity.CPUUsageMilli += node.CPUUsageMilli
		capacity.MemoryUsageBytes += node.MemoryUsageBytes
	}
	runningPods := 0
	unhealthyPods := 0
	for _, pod := range pods {
		switch pod.Status {
		case "Running":
			runningPods++
		case "Succeeded", "Completed", "Terminating":
		default:
			unhealthyPods++
		}
	}
	warningEvents := 0
	for _, event := range events {
		if event.Type == "Warning" {
			warningEvents++
		}
	}
	recent := make([]domain.Event, 0, len(events))
	recent = append(recent, events...)
	if len(recent) > 8 {
		recent = recent[:8]
	}
	status := "Healthy"
	if len(nodes) == 0 || readyNodes == 0 {
		status = "Unavailable"
	} else if readyNodes < len(nodes) {
		status = "Degraded"
	}
	return domain.Overview{
		Cluster: domain.ClusterInfo{
			Name: s.clusterName, Version: version, CNI: s.repository.DetectCNI(ctx), Status: status,
			MetricsAvailable: s.repository.MetricsAvailable(ctx),
		},
		Counts: domain.OverviewCounts{
			Nodes: len(nodes), ReadyNodes: readyNodes, Pods: len(pods), RunningPods: runningPods,
			Workloads: len(workloads), Namespaces: len(namespaces), UnhealthyPods: unhealthyPods, WarningEvents: warningEvents,
		},
		Capacity: capacity,
		Events:   recent,
		Warnings: warnings,
	}, nil
}
