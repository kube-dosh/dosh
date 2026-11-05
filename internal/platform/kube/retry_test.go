package kube

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func responseWithStatus(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestRetryTransportRetriesIdempotent429And5xx(t *testing.T) {
	t.Parallel()
	attempts := 0
	transport := &retryTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			switch attempts {
			case 1:
				return responseWithStatus(http.StatusTooManyRequests), nil
			case 2:
				return responseWithStatus(http.StatusServiceUnavailable), nil
			default:
				return responseWithStatus(http.StatusOK), nil
			}
		}),
		attempts:  3,
		baseDelay: time.Nanosecond,
		maxDelay:  time.Millisecond,
	}
	response, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://kubernetes.example.test/api", nil))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if response.StatusCode != http.StatusOK || attempts != 3 {
		t.Fatalf("status = %d, attempts = %d", response.StatusCode, attempts)
	}
}

func TestRetryTransportDoesNotRetryWrites(t *testing.T) {
	t.Parallel()
	attempts := 0
	transport := &retryTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return responseWithStatus(http.StatusTooManyRequests), nil
		}),
		attempts:  3,
		baseDelay: time.Nanosecond,
	}
	response, err := transport.RoundTrip(httptest.NewRequest(http.MethodPost, "https://kubernetes.example.test/api", strings.NewReader("{}")))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if response.StatusCode != http.StatusTooManyRequests || attempts != 1 {
		t.Fatalf("status = %d, attempts = %d", response.StatusCode, attempts)
	}
}

func TestRetryTransportStopsAtAttemptLimit(t *testing.T) {
	t.Parallel()
	attempts := 0
	transport := &retryTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return responseWithStatus(http.StatusServiceUnavailable), nil
		}),
		attempts:  3,
		baseDelay: time.Nanosecond,
		maxDelay:  time.Millisecond,
	}
	response, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://kubernetes.example.test/api", nil))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || attempts != 3 {
		t.Fatalf("status = %d, attempts = %d", response.StatusCode, attempts)
	}
}

func TestRetryTransportHonorsContextDuringBackoff(t *testing.T) {
	t.Parallel()
	attempts := 0
	transport := &retryTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return responseWithStatus(http.StatusTooManyRequests), nil
		}),
		attempts:  3,
		baseDelay: time.Hour,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	started := time.Now()
	response, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://kubernetes.example.test/api", nil).WithContext(ctx))
	if response != nil || err == nil || attempts != 1 {
		t.Fatalf("response = %#v, error = %v, attempts = %d", response, err, attempts)
	}
	if time.Since(started) > time.Second {
		t.Fatal("retry transport did not respect context cancellation")
	}
}

func TestRetryAfterDelayIsBounded(t *testing.T) {
	t.Parallel()
	if got := retryAfterDelay("1", time.Millisecond, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("retryAfterDelay(1) = %s", got)
	}
	if got := retryAfterDelay("invalid", 250*time.Millisecond, time.Second); got != 250*time.Millisecond {
		t.Fatalf("retryAfterDelay(invalid) = %s", got)
	}
}
