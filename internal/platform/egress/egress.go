// Package egress centralizes every outbound HTTP(S) client used by Dosh.
// Callers select a logical scope, but proxy policy, credentials, CA trust and
// transport defaults are owned here so new integrations cannot silently bypass
// the administrator's network configuration.
package egress

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dosh/internal/platform/config"

	"golang.org/x/net/http/httpproxy"
)

type Scope string

const (
	ScopeHelm     Scope = "helm"
	ScopeRegistry Scope = "registry"
	ScopeOIDC     Scope = "oidc"
	ScopeAudit    Scope = "audit"
	ScopeUpdate   Scope = "update"
)

const (
	SourceBootstrap = "bootstrap"
	SourceRuntime   = "runtime"
)

type Status struct {
	Mode                  string `json:"mode"`
	Enabled               bool   `json:"enabled"`
	HTTPProxy             string `json:"httpProxy,omitempty"`
	HTTPSProxy            string `json:"httpsProxy,omitempty"`
	NoProxy               string `json:"noProxy,omitempty"`
	CredentialsConfigured bool   `json:"credentialsConfigured"`
	CAConfigured          bool   `json:"caConfigured"`
	Source                string `json:"source"`
}

// Settings contains the complete egress policy, including credentials and CA
// material. It may only be persisted in Dosh's protected shared data directory
// and must never be serialized directly into an HTTP response or log entry.
type Settings struct {
	Mode       string
	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string
	Username   string
	Password   string
	CAPEM      []byte
}

type state struct {
	generation            uint64
	mode                  string
	source                string
	proxyFunc             func(*http.Request) (*url.URL, error)
	httpProxy             string
	httpsProxy            string
	noProxy               string
	username              string
	password              string
	credentialsConfigured bool
	proxyCAPEM            []byte
	caConfigured          bool
}

type Factory struct {
	bootstrap  state
	current    atomic.Pointer[state]
	generation atomic.Uint64
}

func New(cfg config.Config) (*Factory, error) {
	settings := Settings{
		Mode:       cfg.EgressProxyMode,
		HTTPProxy:  cfg.EgressHTTPProxy,
		HTTPSProxy: cfg.EgressHTTPSProxy,
		NoProxy:    cfg.EgressNoProxy,
		Username:   cfg.EgressProxyUsername,
		Password:   cfg.EgressProxyPassword,
	}
	if strings.EqualFold(strings.TrimSpace(settings.Mode), "environment") || strings.TrimSpace(settings.Mode) == "" {
		settings.HTTPProxy = cfg.EnvironmentHTTPProxy
		settings.HTTPSProxy = cfg.EnvironmentHTTPSProxy
		settings.NoProxy = cfg.EnvironmentNoProxy
	}
	if cfg.EgressProxyCAFile != "" {
		caPEM, err := loadCAPEM(cfg.EgressProxyCAFile)
		if err != nil {
			return nil, err
		}
		settings.CAPEM = caPEM
	}
	return NewFromSettings(settings)
}

func NewFromSettings(settings Settings) (*Factory, error) {
	st, err := buildState(settings, SourceBootstrap, 1)
	if err != nil {
		return nil, err
	}
	factory := &Factory{bootstrap: st}
	factory.generation.Store(st.generation)
	factory.current.Store(&st)
	return factory, nil
}

func NewDirect() *Factory {
	factory, err := NewFromSettings(Settings{Mode: "direct"})
	if err != nil {
		panic(err)
	}
	return factory
}

// Update atomically applies a runtime override. Existing clients and their
// transports observe the change on their next request without a process restart.
func (f *Factory) Update(settings Settings) error {
	if f == nil {
		return errors.New("egress factory is unavailable")
	}
	st, err := buildState(settings, SourceRuntime, f.generation.Add(1))
	if err != nil {
		return err
	}
	f.current.Store(&st)
	return nil
}

// Reset removes the runtime override and returns to the startup configuration.
func (f *Factory) Reset() {
	if f == nil {
		return
	}
	st := f.bootstrap
	st.generation = f.generation.Add(1)
	f.current.Store(&st)
}

func (f *Factory) Current() Settings {
	st := f.load()
	return Settings{
		Mode: st.mode, HTTPProxy: st.httpProxy, HTTPSProxy: st.httpsProxy, NoProxy: st.noProxy,
		Username: st.username, Password: st.password, CAPEM: append([]byte(nil), st.proxyCAPEM...),
	}
}

func (f *Factory) Client(scope Scope, timeout time.Duration, baseTLS *tls.Config) *http.Client {
	return &http.Client{Timeout: timeout, Transport: f.Transport(scope, baseTLS)}
}

// Transport returns a transport whose proxy and TLS settings follow runtime
// egress updates. Callers may still tune the embedded template transport before
// its first request; those options are copied into each generated transport.
func (f *Factory) Transport(_ Scope, baseTLS *tls.Config) *Transport {
	st := f.load()
	template := http.DefaultTransport.(*http.Transport).Clone()
	template.Proxy = func(request *http.Request) (*url.URL, error) {
		st := f.load()
		if st.proxyFunc == nil {
			return nil, nil
		}
		return st.proxyFunc(request)
	}
	template.TLSClientConfig = st.tlsConfig(baseTLS)
	return &Transport{Transport: template, factory: f, baseTLS: baseTLS, generation: st.generation}
}

func (f *Factory) Status() Status {
	if f == nil {
		return Status{Mode: "direct", Source: SourceBootstrap}
	}
	st := f.load()
	return Status{
		Mode:                  st.mode,
		Enabled:               st.mode != "direct" && (st.httpProxy != "" || st.httpsProxy != ""),
		HTTPProxy:             redactURL(st.httpProxy),
		HTTPSProxy:            redactURL(st.httpsProxy),
		NoProxy:               st.noProxy,
		CredentialsConfigured: st.credentialsConfigured,
		CAConfigured:          st.caConfigured,
		Source:                st.source,
	}
}

func (f *Factory) load() *state {
	if f == nil {
		return directState(SourceBootstrap)
	}
	st := f.current.Load()
	if st == nil {
		return directState(SourceBootstrap)
	}
	return st
}

func directState(source string) *state { return &state{mode: "direct", source: source, generation: 1} }

func buildState(settings Settings, source string, generation uint64) (state, error) {
	mode := strings.ToLower(strings.TrimSpace(settings.Mode))
	if mode == "" {
		mode = "environment"
	}
	st := state{
		generation: generation,
		mode:       mode,
		source:     source,
		username:   settings.Username,
		password:   settings.Password,
	}
	var proxyConfig *httpproxy.Config
	injectCredentials := settings.Username != "" || settings.Password != ""
	if injectCredentials && (settings.Username == "" || settings.Password == "") {
		return state{}, errors.New("egress proxy username and password must be configured together")
	}
	switch mode {
	case "direct":
		// Explicitly no proxy function.
	case "environment", "explicit":
		httpProxy, httpsProxy, err := proxyFallback(settings.HTTPProxy, settings.HTTPSProxy, mode == "explicit")
		if err != nil {
			return state{}, err
		}
		if mode == "explicit" && httpProxy == "" && httpsProxy == "" {
			return state{}, errors.New("explicit egress mode requires an HTTP or HTTPS proxy URL")
		}
		proxyConfig = &httpproxy.Config{HTTPProxy: httpProxy, HTTPSProxy: httpsProxy, NoProxy: settings.NoProxy}
		st.httpProxy = httpProxy
		st.httpsProxy = httpsProxy
		st.noProxy = settings.NoProxy
	default:
		return state{}, fmt.Errorf("unsupported egress proxy mode %q", mode)
	}
	if proxyConfig != nil {
		baseProxy := proxyConfig.ProxyFunc()
		st.proxyFunc = func(request *http.Request) (*url.URL, error) {
			proxyURL, err := baseProxy(request.URL)
			if err != nil || proxyURL == nil || !injectCredentials {
				return proxyURL, err
			}
			withCredentials := *proxyURL
			withCredentials.User = url.UserPassword(settings.Username, settings.Password)
			return &withCredentials, nil
		}
		st.credentialsConfigured = injectCredentials || hasURLUserInfo(st.httpProxy) || hasURLUserInfo(st.httpsProxy)
	}
	if len(settings.CAPEM) > 0 {
		check := x509.NewCertPool()
		if !check.AppendCertsFromPEM(settings.CAPEM) {
			return state{}, errors.New("egress proxy CA contains no valid certificates")
		}
		st.proxyCAPEM = append([]byte(nil), settings.CAPEM...)
		st.caConfigured = true
	}
	return st, nil
}

func proxyFallback(httpProxy, httpsProxy string, strict bool) (string, string, error) {
	httpProxy = strings.TrimSpace(httpProxy)
	httpsProxy = strings.TrimSpace(httpsProxy)
	if httpProxy == "" && httpsProxy != "" {
		httpProxy = httpsProxy
	}
	if httpsProxy == "" && httpProxy != "" {
		httpsProxy = httpProxy
	}
	for _, raw := range []string{httpProxy, httpsProxy} {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", "", fmt.Errorf("egress proxy must be an absolute http or https URL")
		}
		if strict && (parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "") {
			return "", "", errors.New("egress proxy URL must not contain user information, a query, or a fragment")
		}
	}
	return httpProxy, httpsProxy, nil
}

// Transport is an http.RoundTripper that swaps its underlying transport when
// the egress generation changes, so long-lived clients also honor runtime edits.
type Transport struct {
	*http.Transport
	factory    *Factory
	baseTLS    *tls.Config
	mu         sync.Mutex
	generation uint64
	active     *http.Transport
}

func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.roundTripper().RoundTrip(request)
}

func (t *Transport) CloseIdleConnections() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active != nil {
		t.active.CloseIdleConnections()
	}
	t.Transport.CloseIdleConnections()
}

func (t *Transport) roundTripper() *http.Transport {
	st := t.factory.load()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active == nil || t.generation != st.generation {
		if t.active != nil {
			t.active.CloseIdleConnections()
		}
		active := t.Transport.Clone()
		active.Proxy = st.proxyFunc
		active.TLSClientConfig = st.tlsConfig(t.baseTLS)
		t.active = active
		t.generation = st.generation
	}
	return t.active
}

func (s *state) tlsConfig(base *tls.Config) *tls.Config {
	var result *tls.Config
	if base != nil {
		result = base.Clone()
	} else {
		result = &tls.Config{}
	}
	if result.MinVersion == 0 {
		result.MinVersion = tls.VersionTLS12
	}
	if len(s.proxyCAPEM) > 0 {
		roots := result.RootCAs
		if roots != nil {
			roots = roots.Clone()
		} else if system, err := x509.SystemCertPool(); err == nil && system != nil {
			roots = system
		} else {
			roots = x509.NewCertPool()
		}
		roots.AppendCertsFromPEM(s.proxyCAPEM)
		result.RootCAs = roots
	}
	return result
}

func loadCAPEM(path string) ([]byte, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read egress proxy CA file: %w", err)
	}
	check := x509.NewCertPool()
	if !check.AppendCertsFromPEM(pem) {
		return nil, errors.New("egress proxy CA file contains no valid certificates")
	}
	return pem, nil
}

func redactURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid]"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func hasURLUserInfo(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.User != nil
}
