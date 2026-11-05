package domain

import (
	"context"
	"time"
)

type actorContextKey struct{}

// WithActor attaches the authenticated identity to a request-scoped context.
// Infrastructure adapters use it for Kubernetes impersonation and audit.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// WithoutActor removes the authenticated identity from ctx. Infrastructure
// adapters use it for control-plane calls that must execute as the workload
// ServiceAccount rather than the impersonated user.
func WithoutActor(ctx context.Context) context.Context {
	return context.WithValue(ctx, actorContextKey{}, nil)
}

// ActorFromContext returns the authenticated identity, if one was attached by
// the HTTP authentication adapter.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok
}

type Actor struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups,omitempty"`
	Admin    bool     `json:"admin"`
}

type Capabilities struct {
	Read              bool `json:"read"`
	Scale             bool `json:"scale"`
	Restart           bool `json:"restart"`
	UpdateImage       bool `json:"updateImage"`
	SuspendCronJob    bool `json:"suspendCronJob"`
	Delete            bool `json:"delete"`
	Create            bool `json:"create"`
	RevealSecrets     bool `json:"revealSecrets"`
	EditYAML          bool `json:"editYAML"`
	NodeCordon        bool `json:"nodeCordon"`
	ReloadCertificate bool `json:"reloadCertificate"`
}

// ServerCertificate is the non-sensitive runtime view of the TLS certificate
// used by the Dosh HTTP server. It intentionally excludes private key material.
type ServerCertificate struct {
	Mode                  string    `json:"mode"`
	Enabled               bool      `json:"enabled"`
	Configured            bool      `json:"configured"`
	Status                string    `json:"status"`
	Subject               string    `json:"subject,omitempty"`
	Issuer                string    `json:"issuer,omitempty"`
	SerialNumber          string    `json:"serialNumber,omitempty"`
	DNSNames              []string  `json:"dnsNames"`
	IPAddresses           []string  `json:"ipAddresses"`
	NotBefore             time.Time `json:"notBefore,omitempty"`
	NotAfter              time.Time `json:"notAfter,omitempty"`
	DaysRemaining         int       `json:"daysRemaining"`
	FingerprintSHA256     string    `json:"fingerprintSha256,omitempty"`
	KeyAlgorithm          string    `json:"keyAlgorithm,omitempty"`
	AutoReload            bool      `json:"autoReload"`
	ReloadIntervalSeconds int       `json:"reloadIntervalSeconds"`
	LastReloadedAt        time.Time `json:"lastReloadedAt,omitempty"`
	LastReloadError       string    `json:"lastReloadError,omitempty"`
}

type AuditEvent struct {
	Action    string
	Resource  string
	Namespace string
	Name      string
	Result    string
	Err       error
}

// AuditRecord is the transport-neutral representation of one audit event
// retained for interactive inspection. Durable retention remains the
// responsibility of stdout collection or the configured webhook sink.
type AuditRecord struct {
	ID        uint64    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user"`
	Groups    []string  `json:"groups,omitempty"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name,omitempty"`
	Result    string    `json:"result"`
	RequestID string    `json:"requestId,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type AuditQuery struct {
	Namespace string
	Search    string
	Action    string
	Result    string
}

type Auditor interface {
	Record(ctx context.Context, actor Actor, event AuditEvent)
}

// Access describes one Kubernetes authorization decision. The consumer-owned
// interface keeps modules independent from concrete RBAC or local policy
// adapters.
type Access struct {
	Verb        string
	APIGroup    string
	Resource    string
	Subresource string
	Namespace   string
	Name        string
}

type Authorizer interface {
	Require(ctx context.Context, actor Actor, access Access) error
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime,omitempty"`
}

type Taint struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Effect    string `json:"effect"`
	TimeAdded string `json:"timeAdded,omitempty"`
}

type ResourceUsage struct {
	CPURequestsMilli    int64 `json:"cpuRequestsMilli"`
	CPULimitsMilli      int64 `json:"cpuLimitsMilli"`
	MemoryRequestsBytes int64 `json:"memoryRequestsBytes"`
	MemoryLimitsBytes   int64 `json:"memoryLimitsBytes"`
	CPUUsageMilli       int64 `json:"cpuUsageMilli"`
	MemoryUsageBytes    int64 `json:"memoryUsageBytes"`
}

type ClusterInfo struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Platform         string `json:"platform,omitempty"`
	CNI              string `json:"cni,omitempty"`
	Status           string `json:"status"`
	MetricsAvailable bool   `json:"metricsAvailable"`
}

type OverviewCounts struct {
	Nodes         int `json:"nodes"`
	ReadyNodes    int `json:"readyNodes"`
	Pods          int `json:"pods"`
	RunningPods   int `json:"runningPods"`
	Workloads     int `json:"workloads"`
	Namespaces    int `json:"namespaces"`
	UnhealthyPods int `json:"unhealthyPods"`
	WarningEvents int `json:"warningEvents"`
}

type ClusterCapacity struct {
	CPUAllocatableMilli    int64 `json:"cpuAllocatableMilli"`
	MemoryAllocatableBytes int64 `json:"memoryAllocatableBytes"`
	PodCapacity            int64 `json:"podCapacity"`
	CPURequestsMilli       int64 `json:"cpuRequestsMilli"`
	MemoryRequestsBytes    int64 `json:"memoryRequestsBytes"`
	CPULimitsMilli         int64 `json:"cpuLimitsMilli"`
	MemoryLimitsBytes      int64 `json:"memoryLimitsBytes"`
	CPUUsageMilli          int64 `json:"cpuUsageMilli"`
	MemoryUsageBytes       int64 `json:"memoryUsageBytes"`
}

type Overview struct {
	Cluster  ClusterInfo     `json:"cluster"`
	Counts   OverviewCounts  `json:"counts"`
	Capacity ClusterCapacity `json:"capacity"`
	Events   []Event         `json:"recentEvents"`
	Warnings []string        `json:"warnings,omitempty"`
}

type NodeSummary struct {
	Name                string    `json:"name"`
	Status              string    `json:"status"`
	Roles               []string  `json:"roles"`
	InternalIP          string    `json:"internalIP"`
	Version             string    `json:"version"`
	OSImage             string    `json:"osImage"`
	KernelVersion       string    `json:"kernelVersion"`
	ContainerRuntime    string    `json:"containerRuntime"`
	CPUCapacityMilli    int64     `json:"cpuCapacityMilli"`
	MemoryCapacityBytes int64     `json:"memoryCapacityBytes"`
	PodCapacity         int64     `json:"podCapacity"`
	PodCount            int       `json:"podCount"`
	CPURequestsMilli    int64     `json:"cpuRequestsMilli"`
	MemoryRequestsBytes int64     `json:"memoryRequestsBytes"`
	CPUUsageMilli       int64     `json:"cpuUsageMilli,omitempty"`
	MemoryUsageBytes    int64     `json:"memoryUsageBytes,omitempty"`
	HasMetrics          bool      `json:"hasMetrics"`
	Unschedulable       bool      `json:"unschedulable"`
	CreatedAt           time.Time `json:"createdAt"`
}

type NodeDetail struct {
	NodeSummary
	Conditions  []Condition       `json:"conditions"`
	Taints      []Taint           `json:"taints"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Pods        []PodSummary      `json:"pods"`
}

type Namespace struct {
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
}

type NamespaceDetail struct {
	Namespace
	ResourceQuotas []ResourceQuota `json:"resourceQuotas"`
	LimitRanges    []LimitRange    `json:"limitRanges"`
}

type ResourceQuota struct {
	Name      string            `json:"name"`
	Hard      map[string]string `json:"hard"`
	Used      map[string]string `json:"used"`
	CreatedAt time.Time         `json:"createdAt"`
}

type LimitRange struct {
	Name      string           `json:"name"`
	Limits    []LimitRangeItem `json:"limits"`
	CreatedAt time.Time        `json:"createdAt"`
}

type LimitRangeItem struct {
	Type           string            `json:"type"`
	Min            map[string]string `json:"min"`
	Max            map[string]string `json:"max"`
	Default        map[string]string `json:"default"`
	DefaultRequest map[string]string `json:"defaultRequest"`
}

type WorkloadSummary struct {
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	Status          string            `json:"status"`
	Ready           string            `json:"ready"`
	Desired         int32             `json:"desired"`
	Restarts        int32             `json:"restarts"`
	Images          []string          `json:"images"`
	CreatedAt       time.Time         `json:"createdAt"`
	Labels          map[string]string `json:"labels,omitempty"`
}

type WorkloadDetail struct {
	WorkloadSummary
	Conditions []Condition         `json:"conditions"`
	Selector   map[string]string   `json:"selector"`
	Pods       []PodSummary        `json:"pods"`
	Containers []WorkloadContainer `json:"containers"`
}

// WorkloadContainer identifies one regular container in a workload pod
// template. It intentionally excludes runtime-only pod data.
type WorkloadContainer struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// WorkloadImageUpdate is the transport-neutral input for changing one
// container image in an application workload. ResourceVersion enables
// optimistic concurrency when the caller has loaded a previous representation.
type WorkloadImageUpdate struct {
	ContainerName   string `json:"containerName"`
	Image           string `json:"image"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// CronJobSuspendUpdate controls whether a CronJob pauses new Job creation.
// Existing Jobs are not deleted when a CronJob is suspended.
type CronJobSuspendUpdate struct {
	Suspend         bool   `json:"suspend"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

type Container struct {
	Name               string `json:"name"`
	Image              string `json:"image"`
	Ready              bool   `json:"ready"`
	RestartCount       int32  `json:"restartCount"`
	State              string `json:"state"`
	Reason             string `json:"reason,omitempty"`
	CPURequestMilli    int64  `json:"cpuRequestMilli"`
	CPULimitMilli      int64  `json:"cpuLimitMilli"`
	MemoryRequestBytes int64  `json:"memoryRequestBytes"`
	MemoryLimitBytes   int64  `json:"memoryLimitBytes"`
}

type PodSummary struct {
	Name             string            `json:"name"`
	Namespace        string            `json:"namespace"`
	Status           string            `json:"status"`
	Ready            string            `json:"ready"`
	Restarts         int32             `json:"restarts"`
	IP               string            `json:"ip"`
	Node             string            `json:"node"`
	OwnerKind        string            `json:"ownerKind,omitempty"`
	OwnerName        string            `json:"ownerName,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	Labels           map[string]string `json:"labels,omitempty"`
	CPUUsageMilli    int64             `json:"cpuUsageMilli,omitempty"`
	MemoryUsageBytes int64             `json:"memoryUsageBytes,omitempty"`
	HasMetrics       bool              `json:"hasMetrics"`
}

type PodDetail struct {
	PodSummary
	Containers     []Container `json:"containers"`
	Conditions     []Condition `json:"conditions"`
	QOSClass       string      `json:"qosClass"`
	ServiceAccount string      `json:"serviceAccount"`
}

type Event struct {
	UID        string    `json:"uid"`
	Namespace  string    `json:"namespace"`
	Type       string    `json:"type"`
	Reason     string    `json:"reason"`
	Message    string    `json:"message"`
	ObjectKind string    `json:"objectKind"`
	ObjectName string    `json:"objectName"`
	Count      int32     `json:"count"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
	Source     string    `json:"source,omitempty"`
}

type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort"`
	NodePort   int32  `json:"nodePort,omitempty"`
}

type ServiceSummary struct {
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Type          string            `json:"type"`
	ClusterIP     string            `json:"clusterIP"`
	ExternalIP    string            `json:"externalIP"`
	Ports         []ServicePort     `json:"ports"`
	Selector      map[string]string `json:"selector,omitempty"`
	EndpointCount int               `json:"endpointCount"`
	CreatedAt     time.Time         `json:"createdAt"`
}

type ServiceEndpoint struct {
	Addresses       []string `json:"addresses"`
	Ready           bool     `json:"ready"`
	TargetKind      string   `json:"targetKind,omitempty"`
	TargetName      string   `json:"targetName,omitempty"`
	TargetNamespace string   `json:"targetNamespace,omitempty"`
	NodeName        string   `json:"nodeName,omitempty"`
}

type ServiceDetail struct {
	ServiceSummary
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	ExternalName      string            `json:"externalName,omitempty"`
	SessionAffinity   string            `json:"sessionAffinity"`
	IPFamilies        []string          `json:"ipFamilies"`
	IPFamilyPolicy    string            `json:"ipFamilyPolicy,omitempty"`
	LoadBalancerClass string            `json:"loadBalancerClass,omitempty"`
	Endpoints         []ServiceEndpoint `json:"endpoints"`
}

type IngressPath struct {
	Host         string `json:"host"`
	Path         string `json:"path"`
	PathType     string `json:"pathType"`
	Service      string `json:"service"`
	Port         string `json:"port"`
	TargetStatus string `json:"targetStatus"`
}

const (
	IngressTargetReady            = "Ready"
	IngressTargetNoReadyEndpoints = "NoReadyEndpoints"
	IngressTargetServiceNotFound  = "ServiceNotFound"
	IngressTargetPortNotFound     = "PortNotFound"
	IngressTargetUnknown          = "Unknown"
)

type IngressSummary struct {
	Name      string        `json:"name"`
	Namespace string        `json:"namespace"`
	Class     string        `json:"class"`
	Paths     []IngressPath `json:"paths"`
	TLSHosts  []string      `json:"tlsHosts,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
}

type IngressTLS struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secretName,omitempty"`
}

type IngressDetail struct {
	IngressSummary
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	DefaultBackend *IngressPath      `json:"defaultBackend,omitempty"`
	TLS            []IngressTLS      `json:"tls,omitempty"`
}

type IngressClassSummary struct {
	Name       string    `json:"name"`
	Controller string    `json:"controller"`
	Default    bool      `json:"default"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ConfigMapSummary struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Keys      []string  `json:"keys"`
	DataBytes int       `json:"dataBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

type ConfigMapDetail struct {
	ConfigMapSummary
	Data map[string]string `json:"data"`
}

type SecretKey struct {
	Key  string `json:"key"`
	Size int    `json:"size"`
}

type SecretSummary struct {
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
	Type      string      `json:"type"`
	Keys      []SecretKey `json:"keys"`
	CreatedAt time.Time   `json:"createdAt"`
}

type SecretReveal struct {
	SecretSummary
	Data map[string]string `json:"data"`
}

type StorageClass struct {
	Name          string    `json:"name"`
	Provisioner   string    `json:"provisioner"`
	ReclaimPolicy string    `json:"reclaimPolicy"`
	VolumeBinding string    `json:"volumeBinding"`
	Default       bool      `json:"default"`
	CreatedAt     time.Time `json:"createdAt"`
}

type StorageClassDetail struct {
	StorageClass
	Labels               map[string]string `json:"labels,omitempty"`
	Annotations          map[string]string `json:"annotations,omitempty"`
	Parameters           map[string]string `json:"parameters,omitempty"`
	MountOptions         []string          `json:"mountOptions,omitempty"`
	AllowVolumeExpansion bool              `json:"allowVolumeExpansion"`
}

type PersistentVolume struct {
	Name          string    `json:"name"`
	CapacityBytes int64     `json:"capacityBytes"`
	AccessModes   []string  `json:"accessModes"`
	ReclaimPolicy string    `json:"reclaimPolicy"`
	Status        string    `json:"status"`
	Claim         string    `json:"claim,omitempty"`
	StorageClass  string    `json:"storageClass"`
	CreatedAt     time.Time `json:"createdAt"`
}

type PersistentVolumeDetail struct {
	PersistentVolume
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	VolumeMode     string            `json:"volumeMode"`
	MountOptions   []string          `json:"mountOptions,omitempty"`
	ClaimNamespace string            `json:"claimNamespace,omitempty"`
	ClaimName      string            `json:"claimName,omitempty"`
	CSIDriver      string            `json:"csiDriver,omitempty"`
}

type PersistentVolumeClaim struct {
	Name          string    `json:"name"`
	Namespace     string    `json:"namespace"`
	Status        string    `json:"status"`
	VolumeName    string    `json:"volumeName,omitempty"`
	CapacityBytes int64     `json:"capacityBytes"`
	AccessModes   []string  `json:"accessModes"`
	StorageClass  string    `json:"storageClass"`
	CreatedAt     time.Time `json:"createdAt"`
}

type PersistentVolumeClaimDetail struct {
	PersistentVolumeClaim
	Labels                 map[string]string `json:"labels,omitempty"`
	Annotations            map[string]string `json:"annotations,omitempty"`
	VolumeMode             string            `json:"volumeMode"`
	RequestedCapacityBytes int64             `json:"requestedCapacityBytes"`
	Conditions             []Condition       `json:"conditions"`
}

type YAMLResource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	YAML      string `json:"yaml"`
}

type ApplyRequest struct {
	YAML            string `json:"yaml"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
	DryRun          bool   `json:"dryRun"`
}

type ListQuery struct {
	Namespace  string
	Search     string
	Kind       string
	Status     string
	ObjectKind string
	ObjectName string
	Limit      int
}

type DeletionPropagation string

const (
	DeletePropagationBackground DeletionPropagation = "Background"
	DeletePropagationForeground DeletionPropagation = "Foreground"
	DeletePropagationOrphan     DeletionPropagation = "Orphan"
)

type ResourceType string

const (
	ResourceNode                  ResourceType = "node"
	ResourceNamespace             ResourceType = "namespace"
	ResourcePod                   ResourceType = "pod"
	ResourceDeployment            ResourceType = "deployment"
	ResourceStatefulSet           ResourceType = "statefulset"
	ResourceDaemonSet             ResourceType = "daemonset"
	ResourceJob                   ResourceType = "job"
	ResourceCronJob               ResourceType = "cronjob"
	ResourceService               ResourceType = "service"
	ResourceIngress               ResourceType = "ingress"
	ResourceConfigMap             ResourceType = "configmap"
	ResourceSecret                ResourceType = "secret"
	ResourceStorageClass          ResourceType = "storageclass"
	ResourcePersistentVolume      ResourceType = "persistentvolume"
	ResourcePersistentVolumeClaim ResourceType = "persistentvolumeclaim"
)

func ParseResourceType(v string) (ResourceType, bool) {
	switch ResourceType(v) {
	case ResourceNode, ResourceNamespace, ResourcePod, ResourceDeployment,
		ResourceStatefulSet, ResourceDaemonSet, ResourceJob, ResourceCronJob,
		ResourceService, ResourceIngress, ResourceConfigMap, ResourceSecret,
		ResourceStorageClass, ResourcePersistentVolume, ResourcePersistentVolumeClaim:
		return ResourceType(v), true
	default:
		return "", false
	}
}
