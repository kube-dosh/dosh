package http

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	releasesapp "dosh/internal/modules/releases/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *releasesapp.Service
}

func New(service *releasesapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, warnings, err := h.service.List(r.Context(), actor, httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]any{"items": items, "warnings": warnings})
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Get(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/helm/releases", h.List)
	r.Get("/helm/releases/{namespace}/{name}", h.Get)
}
