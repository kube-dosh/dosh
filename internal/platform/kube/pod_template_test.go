package kube

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"dosh/internal/domain"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stesting "k8s.io/client-go/testing"
)

func podTemplateFixture() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: "10"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api", "team": "platform"}},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "migrate", Image: "registry.test/migrate:1"}},
					Containers: []corev1.Container{
						{Name: "api", Image: "registry.test/api:1"},
						{Name: "metrics", Image: "registry.test/metrics:1"},
					},
				},
			},
		},
	}
}

func templateContainersForTest(document map[string]any, init bool) []any {
	spec := document["spec"].(map[string]any)
	key := "containers"
	if init {
		key = "initContainers"
	}
	return spec[key].([]any)
}

func updateContainerImageForTest(document map[string]any, init bool, name, image string) {
	for _, item := range templateContainersForTest(document, init) {
		container := item.(map[string]any)
		if container["name"] == name {
			container["image"] = image
			return
		}
	}
}

func TestUpdateWorkloadPodTemplateUpdatesInitAndMultipleContainersAtomically(t *testing.T) {
	client, clientset := newFakeKubeClient(podTemplateFixture())
	current, err := client.GetWorkloadPodTemplate(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkloadPodTemplate() error = %v", err)
	}
	document := cloneMapForTest(current.Template)
	updateContainerImageForTest(document, false, "api", "registry.test/api:2")
	updateContainerImageForTest(document, false, "metrics", "registry.test/metrics:2")
	updateContainerImageForTest(document, true, "migrate", "registry.test/migrate:2")

	result, err := client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion:  current.ResourceVersion,
		Template:         document,
		ChangeCause:      "release api:2",
		AcknowledgeRisks: true,
	})
	if err != nil {
		t.Fatalf("UpdateWorkloadPodTemplate() error = %v", err)
	}
	if !result.Changed || len(result.Diff) == 0 || result.Rollout.Effect != "rollingUpdate" {
		t.Fatalf("unexpected result: %#v", result)
	}
	updated, err := clientset.AppsV1().Deployments("prod").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated deployment: %v", err)
	}
	if updated.Spec.Template.Spec.Containers[0].Image != "registry.test/api:2" || updated.Spec.Template.Spec.Containers[1].Image != "registry.test/metrics:2" || updated.Spec.Template.Spec.InitContainers[0].Image != "registry.test/migrate:2" {
		t.Fatalf("container images were not updated together: %#v", updated.Spec.Template.Spec)
	}
	if updated.Spec.Template.Annotations[changeCauseAnnotation] != "release api:2" {
		t.Fatalf("change cause = %q", updated.Spec.Template.Annotations[changeCauseAnnotation])
	}
	updates := 0
	for _, action := range clientset.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "deployments" {
			updates++
			update, ok := action.(k8stesting.UpdateAction)
			if !ok {
				t.Fatalf("update action type = %T", action)
			}
			if got := update.GetObject().(metav1.Object).GetResourceVersion(); got != current.ResourceVersion {
				t.Fatalf("update resourceVersion = %q, want %q", got, current.ResourceVersion)
			}
		}
	}
	if updates != 2 {
		t.Fatalf("deployment update actions = %d, want one API Server dry run plus one commit", updates)
	}
}

func TestUpdateWorkloadPodTemplateKeepsUnknownFieldsAndDetectsNoChange(t *testing.T) {
	fixture := podTemplateFixture()
	grace := int64(42)
	fixture.Spec.Template.Spec.TerminationGracePeriodSeconds = &grace
	fixture.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
	client, _ := newFakeKubeClient(fixture)
	current, err := client.GetWorkloadPodTemplate(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkloadPodTemplate() error = %v", err)
	}
	result, err := client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: current.ResourceVersion,
		Template:        cloneMapForTest(current.Template),
	})
	if err != nil {
		t.Fatalf("UpdateWorkloadPodTemplate(no change) error = %v", err)
	}
	if result.Changed || len(result.Diff) != 0 {
		t.Fatalf("no-change result = %#v", result)
	}
	spec := result.Template["spec"].(map[string]any)
	if spec["terminationGracePeriodSeconds"] != float64(42) && spec["terminationGracePeriodSeconds"] != int64(42) {
		t.Fatalf("unknown field was lost: %#v", spec["terminationGracePeriodSeconds"])
	}
}

func TestUpdateWorkloadPodTemplateWarnsOnlyForNewRisks(t *testing.T) {
	fixture := podTemplateFixture()
	fixture.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: ptr(true)}
	client, _ := newFakeKubeClient(fixture)
	current, err := client.GetWorkloadPodTemplate(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkloadPodTemplate() error = %v", err)
	}
	imageOnly := cloneMapForTest(current.Template)
	updateContainerImageForTest(imageOnly, false, "api", "registry.test/api:2")
	result, err := client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: current.ResourceVersion,
		Template:        imageOnly,
	})
	if err != nil {
		t.Fatalf("existing warning blocked image update: %v", err)
	}
	if len(result.Warnings) == 0 || len(result.NewWarnings) != 0 {
		t.Fatalf("warnings = %#v, newWarnings = %#v", result.Warnings, result.NewWarnings)
	}

	risky := cloneMapForTest(current.Template)
	risky["spec"].(map[string]any)["hostNetwork"] = true
	_, err = client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: current.ResourceVersion,
		Template:        risky,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("new risk without acknowledgement error = %v, want ErrInvalid", err)
	}
	riskyResult, err := client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion:  current.ResourceVersion,
		Template:         risky,
		AcknowledgeRisks: true,
	})
	if err != nil {
		t.Fatalf("acknowledged risk update error = %v", err)
	}
	if len(riskyResult.NewWarnings) == 0 {
		t.Fatal("newWarnings must describe the newly introduced hostNetwork risk")
	}
}

func TestUpdateWorkloadPodTemplateRejectsSelectorAndVersionChanges(t *testing.T) {
	client, _ := newFakeKubeClient(podTemplateFixture())
	current, err := client.GetWorkloadPodTemplate(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkloadPodTemplate() error = %v", err)
	}
	selectorChange := cloneMapForTest(current.Template)
	metadata := selectorChange["metadata"].(map[string]any)
	metadata["labels"] = map[string]any{"app": "other"}
	_, err = client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: current.ResourceVersion,
		Template:        selectorChange,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("selector change error = %v, want ErrInvalid", err)
	}
	_, err = client.UpdateWorkloadPodTemplate(context.Background(), "deployment", "prod", "api", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: "stale",
		Template:        cloneMapForTest(current.Template),
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale resourceVersion error = %v, want ErrConflict", err)
	}
}

func TestWorkloadPodTemplateJobIsReadOnly(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "prod", ResourceVersion: "3"},
		Spec:       batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "backup", Image: "registry.test/backup:1"}}}}},
	}
	client, _ := newFakeKubeClient(job)
	template, err := client.GetWorkloadPodTemplate(context.Background(), "job", "prod", "backup")
	if err != nil {
		t.Fatalf("GetWorkloadPodTemplate(job) error = %v", err)
	}
	if template.Editable || len(template.Restrictions) == 0 {
		t.Fatalf("job template = %#v", template)
	}
	_, err = client.UpdateWorkloadPodTemplate(context.Background(), "job", "prod", "backup", domain.WorkloadPodTemplateUpdate{
		ResourceVersion: "3", Template: cloneMapForTest(template.Template),
	})
	if !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("job update error = %v, want ErrUnsupported", err)
	}
}

func TestGetWorkloadContainerInventoryIdentifiesSidecarsAndInstances(t *testing.T) {
	fixture := podTemplateFixture()
	fixture.Spec.Template.Spec.InitContainers = []corev1.Container{
		{Name: "migrate", Image: "registry.test/migrate:1"},
		{Name: "proxy", Image: "registry.test/proxy:1", RestartPolicy: ptr(corev1.ContainerRestartPolicyAlways)},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-abc", Namespace: "prod", Labels: map[string]string{"app": "api"}},
		Spec: corev1.PodSpec{
			NodeName:       "worker-1",
			InitContainers: fixture.Spec.Template.Spec.InitContainers,
			Containers:     fixture.Spec.Template.Spec.Containers,
		},
		Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "migrate", Image: "registry.test/migrate:1", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}},
			{Name: "proxy", Image: "registry.test/proxy:1", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		}},
	}
	client, _ := newFakeKubeClient(fixture, pod)
	inventory, err := client.GetWorkloadContainerInventory(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkloadContainerInventory() error = %v", err)
	}
	if len(inventory.Containers) != 4 {
		t.Fatalf("containers = %d, want 4", len(inventory.Containers))
	}
	byName := map[string]domain.WorkloadContainerState{}
	for _, container := range inventory.Containers {
		byName[container.Name] = container
	}
	if byName["proxy"].Type != "sidecar" || !byName["proxy"].Sidecar || !byName["proxy"].Init {
		t.Fatalf("sidecar classification = %#v", byName["proxy"])
	}
	if byName["proxy"].Runtime.Running != 1 || len(byName["proxy"].Instances) != 1 || byName["proxy"].Instances[0].Pod != "api-abc" {
		t.Fatalf("sidecar runtime = %#v", byName["proxy"])
	}
	if byName["migrate"].Type != "initContainer" || byName["migrate"].Instances[0].State != "Terminated" {
		t.Fatalf("init runtime = %#v", byName["migrate"])
	}
}

func cloneMapForTest(value map[string]any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		panic(err)
	}
	return clone
}
