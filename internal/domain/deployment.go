package domain

// WorkloadCreateSpec is the validated, transport-neutral input for creating a
// supported workload. Containers is preferred; the legacy scalar container
// fields remain accepted so older API clients keep working. The Kubernetes
// adapter owns conversion to API objects so domain and application code remain
// independent of client-go.
type WorkloadCreateSpec struct {
	Kind               string                  `json:"kind"`
	Name               string                  `json:"name"`
	Namespace          string                  `json:"namespace"`
	Replicas           int32                   `json:"replicas,omitempty"`
	ServiceName        string                  `json:"serviceName,omitempty"`
	Schedule           string                  `json:"schedule,omitempty"`
	ConcurrencyPolicy  string                  `json:"concurrencyPolicy,omitempty"`
	Suspend            bool                    `json:"suspend,omitempty"`
	ContainerName      string                  `json:"containerName"`
	Image              string                  `json:"image"`
	RegistryID         string                  `json:"registryId,omitempty"`
	ImagePullSecrets   []string                `json:"-"`
	ContainerPort      int32                   `json:"containerPort"`
	CPURequestMilli    int64                   `json:"cpuRequestMilli"`
	CPULimitMilli      int64                   `json:"cpuLimitMilli"`
	MemoryRequestBytes int64                   `json:"memoryRequestBytes"`
	MemoryLimitBytes   int64                   `json:"memoryLimitBytes"`
	Env                []DeploymentEnvVar      `json:"env,omitempty"`
	Labels             map[string]string       `json:"labels,omitempty"`
	Containers         []WorkloadContainerSpec `json:"containers,omitempty"`
	InitContainers     []WorkloadContainerSpec `json:"initContainers,omitempty"`

	Command []string `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	NodeSelector              map[string]string                  `json:"nodeSelector,omitempty"`
	Tolerations               []WorkloadToleration               `json:"tolerations,omitempty"`
	NodeAffinity              []WorkloadNodeSelectorRequirement  `json:"nodeAffinity,omitempty"`
	TopologySpreadConstraints []WorkloadTopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	PriorityClassName         string                             `json:"priorityClassName,omitempty"`
	ServiceAccountName        string                             `json:"serviceAccountName,omitempty"`
	PodSecurityContext        *WorkloadPodSecurityContext        `json:"podSecurityContext,omitempty"`

	JobCompletions             *int32 `json:"jobCompletions,omitempty"`
	JobParallelism             *int32 `json:"jobParallelism,omitempty"`
	JobBackoffLimit            *int32 `json:"jobBackoffLimit,omitempty"`
	JobActiveDeadlineSeconds   *int64 `json:"jobActiveDeadlineSeconds,omitempty"`
	JobTTLSecondsAfterFinished *int32 `json:"jobTtlSecondsAfterFinished,omitempty"`
}

// DeploymentCreateSpec is retained as an alias for callers that used the
// Deployment-only API before workload creation was generalized.
type DeploymentCreateSpec = WorkloadCreateSpec

type DeploymentEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// WorkloadContainerSpec describes one application or init container accepted
// by the structured create workflow.
type WorkloadContainerSpec struct {
	Name               string                            `json:"name"`
	Image              string                            `json:"image"`
	RegistryID         string                            `json:"registryId,omitempty"`
	ImagePullSecrets   []string                          `json:"-"`
	ContainerPort      int32                             `json:"containerPort,omitempty"`
	CPURequestMilli    int64                             `json:"cpuRequestMilli,omitempty"`
	CPULimitMilli      int64                             `json:"cpuLimitMilli,omitempty"`
	MemoryRequestBytes int64                             `json:"memoryRequestBytes,omitempty"`
	MemoryLimitBytes   int64                             `json:"memoryLimitBytes,omitempty"`
	Env                []DeploymentEnvVar                `json:"env,omitempty"`
	Command            []string                          `json:"command,omitempty"`
	Args               []string                          `json:"args,omitempty"`
	SecurityContext    *WorkloadContainerSecurityContext `json:"securityContext,omitempty"`
}

type WorkloadToleration struct {
	Key               string `json:"key,omitempty"`
	Operator          string `json:"operator,omitempty"`
	Value             string `json:"value,omitempty"`
	Effect            string `json:"effect,omitempty"`
	TolerationSeconds *int64 `json:"tolerationSeconds,omitempty"`
}

type WorkloadNodeSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

type WorkloadTopologySpreadConstraint struct {
	MaxSkew           int32  `json:"maxSkew"`
	TopologyKey       string `json:"topologyKey"`
	WhenUnsatisfiable string `json:"whenUnsatisfiable"`
}

type WorkloadPodSecurityContext struct {
	RunAsUser      *int64 `json:"runAsUser,omitempty"`
	RunAsGroup     *int64 `json:"runAsGroup,omitempty"`
	FSGroup        *int64 `json:"fsGroup,omitempty"`
	RunAsNonRoot   *bool  `json:"runAsNonRoot,omitempty"`
	SeccompProfile string `json:"seccompProfile,omitempty"`
}

type WorkloadContainerSecurityContext struct {
	RunAsUser                *int64   `json:"runAsUser,omitempty"`
	RunAsGroup               *int64   `json:"runAsGroup,omitempty"`
	RunAsNonRoot             *bool    `json:"runAsNonRoot,omitempty"`
	ReadOnlyRootFilesystem   *bool    `json:"readOnlyRootFilesystem,omitempty"`
	AllowPrivilegeEscalation *bool    `json:"allowPrivilegeEscalation,omitempty"`
	CapabilitiesAdd          []string `json:"capabilitiesAdd,omitempty"`
	CapabilitiesDrop         []string `json:"capabilitiesDrop,omitempty"`
}

type WorkloadRolloutUpdate struct {
	Paused          *bool  `json:"paused,omitempty"`
	MaxSurge        string `json:"maxSurge,omitempty"`
	MaxUnavailable  string `json:"maxUnavailable,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

type CronJobRunInput struct {
	ContainerName string   `json:"containerName,omitempty"`
	Command       []string `json:"command,omitempty"`
	Args          []string `json:"args,omitempty"`
}
