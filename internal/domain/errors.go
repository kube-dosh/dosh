package domain

import "errors"

var (
	ErrNotFound      = errors.New("resource not found")
	ErrConflict      = errors.New("resource version conflict")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrForbidden     = errors.New("operation forbidden")
	ErrUnauthorized  = errors.New("authentication required")
	ErrInvalid       = errors.New("invalid request")
	ErrReadOnly      = errors.New("dashboard is read-only")
	ErrUnavailable   = errors.New("dependency unavailable")
	ErrUnsupported   = errors.New("operation not supported")
	ErrRateLimited   = errors.New("too many requests")
	ErrStaleCursor   = errors.New("pagination cursor expired")
)

// ValidationError carries a stable field-level reason for HTTP adapters.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

func (e *ValidationError) Unwrap() error { return ErrInvalid }

// PublicError carries a safe, actionable message for API clients while keeping
// the underlying cause available for logging and errors.Is/errors.As checks.
// Callers must only use it for sanitized information that is safe to expose.
type PublicError struct {
	Cause   error
	Message string
}

func (e *PublicError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return ""
}

func (e *PublicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *PublicError) PublicMessage() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// WithPublicMessage attaches a sanitized message without changing the original
// error chain used for HTTP status mapping.
func WithPublicMessage(err error, message string) error {
	if err == nil || message == "" {
		return err
	}
	return &PublicError{Cause: err, Message: message}
}

// PublicDetail returns the sanitized message carried by err, if any.
func PublicDetail(err error) string {
	var public interface{ PublicMessage() string }
	if errors.As(err, &public) {
		return public.PublicMessage()
	}
	return ""
}
