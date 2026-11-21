package helmrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmregistry "helm.sh/helm/v3/pkg/registry"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
	"sigs.k8s.io/yaml"
)

type ociTarget struct {
	Host       string
	Repository string
	ChartName  string
}

func isOCI(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "oci://")
}

func parseOCITarget(raw string) (ociTarget, error) {
	raw = strings.TrimSpace(raw)
	if !isOCI(raw) {
		return ociTarget{}, fmt.Errorf("%w: OCI repository must use oci://", domain.ErrInvalid)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ociTarget{}, fmt.Errorf("%w: OCI repository URL is invalid", domain.ErrInvalid)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ociTarget{}, fmt.Errorf("%w: OCI repository URL must not contain credentials, a query or a fragment", domain.ErrInvalid)
	}
	cleanPath := strings.Trim(parsed.Path, "/")
	if cleanPath == "" || strings.ContainsAny(cleanPath, " \t\r\n@") {
		return ociTarget{}, fmt.Errorf("%w: OCI repository URL must point to one Chart path", domain.ErrInvalid)
	}
	chartName := path.Base(cleanPath)
	if chartName == "." || chartName == "/" || strings.Contains(chartName, ":") {
		return ociTarget{}, fmt.Errorf("%w: OCI repository URL must not include a tag", domain.ErrInvalid)
	}
	return ociTarget{
		Host:       parsed.Host,
		Repository: parsed.Host + "/" + cleanPath,
		ChartName:  chartName,
	}, nil
}

func (c *Client) ociClient(repository domain.HelmRepository) (*helmregistry.Client, ociTarget, error) {
	target, err := parseOCITarget(repository.URL)
	if err != nil {
		return nil, ociTarget{}, err
	}
	tlsConfig, err := tlsConfigForRepository(repository)
	if err != nil {
		return nil, ociTarget{}, err
	}
	httpClient := &http.Client{
		Timeout: c.timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       tlsConfig,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: c.timeout,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if request.URL.Scheme != "https" {
				return fmt.Errorf("OCI registry redirect must use HTTPS")
			}
			return nil
		},
	}
	authorizer := orasauth.Client{Client: httpClient, Cache: orasauth.NewCache()}
	authorizer.SetUserAgent("dosh/0.1")
	authorizer.Credential = orasauth.StaticCredential(target.Host, ociCredential(repository))
	client, err := helmregistry.NewClient(
		helmregistry.ClientOptAuthorizer(authorizer),
		helmregistry.ClientOptHTTPClient(httpClient),
		helmregistry.ClientOptEnableCache(true),
		helmregistry.ClientOptWriter(io.Discard),
		helmregistry.ClientOptCredentialsFile(ociCredentialsFile()),
	)
	if err != nil {
		return nil, ociTarget{}, fmt.Errorf("%w: initialize OCI registry client: %v", domain.ErrUnavailable, sanitizeError(err))
	}
	return client, target, nil
}

// ociCredentialsFile returns a request-local path for Helm's credential store.
// Dosh injects credentials through oras.StaticCredential and does not rely on
// this store; a unique path avoids concurrent clients sharing one process-wide
// temporary file.
func ociCredentialsFile() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("dosh-helm-registry-%d-%d.json", os.Getpid(), time.Now().UnixNano()))
}

func ociCredential(repository domain.HelmRepository) orasauth.Credential {
	switch repository.AuthType {
	case domain.HelmRepositoryAuthBasic:
		return orasauth.Credential{Username: repository.Credentials.Username, Password: repository.Credentials.Password}
	case domain.HelmRepositoryAuthBearer:
		return orasauth.Credential{AccessToken: repository.Credentials.Token}
	default:
		return orasauth.Credential{}
	}
}

func (c *Client) ociTags(ctx context.Context, repository domain.HelmRepository) ([]string, ociTarget, *helmregistry.Client, error) {
	client, target, err := c.ociClient(repository)
	if err != nil {
		return nil, ociTarget{}, nil, err
	}
	tags, err := client.Tags(target.Repository)
	if err != nil {
		return nil, ociTarget{}, nil, fmt.Errorf("%w: list OCI Chart tags: %v", domain.ErrUnavailable, sanitizeError(err))
	}
	if len(tags) == 0 {
		return nil, target, client, fmt.Errorf("%w: OCI Chart %s has no semver tags", domain.ErrNotFound, target.Repository)
	}
	return tags, target, client, nil
}

func (c *Client) pullOCIChart(repository domain.HelmRepository, client *helmregistry.Client, target ociTarget, version string) ([]byte, string, *chart.Chart, error) {
	result, err := client.Pull("oci://" + target.Repository + ":" + version)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: pull OCI Chart %s:%s: %v", domain.ErrUnavailable, target.Repository, version, sanitizeError(err))
	}
	if result == nil || result.Chart == nil || len(result.Chart.Data) == 0 {
		return nil, "", nil, fmt.Errorf("%w: OCI Chart package is empty", domain.ErrUnavailable)
	}
	if result.Chart.Size > maxChartBytes {
		return nil, "", nil, fmt.Errorf("%w: OCI Chart package exceeds %d bytes", domain.ErrInvalid, maxChartBytes)
	}
	data := result.Chart.Data
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	loaded, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: load OCI Chart package: %v", domain.ErrInvalid, err)
	}
	if loaded.Metadata == nil || loaded.Metadata.Name != target.ChartName || loaded.Metadata.Version != version {
		return nil, "", nil, fmt.Errorf("%w: OCI Chart identity does not match %s:%s", domain.ErrConflict, target.ChartName, version)
	}
	return data, digest, loaded, nil
}

func (c *Client) listOCICharts(ctx context.Context, repository domain.HelmRepository) ([]domain.HelmChartSummary, error) {
	tags, target, client, err := c.ociTags(ctx, repository)
	if err != nil {
		return nil, err
	}
	_, digest, ch, err := c.pullOCIChart(repository, client, target, tags[0])
	if err != nil {
		return nil, err
	}
	return []domain.HelmChartSummary{ociChartSummary(repository, ch.Metadata, digest)}, nil
}

func (c *Client) ociChartDetail(ctx context.Context, repository domain.HelmRepository, chartName string) (domain.HelmChartDetail, error) {
	tags, target, client, err := c.ociTags(ctx, repository)
	if err != nil {
		return domain.HelmChartDetail{}, err
	}
	if target.ChartName != chartName {
		return domain.HelmChartDetail{}, fmt.Errorf("%w: OCI Chart %s", domain.ErrNotFound, chartName)
	}
	detail := domain.HelmChartDetail{
		HelmChartSummary: domain.HelmChartSummary{
			RepositoryID: repository.ID, RepositoryName: repository.Name,
			Name: target.ChartName, Version: tags[0],
		},
		Values:   "{}",
		Versions: make([]domain.HelmChartVersion, 0, len(tags)),
	}
	for _, tag := range tags {
		detail.Versions = append(detail.Versions, domain.HelmChartVersion{
			Version: tag, URLs: []string{"oci://" + target.Repository + ":" + tag},
		})
	}
	_, digest, ch, pullErr := c.pullOCIChart(repository, client, target, tags[0])
	if pullErr != nil {
		detail.Warnings = append(detail.Warnings, "最新 OCI Chart 暂时无法读取，README、默认 values 与 schema 不可用："+pullErr.Error())
		return detail, nil
	}
	detail.HelmChartSummary = ociChartSummary(repository, ch.Metadata, digest)
	detail.Home = ch.Metadata.Home
	detail.Sources = append([]string(nil), ch.Metadata.Sources...)
	for _, maintainer := range ch.Metadata.Maintainers {
		if maintainer == nil {
			continue
		}
		detail.Maintainers = append(detail.Maintainers, domain.HelmChartMaintainer{Name: maintainer.Name, Email: maintainer.Email, URL: maintainer.URL})
	}
	for _, dependency := range ch.Metadata.Dependencies {
		if dependency == nil {
			continue
		}
		detail.Dependencies = append(detail.Dependencies, domain.HelmChartDependency{
			Name: dependency.Name, Version: dependency.Version, Repository: dependency.Repository,
			Condition: dependency.Condition, Alias: dependency.Alias,
		})
	}
	if len(ch.Schema) > 0 {
		detail.Schema = string(ch.Schema)
	}
	for _, file := range ch.Raw {
		switch strings.ToLower(filepath.Base(file.Name)) {
		case "readme.md":
			detail.Readme = string(file.Data)
		case "values.yaml":
			detail.Values = string(file.Data)
		}
	}
	if detail.Values == "{}" && len(ch.Values) > 0 {
		if encoded, marshalErr := yaml.Marshal(ch.Values); marshalErr == nil {
			detail.Values = string(encoded)
		}
	}
	if len(detail.Versions) > 0 {
		detail.Versions[0].Digest = digest
		detail.Versions[0].AppVersion = ch.Metadata.AppVersion
		detail.Versions[0].Deprecated = ch.Metadata.Deprecated
	}
	return detail, nil
}

func (c *Client) downloadOCI(ctx context.Context, repository domain.HelmRepository, chartName, version, expectedDigest string) ([]byte, string, error) {
	tags, target, client, err := c.ociTags(ctx, repository)
	if err != nil {
		return nil, "", err
	}
	if target.ChartName != chartName {
		return nil, "", fmt.Errorf("%w: OCI Chart %s", domain.ErrNotFound, chartName)
	}
	found := false
	for _, tag := range tags {
		if tag == version {
			found = true
			break
		}
	}
	if !found {
		return nil, "", fmt.Errorf("%w: OCI Chart %s version %s", domain.ErrNotFound, chartName, version)
	}
	data, digest, _, err := c.pullOCIChart(repository, client, target, version)
	if err != nil {
		return nil, "", err
	}
	if expectedDigest != "" && !digestsEqual(expectedDigest, digest) {
		return nil, "", fmt.Errorf("%w: selected OCI Chart digest no longer matches the registry", domain.ErrConflict)
	}
	return data, digest, nil
}

func ociChartSummary(repository domain.HelmRepository, meta *chart.Metadata, digest string) domain.HelmChartSummary {
	summary := domain.HelmChartSummary{
		RepositoryID: repository.ID, RepositoryName: repository.Name,
		Name: meta.Name, Version: meta.Version, AppVersion: meta.AppVersion,
		Description: meta.Description, Icon: meta.Icon, Deprecated: meta.Deprecated,
		Digest: digest, Keywords: append([]string(nil), meta.Keywords...),
	}
	return summary
}
