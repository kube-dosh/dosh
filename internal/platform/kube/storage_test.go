package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestStorageClassWritesUseDryRunAndResourceVersions(t *testing.T) {
	client, clientset := newFakeKubeClient()
	created, err := client.CreateStorageClass(context.Background(), domain.StorageClassWriteSpec{
		Name: "fast", Provisioner: "csi.example.test", ReclaimPolicy: "Delete", VolumeBinding: "Immediate",
		AllowVolumeExpansion: true, Default: true, Parameters: map[string]string{"type": "ssd"},
	}, true)
	if err != nil {
		t.Fatalf("CreateStorageClass(dry-run) error = %v", err)
	}
	if created.Name != "fast" || !created.AllowVolumeExpansion || !created.Default || created.Annotations[defaultStorageClassAnnotation] != "true" {
		t.Fatalf("created StorageClass = %#v", created)
	}
	actions := clientset.Actions()
	if len(actions) != 1 {
		t.Fatalf("StorageClass create actions = %#v", actions)
	}
	if actions[0].GetResource().Resource != "storageclasses" || !hasDryRun(actions[0]) {
		t.Fatalf("StorageClass create action = %#v", actions[0])
	}

	existing := &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "standard", ResourceVersion: "7"},
		Provisioner: "csi.example.test", Parameters: map[string]string{"type": "hdd"},
	}
	updateClient, _ := newFakeKubeClient(existing)
	updated, err := updateClient.UpdateStorageClass(context.Background(), "standard", domain.StorageClassWriteSpec{
		Provisioner: "csi.example.test", ReclaimPolicy: "Retain", VolumeBinding: "WaitForFirstConsumer", ResourceVersion: "7",
	}, true)
	if err != nil || updated.ReclaimPolicy != "Retain" || updated.VolumeBinding != "WaitForFirstConsumer" {
		t.Fatalf("UpdateStorageClass() = (%#v, %v)", updated, err)
	}
	if _, err := updateClient.UpdateStorageClass(context.Background(), "standard", domain.StorageClassWriteSpec{
		Provisioner: "csi.example.test", ReclaimPolicy: "Retain", VolumeBinding: "Immediate", ResourceVersion: "stale",
	}, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale StorageClass update error = %v, want ErrConflict", err)
	}
}

func TestPersistentVolumeWritesUseDryRunAndCapacityGuards(t *testing.T) {
	client, clientset := newFakeKubeClient()
	created, err := client.CreatePersistentVolume(context.Background(), domain.PersistentVolumeCreateSpec{
		Name: "pv-new", CapacityBytes: 1 << 30, AccessModes: []string{"ReadWriteOnce"}, ReclaimPolicy: "Retain",
		VolumeMode: "Filesystem", CSI: &domain.CSIPersistentVolumeSource{Driver: "csi.example.test", VolumeHandle: "vol-1"},
	}, true)
	if err != nil {
		t.Fatalf("CreatePersistentVolume(dry-run) error = %v", err)
	}
	if created.Name != "pv-new" || created.Source.CSIVolumeHandle != "vol-1" || created.CapacityBytes != 1<<30 {
		t.Fatalf("created PV = %#v", created)
	}
	actions := clientset.Actions()
	if len(actions) != 1 {
		t.Fatalf("PV create actions = %#v", actions)
	}
	if actions[0].GetResource().Resource != "persistentvolumes" || !hasDryRun(actions[0]) {
		t.Fatalf("PV create action = %#v", actions[0])
	}

	existing := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-a", ResourceVersion: "9"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resourceQuantity("2Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}
	updateClient, _ := newFakeKubeClient(existing)
	updated, err := updateClient.UpdatePersistentVolume(context.Background(), "pv-a", domain.PersistentVolumeUpdateSpec{
		CapacityBytes: 3 << 30, ReclaimPolicy: "Delete", MountOptions: []string{"discard"}, ResourceVersion: "9",
	}, true)
	if err != nil || updated.CapacityBytes != 3<<30 || updated.ReclaimPolicy != "Delete" {
		t.Fatalf("UpdatePersistentVolume() = (%#v, %v)", updated, err)
	}
	if _, err := updateClient.UpdatePersistentVolume(context.Background(), "pv-a", domain.PersistentVolumeUpdateSpec{
		CapacityBytes: 1 << 30, ReclaimPolicy: "Retain", ResourceVersion: "9",
	}, false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("PV shrink error = %v, want ErrInvalid", err)
	}
	if _, err := updateClient.UpdatePersistentVolume(context.Background(), "pv-a", domain.PersistentVolumeUpdateSpec{
		CapacityBytes: 3 << 30, ReclaimPolicy: "Retain", ResourceVersion: "stale",
	}, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale PV update error = %v, want ErrConflict", err)
	}
}

func TestStorageImpactCountsOnlyActivePodsAndAllContainers(t *testing.T) {
	running := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "db-0", Namespace: "prod", OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db", Controller: boolPointer(true),
		}}},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init-db", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/var/lib/postgresql"}}}},
			Containers: []corev1.Container{
				{Name: "postgres", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/var/lib/postgresql/data"}}},
				{Name: "metrics", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/metrics-data", ReadOnly: true}}},
			},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	pending := running.DeepCopy()
	pending.Name = "db-1"
	pending.Status.Phase = corev1.PodPending
	completed := running.DeepCopy()
	completed.Name = "db-migrate"
	completed.Status.Phase = corev1.PodSucceeded
	client, _ := newFakeKubeClient(running, pending, completed)

	impact := client.storageImpactForClaim(context.Background(), "prod", "data")
	if impact.MountCount != 2 || len(impact.Mounts) != 2 || len(impact.Warnings) != 0 {
		t.Fatalf("storage impact = %#v", impact)
	}
	if impact.Mounts[0].Pod != "db-0" || impact.Mounts[0].WorkloadKind != "StatefulSet" || impact.Mounts[0].WorkloadName != "db" {
		t.Fatalf("storage mount = %#v", impact.Mounts[0])
	}
	if got := impact.Mounts[0].Containers; len(got) != 3 || got[0] != "init-db" || got[1] != "metrics" || got[2] != "postgres" {
		t.Fatalf("mounted containers = %#v", got)
	}
	if got := impact.Mounts[0].MountPaths; len(got) != 3 {
		t.Fatalf("mount paths = %#v", got)
	}
}

func TestVolumeSnapshotFeatureDegradesWithoutCRD(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	client := NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, discovery, testConfig())

	feature, err := client.VolumeSnapshotFeature(context.Background())
	if err != nil || feature.Available || feature.Reason != volumeSnapshotUnavailableReason || feature.Classes == nil {
		t.Fatalf("VolumeSnapshotFeature() = (%#v, %v)", feature, err)
	}
	if _, err := client.ListVolumeSnapshots(context.Background(), domain.ListQuery{}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("ListVolumeSnapshots() error = %v, want ErrUnsupported", err)
	}
	if _, err := client.CreateVolumeSnapshot(context.Background(), domain.VolumeSnapshotCreateSpec{Namespace: "prod", Name: "snap", SourcePVC: "data"}, false); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("CreateVolumeSnapshot() error = %v, want ErrUnsupported", err)
	}
}

func TestVolumeSnapshotFeatureAndListingsUseV1API(t *testing.T) {
	snapshotGVR := schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}
	classGVR := schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
	snapshot := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot",
		"metadata": map[string]any{"name": "snap-a", "namespace": "prod", "resourceVersion": "13", "creationTimestamp": "2026-11-19T00:00:00Z"},
		"spec":     map[string]any{"source": map[string]any{"persistentVolumeClaimName": "data"}, "volumeSnapshotClassName": "csi-snap"},
		"status":   map[string]any{"readyToUse": true, "restoreSize": "5Gi", "boundVolumeSnapshotContentName": "content-a", "creationTime": "2026-11-19T00:01:00Z"},
	}}
	class := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass",
		"metadata": map[string]any{"name": "csi-snap", "annotations": map[string]any{"snapshot.storage.kubernetes.io/is-default-class": "true"}},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		snapshotGVR: "VolumeSnapshotList",
		classGVR:    "VolumeSnapshotClassList",
	}, snapshot, class)
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{
		GroupVersion: "snapshot.storage.k8s.io/v1",
		APIResources: []metav1.APIResource{
			{Name: "volumesnapshots", Kind: "VolumeSnapshot", Namespaced: true, Verbs: []string{"get", "list", "create", "delete"}},
			{Name: "volumesnapshotclasses", Kind: "VolumeSnapshotClass", Namespaced: false, Verbs: []string{"get", "list"}},
		},
	}}
	client := NewForClients(k8sfake.NewSimpleClientset(), dynamicClient, discovery, testConfig())

	feature, err := client.VolumeSnapshotFeature(context.Background())
	if err != nil || !feature.Available || feature.APIVersion != "snapshot.storage.k8s.io/v1" || feature.DefaultClass != "csi-snap" ||
		len(feature.Classes) != 1 || feature.Classes[0] != "csi-snap" {
		t.Fatalf("VolumeSnapshotFeature() = (%#v, %v)", feature, err)
	}
	items, err := client.ListVolumeSnapshots(context.Background(), domain.ListQuery{Namespace: "prod", Search: "snap"})
	if err != nil || len(items) != 1 {
		t.Fatalf("ListVolumeSnapshots() = (%#v, %v)", items, err)
	}
	item := items[0]
	if item.Name != "snap-a" || item.Namespace != "prod" || item.SourcePVC != "data" || item.Status != "Ready" ||
		!item.ReadyToUse || item.RestoreSizeBytes != 5<<30 || item.ResourceVersion != "13" {
		t.Fatalf("listed snapshot = %#v", item)
	}

	if _, err := client.CreateVolumeSnapshot(context.Background(), domain.VolumeSnapshotCreateSpec{
		Name: "snap-b", Namespace: "prod", SourcePVC: "data", VolumeSnapshotClassName: "csi-snap",
	}, true); err != nil {
		t.Fatalf("CreateVolumeSnapshot(dry-run) error = %v", err)
	}
	var createAction k8stesting.Action
	for _, action := range dynamicClient.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == "volumesnapshots" {
			createAction = action
		}
	}
	if createAction == nil || !hasDryRun(createAction) {
		t.Fatalf("snapshot create action = %#v", dynamicClient.Actions())
	}
}

func hasDryRun(action k8stesting.Action) bool {
	switch typed := action.(type) {
	case k8stesting.CreateActionImpl:
		return len(typed.CreateOptions.DryRun) == 1 && typed.CreateOptions.DryRun[0] == metav1.DryRunAll
	case *k8stesting.CreateActionImpl:
		return len(typed.CreateOptions.DryRun) == 1 && typed.CreateOptions.DryRun[0] == metav1.DryRunAll
	}
	return false
}
