package kube

import (
	"context"
	"testing"
	"time"

	"dosh/internal/platform/config"
	"dosh/internal/platform/egress"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestEgressSettingsStoreCreatesAndLoadsSecret(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{
		RequestTimeout:       time.Second,
		PreferencesNamespace: "dosh",
	})
	ctx := context.Background()
	if _, found, err := client.LoadEgressSettings(ctx); err != nil || found {
		t.Fatalf("LoadEgressSettings() found=%v err=%v, want absent", found, err)
	}
	saved, err := client.SaveEgressSettings(ctx, egress.StoredSettings{
		Mode: "explicit", HTTPSProxy: "https://proxy.example.test:8443", Username: "dosh", Password: "secret",
	}, "")
	if err != nil {
		t.Fatalf("SaveEgressSettings() error = %v", err)
	}
	if saved.Mode != "explicit" || saved.HTTPSProxy != "https://proxy.example.test:8443" || saved.UpdatedAt.IsZero() {
		t.Fatalf("SaveEgressSettings() = %#v", saved)
	}
	secret, err := clientset.CoreV1().Secrets("dosh").Get(ctx, egressSettingsSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read Secret: %v", err)
	}
	if len(secret.Data[egressSettingsDataKey]) == 0 || secret.Labels["dosh.io/component"] != "egress-settings" {
		t.Fatalf("stored Secret = %#v", secret)
	}
	loaded, found, err := client.LoadEgressSettings(ctx)
	if err != nil || !found {
		t.Fatalf("LoadEgressSettings() found=%v err=%v", found, err)
	}
	if loaded.Mode != saved.Mode || loaded.HTTPSProxy != saved.HTTPSProxy || loaded.Username != saved.Username || loaded.Password != saved.Password {
		t.Fatalf("LoadEgressSettings() = %#v", loaded)
	}
}
