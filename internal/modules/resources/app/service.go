package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dosh/internal/domain"

	yamlv3 "go.yaml.in/yaml/v3"
	"sigs.k8s.io/yaml"
)

// Repository is the consumer-owned port for Kubernetes API Discovery and
// generic resource reads. It deliberately exposes resource descriptors rather
// than arbitrary Kubernetes URLs so the application layer always validates a
// discovered GVR before issuing a request.
type Repository interface {
	DiscoverResources(ctx context.Context) ([]domain.ResourceDescriptor, []string, error)
	ListNamespaceNames(ctx context.Context) ([]string, error)
	GetResourceSchema(ctx context.Context, descriptor domain.ResourceDescriptor) (domain.ResourceSchema, error)
	ListResourceRules(ctx context.Context, actor domain.Actor, namespace string) (domain.ResourceRules, []string, error)
	ListGenericResources(ctx context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.GenericResourcePage, error)
	GetGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (map[string]any, error)
	GetResourceRelations(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (domain.ResourceRelations, error)
	CreateGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace string, object map[string]any, dryRun bool) (map[string]any, error)
	UpdateGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace string, object map[string]any, resourceVersion string, dryRun bool) (map[string]any, error)
	ApplyGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace string, object map[string]any, resourceVersion string, dryRun bool) (map[string]any, error)
	DeleteGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name, resourceVersion string, propagation domain.DeletionPropagation) error
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor

	clusterAuthMu sync.Mutex
	clusterAuth   map[string]cachedClusterListAccess
	searchMu      sync.Mutex
	searchCache   map[string]cachedResourceSearch
}

const clusterListAccessTTL = 30 * time.Second
const resourceSearchTTL = 10 * time.Second
const maxGenericManifestBytes = 1 << 20

type cachedClusterListAccess struct {
	allowed   bool
	fetchedAt time.Time
}

type cachedResourceSearch struct {
	result    domain.GenericResourceSearch
	fetchedAt time.Time
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{
		repository: repository, authorizer: authorizer, auditor: auditor,
		clusterAuth: make(map[string]cachedClusterListAccess),
		searchCache: make(map[string]cachedResourceSearch),
	}
}

func (s *Service) Discovery(ctx context.Context, actor domain.Actor, namespace string) ([]domain.ResourceDescriptor, []string, error) {
	items, warnings, err := s.repository.DiscoverResources(ctx)
	if err != nil {
		return nil, warnings, err
	}
	if namespace == "" {
		return items, warnings, nil
	}
	rules, ruleWarnings, err := s.repository.ListResourceRules(ctx, actor, namespace)
	warnings = append(warnings, ruleWarnings...)
	if err != nil {
		warnings = append(warnings, "无法读取当前身份的 RBAC 规则，资源目录已按失败关闭策略隐藏")
		return []domain.ResourceDescriptor{}, warnings, nil
	}
	filtered := make([]domain.ResourceDescriptor, 0, len(items))
	clusterAuthFailed := false
	for _, item := range items {
		if item.Namespaced {
			if descriptorListable(item, rules, namespace) {
				filtered = append(filtered, item)
			}
			continue
		}
		allowed, authErr := s.canListClusterResource(ctx, actor, item)
		if authErr != nil {
			clusterAuthFailed = true
			continue
		}
		if allowed {
			filtered = append(filtered, item)
		}
	}
	if clusterAuthFailed {
		warnings = append(warnings, "无法完成集群级资源授权检查，相关资源类型已从 Discovery 中隐藏")
	}
	if len(filtered) == 0 {
		warnings = append(warnings, "当前身份在该命名空间没有可列出对象，请选择其他命名空间或检查 RBAC 授权")
	}
	return filtered, warnings, nil
}

// Schema returns the advisory OpenAPI schema for one discovered resource type.
// Schema discovery failures degrade to an unavailable result because API Server
// dry-run remains the authoritative validator for writes.
func (s *Service) Schema(ctx context.Context, ref domain.ResourceReference) (domain.ResourceSchema, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.ResourceSchema{}, err
	}
	schema, err := s.repository.GetResourceSchema(ctx, descriptor)
	if err != nil {
		return domain.ResourceSchema{
			Group: descriptor.Group, Version: descriptor.Version, Resource: descriptor.Resource,
			Kind: descriptor.Kind, Source: "unavailable", Schema: map[string]any{},
			Warnings: []string{"无法读取当前集群 OpenAPI Schema，已回退到 YAML 编辑；API Server Dry Run 仍是最终校验依据"},
		}, nil
	}
	if schema.Group == "" && descriptor.Group == "" {
		schema.Group = ""
	}
	schema.Group, schema.Version, schema.Resource, schema.Kind = descriptor.Group, descriptor.Version, descriptor.Resource, descriptor.Kind
	if schema.Schema == nil {
		schema.Schema = map[string]any{}
	}
	return schema, nil
}

func (s *Service) List(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, query domain.ListQuery, page domain.PageRequest) (domain.GenericResourcePage, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.GenericResourcePage{}, err
	}
	if !descriptor.Namespaced {
		query.Namespace = ""
	}
	access := domain.Access{
		Verb:      "list",
		APIGroup:  descriptor.Group,
		Resource:  descriptor.Resource,
		Namespace: query.Namespace,
	}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		return domain.GenericResourcePage{}, err
	}
	result, err := s.repository.ListGenericResources(ctx, descriptor, query, page)
	if err != nil {
		return domain.GenericResourcePage{}, err
	}
	if isSecret(descriptor) {
		for i := range result.Items {
			result.Items[i].Annotations = redactedAnnotationKeys(result.Items[i].Annotations)
		}
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, namespace, name string) (domain.GenericResourceDetail, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if name == "" {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: resource name is required", domain.ErrInvalid)
	}
	if descriptor.Namespaced && namespace == "" {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: namespace is required for %s", domain.ErrInvalid, descriptor.Resource)
	}
	if !descriptor.Namespaced {
		namespace = ""
	}
	access := domain.Access{
		Verb:      "get",
		APIGroup:  descriptor.Group,
		Resource:  descriptor.Resource,
		Namespace: namespace,
		Name:      name,
	}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		if isSecret(descriptor) {
			s.auditSecret(ctx, actor, namespace, name, err)
		}
		return domain.GenericResourceDetail{}, err
	}
	object, err := s.repository.GetGenericResource(ctx, descriptor, namespace, name)
	if err != nil {
		if isSecret(descriptor) {
			s.auditSecret(ctx, actor, namespace, name, err)
		}
		return domain.GenericResourceDetail{}, err
	}
	detail, err := genericDetailFromObject(descriptor, object)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if detail.Redacted {
		s.auditSecret(ctx, actor, namespace, name, nil)
	}
	return detail, nil
}

// Relations returns bounded direct relationships for one authorized object.
// The source object requires get permission; related object reads are still
// enforced by Kubernetes and permission failures degrade to warnings.
func (s *Service) Relations(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, namespace, name string) (domain.ResourceRelations, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.ResourceRelations{}, err
	}
	if name == "" {
		return domain.ResourceRelations{}, fmt.Errorf("%w: resource name is required", domain.ErrInvalid)
	}
	if descriptor.Namespaced && namespace == "" {
		return domain.ResourceRelations{}, fmt.Errorf("%w: namespace is required for %s", domain.ErrInvalid, descriptor.Resource)
	}
	if !descriptor.Namespaced {
		namespace = ""
	}
	access := domain.Access{Verb: "get", APIGroup: descriptor.Group, Resource: descriptor.Resource, Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		return domain.ResourceRelations{}, err
	}
	result, err := s.repository.GetResourceRelations(ctx, descriptor, namespace, name)
	if err != nil {
		return domain.ResourceRelations{}, err
	}
	if result.Items == nil {
		result.Items = []domain.ResourceRelation{}
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, request domain.GenericResourceMutation) (domain.GenericResourceDetail, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if isSecret(descriptor) {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: generic Secret creation is disabled; use the dedicated Secret editor", domain.ErrUnsupported)
	}
	if !supportsVerb(descriptor, "create") {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: %s does not support create", domain.ErrUnsupported, descriptor.Resource)
	}
	object, namespace, name, err := prepareGenericManifest(descriptor, request)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if err := s.validateSchema(ctx, descriptor, object); err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if err := requireWriteConfirmation(descriptor, name, request.Confirmation, request.DryRun); err != nil {
		s.auditGenericMutation(ctx, actor, mutationAction("create", request.DryRun), descriptor, namespace, name, err)
		return domain.GenericResourceDetail{}, err
	}
	access := domain.Access{Verb: "create", APIGroup: descriptor.Group, Resource: descriptor.Resource, Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditGenericMutation(ctx, actor, mutationAction("create", request.DryRun), descriptor, namespace, name, err)
		return domain.GenericResourceDetail{}, err
	}
	if !request.DryRun {
		if _, err := s.repository.CreateGenericResource(ctx, descriptor, namespace, object, true); err != nil {
			s.auditGenericMutation(ctx, actor, mutationAction("create", false), descriptor, namespace, name, err)
			return domain.GenericResourceDetail{}, err
		}
	}
	result, err := s.repository.CreateGenericResource(ctx, descriptor, namespace, object, request.DryRun)
	s.auditGenericMutation(ctx, actor, mutationAction("create", request.DryRun), descriptor, namespace, name, err)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	return genericDetailFromObject(descriptor, result)
}

func (s *Service) Update(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, name string, request domain.GenericResourceMutation) (domain.GenericResourceDetail, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if isSecret(descriptor) {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: generic Secret updates are disabled; use the dedicated Secret editor", domain.ErrUnsupported)
	}
	if !supportsVerb(descriptor, "update") && !supportsVerb(descriptor, "patch") {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: %s does not support update", domain.ErrUnsupported, descriptor.Resource)
	}
	object, namespace, objectName, err := prepareGenericManifest(descriptor, request)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if name == "" || objectName != name {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: metadata.name must match the resource path", domain.ErrInvalid)
	}
	if err := s.validateSchema(ctx, descriptor, object); err != nil {
		return domain.GenericResourceDetail{}, err
	}
	if err := requireWriteConfirmation(descriptor, name, request.Confirmation, request.DryRun); err != nil {
		s.auditGenericMutation(ctx, actor, mutationAction("update", request.DryRun), descriptor, namespace, name, err)
		return domain.GenericResourceDetail{}, err
	}
	access := domain.Access{Verb: "update", APIGroup: descriptor.Group, Resource: descriptor.Resource, Namespace: namespace, Name: name}
	useApply := false
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		if !errors.Is(err, domain.ErrForbidden) {
			s.auditGenericMutation(ctx, actor, mutationAction("update", request.DryRun), descriptor, namespace, name, err)
			return domain.GenericResourceDetail{}, err
		}
		access.Verb = "patch"
		if patchErr := s.authorizer.Require(ctx, actor, access); patchErr != nil {
			s.auditGenericMutation(ctx, actor, mutationAction("update", request.DryRun), descriptor, namespace, name, patchErr)
			return domain.GenericResourceDetail{}, patchErr
		}
		useApply = true
	}
	mutate := func(dryRun bool) (map[string]any, error) {
		if useApply {
			return s.repository.ApplyGenericResource(ctx, descriptor, namespace, object, request.ResourceVersion, dryRun)
		}
		return s.repository.UpdateGenericResource(ctx, descriptor, namespace, object, request.ResourceVersion, dryRun)
	}
	if !request.DryRun {
		if _, err := mutate(true); err != nil {
			s.auditGenericMutation(ctx, actor, mutationAction("update", false), descriptor, namespace, name, err)
			return domain.GenericResourceDetail{}, err
		}
	}
	result, err := mutate(request.DryRun)
	s.auditGenericMutation(ctx, actor, mutationAction("update", request.DryRun), descriptor, namespace, name, err)
	if err != nil {
		return domain.GenericResourceDetail{}, err
	}
	return genericDetailFromObject(descriptor, result)
}

func (s *Service) Capabilities(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, namespace string) (domain.ResourceCapabilities, error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.ResourceCapabilities{}, err
	}
	if !descriptor.Namespaced {
		namespace = ""
	} else if namespace == "" {
		return domain.ResourceCapabilities{}, fmt.Errorf("%w: namespace is required for %s", domain.ErrInvalid, descriptor.Resource)
	}
	allowed := func(verb string) bool {
		if !supportsVerb(descriptor, verb) {
			return false
		}
		return s.authorizer.Require(ctx, actor, domain.Access{
			Verb: verb, APIGroup: descriptor.Group, Resource: descriptor.Resource, Namespace: namespace,
		}) == nil
	}
	capabilities := domain.ResourceCapabilities{
		Create: allowed("create"),
		Update: allowed("update") || allowed("patch"),
		Delete: allowed("delete"),
	}
	capabilities.RiskLevel, capabilities.RiskReasons, capabilities.RequireConfirmation = resourceRisk(descriptor)
	if isSecret(descriptor) {
		capabilities.Create = false
		capabilities.Update = false
	}
	return capabilities, nil
}

func (s *Service) Delete(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, namespace, name, resourceVersion, propagation, confirmation string) error {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return err
	}
	if !supportsVerb(descriptor, "delete") {
		return fmt.Errorf("%w: %s does not support delete", domain.ErrUnsupported, descriptor.Resource)
	}
	if name == "" {
		return fmt.Errorf("%w: resource name is required", domain.ErrInvalid)
	}
	if !descriptor.Namespaced {
		namespace = ""
	} else if namespace == "" {
		return fmt.Errorf("%w: namespace is required for %s", domain.ErrInvalid, descriptor.Resource)
	}
	if err := requireWriteConfirmation(descriptor, name, confirmation, false); err != nil {
		s.auditGenericMutation(ctx, actor, "delete-generic-resource", descriptor, namespace, name, err)
		return err
	}
	policy := domain.DeletePropagationBackground
	switch propagation {
	case "", "Background":
	case "Foreground":
		policy = domain.DeletePropagationForeground
	case "Orphan":
		policy = domain.DeletePropagationOrphan
	default:
		return fmt.Errorf("%w: unsupported deletion propagation %q", domain.ErrInvalid, propagation)
	}
	access := domain.Access{Verb: "delete", APIGroup: descriptor.Group, Resource: descriptor.Resource, Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditGenericMutation(ctx, actor, "delete-generic-resource", descriptor, namespace, name, err)
		return err
	}
	err = s.repository.DeleteGenericResource(ctx, descriptor, namespace, name, resourceVersion, policy)
	s.auditGenericMutation(ctx, actor, "delete-generic-resource", descriptor, namespace, name, err)
	return err
}

func (s *Service) validateSchema(ctx context.Context, descriptor domain.ResourceDescriptor, object map[string]any) error {
	schema, err := s.repository.GetResourceSchema(ctx, descriptor)
	if err != nil || schema.Source != "openapi-v2" || len(schema.Schema) == 0 {
		return nil
	}
	if problems := validateResourceObject(schema, object); len(problems) > 0 {
		return fmt.Errorf("%w: Schema 校验失败：%s", domain.ErrInvalid, strings.Join(problems, "；"))
	}
	return nil
}

func prepareGenericManifest(descriptor domain.ResourceDescriptor, request domain.GenericResourceMutation) (map[string]any, string, string, error) {
	if len(request.YAML) > maxGenericManifestBytes {
		return nil, "", "", fmt.Errorf("%w: yaml document must not exceed 1 MiB", domain.ErrInvalid)
	}
	decoder := yamlv3.NewDecoder(strings.NewReader(request.YAML))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, "", "", fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if len(object) == 0 {
		return nil, "", "", fmt.Errorf("%w: yaml document is required", domain.ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, "", "", fmt.Errorf("%w: exactly one yaml document is required", domain.ErrInvalid)
		}
		return nil, "", "", fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	expectedAPIVersion := descriptor.Version
	if descriptor.Group != "" {
		expectedAPIVersion = descriptor.Group + "/" + descriptor.Version
	}
	if objectString(object, "apiVersion") != expectedAPIVersion || objectString(object, "kind") != descriptor.Kind {
		return nil, "", "", fmt.Errorf("%w: yaml identity must be %s/%s", domain.ErrInvalid, expectedAPIVersion, descriptor.Kind)
	}
	metadata := objectMap(object, "metadata")
	if metadata == nil {
		return nil, "", "", fmt.Errorf("%w: metadata is required", domain.ErrInvalid)
	}
	name := objectString(metadata, "name")
	if name == "" {
		return nil, "", "", fmt.Errorf("%w: metadata.name is required", domain.ErrInvalid)
	}
	namespace := objectString(metadata, "namespace")
	if descriptor.Namespaced {
		if namespace == "" {
			namespace = request.Namespace
			if namespace != "" {
				metadata["namespace"] = namespace
			}
		}
		if namespace == "" {
			return nil, "", "", fmt.Errorf("%w: metadata.namespace is required", domain.ErrInvalid)
		}
		if request.Namespace != "" && request.Namespace != namespace {
			return nil, "", "", fmt.Errorf("%w: metadata.namespace must match the selected namespace", domain.ErrInvalid)
		}
	} else {
		if namespace != "" {
			return nil, "", "", fmt.Errorf("%w: cluster-scoped resources must not set metadata.namespace", domain.ErrInvalid)
		}
		namespace = ""
	}
	if request.ResourceVersion != "" {
		metadata["resourceVersion"] = request.ResourceVersion
	} else if objectString(metadata, "resourceVersion") == "" {
		delete(metadata, "resourceVersion")
	}
	return object, namespace, name, nil
}

func supportsVerb(descriptor domain.ResourceDescriptor, verb string) bool {
	for _, candidate := range descriptor.Verbs {
		if candidate == verb || candidate == "*" {
			return true
		}
	}
	return false
}

func mutationAction(operation string, dryRun bool) string {
	prefix := operation
	if dryRun {
		prefix = "dry-run"
	}
	return prefix + "-generic-resource"
}

func (s *Service) auditGenericMutation(ctx context.Context, actor domain.Actor, action string, descriptor domain.ResourceDescriptor, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: descriptor.Resource, Namespace: namespace, Name: name, Result: result, Err: err})
}

func genericDetailFromObject(descriptor domain.ResourceDescriptor, object map[string]any) (domain.GenericResourceDetail, error) {
	object = cloneObject(object)
	stripManagedFields(object)
	redacted := isSecret(descriptor)
	warnings := []string(nil)
	if redacted {
		redactSecret(object)
		warnings = append(warnings, "通用资源视图已遮蔽 Secret 数据；如需查看敏感值，请使用 Secret 专用页面")
	}
	detail := genericResourceDetail(object, redacted, warnings)
	encoded, err := yaml.Marshal(object)
	if err != nil {
		return domain.GenericResourceDetail{}, fmt.Errorf("%w: encode resource YAML: %v", domain.ErrUnavailable, err)
	}
	detail.YAML = string(encoded)
	return detail, nil
}

func (s *Service) descriptor(ctx context.Context, ref domain.ResourceReference) (domain.ResourceDescriptor, error) {
	if ref.Version == "" || ref.Resource == "" {
		return domain.ResourceDescriptor{}, fmt.Errorf("%w: version and resource are required", domain.ErrInvalid)
	}
	items, _, err := s.repository.DiscoverResources(ctx)
	if err != nil {
		return domain.ResourceDescriptor{}, err
	}
	for _, item := range items {
		if item.Group == ref.Group && item.Version == ref.Version && item.Resource == ref.Resource {
			return item, nil
		}
	}
	return domain.ResourceDescriptor{}, fmt.Errorf("%w: resource type is not discoverable", domain.ErrNotFound)
}

type searchScope struct {
	descriptor domain.ResourceDescriptor
	namespace  string
}

const (
	maxSearchTypes      = 24
	maxSearchNamespaces = 8
	maxSearchScopes     = 64
)

// Search performs a bounded cross-type search over the resource types the
// actor may list. A concrete namespace searches only that namespace, the
// special value "all" enumerates a bounded set of visible namespaces, and an
// empty namespace searches cluster-scoped resources only. Every namespaced
// type is still gated by a SelfSubjectRulesReview for its concrete scope.
func (s *Service) Search(ctx context.Context, actor domain.Actor, query, namespace string, limit int) (domain.GenericResourceSearch, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return domain.GenericResourceSearch{}, fmt.Errorf("%w: search requires at least two characters", domain.ErrInvalid)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	cacheKey := searchCacheKey(actor, query, namespace, limit)
	if cached, ok := s.cachedSearch(cacheKey); ok {
		return cached, nil
	}
	items, warnings, err := s.repository.DiscoverResources(ctx)
	if err != nil {
		return domain.GenericResourceSearch{}, err
	}
	items = preferredResourceTypes(items)
	allNamespaces := namespace == "all" || namespace == "*"
	if allNamespaces {
		namespace = ""
	}
	namespaceRules := make(map[string]domain.ResourceRules)
	namespaceOrder := make([]string, 0)
	if allNamespaces {
		names, listErr := s.repository.ListNamespaceNames(ctx)
		if listErr != nil {
			warnings = append(warnings, "无法枚举可见命名空间，已回退为仅搜索集群级资源")
		} else {
			if len(names) > maxSearchNamespaces {
				warnings = append(warnings, fmt.Sprintf("跨命名空间搜索最多覆盖 %d 个命名空间，其余命名空间已省略", maxSearchNamespaces))
				names = names[:maxSearchNamespaces]
			}
			for _, name := range names {
				rules, ruleWarnings, ruleErr := s.repository.ListResourceRules(ctx, actor, name)
				warnings = append(warnings, ruleWarnings...)
				if ruleErr != nil {
					warnings = append(warnings, fmt.Sprintf("无法读取命名空间 %s 的 RBAC 规则，该命名空间已跳过", name))
					continue
				}
				namespaceRules[name] = rules
				namespaceOrder = append(namespaceOrder, name)
			}
		}
	} else if namespace != "" {
		rules, ruleWarnings, ruleErr := s.repository.ListResourceRules(ctx, actor, namespace)
		warnings = append(warnings, ruleWarnings...)
		if ruleErr != nil {
			warnings = append(warnings, "无法读取当前身份的 RBAC 规则，搜索已按失败关闭策略停止")
			return domain.GenericResourceSearch{Items: []domain.GenericResourceSummary{}, Warnings: warnings}, nil
		}
		namespaceRules[namespace] = rules
		namespaceOrder = []string{namespace}
	}
	candidates := make([]domain.ResourceDescriptor, 0, len(items))
	clusterAuthFailed := false
	for _, item := range items {
		if item.Namespaced {
			listable := false
			for _, scope := range namespaceOrder {
				if descriptorListable(item, namespaceRules[scope], scope) {
					listable = true
					break
				}
			}
			if listable {
				candidates = append(candidates, item)
			}
			continue
		}
		if namespace == "" || allNamespaces {
			allowed, authErr := s.canListClusterResource(ctx, actor, item)
			if authErr != nil {
				clusterAuthFailed = true
				continue
			}
			if allowed {
				candidates = append(candidates, item)
			}
		}
	}
	if clusterAuthFailed {
		warnings = append(warnings, "无法完成集群级资源授权检查，相关资源类型未参与搜索")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := searchPriority(candidates[i]), searchPriority(candidates[j])
		if left != right {
			return left < right
		}
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Group < candidates[j].Group
	})
	truncated := len(candidates) > maxSearchTypes
	if truncated {
		candidates = candidates[:maxSearchTypes]
		warnings = append(warnings, fmt.Sprintf("仅搜索优先级最高的 %d 个已授权资源类型，请进入资源浏览器缩小范围", maxSearchTypes))
	}
	scopes := make([]searchScope, 0, len(candidates)*max(1, len(namespaceOrder)))
	for _, descriptor := range candidates {
		if !descriptor.Namespaced {
			scopes = append(scopes, searchScope{descriptor: descriptor})
			continue
		}
		for _, scope := range namespaceOrder {
			if descriptorListable(descriptor, namespaceRules[scope], scope) {
				scopes = append(scopes, searchScope{descriptor: descriptor, namespace: scope})
			}
		}
	}
	if len(scopes) > maxSearchScopes {
		scopes = scopes[:maxSearchScopes]
		truncated = true
		warnings = append(warnings, fmt.Sprintf("搜索范围达到 %d 个资源类型/命名空间组合上限，其余范围已省略", maxSearchScopes))
	}
	result := domain.GenericResourceSearch{Items: make([]domain.GenericResourceSummary, 0, limit), Warnings: warnings, Truncated: truncated}
	searchCtx, cancelSearch := context.WithCancel(ctx)
	defer cancelSearch()
	type scopeResult struct {
		items   []domain.GenericResourceSummary
		err     error
		skipped bool
	}
	results := make([]scopeResult, len(scopes))
	sem := make(chan struct{}, 4)
	var wait sync.WaitGroup
	var collected atomic.Int64
	var limitReached atomic.Bool
	for i, scope := range scopes {
		wait.Add(1)
		go func(index int, scope searchScope) {
			defer wait.Done()
			select {
			case sem <- struct{}{}:
			case <-searchCtx.Done():
				results[index] = scopeResult{skipped: true}
				return
			}
			defer func() { <-sem }()
			page, listErr := s.repository.ListGenericResources(searchCtx, scope.descriptor, domain.ListQuery{Namespace: scope.namespace, Search: query}, domain.PageRequest{Number: 1, Size: min(4, limit)})
			if listErr != nil {
				if limitReached.Load() && errors.Is(listErr, context.Canceled) {
					results[index] = scopeResult{skipped: true}
					return
				}
				results[index] = scopeResult{err: listErr}
				return
			}
			items := page.Items
			if isSecret(scope.descriptor) {
				for itemIndex := range items {
					items[itemIndex].Annotations = redactedAnnotationKeys(items[itemIndex].Annotations)
				}
			}
			results[index] = scopeResult{items: items}
			if int(collected.Add(int64(len(items)))) >= limit {
				limitReached.Store(true)
				cancelSearch()
			}
		}(i, scope)
	}
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return domain.GenericResourceSearch{}, err
	}
	if limitReached.Load() && !result.Truncated {
		result.Truncated = true
		result.Warnings = append(result.Warnings, "搜索已达到结果上限，未完成的查询已取消")
	}
	seen := make(map[string]struct{})
	for index, scope := range scopes {
		if results[index].err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s/%s%s 搜索失败", scope.descriptor.Group, scope.descriptor.Resource, namespaceSuffix(scope.namespace)))
			continue
		}
		if results[index].skipped {
			continue
		}
		for _, item := range results[index].items {
			if len(result.Items) >= limit {
				result.Truncated = true
				break
			}
			key := item.UID
			if key == "" {
				key = item.APIVersion + "\x00" + item.Resource + "\x00" + item.Namespace + "\x00" + item.Name
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result.Items = append(result.Items, item)
		}
	}
	s.storeSearch(cacheKey, result)
	return result, nil
}

func searchCacheKey(actor domain.Actor, query, namespace string, limit int) string {
	return actor.Username + "\x00" + strings.Join(actor.Groups, "\x00") + "\x00" + query + "\x00" + namespace + "\x00" + strconv.Itoa(limit)
}

func (s *Service) cachedSearch(key string) (domain.GenericResourceSearch, bool) {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	cached, ok := s.searchCache[key]
	if !ok || time.Since(cached.fetchedAt) >= resourceSearchTTL {
		if ok {
			delete(s.searchCache, key)
		}
		return domain.GenericResourceSearch{}, false
	}
	return cloneResourceSearch(cached.result), true
}

func (s *Service) storeSearch(key string, result domain.GenericResourceSearch) {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	now := time.Now()
	for cachedKey, cached := range s.searchCache {
		if now.Sub(cached.fetchedAt) >= resourceSearchTTL {
			delete(s.searchCache, cachedKey)
		}
	}
	s.searchCache[key] = cachedResourceSearch{result: cloneResourceSearch(result), fetchedAt: now}
}

func cloneResourceSearch(result domain.GenericResourceSearch) domain.GenericResourceSearch {
	result.Items = append([]domain.GenericResourceSummary(nil), result.Items...)
	result.Warnings = append([]string(nil), result.Warnings...)
	return result
}

func namespaceSuffix(namespace string) string {
	if namespace == "" {
		return ""
	}
	return " (命名空间 " + namespace + ")"
}

// preferredResourceTypes collapses served-version duplicates for cross-type
// search. Search results should not repeat the same logical object because the
// API group exposes both v1 and v1beta1 versions of a resource.
func preferredResourceTypes(items []domain.ResourceDescriptor) []domain.ResourceDescriptor {
	index := make(map[string]int)
	out := make([]domain.ResourceDescriptor, 0, len(items))
	for _, item := range items {
		key := item.Group + "\x00" + item.Resource
		position, exists := index[key]
		if !exists {
			index[key] = len(out)
			out = append(out, item)
			continue
		}
		if item.Preferred && !out[position].Preferred {
			out[position] = item
		}
	}
	return out
}

func descriptorListable(descriptor domain.ResourceDescriptor, rules domain.ResourceRules, namespace string) bool {
	if !descriptor.Namespaced || namespace == "" {
		return false
	}
	return rulesAllowUnboundedList(rules.Namespaced, descriptor)
}

func rulesAllowUnboundedList(rules []domain.ResourceRule, descriptor domain.ResourceDescriptor) bool {
	for _, rule := range rules {
		if !containsRuleValue(rule.Verbs, "list") || !containsRuleValue(rule.APIGroups, descriptor.Group) {
			continue
		}
		if !containsRuleValue(rule.Resources, descriptor.Resource) {
			continue
		}
		if !resourceNamesUnbounded(rule.ResourceNames) {
			continue
		}
		return true
	}
	return false
}

func (s *Service) canListClusterResource(ctx context.Context, actor domain.Actor, descriptor domain.ResourceDescriptor) (bool, error) {
	key := actor.Username + "\x00" + strings.Join(actor.Groups, "\x00") + "\x00" + descriptor.Group + "/" + descriptor.Resource
	s.clusterAuthMu.Lock()
	if cached, ok := s.clusterAuth[key]; ok && time.Since(cached.fetchedAt) < clusterListAccessTTL {
		s.clusterAuthMu.Unlock()
		return cached.allowed, nil
	}
	s.clusterAuthMu.Unlock()

	err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", APIGroup: descriptor.Group, Resource: descriptor.Resource})
	if err != nil && !errors.Is(err, domain.ErrForbidden) {
		return false, err
	}
	allowed := err == nil
	s.clusterAuthMu.Lock()
	for cachedKey, cached := range s.clusterAuth {
		if time.Since(cached.fetchedAt) >= clusterListAccessTTL {
			delete(s.clusterAuth, cachedKey)
		}
	}
	s.clusterAuth[key] = cachedClusterListAccess{allowed: allowed, fetchedAt: time.Now()}
	s.clusterAuthMu.Unlock()
	return allowed, nil
}

func resourceNamesUnbounded(resourceNames []string) bool {
	for _, name := range resourceNames {
		if name != "*" {
			return false
		}
	}
	return true
}

func containsRuleValue(values []string, want string) bool {
	for _, value := range values {
		if value == "*" || value == want {
			return true
		}
	}
	return false
}

func searchPriority(descriptor domain.ResourceDescriptor) int {
	key := descriptor.Group + "/" + descriptor.Resource
	priority := map[string]int{
		"/pods": 0, "apps/deployments": 1, "apps/statefulsets": 2, "apps/daemonsets": 3,
		"batch/jobs": 4, "batch/cronjobs": 5, "/services": 6, "networking.k8s.io/ingresses": 7,
		"/configmaps": 8, "/secrets": 9, "/persistentvolumeclaims": 10,
		"/namespaces": 11, "/nodes": 12,
	}
	if value, ok := priority[key]; ok {
		return value
	}
	for _, category := range descriptor.Categories {
		if category == "all" {
			return 20
		}
	}
	if descriptor.Group == "" {
		return 30
	}
	return 40
}

func redactedAnnotationKeys(annotations map[string]string) map[string]string {
	if len(annotations) == 0 {
		return nil
	}
	out := make(map[string]string, len(annotations))
	for key := range annotations {
		out[key] = "[redacted]"
	}
	return out
}

func (s *Service) auditSecret(ctx context.Context, actor domain.Actor, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: "get-generic-secret", Resource: "secret", Namespace: namespace, Name: name, Result: result, Err: err,
	})
}

func isSecret(descriptor domain.ResourceDescriptor) bool {
	return descriptor.Group == "" && descriptor.Resource == "secrets"
}

func redactSecret(object map[string]any) {
	delete(object, "data")
	delete(object, "stringData")
	metadata, _ := object["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	for key := range annotations {
		annotations[key] = "[redacted]"
	}
}

func stripManagedFields(object map[string]any) {
	metadata, _ := object["metadata"].(map[string]any)
	delete(metadata, "managedFields")
}

func cloneObject(object map[string]any) map[string]any {
	if object == nil {
		return map[string]any{}
	}
	cloned, ok := cloneValue(object).(map[string]any)
	if !ok || cloned == nil {
		return object
	}
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func genericResourceDetail(object map[string]any, redacted bool, warnings []string) domain.GenericResourceDetail {
	metadata := objectMap(object, "metadata")
	return domain.GenericResourceDetail{
		APIVersion:      objectString(object, "apiVersion"),
		Kind:            objectString(object, "kind"),
		Name:            objectString(metadata, "name"),
		Namespace:       objectString(metadata, "namespace"),
		UID:             objectString(metadata, "uid"),
		ResourceVersion: objectString(metadata, "resourceVersion"),
		CreatedAt:       objectTime(metadata, "creationTimestamp"),
		Labels:          objectStringMap(metadata, "labels"),
		Annotations:     objectStringMap(metadata, "annotations"),
		Object:          object,
		Redacted:        redacted,
		Warnings:        warnings,
	}
}

func objectMap(object map[string]any, key string) map[string]any {
	value, _ := object[key].(map[string]any)
	return value
}

func objectString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func objectTime(object map[string]any, key string) time.Time {
	value := objectString(object, key)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func objectStringMap(object map[string]any, key string) map[string]string {
	raw, _ := object[key].(map[string]any)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for name, value := range raw {
		if text, ok := value.(string); ok {
			out[name] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
