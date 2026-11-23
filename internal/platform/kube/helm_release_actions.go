package kube

import (
	"bytes"
	"context"
	"strings"
	"time"

	"dosh/internal/domain"

	"helm.sh/helm/v3/pkg/action"
	helmrelease "helm.sh/helm/v3/pkg/release"
)

const maxHelmTestLogBytes = 128 << 10

// TestHelmRelease runs the release's helm.sh/hook: test hooks through the Helm
// SDK. A failed test is a completed test result, not an infrastructure error,
// so callers can render hook status and logs instead of losing the result.
func (c *Client) TestHelmRelease(ctx context.Context, actor domain.Actor, namespace, name string) (domain.HelmReleaseTestResult, error) {
	started := time.Now().UTC()
	if err := c.validateHelmTarget(ctx, namespace, name); err != nil {
		return domain.HelmReleaseTestResult{}, err
	}
	configuration, err := c.helmConfiguration(actor, namespace)
	if err != nil {
		return domain.HelmReleaseTestResult{}, err
	}
	testing := action.NewReleaseTesting(configuration)
	testing.Namespace = namespace
	testing.Timeout = helmActionTimeout
	tested, testErr := testing.Run(name)
	finished := time.Now().UTC()
	result := domain.HelmReleaseTestResult{
		Namespace: namespace, Name: name, Status: "succeeded",
		StartedAt: started, FinishedAt: finished, Hooks: []domain.HelmReleaseTestHook{},
	}
	if tested == nil {
		return result, mapHelmActionError(testErr)
	}
	result.Revision = tested.Version
	testHooks := 0
	for _, hook := range tested.Hooks {
		if !hookHasEvent(hook, helmrelease.HookTest) {
			continue
		}
		testHooks++
		status := strings.TrimSpace(string(hook.LastRun.Phase))
		if status == "" {
			status = "Unknown"
		}
		result.Hooks = append(result.Hooks, domain.HelmReleaseTestHook{
			Name: hook.Name, Kind: hook.Kind, Path: hook.Path, Status: status,
			Message: hook.Name, StartedAt: hook.LastRun.StartedAt.Time, CompletedAt: hook.LastRun.CompletedAt.Time,
		})
	}
	var logs bytes.Buffer
	if logErr := testing.GetPodLogs(&logs, tested); logErr != nil {
		result.Warnings = append(result.Warnings, "部分测试 Pod 日志无法读取："+truncateHelmMessage(logErr.Error()))
	}
	result.Logs = truncateHelmBytes(logs.String(), maxHelmTestLogBytes)
	if testHooks == 0 {
		result.Warnings = append(result.Warnings, "该 Release 没有声明 helm.sh/hook: test 资源，未执行任何测试")
	}
	if testErr != nil {
		result.Status = "failed"
		result.Message = truncateHelmMessage(testErr.Error())
		return result, nil
	}
	result.Message = "Helm test 通过"
	return result, nil
}

func hookHasEvent(hook *helmrelease.Hook, event helmrelease.HookEvent) bool {
	if hook == nil {
		return false
	}
	for _, candidate := range hook.Events {
		if candidate == event {
			return true
		}
	}
	return false
}

func truncateHelmBytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "\n…日志已截断…"
}
