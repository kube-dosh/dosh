package kube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRetryAttempts = 3
	defaultRetryDelay    = 100 * time.Millisecond
	defaultRetryMaxDelay = 2 * time.Second
)

// retryTransport adds bounded retries for idempotent Kubernetes API requests.
// It deliberately does not retry writes: a lost response after a successful
// mutation must not be replayed automatically.
type retryTransport struct {
	base      http.RoundTripper
	attempts  int
	baseDelay time.Duration
	maxDelay  time.Duration
}

func (t *retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.base == nil {
		return nil, errors.New("retry transport has no base transport")
	}
	attempts := t.attempts
	if attempts <= 0 {
		attempts = defaultRetryAttempts
	}
	if !retryableMethod(request.Method) {
		return t.base.RoundTrip(request)
	}
	baseDelay := t.baseDelay
	if baseDelay < 0 {
		baseDelay = 0
	}
	if baseDelay == 0 {
		baseDelay = defaultRetryDelay
	}
	maxDelay := t.maxDelay
	if maxDelay <= 0 {
		maxDelay = defaultRetryMaxDelay
	}

	var lastResponse *http.Response
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptRequest := request.Clone(request.Context())
		if attempt > 0 && request.Body != nil && request.Body != http.NoBody {
			if request.GetBody == nil {
				return nil, errors.New("cannot retry request with a non-replayable body")
			}
			body, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			attemptRequest.Body = body
		}
		response, err := t.base.RoundTrip(attemptRequest)
		lastResponse, lastErr = response, err
		if err == nil && !retryableStatus(response.StatusCode) {
			return response, nil
		}
		if err != nil && !retryableError(err) {
			return nil, err
		}
		if attempt == attempts-1 {
			return response, err
		}
		if response != nil && response.Body != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		delay := retryDelay(attempt, baseDelay, maxDelay)
		if response != nil {
			delay = retryAfterDelay(response.Header.Get("Retry-After"), delay, maxDelay)
		}
		if err := waitForRetry(request.Context(), delay); err != nil {
			return nil, err
		}
	}
	return lastResponse, lastErr
}

func retryableMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableError(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func retryDelay(attempt int, base, max time.Duration) time.Duration {
	delay := base
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	if delay > max {
		return max
	}
	return delay
}

func retryAfterDelay(raw string, fallback, max time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > max {
			return max
		}
		return delay
	}
	if at, err := http.ParseTime(raw); err == nil {
		delay := time.Until(at)
		if delay < 0 {
			return 0
		}
		if delay > max {
			return max
		}
		return delay
	}
	return fallback
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
