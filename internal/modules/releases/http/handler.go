package http

import (
	stdhttp "net/http"
	"strconv"

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

func (h *Handler) Diff(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	from, err := strconv.Atoi(httpx.Query(r, "from"))
	if err != nil {
		httpx.Error(w, r, httpx.Validation("from", "from must be a positive revision"))
		return
	}
	to, err := strconv.Atoi(httpx.Query(r, "to"))
	if err != nil {
		httpx.Error(w, r, httpx.Validation("to", "to must be a positive revision"))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	result, err := h.service.Diff(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), from, to)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, result)
}

type rollbackRequest struct {
	TargetRevision int    `json:"targetRevision"`
	Confirmation   string `json:"confirmation"`
}

func (h *Handler) Rollback(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request rollbackRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	result, err := h.service.Rollback(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), request.TargetRevision, request.Confirmation)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, result)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/helm/releases", h.List)
	r.Get("/helm/releases/{namespace}/{name}", h.Get)
	r.Get("/helm/releases/{namespace}/{name}/diff", h.Diff)
	r.Post("/helm/releases/{namespace}/{name}/rollback", h.Rollback)
}
