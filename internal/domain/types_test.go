package domain

import (
	"context"
	"errors"
	"testing"
)

func TestActorContextRoundTripAndRemoval(t *testing.T) {
	actor := Actor{Username: "alice@example.test", Groups: []string{"dosh-operators"}, Admin: true}
	ctx := WithActor(context.Background(), actor)

	got, ok := ActorFromContext(ctx)
	if !ok {
		t.Fatal("ActorFromContext() ok = false, want true")
	}
	if got.Username != actor.Username || got.Admin != actor.Admin || len(got.Groups) != 1 || got.Groups[0] != actor.Groups[0] {
		t.Fatalf("ActorFromContext() = %#v, want %#v", got, actor)
	}

	if _, ok := ActorFromContext(WithoutActor(ctx)); ok {
		t.Fatal("ActorFromContext(WithoutActor(ctx)) ok = true, want false")
	}
	if _, ok := ActorFromContext(context.Background()); ok {
		t.Fatal("ActorFromContext(background) ok = true, want false")
	}
}

func TestParseResourceType(t *testing.T) {
	valid := []ResourceType{
		ResourceNode, ResourceNamespace, ResourcePod, ResourceDeployment,
		ResourceStatefulSet, ResourceDaemonSet, ResourceJob, ResourceCronJob,
		ResourceService, ResourceIngress, ResourceConfigMap, ResourceSecret,
		ResourceStorageClass, ResourcePersistentVolume, ResourcePersistentVolumeClaim,
	}
	for _, resourceType := range valid {
		got, ok := ParseResourceType(string(resourceType))
		if !ok || got != resourceType {
			t.Fatalf("ParseResourceType(%q) = (%q, %v), want (%q, true)", resourceType, got, ok, resourceType)
		}
	}
	for _, invalid := range []string{"", "pods", "deployment/status", "Node"} {
		if got, ok := ParseResourceType(invalid); ok || got != "" {
			t.Fatalf("ParseResourceType(%q) = (%q, %v), want (\"\", false)", invalid, got, ok)
		}
	}
}

func TestValidationErrorUnwrapsInvalidRequest(t *testing.T) {
	err := &ValidationError{Field: "metadata.name", Message: "is required"}
	if err.Error() != "metadata.name: is required" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("ValidationError does not unwrap to ErrInvalid")
	}
	if (&ValidationError{Message: "bad input"}).Error() != "bad input" {
		t.Fatal("fieldless ValidationError should return only its message")
	}
}
