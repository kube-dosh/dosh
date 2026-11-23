package systemhttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	systemapp "dosh/internal/modules/system/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *systemapp.Service
}

func New(service *systemapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Info(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	info, err := h.service.Info(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, info)
}

func (h *Handler) Update(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	status, err := h.service.Update(r.Context(), false)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, status)
}

func (h *Handler) CheckUpdate(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, ok := domain.ActorFromContext(r.Context())
	if !ok || !actor.Admin {
		httpx.Error(w, r, domain.ErrForbidden)
		return
	}
	status, err := h.service.Update(r.Context(), true)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, status)
}

func (h *Handler) Egress(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	settings, err := h.service.Egress(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, settings)
}

func (h *Handler) SaveEgress(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, ok := domain.ActorFromContext(r.Context())
	if !ok || !actor.Admin {
		httpx.Error(w, r, domain.ErrForbidden)
		return
	}
	var input domain.EgressSettingsUpdate
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	settings, err := h.service.SaveEgress(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, settings)
}

func (h *Handler) PreflightEgress(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, ok := domain.ActorFromContext(r.Context())
	if !ok || !actor.Admin {
		httpx.Error(w, r, domain.ErrForbidden)
		return
	}
	var input domain.EgressSettingsUpdate
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	result, err := h.service.PreflightEgress(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, result)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/system/info", h.Info)
	r.Get("/system/update", h.Update)
	r.Post("/system/update/check", h.CheckUpdate)
	r.Get("/system/egress", h.Egress)
	r.Put("/system/egress", h.SaveEgress)
	r.Post("/system/egress/preflight", h.PreflightEgress)
}
