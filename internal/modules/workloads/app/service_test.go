package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

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
	lastLogStream  io.ReadCloser
	createCalls    []bool
	previewCalls   int
	createErr      error
	previewYAML    string
	previewErr     error
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
	pods           []domain.PodSummary
	podsErr        error
	podDetail      domain.PodDetail
	podErr         error
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
	f.createCalls = append(f.createCalls, dryRun)
	if f.createErr != nil {
		return domain.WorkloadSummary{}, f.createErr
	}
	return domain.WorkloadSummary{Kind: "Deployment", Name: spec.Name, Namespace: spec.Namespace, Desired: spec.Replicas}, nil
}
func (f *fakeRepository) PreviewDeployment(context.Context, domain.DeploymentCreateSpec) (string, error) {
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
func (f *fakeRepository) StreamPodLogs(_ context.Context, _ string, _ string, _ string, _, previous bool, tailLines int64, _ bool, limitBytes int64) (io.ReadCloser, error) {
	f.tailLines = tailLines
	f.limitBytes = limitBytes
	f.previous = previous
	f.lastLogStream = io.NopCloser(strings.NewReader("log line"))
	return f.lastLogStream, nil
}

type fakeGuard struct {
	allow  bool
	err    error
	access *domain.Access
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
}

func (a *fakeAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events++
	a.results = append(a.results, event.Result)
	a.actions = append(a.actions, event.Action)
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
	stream, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "default", "pod", "app", false, false, 999999, false)
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
	if _, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "default", "pod", "app", true, false, 100, false); err != nil {
		t.Fatalf("StreamLogs(follow) error = %v", err)
	}
	if repo.limitBytes != 0 {
		t.Fatalf("follow limitBytes = %d, want 0", repo.limitBytes)
	}
	if _, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "default", "pod", "app", false, false, -1, false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative tailLines error = %v, want ErrInvalid", err)
	}
}

func TestStreamLogsAuthorizesPodsLogAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeRepository{}
	auditor := &fakeAuditor{}
	var access domain.Access
	service := New(repo, fakeGuard{allow: true, access: &access}, auditor)
	stream, err := service.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "prod", "api-0", "app", false, true, 100, true)
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
	if _, err := denied.StreamLogs(context.Background(), domain.Actor{Username: "alice"}, "prod", "api-0", "app", false, false, 100, false); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("StreamLogs(denied) error = %v", err)
	}
	if deniedRepo.lastLogStream != nil || len(deniedAuditor.results) != 1 || deniedAuditor.results[0] != "denied" {
		t.Fatalf("denied stream reached repository or audit was wrong: %#v %#v", deniedRepo.lastLogStream, deniedAuditor.results)
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
