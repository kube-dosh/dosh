package configurationhttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	configurationapp "dosh/internal/modules/configuration/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *configurationapp.Service }

func New(service *configurationapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) ConfigMaps(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListConfigMaps(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) ConfigMap(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetConfigMap(r.Context(), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Secrets(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListSecrets(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) RevealSecret(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.RevealSecret(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/configmaps", h.ConfigMaps)
	r.Get("/configmaps/{namespace}/{name}", h.ConfigMap)
	r.Get("/secrets", h.Secrets)
	r.Post("/secrets/{namespace}/{name}/reveal", h.RevealSecret)
}
