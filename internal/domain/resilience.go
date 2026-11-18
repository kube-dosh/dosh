package domain

import "time"

// AutoscalerSummary explains the desired and observed state of one HPA.
// Resource metrics are rendered as transport-friendly strings so callers do
// not need Kubernetes quantity types.
type AutoscalerSummary struct {
	Name            string             `json:"name"`
	Namespace       string             `json:"namespace"`
	ResourceVersion string             `json:"resourceVersion,omitempty"`
	TargetKind      string             `json:"targetKind"`
	TargetName      string             `json:"targetName"`
	MinReplicas     int32              `json:"minReplicas"`
	MaxReplicas     int32              `json:"maxReplicas"`
	CurrentReplicas int32              `json:"currentReplicas"`
	DesiredReplicas int32              `json:"desiredReplicas"`
	Status          string             `json:"status"`
	Metrics         []AutoscalerMetric `json:"metrics"`
	Conditions      []Condition        `json:"conditions"`
	LastScaleTime   *time.Time         `json:"lastScaleTime,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
}

type AutoscalerMetric struct {
	Type               string `json:"type"`
	Resource           string `json:"resource,omitempty"`
	Current            string `json:"current,omitempty"`
	Target             string `json:"target,omitempty"`
	CurrentUtilization *int32 `json:"currentUtilization,omitempty"`
	TargetUtilization  *int32 `json:"targetUtilization,omitempty"`
}

// AutoscalerSpec is the validated input for creating or updating a resource-
// metric HPA managed by the dedicated resilience workflow.
type AutoscalerSpec struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	TargetKind        string `json:"targetKind"`
	TargetName        string `json:"targetName"`
	MinReplicas       int32  `json:"minReplicas"`
	MaxReplicas       int32  `json:"maxReplicas"`
	CPUUtilization    *int32 `json:"cpuUtilization,omitempty"`
	MemoryUtilization *int32 `json:"memoryUtilization,omitempty"`
	ResourceVersion   string `json:"resourceVersion,omitempty"`
}

// DisruptionBudgetSummary explains a PDB's protection window and why voluntary
// evictions may currently be blocked.
type DisruptionBudgetSummary struct {
	Name               string            `json:"name"`
	Namespace          string            `json:"namespace"`
	ResourceVersion    string            `json:"resourceVersion,omitempty"`
	MinAvailable       string            `json:"minAvailable,omitempty"`
	MaxUnavailable     string            `json:"maxUnavailable,omitempty"`
	Selector           map[string]string `json:"selector"`
	CurrentHealthy     int32             `json:"currentHealthy"`
	DesiredHealthy     int32             `json:"desiredHealthy"`
	DisruptionsAllowed int32             `json:"disruptionsAllowed"`
	ExpectedPods       int32             `json:"expectedPods"`
	ObservedGeneration int64             `json:"observedGeneration"`
	Status             string            `json:"status"`
	Conditions         []Condition       `json:"conditions"`
	CreatedAt          time.Time         `json:"createdAt"`
}

// DisruptionBudgetSpec is the validated input for creating or updating a PDB.
// Exactly one of MinAvailable and MaxUnavailable is required.
type DisruptionBudgetSpec struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	MinAvailable    string            `json:"minAvailable,omitempty"`
	MaxUnavailable  string            `json:"maxUnavailable,omitempty"`
	Selector        map[string]string `json:"selector"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
}
