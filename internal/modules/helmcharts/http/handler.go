package http

import (
	stdhttp "net/http"
	"strconv"

	"dosh/internal/domain"
	helmchartsapp "dosh/internal/modules/helmcharts/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *helmchartsapp.Service
}

func New(service *helmchartsapp.Service) *Handler { return &Handler{service: service} }

type repositoryListResponse struct {
	Items []domain.HelmRepository `json:"items"`
}

type operationListResponse struct {
	Items []domain.HelmChartOperation `json:"items"`
}

func (h *Handler) ListRepositories(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, err := h.service.ListRepositories(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if items == nil {
		items = []domain.HelmRepository{}
	}
	httpx.JSON(w, stdhttp.StatusOK, repositoryListResponse{Items: items})
}

func (h *Handler) CreateRepository(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmRepositoryUpsert
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreateRepository(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) UpdateRepository(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmRepositoryUpsert
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpdateRepository(r.Context(), actor, chi.URLParam(r, "id"), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) DeleteRepository(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteRepository(r.Context(), actor, chi.URLParam(r, "id"), httpx.Query(r, "revision")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) SyncRepository(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.SyncRepository(r.Context(), actor, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) ListCharts(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	items, warnings, err := h.service.Charts(r.Context(), actor, httpx.Query(r, "repositoryId"), httpx.Query(r, "q"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if items == nil {
		items = []domain.HelmChartSummary{}
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]any{"items": items, "warnings": warnings})
}

func (h *Handler) GetChart(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Chart(r.Context(), actor, chi.URLParam(r, "repositoryId"), chi.URLParam(r, "chartName"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) SyncChart(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.SyncChart(r.Context(), actor, chi.URLParam(r, "repositoryId"), chi.URLParam(r, "chartName"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) CacheStats(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CacheStats(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) CleanupCache(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CleanupCache(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Plan(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmChartPlanRequest
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Plan(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Install(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmChartOperationRequest
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Install(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) Upgrade(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmChartOperationRequest
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Upgrade(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Uninstall(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmChartUninstallRequest
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Uninstall(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Rollback(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var input domain.HelmChartRollbackRequest
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Rollback(r.Context(), actor, input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Operation(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.Operation(r.Context(), actor, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) ListOperations(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	limit, _ := strconv.Atoi(httpx.Query(r, "limit"))
	items, err := h.service.ListOperations(r.Context(), actor, domain.HelmOperationFilter{
		Limit: limit, Status: httpx.Query(r, "status"), Namespace: httpx.Query(r, "namespace"),
		ReleaseName: httpx.Query(r, "releaseName"),
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if items == nil {
		items = []domain.HelmChartOperation{}
	}
	httpx.JSON(w, stdhttp.StatusOK, operationListResponse{Items: items})
}

func (h *Handler) CancelOperation(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CancelOperation(r.Context(), actor, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/helm/repositories", h.ListRepositories)
	r.Post("/helm/repositories", h.CreateRepository)
	r.Put("/helm/repositories/{id}", h.UpdateRepository)
	r.Delete("/helm/repositories/{id}", h.DeleteRepository)
	r.Post("/helm/repositories/{id}/sync", h.SyncRepository)
	r.Get("/helm/charts", h.ListCharts)
	r.Post("/helm/charts/plan", h.Plan)
	r.Post("/helm/charts/install", h.Install)
	r.Post("/helm/charts/upgrade", h.Upgrade)
	r.Post("/helm/charts/uninstall", h.Uninstall)
	r.Post("/helm/charts/rollback", h.Rollback)
	r.Get("/helm/charts/{repositoryId}/{chartName}", h.GetChart)
	r.Post("/helm/charts/{repositoryId}/{chartName}/sync", h.SyncChart)
	r.Get("/helm/operations", h.ListOperations)
	r.Get("/helm/operations/{id}", h.Operation)
	r.Post("/helm/operations/{id}/cancel", h.CancelOperation)
	r.Get("/helm/cache", h.CacheStats)
	r.Post("/helm/cache/cleanup", h.CleanupCache)
}
