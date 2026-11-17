package http

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	registriesapp "dosh/internal/modules/registries/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *registriesapp.Service
}

func New(service *registriesapp.Service) *Handler { return &Handler{service: service} }

type listResponse struct {
	Items []domain.ContainerRegistry `json:"items"`
}

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.List(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if items == nil {
		items = []domain.ContainerRegistry{}
	}
	httpx.JSON(w, stdhttp.StatusOK, listResponse{Items: items})
}

func (h *Handler) Create(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.ContainerRegistryUpsert
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Create(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) Update(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.ContainerRegistryUpsert
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Update(r.Context(), actor, chi.URLParam(r, "id"), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Delete(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.Delete(r.Context(), actor, chi.URLParam(r, "id"), httpx.Query(r, "revision")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) Test(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Test(r.Context(), actor, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Repositories(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	query := httpx.Query(r, "q")
	result, err := h.service.Repositories(r.Context(), actor, chi.URLParam(r, "id"), query, domain.PageRequest{
		Number: options.Page, Size: options.PageSize, Cursor: options.Cursor,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	meta := httpx.PageMeta(result, options, map[string]string{"search": query})
	meta.Search = query
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) Tags(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	repository := httpx.Query(r, "repository")
	query := httpx.Query(r, "q")
	result, err := h.service.Tags(r.Context(), actor, chi.URLParam(r, "id"), repository, query, domain.PageRequest{
		Number: options.Page, Size: options.PageSize, Cursor: options.Cursor,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	meta := httpx.PageMeta(result, options, map[string]string{"search": query, "repository": repository})
	meta.Search = query
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) Resolve(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Resolve(r.Context(), actor, httpx.Query(r, "image"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/container-registries", h.List)
	r.Post("/container-registries", h.Create)
	r.Get("/container-registries/resolve", h.Resolve)
	r.Put("/container-registries/{id}", h.Update)
	r.Delete("/container-registries/{id}", h.Delete)
	r.Post("/container-registries/{id}/test", h.Test)
	r.Get("/container-registries/{id}/repositories", h.Repositories)
	r.Get("/container-registries/{id}/tags", h.Tags)
}
