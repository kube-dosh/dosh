package http

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	resilienceapp "dosh/internal/modules/resilience/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *resilienceapp.Service }

func New(service *resilienceapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Autoscalers(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.ListAutoscalers(r.Context(), actor, httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSONList(w, items, httpx.ListMeta{Total: len(items), TotalKnown: true, Page: 1, PageSize: max(len(items), 1)})
}

func (h *Handler) UpsertAutoscaler(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var spec domain.AutoscalerSpec
	if !httpx.DecodeJSON(w, r, &spec) {
		return
	}
	if namespace := chi.URLParam(r, "namespace"); namespace != "" {
		spec.Namespace = namespace
		spec.Name = chi.URLParam(r, "name")
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpsertAutoscaler(r.Context(), actor, spec)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	status := stdhttp.StatusOK
	if spec.ResourceVersion == "" {
		status = stdhttp.StatusCreated
	}
	httpx.JSON(w, status, item)
}

func (h *Handler) DeleteAutoscaler(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteAutoscaler(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) DisruptionBudgets(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.ListDisruptionBudgets(r.Context(), actor, httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSONList(w, items, httpx.ListMeta{Total: len(items), TotalKnown: true, Page: 1, PageSize: max(len(items), 1)})
}

func (h *Handler) UpsertDisruptionBudget(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var spec domain.DisruptionBudgetSpec
	if !httpx.DecodeJSON(w, r, &spec) {
		return
	}
	if namespace := chi.URLParam(r, "namespace"); namespace != "" {
		spec.Namespace = namespace
		spec.Name = chi.URLParam(r, "name")
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpsertDisruptionBudget(r.Context(), actor, spec)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	status := stdhttp.StatusOK
	if spec.ResourceVersion == "" {
		status = stdhttp.StatusCreated
	}
	httpx.JSON(w, status, item)
}

func (h *Handler) DeleteDisruptionBudget(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteDisruptionBudget(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) VerticalAutoscalers(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.ListVerticalAutoscalers(r.Context(), actor, httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSONList(w, items, httpx.ListMeta{Total: len(items), TotalKnown: true, Page: 1, PageSize: max(len(items), 1)})
}

func (h *Handler) UpsertVerticalAutoscaler(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var spec domain.VerticalAutoscalerSpec
	if !httpx.DecodeJSON(w, r, &spec) {
		return
	}
	if namespace := chi.URLParam(r, "namespace"); namespace != "" {
		spec.Namespace = namespace
		spec.Name = chi.URLParam(r, "name")
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpsertVerticalAutoscaler(r.Context(), actor, spec)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	status := stdhttp.StatusOK
	if spec.ResourceVersion == "" {
		status = stdhttp.StatusCreated
	}
	httpx.JSON(w, status, item)
}

func (h *Handler) DeleteVerticalAutoscaler(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteVerticalAutoscaler(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/resilience/autoscalers", h.Autoscalers)
	r.Post("/resilience/autoscalers", h.UpsertAutoscaler)
	r.Put("/resilience/autoscalers/{namespace}/{name}", h.UpsertAutoscaler)
	r.Delete("/resilience/autoscalers/{namespace}/{name}", h.DeleteAutoscaler)
	r.Get("/resilience/disruption-budgets", h.DisruptionBudgets)
	r.Post("/resilience/disruption-budgets", h.UpsertDisruptionBudget)
	r.Put("/resilience/disruption-budgets/{namespace}/{name}", h.UpsertDisruptionBudget)
	r.Delete("/resilience/disruption-budgets/{namespace}/{name}", h.DeleteDisruptionBudget)
	r.Get("/resilience/verticalpodautoscalers", h.VerticalAutoscalers)
	r.Post("/resilience/verticalpodautoscalers", h.UpsertVerticalAutoscaler)
	r.Put("/resilience/verticalpodautoscalers/{namespace}/{name}", h.UpsertVerticalAutoscaler)
	r.Delete("/resilience/verticalpodautoscalers/{namespace}/{name}", h.DeleteVerticalAutoscaler)
}
