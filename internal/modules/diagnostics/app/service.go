package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"
)

const (
	maxDiagnosisEvents        = 8
	maxDiagnosisLogBytes      = 64 * 1024
	maxDiagnosisLogLines      = 80
	maxDiagnosisLogContainers = 2
	maxRelatedPods            = 10
)

var (
	dnsLabelPattern     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dnsSubdomainPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

// Repository is the read-only subset of Kubernetes data consumed by the
// diagnosis rules. Every call executes as the authenticated actor.
type Repository interface {
	GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error)
	GetWorkload(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error)
	GetService(ctx context.Context, namespace, name string) (domain.ServiceDetail, error)
	GetIngress(ctx context.Context, namespace, name string) (domain.IngressDetail, error)
	GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (domain.PersistentVolumeClaimDetail, error)
	GetNode(ctx context.Context, name string) (domain.NodeDetail, error)
	ListPodsBySelector(ctx context.Context, namespace string, selector map[string]string, search string) ([]domain.PodSummary, error)
	ListEvents(ctx context.Context, query domain.ListQuery) ([]domain.Event, error)
	StreamPodLogs(ctx context.Context, namespace, name, container string, follow, previous bool, tailLines int64, timestamps bool, sinceSeconds int64, limitBytes int64) (io.ReadCloser, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
	now        func() time.Time
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor, now: time.Now}
}

// Diagnose inspects one supported Kubernetes object and returns deterministic,
// evidence-backed findings. It never mutates the target or related resources.
func (s *Service) Diagnose(ctx context.Context, actor domain.Actor, kind, namespace, name string) (domain.Diagnosis, error) {
	normalized, ok := normalizeKind(kind)
	if !ok {
		return domain.Diagnosis{}, fmt.Errorf("%w: unsupported diagnosis kind %q", domain.ErrUnsupported, kind)
	}
	if normalized != "node" {
		if err := validateIdentity(namespace, name); err != nil {
			return domain.Diagnosis{}, err
		}
	} else if err := validateName(name); err != nil {
		return domain.Diagnosis{}, err
	}
	access := diagnosisAccess(normalized, "get", namespace, name)
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, normalized, namespace, name, err)
		return domain.Diagnosis{}, err
	}

	var report *reportBuilder
	var err error
	switch normalized {
	case "pod":
		report, err = s.diagnosePod(ctx, actor, namespace, name)
	case "deployment", "statefulset", "daemonset", "job", "cronjob":
		report, err = s.diagnoseWorkload(ctx, actor, normalized, namespace, name)
	case "service":
		report, err = s.diagnoseService(ctx, actor, namespace, name)
	case "ingress":
		report, err = s.diagnoseIngress(ctx, actor, namespace, name)
	case "persistentvolumeclaim":
		report, err = s.diagnosePersistentVolumeClaim(ctx, actor, namespace, name)
	case "node":
		report, err = s.diagnoseNode(ctx, actor, name)
	}
	s.audit(ctx, actor, normalized, namespace, name, err)
	if err != nil {
		return domain.Diagnosis{}, err
	}
	return report.finish(s.now()), nil
}

func normalizeKind(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod", "pods":
		return "pod", true
	case "deployment", "deployments":
		return "deployment", true
	case "statefulset", "statefulsets":
		return "statefulset", true
	case "daemonset", "daemonsets":
		return "daemonset", true
	case "job", "jobs":
		return "job", true
	case "cronjob", "cronjobs":
		return "cronjob", true
	case "service", "services":
		return "service", true
	case "ingress", "ingresses":
		return "ingress", true
	case "pvc", "persistentvolumeclaim", "persistentvolumeclaims":
		return "persistentvolumeclaim", true
	case "node", "nodes":
		return "node", true
	default:
		return "", false
	}
}

func validateIdentity(namespace, name string) error {
	if namespace == "" || len(namespace) > 63 || !dnsLabelPattern.MatchString(namespace) {
		return &domain.ValidationError{Field: "namespace", Message: "namespace must be a valid DNS-1123 label"}
	}
	return validateName(name)
}

func validateName(name string) error {
	if name == "" || len(name) > 253 || !dnsSubdomainPattern.MatchString(name) {
		return &domain.ValidationError{Field: "name", Message: "name must be a valid DNS-1123 subdomain"}
	}
	return nil
}

func diagnosisAccess(kind, verb, namespace, name string) domain.Access {
	access := domain.Access{Verb: verb, Namespace: namespace, Name: name}
	switch kind {
	case "pod":
		access.Resource = "pods"
	case "deployment", "statefulset", "daemonset":
		access.APIGroup = "apps"
		access.Resource = kind + "s"
	case "job", "cronjob":
		access.APIGroup = "batch"
		access.Resource = kind + "s"
	case "service":
		access.Resource = "services"
	case "ingress":
		access.APIGroup = "networking.k8s.io"
		access.Resource = "ingresses"
	case "persistentvolumeclaim":
		access.Resource = "persistentvolumeclaims"
	case "node":
		access.Resource = "nodes"
	}
	return access
}

type reportBuilder struct {
	target   domain.DiagnosisTarget
	checks   []domain.DiagnosisCheck
	findings []domain.DiagnosisFinding
	related  []domain.DiagnosisRelated
	warnings []string
}

func newReport(kind, namespace, name, status string) *reportBuilder {
	return &reportBuilder{
		target: domain.DiagnosisTarget{Kind: kind, Namespace: namespace, Name: name, Status: status},
		checks: make([]domain.DiagnosisCheck, 0), findings: make([]domain.DiagnosisFinding, 0),
		related: make([]domain.DiagnosisRelated, 0), warnings: make([]string, 0),
	}
}

func (r *reportBuilder) check(id, label, status, message string) {
	r.checks = append(r.checks, domain.DiagnosisCheck{ID: id, Label: label, Status: status, Message: message})
}

func (r *reportBuilder) finding(finding domain.DiagnosisFinding) {
	if finding.Evidence == nil {
		finding.Evidence = []domain.DiagnosisEvidence{}
	}
	if finding.Recommendations == nil {
		finding.Recommendations = []domain.DiagnosisRecommendation{}
	}
	r.findings = append(r.findings, finding)
}

func (r *reportBuilder) relatedResource(kind, namespace, name, status, relationship, path string) {
	r.related = append(r.related, domain.DiagnosisRelated{
		Kind: kind, Namespace: namespace, Name: name, Status: status, Relationship: relationship, Path: path,
	})
}

func (r *reportBuilder) warn(message string) {
	message = strings.TrimSpace(message)
	if message != "" {
		r.warnings = append(r.warnings, message)
	}
}

func (r *reportBuilder) finish(now time.Time) domain.Diagnosis {
	severity := domain.DiagnosisHealthy
	for _, finding := range r.findings {
		severity = maxSeverity(severity, finding.Severity)
	}
	for _, check := range r.checks {
		if check.Status == domain.DiagnosisCheckCritical {
			severity = maxSeverity(severity, domain.DiagnosisCritical)
		} else if check.Status == domain.DiagnosisCheckWarning {
			severity = maxSeverity(severity, domain.DiagnosisWarning)
		} else if check.Status == domain.DiagnosisCheckInfo {
			severity = maxSeverity(severity, domain.DiagnosisInfo)
		}
	}
	blocks := 0
	attention := 0
	hints := 0
	for _, finding := range r.findings {
		switch finding.Severity {
		case domain.DiagnosisCritical:
			blocks++
		case domain.DiagnosisWarning:
			attention++
		case domain.DiagnosisInfo:
			hints++
		}
	}
	summary := "未发现明显异常"
	switch severity {
	case domain.DiagnosisCritical:
		summary = fmt.Sprintf("发现 %d 个需要立即处理的问题", maxInt(blocks, 1))
	case domain.DiagnosisWarning:
		summary = fmt.Sprintf("发现 %d 个需要关注的问题", maxInt(attention, 1))
	case domain.DiagnosisInfo:
		summary = fmt.Sprintf("未发现阻断性问题，有 %d 条提示", maxInt(hints, 1))
	}
	sort.SliceStable(r.findings, func(i, j int) bool {
		return severityRank(r.findings[i].Severity) < severityRank(r.findings[j].Severity)
	})
	return domain.Diagnosis{
		Target: r.target, Severity: severity, Summary: summary, GeneratedAt: now,
		Checks: r.checks, Findings: r.findings, Related: r.related, Warnings: r.warnings,
	}
}

func maxSeverity(left, right string) string {
	if severityRank(right) < severityRank(left) {
		return right
	}
	return left
}

func severityRank(value string) int {
	switch value {
	case domain.DiagnosisCritical:
		return 0
	case domain.DiagnosisWarning:
		return 1
	case domain.DiagnosisInfo:
		return 2
	default:
		return 3
	}
}

func severityFromCheck(status string) string {
	switch status {
	case domain.DiagnosisCheckCritical:
		return domain.DiagnosisCritical
	case domain.DiagnosisCheckWarning:
		return domain.DiagnosisWarning
	case domain.DiagnosisCheckInfo:
		return domain.DiagnosisInfo
	default:
		return domain.DiagnosisHealthy
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (s *Service) collectEvents(ctx context.Context, actor domain.Actor, namespace, objectKind, objectName, eventsPath string, report *reportBuilder) {
	access := domain.Access{Verb: "list", Resource: "events", Namespace: namespace}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		report.warn("无法读取事件：当前身份缺少 list events 权限")
		report.check("events", "相关事件", domain.DiagnosisCheckSkipped, "未执行事件检查")
		return
	}
	events, err := s.repository.ListEvents(ctx, domain.ListQuery{Namespace: namespace, ObjectKind: objectKind, ObjectName: objectName})
	if err != nil {
		report.warn("读取相关事件失败：" + err.Error())
		report.check("events", "相关事件", domain.DiagnosisCheckSkipped, "事件读取失败")
		return
	}
	warnings := make([]domain.Event, 0)
	for _, event := range events {
		if strings.EqualFold(event.Type, "Warning") {
			warnings = append(warnings, event)
		}
	}
	if len(warnings) == 0 {
		report.check("events", "相关事件", domain.DiagnosisCheckPass, "未发现 Warning 事件")
		return
	}
	severity := domain.DiagnosisWarning
	for _, event := range warnings {
		if criticalEventReason(event.Reason) {
			severity = domain.DiagnosisCritical
			break
		}
	}
	status := domain.DiagnosisCheckWarning
	if severity == domain.DiagnosisCritical {
		status = domain.DiagnosisCheckCritical
	}
	report.check("events", "相关事件", status, fmt.Sprintf("发现 %d 条 Warning 事件", len(warnings)))
	evidence := make([]domain.DiagnosisEvidence, 0, maxDiagnosisEvents)
	for index, event := range warnings {
		if index >= maxDiagnosisEvents {
			break
		}
		evidence = append(evidence, domain.DiagnosisEvidence{
			Label: event.Reason,
			Value: fmt.Sprintf("%s · %s（%d 次）", event.Message, event.LastSeen.Format(time.RFC3339), event.Count),
		})
	}
	recommendations := make([]domain.DiagnosisRecommendation, 0, 2)
	if eventsPath != "" {
		recommendations = append(recommendations, domain.DiagnosisRecommendation{Label: "查看完整事件", Description: "前往对象事件列表查看时间线。", Path: eventsPath})
	}
	command := fmt.Sprintf("kubectl get events --field-selector involvedObject.kind=%s,involvedObject.name=%s", objectKind, objectName)
	if namespace != "" {
		command += " -n " + namespace
	}
	command += " --sort-by=.lastTimestamp"
	recommendations = append(recommendations, domain.DiagnosisRecommendation{Label: "在 kubectl 中查看事件", Command: command})
	report.finding(domain.DiagnosisFinding{
		ID: "warning-events", Severity: severity, Title: "检测到 Warning 事件",
		Cause:    "Kubernetes 控制器或节点在对象生命周期中报告了异常。优先结合下方事件原因与日志判断根因。",
		Evidence: evidence, Recommendations: recommendations,
	})
}

func criticalEventReason(reason string) bool {
	switch reason {
	case "FailedScheduling", "FailedMount", "FailedAttachVolume", "ErrImagePull", "ImagePullBackOff",
		"BackOff", "FailedCreateContainer", "FailedCreate", "OOMKilling", "Evicted":
		return true
	default:
		return false
	}
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, kind, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: "diagnose", Resource: kind, Namespace: namespace, Name: name, Result: result, Err: err,
	})
}

func workloadPath(kind, namespace, name string) string {
	return fmt.Sprintf("/workloads/%s/%s/%s", kind, namespace, name)
}

func podPath(namespace, name string) string {
	return fmt.Sprintf("/pods/%s/%s", namespace, name)
}

func podTabPath(namespace, name, tab string) string {
	return fmt.Sprintf("/pods/%s/%s?tab=%s", namespace, name, tab)
}

func servicePath(namespace, name string) string {
	return fmt.Sprintf("/network/services/%s/%s", namespace, name)
}

func ingressPath(namespace, name string) string {
	return fmt.Sprintf("/network/ingresses/%s/%s", namespace, name)
}

func pvcPath(namespace, name string) string {
	return fmt.Sprintf("/storage/claims/%s/%s", namespace, name)
}

func nodePath(name string) string {
	return fmt.Sprintf("/nodes/%s", name)
}
