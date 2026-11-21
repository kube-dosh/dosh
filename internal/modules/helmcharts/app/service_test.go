package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/authz"
)

type memoryHelmRepository struct {
	items      []domain.HelmRepository
	operations map[string]domain.HelmChartOperation
}

func (r *memoryHelmRepository) ListHelmRepositories(context.Context) ([]domain.HelmRepository, error) {
	return append([]domain.HelmRepository(nil), r.items...), nil
}

func (r *memoryHelmRepository) GetHelmRepository(_ context.Context, id string) (domain.HelmRepository, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.HelmRepository{}, domain.ErrNotFound
}

func (r *memoryHelmRepository) SaveHelmRepository(_ context.Context, repository domain.HelmRepository) (domain.HelmRepository, error) {
	repository.Revision = "revision-1"
	repository.UpdatedAt = time.Now().UTC()
	repository.HasCredentials = repository.Credentials.Username != "" || repository.Credentials.Password != "" || repository.Credentials.Token != ""
	repository.HasCustomCA = repository.Credentials.CAPEM != ""
	for index := range r.items {
		if r.items[index].ID == repository.ID {
			r.items[index] = repository
			return repository, nil
		}
	}
	r.items = append(r.items, repository)
	return repository, nil
}

func (r *memoryHelmRepository) DeleteHelmRepository(_ context.Context, id, _ string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *memoryHelmRepository) SaveHelmChartOperation(_ context.Context, operation domain.HelmChartOperation) error {
	if r.operations == nil {
		r.operations = map[string]domain.HelmChartOperation{}
	}
	r.operations[operation.ID] = operation
	return nil
}

func (r *memoryHelmRepository) GetHelmChartOperation(_ context.Context, id string) (domain.HelmChartOperation, error) {
	operation, ok := r.operations[id]
	if !ok {
		return domain.HelmChartOperation{}, domain.ErrNotFound
	}
	return operation, nil
}

type fakeHelmSource struct {
	archive []byte
	digest  string
}

func (s fakeHelmSource) Charts(_ context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	return []domain.HelmChartSummary{{RepositoryID: repository.ID, RepositoryName: repository.Name, Name: "demo", Version: "1.0.0"}}, nil
}

func (s fakeHelmSource) Detail(_ context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
	return domain.HelmChartDetail{HelmChartSummary: domain.HelmChartSummary{RepositoryID: repository.ID, RepositoryName: repository.Name, Name: chartName, Version: "1.0.0"}, Values: "{}"}, nil
}

func (s fakeHelmSource) Download(context.Context, domain.HelmRepository, string, string, string) ([]byte, string, error) {
	return s.archive, s.digest, nil
}

type fakeHelmExecutor struct {
	template domain.HelmChartPlanTemplate
	installs int
}

func (e *fakeHelmExecutor) PlanHelmChart(context.Context, domain.Actor, []byte, domain.HelmChartPlanRequest) (domain.HelmChartPlanTemplate, error) {
	return e.template, nil
}

func (e *fakeHelmExecutor) InstallHelmChart(context.Context, domain.Actor, []byte, domain.HelmChartPlanRequest) (int, error) {
	e.installs++
	return 1, nil
}

func (e *fakeHelmExecutor) UpgradeHelmChart(context.Context, domain.Actor, []byte, domain.HelmChartPlanRequest) (int, error) {
	return 2, nil
}

func (e *fakeHelmExecutor) UninstallHelmChart(context.Context, domain.Actor, string, string) error {
	return nil
}

func (e *fakeHelmExecutor) RollbackHelmChart(context.Context, domain.Actor, string, string, int) (int, error) {
	return 2, nil
}

func testHelmActor() domain.Actor { return domain.Actor{Username: "admin", Admin: true} }

func TestCreateRepositoryStripsSecretsAndRecordsAuthType(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	created, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "Private", URL: "https://charts.example.com", AuthType: domain.HelmRepositoryAuthBasic,
		Username: "robot", Password: "secret", CAPEM: "ca-data",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if created.AuthType != domain.HelmRepositoryAuthBasic || !created.HasCredentials || !created.HasCustomCA {
		t.Fatalf("CreateRepository() flags = %#v", created)
	}
	if created.Credentials.Username != "" || created.Credentials.Password != "" || created.Credentials.CAPEM != "" {
		t.Fatalf("CreateRepository() leaked credentials: %#v", created.Credentials)
	}
	stored := repository.items[0]
	if stored.Credentials.Username != "robot" || stored.Credentials.Password != "secret" || stored.Credentials.CAPEM != "ca-data" {
		t.Fatalf("stored credentials = %#v", stored.Credentials)
	}
}

func TestCreateRepositoryAcceptsOCIPathAndRejectsTag(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	created, err := service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "OCI", URL: "oci://registry.example.com/team/charts/demo", AuthType: domain.HelmRepositoryAuthAnonymous,
	})
	if err != nil {
		t.Fatalf("CreateRepository() OCI error = %v", err)
	}
	if created.URL != "oci://registry.example.com/team/charts/demo" || created.ChartCount != 1 || created.LastSync == nil || !created.LastSync.OK {
		t.Fatalf("CreateRepository() OCI = %#v", created)
	}

	_, err = service.CreateRepository(context.Background(), testHelmActor(), domain.HelmRepositoryUpsert{
		Name: "Invalid OCI", URL: "oci://registry.example.com/team/charts/demo:1.2.3",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("CreateRepository() tagged OCI error = %v", err)
	}
}

func TestUpdateRepositoryAnonymousClearsAuthButKeepsCA(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Private", URL: "https://charts.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthBasic, Revision: "revision-1",
		Credentials: domain.HelmRepositoryCredentials{Username: "robot", Password: "secret", CAPEM: "ca-data"},
	}}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	updated, err := service.UpdateRepository(context.Background(), testHelmActor(), "repo-1", domain.HelmRepositoryUpsert{
		Name: "Private", URL: "https://charts.example.com", AuthType: domain.HelmRepositoryAuthAnonymous, CAPEM: "ca-data",
	})
	if err != nil {
		t.Fatalf("UpdateRepository() error = %v", err)
	}
	stored := repository.items[0]
	if updated.AuthType != domain.HelmRepositoryAuthAnonymous || stored.Credentials.Username != "" || stored.Credentials.Password != "" {
		t.Fatalf("anonymous update kept auth credentials: %#v", stored.Credentials)
	}
	if stored.Credentials.CAPEM != "ca-data" || !updated.HasCustomCA {
		t.Fatalf("anonymous update did not retain CA: %#v", stored.Credentials)
	}
}

func TestUpdateRepositoryHostChangeRequiresExplicitCredentialHandling(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Private", URL: "https://old.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthBasic, Revision: "revision-1",
		Credentials: domain.HelmRepositoryCredentials{Username: "robot", Password: "secret"},
	}}}
	service := New(repository, fakeHelmSource{}, &fakeHelmExecutor{}, authz.NewLocal(false), nil)

	_, err := service.UpdateRepository(context.Background(), testHelmActor(), "repo-1", domain.HelmRepositoryUpsert{Name: "Private", URL: "https://new.example.com"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "host") {
		t.Fatalf("UpdateRepository() host change error = %v", err)
	}

	updated, err := service.UpdateRepository(context.Background(), testHelmActor(), "repo-1", domain.HelmRepositoryUpsert{
		Name: "Private", URL: "https://new.example.com", AuthType: domain.HelmRepositoryAuthBasic, Username: "new-robot", Password: "new-secret",
	})
	if err != nil {
		t.Fatalf("UpdateRepository() explicit credential error = %v", err)
	}
	stored := repository.items[0]
	if stored.Credentials.Username != "new-robot" || stored.Credentials.Password != "new-secret" || !updated.HasCredentials {
		t.Fatalf("explicit credential update = %#v", stored.Credentials)
	}
}

func TestInstallRejectsStalePlanDigestBeforeExecuting(t *testing.T) {
	t.Parallel()
	repository := &memoryHelmRepository{items: []domain.HelmRepository{{
		ID: "repo-1", Name: "Public", URL: "https://charts.example.com", Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous,
	}}}
	executor := &fakeHelmExecutor{template: domain.HelmChartPlanTemplate{
		Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
		Values:   "replicas: 1\n", CurrentRevision: 0,
		Resources: []domain.HelmChartPlanResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "demo", Resource: "configmaps", Action: "create"}},
	}}
	service := New(repository, fakeHelmSource{archive: []byte("chart"), digest: "sha256:abc"}, executor, authz.NewLocal(false), nil)
	request := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{
		RepositoryID: "repo-1", ChartName: "demo", Version: "1.0.0",
	}, Mode: "install", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1\n"}

	plan, err := service.Plan(context.Background(), testHelmActor(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.PlanDigest == "" || plan.Digest != "sha256:abc" || len(plan.Resources) != 1 {
		t.Fatalf("Plan() = %#v", plan)
	}

	_, err = service.Install(context.Background(), testHelmActor(), domain.HelmChartOperationRequest{
		HelmChartPlanRequest: request, PlanDigest: "sha256:stale", Confirmation: "demo",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Install() stale digest error = %v", err)
	}
	if executor.installs != 0 {
		t.Fatalf("Install() executed %d times after stale plan", executor.installs)
	}
}

func TestPlanDigestChangesWithReviewedValues(t *testing.T) {
	t.Parallel()
	repository := domain.HelmRepository{ID: "repo-1", URL: "https://charts.example.com"}
	template := domain.HelmChartPlanTemplate{Manifest: "manifest", Values: "replicas: 1", CurrentRevision: 1}
	changedTemplate := template
	changedTemplate.Values = "replicas: 2"
	base := domain.HelmChartPlanRequest{HelmChartRef: domain.HelmChartRef{ChartName: "demo", Version: "1.0.0"}, Mode: "upgrade", Namespace: "default", ReleaseName: "demo", Values: "replicas: 1"}
	changed := base
	changed.Values = "replicas: 2"
	if computePlanDigest(base, repository, "sha256:abc", template) == computePlanDigest(changed, repository, "sha256:abc", changedTemplate) {
		t.Fatal("computePlanDigest() did not change with reviewed values")
	}
}
