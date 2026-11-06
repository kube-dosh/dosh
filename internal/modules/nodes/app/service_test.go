package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type nodesRepository struct {
	nodes              []domain.NodeSummary
	nodesErr           error
	detail             domain.NodeDetail
	detailErr          error
	pods               []domain.PodSummary
	podsErr            error
	namespaces         []domain.Namespace
	nsErr              error
	namespace          domain.NamespaceDetail
	getNSErr           error
	scheduling         domain.NodeSummary
	schedulingErr      error
	setSchedulingCalls int
	lastUnschedulable  bool
	lastSchedulingName string
	labelDetail        domain.NodeDetail
	labelErr           error
	setLabelCalls      int
	lastLabelUpdate    domain.NodeLabelUpdate
	taintDetail        domain.NodeDetail
	taintErr           error
	setTaintCalls      int
	lastTaintUpdate    domain.NodeTaintUpdate
}

func (r *nodesRepository) ListNodes(context.Context, domain.ListQuery) ([]domain.NodeSummary, error) {
	return r.nodes, r.nodesErr
}
func (r *nodesRepository) GetNode(context.Context, string) (domain.NodeDetail, error) {
	return r.detail, r.detailErr
}
func (r *nodesRepository) SetNodeScheduling(_ context.Context, name string, unschedulable bool) (domain.NodeSummary, error) {
	r.setSchedulingCalls++
	r.lastSchedulingName = name
	r.lastUnschedulable = unschedulable
	return r.scheduling, r.schedulingErr
}
func (r *nodesRepository) SetNodeLabels(_ context.Context, _ string, update domain.NodeLabelUpdate) (domain.NodeDetail, error) {
	r.setLabelCalls++
	r.lastLabelUpdate = update
	return r.labelDetail, r.labelErr
}
func (r *nodesRepository) SetNodeTaints(_ context.Context, _ string, update domain.NodeTaintUpdate) (domain.NodeDetail, error) {
	r.setTaintCalls++
	r.lastTaintUpdate = update
	return r.taintDetail, r.taintErr
}
func (r *nodesRepository) ListPodsForNode(context.Context, string) ([]domain.PodSummary, error) {
	return r.pods, r.podsErr
}
func (r *nodesRepository) ListNamespaces(context.Context, domain.ListQuery) ([]domain.Namespace, error) {
	return r.namespaces, r.nsErr
}
func (r *nodesRepository) GetNamespace(context.Context, string) (domain.NamespaceDetail, error) {
	return r.namespace, r.getNSErr
}

type nodesAuthorizer struct {
	allow  bool
	err    error
	access domain.Access
}

func (a *nodesAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.access = access
	if a.err != nil {
		return a.err
	}
	if !a.allow {
		return domain.ErrForbidden
	}
	return nil
}

type nodesAuditor struct{ events []domain.AuditEvent }

func (a *nodesAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func TestNodesServiceListsAndLoadsDetail(t *testing.T) {
	repo := &nodesRepository{
		nodes:      []domain.NodeSummary{{Name: "worker-a"}},
		detail:     domain.NodeDetail{NodeSummary: domain.NodeSummary{Name: "worker-a"}},
		pods:       []domain.PodSummary{{Name: "api-1"}},
		namespaces: []domain.Namespace{{Name: "default"}},
		namespace:  domain.NamespaceDetail{Namespace: domain.Namespace{Name: "prod"}},
	}
	service := New(repo, &nodesAuthorizer{allow: true}, &nodesAuditor{})
	nodes, err := service.List(context.Background(), domain.ListQuery{Search: "worker"})
	if err != nil || len(nodes) != 1 || nodes[0].Name != "worker-a" {
		t.Fatalf("List() = (%#v, %v)", nodes, err)
	}
	detail, err := service.Get(context.Background(), "worker-a")
	if err != nil || detail.Name != "worker-a" || len(detail.Pods) != 1 || detail.Pods[0].Name != "api-1" {
		t.Fatalf("Get() = (%#v, %v)", detail, err)
	}
	namespaces, err := service.ListNamespaces(context.Background(), domain.ListQuery{})
	if err != nil || len(namespaces) != 1 {
		t.Fatalf("ListNamespaces() = (%#v, %v)", namespaces, err)
	}
	namespace, err := service.GetNamespace(context.Background(), "prod")
	if err != nil || namespace.Name != "prod" {
		t.Fatalf("GetNamespace() = (%#v, %v)", namespace, err)
	}
}

func TestNodesServicePropagatesDependencyFailures(t *testing.T) {
	dependencyErr := errors.New("kubernetes unavailable")
	service := New(&nodesRepository{detailErr: dependencyErr}, &nodesAuthorizer{allow: true}, &nodesAuditor{})
	if _, err := service.Get(context.Background(), "worker-a"); !errors.Is(err, dependencyErr) {
		t.Fatalf("Get() node error = %v", err)
	}

	service = New(&nodesRepository{detail: domain.NodeDetail{NodeSummary: domain.NodeSummary{Name: "worker-a"}}, podsErr: dependencyErr}, &nodesAuthorizer{allow: true}, &nodesAuditor{})
	if _, err := service.Get(context.Background(), "worker-a"); !errors.Is(err, dependencyErr) {
		t.Fatalf("Get() pod error = %v", err)
	}

	service = New(&nodesRepository{nodesErr: dependencyErr}, &nodesAuthorizer{allow: true}, &nodesAuditor{})
	if _, err := service.List(context.Background(), domain.ListQuery{}); !errors.Is(err, dependencyErr) {
		t.Fatalf("List() error = %v", err)
	}
}

func TestNodesServiceCordonAndUncordonRequirePermissionAndAudit(t *testing.T) {
	t.Parallel()
	repo := &nodesRepository{scheduling: domain.NodeSummary{Name: "worker-a", Unschedulable: true}}
	authorizer := &nodesAuthorizer{}
	auditor := &nodesAuditor{}
	service := New(repo, authorizer, auditor)

	if _, err := service.Cordon(context.Background(), domain.Actor{Username: "viewer"}, "worker-a"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Cordon() error = %v, want ErrForbidden", err)
	}
	if repo.setSchedulingCalls != 0 {
		t.Fatalf("repository called without permission: %d", repo.setSchedulingCalls)
	}
	if authorizer.access != (domain.Access{Verb: "update", Resource: "nodes", Name: "worker-a"}) {
		t.Fatalf("access = %#v", authorizer.access)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "cordon" || auditor.events[0].Resource != "node" || auditor.events[0].Result != "denied" {
		t.Fatalf("denied audit = %#v", auditor.events)
	}

	authorizer.allow = true
	summary, err := service.Cordon(context.Background(), domain.Actor{Username: "admin"}, "worker-a")
	if err != nil || !summary.Unschedulable || repo.setSchedulingCalls != 1 || !repo.lastUnschedulable || repo.lastSchedulingName != "worker-a" {
		t.Fatalf("Cordon() = (%#v, %v), calls = %d, last = %s/%t", summary, err, repo.setSchedulingCalls, repo.lastSchedulingName, repo.lastUnschedulable)
	}
	if len(auditor.events) != 2 || auditor.events[1].Action != "cordon" || auditor.events[1].Result != "success" {
		t.Fatalf("cordon audit = %#v", auditor.events)
	}

	repo.scheduling = domain.NodeSummary{Name: "worker-a", Unschedulable: false}
	summary, err = service.Uncordon(context.Background(), domain.Actor{Username: "admin"}, "worker-a")
	if err != nil || summary.Unschedulable || repo.setSchedulingCalls != 2 || repo.lastUnschedulable {
		t.Fatalf("Uncordon() = (%#v, %v), calls = %d, last = %t", summary, err, repo.setSchedulingCalls, repo.lastUnschedulable)
	}
	if len(auditor.events) != 3 || auditor.events[2].Action != "uncordon" || auditor.events[2].Result != "success" {
		t.Fatalf("uncordon audit = %#v", auditor.events)
	}
}

func TestNodesServiceAuditsSchedulingFailures(t *testing.T) {
	t.Parallel()
	repo := &nodesRepository{schedulingErr: domain.ErrUnavailable}
	auditor := &nodesAuditor{}
	service := New(repo, &nodesAuthorizer{allow: true}, auditor)
	if _, err := service.Cordon(context.Background(), domain.Actor{Username: "admin"}, "worker-a"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Cordon() error = %v, want ErrUnavailable", err)
	}
	if len(auditor.events) != 1 || auditor.events[0].Result != "failure" || !errors.Is(auditor.events[0].Err, domain.ErrUnavailable) {
		t.Fatalf("failure audit = %#v", auditor.events)
	}
}

func TestNodesServiceUpdatesLabelsAndTaintsWithValidationAndAudit(t *testing.T) {
	t.Parallel()
	repo := &nodesRepository{
		labelDetail: domain.NodeDetail{NodeSummary: domain.NodeSummary{Name: "worker-a"}, Labels: map[string]string{"team": "platform"}},
		taintDetail: domain.NodeDetail{NodeSummary: domain.NodeSummary{Name: "worker-a"}, Taints: []domain.Taint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}}},
	}
	authorizer := &nodesAuthorizer{allow: true}
	auditor := &nodesAuditor{}
	service := New(repo, authorizer, auditor)

	detail, err := service.SetLabels(context.Background(), domain.Actor{Username: "admin"}, "worker-a", domain.NodeLabelUpdate{
		Labels: map[string]string{" team ": " platform ", "topology.kubernetes.io/zone": "az-1"}, ResourceVersion: "9",
	})
	if err != nil || detail.Labels["team"] != "platform" || repo.setLabelCalls != 1 {
		t.Fatalf("SetLabels() = (%#v, %v), calls = %d", detail, err, repo.setLabelCalls)
	}
	if repo.lastLabelUpdate.ResourceVersion != "9" || repo.lastLabelUpdate.Labels["team"] != "platform" || repo.lastLabelUpdate.Labels["topology.kubernetes.io/zone"] != "az-1" {
		t.Fatalf("label update = %#v", repo.lastLabelUpdate)
	}
	if authorizer.access != (domain.Access{Verb: "update", Resource: "nodes", Name: "worker-a"}) {
		t.Fatalf("access = %#v", authorizer.access)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "update-labels" || auditor.events[0].Result != "success" {
		t.Fatalf("label audit = %#v", auditor.events)
	}

	detail, err = service.SetTaints(context.Background(), domain.Actor{Username: "admin"}, "worker-a", domain.NodeTaintUpdate{
		Taints: []domain.Taint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}}, ResourceVersion: "10",
	})
	if err != nil || len(detail.Taints) != 1 || repo.setTaintCalls != 1 {
		t.Fatalf("SetTaints() = (%#v, %v), calls = %d", detail, err, repo.setTaintCalls)
	}
	if repo.lastTaintUpdate.ResourceVersion != "10" || len(repo.lastTaintUpdate.Taints) != 1 {
		t.Fatalf("taint update = %#v", repo.lastTaintUpdate)
	}
	if len(auditor.events) != 2 || auditor.events[1].Action != "update-taints" || auditor.events[1].Result != "success" {
		t.Fatalf("taint audit = %#v", auditor.events)
	}
}

func TestNodesServiceRejectsInvalidMetadataBeforeRepositoryWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		apply func(*Service, *nodesRepository)
	}{
		{name: "invalid label key", field: "labels", apply: func(service *Service, _ *nodesRepository) {
			_, _ = service.SetLabels(context.Background(), domain.Actor{Username: "admin"}, "worker-a", domain.NodeLabelUpdate{Labels: map[string]string{"bad key": "value"}})
		}},
		{name: "invalid label value", field: "labels", apply: func(service *Service, _ *nodesRepository) {
			_, _ = service.SetLabels(context.Background(), domain.Actor{Username: "admin"}, "worker-a", domain.NodeLabelUpdate{Labels: map[string]string{"team": "bad value"}})
		}},
		{name: "invalid taint effect", field: "taints", apply: func(service *Service, _ *nodesRepository) {
			_, _ = service.SetTaints(context.Background(), domain.Actor{Username: "admin"}, "worker-a", domain.NodeTaintUpdate{Taints: []domain.Taint{{Key: "dedicated", Effect: "Block"}}})
		}},
		{name: "duplicate taint", field: "taints", apply: func(service *Service, _ *nodesRepository) {
			_, _ = service.SetTaints(context.Background(), domain.Actor{Username: "admin"}, "worker-a", domain.NodeTaintUpdate{Taints: []domain.Taint{{Key: "dedicated", Effect: "NoSchedule"}, {Key: "dedicated", Effect: "NoSchedule"}}})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &nodesRepository{}
			auditor := &nodesAuditor{}
			service := New(repo, &nodesAuthorizer{allow: true}, auditor)
			test.apply(service, repo)
			if repo.setLabelCalls != 0 || repo.setTaintCalls != 0 {
				t.Fatalf("repository write occurred for invalid input: labels=%d taints=%d", repo.setLabelCalls, repo.setTaintCalls)
			}
			if len(auditor.events) != 1 || auditor.events[0].Result != "failure" {
				t.Fatalf("validation audit = %#v", auditor.events)
			}
			var validation *domain.ValidationError
			if !errors.As(auditor.events[0].Err, &validation) || validation.Field != test.field {
				t.Fatalf("validation error = %#v", auditor.events[0].Err)
			}
		})
	}
}

func TestNodesServiceMetadataUpdatesRequirePermission(t *testing.T) {
	t.Parallel()
	repo := &nodesRepository{}
	authorizer := &nodesAuthorizer{}
	auditor := &nodesAuditor{}
	service := New(repo, authorizer, auditor)

	if _, err := service.SetLabels(context.Background(), domain.Actor{Username: "viewer"}, "worker-a", domain.NodeLabelUpdate{Labels: map[string]string{"team": "platform"}}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("SetLabels() error = %v", err)
	}
	if _, err := service.SetTaints(context.Background(), domain.Actor{Username: "viewer"}, "worker-a", domain.NodeTaintUpdate{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("SetTaints() error = %v", err)
	}
	if repo.setLabelCalls != 0 || repo.setTaintCalls != 0 {
		t.Fatalf("repository called without permission: labels=%d taints=%d", repo.setLabelCalls, repo.setTaintCalls)
	}
	if len(auditor.events) != 2 || auditor.events[0].Action != "update-labels" || auditor.events[1].Action != "update-taints" || auditor.events[0].Result != "denied" || auditor.events[1].Result != "denied" {
		t.Fatalf("denied audit = %#v", auditor.events)
	}
}
