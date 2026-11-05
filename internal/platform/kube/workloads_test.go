package kube

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"dosh/internal/domain"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func resourceQuantity(value string) resource.Quantity { return resource.MustParse(value) }

func newFakeKubeClient(objects ...runtime.Object) (*Client, *k8sfake.Clientset) {
	clientset := k8sfake.NewSimpleClientset(objects...)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client := NewForClients(clientset, dynamicClient, nil, testConfig())
	return client, clientset
}

func testDeployment() *appsv1.Deployment {
	replicas := int32(2)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api", Namespace: "prod", ResourceVersion: "10",
			Labels: map[string]string{"team": "platform"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "registry.test/api:1"}}}},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
}

func testPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "prod", Labels: map[string]string{"app": "api"}},
		Spec: corev1.PodSpec{
			NodeName: "worker-1",
			Containers: []corev1.Container{{
				Name: "api", Image: "registry.test/api:1",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resourceQuantity("100m")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resourceQuantity("64Mi")},
				},
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning, PodIP: "10.0.0.10", QOSClass: corev1.PodQOSBurstable,
			ContainerStatuses: []corev1.ContainerStatus{{Name: "api", Ready: true, RestartCount: 3, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
		},
	}
}

func TestGetWorkloadConvertsSummaryConditionsAndPods(t *testing.T) {
	deployment := testDeployment()
	deployment.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"}}
	client, _ := newFakeKubeClient(deployment, testPod())
	detail, err := client.GetWorkload(context.Background(), "deployment", "prod", "api")
	if err != nil {
		t.Fatalf("GetWorkload() error = %v", err)
	}
	if detail.Kind != "Deployment" || detail.Name != "api" || detail.Ready != "1/2" || detail.Status != "Updating" {
		t.Fatalf("unexpected summary: %#v", detail.WorkloadSummary)
	}
	if detail.ResourceVersion != "10" || detail.Selector["app"] != "api" {
		t.Fatalf("selector/resourceVersion mismatch: %#v", detail)
	}
	if len(detail.Pods) != 1 || detail.Pods[0].Name != "api-0" {
		t.Fatalf("pods = %#v", detail.Pods)
	}
	if detail.Restarts != 3 {
		t.Fatalf("restarts = %d, want 3", detail.Restarts)
	}
	if len(detail.Conditions) != 1 || detail.Conditions[0].Reason != "MinimumReplicasAvailable" {
		t.Fatalf("conditions = %#v", detail.Conditions)
	}
	if len(detail.Containers) != 1 || detail.Containers[0].Name != "api" || detail.Containers[0].Image != "registry.test/api:1" {
		t.Fatalf("containers = %#v", detail.Containers)
	}
}

func TestGetWorkloadMapsNotFound(t *testing.T) {
	client, _ := newFakeKubeClient()
	_, err := client.GetWorkload(context.Background(), "deployment", "prod", "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetWorkload() error = %v, want ErrNotFound", err)
	}
}

func TestListWorkloadsAggregatesFiltersSortsAndCountsRestarts(t *testing.T) {
	replicas := int32(2)
	statefulReplicas := int32(3)
	completions := int32(2)
	suspend := true
	objects := []runtime.Object{
		testDeployment(),
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &statefulReplicas,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "registry.test/db:1"}}}},
			},
			Status: appsv1.StatefulSetStatus{ReadyReplicas: 2, CurrentReplicas: 3},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "prod"},
			Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "agent", Image: "registry.test/worker:1"}}}}},
			Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 1},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "migrate", Namespace: "prod"},
			Spec: batchv1.JobSpec{
				Completions: &completions,
				Template:    corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "migrate", Image: "registry.test/migrate:1"}}}},
			},
			Status: batchv1.JobStatus{Succeeded: 1},
		},
		&batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "prod"},
			Spec: batchv1.CronJobSpec{
				Schedule: "0 * * * *",
				Suspend:  &suspend,
				JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "backup", Image: "registry.test/backup:1"}}}},
				}},
			},
			Status: batchv1.CronJobStatus{Active: []corev1.ObjectReference{{Name: "backup-1"}}},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "other"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "other", Image: "registry.test/other:1"}}}},
			},
		},
		testPod(),
	}
	client, _ := newFakeKubeClient(objects...)
	items, err := client.ListWorkloads(context.Background(), domain.ListQuery{Namespace: "prod"})
	if err != nil {
		t.Fatalf("ListWorkloads() error = %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("workloads = %#v, want 5", items)
	}
	kinds := make([]string, 0, len(items))
	byKind := make(map[string]domain.WorkloadSummary, len(items))
	for _, item := range items {
		kinds = append(kinds, item.Kind)
		byKind[item.Kind] = item
	}
	if strings.Join(kinds, ",") != "CronJob,DaemonSet,Deployment,Job,StatefulSet" {
		t.Fatalf("kind order = %v", kinds)
	}
	if byKind["Deployment"].Restarts != 3 || byKind["Deployment"].Status != "Updating" || byKind["Deployment"].Ready != "1/2" {
		t.Fatalf("deployment summary = %#v", byKind["Deployment"])
	}
	if byKind["StatefulSet"].Status != "Running" || byKind["StatefulSet"].Ready != "2/3" {
		t.Fatalf("statefulset summary = %#v", byKind["StatefulSet"])
	}
	if byKind["DaemonSet"].Status != "Updating" || byKind["DaemonSet"].Ready != "1/2" {
		t.Fatalf("daemonset summary = %#v", byKind["DaemonSet"])
	}
	if byKind["Job"].Status != "Running" || byKind["Job"].Ready != "1/2" {
		t.Fatalf("job summary = %#v", byKind["Job"])
	}
	if byKind["CronJob"].Status != "Suspended" || byKind["CronJob"].Ready != "1" {
		t.Fatalf("cronjob summary = %#v", byKind["CronJob"])
	}

	filtered, err := client.ListWorkloads(context.Background(), domain.ListQuery{Namespace: "prod", Search: "worker"})
	if err != nil || len(filtered) != 1 || filtered[0].Name != "agent" {
		t.Fatalf("filtered workloads = (%#v, %v)", filtered, err)
	}
	noMatches, err := client.ListWorkloads(context.Background(), domain.ListQuery{Namespace: "prod", Search: "no-such-workload"})
	if err != nil || len(noMatches) != 0 {
		t.Fatalf("no-match workloads = (%#v, %v)", noMatches, err)
	}
	updating, err := client.ListWorkloads(context.Background(), domain.ListQuery{Namespace: "prod", Status: "updating"})
	if err != nil || len(updating) != 2 {
		t.Fatalf("updating workloads = (%#v, %v)", updating, err)
	}
	for _, item := range updating {
		if item.Status != "Updating" {
			t.Fatalf("updating filter returned %#v", item)
		}
	}
	runningStatefulSets, err := client.ListWorkloads(context.Background(), domain.ListQuery{Kind: "statefulset", Namespace: "prod", Status: "RUNNING"})
	if err != nil || len(runningStatefulSets) != 1 || runningStatefulSets[0].Name != "db" {
		t.Fatalf("statefulset status filter = (%#v, %v)", runningStatefulSets, err)
	}
	noStatusMatches, err := client.ListWorkloads(context.Background(), domain.ListQuery{Namespace: "prod", Status: "Failed"})
	if err != nil || len(noStatusMatches) != 0 {
		t.Fatalf("no-status-match workloads = (%#v, %v)", noStatusMatches, err)
	}
	for _, kind := range []string{"deployment", "statefulset", "daemonset", "job", "cronjob"} {
		found, err := client.ListWorkloads(context.Background(), domain.ListQuery{Kind: kind, Namespace: "prod"})
		if err != nil || len(found) != 1 || !strings.EqualFold(found[0].Kind, kind) {
			t.Fatalf("ListWorkloads(%s) = (%#v, %v)", kind, found, err)
		}
	}
	if _, err := client.ListWorkloads(context.Background(), domain.ListQuery{Kind: "pod"}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("unsupported kind error = %v", err)
	}
}

func TestWorkloadSummaryStatusBranches(t *testing.T) {
	completed := batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}}
	if summary := jobSummary(completed); summary.Status != "Complete" {
		t.Fatalf("completed job status = %q", summary.Status)
	}
	failed := batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}}
	if summary := jobSummary(failed); summary.Status != "Failed" {
		t.Fatalf("failed job status = %q", summary.Status)
	}
	running := batchv1.CronJob{Status: batchv1.CronJobStatus{Active: []corev1.ObjectReference{{Name: "active"}}}}
	if summary := cronJobSummary(running); summary.Status != "Running" {
		t.Fatalf("running cronjob status = %q", summary.Status)
	}
	for _, test := range []struct {
		desired, ready, available int32
		failed                    bool
		want                      string
	}{
		{desired: 0, want: "Stopped"},
		{desired: 1, ready: 1, want: "Running"},
		{desired: 2, available: 2, want: "Running"},
		{desired: 2, ready: 0, want: "Pending"},
		{desired: 2, ready: 1, want: "Updating"},
		{desired: 2, ready: 2, failed: true, want: "Failed"},
	} {
		if got := replicaStatus(test.desired, test.ready, test.available, test.failed); got != test.want {
			t.Fatalf("replicaStatus(%d,%d,%d,%t) = %q, want %q", test.desired, test.ready, test.available, test.failed, got, test.want)
		}
	}
	if got := readyStatus(0, 0); got != "Stopped" {
		t.Fatalf("readyStatus(0,0) = %q", got)
	}
	if got := readyStatus(3, 2); got != "Running" {
		t.Fatalf("readyStatus(3,2) = %q", got)
	}
	if got := readyStatus(1, 2); got != "Updating" {
		t.Fatalf("readyStatus(1,2) = %q", got)
	}
}

func TestGetWorkloadSupportsAllKindsWithoutNilSelectorPanic(t *testing.T) {
	replicas := int32(2)
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "workload"}}
	jobTemplate := batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "registry.test/workload:1"}}}},
	}}
	objects := []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"},
			Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: selector,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "registry.test/workload:1"}}}}},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"},
			Spec: appsv1.StatefulSetSpec{Replicas: &replicas, Selector: selector,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "registry.test/workload:1"}}}}},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"},
			Spec: appsv1.DaemonSetSpec{Selector: selector,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "registry.test/workload:1"}}}}},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"},
			Spec: batchv1.JobSpec{Selector: selector,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "registry.test/workload:1"}}}}},
		},
		&batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"},
			Spec:       batchv1.CronJobSpec{Schedule: "0 * * * *", JobTemplate: jobTemplate},
		},
	}
	client, _ := newFakeKubeClient(objects...)
	for _, kind := range []string{"deployment", "statefulset", "daemonset", "job", "cronjob"} {
		detail, err := client.GetWorkload(context.Background(), kind, "prod", "workload")
		if err != nil {
			t.Fatalf("GetWorkload(%s) error = %v", kind, err)
		}
		if !strings.EqualFold(detail.Kind, kind) || detail.Selector == nil {
			t.Fatalf("GetWorkload(%s) = %#v", kind, detail)
		}
	}
	if _, err := client.GetWorkload(context.Background(), "pod", "prod", "workload"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("GetWorkload(pod) error = %v", err)
	}
}

func TestRestartAndDeleteSupportAllWorkloadKinds(t *testing.T) {
	replicas := int32(1)
	objects := []runtime.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "prod"}, Spec: batchv1.CronJobSpec{Schedule: "0 * * * *"}},
	}
	client, _ := newFakeKubeClient(objects...)
	for _, kind := range []string{"deployment", "statefulset", "daemonset"} {
		if err := client.RestartWorkload(context.Background(), kind, "prod", "workload"); err != nil {
			t.Fatalf("RestartWorkload(%s) error = %v", kind, err)
		}
	}
	if err := client.RestartWorkload(context.Background(), "cronjob", "prod", "workload"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("RestartWorkload(cronjob) error = %v", err)
	}
	for _, kind := range []string{"deployment", "statefulset", "daemonset", "job", "cronjob"} {
		if err := client.DeleteWorkload(context.Background(), kind, "prod", "workload", domain.DeletePropagationOrphan); err != nil {
			t.Fatalf("DeleteWorkload(%s) error = %v", kind, err)
		}
	}
	if err := client.DeleteWorkload(context.Background(), "pod", "prod", "workload", domain.DeletePropagationBackground); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("DeleteWorkload(pod) error = %v", err)
	}
}

func TestSetCronJobSuspendUsesResourceVersionAndIsIdempotent(t *testing.T) {
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "prod", ResourceVersion: "42"},
		Spec:       batchv1.CronJobSpec{Schedule: "0 * * * *"},
	}
	client, clientset := newFakeKubeClient(cronJob)
	if err := client.SetCronJobSuspend(context.Background(), "prod", "backup", domain.CronJobSuspendUpdate{Suspend: true, ResourceVersion: "42"}); err != nil {
		t.Fatalf("SetCronJobSuspend() error = %v", err)
	}
	updated, err := clientset.BatchV1().CronJobs("prod").Get(context.Background(), "backup", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.Spec.Suspend == nil || !*updated.Spec.Suspend {
		t.Fatalf("CronJob suspend = %#v, want true", updated.Spec.Suspend)
	}

	updateCalls := 0
	clientset.PrependReactor("update", "cronjobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		updateCalls++
		return false, nil, nil
	})
	if err := client.SetCronJobSuspend(context.Background(), "prod", "backup", domain.CronJobSuspendUpdate{Suspend: true, ResourceVersion: "stale"}); err != nil {
		t.Fatalf("idempotent SetCronJobSuspend() error = %v", err)
	}
	if err := client.SetCronJobSuspend(context.Background(), "prod", "backup", domain.CronJobSuspendUpdate{Suspend: false, ResourceVersion: "stale"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale SetCronJobSuspend() error = %v, want ErrConflict", err)
	}
	if updateCalls != 0 {
		t.Fatalf("CronJob updates = %d, want 0 before a matching resourceVersion", updateCalls)
	}

	updated, err = clientset.BatchV1().CronJobs("prod").Get(context.Background(), "backup", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if err := client.SetCronJobSuspend(context.Background(), "prod", "backup", domain.CronJobSuspendUpdate{Suspend: false, ResourceVersion: updated.ResourceVersion}); err != nil {
		t.Fatalf("resume SetCronJobSuspend() error = %v", err)
	}
	updated, err = clientset.BatchV1().CronJobs("prod").Get(context.Background(), "backup", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.Spec.Suspend == nil || *updated.Spec.Suspend {
		t.Fatalf("CronJob suspend = %#v, want false", updated.Spec.Suspend)
	}
}

func TestScaleWorkloadRejectsStaleResourceVersionBeforeUpdate(t *testing.T) {
	client, clientset := newFakeKubeClient()
	updateCalled := false
	clientset.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: "20"}, Spec: autoscalingv1.ScaleSpec{Replicas: 2}}, nil
	})
	clientset.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "scale" {
			updateCalled = true
		}
		return false, nil, nil
	})
	err := client.ScaleWorkload(context.Background(), "deployment", "prod", "api", 3, "19")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ScaleWorkload() error = %v, want ErrConflict", err)
	}
	if updateCalled {
		t.Fatal("UpdateScale was called after a stale resourceVersion check")
	}
}

func TestScaleWorkloadTreatsStaleVersionAsSuccessWhenTargetAlreadyReached(t *testing.T) {
	client, clientset := newFakeKubeClient()
	updateCalled := false
	clientset.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: "20"},
			Spec:       autoscalingv1.ScaleSpec{Replicas: 1},
		}, nil
	})
	clientset.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "scale" {
			updateCalled = true
		}
		return false, nil, nil
	})
	if err := client.ScaleWorkload(context.Background(), "deployment", "prod", "api", 1, "19"); err != nil {
		t.Fatalf("ScaleWorkload() error = %v", err)
	}
	if updateCalled {
		t.Fatal("UpdateScale was called when the requested replica count was already reached")
	}
}

func TestScaleWorkloadUpdatesScaleSubresource(t *testing.T) {
	client, clientset := newFakeKubeClient()
	var updated *autoscalingv1.Scale
	clientset.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: "20"}, Spec: autoscalingv1.ScaleSpec{Replicas: 1}}, nil
	})
	clientset.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		updated = action.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale).DeepCopy()
		return true, updated, nil
	})
	if err := client.ScaleWorkload(context.Background(), "deployment", "prod", "api", 4, "20"); err != nil {
		t.Fatalf("ScaleWorkload() error = %v", err)
	}
	if updated == nil || updated.Spec.Replicas != 4 {
		t.Fatalf("updated scale = %#v", updated)
	}
	if err := client.ScaleWorkload(context.Background(), "daemonset", "prod", "api", 4, ""); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("daemonset scale error = %v, want ErrUnsupported", err)
	}
	if err := client.ScaleWorkload(context.Background(), "deployment", "prod", "api", -1, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative replicas error = %v, want ErrInvalid", err)
	}
}

func TestScaleWorkloadRetriesTransientConflictWithoutPinnedResourceVersion(t *testing.T) {
	client, clientset := newFakeKubeClient()
	getCalls := 0
	updateCalls := 0
	clientset.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		getCalls++
		return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod", ResourceVersion: strconv.Itoa(getCalls)}, Spec: autoscalingv1.ScaleSpec{Replicas: 2}}, nil
	})
	clientset.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		updateCalls++
		scale := action.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
		if updateCalls == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api", errors.New("status changed"))
		}
		return true, scale, nil
	})
	if err := client.ScaleWorkload(context.Background(), "deployment", "prod", "api", 4, ""); err != nil {
		t.Fatalf("ScaleWorkload() error = %v", err)
	}
	if getCalls != 2 || updateCalls != 2 {
		t.Fatalf("scale attempts = get:%d update:%d, want 2 and 2", getCalls, updateCalls)
	}
}

func TestRestartAndDeleteWorkload(t *testing.T) {
	client, clientset := newFakeKubeClient(testDeployment())
	if err := client.RestartWorkload(context.Background(), "deployment", "prod", "api"); err != nil {
		t.Fatalf("RestartWorkload() error = %v", err)
	}
	updated, err := clientset.AppsV1().Deployments("prod").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get restarted deployment: %v", err)
	}
	if updated.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatalf("restart annotation missing: %#v", updated.Spec.Template.Annotations)
	}
	if err := client.RestartWorkload(context.Background(), "job", "prod", "api"); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("job restart error = %v, want ErrUnsupported", err)
	}

	if err := client.DeleteWorkload(context.Background(), "deployment", "prod", "api", domain.DeletePropagationForeground); err != nil {
		t.Fatalf("DeleteWorkload() error = %v", err)
	}
	_, err = client.GetWorkload(context.Background(), "deployment", "prod", "api")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted deployment get error = %v, want ErrNotFound", err)
	}
}

func TestUpdateWorkloadImageSupportsApplicationKinds(t *testing.T) {
	replicas := int32(1)
	deployment := testDeployment()
	deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, corev1.Container{Name: "sidecar", Image: "registry.test/sidecar:1"})
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod", ResourceVersion: "20"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "registry.test/db:1"}}}}},
	}
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "prod", ResourceVersion: "30"},
		Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "agent", Image: "registry.test/agent:1"}}}}},
	}
	client, clientset := newFakeKubeClient(deployment, statefulSet, daemonSet)
	tests := []struct {
		kind, name, container, version string
	}{
		{kind: "deployment", name: "api", container: "api", version: "10"},
		{kind: "statefulset", name: "db", container: "db", version: "20"},
		{kind: "daemonset", name: "agent", container: "agent", version: "30"},
	}
	for _, test := range tests {
		wantImage := "registry.test/" + test.container + ":2"
		if err := client.UpdateWorkloadImage(context.Background(), test.kind, "prod", test.name, domain.WorkloadImageUpdate{
			ContainerName: test.container, Image: wantImage, ResourceVersion: test.version,
		}); err != nil {
			t.Fatalf("UpdateWorkloadImage(%s) error = %v", test.kind, err)
		}
		detail, err := client.GetWorkload(context.Background(), test.kind, "prod", test.name)
		if err != nil {
			t.Fatalf("GetWorkload(%s) error = %v", test.kind, err)
		}
		found := false
		for _, container := range detail.Containers {
			if container.Name == test.container {
				found = true
				if container.Image != wantImage {
					t.Fatalf("%s image = %q, want %q", test.kind, container.Image, wantImage)
				}
			}
		}
		if !found {
			t.Fatalf("%s container %q missing from detail: %#v", test.kind, test.container, detail.Containers)
		}
	}
	updated, err := clientset.AppsV1().Deployments("prod").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated deployment: %v", err)
	}
	if updated.Spec.Template.Spec.Containers[1].Image != "registry.test/sidecar:1" {
		t.Fatalf("sidecar image changed unexpectedly: %#v", updated.Spec.Template.Spec.Containers)
	}
	if err := client.UpdateWorkloadImage(context.Background(), "job", "prod", "migrate", domain.WorkloadImageUpdate{ContainerName: "migrate", Image: "registry.test/migrate:2"}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("job image update error = %v, want ErrUnsupported", err)
	}
}

func TestUpdateWorkloadImageRejectsStaleVersionAndMissingContainer(t *testing.T) {
	client, clientset := newFakeKubeClient(testDeployment())
	update := domain.WorkloadImageUpdate{ContainerName: "api", Image: "registry.test/api:2", ResourceVersion: "9"}
	if err := client.UpdateWorkloadImage(context.Background(), "deployment", "prod", "api", update); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale image update error = %v, want ErrConflict", err)
	}
	deployment, err := clientset.AppsV1().Deployments("prod").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment after conflict: %v", err)
	}
	if deployment.Spec.Template.Spec.Containers[0].Image != "registry.test/api:1" {
		t.Fatalf("image changed after conflict: %#v", deployment.Spec.Template.Spec.Containers[0])
	}
	update.ResourceVersion = "8"
	update.Image = "registry.test/api:1"
	if err := client.UpdateWorkloadImage(context.Background(), "deployment", "prod", "api", update); err != nil {
		t.Fatalf("idempotent stale image update error = %v", err)
	}
	update = domain.WorkloadImageUpdate{ContainerName: "missing", Image: "registry.test/api:2", ResourceVersion: "10"}
	if err := client.UpdateWorkloadImage(context.Background(), "deployment", "prod", "api", update); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing container error = %v, want ErrInvalid", err)
	}
}

func TestUpdateWorkloadImageMapsKubernetesConflict(t *testing.T) {
	client, clientset := newFakeKubeClient(testDeployment())
	clientset.PrependReactor("update", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api", errors.New("concurrent update"))
	})
	err := client.UpdateWorkloadImage(context.Background(), "deployment", "prod", "api", domain.WorkloadImageUpdate{
		ContainerName: "api", Image: "registry.test/api:2", ResourceVersion: "10",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("UpdateWorkloadImage() error = %v, want ErrConflict", err)
	}
}

func TestCreateDeploymentDryRunAndObjectShape(t *testing.T) {
	client, clientset := newFakeKubeClient()
	var dryRuns []bool
	var submitted []*appsv1.Deployment
	clientset.PrependReactor("create", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		create := action.(k8stesting.CreateAction)
		options := action.(k8stesting.CreateActionImpl).GetCreateOptions()
		dryRuns = append(dryRuns, len(options.DryRun) == 1 && options.DryRun[0] == metav1.DryRunAll)
		deployment := create.GetObject().(*appsv1.Deployment).DeepCopy()
		submitted = append(submitted, deployment)
		return true, deployment, nil
	})
	spec := domain.DeploymentCreateSpec{
		Name: "api", Namespace: "prod", Replicas: 3, ContainerName: "api", Image: "registry.test/api:1",
		ContainerPort: 8080, CPURequestMilli: 100, CPULimitMilli: 500,
		MemoryRequestBytes: 128 << 20, MemoryLimitBytes: 512 << 20,
		Env:    []domain.DeploymentEnvVar{{Name: "MODE", Value: "production"}},
		Labels: map[string]string{"team": "platform", "../bad": "ignored"},
	}
	for _, dryRun := range []bool{true, false} {
		if _, err := client.CreateDeployment(context.Background(), spec, dryRun); err != nil {
			t.Fatalf("CreateDeployment(dryRun=%t) error = %v", dryRun, err)
		}
	}
	if len(dryRuns) != 2 || !dryRuns[0] || dryRuns[1] {
		t.Fatalf("dry-run options = %#v, want [true false]", dryRuns)
	}
	deployment := submitted[1]
	if deployment.Name != "api" || deployment.Namespace != "prod" || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 3 {
		t.Fatalf("deployment metadata/spec = %#v", deployment)
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Name != "api" || container.Image != "registry.test/api:1" || container.Ports[0].ContainerPort != 8080 {
		t.Fatalf("container = %#v", container)
	}
	if got := deployment.Spec.Template.Labels[managedByNameLabel]; got != "api" {
		t.Fatalf("managed-by-name label = %q", got)
	}
	if got := container.Resources.Requests.Cpu().String(); got != "100m" {
		t.Fatalf("CPU request = %q", got)
	}
	if got := container.Resources.Limits.Memory().String(); got != "512Mi" {
		t.Fatalf("memory limit = %q", got)
	}
	if len(container.Env) != 1 || container.Env[0].Name != "MODE" || container.Env[0].Value != "production" {
		t.Fatalf("environment = %#v", container.Env)
	}
}

func TestCreateDeploymentMapsAlreadyExistsAndPreviewMatches(t *testing.T) {
	client, clientset := newFakeKubeClient()
	clientset.PrependReactor("create", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api")
	})
	spec := domain.DeploymentCreateSpec{Name: "api", Namespace: "prod", Replicas: 1, Image: "nginx:alpine", ContainerPort: 80}
	if _, err := client.CreateDeployment(context.Background(), spec, true); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("CreateDeployment() error = %v, want ErrAlreadyExists", err)
	}
	preview, err := client.PreviewDeployment(context.Background(), spec)
	if err != nil {
		t.Fatalf("PreviewDeployment() error = %v", err)
	}
	for _, fragment := range []string{"apiVersion: apps/v1", "kind: Deployment", "name: api", "image: nginx:alpine", "containerPort: 80"} {
		if !strings.Contains(preview, fragment) {
			t.Fatalf("preview missing %q:\n%s", fragment, preview)
		}
	}
}
