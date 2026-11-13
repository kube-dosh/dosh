package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"

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
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
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
	for _, item := range items {
		if descriptorListable(item, rules, namespace) {
			filtered = append(filtered, item)
		}
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
	if redacted {
		s.auditSecret(ctx, actor, namespace, name, nil)
	}
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
	rules, ruleWarnings, err := s.repository.ListResourceRules(ctx, actor, namespace)
	warnings = append(warnings, ruleWarnings...)
	if err != nil {
		warnings = append(warnings, "无法读取当前身份的 RBAC 规则，搜索已按失败关闭策略停止")
		return domain.GenericResourceSearch{Items: []domain.GenericResourceSummary{}, Warnings: warnings}, nil
	}
	candidates := make([]domain.ResourceDescriptor, 0, len(items))
	for _, item := range items {
		if item.Namespaced && namespace == "" {
			continue
		}
		if descriptorListable(item, rules, namespace) {
			candidates = append(candidates, item)
		}
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
	scopedRules := rules.Cluster
	if descriptor.Namespaced {
		if namespace == "" {
			return false
		}
		scopedRules = rules.Namespaced
	}
	for _, rule := range scopedRules {
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
