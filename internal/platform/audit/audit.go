package audit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/requestid"
)

type Config struct {
	WebhookURL   string
	BearerToken  string
	HMACKey      []byte
	CAFile       string
	Timeout      time.Duration
	QueueSize    int
	HistoryLimit int
}

type Logger struct {
	log     *slog.Logger
	sink    *webhookSink
	history *historyStore
}

type delivery struct {
	body      []byte
	requestID string
}

type webhookSink struct {
	config  Config
	client  *http.Client
	queue   chan delivery
	pending atomic.Int64
	mu      sync.Mutex
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func New(logger *slog.Logger) *Logger {
	return &Logger{log: logger, history: newHistoryStore(defaultHistoryLimit)}
}

func NewWithConfig(logger *slog.Logger, cfg Config) (*Logger, error) {
	result := &Logger{log: logger, history: newHistoryStore(cfg.HistoryLimit)}
	if cfg.WebhookURL == "" {
		return result, nil
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read audit webhook CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("audit webhook CA file %q contains no valid certificates", cfg.CAFile)
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	sink := &webhookSink{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout, Transport: transport},
		queue:  make(chan delivery, cfg.QueueSize),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	result.sink = sink
	go sink.run(runCtx, logger)
	return result, nil
}

func (l *Logger) Record(ctx context.Context, actor domain.Actor, event domain.AuditEvent) {
	requestID := requestid.From(ctx)
	record := domain.AuditRecord{
		Timestamp: time.Now().UTC(), User: actor.Username, Groups: actor.Groups,
		Action: event.Action, Resource: event.Resource, Namespace: event.Namespace,
		Name: event.Name, Result: event.Result, RequestID: requestID,
	}
	if event.Err != nil {
		record.Error = event.Err.Error()
	}
	l.history.append(record)
	fields := []any{
		"audit", true,
		"user", actor.Username,
		"groups", actor.Groups,
		"action", event.Action,
		"resource", event.Resource,
		"namespace", event.Namespace,
		"name", event.Name,
		"result", event.Result,
		"request_id", requestID,
	}
	if event.Err != nil {
		fields = append(fields, "error", event.Err.Error())
		l.log.Error("audit_event", fields...)
	} else {
		l.log.Info("audit_event", fields...)
	}
	if l.sink == nil {
		return
	}
	payload := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"user":      actor.Username,
		"groups":    actor.Groups,
		"action":    event.Action,
		"resource":  event.Resource,
		"namespace": event.Namespace,
		"name":      event.Name,
		"result":    event.Result,
		"requestId": requestID,
	}
	if event.Err != nil {
		payload["error"] = event.Err.Error()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		l.log.Error("audit_webhook_encode_failed", "request_id", requestID, "error", err)
		return
	}
	switch l.sink.enqueue(delivery{body: body, requestID: requestID}) {
	case enqueueOK:
	case enqueueClosed:
		l.log.Error("audit_webhook_closed", "request_id", requestID)
	case enqueueFull:
		l.log.Error("audit_webhook_queue_full", "request_id", requestID)
	}
}

// ListHistory returns a newest-first snapshot of audit records retained by
// this process. The snapshot is intentionally process-local; durable audit
// retention is provided by stdout collection or the configured webhook sink.
func (l *Logger) ListHistory(ctx context.Context, query domain.AuditQuery) ([]domain.AuditRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.history.list(query), nil
}

// Flush waits until all currently queued audit records have been delivered or
// the supplied context expires. It is used by shutdown and integration tests.
func (l *Logger) Flush(ctx context.Context) error {
	if l.sink == nil {
		return nil
	}
	return l.sink.waitPending(ctx)
}

// Close stops accepting webhook records, drains the currently queued records,
// and then stops the worker. It must be called after the HTTP server has
// stopped accepting new requests.
func (l *Logger) Close(ctx context.Context) error {
	if l.sink == nil {
		return nil
	}
	return l.sink.close(ctx)
}

type enqueueResult int

const (
	enqueueOK enqueueResult = iota
	enqueueFull
	enqueueClosed
)

func (s *webhookSink) enqueue(item delivery) enqueueResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return enqueueClosed
	}
	s.pending.Add(1)
	select {
	case s.queue <- item:
		return enqueueOK
	default:
		s.pending.Add(-1)
		return enqueueFull
	}
}

func (s *webhookSink) waitPending(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.pending.Load() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *webhookSink) close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		<-s.done
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	flushErr := s.waitPending(ctx)
	s.cancel()
	<-s.done
	return flushErr
}

func (s *webhookSink) run(ctx context.Context, logger *slog.Logger) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-s.queue:
			if err := s.deliver(ctx, item); err != nil {
				logger.Error("audit_webhook_delivery_failed", "request_id", item.requestID, "error", err)
			}
			s.pending.Add(-1)
		}
	}
}

func (s *webhookSink) deliver(ctx context.Context, item delivery) error {
	backoff := 200 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := s.post(ctx, item); err != nil {
			lastErr = err
			if attempt == 3 {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}
		return nil
	}
	return lastErr
}

func (s *webhookSink) post(parent context.Context, item delivery) error {
	ctx, cancel := context.WithTimeout(parent, s.config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.WebhookURL, bytes.NewReader(item.body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dosh-audit/1")
	req.Header.Set("X-Request-ID", item.requestID)
	if s.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.BearerToken)
	}
	if len(s.config.HMACKey) > 0 {
		timestamp := time.Now().UTC().Format(time.RFC3339Nano)
		mac := hmac.New(sha256.New, s.config.HMACKey)
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write([]byte("."))
		_, _ = mac.Write(item.body)
		req.Header.Set("X-Dosh-Timestamp", timestamp)
		req.Header.Set("X-Dosh-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("audit webhook returned status %d", resp.StatusCode)
	}
	return nil
}
