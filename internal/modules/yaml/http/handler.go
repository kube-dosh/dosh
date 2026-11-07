package yamlhttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	yamlapp "dosh/internal/modules/yaml/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *yamlapp.Service }

func New(service *yamlapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	resourceType, ok := domain.ParseResourceType(httpx.Query(r, "type"))
	if !ok {
		httpx.Error(w, r, httpx.Validation("type", "unsupported resource type"))
		return
	}
	item, err := h.service.Get(r.Context(), resourceType, httpx.Query(r, "namespace"), httpx.Query(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Apply(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.ApplyRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Apply(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/yaml", h.Get)
	r.Post("/yaml/apply", h.Apply)
}
