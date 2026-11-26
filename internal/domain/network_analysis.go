package domain

import "time"

// GatewayAPICondition is deliberately represented by the shared Condition
// type so Gateway resources use the same status vocabulary as workloads.
type GatewayAPIAddress struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type GatewayAPIListener struct {
	Name          string      `json:"name"`
	Protocol      string      `json:"protocol"`
	Port          int32       `json:"port"`
	Hostname      string      `json:"hostname,omitempty"`
	TLSMode       string      `json:"tlsMode,omitempty"`
	AllowedRoutes string      `json:"allowedRoutes,omitempty"`
	Conditions    []Condition `json:"conditions,omitempty"`
}

type GatewayClassSummary struct {
	Name            string      `json:"name"`
	Controller      string      `json:"controller"`
	Status          string      `json:"status"`
	Conditions      []Condition `json:"conditions,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	APIVersion      string      `json:"apiVersion"`
	Resource        string      `json:"resource"`
}

type GatewaySummary struct {
	Name            string               `json:"name"`
	Namespace       string               `json:"namespace"`
	ClassName       string               `json:"className"`
	Addresses       []GatewayAPIAddress  `json:"addresses,omitempty"`
	Listeners       []GatewayAPIListener `json:"listeners,omitempty"`
	Conditions      []Condition          `json:"conditions,omitempty"`
	Status          string               `json:"status"`
	CreatedAt       time.Time            `json:"createdAt"`
	ResourceVersion string               `json:"resourceVersion,omitempty"`
	APIVersion      string               `json:"apiVersion"`
	Resource        string               `json:"resource"`
}

type GatewayParentReference struct {
	Group       string `json:"group,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	SectionName string `json:"sectionName,omitempty"`
	Port        int32  `json:"port,omitempty"`
}

type GatewayRouteBackend struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Port      string `json:"port,omitempty"`
	Weight    int32  `json:"weight,omitempty"`
}

type GatewayRouteMatch struct {
	Path     string   `json:"path,omitempty"`
	PathType string   `json:"pathType,omitempty"`
	Method   string   `json:"method,omitempty"`
	Headers  []string `json:"headers,omitempty"`
	Query    []string `json:"query,omitempty"`
}

type GatewayRouteRule struct {
	Matches  []GatewayRouteMatch   `json:"matches,omitempty"`
	Filters  []string              `json:"filters,omitempty"`
	Backends []GatewayRouteBackend `json:"backends,omitempty"`
}

type GatewayRouteSummary struct {
	Kind            string                   `json:"kind"`
	Name            string                   `json:"name"`
	Namespace       string                   `json:"namespace"`
	Hostnames       []string                 `json:"hostnames,omitempty"`
	ParentRefs      []GatewayParentReference `json:"parentRefs,omitempty"`
	Rules           []GatewayRouteRule       `json:"rules,omitempty"`
	Conditions      []Condition              `json:"conditions,omitempty"`
	Status          string                   `json:"status"`
	CreatedAt       time.Time                `json:"createdAt"`
	ResourceVersion string                   `json:"resourceVersion,omitempty"`
	APIVersion      string                   `json:"apiVersion"`
	Resource        string                   `json:"resource"`
}

type GatewayAPIOverview struct {
	Available   bool                  `json:"available"`
	Version     string                `json:"version,omitempty"`
	Warnings    []string              `json:"warnings,omitempty"`
	Classes     []GatewayClassSummary `json:"classes"`
	Gateways    []GatewaySummary      `json:"gateways"`
	Routes      []GatewayRouteSummary `json:"routes"`
	GeneratedAt time.Time             `json:"generatedAt"`
}

type NetworkPolicyIPBlock struct {
	CIDR   string   `json:"cidr"`
	Except []string `json:"except,omitempty"`
}

// Nil selectors mean the selector was absent. An empty, non-nil selector means
// the Kubernetes selector was explicitly empty and therefore selects all.
type NetworkPolicyPeer struct {
	PodSelector                  map[string]string          `json:"podSelector,omitempty"`
	PodSelectorExpressions       []LabelSelectorRequirement `json:"podSelectorExpressions,omitempty"`
	NamespaceSelector            map[string]string          `json:"namespaceSelector,omitempty"`
	NamespaceSelectorExpressions []LabelSelectorRequirement `json:"namespaceSelectorExpressions,omitempty"`
	IPBlock                      *NetworkPolicyIPBlock      `json:"ipBlock,omitempty"`
}

type LabelSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

type NetworkPolicyPort struct {
	Protocol string `json:"protocol"`
	Port     string `json:"port,omitempty"`
	EndPort  int32  `json:"endPort,omitempty"`
}

type NetworkPolicyRule struct {
	Peers []NetworkPolicyPeer `json:"peers,omitempty"`
	Ports []NetworkPolicyPort `json:"ports,omitempty"`
}

type NetworkPolicySummary struct {
	Name                   string                     `json:"name"`
	Namespace              string                     `json:"namespace"`
	PodSelector            map[string]string          `json:"podSelector"`
	PodSelectorExpressions []LabelSelectorRequirement `json:"podSelectorExpressions,omitempty"`
	PolicyTypes            []string                   `json:"policyTypes"`
	Ingress                []NetworkPolicyRule        `json:"ingress"`
	Egress                 []NetworkPolicyRule        `json:"egress"`
	MatchedPods            int                        `json:"matchedPods"`
	Warnings               []string                   `json:"warnings,omitempty"`
	ResourceVersion        string                     `json:"resourceVersion,omitempty"`
	CreatedAt              time.Time                  `json:"createdAt"`
	APIVersion             string                     `json:"apiVersion"`
	Resource               string                     `json:"resource"`
}

type NetworkPolicyOverview struct {
	Items       []NetworkPolicySummary `json:"items"`
	Warnings    []string               `json:"warnings,omitempty"`
	GeneratedAt time.Time              `json:"generatedAt"`
}

type ConnectivityRequest struct {
	SourceNamespace      string `json:"sourceNamespace"`
	SourcePod            string `json:"sourcePod"`
	DestinationNamespace string `json:"destinationNamespace"`
	DestinationService   string `json:"destinationService"`
	Port                 int32  `json:"port"`
	Protocol             string `json:"protocol"`
}

type ConnectivitySource struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	IP        string            `json:"ip,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Status    string            `json:"status,omitempty"`
}

type ConnectivityDestination struct {
	Name         string            `json:"name"`
	Namespace    string            `json:"namespace"`
	Type         string            `json:"type"`
	ClusterIP    string            `json:"clusterIP"`
	Headless     bool              `json:"headless"`
	ExternalName string            `json:"externalName,omitempty"`
	Selector     map[string]string `json:"selector,omitempty"`
}

type ConnectivityDNS struct {
	Name         string `json:"name"`
	ClusterIP    string `json:"clusterIP,omitempty"`
	ExternalName string `json:"externalName,omitempty"`
	Headless     bool   `json:"headless"`
	Explanation  string `json:"explanation"`
}

type ConnectivityPortMapping struct {
	ServicePort int32  `json:"servicePort"`
	TargetPort  string `json:"targetPort"`
	Protocol    string `json:"protocol"`
}

type ConnectivityCheck struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type ConnectivityPolicyDecision struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Direction string `json:"direction"`
	Selected  bool   `json:"selected"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason"`
}

type ConnectivityEndpoint struct {
	Pod            string   `json:"pod,omitempty"`
	PodIP          string   `json:"podIP,omitempty"`
	Ready          bool     `json:"ready"`
	EgressAllowed  *bool    `json:"egressAllowed,omitempty"`
	IngressAllowed *bool    `json:"ingressAllowed,omitempty"`
	Allowed        string   `json:"allowed"`
	Reasons        []string `json:"reasons,omitempty"`
}

type ConnectivityDiagnosis struct {
	Verdict         string                       `json:"verdict"`
	Summary         string                       `json:"summary"`
	Source          ConnectivitySource           `json:"source"`
	Destination     ConnectivityDestination      `json:"destination"`
	DNS             ConnectivityDNS              `json:"dns"`
	Port            ConnectivityPortMapping      `json:"port"`
	Endpoints       []ConnectivityEndpoint       `json:"endpoints"`
	Checks          []ConnectivityCheck          `json:"checks"`
	EgressPolicies  []ConnectivityPolicyDecision `json:"egressPolicies"`
	IngressPolicies []ConnectivityPolicyDecision `json:"ingressPolicies"`
	Findings        []DiagnosisFinding           `json:"findings"`
	Warnings        []string                     `json:"warnings,omitempty"`
	GeneratedAt     time.Time                    `json:"generatedAt"`
}
