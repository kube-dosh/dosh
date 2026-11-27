package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"
)

const (
	maxRegistryNameLength   = 128
	maxRegistryEndpointSize = 2048
	maxRegistrySecretSize   = 16 << 10
	maxRegistryCASize       = 64 << 10
)

var (
	repositoryComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	dnsSubdomainPattern        = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
)

type Repository interface {
	ListContainerRegistries(ctx context.Context) ([]domain.ContainerRegistry, error)
	GetContainerRegistry(ctx context.Context, id string) (domain.ContainerRegistry, error)
	SaveContainerRegistry(ctx context.Context, registry domain.ContainerRegistry) (domain.ContainerRegistry, error)
	DeleteContainerRegistry(ctx context.Context, id, expectedRevision string) error
}

type Remote interface {
	Test(ctx context.Context, registry domain.ContainerRegistry) (domain.ContainerRegistryCapabilities, error)
	Repositories(ctx context.Context, registry domain.ContainerRegistry) ([]string, error)
	Tags(ctx context.Context, registry domain.ContainerRegistry, repository string) ([]string, error)
}

type Service struct {
	repository Repository
	remote     Remote
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, remote Remote, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, remote: remote, authorizer: authorizer, auditor: auditor}
}

func (s *Service) List(ctx context.Context, actor domain.Actor) ([]domain.ContainerRegistry, error) {
	if err := s.require(ctx, actor, "list"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListContainerRegistries(ctx)
	if err != nil {
		return nil, err
	}
	return stripRegistrySecrets(items), nil
}

func (s *Service) Create(ctx context.Context, actor domain.Actor, input domain.ContainerRegistryUpsert) (domain.ContainerRegistry, error) {
	if err := s.require(ctx, actor, "create"); err != nil {
		s.record(ctx, actor, "create", "", nil, err)
		return domain.ContainerRegistry{}, err
	}
	registry, err := normalizeRegistry(input, domain.ContainerRegistry{})
	if err != nil {
		s.record(ctx, actor, "create", "", nil, err)
		return domain.ContainerRegistry{}, err
	}
	id, err := newRegistryID()
	if err != nil {
		return domain.ContainerRegistry{}, err
	}
	registry.ID = id
	saved, err := s.repository.SaveContainerRegistry(ctx, registry)
	s.record(ctx, actor, "create", saved.ID, nil, err)
	return stripRegistrySecret(saved), err
}

func (s *Service) Update(ctx context.Context, actor domain.Actor, id string, input domain.ContainerRegistryUpsert) (domain.ContainerRegistry, error) {
	if err := s.require(ctx, actor, "update"); err != nil {
		s.record(ctx, actor, "update", id, nil, err)
		return domain.ContainerRegistry{}, err
	}
	current, err := s.repository.GetContainerRegistry(ctx, id)
	if err != nil {
		s.record(ctx, actor, "update", id, nil, err)
		return domain.ContainerRegistry{}, err
	}
	if input.Revision != "" && input.Revision != current.Revision {
		err := fmt.Errorf("%w: expected registry revision %s, got %s", domain.ErrConflict, input.Revision, current.Revision)
		s.record(ctx, actor, "update", id, nil, err)
		return domain.ContainerRegistry{}, err
	}
	registry, err := normalizeRegistry(input, current)
	if err != nil {
		s.record(ctx, actor, "update", id, nil, err)
		return domain.ContainerRegistry{}, err
	}
	registry.ID = current.ID
	registry.Revision = current.Revision
	saved, err := s.repository.SaveContainerRegistry(ctx, registry)
	s.record(ctx, actor, "update", id, nil, err)
	return stripRegistrySecret(saved), err
}

func (s *Service) Delete(ctx context.Context, actor domain.Actor, id, revision string) error {
	if err := s.require(ctx, actor, "delete"); err != nil {
		s.record(ctx, actor, "delete", id, nil, err)
		return err
	}
	err := s.repository.DeleteContainerRegistry(ctx, id, revision)
	s.record(ctx, actor, "delete", id, nil, err)
	return err
}

func (s *Service) Test(ctx context.Context, actor domain.Actor, id string) (domain.ContainerRegistry, error) {
	if err := s.require(ctx, actor, "update"); err != nil {
		s.record(ctx, actor, "test", id, nil, err)
		return domain.ContainerRegistry{}, err
	}
	registry, err := s.repository.GetContainerRegistry(ctx, id)
	if err != nil {
		s.record(ctx, actor, "test", id, nil, err)
		return domain.ContainerRegistry{}, err
	}
	capabilities, testErr := s.remote.Test(ctx, registry)
	testErr = normalizeRegistryRemoteError(testErr)
	message := "连接成功"
	if testErr != nil {
		message = testErr.Error()
		if len(message) > 300 {
			message = message[:300]
		}
	}
	registry.LastTest = &domain.ContainerRegistryTest{
		OK: testErr == nil, Message: message, TestedAt: time.Now().UTC(), Capabilities: capabilities,
	}
	if testErr == nil {
		registry.Capabilities = capabilities
	}
	saved, saveErr := s.repository.SaveContainerRegistry(ctx, registry)
	if saveErr != nil {
		s.record(ctx, actor, "test", id, nil, saveErr)
		return domain.ContainerRegistry{}, saveErr
	}
	s.record(ctx, actor, "test", id, nil, testErr)
	return stripRegistrySecret(saved), nil
}

func (s *Service) Repositories(ctx context.Context, actor domain.Actor, id, search string, page domain.PageRequest) (domain.PageResult[domain.ContainerRegistryRepository], error) {
	if err := s.require(ctx, actor, "list"); err != nil {
		return domain.PageResult[domain.ContainerRegistryRepository]{}, err
	}
	registry, err := s.repository.GetContainerRegistry(ctx, id)
	if err != nil {
		return domain.PageResult[domain.ContainerRegistryRepository]{}, err
	}
	repositories, err := s.remote.Repositories(ctx, registry)
	if err != nil {
		return domain.PageResult[domain.ContainerRegistryRepository]{}, normalizeRegistryRemoteError(err)
	}
	prefix := strings.Trim(registry.RepositoryPrefix, "/")
	normalizedSearch := strings.ToLower(strings.TrimSpace(search))
	items := make([]domain.ContainerRegistryRepository, 0, len(repositories))
	for _, repository := range repositories {
		repository = strings.Trim(strings.TrimSpace(repository), "/")
		if repository == "" {
			continue
		}
		if prefix != "" && repository != prefix && !strings.HasPrefix(repository, prefix+"/") {
			continue
		}
		if normalizedSearch != "" && !strings.Contains(strings.ToLower(repository), normalizedSearch) {
			continue
		}
		items = append(items, domain.ContainerRegistryRepository{Name: repository, Image: registryImageName(registry, repository)})
	}
	sort.SliceStable(items, func(left, right int) bool { return items[left].Name < items[right].Name })
	return domain.PaginateSliceWithCursor(items, page)
}

func (s *Service) Tags(ctx context.Context, actor domain.Actor, id, repository, search string, page domain.PageRequest) (domain.PageResult[domain.ContainerRegistryTag], error) {
	if err := s.require(ctx, actor, "list"); err != nil {
		return domain.PageResult[domain.ContainerRegistryTag]{}, err
	}
	registry, err := s.repository.GetContainerRegistry(ctx, id)
	if err != nil {
		return domain.PageResult[domain.ContainerRegistryTag]{}, err
	}
	repository, err = normalizeRepository(registry, repository)
	if err != nil {
		return domain.PageResult[domain.ContainerRegistryTag]{}, err
	}
	tags, err := s.remote.Tags(ctx, registry, repository)
	if err != nil {
		return domain.PageResult[domain.ContainerRegistryTag]{}, normalizeRegistryRemoteError(err)
	}
	normalizedSearch := strings.ToLower(strings.TrimSpace(search))
	items := make([]domain.ContainerRegistryTag, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || (normalizedSearch != "" && !strings.Contains(strings.ToLower(tag), normalizedSearch)) {
			continue
		}
		items = append(items, domain.ContainerRegistryTag{Name: tag, Image: registryImageName(registry, repository) + ":" + tag})
	}
	sortTagsDescending(items)
	return domain.PaginateSliceWithCursor(items, page)
}

func (s *Service) Resolve(ctx context.Context, actor domain.Actor, image string) (domain.ImageReferenceResolution, error) {
	if err := s.require(ctx, actor, "list"); err != nil {
		return domain.ImageReferenceResolution{}, err
	}
	reference, err := domain.ParseImageReference(image)
	if err != nil {
		return domain.ImageReferenceResolution{}, err
	}
	resolution := domain.ImageReferenceResolution{
		Image: image, ExplicitRegistry: reference.ExplicitRegistry,
		Repository: reference.Repository, Tag: reference.Tag, Digest: reference.Digest,
	}
	items, err := s.repository.ListContainerRegistries(ctx)
	if err != nil {
		return domain.ImageReferenceResolution{}, err
	}
	registry, matched := matchRegistry(items, reference)
	if !matched {
		return resolution, nil
	}
	resolution.RegistryID = registry.ID
	resolution.RegistryName = registry.Name
	resolution.RegistryEndpoint = registry.Endpoint
	resolution.Matched = true
	return resolution, nil
}

// ResolvePullSecret validates that the selected image belongs to the selected
// registry and returns the optional Pod-level imagePullSecrets entry.
func (s *Service) ResolvePullSecret(ctx context.Context, actor domain.Actor, registryID, image string) (string, error) {
	if strings.TrimSpace(registryID) == "" {
		return "", nil
	}
	if err := s.require(ctx, actor, "get"); err != nil {
		return "", err
	}
	registry, err := s.repository.GetContainerRegistry(ctx, registryID)
	if err != nil {
		return "", err
	}
	reference, err := domain.ParseImageReference(image)
	if err != nil {
		return "", err
	}
	if _, matched := matchRegistry([]domain.ContainerRegistry{registry}, reference); !matched {
		return "", fmt.Errorf("%w: image %q does not belong to the selected container registry", domain.ErrInvalid, image)
	}
	return registry.PullSecretName, nil
}

func normalizeRegistry(input domain.ContainerRegistryUpsert, current domain.ContainerRegistry) (domain.ContainerRegistry, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > maxRegistryNameLength {
		return domain.ContainerRegistry{}, fmt.Errorf("%w: name is required and must not exceed %d characters", domain.ErrInvalid, maxRegistryNameLength)
	}
	endpoint, err := normalizeEndpoint(input.Endpoint)
	if err != nil {
		return domain.ContainerRegistry{}, err
	}
	prefix := strings.Trim(strings.TrimSpace(input.RepositoryPrefix), "/")
	if prefix != "" {
		if err := validateRepositoryPath(prefix); err != nil {
			return domain.ContainerRegistry{}, fmt.Errorf("%w: repositoryPrefix is invalid", domain.ErrInvalid)
		}
	}
	authType := strings.ToLower(strings.TrimSpace(input.AuthType))
	if authType == "" {
		authType = domain.RegistryAuthAnonymous
	}
	switch authType {
	case domain.RegistryAuthAnonymous, domain.RegistryAuthBasic, domain.RegistryAuthBearer:
	default:
		return domain.ContainerRegistry{}, fmt.Errorf("%w: authType must be anonymous, basic or bearer", domain.ErrInvalid)
	}
	pullSecretName := strings.TrimSpace(input.PullSecretName)
	if pullSecretName != "" && (len(pullSecretName) > 253 || !dnsSubdomainPattern.MatchString(pullSecretName)) {
		return domain.ContainerRegistry{}, fmt.Errorf("%w: pullSecretName must be a DNS-1123 subdomain", domain.ErrInvalid)
	}

	credentials := current.Credentials
	if input.ClearCredentials {
		credentials = domain.ContainerRegistryCredentials{}
	} else if input.Credentials != nil {
		if input.Credentials.Username != "" {
			credentials.Username = input.Credentials.Username
		}
		if input.Credentials.Password != "" {
			credentials.Password = input.Credentials.Password
		}
		if input.Credentials.Token != "" {
			credentials.Token = input.Credentials.Token
		}
		if input.Credentials.CAPEM != "" {
			credentials.CAPEM = input.Credentials.CAPEM
		}
	}
	if authType == domain.RegistryAuthAnonymous {
		credentials.Username = ""
		credentials.Password = ""
		credentials.Token = ""
	}
	if err := validateCredentials(credentials); err != nil {
		return domain.ContainerRegistry{}, err
	}
	if authType == domain.RegistryAuthBasic && strings.TrimSpace(credentials.Username) == "" {
		return domain.ContainerRegistry{}, fmt.Errorf("%w: basic authentication requires a username", domain.ErrInvalid)
	}
	if authType == domain.RegistryAuthBearer && strings.TrimSpace(credentials.Token) == "" {
		return domain.ContainerRegistry{}, fmt.Errorf("%w: bearer authentication requires a token", domain.ErrInvalid)
	}

	now := time.Now().UTC()
	return domain.ContainerRegistry{
		Name: name, Endpoint: endpoint, RepositoryPrefix: prefix, AuthType: authType,
		HasCredentials: credentialsConfigured(credentials), HasCustomCA: strings.TrimSpace(credentials.CAPEM) != "",
		InsecureSkipTLSVerify: input.InsecureSkipTLSVerify, PullSecretName: pullSecretName,
		Default: input.Default, Credentials: credentials,
		Capabilities: current.Capabilities, LastTest: current.LastTest,
		CreatedAt: current.CreatedAt, UpdatedAt: now,
	}, nil
}

func credentialsConfigured(credentials domain.ContainerRegistryCredentials) bool {
	return strings.TrimSpace(credentials.Username) != "" || strings.TrimSpace(credentials.Password) != "" || strings.TrimSpace(credentials.Token) != ""
}

func normalizeEndpoint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxRegistryEndpointSize {
		return "", fmt.Errorf("%w: endpoint is required and must not exceed %d characters", domain.ErrInvalid, maxRegistryEndpointSize)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: endpoint must be an absolute HTTP or HTTPS URL", domain.ErrInvalid)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: endpoint scheme must be http or https", domain.ErrInvalid)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("%w: endpoint must contain only scheme and host; put repository scope in repositoryPrefix", domain.ErrInvalid)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", fmt.Errorf("%w: loopback registry endpoints are not allowed", domain.ErrInvalid)
	}
	if address := net.ParseIP(host); address != nil {
		if address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast() {
			return "", fmt.Errorf("%w: link-local, loopback and multicast registry endpoints are not allowed", domain.ErrInvalid)
		}
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("%w: endpoint port is invalid", domain.ErrInvalid)
		}
	}
	return parsed.Scheme + "://" + strings.ToLower(parsed.Host), nil
}

func validateRepositoryPath(value string) error {
	parts := strings.Split(value, "/")
	if len(parts) == 0 {
		return fmt.Errorf("empty repository")
	}
	for _, part := range parts {
		if !repositoryComponentPattern.MatchString(part) {
			return fmt.Errorf("invalid component")
		}
	}
	return nil
}

func normalizeRepository(registry domain.ContainerRegistry, repository string) (string, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return "", fmt.Errorf("%w: repository is required", domain.ErrInvalid)
	}
	if err := validateRepositoryPath(repository); err != nil {
		return "", fmt.Errorf("%w: repository is invalid", domain.ErrInvalid)
	}
	prefix := strings.Trim(registry.RepositoryPrefix, "/")
	if prefix != "" && repository != prefix && !strings.HasPrefix(repository, prefix+"/") {
		return "", fmt.Errorf("%w: repository is outside the configured registry scope", domain.ErrInvalid)
	}
	return repository, nil
}

func validateCredentials(credentials domain.ContainerRegistryCredentials) error {
	if len(credentials.Username) > maxRegistrySecretSize || len(credentials.Password) > maxRegistrySecretSize || len(credentials.Token) > maxRegistrySecretSize {
		return fmt.Errorf("%w: registry credential fields are too large", domain.ErrInvalid)
	}
	if len(credentials.CAPEM) > maxRegistryCASize {
		return fmt.Errorf("%w: registry custom CA is too large", domain.ErrInvalid)
	}
	return nil
}

func registryImageName(registry domain.ContainerRegistry, repository string) string {
	parsed, err := url.Parse(registry.Endpoint)
	if err != nil || parsed.Host == "" {
		return repository
	}
	return parsed.Host + "/" + repository
}

func matchRegistry(registries []domain.ContainerRegistry, reference domain.ImageReference) (domain.ContainerRegistry, bool) {
	if !reference.ExplicitRegistry {
		return domain.ContainerRegistry{}, false
	}
	var matched domain.ContainerRegistry
	bestPrefix := -1
	for _, registry := range registries {
		parsed, err := url.Parse(registry.Endpoint)
		if err != nil || !strings.EqualFold(parsed.Host, reference.Registry) {
			continue
		}
		prefix := strings.Trim(registry.RepositoryPrefix, "/")
		if prefix != "" && reference.Repository != prefix && !strings.HasPrefix(reference.Repository, prefix+"/") {
			continue
		}
		if len(prefix) > bestPrefix {
			matched = registry
			bestPrefix = len(prefix)
		}
	}
	return matched, bestPrefix >= 0
}

func stripRegistrySecret(registry domain.ContainerRegistry) domain.ContainerRegistry {
	registry.Credentials = domain.ContainerRegistryCredentials{}
	return registry
}

func stripRegistrySecrets(items []domain.ContainerRegistry) []domain.ContainerRegistry {
	for index := range items {
		items[index] = stripRegistrySecret(items[index])
	}
	return items
}

func newRegistryID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate container registry id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func (s *Service) require(ctx context.Context, actor domain.Actor, verb string) error {
	if s.authorizer == nil {
		return domain.ErrForbidden
	}
	return s.authorizer.Require(ctx, actor, domain.Access{Verb: verb, APIGroup: "dosh.io", Resource: "container-registries"})
}

func (s *Service) record(ctx context.Context, actor domain.Actor, action, id string, details map[string]any, err error) {
	if s.auditor == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: action + "-container-registry", Resource: "container-registry", Name: id,
		Result: result, Details: details, Err: err,
	})
}

func normalizeRegistryRemoteError(err error) error {
	if err == nil || errors.Is(err, domain.ErrDependencyUnauthorized) {
		return err
	}
	if !errors.Is(err, domain.ErrUnauthorized) {
		return err
	}
	message := domain.PublicDetail(err)
	if message == "" {
		message = "镜像仓认证失败，请检查镜像仓认证配置。"
	}
	return domain.WithPublicMessage(
		fmt.Errorf("%w: upstream registry authentication failed: %v", domain.ErrDependencyUnauthorized, err),
		message,
	)
}
