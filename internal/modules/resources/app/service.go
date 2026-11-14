package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
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
	ListResourceRules(ctx context.Context, actor domain.Actor, namespace string) (domain.ResourceRules, []string, error)
	ListGenericResources(ctx context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.GenericResourcePage, error)
	GetGenericResource(ctx context.Context, descriptor domain.ResourceDescriptor, namespace, name string) (map[string]any, error)
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
}

const clusterListAccessTTL = 30 * time.Second
const maxGenericManifestBytes = 1 << 20

type cachedClusterListAccess struct {
	allowed   bool
	fetchedAt time.Time
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor, clusterAuth: make(map[string]cachedClusterListAccess)}
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
	if isSecret(descriptor) {
		capabilities.Create = false
		capabilities.Update = false
	}
	return capabilities, nil
}

func (s *Service) Delete(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, namespace, name, resourceVersion, propagation string) error {
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

// Search performs a bounded cross-type search over the resource types the
// actor may list. Cluster-scoped resources are searched when namespace is
// empty; namespaced resources require an explicit namespace so the
// SelfSubjectRulesReview decision has a concrete authorization scope.
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
	items, warnings, err := s.repository.DiscoverResources(ctx)
	if err != nil {
		return domain.GenericResourceSearch{}, err
	}
	candidates := make([]domain.ResourceDescriptor, 0, len(items))
	clusterAuthFailed := false
	if namespace == "" {
		for _, item := range items {
			if item.Namespaced {
				continue
			}
			allowed, authErr := s.canListClusterResource(ctx, actor, item)
			if authErr != nil {
				clusterAuthFailed = true
				continue
			}
			if allowed {
				candidates = append(candidates, item)
			}
		}
	} else {
		rules, ruleWarnings, err := s.repository.ListResourceRules(ctx, actor, namespace)
		warnings = append(warnings, ruleWarnings...)
		if err != nil {
			warnings = append(warnings, "无法读取当前身份的 RBAC 规则，搜索已按失败关闭策略停止")
			return domain.GenericResourceSearch{Items: []domain.GenericResourceSummary{}, Warnings: warnings}, nil
		}
		for _, item := range items {
			if item.Namespaced && descriptorListable(item, rules, namespace) {
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
	const maxSearchTypes = 16
	truncated := len(candidates) > maxSearchTypes
	if truncated {
		candidates = candidates[:maxSearchTypes]
		warnings = append(warnings, "仅搜索优先级最高的 16 个已授权资源类型，请进入资源浏览器缩小范围")
	}
	result := domain.GenericResourceSearch{Items: make([]domain.GenericResourceSummary, 0, limit), Warnings: warnings, Truncated: truncated}
	type typeResult struct {
		items []domain.GenericResourceSummary
		err   error
	}
	results := make([]typeResult, len(candidates))
	sem := make(chan struct{}, 4)
	var wait sync.WaitGroup
	for i, descriptor := range candidates {
		wait.Add(1)
		go func(index int, descriptor domain.ResourceDescriptor) {
			defer wait.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			scope := ""
			if descriptor.Namespaced {
				scope = namespace
			}
			page, listErr := s.repository.ListGenericResources(ctx, descriptor, domain.ListQuery{Namespace: scope, Search: query}, domain.PageRequest{Number: 1, Size: min(4, limit)})
			if listErr != nil {
				results[index] = typeResult{err: listErr}
				return
			}
			items := page.Items
			if isSecret(descriptor) {
				for itemIndex := range items {
					items[itemIndex].Annotations = redactedAnnotationKeys(items[itemIndex].Annotations)
				}
			}
			results[index] = typeResult{items: items}
		}(i, descriptor)
	}
	wait.Wait()
	for index, candidate := range candidates {
		if results[index].err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s/%s 搜索失败", candidate.Group, candidate.Resource))
			continue
		}
		for _, item := range results[index].items {
			if len(result.Items) >= limit {
				result.Truncated = true
				break
			}
			result.Items = append(result.Items, item)
		}
	}
	return result, nil
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
