package egress

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"
)

const proxyProbeTimeout = 2 * time.Second

// Preflight validates proposed egress settings without changing the effective
// transport. The administrator must still persist the settings after the
// result has been reviewed.
func (c *Controller) Preflight(ctx context.Context, input UpdateInput) (Preflight, error) {
	if c == nil || c.factory == nil {
		return Preflight{}, fmt.Errorf("%w: runtime egress settings are unavailable", domain.ErrUnsupported)
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	settings := Settings{Mode: mode}
	if mode == "explicit" {
		settings.HTTPProxy = strings.TrimSpace(input.HTTPProxy)
		settings.HTTPSProxy = strings.TrimSpace(input.HTTPSProxy)
		settings.NoProxy = strings.TrimSpace(input.NoProxy)
	}
	if mode != "bootstrap" && mode != "direct" && mode != "explicit" {
		return Preflight{}, fmt.Errorf("%w: mode must be bootstrap, explicit, or direct", domain.ErrInvalid)
	}
	if mode == "explicit" {
		if _, err := buildState(settings, SourceRuntime, 0); err != nil {
			return Preflight{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
	}
	result, _, err := c.preflightSettings(ctx, settings)
	return result, err
}

func (c *Controller) preflightSettings(ctx context.Context, settings Settings) (Preflight, []ProxyTarget, error) {
	result := Preflight{
		Status: "ok", NetworkPolicy: c.networkPolicyStatus(), Targets: []ProxyTarget{},
		RequiredIPBlocks: []string{}, RequiredPorts: []int{},
	}
	targets, err := resolveProxyTargets(ctx, settings)
	if err != nil {
		return Preflight{}, nil, err
	}
	if len(targets) == 0 {
		result.Message = "当前模式不需要代理出口放行。"
		if result.NetworkPolicy.Mode == NetworkPolicyModeRuntime {
			result.Message += " 保存后将删除 Dosh 自动维护的代理 NetworkPolicy。"
		}
		return result, targets, nil
	}

	if c != nil && c.policy != nil {
		checked, checkErr := c.policy.CheckTargets(ctx, targets)
		if checkErr != nil {
			result.Status = "error"
			result.Message = "无法读取当前 NetworkPolicy：" + checkErr.Error()
			result.Targets = targets
			return result, targets, nil
		}
		targets = checked
	} else {
		for index := range targets {
			targets[index].Allowed = true
			targets[index].PolicySource = "unmanaged"
		}
	}

	for index := range targets {
		probeProxyTarget(ctx, &targets[index])
	}
	result.Targets = targets
	result.RequiredIPBlocks, result.RequiredPorts = targetRequirements(targets)

	blocked := false
	unreachable := false
	hostname := false
	for _, target := range targets {
		blocked = blocked || !target.Allowed
		unreachable = unreachable || !target.Reachable
		hostname = hostname || target.Hostname
	}
	switch {
	case blocked && result.NetworkPolicy.Mode == NetworkPolicyModeHelm:
		result.Status = "error"
		result.Message = "Helm 管理的 NetworkPolicy 尚未放行全部代理目标。保存前请将 requiredIpBlocks 和 requiredPorts 写入 networkPolicy.outboundProxy。"
	case blocked:
		result.Status = "error"
		result.Message = "当前 NetworkPolicy 管理方式无法自动放行代理目标。"
	case result.NetworkPolicy.Mode == NetworkPolicyModeRuntime:
		result.Message = "Dosh 将按解析出的精确 IP 和端口自动维护专用 NetworkPolicy；不会放行 0.0.0.0/0 或 ::/0。"
		if unreachable {
			result.Status = "warning"
			result.Message += " 当前 TCP 探测尚不可达，保存后策略生效时请再次验证。"
		}
	case unreachable:
		result.Status = "warning"
		result.Message = "代理目标已获得 NetworkPolicy 放行，但当前 TCP 连接不可达。请检查代理进程、路由和端口。"
	default:
		result.Message = "代理目标解析、NetworkPolicy 放行状态和 TCP 连通性检查通过。"
	}
	if hostname && result.Status != "error" {
		if result.Status == "ok" {
			result.Status = "warning"
		}
		result.Message += " 代理使用域名，DNS 解析结果变化后需要重新保存配置以刷新精确 IP 放行。"
	}
	return result, targets, nil
}

func resolveProxyTargets(ctx context.Context, settings Settings) ([]ProxyTarget, error) {
	mode := strings.ToLower(strings.TrimSpace(settings.Mode))
	if mode == "bootstrap" || mode == "direct" {
		return nil, nil
	}
	httpProxy, httpsProxy, err := proxyFallback(settings.HTTPProxy, settings.HTTPSProxy, mode == "explicit")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	targets := make([]ProxyTarget, 0, 2)
	for _, entry := range []struct {
		kind string
		raw  string
	}{{kind: "http", raw: httpProxy}, {kind: "https", raw: httpsProxy}} {
		if strings.TrimSpace(entry.raw) == "" {
			continue
		}
		target, err := resolveProxyTarget(ctx, entry.kind, entry.raw)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return dedupeTargets(targets), nil
}

func resolveProxyTarget(ctx context.Context, kind, raw string) (ProxyTarget, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ProxyTarget{}, fmt.Errorf("%w: egress proxy must be an absolute http or https URL", domain.ErrInvalid)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ProxyTarget{}, fmt.Errorf("%w: egress proxy URL must not contain user information, a query, or a fragment", domain.ErrInvalid)
	}
	port := 0
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return ProxyTarget{}, fmt.Errorf("%w: egress proxy port must be between 1 and 65535", domain.ErrInvalid)
		}
		port = parsedPort
	} else if parsed.Scheme == "https" {
		port = 443
	} else {
		port = 80
	}
	host := parsed.Hostname()
	if host == "" {
		return ProxyTarget{}, fmt.Errorf("%w: egress proxy host is required", domain.ErrInvalid)
	}
	target := ProxyTarget{Kind: kind, URL: redactURL(raw), Host: host, Port: port}
	addresses := []string{}
	if ip := net.ParseIP(host); ip != nil {
		addresses = append(addresses, ip.String())
	} else {
		target.Hostname = true
		lookupCtx, cancel := context.WithTimeout(ctx, proxyProbeTimeout)
		defer cancel()
		resolved, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
		if err != nil {
			target.Error = "DNS 解析失败：" + err.Error()
			return target, nil
		}
		for _, address := range resolved {
			if ip := address.IP; ip != nil {
				addresses = append(addresses, ip.String())
			}
		}
	}
	addresses = uniqueStrings(addresses)
	if len(addresses) == 0 {
		target.Error = "DNS 解析未返回可用 IP"
		return target, nil
	}
	target.Addresses = addresses
	for _, address := range addresses {
		if strings.Contains(address, ":") {
			target.CIDRs = append(target.CIDRs, address+"/128")
		} else {
			target.CIDRs = append(target.CIDRs, address+"/32")
		}
	}
	target.CIDRs = uniqueStrings(target.CIDRs)
	return target, nil
}

func probeProxyTarget(ctx context.Context, target *ProxyTarget) {
	if target == nil || target.Error != "" || len(target.Addresses) == 0 {
		return
	}
	started := time.Now()
	for _, address := range target.Addresses {
		dialCtx, cancel := context.WithTimeout(ctx, proxyProbeTimeout)
		connection, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(address, strconv.Itoa(target.Port)))
		cancel()
		if err == nil {
			_ = connection.Close()
			target.Reachable = true
			target.Error = ""
			target.LatencyMS = time.Since(started).Milliseconds()
			return
		}
		target.Error = "TCP 连接失败：" + err.Error()
	}
}

func targetRequirements(targets []ProxyTarget) ([]string, []int) {
	cidrs := []string{}
	ports := []int{}
	for _, target := range targets {
		cidrs = append(cidrs, target.CIDRs...)
		ports = append(ports, target.Port)
	}
	cidrs = uniqueStrings(cidrs)
	sort.Strings(cidrs)
	ports = uniqueInts(ports)
	sort.Ints(ports)
	return cidrs, ports
}

func dedupeTargets(targets []ProxyTarget) []ProxyTarget {
	seen := map[string]struct{}{}
	result := make([]ProxyTarget, 0, len(targets))
	for _, target := range targets {
		key := target.Host + "|" + strconv.Itoa(target.Port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, target)
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueInts(values []int) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
