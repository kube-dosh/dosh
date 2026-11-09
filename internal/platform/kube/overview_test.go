package kube

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeOverviewAggregatesNodesAndPodRequests(t *testing.T) {
	pod := testPod()
	pod.Spec.NodeName = "worker-1"
	client, _ := newFakeKubeClient(testNode(), pod)

	result, err := client.NodeOverview(context.Background())
	if err != nil {
		t.Fatalf("NodeOverview() error = %v", err)
	}
	if result.Total != 1 || result.Ready != 1 {
		t.Fatalf("node counts = %#v", result)
	}
	if result.Capacity.PodCapacity != 110 || result.Capacity.CPURequestsMilli != 100 {
		t.Fatalf("node capacity = %#v", result.Capacity)
	}
}

func TestPodOverviewCountsStatusesWithoutMaterializingPods(t *testing.T) {
	running := testPod()
	pending := testPod()
	pending.Name = "pending"
	pending.Status.Phase = corev1.PodPending
	succeeded := testPod()
	succeeded.Name = "succeeded"
	succeeded.Status.Phase = corev1.PodSucceeded
	terminating := testPod()
	terminating.Name = "terminating"
	now := metav1.NewTime(time.Now())
	terminating.DeletionTimestamp = &now
	client, _ := newFakeKubeClient(running, pending, succeeded, terminating)

	result, err := client.PodOverview(context.Background())
	if err != nil {
		t.Fatalf("PodOverview() error = %v", err)
	}
	if result.Total != 4 || result.Running != 1 || result.Unhealthy != 1 {
		t.Fatalf("pod overview = %#v", result)
	}
}

func TestOverviewCountsUseBoundedCollectionMetadata(t *testing.T) {
	client, _ := newFakeKubeClient(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "prod"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}},
	)

	workloads, err := client.CountWorkloads(context.Background())
	if err != nil || workloads != 2 {
		t.Fatalf("CountWorkloads() = (%d, %v), want (2, nil)", workloads, err)
	}
	namespaces, err := client.CountNamespaces(context.Background())
	if err != nil || namespaces != 2 {
		t.Fatalf("CountNamespaces() = (%d, %v), want (2, nil)", namespaces, err)
	}
}
