// Command oidctestidp runs a small, signed OpenID Connect provider for
// acceptance tests. It is intentionally separate from the dosh production
// image and must never be deployed as an application dependency.
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const authorizationCodeTTL = 5 * time.Minute

const (
	faultNonceMismatch = "nonce_mismatch"
)

type identity struct {
	Subject string
	Email   string
	Name    string
	Groups  []string
}

var testIdentities = map[string]identity{
	"viewer": {
		Subject: "e2e-viewer-subject", Email: "viewer@example.test", Name: "E2E Viewer",
		Groups: []string{"dosh-e2e-viewers", "dosh-e2e-users"},
	},
	"operator": {
		Subject: "e2e-operator-subject", Email: "operator@example.test", Name: "E2E Operator",
		Groups: []string{"dosh-e2e-operators", "dosh-e2e-users"},
	},
	"admin": {
		Subject: "e2e-admin-subject", Email: "admin@example.test", Name: "E2E Admin",
		Groups: []string{"dosh-e2e-admins", "dosh-e2e-users"},
	},
}

type authorization struct {
	Challenge string
	Nonce     string
	Redirect  string
	State     string
	Identity  string
	Fault     string
	ExpiresAt time.Time
}

type server struct {
	issuer       string
	clientID     string
	clientSecret string
	keyID        string
	key          *rsa.PrivateKey

	mu            sync.Mutex
	codes         map[string]authorization
	logoutOrigins map[string]struct{}
}

type queryValue struct {
	Name  string
	Value string
}

type loginPageData struct {
	Query      []queryValue
	Identities []string
}

var loginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>dosh E2E IdP</title>
<style>body{font:16px system-ui;max-width:44rem;margin:4rem auto;padding:0 1rem}button{font:inherit;margin:.4rem;padding:.7rem 1rem;cursor:pointer}</style>
</head><body><h1>dosh E2E IdP</h1><p>Choose an identity. This provider is for acceptance tests only.</p>
{{range .Identities}}<form method="get" action="/authorize">{{range $.Query}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}<button type="submit" name="user" value="{{.}}">{{.}}</button></form>{{end}}
</body></html>`))

func main() {
	addr := flag.String("addr", ":9443", "listen address")
	certFile := flag.String("cert", "", "TLS certificate file")
	keyFile := flag.String("key", "", "TLS private key file")
	issuer := flag.String("issuer", "", "public HTTPS issuer URL")
	clientID := flag.String("client-id", "dosh-e2e", "OIDC client ID")
	clientSecret := flag.String("client-secret", "dosh-e2e-secret", "OIDC client secret")
	flag.Parse()
	if *certFile == "" || *keyFile == "" || *issuer == "" {
		log.Fatal("-cert, -key and -issuer are required")
	}
	issuerURL, err := url.Parse(*issuer)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Host == "" {
		log.Fatal("-issuer must be an absolute https URL")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("generate RSA key: %v", err)
	}
	s := &server{
		issuer: strings.TrimSuffix(*issuer, "/"), clientID: *clientID, clientSecret: *clientSecret,
		keyID: "dosh-e2e-key", key: key, codes: make(map[string]authorization),
		logoutOrigins: make(map[string]struct{}),
	}
	log.Printf("dosh E2E IdP listening on %s with issuer %s", *addr, s.issuer)
	httpServer := &http.Server{
		Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	log.Fatal(httpServer.ListenAndServeTLS(*certFile, *keyFile))
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /jwks", s.jwks)
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("GET /logout", s.logout)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func (s *server) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.issuer,
		"authorization_endpoint":                s.issuer + "/authorize",
		"token_endpoint":                        s.issuer + "/token",
		"jwks_uri":                              s.issuer + "/jwks",
		"end_session_endpoint":                  s.issuer + "/logout",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"claims_supported":                      []string{"iss", "sub", "aud", "exp", "iat", "nonce", "email", "email_verified", "name", "preferred_username", "groups"},
	})
}

func (s *server) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.keyID,
		"n": base64.RawURLEncoding.EncodeToString(s.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes()),
	}}})
}

func (s *server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("user") == "" {
		data := loginPageData{Identities: []string{"viewer", "operator", "admin"}}
		for name := range q {
			for _, value := range q[name] {
				data.Query = append(data.Query, queryValue{Name: name, Value: value})
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := loginPage.Execute(w, data); err != nil {
			log.Printf("render login page: %v", err)
		}
		return
	}
	if q.Get("client_id") != s.clientID || q.Get("response_type") != "code" {
		http.Error(w, "invalid client or response type", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "S256 PKCE is required", http.StatusBadRequest)
		return
	}
	if _, ok := testIdentities[q.Get("user")]; !ok {
		http.Error(w, "unknown test identity", http.StatusBadRequest)
		return
	}
	fault := q.Get("fault")
	if fault != "" && fault != faultNonceMismatch {
		http.Error(w, "unsupported authorization fault", http.StatusBadRequest)
		return
	}
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Host == "" || (redirect.Scheme != "https" && redirect.Scheme != "http") {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	code, err := randomValue()
	if err != nil {
		http.Error(w, "failed to create authorization code", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.codes[code] = authorization{
		Challenge: q.Get("code_challenge"), Nonce: q.Get("nonce"), Redirect: redirect.String(),
		State: q.Get("state"), Identity: q.Get("user"), Fault: fault, ExpiresAt: time.Now().Add(authorizationCodeTTL),
	}
	s.logoutOrigins[redirect.Scheme+"://"+redirect.Host] = struct{}{}
	s.mu.Unlock()
	query := redirect.Query()
	query.Set("code", code)
	query.Set("state", q.Get("state"))
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request")
		return
	}
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID, clientSecret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	if clientID != s.clientID || clientSecret != s.clientSecret {
		w.Header().Set("WWW-Authenticate", `Basic realm="dosh-e2e"`)
		writeOAuthError(w, "invalid_client")
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		writeOAuthError(w, "unsupported_grant_type")
		return
	}
	code := r.Form.Get("code")
	s.mu.Lock()
	pending, found := s.codes[code]
	delete(s.codes, code)
	s.mu.Unlock()
	if !found || time.Now().After(pending.ExpiresAt) {
		writeOAuthError(w, "invalid_grant")
		return
	}
	verifier := r.Form.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != pending.Challenge {
		writeOAuthError(w, "invalid_grant")
		return
	}
	user, found := testIdentities[pending.Identity]
	if !found {
		writeOAuthError(w, "invalid_grant")
		return
	}
	nonce := pending.Nonce
	if pending.Fault == faultNonceMismatch {
		nonce = nonce + "-mismatch"
	}
	now := time.Now()
	idToken, err := s.signToken(map[string]any{
		"iss": s.issuer, "aud": s.clientID, "sub": user.Subject, "nonce": nonce,
		"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		"email": user.Email, "email_verified": true, "name": user.Name,
		"preferred_username": pending.Identity, "groups": user.Groups,
	})
	if err != nil {
		writeOAuthError(w, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": "dosh-e2e-access-token", "token_type": "Bearer", "expires_in": 600, "id_token": idToken,
	})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("post_logout_redirect_uri")
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		http.Error(w, "invalid post_logout_redirect_uri", http.StatusBadRequest)
		return
	}
	origin := parsed.Scheme + "://" + parsed.Host
	s.mu.Lock()
	_, allowed := s.logoutOrigins[origin]
	s.mu.Unlock()
	if !allowed {
		http.Error(w, "post_logout_redirect_uri is not registered", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *server) signToken(claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": s.keyID, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func randomValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func writeOAuthError(w http.ResponseWriter, code string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func (s *server) String() string { return fmt.Sprintf("oidctestidp(%s)", s.issuer) }
