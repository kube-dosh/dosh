package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr              string
	TLSEnabled              bool
	TLSCertFile             string
	TLSKeyFile              string
	TLSReloadInterval       time.Duration
	AuthMode                string
	AdminUsername           string
	AdminPasswordHash       string
	SessionKey              string
	SessionTTL              time.Duration
	LoginRateLimitMode      string
	LoginRateLimitNamespace string
	CookieSecure            bool
	ReadOnly                bool
	KubeQPS                 float32
	KubeBurst               int
	KubeAPIServer           string
	RequestTimeout          time.Duration
	MaxBodyBytes            int64
	LogLevel                string
	ClusterName             string
	InsecureSkipVerify      bool
	AuditWebhookURL         string
	AuditWebhookToken       string
	AuditWebhookHMAC        string
	AuditWebhookCAFile      string
	AuditWebhookTimeout     time.Duration
	AuditWebhookQueue       int
	OIDCIssuer              string
	OIDCClientID            string
	OIDCClientSecret        string
	OIDCRedirectURL         string
	OIDCScopes              []string
	OIDCUsernameClaim       string
	OIDCGroupsClaim         string
	OIDCAdminGroups         []string
	OIDCCAFile              string
	OIDCTimeout             time.Duration
	OIDCStateTTL            time.Duration
	TokenReviewAudience     string
}

func Load() (Config, error) {
	var loadErrs []error
	cfg := Config{
		ListenAddr:              env("DOSH_LISTEN_ADDR", ":32626"),
		TLSEnabled:              boolEnv("DOSH_TLS_ENABLED", false, &loadErrs),
		TLSCertFile:             env("DOSH_TLS_CERT_FILE", "/etc/dosh/tls/tls.crt"),
		TLSKeyFile:              env("DOSH_TLS_KEY_FILE", "/etc/dosh/tls/tls.key"),
		TLSReloadInterval:       durationEnv("DOSH_TLS_RELOAD_INTERVAL", 30*time.Second, &loadErrs),
		AuthMode:                strings.ToLower(env("DOSH_AUTH_MODE", "token")),
		AdminUsername:           env("DOSH_ADMIN_USERNAME", ""),
		AdminPasswordHash:       env("DOSH_ADMIN_PASSWORD_HASH", ""),
		SessionKey:              env("DOSH_SESSION_KEY", ""),
		SessionTTL:              durationEnv("DOSH_SESSION_TTL", 8*time.Hour, &loadErrs),
		LoginRateLimitMode:      strings.ToLower(env("DOSH_LOGIN_RATE_LIMIT_MODE", "memory")),
		LoginRateLimitNamespace: strings.TrimSpace(env("DOSH_LOGIN_RATE_LIMIT_NAMESPACE", "")),
		ReadOnly:                boolEnv("DOSH_READ_ONLY", false, &loadErrs),
		KubeQPS:                 floatEnv("DOSH_KUBE_QPS", 50, &loadErrs),
		KubeBurst:               intEnv("DOSH_KUBE_BURST", 100, &loadErrs),
		KubeAPIServer:           strings.TrimSpace(env("DOSH_KUBE_API_SERVER", "")),
		RequestTimeout:          durationEnv("DOSH_REQUEST_TIMEOUT", 15*time.Second, &loadErrs),
		MaxBodyBytes:            int64(intEnv("DOSH_MAX_REQUEST_BODY", 2*1024*1024, &loadErrs)),
		LogLevel:                strings.ToLower(env("DOSH_LOG_LEVEL", "info")),
		ClusterName:             env("DOSH_CLUSTER_NAME", "kubernetes"),
		InsecureSkipVerify:      boolEnv("DOSH_INSECURE_SKIP_VERIFY", false, &loadErrs),
		AuditWebhookURL:         env("DOSH_AUDIT_WEBHOOK_URL", ""),
		AuditWebhookToken:       env("DOSH_AUDIT_WEBHOOK_TOKEN", ""),
		AuditWebhookHMAC:        env("DOSH_AUDIT_WEBHOOK_HMAC_KEY", ""),
		AuditWebhookCAFile:      env("DOSH_AUDIT_WEBHOOK_CA_FILE", ""),
		AuditWebhookTimeout:     durationEnv("DOSH_AUDIT_WEBHOOK_TIMEOUT", 3*time.Second, &loadErrs),
		AuditWebhookQueue:       intEnv("DOSH_AUDIT_WEBHOOK_QUEUE_SIZE", 1024, &loadErrs),
		OIDCIssuer:              env("DOSH_OIDC_ISSUER", ""),
		OIDCClientID:            env("DOSH_OIDC_CLIENT_ID", ""),
		OIDCClientSecret:        env("DOSH_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:         env("DOSH_OIDC_REDIRECT_URL", ""),
		OIDCScopes:              listEnv("DOSH_OIDC_SCOPES", []string{"openid", "profile", "email", "groups"}),
		OIDCUsernameClaim:       env("DOSH_OIDC_USERNAME_CLAIM", "email"),
		OIDCGroupsClaim:         env("DOSH_OIDC_GROUPS_CLAIM", "groups"),
		OIDCAdminGroups:         listEnv("DOSH_OIDC_ADMIN_GROUPS", nil),
		OIDCCAFile:              env("DOSH_OIDC_CA_FILE", ""),
		OIDCTimeout:             durationEnv("DOSH_OIDC_TIMEOUT", 10*time.Second, &loadErrs),
		OIDCStateTTL:            durationEnv("DOSH_OIDC_STATE_TTL", 10*time.Minute, &loadErrs),
		TokenReviewAudience:     strings.TrimSpace(env("DOSH_TOKEN_REVIEW_AUDIENCE", "")),
	}
	cfg.CookieSecure = boolEnv("DOSH_COOKIE_SECURE", cfg.TLSEnabled, &loadErrs)
	loadErrs = append(loadErrs, cfg.Validate())
	if err := errors.Join(loadErrs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error
	if strings.TrimSpace(c.ListenAddr) == "" {
		errs = append(errs, errors.New("DOSH_LISTEN_ADDR must not be empty"))
	}
	if c.SessionTTL <= 0 || c.SessionTTL > 8*time.Hour {
		errs = append(errs, errors.New("DOSH_SESSION_TTL must be between 1ns and 8h"))
	}
	if c.RequestTimeout <= 0 || c.RequestTimeout > 5*time.Minute {
		errs = append(errs, errors.New("DOSH_REQUEST_TIMEOUT must be between 1ns and 5m"))
	}
	if c.MaxBodyBytes <= 0 || c.MaxBodyBytes > 16*1024*1024 {
		errs = append(errs, errors.New("DOSH_MAX_REQUEST_BODY must be between 1 and 16MiB"))
	}
	if strings.TrimSpace(c.ClusterName) == "" {
		errs = append(errs, errors.New("DOSH_CLUSTER_NAME must not be empty"))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, errors.New("DOSH_LOG_LEVEL must be one of debug, info, warn, error"))
	}
	if c.KubeQPS <= 0 {
		errs = append(errs, errors.New("DOSH_KUBE_QPS must be positive"))
	}
	if c.KubeBurst <= 0 {
		errs = append(errs, errors.New("DOSH_KUBE_BURST must be positive"))
	}
	if c.KubeAPIServer != "" {
		if err := validateAbsoluteURL("DOSH_KUBE_API_SERVER", c.KubeAPIServer, true); err != nil {
			errs = append(errs, err)
		}
	}
	if c.InsecureSkipVerify && c.KubeAPIServer == "" {
		errs = append(errs, errors.New("DOSH_INSECURE_SKIP_VERIFY requires DOSH_KUBE_API_SERVER"))
	}
	if c.TLSEnabled && (c.TLSCertFile == "" || c.TLSKeyFile == "") {
		errs = append(errs, errors.New("TLS certificate and key files are required when TLS is enabled"))
	}
	if c.TLSReloadInterval < 0 || (c.TLSReloadInterval > 0 && c.TLSReloadInterval < 5*time.Second) || c.TLSReloadInterval > 24*time.Hour {
		errs = append(errs, errors.New("DOSH_TLS_RELOAD_INTERVAL must be 0 or between 5s and 24h"))
	}
	if c.TLSEnabled && !c.CookieSecure {
		errs = append(errs, errors.New("DOSH_COOKIE_SECURE is required when DOSH_TLS_ENABLED=true"))
	}
	if c.CookieSecure && !c.TLSEnabled {
		errs = append(errs, errors.New("DOSH_COOKIE_SECURE requires DOSH_TLS_ENABLED"))
	}
	if c.AuthMode != "local" && c.AuthMode != "oidc" && c.AuthMode != "token" {
		errs = append(errs, fmt.Errorf("unsupported DOSH_AUTH_MODE %q; supported: local, oidc, token", c.AuthMode))
	}
	if len(c.SessionKeyBytes()) < 32 {
		errs = append(errs, errors.New("DOSH_SESSION_KEY must contain at least 32 bytes (raw or base64)"))
	}
	switch c.LoginRateLimitMode {
	case "", "memory":
	case "kubernetes":
		if c.AuthMode != "local" && c.AuthMode != "token" {
			errs = append(errs, errors.New("DOSH_LOGIN_RATE_LIMIT_MODE=kubernetes is supported only in local or token auth mode"))
		}
		if c.LoginRateLimitNamespace == "" {
			errs = append(errs, errors.New("DOSH_LOGIN_RATE_LIMIT_NAMESPACE is required when DOSH_LOGIN_RATE_LIMIT_MODE=kubernetes"))
		}
		if len(c.LoginRateLimitNamespace) > 63 {
			errs = append(errs, errors.New("DOSH_LOGIN_RATE_LIMIT_NAMESPACE must be at most 63 characters"))
		}
	default:
		errs = append(errs, fmt.Errorf("unsupported DOSH_LOGIN_RATE_LIMIT_MODE %q; supported: memory, kubernetes", c.LoginRateLimitMode))
	}
	if c.AuthMode == "local" {
		if strings.TrimSpace(c.AdminUsername) == "" {
			errs = append(errs, errors.New("DOSH_ADMIN_USERNAME is required in local auth mode"))
		}
		if !strings.HasPrefix(c.AdminPasswordHash, "$argon2id$") {
			errs = append(errs, errors.New("DOSH_ADMIN_PASSWORD_HASH must be an Argon2id hash"))
		}
	}
	if len(c.TokenReviewAudience) > 1024 {
		errs = append(errs, errors.New("DOSH_TOKEN_REVIEW_AUDIENCE must be at most 1024 characters"))
	}
	if c.AuthMode == "oidc" {
		if err := validateAbsoluteURL("DOSH_OIDC_ISSUER", c.OIDCIssuer, true); err != nil {
			errs = append(errs, err)
		}
		if strings.TrimSpace(c.OIDCClientID) == "" {
			errs = append(errs, errors.New("DOSH_OIDC_CLIENT_ID is required in OIDC auth mode"))
		}
		if strings.TrimSpace(c.OIDCClientSecret) == "" {
			errs = append(errs, errors.New("DOSH_OIDC_CLIENT_SECRET is required in OIDC auth mode"))
		}
		if err := validateAbsoluteURL("DOSH_OIDC_REDIRECT_URL", c.OIDCRedirectURL, false); err != nil {
			errs = append(errs, err)
		}
		if !contains(c.OIDCScopes, "openid") {
			errs = append(errs, errors.New("DOSH_OIDC_SCOPES must include openid"))
		}
		switch c.OIDCUsernameClaim {
		case "sub", "email", "preferred_username", "name":
		default:
			errs = append(errs, errors.New("DOSH_OIDC_USERNAME_CLAIM must be one of sub, email, preferred_username, name"))
		}
		if c.OIDCGroupsClaim != "groups" {
			errs = append(errs, errors.New("DOSH_OIDC_GROUPS_CLAIM must be groups"))
		}
		if c.OIDCTimeout <= 0 || c.OIDCTimeout > 30*time.Second {
			errs = append(errs, errors.New("DOSH_OIDC_TIMEOUT must be between 1ns and 30s"))
		}
		if c.OIDCStateTTL < time.Second || c.OIDCStateTTL > 30*time.Minute {
			errs = append(errs, errors.New("DOSH_OIDC_STATE_TTL must be between 1s and 30m"))
		}
	}
	if c.AuditWebhookURL != "" {
		if err := validateAbsoluteURL("DOSH_AUDIT_WEBHOOK_URL", c.AuditWebhookURL, true); err != nil {
			errs = append(errs, err)
		}
		if strings.TrimSpace(c.AuditWebhookToken) == "" && strings.TrimSpace(c.AuditWebhookHMAC) == "" {
			errs = append(errs, errors.New("audit webhook requires DOSH_AUDIT_WEBHOOK_TOKEN or DOSH_AUDIT_WEBHOOK_HMAC_KEY"))
		}
		if c.AuditWebhookTimeout <= 0 || c.AuditWebhookTimeout > 30*time.Second {
			errs = append(errs, errors.New("DOSH_AUDIT_WEBHOOK_TIMEOUT must be between 1ns and 30s"))
		}
		if c.AuditWebhookQueue < 1 || c.AuditWebhookQueue > 100000 {
			errs = append(errs, errors.New("DOSH_AUDIT_WEBHOOK_QUEUE_SIZE must be between 1 and 100000"))
		}
	}
	return errors.Join(errs...)
}

func (c Config) SessionKeyBytes() []byte {
	if decoded, err := base64.StdEncoding.DecodeString(c.SessionKey); err == nil && len(decoded) >= 32 {
		return decoded
	}
	return []byte(c.SessionKey)
}

func validateAbsoluteURL(name, raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http or https URL", name)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s must not contain user information or a fragment", name)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", name)
	}
	return nil
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool, errs *[]error) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a boolean: %w", key, err))
		return fallback
	}
	return parsed
}

func intEnv(key string, fallback int, errs *[]error) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return parsed
}

func floatEnv(key string, fallback float32, errs *[]error) float32 {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(v, 32)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a number: %w", key, err))
		return fallback
	}
	return float32(parsed)
}

func durationEnv(key string, fallback time.Duration, errs *[]error) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a duration: %w", key, err))
		return fallback
	}
	return parsed
}

func listEnv(key string, fallback []string) []string {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	result := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
