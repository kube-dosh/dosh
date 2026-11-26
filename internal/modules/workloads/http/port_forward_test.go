package workloadshttp

import (
	stdhttp "net/http"
	"strings"
	"testing"
)

func TestFilterTunnelCookiesRemovesDoshSessionAndOIDCState(t *testing.T) {
	header := "theme=dark; dosh_session=secret; app_session=upstream; dosh_oidc_state=state; mode=compact"
	if got := filterTunnelCookies(header); got != "theme=dark; app_session=upstream; mode=compact" {
		t.Fatalf("filterTunnelCookies() = %q", got)
	}
	if got := filterTunnelCookies(""); got != "" {
		t.Fatalf("filterTunnelCookies(empty) = %q", got)
	}
}

func TestQuerySuffix(t *testing.T) {
	if got := querySuffix("a=1&b=2"); got != "?a=1&b=2" {
		t.Fatalf("querySuffix() = %q", got)
	}
	if got := querySuffix(""); got != "" {
		t.Fatalf("querySuffix(empty) = %q", got)
	}
}

func TestFilterTunnelSetCookiesPreservesTargetCookies(t *testing.T) {
	header := make(stdhttp.Header)
	header.Add("Set-Cookie", "app_session=upstream; Path=/; HttpOnly")
	header.Add("Set-Cookie", "dosh_session=forged; Path=/")
	header.Add("Set-Cookie", "theme=dark; Path=/")
	header.Add("Set-Cookie", "dosh_oidc_state=forged; Path=/api/v1/auth/oidc")
	header.Add("Set-Cookie", "malformed-cookie")

	filterTunnelSetCookies(header, "/api/v1/tunnels/session/ports/80/")

	values := header.Values("Set-Cookie")
	if len(values) != 2 || !strings.Contains(values[0], "app_session=upstream") || !strings.Contains(values[0], "Path=/api/v1/tunnels/session/ports/80/") || !strings.Contains(values[0], "HttpOnly") {
		t.Fatalf("Set-Cookie = %#v", values)
	}
	if !strings.Contains(values[1], "theme=dark") || !strings.Contains(values[1], "Path=/api/v1/tunnels/session/ports/80/") {
		t.Fatalf("Set-Cookie = %#v", values)
	}
}

func TestFilterTunnelSetCookiesKeepsScopedPathAndDropsDomain(t *testing.T) {
	header := make(stdhttp.Header)
	header.Add("Set-Cookie", "app_session=upstream; Domain=upstream.internal; Path=/app; SameSite=Lax")
	filterTunnelSetCookies(header, "/api/v1/tunnels/id/ports/8080/")
	values := header.Values("Set-Cookie")
	if len(values) != 1 || strings.Contains(values[0], "Domain=") || !strings.Contains(values[0], "Path=/api/v1/tunnels/id/ports/8080/app") {
		t.Fatalf("Set-Cookie = %#v", values)
	}
}
