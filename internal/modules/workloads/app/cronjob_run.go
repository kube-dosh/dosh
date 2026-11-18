package app

import (
	"context"
	"fmt"
	"strings"

	"dosh/internal/domain"
)

// RunCronJobOnce starts a one-off Job from the CronJob template. It requires
// get on the CronJob and create on Jobs, matching the effective privilege of
// `kubectl create job --from=cronjob/<name>`.
func (s *Service) RunCronJobOnce(ctx context.Context, actor domain.Actor, namespace, name string) (domain.WorkloadSummary, error) {
	const action = "run-cronjob"
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if err := validateDNSLabel("namespace", namespace); err != nil {
		s.audit(ctx, actor, action, "cronjob", namespace, name, err)
		return domain.WorkloadSummary{}, err
	}
	if err := validateDNSLabel("name", name); err != nil {
		s.audit(ctx, actor, action, "cronjob", namespace, name, err)
		return domain.WorkloadSummary{}, err
	}
	if err := s.authorizer.Require(ctx, actor, workloadAccess("cronjob", "get", namespace, name, "")); err != nil {
		s.audit(ctx, actor, action, "cronjob", namespace, name, err)
		return domain.WorkloadSummary{}, err
	}
	if err := s.authorizer.Require(ctx, actor, workloadAccess("job", "create", namespace, "", "")); err != nil {
		s.audit(ctx, actor, action, "cronjob", namespace, name, err)
		return domain.WorkloadSummary{}, err
	}
	summary, err := s.repository.RunCronJobOnce(ctx, namespace, name)
	if err != nil {
		s.audit(ctx, actor, action, "cronjob", namespace, name, fmt.Errorf("create job from cronjob: %w", err))
		return domain.WorkloadSummary{}, err
	}
	s.audit(ctx, actor, action, "cronjob", namespace, name, nil)
	return summary, nil
}
