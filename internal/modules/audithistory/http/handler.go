package audithistoryhttp

import (
	"encoding/csv"
	"encoding/json"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"dosh/internal/domain"
	audithistoryapp "dosh/internal/modules/audithistory/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *audithistoryapp.Service }

func New(service *audithistoryapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	options, err := httpx.ParseListOptions(r, []string{"id", "timestamp", "user", "action", "resource", "namespace", "name", "result"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if options.Sort == "" {
		options.Sort = "timestamp"
		options.Order = "desc"
	}
	query := domain.AuditQuery{
		Namespace: httpx.Query(r, "namespace"),
		Search:    httpx.Query(r, "q"),
		Action:    httpx.Query(r, "action"),
		Result:    httpx.Query(r, "result"),
	}
	if query.Result != "" && !isAuditResult(query.Result) {
		httpx.Error(w, r, &domain.ValidationError{Field: "result", Message: "result must be success, failure or denied"})
		return
	}
	records, err := h.service.List(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	records, meta, err := httpx.PageItems(records, options, map[string]string{
		"namespace": query.Namespace, "search": query.Search,
		"action": query.Action, "result": query.Result,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, records, meta)
}

func (h *Handler) Actions(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actions, err := h.service.Actions(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]any{"items": actions})
}

func (h *Handler) Export(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query, err := auditQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	format := strings.ToLower(strings.TrimSpace(httpx.Query(r, "format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		httpx.Error(w, r, &domain.ValidationError{Field: "format", Message: "format must be csv or json"})
		return
	}
	records, err := h.service.Export(r.Context(), query, format)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="dosh-audit-`+stamp+`.json"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"exportedAt": time.Now().UTC(),
			"count":      len(records),
			"items":      records,
		})
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="dosh-audit-`+stamp+`.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"id", "timestamp", "user", "groups", "action", "resource", "namespace", "name", "result", "requestId", "error", "details"})
	for _, record := range records {
		details, _ := json.Marshal(record.Details)
		_ = writer.Write([]string{
			spreadsheetSafe(strconv.FormatUint(record.ID, 10)), spreadsheetSafe(record.Timestamp.UTC().Format(time.RFC3339Nano)),
			spreadsheetSafe(record.User), spreadsheetSafe(strings.Join(record.Groups, ";")), spreadsheetSafe(record.Action),
			spreadsheetSafe(record.Resource), spreadsheetSafe(record.Namespace), spreadsheetSafe(record.Name),
			spreadsheetSafe(record.Result), spreadsheetSafe(record.RequestID), spreadsheetSafe(record.Error), spreadsheetSafe(string(details)),
		})
	}
	writer.Flush()
}

func spreadsheetSafe(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}

func auditQuery(r *stdhttp.Request) (domain.AuditQuery, error) {
	query := domain.AuditQuery{
		Namespace: httpx.Query(r, "namespace"),
		Search:    httpx.Query(r, "q"),
		Action:    httpx.Query(r, "action"),
		Result:    httpx.Query(r, "result"),
	}
	if query.Result != "" && !isAuditResult(query.Result) {
		return domain.AuditQuery{}, &domain.ValidationError{Field: "result", Message: "result must be success, failure or denied"}
	}
	return query, nil
}

func isAuditResult(value string) bool {
	switch strings.ToLower(value) {
	case "success", "failure", "denied":
		return true
	default:
		return false
	}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/audit-events", h.List)
	r.Get("/audit-events/actions", h.Actions)
	r.Get("/audit-events/export", h.Export)
}
