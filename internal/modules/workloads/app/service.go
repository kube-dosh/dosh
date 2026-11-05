package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"dosh/internal/domain"
)

const maxNonStreamLogBytes int64 = 1 << 20

type Repository interface {
	ListWorkloads(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error)
	GetWorkload(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error)
	CreateDeployment(ctx context.Context, spec domain.DeploymentCreateSpec, dryRun bool) (domain.WorkloadSummary, error)
	PreviewDeployment(ctx context.Context, spec domain.DeploymentCreateSpec) (string, error)
	ScaleWorkload(ctx context.Context, kind, namespace, name string, replicas int32, expectedResourceVersion string) error
	RestartWorkload(ctx context.Context, kind, namespace, name string) error
	UpdateWorkloadImage(ctx context.Context, kind, namespace, name string, update domain.WorkloadImageUpdate) error
	SetCronJobSuspend(ctx context.Context, namespace, name string, update domain.CronJobSuspendUpdate) error
	DeleteWorkload(ctx context.Context, kind, namespace, name string, propagation domain.DeletionPropagation) error
	ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error)
	GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error)
	StreamPodLogs(ctx context.Context, namespace, name, container string, follow bool, tailLines int64, timestamps bool, limitBytes int64) (io.ReadCloser, error)
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

func (s *Service) ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error) {
	return s.repository.ListPods(ctx, query)
}

func (s *Service) GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error) {
	return s.repository.GetPod(ctx, namespace, name)
}

func (s *Service) StreamLogs(ctx context.Context, namespace, name, container string, follow bool, tailLines int64, timestamps bool) (io.ReadCloser, error) {
	if tailLines < 0 {
		return nil, fmt.Errorf("%w: tailLines must not be negative", domain.ErrInvalid)
	}
	if tailLines > 5000 {
		tailLines = 5000
	}
	limitBytes := int64(0)
	if !follow {
		limitBytes = maxNonStreamLogBytes
	}
	return s.repository.StreamPodLogs(ctx, namespace, name, container, follow, tailLines, timestamps, limitBytes)
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
