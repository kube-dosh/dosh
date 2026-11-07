package storagehttp

import (
	stdhttp "net/http"

	"dosh/internal/domain"
	storageapp "dosh/internal/modules/storage/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ service *storageapp.Service }

func New(service *storageapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) StorageClasses(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListStorageClasses(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"search": query.Search})
	meta.Search = query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) PersistentVolumes(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListPersistentVolumes(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"search": query.Search})
	meta.Search = query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) PersistentVolumeClaims(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListPersistentVolumeClaims(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) StorageClass(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetStorageClass(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) PersistentVolume(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetPersistentVolume(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) PersistentVolumeClaim(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetPersistentVolumeClaim(r.Context(), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) CreatePersistentVolumeClaim(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.PersistentVolumeClaimCreateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreatePersistentVolumeClaim(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) UpdatePersistentVolumeClaim(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.PersistentVolumeClaimUpdateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpdatePersistentVolumeClaim(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) DeletePersistentVolumeClaim(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeletePersistentVolumeClaim(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/storageclasses", h.StorageClasses)
	r.Get("/storageclasses/{name}", h.StorageClass)
	r.Get("/persistentvolumes", h.PersistentVolumes)
	r.Get("/persistentvolumes/{name}", h.PersistentVolume)
	r.Get("/persistentvolumeclaims", h.PersistentVolumeClaims)
	r.Post("/persistentvolumeclaims", h.CreatePersistentVolumeClaim)
	r.Get("/persistentvolumeclaims/{namespace}/{name}", h.PersistentVolumeClaim)
	r.Put("/persistentvolumeclaims/{namespace}/{name}", h.UpdatePersistentVolumeClaim)
	r.Delete("/persistentvolumeclaims/{namespace}/{name}", h.DeletePersistentVolumeClaim)
}
