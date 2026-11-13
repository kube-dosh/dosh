package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"
)

type fakeResourceRepository struct {
	descriptors []domain.ResourceDescriptor
	warnings    []string
	discoverErr error
	listResult  domain.PageResult[domain.GenericResourceSummary]
	listErr     error
	object      map[string]any
	getErr      error

	lastDescriptor domain.ResourceDescriptor
	lastQuery      domain.ListQuery
	lastPage       domain.PageRequest
	lastNamespace  string
	lastName       string
	listCalls      int
	getCalls       int
}

func (r *fakeResourceRepository) DiscoverResources(context.Context) ([]domain.ResourceDescriptor, []string, error) {
	return r.descriptors, r.warnings, r.discoverErr
}

func (r *fakeResourceRepository) ListGenericResources(_ context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.GenericResourceSummary], error) {
	r.listCalls++
	r.lastDescriptor, r.lastQuery, r.lastPage = descriptor, query, page
	return r.listResult, r.listErr
}

func (r *fakeResourceRepository) GetGenericResource(_ context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (map[string]any, error) {
	r.getCalls++
	r.lastDescriptor, r.lastNamespace, r.lastName = descriptor, namespace, name
	return r.object, r.getErr
}

type recordingAuthorizer struct {
	accesses []domain.Access
	err      error
}

func (a *recordingAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.accesses = append(a.accesses, access)
	return a.err
}

type recordingAuditor struct {
	events []domain.AuditEvent
}

func (a *recordingAuditor) Record(_ context.Context, _ domain.Actor, event domain.AuditEvent) {
	a.events = append(a.events, event)
}

func resourceServiceFixture() (*Service, *fakeResourceRepository, *recordingAuthorizer, *recordingAuditor) {
	repository := &fakeResourceRepository{
		descriptors: []domain.ResourceDescriptor{
			{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true},
			{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true},
			{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true},
			{Group: "", Version: "v1", Resource: "nodes", Kind: "Node"},
			{Group: "example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true},
		},
	}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	return New(repository, authorizer, auditor), repository, authorizer, auditor
}

func TestServiceDiscoveryPassesThroughRepositoryMetadata(t *testing.T) {
	service, repository, _, _ := resourceServiceFixture()
	repository.warnings = []string{"one API group is unavailable"}

	items, warnings, err := service.Discovery(context.Background())
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}
	if len(items) != 5 || items[4].Group != "example.io" {
		t.Fatalf("Discovery() items = %#v", items)
	}
	if len(warnings) != 1 || warnings[0] != "one API group is unavailable" {
		t.Fatalf("Discovery() warnings = %#v", warnings)
	}
}

func TestServiceListValidatesGVRAndAuthorizesRequestedNamespace(t *testing.T) {
	service, repository, authorizer, _ := resourceServiceFixture()
	repository.listResult = domain.PageResult[domain.GenericResourceSummary]{
		Items:      []domain.GenericResourceSummary{{Name: "api-0"}},
		Total:      1,
		TotalKnown: true,
	}
	actor := domain.Actor{Username: "alice", Groups: []string{"viewers"}}
	query := domain.ListQuery{Namespace: "prod", Search: "api"}
	page := domain.PageRequest{Number: 2, Size: 10, Cursor: "cursor"}

	result, err := service.List(context.Background(), actor, domain.ResourceReference{
		Version: "v1", Resource: "pods",
	}, query, page)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Name != "api-0" {
		t.Fatalf("List() result = %#v", result)
	}
	if repository.listCalls != 1 || repository.lastDescriptor.Resource != "pods" || repository.lastQuery != query || repository.lastPage != page {
		t.Fatalf("repository call = (%d, %#v, %#v, %#v)", repository.listCalls, repository.lastDescriptor, repository.lastQuery, repository.lastPage)
	}
	if len(authorizer.accesses) != 1 {
		t.Fatalf("authorization calls = %#v", authorizer.accesses)
	}
	want := domain.Access{Verb: "list", Resource: "pods", Namespace: "prod"}
	if authorizer.accesses[0] != want {
		t.Fatalf("authorization = %#v, want %#v", authorizer.accesses[0], want)
	}
}

func TestServiceListClearsNamespaceForClusterScopedResource(t *testing.T) {
	service, repository, authorizer, _ := resourceServiceFixture()
	_, err := service.List(context.Background(), domain.Actor{Username: "admin", Admin: true}, domain.ResourceReference{
		Version: "v1", Resource: "nodes",
	}, domain.ListQuery{Namespace: "ignored"}, domain.PageRequest{Size: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if repository.lastQuery.Namespace != "" {
		t.Fatalf("repository namespace = %q, want empty", repository.lastQuery.Namespace)
	}
	if len(authorizer.accesses) != 1 || authorizer.accesses[0].Namespace != "" {
		t.Fatalf("authorization = %#v", authorizer.accesses)
	}
}

func TestServiceRejectsUnknownOrInvalidResourceReferences(t *testing.T) {
	service, repository, authorizer, _ := resourceServiceFixture()
	tests := []struct {
		name string
		ref  domain.ResourceReference
		want error
	}{
		{name: "missing version", ref: domain.ResourceReference{Resource: "pods"}, want: domain.ErrInvalid},
		{name: "missing resource", ref: domain.ResourceReference{Version: "v1"}, want: domain.ErrInvalid},
		{name: "not discovered", ref: domain.ResourceReference{Version: "v1", Resource: "missing"}, want: domain.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.List(context.Background(), domain.Actor{Username: "admin"}, test.ref, domain.ListQuery{}, domain.PageRequest{})
			if !errors.Is(err, test.want) {
				t.Fatalf("List() error = %v, want %v", err, test.want)
			}
		})
	}
	if repository.listCalls != 0 || len(authorizer.accesses) != 0 {
		t.Fatalf("invalid references reached repository/authorizer: %d, %#v", repository.listCalls, authorizer.accesses)
	}
}

func TestServiceGetRequiresNamespaceForNamespacedResource(t *testing.T) {
	service, repository, _, _ := resourceServiceFixture()
	_, err := service.Get(context.Background(), domain.Actor{Username: "admin"}, domain.ResourceReference{
		Version: "v1", Resource: "pods",
	}, "", "api-0")
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Get() error = %v, want ErrInvalid", err)
	}
	if repository.getCalls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.getCalls)
	}
}

func TestServiceGetRedactsSecretAndAuditsSuccessfulRead(t *testing.T) {
	service, repository, authorizer, auditor := resourceServiceFixture()
	repository.object = map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name": "credentials", "namespace": "prod", "uid": "secret-uid", "resourceVersion": "12",
			"creationTimestamp": "2026-11-01T00:00:00Z",
			"labels":            map[string]any{"app": "api"},
			"annotations":       map[string]any{"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"password":"c2VjcmV0"}}`},
			"managedFields":     []any{map[string]any{"manager": "kubectl"}},
		},
		"data":       map[string]any{"password": "c2VjcmV0"},
		"stringData": map[string]any{"token": "plain"},
	}

	detail, err := service.Get(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Version: "v1", Resource: "secrets",
	}, "prod", "credentials")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !detail.Redacted || detail.Name != "credentials" || detail.UID != "secret-uid" || detail.Labels["app"] != "api" {
		t.Fatalf("detail = %#v", detail)
	}
	if _, ok := detail.Object["data"]; ok {
		t.Fatal("secret data was not redacted")
	}
	if _, ok := detail.Object["stringData"]; ok {
		t.Fatal("secret stringData was not redacted")
	}
	metadata := detail.Object["metadata"].(map[string]any)
	if _, ok := metadata["managedFields"]; ok {
		t.Fatal("managedFields was not stripped")
	}
	annotations := metadata["annotations"].(map[string]any)
	if annotations["kubectl.kubernetes.io/last-applied-configuration"] != "[redacted]" {
		t.Fatalf("annotations were not redacted: %#v", annotations)
	}
	if strings.Contains(detail.YAML, "c2VjcmV0") || strings.Contains(detail.YAML, "plain") {
		t.Fatalf("YAML contains secret material: %s", detail.YAML)
	}
	if len(detail.Warnings) != 1 || !strings.Contains(detail.Warnings[0], "遮蔽") {
		t.Fatalf("warnings = %#v", detail.Warnings)
	}
	if len(authorizer.accesses) != 1 {
		t.Fatalf("authorization calls = %#v", authorizer.accesses)
	}
	wantAccess := domain.Access{Verb: "get", Resource: "secrets", Namespace: "prod", Name: "credentials"}
	if authorizer.accesses[0] != wantAccess {
		t.Fatalf("authorization = %#v, want %#v", authorizer.accesses[0], wantAccess)
	}
	if len(auditor.events) != 1 {
		t.Fatalf("audit events = %#v", auditor.events)
	}
	event := auditor.events[0]
	if event.Action != "get-generic-secret" || event.Resource != "secret" || event.Namespace != "prod" || event.Name != "credentials" || event.Result != "success" || event.Err != nil {
		t.Fatalf("audit event = %#v", event)
	}
}

func TestServiceGetAuditsDeniedSecretReadWithoutRepositoryCall(t *testing.T) {
	service, repository, authorizer, auditor := resourceServiceFixture()
	authorizer.err = domain.ErrForbidden

	_, err := service.Get(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Version: "v1", Resource: "secrets",
	}, "prod", "credentials")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Get() error = %v, want ErrForbidden", err)
	}
	if repository.getCalls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.getCalls)
	}
	if len(auditor.events) != 1 {
		t.Fatalf("audit events = %#v", auditor.events)
	}
	event := auditor.events[0]
	if event.Result != "failure" || !errors.Is(event.Err, domain.ErrForbidden) {
		t.Fatalf("audit event = %#v", event)
	}
}

func TestServiceGetDoesNotRedactRegularResources(t *testing.T) {
	service, repository, _, auditor := resourceServiceFixture()
	repository.object = map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "settings", "namespace": "prod"},
		"data":       map[string]any{"mode": "production"},
	}

	detail, err := service.Get(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Version: "v1", Resource: "configmaps",
	}, "prod", "settings")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if detail.Redacted || len(detail.Warnings) != 0 || len(auditor.events) != 0 {
		t.Fatalf("detail = %#v, audits = %#v", detail, auditor.events)
	}
	if !strings.Contains(detail.YAML, "mode: production") {
		t.Fatalf("YAML did not contain object data: %s", detail.YAML)
	}
}
