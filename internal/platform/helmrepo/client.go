package helmrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sync/singleflight"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	maxIndexBytes              = 32 << 20
	maxChartBytes              = 64 << 20
	detailCacheTTL             = 10 * time.Minute
	maxChartDetailCacheEntries = 64
	defaultCacheLimitBytes     = 2 << 30
)

type cachedIndex struct {
	index        *repo.IndexFile
	modTime      time.Time
	size         int64
	etag         string
	lastModified string
	persisted    bool
}

type chartDetailContent struct {
	readme   string
	values   string
	schema   string
	warnings []string
}

type cachedChartDetail struct {
	content   chartDetailContent
	fetchedAt time.Time
}

// Client fetches administrator-approved Helm repository indexes and chart
// packages. It never accepts a user-supplied URL: every request originates from
// a persisted repository configuration or an absolute package URL published by
// that repository's index. Repository credentials are only sent to the
// configured repository host.
type Client struct {
	timeout       time.Duration
	egress        *egress.Factory
	mu            sync.Mutex
	cache         map[string]cachedIndex
	detailCache   map[string]cachedChartDetail
	cacheDir      string
	cacheLimit    int64
	indexFlight   singleflight.Group
	detailFlight  singleflight.Group
	packageFlight singleflight.Group
}

type cachedChartPackage struct {
	data   []byte
	digest string
}

func New(timeout time.Duration, factories ...*egress.Factory) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	factory := egress.NewDirect()
	if len(factories) > 0 && factories[0] != nil {
		factory = factories[0]
	}
	return &Client{
		timeout: timeout, egress: factory,
		cache: map[string]cachedIndex{}, detailCache: map[string]cachedChartDetail{}, cacheLimit: defaultCacheLimitBytes,
	}
}

// SetCacheDir enables the shared on-disk repository index and Chart package
// cache. The directory is normally the PVC mounted by the deployment. Cache
// failures are best-effort and never prevent a live repository fetch from
// succeeding.
func (c *Client) SetCacheDir(dir string) {
	c.cacheDir = strings.TrimSpace(dir)
}

func (c *Client) SetCacheLimit(maxBytes int64) {
	if maxBytes > 0 {
		c.cacheLimit = maxBytes
	}
}

func (c *Client) Index(ctx context.Context, repository domain.HelmRepository) (*repo.IndexFile, error) {
	if isOCI(repository.URL) {
		return nil, fmt.Errorf("%w: OCI Chart repositories do not expose index.yaml", domain.ErrUnsupported)
	}
	key := cacheKey(repository)
	c.mu.Lock()
	cached, ok := c.cache[key]
	c.mu.Unlock()
	if ok && c.indexCacheCurrent(repository, cached) {
		return cached.index, nil
	}
	result, err, _ := c.indexFlight.Do(key, func() (any, error) {
		c.mu.Lock()
		cached, ok := c.cache[key]
		c.mu.Unlock()
		if ok && c.indexCacheCurrent(repository, cached) {
			return cached.index, nil
		}
		if diskCache, diskErr := c.loadIndexCache(repository); diskErr == nil {
			c.mu.Lock()
			c.cache[key] = diskCache
			c.mu.Unlock()
			return diskCache.index, nil
		}
		index, fetchErr := c.fetchIndex(ctx, repository)
		if fetchErr != nil {
			return nil, fetchErr
		}
		c.mu.Lock()
		c.cache[key] = index
		c.mu.Unlock()
		return index.index, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*repo.IndexFile), nil
}

// Refresh performs a network-backed synchronization while still using HTTP
// validators when the repository supports them. A 304 response reuses the
// persisted index instead of downloading it again.
func (c *Client) Refresh(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	if isOCI(repository.URL) {
		if err := c.refreshOCITags(ctx, repository); err != nil {
			return nil, err
		}
		return c.Charts(ctx, repository)
	}
	key := cacheKey(repository)
	c.mu.Lock()
	delete(c.cache, key)
	c.mu.Unlock()
	cached, err := c.fetchIndex(ctx, repository)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[key] = cached
	c.mu.Unlock()
	return chartSummaries(repository, cached.index), nil
}

// Invalidate clears the in-memory and shared on-disk repository cache. The
// index itself has no time-based expiry: an administrator-triggered repository
// sync is the explicit refresh boundary.
func (c *Client) Invalidate(repository domain.HelmRepository) {
	key := cacheKey(repository)
	prefix := key + "\x00"
	c.mu.Lock()
	delete(c.cache, key)
	for detailKey := range c.detailCache {
		if strings.HasPrefix(detailKey, prefix) {
			delete(c.detailCache, detailKey)
		}
	}
	c.mu.Unlock()
	if c.cacheDir != "" {
		_ = os.Remove(c.indexCachePath(repository))
	}
}

func (c *Client) fetchIndex(ctx context.Context, repository domain.HelmRepository) (cachedIndex, error) {
	client, err := c.httpClient(repository)
	if err != nil {
		return cachedIndex{}, err
	}
	target, err := indexURL(repository.URL)
	if err != nil {
		return cachedIndex{}, err
	}
	metadata, _ := c.loadIndexCacheMetadata(repository)
	headers := make(map[string]string, 2)
	if metadata.ETag != "" {
		headers["If-None-Match"] = metadata.ETag
	}
	if metadata.LastModified != "" {
		headers["If-Modified-Since"] = metadata.LastModified
	}
	response, err := c.getWithHeaders(ctx, client, repository, target, headers)
	if err != nil {
		return cachedIndex{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		cached, cacheErr := c.loadIndexCache(repository)
		if cacheErr != nil {
			return cachedIndex{}, fmt.Errorf("%w: repository returned 304 but no valid cached index exists", domain.ErrUnavailable)
		}
		metadata.FetchedAt = time.Now().UTC()
		if response.Header.Get("ETag") != "" {
			metadata.ETag = response.Header.Get("ETag")
		}
		if response.Header.Get("Last-Modified") != "" {
			metadata.LastModified = response.Header.Get("Last-Modified")
		}
		_ = c.storeIndexCacheMetadata(repository, metadata)
		cached.etag = metadata.ETag
		cached.lastModified = metadata.LastModified
		return cached, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return cachedIndex{}, fmt.Errorf("%w: Helm repository returned HTTP %d", domain.ErrUnavailable, response.StatusCode)
	}
	body, err := readBounded(response.Body, maxIndexBytes)
	if err != nil {
		return cachedIndex{}, fmt.Errorf("%w: read Helm repository index: %v", domain.ErrUnavailable, err)
	}
	var index repo.IndexFile
	if err := yaml.Unmarshal(body, &index); err != nil {
		return cachedIndex{}, fmt.Errorf("%w: decode Helm repository index: %v", domain.ErrUnavailable, err)
	}
	if strings.TrimSpace(index.APIVersion) == "" {
		return cachedIndex{}, fmt.Errorf("%w: Helm repository index has no apiVersion", domain.ErrUnavailable)
	}
	if index.Entries == nil {
		index.Entries = map[string]repo.ChartVersions{}
	}
	cached := cachedIndex{index: &index}
	if stored, storeErr := c.storeIndexCache(repository, body, &index); storeErr == nil {
		cached = stored
	}
	metadata = indexCacheMetadata{
		ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), FetchedAt: time.Now().UTC(),
	}
	_ = c.storeIndexCacheMetadata(repository, metadata)
	cached.etag = metadata.ETag
	cached.lastModified = metadata.LastModified
	return cached, nil
}

func (c *Client) Download(ctx context.Context, repository domain.HelmRepository, chartName, version, expectedDigest string) ([]byte, string, error) {
	if data, digest, ok := c.loadChartPackage(repository, chartName, version, expectedDigest); ok {
		return data, digest, nil
	}
	key := strings.Join([]string{cacheKey(repository), chartName, version, strings.ToLower(strings.TrimSpace(expectedDigest))}, "\x00")
	result, err, _ := c.packageFlight.Do(key, func() (any, error) {
		if data, digest, ok := c.loadChartPackage(repository, chartName, version, expectedDigest); ok {
			return cachedChartPackage{data: data, digest: digest}, nil
		}
		data, digest, fetchErr := c.downloadRemote(ctx, repository, chartName, version, expectedDigest)
		if fetchErr != nil {
			return nil, fetchErr
		}
		_ = c.storeChartPackage(repository, chartName, version, data)
		return cachedChartPackage{data: data, digest: digest}, nil
	})
	if err != nil {
		return nil, "", err
	}
	pkg := result.(cachedChartPackage)
	return pkg.data, pkg.digest, nil
}

func (c *Client) downloadRemote(ctx context.Context, repository domain.HelmRepository, chartName, version, expectedDigest string) ([]byte, string, error) {
	if isOCI(repository.URL) {
		return c.downloadOCI(ctx, repository, chartName, version, expectedDigest)
	}
	index, err := c.Index(ctx, repository)
	if err != nil {
		return nil, "", err
	}
	entry, err := findVersion(index, chartName, version)
	if err != nil {
		return nil, "", err
	}
	if entry.Removed {
		return nil, "", fmt.Errorf("%w: chart version %s/%s@%s is removed", domain.ErrNotFound, chartName, version, chartName)
	}
	if len(entry.URLs) == 0 {
		return nil, "", fmt.Errorf("%w: chart version %s@%s has no download URL", domain.ErrUnavailable, chartName, version)
	}
	if isOCI(entry.URLs[0]) {
		return c.downloadOCIReference(repository, chartName, version, expectedDigest, entry.URLs[0])
	}
	target, err := packageURL(repository.URL, entry.URLs[0])
	if err != nil {
		return nil, "", err
	}
	client, err := c.httpClient(repository)
	if err != nil {
		return nil, "", err
	}
	response, err := c.get(ctx, client, repository, target)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%w: chart download returned HTTP %d", domain.ErrUnavailable, response.StatusCode)
	}
	body, err := readBounded(response.Body, maxChartBytes)
	if err != nil {
		return nil, "", fmt.Errorf("%w: read chart package: %v", domain.ErrUnavailable, err)
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	indexDigest := strings.TrimSpace(entry.Digest)
	if indexDigest == "" {
		indexDigest = strings.TrimSpace(expectedDigest)
	}
	if indexDigest != "" && !digestsEqual(indexDigest, digest) {
		return nil, "", fmt.Errorf("%w: chart package digest mismatch", domain.ErrConflict)
	}
	if expectedDigest != "" && !digestsEqual(expectedDigest, digest) && !digestsEqual(expectedDigest, indexDigest) {
		return nil, "", fmt.Errorf("%w: selected chart digest no longer matches the repository", domain.ErrConflict)
	}
	return body, digest, nil
}

// RefreshChart removes the cached package for the latest Chart version and
// downloads it again. Repository indexes remain unchanged; repository-wide
// synchronization is handled separately.
func (c *Client) RefreshChart(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
	version := ""
	if isOCI(repository.URL) {
		target, parseErr := parseOCITarget(repository.URL)
		if parseErr != nil {
			return domain.HelmChartDetail{}, parseErr
		}
		if err := c.refreshOCITags(ctx, repository); err != nil {
			return domain.HelmChartDetail{}, err
		}
		tags, ok := c.loadOCITagsCache(repository)
		if !ok || len(tags) == 0 {
			return domain.HelmChartDetail{}, fmt.Errorf("%w: OCI Chart %s has no semver tags", domain.ErrNotFound, target.Repository)
		}
		if target.ChartName != chartName {
			return domain.HelmChartDetail{}, fmt.Errorf("%w: OCI Chart %s", domain.ErrNotFound, chartName)
		}
		version = tags[0]
	} else {
		index, err := c.Index(ctx, repository)
		if err != nil {
			return domain.HelmChartDetail{}, err
		}
		versions := sortedChartVersions(index.Entries[chartName])
		if len(versions) == 0 {
			return domain.HelmChartDetail{}, fmt.Errorf("%w: chart %s", domain.ErrNotFound, chartName)
		}
		version = versions[0].Version
	}
	c.invalidateChartDetailCache(repository, chartName)
	c.removeChartPackage(repository, chartName, version)
	return c.Detail(ctx, repository, chartName)
}

// Charts returns the latest visible version of each chart in a repository.
func (c *Client) Charts(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	if isOCI(repository.URL) {
		return c.listOCICharts(ctx, repository)
	}
	index, err := c.Index(ctx, repository)
	if err != nil {
		return nil, err
	}
	return chartSummaries(repository, index), nil
}

func chartSummaries(repository domain.HelmRepository, index *repo.IndexFile) []domain.HelmChartSummary {
	items := make([]domain.HelmChartSummary, 0, len(index.Entries))
	for name, versions := range index.Entries {
		latest := latestChartVersion(versions)
		if latest == nil {
			continue
		}
		items = append(items, helmChartSummary(repository, name, latest))
	}
	sort.SliceStable(items, func(left, right int) bool {
		return strings.ToLower(items[left].Name) < strings.ToLower(items[right].Name)
	})
	return items
}

// Detail returns index metadata plus the README, default values and schema
// embedded in the latest chart package. It never returns package bytes.
func (c *Client) Detail(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
	if isOCI(repository.URL) {
		return c.ociChartDetail(ctx, repository, chartName)
	}
	index, err := c.Index(ctx, repository)
	if err != nil {
		return domain.HelmChartDetail{}, err
	}
	versions := sortedChartVersions(index.Entries[chartName])
	if len(versions) == 0 {
		return domain.HelmChartDetail{}, fmt.Errorf("%w: chart %s", domain.ErrNotFound, chartName)
	}
	latest := versions[0]
	detail := domain.HelmChartDetail{
		HelmChartSummary: helmChartSummary(repository, chartName, latest),
		Home:             latest.Home,
		Sources:          append([]string(nil), latest.Sources...),
		Maintainers:      make([]domain.HelmChartMaintainer, 0, len(latest.Maintainers)),
		Dependencies:     make([]domain.HelmChartDependency, 0, len(latest.Dependencies)),
		Versions:         make([]domain.HelmChartVersion, 0, len(versions)),
	}
	for _, maintainer := range latest.Maintainers {
		if maintainer == nil {
			continue
		}
		detail.Maintainers = append(detail.Maintainers, domain.HelmChartMaintainer{Name: maintainer.Name, Email: maintainer.Email, URL: maintainer.URL})
	}
	for _, dependency := range latest.Dependencies {
		if dependency == nil {
			continue
		}
		detail.Dependencies = append(detail.Dependencies, domain.HelmChartDependency{
			Name: dependency.Name, Version: dependency.Version, Repository: dependency.Repository,
			Condition: dependency.Condition, Alias: dependency.Alias,
		})
	}
	for _, version := range versions {
		detail.Versions = append(detail.Versions, domain.HelmChartVersion{
			Version: version.Version, AppVersion: version.AppVersion, Digest: version.Digest,
			CreatedAt: version.Created, Deprecated: version.Deprecated, URLs: append([]string(nil), version.URLs...),
		})
	}
	content, contentErr := c.chartDetailContent(ctx, repository, chartName, latest)
	if contentErr != nil {
		detail.Warnings = append(detail.Warnings, "最新 Chart 包暂时无法读取，README、默认 values 与 schema 不可用："+contentErr.Error())
		return detail, nil
	}
	detail.Readme = content.readme
	detail.Values = content.values
	detail.Schema = content.schema
	detail.Warnings = append(detail.Warnings, content.warnings...)
	return detail, nil
}

func (c *Client) chartDetailContent(ctx context.Context, repository domain.HelmRepository, chartName string, version *repo.ChartVersion) (chartDetailContent, error) {
	key := chartDetailCacheKey(repository, chartName, version)
	c.mu.Lock()
	cached, ok := c.detailCache[key]
	if ok && time.Since(cached.fetchedAt) < detailCacheTTL {
		c.mu.Unlock()
		return cached.content, nil
	}
	if ok {
		delete(c.detailCache, key)
	}
	c.mu.Unlock()

	result, err, _ := c.detailFlight.Do(key, func() (any, error) {
		content, fetchErr := c.fetchChartDetailContent(ctx, repository, chartName, version)
		if fetchErr != nil {
			return chartDetailContent{}, fetchErr
		}
		c.storeChartDetailContent(key, content)
		return content, nil
	})
	if err != nil {
		return chartDetailContent{}, err
	}
	return result.(chartDetailContent), nil
}

func (c *Client) fetchChartDetailContent(ctx context.Context, repository domain.HelmRepository, chartName string, version *repo.ChartVersion) (chartDetailContent, error) {
	data, _, err := c.Download(ctx, repository, chartName, version.Version, version.Digest)
	if err != nil {
		return chartDetailContent{}, err
	}
	ch, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return chartDetailContent{}, err
	}
	content := chartDetailContent{values: "{}"}
	if ch.Schema != nil {
		content.schema = string(ch.Schema)
	}
	for _, file := range ch.Raw {
		switch strings.ToLower(filepath.Base(file.Name)) {
		case "readme.md":
			content.readme = string(file.Data)
		case "values.yaml":
			content.values = string(file.Data)
		}
	}
	if content.values == "{}" && len(ch.Values) > 0 {
		encoded, marshalErr := yaml.Marshal(ch.Values)
		if marshalErr != nil {
			content.warnings = append(content.warnings, "默认 values 无法序列化")
		} else {
			content.values = string(encoded)
		}
	}
	return content, nil
}

func (c *Client) storeChartDetailContent(key string, content chartDetailContent) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for cachedKey, cached := range c.detailCache {
		if now.Sub(cached.fetchedAt) >= detailCacheTTL {
			delete(c.detailCache, cachedKey)
		}
	}
	if len(c.detailCache) >= maxChartDetailCacheEntries {
		var oldestKey string
		var oldestAt time.Time
		for cachedKey, cached := range c.detailCache {
			if oldestKey == "" || cached.fetchedAt.Before(oldestAt) {
				oldestKey = cachedKey
				oldestAt = cached.fetchedAt
			}
		}
		delete(c.detailCache, oldestKey)
	}
	c.detailCache[key] = cachedChartDetail{content: content, fetchedAt: now}
}

func helmChartSummary(repository domain.HelmRepository, name string, version *repo.ChartVersion) domain.HelmChartSummary {
	summary := domain.HelmChartSummary{RepositoryID: repository.ID, RepositoryName: repository.Name, Name: name}
	if version == nil {
		return summary
	}
	summary.Version = version.Version
	summary.AppVersion = version.AppVersion
	summary.Description = version.Description
	summary.Icon = version.Icon
	summary.Deprecated = version.Deprecated
	summary.Digest = version.Digest
	summary.CreatedAt = version.Created
	summary.Keywords = append([]string(nil), version.Keywords...)
	return summary
}

func latestChartVersion(versions repo.ChartVersions) *repo.ChartVersion {
	sorted := sortedChartVersions(versions)
	if len(sorted) == 0 {
		return nil
	}
	return sorted[0]
}

func sortedChartVersions(versions repo.ChartVersions) []*repo.ChartVersion {
	result := make([]*repo.ChartVersion, 0, len(versions))
	for _, version := range versions {
		if version != nil && version.Metadata != nil && !version.Removed {
			result = append(result, version)
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		leftVersion, leftErr := semver.NewVersion(result[left].Version)
		rightVersion, rightErr := semver.NewVersion(result[right].Version)
		if leftErr == nil && rightErr == nil && !leftVersion.Equal(rightVersion) {
			return leftVersion.GreaterThan(rightVersion)
		}
		if leftErr == nil && rightErr != nil {
			return true
		}
		if leftErr != nil && rightErr == nil {
			return false
		}
		return result[left].Version > result[right].Version
	})
	return result
}

func (c *Client) httpClient(repository domain.HelmRepository) (*http.Client, error) {
	tlsConfig, err := tlsConfigForRepository(repository)
	if err != nil {
		return nil, err
	}
	transport := c.egress.Transport(egress.ScopeHelm, tlsConfig)
	transport.ForceAttemptHTTP2 = true
	transport.MaxIdleConns = 20
	transport.IdleConnTimeout = 60 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = c.timeout
	origin, _ := url.Parse(repository.URL)
	return &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if request.URL.Scheme != "https" && !(origin != nil && origin.Scheme == "http") {
				return fmt.Errorf("redirect to an insecure scheme is not allowed")
			}
			return nil
		},
	}, nil
}

func tlsConfigForRepository(repository domain.HelmRepository) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: repository.InsecureSkipTLS} //nolint:gosec // explicitly administrator-controlled
	if ca := strings.TrimSpace(repository.Credentials.CAPEM); ca != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, fmt.Errorf("%w: Helm repository custom CA is not valid PEM", domain.ErrInvalid)
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}

func (c *Client) get(ctx context.Context, client *http.Client, repository domain.HelmRepository, target string) (*http.Response, error) {
	return c.getWithHeaders(ctx, client, repository, target, nil)
}

func (c *Client) getWithHeaders(ctx context.Context, client *http.Client, repository domain.HelmRepository, target string, headers map[string]string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: Helm repository request URL is invalid", domain.ErrInvalid)
	}
	request.Header.Set("Accept", "application/yaml, text/yaml, application/x-yaml, application/octet-stream")
	request.Header.Set("User-Agent", "dosh/0.1")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if sameRepositoryHost(repository.URL, target) {
		if token := strings.TrimSpace(repository.Credentials.Token); token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		} else if username := strings.TrimSpace(repository.Credentials.Username); username != "" {
			request.SetBasicAuth(username, repository.Credentials.Password)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: Helm repository request failed: %v", domain.ErrUnavailable, sanitizeError(err))
	}
	return response, nil
}

func findVersion(index *repo.IndexFile, chartName, version string) (*repo.ChartVersion, error) {
	versions, ok := index.Entries[chartName]
	if !ok {
		return nil, fmt.Errorf("%w: chart %s", domain.ErrNotFound, chartName)
	}
	for _, entry := range versions {
		if entry != nil && entry.Metadata != nil && entry.Version == version {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("%w: chart %s version %s", domain.ErrNotFound, chartName, version)
}

func indexURL(repositoryURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(repositoryURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: Helm repository URL is invalid", domain.ErrInvalid)
	}
	parsed.Path = path.Join(parsed.Path, "index.yaml")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func packageURL(repositoryURL, raw string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(repositoryURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("%w: Helm repository URL is invalid", domain.ErrInvalid)
	}
	if base.Path != "" && !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: chart package URL is invalid", domain.ErrInvalid)
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "https" && resolved.Scheme != "http" {
		return "", fmt.Errorf("%w: chart package URL must use HTTP or HTTPS", domain.ErrInvalid)
	}
	if base.Scheme == "https" && resolved.Scheme != "https" {
		return "", fmt.Errorf("%w: HTTPS chart repositories may not downgrade package downloads", domain.ErrInvalid)
	}
	if !strings.EqualFold(resolved.Host, base.Host) && resolved.Scheme != "https" {
		return "", fmt.Errorf("%w: chart package URL from another host must use HTTPS", domain.ErrInvalid)
	}
	resolved.User = nil
	resolved.Fragment = ""
	return resolved.String(), nil
}

func sameRepositoryHost(repositoryURL, target string) bool {
	repository, err := url.Parse(strings.TrimSpace(repositoryURL))
	if err != nil || repository.Host == "" {
		return false
	}
	packageTarget, err := url.Parse(strings.TrimSpace(target))
	if err != nil || packageTarget.Host == "" {
		return false
	}
	return strings.EqualFold(repository.Host, packageTarget.Host)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func cacheKey(repository domain.HelmRepository) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		repository.ID, repository.URL, repository.Credentials.Username,
		repository.Credentials.Password, repository.Credentials.Token,
		repository.Credentials.CAPEM, fmt.Sprint(repository.InsecureSkipTLS),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func chartDetailCacheKey(repository domain.HelmRepository, chartName string, version *repo.ChartVersion) string {
	return cacheKey(repository) + "\x00" + chartName + "\x00" + version.Version + "\x00" + version.Digest
}

func digestsEqual(left, right string) bool {
	left = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(left)), "sha256:")
	right = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(right)), "sha256:")
	return left != "" && left == right
}

func sanitizeError(err error) string {
	message := err.Error()
	if parsed, parseErr := url.Parse(message); parseErr == nil && parsed.User != nil {
		parsed.User = nil
		return parsed.String()
	}
	if index := strings.Index(message, "://"); index >= 0 {
		if at := strings.Index(message[index:], "@"); at >= 0 {
			message = message[:index+3] + message[index+at+1:]
		}
	}
	return message
}
