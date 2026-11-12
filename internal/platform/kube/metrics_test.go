package kube

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	restfake "k8s.io/client-go/rest/fake"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestMetricsUsageIsAttachedToNodesAndPods(t *testing.T) {
	pod := testPod()
	pod.Spec.NodeName = "worker-1"
	clientset := fake.NewSimpleClientset(testNode(), pod)
	nodeMetric := &metricsv1beta1.NodeMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Usage:      coreResourceList("750m", "2Gi"),
	}
	podMetric := &metricsv1beta1.PodMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "prod"},
		Containers: []metricsv1beta1.ContainerMetrics{{
			Name: "api", Usage: coreResourceList("125m", "256Mi"),
		}},
	}
	metricsClient := metricsfake.NewSimpleClientset()
	metricsClient.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.NodeMetricsList{Items: []metricsv1beta1.NodeMetrics{*nodeMetric}}, nil
	})
	metricsClient.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.PodMetricsList{Items: []metricsv1beta1.PodMetrics{*podMetric}}, nil
	})
	metricsClient.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, podMetric, nil
	})
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{
		GroupVersion: "metrics.k8s.io/v1beta1",
		APIResources: []metav1.APIResource{{Name: "nodes"}, {Name: "pods"}},
	}}
	client := NewForClientsWithMetrics(clientset, nil, discovery, metricsClient, testConfig())
	if !client.MetricsAvailable(context.Background()) {
		t.Fatal("metrics API was not detected")
	}

	nodes, err := client.ListNodes(context.Background(), domain.ListQuery{})
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].CPUUsageMilli != 750 || nodes[0].MemoryUsageBytes != 2*1024*1024*1024 || !nodes[0].HasMetrics {
		t.Fatalf("node metrics = %#v", nodes)
	}
	pods, err := client.ListPods(context.Background(), domain.ListQuery{Namespace: "prod"})
	if err != nil {
		t.Fatalf("ListPods() error = %v", err)
	}
	if len(pods) != 1 || pods[0].CPUUsageMilli != 125 || pods[0].MemoryUsageBytes != 256*1024*1024 || !pods[0].HasMetrics {
		t.Fatalf("pod metrics = %#v", pods)
	}
	podPage, err := client.ListPodsPage(context.Background(), domain.ListQuery{Namespace: "prod"}, domain.PageRequest{Number: 1, Size: 20})
	if err != nil {
		t.Fatalf("ListPodsPage() error = %v", err)
	}
	if len(podPage.Items) != 1 || podPage.Items[0].CPUUsageMilli != 125 || podPage.Items[0].MemoryUsageBytes != 256*1024*1024 || !podPage.Items[0].HasMetrics {
		t.Fatalf("pod page metrics = %#v", podPage.Items)
	}
	detail, err := client.GetPod(context.Background(), "prod", "api-0")
	if err != nil {
		t.Fatalf("GetPod() error = %v", err)
	}
	if detail.CPUUsageMilli != 125 || detail.MemoryUsageBytes != 256*1024*1024 || !detail.HasMetrics {
		t.Fatalf("pod detail metrics = %#v", detail.PodSummary)
	}
}

func coreResourceList(cpu, memory string) map[corev1.ResourceName]resource.Quantity {
	return map[corev1.ResourceName]resource.Quantity{
		corev1.ResourceCPU:    resourceQuantity(cpu),
		corev1.ResourceMemory: resourceQuantity(memory),
	}
}

func TestMetricsAvailableDoesNotCacheContextFailures(t *testing.T) {
	requests := 0
	restClient := &restfake.RESTClient{Client: restfake.CreateHTTPClient(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"groupVersion":"metrics.k8s.io/v1beta1","resources":[]}`)),
			Request:    request,
		}, nil
	})}
	client := NewForClientsWithMetrics(
		fake.NewSimpleClientset(),
		nil,
		discovery.NewDiscoveryClient(restClient),
		metricsfake.NewSimpleClientset(),
		testConfig(),
	)

	if client.MetricsAvailable(context.Background()) {
		t.Fatal("MetricsAvailable() = true after a context deadline")
	}
	if !client.MetricsAvailable(context.Background()) {
		t.Fatal("context failure was cached and prevented the next probe")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}
