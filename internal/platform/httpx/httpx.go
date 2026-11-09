package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/requestid"

	"github.com/go-chi/chi/v5/middleware"
)

type Problem struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	RequestID   string            `json:"requestId,omitempty"`
	FieldErrors map[string]string `json:"fieldErrors,omitempty"`
}

var maxBodyBytes int64 = 2 * 1024 * 1024

// SetMaxBodyBytes configures the JSON request body limit once during bootstrap.
// It is transport policy only and does not affect domain or application layers.
func SetMaxBodyBytes(limit int64) {
	if limit > 0 {
		maxBodyBytes = limit
	}
}

type ListMeta struct {
	Total      int               `json:"total"`
	TotalKnown bool              `json:"totalKnown"`
	Page       int               `json:"page"`
	PageSize   int               `json:"pageSize"`
	TotalPages int               `json:"totalPages"`
	HasMore    bool              `json:"hasMore"`
	NextCursor string            `json:"nextCursor,omitempty"`
	Truncated  bool              `json:"truncated,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Sort       string            `json:"sort,omitempty"`
	Order      string            `json:"order,omitempty"`
	Filters    map[string]string `json:"filters,omitempty"`
	Namespace  string            `json:"namespace,omitempty"`
	Search     string            `json:"search,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Status     string            `json:"status,omitempty"`
}

// ListOptions is transport-level pagination and sorting policy. Repositories
// remain responsible for filtering, while the HTTP adapter applies a stable,
// deterministic page to the returned collection.
type ListOptions struct {
	Page     int
	PageSize int
	Cursor   string
	Sort     string
	Order    string
}

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

func ParseListOptions(r *http.Request, allowedSorts []string) (ListOptions, error) {
	options := ListOptions{Page: 1, PageSize: defaultPageSize, Order: "asc"}
	pageRaw := Query(r, "page")
	pageSizeRaw := Query(r, "pageSize")
	cursorRaw := Query(r, "cursor")
	sortRaw := Query(r, "sort")
	orderRaw := strings.ToLower(Query(r, "order"))

	if pageRaw != "" {
		page, err := strconv.Atoi(pageRaw)
		if err != nil || page < 1 {
			return options, Validation("page", "page must be a positive integer")
		}
		options.Page = page
	}
	if pageSizeRaw != "" {
		pageSize, err := strconv.Atoi(pageSizeRaw)
		if err != nil || pageSize < 1 || pageSize > maxPageSize {
			return options, Validation("pageSize", "pageSize must be between 1 and 200")
		}
		options.PageSize = pageSize
	}
	if cursorRaw != "" {
		if len(cursorRaw) > 8192 {
			return options, Validation("cursor", "cursor is too long")
		}
		options.Cursor = cursorRaw
	}

	if sortRaw != "" {
		allowed := false
		for _, field := range allowedSorts {
			if sortRaw == field {
				allowed = true
				break
			}
		}
		if !allowed {
			return options, Validation("sort", "unsupported sort field")
		}
		options.Sort = sortRaw
	}
	if orderRaw != "" {
		if orderRaw != "asc" && orderRaw != "desc" {
			return options, Validation("order", "order must be asc or desc")
		}
		if options.Sort == "" {
			return options, Validation("order", "order requires sort")
		}
		options.Order = orderRaw
	}
	return options, nil
}

func PageItems[T any](items []T, options ListOptions, filters map[string]string) ([]T, ListMeta, error) {
	SortItems(items, options)
	result, err := domain.PaginateSliceWithCursor(items, domain.PageRequest{
		Number: options.Page,
		Size:   options.PageSize,
		Cursor: options.Cursor,
	})
	if err != nil {
		return nil, ListMeta{}, err
	}
	return result.Items, PageMeta(result, options, filters), nil
}

// SortItems applies an allow-listed sort field to a bounded page. Kubernetes
// pagination is opaque, so sorting is intentionally page-local; callers that
// need a globally sorted collection must materialize it explicitly.
func SortItems[T any](items []T, options ListOptions) {
	if options.Sort == "" {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := sortableField(items[i], options.Sort)
		right := sortableField(items[j], options.Sort)
		if options.Order == "desc" {
			return left > right
		}
		return left < right
	})
}

// PageMeta converts a repository page into the stable HTTP list contract.
func PageMeta[T any](page domain.PageResult[T], options ListOptions, filters map[string]string) ListMeta {
	totalPages := 0
	if page.TotalKnown && page.Total > 0 && options.PageSize > 0 {
		totalPages = (page.Total + options.PageSize - 1) / options.PageSize
	}
	return ListMeta{
		Total:      page.Total,
		TotalKnown: page.TotalKnown,
		Page:       options.Page,
		PageSize:   options.PageSize,
		TotalPages: totalPages,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
		Truncated:  page.Truncated,
		Warnings:   page.Warnings,
		Sort:       options.Sort,
		Order:      options.Order,
		Filters:    filters,
	}
}

func sortableField[T any](item T, field string) string {
	encoded, err := json.Marshal(item)
	if err != nil {
		return ""
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return ""
	}
	switch value := object[field].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}

type ListResponse[T any] struct {
	Items []T      `json:"items"`
	Meta  ListMeta `json:"meta"`
}

func RequestID(next http.Handler) http.Handler {
	return middleware.RequestID(next)
}

func RequestIDContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.GetReqID(r.Context())
		if id != "" {
			w.Header().Set("X-Request-ID", id)
		}
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// NoStoreAPI prevents browsers and intermediaries from caching authenticated
// API representations. Streaming handlers may override this with their own
// no-cache/no-transform policy.
func NoStoreAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// SameOrigin rejects browser-originated unsafe requests that declare a
// cross-site initiator or an Origin different from the request host. Requests
// without browser origin metadata, such as CLI clients, remain supported; CSRF
// tokens continue to protect authenticated state changes.
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
			Error(w, r, domain.ErrForbidden)
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			!strings.EqualFold(parsed.Host, r.Host) {
			Error(w, r, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", requestid.From(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value == nil || status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func JSONList[T any](w http.ResponseWriter, items []T, meta ListMeta) {
	if items == nil {
		items = []T{}
	}
	if meta.Total == 0 {
		meta.Total = len(items)
	}
	JSON(w, http.StatusOK, ListResponse[T]{Items: items, Meta: meta})
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			JSON(w, http.StatusRequestEntityTooLarge, Problem{
				Code:      "request_too_large",
				Message:   fmt.Sprintf("request body must not exceed %d bytes", maxBytesErr.Limit),
				RequestID: requestid.From(r.Context()),
			})
			return false
		}
		Error(w, r, &domain.ValidationError{Field: "body", Message: err.Error()})
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		Error(w, r, &domain.ValidationError{Field: "body", Message: "request body must contain exactly one JSON value"})
		return false
	}
	return true
}

func Error(w http.ResponseWriter, r *http.Request, err error) {
	status, problem := ProblemFor(err, requestid.From(r.Context()))
	if status == http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request_failed", "error", err, "request_id", requestid.From(r.Context()))
	}
	JSON(w, status, problem)
}

// ProblemFor maps a domain error to a stable HTTP status and response body.
// Streaming adapters use the same mapping after response headers have already
// been sent, so clients still receive an actionable terminal frame.
func ProblemFor(err error, requestID string) (int, Problem) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "internal server error"
	var fieldErrs map[string]string
	var validation *domain.ValidationError

	switch {
	case errors.As(err, &validation):
		status, code, message = http.StatusBadRequest, "validation_failed", validation.Message
		fieldErrs = map[string]string{}
		if validation.Field != "" {
			fieldErrs[validation.Field] = validation.Message
		}
	case errors.Is(err, domain.ErrInvalid):
		status, code, message = http.StatusBadRequest, "invalid_request", "invalid request"
	case errors.Is(err, domain.ErrUnauthorized):
		status, code, message = http.StatusUnauthorized, "unauthorized", "authentication required"
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrReadOnly):
		status, code, message = http.StatusForbidden, "forbidden", "operation is not permitted"
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "resource not found"
	case errors.Is(err, domain.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "resource changed since it was loaded"
	case errors.Is(err, domain.ErrAlreadyExists):
		status, code, message = http.StatusConflict, "already_exists", "resource already exists"
	case errors.Is(err, domain.ErrUnavailable):
		status, code, message = http.StatusServiceUnavailable, "dependency_unavailable", "cluster API is unavailable"
	case errors.Is(err, domain.ErrUnsupported):
		status, code, message = http.StatusNotImplemented, "not_supported", "operation is not supported"
	case errors.Is(err, domain.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, "rate_limited", "too many requests; try again later"
	case errors.Is(err, domain.ErrStaleCursor):
		status, code, message = http.StatusBadRequest, "stale_cursor", "pagination cursor expired; refresh the list"
	}
	return status, Problem{Code: code, Message: message, RequestID: requestID, FieldErrors: fieldErrs}
}

func Validation(field, message string) error {
	return &domain.ValidationError{Field: field, Message: message}
}

func TimeoutContext(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(r.Context())
	}
	return context.WithTimeout(r.Context(), timeout)
}

func Query(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

func BoolQuery(r *http.Request, key string, fallback bool) bool {
	raw := Query(r, key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconvParseBool(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func IntQuery(r *http.Request, key string, fallback int) int {
	raw := Query(r, key)
	if raw == "" {
		return fallback
	}
	var parsed int
	_, err := fmt.Sscanf(raw, "%d", &parsed)
	if err != nil {
		return fallback
	}
	return parsed
}

func strconvParseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("invalid boolean")
	}
}
