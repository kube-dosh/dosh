package helmrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	helmRepositoryCacheDirectory = "cache/helm-repositories"
	helmChartCacheDirectory      = "cache/helm-charts"
)

func (c *Client) indexCachePath(repository domain.HelmRepository) string {
	return filepath.Join(c.cacheDir, helmRepositoryCacheDirectory, cacheKey(repository), "index.yaml")
}

func (c *Client) indexCacheCurrent(repository domain.HelmRepository, cached cachedIndex) bool {
	if c.cacheDir == "" || !cached.persisted {
		return true
	}
	info, err := os.Stat(c.indexCachePath(repository))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.ModTime().Equal(cached.modTime) && info.Size() == cached.size
}

func (c *Client) loadIndexCache(repository domain.HelmRepository) (cachedIndex, error) {
	if c.cacheDir == "" {
		return cachedIndex{}, os.ErrNotExist
	}
	path := c.indexCachePath(repository)
	file, err := os.Open(path)
	if err != nil {
		return cachedIndex{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return cachedIndex{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxIndexBytes {
		return cachedIndex{}, fmt.Errorf("invalid Helm repository cache file")
	}
	body, err := readBounded(file, maxIndexBytes)
	if err != nil {
		return cachedIndex{}, err
	}
	var index repo.IndexFile
	if err := yaml.Unmarshal(body, &index); err != nil {
		return cachedIndex{}, err
	}
	if index.APIVersion == "" {
		return cachedIndex{}, fmt.Errorf("Helm repository cache has no apiVersion")
	}
	if index.Entries == nil {
		index.Entries = map[string]repo.ChartVersions{}
	}
	return cachedIndex{
		index: &index, modTime: info.ModTime(), size: info.Size(), persisted: true,
	}, nil
}

func (c *Client) storeIndexCache(repository domain.HelmRepository, body []byte, index *repo.IndexFile) (cachedIndex, error) {
	if c.cacheDir == "" {
		return cachedIndex{}, errors.New("Helm repository cache directory is not configured")
	}
	path := c.indexCachePath(repository)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return cachedIndex{}, err
	}
	temporary, err := os.CreateTemp(directory, ".index-*.tmp")
	if err != nil {
		return cachedIndex{}, err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o640); err != nil {
		cleanup()
		return cachedIndex{}, err
	}
	if _, err := temporary.Write(body); err != nil {
		cleanup()
		return cachedIndex{}, err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return cachedIndex{}, err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return cachedIndex{}, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return cachedIndex{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return cachedIndex{}, err
	}
	return cachedIndex{
		index: index, modTime: info.ModTime(), size: info.Size(), persisted: true,
	}, nil
}

func cachePathComponent(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (c *Client) chartPackageCachePath(repository domain.HelmRepository, chartName, version string) string {
	return filepath.Join(
		c.cacheDir,
		helmChartCacheDirectory,
		cacheKey(repository),
		cachePathComponent(chartName),
		cachePathComponent(version)+".tgz",
	)
}

// loadChartPackage returns a locally cached Chart package. A cache entry that
// no longer matches the caller's reviewed digest is removed so it cannot be
// used for an install plan or release operation.
func (c *Client) loadChartPackage(repository domain.HelmRepository, chartName, version, expectedDigest string) ([]byte, string, bool) {
	if c.cacheDir == "" {
		return nil, "", false
	}
	path := c.chartPackageCachePath(repository, chartName, version)
	file, err := os.Open(path)
	if err != nil {
		return nil, "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxChartBytes {
		_ = os.Remove(path)
		return nil, "", false
	}
	body, err := readBounded(file, maxChartBytes)
	if err != nil {
		_ = os.Remove(path)
		return nil, "", false
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if strings.TrimSpace(expectedDigest) != "" && !digestsEqual(expectedDigest, digest) {
		_ = os.Remove(path)
		return nil, "", false
	}
	return body, digest, true
}

func (c *Client) storeChartPackage(repository domain.HelmRepository, chartName, version string, body []byte) error {
	if c.cacheDir == "" {
		return errors.New("Helm Chart cache directory is not configured")
	}
	path := c.chartPackageCachePath(repository, chartName, version)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".chart-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o640); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

func (c *Client) removeChartPackage(repository domain.HelmRepository, chartName, version string) {
	if c.cacheDir == "" {
		return
	}
	_ = os.Remove(c.chartPackageCachePath(repository, chartName, version))
}

func (c *Client) invalidateChartDetailCache(repository domain.HelmRepository, chartName string) {
	prefix := cacheKey(repository) + "\x00" + chartName + "\x00"
	c.mu.Lock()
	defer c.mu.Unlock()
	for detailKey := range c.detailCache {
		if strings.HasPrefix(detailKey, prefix) {
			delete(c.detailCache, detailKey)
		}
	}
}
