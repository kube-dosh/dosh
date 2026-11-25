package domain

import "time"

// WorkloadPodTemplate is the editable PodTemplateSpec of one workload. The
// template is intentionally represented as an untyped JSON document so form
// edits and YAML edits share the same source of truth and do not drop fields
// merely because the current UI does not render them.
type WorkloadPodTemplate struct {
	Kind            string            `json:"kind"`
	Namespace       string            `json:"namespace"`
	Name            string            `json:"name"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	Template        map[string]any    `json:"template"`
	YAML            string            `json:"yaml"`
	Selector        map[string]string `json:"selector,omitempty"`
	ProtectedLabels []string          `json:"protectedLabels,omitempty"`
	Editable        bool              `json:"editable"`
	Restrictions    []string          `json:"restrictions,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
}

// WorkloadPodTemplateUpdate replaces one workload's PodTemplateSpec. The
// Kubernetes adapter reads the current workload, verifies resourceVersion and
// performs exactly one atomic update after an API Server dry run succeeds.
type WorkloadPodTemplateUpdate struct {
	ResourceVersion  string         `json:"resourceVersion,omitempty"`
	Template         map[string]any `json:"template"`
	ChangeCause      string         `json:"changeCause,omitempty"`
	Source           string         `json:"source,omitempty"`
	DryRun           bool           `json:"dryRun,omitempty"`
	AcknowledgeRisks bool           `json:"acknowledgeRisks,omitempty"`
}

type WorkloadTemplateDiff struct {
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

type WorkloadTemplateRollout struct {
	Effect  string `json:"effect"`
	Message string `json:"message"`
}

type WorkloadPodTemplateResult struct {
	WorkloadPodTemplate
	Changed     bool                    `json:"changed"`
	Diff        []WorkloadTemplateDiff  `json:"diff"`
	Rollout     WorkloadTemplateRollout `json:"rollout"`
	NewWarnings []string                `json:"newWarnings,omitempty"`
}

// WorkloadContainerInventory combines one workload's declarative container
// definitions with the runtime state of matching Pod containers. Template
// configuration and actual Pod images are kept separate so rollout drift is
// visible instead of being hidden behind the desired image.
type WorkloadContainerInventory struct {
	Kind             string                   `json:"kind"`
	Namespace        string                   `json:"namespace"`
	Name             string                   `json:"name"`
	ResourceVersion  string                   `json:"resourceVersion,omitempty"`
	Selector         map[string]string        `json:"selector,omitempty"`
	Containers       []WorkloadContainerState `json:"containers"`
	TemplateEditable bool                     `json:"templateEditable"`
	MetricsAvailable bool                     `json:"metricsAvailable"`
	Warnings         []string                 `json:"warnings,omitempty"`
	RefreshedAt      time.Time                `json:"refreshedAt"`
}

type WorkloadContainerState struct {
	WorkloadContainer
	Type      string                      `json:"type"`
	Init      bool                        `json:"init"`
	Sidecar   bool                        `json:"sidecar"`
	Runtime   WorkloadContainerRuntime    `json:"runtime"`
	Instances []WorkloadContainerInstance `json:"instances"`
}

type WorkloadContainerRuntime struct {
	Pods             int32 `json:"pods"`
	Running          int32 `json:"running"`
	Ready            int32 `json:"ready"`
	Terminated       int32 `json:"terminated"`
	Restarts         int32 `json:"restarts"`
	MetricsAvailable bool  `json:"metricsAvailable"`
	CPUUsageMilli    int64 `json:"cpuUsageMilli,omitempty"`
	MemoryUsageBytes int64 `json:"memoryUsageBytes,omitempty"`
}

type WorkloadContainerInstance struct {
	Pod              string     `json:"pod"`
	Namespace        string     `json:"namespace"`
	Node             string     `json:"node,omitempty"`
	Job              string     `json:"job,omitempty"`
	Image            string     `json:"image"`
	State            string     `json:"state"`
	Reason           string     `json:"reason,omitempty"`
	Message          string     `json:"message,omitempty"`
	Ready            bool       `json:"ready"`
	RestartCount     int32      `json:"restartCount"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	MetricsAvailable bool       `json:"metricsAvailable"`
	CPUUsageMilli    int64      `json:"cpuUsageMilli,omitempty"`
	MemoryUsageBytes int64      `json:"memoryUsageBytes,omitempty"`
}
