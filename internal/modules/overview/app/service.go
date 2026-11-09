package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"dosh/internal/domain"

	"golang.org/x/sync/errgroup"
)

type Repository interface {
	Version(ctx context.Context) (string, error)
	MetricsAvailable(ctx context.Context) bool
	DetectCNI(ctx context.Context) string
	NodeOverview(ctx context.Context) (domain.NodeOverview, error)
	PodOverview(ctx context.Context) (domain.PodOverview, error)
	CountWorkloads(ctx context.Context) (int, error)
	CountNamespaces(ctx context.Context) (int, error)
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
		nodes      domain.NodeOverview
		pods       domain.PodOverview
		workloads  int
		namespaces int
		events     []domain.Event
	)
	var mu sync.Mutex
	warnings := make([]string, 0)
	coreFailures := make([]error, 0, 3)
	nodesLoaded := false
	podsLoaded := false
	workloadsLoaded := false
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
		value, err := s.repository.NodeOverview(groupCtx)
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "node data is unavailable")
			coreFailures = append(coreFailures, err)
			mu.Unlock()
			return nil
		}
		nodes = value
		nodesLoaded = true
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.PodOverview(groupCtx)
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "pod data is unavailable")
			coreFailures = append(coreFailures, err)
			mu.Unlock()
			return nil
		}
		pods = value
		podsLoaded = true
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.CountWorkloads(groupCtx)
		if err != nil {
			mu.Lock()
			warnings = append(warnings, "workload data is unavailable")
			coreFailures = append(coreFailures, err)
			mu.Unlock()
			return nil
		}
		workloads = value
		workloadsLoaded = true
		return nil
	})
	group.Go(func() error {
		value, err := s.repository.CountNamespaces(groupCtx)
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
	if !nodesLoaded && !podsLoaded && !workloadsLoaded {
		return domain.Overview{}, overviewUnavailableError(coreFailures)
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
	if nodes.Total == 0 || nodes.Ready == 0 {
		status = "Unavailable"
	} else if nodes.Ready < nodes.Total {
		status = "Degraded"
	}
	return domain.Overview{
		Cluster: domain.ClusterInfo{
			Name: s.clusterName, Version: version, CNI: s.repository.DetectCNI(ctx), Status: status,
			MetricsAvailable: s.repository.MetricsAvailable(ctx),
		},
		Counts: domain.OverviewCounts{
			Nodes: nodes.Total, ReadyNodes: nodes.Ready, Pods: pods.Total, RunningPods: pods.Running,
			Workloads: workloads, Namespaces: namespaces, UnhealthyPods: pods.Unhealthy, WarningEvents: warningEvents,
		},
		Capacity: nodes.Capacity,
		Events:   recent,
		Warnings: warnings,
	}, nil
}

func overviewUnavailableError(failures []error) error {
	for _, failure := range failures {
		if errors.Is(failure, domain.ErrForbidden) {
			return fmt.Errorf("%w: cluster data access was denied", domain.ErrForbidden)
		}
	}
	return fmt.Errorf("%w: no cluster data could be loaded", domain.ErrUnavailable)
}
