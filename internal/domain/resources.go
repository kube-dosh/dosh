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

// ResourceColumn describes one column returned by the Kubernetes Table API or
// a safe fallback set used when a server does not implement Table responses.
type ResourceColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
}

// ResourceRule is the normalized subset of Kubernetes RBAC resource rules used
// to filter API Discovery results. It mirrors authorizationv1.ResourceRule
// without leaking the Kubernetes transport type into application modules.
type ResourceRule struct {
	Verbs         []string `json:"verbs,omitempty"`
	APIGroups     []string `json:"apiGroups,omitempty"`
	Resources     []string `json:"resources,omitempty"`
	ResourceNames []string `json:"resourceNames,omitempty"`
}

// ResourceRules contains the rules applicable to namespaced objects in one
// namespace and to cluster-scoped objects. Namespaced may be empty when the
// caller has not selected a namespace.
type ResourceRules struct {
	Namespaced []ResourceRule `json:"namespaced,omitempty"`
	Cluster    []ResourceRule `json:"cluster,omitempty"`
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
	Resource        string            `json:"resource,omitempty"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	UID             string            `json:"uid,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	CreatedAt       time.Time         `json:"createdAt,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Cells           map[string]any    `json:"cells,omitempty"`
}

// GenericResourcePage is one bounded page of generic resources together with
// the display columns selected for the resource type.
type GenericResourcePage struct {
	PageResult[GenericResourceSummary]
	Columns []ResourceColumn `json:"columns,omitempty"`
}

// GenericResourceSearch contains bounded cross-type search results. Warnings
// identify types that could not be searched without failing the whole result.
type GenericResourceSearch struct {
	Items     []GenericResourceSummary `json:"items"`
	Warnings  []string                 `json:"warnings,omitempty"`
	Truncated bool                     `json:"truncated,omitempty"`
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
