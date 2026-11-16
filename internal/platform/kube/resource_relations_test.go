package kube

import (
	"context"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestGetResourceRelationsBuildsPodTopology(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"name": "api-0", "namespace": "prod", "uid": "pod-uid",
			"labels": map[string]any{"app": "api"},
			"ownerReferences": []any{map[string]any{
				"apiVersion": "apps/v1", "kind": "ReplicaSet", "name": "api-7d9f", "uid": "rs-uid",
			}},
		},
		"spec": map[string]any{
			"nodeName": "worker-1",
			"volumes": []any{map[string]any{
				"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "api-data"},
			}},
		},
	}}
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PodList"}, pod,
	)
	clientset := k8sfake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}, Ports: []corev1.ServicePort{{Port: 80}}},
	})
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "services", Kind: "Service", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "nodes", Kind: "Node", Verbs: []string{"get", "list"}},
		}},
		{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "replicasets", Kind: "ReplicaSet", Namespaced: true, Verbs: []string{"get", "list"}}}},
	}
	client := NewForClients(clientset, dynamicClient, discovery, testConfig())

	result, err := client.GetResourceRelations(context.Background(), domain.ResourceDescriptor{
		Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true,
	}, "prod", "api-0")
	if err != nil {
		t.Fatalf("GetResourceRelations() error = %v", err)
	}
	found := make(map[string]bool)
	for _, item := range result.Items {
		found[item.Kind+"/"+item.Name] = true
	}
	for _, want := range []string{"ReplicaSet/api-7d9f", "Node/worker-1", "PersistentVolumeClaim/api-data", "Service/api"} {
		if !found[want] {
			t.Fatalf("relations missing %s: %#v", want, result.Items)
		}
	}
	if result.Items[0].Kind != "ReplicaSet" || result.Items[0].Direction != "upstream" {
		t.Fatalf("owner relation = %#v", result.Items[0])
	}
}
