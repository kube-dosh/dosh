package kube

import (
	"context"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestSaveContainerRegistryPersistsCustomCAForAnonymousRegistry(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	ca := "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----"
	saved, err := client.SaveContainerRegistry(context.Background(), domain.ContainerRegistry{
		ID:       "registry-1",
		Name:     "default",
		Endpoint: "https://registry.example.com",
		AuthType: domain.RegistryAuthAnonymous,
		Credentials: domain.ContainerRegistryCredentials{
			CAPEM: ca,
		},
	})
	if err != nil {
		t.Fatalf("SaveContainerRegistry() error = %v", err)
	}
	if !saved.HasCustomCA || saved.HasCredentials {
		t.Fatalf("saved flags = hasCustomCA:%v hasCredentials:%v", saved.HasCustomCA, saved.HasCredentials)
	}

	items, err := client.ListContainerRegistries(context.Background())
	if err != nil {
		t.Fatalf("ListContainerRegistries() error = %v", err)
	}
	if len(items) != 1 || !items[0].HasCustomCA || items[0].Credentials.CAPEM != ca || items[0].HasCredentials {
		t.Fatalf("stored registry = %#v", items)
	}
}
