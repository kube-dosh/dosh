package egress

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dosh/internal/platform/config"
)

func TestFactorySelectsEnvironmentProxyAndHonorsNoProxy(t *testing.T) {
	factory, err := New(config.Config{
		EgressProxyMode:       "environment",
		EnvironmentHTTPProxy:  "http://proxy.example.test:3128",
		EnvironmentHTTPSProxy: "https://proxy.example.test:8443",
		EnvironmentNoProxy:    "internal.example.test,.svc",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	transport := factory.Transport(ScopeHelm, nil)
	for _, test := range []struct {
		name string
		url  string
		want string
	}{
		{name: "http proxy", url: "http://charts.example.test/index.yaml", want: "http://proxy.example.test:3128"},
		{name: "https proxy", url: "https://charts.example.test/index.yaml", want: "https://proxy.example.test:8443"},
		{name: "no proxy", url: "https://api.internal.example.test/version", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &http.Request{URL: mustURL(t, test.url)}
			proxyURL, err := transport.Proxy(request)
			if err != nil {
				t.Fatalf("Proxy() error = %v", err)
			}
			if test.want == "" {
				if proxyURL != nil {
					t.Fatalf("Proxy() = %s, want direct", proxyURL)
				}
				return
			}
			if proxyURL == nil || proxyURL.String() != test.want {
				t.Fatalf("Proxy() = %v, want %s", proxyURL, test.want)
			}
		})
	}
}

func TestFactoryExplicitCredentialsAndFallback(t *testing.T) {
	factory, err := New(config.Config{
		EgressProxyMode:     "explicit",
		EgressHTTPSProxy:    "https://proxy.example.test:8443",
		EgressProxyUsername: "dosh-user",
		EgressProxyPassword: "super-secret",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	transport := factory.Transport(ScopeRegistry, nil)
	for _, raw := range []string{"http://registry.example.test", "https://registry.example.test"} {
		proxyURL, err := transport.Proxy(&http.Request{URL: mustURL(t, raw)})
		if err != nil {
			t.Fatalf("Proxy() error = %v", err)
		}
		if proxyURL == nil || proxyURL.Host != "proxy.example.test:8443" {
			t.Fatalf("Proxy() = %v, want fallback proxy host", proxyURL)
		}
		if proxyURL.User == nil {
			t.Fatal("Proxy() did not attach credentials")
		}
		password, _ := proxyURL.User.Password()
		if proxyURL.User.Username() != "dosh-user" || password != "super-secret" {
			t.Fatalf("Proxy() credentials = %v, want configured credentials", proxyURL.User)
		}
	}
}

func TestFactoryDirectDoesNotUseProxy(t *testing.T) {
	factory, err := New(config.Config{EgressProxyMode: "direct"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	proxyURL, err := factory.Transport(ScopeUpdate, nil).Proxy(&http.Request{URL: mustURL(t, "https://releases.example.test")})
	if err != nil || proxyURL != nil {
		t.Fatalf("Proxy() = %v, %v, want direct", proxyURL, err)
	}
	if status := factory.Status(); status.Mode != "direct" || status.Enabled {
		t.Fatalf("Status() = %#v, want disabled direct mode", status)
	}
}

func TestFactoryUpdateAffectsExistingTransport(t *testing.T) {
	factory, err := New(config.Config{EgressProxyMode: "direct"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	transport := factory.Transport(ScopeUpdate, nil)
	request := &http.Request{URL: mustURL(t, "https://releases.example.test")}
	if proxyURL, _ := transport.Proxy(request); proxyURL != nil {
		t.Fatalf("Proxy() before Update() = %v, want direct", proxyURL)
	}
	if err := factory.Update(Settings{Mode: "explicit", HTTPSProxy: "https://proxy.example.test:8443"}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("Proxy() after Update() error = %v", err)
	}
	if proxyURL == nil || proxyURL.Host != "proxy.example.test:8443" {
		t.Fatalf("Proxy() after Update() = %v, want runtime proxy", proxyURL)
	}
	factory.Reset()
	if proxyURL, _ := transport.Proxy(request); proxyURL != nil {
		t.Fatalf("Proxy() after Reset() = %v, want bootstrap direct", proxyURL)
	}
}

func TestFactoryStatusRedactsCredentials(t *testing.T) {
	factory, err := New(config.Config{
		EgressProxyMode:       "environment",
		EnvironmentHTTPSProxy: "https://proxy-user:proxy-password@proxy.example.test:8443/path?token=hidden",
		EnvironmentNoProxy:    "localhost,127.0.0.1",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	status := factory.Status()
	if !status.Enabled || !status.CredentialsConfigured {
		t.Fatalf("Status() = %#v, want enabled with credentials", status)
	}
	serialized := status.HTTPProxy + status.HTTPSProxy + status.NoProxy
	for _, secret := range []string{"proxy-user", "proxy-password", "token=hidden"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("Status() leaked %q: %#v", secret, status)
		}
	}
	if status.HTTPSProxy != "https://proxy.example.test:8443/path" {
		t.Fatalf("Status().HTTPSProxy = %q, want redacted URL", status.HTTPSProxy)
	}
}

func TestFactoryPreservesCredentialsEmbeddedInEnvironmentProxy(t *testing.T) {
	factory, err := New(config.Config{
		EgressProxyMode:       "environment",
		EnvironmentHTTPSProxy: "https://proxy-user:proxy-password@proxy.example.test:8443",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	proxyURL, err := factory.Transport(ScopeRegistry, nil).Proxy(&http.Request{URL: mustURL(t, "https://registry.example.test")})
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxyURL == nil || proxyURL.User == nil {
		t.Fatalf("Proxy() = %v, want embedded proxy credentials", proxyURL)
	}
	password, _ := proxyURL.User.Password()
	if proxyURL.User.Username() != "proxy-user" || password != "proxy-password" {
		t.Fatalf("Proxy() credentials = %v, want credentials from environment URL", proxyURL.User)
	}
}

func TestFactoryLoadsProxyCA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-ca.crt")
	if err := os.WriteFile(path, testCertificatePEM(t), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	factory, err := New(config.Config{
		EgressProxyMode:   "explicit",
		EgressHTTPSProxy:  "https://proxy.example.test:8443",
		EgressProxyCAFile: path,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !factory.Status().CAConfigured {
		t.Fatal("Status().CAConfigured = false, want true")
	}
	transport := factory.Transport(ScopeHelm, nil)
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("Transport() did not configure RootCAs")
	}
	subjects := transport.TLSClientConfig.RootCAs.Subjects()
	system, err := x509.SystemCertPool()
	if err != nil {
		t.Fatalf("SystemCertPool() error = %v", err)
	}
	if want := len(system.Subjects()) + 1; len(subjects) != want {
		t.Fatalf("RootCAs subjects = %d, want system roots plus proxy CA (%d)", len(subjects), want)
	}
}

func TestFactoryRejectsUnsafeProxyURLs(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  config.Config
	}{
		{name: "invalid scheme", cfg: config.Config{EgressProxyMode: "explicit", EgressHTTPSProxy: "socks5://proxy.example.test:1080"}},
		{name: "embedded credentials", cfg: config.Config{EgressProxyMode: "explicit", EgressHTTPSProxy: "https://user:password@proxy.example.test:8443"}},
		{name: "query string", cfg: config.Config{EgressProxyMode: "explicit", EgressHTTPSProxy: "https://proxy.example.test:8443?token=secret"}},
		{name: "unsupported mode", cfg: config.Config{EgressProxyMode: "transparent"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.cfg); err == nil {
				t.Fatal("New() error = nil, want rejection")
			}
		})
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	return parsed
}

func testCertificatePEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Dosh Egress Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
