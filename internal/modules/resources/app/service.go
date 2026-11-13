package app

import (
	"context"
	"fmt"
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
	ListGenericResources(ctx context.Context, descriptor domain.ResourceDescriptor, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.GenericResourceSummary], error)
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

func (s *Service) Discovery(ctx context.Context) ([]domain.ResourceDescriptor, []string, error) {
	return s.repository.DiscoverResources(ctx)
}

func (s *Service) List(ctx context.Context, actor domain.Actor, ref domain.ResourceReference, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.GenericResourceSummary], error) {
	descriptor, err := s.descriptor(ctx, ref)
	if err != nil {
		return domain.PageResult[domain.GenericResourceSummary]{}, err
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
		return domain.PageResult[domain.GenericResourceSummary]{}, err
	}
	return s.repository.ListGenericResources(ctx, descriptor, query, page)
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
