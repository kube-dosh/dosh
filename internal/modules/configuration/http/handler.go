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
	result, err := h.service.ListConfigMapsPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) ConfigMap(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetConfigMap(r.Context(), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) CreateConfigMap(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.ConfigMapCreateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreateConfigMap(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) UpdateConfigMap(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.ConfigMapUpdateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpdateConfigMap(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) DeleteConfigMap(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteConfigMap(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) Secrets(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	result, err := h.service.ListSecretsPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, result.Items, meta)
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

func (h *Handler) CreateSecret(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.SecretWriteSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreateSecret(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) UpdateSecret(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.SecretWriteSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpdateSecret(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) DeleteSecret(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteSecret(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/configmaps", h.ConfigMaps)
	r.Post("/configmaps", h.CreateConfigMap)
	r.Get("/configmaps/{namespace}/{name}", h.ConfigMap)
	r.Put("/configmaps/{namespace}/{name}", h.UpdateConfigMap)
	r.Delete("/configmaps/{namespace}/{name}", h.DeleteConfigMap)
	r.Get("/secrets", h.Secrets)
	r.Post("/secrets", h.CreateSecret)
	r.Post("/secrets/{namespace}/{name}/reveal", h.RevealSecret)
	r.Put("/secrets/{namespace}/{name}", h.UpdateSecret)
	r.Delete("/secrets/{namespace}/{name}", h.DeleteSecret)
}
