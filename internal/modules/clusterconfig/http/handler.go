package clusterconfighttp

import (
	stdhttp "net/http"

	clusterconfigapp "dosh/internal/modules/clusterconfig/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *clusterconfigapp.Service
}

func New(service *clusterconfigapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	configuration, err := h.service.Get(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, configuration)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/cluster/config", h.Get)
}
