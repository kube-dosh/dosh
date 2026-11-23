package app

import (
	"context"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"
)

type UpdateChecker interface {
	Status(ctx context.Context, force bool) domain.UpdateStatus
}

type EgressRuntime interface {
	Status() egress.Status
	View() egress.View
	Save(ctx context.Context, input egress.UpdateInput) (egress.View, error)
	Preflight(ctx context.Context, input egress.UpdateInput) (egress.Preflight, error)
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
		status := s.egress.Status()
		info.Egress = domain.EgressStatus{
			Mode: status.Mode, Enabled: status.Enabled, HTTPProxy: status.HTTPProxy, HTTPSProxy: status.HTTPSProxy,
			NoProxy: status.NoProxy, CredentialsConfigured: status.CredentialsConfigured, CAConfigured: status.CAConfigured,
			Source: status.Source,
		}
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
	return egressSettingsView(s.egress.View()), nil
}

func (s *Service) SaveEgress(ctx context.Context, actor domain.Actor, input domain.EgressSettingsUpdate) (domain.EgressSettings, error) {
	if !actor.Admin {
		return domain.EgressSettings{}, domain.ErrForbidden
	}
	if s.egress == nil {
		return domain.EgressSettings{}, domain.ErrUnsupported
	}
	saved, err := s.egress.Save(ctx, egress.UpdateInput{
		Mode: input.Mode, HTTPProxy: input.HTTPProxy, HTTPSProxy: input.HTTPSProxy, NoProxy: input.NoProxy,
		Username: input.Username, Password: input.Password, ClearCredentials: input.ClearCredentials,
		CAPEM: input.CAPEM, ClearCA: input.ClearCA, Revision: input.Revision,
	})
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
	return egressSettingsView(saved), nil
}

func (s *Service) PreflightEgress(ctx context.Context, actor domain.Actor, input domain.EgressSettingsUpdate) (domain.EgressPreflight, error) {
	if !actor.Admin {
		return domain.EgressPreflight{}, domain.ErrForbidden
	}
	if s.egress == nil {
		return domain.EgressPreflight{}, domain.ErrUnsupported
	}
	result, err := s.egress.Preflight(ctx, egress.UpdateInput{
		Mode: input.Mode, HTTPProxy: input.HTTPProxy, HTTPSProxy: input.HTTPSProxy,
		NoProxy: input.NoProxy, Username: input.Username, ClearCredentials: input.ClearCredentials,
		ClearCA: input.ClearCA, Revision: input.Revision,
	})
	if err != nil {
		return domain.EgressPreflight{}, err
	}
	return egressPreflightView(result), nil
}

func egressSettingsView(view egress.View) domain.EgressSettings {
	updatedAt := ""
	if !view.UpdatedAt.IsZero() {
		updatedAt = view.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return domain.EgressSettings{
		Mode: view.Mode, EffectiveMode: view.EffectiveMode, Enabled: view.Enabled,
		HTTPProxy: view.HTTPProxy, HTTPSProxy: view.HTTPSProxy, NoProxy: view.NoProxy, Username: view.Username,
		CredentialsConfigured: view.CredentialsConfigured, CAConfigured: view.CAConfigured,
		Source: view.Source, Revision: view.Revision, UpdatedAt: updatedAt,
		NetworkPolicy: domain.EgressNetworkPolicyStatus{
			Enabled: view.NetworkPolicy.Enabled, Mode: view.NetworkPolicy.Mode,
			AutoManage: view.NetworkPolicy.AutoManage, CanAutoManage: view.NetworkPolicy.CanAutoManage,
			PolicyName: view.NetworkPolicy.PolicyName, HelmPolicyName: view.NetworkPolicy.HelmPolicyName,
			Message: view.NetworkPolicy.Message,
		},
	}
}

func egressPreflightView(result egress.Preflight) domain.EgressPreflight {
	view := domain.EgressPreflight{
		Status: result.Status, Message: result.Message, RequiredIPBlocks: append([]string(nil), result.RequiredIPBlocks...),
		RequiredPorts: append([]int(nil), result.RequiredPorts...),
		NetworkPolicy: domain.EgressNetworkPolicyStatus{
			Enabled: result.NetworkPolicy.Enabled, Mode: result.NetworkPolicy.Mode,
			AutoManage: result.NetworkPolicy.AutoManage, CanAutoManage: result.NetworkPolicy.CanAutoManage,
			PolicyName: result.NetworkPolicy.PolicyName, HelmPolicyName: result.NetworkPolicy.HelmPolicyName,
			Message: result.NetworkPolicy.Message,
		},
		Targets: make([]domain.EgressProxyTarget, 0, len(result.Targets)),
	}
	for _, target := range result.Targets {
		view.Targets = append(view.Targets, domain.EgressProxyTarget{
			Kind: target.Kind, URL: target.URL, Host: target.Host, Port: target.Port, Hostname: target.Hostname,
			Addresses: append([]string(nil), target.Addresses...), CIDRs: append([]string(nil), target.CIDRs...),
			Reachable: target.Reachable, LatencyMS: target.LatencyMS, Error: target.Error,
			Allowed: target.Allowed, PolicySource: target.PolicySource,
		})
	}
	return view
}
