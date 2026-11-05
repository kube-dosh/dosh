package authhttp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"dosh/internal/domain"
	authapp "dosh/internal/modules/auth/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/httpx"
	oidcprovider "dosh/internal/platform/oidc"

	"github.com/go-chi/chi/v5"
)

type signedIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	mu        sync.Mutex
	challenge string
	nonce     string
	code      string
}

func newSignedIDP(t *testing.T) *signedIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	idp := &signedIDP{key: key, code: "integration-code"}
	idp.server = httptest.NewServer(http.HandlerFunc(idp.handle))
	t.Cleanup(idp.server.Close)
	return idp
}

func (i *signedIDP) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeOIDCTestJSON(w, map[string]any{
			"issuer": i.server.URL, "authorization_endpoint": i.server.URL + "/authorize",
			"token_endpoint": i.server.URL + "/token", "jwks_uri": i.server.URL + "/jwks",
			"end_session_endpoint": i.server.URL + "/logout", "response_types_supported": []string{"code"},
			"subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/authorize":
		i.mu.Lock()
		i.challenge = r.URL.Query().Get("code_challenge")
		i.nonce = r.URL.Query().Get("nonce")
		i.mu.Unlock()
		redirect, err := url.Parse(r.URL.Query().Get("redirect_uri"))
		if err != nil {
			http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
			return
		}
		query := redirect.Query()
		query.Set("code", i.code)
		query.Set("state", r.URL.Query().Get("state"))
		redirect.RawQuery = query.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	case "/jwks":
		writeOIDCTestJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "integration-key",
			"n": base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes()),
		}}})
	case "/token":
		i.handleToken(w, r)
	case "/logout":
		http.Redirect(w, r, r.URL.Query().Get("post_logout_redirect_uri"), http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

func (i *signedIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	verifier := r.Form.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	verifierChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	i.mu.Lock()
	challenge, nonce, code := i.challenge, i.nonce, i.code
	i.mu.Unlock()
	if r.Form.Get("code") != code || challenge == "" || verifierChallenge != challenge {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
		return
	}
	now := time.Now()
	rawToken, err := signOIDCTestJWT(i.key, map[string]any{
		"iss": i.server.URL, "aud": "dosh-integration", "sub": "subject-1", "nonce": nonce,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"email": "alice@example.test", "email_verified": true, "groups": []string{"viewers", "dosh-operators"},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeOIDCTestJSON(w, map[string]any{"access_token": "unused", "token_type": "Bearer", "expires_in": 300, "id_token": rawToken})
}

type integrationAuthorizer struct{}

func (integrationAuthorizer) Require(_ context.Context, actor domain.Actor, access domain.Access) error {
	if actor.Username == "" {
		return domain.ErrUnauthorized
	}
	if access.Resource == "secrets" && access.Verb == "get" {
		return domain.ErrForbidden
	}
	return nil
}

func TestRealProviderHTTPFlowCreatesSessionAndLogsOut(t *testing.T) {
	idp := newSignedIDP(t)
	var routes http.Handler
	doshServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes.ServeHTTP(w, r)
	}))
	t.Cleanup(doshServer.Close)

	provider, err := oidcprovider.New(context.Background(), oidcprovider.Config{
		IssuerURL: idp.server.URL, ClientID: "dosh-integration", ClientSecret: "secret",
		RedirectURL: doshServer.URL + "/api/v1/auth/oidc/callback",
		Scopes:      []string{"openid", "profile", "email", "groups"}, Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("oidcprovider.New() error = %v", err)
	}
	manager, err := authapp.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	states, err := authapp.NewOIDCStateManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewOIDCStateManager() error = %v", err)
	}
	sessions := authapp.NewSessionService(manager, integrationAuthorizer{}, "oidc")
	oidcService := authapp.NewOIDCService(provider, sessions, states, authapp.OIDCConfig{
		SessionTTL: time.Hour, PostLogoutRedirectURL: doshServer.URL + "/login",
	})
	handler := New(Options{
		Mode: "oidc", ProviderName: "signed-test-idp", OIDC: oidcService, Sessions: sessions,
		Audit: audit.New(discardLogger()), Logger: discardLogger(), CookieSecure: true,
	})
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.RequestIDContext)
	router.Get("/pods", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Get("/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Route("/api/v1", func(r chi.Router) {
		MountPublic(r, handler)
		r.Group(func(r chi.Router) {
			r.Use(handler.RequireSession)
			r.Use(handler.RequireCSRF)
			MountProtected(r, handler)
		})
	})
	routes = router

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := doshServer.Client()
	client.Jar = jar
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 {
			return http.ErrUseLastResponse
		}
		return nil
	}

	loginURL := doshServer.URL + "/api/v1/auth/oidc/login?returnTo=%2Fpods"
	response, err := client.Get(loginURL)
	if err != nil {
		t.Fatalf("login request error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/pods" {
		t.Fatalf("final login response = %d %s", response.StatusCode, response.Request.URL)
	}

	sessionResponse, err := client.Get(doshServer.URL + "/api/v1/session")
	if err != nil {
		t.Fatalf("session request error = %v", err)
	}
	defer sessionResponse.Body.Close()
	var session map[string]any
	if err := json.NewDecoder(sessionResponse.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session["authMode"] != "oidc" || session["user"].(map[string]any)["username"] != "alice@example.test" {
		t.Fatalf("session = %#v", session)
	}
	capabilities := session["capabilities"].(map[string]any)
	if capabilities["read"] != true || capabilities["revealSecrets"] != false {
		t.Fatalf("capabilities = %#v", capabilities)
	}

	logoutResponse, err := client.Get(doshServer.URL + "/api/v1/auth/oidc/logout")
	if err != nil {
		t.Fatalf("logout request error = %v", err)
	}
	defer logoutResponse.Body.Close()
	if logoutResponse.StatusCode != http.StatusOK || logoutResponse.Request.URL.Path != "/login" {
		t.Fatalf("final logout response = %d %s", logoutResponse.StatusCode, logoutResponse.Request.URL)
	}
}

func signOIDCTestJWT(key *rsa.PrivateKey, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": "integration-key", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func writeOIDCTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
