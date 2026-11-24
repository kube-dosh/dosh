package kube

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	accessGrantLabel            = "dosh.io/access-grant"
	accessGrantExpiresAtAnn     = "dosh.io/access-grant-expires-at"
	accessGrantCreatedByAnn     = "dosh.io/access-grant-created-by"
	accessGrantDisplayNameAnn   = "dosh.io/access-grant-display-name"
	accessGrantDefaultTTL       = 3600
	accessGrantMinTTL           = 300
	accessGrantMaxTTL           = 86400
	accessGrantNameMaxSlug      = 32
	accessGrantNameRandomBytes  = 4
	accessGrantManagedByLabel   = "app.kubernetes.io/managed-by"
	accessGrantManagedByDoshVal = "dosh"
)

var accessGrantNamePattern = regexp.MustCompile(`[^a-z0-9]+`)

// IssueAccessToken creates a dedicated ServiceAccount, namespace Role and
// RoleBinding, then requests a bounded token for that identity. The caller's
// context identity is preserved so token/OIDC mode is authorized by the current
// user's Kubernetes RBAC. In local mode the Dosh ServiceAccount performs the
// writes through the narrowly scoped RBAC rules published with the chart.
func (c *Client) IssueAccessToken(ctx context.Context, request domain.AccessTokenIssueRequest) (domain.IssuedAccessToken, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	name, err := newAccessGrantName(request.Name)
	if err != nil {
		return domain.IssuedAccessToken{}, err
	}
	actor, _ := domain.ActorFromContext(ctx)
	ttl := request.TTLSeconds
	if ttl <= 0 {
		ttl = accessGrantDefaultTTL
	}
	if ttl < accessGrantMinTTL {
		ttl = accessGrantMinTTL
	}
	if ttl > accessGrantMaxTTL {
		ttl = accessGrantMaxTTL
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(ttl) * time.Second)
	labels := map[string]string{
		accessGrantManagedByLabel: accessGrantManagedByDoshVal,
		accessGrantLabel:          "true",
	}
	annotations := map[string]string{
		accessGrantExpiresAtAnn:   expiresAt.Format(time.RFC3339),
		accessGrantCreatedByAnn:   actor.Username,
		accessGrantDisplayNameAnn: strings.TrimSpace(request.Name),
	}
	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: request.Namespace, Labels: labels, Annotations: annotations,
	}}
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: request.Namespace, Labels: labels, Annotations: annotations},
		Rules:      accessGrantPolicyRules(request.Rules),
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: request.Namespace, Labels: labels, Annotations: annotations},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: name, Namespace: request.Namespace}},
	}
	rollback := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cleanupCancel()
		_ = c.clientset.RbacV1().RoleBindings(request.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		_ = c.clientset.RbacV1().Roles(request.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		_ = c.clientset.CoreV1().ServiceAccounts(request.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
	}
	if _, err := c.clientset.CoreV1().ServiceAccounts(request.Namespace).Create(ctx, serviceAccount, metav1.CreateOptions{}); err != nil {
		return domain.IssuedAccessToken{}, mapKubeError(err)
	}
	if _, err := c.clientset.RbacV1().Roles(request.Namespace).Create(ctx, role, metav1.CreateOptions{}); err != nil {
		rollback()
		return domain.IssuedAccessToken{}, mapKubeError(err)
	}
	if _, err := c.clientset.RbacV1().RoleBindings(request.Namespace).Create(ctx, binding, metav1.CreateOptions{}); err != nil {
		rollback()
		return domain.IssuedAccessToken{}, mapKubeError(err)
	}
	tokenRequest := &authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &ttl}}
	response, err := c.clientset.CoreV1().ServiceAccounts(request.Namespace).CreateToken(ctx, name, tokenRequest, metav1.CreateOptions{})
	if err != nil || strings.TrimSpace(response.Status.Token) == "" {
		rollback()
		if err != nil {
			return domain.IssuedAccessToken{}, mapKubeError(err)
		}
		return domain.IssuedAccessToken{}, fmt.Errorf("%w: Kubernetes returned an empty ServiceAccount token", domain.ErrUnavailable)
	}
	tokenExpiresAt := response.Status.ExpirationTimestamp.Time.UTC()
	if tokenExpiresAt.IsZero() {
		tokenExpiresAt = expiresAt
	}
	return domain.IssuedAccessToken{
		Token: response.Status.Token, Namespace: request.Namespace, ServiceAccount: name,
		CreatedAt: now, ExpiresAt: tokenExpiresAt, Rules: request.Rules,
	}, nil
}

// CleanupExpiredAccessGrants removes grants in one namespace after their token
// expiry. Tokens become invalid automatically, while this removes the lingering
// ServiceAccount and RBAC objects created for the grant.
func (c *Client) CleanupExpiredAccessGrants(ctx context.Context, namespace string) (int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	selector := accessGrantLabel + "=true," + accessGrantManagedByLabel + "=" + accessGrantManagedByDoshVal
	serviceAccounts, err := c.clientset.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, mapKubeError(err)
	}
	now := time.Now().UTC()
	deleted := 0
	for index := range serviceAccounts.Items {
		serviceAccount := &serviceAccounts.Items[index]
		if !strings.HasPrefix(serviceAccount.Name, "dosh-access-") {
			continue
		}
		rawExpiry := strings.TrimSpace(serviceAccount.Annotations[accessGrantExpiresAtAnn])
		expiresAt, err := time.Parse(time.RFC3339, rawExpiry)
		if err != nil || expiresAt.After(now) {
			continue
		}
		if err := c.clientset.RbacV1().RoleBindings(namespace).Delete(ctx, serviceAccount.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return deleted, mapKubeError(err)
		}
		if err := c.clientset.RbacV1().Roles(namespace).Delete(ctx, serviceAccount.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return deleted, mapKubeError(err)
		}
		if err := c.clientset.CoreV1().ServiceAccounts(namespace).Delete(ctx, serviceAccount.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return deleted, mapKubeError(err)
		}
		deleted++
	}
	return deleted, nil
}

func accessGrantPolicyRules(rules []domain.AccessTokenRule) []rbacv1.PolicyRule {
	out := make([]rbacv1.PolicyRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rbacv1.PolicyRule{
			Verbs: rule.Verbs, APIGroups: rule.APIGroups, Resources: rule.Resources, ResourceNames: rule.ResourceNames,
		})
	}
	return out
}

func newAccessGrantName(displayName string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(displayName))
	slug = accessGrantNamePattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "token"
	}
	if len(slug) > accessGrantNameMaxSlug {
		slug = strings.Trim(slug[:accessGrantNameMaxSlug], "-")
	}
	random := make([]byte, accessGrantNameRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("%w: generate access grant name", domain.ErrUnavailable)
	}
	return fmt.Sprintf("dosh-access-%s-%s", slug, hex.EncodeToString(random)), nil
}
