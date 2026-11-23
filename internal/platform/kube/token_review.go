package kube

import (
	"context"
	"strings"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AuthenticateToken validates a user-provided bearer token with the Kubernetes
// TokenReview API. The review is executed as the dosh ServiceAccount and the
// raw token is never retained by the application.
func (c *Client) AuthenticateToken(ctx context.Context, token string) (domain.Actor, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.Actor{}, domain.ErrUnauthorized
	}
	ctx, cancel := c.withTimeout(domain.WithoutActor(ctx))
	defer cancel()
	review := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{Token: token},
	}
	if c.tokenAudience != "" {
		review.Spec.Audiences = []string{c.tokenAudience}
	}
	response, err := c.clientset.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return domain.Actor{}, mapKubeError(err)
	}
	if !response.Status.Authenticated || strings.TrimSpace(response.Status.User.Username) == "" {
		return domain.Actor{}, domain.ErrUnauthorized
	}
	actor := domain.Actor{
		Username: response.Status.User.Username,
		Groups:   nonEmptyStrings(response.Status.User.Groups),
		Admin:    containsString(response.Status.User.Groups, "system:masters"),
	}
	if !actor.Admin {
		actor.Admin = c.canManageControlPlane(ctx, actor.Username, actor.Groups)
	}
	return actor, nil
}

// canManageControlPlane treats Kubernetes RBAC permission to update Dosh's
// control-plane settings Secret as the token-mode administrator boundary. This
// keeps least-privilege ServiceAccounts and cluster-admin bindings working even
// though ServiceAccount groups never include system:masters.
func (c *Client) canManageControlPlane(ctx context.Context, username string, groups []string) bool {
	if c.preferencesNamespace == "" {
		return false
	}
	review := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   username,
			Groups: groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: c.preferencesNamespace,
				Verb:      "update",
				Resource:  "secrets",
				Name:      "dosh-egress-settings",
			},
		},
	}
	response, err := c.clientset.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	return err == nil && response.Status.Allowed
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
