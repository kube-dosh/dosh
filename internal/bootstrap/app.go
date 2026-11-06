package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"dosh/internal/domain"
	audithistoryapp "dosh/internal/modules/audithistory/app"
	audithistoryhttp "dosh/internal/modules/audithistory/http"
	"dosh/internal/modules/auth/app"
	authhttp "dosh/internal/modules/auth/http"
	certificatesapp "dosh/internal/modules/certificates/app"
	certificateshttp "dosh/internal/modules/certificates/http"
	configurationapp "dosh/internal/modules/configuration/app"
	configurationhttp "dosh/internal/modules/configuration/http"
	eventsapp "dosh/internal/modules/events/app"
	eventshttp "dosh/internal/modules/events/http"
	networkapp "dosh/internal/modules/network/app"
	networkhttp "dosh/internal/modules/network/http"
	nodesapp "dosh/internal/modules/nodes/app"
	nodeshttp "dosh/internal/modules/nodes/http"
	overviewapp "dosh/internal/modules/overview/app"
	overviewhttp "dosh/internal/modules/overview/http"
	storageapp "dosh/internal/modules/storage/app"
	storagehttp "dosh/internal/modules/storage/http"
	workloadsapp "dosh/internal/modules/workloads/app"
	workloadshttp "dosh/internal/modules/workloads/http"
	yamlapp "dosh/internal/modules/yaml/app"
	yamlhttp "dosh/internal/modules/yaml/http"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"
	"dosh/internal/platform/config"
	"dosh/internal/platform/httpserver"
	"dosh/internal/platform/httpx"
	"dosh/internal/platform/kube"
	oidcprovider "dosh/internal/platform/oidc"
	"dosh/internal/platform/tlscert"
	"dosh/internal/version"
)

type App struct {
	Config config.Config
	Logger *slog.Logger
	Router stdhttp.Handler
	audit  *audit.Logger
	certs  *tlscert.Store
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	logger := newLogger(cfg.LogLevel)
	httpx.SetMaxBodyBytes(cfg.MaxBodyBytes)
	var err error
	var certificateStore *tlscert.Store
	if cfg.TLSEnabled {
		certificateStore, err = tlscert.New(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSReloadInterval)
		if err != nil {
			return nil, err
		}
	}
	kubeClient, err := kube.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	auditLogger, err := audit.NewWithConfig(logger, audit.Config{
		WebhookURL: cfg.AuditWebhookURL, BearerToken: cfg.AuditWebhookToken,
		HMACKey: []byte(cfg.AuditWebhookHMAC), CAFile: cfg.AuditWebhookCAFile,
		Timeout: cfg.AuditWebhookTimeout, QueueSize: cfg.AuditWebhookQueue,
	})
	if err != nil {
		return nil, err
	}
	sessionManager, err := app.NewSessionManager(cfg.SessionKeyBytes(), cfg.SessionTTL)
	if err != nil {
		return nil, err
	}
	var authorizer domain.Authorizer
	var localService *app.Service
	var tokenService *app.KubernetesTokenService
	var oidcService *app.OIDCService
	var sessions *app.SessionService
	providerName := ""
	switch cfg.AuthMode {
	case "local":
		authorizer = authz.NewLocal(cfg.ReadOnly)
	case "oidc", "token":
		authorizer = authz.NewKubernetes(kubeClient, cfg.ReadOnly)
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", cfg.AuthMode)
	}
	sessions = app.NewSessionService(sessionManager, authorizer, cfg.AuthMode)
	if cfg.AuthMode == "oidc" {
		provider, err := oidcprovider.New(ctx, oidcprovider.Config{
			IssuerURL: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
			RedirectURL: cfg.OIDCRedirectURL, Scopes: cfg.OIDCScopes, UsernameClaim: cfg.OIDCUsernameClaim,
			GroupsClaim: cfg.OIDCGroupsClaim, CAFile: cfg.OIDCCAFile, Timeout: cfg.OIDCTimeout,
		})
		if err != nil {
			return nil, err
		}
		stateManager, err := app.NewOIDCStateManager(cfg.SessionKeyBytes())
		if err != nil {
			return nil, err
		}
		oidcService = app.NewOIDCService(provider, sessions, stateManager, app.OIDCConfig{
			AdminGroups:           cfg.OIDCAdminGroups,
			SessionTTL:            cfg.SessionTTL,
			StateTTL:              cfg.OIDCStateTTL,
			PostLogoutRedirectURL: oidcPostLogoutRedirect(cfg.OIDCRedirectURL),
		})
		if parsed, err := url.Parse(cfg.OIDCIssuer); err == nil {
			providerName = parsed.Host
		}
	}
	var loginAttempts app.LoginAttemptGate
	if (cfg.AuthMode == "local" || cfg.AuthMode == "token") && cfg.LoginRateLimitMode == "kubernetes" {
		loginAttempts = kubeClient.NewKubernetesLoginAttempts(cfg.LoginRateLimitNamespace)
	}
	if cfg.AuthMode == "local" {
		localService = app.NewServiceWithLoginAttempts(app.Config{Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash, ReadOnly: cfg.ReadOnly}, sessions, loginAttempts)
	} else if cfg.AuthMode == "token" {
		tokenService = app.NewKubernetesTokenService(kubeClient, sessions, loginAttempts)
		providerName = "Kubernetes"
	}
	authHandler := authhttp.New(authhttp.Options{
		Mode: cfg.AuthMode, ProviderName: providerName, Local: localService, Token: tokenService, OIDC: oidcService,
		Sessions: sessions, Audit: auditLogger, Logger: logger, CookieSecure: cfg.CookieSecure,
	})

	overviewService := overviewapp.New(kubeClient, cfg.ClusterName)
	nodesService := nodesapp.New(kubeClient, authorizer, auditLogger)
	workloadsService := workloadsapp.New(kubeClient, authorizer, auditLogger)
	eventsService := eventsapp.New(kubeClient)
	networkService := networkapp.New(kubeClient, authorizer, auditLogger)
	configurationService := configurationapp.New(kubeClient, authorizer, auditLogger)
	certificatesService := certificatesapp.New(certificateStore, authorizer, auditLogger)
	storageService := storageapp.New(kubeClient)
	yamlService := yamlapp.New(kubeClient, authorizer, auditLogger)
	auditHistoryService := audithistoryapp.New(auditLogger, authorizer)

	router, err := buildRouter(routerDependencies{
		logger:        logger,
		cfg:           cfg,
		kube:          kubeClient,
		auth:          authHandler,
		overview:      overviewhttp.New(overviewService),
		nodes:         nodeshttp.New(nodesService),
		workloads:     workloadshttp.New(workloadsService),
		events:        eventshttp.New(eventsService),
		network:       networkhttp.New(networkService),
		configuration: configurationhttp.New(configurationService),
		certificates:  certificateshttp.New(certificatesService),
		storage:       storagehttp.New(storageService),
		yaml:          yamlhttp.New(yamlService),
		auditHistory:  audithistoryhttp.New(auditHistoryService),
	})
	if err != nil {
		return nil, err
	}
	logger.Info("dosh_initialized", "version", version.Version, "commit", version.Commit, "auth_mode", cfg.AuthMode, "read_only", cfg.ReadOnly, "login_rate_limit_mode", cfg.LoginRateLimitMode, "audit_webhook", cfg.AuditWebhookURL != "", "oidc_issuer", cfg.OIDCIssuer)
	return &App{Config: cfg, Logger: logger, Router: router, audit: auditLogger, certs: certificateStore}, nil
}

func oidcPostLogoutRedirect(callbackURL string) string {
	parsed, err := url.Parse(callbackURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.Path = "/login"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func (a *App) Run(ctx context.Context) error {
	if a.certs != nil {
		go a.certs.Run(ctx, a.Logger)
	}
	server := httpserver.New(a.Router, a.Config, a.Logger)
	httpserver.UseCertificate(server, a.certs)
	serverErr := httpserver.Run(ctx, server, a.Config, a.Logger)
	auditCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	auditErr := a.audit.Close(auditCtx)
	if auditErr != nil {
		a.Logger.Error("audit_flush_failed", "error", auditErr)
	}
	return errors.Join(serverErr, auditErr)
}

func newLogger(level string) *slog.Logger {
	var parsed slog.Level
	switch strings.ToLower(level) {
	case "debug":
		parsed = slog.LevelDebug
	case "warn":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	default:
		parsed = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed})
	return slog.New(handler)
}

func readiness(ctx context.Context, kubeClient *kube.Client) (int, map[string]any) {
	start := time.Now()
	ks := "ok"
	status := stdhttp.StatusOK
	if _, err := kubeClient.Version(ctx); err != nil {
		ks = "unavailable"
		status = stdhttp.StatusServiceUnavailable
	}
	return status, map[string]any{
		"status": "ok", "kubernetes": ks, "latencyMs": time.Since(start).Milliseconds(), "version": version.Version,
	}
}

func staticHandler() (stdhttp.Handler, error) { return newSPAHandler() }
