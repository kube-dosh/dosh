package nodeshttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	nodesapp "dosh/internal/modules/nodes/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *nodesapp.Service
}

func New(service *nodesapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.List(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"search": query.Search})
	meta.Search = query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.Get(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Cordon(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Cordon(r.Context(), actor, chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Uncordon(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Uncordon(r.Context(), actor, chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) ListNamespaces(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListNamespaces(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"search": query.Search})
	meta.Search = query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) GetNamespace(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetNamespace(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/nodes", h.List)
	r.Get("/nodes/{name}", h.Get)
	r.Post("/nodes/{name}/cordon", h.Cordon)
	r.Post("/nodes/{name}/uncordon", h.Uncordon)
	r.Get("/namespaces", h.ListNamespaces)
	r.Get("/namespaces/{name}", h.GetNamespace)
}
