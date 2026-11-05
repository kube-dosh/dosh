package kube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestKubernetesLoginAttemptsSharesStateAcrossReplicas(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	first := NewKubernetesLoginAttempts(clientset, "dosh")
	second := NewKubernetesLoginAttempts(clientset, "dosh")
	ctx := context.Background()

	for attempt := range loginAttemptMaxAttempts {
		limiter := first
		if attempt%2 == 1 {
			limiter = second
		}
		allowed, err := limiter.Begin(ctx, "192.0.2.10")
		if err != nil || !allowed {
			t.Fatalf("attempt %d Begin() = (%t, %v)", attempt+1, allowed, err)
		}
		if err := limiter.Fail(ctx, "192.0.2.10"); err != nil {
			t.Fatalf("attempt %d Fail() error = %v", attempt+1, err)
		}
	}
	if allowed, err := second.Begin(ctx, "192.0.2.10"); err != nil || allowed {
		t.Fatalf("cross-replica blocked Begin() = (%t, %v), want false", allowed, err)
	}
	if allowed, err := first.Begin(ctx, "192.0.2.11"); err != nil || !allowed {
		t.Fatalf("unrelated IP Begin() = (%t, %v)", allowed, err)
	}
	if err := first.Reset(ctx, "192.0.2.10"); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if allowed, err := second.Begin(ctx, "192.0.2.10"); err != nil || !allowed {
		t.Fatalf("Begin() after reset = (%t, %v)", allowed, err)
	}

	leases, err := clientset.CoordinationV1().Leases("dosh").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list leases: %v", err)
	}
	if len(leases.Items) == 0 {
		t.Fatal("no shared Lease was created")
	}
	for _, lease := range leases.Items {
		if strings.Contains(lease.Name, "192.0.2.10") || strings.Contains(lease.Annotations[loginAttemptAnnotation], "192.0.2.10") {
			t.Fatalf("login limiter stored a raw client IP: %#v", lease)
		}
	}
}

func TestKubernetesLoginAttemptsExpiresBlocksAndResets(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	limiter := NewKubernetesLoginAttempts(clientset, "dosh")
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	ctx := context.Background()
	for range loginAttemptMaxAttempts {
		if allowed, err := limiter.Begin(ctx, "192.0.2.20"); err != nil || !allowed {
			t.Fatalf("Begin() = (%t, %v)", allowed, err)
		}
		if err := limiter.Fail(ctx, "192.0.2.20"); err != nil {
			t.Fatalf("Fail() error = %v", err)
		}
	}
	if allowed, err := limiter.Begin(ctx, "192.0.2.20"); err != nil || allowed {
		t.Fatalf("blocked Begin() = (%t, %v)", allowed, err)
	}
	now = now.Add(loginAttemptBlockDuration + time.Second)
	if allowed, err := limiter.Begin(ctx, "192.0.2.20"); err != nil || !allowed {
		t.Fatalf("expired block Begin() = (%t, %v)", allowed, err)
	}
}

func TestKubernetesLoginAttemptsRetriesOptimisticConflict(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	limiter := NewKubernetesLoginAttempts(clientset, "dosh")
	ctx := context.Background()
	if allowed, err := limiter.Begin(ctx, "192.0.2.30"); err != nil || !allowed {
		t.Fatalf("initial Begin() = (%t, %v)", allowed, err)
	}
	var updates int
	clientset.PrependReactor("update", "leases", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updates++
		if updates == 1 {
			name := action.(k8stesting.UpdateAction).GetObject().(*coordinationv1.Lease).Name
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"}, name, errors.New("concurrent update"))
		}
		return false, nil, nil
	})

	if allowed, err := limiter.Begin(ctx, "192.0.2.30"); err != nil || !allowed {
		t.Fatalf("Begin() after conflict = (%t, %v)", allowed, err)
	}
	if updates < 2 {
		t.Fatalf("Lease updates = %d, want at least 2", updates)
	}
}

func TestKubernetesLoginAttemptsFailsClosedOnCorruptState(t *testing.T) {
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name: loginAttemptLeaseName("192.0.2.40"), Namespace: "dosh",
			Annotations: map[string]string{loginAttemptAnnotation: "{"},
		},
	}
	clientset := k8sfake.NewSimpleClientset(lease)
	limiter := NewKubernetesLoginAttempts(clientset, "dosh")
	if _, err := limiter.Begin(context.Background(), "192.0.2.40"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Begin() error = %v, want ErrUnavailable", err)
	}
}
