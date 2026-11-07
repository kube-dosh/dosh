package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

const maxIngressPaths = 50

var (
	ingressHostPattern    = regexp.MustCompile(`^(\*\.)?[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	ingressPathTypeValues = map[string]struct{}{"Exact": {}, "Prefix": {}, "ImplementationSpecific": {}}
)

func normalizeIngressCreateSpec(spec *domain.IngressCreateSpec) error {
	if err := validateIngressIdentity(spec.Namespace, spec.Name); err != nil {
		return err
	}
	return normalizeIngressSpec(&spec.Class, &spec.Paths, &spec.TLS, &spec.Annotations)
}

func normalizeIngressUpdateSpec(namespace, name string, update *domain.IngressUpdateSpec) error {
	if err := validateIngressIdentity(namespace, name); err != nil {
		return err
	}
	return normalizeIngressSpec(&update.Class, &update.Paths, &update.TLS, &update.Annotations)
}

func validateIngressIdentity(namespace, name string) error {
	if namespace == "" || len(namespace) > 63 || !dnsLabelPattern.MatchString(namespace) {
		return ingressValidationError("namespace", "namespace must be a valid DNS-1123 label")
	}
	if name == "" || len(name) > 253 || !dnsLabelPattern.MatchString(name) {
		return ingressValidationError("name", "name must be a valid DNS-1123 label")
	}
	return nil
}

func normalizeIngressSpec(class *string, paths *[]domain.IngressPathInput, tls *[]domain.IngressTLS, annotations *map[string]string) error {
	*class = strings.TrimSpace(*class)
	if len(*class) > 253 || (*class != "" && !dnsLabelPattern.MatchString(*class)) {
		return ingressValidationError("class", "class must be a valid DNS-1123 label")
	}
	if len(*paths) == 0 || len(*paths) > maxIngressPaths {
		return ingressValidationError("paths", "between 1 and %d routes are required", maxIngressPaths)
	}
	normalizedPaths := make([]domain.IngressPathInput, 0, len(*paths))
	for index, route := range *paths {
		route.Host = strings.TrimSpace(route.Host)
		if route.Host != "" && (len(route.Host) > 253 || !ingressHostPattern.MatchString(route.Host)) {
			return ingressValidationError("paths", "route %d has an invalid host", index+1)
		}
		route.Path = strings.TrimSpace(route.Path)
		if route.Path == "" {
			route.Path = "/"
		}
		if !strings.HasPrefix(route.Path, "/") || len(route.Path) > 2048 {
			return ingressValidationError("paths", "route %d path must start with /", index+1)
		}
		route.PathType = strings.TrimSpace(route.PathType)
		if route.PathType == "" {
			route.PathType = "Prefix"
		}
		if _, ok := ingressPathTypeValues[route.PathType]; !ok {
			return ingressValidationError("paths", "route %d pathType must be Exact, Prefix, or ImplementationSpecific", index+1)
		}
		route.Service = strings.TrimSpace(route.Service)
		if len(route.Service) > 63 || !dnsLabelPattern.MatchString(route.Service) {
			return ingressValidationError("paths", "route %d backend service must be a valid DNS-1123 label", index+1)
		}
		route.Port = strings.TrimSpace(route.Port)
		if route.Port == "" {
			return ingressValidationError("paths", "route %d backend port is required", index+1)
		}
		if len(route.Port) > 15 {
			return ingressValidationError("paths", "route %d backend port is invalid", index+1)
		}
		if number, err := strconv.Atoi(route.Port); err == nil {
			if number < 1 || number > 65535 {
				return ingressValidationError("paths", "route %d backend port must be between 1 and 65535", index+1)
			}
		} else if !dnsLabelPattern.MatchString(route.Port) {
			return ingressValidationError("paths", "route %d backend port must be a number or port name", index+1)
		}
		normalizedPaths = append(normalizedPaths, route)
	}
	*paths = normalizedPaths

	if len(*tls) > 20 {
		return ingressValidationError("tls", "at most 20 TLS entries are allowed")
	}
	normalizedTLS := make([]domain.IngressTLS, 0, len(*tls))
	for index, entry := range *tls {
		entry.SecretName = strings.TrimSpace(entry.SecretName)
		if entry.SecretName == "" {
			return ingressValidationError("tls", "TLS entry %d requires a secret name", index+1)
		}
		if len(entry.SecretName) > 253 || !dnsLabelPattern.MatchString(entry.SecretName) {
			return ingressValidationError("tls", "TLS entry %d has an invalid secret name", index+1)
		}
		hosts := make([]string, 0, len(entry.Hosts))
		for _, host := range entry.Hosts {
			host = strings.TrimSpace(host)
			if host == "" || len(host) > 253 || !ingressHostPattern.MatchString(host) {
				return ingressValidationError("tls", "TLS entry %d has an invalid host", index+1)
			}
			hosts = append(hosts, host)
		}
		entry.Hosts = hosts
		normalizedTLS = append(normalizedTLS, entry)
	}
	*tls = normalizedTLS

	normalizedAnnotations := make(map[string]string, len(*annotations))
	for key, value := range *annotations {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 253 {
			return ingressValidationError("annotations", "annotation keys must not be empty or longer than 253 characters")
		}
		normalizedAnnotations[key] = value
	}
	*annotations = normalizedAnnotations
	return nil
}

func ingressValidationError(field, format string, args ...any) error {
	return &domain.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
