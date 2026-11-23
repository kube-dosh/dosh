package domain

type EgressStatus struct {
	Mode                  string `json:"mode"`
	Enabled               bool   `json:"enabled"`
	HTTPProxy             string `json:"httpProxy,omitempty"`
	HTTPSProxy            string `json:"httpsProxy,omitempty"`
	NoProxy               string `json:"noProxy,omitempty"`
	CredentialsConfigured bool   `json:"credentialsConfigured"`
	CAConfigured          bool   `json:"caConfigured"`
	Source                string `json:"source"`
}

type EgressSettings struct {
	Mode                  string                    `json:"mode"`
	EffectiveMode         string                    `json:"effectiveMode"`
	Enabled               bool                      `json:"enabled"`
	HTTPProxy             string                    `json:"httpProxy,omitempty"`
	HTTPSProxy            string                    `json:"httpsProxy,omitempty"`
	NoProxy               string                    `json:"noProxy,omitempty"`
	Username              string                    `json:"username,omitempty"`
	CredentialsConfigured bool                      `json:"credentialsConfigured"`
	CAConfigured          bool                      `json:"caConfigured"`
	Source                string                    `json:"source"`
	Revision              string                    `json:"revision,omitempty"`
	UpdatedAt             string                    `json:"updatedAt,omitempty"`
	NetworkPolicy         EgressNetworkPolicyStatus `json:"networkPolicy"`
}

type EgressNetworkPolicyStatus struct {
	Enabled        bool   `json:"enabled"`
	Mode           string `json:"mode"`
	AutoManage     bool   `json:"autoManage"`
	CanAutoManage  bool   `json:"canAutoManage"`
	PolicyName     string `json:"policyName,omitempty"`
	HelmPolicyName string `json:"helmPolicyName,omitempty"`
	Message        string `json:"message,omitempty"`
}

type EgressProxyTarget struct {
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

type EgressPreflight struct {
	Status           string                    `json:"status"`
	Message          string                    `json:"message"`
	NetworkPolicy    EgressNetworkPolicyStatus `json:"networkPolicy"`
	Targets          []EgressProxyTarget       `json:"targets"`
	RequiredIPBlocks []string                  `json:"requiredIpBlocks"`
	RequiredPorts    []int                     `json:"requiredPorts"`
}

type EgressSettingsUpdate struct {
	Mode             string `json:"mode"`
	HTTPProxy        string `json:"httpProxy"`
	HTTPSProxy       string `json:"httpsProxy"`
	NoProxy          string `json:"noProxy"`
	Username         string `json:"username"`
	Password         string `json:"password,omitempty"`
	ClearCredentials bool   `json:"clearCredentials"`
	CAPEM            string `json:"caPEM,omitempty"`
	ClearCA          bool   `json:"clearCA"`
	Revision         string `json:"revision,omitempty"`
}

type SystemInfo struct {
	Version        string       `json:"version"`
	Commit         string       `json:"commit"`
	BuildDate      string       `json:"buildDate"`
	ChartVersion   string       `json:"chartVersion"`
	GoVersion      string       `json:"goVersion"`
	ClusterName    string       `json:"clusterName"`
	AuthMode       string       `json:"authMode"`
	ReadOnly       bool         `json:"readOnly"`
	RequestTimeout string       `json:"requestTimeout"`
	Egress         EgressStatus `json:"egress"`
}

type UpdateStatus struct {
	Enabled        bool   `json:"enabled"`
	Status         string `json:"status"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	LatestURL      string `json:"latestUrl,omitempty"`
	PublishedAt    string `json:"publishedAt,omitempty"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	Notes          string `json:"notes,omitempty"`
	Message        string `json:"message,omitempty"`
	Source         string `json:"source,omitempty"`
}
