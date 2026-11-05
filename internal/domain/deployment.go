package domain

// DeploymentCreateSpec is the validated, transport-neutral input for creating
// one application Deployment. The Kubernetes adapter owns conversion to API
// objects so domain and application code remain independent of client-go.
type DeploymentCreateSpec struct {
	Name               string             `json:"name"`
	Namespace          string             `json:"namespace"`
	Replicas           int32              `json:"replicas"`
	ContainerName      string             `json:"containerName"`
	Image              string             `json:"image"`
	ContainerPort      int32              `json:"containerPort"`
	CPURequestMilli    int64              `json:"cpuRequestMilli"`
	CPULimitMilli      int64              `json:"cpuLimitMilli"`
	MemoryRequestBytes int64              `json:"memoryRequestBytes"`
	MemoryLimitBytes   int64              `json:"memoryLimitBytes"`
	Env                []DeploymentEnvVar `json:"env,omitempty"`
	Labels             map[string]string  `json:"labels,omitempty"`
}

type DeploymentEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
