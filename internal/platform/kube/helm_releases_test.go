package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

func helmSecret(name string, revision int, chart, appVersion, manifest string) *corev1.Secret {
	payload := map[string]any{
		"name": name, "namespace": "apps", "version": revision,
		"info":     map[string]any{"status": "deployed", "last_deployed": time.Now().UTC(), "description": "upgrade complete"},
		"chart":    map[string]any{"metadata": map[string]any{"name": chart, "version": "1.2.3", "appVersion": appVersion}},
		"config":   map[string]any{"password": "plain", "replicaCount": 2},
		"manifest": manifest,
	}
	encoded, _ := json.Marshal(payload)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(encoded)
	_ = writer.Close()
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, revision), Namespace: "apps", Labels: map[string]string{"owner": "helm"}},
		Type:       corev1.SecretType(helmReleaseSecretType),
		Data:       map[string][]byte{"release": []byte(base64.StdEncoding.EncodeToString(compressed.Bytes()))},
	}
}

func TestHelmReleaseDecodeAndLatestHistory(t *testing.T) {
	first := helmSecret("web", 1, "web", "1.0", "apiVersion: v1\nkind: Service\n")
	second := helmSecret("web", 2, "web", "2.0", "apiVersion: v1\nkind: ConfigMap\n")
	client, _ := newFakeKubeClient(first, second)
	items, warnings, err := client.ListHelmReleases(context.Background(), "apps")
	if err != nil {
		t.Fatalf("ListHelmReleases() error = %v", err)
	}
	if len(warnings) != 0 || len(items) != 1 || items[0].Revision != 2 || items[0].AppVersion != "2.0" {
		t.Fatalf("releases = %#v warnings=%v", items, warnings)
	}
	detail, err := client.GetHelmRelease(context.Background(), "apps", "web")
	if err != nil {
		t.Fatalf("GetHelmRelease() error = %v", err)
	}
	if detail.Revision != 2 || len(detail.History) != 2 || detail.History[0].Revision != 2 || detail.Manifest == "" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestHelmReleaseRejectsMalformedSecretWithWarning(t *testing.T) {
	broken := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "apps", Labels: map[string]string{"owner": "helm"}}, Type: corev1.SecretType(helmReleaseSecretType), Data: map[string][]byte{"release": []byte("not-base64")}}
	client, _ := newFakeKubeClient(broken)
	items, warnings, err := client.ListHelmReleases(context.Background(), "apps")
	if err != nil {
		t.Fatalf("ListHelmReleases() error = %v", err)
	}
	if len(items) != 0 || len(warnings) != 1 {
		t.Fatalf("items=%#v warnings=%#v", items, warnings)
	}
}

func helmRollbackClient(first, second *corev1.Secret) (*Client, *k8sfake.Clientset, *dynamicfake.FakeDynamicClient) {
	clientset := k8sfake.NewSimpleClientset(first, second)
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "ConfigMapList"},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": "web-config", "namespace": "apps"},
			"data":     map[string]any{"mode": "current"},
		}},
	)
	discovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discovery.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "configmaps", SingularName: "configmap", Kind: "ConfigMap", Namespaced: true, Verbs: []string{"get", "list", "create", "update", "patch"}}},
	}}
	return NewForClients(clientset, dynamicClient, discovery, testConfig()), clientset, dynamicClient
}

func TestHelmRollbackDryRunsBeforeApplyAndRecordsRevision(t *testing.T) {
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web-config\n  namespace: apps\ndata:\n  mode: target\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: upgrade-hook\n  namespace: apps\n  annotations:\n    helm.sh/hook: pre-upgrade\n"
	client, clientset, dynamicClient := helmRollbackClient(
		helmSecret("web", 1, "web", "1.0", manifest),
		helmSecret("web", 2, "web", "2.0", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web-config\n  namespace: apps\ndata:\n  mode: current\n"),
	)
	type recordedPatch struct {
		patch   []byte
		options metav1.PatchOptions
	}
	var patches []recordedPatch
	dynamicClient.PrependReactor("patch", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch := action.(k8stesting.PatchAction)
		options := metav1.PatchOptions{}
		if value, ok := action.(interface{ GetPatchOptions() metav1.PatchOptions }); ok {
			options = value.GetPatchOptions()
		}
		patches = append(patches, recordedPatch{patch: patch.GetPatch(), options: options})
		return true, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": "web-config", "namespace": "apps", "resourceVersion": "2"},
			"data":     map[string]any{"mode": "target"},
		}}, nil
	})

	result, err := client.RollbackHelmRelease(context.Background(), "apps", "web", 1)
	if err != nil {
		t.Fatalf("RollbackHelmRelease() error = %v", err)
	}
	if result.Revision != 3 || result.AppliedObjects != 1 || len(result.Warnings) < 2 {
		t.Fatalf("rollback result = %#v", result)
	}
	if len(patches) != 2 {
		t.Fatalf("patch actions = %d, want dry-run + apply", len(patches))
	}
	if dryRun := patches[0].options.DryRun; len(dryRun) != 1 || dryRun[0] != metav1.DryRunAll {
		t.Fatalf("first patch dry-run = %#v", dryRun)
	}
	if dryRun := patches[1].options.DryRun; len(dryRun) != 0 {
		t.Fatalf("second patch must be live, dry-run = %#v", dryRun)
	}
	for _, patch := range patches {
		if strings.Contains(string(patch.patch), "upgrade-hook") {
			t.Fatal("Helm hook was applied")
		}
	}
	created, err := clientset.CoreV1().Secrets("apps").Get(context.Background(), "sh.helm.release.v1.web.v3", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("new Helm revision secret: %v", err)
	}
	payload, _, err := decodeHelmReleaseSecret(*created)
	if err != nil || payload.Version != 3 || payload.Info.Status != "deployed" {
		t.Fatalf("new revision payload = %#v, err = %v", payload, err)
	}
	superseded, err := clientset.CoreV1().Secrets("apps").Get(context.Background(), "sh.helm.release.v1.web.v2", metav1.GetOptions{})
	if err != nil || superseded.Labels["status"] != "superseded" {
		t.Fatalf("previous revision labels = %#v, err = %v", superseded.Labels, err)
	}
}

func TestHelmRollbackStopsWhenDryRunFails(t *testing.T) {
	client, clientset, dynamicClient := helmRollbackClient(
		helmSecret("web", 1, "web", "1.0", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web-config\n  namespace: apps\n"),
		helmSecret("web", 2, "web", "2.0", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web-config\n  namespace: apps\n"),
	)
	patches := 0
	dynamicClient.PrependReactor("patch", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		patches++
		return true, nil, errors.New("admission denied")
	})
	if _, err := client.RollbackHelmRelease(context.Background(), "apps", "web", 1); err == nil {
		t.Fatal("RollbackHelmRelease() error = nil, want preflight failure")
	}
	if patches != 1 {
		t.Fatalf("patch actions = %d, want only dry-run", patches)
	}
	if _, err := clientset.CoreV1().Secrets("apps").Get(context.Background(), "sh.helm.release.v1.web.v3", metav1.GetOptions{}); err == nil {
		t.Fatal("Helm revision was recorded after a failed preflight")
	}
}
