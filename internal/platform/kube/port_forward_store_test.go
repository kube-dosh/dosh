package kube

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func testPortForwardSession(id, namespace, pod string, requestedPort, targetPort int32, expiresAt time.Time) domain.PortForwardSession {
	return domain.PortForwardSession{
		ID: id, Namespace: namespace, Pod: pod, Port: targetPort, Path: tunnelPathForTest(id, requestedPort),
		CreatedAt: time.Now().UTC(), ExpiresAt: expiresAt,
		Ports: []domain.PortForwardPort{{
			ID: id + "-port", Namespace: namespace, Pod: pod, RequestedPort: requestedPort,
			Port: targetPort, Protocol: "TCP", Path: tunnelPathForTest(id, requestedPort), ExpiresAt: expiresAt,
		}},
	}
}

func tunnelPathForTest(id string, port int32) string {
	return fmt.Sprintf("/api/v1/tunnels/%s/ports/%d/", id, port)
}

func TestPortForwardStoreIsSharedAcrossReplicasAndOwnerScoped(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	cfg := config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"}
	first := NewForClients(clientset, nil, nil, cfg)
	second := NewForClients(clientset, nil, nil, cfg)
	ctx := context.Background()
	expiresAt := time.Now().UTC().Add(time.Minute)
	aliceSession := testPortForwardSession("alice-session", "prod", "api-0", 8080, 8080, expiresAt)
	if err := first.CreatePortForward(ctx, "alice", aliceSession, 1); err != nil {
		t.Fatalf("CreatePortForward(alice) error = %v", err)
	}
	loaded, err := second.GetPortForward(ctx, "alice", aliceSession.ID)
	if err != nil || loaded.Pod != "api-0" || len(loaded.Ports) != 1 || loaded.CreatedAt.IsZero() {
		t.Fatalf("GetPortForward(from second replica) = %#v, %v", loaded, err)
	}
	if _, err := second.GetPortForward(ctx, "bob", aliceSession.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-owner GetPortForward() error = %v, want not found", err)
	}
	if err := second.DeletePortForward(ctx, "bob", aliceSession.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-owner DeletePortForward() error = %v, want not found", err)
	}
	if err := second.CreatePortForward(ctx, "alice", testPortForwardSession("second", "prod", "api-1", 80, 80, expiresAt), 1); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("cross-replica session limit error = %v, want rate limited", err)
	}
	bobSession := testPortForwardSession("bob-session", "prod", "web-0", 80, 8080, expiresAt)
	if err := second.CreatePortForward(ctx, "bob", bobSession, 1); err != nil {
		t.Fatalf("CreatePortForward(bob) error = %v", err)
	}
	if closed, err := first.DeleteAllPortForwards(ctx, "bob"); err != nil || closed != 1 {
		t.Fatalf("DeleteAllPortForwards(bob) = %d, %v", closed, err)
	}
	aliceItems, err := second.ListPortForwards(ctx, "alice")
	if err != nil || len(aliceItems) != 1 || aliceItems[0].ID != aliceSession.ID {
		t.Fatalf("alice sessions after bob close = %#v, %v", aliceItems, err)
	}
}

func TestPortForwardStorePrunesExpiredSessionsAndRejectsEmptyOwners(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	now := time.Now().UTC()
	expired := testPortForwardSession("expired", "prod", "api-0", 80, 80, now.Add(-time.Minute))
	active := testPortForwardSession("active", "prod", "api-1", 80, 80, now.Add(time.Minute))
	payload, err := encodePortForwardCatalog([]domain.PortForwardSession{expired, active})
	if err != nil {
		t.Fatalf("encodePortForwardCatalog() error = %v", err)
	}
	if _, err := clientset.CoreV1().ConfigMaps("dosh").Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: portForwardCatalogName("alice"), Namespace: "dosh"},
		Data:       map[string]string{portForwardCatalogKey: payload},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create catalog: %v", err)
	}
	items, err := client.ListPortForwards(ctx, "alice")
	if err != nil || len(items) != 1 || items[0].ID != "active" {
		t.Fatalf("ListPortForwards() = %#v, %v", items, err)
	}
	if _, err := client.GetPortForward(ctx, "alice", "expired"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPortForward(expired) error = %v, want not found", err)
	}
	if err := client.CreatePortForward(ctx, "", active, 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("CreatePortForward(empty owner) error = %v, want invalid", err)
	}
	if _, err := client.ListPortForwards(ctx, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("ListPortForwards(empty owner) error = %v, want invalid", err)
	}
}

func TestPortForwardStoreRetriesOptimisticConcurrencyConflicts(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second, PreferencesNamespace: "dosh"})
	ctx := context.Background()
	expiresAt := time.Now().UTC().Add(time.Minute)
	if err := client.CreatePortForward(ctx, "alice", testPortForwardSession("first", "prod", "api-0", 80, 80, expiresAt), 2); err != nil {
		t.Fatalf("initial CreatePortForward() error = %v", err)
	}
	conflicts := 1
	clientset.PrependReactor("update", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		if conflicts == 0 {
			return false, nil, nil
		}
		conflicts--
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, portForwardCatalogName("alice"), errors.New("simulated conflict"))
	})
	if err := client.CreatePortForward(ctx, "alice", testPortForwardSession("second", "prod", "api-1", 80, 80, expiresAt), 2); err != nil {
		t.Fatalf("CreatePortForward() after conflict retry error = %v", err)
	}
	items, err := client.ListPortForwards(ctx, "alice")
	if err != nil || len(items) != 2 {
		t.Fatalf("sessions after conflict retry = %#v, %v", items, err)
	}
}
