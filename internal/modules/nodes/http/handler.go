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

type nodeLabelsRequest struct {
	Labels          *map[string]string `json:"labels"`
	ResourceVersion string             `json:"resourceVersion"`
}

func (h *Handler) SetLabels(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request nodeLabelsRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if request.Labels == nil {
		httpx.Error(w, r, httpx.Validation("labels", "labels is required"))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.SetLabels(r.Context(), actor, chi.URLParam(r, "name"), domain.NodeLabelUpdate{
		Labels: *request.Labels, ResourceVersion: request.ResourceVersion,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

type nodeTaintsRequest struct {
	Taints          *[]domain.Taint `json:"taints"`
	ResourceVersion string          `json:"resourceVersion"`
}

func (h *Handler) SetTaints(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request nodeTaintsRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if request.Taints == nil {
		httpx.Error(w, r, httpx.Validation("taints", "taints is required"))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.SetTaints(r.Context(), actor, chi.URLParam(r, "name"), domain.NodeTaintUpdate{
		Taints: *request.Taints, ResourceVersion: request.ResourceVersion,
	})
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

func (h *Handler) DeleteNamespace(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteNamespace(r.Context(), actor, chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/nodes", h.List)
	r.Get("/nodes/{name}", h.Get)
	r.Post("/nodes/{name}/cordon", h.Cordon)
	r.Post("/nodes/{name}/uncordon", h.Uncordon)
	r.Put("/nodes/{name}/labels", h.SetLabels)
	r.Put("/nodes/{name}/taints", h.SetTaints)
	r.Get("/namespaces", h.ListNamespaces)
	r.Get("/namespaces/{name}", h.GetNamespace)
	r.Delete("/namespaces/{name}", h.DeleteNamespace)
}
