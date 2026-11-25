package registryclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"
)

const (
	maxRegistryBodyBytes = 4 << 20
	maxRegistryPages     = 8
	maxRegistryItems     = 5000
	registryPageSize     = 1000
)

var challengeParamPattern = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)="([^"]*)"`)

type Client struct {
	timeout time.Duration
	egress  *egress.Factory
	mu      sync.Mutex
	tokens  map[string]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

type challenge struct {
	scheme string
	params map[string]string
}

func New(timeout time.Duration, factories ...*egress.Factory) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	factory := egress.NewDirect()
	if len(factories) > 0 && factories[0] != nil {
		factory = factories[0]
	}
	return &Client{timeout: timeout, egress: factory, tokens: map[string]cachedToken{}}
}

func (c *Client) Test(ctx context.Context, registry domain.ContainerRegistry) (domain.ContainerRegistryCapabilities, error) {
	capabilities := domain.ContainerRegistryCapabilities{}
	response, err := c.do(ctx, registry, "/v2/", nil, "")
	if err != nil {
		return capabilities, err
	}
	_ = response.Body.Close()
	capabilities.Catalog = true
	capabilities.Tags = true
	catalogResponse, err := c.do(ctx, registry, "/v2/_catalog", url.Values{"n": []string{"1"}}, "registry:catalog:*")
	if err != nil {
		capabilities.Catalog = false
		capabilities.Tags = false
		return capabilities, domain.WithPublicMessage(err,
			"镜像仓服务可连接，但仓库目录 /v2/_catalog 读取失败："+catalogErrorMessage(err)+"。请检查网络出口、代理 NO_PROXY、TLS/CA 与镜像仓凭证权限。")
	}
	var payload struct {
		Repositories []string `json:"repositories"`
	}
	if err := decodeBounded(catalogResponse, &payload); err != nil {
		capabilities.Catalog = false
		capabilities.Tags = false
		return capabilities, domain.WithPublicMessage(err,
			"镜像仓仓库目录返回了无法解析的响应："+catalogErrorMessage(err)+"")
	}
	return capabilities, nil
}

func (c *Client) Repositories(ctx context.Context, registry domain.ContainerRegistry) ([]string, error) {
	path := "/v2/_catalog"
	query := url.Values{"n": []string{strconv.Itoa(registryPageSize)}}
	repositories := make([]string, 0)
	seen := map[string]struct{}{}
	for page := 0; page < maxRegistryPages; page++ {
		response, err := c.do(ctx, registry, path, query, "registry:catalog:*")
		if err != nil {
			return nil, domain.WithPublicMessage(err, "读取镜像仓仓库列表失败："+catalogErrorMessage(err)+
				"。请检查镜像仓地址、HTTPS/CA 配置、统一出站代理 NO_PROXY 和 NetworkPolicy 出口策略。")
		}
		var payload struct {
			Repositories []string `json:"repositories"`
		}
		if err := decodeBounded(response, &payload); err != nil {
			return nil, err
		}
		for _, repository := range payload.Repositories {
			repository = strings.TrimSpace(repository)
			if repository == "" {
				continue
			}
			if _, exists := seen[repository]; exists {
				continue
			}
			seen[repository] = struct{}{}
			repositories = append(repositories, repository)
			if len(repositories) >= maxRegistryItems {
				return repositories, nil
			}
		}
		next, ok := nextLink(response.Header.Get("Link"))
		if !ok {
			break
		}
		path, query = next.Path, next.Query()
	}
	return repositories, nil
}

func (c *Client) Tags(ctx context.Context, registry domain.ContainerRegistry, repository string) ([]string, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return nil, fmt.Errorf("%w: repository is required", domain.ErrInvalid)
	}
	path := "/v2/" + escapeRepository(repository) + "/tags/list"
	query := url.Values{"n": []string{strconv.Itoa(registryPageSize)}}
	scope := "repository:" + repository + ":pull"
	tags := make([]string, 0)
	seen := map[string]struct{}{}
	for page := 0; page < maxRegistryPages; page++ {
		response, err := c.do(ctx, registry, path, query, scope)
		if err != nil {
			return nil, domain.WithPublicMessage(err, "读取镜像 Tag 失败："+catalogErrorMessage(err)+
				"。请检查镜像仓凭证、仓库路径和网络出口策略。")
		}
		var payload struct {
			Tags []string `json:"tags"`
		}
		if err := decodeBounded(response, &payload); err != nil {
			return nil, err
		}
		for _, tag := range payload.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			tags = append(tags, tag)
			if len(tags) >= maxRegistryItems {
				return tags, nil
			}
		}
		next, ok := nextLink(response.Header.Get("Link"))
		if !ok {
			break
		}
		path, query = next.Path, next.Query()
	}
	return tags, nil
}

func (c *Client) do(ctx context.Context, registry domain.ContainerRegistry, path string, query url.Values, scope string) (*http.Response, error) {
	client, err := c.httpClient(registry)
	if err != nil {
		return nil, err
	}
	requestURL := strings.TrimSuffix(registry.Endpoint, "/") + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	authorization := preemptiveAuthorization(registry)
	response, err := c.request(ctx, client, requestURL, authorization)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusUnauthorized {
		return finishResponse(response)
	}
	header := response.Header.Get("WWW-Authenticate")
	_ = response.Body.Close()
	parsed, ok := parseChallenge(header)
	if !ok {
		return nil, fmt.Errorf("%w: registry authentication challenge is not supported", domain.ErrUnauthorized)
	}
	switch parsed.scheme {
	case "basic":
		if registry.Credentials.Username == "" {
			return nil, fmt.Errorf("%w: registry requires basic authentication", domain.ErrUnauthorized)
		}
		response, err = c.request(ctx, client, requestURL, basicAuthorization(registry))
	case "bearer":
		token, tokenErr := c.bearerToken(ctx, client, registry, parsed, scope)
		if tokenErr != nil {
			return nil, tokenErr
		}
		response, err = c.request(ctx, client, requestURL, "Bearer "+token)
	default:
		return nil, fmt.Errorf("%w: registry authentication scheme %q is not supported", domain.ErrUnauthorized, parsed.scheme)
	}
	if err != nil {
		return nil, err
	}
	return finishResponse(response)
}

func finishResponse(response *http.Response) (*http.Response, error) {
	if response == nil {
		return nil, fmt.Errorf("%w: registry returned an empty response", domain.ErrUnavailable)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: registry authentication failed", domain.ErrUnauthorized)
	}
	if response.StatusCode == http.StatusNotFound {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: registry resource was not found", domain.ErrNotFound)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := response.StatusCode
		_ = response.Body.Close()
		cause := fmt.Errorf("%w: registry returned HTTP %d", domain.ErrUnavailable, status)
		message := fmt.Sprintf("镜像仓返回 HTTP %d。", status)
		switch {
		case status == http.StatusBadRequest:
			message = "镜像仓返回 HTTP 400。若地址使用 http://，请改为 https:// 后重试，并检查反向代理与 TLS 配置。"
		case status >= 500:
			message = fmt.Sprintf("镜像仓返回 HTTP %d，请检查 Registry 服务状态。", status)
		}
		return nil, domain.WithPublicMessage(cause, message)
	}
	return response, nil
}

func (c *Client) request(ctx context.Context, client *http.Client, target, authorization string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: registry request is invalid", domain.ErrInvalid)
	}
	request.Header.Set("Accept", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := client.Do(request)
	if err != nil {
		detail := sanitizeTransportError(err)
		cause := fmt.Errorf("%w: registry request failed: %s", domain.ErrUnavailable, detail)
		return nil, domain.WithPublicMessage(cause,
			"连接镜像仓失败："+detail+"。请检查镜像仓地址、HTTPS/CA 配置、统一出站代理 NO_PROXY 和 NetworkPolicy 出口策略。")
	}
	return response, nil
}

func (c *Client) bearerToken(ctx context.Context, client *http.Client, registry domain.ContainerRegistry, auth challenge, fallbackScope string) (string, error) {
	realm := strings.TrimSpace(auth.params["realm"])
	if realm == "" {
		if strings.TrimSpace(registry.Credentials.Token) != "" {
			return registry.Credentials.Token, nil
		}
		return "", fmt.Errorf("%w: registry bearer challenge has no realm", domain.ErrUnauthorized)
	}
	scope := strings.TrimSpace(auth.params["scope"])
	if scope == "" {
		scope = fallbackScope
	}
	cacheKey := registry.Endpoint + "|" + auth.params["service"] + "|" + scope + "|" + registry.Credentials.Username + "|" + registry.Credentials.Token
	c.mu.Lock()
	cached, found := c.tokens[cacheKey]
	c.mu.Unlock()
	if found && cached.token != "" && time.Now().Before(cached.expires) {
		return cached.token, nil
	}

	tokenURL, err := url.Parse(realm)
	if err != nil || tokenURL.Scheme == "" || tokenURL.Host == "" {
		return "", fmt.Errorf("%w: registry token realm is invalid", domain.ErrUnauthorized)
	}
	values := tokenURL.Query()
	if service := auth.params["service"]; service != "" {
		values.Set("service", service)
	}
	if scope != "" {
		values.Set("scope", scope)
	}
	tokenURL.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("%w: registry token request is invalid", domain.ErrInvalid)
	}
	request.Header.Set("Accept", "application/json")
	if registry.Credentials.Username != "" {
		request.Header.Set("Authorization", basicAuthorization(registry))
	} else if registry.Credentials.Token != "" && auth.params["realm"] == "" {
		request.Header.Set("Authorization", "Bearer "+registry.Credentials.Token)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: registry token request failed: %v", domain.ErrUnavailable, sanitizeTransportError(err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := response.StatusCode
		_ = response.Body.Close()
		return "", fmt.Errorf("%w: registry token endpoint returned HTTP %d", domain.ErrUnauthorized, status)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		IssuedAt    string `json:"issued_at"`
	}
	if err := decodeBounded(response, &payload); err != nil {
		return "", err
	}
	if payload.Token == "" {
		payload.Token = payload.AccessToken
	}
	if payload.Token == "" {
		return "", fmt.Errorf("%w: registry token endpoint returned no token", domain.ErrUnauthorized)
	}
	expiresIn := time.Duration(payload.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = time.Minute
	}
	expires := time.Now().Add(expiresIn - 5*time.Second)
	if expires.Before(time.Now().Add(5 * time.Second)) {
		expires = time.Now().Add(5 * time.Second)
	}
	c.mu.Lock()
	if len(c.tokens) > 256 {
		c.tokens = map[string]cachedToken{}
	}
	c.tokens[cacheKey] = cachedToken{token: payload.Token, expires: expires}
	c.mu.Unlock()
	return payload.Token, nil
}

func (c *Client) httpClient(registry domain.ContainerRegistry) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: registry.InsecureSkipTLSVerify} //nolint:gosec
	if strings.TrimSpace(registry.Credentials.CAPEM) != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(registry.Credentials.CAPEM)) {
			return nil, fmt.Errorf("%w: registry custom CA contains no certificates", domain.ErrInvalid)
		}
		tlsConfig.RootCAs = roots
	}
	transport := c.egress.Transport(egress.ScopeRegistry, tlsConfig)
	return &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return fmt.Errorf("too many registry redirects")
			}
			if len(via) > 0 && !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}, nil
}

func preemptiveAuthorization(registry domain.ContainerRegistry) string {
	if registry.AuthType == domain.RegistryAuthBearer && strings.TrimSpace(registry.Credentials.Token) != "" {
		return "Bearer " + strings.TrimSpace(registry.Credentials.Token)
	}
	if registry.AuthType == domain.RegistryAuthBasic && registry.Credentials.Username != "" {
		return basicAuthorization(registry)
	}
	return ""
}

func basicAuthorization(registry domain.ContainerRegistry) string {
	request, _ := http.NewRequest(http.MethodGet, "http://registry.invalid", nil)
	request.SetBasicAuth(registry.Credentials.Username, registry.Credentials.Password)
	return request.Header.Get("Authorization")
}

func parseChallenge(header string) (challenge, bool) {
	header = strings.TrimSpace(header)
	space := strings.IndexByte(header, ' ')
	if space <= 0 {
		return challenge{}, false
	}
	result := challenge{scheme: strings.ToLower(header[:space]), params: map[string]string{}}
	for _, match := range challengeParamPattern.FindAllStringSubmatch(header[space+1:], -1) {
		result.params[strings.ToLower(match[1])] = match[2]
	}
	return result, true
}

func decodeBounded(response *http.Response, target any) error {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: registry returned HTTP %d", domain.ErrUnavailable, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxRegistryBodyBytes))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode registry response: %v", domain.ErrUnavailable, err)
	}
	return nil
}

func nextLink(header string) (*url.URL, bool) {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "<") {
			continue
		}
		end := strings.IndexByte(part, '>')
		if end < 0 {
			continue
		}
		parsed, err := url.Parse(part[1:end])
		if err != nil {
			continue
		}
		return parsed, true
	}
	return nil, false
}

func escapeRepository(repository string) string {
	parts := strings.Split(repository, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func sanitizeTransportError(err error) string {
	message := err.Error()
	if len(message) > 300 {
		message = message[:300]
	}
	return strings.ReplaceAll(message, "\n", " ")
}

func catalogErrorMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		return "镜像仓认证失败或凭证无权读取 catalog"
	case errors.Is(err, domain.ErrNotFound):
		return "镜像仓未提供 catalog API"
	case errors.Is(err, domain.ErrUnsupported):
		return "镜像仓未开放 catalog API"
	}
	message := strings.TrimSpace(sanitizeTransportError(err))
	if message == "" {
		return "未知错误"
	}
	return message
}
