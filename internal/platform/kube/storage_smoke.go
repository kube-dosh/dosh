package kube

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultStorageSmokeTimeout = 90 * time.Second

// RunStorageSmokeTest creates a short-lived PVC and Pod, waits for dynamic
// provisioning and mount readiness, then removes both objects. The Pod is
// required for WaitForFirstConsumer classes; a PVC-only check would incorrectly
// remain Pending on the most common storage classes.
func (c *Client) RunStorageSmokeTest(ctx context.Context, spec domain.StorageSmokeTestSpec) (result domain.StorageSmokeTestResult, err error) {
	result = domain.StorageSmokeTestResult{
		Name: spec.Namespace + "/" + spec.StorageClass, Namespace: spec.Namespace, StorageClass: spec.StorageClass,
		PVCName: spec.Namespace + "/" + spec.StorageClass, PodName: spec.Namespace + "/" + spec.StorageClass,
	}
	timeout := defaultStorageSmokeTimeout
	if spec.TimeoutSeconds > 0 {
		timeout = time.Duration(spec.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()

	name := smokeTestName(spec.StorageClass)
	result.Name, result.PVCName, result.PodName = name, name, name
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "dosh",
		"dosh.io/storage-smoke-test":   "true",
		"dosh.io/storage-class":        sanitizeLabelValue(spec.StorageClass),
	}
	storageClass := spec.StorageClass
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: spec.Namespace, Labels: labels, Annotations: map[string]string{"dosh.io/storage-smoke-test": "true"}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
			StorageClassName: &storageClass,
		},
	}
	createdPVC, createErr := c.clientset.CoreV1().PersistentVolumeClaims(spec.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if createErr != nil {
		result.Message = "创建 Smoke Test PVC 失败：" + mapKubeError(createErr).Error()
		return result, mapKubeError(createErr)
	}
	cleanupNeeded := true
	defer func() {
		if !cleanupNeeded {
			return
		}
		if cleanupErr := c.cleanupStorageSmokeTest(spec.Namespace, createdPVC.Name); cleanupErr != nil {
			result.Message = strings.TrimSpace(result.Message + " 清理临时资源失败：" + cleanupErr.Error())
			result.CleanedUp = false
			return
		}
		result.CleanedUp = true
	}()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: spec.Namespace, Labels: labels, Annotations: map[string]string{"dosh.io/storage-smoke-test": "true"}},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: boolPtr(false),
			RestartPolicy:                corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "probe",
				Image:           spec.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPtr(false),
					RunAsNonRoot:             boolPtr(true),
					RunAsUser:                int64Ptr(65535),
					RunAsGroup:               int64Ptr(65535),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
			}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: createdPVC.Name}}}},
		},
	}
	if _, createErr := c.clientset.CoreV1().Pods(spec.Namespace).Create(ctx, pod, metav1.CreateOptions{}); createErr != nil {
		result.Message = "创建 Smoke Test Pod 失败：" + mapKubeError(createErr).Error()
		return result, mapKubeError(createErr)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastPVC *corev1.PersistentVolumeClaim
	var lastPod *corev1.Pod
	for {
		currentPVC, pvcErr := c.clientset.CoreV1().PersistentVolumeClaims(spec.Namespace).Get(ctx, createdPVC.Name, metav1.GetOptions{})
		if pvcErr == nil {
			lastPVC = currentPVC
			result.Bound = currentPVC.Status.Phase == corev1.ClaimBound
			result.VolumeName = currentPVC.Spec.VolumeName
		}
		currentPod, podErr := c.clientset.CoreV1().Pods(spec.Namespace).Get(ctx, name, metav1.GetOptions{})
		if podErr == nil {
			lastPod = currentPod
			result.PodRunning = currentPod.Status.Phase == corev1.PodRunning
		}
		if result.Bound && result.PodRunning {
			result.Message = "PVC 已绑定且 Pod 已成功挂载存储。"
			cleanupNeeded = false
			if cleanupErr := c.cleanupStorageSmokeTest(spec.Namespace, createdPVC.Name); cleanupErr != nil {
				result.Message += " 清理临时资源失败：" + cleanupErr.Error()
				result.CleanedUp = false
			} else {
				result.CleanedUp = true
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			result.Message = storageSmokeTimeoutMessage(ctx, name, spec.Namespace, lastPVC, lastPod)
			cleanupNeeded = false
			if cleanupErr := c.cleanupStorageSmokeTest(spec.Namespace, name); cleanupErr != nil {
				result.Message += " 清理临时资源失败：" + cleanupErr.Error()
				result.CleanedUp = false
			} else {
				result.CleanedUp = true
			}
			return result, nil
		case <-ticker.C:
		}
	}
}

func (c *Client) cleanupStorageSmokeTest(namespace, name string) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []string
	if err := c.clientset.CoreV1().Pods(namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{GracePeriodSeconds: int64Ptr(0)}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, "Pod: "+err.Error())
	}
	if err := c.clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, "PVC: "+err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return nil
}

func storageSmokeTimeoutMessage(ctx context.Context, name, namespace string, pvc *corev1.PersistentVolumeClaim, pod *corev1.Pod) string {
	details := make([]string, 0, 3)
	if pvc != nil {
		details = append(details, fmt.Sprintf("PVC 状态=%s", pvc.Status.Phase))
	} else {
		details = append(details, "PVC 尚未可读取")
	}
	if pod != nil {
		phase := string(pod.Status.Phase)
		reason := ""
		if len(pod.Status.ContainerStatuses) > 0 {
			state := pod.Status.ContainerStatuses[0].State
			if state.Waiting != nil {
				reason = state.Waiting.Reason
				if state.Waiting.Message != "" {
					reason += "：" + state.Waiting.Message
				}
			}
		}
		if reason == "" {
			reason = pod.Status.Message
		}
		if reason != "" {
			details = append(details, fmt.Sprintf("Pod 状态=%s（%s）", phase, reason))
		} else {
			details = append(details, "Pod 状态="+phase)
		}
	} else {
		details = append(details, "Pod 尚未可读取")
	}
	if ctx.Err() == context.DeadlineExceeded {
		details = append(details, "等待超时")
	}
	return fmt.Sprintf("%s/%s 存储冒烟测试未完成：%s", namespace, name, strings.Join(details, "；"))
}

func smokeTestName(storageClass string) string {
	suffix := sanitizeDNSLabel(storageClass)
	if len(suffix) > 20 {
		suffix = suffix[:20]
	}
	return fmt.Sprintf("dosh-smoke-%s-%d", suffix, time.Now().UnixNano()%100000000)
}

func sanitizeDNSLabel(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "storage"
	}
	return out
}

func sanitizeLabelValue(value string) string {
	out := sanitizeDNSLabel(value)
	if len(out) > 63 {
		return out[:63]
	}
	return out
}

func boolPtr(value bool) *bool    { return &value }
func int64Ptr(value int64) *int64 { return &value }
