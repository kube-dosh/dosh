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
