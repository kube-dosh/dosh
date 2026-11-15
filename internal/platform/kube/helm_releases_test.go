package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
