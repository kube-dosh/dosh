package http

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	preferencesapp "dosh/internal/modules/preferences/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *preferencesapp.Service
}

func New(service *preferencesapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	preferences, err := h.service.Get(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, preferences)
}

func (h *Handler) Save(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var preferences domain.UserPreferences
	if !httpx.DecodeJSON(w, r, &preferences) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	saved, err := h.service.Save(r.Context(), actor, preferences)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, saved)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/preferences", h.Get)
	r.Put("/preferences", h.Save)
}
