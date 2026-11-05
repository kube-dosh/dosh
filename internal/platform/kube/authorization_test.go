package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestReviewAccessUsesSubjectAccessReview(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	var captured *authv1.SubjectAccessReview
	clientset.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		captured = action.(k8stesting.CreateAction).GetObject().(*authv1.SubjectAccessReview)
		captured.Status.Allowed = true
		return true, captured, nil
	})
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second})
	actor := domain.Actor{Username: "alice@example.test", Groups: []string{"viewers", "", "dosh-operators"}}
	allowed, err := client.ReviewAccess(context.Background(), actor, domain.Access{
		Verb: "update", APIGroup: "apps", Resource: "deployments", Subresource: "scale", Namespace: "prod", Name: "web",
	})
	if err != nil || !allowed {
		t.Fatalf("ReviewAccess() = %v, %v", allowed, err)
	}
	if captured == nil || captured.Spec.ResourceAttributes == nil {
		t.Fatal("SubjectAccessReview was not submitted")
	}
	if captured.Spec.User != actor.Username {
		t.Fatalf("subject user = %q", captured.Spec.User)
	}
	if groups := captured.Spec.Groups; len(groups) != 2 || groups[0] != "viewers" || groups[1] != "dosh-operators" {
		t.Fatalf("subject groups = %#v", groups)
	}
	attributes := captured.Spec.ResourceAttributes
	if attributes.Verb != "update" || attributes.Group != "apps" || attributes.Resource != "deployments" || attributes.Subresource != "scale" || attributes.Namespace != "prod" || attributes.Name != "web" {
		t.Fatalf("resource attributes = %#v", attributes)
	}
}

func TestReviewAccessFailsClosedWithoutActor(t *testing.T) {
	t.Parallel()
	client := NewForClients(k8sfake.NewSimpleClientset(), nil, nil, config.Config{RequestTimeout: time.Second})
	allowed, err := client.ReviewAccess(context.Background(), domain.Actor{}, domain.Access{Verb: "get", Resource: "pods"})
	if allowed || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("ReviewAccess() = %v, %v", allowed, err)
	}
}
