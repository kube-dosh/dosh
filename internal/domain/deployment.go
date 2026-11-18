package domain

// WorkloadCreateSpec is the validated, transport-neutral input for creating a
// supported workload with one application container. The Kubernetes adapter
// owns conversion to API objects so domain and application code remain
// independent of client-go.
type WorkloadCreateSpec struct {
	Kind               string             `json:"kind"`
	Name               string             `json:"name"`
	Namespace          string             `json:"namespace"`
	Replicas           int32              `json:"replicas,omitempty"`
	ServiceName        string             `json:"serviceName,omitempty"`
	Schedule           string             `json:"schedule,omitempty"`
	ConcurrencyPolicy  string             `json:"concurrencyPolicy,omitempty"`
	Suspend            bool               `json:"suspend,omitempty"`
	ContainerName      string             `json:"containerName"`
	Image              string             `json:"image"`
	RegistryID         string             `json:"registryId,omitempty"`
	ImagePullSecrets   []string           `json:"-"`
	ContainerPort      int32              `json:"containerPort"`
	CPURequestMilli    int64              `json:"cpuRequestMilli"`
	CPULimitMilli      int64              `json:"cpuLimitMilli"`
	MemoryRequestBytes int64              `json:"memoryRequestBytes"`
	MemoryLimitBytes   int64              `json:"memoryLimitBytes"`
	Env                []DeploymentEnvVar `json:"env,omitempty"`
	Labels             map[string]string  `json:"labels,omitempty"`
}

// DeploymentCreateSpec is retained as an alias for callers that used the
// Deployment-only API before workload creation was generalized.
type DeploymentCreateSpec = WorkloadCreateSpec

type DeploymentEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
