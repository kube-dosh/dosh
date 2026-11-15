package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"dosh/internal/domain"

	"sigs.k8s.io/yaml"
)

type Repository interface {
	ListHelmReleases(ctx context.Context, namespace string) ([]domain.HelmReleaseSummary, []string, error)
	GetHelmRelease(ctx context.Context, namespace, name string) (domain.HelmReleaseDetail, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) List(ctx context.Context, actor domain.Actor, namespace string) ([]domain.HelmReleaseSummary, []string, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, nil, fmt.Errorf("%w: namespace is required for Helm releases", domain.ErrInvalid)
	}
	if err := s.authorize(ctx, actor, namespace); err != nil {
		s.audit(ctx, actor, "list-helm-releases", namespace, "", err)
		return nil, nil, err
	}
	items, warnings, err := s.repository.ListHelmReleases(ctx, namespace)
	s.audit(ctx, actor, "list-helm-releases", namespace, "", err)
	return items, warnings, err
}

func (s *Service) Get(ctx context.Context, actor domain.Actor, namespace, name string) (domain.HelmReleaseDetail, error) {
	if strings.TrimSpace(name) == "" {
		return domain.HelmReleaseDetail{}, fmt.Errorf("%w: Helm release name is required", domain.ErrInvalid)
	}
	if err := s.authorize(ctx, actor, namespace); err != nil {
		s.audit(ctx, actor, "get-helm-release", namespace, name, err)
		return domain.HelmReleaseDetail{}, err
	}
	detail, err := s.repository.GetHelmRelease(ctx, namespace, name)
	if err != nil {
		s.audit(ctx, actor, "get-helm-release", namespace, name, err)
		return domain.HelmReleaseDetail{}, err
	}
	detail.Values = redactValues(detail.Values)
	manifest, manifestErr := redactManifest(detail.Manifest)
	if manifestErr != nil {
		detail.Warnings = append(detail.Warnings, "manifest 解析失败，已隐藏完整内容")
		detail.Manifest = "# manifest 无法安全解析，内容已隐藏"
	} else {
		detail.Manifest = manifest
	}
	s.audit(ctx, actor, "get-helm-release", namespace, name, nil)
	return detail, nil
}

func (s *Service) authorize(ctx context.Context, actor domain.Actor, namespace string) error {
	// Helm stores release payloads as Secrets. Requiring list Secrets is
	// intentionally stronger than a generic resource read because the payload
	// contains chart values and rendered manifests.
	return s.authorizer.Require(ctx, actor, domain.Access{Verb: "list", Resource: "secrets", Namespace: namespace})
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: "helmrelease", Namespace: namespace, Name: name, Result: result, Err: err})
}

var sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[-_]?key|private[-_]?key|client[-_]?secret|access[-_]?key|credential)`)

func redactValues(raw string) string {
	var object map[string]any
	if err := yaml.Unmarshal([]byte(raw), &object); err != nil {
		return "# values 解析失败，内容已隐藏"
	}
	maskSensitive(object)
	encoded, err := yaml.Marshal(object)
	if err != nil {
		return "# values 无法安全序列化，内容已隐藏"
	}
	return string(encoded)
}

func redactManifest(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	documents := strings.Split(raw, "\n---")
	redacted := make([]string, 0, len(documents))
	for _, document := range documents {
		document = strings.TrimSpace(strings.TrimPrefix(document, "---"))
		if document == "" {
			continue
		}
		var object map[string]any
		if err := yaml.Unmarshal([]byte(document), &object); err != nil {
			return "", err
		}
		maskSensitive(object)
		if strings.EqualFold(stringValue(object["kind"]), "Secret") {
			for _, key := range []string{"data", "stringData"} {
				if values, ok := object[key].(map[string]any); ok {
					for name := range values {
						values[name] = "<redacted>"
					}
				}
			}
		}
		encoded, err := yaml.Marshal(object)
		if err != nil {
			return "", err
		}
		redacted = append(redacted, strings.TrimSpace(string(encoded)))
	}
	return strings.Join(redacted, "\n---\n"), nil
}

func maskSensitive(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if name, ok := typed["name"].(string); ok && sensitiveKey.MatchString(name) {
			if _, hasValue := typed["value"]; hasValue {
				typed["value"] = "<redacted>"
			}
		}
		for key, nested := range typed {
			if sensitiveKey.MatchString(key) {
				typed[key] = "<redacted>"
				continue
			}
			maskSensitive(nested)
		}
	case []any:
		for _, nested := range typed {
			maskSensitive(nested)
		}
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
