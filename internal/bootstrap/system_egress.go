package bootstrap

import (
	"context"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"
)

// systemEgressRuntime adapts the platform egress controller to the
// domain-only interface consumed by the system module. The application layer
// must not depend on transport or runtime implementation packages.
type systemEgressRuntime struct {
	controller *egress.Controller
}

func newSystemEgressRuntime(controller *egress.Controller) systemEgressRuntime {
	return systemEgressRuntime{controller: controller}
}

func (r systemEgressRuntime) Status() domain.EgressStatus {
	if r.controller == nil {
		return domain.EgressStatus{Mode: "direct", Source: egress.SourceBootstrap}
	}
	return egressStatus(r.controller.Status())
}

func (r systemEgressRuntime) View() domain.EgressSettings {
	if r.controller == nil {
		return domain.EgressSettings{Mode: "bootstrap", EffectiveMode: "direct", Source: egress.SourceBootstrap}
	}
	return egressSettings(r.controller.View())
}

func (r systemEgressRuntime) Save(ctx context.Context, input domain.EgressSettingsUpdate) (domain.EgressSettings, error) {
	if r.controller == nil {
		return domain.EgressSettings{}, domain.ErrUnsupported
	}
	saved, err := r.controller.Save(ctx, egressUpdateInput(input))
	if err != nil {
		return domain.EgressSettings{}, err
	}
	return egressSettings(saved), nil
}

func (r systemEgressRuntime) Preflight(ctx context.Context, input domain.EgressSettingsUpdate) (domain.EgressPreflight, error) {
	if r.controller == nil {
		return domain.EgressPreflight{}, domain.ErrUnsupported
	}
	result, err := r.controller.Preflight(ctx, egressUpdateInput(input))
	if err != nil {
		return domain.EgressPreflight{}, err
	}
	return egressPreflight(result), nil
}

func egressStatus(status egress.Status) domain.EgressStatus {
	return domain.EgressStatus{
		Mode: status.Mode, Enabled: status.Enabled, HTTPProxy: status.HTTPProxy, HTTPSProxy: status.HTTPSProxy,
		NoProxy: status.NoProxy, CredentialsConfigured: status.CredentialsConfigured, CAConfigured: status.CAConfigured,
		Source: status.Source,
	}
}

func egressSettings(view egress.View) domain.EgressSettings {
	updatedAt := ""
	if !view.UpdatedAt.IsZero() {
		updatedAt = view.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return domain.EgressSettings{
		Mode: view.Mode, EffectiveMode: view.EffectiveMode, Enabled: view.Enabled,
		HTTPProxy: view.HTTPProxy, HTTPSProxy: view.HTTPSProxy, NoProxy: view.NoProxy, Username: view.Username,
		CredentialsConfigured: view.CredentialsConfigured, CAConfigured: view.CAConfigured,
		Source: view.Source, Revision: view.Revision, UpdatedAt: updatedAt,
		NetworkPolicy: egressNetworkPolicyStatus(view.NetworkPolicy),
	}
}

func egressPreflight(result egress.Preflight) domain.EgressPreflight {
	view := domain.EgressPreflight{
		Status: result.Status, Message: result.Message,
		RequiredIPBlocks: append([]string(nil), result.RequiredIPBlocks...),
		RequiredPorts:    append([]int(nil), result.RequiredPorts...),
		NetworkPolicy:    egressNetworkPolicyStatus(result.NetworkPolicy),
		Targets:          make([]domain.EgressProxyTarget, 0, len(result.Targets)),
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

func egressNetworkPolicyStatus(status egress.NetworkPolicyStatus) domain.EgressNetworkPolicyStatus {
	return domain.EgressNetworkPolicyStatus{
		Enabled: status.Enabled, Mode: status.Mode, AutoManage: status.AutoManage,
		CanAutoManage: status.CanAutoManage, PolicyName: status.PolicyName,
		HelmPolicyName: status.HelmPolicyName, Message: status.Message,
	}
}

func egressUpdateInput(input domain.EgressSettingsUpdate) egress.UpdateInput {
	return egress.UpdateInput{
		Mode: input.Mode, HTTPProxy: input.HTTPProxy, HTTPSProxy: input.HTTPSProxy, NoProxy: input.NoProxy,
		Username: input.Username, Password: input.Password, ClearCredentials: input.ClearCredentials,
		CAPEM: input.CAPEM, ClearCA: input.ClearCA, Revision: input.Revision,
	}
}
