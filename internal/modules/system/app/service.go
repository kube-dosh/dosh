package app

import (
	"context"

	"dosh/internal/domain"
)

type UpdateChecker interface {
	Status(ctx context.Context, force bool) domain.UpdateStatus
}

type EgressRuntime interface {
	Status() domain.EgressStatus
	View() domain.EgressSettings
	Save(ctx context.Context, input domain.EgressSettingsUpdate) (domain.EgressSettings, error)
	Preflight(ctx context.Context, input domain.EgressSettingsUpdate) (domain.EgressPreflight, error)
}

type Service struct {
	info    domain.SystemInfo
	checker UpdateChecker
	egress  EgressRuntime
	auditor domain.Auditor
}

func New(info domain.SystemInfo, checker UpdateChecker, egressRuntime EgressRuntime, auditor domain.Auditor) *Service {
	return &Service{info: info, checker: checker, egress: egressRuntime, auditor: auditor}
}

func (s *Service) Info(ctx context.Context) (domain.SystemInfo, error) {
	if err := ctx.Err(); err != nil {
		return domain.SystemInfo{}, err
	}
	info := s.info
	if s.egress != nil {
		info.Egress = s.egress.Status()
	}
	return info, nil
}

func (s *Service) Update(ctx context.Context, force bool) (domain.UpdateStatus, error) {
	if err := ctx.Err(); err != nil {
		return domain.UpdateStatus{}, err
	}
	return s.checker.Status(ctx, force), nil
}

func (s *Service) Egress(ctx context.Context) (domain.EgressSettings, error) {
	if err := ctx.Err(); err != nil {
		return domain.EgressSettings{}, err
	}
	if s.egress == nil {
		return domain.EgressSettings{}, domain.ErrUnsupported
	}
	return s.egress.View(), nil
}

func (s *Service) SaveEgress(ctx context.Context, actor domain.Actor, input domain.EgressSettingsUpdate) (domain.EgressSettings, error) {
	if !actor.Admin {
		return domain.EgressSettings{}, domain.ErrForbidden
	}
	if s.egress == nil {
		return domain.EgressSettings{}, domain.ErrUnsupported
	}
	saved, err := s.egress.Save(ctx, input)
	if s.auditor != nil {
		event := domain.AuditEvent{Action: "update", Resource: "system-egress", Result: "success"}
		if err != nil {
			event.Result = "error"
			event.Err = err
		} else {
			event.Details = map[string]any{"mode": saved.Mode, "source": saved.Source}
		}
		s.auditor.Record(ctx, actor, event)
	}
	if err != nil {
		return domain.EgressSettings{}, err
	}
	return saved, nil
}

func (s *Service) PreflightEgress(ctx context.Context, actor domain.Actor, input domain.EgressSettingsUpdate) (domain.EgressPreflight, error) {
	if !actor.Admin {
		return domain.EgressPreflight{}, domain.ErrForbidden
	}
	if s.egress == nil {
		return domain.EgressPreflight{}, domain.ErrUnsupported
	}
	result, err := s.egress.Preflight(ctx, input)
	if err != nil {
		return domain.EgressPreflight{}, err
	}
	return result, nil
}
