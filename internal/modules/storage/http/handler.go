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

func (h *Handler) Mount(r chi.Router) {
	r.Get("/storageclasses", h.StorageClasses)
	r.Get("/storageclasses/{name}", h.StorageClass)
	r.Get("/persistentvolumes", h.PersistentVolumes)
	r.Get("/persistentvolumes/{name}", h.PersistentVolume)
	r.Get("/persistentvolumeclaims", h.PersistentVolumeClaims)
	r.Get("/persistentvolumeclaims/{namespace}/{name}", h.PersistentVolumeClaim)
}
