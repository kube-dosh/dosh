package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"

	helmrelease "helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateHelmTargetNamespaceAllowsMissingNamespaceForCreate(t *testing.T) {
	client, _ := newFakeKubeClient()
	exists, err := client.validateHelmTargetNamespace(context.Background(), "new-team", "demo", true)
	if err != nil {
		t.Fatalf("validateHelmTargetNamespace() error = %v", err)
	}
	if exists {
		t.Fatal("missing namespace was reported as existing")
	}
	if _, err := client.validateHelmTargetNamespace(context.Background(), "new-team", "demo", false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("required namespace error = %v", err)
	}
}

func TestValidateHelmTargetNamespaceReportsExistingNamespace(t *testing.T) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "apps"}}
	client, _ := newFakeKubeClient(namespace)
	exists, err := client.validateHelmTargetNamespace(context.Background(), "apps", "demo", false)
	if err != nil {
		t.Fatalf("validateHelmTargetNamespace() error = %v", err)
	}
	if !exists {
		t.Fatal("existing namespace was reported as missing")
	}
}

func TestHelmReleaseCanUpgradeAllowsRecoverableStatuses(t *testing.T) {
	for _, status := range []helmrelease.Status{
		helmrelease.StatusDeployed,
		helmrelease.StatusFailed,
		helmrelease.StatusSuperseded,
	} {
		if !helmReleaseCanUpgrade(status) {
			t.Fatalf("helmReleaseCanUpgrade(%q) = false", status)
		}
	}
	for _, status := range []helmrelease.Status{
		helmrelease.StatusPendingInstall,
		helmrelease.StatusPendingUpgrade,
		helmrelease.StatusPendingRollback,
		helmrelease.StatusUninstalled,
	} {
		if helmReleaseCanUpgrade(status) {
			t.Fatalf("helmReleaseCanUpgrade(%q) = true", status)
		}
	}
}

func TestHelmPlanRemovedResourcesUsesHelmIdentityAndKeepPolicy(t *testing.T) {
	client := &Client{}
	byGVK := map[string]domain.ResourceDescriptor{
		"v1/ConfigMap": {Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true},
		"v1/Secret":    {Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true},
	}
	target := []domain.HelmChartPlanResource{{
		APIVersion: "v1", Kind: "ConfigMap", Namespace: "apps", Name: "keep-me",
	}}
	manifest := strings.Join([]string{
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: keep-me\n  namespace: apps\n",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: remove-me\n  namespace: apps\n",
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: retained\n  namespace: apps\n  annotations:\n    helm.sh/resource-policy: keep\n",
	}, "\n---\n")
	removed, warnings := client.helmPlanRemovedResources(manifest, "apps", target, byGVK)
	if len(removed) != 1 || removed[0].Name != "remove-me" || removed[0].Action != "delete" {
		t.Fatalf("removed resources = %#v", removed)
	}
	if !strings.Contains(strings.Join(warnings, "；"), "resource-policy=keep") {
		t.Fatalf("keep-policy warning missing: %#v", warnings)
	}
}

func TestValidateHelmPlanOwnershipAcceptsOnlySameRelease(t *testing.T) {
	owned := map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{"app.kubernetes.io/managed-by": "Helm"},
			"annotations": map[string]any{
				"meta.helm.sh/release-name": "demo", "meta.helm.sh/release-namespace": "apps",
			},
		},
	}
	if err := validateHelmPlanOwnership(owned, "demo", "apps"); err != nil {
		t.Fatalf("owned resource rejected: %v", err)
	}
	if err := validateHelmPlanOwnership(owned, "other", "apps"); err == nil {
		t.Fatal("foreign release was accepted")
	}
	unmanaged := map[string]any{"metadata": map[string]any{"name": "preexisting"}}
	if err := validateHelmPlanOwnership(unmanaged, "demo", "apps"); err == nil {
		t.Fatal("unmanaged resource was accepted")
	}
}

func TestValidateHelmExpectedRevisionRejectsDrift(t *testing.T) {
	if err := validateHelmExpectedRevision(3, 0); err != nil {
		t.Fatalf("unbounded revision guard error = %v", err)
	}
	if err := validateHelmExpectedRevision(3, 3); err != nil {
		t.Fatalf("matching revision error = %v", err)
	}
	if err := validateHelmExpectedRevision(4, 3); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("drifted revision error = %v", err)
	}
}
