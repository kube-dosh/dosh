package domain

import "time"

// ResourceDescriptor is the stable, transport-neutral description of one
// discoverable Kubernetes resource. Group is empty for the core API group.
type ResourceDescriptor struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Singular   string   `json:"singularName,omitempty"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	ShortNames []string `json:"shortNames,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// ResourceReference identifies one resource type returned by API Discovery.
type ResourceReference struct {
	Group    string
	Version  string
	Resource string
}

// GenericResourceSummary is the bounded metadata exposed by the generic
// resource list. Full objects remain available through the detail endpoint.
type GenericResourceSummary struct {
	APIVersion      string            `json:"apiVersion"`
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	UID             string            `json:"uid,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	CreatedAt       time.Time         `json:"createdAt,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
}

// GenericResourceDetail contains the API object and a YAML representation for
// one discovered resource. Sensitive data may be redacted by the application
// layer before it reaches this type.
type GenericResourceDetail struct {
	APIVersion      string            `json:"apiVersion"`
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	UID             string            `json:"uid,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	CreatedAt       time.Time         `json:"createdAt,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Object          map[string]any    `json:"object"`
	YAML            string            `json:"yaml"`
	Redacted        bool              `json:"redacted,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
}
