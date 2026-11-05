package kube

import (
	"context"
	"strings"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authentication/v1"
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
	return domain.Actor{
		Username: response.Status.User.Username,
		Groups:   nonEmptyStrings(response.Status.User.Groups),
		Admin:    containsString(response.Status.User.Groups, "system:masters"),
	}, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
