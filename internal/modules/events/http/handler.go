package eventshttp

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"time"

	"dosh/internal/domain"
	eventsapp "dosh/internal/modules/events/app"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *eventsapp.Service
}

func New(service *eventsapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	options, err := httpx.ParseListOptions(r, []string{"name", "lastSeen", "type", "reason"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	query := domain.ListQuery{
		Namespace:  httpx.Query(r, "namespace"),
		Search:     httpx.Query(r, "q"),
		ObjectKind: httpx.Query(r, "objectKind"),
		ObjectName: httpx.Query(r, "objectName"),
	}
	items, err := h.service.List(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{
		"namespace": query.Namespace, "search": query.Search,
		"objectKind": query.ObjectKind, "objectName": query.ObjectName,
	})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) Stream(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		httpx.Error(w, r, fmt.Errorf("%w: streaming is not supported", domain.ErrUnavailable))
		return
	}
	events, errs, err := h.service.Watch(r.Context(), httpx.Query(r, "namespace"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(stdhttp.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			payload, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				continue
			}
			fmt.Fprintf(w, "event: event\ndata: %s\n\n", payload)
			flusher.Flush()
		case watchErr, ok := <-errs:
			if ok && watchErr != nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", watchErr.Error())
				flusher.Flush()
			}
			return
		}
	}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/events", h.List)
	r.Get("/events/stream", h.Stream)
}
