package kube

import (
	"context"
	"fmt"

	"dosh/internal/domain"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReviewAccess asks Kubernetes to authorize a specific actor. The review is a
// control-plane call made by the application ServiceAccount, so the context is
// stripped of the actor before the impersonation transport runs.
func (c *Client) ReviewAccess(ctx context.Context, actor domain.Actor, access domain.Access) (bool, error) {
	if actor.Username == "" {
		return false, domain.ErrForbidden
	}
	ctx, cancel := c.withTimeout(domain.WithoutActor(ctx))
	defer cancel()
	review := &authv1.SubjectAccessReview{
		Spec: authv1.SubjectAccessReviewSpec{
			User:   actor.Username,
			Groups: nonEmptyStrings(actor.Groups),
			ResourceAttributes: &authv1.ResourceAttributes{
				Verb:        access.Verb,
				Group:       access.APIGroup,
				Resource:    access.Resource,
				Subresource: access.Subresource,
				Namespace:   access.Namespace,
				Name:        access.Name,
			},
		},
	}
	response, err := c.clientset.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, mapKubeError(err)
	}
	if response.Status.EvaluationError != "" {
		return false, fmt.Errorf("%w: authorization review: %s", domain.ErrUnavailable, response.Status.EvaluationError)
	}
	return response.Status.Allowed, nil
}

func nonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
