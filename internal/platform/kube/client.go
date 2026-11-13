package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/config"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Client struct {
	clientset      kubernetes.Interface
	dynamic        dynamic.Interface
	discovery      discovery.DiscoveryInterface
	metrics        metricsclient.Interface
	requestTimeout time.Duration
	clusterName    string
	tokenAudience  string

	metricsMu        sync.Mutex
	metricsCheckedAt time.Time
	metricsAvailable bool

	resourceDiscoveryMu       sync.Mutex
	resourceDiscovery         []domain.ResourceDescriptor
	resourceDiscoveryWarnings []string
	resourceDiscoveryFetched  time.Time
}

func New(ctx context.Context, cfg config.Config) (*Client, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
		restConfig, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("load Kubernetes config: %w", err)
		}
	}
	restConfig.QPS = cfg.KubeQPS
	restConfig.Burst = cfg.KubeBurst
	restConfig.UserAgent = "dosh/0.1"
	return NewFromRESTConfig(restConfig, cfg)
}

func NewFromRESTConfig(restConfig *rest.Config, cfg config.Config) (*Client, error) {
	restConfig = prepareRESTConfig(restConfig, cfg)
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes clientset: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes discovery client: %w", err)
	}
	metricsClient, err := metricsclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes metrics client: %w", err)
	}
	return NewForClientsWithMetrics(clientset, dynamicClient, discoveryClient, metricsClient, cfg), nil
}

func prepareRESTConfig(restConfig *rest.Config, cfg config.Config) *rest.Config {
	restConfig = rest.CopyConfig(restConfig)
	if cfg.KubeQPS > 0 {
		restConfig.QPS = cfg.KubeQPS
	}
	if cfg.KubeBurst > 0 {
		restConfig.Burst = cfg.KubeBurst
	}
	restConfig.UserAgent = "dosh/0.1"
	// A global REST timeout also terminates watch and log-follow requests. Normal
	// repository methods apply cfg.RequestTimeout through withTimeout instead.
	restConfig.Timeout = 0
	if cfg.KubeAPIServer != "" {
		restConfig.Host = cfg.KubeAPIServer
	}
	if cfg.InsecureSkipVerify {
		restConfig.TLSClientConfig.Insecure = true
		restConfig.TLSClientConfig.CAData = nil
		restConfig.TLSClientConfig.CAFile = ""
	}
	restConfig.WrapTransport = func(base http.RoundTripper) http.RoundTripper {
		var transport http.RoundTripper = &retryTransport{base: base}
		if cfg.AuthMode == "oidc" || cfg.AuthMode == "token" {
			transport = &impersonationTransport{base: transport}
		}
		return transport
	}
	return restConfig
}

func NewForClients(clientset kubernetes.Interface, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, cfg config.Config) *Client {
	return NewForClientsWithMetrics(clientset, dynamicClient, discoveryClient, nil, cfg)
}

func NewForClientsWithMetrics(clientset kubernetes.Interface, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, metricsClient metricsclient.Interface, cfg config.Config) *Client {
	return &Client{
		clientset:      clientset,
		dynamic:        dynamicClient,
		discovery:      discoveryClient,
		metrics:        metricsClient,
		requestTimeout: cfg.RequestTimeout,
		clusterName:    cfg.ClusterName,
		tokenAudience:  cfg.TokenReviewAudience,
	}
}

func (c *Client) ClusterName() string { return c.clusterName }

func (c *Client) Version(ctx context.Context) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	info, err := c.serverVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	return info.GitVersion, nil
}

func (c *Client) MetricsAvailable(ctx context.Context) bool {
	if c.metrics == nil {
		return false
	}
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	if time.Since(c.metricsCheckedAt) < 30*time.Second {
		return c.metricsAvailable
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err := c.serverResourcesForGroupVersion(ctx, "metrics.k8s.io/v1beta1")
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return c.metricsAvailable
	}
	c.metricsAvailable = err == nil
	c.metricsCheckedAt = time.Now()
	return c.metricsAvailable
}

func (c *Client) DetectCNI(ctx context.Context) string {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	groups, err := c.serverGroups(ctx)
	if err == nil {
		for _, group := range groups.Groups {
			if group.Name == "cilium.io" {
				return "Cilium"
			}
		}
	}
	return "Unknown"
}

func (c *Client) serverVersion(ctx context.Context) (*version.Info, error) {
	restClient := c.discovery.RESTClient()
	if restClient == nil {
		return c.discovery.ServerVersion()
	}
	body, err := restClient.Get().AbsPath("/version").DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var info version.Info
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode Kubernetes version: %w", err)
	}
	return &info, nil
}

func (c *Client) serverResourcesForGroupVersion(ctx context.Context, groupVersion string) (*metav1.APIResourceList, error) {
	restClient := c.discovery.RESTClient()
	if restClient == nil {
		return c.discovery.ServerResourcesForGroupVersion(groupVersion)
	}
	path := "/apis/" + groupVersion
	if groupVersion == "v1" {
		path = "/api/v1"
	}
	body, err := restClient.Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var resources metav1.APIResourceList
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("decode Kubernetes resources for %s: %w", groupVersion, err)
	}
	return &resources, nil
}

func (c *Client) serverGroups(ctx context.Context) (*metav1.APIGroupList, error) {
	restClient := c.discovery.RESTClient()
	if restClient == nil {
		return c.discovery.ServerGroups()
	}
	body, err := restClient.Get().AbsPath("/apis").DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var groups metav1.APIGroupList
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("decode Kubernetes API groups: %w", err)
	}
	return &groups, nil
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.requestTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}

func mapKubeError(err error) error {
	if err == nil {
		return nil
	}
	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("%w: %s", domain.ErrAlreadyExists, err.Error())
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	case errors.Is(err, context.Canceled):
		return err
	}
	status, ok := err.(apierrors.APIStatus)
	if !ok {
		return err
	}
	s := status.Status()
	switch s.Code {
	case 404:
		return fmt.Errorf("%w: %s", domain.ErrNotFound, s.Message)
	case 409:
		return fmt.Errorf("%w: %s", domain.ErrConflict, s.Message)
	case 410:
		return fmt.Errorf("%w: %s", domain.ErrStaleCursor, s.Message)
	case 403:
		return fmt.Errorf("%w: %s", domain.ErrForbidden, s.Message)
	case 400:
		return fmt.Errorf("%w: %s", domain.ErrInvalid, s.Message)
	case 401:
		return fmt.Errorf("%w: %s", domain.ErrForbidden, s.Message)
	case 429:
		return fmt.Errorf("%w: %s", domain.ErrRateLimited, s.Message)
	case 500, 502, 503, 504:
		return fmt.Errorf("%w: %s", domain.ErrUnavailable, s.Message)
	}
	return err
}

func gvrFor(resourceType domain.ResourceType) (schema.GroupVersionResource, bool, bool) {
	switch resourceType {
	case domain.ResourceNode:
		return schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, false, true
	case domain.ResourceNamespace:
		return schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, false, true
	case domain.ResourcePod:
		return schema.GroupVersionResource{Version: "v1", Resource: "pods"}, true, true
	case domain.ResourceDeployment:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true, true
	case domain.ResourceStatefulSet:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true, true
	case domain.ResourceDaemonSet:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true, true
	case domain.ResourceJob:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, true, true
	case domain.ResourceCronJob:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true, true
	case domain.ResourceService:
		return schema.GroupVersionResource{Version: "v1", Resource: "services"}, true, true
	case domain.ResourceIngress:
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true, true
	case domain.ResourceConfigMap:
		return schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true, true
	case domain.ResourceSecret:
		return schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true, true
	case domain.ResourceStorageClass:
		return schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, false, true
	case domain.ResourcePersistentVolume:
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, false, true
	case domain.ResourcePersistentVolumeClaim:
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, true, true
	default:
		return schema.GroupVersionResource{}, false, false
	}
}
