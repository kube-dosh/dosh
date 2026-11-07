package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"dosh/internal/domain"
)

const maxConfigurationDataBytes = 1 << 20

var configurationNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func (s *Service) CreateConfigMap(ctx context.Context, actor domain.Actor, spec domain.ConfigMapCreateSpec) (domain.ConfigMapDetail, error) {
	if err := normalizeConfigMapCreateSpec(&spec); err != nil {
		s.auditWrite(ctx, actor, "create-configmap", spec.Namespace, spec.Name, err)
		return domain.ConfigMapDetail{}, err
	}
	if err := s.authorizeWrite(ctx, actor, "create-configmap", "configmaps", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.ConfigMapDetail{}, err
	}
	detail, err := s.repository.CreateConfigMap(ctx, spec)
	s.auditWrite(ctx, actor, "create-configmap", spec.Namespace, spec.Name, err)
	return detail, err
}

func (s *Service) UpdateConfigMap(ctx context.Context, actor domain.Actor, namespace, name string, update domain.ConfigMapUpdateSpec) (domain.ConfigMapDetail, error) {
	if err := normalizeConfigMapUpdateSpec(namespace, name, &update); err != nil {
		s.auditWrite(ctx, actor, "update-configmap", namespace, name, err)
		return domain.ConfigMapDetail{}, err
	}
	if err := s.authorizeWrite(ctx, actor, "update-configmap", "configmaps", namespace, name, "update"); err != nil {
		return domain.ConfigMapDetail{}, err
	}
	detail, err := s.repository.UpdateConfigMap(ctx, namespace, name, update)
	s.auditWrite(ctx, actor, "update-configmap", namespace, name, err)
	return detail, err
}

func (s *Service) DeleteConfigMap(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validateConfigurationIdentity(namespace, name); err != nil {
		s.auditWrite(ctx, actor, "delete-configmap", namespace, name, err)
		return err
	}
	if err := s.authorizeWrite(ctx, actor, "delete-configmap", "configmaps", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeleteConfigMap(ctx, namespace, name, resourceVersion)
	s.auditWrite(ctx, actor, "delete-configmap", namespace, name, err)
	return err
}

func (s *Service) CreateSecret(ctx context.Context, actor domain.Actor, spec domain.SecretWriteSpec) (domain.SecretSummary, error) {
	if err := normalizeSecretWriteSpec("", "", &spec, true); err != nil {
		s.auditWrite(ctx, actor, "create-secret", spec.Namespace, spec.Name, err)
		return domain.SecretSummary{}, err
	}
	if err := s.authorizeWrite(ctx, actor, "create-secret", "secrets", spec.Namespace, spec.Name, "create"); err != nil {
		return domain.SecretSummary{}, err
	}
	summary, err := s.repository.CreateSecret(ctx, spec)
	s.auditWrite(ctx, actor, "create-secret", spec.Namespace, spec.Name, err)
	return summary, err
}

func (s *Service) UpdateSecret(ctx context.Context, actor domain.Actor, namespace, name string, update domain.SecretWriteSpec) (domain.SecretSummary, error) {
	if err := normalizeSecretWriteSpec(namespace, name, &update, false); err != nil {
		s.auditWrite(ctx, actor, "update-secret", namespace, name, err)
		return domain.SecretSummary{}, err
	}
	if err := s.authorizeWrite(ctx, actor, "update-secret", "secrets", namespace, name, "update"); err != nil {
		return domain.SecretSummary{}, err
	}
	summary, err := s.repository.UpdateSecret(ctx, namespace, name, update)
	s.auditWrite(ctx, actor, "update-secret", namespace, name, err)
	return summary, err
}

func (s *Service) DeleteSecret(ctx context.Context, actor domain.Actor, namespace, name, resourceVersion string) error {
	if err := validateConfigurationIdentity(namespace, name); err != nil {
		s.auditWrite(ctx, actor, "delete-secret", namespace, name, err)
		return err
	}
	if err := s.authorizeWrite(ctx, actor, "delete-secret", "secrets", namespace, name, "delete"); err != nil {
		return err
	}
	err := s.repository.DeleteSecret(ctx, namespace, name, resourceVersion)
	s.auditWrite(ctx, actor, "delete-secret", namespace, name, err)
	return err
}

func (s *Service) authorizeWrite(ctx context.Context, actor domain.Actor, action, resource, namespace, name, verb string) error {
	access := domain.Access{Verb: verb, Resource: resource, Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.auditWrite(ctx, actor, action, namespace, name, err)
		return err
	}
	return nil
}

func (s *Service) auditWrite(ctx context.Context, actor domain.Actor, action, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	resource := "configmap"
	if strings.Contains(action, "secret") {
		resource = "secret"
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: resource, Namespace: namespace, Name: name, Result: result, Err: err})
}

func validateConfigurationIdentity(namespace, name string) error {
	if namespace == "" || len(namespace) > 63 || !configurationNamePattern.MatchString(namespace) {
		return configurationValidationError("namespace", "namespace must be a valid DNS-1123 label")
	}
	if name == "" || len(name) > 253 || !configurationNamePattern.MatchString(name) {
		return configurationValidationError("name", "name must be a valid DNS-1123 label")
	}
	return nil
}

func normalizeConfigMapCreateSpec(spec *domain.ConfigMapCreateSpec) error {
	if err := validateConfigurationIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	return normalizeConfigMapData(&spec.Data, &spec.Annotations)
}

func normalizeConfigMapUpdateSpec(namespace, name string, update *domain.ConfigMapUpdateSpec) error {
	if err := validateConfigurationIdentity(namespace, name); err != nil {
		return err
	}
	return normalizeConfigMapData(&update.Data, &update.Annotations)
}

func normalizeConfigMapData(data *map[string]string, annotations *map[string]string) error {
	if *data == nil {
		*data = map[string]string{}
	}
	total := 0
	for key, value := range *data {
		if err := validateConfigurationKey(key); err != nil {
			return err
		}
		if !utf8.ValidString(value) {
			return configurationValidationError("data", "ConfigMap value %q must be valid UTF-8", key)
		}
		total += len(key) + len(value)
	}
	if total > maxConfigurationDataBytes {
		return configurationValidationError("data", "ConfigMap data must not exceed 1 MiB")
	}
	normalizeAnnotations(annotations)
	return nil
}

func normalizeSecretWriteSpec(namespace, name string, spec *domain.SecretWriteSpec, creating bool) error {
	if !creating {
		spec.Namespace, spec.Name = namespace, name
	}
	if err := validateConfigurationIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	spec.Type = strings.TrimSpace(spec.Type)
	if spec.Type == "" {
		spec.Type = "Opaque"
	}
	if len(spec.Type) > 253 {
		return configurationValidationError("type", "type must not exceed 253 characters")
	}
	if spec.Data == nil {
		spec.Data = map[string]string{}
	}
	total := 0
	for key, value := range spec.Data {
		if err := validateConfigurationKey(key); err != nil {
			return err
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return configurationValidationError("data", "Secret value %q must be valid Base64", key)
		}
		total += len(key) + len(decoded)
	}
	if total > maxConfigurationDataBytes {
		return configurationValidationError("data", "Secret data must not exceed 1 MiB")
	}
	normalizeAnnotations(&spec.Annotations)
	return nil
}

func normalizeAnnotations(annotations *map[string]string) {
	normalized := make(map[string]string, len(*annotations))
	for key, value := range *annotations {
		key = strings.TrimSpace(key)
		if key != "" && len(key) <= 253 {
			normalized[key] = value
		}
	}
	*annotations = normalized
}

func validateConfigurationKey(key string) error {
	if strings.TrimSpace(key) != key || key == "" || len(key) > 253 {
		return configurationValidationError("data", "data keys must be non-empty, trimmed, and no longer than 253 characters")
	}
	if strings.ContainsAny(key, "\r\n\t") {
		return configurationValidationError("data", "data key %q contains control characters", key)
	}
	return nil
}

func configurationValidationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
