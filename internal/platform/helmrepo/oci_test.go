package helmrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
)

func TestParseOCITarget(t *testing.T) {
	t.Parallel()
	target, err := parseOCITarget("oci://registry.example.com/team/charts/demo")
	if err != nil {
		t.Fatalf("parseOCITarget() error = %v", err)
	}
	if target.Host != "registry.example.com" || target.Repository != "registry.example.com/team/charts/demo" || target.ChartName != "demo" {
		t.Fatalf("parseOCITarget() = %#v", target)
	}
	for _, raw := range []string{
		"https://registry.example.com/charts/demo",
		"oci://registry.example.com/team/charts/demo:1.2.3",
		"oci://registry.example.com/team/charts/demo?tag=1",
		"oci://registry.example.com/team/charts/demo#fragment",
		"oci://registry.example.com",
	} {
		if _, err := parseOCITarget(raw); err == nil {
			t.Fatalf("parseOCITarget(%q) accepted an invalid reference", raw)
		}
	}
}

func TestClientOCIChartsDetailAndDownload(t *testing.T) {
	t.Parallel()
	archive := testOCIChartArchive(t)
	meta := testOCIChartMetadata()
	configData, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal chart metadata: %v", err)
	}
	configDigest, configSize := digestBytes(configData)
	chartDigest, chartSize := digestBytes(archive)
	manifest := fmt.Sprintf(`{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": %q, "size": %d},
  "layers": [{"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": %q, "size": %d}]
}`, configDigest, configSize, chartDigest, chartSize)
	manifestDigest, _ := digestBytes([]byte(manifest))

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/" || r.URL.Path == "/v2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		case r.URL.Path == "/v2/charts/demo/tags/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"charts/demo","tags":["1.2.3"]}`))
		case r.URL.Path == "/v2/charts/demo/manifests/1.2.3":
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Header().Set("Docker-Content-Digest", manifestDigest)
			w.Header().Set("Content-Length", fmt.Sprint(len(manifest)))
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(manifest))
			}
		case r.URL.Path == "/v2/charts/demo/blobs/"+configDigest:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Docker-Content-Digest", configDigest)
			_, _ = w.Write(configData)
		case r.URL.Path == "/v2/charts/demo/blobs/"+chartDigest:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Docker-Content-Digest", chartDigest)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repository := domain.HelmRepository{
		ID: "repo-oci", Name: "OCI", URL: "oci://" + strings.TrimPrefix(server.URL, "https://") + "/charts/demo",
		Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous, InsecureSkipTLS: true,
	}
	client := New(5 * time.Second)
	charts, err := client.Charts(context.Background(), repository)
	if err != nil {
		t.Fatalf("Charts() error = %v", err)
	}
	if len(charts) != 1 || charts[0].Name != "demo" || charts[0].Version != "1.2.3" || charts[0].AppVersion != "4.5.6" || charts[0].Digest != chartDigest {
		t.Fatalf("Charts() = %#v", charts)
	}

	detail, err := client.Detail(context.Background(), repository, "demo")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if detail.Name != "demo" || detail.Version != "1.2.3" || !strings.Contains(detail.Readme, "# Demo") || !strings.Contains(detail.Values, "replicaCount: 1") || !strings.Contains(detail.Schema, "replicaCount") {
		t.Fatalf("Detail() = %#v", detail)
	}
	if len(detail.Versions) != 1 || detail.Versions[0].Version != "1.2.3" || detail.Versions[0].Digest != chartDigest {
		t.Fatalf("Detail().Versions = %#v", detail.Versions)
	}

	data, digest, err := client.Download(context.Background(), repository, "demo", "1.2.3", chartDigest)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if digest != chartDigest || string(data) != string(archive) {
		t.Fatalf("Download() = (%d bytes, %q)", len(data), digest)
	}
	if _, _, err := client.Download(context.Background(), repository, "demo", "1.2.3", "sha256:deadbeef"); err == nil {
		t.Fatal("Download() accepted a mismatched reviewed digest")
	}
}

func testOCIChartMetadata() *chart.Metadata {
	return &chart.Metadata{
		APIVersion: "v2", Name: "demo", Version: "1.2.3", AppVersion: "4.5.6",
		Description: "OCI demo chart", Keywords: []string{"demo", "oci"},
	}
}

func testOCIChartArchive(t *testing.T) []byte {
	t.Helper()
	ch := &chart.Chart{
		Metadata: testOCIChartMetadata(),
		Values:   map[string]any{"replicaCount": 1},
		Schema:   []byte(`{"type":"object","properties":{"replicaCount":{"type":"integer"}}}`),
		Templates: []*chart.File{{
			Name: "templates/configmap.yaml",
			Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n"),
		}},
		Files: []*chart.File{
			{Name: "README.md", Data: []byte("# Demo\n\nOCI chart.\n")},
			{Name: "values.yaml", Data: []byte("replicaCount: 1\n")},
		},
	}
	output, err := chartutil.Save(ch, t.TempDir())
	if err != nil {
		t.Fatalf("save chart archive: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read chart archive: %v", err)
	}
	return data
}

func digestBytes(data []byte) (string, int) {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), len(data)
}
