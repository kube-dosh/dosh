package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"time"

	"dosh/internal/platform/config"
)

// CertificateProvider supplies the active server certificate for each TLS
// handshake. Implementations must return immutable certificate values.
type CertificateProvider interface {
	GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

func New(handler stdhttp.Handler, cfg config.Config, logger *slog.Logger) *stdhttp.Server {
	server := &stdhttp.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	if cfg.TLSEnabled {
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return server
}

// UseCertificate makes an HTTP server serve certificates from a dynamic
// provider. It must be called before Run starts serving requests.
func UseCertificate(server *stdhttp.Server, provider CertificateProvider) {
	if provider == nil {
		return
	}
	if server.TLSConfig == nil {
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	server.TLSConfig.GetCertificate = provider.GetCertificate
}

func Run(ctx context.Context, server *stdhttp.Server, cfg config.Config, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server_started", "addr", cfg.ListenAddr, "tls", cfg.TLSEnabled)
		var err error
		if cfg.TLSEnabled {
			certFile, keyFile := cfg.TLSCertFile, cfg.TLSKeyFile
			if server.TLSConfig != nil && server.TLSConfig.GetCertificate != nil {
				// A dynamic provider owns certificate loading; passing file paths here
				// would make net/http preload a second, non-reloadable certificate.
				certFile, keyFile = "", ""
			}
			err = server.ListenAndServeTLS(certFile, keyFile)
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
