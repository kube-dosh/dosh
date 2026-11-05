package oidc

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"dosh/internal/modules/auth/app"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	UsernameClaim string
	GroupsClaim   string
	CAFile        string
	Timeout       time.Duration
}

type Provider struct {
	oauth      oauth2.Config
	verifier   *oidc.IDTokenVerifier
	client     *http.Client
	username   string
	groups     string
	endSession string
}

type discoveryClaims struct {
	EndSessionEndpoint string `json:"end_session_endpoint"`
}

type idTokenClaims struct {
	Email             string   `json:"email"`
	EmailVerified     *bool    `json:"email_verified"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Groups            []string `json:"groups"`
}

func New(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	client, err := httpClient(cfg)
	if err != nil {
		return nil, err
	}
	discoveryCtx := oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(discoveryCtx, strings.TrimSuffix(cfg.IssuerURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	var claims discoveryClaims
	_ = provider.Claims(&claims)
	endpoint := provider.Endpoint()
	if err := validateProviderEndpoints(cfg.IssuerURL, endpoint.AuthURL, endpoint.TokenURL, claims.EndSessionEndpoint); err != nil {
		return nil, err
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	}
	return &Provider{
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			RedirectURL: cfg.RedirectURL, Endpoint: endpoint, Scopes: scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}), client: client,
		username: defaultString(cfg.UsernameClaim, "email"), groups: defaultString(cfg.GroupsClaim, "groups"),
		endSession: claims.EndSessionEndpoint,
	}, nil
}

func validateProviderEndpoints(issuer, authorization, token, endSession string) error {
	parsedIssuer, err := url.Parse(issuer)
	if err != nil || parsedIssuer.Scheme == "" || parsedIssuer.Host == "" {
		return errors.New("OIDC issuer must be an absolute URL")
	}
	for _, endpoint := range []struct {
		name  string
		value string
	}{
		{name: "authorization", value: authorization},
		{name: "token", value: token},
		{name: "end session", value: endSession},
	} {
		if endpoint.value == "" {
			if endpoint.name == "end session" {
				continue
			}
			return fmt.Errorf("OIDC discovery did not provide a %s endpoint", endpoint.name)
		}
		parsed, err := url.Parse(endpoint.value)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("OIDC discovery %s endpoint is not a safe absolute HTTP(S) URL", endpoint.name)
		}
		if parsedIssuer.Scheme == "https" && parsed.Scheme != "https" {
			return fmt.Errorf("OIDC discovery %s endpoint downgrades HTTPS", endpoint.name)
		}
	}
	return nil
}

func (p *Provider) AuthCodeURL(state, nonce, codeVerifier string) string {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(codeVerifier), oidc.Nonce(nonce))
}

func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (app.OIDCIdentity, error) {
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return app.OIDCIdentity{}, fmt.Errorf("exchange authorization code: %w", tokenExchangeError(err))
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return app.OIDCIdentity{}, errors.New("OIDC response did not include an ID token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return app.OIDCIdentity{}, fmt.Errorf("verify ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(expectedNonce)) != 1 {
		return app.OIDCIdentity{}, errors.New("OIDC nonce mismatch")
	}
	var claims idTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return app.OIDCIdentity{}, fmt.Errorf("decode ID token claims: %w", err)
	}
	if p.username == "email" && (claims.Email == "" || claims.EmailVerified == nil || !*claims.EmailVerified) {
		return app.OIDCIdentity{}, errors.New("OIDC email must be explicitly verified")
	}
	username := p.usernameValue(idToken.Subject, claims)
	if username == "" {
		return app.OIDCIdentity{}, fmt.Errorf("OIDC username claim %q is empty", p.username)
	}
	groups, err := p.groupValues(claims)
	if err != nil {
		return app.OIDCIdentity{}, err
	}
	return app.OIDCIdentity{Subject: idToken.Subject, Username: username, Groups: groups, ExpiresAt: idToken.Expiry}, nil
}

func tokenExchangeError(err error) error {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return fmt.Errorf("token request failed: %w", err)
	}
	status := 0
	if retrieve.Response != nil {
		status = retrieve.Response.StatusCode
	}
	code := sanitizeOAuthCode(retrieve.ErrorCode)
	switch {
	case status > 0 && code != "":
		return fmt.Errorf("token endpoint returned HTTP %d (%s)", status, code)
	case status > 0:
		return fmt.Errorf("token endpoint returned HTTP %d", status)
	case code != "":
		return fmt.Errorf("token endpoint returned %s", code)
	default:
		return errors.New("token endpoint request failed")
	}
}

func sanitizeOAuthCode(code string) string {
	if code == "" || len(code) > 64 {
		return ""
	}
	for _, char := range code {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return ""
	}
	return code
}

func (p *Provider) EndSessionURL(postLogoutRedirectURL string) string {
	if p.endSession == "" {
		return ""
	}
	parsed, err := url.Parse(p.endSession)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	query.Set("post_logout_redirect_uri", postLogoutRedirectURL)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (p *Provider) usernameValue(subject string, claims idTokenClaims) string {
	switch p.username {
	case "sub":
		return subject
	case "preferred_username":
		return claims.PreferredUsername
	case "email":
		return claims.Email
	case "name":
		return claims.Name
	default:
		return ""
	}
}

func (p *Provider) groupValues(claims idTokenClaims) ([]string, error) {
	if p.groups == "groups" {
		return claims.Groups, nil
	}
	return nil, fmt.Errorf("unsupported OIDC groups claim %q", p.groups)
}

func httpClient(cfg Config) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read OIDC CA file: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("OIDC CA file does not contain a valid certificate")
		}
		tlsConfig.RootCAs = roots
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Timeout: cfg.Timeout, Transport: transport}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
