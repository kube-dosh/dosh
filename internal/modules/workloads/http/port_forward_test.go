package workloadshttp

import (
	stdhttp "net/http"
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

	filterTunnelSetCookies(header)

	values := header.Values("Set-Cookie")
	if len(values) != 2 || values[0] != "app_session=upstream; Path=/; HttpOnly" || values[1] != "theme=dark; Path=/" {
		t.Fatalf("Set-Cookie = %#v", values)
	}
}
