package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dosh/internal/domain"
)

const (
	defaultAccessTokenTTL = time.Hour
	minAccessTokenTTL     = 5 * time.Minute
	maxAccessTokenTTL     = 24 * time.Hour
	maxAccessTokenRules   = 20
	maxAccessTokenValues  = 16
	maxAccessTokenNameLen = 80
)

var (
	accessTokenNamespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	allowedAccessTokenVerbs     = map[string]bool{
		"get": true, "list": true, "watch": true, "create": true,
		"update": true, "patch": true, "delete": true, "deletecollection": true,
	}
)

// TokenAccessReviewer evaluates a token identity against Kubernetes RBAC. It
// is separate from the current session authorizer so token inspection still
// works when Dosh itself is running in local authentication mode.
type TokenAccessReviewer interface {
	ReviewAccess(ctx context.Context, actor domain.Actor, access domain.Access) (bool, error)
}

// AccessTokenIssuer creates the temporary Kubernetes identity and receives a
// bounded ServiceAccount token. Implementations must never retain the token.
type AccessTokenIssuer interface {
	IssueAccessToken(ctx context.Context, request domain.AccessTokenIssueRequest) (domain.IssuedAccessToken, error)
	CleanupExpiredAccessGrants(ctx context.Context, namespace string) (int, error)
}

type AccessTokenService struct {
	authenticator TokenAuthenticator
	reviewer      TokenAccessReviewer
	issuer        AccessTokenIssuer
	readOnly      bool
}

func NewAccessTokenService(authenticator TokenAuthenticator, reviewer TokenAccessReviewer, issuer AccessTokenIssuer, readOnly bool) *AccessTokenService {
	return &AccessTokenService{authenticator: authenticator, reviewer: reviewer, issuer: issuer, readOnly: readOnly}
}

func (s *AccessTokenService) ReviewToken(ctx context.Context, token string, access domain.Access) (domain.TokenAccessReview, error) {
	if s.authenticator == nil || s.reviewer == nil {
		return domain.TokenAccessReview{}, fmt.Errorf("%w: token access review is unavailable", domain.ErrUnsupported)
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxKubernetesTokenLength {
		return domain.TokenAccessReview{}, domain.ErrUnauthorized
	}
	actor, err := s.authenticator.AuthenticateToken(ctx, token)
	if err != nil {
		return domain.TokenAccessReview{}, err
	}
	allowed, reviewErr := s.reviewer.ReviewAccess(ctx, actor, access)
	return domain.TokenAccessReview{Identity: actor, Review: newAccessReview(access, allowed, reviewErr)}, nil
}

func (s *AccessTokenService) Issue(ctx context.Context, actor domain.Actor, request domain.AccessTokenIssueRequest) (domain.IssuedAccessToken, error) {
	if !actor.Admin {
		return domain.IssuedAccessToken{}, domain.ErrForbidden
	}
	if s.readOnly {
		return domain.IssuedAccessToken{}, domain.ErrReadOnly
	}
	if s.issuer == nil {
		return domain.IssuedAccessToken{}, fmt.Errorf("%w: access token issuance is unavailable", domain.ErrUnsupported)
	}
	normalized, err := normalizeAccessTokenRequest(request)
	if err != nil {
		return domain.IssuedAccessToken{}, err
	}
	return s.issuer.IssueAccessToken(ctx, normalized)
}

func (s *AccessTokenService) Cleanup(ctx context.Context, actor domain.Actor, namespace string) (domain.AccessGrantCleanupResult, error) {
	if !actor.Admin {
		return domain.AccessGrantCleanupResult{}, domain.ErrForbidden
	}
	if s.readOnly {
		return domain.AccessGrantCleanupResult{}, domain.ErrReadOnly
	}
	if s.issuer == nil {
		return domain.AccessGrantCleanupResult{}, fmt.Errorf("%w: access token cleanup is unavailable", domain.ErrUnsupported)
	}
	namespace = strings.TrimSpace(namespace)
	if !validAccessTokenNamespace(namespace) {
		return domain.AccessGrantCleanupResult{}, fmt.Errorf("%w: namespace is invalid", domain.ErrInvalid)
	}
	deleted, err := s.issuer.CleanupExpiredAccessGrants(ctx, namespace)
	if err != nil {
		return domain.AccessGrantCleanupResult{}, err
	}
	return domain.AccessGrantCleanupResult{Deleted: deleted}, nil
}

func normalizeAccessTokenRequest(request domain.AccessTokenIssueRequest) (domain.AccessTokenIssueRequest, error) {
	request.Name = strings.TrimSpace(request.Name)
	if len(request.Name) > maxAccessTokenNameLen || strings.ContainsAny(request.Name, "\r\n\x00") {
		return domain.AccessTokenIssueRequest{}, fmt.Errorf("%w: access token name is invalid", domain.ErrInvalid)
	}
	request.Namespace = strings.TrimSpace(request.Namespace)
	if !validAccessTokenNamespace(request.Namespace) {
		return domain.AccessTokenIssueRequest{}, fmt.Errorf("%w: namespace is invalid", domain.ErrInvalid)
	}
	request.TTLSeconds = normalizeAccessTokenTTL(request.TTLSeconds)
	if len(request.Rules) == 0 || len(request.Rules) > maxAccessTokenRules {
		return domain.AccessTokenIssueRequest{}, fmt.Errorf("%w: between 1 and %d permission rules are required", domain.ErrInvalid, maxAccessTokenRules)
	}
	rules := make([]domain.AccessTokenRule, 0, len(request.Rules))
	for _, rule := range request.Rules {
		normalized, err := normalizeAccessTokenRule(rule)
		if err != nil {
			return domain.AccessTokenIssueRequest{}, err
		}
		rules = append(rules, normalized)
	}
	request.Rules = rules
	return request, nil
}

func normalizeAccessTokenRule(rule domain.AccessTokenRule) (domain.AccessTokenRule, error) {
	verbs := normalizeAccessTokenValues(rule.Verbs)
	if len(verbs) == 0 || len(verbs) > maxAccessTokenValues {
		return domain.AccessTokenRule{}, fmt.Errorf("%w: each permission rule needs between 1 and %d verbs", domain.ErrInvalid, maxAccessTokenValues)
	}
	for _, verb := range verbs {
		if !allowedAccessTokenVerbs[verb] {
			return domain.AccessTokenRule{}, fmt.Errorf("%w: unsupported verb %q", domain.ErrInvalid, verb)
		}
	}
	groups := normalizeAccessTokenValues(rule.APIGroups)
	if len(groups) == 0 {
		groups = []string{""}
	}
	if len(groups) > maxAccessTokenValues {
		return domain.AccessTokenRule{}, fmt.Errorf("%w: each permission rule supports at most %d API groups", domain.ErrInvalid, maxAccessTokenValues)
	}
	resources := normalizeAccessTokenValues(rule.Resources)
	if len(resources) == 0 || len(resources) > maxAccessTokenValues {
		return domain.AccessTokenRule{}, fmt.Errorf("%w: each permission rule needs between 1 and %d resources", domain.ErrInvalid, maxAccessTokenValues)
	}
	for _, resource := range resources {
		if strings.Contains(resource, "*") || strings.ContainsAny(resource, "\r\n\x00 ") {
			return domain.AccessTokenRule{}, fmt.Errorf("%w: resource %q is invalid", domain.ErrInvalid, resource)
		}
	}
	for _, group := range groups {
		if strings.Contains(group, "*") || strings.ContainsAny(group, "\r\n\x00 ") {
			return domain.AccessTokenRule{}, fmt.Errorf("%w: API group %q is invalid", domain.ErrInvalid, group)
		}
	}
	resourceNames := normalizeAccessTokenValues(rule.ResourceNames)
	if len(resourceNames) > maxAccessTokenValues {
		return domain.AccessTokenRule{}, fmt.Errorf("%w: each permission rule supports at most %d resource names", domain.ErrInvalid, maxAccessTokenValues)
	}
	for _, resourceName := range resourceNames {
		if strings.Contains(resourceName, "*") || strings.ContainsAny(resourceName, "\r\n\x00") {
			return domain.AccessTokenRule{}, fmt.Errorf("%w: resource name %q is invalid", domain.ErrInvalid, resourceName)
		}
	}
	return domain.AccessTokenRule{
		Verbs: verbs, APIGroups: groups, Resources: resources,
		ResourceNames: resourceNames,
	}, nil
}

func normalizeAccessTokenValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func normalizeAccessTokenTTL(seconds int64) int64 {
	ttl := time.Duration(seconds) * time.Second
	if ttl <= 0 {
		ttl = defaultAccessTokenTTL
	}
	if ttl < minAccessTokenTTL {
		ttl = minAccessTokenTTL
	}
	if ttl > maxAccessTokenTTL {
		ttl = maxAccessTokenTTL
	}
	return int64(ttl / time.Second)
}

func validAccessTokenNamespace(namespace string) bool {
	return len(namespace) <= 63 && accessTokenNamespacePattern.MatchString(namespace)
}
