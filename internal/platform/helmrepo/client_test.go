package helmrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

func TestPackageURLKeepsRepositoryHostAndHTTPS(t *testing.T) {
	t.Parallel()
	target, err := packageURL("https://charts.example.com/stable", "demo-1.0.0.tgz")
	if err != nil || target != "https://charts.example.com/stable/demo-1.0.0.tgz" {
		t.Fatalf("packageURL() = (%q, %v)", target, err)
	}
	if _, err := packageURL("https://charts.example.com/stable", "https://other.example.com/demo.tgz"); err == nil {
		t.Fatal("packageURL() accepted a different host")
	}
	if _, err := packageURL("https://charts.example.com/stable", "http://charts.example.com/demo.tgz"); err == nil {
		t.Fatal("packageURL() accepted an HTTPS downgrade")
	}
}

func TestClientIndexChartsAndDigestVerification(t *testing.T) {
	t.Parallel()
	archive := []byte("chart-archive")
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      appVersion: "2.0.0"
      description: Demo chart
      created: "2026-11-01T00:00:00Z"
      digest: %s
      urls:
        - /demo-1.0.0.tgz
generated: "2026-11-01T00:00:00Z"
`, digest)
		case "/demo-1.0.0.tgz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
		_ = server
	}))
	defer server.Close()

	client := New(5 * time.Second)
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	charts, err := client.Charts(context.Background(), repository)
	if err != nil {
		t.Fatalf("Charts() error = %v", err)
	}
	if len(charts) != 1 || charts[0].Name != "demo" || charts[0].Version != "1.0.0" || charts[0].Digest != digest {
		t.Fatalf("Charts() = %#v", charts)
	}
	body, actual, err := client.Download(context.Background(), repository, "demo", "1.0.0", digest)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if string(body) != string(archive) || actual != digest {
		t.Fatalf("Download() = (%q, %q)", body, actual)
	}
	if _, _, err := client.Download(context.Background(), repository, "demo", "1.0.0", "sha256:deadbeef"); err == nil {
		t.Fatal("Download() accepted a mismatched reviewed digest")
	}
}

func TestDigestsEqualNormalizesPrefixAndCase(t *testing.T) {
	t.Parallel()
	if !digestsEqual("SHA256:ABCDEF", "sha256:abcdef") {
		t.Fatal("digestsEqual() rejected equivalent digests")
	}
	if digestsEqual("", "") || !strings.EqualFold("sha256:abc", "sha256:abc") {
		t.Fatal("digestsEqual() handling is incorrect")
	}
}
