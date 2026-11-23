package domain

import (
	"fmt"
	"time"
)

// HelmRepository is an administrator-managed Chart repository. Credentials are
// held separately from the catalog and must never be returned to the browser.
type HelmRepository struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	URL              string                    `json:"url"`
	Description      string                    `json:"description,omitempty"`
	Default          bool                      `json:"default"`
	Enabled          bool                      `json:"enabled"`
	SyncEnabled      bool                      `json:"syncEnabled"`
	SyncIntervalSec  int                       `json:"syncIntervalSeconds"`
	SyncFailureCount int                       `json:"syncFailureCount,omitempty"`
	InsecureSkipTLS  bool                      `json:"insecureSkipTLSVerify"`
	AuthType         string                    `json:"authType"`
	Credentials      HelmRepositoryCredentials `json:"-"`
	HasCredentials   bool                      `json:"hasCredentials"`
	HasCustomCA      bool                      `json:"hasCustomCA"`
	LastSync         *HelmRepositorySync       `json:"lastSync,omitempty"`
	SyncHistory      []HelmRepositorySync      `json:"syncHistory,omitempty"`
	NextSyncAt       *time.Time                `json:"nextSyncAt,omitempty"`
	ChartCount       int                       `json:"chartCount"`
	Revision         string                    `json:"revision,omitempty"`
	CreatedAt        time.Time                 `json:"createdAt"`
	UpdatedAt        time.Time                 `json:"updatedAt"`
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
	SyncEnabled      *bool  `json:"syncEnabled,omitempty"`
	SyncIntervalSec  int    `json:"syncIntervalSeconds,omitempty"`
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

	// DefaultHelmRepositorySyncInterval is three hours. Scheduled synchronization
	// remains opt-in so an administrator explicitly accepts upstream polling.
	DefaultHelmRepositorySyncInterval = 3 * time.Hour
	MinHelmRepositorySyncInterval     = 5 * time.Minute
	MaxHelmRepositorySyncInterval     = 7 * 24 * time.Hour

	HelmRepositorySyncTriggerCreate    = "create"
	HelmRepositorySyncTriggerUpdate    = "update"
	HelmRepositorySyncTriggerManual    = "manual"
	HelmRepositorySyncTriggerScheduled = "scheduled"
	MaxHelmRepositorySyncHistory       = 20
)

type HelmRepositorySync struct {
	OK         bool      `json:"ok"`
	Message    string    `json:"message"`
	ChartCount int       `json:"chartCount"`
	Trigger    string    `json:"trigger,omitempty"`
	Attempt    int       `json:"attempt,omitempty"`
	DurationMS int64     `json:"durationMs,omitempty"`
	SyncedAt   time.Time `json:"syncedAt"`
}

type HelmCacheStats struct {
	Root         string    `json:"root"`
	LimitBytes   int64     `json:"limitBytes"`
	TotalBytes   int64     `json:"totalBytes"`
	IndexBytes   int64     `json:"indexBytes"`
	PackageBytes int64     `json:"packageBytes"`
	IndexCount   int       `json:"indexCount"`
	PackageCount int       `json:"packageCount"`
	OldestAt     time.Time `json:"oldestAt,omitempty"`
	NewestAt     time.Time `json:"newestAt,omitempty"`
}

type HelmCacheCleanupResult struct {
	DeletedFiles int            `json:"deletedFiles"`
	FreedBytes   int64          `json:"freedBytes"`
	Stats        HelmCacheStats `json:"stats"`
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
	Mode        string                     `json:"mode"`
	Namespace   string                     `json:"namespace"`
	ReleaseName string                     `json:"releaseName"`
	Values      string                     `json:"values,omitempty"`
	Options     *HelmExecutionOptionsInput `json:"options,omitempty"`
	// ExpectedRevision is an internal execution guard. It is populated from
	// the reviewed plan after the server has acquired the release lock and is
	// never accepted from an API caller.
	ExpectedRevision int `json:"-"`
}

// HelmExecutionOptionsInput lets API callers omit individual execution flags.
// Omitted flags are resolved to conservative defaults by
// ResolveHelmExecutionOptions.
type HelmExecutionOptionsInput struct {
	TimeoutSeconds  int   `json:"timeoutSeconds,omitempty"`
	Wait            *bool `json:"wait,omitempty"`
	WaitForJobs     *bool `json:"waitForJobs,omitempty"`
	Atomic          *bool `json:"atomic,omitempty"`
	CleanupOnFail   *bool `json:"cleanupOnFail,omitempty"`
	MaxHistory      int   `json:"maxHistory,omitempty"`
	CreateNamespace *bool `json:"createNamespace,omitempty"`
	DisableHooks    *bool `json:"disableHooks,omitempty"`
	SkipCRDs        *bool `json:"skipCRDs,omitempty"`
	KeepHistory     *bool `json:"keepHistory,omitempty"`
}

// HelmExecutionOptions is the normalized policy persisted with an operation
// and used by the platform adapter.
type HelmExecutionOptions struct {
	TimeoutSeconds  int  `json:"timeoutSeconds"`
	Wait            bool `json:"wait"`
	WaitForJobs     bool `json:"waitForJobs"`
	Atomic          bool `json:"atomic"`
	CleanupOnFail   bool `json:"cleanupOnFail"`
	MaxHistory      int  `json:"maxHistory"`
	CreateNamespace bool `json:"createNamespace"`
	DisableHooks    bool `json:"disableHooks"`
	SkipCRDs        bool `json:"skipCRDs"`
	KeepHistory     bool `json:"keepHistory"`
}

const (
	DefaultHelmTimeoutSeconds = 600
	MinHelmTimeoutSeconds     = 30
	MaxHelmTimeoutSeconds     = 3600
	DefaultHelmMaxHistory     = 10
	MinHelmMaxHistory         = 1
	MaxHelmMaxHistory         = 100
)

// ResolveHelmExecutionOptions applies defaults and validates the bounded Helm
// execution policy. A nil input is valid and produces the product defaults.
func ResolveHelmExecutionOptions(input *HelmExecutionOptionsInput) (HelmExecutionOptions, error) {
	options := HelmExecutionOptions{
		TimeoutSeconds: DefaultHelmTimeoutSeconds,
		Wait:           true, WaitForJobs: true, Atomic: true, CleanupOnFail: true,
		MaxHistory: DefaultHelmMaxHistory,
	}
	if input == nil {
		return options, nil
	}
	if input.TimeoutSeconds != 0 {
		options.TimeoutSeconds = input.TimeoutSeconds
	}
	if input.MaxHistory != 0 {
		options.MaxHistory = input.MaxHistory
	}
	options.Wait = helmBoolOption(input.Wait, options.Wait)
	options.WaitForJobs = helmBoolOption(input.WaitForJobs, options.WaitForJobs)
	options.Atomic = helmBoolOption(input.Atomic, options.Atomic)
	options.CleanupOnFail = helmBoolOption(input.CleanupOnFail, options.CleanupOnFail)
	options.CreateNamespace = helmBoolOption(input.CreateNamespace, options.CreateNamespace)
	options.DisableHooks = helmBoolOption(input.DisableHooks, options.DisableHooks)
	options.SkipCRDs = helmBoolOption(input.SkipCRDs, options.SkipCRDs)
	options.KeepHistory = helmBoolOption(input.KeepHistory, options.KeepHistory)
	if options.TimeoutSeconds < MinHelmTimeoutSeconds || options.TimeoutSeconds > MaxHelmTimeoutSeconds {
		return HelmExecutionOptions{}, fmt.Errorf("%w: Helm timeout must be between %d and %d seconds", ErrInvalid, MinHelmTimeoutSeconds, MaxHelmTimeoutSeconds)
	}
	if options.MaxHistory < MinHelmMaxHistory || options.MaxHistory > MaxHelmMaxHistory {
		return HelmExecutionOptions{}, fmt.Errorf("%w: Helm max history must be between %d and %d", ErrInvalid, MinHelmMaxHistory, MaxHelmMaxHistory)
	}
	if options.Atomic && !options.Wait {
		return HelmExecutionOptions{}, fmt.Errorf("%w: Helm atomic execution requires wait=true", ErrInvalid)
	}
	if options.CleanupOnFail && !options.Wait {
		return HelmExecutionOptions{}, fmt.Errorf("%w: cleanupOnFail requires wait=true", ErrInvalid)
	}
	return options, nil
}

func helmBoolOption(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
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
	Options         HelmExecutionOptions    `json:"options"`
}

type HelmChartPlanTemplate struct {
	ChartName       string
	ChartVersion    string
	Manifest        string
	Values          string
	Resources       []HelmChartPlanResource
	CurrentRevision int
	Warnings        []string
}

type HelmReleaseUpgradeOperationRequest struct {
	Namespace       string                     `json:"namespace"`
	ReleaseName     string                     `json:"releaseName"`
	Values          string                     `json:"values,omitempty"`
	Confirmation    string                     `json:"confirmation"`
	CurrentRevision int                        `json:"currentRevision,omitempty"`
	Options         *HelmExecutionOptionsInput `json:"options,omitempty"`
}

type HelmChartOperationRequest struct {
	HelmChartPlanRequest
	PlanDigest   string `json:"planDigest"`
	Confirmation string `json:"confirmation"`
}

type HelmChartUninstallRequest struct {
	Namespace    string                     `json:"namespace"`
	ReleaseName  string                     `json:"releaseName"`
	Confirmation string                     `json:"confirmation"`
	Options      *HelmExecutionOptionsInput `json:"options,omitempty"`
}

type HelmChartRollbackRequest struct {
	Namespace      string                     `json:"namespace"`
	ReleaseName    string                     `json:"releaseName"`
	TargetRevision int                        `json:"targetRevision"`
	Confirmation   string                     `json:"confirmation"`
	Options        *HelmExecutionOptionsInput `json:"options,omitempty"`
}

type HelmChartOperation struct {
	ID              string                  `json:"id"`
	Type            string                  `json:"type"`
	Status          string                  `json:"status"`
	Message         string                  `json:"message,omitempty"`
	RepositoryID    string                  `json:"repositoryId,omitempty"`
	ChartName       string                  `json:"chartName,omitempty"`
	ChartVersion    string                  `json:"chartVersion,omitempty"`
	Digest          string                  `json:"digest,omitempty"`
	Namespace       string                  `json:"namespace"`
	ReleaseName     string                  `json:"releaseName"`
	Revision        int                     `json:"revision,omitempty"`
	Resources       []HelmOperationResource `json:"resources,omitempty"`
	Warnings        []string                `json:"warnings,omitempty"`
	Options         *HelmExecutionOptions   `json:"options,omitempty"`
	StartedAt       time.Time               `json:"startedAt"`
	FinishedAt      time.Time               `json:"finishedAt,omitempty"`
	HeartbeatAt     time.Time               `json:"heartbeatAt,omitempty"`
	CancelRequested bool                    `json:"cancelRequested,omitempty"`
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
	HelmOperationPending   = "pending"
	HelmOperationRunning   = "running"
	HelmOperationSuccess   = "success"
	HelmOperationFailed    = "failed"
	HelmOperationCancelled = "cancelled"
)

type HelmOperationFilter struct {
	Limit       int
	Status      string
	Namespace   string
	ReleaseName string
}
