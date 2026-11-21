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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"

	"github.com/Masterminds/semver/v3"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	maxIndexBytes = 16 << 20
	maxChartBytes = 64 << 20
	indexCacheTTL = 45 * time.Second
)

type cachedIndex struct {
	index     *repo.IndexFile
	fetchedAt time.Time
}

// Client fetches administrator-approved Helm repository indexes and chart
// packages. It never accepts a user-supplied URL: every request originates from
// a persisted repository configuration and the package URL must remain on the
// configured repository host.
type Client struct {
	timeout time.Duration
	mu      sync.Mutex
	cache   map[string]cachedIndex
}

func New(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{timeout: timeout, cache: map[string]cachedIndex{}}
}

func (c *Client) Index(ctx context.Context, repository domain.HelmRepository) (*repo.IndexFile, error) {
	key := cacheKey(repository)
	c.mu.Lock()
	cached, ok := c.cache[key]
	c.mu.Unlock()
	if ok && time.Since(cached.fetchedAt) < indexCacheTTL {
		return cached.index, nil
	}

	client, err := c.httpClient(repository)
	if err != nil {
		return nil, err
	}
	target, err := indexURL(repository.URL)
	if err != nil {
		return nil, err
	}
	response, err := c.get(ctx, client, repository, target)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: Helm repository returned HTTP %d", domain.ErrUnavailable, response.StatusCode)
	}
	body, err := readBounded(response.Body, maxIndexBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: read Helm repository index: %v", domain.ErrUnavailable, err)
	}
	var index repo.IndexFile
	if err := yaml.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("%w: decode Helm repository index: %v", domain.ErrUnavailable, err)
	}
	if strings.TrimSpace(index.APIVersion) == "" {
		return nil, fmt.Errorf("%w: Helm repository index has no apiVersion", domain.ErrUnavailable)
	}
	if index.Entries == nil {
		index.Entries = map[string]repo.ChartVersions{}
	}
	c.mu.Lock()
	c.cache[key] = cachedIndex{index: &index, fetchedAt: time.Now()}
	c.mu.Unlock()
	return &index, nil
}

func (c *Client) Download(ctx context.Context, repository domain.HelmRepository, chartName, version, expectedDigest string) ([]byte, string, error) {
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

// Charts returns the latest visible version of each chart in a repository.
func (c *Client) Charts(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	index, err := c.Index(ctx, repository)
	if err != nil {
		return nil, err
	}
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
	return items, nil
}

// Detail returns index metadata plus the README, default values and schema
// embedded in the latest chart package. It never returns package bytes.
func (c *Client) Detail(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
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
	data, _, downloadErr := c.Download(ctx, repository, chartName, latest.Version, latest.Digest)
	if downloadErr != nil {
		detail.Warnings = append(detail.Warnings, "最新 Chart 包暂时无法读取，README、默认 values 与 schema 不可用："+downloadErr.Error())
		return detail, nil
	}
	ch, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		detail.Warnings = append(detail.Warnings, "Chart 包无法解析，README、默认 values 与 schema 不可用")
		return detail, nil
	}
	detail.Values = "{}"
	if ch.Schema != nil {
		detail.Schema = string(ch.Schema)
	}
	for _, file := range ch.Raw {
		base := strings.ToLower(filepath.Base(file.Name))
		switch base {
		case "readme.md":
			detail.Readme = string(file.Data)
		case "values.yaml":
			detail.Values = string(file.Data)
		}
	}
	if detail.Values == "{}" && len(ch.Values) > 0 {
		encoded, marshalErr := yaml.Marshal(ch.Values)
		if marshalErr == nil {
			detail.Values = string(encoded)
		} else {
			detail.Warnings = append(detail.Warnings, "默认 values 无法序列化")
		}
	}
	return detail, nil
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
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       tlsConfig,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: c.timeout,
	}
	origin, _ := url.Parse(repository.URL)
	return &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if origin != nil && !strings.EqualFold(request.URL.Host, origin.Host) {
				return fmt.Errorf("redirect to a different host is not allowed")
			}
			if request.URL.Scheme != "https" && !(origin != nil && origin.Scheme == "http") {
				return fmt.Errorf("redirect to an insecure scheme is not allowed")
			}
			return nil
		},
	}, nil
}

func (c *Client) get(ctx context.Context, client *http.Client, repository domain.HelmRepository, target string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: Helm repository request URL is invalid", domain.ErrInvalid)
	}
	request.Header.Set("Accept", "application/yaml, text/yaml, application/x-yaml, application/octet-stream")
	request.Header.Set("User-Agent", "dosh/0.1")
	if token := strings.TrimSpace(repository.Credentials.Token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	} else if username := strings.TrimSpace(repository.Credentials.Username); username != "" {
		request.SetBasicAuth(username, repository.Credentials.Password)
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
	if !strings.EqualFold(resolved.Host, base.Host) {
		return "", fmt.Errorf("%w: chart package URL must use the repository host", domain.ErrInvalid)
	}
	resolved.User = nil
	resolved.Fragment = ""
	return resolved.String(), nil
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
