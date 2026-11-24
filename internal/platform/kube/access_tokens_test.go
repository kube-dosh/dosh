package kube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestIssueAccessTokenCreatesBoundedServiceAccountAndRBAC(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	now := time.Now().UTC()
	var capturedTokenRequest *authv1.TokenRequest
	clientset.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "token" {
			return false, nil, nil
		}
		request := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenRequest)
		capturedTokenRequest = request
		request.Status.Token = "issued-access-token"
		request.Status.ExpirationTimestamp = metav1.NewTime(now.Add(15 * time.Minute))
		return true, request, nil
	})
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second})
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "admin"})
	result, err := client.IssueAccessToken(ctx, domain.AccessTokenIssueRequest{
		Name: "oncall", Namespace: "default", TTLSeconds: 900,
		Rules: []domain.AccessTokenRule{{
			Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"pods"}, ResourceNames: []string{"api-0"},
		}},
	})
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	if result.Token != "issued-access-token" || result.Namespace != "default" || !strings.HasPrefix(result.ServiceAccount, "dosh-access-oncall-") {
		t.Fatalf("issued token = %#v", result)
	}
	if capturedTokenRequest == nil || capturedTokenRequest.Spec.ExpirationSeconds == nil || *capturedTokenRequest.Spec.ExpirationSeconds != 900 {
		t.Fatalf("TokenRequest = %#v", capturedTokenRequest)
	}
	serviceAccount, err := clientset.CoreV1().ServiceAccounts("default").Get(ctx, result.ServiceAccount, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get ServiceAccount: %v", err)
	}
	if serviceAccount.Labels[accessGrantLabel] != "true" || serviceAccount.Annotations[accessGrantCreatedByAnn] != "admin" || serviceAccount.Annotations[accessGrantDisplayNameAnn] != "oncall" {
		t.Fatalf("ServiceAccount labels/annotations = %#v %#v", serviceAccount.Labels, serviceAccount.Annotations)
	}
	role, err := clientset.RbacV1().Roles("default").Get(ctx, result.ServiceAccount, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get Role: %v", err)
	}
	if len(role.Rules) != 1 || len(role.Rules[0].Verbs) != 2 || len(role.Rules[0].Resources) != 1 || role.Rules[0].Resources[0] != "pods" || role.Rules[0].ResourceNames[0] != "api-0" {
		t.Fatalf("Role rules = %#v", role.Rules)
	}
	binding, err := clientset.RbacV1().RoleBindings("default").Get(ctx, result.ServiceAccount, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get RoleBinding: %v", err)
	}
	if binding.RoleRef.Name != result.ServiceAccount || len(binding.Subjects) != 1 || binding.Subjects[0].Kind != rbacv1.ServiceAccountKind {
		t.Fatalf("RoleBinding = %#v", binding)
	}
}

func TestIssueAccessTokenRollsBackWhenTokenCreationFails(t *testing.T) {
	t.Parallel()
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "token" {
			return false, nil, nil
		}
		return true, nil, errors.New("token issuer unavailable")
	})
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second})
	ctx := domain.WithActor(context.Background(), domain.Actor{Username: "admin"})
	_, err := client.IssueAccessToken(ctx, domain.AccessTokenIssueRequest{
		Namespace: "default", TTLSeconds: 900,
		Rules: []domain.AccessTokenRule{{Verbs: []string{"get"}, Resources: []string{"pods"}}},
	})
	if err == nil {
		t.Fatal("IssueAccessToken() succeeded despite token failure")
	}
	serviceAccounts, listErr := clientset.CoreV1().ServiceAccounts("default").List(ctx, metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("List ServiceAccounts: %v", listErr)
	}
	if len(serviceAccounts.Items) != 0 {
		t.Fatalf("rollback left ServiceAccounts: %#v", serviceAccounts.Items)
	}
	roles, listErr := clientset.RbacV1().Roles("default").List(ctx, metav1.ListOptions{})
	if listErr != nil || len(roles.Items) != 0 {
		t.Fatalf("rollback left Roles: %#v, error = %v", roles.Items, listErr)
	}
	bindings, listErr := clientset.RbacV1().RoleBindings("default").List(ctx, metav1.ListOptions{})
	if listErr != nil || len(bindings.Items) != 0 {
		t.Fatalf("rollback left RoleBindings: %#v, error = %v", bindings.Items, listErr)
	}
}

func TestCleanupExpiredAccessGrantsRemovesOnlyExpiredDoshGrants(t *testing.T) {
	t.Parallel()
	expiredAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	activeAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	labels := map[string]string{accessGrantLabel: "true", accessGrantManagedByLabel: accessGrantManagedByDoshVal}
	clientset := k8sfake.NewSimpleClientset(
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "dosh-access-expired-a1", Namespace: "default", Labels: labels, Annotations: map[string]string{accessGrantExpiresAtAnn: expiredAt}}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "dosh-access-active-a2", Namespace: "default", Labels: labels, Annotations: map[string]string{accessGrantExpiresAtAnn: activeAt}}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-a3", Namespace: "default", Labels: labels, Annotations: map[string]string{accessGrantExpiresAtAnn: expiredAt}}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "dosh-access-expired-a1", Namespace: "default"}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "dosh-access-expired-a1", Namespace: "default"}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "dosh-access-active-a2", Namespace: "default"}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "dosh-access-active-a2", Namespace: "default"}},
	)
	client := NewForClients(clientset, nil, nil, config.Config{RequestTimeout: time.Second})
	deleted, err := client.CleanupExpiredAccessGrants(context.Background(), "default")
	if err != nil {
		t.Fatalf("CleanupExpiredAccessGrants() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, err := clientset.CoreV1().ServiceAccounts("default").Get(context.Background(), "dosh-access-expired-a1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expired ServiceAccount still exists: %v", err)
	}
	if _, err := clientset.RbacV1().Roles("default").Get(context.Background(), "dosh-access-expired-a1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expired Role still exists: %v", err)
	}
	if _, err := clientset.CoreV1().ServiceAccounts("default").Get(context.Background(), "dosh-access-active-a2", metav1.GetOptions{}); err != nil {
		t.Fatalf("active ServiceAccount was removed: %v", err)
	}
	if _, err := clientset.CoreV1().ServiceAccounts("default").Get(context.Background(), "unrelated-a3", metav1.GetOptions{}); err != nil {
		t.Fatalf("unrelated ServiceAccount was removed: %v", err)
	}
}
