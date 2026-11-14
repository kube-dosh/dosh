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
	listResult  domain.GenericResourcePage
	listErr     error
	rules       domain.ResourceRules
	rulesErr    error
	object      map[string]any
	getErr      error

	lastDescriptor domain.ResourceDescriptor
	lastQuery      domain.ListQuery
	lastPage       domain.PageRequest
	lastNamespace  string
	lastName       string
	seenResources  []string
	listCalls      int
	getCalls       int
	mutation       map[string]any
	mutationErr    error
	mutationErrAt  int
	lastMutation   string
	lastMutations  int
	mutationDryRun []bool
}

func (r *fakeResourceRepository) DiscoverResources(context.Context) ([]domain.ResourceDescriptor, []string, error) {
	return r.descriptors, r.warnings, r.discoverErr
}

func (r *fakeResourceRepository) ListResourceRules(context.Context, domain.Actor, string) (domain.ResourceRules, []string, error) {
	return r.rules, nil, r.rulesErr
}

func (r *fakeResourceRepository) ListGenericResources(_ context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.GenericResourcePage, error) {
	r.listCalls++
	r.lastDescriptor, r.lastQuery, r.lastPage = descriptor, query, page
	r.seenResources = append(r.seenResources, descriptor.Resource)
	return r.listResult, r.listErr
}

func (r *fakeResourceRepository) GetGenericResource(_ context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (map[string]any, error) {
	r.getCalls++
	r.lastDescriptor, r.lastNamespace, r.lastName = descriptor, namespace, name
	return r.object, r.getErr
}

func (r *fakeResourceRepository) recordMutation(action string, descriptor domain.ResourceDescriptor, namespace string, dryRun bool) error {
	r.lastMutation = action
	r.lastMutations++
	r.mutationDryRun = append(r.mutationDryRun, dryRun)
	r.lastDescriptor, r.lastNamespace = descriptor, namespace
	if r.mutationErrAt > 0 && r.lastMutations == r.mutationErrAt {
		return r.mutationErr
	}
	return nil
}

func (r *fakeResourceRepository) CreateGenericResource(_ context.Context, descriptor domain.ResourceDescriptor, namespace string, _ map[string]any, dryRun bool) (map[string]any, error) {
	if err := r.recordMutation("create", descriptor, namespace, dryRun); err != nil {
		return nil, err
	}
	return r.mutation, nil
}

func (r *fakeResourceRepository) UpdateGenericResource(_ context.Context, descriptor domain.ResourceDescriptor, namespace string, _ map[string]any, _ string, dryRun bool) (map[string]any, error) {
	if err := r.recordMutation("update", descriptor, namespace, dryRun); err != nil {
		return nil, err
	}
	return r.mutation, nil
}

func (r *fakeResourceRepository) ApplyGenericResource(_ context.Context, descriptor domain.ResourceDescriptor, namespace string, _ map[string]any, _ string, dryRun bool) (map[string]any, error) {
	if err := r.recordMutation("apply", descriptor, namespace, dryRun); err != nil {
		return nil, err
	}
	return r.mutation, nil
}

func (r *fakeResourceRepository) DeleteGenericResource(_ context.Context, descriptor domain.ResourceDescriptor, namespace, name, _ string, _ domain.DeletionPropagation) error {
	r.lastMutation, r.lastMutations = "delete", r.lastMutations+1
	r.lastDescriptor, r.lastNamespace, r.lastName = descriptor, namespace, name
	return r.mutationErr
}

type recordingAuthorizer struct {
	accesses []domain.Access
	err      error
	denied   map[string]bool
}

func (a *recordingAuthorizer) Require(_ context.Context, _ domain.Actor, access domain.Access) error {
	a.accesses = append(a.accesses, access)
	if a.denied[access.Verb] {
		return domain.ErrForbidden
	}
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
			{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
			{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
			{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
			{Group: "", Version: "v1", Resource: "nodes", Kind: "Node", Verbs: []string{"get", "list", "update", "delete"}},
			{Group: "example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
		},
	}
	authorizer := &recordingAuthorizer{}
	auditor := &recordingAuditor{}
	return New(repository, authorizer, auditor), repository, authorizer, auditor
}

func TestServiceDiscoveryPassesThroughRepositoryMetadata(t *testing.T) {
	service, repository, _, _ := resourceServiceFixture()
	repository.warnings = []string{"one API group is unavailable"}

	items, warnings, err := service.Discovery(context.Background(), domain.Actor{Username: "alice"}, "")
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

func TestServiceDiscoveryFiltersTypesBySelfSubjectRules(t *testing.T) {
	service, repository, _, _ := resourceServiceFixture()
	repository.rules = domain.ResourceRules{
		Namespaced: []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
		Cluster:    []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"nodes"}}},
	}

	items, warnings, err := service.Discovery(context.Background(), domain.Actor{Username: "alice"}, "prod")
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}
	if len(items) != 2 || items[0].Resource != "pods" || items[1].Resource != "nodes" {
		t.Fatalf("Discovery() items = %#v", items)
	}
	if len(warnings) != 0 {
		t.Fatalf("Discovery() warnings = %#v", warnings)
	}
}

func TestServiceDiscoveryFailsClosedWhenRulesAreUnavailable(t *testing.T) {
	service, repository, _, _ := resourceServiceFixture()
	repository.rulesErr = domain.ErrUnavailable

	items, warnings, err := service.Discovery(context.Background(), domain.Actor{Username: "alice"}, "prod")
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("Discovery() items = %#v, want none", items)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "失败关闭") {
		t.Fatalf("Discovery() warnings = %#v", warnings)
	}
}

func TestServiceSearchSearchesOnlyClusterResourcesWithoutNamespace(t *testing.T) {
	service, repository, _, _ := resourceServiceFixture()
	repository.rules = domain.ResourceRules{
		Namespaced: []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
		Cluster:    []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"nodes"}}},
	}
	repository.listResult = domain.GenericResourcePage{PageResult: domain.PageResult[domain.GenericResourceSummary]{
		Items: []domain.GenericResourceSummary{{Kind: "Node", Name: "worker-1"}}, Total: 1, TotalKnown: true,
	}}

	result, err := service.Search(context.Background(), domain.Actor{Username: "alice"}, "worker", "", 20)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Kind != "Node" {
		t.Fatalf("Search() items = %#v", result.Items)
	}
	if len(repository.seenResources) != 1 || repository.seenResources[0] != "nodes" {
		t.Fatalf("searched resources = %#v", repository.seenResources)
	}
}

func TestDescriptorListableHonorsWildcardsAndResourceNames(t *testing.T) {
	wildcardRules := domain.ResourceRules{Namespaced: []domain.ResourceRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}}
	descriptor := domain.ResourceDescriptor{Group: "example.io", Version: "v1", Resource: "widgets", Namespaced: true}
	if !descriptorListable(domain.ResourceDescriptor{Resource: "pods", Namespaced: true}, wildcardRules, "prod") {
		t.Fatal("wildcard rule did not authorize pods")
	}
	namedRules := domain.ResourceRules{Namespaced: []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{"example.io"}, Resources: []string{"widgets"}, ResourceNames: []string{"one"}}}}
	if descriptorListable(descriptor, namedRules, "prod") {
		t.Fatal("resourceNames rule must not authorize an unbounded list")
	}
	wildcardNames := domain.ResourceRules{Namespaced: []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{"example.io"}, Resources: []string{"widgets"}, ResourceNames: []string{"*"}}}}
	if !descriptorListable(descriptor, wildcardNames, "prod") {
		t.Fatal("wildcard resourceNames should authorize an unbounded list")
	}
	if descriptorListable(descriptor, namedRules, "") {
		t.Fatal("namespaced resource must not be listable without a namespace")
	}
}

func TestServiceListValidatesGVRAndAuthorizesRequestedNamespace(t *testing.T) {
	service, repository, authorizer, _ := resourceServiceFixture()
	repository.listResult = domain.GenericResourcePage{
		PageResult: domain.PageResult[domain.GenericResourceSummary]{
			Items:      []domain.GenericResourceSummary{{Name: "api-0"}},
			Total:      1,
			TotalKnown: true,
		},
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

func genericWidgetManifest(name string) string {
	return `apiVersion: example.io/v1
kind: Widget
metadata:
  name: ` + name + `
  namespace: prod
spec:
  size: large
`
}

func TestServiceCreateRunsImplicitDryRunBeforeWrite(t *testing.T) {
	service, repository, _, auditor := resourceServiceFixture()
	repository.mutation = map[string]any{
		"apiVersion": "example.io/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "sample", "namespace": "prod", "resourceVersion": "3"},
	}

	detail, err := service.Create(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Group: "example.io", Version: "v1", Resource: "widgets",
	}, domain.GenericResourceMutation{YAML: genericWidgetManifest("sample"), Namespace: "prod"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if detail.Name != "sample" || detail.Namespace != "prod" {
		t.Fatalf("detail = %#v", detail)
	}
	if repository.lastMutations != 2 || len(repository.mutationDryRun) != 2 || !repository.mutationDryRun[0] || repository.mutationDryRun[1] {
		t.Fatalf("mutation dry-run sequence = %#v, calls = %d", repository.mutationDryRun, repository.lastMutations)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "create-generic-resource" || auditor.events[0].Result != "success" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestServiceCreateStopsWhenImplicitDryRunFails(t *testing.T) {
	service, repository, _, auditor := resourceServiceFixture()
	repository.mutationErr = domain.ErrInvalid
	repository.mutationErrAt = 1

	_, err := service.Create(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Group: "example.io", Version: "v1", Resource: "widgets",
	}, domain.GenericResourceMutation{YAML: genericWidgetManifest("sample"), Namespace: "prod"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create() error = %v, want ErrInvalid", err)
	}
	if repository.lastMutations != 1 || len(repository.mutationDryRun) != 1 || !repository.mutationDryRun[0] {
		t.Fatalf("mutation calls = %#v, want one dry-run", repository.mutationDryRun)
	}
	if len(auditor.events) != 1 || auditor.events[0].Result != "failure" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestServiceExplicitDryRunDoesNotWrite(t *testing.T) {
	service, repository, _, auditor := resourceServiceFixture()
	repository.mutation = map[string]any{
		"apiVersion": "example.io/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "sample", "namespace": "prod"},
	}

	_, err := service.Create(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Group: "example.io", Version: "v1", Resource: "widgets",
	}, domain.GenericResourceMutation{YAML: genericWidgetManifest("sample"), Namespace: "prod", DryRun: true})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.lastMutations != 1 || len(repository.mutationDryRun) != 1 || !repository.mutationDryRun[0] {
		t.Fatalf("mutation calls = %#v, want one dry-run", repository.mutationDryRun)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "dry-run-generic-resource" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestServiceRejectsGenericSecretCreation(t *testing.T) {
	service, repository, authorizer, _ := resourceServiceFixture()
	_, err := service.Create(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Version: "v1", Resource: "secrets",
	}, domain.GenericResourceMutation{YAML: `apiVersion: v1
kind: Secret
metadata:
  name: credentials
  namespace: prod
`, Namespace: "prod"})
	if !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("Create() error = %v, want ErrUnsupported", err)
	}
	if repository.lastMutations != 0 || len(authorizer.accesses) != 0 {
		t.Fatalf("secret creation reached repository/authorizer: %d, %#v", repository.lastMutations, authorizer.accesses)
	}
}

func TestServiceUpdatePreflightsAndFallsBackToApply(t *testing.T) {
	service, repository, authorizer, auditor := resourceServiceFixture()
	authorizer.denied = map[string]bool{"update": true}
	repository.mutation = map[string]any{
		"apiVersion": "example.io/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "sample", "namespace": "prod", "resourceVersion": "4"},
	}

	_, err := service.Update(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Group: "example.io", Version: "v1", Resource: "widgets",
	}, "sample", domain.GenericResourceMutation{YAML: genericWidgetManifest("sample"), Namespace: "prod", ResourceVersion: "3"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if repository.lastMutation != "apply" || repository.lastMutations != 2 || len(repository.mutationDryRun) != 2 || !repository.mutationDryRun[0] || repository.mutationDryRun[1] {
		t.Fatalf("mutation = %q calls=%d dry-runs=%#v", repository.lastMutation, repository.lastMutations, repository.mutationDryRun)
	}
	if len(authorizer.accesses) != 2 || authorizer.accesses[0].Verb != "update" || authorizer.accesses[1].Verb != "patch" {
		t.Fatalf("authorization = %#v", authorizer.accesses)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "update-generic-resource" || auditor.events[0].Result != "success" {
		t.Fatalf("audit events = %#v", auditor.events)
	}
}

func TestServiceCapabilitiesUseCurrentActorAndProtectSecrets(t *testing.T) {
	service, _, authorizer, _ := resourceServiceFixture()
	authorizer.denied = map[string]bool{"delete": true}

	capabilities, err := service.Capabilities(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Group: "example.io", Version: "v1", Resource: "widgets",
	}, "prod")
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if !capabilities.Create || !capabilities.Update || capabilities.Delete {
		t.Fatalf("capabilities = %#v", capabilities)
	}

	secretCapabilities, err := service.Capabilities(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Version: "v1", Resource: "secrets",
	}, "prod")
	if err != nil {
		t.Fatalf("Secret Capabilities() error = %v", err)
	}
	if secretCapabilities.Create || secretCapabilities.Update || secretCapabilities.Delete {
		t.Fatalf("secret capabilities = %#v", secretCapabilities)
	}

	if _, err := service.Capabilities(context.Background(), domain.Actor{Username: "alice"}, domain.ResourceReference{
		Group: "example.io", Version: "v1", Resource: "widgets",
	}, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing namespace error = %v, want ErrInvalid", err)
	}
}

func TestServiceDiscoveryFiltersClusterResourcesWithSubjectAccessReview(t *testing.T) {
	service, repository, authorizer, _ := resourceServiceFixture()
	repository.rules = domain.ResourceRules{
		Namespaced: []domain.ResourceRule{{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
	}
	authorizer.denied = map[string]bool{"list": true}

	items, warnings, err := service.Discovery(context.Background(), domain.Actor{Username: "alice"}, "prod")
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}
	if len(items) != 1 || items[0].Resource != "pods" {
		t.Fatalf("Discovery() items = %#v", items)
	}
	if len(warnings) != 0 {
		t.Fatalf("Discovery() warnings = %#v", warnings)
	}
}
