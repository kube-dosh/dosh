package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

type fakeRepository struct {
	scaleCalls     int
	scaleErr       error
	deletePolicy   domain.DeletionPropagation
	deleteErr      error
	tailLines      int64
	limitBytes     int64
	previous       bool
	sinceSeconds   int64
	lastLogStream  io.ReadCloser
	podLogCalls    []string
	podLogErrors   map[string]error
	createCalls    []bool
	lastCreateSpec domain.WorkloadCreateSpec
	previewCalls   int
	createErr      error
	previewYAML    string
	previewErr     error
	runErr         error
	workloads      []domain.WorkloadSummary
	workloadsErr   error
	lastListQuery  domain.ListQuery
	workloadDetail domain.WorkloadDetail
	workloadErr    error
	lastKind       string
	lastNamespace  string
	lastName       string
	restartCalls   int
	restartErr     error
	imageCalls     int
	imageErr       error
	lastImage      domain.WorkloadImageUpdate
	containerCalls int
	containerErr   error
	lastContainer  domain.WorkloadContainerConfigUpdate
	suspendCalls   int
	suspendErr     error
	lastSuspend    domain.CronJobSuspendUpdate
	rolloutCalls   int
	rolloutErr     error
	lastRollout    domain.WorkloadRolloutUpdate
	lastRunInput   domain.CronJobRunInput
	pods           []domain.PodSummary
	podsErr        error
	podDetail      domain.PodDetail
	podErr         error
	revisions      []domain.WorkloadRevision
	revisionErr    error
	rollbackCalls  int
	rollbackErr    error
	terminalCalls  int
	terminalErr    error
	terminalOpts   domain.PodTerminalOptions
	terminal       domain.PodTerminalSession
	ephemeralCalls int
	ephemeralErr   error
	ephemeralOpts  domain.PodEphemeralContainerRequest
	ephemeralName  string
	lastRevision   int64
	lastRV         string
	forwardPod     domain.PortForwardPod
	forwardErr     error
	forwardAddress string
	forwardErrOpen error
	forwardCloser  io.Closer
	forwardCalls   int
	lastForward    domain.PortForwardTarget
}

func (f *fakeRepository) ListWorkloads(_ context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error) {
	f.lastListQuery = query
	return f.workloads, f.workloadsErr
}
func (f *fakeRepository) ListWorkloadsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.WorkloadSummary], error) {
	items, err := f.ListWorkloads(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (f *fakeRepository) GetWorkload(_ context.Context, kind, namespace, name string) (domain.WorkloadDetail, error) {
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	return f.workloadDetail, f.workloadErr
}
func (f *fakeRepository) CreateDeployment(_ context.Context, spec domain.DeploymentCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	return f.CreateWorkload(context.Background(), spec, dryRun)
}
func (f *fakeRepository) CreateWorkload(_ context.Context, spec domain.WorkloadCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	f.createCalls = append(f.createCalls, dryRun)
	f.lastCreateSpec = spec
	if f.createErr != nil {
		return domain.WorkloadSummary{}, f.createErr
	}
	return domain.WorkloadSummary{Kind: "Deployment", Name: spec.Name, Namespace: spec.Namespace, Desired: spec.Replicas}, nil
}
func (f *fakeRepository) PreviewDeployment(context.Context, domain.DeploymentCreateSpec) (string, error) {
	return f.PreviewWorkload(context.Background(), domain.WorkloadCreateSpec{})
}
func (f *fakeRepository) PreviewWorkload(context.Context, domain.WorkloadCreateSpec) (string, error) {
	f.previewCalls++
	return f.previewYAML, f.previewErr
}
func (f *fakeRepository) ScaleWorkload(context.Context, string, string, string, int32, string) error {
	f.scaleCalls++
	return f.scaleErr
}
func (f *fakeRepository) RestartWorkload(_ context.Context, kind, namespace, name string) error {
	f.restartCalls++
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	return f.restartErr
}
func (f *fakeRepository) UpdateWorkloadImage(_ context.Context, kind, namespace, name string, update domain.WorkloadImageUpdate) error {
	f.imageCalls++
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	f.lastImage = update
	return f.imageErr
}
func (f *fakeRepository) UpdateWorkloadContainerConfig(_ context.Context, kind, namespace, name string, update domain.WorkloadContainerConfigUpdate) error {
	f.containerCalls++
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	f.lastContainer = update
	return f.containerErr
}
func (f *fakeRepository) SetCronJobSuspend(_ context.Context, namespace, name string, update domain.CronJobSuspendUpdate) error {
	f.suspendCalls++
	f.lastNamespace, f.lastName = namespace, name
	f.lastSuspend = update
	return f.suspendErr
}
func (f *fakeRepository) RunCronJobOnce(_ context.Context, namespace, name string, input domain.CronJobRunInput) (domain.WorkloadSummary, error) {
	f.lastNamespace, f.lastName = namespace, name
	f.lastRunInput = input
	if f.runErr != nil {
		return domain.WorkloadSummary{}, f.runErr
	}
	return domain.WorkloadSummary{Kind: "Job", Name: name + "-manual", Namespace: namespace}, nil
}

func (f *fakeRepository) UpdateWorkloadRollout(_ context.Context, kind, namespace, name string, update domain.WorkloadRolloutUpdate) error {
	f.rolloutCalls++
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	f.lastRollout = update
	return f.rolloutErr
}
func (f *fakeRepository) ListWorkloadRevisions(_ context.Context, kind, namespace, name string) ([]domain.WorkloadRevision, error) {
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	return f.revisions, f.revisionErr
}
func (f *fakeRepository) RollbackWorkload(_ context.Context, kind, namespace, name string, revision int64) error {
	f.rollbackCalls++
	f.lastKind, f.lastNamespace, f.lastName = kind, namespace, name
	f.lastRevision = revision
	return f.rollbackErr
}
func (f *fakeRepository) DeleteWorkload(_ context.Context, _ string, _ string, _ string, propagation domain.DeletionPropagation) error {
	f.deletePolicy = propagation
	return f.deleteErr
}
func (f *fakeRepository) ListPods(_ context.Context, query domain.ListQuery) ([]domain.PodSummary, error) {
	f.lastListQuery = query
	return f.pods, f.podsErr
}
func (f *fakeRepository) ListPodsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PodSummary], error) {
	items, err := f.ListPods(ctx, query)
	return domain.PaginateSlice(items, page), err
}
func (f *fakeRepository) GetPod(_ context.Context, namespace, name string) (domain.PodDetail, error) {
	f.lastNamespace, f.lastName = namespace, name
	return f.podDetail, f.podErr
}
func (f *fakeRepository) DeletePod(_ context.Context, namespace, name string) error {
	f.lastNamespace, f.lastName = namespace, name
	return nil
}
func (f *fakeRepository) StreamPodLogs(_ context.Context, namespace, name, container string, _, previous bool, tailLines int64, _ bool, sinceSeconds int64, limitBytes int64) (io.ReadCloser, error) {
	f.tailLines = tailLines
	f.limitBytes = limitBytes
	f.previous = previous
	f.sinceSeconds = sinceSeconds
	key := namespace + "/" + name + "/" + container
	f.podLogCalls = append(f.podLogCalls, key)
	if err := f.podLogErrors[key]; err != nil {
		return nil, err
	}
	f.lastLogStream = io.NopCloser(strings.NewReader("log line"))
	return f.lastLogStream, nil
}
func (f *fakeRepository) OpenPodTerminal(_ context.Context, _ string, _ string, options domain.PodTerminalOptions) (domain.PodTerminalSession, error) {
	f.terminalCalls++
	f.terminalOpts = options
	if f.terminalErr != nil {
		return nil, f.terminalErr
	}
	if f.terminal != nil {
		return f.terminal, nil
	}
	return &fakeTerminalSession{}, nil
}

func (f *fakeRepository) AddPodEphemeralContainer(_ context.Context, _ string, _ string, request domain.PodEphemeralContainerRequest, containerName string) (string, error) {
	f.ephemeralCalls++
	f.ephemeralOpts = request
	if f.ephemeralErr != nil {
		return "", f.ephemeralErr
	}
	if f.ephemeralName != "" {
		return f.ephemeralName, nil
	}
	return containerName, nil
}

func (f *fakeRepository) ResolvePortForwardTarget(_ context.Context, target domain.PortForwardTarget) (domain.PortForwardPod, error) {
	f.lastForward = target
	if f.forwardErr != nil {
		return domain.PortForwardPod{}, f.forwardErr
	}
	if f.forwardPod.Pod != "" {
		return f.forwardPod, nil
	}
	return domain.PortForwardPod{Namespace: target.Namespace, Pod: target.Name, Port: target.Port}, nil
}

func (f *fakeRepository) OpenPodPortForward(_ context.Context, namespace, name string, port int32) (string, io.Closer, error) {
	f.forwardCalls++
	f.lastNamespace, f.lastName = namespace, name
	if f.forwardErrOpen != nil {
		return "", nil, f.forwardErrOpen
	}
	if f.forwardCloser != nil {
		return f.forwardAddress, f.forwardCloser, nil
	}
	return "127.0.0.1:41000", io.NopCloser(strings.NewReader("")), nil
}

type fakeCloser struct{ closed int }

func (c *fakeCloser) Close() error { c.closed++; return nil }

type fakeTerminalSession struct {
	closed int
}

func (*fakeTerminalSession) Read([]byte) (int, error)         { return 0, io.EOF }
func (*fakeTerminalSession) Write(buffer []byte) (int, error) { return len(buffer), nil }
func (*fakeTerminalSession) Resize(domain.TerminalSize)       {}
func (*fakeTerminalSession) Wait() error                      { return nil }
func (f *fakeTerminalSession) Close() error                   { f.closed++; return nil }

type fakeGuard struct {
	allow  bool
	err    error
	access *domain.Access
}

type fakeImageRegistryResolver struct {
	secret     string
	err        error
	registryID string
	image      string
	calls      int
}

func (r *fakeImageRegistryResolver) ResolvePullSecret(_ context.Context, _ domain.Actor, registryID, image string) (string, error) {
	r.calls++
	r.registryID = registryID
	r.image = image
	return r.secret, r.err
}

type selectiveGuard struct {
	deniedPods map[string]bool
}

func (g selectiveGuard) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	if g.deniedPods[access.Name] {
		return domain.ErrForbidden
	}
	return nil
}

func (g fakeGuard) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	if g.access != nil {
		*g.access = access
	}
	if g.err != nil {
		return g.err
	}
	if !g.allow {
		return domain.ErrForbidden
	}
	return nil
}

type fakeAuditor struct {
	events  int
	results []string
	actions []string
	details []map[string]any
}

type recordingGuard struct {
	accesses []domain.Access
	denied   map[string]bool
}

func (g *recordingGuard) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	g.accesses = append(g.accesses, access)
	if g.denied[access.Resource+"/"+access.Subresource] {
		return domain.ErrForbidden
	}
	return nil
}

func (a *fakeAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events++
	a.results = append(a.results, event.Result)
	a.actions = append(a.actions, event.Action)
	a.details = append(a.details, event.Details)
}

func TestScaleEnforcesGuardAndKind(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: false}, auditor)
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", 3, ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Scale() error = %v, want ErrForbidden", err)
	}
	if repo.scaleCalls != 0 {
		t.Fatal("repository was called without permission")
	}
	if len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("denied audit result = %#v", auditor.results)
	}
	service = New(repo, fakeGuard{allow: true}, auditor)
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "daemonset", "default", "web", 3, ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Scale(daemonset) error = %v, want ErrUnsupported", err)
	}
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "pod", "default", "web", 3, ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Scale(pod) error = %v, want ErrUnsupported", err)
	}
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", 3, "42"); err != nil {
		t.Fatalf("Scale() error = %v", err)
	}
	repo.scaleErr = domain.ErrUnavailable
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", 3, "43"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Scale() dependency error = %v", err)
	}
	if repo.scaleCalls != 2 || auditor.events != 5 {
		t.Fatalf("scale calls = %d, audit events = %d", repo.scaleCalls, auditor.events)
	}

	repo = &fakeRepository{}
	auditor = &fakeAuditor{}
	service = New(repo, fakeGuard{allow: false}, auditor)
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "pod", "default", "web", 3, ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Scale(pod) with denied guard error = %v, want ErrUnsupported", err)
	}
	if repo.scaleCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("invalid scale calls = %d audit = %#v", repo.scaleCalls, auditor.results)
	}
}

func TestOpenPortForwardAuthorizesPodSubresourceAndCloseRevokes(t *testing.T) {
	closer := &fakeCloser{}
	repo := &fakeRepository{forwardAddress: "127.0.0.1:41000", forwardCloser: closer}
	auditor := &fakeAuditor{}
	guard := &recordingGuard{}
	service := New(repo, guard, auditor)
	service.SetPortForwardLimits(domain.PortForwardLimits{MaxDuration: time.Minute, MaxSessionsPerUser: 2})
	actor := domain.Actor{Username: "alice"}

	session, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{Kind: "pod", Namespace: "prod", Name: "api-0", Port: 8080})
	if err != nil {
		t.Fatalf("OpenPortForward() error = %v", err)
	}
	if session.Pod != "api-0" || session.Port != 8080 || !strings.HasPrefix(session.Path, "/api/v1/tunnels/") || session.LocalAddress != "127.0.0.1:41000" {
		t.Fatalf("session = %#v", session)
	}
	if len(guard.accesses) != 1 || guard.accesses[0] != (domain.Access{Verb: "create", Resource: "pods", Subresource: "portforward", Namespace: "prod", Name: "api-0"}) {
		t.Fatalf("authorization = %#v", guard.accesses)
	}
	if _, err := service.GetPortForward(context.Background(), domain.Actor{Username: "bob"}, session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPortForward(other user) error = %v, want not found", err)
	}
	if err := service.ClosePortForward(context.Background(), actor, session.ID); err != nil {
		t.Fatalf("ClosePortForward() error = %v", err)
	}
	if closer.closed != 1 {
		t.Fatalf("closer calls = %d, want 1", closer.closed)
	}
	if _, err := service.GetPortForward(context.Background(), actor, session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPortForward(closed) error = %v, want not found", err)
	}
	if len(auditor.actions) < 2 || auditor.actions[0] != "open-port-forward" || auditor.actions[1] != "close-port-forward" {
		t.Fatalf("audit actions = %#v", auditor.actions)
	}
}

func TestOpenPortForwardEnforcesPerUserSessionLimit(t *testing.T) {
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	service.SetPortForwardLimits(domain.PortForwardLimits{MaxDuration: time.Minute, MaxSessionsPerUser: 1})
	actor := domain.Actor{Username: "alice"}
	if _, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{Kind: "pod", Namespace: "prod", Name: "api-0", Port: 80}); err != nil {
		t.Fatalf("first OpenPortForward() error = %v", err)
	}
	if _, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{Kind: "pod", Namespace: "prod", Name: "api-1", Port: 80}); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("second OpenPortForward() error = %v, want rate limited", err)
	}
	if repo.forwardCalls != 1 {
		t.Fatalf("port-forward opens = %d, want 1", repo.forwardCalls)
	}
	if _, err := service.OpenPortForward(context.Background(), domain.Actor{Username: "bob"}, domain.PortForwardTarget{Kind: "pod", Namespace: "prod", Name: "api-2", Port: 80}); err != nil {
		t.Fatalf("OpenPortForward(other user) error = %v", err)
	}
}

func TestReadOnlyPolicyIsAuditedAsDenied(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: true, err: domain.ErrReadOnly}, auditor)
	if err := service.Scale(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", 3, ""); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("Scale() error = %v, want ErrReadOnly", err)
	}
	if repo.scaleCalls != 0 {
		t.Fatalf("read-only policy reached repository %d times", repo.scaleCalls)
	}
	if len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("audit results = %#v, want [denied]", auditor.results)
	}
}

func TestStreamLogsClampsTailLines(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	stream, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "default", "pod", "app", false, false, 999999, false, 900)
	if err != nil {
		t.Fatalf("StreamLogs() error = %v", err)
	}
	_ = stream.Close()
	if repo.tailLines != 5000 {
		t.Fatalf("tailLines = %d, want 5000", repo.tailLines)
	}
	if repo.limitBytes != maxNonStreamLogBytes {
		t.Fatalf("limitBytes = %d, want %d", repo.limitBytes, maxNonStreamLogBytes)
	}
	if repo.sinceSeconds != 900 {
		t.Fatalf("sinceSeconds = %d, want 900", repo.sinceSeconds)
	}
	if _, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "default", "pod", "app", true, false, 100, false, 0); err != nil {
		t.Fatalf("StreamLogs(follow) error = %v", err)
	}
	if repo.limitBytes != 0 {
		t.Fatalf("follow limitBytes = %d, want 0", repo.limitBytes)
	}
	if _, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "default", "pod", "app", false, false, -1, false, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative tailLines error = %v, want ErrInvalid", err)
	}
}

func TestStreamLogsAuthorizesPodsLogAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	var access domain.Access
	service := New(repo, fakeGuard{allow: true, access: &access}, auditor)
	stream, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "prod", "api-0", "app", false, true, 100, true, 0)
	if err != nil {
		t.Fatalf("StreamLogs() error = %v", err)
	}
	_ = stream.Close()
	if access != (domain.Access{Verb: "get", Resource: "pods", Subresource: "log", Namespace: "prod", Name: "api-0"}) {
		t.Fatalf("authorization access = %#v", access)
	}
	if !repo.previous {
		t.Fatal("previous log option was not forwarded")
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "stream-pod-logs" || auditor.results[0] != "success" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}

	deniedRepo := &fakeRepository{}
	deniedAuditor := &fakeAuditor{}
	denied := New(deniedRepo, fakeGuard{allow: false}, deniedAuditor)
	if _, err := denied.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "prod", "api-0", "app", false, false, 100, false, 0); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("StreamLogs(denied) error = %v", err)
	}
	if deniedRepo.lastLogStream != nil || len(deniedAuditor.results) != 1 || deniedAuditor.results[0] != "denied" {
		t.Fatalf("denied stream reached repository or audit was wrong: %#v %#v", deniedRepo.lastLogStream, deniedAuditor.results)
	}
}

func TestStreamWorkloadLogsAggregatesContainersAndPods(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		workloadDetail: domain.WorkloadDetail{
			Pods: []domain.PodSummary{
				{Name: "api-0", Namespace: "prod"},
				{Name: "api-1", Namespace: "prod"},
			},
			Containers: []domain.WorkloadContainer{{Name: "app"}, {Name: "sidecar"}},
		},
		podLogErrors: map[string]error{"prod/api-1/sidecar": domain.ErrUnavailable},
	}
	auditor := &fakeAuditor{}
	service := New(repo, selectiveGuard{}, auditor)

	result, err := service.StreamWorkloadLogs(context.Background(), domain.Actor{Username: "alice"}, "deployment", "prod", "api", "", false, false, 100, false, 0)
	if err != nil {
		t.Fatalf("StreamWorkloadLogs() error = %v", err)
	}
	defer func() {
		for _, stream := range result.Streams {
			_ = stream.Stream.Close()
		}
	}()
	if len(result.Streams) != 3 {
		t.Fatalf("streams = %#v, want 3", result.Streams)
	}
	if len(repo.podLogCalls) != 4 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "api-1/sidecar") {
		t.Fatalf("calls = %#v warnings = %#v", repo.podLogCalls, result.Warnings)
	}
	if result.Streams[0].Pod != "api-0" || result.Streams[0].Container != "app" || result.Streams[2].Pod != "api-1" || result.Streams[2].Container != "app" {
		t.Fatalf("stream sources = %#v", result.Streams)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "stream-workload-logs" || auditor.results[0] != "success" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}
}

func TestStreamWorkloadLogsFiltersContainerAndBoundsStreams(t *testing.T) {
	t.Parallel()
	pods := make([]domain.PodSummary, 21)
	for index := range pods {
		pods[index] = domain.PodSummary{Name: "api-" + string(rune('a'+index)), Namespace: "prod"}
	}
	repo := &fakeRepository{workloadDetail: domain.WorkloadDetail{
		Pods: pods, Containers: []domain.WorkloadContainer{{Name: "app"}, {Name: "sidecar"}},
	}}
	service := New(repo, selectiveGuard{}, &fakeAuditor{})
	result, err := service.StreamWorkloadLogs(context.Background(), domain.Actor{Username: "alice"}, "deployment", "prod", "api", "sidecar", false, false, 100, false, 0)
	if err != nil {
		t.Fatalf("StreamWorkloadLogs() error = %v", err)
	}
	defer func() {
		for _, stream := range result.Streams {
			_ = stream.Stream.Close()
		}
	}()
	if len(result.Streams) != maxWorkloadLogStreams || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "20") {
		t.Fatalf("streams = %d warnings = %#v", len(result.Streams), result.Warnings)
	}
	for _, call := range repo.podLogCalls {
		if !strings.HasSuffix(call, "/sidecar") {
			t.Fatalf("container filter leaked into call %q", call)
		}
	}
}

func TestStreamWorkloadLogsRequiresPerPodPermission(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{workloadDetail: domain.WorkloadDetail{
		Pods:       []domain.PodSummary{{Name: "api-0", Namespace: "prod"}, {Name: "api-1", Namespace: "prod"}},
		Containers: []domain.WorkloadContainer{{Name: "app"}},
	}}
	service := New(repo, selectiveGuard{deniedPods: map[string]bool{"api-1": true}}, &fakeAuditor{})
	result, err := service.StreamWorkloadLogs(context.Background(), domain.Actor{Username: "alice"}, "deployment", "prod", "api", "", false, false, 100, false, 0)
	if err != nil {
		t.Fatalf("StreamWorkloadLogs() error = %v", err)
	}
	defer func() {
		for _, stream := range result.Streams {
			_ = stream.Stream.Close()
		}
	}()
	if len(result.Streams) != 1 || result.Streams[0].Pod != "api-0" {
		t.Fatalf("streams = %#v", result.Streams)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "api-1") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}

	failedRepo := &fakeRepository{
		workloadDetail: domain.WorkloadDetail{
			Pods:       []domain.PodSummary{{Name: "api-0", Namespace: "prod"}},
			Containers: []domain.WorkloadContainer{{Name: "app"}},
		},
		podLogErrors: map[string]error{"prod/api-0/app": domain.ErrUnavailable},
	}
	failed := New(failedRepo, selectiveGuard{}, &fakeAuditor{})
	if _, err := failed.StreamWorkloadLogs(context.Background(), domain.Actor{Username: "alice"}, "deployment", "prod", "api", "", false, false, 100, false, 0); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("all-stream failure error = %v, want ErrUnavailable", err)
	}
}

func TestOpenTerminalValidatesShellSizeAndAuthorization(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	var access domain.Access
	service := New(repo, fakeGuard{allow: true, access: &access}, auditor)
	actor := domain.Actor{Username: "alice"}
	valid := domain.PodTerminalOptions{Container: "api", Command: []string{"/bin/sh"}, TTY: true, Size: domain.TerminalSize{Columns: 100, Rows: 30}}

	for name, options := range map[string]domain.PodTerminalOptions{
		"builtin command":  {Container: "api", Command: []string{"sh", "-c", "id"}, Size: valid.Size},
		"disallowed shell": {Container: "api", Command: []string{"/bin/zsh"}, Size: valid.Size},
		"too narrow":       {Container: "api", Command: valid.Command, Size: domain.TerminalSize{Columns: 19, Rows: 30}},
		"too tall":         {Container: "api", Command: valid.Command, Size: domain.TerminalSize{Columns: 100, Rows: 101}},
	} {
		if _, err := service.OpenTerminal(context.Background(), actor, "prod", "api-0", options); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%s error = %v, want ErrInvalid", name, err)
		}
	}
	if repo.terminalCalls != 0 {
		t.Fatalf("invalid requests reached repository %d times", repo.terminalCalls)
	}

	session, err := service.OpenTerminal(context.Background(), actor, "prod", "api-0", valid)
	if err != nil {
		t.Fatalf("OpenTerminal() error = %v", err)
	}
	if access != (domain.Access{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: "prod", Name: "api-0"}) {
		t.Fatalf("authorization access = %#v", access)
	}
	if repo.terminalCalls != 1 || repo.terminalOpts.Container != "api" || !repo.terminalOpts.TTY {
		t.Fatalf("terminal options = %#v calls=%d", repo.terminalOpts, repo.terminalCalls)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "exec" || auditor.results[0] != "success" {
		t.Fatalf("open audit = %#v %#v", auditor.actions, auditor.results)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if len(auditor.actions) != 2 || auditor.actions[1] != "exec-close" {
		t.Fatalf("close audit = %#v", auditor.actions)
	}
	if auditor.details[1]["container"] != "api" || auditor.details[1]["reason"] != "client_closed" {
		t.Fatalf("close audit details = %#v", auditor.details[1])
	}

	deniedRepo := &fakeRepository{}
	deniedAuditor := &fakeAuditor{}
	denied := New(deniedRepo, fakeGuard{allow: false}, deniedAuditor)
	if _, err := denied.OpenTerminal(context.Background(), actor, "prod", "api-0", valid); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied OpenTerminal() error = %v", err)
	}
	if deniedRepo.terminalCalls != 0 || len(deniedAuditor.results) != 1 || deniedAuditor.results[0] != "denied" {
		t.Fatalf("denied calls = %d audit = %#v", deniedRepo.terminalCalls, deniedAuditor.results)
	}
}

func TestOpenTerminalEnforcesPerUserSessionLimit(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	limits := domain.DefaultTerminalLimits()
	limits.MaxSessionsPerUser = 1
	service := New(repo, fakeGuard{allow: true}, auditor, limits)
	actor := domain.Actor{Username: "alice"}
	options := domain.PodTerminalOptions{Container: "api", Command: []string{"/bin/sh"}, TTY: true, Size: domain.TerminalSize{Columns: 100, Rows: 30}}

	first, err := service.OpenTerminal(context.Background(), actor, "prod", "api-0", options)
	if err != nil {
		t.Fatalf("first OpenTerminal() error = %v", err)
	}
	if _, err := service.OpenTerminal(context.Background(), actor, "prod", "api-0", options); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("second OpenTerminal() error = %v, want ErrRateLimited", err)
	}
	if repo.terminalCalls != 1 {
		t.Fatalf("repository calls = %d, want 1", repo.terminalCalls)
	}
	last := auditor.details[len(auditor.details)-1]
	if last["reason"] != "session_limit" {
		t.Fatalf("limit audit details = %#v", last)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	third, err := service.OpenTerminal(context.Background(), actor, "prod", "api-0", options)
	if err != nil {
		t.Fatalf("third OpenTerminal() error = %v", err)
	}
	_ = third.Close()
}

func TestCreateEphemeralContainerValidatesAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	var access domain.Access
	service := New(repo, fakeGuard{allow: true, access: &access}, auditor)
	actor := domain.Actor{Username: "alice"}
	result, err := service.CreateEphemeralContainer(context.Background(), actor, "prod", "api-0", domain.PodEphemeralContainerRequest{
		Image: "busybox:1.36", TargetContainerName: "api", Shell: "sh",
	})
	if err != nil {
		t.Fatalf("CreateEphemeralContainer() error = %v", err)
	}
	if !strings.HasPrefix(result.Name, "debug-") || result.Status != "created" {
		t.Fatalf("result = %#v", result)
	}
	if access != (domain.Access{Verb: "update", Resource: "pods", Subresource: "ephemeralcontainers", Namespace: "prod", Name: "api-0"}) {
		t.Fatalf("authorization access = %#v", access)
	}
	if repo.ephemeralCalls != 1 || repo.ephemeralOpts.Image != "busybox:1.36" || repo.ephemeralOpts.TargetContainerName != "api" {
		t.Fatalf("repository options = %#v calls=%d", repo.ephemeralOpts, repo.ephemeralCalls)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "create-ephemeral-container" || auditor.results[0] != "success" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}
	if auditor.details[0]["container"] != result.Name || auditor.details[0]["targetContainer"] != "api" {
		t.Fatalf("audit details = %#v", auditor.details[0])
	}

	for name, request := range map[string]domain.PodEphemeralContainerRequest{
		"missing image": {Shell: "sh"},
		"bad image":     {Image: "busybox 1.36", Shell: "sh"},
		"bad shell":     {Image: "busybox:1.36", Shell: "zsh"},
	} {
		if _, err := service.CreateEphemeralContainer(context.Background(), actor, "prod", "api-0", request); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%s error = %v, want ErrInvalid", name, err)
		}
	}
	if repo.ephemeralCalls != 1 {
		t.Fatalf("invalid requests reached repository: %d", repo.ephemeralCalls)
	}

	repo.ephemeralErr = domain.ErrConflict
	if _, err := service.CreateEphemeralContainer(context.Background(), actor, "prod", "api-0", domain.PodEphemeralContainerRequest{Image: "busybox:1.36", Shell: "sh"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}

	deniedRepo := &fakeRepository{}
	denied := New(deniedRepo, fakeGuard{allow: false}, &fakeAuditor{})
	if _, err := denied.CreateEphemeralContainer(context.Background(), actor, "prod", "api-0", domain.PodEphemeralContainerRequest{Image: "busybox:1.36"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied error = %v", err)
	}
	if deniedRepo.ephemeralCalls != 0 {
		t.Fatalf("denied request reached repository %d times", deniedRepo.ephemeralCalls)
	}
}

func TestDeletePropagationValidation(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	if err := service.Delete(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", "Foreground"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if repo.deletePolicy != domain.DeletePropagationForeground {
		t.Fatalf("propagation = %q", repo.deletePolicy)
	}
	for _, policy := range []struct {
		input string
		want  domain.DeletionPropagation
	}{
		{input: "", want: domain.DeletePropagationBackground},
		{input: "Background", want: domain.DeletePropagationBackground},
		{input: "Orphan", want: domain.DeletePropagationOrphan},
	} {
		if err := service.Delete(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", policy.input); err != nil {
			t.Fatalf("Delete(%q) error = %v", policy.input, err)
		}
		if repo.deletePolicy != policy.want {
			t.Fatalf("Delete(%q) propagation = %q, want %q", policy.input, repo.deletePolicy, policy.want)
		}
	}
	if err := service.Delete(context.Background(), domain.Actor{Username: "admin"}, "deployment", "default", "web", "Invalid"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid propagation error = %v, want ErrInvalid", err)
	}
}

func validDeploymentSpec() domain.DeploymentCreateSpec {
	return domain.DeploymentCreateSpec{
		Name: "api", Namespace: "prod", Replicas: 2, ContainerName: "api", Image: "registry.test/api:1.0",
		ContainerPort: 8080, CPURequestMilli: 100, CPULimitMilli: 500,
		MemoryRequestBytes: 128 << 20, MemoryLimitBytes: 512 << 20,
		Env:    []domain.DeploymentEnvVar{{Name: "MODE", Value: "production"}},
		Labels: map[string]string{"team": "platform"},
	}
}

func TestCreateDeploymentDryRunsBeforeWritingAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: true}, auditor)
	spec := validDeploymentSpec()
	created, err := service.CreateDeployment(context.Background(), domain.Actor{Username: "admin"}, spec)
	if err != nil {
		t.Fatalf("CreateDeployment() error = %v", err)
	}
	if created.Name != "api" || created.Namespace != "prod" || created.Desired != 2 {
		t.Fatalf("created summary = %#v", created)
	}
	if len(repo.createCalls) != 2 || !repo.createCalls[0] || repo.createCalls[1] {
		t.Fatalf("create calls = %#v, want [true false]", repo.createCalls)
	}
	if auditor.events != 1 || auditor.results[0] != "success" {
		t.Fatalf("audit = events %d results %#v", auditor.events, auditor.results)
	}
}

func TestCreateWorkloadSupportsAllKindsWithKindSpecificDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind         string
		mutate       func(*domain.WorkloadCreateSpec)
		wantReplicas int32
		wantService  string
		wantSchedule string
		wantPolicy   string
	}{
		{kind: "deployment", wantReplicas: 2},
		{kind: "statefulset", wantReplicas: 2, wantService: "api"},
		{kind: "daemonset"},
		{kind: "job"},
		{kind: "cronjob", mutate: func(spec *domain.WorkloadCreateSpec) {
			spec.Schedule = " */5 * * * * "
			spec.ConcurrencyPolicy = "Forbid"
		}, wantSchedule: "*/5 * * * *", wantPolicy: "Forbid"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			repo := &fakeRepository{}
			auditor := &fakeAuditor{}
			service := New(repo, fakeGuard{allow: true}, auditor)
			spec := validDeploymentSpec()
			spec.Kind = tt.kind
			if tt.mutate != nil {
				tt.mutate(&spec)
			}
			created, err := service.CreateWorkload(context.Background(), domain.Actor{Username: "admin"}, spec)
			if err != nil {
				t.Fatalf("CreateWorkload() error = %v", err)
			}
			if created.Name != "api" || len(repo.createCalls) != 2 || !repo.createCalls[0] || repo.createCalls[1] {
				t.Fatalf("created = %#v calls = %#v", created, repo.createCalls)
			}
			got := repo.lastCreateSpec
			if got.Kind != tt.kind || got.Replicas != tt.wantReplicas || got.ServiceName != tt.wantService || got.Schedule != tt.wantSchedule || got.ConcurrencyPolicy != tt.wantPolicy {
				t.Fatalf("normalized spec = %#v", got)
			}
			if len(auditor.actions) != 1 || auditor.actions[0] != "create" || auditor.results[0] != "success" {
				t.Fatalf("audit actions=%#v results=%#v", auditor.actions, auditor.results)
			}
		})
	}
}

func TestCreateWorkloadRejectsKindSpecificInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*domain.WorkloadCreateSpec)
		field  string
	}{
		{name: "unsupported kind", mutate: func(spec *domain.WorkloadCreateSpec) { spec.Kind = "pod" }, field: "kind"},
		{name: "cronjob missing schedule", mutate: func(spec *domain.WorkloadCreateSpec) { spec.Kind = "cronjob"; spec.Schedule = "" }, field: "schedule"},
		{name: "cronjob invalid policy", mutate: func(spec *domain.WorkloadCreateSpec) {
			spec.Kind, spec.Schedule, spec.ConcurrencyPolicy = "cronjob", "*/5 * * * *", "Sometimes"
		}, field: "concurrencyPolicy"},
		{name: "statefulset invalid service", mutate: func(spec *domain.WorkloadCreateSpec) { spec.Kind = "statefulset"; spec.ServiceName = "Bad_Service" }, field: "serviceName"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
			spec := validDeploymentSpec()
			tt.mutate(&spec)
			_, err := service.CreateWorkload(context.Background(), domain.Actor{Username: "admin"}, spec)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || validation.Field != tt.field {
				t.Fatalf("validation error = %#v, want field %q", validation, tt.field)
			}
			if len(repo.createCalls) != 0 {
				t.Fatalf("repository called for invalid input: %#v", repo.createCalls)
			}
		})
	}
}

func TestCreateWorkloadSupportsVolumesEnvSourcesProbesAndRuntimeSettings(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	spec := validDeploymentSpec()
	spec.Kind = "deployment"
	grace := int64(30)
	optional := true
	spec.ImagePullSecrets = []string{" registry-pull ", "registry-pull"}
	spec.RuntimeClassName = "gvisor"
	spec.TerminationGracePeriod = &grace
	spec.Volumes = []domain.WorkloadVolumeSpec{
		{Name: "config", Type: "configMap", Source: "web-config", Optional: optional},
		{Name: "data", Type: "persistentVolumeClaim", Source: "web-data"},
	}
	spec.Containers = []domain.WorkloadContainerSpec{{
		Name: "api", Image: "registry.test/api:1", ContainerPort: 8080,
		CPURequestMilli: 100, CPULimitMilli: 500, MemoryRequestBytes: 128 << 20, MemoryLimitBytes: 512 << 20,
		Env: []domain.WorkloadEnvVar{
			{Name: "MODE", Value: "production"},
			{Name: "TOKEN", ValueFrom: &domain.WorkloadEnvValueFrom{Kind: "secretKeyRef", Name: "api-secret", Key: "token", Optional: optional}},
			{Name: "POD_NAME", ValueFrom: &domain.WorkloadEnvValueFrom{Kind: "fieldRef", Name: "metadata.name"}},
		},
		VolumeMounts: []domain.WorkloadVolumeMount{
			{Name: "config", MountPath: "/etc/api", SubPath: "config.yaml", ReadOnly: true},
			{Name: "data", MountPath: "/var/lib/api"},
		},
		LivenessProbe:  &domain.WorkloadProbe{Type: "httpGet", Path: "/healthz", Port: "8080", Scheme: "HTTP", PeriodSeconds: 10, TimeoutSeconds: 1, SuccessThreshold: 1, FailureThreshold: 3},
		ReadinessProbe: &domain.WorkloadProbe{Type: "exec", Command: []string{"cat", "/tmp/ready"}, PeriodSeconds: 5, TimeoutSeconds: 1, SuccessThreshold: 1, FailureThreshold: 3},
	}}
	if _, err := service.CreateWorkload(context.Background(), domain.Actor{Username: "alice"}, spec); err != nil {
		t.Fatalf("CreateWorkload() error = %v", err)
	}
	if len(repo.createCalls) != 2 || !repo.createCalls[0] || repo.createCalls[1] {
		t.Fatalf("create calls = %#v", repo.createCalls)
	}
	got := repo.lastCreateSpec
	if len(got.ImagePullSecrets) != 1 || got.ImagePullSecrets[0] != "registry-pull" || got.RuntimeClassName != "gvisor" || got.TerminationGracePeriod == nil || *got.TerminationGracePeriod != 30 {
		t.Fatalf("runtime settings = %#v", got)
	}
	if len(got.Volumes) != 2 || len(got.Containers) != 1 || len(got.Containers[0].Env) != 3 || len(got.Containers[0].VolumeMounts) != 2 || got.Containers[0].LivenessProbe == nil || got.Containers[0].ReadinessProbe == nil {
		t.Fatalf("container configuration = %#v", got)
	}
	if got.Containers[0].Env[1].ValueFrom == nil || got.Containers[0].Env[1].ValueFrom.Kind != "secretKeyRef" || !optional {
		t.Fatalf("env source = %#v", got.Containers[0].Env[1])
	}
}

func TestCreateWorkloadRejectsUndeclaredVolumeMount(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	spec := validDeploymentSpec()
	spec.Kind = "deployment"
	spec.Containers = []domain.WorkloadContainerSpec{{
		Name: "api", Image: "registry.test/api:1", ContainerPort: 8080,
		CPURequestMilli: 100, CPULimitMilli: 500, MemoryRequestBytes: 128 << 20, MemoryLimitBytes: 512 << 20,
		VolumeMounts: []domain.WorkloadVolumeMount{{Name: "missing", MountPath: "/etc/config"}},
	}}
	_, err := service.CreateWorkload(context.Background(), domain.Actor{Username: "alice"}, spec)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Field != "containers.0.volumeMounts.0.name" {
		t.Fatalf("validation = %#v", validation)
	}
	if len(repo.createCalls) != 0 {
		t.Fatalf("repository called for invalid mount: %#v", repo.createCalls)
	}
}

func TestRunCronJobOnceRequiresGetAndJobCreateAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	guard := &recordingGuard{}
	auditor := &fakeAuditor{}
	service := New(repo, guard, auditor)
	created, err := service.RunCronJobOnce(context.Background(), domain.Actor{Username: "admin"}, " prod ", " backup ", domain.CronJobRunInput{})
	if err != nil {
		t.Fatalf("RunCronJobOnce() error = %v", err)
	}
	if created.Name != "backup-manual" || repo.lastNamespace != "prod" || repo.lastName != "backup" {
		t.Fatalf("created=%#v namespace=%q name=%q", created, repo.lastNamespace, repo.lastName)
	}
	if len(guard.accesses) != 2 {
		t.Fatalf("accesses = %#v", guard.accesses)
	}
	if got := guard.accesses[0]; got.Verb != "get" || got.APIGroup != "batch" || got.Resource != "cronjobs" || got.Namespace != "prod" || got.Name != "backup" {
		t.Fatalf("cronjob access = %#v", got)
	}
	if got := guard.accesses[1]; got.Verb != "create" || got.APIGroup != "batch" || got.Resource != "jobs" || got.Namespace != "prod" {
		t.Fatalf("job access = %#v", got)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "run-cronjob" || auditor.results[0] != "success" {
		t.Fatalf("audit actions=%#v results=%#v", auditor.actions, auditor.results)
	}
}

func TestRunCronJobOnceRejectsInvalidNamesAndAuditsFailures(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	ax := &fakeAuditor{}
	service := New(repo, &recordingGuard{}, ax)
	if _, err := service.RunCronJobOnce(context.Background(), domain.Actor{Username: "admin"}, "Bad_NS", "backup", domain.CronJobRunInput{}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid namespace error = %v", err)
	}
	repo.runErr = domain.ErrUnavailable
	if _, err := service.RunCronJobOnce(context.Background(), domain.Actor{Username: "admin"}, "prod", "backup", domain.CronJobRunInput{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("repository error = %v", err)
	}
	if len(ax.actions) != 2 || ax.actions[0] != "run-cronjob" || ax.results[0] != "failure" || ax.results[1] != "failure" {
		t.Fatalf("audit actions=%#v results=%#v", ax.actions, ax.results)
	}
}

func TestWorkloadImageUpdatesMergeRegistryPullSecret(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	resolver := &fakeImageRegistryResolver{secret: "registry-pull"}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	service.SetImageRegistryResolver(resolver)
	update := domain.WorkloadImageUpdate{
		ContainerName: "api", Image: "registry.example.com/platform/api:2", ResourceVersion: "42",
		RegistryID: "reg-primary", ImagePullSecrets: []string{"existing-pull"},
	}
	if err := service.UpdateImage(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", update); err != nil {
		t.Fatalf("UpdateImage() error = %v", err)
	}
	if resolver.calls != 1 || resolver.registryID != "reg-primary" || resolver.image != update.Image {
		t.Fatalf("resolver call = %#v", resolver)
	}
	if len(repo.lastImage.ImagePullSecrets) != 2 || repo.lastImage.ImagePullSecrets[0] != "existing-pull" || repo.lastImage.ImagePullSecrets[1] != "registry-pull" {
		t.Fatalf("image pull secrets = %#v", repo.lastImage.ImagePullSecrets)
	}
}

func TestCreateDeploymentRejectsInvalidInputWithoutRepositoryCall(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: true}, auditor)
	tests := []struct {
		name   string
		mutate func(*domain.DeploymentCreateSpec)
	}{
		{name: "name", mutate: func(spec *domain.DeploymentCreateSpec) { spec.Name = "Bad_Name" }},
		{name: "image", mutate: func(spec *domain.DeploymentCreateSpec) { spec.Image = "bad image" }},
		{name: "replicas", mutate: func(spec *domain.DeploymentCreateSpec) { spec.Replicas = -1 }},
		{name: "port", mutate: func(spec *domain.DeploymentCreateSpec) { spec.ContainerPort = 70000 }},
		{name: "cpu limit below request", mutate: func(spec *domain.DeploymentCreateSpec) { spec.CPULimitMilli = 50 }},
		{name: "duplicate env", mutate: func(spec *domain.DeploymentCreateSpec) {
			spec.Env = append(spec.Env, domain.DeploymentEnvVar{Name: "MODE", Value: "other"})
		}},
		{name: "invalid label", mutate: func(spec *domain.DeploymentCreateSpec) { spec.Labels = map[string]string{"bad key": "value"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validDeploymentSpec()
			tt.mutate(&spec)
			if _, err := service.CreateDeployment(context.Background(), domain.Actor{Username: "admin"}, spec); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			if len(repo.createCalls) != 0 {
				t.Fatalf("repository was called for invalid input: %#v", repo.createCalls)
			}
		})
	}
	if auditor.events != len(tests) || auditor.results[0] != "failure" {
		t.Fatalf("audit = events %d results %#v", auditor.events, auditor.results)
	}
}

func TestCreateDeploymentPermissionAndDryRunFailuresAreAudited(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: false}, auditor)
	if _, err := service.CreateDeployment(context.Background(), domain.Actor{Username: "viewer"}, validDeploymentSpec()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("forbidden error = %v", err)
	}
	if len(repo.createCalls) != 0 || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("forbidden calls=%#v audit=%#v", repo.createCalls, auditor.results)
	}

	repo = &fakeRepository{createErr: domain.ErrUnavailable}
	auditor = &fakeAuditor{}
	service = New(repo, fakeGuard{allow: true}, auditor)
	if _, err := service.CreateDeployment(context.Background(), domain.Actor{Username: "admin"}, validDeploymentSpec()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("dry-run error = %v", err)
	}
	if len(repo.createCalls) != 1 || !repo.createCalls[0] || auditor.results[0] != "failure" {
		t.Fatalf("dry-run calls=%#v audit=%#v", repo.createCalls, auditor.results)
	}
}

func TestListAndGetValidateKindsAndDelegate(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		workloads:      []domain.WorkloadSummary{{Kind: "Deployment", Name: "api", Namespace: "prod"}},
		workloadDetail: domain.WorkloadDetail{WorkloadSummary: domain.WorkloadSummary{Kind: "Deployment", Name: "api", Namespace: "prod"}},
	}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})

	if _, err := service.List(context.Background(), domain.ListQuery{Kind: "pod"}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("List(invalid kind) error = %v", err)
	}
	if repo.lastListQuery.Kind != "" {
		t.Fatalf("repository called for invalid kind: %#v", repo.lastListQuery)
	}
	items, err := service.List(context.Background(), domain.ListQuery{Kind: "Deployment", Namespace: "prod", Search: "api"})
	if err != nil || len(items) != 1 || repo.lastListQuery.Kind != "Deployment" {
		t.Fatalf("List() = (%#v, %v), query = %#v", items, err, repo.lastListQuery)
	}
	if _, err := service.Get(context.Background(), "pod", "prod", "api"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Get(invalid kind) error = %v", err)
	}
	detail, err := service.Get(context.Background(), "Deployment", "prod", "api")
	if err != nil || detail.Name != "api" || repo.lastKind != "Deployment" || repo.lastNamespace != "prod" || repo.lastName != "api" {
		t.Fatalf("Get() = (%#v, %v), last = %s/%s/%s", detail, err, repo.lastKind, repo.lastNamespace, repo.lastName)
	}
	repo.workloadsErr = domain.ErrUnavailable
	if _, err := service.List(context.Background(), domain.ListQuery{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("List() dependency error = %v", err)
	}
	repo.workloadErr = domain.ErrNotFound
	if _, err := service.Get(context.Background(), "deployment", "prod", "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get() dependency error = %v", err)
	}
}

func TestRestartAuthorizesAuditsAndRejectsUnsupportedKinds(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	denied := New(repo, fakeGuard{allow: false}, auditor)
	if err := denied.Restart(context.Background(), domain.Actor{Username: "viewer"}, "deployment", "prod", "api"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Restart() error = %v", err)
	}
	if repo.restartCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("restart calls = %d audit = %#v", repo.restartCalls, auditor.results)
	}

	repo = &fakeRepository{}
	auditor = &fakeAuditor{}
	service := New(repo, fakeGuard{allow: true}, auditor)
	if err := service.Restart(context.Background(), domain.Actor{Username: "admin"}, "job", "prod", "api"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Restart(job) error = %v", err)
	}
	if err := service.Restart(context.Background(), domain.Actor{Username: "admin"}, "pod", "prod", "api"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Restart(pod) error = %v", err)
	}
	if repo.restartCalls != 0 || len(auditor.results) != 2 || auditor.results[0] != "failure" || auditor.results[1] != "failure" {
		t.Fatalf("unsupported restart calls = %d audit = %#v", repo.restartCalls, auditor.results)
	}
	if err := service.Restart(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api"); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	repo.restartErr = domain.ErrUnavailable
	if err := service.Restart(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Restart() dependency error = %v", err)
	}
	if repo.restartCalls != 2 || repo.lastKind != "deployment" || len(auditor.results) != 4 || auditor.results[2] != "success" || auditor.results[3] != "failure" {
		t.Fatalf("restart calls = %d last = %s audit = %#v", repo.restartCalls, repo.lastKind, auditor.results)
	}

	repo = &fakeRepository{}
	auditor = &fakeAuditor{}
	service = New(repo, fakeGuard{allow: false}, auditor)
	if err := service.Restart(context.Background(), domain.Actor{Username: "viewer"}, "pod", "prod", "api"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Restart(pod) with denied guard error = %v, want ErrUnsupported", err)
	}
	if repo.restartCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("invalid restart calls = %d audit = %#v", repo.restartCalls, auditor.results)
	}
}

func TestUpdateImageValidatesAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	denied := New(repo, fakeGuard{allow: false}, auditor)
	update := domain.WorkloadImageUpdate{ContainerName: "api", Image: "registry.test/api:2", ResourceVersion: "42"}
	if err := denied.UpdateImage(context.Background(), domain.Actor{Username: "viewer"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied UpdateImage() error = %v", err)
	}
	if repo.imageCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("denied calls = %d audit = %#v", repo.imageCalls, auditor.results)
	}

	service := New(repo, fakeGuard{allow: true}, auditor)
	if err := service.UpdateImage(context.Background(), domain.Actor{Username: "admin"}, "job", "prod", "migrate", update); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("UpdateImage(job) error = %v", err)
	}
	invalidCases := []domain.WorkloadImageUpdate{
		{ContainerName: "Bad_Name", Image: "registry.test/api:2"},
		{ContainerName: "api", Image: "bad image"},
		{ContainerName: "api", Image: strings.Repeat("a", 513)},
	}
	for _, invalid := range invalidCases {
		if err := service.UpdateImage(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", invalid); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid UpdateImage(%#v) error = %v", invalid, err)
		}
	}
	if repo.imageCalls != 0 || len(auditor.results) != 5 || auditor.results[4] != "failure" {
		t.Fatalf("invalid calls = %d audit = %#v", repo.imageCalls, auditor.results)
	}

	update.ContainerName = "  api  "
	update.Image = "  registry.test/api:2  "
	if err := service.UpdateImage(context.Background(), domain.Actor{Username: "admin"}, "Deployment", " prod ", " api ", update); err != nil {
		t.Fatalf("UpdateImage() error = %v", err)
	}
	if repo.imageCalls != 1 || repo.lastKind != "Deployment" || repo.lastNamespace != "prod" || repo.lastName != "api" ||
		repo.lastImage.ContainerName != "api" || repo.lastImage.Image != "registry.test/api:2" || repo.lastImage.ResourceVersion != "42" {
		t.Fatalf("image calls = %d last = %s/%s/%s update = %#v", repo.imageCalls, repo.lastKind, repo.lastNamespace, repo.lastName, repo.lastImage)
	}
	repo.imageErr = domain.ErrConflict
	if err := service.UpdateImage(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflict UpdateImage() error = %v", err)
	}
	if repo.imageCalls != 2 || len(auditor.results) != 7 || auditor.results[5] != "success" || auditor.results[6] != "failure" {
		t.Fatalf("success/conflict calls = %d audit = %#v", repo.imageCalls, auditor.results)
	}
}

func TestSetCronJobSuspendValidatesAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	update := domain.CronJobSuspendUpdate{Suspend: true, ResourceVersion: "42"}
	denied := New(repo, fakeGuard{allow: false}, auditor)
	if err := denied.SetCronJobSuspend(context.Background(), domain.Actor{Username: "viewer"}, "cronjob", "prod", "backup", update); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied SetCronJobSuspend() error = %v", err)
	}
	if repo.suspendCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("denied calls = %d audit = %#v", repo.suspendCalls, auditor.results)
	}

	service := New(repo, fakeGuard{allow: true}, auditor)
	if err := service.SetCronJobSuspend(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("SetCronJobSuspend(deployment) error = %v", err)
	}
	invalidCases := []struct {
		namespace, name string
		update          domain.CronJobSuspendUpdate
	}{
		{namespace: "Bad_Namespace", name: "backup", update: update},
		{namespace: "prod", name: strings.Repeat("a", 254), update: update},
		{namespace: "prod", name: "backup", update: domain.CronJobSuspendUpdate{Suspend: false, ResourceVersion: strings.Repeat("x", 257)}},
	}
	for _, invalid := range invalidCases {
		if err := service.SetCronJobSuspend(context.Background(), domain.Actor{Username: "admin"}, "cronjob", invalid.namespace, invalid.name, invalid.update); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid SetCronJobSuspend(%s/%s) error = %v", invalid.namespace, invalid.name, err)
		}
	}
	if repo.suspendCalls != 0 || len(auditor.results) != 5 || auditor.results[4] != "failure" {
		t.Fatalf("invalid calls = %d audit = %#v", repo.suspendCalls, auditor.results)
	}

	update.ResourceVersion = " 42 "
	if err := service.SetCronJobSuspend(context.Background(), domain.Actor{Username: "admin"}, " CronJob ", " prod ", " backup ", update); err != nil {
		t.Fatalf("SetCronJobSuspend() error = %v", err)
	}
	if repo.suspendCalls != 1 || repo.lastNamespace != "prod" || repo.lastName != "backup" || repo.lastSuspend != (domain.CronJobSuspendUpdate{Suspend: true, ResourceVersion: "42"}) {
		t.Fatalf("suspend calls = %d last = %s/%s update = %#v", repo.suspendCalls, repo.lastNamespace, repo.lastName, repo.lastSuspend)
	}
	repo.suspendErr = domain.ErrConflict
	update.Suspend = false
	if err := service.SetCronJobSuspend(context.Background(), domain.Actor{Username: "admin"}, "cronjob", "prod", "backup", update); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflict SetCronJobSuspend() error = %v", err)
	}
	if repo.suspendCalls != 2 || len(auditor.results) != 7 || auditor.results[5] != "success" || auditor.results[6] != "failure" {
		t.Fatalf("success/conflict calls = %d audit = %#v", repo.suspendCalls, auditor.results)
	}
	if auditor.actions[5] != "suspend" || auditor.actions[6] != "resume" {
		t.Fatalf("suspend/resume audit actions = %#v", auditor.actions)
	}
}

func TestPodReadsDelegateAndPropagateFailures(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{
		pods:      []domain.PodSummary{{Name: "api-1", Namespace: "prod"}},
		podDetail: domain.PodDetail{PodSummary: domain.PodSummary{Name: "api-1", Namespace: "prod"}},
	}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	items, err := service.ListPods(context.Background(), domain.ListQuery{Namespace: "prod", Search: "api"})
	if err != nil || len(items) != 1 || repo.lastListQuery.Namespace != "prod" {
		t.Fatalf("ListPods() = (%#v, %v), query = %#v", items, err, repo.lastListQuery)
	}
	detail, err := service.GetPod(context.Background(), "prod", "api-1")
	if err != nil || detail.Name != "api-1" || repo.lastNamespace != "prod" || repo.lastName != "api-1" {
		t.Fatalf("GetPod() = (%#v, %v), last = %s/%s", detail, err, repo.lastNamespace, repo.lastName)
	}
	repo.podsErr = domain.ErrUnavailable
	if _, err := service.ListPods(context.Background(), domain.ListQuery{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("ListPods() error = %v", err)
	}
	repo.podErr = domain.ErrNotFound
	if _, err := service.GetPod(context.Background(), "prod", "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPod() error = %v", err)
	}
}

func TestPreviewDeploymentEnforcesPermissionAndValidation(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{previewYAML: "apiVersion: apps/v1\nkind: Deployment\n"}
	auditor := &fakeAuditor{}
	denied := New(repo, fakeGuard{allow: false}, auditor)
	if _, err := denied.PreviewDeployment(context.Background(), domain.Actor{Username: "viewer"}, validDeploymentSpec()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("PreviewDeployment() permission error = %v", err)
	}
	if repo.previewCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "denied" {
		t.Fatalf("preview calls = %d audit = %#v", repo.previewCalls, auditor.results)
	}

	service := New(repo, fakeGuard{allow: true}, auditor)
	invalid := validDeploymentSpec()
	invalid.Image = "bad image"
	if _, err := service.PreviewDeployment(context.Background(), domain.Actor{Username: "admin"}, invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("PreviewDeployment() validation error = %v", err)
	}
	if repo.previewCalls != 0 {
		t.Fatalf("repository called for invalid preview: %d", repo.previewCalls)
	}
	spec := validDeploymentSpec()
	spec.Name = "  api  "
	spec.ContainerName = ""
	manifest, err := service.PreviewDeployment(context.Background(), domain.Actor{Username: "admin"}, spec)
	if err != nil || manifest != repo.previewYAML || repo.previewCalls != 1 {
		t.Fatalf("PreviewDeployment() = (%q, %v), calls = %d", manifest, err, repo.previewCalls)
	}
	repo.previewErr = domain.ErrUnavailable
	if _, err := service.PreviewDeployment(context.Background(), domain.Actor{Username: "admin"}, validDeploymentSpec()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("PreviewDeployment() dependency error = %v", err)
	}
}

func TestDeleteValidationFailuresAreAudited(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: true}, auditor)
	if err := service.Delete(context.Background(), domain.Actor{Username: "admin"}, "pod", "prod", "api", ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Delete(invalid kind) error = %v", err)
	}
	if err := service.Delete(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", "Invalid"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Delete(invalid propagation) error = %v", err)
	}
	repo.deleteErr = domain.ErrUnavailable
	if err := service.Delete(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", "Foreground"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Delete() dependency error = %v", err)
	}
	if len(auditor.results) != 3 || auditor.results[0] != "failure" || auditor.results[1] != "failure" || auditor.results[2] != "failure" {
		t.Fatalf("audit = %#v", auditor.results)
	}

	repo = &fakeRepository{}
	auditor = &fakeAuditor{}
	service = New(repo, fakeGuard{allow: false}, auditor)
	if err := service.Delete(context.Background(), domain.Actor{Username: "viewer"}, "pod", "prod", "api", ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Delete(pod) with denied guard error = %v, want ErrUnsupported", err)
	}
	if len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("invalid delete audit = %#v", auditor.results)
	}
}

func TestWorkloadAccessMapsResourcesAndSubresources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind, verb, namespace, name, subresource string
		want                                     domain.Access
	}{
		{kind: "Deployment", verb: "update", namespace: "prod", name: "api", subresource: "scale", want: domain.Access{Verb: "update", APIGroup: "apps", Resource: "deployments", Namespace: "prod", Name: "api", Subresource: "scale"}},
		{kind: "statefulset", verb: "patch", namespace: "prod", name: "db", want: domain.Access{Verb: "patch", APIGroup: "apps", Resource: "statefulsets", Namespace: "prod", Name: "db"}},
		{kind: "daemonset", verb: "delete", namespace: "prod", name: "agent", want: domain.Access{Verb: "delete", APIGroup: "apps", Resource: "daemonsets", Namespace: "prod", Name: "agent"}},
		{kind: "job", verb: "delete", namespace: "prod", name: "migrate", want: domain.Access{Verb: "delete", APIGroup: "batch", Resource: "jobs", Namespace: "prod", Name: "migrate"}},
		{kind: "CronJob", verb: "create", namespace: "prod", name: "backup", want: domain.Access{Verb: "create", APIGroup: "batch", Resource: "cronjobs", Namespace: "prod", Name: "backup"}},
		{kind: "unknown", verb: "get", namespace: "prod", name: "api", want: domain.Access{Verb: "get", Resource: "unknown", Namespace: "prod", Name: "api"}},
	}
	for _, test := range tests {
		if got := workloadAccess(test.kind, test.verb, test.namespace, test.name, test.subresource); got != test.want {
			t.Fatalf("workloadAccess(%s) = %#v, want %#v", test.kind, got, test.want)
		}
	}
	if err := ValidateKind("pod"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("ValidateKind(pod) error = %v", err)
	}
}

func TestRevisionsValidateKindAndDelegate(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{revisions: []domain.WorkloadRevision{{Kind: "Deployment", Name: "api", Revision: 2}}}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	items, err := service.Revisions(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("Revisions() error = %v", err)
	}
	if len(items) != 1 || items[0].Revision != 2 || repo.lastKind != "deployment" || repo.lastNamespace != "prod" || repo.lastName != "api" {
		t.Fatalf("items=%#v last=(%q,%q,%q)", items, repo.lastKind, repo.lastNamespace, repo.lastName)
	}
	for _, kind := range []string{"daemonset", "job", "cronjob", "pod"} {
		if _, err := service.Revisions(context.Background(), kind, "prod", "api"); !errors.Is(err, domain.ErrUnsupported) {
			t.Fatalf("Revisions(%s) error = %v, want ErrUnsupported", kind, err)
		}
	}
}

func TestRollbackRequiresPermissionAndAudits(t *testing.T) {
	t.Parallel()
	auditor := &fakeAuditor{}
	service := New(&fakeRepository{}, fakeGuard{allow: false}, auditor)
	if err := service.Rollback(context.Background(), domain.Actor{Username: "viewer"}, "deployment", "prod", "api", 1); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Rollback() error = %v, want ErrForbidden", err)
	}
	if len(auditor.results) != 1 || auditor.results[0] != "denied" || auditor.actions[0] != "rollback" {
		t.Fatalf("audit results=%#v actions=%#v", auditor.results, auditor.actions)
	}

	repo := &fakeRepository{}
	auditor = &fakeAuditor{}
	service = New(repo, fakeGuard{allow: true}, auditor)
	if err := service.Rollback(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", 0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Rollback(invalid revision) error = %v", err)
	}
	if repo.rollbackCalls != 0 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("invalid rollback calls=%d audit=%#v", repo.rollbackCalls, auditor.results)
	}

	repo = &fakeRepository{rollbackErr: domain.ErrUnavailable}
	auditor = &fakeAuditor{}
	service = New(repo, fakeGuard{allow: true}, auditor)
	if err := service.Rollback(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", 2); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Rollback(dependency) error = %v", err)
	}
	if repo.rollbackCalls != 1 || repo.lastRevision != 2 || len(auditor.results) != 1 || auditor.results[0] != "failure" {
		t.Fatalf("rollback calls=%d revision=%d audit=%#v", repo.rollbackCalls, repo.lastRevision, auditor.results)
	}
}

func TestCreateWorkloadSupportsMultiContainerSchedulingAndBatchParameters(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	runAsUser := int64(1000)
	runAsNonRoot := true
	readOnly := true
	tolerationSeconds := int64(60)
	completions := int32(3)
	parallelism := int32(2)
	backoffLimit := int32(4)
	deadline := int64(300)
	ttl := int32(120)
	spec := domain.WorkloadCreateSpec{
		Kind: "job", Name: "migrate", Namespace: "prod",
		Containers: []domain.WorkloadContainerSpec{
			{Name: "app", Image: "registry.test/app:2", ContainerPort: 8080, Command: []string{"/app"}, Args: []string{"serve"}},
			{Name: "sidecar", Image: "registry.test/sidecar:1", Env: []domain.WorkloadEnvVar{{Name: "MODE", Value: "proxy"}}},
		},
		InitContainers:            []domain.WorkloadContainerSpec{{Name: "init-db", Image: "busybox:1.37", Command: []string{"sh", "-c", "wait-db"}}},
		NodeSelector:              map[string]string{"node-pool": "gpu"},
		Tolerations:               []domain.WorkloadToleration{{Key: "dedicated", Operator: "Equal", Value: "gpu", Effect: "NoExecute", TolerationSeconds: &tolerationSeconds}},
		NodeAffinity:              []domain.WorkloadNodeSelectorRequirement{{Key: "topology.kubernetes.io/zone", Operator: "In", Values: []string{"az-a", "az-b"}}},
		TopologySpreadConstraints: []domain.WorkloadTopologySpreadConstraint{{MaxSkew: 1, TopologyKey: "kubernetes.io/hostname", WhenUnsatisfiable: "ScheduleAnyway"}},
		PriorityClassName:         "high-priority", ServiceAccountName: "migrator",
		PodSecurityContext: &domain.WorkloadPodSecurityContext{RunAsNonRoot: &runAsNonRoot, FSGroup: &runAsUser},
		JobCompletions:     &completions, JobParallelism: &parallelism, JobBackoffLimit: &backoffLimit,
		JobActiveDeadlineSeconds: &deadline, JobTTLSecondsAfterFinished: &ttl,
	}
	spec.Containers[0].SecurityContext = &domain.WorkloadContainerSecurityContext{RunAsUser: &runAsUser, ReadOnlyRootFilesystem: &readOnly}
	if _, err := service.CreateWorkload(context.Background(), domain.Actor{Username: "admin"}, spec); err != nil {
		t.Fatalf("CreateWorkload() error = %v", err)
	}
	got := repo.lastCreateSpec
	if len(got.Containers) != 2 || len(got.InitContainers) != 1 || got.Containers[1].Name != "sidecar" || got.InitContainers[0].Image != "busybox:1.37" {
		t.Fatalf("containers = %#v init = %#v", got.Containers, got.InitContainers)
	}
	if got.NodeSelector["node-pool"] != "gpu" || len(got.Tolerations) != 1 || got.Tolerations[0].TolerationSeconds == nil || got.PriorityClassName != "high-priority" || got.ServiceAccountName != "migrator" {
		t.Fatalf("scheduling = %#v", got)
	}
	if got.PodSecurityContext == nil || got.PodSecurityContext.RunAsNonRoot == nil || !*got.PodSecurityContext.RunAsNonRoot || got.Containers[0].SecurityContext == nil || !*got.Containers[0].SecurityContext.ReadOnlyRootFilesystem {
		t.Fatalf("security context = pod %#v container %#v", got.PodSecurityContext, got.Containers[0].SecurityContext)
	}
	if got.JobCompletions == nil || *got.JobCompletions != 3 || got.JobParallelism == nil || *got.JobParallelism != 2 || got.JobBackoffLimit == nil || *got.JobBackoffLimit != 4 || got.JobTTLSecondsAfterFinished == nil || *got.JobTTLSecondsAfterFinished != 120 {
		t.Fatalf("batch parameters = %#v", got)
	}
}

func TestCreateWorkloadRejectsInvalidContainerAndSchedulingShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spec  domain.WorkloadCreateSpec
		field string
	}{
		{
			name:  "duplicate container names",
			spec:  domain.WorkloadCreateSpec{Kind: "deployment", Name: "api", Namespace: "prod", Replicas: 1, Containers: []domain.WorkloadContainerSpec{{Name: "app", Image: "nginx:1"}, {Name: "app", Image: "nginx:2"}}},
			field: "containers.1.name",
		},
		{
			name:  "invalid container security context",
			spec:  domain.WorkloadCreateSpec{Kind: "deployment", Name: "api", Namespace: "prod", Replicas: 1, Containers: []domain.WorkloadContainerSpec{{Name: "app", Image: "nginx:1", SecurityContext: &domain.WorkloadContainerSecurityContext{CapabilitiesAdd: []string{"net_admin"}}}}},
			field: "containers.0.securityContext.capabilities.0",
		},
		{
			name:  "invalid toleration seconds effect",
			spec:  domain.WorkloadCreateSpec{Kind: "deployment", Name: "api", Namespace: "prod", Replicas: 1, Containers: []domain.WorkloadContainerSpec{{Name: "app", Image: "nginx:1"}}, Tolerations: []domain.WorkloadToleration{{Key: "dedicated", Operator: "Exists", Effect: "NoSchedule", TolerationSeconds: int64Pointer(10)}}},
			field: "tolerations.0.tolerationSeconds",
		},
		{
			name:  "invalid topology policy",
			spec:  domain.WorkloadCreateSpec{Kind: "deployment", Name: "api", Namespace: "prod", Replicas: 1, Containers: []domain.WorkloadContainerSpec{{Name: "app", Image: "nginx:1"}}, TopologySpreadConstraints: []domain.WorkloadTopologySpreadConstraint{{MaxSkew: 1, TopologyKey: "zone", WhenUnsatisfiable: "Sometimes"}}},
			field: "topologySpreadConstraints.0.whenUnsatisfiable",
		},
		{
			name:  "job completions out of range",
			spec:  domain.WorkloadCreateSpec{Kind: "job", Name: "api", Namespace: "prod", Containers: []domain.WorkloadContainerSpec{{Name: "app", Image: "nginx:1"}}, JobCompletions: int32Pointer(0)},
			field: "jobCompletions",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
			_, err := service.CreateWorkload(context.Background(), domain.Actor{Username: "admin"}, tt.spec)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || validation.Field != tt.field {
				t.Fatalf("validation = %#v, want field %q", validation, tt.field)
			}
			if len(repo.createCalls) != 0 {
				t.Fatalf("repository called for invalid spec: %#v", repo.createCalls)
			}
		})
	}
}

func TestUpdateRolloutValidatesAuthorizesAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	service := New(repo, fakeGuard{allow: true}, auditor)
	paused := true
	if err := service.UpdateRollout(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", domain.WorkloadRolloutUpdate{Paused: &paused, MaxSurge: "25%", MaxUnavailable: "0"}); err != nil {
		t.Fatalf("UpdateRollout() error = %v", err)
	}
	if repo.rolloutCalls != 1 || repo.lastRollout.Paused == nil || !*repo.lastRollout.Paused || repo.lastRollout.MaxSurge != "25%" || repo.lastRollout.MaxUnavailable != "0" {
		t.Fatalf("rollout = %#v calls = %d", repo.lastRollout, repo.rolloutCalls)
	}
	if len(auditor.actions) != 1 || auditor.actions[0] != "update-rollout" || auditor.results[0] != "success" {
		t.Fatalf("audit = %#v %#v", auditor.actions, auditor.results)
	}
	for _, update := range []domain.WorkloadRolloutUpdate{{}, {MaxSurge: "-1"}, {MaxSurge: "101%"}, {MaxSurge: "0", MaxUnavailable: "0%"}} {
		if err := service.UpdateRollout(context.Background(), domain.Actor{Username: "admin"}, "deployment", "prod", "api", update); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("UpdateRollout(%#v) error = %v", update, err)
		}
	}
	if err := service.UpdateRollout(context.Background(), domain.Actor{Username: "admin"}, "statefulset", "prod", "db", domain.WorkloadRolloutUpdate{Paused: &paused}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("statefulset error = %v", err)
	}
	partition := int32(2)
	if err := service.UpdateRollout(context.Background(), domain.Actor{Username: "admin"}, "statefulset", "prod", "db", domain.WorkloadRolloutUpdate{Partition: &partition}); err != nil {
		t.Fatalf("statefulset partition error = %v", err)
	}
	if repo.lastRollout.Partition == nil || *repo.lastRollout.Partition != 2 || repo.lastRollout.Paused != nil || repo.lastRollout.MaxSurge != "" {
		t.Fatalf("statefulset rollout = %#v", repo.lastRollout)
	}
	if err := service.UpdateRollout(context.Background(), domain.Actor{Username: "admin"}, "daemonset", "prod", "agent", domain.WorkloadRolloutUpdate{MaxSurge: "1", MaxUnavailable: "0"}); err != nil {
		t.Fatalf("daemonset rollout error = %v", err)
	}
	if repo.lastRollout.MaxSurge != "1" || repo.lastRollout.MaxUnavailable != "0" || repo.lastRollout.Partition != nil {
		t.Fatalf("daemonset rollout = %#v", repo.lastRollout)
	}
	if err := service.UpdateRollout(context.Background(), domain.Actor{Username: "admin"}, "daemonset", "prod", "agent", domain.WorkloadRolloutUpdate{MaxSurge: "0", MaxUnavailable: "0"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("daemonset both zero error = %v", err)
	}
	deniedRepo := &fakeRepository{}
	denied := New(deniedRepo, fakeGuard{allow: false}, &fakeAuditor{})
	if err := denied.UpdateRollout(context.Background(), domain.Actor{Username: "viewer"}, "deployment", "prod", "api", domain.WorkloadRolloutUpdate{Paused: &paused}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denied error = %v", err)
	}
	if deniedRepo.rolloutCalls != 0 {
		t.Fatalf("repository called without permission")
	}
}

func TestRunCronJobOncePassesContainerAndCommandOverrides(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	service := New(repo, fakeGuard{allow: true}, &fakeAuditor{})
	input := domain.CronJobRunInput{ContainerName: "worker", Command: []string{"/bin/sh", "-c"}, Args: []string{"backup --once"}}
	if _, err := service.RunCronJobOnce(context.Background(), domain.Actor{Username: "admin"}, "prod", "backup", input); err != nil {
		t.Fatalf("RunCronJobOnce() error = %v", err)
	}
	if repo.lastRunInput.ContainerName != "worker" || len(repo.lastRunInput.Command) != 2 || repo.lastRunInput.Args[0] != "backup --once" {
		t.Fatalf("run input = %#v", repo.lastRunInput)
	}
}

func int32Pointer(value int32) *int32 { return &value }
func int64Pointer(value int64) *int64 { return &value }
