package kube

import (
	"context"
	"fmt"
	"strings"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	maxTokenPermissionRules          = 200
	maxTokenPermissionRuleValues     = 64
	tokenPermissionTruncationWarning = "该 Token 的权限规则超过展示上限，列表已截断。"
)

// ReviewTokenRules asks Kubernetes for the token identity's effective
// namespaced RBAC rules. The request is made with the supplied token itself so
// Dosh does not need impersonation permission and never attributes the review
// to its own ServiceAccount identity. The token is used only for this call.
func (c *Client) ReviewTokenRules(ctx context.Context, token, namespace string) (domain.TokenRulesSnapshot, error) {
	token = strings.TrimSpace(token)
	namespace = strings.TrimSpace(namespace)
	if token == "" {
		return domain.TokenRulesSnapshot{}, domain.ErrUnauthorized
	}
	if namespace == "" || strings.ContainsAny(namespace, "\r\n\x00 /") {
		return domain.TokenRulesSnapshot{}, fmt.Errorf("%w: rules namespace is invalid", domain.ErrInvalid)
	}
	if c.restConfig == nil || strings.TrimSpace(c.restConfig.Host) == "" {
		return domain.TokenRulesSnapshot{}, fmt.Errorf("%w: Kubernetes REST configuration is unavailable", domain.ErrUnsupported)
	}

	config := rest.CopyConfig(c.restConfig)
	config.BearerToken = token
	config.BearerTokenFile = ""
	config.Username = ""
	config.Password = ""
	config.AuthProvider = nil
	config.ExecProvider = nil
	config.Impersonate = rest.ImpersonationConfig{}
	config.TLSClientConfig.CertData = nil
	config.TLSClientConfig.CertFile = ""
	config.TLSClientConfig.KeyData = nil
	config.TLSClientConfig.KeyFile = ""
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return domain.TokenRulesSnapshot{}, fmt.Errorf("%w: create token rules client: %v", domain.ErrUnavailable, err)
	}

	ctx, cancel := c.withTimeout(domain.WithoutActor(ctx))
	defer cancel()
	review := &authv1.SelfSubjectRulesReview{Spec: authv1.SelfSubjectRulesReviewSpec{Namespace: namespace}}
	response, err := clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return domain.TokenRulesSnapshot{}, mapKubeError(err)
	}

	snapshot := domain.TokenRulesSnapshot{
		Namespace:  namespace,
		Incomplete: response.Status.Incomplete,
	}
	if response.Status.EvaluationError != "" {
		snapshot.Incomplete = true
		snapshot.Warnings = append(snapshot.Warnings, "Kubernetes 未能完整评估该 Token 的权限规则："+response.Status.EvaluationError)
	}
	for _, rule := range response.Status.ResourceRules {
		if len(snapshot.Rules) >= maxTokenPermissionRules {
			snapshot.Warnings = append(snapshot.Warnings, tokenPermissionTruncationWarning)
			break
		}
		snapshot.Rules = append(snapshot.Rules, domain.ResourceRule{
			Verbs:         boundedTokenPermissionValues(rule.Verbs),
			APIGroups:     boundedTokenPermissionValues(rule.APIGroups),
			Resources:     boundedTokenPermissionValues(rule.Resources),
			ResourceNames: boundedTokenPermissionValues(rule.ResourceNames),
		})
	}
	return snapshot, nil
}

func boundedTokenPermissionValues(values []string) []string {
	if len(values) > maxTokenPermissionRuleValues {
		values = values[:maxTokenPermissionRuleValues]
	}
	return append([]string(nil), values...)
}
