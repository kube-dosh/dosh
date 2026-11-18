package app

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

var rolloutValuePattern = regexp.MustCompile(`^([0-9]+)(%?)$`)

// UpdateRollout changes supported controller rollout controls. Deployments use
// pause/maxSurge/maxUnavailable, StatefulSets use partition, and DaemonSets use
// maxSurge/maxUnavailable. It requires resource-level update permission and
// records the resulting operation in the audit trail.
func (s *Service) UpdateRollout(ctx context.Context, actor domain.Actor, kind, namespace, name string, update domain.WorkloadRolloutUpdate) error {
	const action = "update-rollout"
	kind = normalizeCreateKind(kind)
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	if kind != "deployment" && kind != "statefulset" && kind != "daemonset" {
		err := fmt.Errorf("%w: rollout controls are not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	update.MaxSurge = strings.TrimSpace(update.MaxSurge)
	update.MaxUnavailable = strings.TrimSpace(update.MaxUnavailable)
	maxSurge, surgeSet, err := normalizeRolloutValue(update.MaxSurge)
	if err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	maxUnavailable, unavailableSet, err := normalizeRolloutValue(update.MaxUnavailable)
	if err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	switch kind {
	case "deployment":
		if update.Partition != nil {
			err := fmt.Errorf("%w: partition is only supported for StatefulSets", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		if update.Paused == nil && !surgeSet && !unavailableSet {
			err := fmt.Errorf("%w: paused, maxSurge or maxUnavailable is required", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		if surgeSet && unavailableSet && isZeroRolloutValue(maxSurge) && isZeroRolloutValue(maxUnavailable) {
			err := fmt.Errorf("%w: maxSurge and maxUnavailable cannot both be zero", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		update.Partition = nil
	case "statefulset":
		if update.Paused != nil || surgeSet || unavailableSet {
			err := fmt.Errorf("%w: StatefulSet rollout control supports partition only", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		if update.Partition == nil {
			err := fmt.Errorf("%w: partition is required for StatefulSet rollout control", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		if *update.Partition < 0 || *update.Partition > 100000 {
			err := fmt.Errorf("%w: partition must be between 0 and 100000", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		update.MaxSurge, update.MaxUnavailable = "", ""
	case "daemonset":
		if update.Paused != nil || update.Partition != nil {
			err := fmt.Errorf("%w: DaemonSet rollout control supports maxSurge and maxUnavailable only", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		if !surgeSet && !unavailableSet {
			err := fmt.Errorf("%w: maxSurge or maxUnavailable is required", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		if surgeSet && unavailableSet && isZeroRolloutValue(maxSurge) && isZeroRolloutValue(maxUnavailable) {
			err := fmt.Errorf("%w: maxSurge and maxUnavailable cannot both be zero", domain.ErrInvalid)
			s.audit(ctx, actor, action, kind, namespace, name, err)
			return err
		}
		update.MaxSurge, update.MaxUnavailable = maxSurge, maxUnavailable
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, action, kind, namespace, name, access); err != nil {
		return err
	}
	err = s.repository.UpdateWorkloadRollout(ctx, kind, namespace, name, update)
	s.audit(ctx, actor, action, kind, namespace, name, err)
	return err
}

func isZeroRolloutValue(value string) bool {
	return value == "0" || value == "0%"
}

func normalizeRolloutValue(value string) (string, bool, error) {
	if value == "" {
		return "", false, nil
	}
	matches := rolloutValuePattern.FindStringSubmatch(value)
	if matches == nil {
		return "", false, fmt.Errorf("%w: rollout value must be an integer or percentage", domain.ErrInvalid)
	}
	number, err := strconv.Atoi(matches[1])
	if err != nil || number < 0 || (matches[2] == "%" && number > 100) {
		return "", false, fmt.Errorf("%w: rollout value is out of range", domain.ErrInvalid)
	}
	return value, true, nil
}
