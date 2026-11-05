package bootstrap

import "testing"

func TestOIDCPostLogoutRedirect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		callback string
		want     string
	}{
		{name: "same origin login", callback: "https://dosh.example.test/api/v1/auth/oidc/callback", want: "https://dosh.example.test/login"},
		{name: "drops query and fragment", callback: "https://dosh.example.test:32626/callback?x=1#fragment", want: "https://dosh.example.test:32626/login"},
		{name: "invalid URL", callback: "/relative/callback", want: ""},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := oidcPostLogoutRedirect(test.callback); got != test.want {
				t.Fatalf("oidcPostLogoutRedirect(%q) = %q, want %q", test.callback, got, test.want)
			}
		})
	}
}
