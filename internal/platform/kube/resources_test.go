package kube

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
)

func testNode() *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1", Labels: map[string]string{"node-role.kubernetes.io/worker": ""}},
		Spec:       corev1.NodeSpec{},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.11"}},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resourceQuantity("4"),
				corev1.ResourceMemory: resourceQuantity("8Gi"),
				corev1.ResourcePods:   resourceQuantity("110"),
			},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.36.5", OSImage: "Linux", KernelVersion: "6.8", ContainerRuntimeVersion: "containerd://2"},
		},
	}
}

func TestListPodsSearchAndGetPodConversion(t *testing.T) {
	pod := testPod()
	other := testPod()
	other.Name = "other-0"
	other.Namespace = "dev"
	client, _ := newFakeKubeClient(pod, other)

	items, err := client.ListPods(context.Background(), domain.ListQuery{Namespace: "prod", Search: "api"})
	if err != nil {
		t.Fatalf("ListPods() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "api-0" || items[0].Ready != "1/1" || items[0].Restarts != 3 {
		t.Fatalf("ListPods() = %#v", items)
	}
	detail, err := client.GetPod(context.Background(), "prod", "api-0")
	if err != nil {
		t.Fatalf("GetPod() error = %v", err)
	}
	if detail.QOSClass != string(corev1.PodQOSBurstable) || detail.ServiceAccount != "" {
		t.Fatalf("pod detail = %#v", detail)
	}
	if len(detail.Containers) != 1 || detail.Containers[0].CPURequestMilli != 100 || detail.Containers[0].MemoryLimitBytes != 64*1024*1024 {
		t.Fatalf("containers = %#v", detail.Containers)
	}
	if detail.Containers[0].State != "Running" || !detail.Containers[0].Ready {
		t.Fatalf("container state = %#v", detail.Containers[0])
	}
	if detail.Conditions == nil || detail.Containers == nil {
		t.Fatalf("pod detail collections must be non-nil: %#v", detail)
	}
}

func TestGetNamespaceIncludesQuotasAndLimitRanges(t *testing.T) {
	client, _ := newFakeKubeClient(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod", Labels: map[string]string{"team": "platform"}, Annotations: map[string]string{"owner": "ops"}}},
		&corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "compute", Namespace: "prod"},
			Status: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{corev1.ResourceRequestsCPU: resourceQuantity("2"), corev1.ResourceRequestsMemory: resourceQuantity("2Gi")},
				Used: corev1.ResourceList{corev1.ResourceRequestsCPU: resourceQuantity("500m"), corev1.ResourceRequestsMemory: resourceQuantity("512Mi")},
			},
		},
		&corev1.LimitRange{
			ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "prod"},
			Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
				Type:           corev1.LimitTypeContainer,
				Default:        corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("500m")},
				DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("100m")},
				Max:            corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("1")},
			}}},
		},
	)
	detail, err := client.GetNamespace(context.Background(), "prod")
	if err != nil {
		t.Fatalf("GetNamespace() error = %v", err)
	}
	if detail.Name != "prod" || detail.Annotations["owner"] != "ops" || len(detail.ResourceQuotas) != 1 || len(detail.LimitRanges) != 1 {
		t.Fatalf("namespace detail = %#v", detail)
	}
	if detail.ResourceQuotas[0].Hard["requests.cpu"] != "2" || detail.ResourceQuotas[0].Used["requests.cpu"] != "500m" {
		t.Fatalf("resource quota = %#v", detail.ResourceQuotas[0])
	}
	if detail.LimitRanges[0].Limits[0].Type != "Container" || detail.LimitRanges[0].Limits[0].DefaultRequest["cpu"] != "100m" {
		t.Fatalf("limit range = %#v", detail.LimitRanges[0])
	}
}

func TestListNodesIncludesRequestsPodsAndRoles(t *testing.T) {
	pod := testPod()
	pod.Spec.NodeName = "worker-1"
	client, _ := newFakeKubeClient(testNode(), pod)
	items, err := client.ListNodes(context.Background(), domain.ListQuery{})
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("nodes = %#v", items)
	}
	node := items[0]
	if node.Status != "Ready" || node.InternalIP != "10.0.0.11" || node.PodCount != 1 || node.CPURequestsMilli != 100 {
		t.Fatalf("node summary = %#v", node)
	}
	if node.CPUCapacityMilli != 4000 || node.MemoryCapacityBytes != 8*1024*1024*1024 || node.PodCapacity != 110 {
		t.Fatalf("node capacity = %#v", node)
	}
	if len(node.Roles) != 1 || node.Roles[0] != "worker" {
		t.Fatalf("roles = %#v", node.Roles)
	}
	detail, err := client.GetNode(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if len(detail.Conditions) != 1 {
		t.Fatalf("node detail = %#v", detail)
	}
	if detail.Pods == nil || len(detail.Pods) != 1 || detail.Pods[0].Name != "api-0" {
		t.Fatalf("node detail pods = %#v", detail.Pods)
	}
	if detail.Taints == nil {
		t.Fatalf("node detail taints must be non-nil: %#v", detail)
	}
	pods, err := client.ListPodsForNode(context.Background(), "worker-1")
	if err != nil || len(pods) != 1 {
		t.Fatalf("node pods = %#v, err = %v", pods, err)
	}
}

func TestSetNodeSchedulingUpdatesOnlyWhenNeeded(t *testing.T) {
	pod := testPod()
	pod.Spec.NodeName = "worker-1"
	client, clientset := newFakeKubeClient(testNode(), pod)

	summary, err := client.SetNodeScheduling(context.Background(), "worker-1", true)
	if err != nil {
		t.Fatalf("SetNodeScheduling(true) error = %v", err)
	}
	if !summary.Unschedulable || summary.PodCount != 1 || summary.Name != "worker-1" {
		t.Fatalf("cordon summary = %#v", summary)
	}
	node, err := clientset.CoreV1().Nodes().Get(context.Background(), "worker-1", metav1.GetOptions{})
	if err != nil || !node.Spec.Unschedulable {
		t.Fatalf("node after cordon = %#v, err = %v", node.Spec, err)
	}
	updates, patches := 0, 0
	for _, action := range clientset.Actions() {
		switch action.GetVerb() {
		case "update":
			updates++
		case "patch":
			patches++
			patch := action.(k8stesting.PatchAction).GetPatch()
			if strings.Contains(string(patch), "resourceVersion") || strings.Contains(string(patch), "status") {
				t.Fatalf("node scheduling patch contains unrelated fields: %s", patch)
			}
		}
	}
	if updates != 0 || patches != 1 {
		t.Fatalf("scheduling actions: updates=%d patches=%d, want updates=0 patches=1", updates, patches)
	}

	summary, err = client.SetNodeScheduling(context.Background(), "worker-1", true)
	if err != nil || !summary.Unschedulable {
		t.Fatalf("idempotent cordon = (%#v, %v)", summary, err)
	}
	updates, patches = 0, 0
	for _, action := range clientset.Actions() {
		switch action.GetVerb() {
		case "update":
			updates++
		case "patch":
			patches++
		}
	}
	if updates != 0 || patches != 1 {
		t.Fatalf("idempotent cordon issued a write: updates=%d patches=%d", updates, patches)
	}

	summary, err = client.SetNodeScheduling(context.Background(), "worker-1", false)
	if err != nil || summary.Unschedulable {
		t.Fatalf("uncordon summary = (%#v, %v)", summary, err)
	}
	node, err = clientset.CoreV1().Nodes().Get(context.Background(), "worker-1", metav1.GetOptions{})
	if err != nil || node.Spec.Unschedulable {
		t.Fatalf("node after uncordon = %#v, err = %v", node.Spec, err)
	}
}

func TestSetNodeSchedulingMapsMissingNode(t *testing.T) {
	client, _ := newFakeKubeClient()
	_, err := client.SetNodeScheduling(context.Background(), "missing", true)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetNodeScheduling() error = %v, want ErrNotFound", err)
	}
}

func TestSetNodeLabelsPreservesNodeAndChecksResourceVersion(t *testing.T) {
	node := testNode()
	node.ResourceVersion = "9"
	node.Annotations = map[string]string{"owner": "platform"}
	client, clientset := newFakeKubeClient(node)

	detail, err := client.SetNodeLabels(context.Background(), "worker-1", domain.NodeLabelUpdate{
		Labels: map[string]string{"node-role.kubernetes.io/worker": "", "topology.kubernetes.io/zone": "az-1"}, ResourceVersion: "9",
	})
	if err != nil || detail.Labels["topology.kubernetes.io/zone"] != "az-1" || detail.Annotations["owner"] != "platform" || detail.ResourceVersion == "" {
		t.Fatalf("SetNodeLabels() = (%#v, %v)", detail, err)
	}
	stored, err := clientset.CoreV1().Nodes().Get(context.Background(), "worker-1", metav1.GetOptions{})
	if err != nil || stored.Labels["topology.kubernetes.io/zone"] != "az-1" || stored.Annotations["owner"] != "platform" {
		t.Fatalf("node after label update = %#v, err = %v", stored, err)
	}

	if _, err := client.SetNodeLabels(context.Background(), "worker-1", domain.NodeLabelUpdate{
		Labels: map[string]string{"node-role.kubernetes.io/worker": "", "topology.kubernetes.io/zone": "az-1"}, ResourceVersion: "stale",
	}); err != nil {
		t.Fatalf("idempotent label update = %v", err)
	}
	if _, err := client.SetNodeLabels(context.Background(), "worker-1", domain.NodeLabelUpdate{
		Labels: map[string]string{"team": "other"}, ResourceVersion: "stale",
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale label update error = %v, want ErrConflict", err)
	}
}

func TestSetNodeTaintsPreservesTimeAddedAndChecksResourceVersion(t *testing.T) {
	added := metav1.NewTime(time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC))
	node := testNode()
	node.ResourceVersion = "12"
	node.Spec.Taints = []corev1.Taint{{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule, TimeAdded: &added}}
	client, clientset := newFakeKubeClient(node)

	detail, err := client.SetNodeTaints(context.Background(), "worker-1", domain.NodeTaintUpdate{
		Taints: []domain.Taint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}, {Key: "maintenance", Value: "true", Effect: "NoExecute"}}, ResourceVersion: "12",
	})
	if err != nil || len(detail.Taints) != 2 || detail.Taints[1].Effect != "NoExecute" {
		t.Fatalf("SetNodeTaints() = (%#v, %v)", detail, err)
	}
	stored, err := clientset.CoreV1().Nodes().Get(context.Background(), "worker-1", metav1.GetOptions{})
	if err != nil || len(stored.Spec.Taints) != 2 || stored.Spec.Taints[0].TimeAdded == nil || !stored.Spec.Taints[0].TimeAdded.Equal(&added) || stored.Spec.Taints[1].TimeAdded == nil {
		t.Fatalf("node after taint update = %#v, err = %v", stored.Spec.Taints, err)
	}
	if _, err := client.SetNodeTaints(context.Background(), "worker-1", domain.NodeTaintUpdate{
		Taints: []domain.Taint{{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"}, {Key: "maintenance", Value: "true", Effect: "NoExecute"}}, ResourceVersion: "stale",
	}); err != nil {
		t.Fatalf("idempotent taint update = %v", err)
	}
	if _, err := client.SetNodeTaints(context.Background(), "worker-1", domain.NodeTaintUpdate{
		Taints: []domain.Taint{{Key: "other", Effect: "NoSchedule"}}, ResourceVersion: "stale",
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale taint update error = %v, want ErrConflict", err)
	}
}

func TestNamespacesSearchAndNotFound(t *testing.T) {
	client, _ := newFakeKubeClient(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
	)
	items, err := client.ListNamespaces(context.Background(), domain.ListQuery{Search: "pro"})
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "prod" {
		t.Fatalf("namespaces = %#v", items)
	}
	_, err = client.GetNamespace(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetNamespace() error = %v, want ErrNotFound", err)
	}
}

func TestSecretSummaryDoesNotExposeValuesAndRevealBase64Encodes(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "prod"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"token": []byte("sensitive")},
	}
	client, _ := newFakeKubeClient(secret)
	items, err := client.ListSecrets(context.Background(), domain.ListQuery{Namespace: "prod"})
	if err != nil {
		t.Fatalf("ListSecrets() error = %v", err)
	}
	if len(items) != 1 || len(items[0].Keys) != 1 || items[0].Keys[0].Key != "token" || items[0].Keys[0].Size != len("sensitive") {
		t.Fatalf("secret summaries = %#v", items)
	}
	reveal, err := client.RevealSecret(context.Background(), "prod", "app")
	if err != nil {
		t.Fatalf("RevealSecret() error = %v", err)
	}
	if reveal.Data["token"] != base64.StdEncoding.EncodeToString([]byte("sensitive")) {
		t.Fatalf("revealed data = %#v", reveal.Data)
	}
}

func TestConfigMapServiceIngressAndStorageConversions(t *testing.T) {
	class := "nginx"
	pathType := networkingv1.PathTypePrefix
	reclaim := corev1.PersistentVolumeReclaimDelete
	binding := storagev1.VolumeBindingWaitForFirstConsumer
	storageClass := "fast"
	ready := true
	notReady := false
	terminating := true
	objects := []runtime.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "prod"}, Data: map[string]string{"a": "1", "b": "22"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"}, Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort, ClusterIP: "10.96.0.1",
			Ports: []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromString("http"), NodePort: 30080}},
		}},
		&discoveryv1.EndpointSlice{
			ObjectMeta:  metav1.ObjectMeta{Name: "api-abc", Namespace: "prod", Labels: map[string]string{discoveryv1.LabelServiceName: "api"}},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.10", "10.0.0.11"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
				{Addresses: []string{"10.0.0.12"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
				{Addresses: []string{"10.0.0.13"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready, Terminating: &terminating}},
			},
		},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"}, Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			TLS:              []networkingv1.IngressTLS{{Hosts: []string{"app.example.test"}}},
			Rules: []networkingv1.IngressRule{{Host: "app.example.test", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Path: "/", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api", Port: networkingv1.ServiceBackendPort{Number: 80}}},
			}}}}}},
		}},
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "nginx", Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}}, Spec: networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"}},
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast", Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}}, Provisioner: "test.io/disk", ReclaimPolicy: &reclaim, VolumeBindingMode: &binding},
		&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-1"}, Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{corev1.ResourceStorage: resourceQuantity("1Gi")}, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, StorageClassName: storageClass}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: &storageClass, VolumeName: "pv-1"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, Capacity: corev1.ResourceList{corev1.ResourceStorage: resourceQuantity("1Gi")}}},
	}
	client, _ := newFakeKubeClient(objects...)

	configMaps, err := client.ListConfigMaps(context.Background(), domain.ListQuery{Namespace: "prod", Search: "b"})
	if err != nil || len(configMaps) != 1 || configMaps[0].DataBytes != 3 {
		t.Fatalf("configmaps = %#v, err = %v", configMaps, err)
	}
	configMap, err := client.GetConfigMap(context.Background(), "prod", "app")
	if err != nil || configMap.Data["a"] != "1" || configMap.Data["b"] != "22" || len(configMap.Keys) != 2 {
		t.Fatalf("configmap detail = %#v, err = %v", configMap, err)
	}
	if _, err := client.GetConfigMap(context.Background(), "prod", "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetConfigMap() missing error = %v", err)
	}
	services, err := client.ListServices(context.Background(), domain.ListQuery{Namespace: "prod", Search: "nodeport"})
	if err != nil || len(services) != 1 || services[0].Ports[0].NodePort != 30080 || services[0].Ports[0].TargetPort != "http" || services[0].EndpointCount != 2 {
		t.Fatalf("services = %#v, err = %v", services, err)
	}
	ingresses, err := client.ListIngresses(context.Background(), domain.ListQuery{Namespace: "prod", Search: "example"})
	if err != nil || len(ingresses) != 1 || ingresses[0].Class != "nginx" || len(ingresses[0].Paths) != 1 || ingresses[0].Paths[0].TargetStatus != domain.IngressTargetReady {
		t.Fatalf("ingresses = %#v, err = %v", ingresses, err)
	}
	ingressClasses, err := client.ListIngressClasses(context.Background(), domain.ListQuery{Search: "nginx"})
	if err != nil || len(ingressClasses) != 1 || !ingressClasses[0].Default || ingressClasses[0].Controller != "k8s.io/ingress-nginx" {
		t.Fatalf("ingress classes = %#v, err = %v", ingressClasses, err)
	}
	classes, err := client.ListStorageClasses(context.Background(), domain.ListQuery{Search: "fast"})
	if err != nil || len(classes) != 1 || !classes[0].Default || classes[0].VolumeBinding != string(binding) {
		t.Fatalf("storage classes = %#v, err = %v", classes, err)
	}
	pvs, err := client.ListPersistentVolumes(context.Background(), domain.ListQuery{Search: "pv-1"})
	if err != nil || len(pvs) != 1 || pvs[0].CapacityBytes != 1024*1024*1024 {
		t.Fatalf("persistent volumes = %#v, err = %v", pvs, err)
	}
	pvcs, err := client.ListPersistentVolumeClaims(context.Background(), domain.ListQuery{Namespace: "prod"})
	if err != nil || len(pvcs) != 1 || pvcs[0].VolumeName != "pv-1" {
		t.Fatalf("persistent volume claims = %#v, err = %v", pvcs, err)
	}
}

func TestStorageDetailConversionsAndNotFound(t *testing.T) {
	allowExpansion := true
	blockMode := corev1.PersistentVolumeBlock
	filesystemMode := corev1.PersistentVolumeFilesystem
	reclaim := corev1.PersistentVolumeReclaimRetain
	binding := storagev1.VolumeBindingWaitForFirstConsumer
	className := "fast"
	transition := metav1.NewTime(time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC))
	objects := []runtime.Object{
		&storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: "fast", Labels: map[string]string{"tier": "gold"}, Annotations: map[string]string{"owner": "platform"}},
			Provisioner: "csi.example.test", ReclaimPolicy: &reclaim, VolumeBindingMode: &binding,
			AllowVolumeExpansion: &allowExpansion, Parameters: map[string]string{"type": "ssd"}, MountOptions: []string{"discard"},
		},
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-a", Labels: map[string]string{"tier": "gold"}},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:               corev1.ResourceList{corev1.ResourceStorage: resourceQuantity("2Gi")},
				PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "csi.example.test"}},
				AccessModes:            []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, PersistentVolumeReclaimPolicy: reclaim,
				StorageClassName: className, VolumeMode: &blockMode, MountOptions: []string{"discard"},
				ClaimRef: &corev1.ObjectReference{Namespace: "prod", Name: "data"},
			},
			Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod", Annotations: map[string]string{"backup": "daily"}},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: &className,
				VolumeName: "pv-a", VolumeMode: &filesystemMode,
				Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resourceQuantity("2Gi")}},
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase: corev1.ClaimBound, Capacity: corev1.ResourceList{corev1.ResourceStorage: resourceQuantity("2Gi")},
				Conditions: []corev1.PersistentVolumeClaimCondition{{Type: corev1.PersistentVolumeClaimResizing, Status: corev1.ConditionTrue, Reason: "Resizing", Message: "expanding", LastTransitionTime: transition}},
			},
		},
	}
	client, _ := newFakeKubeClient(objects...)

	class, err := client.GetStorageClass(context.Background(), "fast")
	if err != nil || !class.AllowVolumeExpansion || class.Parameters["type"] != "ssd" || class.Labels["tier"] != "gold" || len(class.MountOptions) != 1 {
		t.Fatalf("storage class detail = %#v, err = %v", class, err)
	}
	volume, err := client.GetPersistentVolume(context.Background(), "pv-a")
	if err != nil || volume.VolumeMode != "Block" || volume.ClaimNamespace != "prod" || volume.ClaimName != "data" || volume.CSIDriver != "csi.example.test" {
		t.Fatalf("persistent volume detail = %#v, err = %v", volume, err)
	}
	claim, err := client.GetPersistentVolumeClaim(context.Background(), "prod", "data")
	if err != nil || claim.VolumeMode != "Filesystem" || claim.RequestedCapacityBytes != 2*1024*1024*1024 || len(claim.Conditions) != 1 || claim.Annotations["backup"] != "daily" {
		t.Fatalf("persistent volume claim detail = %#v, err = %v", claim, err)
	}
	if _, err := client.GetStorageClass(context.Background(), "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetStorageClass() missing error = %v", err)
	}
	if _, err := client.GetPersistentVolume(context.Background(), "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPersistentVolume() missing error = %v", err)
	}
	if _, err := client.GetPersistentVolumeClaim(context.Background(), "prod", "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPersistentVolumeClaim() missing error = %v", err)
	}
}

func TestIngressTargetStatuses(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	ready := true
	objects := []runtime.Object{
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "ready", Namespace: "prod"}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "idle", Namespace: "prod"}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}}},
		&discoveryv1.EndpointSlice{
			ObjectMeta:  metav1.ObjectMeta{Name: "ready-abc", Namespace: "prod", Labels: map[string]string{discoveryv1.LabelServiceName: "ready"}},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}}},
		},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			Host: "app.example.test", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
				{Path: "/", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "ready", Port: networkingv1.ServiceBackendPort{Number: 80}}}},
				{Path: "/idle", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "idle", Port: networkingv1.ServiceBackendPort{Number: 80}}}},
				{Path: "/wrong-port", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "ready", Port: networkingv1.ServiceBackendPort{Number: 81}}}},
				{Path: "/missing", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "missing", Port: networkingv1.ServiceBackendPort{Number: 80}}}},
			}}},
		}}}},
	}
	client, _ := newFakeKubeClient(objects...)

	ingresses, err := client.ListIngresses(context.Background(), domain.ListQuery{Namespace: "prod"})
	if err != nil {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if len(ingresses) != 1 || len(ingresses[0].Paths) != 4 {
		t.Fatalf("ingresses = %#v", ingresses)
	}
	want := []string{domain.IngressTargetReady, domain.IngressTargetNoReadyEndpoints, domain.IngressTargetPortNotFound, domain.IngressTargetServiceNotFound}
	for index, status := range want {
		if ingresses[0].Paths[index].TargetStatus != status {
			t.Fatalf("path %d target status = %q, want %q", index, ingresses[0].Paths[index].TargetStatus, status)
		}
	}
}

func TestListEventsAndWatch(t *testing.T) {
	event := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "evt", Namespace: "prod", UID: "event-uid", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute))},
		Type:           corev1.EventTypeWarning,
		Reason:         "BackOff",
		Message:        "container restarting",
		Count:          2,
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-0"},
		Source:         corev1.EventSource{Component: "kubelet"},
	}
	client, _ := newFakeKubeClient(event)
	items, err := client.ListEvents(context.Background(), domain.ListQuery{Namespace: "prod", Search: "backoff", ObjectKind: "pod", ObjectName: "api-0"})
	if err != nil || len(items) != 1 || items[0].ObjectName != "api-0" || items[0].Count != 2 || items[0].UID != "event-uid" {
		t.Fatalf("events = %#v, err = %v", items, err)
	}
	items, err = client.ListEvents(context.Background(), domain.ListQuery{Namespace: "prod", ObjectName: "different"})
	if err != nil || len(items) != 0 {
		t.Fatalf("filtered events = %#v, err = %v", items, err)
	}
	watcher, errs, err := client.WatchEvents(context.Background(), "prod")
	if err != nil {
		t.Fatalf("WatchEvents() error = %v", err)
	}
	if watcher == nil || errs == nil {
		t.Fatal("WatchEvents() returned nil channels")
	}
}
