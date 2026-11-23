package domain

import "time"

// HelmRepository is an administrator-managed Chart repository. Credentials are
// held separately from the catalog and must never be returned to the browser.
type HelmRepository struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	URL             string                    `json:"url"`
	Description     string                    `json:"description,omitempty"`
	Default         bool                      `json:"default"`
	Enabled         bool                      `json:"enabled"`
	InsecureSkipTLS bool                      `json:"insecureSkipTLSVerify"`
	AuthType        string                    `json:"authType"`
	Credentials     HelmRepositoryCredentials `json:"-"`
	HasCredentials  bool                      `json:"hasCredentials"`
	HasCustomCA     bool                      `json:"hasCustomCA"`
	LastSync        *HelmRepositorySync       `json:"lastSync,omitempty"`
	ChartCount      int                       `json:"chartCount"`
	Revision        string                    `json:"revision,omitempty"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
}

type HelmRepositoryCredentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	CAPEM    string `json:"caPEM,omitempty"`
}

type HelmRepositoryUpsert struct {
	Name             string `json:"name"`
	URL              string `json:"url"`
	Description      string `json:"description,omitempty"`
	Default          bool   `json:"default"`
	Enabled          *bool  `json:"enabled,omitempty"`
	InsecureSkipTLS  bool   `json:"insecureSkipTLSVerify,omitempty"`
	AuthType         string `json:"authType,omitempty"`
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	Token            string `json:"token,omitempty"`
	CAPEM            string `json:"caPEM,omitempty"`
	ClearCredentials bool   `json:"clearCredentials,omitempty"`
	Revision         string `json:"revision,omitempty"`
}

const (
	HelmRepositoryAuthAnonymous = "anonymous"
	HelmRepositoryAuthBasic     = "basic"
	HelmRepositoryAuthBearer    = "bearer"
)

type HelmRepositorySync struct {
	OK         bool      `json:"ok"`
	Message    string    `json:"message"`
	ChartCount int       `json:"chartCount"`
	SyncedAt   time.Time `json:"syncedAt"`
}

type HelmRepositoryCapabilities struct {
	Index       bool `json:"index"`
	Credentials bool `json:"credentials"`
	CustomCA    bool `json:"customCA"`
}

type HelmChartSummary struct {
	RepositoryID   string    `json:"repositoryId"`
	RepositoryName string    `json:"repositoryName"`
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	AppVersion     string    `json:"appVersion,omitempty"`
	Description    string    `json:"description,omitempty"`
	Icon           string    `json:"icon,omitempty"`
	Deprecated     bool      `json:"deprecated"`
	Digest         string    `json:"digest,omitempty"`
	CreatedAt      time.Time `json:"createdAt,omitempty"`
	Keywords       []string  `json:"keywords,omitempty"`
}

type HelmChartVersion struct {
	Version    string    `json:"version"`
	AppVersion string    `json:"appVersion,omitempty"`
	Digest     string    `json:"digest,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	Deprecated bool      `json:"deprecated"`
	URLs       []string  `json:"urls,omitempty"`
}

type HelmChartMaintainer struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

type HelmChartDependency struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Repository string `json:"repository,omitempty"`
	Condition  string `json:"condition,omitempty"`
	Alias      string `json:"alias,omitempty"`
}

type HelmChartDetail struct {
	HelmChartSummary
	Home         string                `json:"home,omitempty"`
	Sources      []string              `json:"sources,omitempty"`
	Maintainers  []HelmChartMaintainer `json:"maintainers,omitempty"`
	Readme       string                `json:"readme,omitempty"`
	Values       string                `json:"values"`
	Schema       string                `json:"schema,omitempty"`
	Dependencies []HelmChartDependency `json:"dependencies,omitempty"`
	Versions     []HelmChartVersion    `json:"versions"`
	Warnings     []string              `json:"warnings,omitempty"`
}

type HelmChartRef struct {
	RepositoryID string `json:"repositoryId"`
	ChartName    string `json:"chartName"`
	Version      string `json:"version"`
	Digest       string `json:"digest,omitempty"`
}

type HelmChartPlanRequest struct {
	HelmChartRef
	Mode        string `json:"mode"`
	Namespace   string `json:"namespace"`
	ReleaseName string `json:"releaseName"`
	Values      string `json:"values,omitempty"`
}

type HelmChartPlanResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Hook       bool   `json:"hook"`
	CRD        bool   `json:"crd"`
	Action     string `json:"action"`
	APIGroup   string `json:"apiGroup,omitempty"`
	Resource   string `json:"resource"`
	Exists     bool   `json:"exists"`
}

type HelmChartPlan struct {
	Action          string                  `json:"action"`
	RepositoryID    string                  `json:"repositoryId"`
	RepositoryName  string                  `json:"repositoryName"`
	ChartName       string                  `json:"chartName"`
	ChartVersion    string                  `json:"chartVersion"`
	AppVersion      string                  `json:"appVersion,omitempty"`
	Digest          string                  `json:"digest"`
	Namespace       string                  `json:"namespace"`
	ReleaseName     string                  `json:"releaseName"`
	CurrentRevision int                     `json:"currentRevision,omitempty"`
	Manifest        string                  `json:"manifest"`
	Values          string                  `json:"values"`
	Resources       []HelmChartPlanResource `json:"resources"`
	HookCount       int                     `json:"hookCount"`
	CRDCount        int                     `json:"crdCount"`
	Warnings        []string                `json:"warnings,omitempty"`
	PlanDigest      string                  `json:"planDigest"`
}

type HelmChartPlanTemplate struct {
	Manifest        string
	Values          string
	Resources       []HelmChartPlanResource
	CurrentRevision int
	Warnings        []string
}

type HelmChartOperationRequest struct {
	HelmChartPlanRequest
	PlanDigest   string `json:"planDigest"`
	Confirmation string `json:"confirmation"`
}

type HelmChartUninstallRequest struct {
	Namespace    string `json:"namespace"`
	ReleaseName  string `json:"releaseName"`
	Confirmation string `json:"confirmation"`
}

type HelmChartRollbackRequest struct {
	Namespace      string `json:"namespace"`
	ReleaseName    string `json:"releaseName"`
	TargetRevision int    `json:"targetRevision"`
	Confirmation   string `json:"confirmation"`
}

type HelmChartOperation struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Status       string                  `json:"status"`
	Message      string                  `json:"message,omitempty"`
	RepositoryID string                  `json:"repositoryId,omitempty"`
	ChartName    string                  `json:"chartName,omitempty"`
	ChartVersion string                  `json:"chartVersion,omitempty"`
	Digest       string                  `json:"digest,omitempty"`
	Namespace    string                  `json:"namespace"`
	ReleaseName  string                  `json:"releaseName"`
	Revision     int                     `json:"revision,omitempty"`
	Resources    []HelmOperationResource `json:"resources,omitempty"`
	Warnings     []string                `json:"warnings,omitempty"`
	StartedAt    time.Time               `json:"startedAt"`
	FinishedAt   time.Time               `json:"finishedAt,omitempty"`
}

// HelmOperationResource is the live status of one resource rendered by a Helm
// operation. The plan identity is copied so clients can keep rendering stable
// progress rows even after the operation record is refreshed.
type HelmOperationResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Hook       bool   `json:"hook"`
	CRD        bool   `json:"crd"`
	Action     string `json:"action"`
	APIGroup   string `json:"apiGroup,omitempty"`
	Resource   string `json:"resource"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

const (
	HelmOperationPending = "pending"
	HelmOperationRunning = "running"
	HelmOperationSuccess = "success"
	HelmOperationFailed  = "failed"
)
