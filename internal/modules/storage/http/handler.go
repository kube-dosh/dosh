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
	result, err := h.service.ListStorageClassesPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{"search": query.Search})
	meta.Search = query.Search
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) PersistentVolumes(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	result, err := h.service.ListPersistentVolumesPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{"search": query.Search})
	meta.Search = query.Search
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) PersistentVolumeClaims(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	result, err := h.service.ListPersistentVolumeClaimsPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) StorageClass(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetStorageClass(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) CreateStorageClass(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.StorageClassWriteSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreateStorageClass(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) UpdateStorageClass(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.StorageClassWriteSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpdateStorageClass(r.Context(), actor, chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) DeleteStorageClass(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteStorageClass(r.Context(), actor, chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) PersistentVolume(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetPersistentVolume(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) CreatePersistentVolume(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.PersistentVolumeCreateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreatePersistentVolume(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) UpdatePersistentVolume(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.PersistentVolumeUpdateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.UpdatePersistentVolume(r.Context(), actor, chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) DeletePersistentVolume(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeletePersistentVolume(r.Context(), actor, chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
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

func (h *Handler) VolumeSnapshotFeature(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	feature, err := h.service.VolumeSnapshotFeature(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, feature)
}

func (h *Handler) VolumeSnapshots(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	items, err := h.service.ListVolumeSnapshots(r.Context(), actor, query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	meta := httpx.ListMeta{Total: len(items), TotalKnown: true, Page: 1, PageSize: len(items), TotalPages: 1, Namespace: query.Namespace, Search: query.Search}
	if len(items) == 0 {
		meta.TotalPages = 0
	}
	httpx.JSONList(w, items, meta)
}

func (h *Handler) CreateVolumeSnapshot(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.VolumeSnapshotCreateSpec
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreateVolumeSnapshot(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) DeleteVolumeSnapshot(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeleteVolumeSnapshot(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), httpx.Query(r, "resourceVersion")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) StorageCenter(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.StorageCenter(r.Context(), actor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) StoragePreflight(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.StoragePreflightRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.StoragePreflight(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) VerifyStorage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.StorageVerifyRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.VerifyStorage(r.Context(), actor, request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/storage/center", h.StorageCenter)
	r.Post("/storage/center/preflight", h.StoragePreflight)
	r.Post("/storage/center/verify", h.VerifyStorage)
	r.Get("/storageclasses", h.StorageClasses)
	r.Get("/storageclasses/{name}", h.StorageClass)
	r.Post("/storageclasses", h.CreateStorageClass)
	r.Put("/storageclasses/{name}", h.UpdateStorageClass)
	r.Delete("/storageclasses/{name}", h.DeleteStorageClass)
	r.Get("/persistentvolumes", h.PersistentVolumes)
	r.Get("/persistentvolumes/{name}", h.PersistentVolume)
	r.Post("/persistentvolumes", h.CreatePersistentVolume)
	r.Put("/persistentvolumes/{name}", h.UpdatePersistentVolume)
	r.Delete("/persistentvolumes/{name}", h.DeletePersistentVolume)
	r.Get("/persistentvolumeclaims", h.PersistentVolumeClaims)
	r.Post("/persistentvolumeclaims", h.CreatePersistentVolumeClaim)
	r.Get("/persistentvolumeclaims/{namespace}/{name}", h.PersistentVolumeClaim)
	r.Put("/persistentvolumeclaims/{namespace}/{name}", h.UpdatePersistentVolumeClaim)
	r.Delete("/persistentvolumeclaims/{namespace}/{name}", h.DeletePersistentVolumeClaim)
	r.Get("/volume-snapshots/feature", h.VolumeSnapshotFeature)
	r.Get("/volume-snapshots", h.VolumeSnapshots)
	r.Post("/volume-snapshots", h.CreateVolumeSnapshot)
	r.Delete("/volume-snapshots/{namespace}/{name}", h.DeleteVolumeSnapshot)
}
