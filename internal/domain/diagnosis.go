package domain

import "time"

const (
	DiagnosisHealthy  = "healthy"
	DiagnosisInfo     = "info"
	DiagnosisWarning  = "warning"
	DiagnosisCritical = "critical"
)

const (
	DiagnosisCheckPass     = "pass"
	DiagnosisCheckInfo     = "info"
	DiagnosisCheckWarning  = "warning"
	DiagnosisCheckCritical = "critical"
	DiagnosisCheckSkipped  = "skipped"
)

// DiagnosisTarget identifies the Kubernetes object inspected by a diagnosis.
type DiagnosisTarget struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
}

// Diagnosis is a deterministic, read-only assessment of one Kubernetes object.
// It intentionally avoids opaque health scores and keeps every conclusion tied
// to observable checks and evidence.
type Diagnosis struct {
	Target      DiagnosisTarget    `json:"target"`
	Severity    string             `json:"severity"`
	Summary     string             `json:"summary"`
	GeneratedAt time.Time          `json:"generatedAt"`
	Checks      []DiagnosisCheck   `json:"checks"`
	Findings    []DiagnosisFinding `json:"findings"`
	Related     []DiagnosisRelated `json:"related"`
	Warnings    []string           `json:"warnings"`
}

type DiagnosisCheck struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type DiagnosisFinding struct {
	ID              string                    `json:"id"`
	Severity        string                    `json:"severity"`
	Title           string                    `json:"title"`
	Cause           string                    `json:"cause"`
	Evidence        []DiagnosisEvidence       `json:"evidence"`
	Recommendations []DiagnosisRecommendation `json:"recommendations"`
}

type DiagnosisEvidence struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// DiagnosisRecommendation is a navigation target or a copyable command. Dosh
// never executes a remediation from the diagnosis response itself.
type DiagnosisRecommendation struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Command     string `json:"command,omitempty"`
}

type DiagnosisRelated struct {
	Kind         string `json:"kind"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name"`
	Status       string `json:"status,omitempty"`
	Relationship string `json:"relationship"`
	Path         string `json:"path,omitempty"`
}
