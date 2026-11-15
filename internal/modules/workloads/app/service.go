package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"dosh/internal/domain"
)

const maxNonStreamLogBytes int64 = 1 << 20

type Repository interface {
	ListWorkloads(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error)
	ListWorkloadsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.WorkloadSummary], error)
	GetWorkload(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error)
	CreateDeployment(ctx context.Context, spec domain.DeploymentCreateSpec, dryRun bool) (domain.WorkloadSummary, error)
	PreviewDeployment(ctx context.Context, spec domain.DeploymentCreateSpec) (string, error)
	ScaleWorkload(ctx context.Context, kind, namespace, name string, replicas int32, expectedResourceVersion string) error
	RestartWorkload(ctx context.Context, kind, namespace, name string) error
	UpdateWorkloadImage(ctx context.Context, kind, namespace, name string, update domain.WorkloadImageUpdate) error
	UpdateWorkloadContainerConfig(ctx context.Context, kind, namespace, name string, update domain.WorkloadContainerConfigUpdate) error
	SetCronJobSuspend(ctx context.Context, namespace, name string, update domain.CronJobSuspendUpdate) error
	ListWorkloadRevisions(ctx context.Context, kind, namespace, name string) ([]domain.WorkloadRevision, error)
	RollbackWorkload(ctx context.Context, kind, namespace, name string, revision int64) error
	DeleteWorkload(ctx context.Context, kind, namespace, name string, propagation domain.DeletionPropagation) error
	ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error)
	ListPodsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PodSummary], error)
	GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error)
	DeletePod(ctx context.Context, namespace, name string) error
	StreamPodLogs(ctx context.Context, namespace, name, container string, follow, previous bool, tailLines int64, timestamps bool, sinceSeconds int64, limitBytes int64) (io.ReadCloser, error)
	OpenPodTerminal(ctx context.Context, namespace, name string, options domain.PodTerminalOptions) (domain.PodTerminalSession, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor) *Service {
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

var supportedKinds = map[string]bool{
	"deployment": true, "statefulset": true, "daemonset": true, "job": true, "cronjob": true,
}

func ValidateKind(kind string) error {
	if !supportedKinds[strings.ToLower(kind)] {
		return fmt.Errorf("%w: workload kind %q", domain.ErrUnsupported, kind)
	}
	return nil
}

func (s *Service) List(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error) {
	if query.Kind != "" {
		if err := ValidateKind(query.Kind); err != nil {
			return nil, err
		}
	}
	return s.repository.ListWorkloads(ctx, query)
}

func (s *Service) ListPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.WorkloadSummary], error) {
	if query.Kind != "" {
		if err := ValidateKind(query.Kind); err != nil {
			return domain.PageResult[domain.WorkloadSummary]{}, err
		}
	}
	return s.repository.ListWorkloadsPage(ctx, query, page)
}

func (s *Service) Get(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error) {
	if err := ValidateKind(kind); err != nil {
		return domain.WorkloadDetail{}, err
	}
	return s.repository.GetWorkload(ctx, kind, namespace, name)
}

func (s *Service) Scale(ctx context.Context, actor domain.Actor, kind, namespace, name string, replicas int32, resourceVersion string) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "scale", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "scale")
	if err := s.authorize(ctx, actor, "scale", kind, namespace, name, access); err != nil {
		return err
	}
	if strings.EqualFold(kind, "daemonset") || strings.EqualFold(kind, "job") || strings.EqualFold(kind, "cronjob") {
		err := fmt.Errorf("%w: scale is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "scale", kind, namespace, name, err)
		return err
	}
	err := s.repository.ScaleWorkload(ctx, kind, namespace, name, replicas, resourceVersion)
	s.audit(ctx, actor, "scale", kind, namespace, name, err)
	return err
}

func (s *Service) Restart(ctx context.Context, actor domain.Actor, kind, namespace, name string) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "restart", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "patch", namespace, name, "")
	if err := s.authorize(ctx, actor, "restart", kind, namespace, name, access); err != nil {
		return err
	}
	if strings.EqualFold(kind, "job") || strings.EqualFold(kind, "cronjob") {
		err := fmt.Errorf("%w: restart is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "restart", kind, namespace, name, err)
		return err
	}
	err := s.repository.RestartWorkload(ctx, kind, namespace, name)
	s.audit(ctx, actor, "restart", kind, namespace, name, err)
	return err
}

func (s *Service) Delete(ctx context.Context, actor domain.Actor, kind, namespace, name, propagation string) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "delete", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "delete", namespace, name, "")
	if err := s.authorize(ctx, actor, "delete", kind, namespace, name, access); err != nil {
		return err
	}
	policy := domain.DeletePropagationBackground
	switch propagation {
	case "", "Background":
	case "Foreground":
		policy = domain.DeletePropagationForeground
	case "Orphan":
		policy = domain.DeletePropagationOrphan
	default:
		err := fmt.Errorf("%w: unsupported deletion propagation %q", domain.ErrInvalid, propagation)
		s.audit(ctx, actor, "delete", kind, namespace, name, err)
		return err
	}
	err := s.repository.DeleteWorkload(ctx, kind, namespace, name, policy)
	s.audit(ctx, actor, "delete", kind, namespace, name, err)
	return err
}

func (s *Service) Revisions(ctx context.Context, kind, namespace, name string) ([]domain.WorkloadRevision, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	if !strings.EqualFold(kind, "deployment") && !strings.EqualFold(kind, "statefulset") {
		return nil, fmt.Errorf("%w: revisions are not supported for %s", domain.ErrUnsupported, kind)
	}
	return s.repository.ListWorkloadRevisions(ctx, kind, namespace, name)
}

func (s *Service) Rollback(ctx context.Context, actor domain.Actor, kind, namespace, name string, revision int64) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "rollback", kind, namespace, name, err)
		return err
	}
	if !strings.EqualFold(kind, "deployment") && !strings.EqualFold(kind, "statefulset") {
		err := fmt.Errorf("%w: rollback is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "rollback", kind, namespace, name, err)
		return err
	}
	if revision < 1 {
		err := fmt.Errorf("%w: revision must be positive", domain.ErrInvalid)
		s.audit(ctx, actor, "rollback", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, "rollback", kind, namespace, name, access); err != nil {
		return err
	}
	err := s.repository.RollbackWorkload(ctx, kind, namespace, name, revision)
	s.audit(ctx, actor, "rollback", kind, namespace, name, err)
	return err
}

func (s *Service) ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error) {
	return s.repository.ListPods(ctx, query)
}

func (s *Service) ListPodsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PodSummary], error) {
	return s.repository.ListPodsPage(ctx, query, page)
}

func (s *Service) GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error) {
	return s.repository.GetPod(ctx, namespace, name)
}

func (s *Service) DeletePod(ctx context.Context, actor domain.Actor, namespace, name string) error {
	access := domain.Access{Verb: "delete", Resource: "pods", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, "delete-pod", "pod", namespace, name, err)
		return err
	}
	err := s.repository.DeletePod(ctx, namespace, name)
	s.audit(ctx, actor, "delete-pod", "pod", namespace, name, err)
	return err
}

func (s *Service) StreamLogs(ctx context.Context, actor domain.Actor, namespace, name, container string, follow, previous bool, tailLines int64, timestamps bool, sinceSeconds int64) (io.ReadCloser, error) {
	if tailLines < 0 {
		return nil, fmt.Errorf("%w: tailLines must not be negative", domain.ErrInvalid)
	}
	if tailLines > 5000 {
		tailLines = 5000
	}
	if sinceSeconds < 0 {
		return nil, fmt.Errorf("%w: sinceSeconds must not be negative", domain.ErrInvalid)
	}
	if sinceSeconds > 7*24*60*60 {
		sinceSeconds = 7 * 24 * 60 * 60
	}
	limitBytes := int64(0)
	if !follow {
		limitBytes = maxNonStreamLogBytes
	}
	access := domain.Access{Verb: "get", Resource: "pods", Subresource: "log", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, "stream-pod-logs", "pod", namespace, name, err)
		return nil, err
	}
	stream, err := s.repository.StreamPodLogs(ctx, namespace, name, container, follow, previous, tailLines, timestamps, sinceSeconds, limitBytes)
	s.audit(ctx, actor, "stream-pod-logs", "pod", namespace, name, err)
	return stream, err
}

// OpenTerminal authorizes pods/exec before opening the SPDY stream. The
// returned wrapper records both successful and failed interactive sessions.
func (s *Service) OpenTerminal(ctx context.Context, actor domain.Actor, namespace, name string, options domain.PodTerminalOptions) (domain.PodTerminalSession, error) {
	if namespace == "" || name == "" || options.Container == "" {
		return nil, fmt.Errorf("%w: namespace, pod and container are required", domain.ErrInvalid)
	}
	if len(options.Command) == 0 {
		options.Command = []string{"/bin/sh"}
	}
	if !supportedShell(options.Command) {
		return nil, fmt.Errorf("%w: terminal shell is not allow-listed", domain.ErrInvalid)
	}
	if options.Size.Columns < 20 || options.Size.Columns > 300 || options.Size.Rows < 5 || options.Size.Rows > 100 {
		return nil, fmt.Errorf("%w: terminal size is out of range", domain.ErrInvalid)
	}
	access := domain.Access{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, "exec", "pod", namespace, name, err)
		return nil, err
	}
	session, err := s.repository.OpenPodTerminal(ctx, namespace, name, options)
	if err != nil {
		s.audit(ctx, actor, "exec", "pod", namespace, name, err)
		return nil, err
	}
	s.audit(ctx, actor, "exec", "pod", namespace, name, nil)
	return &auditedTerminalSession{
		PodTerminalSession: session, service: s, ctx: ctx, actor: actor,
		namespace: namespace, name: name,
	}, nil
}

func supportedShell(command []string) bool {
	if len(command) != 1 {
		return false
	}
	switch command[0] {
	case "/bin/sh", "/bin/bash", "/bin/ash", "/usr/bin/sh", "/usr/bin/bash", "/usr/bin/ash", "sh", "bash", "ash":
		return true
	default:
		return false
	}
}

type auditedTerminalSession struct {
	domain.PodTerminalSession
	service   *Service
	ctx       context.Context
	actor     domain.Actor
	namespace string
	name      string
	closeOnce sync.Once
}

func (s *auditedTerminalSession) Close() error {
	err := s.PodTerminalSession.Close()
	s.closeOnce.Do(func() {
		s.service.audit(s.ctx, s.actor, "exec-close", "pod", s.namespace, s.name, err)
	})
	return err
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action, resource, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: resource, Namespace: namespace, Name: name, Result: result, Err: err})
}

func (s *Service) authorize(ctx context.Context, actor domain.Actor, action, kind, namespace, name string, access domain.Access) error {
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	return nil
}

func workloadAccess(kind, verb, namespace, name, subresource string) domain.Access {
	normalized := strings.ToLower(kind)
	access := domain.Access{
		Verb: verb, Namespace: namespace, Name: name, Subresource: subresource,
	}
	switch normalized {
	case "deployment", "statefulset", "daemonset":
		access.APIGroup = "apps"
		access.Resource = normalized + "s"
	case "job", "cronjob":
		access.APIGroup = "batch"
		access.Resource = normalized + "s"
	default:
		access.Resource = normalized
	}
	return access
}
