package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dosh/internal/domain"
)

func writeCertificatePair(t *testing.T, directory string, serial int64, notBefore, notAfter time.Time) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "dosh.example.test"},
		DNSNames:     []string{"dosh.example.test"},
		IPAddresses:  []net.IP{net.ParseIP("192.168.122.100")},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	certFile := filepath.Join(directory, "tls.crt")
	keyFile := filepath.Join(directory, "tls.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return certFile, keyFile
}

func TestStoreLoadsAndReportsCertificateMetadata(t *testing.T) {
	directory := t.TempDir()
	certFile, keyFile := writeCertificatePair(t, directory, 7, time.Now().Add(-time.Hour), time.Now().Add(90*24*time.Hour))
	store, err := New(certFile, keyFile, 30*time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Configured || status.Status != "valid" || status.KeyAlgorithm != "ECDSA" {
		t.Fatalf("status = %#v", status)
	}
	if len(status.DNSNames) != 1 || len(status.IPAddresses) != 1 || status.DNSNames[0] != "dosh.example.test" || status.IPAddresses[0] != "192.168.122.100" {
		t.Fatalf("SANs = %#v %#v", status.DNSNames, status.IPAddresses)
	}
	if len(status.FingerprintSHA256) != 64 || !status.AutoReload || status.ReloadIntervalSeconds != 30 || status.DaysRemaining < 89 {
		t.Fatalf("cached metadata = %#v", status)
	}
	certificate, err := store.GetCertificate(nil)
	if err != nil || certificate == nil || certificate.Leaf == nil {
		t.Fatalf("GetCertificate() = (%#v, %v)", certificate, err)
	}
}

func TestStoreReloadSwapsCertificateAndKeepsLastGoodOnFailure(t *testing.T) {
	directory := t.TempDir()
	certFile, keyFile := writeCertificatePair(t, directory, 1, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	store, err := New(certFile, keyFile, 0)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first, _ := store.Status(context.Background())

	writeCertificatePair(t, directory, 2, time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour))
	second, err := store.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if second.FingerprintSHA256 == first.FingerprintSHA256 {
		t.Fatal("reload did not replace the certificate")
	}

	if err := os.WriteFile(certFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write invalid certificate: %v", err)
	}
	if _, err := store.Reload(context.Background()); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Reload() error = %v, want ErrInvalid", err)
	}
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.FingerprintSHA256 != second.FingerprintSHA256 || status.LastReloadError == "" {
		t.Fatalf("status after failed reload = %#v", status)
	}
	if certificate, err := store.GetCertificate(nil); err != nil || certificate == nil {
		t.Fatalf("last known-good certificate was not retained: (%#v, %v)", certificate, err)
	}
}

func TestStoreRunReloadsChangedFiles(t *testing.T) {
	directory := t.TempDir()
	certFile, keyFile := writeCertificatePair(t, directory, 1, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	store, err := New(certFile, keyFile, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	initial, _ := store.Status(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	writeCertificatePair(t, directory, 2, time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour))
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, statusErr := store.Status(context.Background())
		if statusErr != nil {
			t.Fatalf("Status() error = %v", statusErr)
		}
		if status.FingerprintSHA256 != initial.FingerprintSHA256 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic reload did not observe changed certificate files")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}

func TestStoreRejectsInvalidInitialCertificate(t *testing.T) {
	directory := t.TempDir()
	certFile := filepath.Join(directory, "tls.crt")
	keyFile := filepath.Join(directory, "tls.key")
	if err := os.WriteFile(certFile, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if _, err := New(certFile, keyFile, 0); err == nil {
		t.Fatal("New() accepted invalid certificate material")
	}
}

func TestStoreRecomputesExpiryWhileRunning(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	certFile, keyFile := writeCertificatePair(t, directory, 11, now.Add(-time.Hour), now.Add(time.Hour))
	store, err := New(certFile, keyFile, 0)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	store.now = func() time.Time { return now.Add(2 * time.Hour) }
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Status != "expired" || status.DaysRemaining != 0 {
		t.Fatalf("status = %#v, want expired", status)
	}
}
