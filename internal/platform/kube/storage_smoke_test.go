package kube

import (
	"context"
	"testing"

	"dosh/internal/domain"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStorageSmokeTestUsesPodAndCleansTemporaryResources(t *testing.T) {
	client, clientset := newFakeKubeClient()
	result, err := client.RunStorageSmokeTest(context.Background(), domain.StorageSmokeTestSpec{
		Namespace: "prod", StorageClass: "local-path", Image: "registry.k8s.io/pause:3.10", TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("RunStorageSmokeTest() error = %v", err)
	}
	if result.Bound || result.PodRunning || !result.CleanedUp {
		t.Fatalf("result = %#v", result)
	}
	if result.DurationMS <= 0 {
		t.Fatalf("durationMs = %d, want > 0", result.DurationMS)
	}
	if _, err := clientset.CoreV1().Pods("prod").Get(context.Background(), result.PodName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("temporary Pod still exists: %v", err)
	}
	if _, err := clientset.CoreV1().PersistentVolumeClaims("prod").Get(context.Background(), result.PVCName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("temporary PVC still exists: %v", err)
	}
}
