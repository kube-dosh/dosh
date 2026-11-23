package helmrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	helmRepositoryCacheDirectory = "cache/helm-repositories"
	helmChartCacheDirectory      = "cache/helm-charts"
	helmCacheMarkerFile          = ".repository.json"
	helmIndexMetadataFile        = "index.meta.json"
	helmOCITagsFile              = "oci-tags.json"
)

type repositoryCacheMarker struct {
	RepositoryID   string `json:"repositoryId"`
	RepositoryName string `json:"repositoryName"`
	URL            string `json:"url"`
}

type indexCacheMetadata struct {
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	FetchedAt    time.Time `json:"fetchedAt"`
}

type ociTagsMetadata struct {
	Tags      []string  `json:"tags"`
	FetchedAt time.Time `json:"fetchedAt"`
}

type cacheFile struct {
	path    string
	size    int64
	modTime time.Time
	index   bool
}

func (c *Client) indexCachePath(repository domain.HelmRepository) string {
	return filepath.Join(c.cacheDir, helmRepositoryCacheDirectory, cacheKey(repository), "index.yaml")
}

func (c *Client) repositoryCacheDirectory(root string, repository domain.HelmRepository) string {
	return filepath.Join(c.cacheDir, root, cacheKey(repository))
}

func (c *Client) indexMetadataPath(repository domain.HelmRepository) string {
	return filepath.Join(c.repositoryCacheDirectory(helmRepositoryCacheDirectory, repository), helmIndexMetadataFile)
}

func (c *Client) ociTagsCachePath(repository domain.HelmRepository) string {
	return filepath.Join(c.repositoryCacheDirectory(helmRepositoryCacheDirectory, repository), helmOCITagsFile)
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
	metadata, _ := c.loadIndexCacheMetadata(repository)
	return cachedIndex{
		index: &index, modTime: info.ModTime(), size: info.Size(), persisted: true,
		etag: metadata.ETag, lastModified: metadata.LastModified,
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
	c.writeRepositoryCacheMarker(helmRepositoryCacheDirectory, repository)
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
	c.writeRepositoryCacheMarker(helmChartCacheDirectory, repository)
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

func (c *Client) loadIndexCacheMetadata(repository domain.HelmRepository) (indexCacheMetadata, error) {
	if c.cacheDir == "" {
		return indexCacheMetadata{}, os.ErrNotExist
	}
	var metadata indexCacheMetadata
	if err := readJSONFile(c.indexMetadataPath(repository), &metadata); err != nil {
		return indexCacheMetadata{}, err
	}
	return metadata, nil
}

func (c *Client) storeIndexCacheMetadata(repository domain.HelmRepository, metadata indexCacheMetadata) error {
	if c.cacheDir == "" {
		return errors.New("Helm repository cache directory is not configured")
	}
	return writeJSONFile(c.indexMetadataPath(repository), metadata, 0o640)
}

func (c *Client) loadOCITagsCache(repository domain.HelmRepository) ([]string, bool) {
	if c.cacheDir == "" {
		return nil, false
	}
	var metadata ociTagsMetadata
	if err := readJSONFile(c.ociTagsCachePath(repository), &metadata); err != nil || len(metadata.Tags) == 0 {
		return nil, false
	}
	return append([]string(nil), metadata.Tags...), true
}

func (c *Client) storeOCITagsCache(repository domain.HelmRepository, tags []string) error {
	if c.cacheDir == "" {
		return errors.New("Helm repository cache directory is not configured")
	}
	c.writeRepositoryCacheMarker(helmRepositoryCacheDirectory, repository)
	return writeJSONFile(c.ociTagsCachePath(repository), ociTagsMetadata{
		Tags: append([]string(nil), tags...), FetchedAt: time.Now().UTC(),
	}, 0o640)
}

func (c *Client) writeRepositoryCacheMarker(root string, repository domain.HelmRepository) {
	if c.cacheDir == "" {
		return
	}
	directory := c.repositoryCacheDirectory(root, repository)
	_ = writeJSONFile(filepath.Join(directory, helmCacheMarkerFile), repositoryCacheMarker{
		RepositoryID: repository.ID, RepositoryName: repository.Name, URL: repository.URL,
	}, 0o640)
}

// RemoveRepositoryCache removes every persisted index, Chart package, and OCI
// tag metadata entry owned by the repository. Markers allow cleanup of cache
// directories created with older credentials or repository URLs.
func (c *Client) RemoveRepositoryCache(repository domain.HelmRepository) error {
	if c.cacheDir == "" {
		return nil
	}
	removed := map[string]bool{}
	for _, root := range []string{helmRepositoryCacheDirectory, helmChartCacheDirectory} {
		directory := c.repositoryCacheDirectory(root, repository)
		if err := os.RemoveAll(directory); err != nil {
			return err
		}
		removed[directory] = true
		entries, err := os.ReadDir(filepath.Join(c.cacheDir, root))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			candidate := filepath.Join(c.cacheDir, root, entry.Name())
			if removed[candidate] {
				continue
			}
			var marker repositoryCacheMarker
			if err := readJSONFile(filepath.Join(candidate, helmCacheMarkerFile), &marker); err != nil {
				continue
			}
			if marker.RepositoryID == repository.ID {
				if err := os.RemoveAll(candidate); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Client) CacheStats() (domain.HelmCacheStats, error) {
	stats := domain.HelmCacheStats{Root: c.cacheDir, LimitBytes: c.cacheLimit}
	if c.cacheDir == "" {
		return stats, nil
	}
	for _, root := range []string{helmRepositoryCacheDirectory, helmChartCacheDirectory} {
		base := filepath.Join(c.cacheDir, root)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, os.ErrNotExist) {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			stats.TotalBytes += info.Size()
			if stats.OldestAt.IsZero() || info.ModTime().Before(stats.OldestAt) {
				stats.OldestAt = info.ModTime()
			}
			if stats.NewestAt.IsZero() || info.ModTime().After(stats.NewestAt) {
				stats.NewestAt = info.ModTime()
			}
			switch {
			case entry.Name() == "index.yaml":
				stats.IndexCount++
				stats.IndexBytes += info.Size()
			case strings.HasSuffix(entry.Name(), ".tgz"):
				stats.PackageCount++
				stats.PackageBytes += info.Size()
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return stats, err
		}
	}
	return stats, nil
}

func (c *Client) CleanupCache(maxBytes int64) (domain.HelmCacheCleanupResult, error) {
	result := domain.HelmCacheCleanupResult{}
	stats, err := c.CacheStats()
	if err != nil {
		return result, err
	}
	if maxBytes <= 0 {
		maxBytes = c.cacheLimit
	}
	if maxBytes <= 0 || stats.TotalBytes <= maxBytes {
		result.Stats = stats
		return result, nil
	}
	files := make([]cacheFile, 0)
	for _, root := range []string{helmChartCacheDirectory, helmRepositoryCacheDirectory} {
		base := filepath.Join(c.cacheDir, root)
		_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() {
				return nil
			}
			if entry.Name() == helmCacheMarkerFile || entry.Name() == helmIndexMetadataFile || entry.Name() == helmOCITagsFile {
				return nil
			}
			files = append(files, cacheFile{
				path: path, size: info.Size(), modTime: info.ModTime(),
				index: entry.Name() == "index.yaml",
			})
			return nil
		})
	}
	sort.SliceStable(files, func(left, right int) bool {
		if files[left].index != files[right].index {
			return !files[left].index
		}
		return files[left].modTime.Before(files[right].modTime)
	})
	for _, file := range files {
		if stats.TotalBytes <= maxBytes {
			break
		}
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
		stats.TotalBytes -= file.size
		result.DeletedFiles++
		result.FreedBytes += file.size
	}
	result.Stats, err = c.CacheStats()
	return result, err
}

func readJSONFile(path string, target any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func writeJSONFile(path string, value any, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".json-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(mode); err != nil {
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
