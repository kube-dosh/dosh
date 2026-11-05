package certificateshttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	certificatesapp "dosh/internal/modules/certificates/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *certificatesapp.Service }

func New(service *certificatesapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	status, err := h.service.Status(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, status)
}

func (h *Handler) Reload(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	status, err := h.service.Reload(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, status)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/certificate", h.Status)
	r.Post("/certificate/reload", h.Reload)
}
