package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
)

type diagnosticRepository struct {
	pod              domain.PodDetail
	workload         domain.WorkloadDetail
	service          domain.ServiceDetail
	ingress          domain.IngressDetail
	pvc              domain.PersistentVolumeClaimDetail
	node             domain.NodeDetail
	selectorPods     []domain.PodSummary
	events           []domain.Event
	logs             string
	podCalls         int
	logCalls         int
	podsSelectorCall int
}

func (r *diagnosticRepository) GetPod(context.Context, string, string) (domain.PodDetail, error) {
	r.podCalls++
	return r.pod, nil
}

func (r *diagnosticRepository) GetWorkload(context.Context, string, string, string) (domain.WorkloadDetail, error) {
	return r.workload, nil
}

func (r *diagnosticRepository) GetService(context.Context, string, string) (domain.ServiceDetail, error) {
	return r.service, nil
}

func (r *diagnosticRepository) GetIngress(context.Context, string, string) (domain.IngressDetail, error) {
	return r.ingress, nil
}

func (r *diagnosticRepository) GetPersistentVolumeClaim(context.Context, string, string) (domain.PersistentVolumeClaimDetail, error) {
	return r.pvc, nil
}

func (r *diagnosticRepository) GetNode(context.Context, string) (domain.NodeDetail, error) {
	return r.node, nil
}

func (r *diagnosticRepository) ListPodsBySelector(context.Context, string, map[string]string, string) ([]domain.PodSummary, error) {
	r.podsSelectorCall++
	return r.selectorPods, nil
}

func (r *diagnosticRepository) ListEvents(context.Context, domain.ListQuery) ([]domain.Event, error) {
	return r.events, nil
}

func (r *diagnosticRepository) StreamPodLogs(context.Context, string, string, string, bool, bool, int64, bool, int64, int64) (io.ReadCloser, error) {
	r.logCalls++
	return io.NopCloser(strings.NewReader(r.logs)), nil
}

type recordingAuditor struct {
	events []domain.AuditEvent
}

func (a *recordingAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

type denyLogAuthorizer struct {
	base domain.Authorizer
}

func (a denyLogAuthorizer) Require(ctx context.Context, actor domain.Actor, access domain.Access) error {
	if access.Subresource == "log" {
		return domain.ErrForbidden
	}
	return a.base.Require(ctx, actor, access)
}

type denyAllAuthorizer struct {
	calls int
}

func (a *denyAllAuthorizer) Require(context.Context, domain.Actor, domain.Access) error {
	a.calls++
	return domain.ErrForbidden
}

func newDiagnosticService(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	service := New(repository, authorizer, auditor)
	service.now = func() time.Time { return time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC) }
	return service
}

func TestDiagnosePodFindsCrashLoopOOMAndLogPatterns(t *testing.T) {
	repository := &diagnosticRepository{
		pod: domain.PodDetail{
			PodSummary: domain.PodSummary{Name: "web-1", Namespace: "default", Status: "Running", Ready: "0/1", Restarts: 3},
			Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "ContainersNotReady", Message: "containers with unready status: [web]"}},
			Containers: []domain.Container{{
				Name: "web", Image: "example/web:1", State: "Waiting", Reason: "CrashLoopBackOff", LastState: "Terminated",
				LastReason: "OOMKilled", LastExitCode: 137, RestartCount: 3, MemoryLimitBytes: 128 * 1024 * 1024,
			}},
		},
		logs: "2026-11-17T07:59:00Z fatal: out of memory while handling request\n",
		events: []domain.Event{{
			Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container", Count: 4, LastSeen: time.Now(),
		}},
	}
	auditor := &recordingAuditor{}
	service := newDiagnosticService(repository, authz.NewLocal(false), auditor)

	report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "pod", "default", "web-1")
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if report.Severity != domain.DiagnosisCritical {
		t.Fatalf("severity = %q, want critical", report.Severity)
	}
	for _, id := range []string{"pod.web.crashloop", "pod.web.oom", "logs.out-of-memory"} {
		if !hasFinding(report, id) {
			t.Errorf("missing finding %q; got %#v", id, report.Findings)
		}
	}
	if countChecks(report, "logs") != 1 {
		t.Errorf("logs checks = %d, want 1", countChecks(report, "logs"))
	}
	if repository.logCalls != 1 {
		t.Errorf("log calls = %d, want 1", repository.logCalls)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "diagnose" || auditor.events[0].Result != "success" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestDiagnosePodSkipsLogsWhenForbidden(t *testing.T) {
	repository := &diagnosticRepository{
		pod: domain.PodDetail{
			PodSummary: domain.PodSummary{Name: "web-1", Namespace: "default", Status: "Running", Ready: "0/1", Restarts: 2},
			Containers: []domain.Container{{Name: "web", State: "Waiting", Reason: "CrashLoopBackOff", RestartCount: 2}},
		},
	}
	service := newDiagnosticService(repository, denyLogAuthorizer{base: authz.NewLocal(false)}, &recordingAuditor{})

	report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "pod", "default", "web-1")
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if status := checkStatus(report, "logs"); status != domain.DiagnosisCheckSkipped {
		t.Fatalf("logs check status = %q, want skipped", status)
	}
	if repository.logCalls != 0 {
		t.Fatalf("log calls = %d, want 0", repository.logCalls)
	}
	if len(report.Warnings) == 0 {
		t.Fatal("expected an RBAC warning")
	}
}

func TestDiagnoseWorkloadReportsReadyDeficitAndContainerFindings(t *testing.T) {
	pod := domain.PodSummary{Name: "web-1", Namespace: "default", Status: "Pending", Ready: "0/1"}
	repository := &diagnosticRepository{
		workload: domain.WorkloadDetail{
			WorkloadSummary: domain.WorkloadSummary{Kind: "Deployment", Name: "web", Namespace: "default", Status: "Pending", Ready: "0/2", Desired: 2},
			Selector:        map[string]string{"app": "web"},
			Pods:            []domain.PodSummary{pod},
		},
		pod: domain.PodDetail{
			PodSummary: pod,
			Containers: []domain.Container{{Name: "web", Image: "example/web:1", State: "Waiting", Reason: "ImagePullBackOff"}},
		},
	}
	service := newDiagnosticService(repository, authz.NewLocal(false), &recordingAuditor{})
	report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "deployment", "default", "web")
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	for _, id := range []string{"workload.availability", "workload.web-1.web.imagepull"} {
		if !hasFinding(report, id) {
			t.Errorf("missing finding %q; got %#v", id, report.Findings)
		}
	}
}

func TestDiagnoseServiceReportsMissingEndpoints(t *testing.T) {
	repository := &diagnosticRepository{
		service: domain.ServiceDetail{
			ServiceSummary: domain.ServiceSummary{
				Name: "web", Namespace: "default", Type: "ClusterIP", Headless: false, EndpointCount: 0,
				Ports: []domain.ServicePort{{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "8080"}}, Selector: map[string]string{"app": "web"},
			},
		},
		selectorPods: []domain.PodSummary{{Name: "web-1", Namespace: "default", Status: "Running", Ready: "1/1"}},
	}
	service := newDiagnosticService(repository, authz.NewLocal(false), &recordingAuditor{})
	report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "service", "default", "web")
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if !hasFinding(report, "service.no-endpoints") || report.Severity != domain.DiagnosisCritical {
		t.Fatalf("unexpected service diagnosis: %#v", report)
	}
	if repository.podsSelectorCall != 1 {
		t.Fatalf("selector calls = %d, want 1", repository.podsSelectorCall)
	}
}

func TestDiagnoseIngressReportsMissingService(t *testing.T) {
	repository := &diagnosticRepository{ingress: domain.IngressDetail{
		IngressSummary: domain.IngressSummary{Name: "web", Namespace: "default", Class: "nginx", Paths: []domain.IngressPath{{
			Host: "web.example.test", Path: "/", Service: "missing", Port: "80", TargetStatus: domain.IngressTargetServiceNotFound,
		}}},
	}}
	service := newDiagnosticService(repository, authz.NewLocal(false), &recordingAuditor{})
	report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "ingress", "default", "web")
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if !hasFinding(report, "ingress.backend-unavailable") {
		t.Fatalf("missing ingress backend finding: %#v", report.Findings)
	}
}

func TestDiagnosePVCPendingAndNodePressure(t *testing.T) {
	t.Run("pvc", func(t *testing.T) {
		repository := &diagnosticRepository{pvc: domain.PersistentVolumeClaimDetail{
			PersistentVolumeClaim:  domain.PersistentVolumeClaim{Name: "data", Namespace: "default", Status: "Pending", StorageClass: "missing"},
			RequestedCapacityBytes: 1024,
		}}
		service := newDiagnosticService(repository, authz.NewLocal(false), &recordingAuditor{})
		report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "pvc", "default", "data")
		if err != nil {
			t.Fatalf("Diagnose() error = %v", err)
		}
		if !hasFinding(report, "pvc.pending") {
			t.Fatalf("missing pvc pending finding: %#v", report.Findings)
		}
	})
	t.Run("node", func(t *testing.T) {
		repository := &diagnosticRepository{node: domain.NodeDetail{
			NodeSummary: domain.NodeSummary{Name: "worker-1", Status: "Ready", PodCapacity: 110},
			Conditions:  []domain.Condition{{Type: "MemoryPressure", Status: "True", Reason: "KubeletHasInsufficientMemory", Message: "memory pressure"}},
		}}
		service := newDiagnosticService(repository, authz.NewLocal(false), &recordingAuditor{})
		report, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "node", "", "worker-1")
		if err != nil {
			t.Fatalf("Diagnose() error = %v", err)
		}
		if !hasFinding(report, "node.condition.memorypressure") {
			t.Fatalf("missing node pressure finding: %#v", report.Findings)
		}
	})
}

func TestDiagnoseRejectsInvalidKindAndDeniedTarget(t *testing.T) {
	t.Run("invalid kind", func(t *testing.T) {
		repository := &diagnosticRepository{}
		service := newDiagnosticService(repository, authz.NewLocal(false), &recordingAuditor{})
		if _, err := service.Diagnose(context.Background(), domain.Actor{Username: "admin", Admin: true}, "secret", "default", "web"); !errors.Is(err, domain.ErrUnsupported) {
			t.Fatalf("error = %v, want unsupported", err)
		}
		if repository.podCalls != 0 {
			t.Fatalf("repository calls = %d, want 0", repository.podCalls)
		}
	})
	t.Run("denied target", func(t *testing.T) {
		repository := &diagnosticRepository{}
		authorizer := &denyAllAuthorizer{}
		auditor := &recordingAuditor{}
		service := newDiagnosticService(repository, authorizer, auditor)
		_, err := service.Diagnose(context.Background(), domain.Actor{Username: "viewer"}, "pod", "default", "web")
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("error = %v, want forbidden", err)
		}
		if repository.podCalls != 0 {
			t.Fatalf("repository calls = %d, want 0", repository.podCalls)
		}
		if len(auditor.events) != 1 || auditor.events[0].Result != "denied" {
			t.Fatalf("audit events = %#v", auditor.events)
		}
	})
}

func hasFinding(report domain.Diagnosis, id string) bool {
	for _, finding := range report.Findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func countChecks(report domain.Diagnosis, id string) int {
	count := 0
	for _, check := range report.Checks {
		if check.ID == id {
			count++
		}
	}
	return count
}

func checkStatus(report domain.Diagnosis, id string) string {
	for _, check := range report.Checks {
		if check.ID == id {
			return check.Status
		}
	}
	return ""
}
