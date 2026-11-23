package egress

import "context"

const (
	NetworkPolicyModeDisabled = "disabled"
	NetworkPolicyModeHelm     = "helm"
	NetworkPolicyModeRuntime  = "runtime"
)

// NetworkPolicyStatus describes who owns outbound NetworkPolicy changes. It is
// safe to expose through the API and never contains Secret material.
type NetworkPolicyStatus struct {
	Enabled        bool   `json:"enabled"`
	Mode           string `json:"mode"`
	AutoManage     bool   `json:"autoManage"`
	CanAutoManage  bool   `json:"canAutoManage"`
	PolicyName     string `json:"policyName,omitempty"`
	HelmPolicyName string `json:"helmPolicyName,omitempty"`
	Message        string `json:"message,omitempty"`
}

// ProxyTarget is a resolved proxy endpoint. Addresses contains the exact IPs
// that must be permitted; CIDRs is derived from those addresses and never
// contains a broad default route.
type ProxyTarget struct {
	Kind         string   `json:"kind"`
	URL          string   `json:"url"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Hostname     bool     `json:"hostname"`
	Addresses    []string `json:"addresses"`
	CIDRs        []string `json:"cidrs"`
	Reachable    bool     `json:"reachable"`
	LatencyMS    int64    `json:"latencyMs,omitempty"`
	Error        string   `json:"error,omitempty"`
	Allowed      bool     `json:"allowed"`
	PolicySource string   `json:"policySource,omitempty"`
}

// Preflight is the result of validating a proposed egress configuration before
// it is persisted or allowed to change the effective transport.
type Preflight struct {
	Status           string              `json:"status"`
	Message          string              `json:"message"`
	NetworkPolicy    NetworkPolicyStatus `json:"networkPolicy"`
	Targets          []ProxyTarget       `json:"targets"`
	RequiredIPBlocks []string            `json:"requiredIpBlocks"`
	RequiredPorts    []int               `json:"requiredPorts"`
}

// NetworkPolicyManager reconciles the dedicated runtime NetworkPolicy and
// inspects the current policy set for preflight reporting.
type NetworkPolicyManager interface {
	Status() NetworkPolicyStatus
	CheckTargets(ctx context.Context, targets []ProxyTarget) ([]ProxyTarget, error)
	Ensure(ctx context.Context, targets []ProxyTarget) error
	Cleanup(ctx context.Context) error
}
