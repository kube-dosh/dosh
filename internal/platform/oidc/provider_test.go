package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testClientID = "dosh-test-client"

type testIDP struct {
	t      *testing.T
	server *httptest.Server

	mu             sync.Mutex
	signingKey     *rsa.PrivateKey
	signingKeyID   string
	publishedKeys  map[string]*rsa.PublicKey
	claims         map[string]any
	challenge      string
	lastVerifier   string
	tokenCalls     int
	tokenErrorBody string
}

func newTestIDP(t *testing.T) *testIDP {
	t.Helper()
	key := mustRSAKey(t)
	idp := &testIDP{
		t: t, signingKey: key, signingKeyID: "key-1",
		publishedKeys: map[string]*rsa.PublicKey{"key-1": &key.PublicKey},
		claims:        map[string]any{},
	}
	idp.server = httptest.NewServer(http.HandlerFunc(idp.handle))
	t.Cleanup(idp.server.Close)
	return idp
}

func (i *testIDP) issuer() string { return i.server.URL }

func (i *testIDP) provider(t *testing.T) *Provider {
	t.Helper()
	provider, err := New(context.Background(), Config{
		IssuerURL: i.issuer(), ClientID: testClientID, ClientSecret: "not-used-by-pkce",
		RedirectURL:   "https://dosh.example.test/api/v1/auth/oidc/callback",
		Scopes:        []string{"openid", "profile", "email", "groups"},
		UsernameClaim: "email", GroupsClaim: "groups", Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return provider
}

func (i *testIDP) setClaims(claims map[string]any) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.claims = claims
}

func (i *testIDP) setSigningKey(id string, key *rsa.PrivateKey, published []*rsa.PrivateKey) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.signingKey = key
	i.signingKeyID = id
	i.publishedKeys = make(map[string]*rsa.PublicKey, len(published))
	for index, publishedKey := range published {
		i.publishedKeys[fmt.Sprintf("key-%d", index+1)] = &publishedKey.PublicKey
	}
}

func (i *testIDP) setChallenge(challenge string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.challenge = challenge
}

func (i *testIDP) setTokenErrorBody(body string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tokenErrorBody = body
}

func (i *testIDP) verifier() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.lastVerifier
}

func (i *testIDP) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeJSON(w, map[string]any{
			"issuer": i.issuer(), "authorization_endpoint": i.issuer() + "/authorize",
			"token_endpoint": i.issuer() + "/token", "jwks_uri": i.issuer() + "/jwks",
			"end_session_endpoint": i.issuer() + "/logout", "response_types_supported": []string{"code"},
			"subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/jwks":
		i.writeJWKS(w)
	case "/token":
		i.writeToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (i *testIDP) writeJWKS(w http.ResponseWriter) {
	i.mu.Lock()
	defer i.mu.Unlock()
	keys := make([]map[string]any, 0, len(i.publishedKeys))
	for kid, key := range i.publishedKeys {
		keys = append(keys, map[string]any{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		})
	}
	writeJSON(w, map[string]any{"keys": keys})
}

func (i *testIDP) writeToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	verifier := r.Form.Get("code_verifier")
	challenge := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(challenge[:])

	i.mu.Lock()
	i.lastVerifier = verifier
	i.tokenCalls++
	challengeMatches := i.challenge != "" && expectedChallenge == i.challenge
	claims := cloneClaims(i.claims)
	signingKey := i.signingKey
	kid := i.signingKeyID
	tokenErrorBody := i.tokenErrorBody
	i.mu.Unlock()

	if tokenErrorBody != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, tokenErrorBody)
		return
	}

	if !challengeMatches {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "PKCE verification failed"})
		return
	}
	if claims["iss"] == nil {
		claims["iss"] = i.issuer()
	}
	if claims["aud"] == nil {
		claims["aud"] = testClientID
	}
	if claims["iat"] == nil {
		claims["iat"] = time.Now().Add(-time.Minute).Unix()
	}
	if claims["exp"] == nil {
		claims["exp"] = time.Now().Add(5 * time.Minute).Unix()
	}
	rawIDToken, err := signJWT(signingKey, kid, claims)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"access_token": "access-token-not-used", "token_type": "Bearer", "expires_in": 300,
		"id_token": rawIDToken,
	})
}

func TestProviderExchangeValidatesSignedTokenAndPKCE(t *testing.T) {
	t.Parallel()
	idp := newTestIDP(t)
	provider := idp.provider(t)
	verifier, challenge := pkcePair()
	authorizationURL := provider.AuthCodeURL("state-1", "nonce-1", verifier)
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("state") != "state-1" || query.Get("nonce") != "nonce-1" || query.Get("code_challenge") != challenge || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization query = %v", query)
	}
	idp.setChallenge(challenge)
	idp.setClaims(map[string]any{
		"sub": "subject-1", "nonce": "nonce-1", "email": "alice@example.test", "email_verified": true,
		"groups": []string{"viewers", "dosh-operators"},
	})
	identity, err := provider.Exchange(context.Background(), "code-1", verifier, "nonce-1")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if identity.Subject != "subject-1" || identity.Username != "alice@example.test" || len(identity.Groups) != 2 {
		t.Fatalf("identity = %#v", identity)
	}
	if got := idp.verifier(); got != verifier {
		t.Fatalf("code_verifier = %q, want %q", got, verifier)
	}
}

func TestProviderRejectsInvalidTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		claims map[string]any
		mutate func(*testIDP)
	}{
		{name: "nonce mismatch", claims: map[string]any{"sub": "subject-1", "nonce": "different", "email": "alice@example.test", "email_verified": true}},
		{name: "wrong issuer", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "iss": "https://wrong.example.test", "email": "alice@example.test", "email_verified": true}},
		{name: "wrong audience", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "aud": "another-client", "email": "alice@example.test", "email_verified": true}},
		{name: "expired", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "exp": time.Now().Add(-time.Hour).Unix(), "email": "alice@example.test", "email_verified": true}},
		{name: "unverified email", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "email": "alice@example.test", "email_verified": false}},
		{name: "missing email verification", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "email": "alice@example.test"}},
		{name: "missing email claim", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "email_verified": true}},
		{name: "bad signature", claims: map[string]any{"sub": "subject-1", "nonce": "nonce-1", "email": "alice@example.test", "email_verified": true}, mutate: func(idp *testIDP) {
			idp.mu.Lock()
			defer idp.mu.Unlock()
			idp.signingKey = mustRSAKey(t)
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			idp := newTestIDP(t)
			provider := idp.provider(t)
			verifier, challenge := pkcePair()
			idp.setChallenge(challenge)
			idp.setClaims(test.claims)
			if test.mutate != nil {
				test.mutate(idp)
			}
			if _, err := provider.Exchange(context.Background(), "code-1", verifier, "nonce-1"); err == nil {
				t.Fatal("Exchange() succeeded for an invalid token")
			}
		})
	}
}

func TestProviderRefreshesJWKSForRotatedKey(t *testing.T) {
	t.Parallel()
	idp := newTestIDP(t)
	provider := idp.provider(t)
	verifier, challenge := pkcePair()
	idp.setChallenge(challenge)
	idp.setClaims(map[string]any{"sub": "subject-1", "nonce": "nonce-1", "email": "alice@example.test", "email_verified": true})
	if _, err := provider.Exchange(context.Background(), "code-1", verifier, "nonce-1"); err != nil {
		t.Fatalf("initial Exchange() error = %v", err)
	}

	rotatedKey := mustRSAKey(t)
	idp.setSigningKey("key-2", rotatedKey, []*rsa.PrivateKey{mustRSAKey(t), rotatedKey})
	if _, err := provider.Exchange(context.Background(), "code-2", verifier, "nonce-1"); err != nil {
		t.Fatalf("Exchange() after JWKS rotation error = %v", err)
	}
}

func TestProviderRejectsPKCEVerifierMismatch(t *testing.T) {
	t.Parallel()
	idp := newTestIDP(t)
	provider := idp.provider(t)
	verifier, challenge := pkcePair()
	idp.setChallenge(challenge)
	idp.setClaims(map[string]any{"sub": "subject-1", "nonce": "nonce-1", "email": "alice@example.test", "email_verified": true})
	_, otherChallenge := pkcePair()
	if _, err := provider.Exchange(context.Background(), "code-1", verifier+"-tampered", "nonce-1"); err == nil {
		t.Fatalf("Exchange() accepted a verifier whose challenge is %s instead of %s", otherChallenge, challenge)
	}
}

func TestProviderTokenErrorsDoNotLeakResponseBody(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"oauth description": `{"error":"invalid_grant","error_description":"access_token=secret-description"}`,
		"raw response":      `token=secret-raw-response`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			idp := newTestIDP(t)
			provider := idp.provider(t)
			verifier, challenge := pkcePair()
			idp.setChallenge(challenge)
			idp.setTokenErrorBody(body)
			_, err := provider.Exchange(context.Background(), "code-1", verifier, "nonce-1")
			if err == nil {
				t.Fatal("Exchange() succeeded for a token endpoint failure")
			}
			if strings.Contains(err.Error(), "secret-") {
				t.Fatalf("Exchange() error leaked token endpoint body: %v", err)
			}
		})
	}
}

func TestProviderEndSessionURL(t *testing.T) {
	t.Parallel()
	idp := newTestIDP(t)
	provider := idp.provider(t)
	got := provider.EndSessionURL("https://dosh.example.test/login")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse logout URL: %v", err)
	}
	if parsed.Path != "/logout" || parsed.Query().Get("post_logout_redirect_uri") != "https://dosh.example.test/login" {
		t.Fatalf("logout URL = %q", got)
	}
}

func TestProviderRejectsUnsafeDiscoveredEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		issuer        string
		authorization string
		token         string
		endSession    string
		wantErr       string
	}{
		{
			name: "https downgrade", issuer: "https://idp.example.test",
			authorization: "https://idp.example.test/authorize", token: "http://idp.example.test/token",
			wantErr: "downgrades HTTPS",
		},
		{
			name: "userinfo", issuer: "https://idp.example.test",
			authorization: "https://user:password@idp.example.test/authorize", token: "https://idp.example.test/token",
			wantErr: "safe absolute HTTP(S) URL",
		},
		{
			name: "fragment", issuer: "https://idp.example.test",
			authorization: "https://idp.example.test/authorize", token: "https://idp.example.test/token#fragment",
			wantErr: "safe absolute HTTP(S) URL",
		},
		{
			name: "missing token endpoint", issuer: "https://idp.example.test",
			authorization: "https://idp.example.test/authorize",
			wantErr:       "did not provide a token endpoint",
		},
		{
			name: "http test issuer allowed", issuer: "http://idp.example.test",
			authorization: "http://idp.example.test/authorize", token: "http://idp.example.test/token",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateProviderEndpoints(test.issuer, test.authorization, test.token, test.endSession)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validateProviderEndpoints() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateProviderEndpoints() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestHTTPClientTrustsConfiguredCAAndRejectsUnknownTLS(t *testing.T) {
	t.Parallel()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()

	caPath := filepath.Join(t.TempDir(), "idp-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}

	trusted, err := httpClient(Config{CAFile: caPath, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("httpClient() with CA file error = %v", err)
	}
	response, err := trusted.Get(server.URL)
	if err != nil {
		t.Fatalf("request with configured CA error = %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("request with configured CA status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	untrusted, err := httpClient(Config{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("httpClient() without CA file error = %v", err)
	}
	response, err = untrusted.Get(server.URL)
	if err == nil {
		response.Body.Close()
		t.Fatal("request without configured CA unexpectedly succeeded")
	}
}

func TestHTTPClientRejectsInvalidCAFile(t *testing.T) {
	t.Parallel()
	caPath := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(caPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write invalid CA file: %v", err)
	}
	if _, err := httpClient(Config{CAFile: caPath}); err == nil {
		t.Fatal("httpClient() accepted an invalid CA file")
	}
}

func pkcePair() (string, string) {
	verifier := strings.Repeat("a", 64)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func signJWT(key *rsa.PrivateKey, kid string, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"})
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

func cloneClaims(claims map[string]any) map[string]any {
	clone := make(map[string]any, len(claims))
	for key, value := range claims {
		clone[key] = value
	}
	return clone
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
