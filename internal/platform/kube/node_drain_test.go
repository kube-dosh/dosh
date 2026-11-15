package kube

import (
	"context"
	"testing"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestPlanNodeDrainClassifiesPodsAndPDBs(t *testing.T) {
	controller := true
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	pod := func(name string, owner string, labels map[string]string) *corev1.Pod {
		item := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "prod", Labels: labels}, Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
		if owner != "" {
			item.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: owner, Name: owner + "-owner", Controller: &controller}}
		}
		return item
	}
	pdb := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "api-pdb", Namespace: "prod"}, Spec: policyv1.PodDisruptionBudgetSpec{MinAvailable: intstrPtr(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}}}
	client, _ := newFakeKubeClient(node,
		pod("api-0", "ReplicaSet", map[string]string{"app": "api"}),
		pod("agent-0", "DaemonSet", nil),
		pod("bare-0", "", nil),
		pdb,
	)
	plan, err := client.PlanNodeDrain(context.Background(), "worker-1", domain.NodeDrainOptions{})
	if err != nil {
		t.Fatalf("PlanNodeDrain() error = %v", err)
	}
	actions := map[string]string{}
	for _, item := range plan.Pods {
		actions[item.Name] = item.Action
	}
	if actions["api-0"] != drainActionEvict || actions["agent-0"] != drainActionBlock || actions["bare-0"] != drainActionBlock {
		t.Fatalf("actions = %#v", actions)
	}
	for _, item := range plan.Pods {
		if item.Name == "api-0" && (len(item.PDBNames) != 1 || item.PDBNames[0] != "api-pdb") {
			t.Fatalf("api pod PDBs = %#v", item.PDBNames)
		}
	}
}

func TestClassifyDrainPodSkipsMirrorAndTerminated(t *testing.T) {
	mirror := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mirror", Annotations: map[string]string{"kubernetes.io/config.mirror": "hash"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if item := classifyDrainPod(mirror, domain.NodeDrainOptions{}, nil); item.Action != drainActionSkip {
		t.Fatalf("mirror action = %s", item.Action)
	}
	done := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
	if item := classifyDrainPod(done, domain.NodeDrainOptions{}, nil); item.Action != drainActionSkip {
		t.Fatalf("completed action = %s", item.Action)
	}
}

func intstrPtr(value int) *intstr.IntOrString { parsed := intstr.FromInt(value); return &parsed }
