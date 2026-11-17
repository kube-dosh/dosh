package diagnosticshttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	diagnosticsapp "dosh/internal/modules/diagnostics/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *diagnosticsapp.Service
}

func New(service *diagnosticsapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Diagnose(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	result, err := h.service.Diagnose(
		r.Context(), actor,
		chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"),
	)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, result)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/diagnostics/{kind}/{namespace}/{name}", h.Diagnose)
	r.Get("/diagnostics/{kind}/{name}", h.Diagnose)
}
