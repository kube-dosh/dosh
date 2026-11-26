package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const portForwardReadyTimeout = 15 * time.Second
const portForwardBackendScanLimit = 100

// ResolvePortForwardTarget validates a Pod or Service port and resolves a
// Service to one ready backing Pod. Service selector resolution intentionally
// supports the standard label-selector path; ExternalName and selector-less
// Services fail closed.
func (c *Client) ResolvePortForwardTarget(ctx context.Context, target domain.PortForwardTarget) (domain.PortForwardPod, error) {
	if c.clientset == nil {
		return domain.PortForwardPod{}, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	if target.Namespace == "" || target.Name == "" || target.Port < 1 || target.Port > 65535 {
		return domain.PortForwardPod{}, fmt.Errorf("%w: namespace, name and a port between 1 and 65535 are required", domain.ErrInvalid)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	switch target.Kind {
	case "pod":
		pod, err := c.clientset.CoreV1().Pods(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return domain.PortForwardPod{}, mapKubeError(err)
		}
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			return domain.PortForwardPod{}, fmt.Errorf("%w: Pod %s/%s is not running", domain.ErrUnavailable, target.Namespace, target.Name)
		}
		return domain.PortForwardPod{Namespace: target.Namespace, Pod: target.Name, Port: target.Port}, nil
	case "service":
		service, err := c.clientset.CoreV1().Services(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return domain.PortForwardPod{}, mapKubeError(err)
		}
		servicePort, ok := serviceForwardPort(service, target.Port)
		if !ok {
			return domain.PortForwardPod{}, fmt.Errorf("%w: Service %s/%s does not expose port %d", domain.ErrNotFound, target.Namespace, target.Name, target.Port)
		}
		if service.Spec.Type == corev1.ServiceTypeExternalName || len(service.Spec.Selector) == 0 {
			return domain.PortForwardPod{}, fmt.Errorf("%w: Service port-forward requires a selector-backed Service", domain.ErrUnsupported)
		}
		if target.PodName != "" {
			pod, getErr := c.clientset.CoreV1().Pods(target.Namespace).Get(ctx, target.PodName, metav1.GetOptions{})
			if getErr != nil {
				return domain.PortForwardPod{}, mapKubeError(getErr)
			}
			if !podMatchesSelector(pod, service.Spec.Selector) {
				return domain.PortForwardPod{}, fmt.Errorf("%w: Pod %s/%s is not a backend of Service %s", domain.ErrInvalid, target.Namespace, target.PodName, target.Name)
			}
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || !podReady(pod) {
				return domain.PortForwardPod{}, fmt.Errorf("%w: Pod %s/%s is not ready", domain.ErrUnavailable, target.Namespace, target.PodName)
			}
			remotePort, portErr := resolvedTargetPort(servicePort, pod)
			if portErr != nil {
				return domain.PortForwardPod{}, portErr
			}
			return domain.PortForwardPod{Namespace: target.Namespace, Pod: pod.Name, Service: service.Name, Port: remotePort}, nil
		}
		pods, err := c.clientset.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selectorString(service.Spec.Selector), FieldSelector: "status.phase=Running", Limit: portForwardBackendScanLimit,
		})
		if err != nil {
			return domain.PortForwardPod{}, mapKubeError(err)
		}
		sort.SliceStable(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
		var targetPortErr error
		for index := range pods.Items {
			pod := &pods.Items[index]
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || !podReady(pod) {
				continue
			}
			remotePort, err := resolvedTargetPort(servicePort, pod)
			if err != nil {
				targetPortErr = err
				continue
			}
			return domain.PortForwardPod{Namespace: target.Namespace, Pod: pod.Name, Service: service.Name, Port: remotePort}, nil
		}
		if targetPortErr != nil {
			return domain.PortForwardPod{}, targetPortErr
		}
		return domain.PortForwardPod{}, fmt.Errorf("%w: Service %s/%s has no ready backing Pod", domain.ErrUnavailable, target.Namespace, target.Name)
	default:
		return domain.PortForwardPod{}, fmt.Errorf("%w: port-forward target kind must be pod or service", domain.ErrInvalid)
	}
}

// ListPortForwardBackendPods returns every Pod currently selected by a Service.
// The result is selector-scoped rather than a bounded slice of all namespace
// Pods, so the UI can reliably pin a multi-port tunnel to any ready replica.
func (c *Client) ListPortForwardBackendPods(ctx context.Context, namespace, serviceName string) ([]domain.PodSummary, error) {
	if c.clientset == nil {
		return nil, fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	namespace, serviceName = strings.TrimSpace(namespace), strings.TrimSpace(serviceName)
	if namespace == "" || serviceName == "" {
		return nil, fmt.Errorf("%w: namespace and Service name are required", domain.ErrInvalid)
	}
	serviceCtx, cancel := c.withTimeout(ctx)
	service, err := c.clientset.CoreV1().Services(namespace).Get(serviceCtx, serviceName, metav1.GetOptions{})
	cancel()
	if err != nil {
		return nil, mapKubeError(err)
	}
	if service.Spec.Type == corev1.ServiceTypeExternalName || len(service.Spec.Selector) == 0 {
		return nil, fmt.Errorf("%w: Service port-forward requires a selector-backed Service", domain.ErrUnsupported)
	}
	items, err := c.ListPodsBySelector(ctx, namespace, service.Spec.Selector, "")
	if err != nil {
		return nil, err
	}
	return items, nil
}

func podMatchesSelector(pod *corev1.Pod, selector map[string]string) bool {
	for key, expected := range selector {
		if pod.Labels[key] != expected {
			return false
		}
	}
	return true
}

func serviceForwardPort(service *corev1.Service, port int32) (corev1.ServicePort, bool) {
	for _, item := range service.Spec.Ports {
		if item.Port == port {
			if item.Protocol != "" && item.Protocol != corev1.ProtocolTCP {
				return corev1.ServicePort{}, false
			}
			return item, true
		}
	}
	return corev1.ServicePort{}, false
}

func resolvedTargetPort(servicePort corev1.ServicePort, pod *corev1.Pod) (int32, error) {
	if servicePort.TargetPort.IntVal > 0 {
		return servicePort.TargetPort.IntVal, nil
	}
	if servicePort.TargetPort.StrVal == "" {
		return servicePort.Port, nil
	}
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == servicePort.TargetPort.StrVal && (port.Protocol == "" || port.Protocol == corev1.ProtocolTCP) {
				return port.ContainerPort, nil
			}
		}
	}
	return 0, fmt.Errorf("%w: named target port %q is not exposed by Pod %s/%s", domain.ErrNotFound, servicePort.TargetPort.StrVal, pod.Namespace, pod.Name)
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// OpenPodPortForward creates a loopback-only listener backed by the Kubernetes
// Pod port-forward subresource. Impersonation is captured when the session is
// opened so the SPDY dial, which has no request context, cannot lose identity.
func (c *Client) OpenPodPortForward(ctx context.Context, namespace, name string, port int32) (string, io.Closer, error) {
	if c.restConfig == nil {
		return "", nil, fmt.Errorf("%w: Kubernetes REST configuration is unavailable", domain.ErrUnavailable)
	}
	if namespace == "" || name == "" || port < 1 || port > 65535 {
		return "", nil, fmt.Errorf("%w: namespace, Pod name and a port between 1 and 65535 are required", domain.ErrInvalid)
	}
	config := rest.CopyConfig(c.restConfig)
	if actor, ok := domain.ActorFromContext(ctx); ok && actor.Username != "" {
		config.Impersonate = rest.ImpersonationConfig{UserName: actor.Username, Groups: actor.Groups}
	}
	roundTripper, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return "", nil, fmt.Errorf("%w: prepare Pod port-forward transport: %v", domain.ErrUnavailable, err)
	}
	target, err := url.Parse(config.Host)
	if err != nil {
		return "", nil, fmt.Errorf("%w: invalid Kubernetes API server URL", domain.ErrUnavailable)
	}
	target.Path = path.Join(target.Path, "/api/v1/namespaces", url.PathEscape(namespace), "pods", url.PathEscape(name), "portforward")
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, target)
	stop := make(chan struct{})
	ready := make(chan struct{})
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("0:%d", port)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		return "", nil, fmt.Errorf("%w: create Pod port-forward: %v", domain.ErrUnavailable, err)
	}
	done := make(chan error, 1)
	go func() { done <- forwarder.ForwardPorts() }()
	select {
	case <-ready:
	case forwardErr := <-done:
		close(stop)
		if forwardErr == nil {
			forwardErr = errors.New("port-forward stopped before becoming ready")
		}
		return "", nil, fmt.Errorf("%w: %v", domain.ErrUnavailable, forwardErr)
	case <-time.After(portForwardReadyTimeout):
		close(stop)
		return "", nil, fmt.Errorf("%w: Pod port-forward did not become ready", domain.ErrUnavailable)
	case <-ctx.Done():
		close(stop)
		return "", nil, ctx.Err()
	}
	ports, err := forwarder.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stop)
		return "", nil, fmt.Errorf("%w: read Pod port-forward listener", domain.ErrUnavailable)
	}
	return fmt.Sprintf("127.0.0.1:%d", ports[0].Local), &portForwardCloser{stop: stop, done: done}, nil
}

type portForwardCloser struct {
	stop chan struct{}
	done chan error
	once sync.Once
}

func (c *portForwardCloser) Close() error {
	c.once.Do(func() {
		close(c.stop)
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
		}
	})
	return nil
}
