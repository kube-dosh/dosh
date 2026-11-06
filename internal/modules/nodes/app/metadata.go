package app

import (
	"fmt"
	"regexp"
	"strings"

	"dosh/internal/domain"
)

const maxNodeMetadataEntries = 64

var (
	nodeLabelNamePattern   = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
	nodeLabelValuePattern  = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)
	nodeLabelPrefixPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

var nodeTaintEffects = map[string]struct{}{
	"NoSchedule":       {},
	"PreferNoSchedule": {},
	"NoExecute":        {},
}

func normalizeNodeLabels(labels map[string]string) (map[string]string, error) {
	if len(labels) > maxNodeMetadataEntries {
		return nil, nodeValidationError("labels", "at most %d labels are allowed", maxNodeMetadataEntries)
	}
	normalized := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if err := validateQualifiedName("labels", key); err != nil {
			return nil, err
		}
		if len(value) > 63 || !nodeLabelValuePattern.MatchString(value) {
			return nil, nodeValidationError("labels", "label %q has an invalid value", key)
		}
		if _, exists := normalized[key]; exists {
			return nil, nodeValidationError("labels", "label key %q is duplicated", key)
		}
		normalized[key] = value
	}
	return normalized, nil
}

func normalizeNodeTaints(taints []domain.Taint) ([]domain.Taint, error) {
	if len(taints) > maxNodeMetadataEntries {
		return nil, nodeValidationError("taints", "at most %d taints are allowed", maxNodeMetadataEntries)
	}
	normalized := make([]domain.Taint, 0, len(taints))
	seen := make(map[string]struct{}, len(taints))
	for index, taint := range taints {
		taint.Key = strings.TrimSpace(taint.Key)
		taint.Value = strings.TrimSpace(taint.Value)
		taint.Effect = strings.TrimSpace(taint.Effect)
		if err := validateQualifiedName("taints", taint.Key); err != nil {
			return nil, nodeValidationError("taints", "taint %d has an invalid key", index+1)
		}
		if len(taint.Value) > 63 || !nodeLabelValuePattern.MatchString(taint.Value) {
			return nil, nodeValidationError("taints", "taint %q has an invalid value", taint.Key)
		}
		if _, ok := nodeTaintEffects[taint.Effect]; !ok {
			return nil, nodeValidationError("taints", "taint %q has an invalid effect", taint.Key)
		}
		identity := taint.Key + "\x00" + taint.Effect
		if _, exists := seen[identity]; exists {
			return nil, nodeValidationError("taints", "taint %q with effect %s is duplicated", taint.Key, taint.Effect)
		}
		seen[identity] = struct{}{}
		normalized = append(normalized, taint)
	}
	return normalized, nil
}

func validateQualifiedName(field, value string) error {
	if value == "" || len(value) > 253 {
		return nodeValidationError(field, "key must be a valid qualified name")
	}
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return nodeValidationError(field, "key %q contains more than one slash", value)
	}
	name := parts[0]
	if len(parts) == 2 {
		prefix := parts[0]
		if prefix == "" || len(prefix) > 253 || !nodeLabelPrefixPattern.MatchString(prefix) {
			return nodeValidationError(field, "key prefix %q is invalid", prefix)
		}
		name = parts[1]
	}
	if name == "" || len(name) > 63 || !nodeLabelNamePattern.MatchString(name) {
		return nodeValidationError(field, "key name %q is invalid", name)
	}
	return nil
}

func nodeValidationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
