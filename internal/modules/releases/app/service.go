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
	GetHelmReleaseRevision(ctx context.Context, namespace, name string, revision int) (domain.HelmReleaseDetail, error)
	RollbackHelmRelease(ctx context.Context, namespace, name string, targetRevision int) (domain.HelmReleaseRollbackResult, error)
	PreviewHelmUpgrade(ctx context.Context, namespace, name, values string) (domain.HelmReleaseUpgradeRender, error)
	UpgradeHelmRelease(ctx context.Context, namespace, name, values string) (domain.HelmReleaseUpgradeResult, error)
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

func (s *Service) Diff(ctx context.Context, actor domain.Actor, namespace, name string, fromRevision, toRevision int) (domain.HelmReleaseDiff, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domain.HelmReleaseDiff{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	if fromRevision <= 0 || toRevision <= 0 || fromRevision == toRevision {
		return domain.HelmReleaseDiff{}, fmt.Errorf("%w: from and to revisions must be positive and different", domain.ErrInvalid)
	}
	if err := s.authorize(ctx, actor, namespace); err != nil {
		s.audit(ctx, actor, "diff-helm-release", namespace, name, err)
		return domain.HelmReleaseDiff{}, err
	}
	from, err := s.repository.GetHelmReleaseRevision(ctx, namespace, name, fromRevision)
	if err != nil {
		s.audit(ctx, actor, "diff-helm-release", namespace, name, err)
		return domain.HelmReleaseDiff{}, err
	}
	to, err := s.repository.GetHelmReleaseRevision(ctx, namespace, name, toRevision)
	if err != nil {
		s.audit(ctx, actor, "diff-helm-release", namespace, name, err)
		return domain.HelmReleaseDiff{}, err
	}
	from, to = s.redactDetail(from), s.redactDetail(to)
	result := domain.HelmReleaseDiff{
		Namespace: namespace, Name: name, From: from.HelmReleaseSummary, To: to.HelmReleaseSummary,
		ValuesDiff:   unifiedDiff(from.Values, to.Values, fmt.Sprintf("values rev %d", fromRevision), fmt.Sprintf("values rev %d", toRevision)),
		ManifestDiff: unifiedDiff(from.Manifest, to.Manifest, fmt.Sprintf("manifest rev %d", fromRevision), fmt.Sprintf("manifest rev %d", toRevision)),
		Warnings:     append(append([]string(nil), from.Warnings...), to.Warnings...),
	}
	s.audit(ctx, actor, "diff-helm-release", namespace, name, nil)
	return result, nil
}

func (s *Service) Rollback(ctx context.Context, actor domain.Actor, namespace, name string, targetRevision int, confirmation string) (domain.HelmReleaseRollbackResult, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domain.HelmReleaseRollbackResult{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	if targetRevision <= 0 {
		return domain.HelmReleaseRollbackResult{}, fmt.Errorf("%w: target revision must be positive", domain.ErrInvalid)
	}
	if confirmation != name {
		err := fmt.Errorf("%w: release name confirmation does not match", domain.ErrInvalid)
		s.audit(ctx, actor, "rollback-helm-release", namespace, name, err)
		return domain.HelmReleaseRollbackResult{}, err
	}
	// Helm stores its release record in a Secret and may update that Secret
	// multiple times during rollback. Requiring both verbs keeps the write
	// permission explicit even though the API server remains authoritative for
	// every manifest object applied by the Helm-compatible action.
	for _, verb := range []string{"update", "create"} {
		if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: verb, Resource: "secrets", Namespace: namespace}); err != nil {
			s.audit(ctx, actor, "rollback-helm-release", namespace, name, err)
			return domain.HelmReleaseRollbackResult{}, err
		}
	}
	result, err := s.repository.RollbackHelmRelease(ctx, namespace, name, targetRevision)
	s.audit(ctx, actor, "rollback-helm-release", namespace, name, err)
	return result, err
}

// PreviewUpgrade renders a candidate upgrade against the chart stored in the
// current Helm revision. The current server-side values are used as the merge
// base; the browser never has to round-trip redacted values back to the server.
func (s *Service) PreviewUpgrade(ctx context.Context, actor domain.Actor, namespace, name, values string) (domain.HelmReleaseUpgradePreview, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domain.HelmReleaseUpgradePreview{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	if len(values) > 1<<20 {
		return domain.HelmReleaseUpgradePreview{}, fmt.Errorf("%w: values must not exceed 1 MiB", domain.ErrInvalid)
	}
	if err := s.authorize(ctx, actor, namespace); err != nil {
		s.audit(ctx, actor, "preview-helm-upgrade", namespace, name, err)
		return domain.HelmReleaseUpgradePreview{}, err
	}
	render, err := s.repository.PreviewHelmUpgrade(ctx, namespace, name, values)
	if err != nil {
		s.audit(ctx, actor, "preview-helm-upgrade", namespace, name, err)
		return domain.HelmReleaseUpgradePreview{}, err
	}
	currentValues := redactValues(render.CurrentValues)
	nextValues := redactValues(render.NextValues)
	currentManifest, currentErr := redactManifest(render.CurrentManifest)
	nextManifest, nextErr := redactManifest(render.NextManifest)
	warnings := append([]string(nil), render.Warnings...)
	if currentErr != nil {
		warnings = append(warnings, "当前版本 manifest 无法安全解析，Manifest Diff 已隐藏")
		currentManifest = "# manifest 无法安全解析，内容已隐藏"
	}
	if nextErr != nil {
		warnings = append(warnings, "升级后 manifest 无法安全解析，Manifest Diff 已隐藏")
		nextManifest = "# manifest 无法安全解析，内容已隐藏"
	}
	warnings = append(warnings, "升级预览不会修改集群；Helm Hook 在正式升级时不会执行")
	result := domain.HelmReleaseUpgradePreview{
		Namespace: namespace, Name: name, CurrentRevision: render.CurrentRevision, NextRevision: render.NextRevision,
		ValuesDiff:   unifiedDiff(currentValues, nextValues, fmt.Sprintf("values rev %d", render.CurrentRevision), "values 升级后"),
		ManifestDiff: unifiedDiff(currentManifest, nextManifest, fmt.Sprintf("manifest rev %d", render.CurrentRevision), "manifest 升级后"),
		Warnings:     warnings,
	}
	s.audit(ctx, actor, "preview-helm-upgrade", namespace, name, nil)
	return result, nil
}

// Upgrade applies a server-rendered chart revision after confirming both the
// release name and the ability to write Helm's Secret-backed history.
func (s *Service) Upgrade(ctx context.Context, actor domain.Actor, namespace, name, values, confirmation string) (domain.HelmReleaseUpgradeResult, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domain.HelmReleaseUpgradeResult{}, fmt.Errorf("%w: namespace and Helm release name are required", domain.ErrInvalid)
	}
	if len(values) > 1<<20 {
		return domain.HelmReleaseUpgradeResult{}, fmt.Errorf("%w: values must not exceed 1 MiB", domain.ErrInvalid)
	}
	if confirmation != name {
		err := fmt.Errorf("%w: release name confirmation does not match", domain.ErrInvalid)
		s.audit(ctx, actor, "upgrade-helm-release", namespace, name, err)
		return domain.HelmReleaseUpgradeResult{}, err
	}
	for _, verb := range []string{"update", "create"} {
		if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: verb, Resource: "secrets", Namespace: namespace}); err != nil {
			s.audit(ctx, actor, "upgrade-helm-release", namespace, name, err)
			return domain.HelmReleaseUpgradeResult{}, err
		}
	}
	result, err := s.repository.UpgradeHelmRelease(ctx, namespace, name, values)
	s.audit(ctx, actor, "upgrade-helm-release", namespace, name, err)
	return result, err
}

func (s *Service) redactDetail(detail domain.HelmReleaseDetail) domain.HelmReleaseDetail {
	detail.Values = redactValues(detail.Values)
	manifest, err := redactManifest(detail.Manifest)
	if err != nil {
		detail.Warnings = append(detail.Warnings, "manifest 解析失败，已隐藏完整内容")
		detail.Manifest = "# manifest 无法安全解析，内容已隐藏"
	} else {
		detail.Manifest = manifest
	}
	return detail
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
