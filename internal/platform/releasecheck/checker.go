package releasecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"

	"github.com/Masterminds/semver/v3"
)

const maxReleaseBodyBytes = 1 << 20

type Config struct {
	Enabled        bool
	URL            string
	Token          string
	Channel        string
	CurrentVersion string
	Timeout        time.Duration
	CacheTTL       time.Duration
}

type Checker struct {
	config  Config
	client  *http.Client
	mu      sync.Mutex
	cached  domain.UpdateStatus
	expires time.Time
}

type releasePayload struct {
	Version     string `json:"version"`
	TagName     string `json:"tag_name"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Notes       string `json:"notes"`
	Body        string `json:"body"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
}

func New(cfg Config, factory *egress.Factory) *Checker {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 6 * time.Hour
	}
	if cfg.Channel == "" {
		cfg.Channel = "stable"
	}
	if factory == nil {
		factory = egress.NewDirect()
	}
	client := factory.Client(egress.ScopeUpdate, cfg.Timeout, nil)
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("update source redirected too many times")
		}
		if request.URL.Scheme != "https" {
			return errors.New("update source redirect must use HTTPS")
		}
		return nil
	}
	return &Checker{
		config: cfg,
		client: client,
	}
}

func (c *Checker) Status(ctx context.Context, force bool) domain.UpdateStatus {
	now := time.Now().UTC()
	if !c.config.Enabled {
		return domain.UpdateStatus{
			Enabled: false, Status: "disabled", CurrentVersion: c.config.CurrentVersion,
			CheckedAt: now.Format(time.RFC3339), Message: "更新检查未启用。",
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && !c.expires.IsZero() && now.Before(c.expires) {
		return c.cached
	}
	status := c.fetch(ctx, now)
	c.cached = status
	cacheFor := c.config.CacheTTL
	if status.Status == "error" {
		cacheFor = time.Minute
	}
	c.expires = now.Add(cacheFor)
	return status
}

func (c *Checker) fetch(ctx context.Context, now time.Time) domain.UpdateStatus {
	status := domain.UpdateStatus{
		Enabled: true, Status: "error", CurrentVersion: c.config.CurrentVersion,
		CheckedAt: now.Format(time.RFC3339), Source: redactSource(c.config.URL),
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.URL, nil)
	if err != nil {
		status.Message = "更新检查地址无效。"
		return status
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "dosh/"+c.config.CurrentVersion)
	if c.config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.config.Token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		status.Message = "无法连接更新源，请检查网络和出站代理。"
		return status
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status.Message = fmt.Sprintf("更新源返回 HTTP %d。", response.StatusCode)
		return status
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBodyBytes+1))
	if err != nil || len(body) > maxReleaseBodyBytes {
		status.Message = "更新源响应过大或读取失败。"
		return status
	}
	var payload releasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		status.Message = "更新源响应不是有效的版本信息。"
		return status
	}
	latest := strings.TrimPrefix(strings.TrimSpace(firstNonEmpty(payload.Version, payload.TagName)), "v")
	if latest == "" {
		status.Message = "更新源未返回版本号。"
		return status
	}
	status.LatestVersion = latest
	status.LatestURL = safeReleaseURL(firstNonEmpty(payload.HTMLURL, payload.URL))
	status.PublishedAt = strings.TrimSpace(payload.PublishedAt)
	status.Notes = truncate(firstNonEmpty(payload.Notes, payload.Body), 4000)
	status.Status = "unknown"
	status.Message = "当前版本不是可比较的语义化版本。"

	current, currentErr := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(c.config.CurrentVersion), "v"))
	latestVersion, latestErr := semver.NewVersion(latest)
	if currentErr != nil || latestErr != nil {
		return status
	}
	if payload.Draft {
		status.Status = "current"
		status.Message = "更新源返回的是草稿版本，已忽略。"
		return status
	}
	if (payload.Prerelease || latestVersion.Prerelease() != "") && c.config.Channel != "prerelease" {
		status.Status = "current"
		status.Message = "更新源返回的是预发布版本，稳定版通道已忽略。"
		return status
	}
	if latestVersion.GreaterThan(current) {
		status.Status = "update_available"
		status.Message = fmt.Sprintf("发现新版本 v%s。", latest)
		return status
	}
	status.Status = "current"
	status.Message = "当前已是最新版本。"
	return status
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func safeReleaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func redactSource(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
