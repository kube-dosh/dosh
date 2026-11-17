package domain

import "time"

const (
	RegistryAuthAnonymous = "anonymous"
	RegistryAuthBasic     = "basic"
	RegistryAuthBearer    = "bearer"
)

// ContainerRegistryCredentials are stored separately from the registry record
// and are deliberately excluded from JSON so they can never be returned by an
// API handler that serializes ContainerRegistry.
type ContainerRegistryCredentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	CAPEM    string `json:"caPem,omitempty"`
}

type ContainerRegistryCapabilities struct {
	Catalog bool `json:"catalog"`
	Tags    bool `json:"tags"`
}

type ContainerRegistryTest struct {
	OK           bool                          `json:"ok"`
	Message      string                        `json:"message,omitempty"`
	TestedAt     time.Time                     `json:"testedAt"`
	Capabilities ContainerRegistryCapabilities `json:"capabilities"`
}

type ContainerRegistry struct {
	ID                    string                        `json:"id"`
	Name                  string                        `json:"name"`
	Endpoint              string                        `json:"endpoint"`
	RepositoryPrefix      string                        `json:"repositoryPrefix,omitempty"`
	AuthType              string                        `json:"authType"`
	HasCredentials        bool                          `json:"hasCredentials"`
	HasCustomCA           bool                          `json:"hasCustomCa"`
	InsecureSkipTLSVerify bool                          `json:"insecureSkipTlsVerify"`
	PullSecretName        string                        `json:"pullSecretName,omitempty"`
	Default               bool                          `json:"default"`
	Capabilities          ContainerRegistryCapabilities `json:"capabilities"`
	LastTest              *ContainerRegistryTest        `json:"lastTest,omitempty"`
	CreatedAt             time.Time                     `json:"createdAt"`
	UpdatedAt             time.Time                     `json:"updatedAt"`
	Revision              string                        `json:"revision,omitempty"`
	Credentials           ContainerRegistryCredentials  `json:"-"`
}

type ContainerRegistryUpsert struct {
	Name                  string                        `json:"name"`
	Endpoint              string                        `json:"endpoint"`
	RepositoryPrefix      string                        `json:"repositoryPrefix,omitempty"`
	AuthType              string                        `json:"authType"`
	Credentials           *ContainerRegistryCredentials `json:"credentials,omitempty"`
	ClearCredentials      bool                          `json:"clearCredentials,omitempty"`
	InsecureSkipTLSVerify bool                          `json:"insecureSkipTlsVerify,omitempty"`
	PullSecretName        string                        `json:"pullSecretName,omitempty"`
	Default               bool                          `json:"default,omitempty"`
	Revision              string                        `json:"revision,omitempty"`
}

type ContainerRegistryRepository struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type ContainerRegistryTag struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type ImageReferenceResolution struct {
	Image            string `json:"image"`
	ExplicitRegistry bool   `json:"explicitRegistry"`
	RegistryID       string `json:"registryId,omitempty"`
	RegistryName     string `json:"registryName,omitempty"`
	RegistryEndpoint string `json:"registryEndpoint,omitempty"`
	Repository       string `json:"repository,omitempty"`
	Tag              string `json:"tag,omitempty"`
	Digest           string `json:"digest,omitempty"`
	Matched          bool   `json:"matched"`
}
