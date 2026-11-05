package authz

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

func TestLocalAuthorizerEnforcesAdminAndReadOnly(t *testing.T) {
	t.Parallel()
	admin := domain.Actor{Username: "admin", Admin: true}
	if err := NewLocal(false).Require(context.Background(), admin, domain.Access{Verb: "delete", Resource: "deployments"}); err != nil {
		t.Fatalf("writable admin error = %v", err)
	}
	if err := NewLocal(true).Require(context.Background(), admin, domain.Access{Verb: "delete", Resource: "deployments"}); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("read-only write error = %v", err)
	}
	if err := NewLocal(true).Require(context.Background(), admin, domain.Access{Verb: "get", Resource: "secrets"}); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("read-only secret reveal error = %v", err)
	}
	if err := NewLocal(false).Require(context.Background(), admin, domain.Access{Verb: "list", Resource: "secrets"}); err != nil {
		t.Fatalf("read-only secret metadata error = %v", err)
	}
	if err := NewLocal(false).Require(context.Background(), domain.Actor{Username: "viewer"}, domain.Access{Verb: "get", Resource: "pods"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin error = %v", err)
	}
}

type reviewFunc func(context.Context, domain.Actor, domain.Access) (bool, error)

func (f reviewFunc) ReviewAccess(ctx context.Context, actor domain.Actor, access domain.Access) (bool, error) {
	return f(ctx, actor, access)
}

func TestKubernetesAuthorizerPassesActor(t *testing.T) {
	t.Parallel()
	authorizer := NewKubernetes(reviewFunc(func(_ context.Context, actor domain.Actor, access domain.Access) (bool, error) {
		if actor.Username != "alice@example.test" || len(actor.Groups) != 1 || actor.Groups[0] != "viewers" {
			t.Fatalf("actor = %#v", actor)
		}
		if access.Resource != "deployments" || access.Subresource != "scale" {
			t.Fatalf("access = %#v", access)
		}
		return true, nil
	}), false)
	identity := domain.Actor{Username: "alice@example.test", Groups: []string{"viewers"}}
	if err := authorizer.Require(context.Background(), identity, domain.Access{Verb: "update", APIGroup: "apps", Resource: "deployments", Subresource: "scale"}); err != nil {
		t.Fatalf("Require() error = %v", err)
	}
}

func TestKubernetesAuthorizerMapsDenialAndReadOnly(t *testing.T) {
	t.Parallel()
	deny := NewKubernetes(reviewFunc(func(context.Context, domain.Actor, domain.Access) (bool, error) { return false, nil }), false)
	actor := domain.Actor{Username: "alice@example.test"}
	if err := deny.Require(context.Background(), actor, domain.Access{Verb: "get", Resource: "pods"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denial error = %v", err)
	}
	readOnly := NewKubernetes(reviewFunc(func(context.Context, domain.Actor, domain.Access) (bool, error) {
		t.Fatal("read-only write reached the reviewer")
		return false, nil
	}), true)
	if err := readOnly.Require(context.Background(), actor, domain.Access{Verb: "delete", Resource: "pods"}); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("read-only error = %v", err)
	}
}

func TestKubernetesAuthorizerPreservesReviewerErrors(t *testing.T) {
	t.Parallel()
	reviewErr := errors.New("authorization unavailable")
	authorizer := NewKubernetes(reviewFunc(func(context.Context, domain.Actor, domain.Access) (bool, error) {
		return false, reviewErr
	}), false)
	err := authorizer.Require(context.Background(), domain.Actor{Username: "alice@example.test"}, domain.Access{Verb: "get", Resource: "pods"})
	if !errors.Is(err, reviewErr) {
		t.Fatalf("reviewer error = %v", err)
	}
}
