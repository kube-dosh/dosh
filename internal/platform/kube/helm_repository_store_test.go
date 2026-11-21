package kube

import (
	"context"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestHelmRepositoryStoreSeparatesCredentialsAndTracksAuthType(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	saved, err := client.SaveHelmRepository(context.Background(), domain.HelmRepository{
		ID: "repo-1", Name: "Private", URL: "https://charts.example.com", Enabled: true,
		AuthType: domain.HelmRepositoryAuthBasic,
		Credentials: domain.HelmRepositoryCredentials{
			Username: "robot", Password: "secret", CAPEM: "ca-data",
		},
		Default: true,
	})
	if err != nil {
		t.Fatalf("SaveHelmRepository() error = %v", err)
	}
	if saved.AuthType != domain.HelmRepositoryAuthBasic || !saved.HasCredentials || !saved.HasCustomCA || saved.Credentials.Password != "secret" {
		t.Fatalf("SaveHelmRepository() = %#v", saved)
	}

	items, err := client.ListHelmRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListHelmRepositories() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListHelmRepositories() count = %d", len(items))
	}
	item := items[0]
	if item.AuthType != domain.HelmRepositoryAuthBasic || !item.HasCredentials || !item.HasCustomCA {
		t.Fatalf("ListHelmRepositories() flags = %#v", item)
	}
	if item.Credentials.Username != "robot" || item.Credentials.Password != "secret" || item.Credentials.CAPEM != "ca-data" {
		t.Fatalf("ListHelmRepositories() credentials = %#v", item.Credentials)
	}
}

func TestHelmRepositoryStoreEnforcesSingleDefaultAndRevision(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	first, err := client.SaveHelmRepository(context.Background(), domain.HelmRepository{ID: "repo-1", Name: "First", URL: "https://one.example.com", Enabled: true, Default: true, AuthType: domain.HelmRepositoryAuthAnonymous})
	if err != nil {
		t.Fatalf("SaveHelmRepository(first) error = %v", err)
	}
	_, err = client.SaveHelmRepository(context.Background(), domain.HelmRepository{ID: "repo-2", Name: "Second", URL: "https://two.example.com", Enabled: true, Default: true, AuthType: domain.HelmRepositoryAuthAnonymous})
	if err != nil {
		t.Fatalf("SaveHelmRepository(second) error = %v", err)
	}
	items, err := client.ListHelmRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListHelmRepositories() error = %v", err)
	}
	for _, item := range items {
		if item.ID == first.ID && item.Default {
			t.Fatalf("first repository remained default: %#v", items)
		}
	}
}

func TestAttachHelmRepositoryRevision(t *testing.T) {
	t.Parallel()
	items := attachHelmRepositoryRevision([]domain.HelmRepository{{ID: "repo-1"}, {ID: "repo-2"}}, "42")
	for _, item := range items {
		if item.Revision != "42" {
			t.Fatalf("attachHelmRepositoryRevision() = %#v", items)
		}
	}
}

func TestHelmRepositoryStoreReportsAnonymousForCustomCAOnly(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	saved, err := client.SaveHelmRepository(context.Background(), domain.HelmRepository{
		ID: "repo-1", Name: "Internal TLS", URL: "https://charts.internal.example.com", Enabled: true,
		AuthType:    domain.HelmRepositoryAuthAnonymous,
		Credentials: domain.HelmRepositoryCredentials{CAPEM: "ca-data"},
	})
	if err != nil {
		t.Fatalf("SaveHelmRepository() error = %v", err)
	}
	if saved.HasCredentials || !saved.HasCustomCA || saved.AuthType != domain.HelmRepositoryAuthAnonymous {
		t.Fatalf("SaveHelmRepository() flags = %#v", saved)
	}
}
