package domain

import "time"

// VerticalAutoscalerSummary is the operator-facing view of a VPA. Resource
// recommendations are kept as Kubernetes quantity strings so the UI can render
// them without duplicating apimachinery quantity parsing.
type VerticalAutoscalerSummary struct {
	Name              string               `json:"name"`
	Namespace         string               `json:"namespace"`
	ResourceVersion   string               `json:"resourceVersion,omitempty"`
	TargetAPIVersion  string               `json:"targetApiVersion"`
	TargetKind        string               `json:"targetKind"`
	TargetName        string               `json:"targetName"`
	UpdatePolicy      string               `json:"updatePolicy"`
	MinReplicas       *int32               `json:"minReplicas,omitempty"`
	ContainerPolicies []VPAContainerPolicy `json:"containerPolicies"`
	Recommendation    []VPARecommendation  `json:"recommendation"`
	RecommendationAge *time.Time           `json:"recommendationAge,omitempty"`
	Conditions        []Condition          `json:"conditions"`
	Status            string               `json:"status"`
	Message           string               `json:"message,omitempty"`
	CreatedAt         time.Time            `json:"createdAt"`
}

type VPAContainerPolicy struct {
	ContainerName       string            `json:"containerName"`
	Mode                string            `json:"mode,omitempty"`
	MinAllowed          map[string]string `json:"minAllowed,omitempty"`
	MaxAllowed          map[string]string `json:"maxAllowed,omitempty"`
	ControlledValues    string            `json:"controlledValues,omitempty"`
	ControlledResources []string          `json:"controlledResources,omitempty"`
}

type VPARecommendation struct {
	ContainerName  string            `json:"containerName"`
	LowerBound     map[string]string `json:"lowerBound,omitempty"`
	Target         map[string]string `json:"target"`
	UpperBound     map[string]string `json:"upperBound,omitempty"`
	UncappedTarget map[string]string `json:"uncappedTarget,omitempty"`
}

type VerticalAutoscalerSpec struct {
	Name              string               `json:"name"`
	Namespace         string               `json:"namespace"`
	TargetKind        string               `json:"targetKind"`
	TargetName        string               `json:"targetName"`
	UpdatePolicy      string               `json:"updatePolicy"`
	MinReplicas       *int32               `json:"minReplicas,omitempty"`
	ContainerPolicies []VPAContainerPolicy `json:"containerPolicies,omitempty"`
	ResourceVersion   string               `json:"resourceVersion,omitempty"`
	TargetAPIVersion  string               `json:"targetApiVersion,omitempty"`
}
