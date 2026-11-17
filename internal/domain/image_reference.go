package domain

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	imageRepositoryComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	imageTagPattern                 = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	imageDigestPattern              = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*:[0-9A-Fa-f]{32,}$`)
)

type ImageReference struct {
	Original         string
	Registry         string
	Repository       string
	Tag              string
	Digest           string
	ExplicitRegistry bool
}

func (r ImageReference) String() string {
	name := r.Repository
	if r.ExplicitRegistry {
		name = r.Registry + "/" + r.Repository
	}
	if r.Digest != "" {
		return name + "@" + r.Digest
	}
	if r.Tag != "" {
		return name + ":" + r.Tag
	}
	return name
}

// ParseImageReference accepts the Docker/OCI image reference forms used by
// Kubernetes while preserving whether the registry was explicitly specified.
func ParseImageReference(value string) (ImageReference, error) {
	original := strings.TrimSpace(value)
	if original == "" || len(original) > 512 {
		return ImageReference{}, fmt.Errorf("%w: image reference is required and must not exceed 512 characters", ErrInvalid)
	}
	if strings.Contains(original, "://") || strings.ContainsAny(original, " \t\r\n") {
		return ImageReference{}, fmt.Errorf("%w: image reference is invalid", ErrInvalid)
	}

	reference := ImageReference{Original: original}
	name := original
	if at := strings.LastIndex(name, "@"); at >= 0 {
		if strings.Count(name, "@") != 1 {
			return ImageReference{}, fmt.Errorf("%w: image digest is invalid", ErrInvalid)
		}
		reference.Digest = name[at+1:]
		name = name[:at]
		if !imageDigestPattern.MatchString(reference.Digest) {
			return ImageReference{}, fmt.Errorf("%w: image digest is invalid", ErrInvalid)
		}
	}

	lastSlash := strings.LastIndex(name, "/")
	if colon := strings.LastIndex(name, ":"); colon > lastSlash {
		reference.Tag = name[colon+1:]
		name = name[:colon]
		if !imageTagPattern.MatchString(reference.Tag) {
			return ImageReference{}, fmt.Errorf("%w: image tag is invalid", ErrInvalid)
		}
	}
	if name == "" || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return ImageReference{}, fmt.Errorf("%w: image repository is invalid", ErrInvalid)
	}

	parts := strings.Split(name, "/")
	repositoryParts := parts
	if len(parts) > 1 && isImageRegistryComponent(parts[0]) {
		host, err := validateImageRegistryHost(parts[0])
		if err != nil {
			return ImageReference{}, err
		}
		reference.Registry = host
		reference.Repository = strings.Join(parts[1:], "/")
		reference.ExplicitRegistry = true
		repositoryParts = parts[1:]
	} else {
		reference.Registry = "docker.io"
		reference.Repository = name
	}
	for _, part := range repositoryParts {
		if part == "" || !imageRepositoryComponentPattern.MatchString(part) {
			return ImageReference{}, fmt.Errorf("%w: image repository components must be lowercase alphanumeric with single separators", ErrInvalid)
		}
	}
	if reference.Tag == "" && reference.Digest == "" {
		reference.Tag = "latest"
	}
	return reference, nil
}

func isImageRegistryComponent(value string) bool {
	return value == "localhost" || strings.Contains(value, ".") || strings.Contains(value, ":")
}

func validateImageRegistryHost(value string) (string, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		host, port = value, ""
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || len(host) > 253 {
		return "", fmt.Errorf("%w: registry host is invalid", ErrInvalid)
	}
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("%w: registry port is invalid", ErrInvalid)
		}
		return net.JoinHostPort(host, port), nil
	}
	if strings.Contains(host, ":") {
		return "", fmt.Errorf("%w: registry host is invalid", ErrInvalid)
	}
	return host, nil
}
