//go:build capacity

package performance_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	stdruntime "runtime"
	"sort"
	"testing"
	"time"

	"dosh/internal/domain"
	overviewapp "dosh/internal/modules/overview/app"
	overviewhttp "dosh/internal/modules/overview/http"
	workloadsapp "dosh/internal/modules/workloads/app"
	workloadshttp "dosh/internal/modules/workloads/http"
	"dosh/internal/platform/config"
	"dosh/internal/platform/kube"

	"github.com/go-chi/chi/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	capacityPods      = 1000
	capacityWorkloads = 200
	latencySamples    = 50
)

type allowWrites struct{}

func (allowWrites) Require(context.Context, domain.Actor, domain.Access) error { return nil }

type discardAudit struct{}

func (discardAudit) Record(context.Context, domain.Actor, domain.AuditEvent) {}

func capacityObjects() []k8sruntime.Object {
	now := time.Now().Add(-time.Hour)
	objects := make([]k8sruntime.Object, 0, capacityPods+capacityWorkloads+16)
	for i := 0; i < 8; i++ {
		objects = append(objects, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("worker-%04d", i), CreationTimestamp: metav1.NewTime(now)},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourcePods:   resource.MustParse("110"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1900m"),
					corev1.ResourceMemory: resource.MustParse("3800Mi"),
					corev1.ResourcePods:   resource.MustParse("110"),
				},
			},
		})
	}
	for i := 0; i < 9; i++ {
		objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("namespace-%02d", i), CreationTimestamp: metav1.NewTime(now)}})
	}
	for i := 0; i < capacityWorkloads; i++ {
		replicas := int32(5)
		objects = append(objects, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("workload-%04d", i), Namespace: fmt.Sprintf("namespace-%02d", i%9),
				CreationTimestamp: metav1.NewTime(now), Labels: map[string]string{"app": fmt.Sprintf("workload-%04d", i)},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": fmt.Sprintf("workload-%04d", i)}},
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.test/app:1"}}}},
			},
			Status: appsv1.DeploymentStatus{Replicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas},
		})
	}
	for i := 0; i < capacityPods; i++ {
		objects = append(objects, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("workload-%04d-pod-%04d", i%capacityWorkloads, i),
				Namespace: fmt.Sprintf("namespace-%02d", i%9), CreationTimestamp: metav1.NewTime(now),
				Labels:          map[string]string{"app": fmt.Sprintf("workload-%04d", i%capacityWorkloads)},
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: fmt.Sprintf("workload-%04d", i%capacityWorkloads)}},
			},
			Spec: corev1.PodSpec{
				NodeName: fmt.Sprintf("worker-%04d", i%8),
				Containers: []corev1.Container{{
					Name: "app", Image: "registry.test/app:1",
					Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi"),
					}},
				}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning, PodIP: fmt.Sprintf("10.244.%d.%d", i/250, i%250+1),
				ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			},
		})
	}
	for i := 0; i < 100; i++ {
		objects = append(objects, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("event-%04d", i), Namespace: fmt.Sprintf("namespace-%02d", i%9)},
			Type:       corev1.EventTypeNormal, Reason: "Scheduled", Message: "assigned to node",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: fmt.Sprintf("workload-0000-pod-%04d", i)},
			FirstTimestamp: metav1.NewTime(now), LastTimestamp: metav1.NewTime(now.Add(time.Duration(i) * time.Second)),
		})
	}
	return objects
}

func capacityServer(t *testing.T) http.Handler {
	t.Helper()
	clientset := k8sfake.NewSimpleClientset(capacityObjects()...)
	discovery := &discoveryfake.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.FakedServerVersion = &version.Info{GitVersion: "v1.36.5"}
	client := kube.NewForClients(
		clientset,
		dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()),
		discovery,
		config.Config{ClusterName: "capacity", RequestTimeout: 5 * time.Second},
	)
	workloadService := workloadsapp.New(client, allowWrites{}, discardAudit{})
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		overviewhttp.New(overviewapp.New(client, "capacity")).Mount(r)
		workloadshttp.New(workloadService).Mount(r)
	})
	return router
}

func requestLatencies(handler http.Handler, path string) []time.Duration {
	latencies := make([]time.Duration, 0, latencySamples)
	for i := 0; i < latencySamples; i++ {
		started := time.Now()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		latencies = append(latencies, time.Since(started))
		if response.Code != http.StatusOK {
			panic(fmt.Sprintf("%s returned %d", path, response.Code))
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return latencies
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	index := int(float64(len(values))*fraction+0.999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func TestCapacityLatencySLO(t *testing.T) {
	handler := capacityServer(t)
	for _, path := range []string{"/api/v1/pods", "/api/v1/workloads", "/api/v1/overview"} {
		_ = requestLatencies(handler, path)[0]
	}
	tests := []struct {
		path    string
		maximum time.Duration
	}{
		{path: "/api/v1/pods", maximum: time.Second},
		{path: "/api/v1/workloads", maximum: time.Second},
		{path: "/api/v1/overview", maximum: 1500 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			latencies := requestLatencies(handler, test.path)
			p95 := percentile(latencies, 0.95)
			t.Logf("dataset pods=%d workloads=%d p50=%s p95=%s", capacityPods, capacityWorkloads, percentile(latencies, 0.50), p95)
			if p95 > test.maximum {
				t.Fatalf("p95 %s exceeds %s", p95, test.maximum)
			}
		})
	}

	stdruntime.GC()
	var stats stdruntime.MemStats
	stdruntime.ReadMemStats(&stats)
	t.Logf("heap after capacity requests: heapAlloc=%d MiB heapInuse=%d MiB", stats.HeapAlloc/(1024*1024), stats.HeapInuse/(1024*1024))
	if stats.HeapAlloc > 256*1024*1024 {
		t.Fatalf("heapAlloc %d MiB exceeds 256 MiB", stats.HeapAlloc/(1024*1024))
	}
}
