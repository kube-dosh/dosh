package audithistoryhttp

import (
	stdhttp "net/http"
	"strings"

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
}
