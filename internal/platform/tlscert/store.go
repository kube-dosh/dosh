package tlscert

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dosh/internal/domain"
)

const expiringWindow = 30 * 24 * time.Hour

type loadedState struct {
	certificate *tls.Certificate
	status      domain.ServerCertificate
}

// Store loads the Dosh server certificate and atomically swaps it when the
// mounted certificate files change. The private key is never included in
// status responses, logs, or audit events.
type Store struct {
	certFile       string
	keyFile        string
	reloadInterval time.Duration
	now            func() time.Time

	current         atomic.Pointer[loadedState]
	mu              sync.Mutex
	observed        [sha256.Size]byte
	hasObserved     bool
	lastReloadedAt  time.Time
	lastReloadError string
}

func New(certFile, keyFile string, reloadInterval time.Duration) (*Store, error) {
	store := &Store{
		certFile: certFile, keyFile: keyFile, reloadInterval: reloadInterval,
		now: time.Now,
	}
	pair, leaf, digest, err := loadPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	store.observed = digest
	store.hasObserved = true
	store.lastReloadedAt = store.now().UTC()
	store.current.Store(&loadedState{
		certificate: pair,
		status:      statusFromCertificate(leaf, store.now(), reloadInterval),
	})
	return store, nil
}

// GetCertificate implements tls.Config.GetCertificate without file I/O on the
// handshake path. The returned certificate is immutable after publication.
func (s *Store) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	current := s.current.Load()
	if current == nil || current.certificate == nil {
		return nil, fmt.Errorf("TLS certificate is unavailable")
	}
	return current.certificate, nil
}

func (s *Store) Status(ctx context.Context) (domain.ServerCertificate, error) {
	if err := ctx.Err(); err != nil {
		return domain.ServerCertificate{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(), nil
}

func (s *Store) Reload(ctx context.Context) (domain.ServerCertificate, error) {
	if err := ctx.Err(); err != nil {
		return domain.ServerCertificate{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status, _, err := s.refreshLocked(true)
	return status, err
}

// Run polls for updated certificate files until ctx is canceled. A failed
// reload keeps the last known-good certificate active and retries on the next
// interval.
func (s *Store) Run(ctx context.Context, logger *slog.Logger) {
	if s.reloadInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.reloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			status, changed, err := s.refreshLocked(false)
			s.mu.Unlock()
			if err != nil {
				logger.Error("tls_certificate_reload_failed", "error", err)
				continue
			}
			if changed {
				logger.Info("tls_certificate_reloaded", "not_after", status.NotAfter, "fingerprint", status.FingerprintSHA256)
			}
		}
	}
}

func (s *Store) refreshLocked(force bool) (domain.ServerCertificate, bool, error) {
	pair, leaf, digest, err := loadPair(s.certFile, s.keyFile)
	if err != nil {
		s.lastReloadError = cleanError(err)
		return s.statusLocked(), false, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if !force && s.hasObserved && digest == s.observed {
		s.lastReloadError = ""
		return s.statusLocked(), false, nil
	}
	status := statusFromCertificate(leaf, s.now(), s.reloadInterval)
	s.current.Store(&loadedState{certificate: pair, status: status})
	s.observed = digest
	s.hasObserved = true
	s.lastReloadedAt = s.now().UTC()
	s.lastReloadError = ""
	return s.statusLocked(), true, nil
}

func (s *Store) statusLocked() domain.ServerCertificate {
	current := s.current.Load()
	if current == nil {
		return domain.ServerCertificate{
			Mode: "https", Enabled: true, Status: "unavailable",
			AutoReload: s.reloadInterval > 0, ReloadIntervalSeconds: intervalSeconds(s.reloadInterval),
			LastReloadError: s.lastReloadError,
		}
	}
	status := current.status
	if current.certificate != nil && current.certificate.Leaf != nil {
		// Recompute expiry state on every read so a certificate that expires
		// while the process is running does not remain reported as valid.
		status = statusFromCertificate(current.certificate.Leaf, s.now(), s.reloadInterval)
	}
	status.DNSNames = append([]string(nil), status.DNSNames...)
	status.IPAddresses = append([]string(nil), status.IPAddresses...)
	status.LastReloadedAt = s.lastReloadedAt
	status.LastReloadError = s.lastReloadError
	status.AutoReload = s.reloadInterval > 0
	status.ReloadIntervalSeconds = intervalSeconds(s.reloadInterval)
	return status
}

func loadPair(certFile, keyFile string) (*tls.Certificate, *x509.Certificate, [sha256.Size]byte, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("read certificate file: %w", err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("read private key file: %w", err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("parse certificate and private key pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, [sha256.Size]byte{}, fmt.Errorf("parse leaf certificate: %w", err)
	}
	pair.Leaf = leaf

	digest := sha256.New()
	_, _ = digest.Write(certPEM)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(keyPEM)
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return &pair, leaf, sum, nil
}

func statusFromCertificate(leaf *x509.Certificate, now time.Time, reloadInterval time.Duration) domain.ServerCertificate {
	status := "valid"
	switch {
	case now.Before(leaf.NotBefore):
		status = "not_yet_valid"
	case !now.Before(leaf.NotAfter):
		status = "expired"
	case leaf.NotAfter.Sub(now) <= expiringWindow:
		status = "expiring"
	}
	remaining := int(math.Ceil(leaf.NotAfter.Sub(now).Hours() / 24))
	if remaining < 0 {
		remaining = 0
	}
	fingerprint := sha256.Sum256(leaf.Raw)
	return domain.ServerCertificate{
		Mode: "https", Enabled: true, Configured: true, Status: status,
		Subject: leaf.Subject.String(), Issuer: leaf.Issuer.String(), SerialNumber: leaf.SerialNumber.Text(16),
		DNSNames: append([]string(nil), leaf.DNSNames...), IPAddresses: ipStrings(leaf),
		NotBefore: leaf.NotBefore.UTC(), NotAfter: leaf.NotAfter.UTC(), DaysRemaining: remaining,
		FingerprintSHA256: fmt.Sprintf("%X", fingerprint[:]), KeyAlgorithm: leaf.PublicKeyAlgorithm.String(),
		AutoReload: reloadInterval > 0, ReloadIntervalSeconds: intervalSeconds(reloadInterval),
	}
}

func ipStrings(leaf *x509.Certificate) []string {
	result := make([]string, 0, len(leaf.IPAddresses))
	for _, address := range leaf.IPAddresses {
		result = append(result, address.String())
	}
	return result
}

func intervalSeconds(interval time.Duration) int {
	if interval <= 0 {
		return 0
	}
	return int(interval / time.Second)
}

func cleanError(err error) string {
	value := strings.Join(strings.Fields(err.Error()), " ")
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}
