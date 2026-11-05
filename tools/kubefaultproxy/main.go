package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type faultConfig struct {
	Method       string `json:"method"`
	PathContains string `json:"pathContains"`
	Status       int    `json:"status"`
	Remaining    int    `json:"remaining"`
	RetryAfter   string `json:"retryAfter,omitempty"`
	DelayMs      int    `json:"delayMs,omitempty"`
	Abort        bool   `json:"abort,omitempty"`
}

type statKey struct {
	Method string
	Path   string
	Status int
}

type requestStat struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Count  int    `json:"count"`
}

type faultProxy struct {
	mu     sync.Mutex
	fault  faultConfig
	counts map[statKey]int
	proxy  *httputil.ReverseProxy
}

func newFaultProxy(upstream *url.URL) *faultProxy {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // Test-only upstream to the in-cluster API server.
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = transport
	proxy.FlushInterval = -1
	item := &faultProxy{counts: make(map[statKey]int), proxy: proxy}
	proxy.ModifyResponse = func(response *http.Response) error {
		item.record(response.Request.Method, response.Request.URL.RequestURI(), response.StatusCode)
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		item.record(r.Method, r.URL.RequestURI(), http.StatusBadGateway)
		log.Printf("upstream_error method=%s path=%s error=%q", r.Method, r.URL.RequestURI(), err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	return item
}

func (p *faultProxy) takeFault(method, path string) (faultConfig, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fault := p.fault
	if fault.Remaining <= 0 {
		return faultConfig{}, false
	}
	if fault.Method != "" && !strings.EqualFold(fault.Method, method) {
		return faultConfig{}, false
	}
	if fault.PathContains != "" && !strings.Contains(path, fault.PathContains) {
		return faultConfig{}, false
	}
	p.fault.Remaining--
	return fault, true
}

func (p *faultProxy) record(method, path string, status int) {
	p.mu.Lock()
	p.counts[statKey{Method: method, Path: path, Status: status}]++
	p.mu.Unlock()
}

func (p *faultProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fault, ok := p.takeFault(r.Method, r.URL.RequestURI())
	if !ok {
		p.proxy.ServeHTTP(w, r)
		return
	}
	if fault.DelayMs > 0 {
		timer := time.NewTimer(time.Duration(fault.DelayMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			p.record(r.Method, r.URL.RequestURI(), 0)
			return
		case <-timer.C:
		}
	}
	if fault.Abort {
		p.record(r.Method, r.URL.RequestURI(), 0)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "connection abort unavailable", http.StatusInternalServerError)
			return
		}
		connection, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		_ = connection.Close()
		return
	}
	status := fault.Status
	if status < 400 || status > 599 {
		status = http.StatusServiceUnavailable
	}
	if fault.RetryAfter != "" {
		w.Header().Set("Retry-After", fault.RetryAfter)
	}
	p.record(r.Method, r.URL.RequestURI(), status)
	log.Printf("injected_fault method=%s path=%s status=%d", r.Method, r.URL.RequestURI(), status)
	writeKubernetesStatus(w, status)
}

func writeKubernetesStatus(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"kind":       "Status",
		"apiVersion": "v1",
		"metadata":   map[string]any{},
		"status":     "Failure",
		"message":    "injected Kubernetes API fault: " + http.StatusText(status),
		"reason":     http.StatusText(status),
		"code":       status,
	})
}

func (p *faultProxy) adminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			p.mu.Lock()
			fault := p.fault
			p.mu.Unlock()
			writeJSON(w, http.StatusOK, fault)
		case http.MethodPost:
			var fault faultConfig
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&fault); err != nil {
				http.Error(w, "invalid fault config: "+err.Error(), http.StatusBadRequest)
				return
			}
			if fault.Remaining < 0 || fault.DelayMs < 0 || fault.DelayMs > 60000 {
				http.Error(w, "invalid remaining or delayMs", http.StatusBadRequest)
				return
			}
			p.mu.Lock()
			p.fault = fault
			p.mu.Unlock()
			writeJSON(w, http.StatusOK, fault)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p.mu.Lock()
		stats := make([]requestStat, 0, len(p.counts))
		for key, count := range p.counts {
			stats = append(stats, requestStat{Method: key.Method, Path: key.Path, Status: key.Status, Count: count})
		}
		p.mu.Unlock()
		sort.Slice(stats, func(i, j int) bool {
			if stats[i].Method != stats[j].Method {
				return stats[i].Method < stats[j].Method
			}
			if stats[i].Path != stats[j].Path {
				return stats[i].Path < stats[j].Path
			}
			return stats[i].Status < stats[j].Status
		})
		writeJSON(w, http.StatusOK, map[string]any{"requests": stats})
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p.mu.Lock()
		p.fault = faultConfig{}
		p.counts = make(map[statKey]int)
		p.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func selfSignedCertificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "dosh-kube-fault-proxy"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func main() {
	upstreamRaw := env("UPSTREAM_URL", "https://kubernetes.default.svc:443")
	upstream, err := url.Parse(upstreamRaw)
	if err != nil || upstream.Scheme != "https" || upstream.Host == "" {
		log.Fatalf("invalid UPSTREAM_URL %q", upstreamRaw)
	}
	certificate, err := selfSignedCertificate()
	if err != nil {
		log.Fatalf("generate test certificate: %v", err)
	}
	proxy := newFaultProxy(upstream)
	proxyServer := &http.Server{
		Addr:              env("PROXY_ADDR", ":8443"),
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
			NextProtos:   []string{"http/1.1"},
		},
	}
	adminServer := &http.Server{
		Addr:              env("ADMIN_ADDR", ":8081"),
		Handler:           proxy.adminHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 2)
	go func() { errCh <- proxyServer.ListenAndServeTLS("", "") }()
	go func() { errCh <- adminServer.ListenAndServe() }()
	log.Printf("kube fault proxy started upstream=%s proxy=%s admin=%s", upstream, proxyServer.Addr, adminServer.Addr)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	select {
	case received := <-signals:
		log.Printf("received signal %s", received)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = proxyServer.Shutdown(ctx)
	_ = adminServer.Shutdown(ctx)
}
