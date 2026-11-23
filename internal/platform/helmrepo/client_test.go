package helmrepo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dosh/internal/domain"
)

func TestPackageURLSupportsExternalHTTPSPackagesWithoutAllowingDowngrades(t *testing.T) {
	t.Parallel()
	target, err := packageURL("https://charts.example.com/stable", "demo-1.0.0.tgz")
	if err != nil || target != "https://charts.example.com/stable/demo-1.0.0.tgz" {
		t.Fatalf("packageURL() = (%q, %v)", target, err)
	}
	target, err = packageURL("https://charts.example.com/stable", "https://github.com/example/demo.tgz")
	if err != nil || target != "https://github.com/example/demo.tgz" {
		t.Fatalf("packageURL() external HTTPS = (%q, %v)", target, err)
	}
	if _, err := packageURL("https://charts.example.com/stable", "http://charts.example.com/demo.tgz"); err == nil {
		t.Fatal("packageURL() accepted an HTTPS downgrade")
	}
	if _, err := packageURL("http://charts.example.com/stable", "http://other.example.com/demo.tgz"); err == nil {
		t.Fatal("packageURL() accepted a cross-host insecure package URL")
	}
}

func TestGetOnlySendsCredentialsToRepositoryHost(t *testing.T) {
	t.Parallel()
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{timeout: time.Second}
	repository := domain.HelmRepository{
		URL: "https://charts.example.com", Credentials: domain.HelmRepositoryCredentials{Token: "repository-token"},
	}
	response, err := client.get(context.Background(), server.Client(), repository, server.URL+"/demo.tgz")
	if err != nil {
		t.Fatalf("get() external package error = %v", err)
	}
	response.Body.Close()
	if authorization != "" {
		t.Fatalf("external package received authorization %q", authorization)
	}
	response, err = client.get(context.Background(), server.Client(), domain.HelmRepository{
		URL: server.URL, Credentials: domain.HelmRepositoryCredentials{Token: "repository-token"},
	}, server.URL+"/demo.tgz")
	if err != nil {
		t.Fatalf("get() repository package error = %v", err)
	}
	response.Body.Close()
	if authorization != "Bearer repository-token" {
		t.Fatalf("repository package authorization = %q", authorization)
	}
}

func TestHTTPClientAllowsHTTPSRedirectsAcrossHostsButRejectsDowngrades(t *testing.T) {
	t.Parallel()
	client, err := New(5 * time.Second).httpClient(domain.HelmRepository{URL: "https://charts.example.com/bitnami"})
	if err != nil {
		t.Fatalf("httpClient() error = %v", err)
	}
	origin, _ := http.NewRequest(http.MethodGet, "https://charts.example.com/bitnami/index.yaml", nil)
	redirect, _ := http.NewRequest(http.MethodGet, "https://repo.example.net/files/index.yaml", nil)
	if err := client.CheckRedirect(redirect, []*http.Request{origin}); err != nil {
		t.Fatalf("CheckRedirect() rejected cross-host HTTPS redirect: %v", err)
	}
	downgrade, _ := http.NewRequest(http.MethodGet, "http://repo.example.net/files/index.yaml", nil)
	if err := client.CheckRedirect(downgrade, []*http.Request{origin}); err == nil {
		t.Fatal("CheckRedirect() accepted HTTPS downgrade")
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

func TestRefreshUsesConditionalIndexRequest(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	var conditional atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("If-None-Match") == `"index-v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		requests.Add(1)
		w.Header().Set("ETag", `"index-v1"`)
		w.Header().Set("Last-Modified", "Wed, 19 Nov 2026 08:00:00 GMT")
		_, _ = fmt.Fprint(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      description: Demo chart
      urls:
        - /demo-1.0.0.tgz
generated: "2026-11-19T08:00:00Z"
`)
	}))
	defer server.Close()

	client := New(5 * time.Second)
	client.SetCacheDir(t.TempDir())
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	first, err := client.Refresh(context.Background(), repository)
	if err != nil || len(first) != 1 {
		t.Fatalf("first Refresh() = (%#v, %v)", first, err)
	}
	second, err := client.Refresh(context.Background(), repository)
	if err != nil || len(second) != 1 {
		t.Fatalf("second Refresh() = (%#v, %v)", second, err)
	}
	if requests.Load() != 1 || conditional.Load() != 1 {
		t.Fatalf("conditional requests = full %d, 304 %d", requests.Load(), conditional.Load())
	}
}

func TestCacheCleanupAndRepositoryRemoval(t *testing.T) {
	t.Parallel()
	client := New(5 * time.Second)
	client.SetCacheDir(t.TempDir())
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: "https://charts.example.com", Enabled: true}
	if err := client.storeChartPackage(repository, "demo", "1.0.0", bytes.Repeat([]byte("a"), 1024)); err != nil {
		t.Fatalf("storeChartPackage() error = %v", err)
	}
	if err := client.storeChartPackage(repository, "demo", "2.0.0", bytes.Repeat([]byte("b"), 1024)); err != nil {
		t.Fatalf("storeChartPackage(second) error = %v", err)
	}
	stats, err := client.CacheStats()
	if err != nil || stats.PackageCount != 2 {
		t.Fatalf("CacheStats() = (%#v, %v)", stats, err)
	}
	result, err := client.CleanupCache(1024)
	if err != nil || result.DeletedFiles == 0 || result.Stats.TotalBytes > 1024 {
		t.Fatalf("CleanupCache() = (%#v, %v)", result, err)
	}
	if err := client.RemoveRepositoryCache(repository); err != nil {
		t.Fatalf("RemoveRepositoryCache() error = %v", err)
	}
	stats, err = client.CacheStats()
	if err != nil || stats.PackageCount != 0 {
		t.Fatalf("CacheStats() after removal = (%#v, %v)", stats, err)
	}
}

func TestIndexCacheSurvivesClientRestartAndRefreshesAfterInvalidate(t *testing.T) {
	t.Parallel()
	var indexRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			http.NotFound(w, r)
			return
		}
		indexRequests.Add(1)
		_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      urls:
        - /demo-1.0.0.tgz
generated: "2026-11-01T00:00:00Z"
`)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	first := New(5 * time.Second)
	first.SetCacheDir(cacheDir)
	if _, err := first.Charts(context.Background(), repository); err != nil {
		t.Fatalf("first Charts() error = %v", err)
	}
	if indexRequests.Load() != 1 {
		t.Fatalf("first client index requests = %d", indexRequests.Load())
	}
	if _, err := os.Stat(filepath.Join(cacheDir, helmRepositoryCacheDirectory, cacheKey(repository), "index.yaml")); err != nil {
		t.Fatalf("index cache file error = %v", err)
	}

	restarted := New(5 * time.Second)
	restarted.SetCacheDir(cacheDir)
	if _, err := restarted.Charts(context.Background(), repository); err != nil {
		t.Fatalf("restarted Charts() error = %v", err)
	}
	if indexRequests.Load() != 1 {
		t.Fatalf("restarted client fetched the remote index again: requests = %d", indexRequests.Load())
	}

	restarted.Invalidate(repository)
	if _, err := restarted.Charts(context.Background(), repository); err != nil {
		t.Fatalf("Charts() after invalidate error = %v", err)
	}
	if indexRequests.Load() != 2 {
		t.Fatalf("invalidated client index requests = %d", indexRequests.Load())
	}
}

func TestIndexCacheObservesRefreshFromAnotherReplica(t *testing.T) {
	t.Parallel()
	var indexRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.yaml" {
			http.NotFound(w, r)
			return
		}
		count := indexRequests.Add(1)
		_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.%d
      urls:
        - /demo-1.0.%d.tgz
generated: "2026-11-01T00:00:00Z"
`, count, count)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	replicaA := New(5 * time.Second)
	replicaA.SetCacheDir(cacheDir)
	replicaB := New(5 * time.Second)
	replicaB.SetCacheDir(cacheDir)
	if _, err := replicaA.Charts(context.Background(), repository); err != nil {
		t.Fatalf("replica A Charts() error = %v", err)
	}
	if _, err := replicaB.Charts(context.Background(), repository); err != nil {
		t.Fatalf("replica B Charts() error = %v", err)
	}
	replicaA.Invalidate(repository)
	if _, err := replicaA.Charts(context.Background(), repository); err != nil {
		t.Fatalf("replica A refresh error = %v", err)
	}
	charts, err := replicaB.Charts(context.Background(), repository)
	if err != nil {
		t.Fatalf("replica B refreshed Charts() error = %v", err)
	}
	if indexRequests.Load() != 2 {
		t.Fatalf("replica B did not load the shared refreshed cache: requests = %d", indexRequests.Load())
	}
	if len(charts) != 1 || charts[0].Version != "1.0.2" {
		t.Fatalf("replica B chart version = %#v", charts)
	}
}

func TestDownloadSupportsAbsoluteHTTPSPackageURLWithoutForwardingCredentials(t *testing.T) {
	t.Parallel()
	archive := []byte("chart-archive")
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var packageAuthorization string
	packageServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		packageAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write(archive)
	}))
	defer packageServer.Close()
	indexServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      digest: %s
      urls:
        - %s/demo-1.0.0.tgz
generated: "2026-11-01T00:00:00Z"
`, digest, packageServer.URL)
	}))
	defer indexServer.Close()

	repository := domain.HelmRepository{
		ID: "repo-1", Name: "Demo", URL: indexServer.URL, Enabled: true, InsecureSkipTLS: true,
		AuthType: domain.HelmRepositoryAuthBearer, Credentials: domain.HelmRepositoryCredentials{Token: "repository-token"},
	}
	body, actual, err := New(5*time.Second).Download(context.Background(), repository, "demo", "1.0.0", digest)
	if err != nil {
		t.Fatalf("Download() absolute package URL error = %v", err)
	}
	if string(body) != string(archive) || actual != digest {
		t.Fatalf("Download() = (%q, %q)", body, actual)
	}
	if packageAuthorization != "" {
		t.Fatalf("external package received authorization %q", packageAuthorization)
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

func TestChartDetailCacheReusesPackageUntilInvalidated(t *testing.T) {
	t.Parallel()
	archive := testChartArchive(t)
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var indexRequests atomic.Int32
	var packageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			indexRequests.Add(1)
			_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      description: Demo chart
      digest: %s
      urls:
        - /demo-1.0.0.tgz
generated: "2026-11-01T00:00:00Z"
`, digest)
		case "/demo-1.0.0.tgz":
			packageRequests.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(5 * time.Second)
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	for attempt := 0; attempt < 2; attempt++ {
		detail, err := client.Detail(context.Background(), repository, "demo")
		if err != nil {
			t.Fatalf("Detail() attempt %d error = %v", attempt, err)
		}
		if detail.Readme != "# Demo\n" || !strings.Contains(detail.Values, "replicaCount: 2") {
			t.Fatalf("Detail() attempt %d content = %#v", attempt, detail)
		}
	}
	if indexRequests.Load() != 1 || packageRequests.Load() != 1 {
		t.Fatalf("cache requests = index %d, package %d", indexRequests.Load(), packageRequests.Load())
	}

	client.Invalidate(repository)
	if _, err := client.Detail(context.Background(), repository, "demo"); err != nil {
		t.Fatalf("Detail() after invalidate error = %v", err)
	}
	if indexRequests.Load() != 2 || packageRequests.Load() != 2 {
		t.Fatalf("invalidated requests = index %d, package %d", indexRequests.Load(), packageRequests.Load())
	}
}

func TestChartPackageCacheSurvivesClientRestartAndRejectsTampering(t *testing.T) {
	t.Parallel()
	archive := []byte("chart-archive")
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var packageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      digest: %s
      urls:
        - /demo-1.0.0.tgz
generated: "2026-11-01T00:00:00Z"
`, digest)
		case "/demo-1.0.0.tgz":
			packageRequests.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	first := New(5 * time.Second)
	first.SetCacheDir(cacheDir)
	if body, actual, err := first.Download(context.Background(), repository, "demo", "1.0.0", digest); err != nil || string(body) != string(archive) || actual != digest {
		t.Fatalf("first Download() = (%q, %q, %v)", body, actual, err)
	}
	if packageRequests.Load() != 1 {
		t.Fatalf("first package requests = %d", packageRequests.Load())
	}
	cachePath := first.chartPackageCachePath(repository, "demo", "1.0.0")
	if info, err := os.Stat(cachePath); err != nil || info.Size() != int64(len(archive)) {
		t.Fatalf("Chart package cache stat = (%#v, %v)", info, err)
	}

	restarted := New(5 * time.Second)
	restarted.SetCacheDir(cacheDir)
	if body, actual, err := restarted.Download(context.Background(), repository, "demo", "1.0.0", digest); err != nil || string(body) != string(archive) || actual != digest {
		t.Fatalf("restarted Download() = (%q, %q, %v)", body, actual, err)
	}
	if packageRequests.Load() != 1 {
		t.Fatalf("restarted client downloaded the package again: requests = %d", packageRequests.Load())
	}

	if err := os.WriteFile(cachePath, []byte("tampered"), 0o640); err != nil {
		t.Fatalf("tamper cache file: %v", err)
	}
	if body, actual, err := restarted.Download(context.Background(), repository, "demo", "1.0.0", digest); err != nil || string(body) != string(archive) || actual != digest {
		t.Fatalf("Download() after tamper = (%q, %q, %v)", body, actual, err)
	}
	if packageRequests.Load() != 2 {
		t.Fatalf("tampered cache was not replaced: requests = %d", packageRequests.Load())
	}
}

func TestRefreshChartForcesPackageDownloadAndDetailRefresh(t *testing.T) {
	t.Parallel()
	archive := testChartArchive(t)
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var indexRequests atomic.Int32
	var packageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			indexRequests.Add(1)
			_, _ = fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
    - apiVersion: v2
      name: demo
      version: 1.0.0
      description: Demo chart
      digest: %s
      urls:
        - /demo-1.0.0.tgz
generated: "2026-11-01T00:00:00Z"
`, digest)
		case "/demo-1.0.0.tgz":
			packageRequests.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(5 * time.Second)
	client.SetCacheDir(t.TempDir())
	repository := domain.HelmRepository{ID: "repo-1", Name: "Demo", URL: server.URL, Enabled: true, AuthType: domain.HelmRepositoryAuthAnonymous}
	if _, err := client.Detail(context.Background(), repository, "demo"); err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if indexRequests.Load() != 1 || packageRequests.Load() != 1 {
		t.Fatalf("initial requests = index %d, package %d", indexRequests.Load(), packageRequests.Load())
	}
	if _, err := client.Detail(context.Background(), repository, "demo"); err != nil {
		t.Fatalf("cached Detail() error = %v", err)
	}
	if packageRequests.Load() != 1 {
		t.Fatalf("cached detail downloaded package again: requests = %d", packageRequests.Load())
	}
	detail, err := client.RefreshChart(context.Background(), repository, "demo")
	if err != nil {
		t.Fatalf("RefreshChart() error = %v", err)
	}
	if detail.Readme != "# Demo\n" || !strings.Contains(detail.Values, "replicaCount: 2") {
		t.Fatalf("RefreshChart() detail = %#v", detail)
	}
	if indexRequests.Load() != 1 || packageRequests.Load() != 2 {
		t.Fatalf("refresh requests = index %d, package %d", indexRequests.Load(), packageRequests.Load())
	}
}

func testChartArchive(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		"demo/Chart.yaml":  "apiVersion: v2\nname: demo\nversion: 1.0.0\ndescription: Demo chart\n",
		"demo/values.yaml": "replicaCount: 2\n",
		"demo/README.md":   "# Demo\n",
	}
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, body := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if _, err := tarWriter.Write([]byte(body)); err != nil {
			t.Fatalf("tar write error = %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar close error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}
	return buffer.Bytes()
}
