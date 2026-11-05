package httpserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"dosh/internal/platform/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewSetsTimeoutsAndTLSPolicy(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := New(handler, config.Config{ListenAddr: "127.0.0.1:32626", TLSEnabled: true}, testLogger())
	if server.Addr != "127.0.0.1:32626" {
		t.Fatalf("Addr = %q", server.Addr)
	}
	if server.ReadHeaderTimeout != 10*time.Second || server.IdleTimeout != 2*time.Minute || server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unexpected server timeouts: read=%s idle=%s headers=%d", server.ReadHeaderTimeout, server.IdleTimeout, server.MaxHeaderBytes)
	}
	if server.TLSConfig == nil || server.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLSConfig = %#v, want TLS 1.2 minimum", server.TLSConfig)
	}

	plain := New(handler, config.Config{ListenAddr: "127.0.0.1:32626"}, testLogger())
	if plain.TLSConfig != nil {
		t.Fatalf("TLSConfig = %#v for plain HTTP server, want nil", plain.TLSConfig)
	}
}

type testCertificateProvider struct{ certificate *tls.Certificate }

func (p testCertificateProvider) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return p.certificate, nil
}

func TestUseCertificateInstallsDynamicProvider(t *testing.T) {
	server := New(http.NotFoundHandler(), config.Config{ListenAddr: "127.0.0.1:32626", TLSEnabled: true}, testLogger())
	certificate := &tls.Certificate{}
	UseCertificate(server, testCertificateProvider{certificate: certificate})
	if server.TLSConfig == nil || server.TLSConfig.GetCertificate == nil {
		t.Fatal("UseCertificate() did not install GetCertificate")
	}
	got, err := server.TLSConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil || got != certificate {
		t.Fatalf("GetCertificate() = (%#v, %v)", got, err)
	}
}

func TestRunReturnsListenAndTLSErrors(t *testing.T) {
	cfg := config.Config{ListenAddr: "127.0.0.1:not-a-port"}
	if err := Run(context.Background(), New(http.NotFoundHandler(), cfg, testLogger()), cfg, testLogger()); err == nil {
		t.Fatal("Run() accepted an invalid listen address")
	}

	cfg = config.Config{ListenAddr: "127.0.0.1:0", TLSEnabled: true, TLSCertFile: "/does/not/exist.crt", TLSKeyFile: "/does/not/exist.key"}
	if err := Run(context.Background(), New(http.NotFoundHandler(), cfg, testLogger()), cfg, testLogger()); err == nil {
		t.Fatal("Run() accepted missing TLS certificate files")
	}
}

func TestRunShutsDownOnContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}

	cfg := config.Config{ListenAddr: addr}
	server := New(http.NotFoundHandler(), cfg, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, cfg, testLogger()) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			cancel()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start on %s: %v", addr, dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

func dynamicTestCertificate(t *testing.T, serial int64) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "dynamic.dosh.test"},
		DNSNames:     []string{"dynamic.dosh.test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: template}
}

func TestRunUsesDynamicCertificateWithoutPreloadingFiles(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}

	certificate := dynamicTestCertificate(t, 42)
	cfg := config.Config{
		ListenAddr: addr, TLSEnabled: true,
		TLSCertFile: "/does/not/exist.crt", TLSKeyFile: "/does/not/exist.key",
	}
	server := New(http.NotFoundHandler(), cfg, testLogger())
	UseCertificate(server, testCertificateProvider{certificate: &certificate})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, cfg, testLogger()) }()

	deadline := time.Now().Add(2 * time.Second)
	var connection *tls.Conn
	for {
		dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
		connection, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TLS server did not start with dynamic certificate: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	peer := connection.ConnectionState().PeerCertificates[0]
	_ = connection.Close()
	if peer.Subject.CommonName != "dynamic.dosh.test" || peer.SerialNumber.Int64() != 42 {
		t.Fatalf("served certificate = %s serial=%s", peer.Subject.CommonName, peer.SerialNumber)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
