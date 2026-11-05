package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		ListenAddr:        ":32626",
		AuthMode:          "local",
		AdminUsername:     "admin",
		AdminPasswordHash: "$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SessionKey:        base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
		SessionTTL:        8 * time.Hour,
		RequestTimeout:    15 * time.Second,
		MaxBodyBytes:      2 * 1024 * 1024,
		KubeQPS:           50,
		KubeBurst:         100,
		ClusterName:       "kubedo-lab",
		LogLevel:          "info",
	}
}

func TestValidateAcceptsSecureLocalConfig(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(cfg.SessionKeyBytes()) < 32 {
		t.Fatal("SessionKeyBytes() returned a short key")
	}
}

func TestValidateAcceptsKubernetesTokenAuthConfig(t *testing.T) {
	cfg := validConfig()
	cfg.AuthMode = "token"
	cfg.AdminUsername = ""
	cfg.AdminPasswordHash = ""
	cfg.TokenReviewAudience = "https://kubernetes.default.svc"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected token auth config: %v", err)
	}
	cfg.LoginRateLimitMode = "kubernetes"
	cfg.LoginRateLimitNamespace = "dosh"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected token distributed rate limiter: %v", err)
	}
	cfg.AuthMode = "oidc"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "only in local or token auth mode") {
		t.Fatalf("Validate() error = %v, want OIDC rate limiter restriction", err)
	}
}

func TestValidateLoginRateLimitConfiguration(t *testing.T) {
	cfg := validConfig()
	cfg.LoginRateLimitMode = "memory"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(memory) error = %v", err)
	}

	cfg.LoginRateLimitMode = "kubernetes"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DOSH_LOGIN_RATE_LIMIT_NAMESPACE") {
		t.Fatalf("Validate(kubernetes without namespace) error = %v", err)
	}
	cfg.LoginRateLimitNamespace = "dosh"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(kubernetes with namespace) error = %v", err)
	}

	cfg.LoginRateLimitMode = "redis"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DOSH_LOGIN_RATE_LIMIT_MODE") {
		t.Fatalf("Validate(unsupported mode) error = %v", err)
	}
}

func TestValidateRejectsMissingSecurityMaterial(t *testing.T) {
	cfg := validConfig()
	cfg.AdminPasswordHash = "plain-text"
	cfg.SessionKey = "short"
	cfg.SessionTTL = 48 * time.Hour
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() accepted insecure configuration")
	}
	for _, want := range []string{"DOSH_ADMIN_PASSWORD_HASH", "DOSH_SESSION_KEY", "DOSH_SESSION_TTL"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Validate() error %q does not mention %s", err, want)
		}
	}
}

func TestValidateRequiresSecureCookieWhenTLSIsEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.TLSEnabled = true
	cfg.TLSCertFile = "/etc/dosh/tls/tls.crt"
	cfg.TLSKeyFile = "/etc/dosh/tls/tls.key"
	cfg.CookieSecure = false
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DOSH_COOKIE_SECURE is required") {
		t.Fatalf("Validate() error = %v, want secure-cookie error", err)
	}
}

func TestValidateRejectsSessionTTLLongerThanEightHours(t *testing.T) {
	cfg := validConfig()
	cfg.SessionTTL = 8*time.Hour + time.Second
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DOSH_SESSION_TTL") {
		t.Fatalf("Validate() error = %v, want session TTL error", err)
	}
}

func TestValidateRejectsTLSWithoutFiles(t *testing.T) {
	cfg := validConfig()
	cfg.TLSEnabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("Validate() error = %v, want TLS validation error", err)
	}
}

func TestValidateTLSReloadInterval(t *testing.T) {
	cfg := validConfig()
	for _, interval := range []time.Duration{0, 5 * time.Second, 30 * time.Second, 24 * time.Hour} {
		cfg.TLSReloadInterval = interval
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() rejected reload interval %s: %v", interval, err)
		}
	}
	for _, interval := range []time.Duration{-time.Second, time.Second, 24*time.Hour + time.Second} {
		cfg.TLSReloadInterval = interval
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DOSH_TLS_RELOAD_INTERVAL") {
			t.Fatalf("Validate() error = %v for interval %s", err, interval)
		}
	}
}

func TestValidateRejectsInvalidOperationalSettings(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Config)
		wantErr string
	}{
		{name: "zero request timeout", change: func(cfg *Config) { cfg.RequestTimeout = 0 }, wantErr: "DOSH_REQUEST_TIMEOUT"},
		{name: "request timeout too long", change: func(cfg *Config) { cfg.RequestTimeout = 6 * time.Minute }, wantErr: "DOSH_REQUEST_TIMEOUT"},
		{name: "empty cluster name", change: func(cfg *Config) { cfg.ClusterName = " " }, wantErr: "DOSH_CLUSTER_NAME"},
		{name: "invalid log level", change: func(cfg *Config) { cfg.LogLevel = "verbose" }, wantErr: "DOSH_LOG_LEVEL"},
		{name: "secure cookie without TLS", change: func(cfg *Config) { cfg.CookieSecure = true }, wantErr: "DOSH_COOKIE_SECURE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			test.change(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateKubeAPIServerOverride(t *testing.T) {
	cfg := validConfig()
	cfg.KubeAPIServer = "https://kube-fault-proxy.dosh-e2e.svc:443"
	cfg.InsecureSkipVerify = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected test API override: %v", err)
	}

	invalid := []struct {
		name    string
		change  func(*Config)
		wantErr string
	}{
		{
			name:    "http endpoint",
			change:  func(cfg *Config) { cfg.KubeAPIServer = "http://kube-fault-proxy:443" },
			wantErr: "DOSH_KUBE_API_SERVER",
		},
		{
			name: "insecure without override",
			change: func(cfg *Config) {
				cfg.KubeAPIServer = ""
				cfg.InsecureSkipVerify = true
			},
			wantErr: "DOSH_INSECURE_SKIP_VERIFY requires DOSH_KUBE_API_SERVER",
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			candidate := cfg
			test.change(&candidate)
			if err := candidate.Validate(); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateAuditWebhook(t *testing.T) {
	valid := validConfig()
	valid.AuditWebhookURL = "https://siem.example.test/audit"
	valid.AuditWebhookToken = "token"
	valid.AuditWebhookTimeout = 3 * time.Second
	valid.AuditWebhookQueue = 1024
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() rejected valid audit config: %v", err)
	}

	hmacOnly := valid
	hmacOnly.AuditWebhookToken = ""
	hmacOnly.AuditWebhookHMAC = "hmac-secret"
	if err := hmacOnly.Validate(); err != nil {
		t.Fatalf("Validate() rejected HMAC-only audit config: %v", err)
	}

	tests := []struct {
		name    string
		change  func(*Config)
		wantErr string
	}{
		{
			name:    "relative URL",
			change:  func(cfg *Config) { cfg.AuditWebhookURL = "siem.example.test/audit" },
			wantErr: "absolute http or https URL",
		},
		{
			name:    "unsupported scheme",
			change:  func(cfg *Config) { cfg.AuditWebhookURL = "ftp://siem.example.test/audit" },
			wantErr: "absolute http or https URL",
		},
		{
			name:    "missing host",
			change:  func(cfg *Config) { cfg.AuditWebhookURL = "https:///audit" },
			wantErr: "absolute http or https URL",
		},
		{
			name:    "non-HTTPS URL",
			change:  func(cfg *Config) { cfg.AuditWebhookURL = "http://siem.example.test/audit" },
			wantErr: "DOSH_AUDIT_WEBHOOK_URL must use https",
		},
		{
			name: "missing authentication",
			change: func(cfg *Config) {
				cfg.AuditWebhookToken = " "
				cfg.AuditWebhookHMAC = ""
			},
			wantErr: "requires DOSH_AUDIT_WEBHOOK_TOKEN or DOSH_AUDIT_WEBHOOK_HMAC_KEY",
		},
		{
			name:    "zero timeout",
			change:  func(cfg *Config) { cfg.AuditWebhookTimeout = 0 },
			wantErr: "DOSH_AUDIT_WEBHOOK_TIMEOUT",
		},
		{
			name:    "negative timeout",
			change:  func(cfg *Config) { cfg.AuditWebhookTimeout = -time.Second },
			wantErr: "DOSH_AUDIT_WEBHOOK_TIMEOUT",
		},
		{
			name:    "timeout too long",
			change:  func(cfg *Config) { cfg.AuditWebhookTimeout = 31 * time.Second },
			wantErr: "DOSH_AUDIT_WEBHOOK_TIMEOUT",
		},
		{
			name:    "zero queue",
			change:  func(cfg *Config) { cfg.AuditWebhookQueue = 0 },
			wantErr: "DOSH_AUDIT_WEBHOOK_QUEUE_SIZE",
		},
		{
			name:    "negative queue",
			change:  func(cfg *Config) { cfg.AuditWebhookQueue = -1 },
			wantErr: "DOSH_AUDIT_WEBHOOK_QUEUE_SIZE",
		},
		{
			name:    "queue too large",
			change:  func(cfg *Config) { cfg.AuditWebhookQueue = 100001 },
			wantErr: "DOSH_AUDIT_WEBHOOK_QUEUE_SIZE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.change(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateOIDCConfig(t *testing.T) {
	cfg := validConfig()
	cfg.AuthMode = "oidc"
	cfg.AdminUsername = ""
	cfg.AdminPasswordHash = ""
	cfg.OIDCIssuer = "https://idp.example.test"
	cfg.OIDCClientID = "dosh"
	cfg.OIDCClientSecret = "client-secret"
	cfg.OIDCRedirectURL = "https://dosh.example.test/api/v1/auth/oidc/callback"
	cfg.OIDCScopes = []string{"openid", "profile", "email", "groups"}
	cfg.OIDCUsernameClaim = "email"
	cfg.OIDCGroupsClaim = "groups"
	cfg.OIDCAdminGroups = []string{"dosh-admins"}
	cfg.OIDCTimeout = 10 * time.Second
	cfg.OIDCStateTTL = 10 * time.Minute
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected valid OIDC config: %v", err)
	}

	tests := []struct {
		name    string
		change  func(*Config)
		wantErr string
	}{
		{name: "missing issuer", change: func(cfg *Config) { cfg.OIDCIssuer = "" }, wantErr: "DOSH_OIDC_ISSUER"},
		{name: "http issuer", change: func(cfg *Config) { cfg.OIDCIssuer = "http://idp.example.test" }, wantErr: "https"},
		{name: "issuer userinfo", change: func(cfg *Config) { cfg.OIDCIssuer = "https://user:password@idp.example.test" }, wantErr: "must not contain user information"},
		{name: "redirect fragment", change: func(cfg *Config) { cfg.OIDCRedirectURL = "https://dosh.example.test/callback#fragment" }, wantErr: "must not contain user information"},
		{name: "missing client id", change: func(cfg *Config) { cfg.OIDCClientID = "" }, wantErr: "DOSH_OIDC_CLIENT_ID"},
		{name: "missing client secret", change: func(cfg *Config) { cfg.OIDCClientSecret = "" }, wantErr: "DOSH_OIDC_CLIENT_SECRET"},
		{name: "relative redirect", change: func(cfg *Config) { cfg.OIDCRedirectURL = "/callback" }, wantErr: "DOSH_OIDC_REDIRECT_URL"},
		{name: "missing openid scope", change: func(cfg *Config) { cfg.OIDCScopes = []string{"profile"} }, wantErr: "openid"},
		{name: "unsupported username claim", change: func(cfg *Config) { cfg.OIDCUsernameClaim = "dn" }, wantErr: "DOSH_OIDC_USERNAME_CLAIM"},
		{name: "unsupported groups claim", change: func(cfg *Config) { cfg.OIDCGroupsClaim = "roles" }, wantErr: "DOSH_OIDC_GROUPS_CLAIM"},
		{name: "zero timeout", change: func(cfg *Config) { cfg.OIDCTimeout = 0 }, wantErr: "DOSH_OIDC_TIMEOUT"},
		{name: "state TTL too short", change: func(cfg *Config) { cfg.OIDCStateTTL = 999 * time.Millisecond }, wantErr: "DOSH_OIDC_STATE_TTL"},
		{name: "state TTL too long", change: func(cfg *Config) { cfg.OIDCStateTTL = 31 * time.Minute }, wantErr: "DOSH_OIDC_STATE_TTL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := cfg
			test.change(&invalid)
			if err := invalid.Validate(); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadRejectsMalformedEnvironmentValues(t *testing.T) {
	clearDOSHEnvironment(t)
	t.Setenv("DOSH_AUTH_MODE", "local")
	t.Setenv("DOSH_ADMIN_USERNAME", "admin")
	t.Setenv("DOSH_ADMIN_PASSWORD_HASH", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("DOSH_SESSION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))

	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "boolean", key: "DOSH_TLS_ENABLED", value: "sometimes"},
		{name: "duration", key: "DOSH_SESSION_TTL", value: "later"},
		{name: "TLS reload duration", key: "DOSH_TLS_RELOAD_INTERVAL", value: "later"},
		{name: "float", key: "DOSH_KUBE_QPS", value: "fast"},
		{name: "integer", key: "DOSH_KUBE_BURST", value: "many"},
		{name: "body size", key: "DOSH_MAX_REQUEST_BODY", value: "2M"},
		{name: "audit queue", key: "DOSH_AUDIT_WEBHOOK_QUEUE_SIZE", value: "lots"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("Load() error = %v, want malformed %s error", err, test.key)
			}
		})
	}
}

func clearDOSHEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "DOSH_") {
			continue
		}
		value, exists := os.LookupEnv(key)
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%s): %v", key, err)
		}
	}
}
