package app

import (
	"context"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

// SetCronJobSuspend pauses or resumes CronJob scheduling. Existing Jobs are
// left untouched; only future scheduling is affected.
func (s *Service) SetCronJobSuspend(ctx context.Context, actor domain.Actor, kind, namespace, name string, update domain.CronJobSuspendUpdate) error {
	kind = strings.TrimSpace(kind)
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	action := "suspend"
	if !update.Suspend {
		action = "resume"
	}
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, action, kind, namespace, name, access); err != nil {
		return err
	}
	if !strings.EqualFold(kind, "cronjob") {
		err := fmt.Errorf("%w: suspend is only supported for cronjob", domain.ErrUnsupported)
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	update.ResourceVersion = strings.TrimSpace(update.ResourceVersion)
	if err := validateWorkloadIdentity(namespace, name); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	if len(update.ResourceVersion) > 256 {
		err := validationError("resourceVersion", "resourceVersion is too long")
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	err := s.repository.SetCronJobSuspend(ctx, namespace, name, update)
	s.audit(ctx, actor, action, kind, namespace, name, err)
	return err
}
