package http

import (
	stdhttp "net/http"
	"strconv"

	"dosh/internal/domain"
	resourcesapp "dosh/internal/modules/resources/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *resourcesapp.Service }

func New(service *resourcesapp.Service) *Handler { return &Handler{service: service} }

type discoveryResponse struct {
	Items    []domain.ResourceDescriptor `json:"items"`
	Warnings []string                    `json:"warnings,omitempty"`
}

func (h *Handler) Discovery(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, warnings, err := h.service.Discovery(r.Context(), actor, httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, discoveryResponse{Items: items, Warnings: warnings})
}

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ref, err := referenceFromRequest(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	options, err := httpx.ParseListOptions(r, []string{"name", "namespace", "createdAt"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	actor, _ := domain.ActorFromContext(r.Context())
	result, err := h.service.List(r.Context(), actor, ref, query, domain.PageRequest{
		Number: options.Page, Size: options.PageSize, Cursor: options.Cursor,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result.PageResult, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	meta.Columns = result.Columns
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) Search(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	limit := 20
	if raw := httpx.Query(r, "limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			httpx.Error(w, r, httpx.Validation("limit", "limit must be between 1 and 50"))
			return
		}
		limit = parsed
	}
	result, err := h.service.Search(r.Context(), actor, httpx.Query(r, "q"), httpx.Query(r, "namespace"), limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, result)
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ref, err := referenceFromRequest(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Get(r.Context(), actor, ref, httpx.Query(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Capabilities(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ref := domain.ResourceReference{
		Group: httpx.Query(r, "group"), Version: httpx.Query(r, "version"), Resource: httpx.Query(r, "resource"),
	}
	if ref.Group == "_core" || ref.Group == "core" {
		ref.Group = ""
	}
	if ref.Version == "" || ref.Resource == "" {
		httpx.Error(w, r, httpx.Validation("resource", "version and resource are required"))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	capabilities, err := h.service.Capabilities(r.Context(), actor, ref, httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusOK, capabilities)
}

func (h *Handler) Create(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ref, err := referenceFromRequest(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var request domain.GenericResourceMutation
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Create(r.Context(), actor, ref, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) Update(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ref, err := referenceFromRequest(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var request domain.GenericResourceMutation
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Update(r.Context(), actor, ref, chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Delete(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ref, err := referenceFromRequest(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	err = h.service.Delete(
		r.Context(), actor, ref, httpx.Query(r, "namespace"), chi.URLParam(r, "name"),
		httpx.Query(r, "resourceVersion"), httpx.Query(r, "propagation"),
	)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func referenceFromRequest(r *stdhttp.Request) (domain.ResourceReference, error) {
	group := chi.URLParam(r, "group")
	if group == "_core" || group == "core" {
		group = ""
	}
	ref := domain.ResourceReference{
		Group: group, Version: chi.URLParam(r, "version"), Resource: chi.URLParam(r, "resource"),
	}
	if ref.Version == "" || ref.Resource == "" {
		return domain.ResourceReference{}, httpx.Validation("resource", "version and resource are required")
	}
	return ref, nil
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/resources/discovery", h.Discovery)
	r.Get("/resources/search", h.Search)
	r.Get("/resources/capabilities", h.Capabilities)
	r.Get("/resources/{group}/{version}/{resource}", h.List)
	r.Get("/resources/{group}/{version}/{resource}/{name}", h.Get)
	r.Post("/resources/{group}/{version}/{resource}", h.Create)
	r.Put("/resources/{group}/{version}/{resource}/{name}", h.Update)
	r.Delete("/resources/{group}/{version}/{resource}/{name}", h.Delete)
}
