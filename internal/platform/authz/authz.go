package authz

import (
	"context"

	"dosh/internal/domain"
)

// AccessReviewer is implemented by the Kubernetes platform adapter. It must
// evaluate access for the explicitly supplied actor, not for the credential
// used by the application itself.
type AccessReviewer interface {
	ReviewAccess(ctx context.Context, actor domain.Actor, access domain.Access) (bool, error)
}

// Local implements the single-admin policy used by local authentication.
type Local struct {
	readOnly bool
}

func NewLocal(readOnly bool) *Local { return &Local{readOnly: readOnly} }

func (a *Local) Require(_ context.Context, actor domain.Actor, access domain.Access) error {
	if a.readOnly && isWritePolicyAccess(access) {
		return domain.ErrReadOnly
	}
	if !actor.Admin {
		return domain.ErrForbidden
	}
	return nil
}

// Kubernetes delegates authorization to Kubernetes RBAC through
// SubjectAccessReview. Business calls impersonate the actor carried in ctx;
// the authorization review itself is explicitly performed by the app
// ServiceAccount and must not inherit that impersonation.
type Kubernetes struct {
	reviewer AccessReviewer
	readOnly bool
}

func NewKubernetes(reviewer AccessReviewer, readOnly bool) *Kubernetes {
	return &Kubernetes{reviewer: reviewer, readOnly: readOnly}
}

func (a *Kubernetes) Require(ctx context.Context, actor domain.Actor, access domain.Access) error {
	if a.readOnly && isWritePolicyAccess(access) {
		return domain.ErrReadOnly
	}
	allowed, err := a.reviewer.ReviewAccess(ctx, actor, access)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.ErrForbidden
	}
	return nil
}

func isReadAccess(access domain.Access) bool {
	switch access.Verb {
	case "get", "list", "watch":
		return true
	default:
		return false
	}
}

// isWritePolicyAccess treats reading a Secret value as sensitive even though
// the Kubernetes verb is get. Listing Secret metadata remains read-only.
func isWritePolicyAccess(access domain.Access) bool {
	if !isReadAccess(access) {
		return true
	}
	return access.Verb == "get" && access.Resource == "secrets"
}
