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

type memoryRepository struct {
	items []domain.ContainerRegistry
}

func (r *memoryRepository) ListContainerRegistries(context.Context) ([]domain.ContainerRegistry, error) {
	return append([]domain.ContainerRegistry(nil), r.items...), nil
}

func (r *memoryRepository) GetContainerRegistry(_ context.Context, id string) (domain.ContainerRegistry, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.ContainerRegistry{}, domain.ErrNotFound
}

func (r *memoryRepository) SaveContainerRegistry(_ context.Context, registry domain.ContainerRegistry) (domain.ContainerRegistry, error) {
	registry.Revision = "revision-1"
	registry.UpdatedAt = time.Now().UTC()
	for index := range r.items {
		if r.items[index].ID == registry.ID {
			r.items[index] = registry
			return registry, nil
		}
	}
	r.items = append(r.items, registry)
	return registry, nil
}

func (r *memoryRepository) DeleteContainerRegistry(_ context.Context, id, _ string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type fakeRemote struct {
	repositories []string
	tags         []string
	testErr      error
}

func (r fakeRemote) Test(context.Context, domain.ContainerRegistry) (domain.ContainerRegistryCapabilities, error) {
	return domain.ContainerRegistryCapabilities{Catalog: r.testErr == nil, Tags: r.testErr == nil}, r.testErr
}

func (r fakeRemote) Repositories(context.Context, domain.ContainerRegistry) ([]string, error) {
	return append([]string(nil), r.repositories...), nil
}

func (r fakeRemote) Tags(context.Context, domain.ContainerRegistry, string) ([]string, error) {
	return append([]string(nil), r.tags...), nil
}

func testActor() domain.Actor { return domain.Actor{Username: "admin", Admin: true} }

func TestServiceCreateResolveAndSearch(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{}
	service := New(repository, fakeRemote{
		repositories: []string{"platform/api", "other/web", "platform/worker"},
		tags:         []string{"2.0.0", "1.0.0"},
	}, authz.NewLocal(false), nil)

	created, err := service.Create(context.Background(), testActor(), domain.ContainerRegistryUpsert{
		Name: "Primary", Endpoint: "https://registry.example.com", RepositoryPrefix: "platform", Default: true,
		AuthType:    domain.RegistryAuthBasic,
		Credentials: &domain.ContainerRegistryCredentials{Username: "robot", Password: "secret"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" || created.HasCredentials != true || created.Credentials.Username != "" {
		t.Fatalf("Create() returned credentials or missing id: %#v", created)
	}
	resolved, err := service.Resolve(context.Background(), testActor(), "registry.example.com/platform/api:1.0.0")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !resolved.Matched || resolved.RegistryID != created.ID || resolved.Repository != "platform/api" || resolved.Tag != "1.0.0" {
		t.Fatalf("Resolve() = %#v", resolved)
	}
	external, err := service.Resolve(context.Background(), testActor(), "nginx:alpine")
	if err != nil {
		t.Fatalf("Resolve() external error = %v", err)
	}
	if external.Matched || external.ExplicitRegistry {
		t.Fatalf("Resolve() external = %#v", external)
	}
	repositories, err := service.Repositories(context.Background(), testActor(), created.ID, "api", domain.PageRequest{Number: 1, Size: 20})
	if err != nil {
		t.Fatalf("Repositories() error = %v", err)
	}
	if len(repositories.Items) != 1 || repositories.Items[0].Name != "platform/api" || repositories.Items[0].Image != "registry.example.com/platform/api" {
		t.Fatalf("Repositories() = %#v", repositories)
	}
	tags, err := service.Tags(context.Background(), testActor(), created.ID, "platform/api", "", domain.PageRequest{Number: 1, Size: 20})
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}
	if len(tags.Items) != 2 || tags.Items[0].Name != "2.0.0" || tags.Items[1].Name != "1.0.0" {
		t.Fatalf("Tags() = %#v", tags)
	}
	secret, err := service.ResolvePullSecret(context.Background(), testActor(), created.ID, "registry.example.com/platform/api:2.0.0")
	if err != nil || secret != "" {
		t.Fatalf("ResolvePullSecret() = (%q, %v)", secret, err)
	}
}

func TestServiceResolvePullSecretRejectsWrongRegistry(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{items: []domain.ContainerRegistry{{
		ID: "reg-1", Name: "Primary", Endpoint: "https://registry.example.com", RepositoryPrefix: "platform",
		AuthType: domain.RegistryAuthAnonymous, PullSecretName: "registry-pull", Default: true,
	}}}
	service := New(repository, fakeRemote{}, authz.NewLocal(false), nil)
	secret, err := service.ResolvePullSecret(context.Background(), testActor(), "reg-1", "registry.example.com/platform/api:1")
	if err != nil || secret != "registry-pull" {
		t.Fatalf("ResolvePullSecret() = (%q, %v)", secret, err)
	}
	if _, err := service.ResolvePullSecret(context.Background(), testActor(), "reg-1", "registry.example.com/outside/api:1"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("ResolvePullSecret() error = %v", err)
	}
}

func TestAnonymousRegistryRetainsCustomCAAndDropsAuthSecrets(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{}
	service := New(repository, fakeRemote{}, authz.NewLocal(false), nil)

	created, err := service.Create(context.Background(), testActor(), domain.ContainerRegistryUpsert{
		Name:     "Anonymous TLS",
		Endpoint: "https://registry.example.com",
		AuthType: domain.RegistryAuthAnonymous,
		Credentials: &domain.ContainerRegistryCredentials{
			Username: "ignored", Password: "ignored", Token: "ignored", CAPEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.HasCustomCA || created.HasCredentials {
		t.Fatalf("Create() credential flags = hasCustomCA:%v hasCredentials:%v", created.HasCustomCA, created.HasCredentials)
	}
	stored := repository.items[0].Credentials
	if stored.CAPEM == "" || stored.Username != "" || stored.Password != "" || stored.Token != "" {
		t.Fatalf("stored credentials = %#v", stored)
	}
}

func TestServiceUsesMostSpecificRegistryAndRecordsFailedTest(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{items: []domain.ContainerRegistry{
		{ID: "broad", Name: "Broad", Endpoint: "https://registry.example.com", AuthType: domain.RegistryAuthAnonymous},
		{ID: "specific", Name: "Specific", Endpoint: "https://registry.example.com", RepositoryPrefix: "platform", AuthType: domain.RegistryAuthAnonymous},
	}}
	service := New(repository, fakeRemote{testErr: errors.New("registry authentication failed")}, authz.NewLocal(false), nil)
	resolved, err := service.Resolve(context.Background(), testActor(), "registry.example.com/platform/api:1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.RegistryID != "specific" {
		t.Fatalf("Resolve() registry = %q", resolved.RegistryID)
	}
	tested, err := service.Test(context.Background(), testActor(), "specific")
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if tested.LastTest == nil || tested.LastTest.OK || !strings.Contains(tested.LastTest.Message, "authentication") {
		t.Fatalf("Test() result = %#v", tested.LastTest)
	}
}

func TestServiceRequiresAdministratorInLocalMode(t *testing.T) {
	t.Parallel()
	service := New(&memoryRepository{}, fakeRemote{}, authz.NewLocal(false), nil)
	if _, err := service.List(context.Background(), domain.Actor{Username: "viewer"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("List() error = %v", err)
	}
}
