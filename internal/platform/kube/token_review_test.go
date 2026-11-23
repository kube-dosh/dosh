package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	authv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestAuthenticateTokenUsesTokenReview(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	var captured *authv1.TokenReview
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		captured = action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview)
		captured.Status.Authenticated = true
		captured.Status.User.Username = "alice@example.test"
		captured.Status.User.Groups = []string{"dosh-operators", "system:masters"}
		return true, captured, nil
	})
	client := NewForClients(clientset, nil, nil, config.Config{
		RequestTimeout: time.Second, TokenReviewAudience: "https://kubernetes.default.svc",
	})
	actor, err := client.AuthenticateToken(context.Background(), "  token-value  ")
	if err != nil {
		t.Fatalf("AuthenticateToken() error = %v", err)
	}
	if captured == nil || captured.Spec.Token != "token-value" {
		t.Fatalf("TokenReview token = %#v", captured)
	}
	if len(captured.Spec.Audiences) != 1 || captured.Spec.Audiences[0] != "https://kubernetes.default.svc" {
		t.Fatalf("TokenReview audiences = %#v", captured.Spec.Audiences)
	}
	if actor.Username != "alice@example.test" || !actor.Admin || len(actor.Groups) != 2 {
		t.Fatalf("authenticated actor = %#v", actor)
	}
}

func TestAuthenticateTokenUsesControlPlaneSubjectAccessReviewForAdmin(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview)
		review.Status.Authenticated = true
		review.Status.User.Username = "system:serviceaccount:dosh:dosh-admin"
		review.Status.User.Groups = []string{"system:serviceaccounts", "system:serviceaccounts:dosh", "system:authenticated"}
		return true, review, nil
	})
	var captured *authorizationv1.SubjectAccessReview
	clientset.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		captured = action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		captured.Status.Allowed = true
		return true, captured, nil
	})
	client := NewForClients(clientset, nil, nil, config.Config{
		RequestTimeout: time.Second, PreferencesNamespace: "dosh",
	})
	actor, err := client.AuthenticateToken(context.Background(), "admin-token")
	if err != nil {
		t.Fatalf("AuthenticateToken() error = %v", err)
	}
	if !actor.Admin {
		t.Fatalf("authenticated actor = %#v, want admin from SubjectAccessReview", actor)
	}
	if captured == nil || captured.Spec.User != actor.Username || captured.Spec.ResourceAttributes == nil {
		t.Fatalf("SubjectAccessReview = %#v", captured)
	}
	attributes := captured.Spec.ResourceAttributes
	if attributes.Namespace != "dosh" || attributes.Verb != "update" || attributes.Resource != "secrets" || attributes.Name != "dosh-egress-settings" {
		t.Fatalf("SubjectAccessReview attributes = %#v", attributes)
	}
}

func TestAuthenticateTokenRejectsUnauthenticatedAndEmptyTokens(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview)
		review.Status.Authenticated = false
		return true, review, nil
	})
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second})
	for _, token := range []string{"", "   ", "invalid"} {
		if _, err := client.AuthenticateToken(context.Background(), token); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("AuthenticateToken(%q) error = %v, want unauthorized", token, err)
		}
	}
}
