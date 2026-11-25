package domain

import "time"

// Storage support levels describe the strength of the product guarantee, not
// the quality of the upstream storage implementation.
const (
	StorageSupportVerified = "verified"
	StorageSupportGeneric  = "generic"

	StorageCatalogAvailable         = "available"
	StorageCatalogMissingRepository = "repository-missing"
	StorageCatalogMissingChart      = "chart-missing"
	StorageCatalogForbidden         = "forbidden"

	StorageIntegrationManaged  = "managed"
	StorageIntegrationAdopted  = "adopted"
	StorageIntegrationHealthy  = "healthy"
	StorageIntegrationDegraded = "degraded"
	StorageIntegrationUnknown  = "unknown"

	StorageCheckPass    = "pass"
	StorageCheckWarning = "warning"
	StorageCheckFail    = "fail"
	StorageCheckInfo    = "info"
)

// StorageCenter is the product-level storage overview. StorageClass remains a
// Kubernetes resource; a Catalog item represents a driver/provisioner package
// that may create one or more classes.
type StorageCenter struct {
	Summary      StorageCenterSummary `json:"summary"`
	Catalog      []StorageCatalogItem `json:"catalog"`
	Integrations []StorageIntegration `json:"integrations"`
	Diagnostics  []StorageDiagnostic  `json:"diagnostics"`
	Warnings     []string             `json:"warnings,omitempty"`
	GeneratedAt  time.Time            `json:"generatedAt"`
}

type StorageCenterSummary struct {
	NodeCount                  int  `json:"nodeCount"`
	ReadyNodeCount             int  `json:"readyNodeCount"`
	StorageClassCount          int  `json:"storageClassCount"`
	DefaultStorageClassCount   int  `json:"defaultStorageClassCount"`
	PersistentVolumeCount      int  `json:"persistentVolumeCount"`
	PersistentVolumeClaimCount int  `json:"persistentVolumeClaimCount"`
	PendingClaimCount          int  `json:"pendingClaimCount"`
	ManagedIntegrationCount    int  `json:"managedIntegrationCount"`
	AdoptedIntegrationCount    int  `json:"adoptedIntegrationCount"`
	VolumeSnapshotAvailable    bool `json:"volumeSnapshotAvailable"`
}

type StorageCatalogItem struct {
	ID                     string               `json:"id"`
	Name                   string               `json:"name"`
	Vendor                 string               `json:"vendor"`
	Project                string               `json:"project"`
	Category               string               `json:"category"`
	Description            string               `json:"description"`
	SupportLevel           string               `json:"supportLevel"`
	Status                 string               `json:"status"`
	StatusMessage          string               `json:"statusMessage,omitempty"`
	Chart                  *StorageCatalogChart `json:"chart,omitempty"`
	DefaultNamespace       string               `json:"defaultNamespace"`
	DefaultReleaseName     string               `json:"defaultReleaseName"`
	Provisioners           []string             `json:"provisioners"`
	ExpectedStorageClasses []string             `json:"expectedStorageClasses"`
	Capabilities           []string             `json:"capabilities"`
	Prerequisites          []string             `json:"prerequisites"`
	Warnings               []string             `json:"warnings,omitempty"`
	Installed              bool                 `json:"installed"`
	IntegrationID          string               `json:"integrationId,omitempty"`
}

type StorageCatalogChart struct {
	RepositoryID   string `json:"repositoryId"`
	RepositoryName string `json:"repositoryName"`
	ChartName      string `json:"chartName"`
	Version        string `json:"version"`
	AppVersion     string `json:"appVersion,omitempty"`
	Digest         string `json:"digest,omitempty"`
	Deprecated     bool   `json:"deprecated"`
}

type StorageIntegration struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Provisioner      string   `json:"provisioner,omitempty"`
	Source           string   `json:"source"`
	Status           string   `json:"status"`
	ReleaseName      string   `json:"releaseName,omitempty"`
	ReleaseNamespace string   `json:"releaseNamespace,omitempty"`
	StorageClasses   []string `json:"storageClasses"`
	DriverRegistered bool     `json:"driverRegistered"`
	PodCount         int      `json:"podCount"`
	ReadyPodCount    int      `json:"readyPodCount"`
	DefaultClass     string   `json:"defaultClass,omitempty"`
	Message          string   `json:"message,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

type StoragePreflightRequest struct {
	CatalogID   string `json:"catalogId"`
	Namespace   string `json:"namespace,omitempty"`
	ReleaseName string `json:"releaseName,omitempty"`
}

type StoragePreflight struct {
	CatalogID string              `json:"catalogId"`
	Status    string              `json:"status"`
	Checks    []StorageCheck      `json:"checks"`
	Warnings  []string            `json:"warnings,omitempty"`
	Catalog   *StorageCatalogItem `json:"catalog,omitempty"`
}

type StorageCheck struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type StorageVerifyRequest struct {
	CatalogID      string `json:"catalogId,omitempty"`
	Namespace      string `json:"namespace"`
	StorageClass   string `json:"storageClass"`
	ReleaseName    string `json:"releaseName,omitempty"`
	SmokeTest      bool   `json:"smokeTest"`
	SmokeTestImage string `json:"smokeTestImage,omitempty"`
}

type StorageVerification struct {
	Status       string                  `json:"status"`
	Namespace    string                  `json:"namespace"`
	StorageClass string                  `json:"storageClass"`
	Checks       []StorageCheck          `json:"checks"`
	SmokeTest    *StorageSmokeTestResult `json:"smokeTest,omitempty"`
	Warnings     []string                `json:"warnings,omitempty"`
}

type StorageSmokeTestSpec struct {
	Namespace      string
	StorageClass   string
	Image          string
	TimeoutSeconds int
}

type StorageSmokeTestResult struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	StorageClass string `json:"storageClass"`
	PVCName      string `json:"pvcName,omitempty"`
	PodName      string `json:"podName,omitempty"`
	Bound        bool   `json:"bound"`
	PodRunning   bool   `json:"podRunning"`
	VolumeName   string `json:"volumeName,omitempty"`
	DurationMS   int64  `json:"durationMs"`
	CleanedUp    bool   `json:"cleanedUp"`
	Message      string `json:"message,omitempty"`
}

type StorageDiagnostic struct {
	ID         string `json:"id"`
	Severity   string `json:"severity"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	Resource   string `json:"resource,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

type CSIDriverInfo struct {
	Name                 string   `json:"name"`
	AttachRequired       bool     `json:"attachRequired"`
	PodInfoOnMount       bool     `json:"podInfoOnMount"`
	StorageCapacity      bool     `json:"storageCapacity"`
	VolumeLifecycleModes []string `json:"volumeLifecycleModes"`
}
