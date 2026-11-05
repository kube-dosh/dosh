package workloadshttp

import (
	"encoding/json"
	"io"
	stdhttp "net/http"
	"strconv"
	"unicode/utf8"

	"dosh/internal/domain"
	workloadsapp "dosh/internal/modules/workloads/app"
	"dosh/internal/platform/httpx"
	"dosh/internal/platform/requestid"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *workloadsapp.Service
}

func New(service *workloadsapp.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	query := domain.ListQuery{
		Namespace: httpx.Query(r, "namespace"),
		Search:    httpx.Query(r, "q"),
		Kind:      httpx.Query(r, "kind"),
		Status:    httpx.Query(r, "status"),
	}
	items, err := h.service.List(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{
		"namespace": query.Namespace, "search": query.Search, "kind": query.Kind, "status": query.Status,
	})
	meta.Namespace, meta.Search, meta.Kind, meta.Status = query.Namespace, query.Search, query.Kind, query.Status
	httpx.JSONList(w, items, meta)
}

func (h *Handler) Get(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.Get(r.Context(), chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

type scaleRequest struct {
	Replicas        *int32 `json:"replicas"`
	ResourceVersion string `json:"resourceVersion"`
}

type imageUpdateRequest struct {
	ContainerName   string `json:"containerName"`
	Image           string `json:"image"`
	ResourceVersion string `json:"resourceVersion"`
}

type suspendRequest struct {
	Suspend         *bool  `json:"suspend"`
	ResourceVersion string `json:"resourceVersion"`
}

type deploymentEnvVarRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type deploymentCreateRequest struct {
	Name               string                    `json:"name"`
	Namespace          string                    `json:"namespace"`
	Replicas           int32                     `json:"replicas"`
	ContainerName      string                    `json:"containerName"`
	Image              string                    `json:"image"`
	ContainerPort      int32                     `json:"containerPort"`
	CPURequestMilli    int64                     `json:"cpuRequestMilli"`
	CPULimitMilli      int64                     `json:"cpuLimitMilli"`
	MemoryRequestBytes int64                     `json:"memoryRequestBytes"`
	MemoryLimitBytes   int64                     `json:"memoryLimitBytes"`
	Env                []deploymentEnvVarRequest `json:"env"`
	Labels             map[string]string         `json:"labels"`
}

func (request deploymentCreateRequest) spec() domain.DeploymentCreateSpec {
	env := make([]domain.DeploymentEnvVar, 0, len(request.Env))
	for _, item := range request.Env {
		env = append(env, domain.DeploymentEnvVar{Name: item.Name, Value: item.Value})
	}
	return domain.DeploymentCreateSpec{
		Name: request.Name, Namespace: request.Namespace, Replicas: request.Replicas,
		ContainerName: request.ContainerName, Image: request.Image, ContainerPort: request.ContainerPort,
		CPURequestMilli: request.CPURequestMilli, CPULimitMilli: request.CPULimitMilli,
		MemoryRequestBytes: request.MemoryRequestBytes, MemoryLimitBytes: request.MemoryLimitBytes,
		Env: env, Labels: request.Labels,
	}
}

func (h *Handler) CreateDeployment(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request deploymentCreateRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	item, err := h.service.CreateDeployment(r.Context(), actor, request.spec())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, item)
}

func (h *Handler) PreviewDeployment(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request deploymentCreateRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	manifest, err := h.service.PreviewDeployment(r.Context(), actor, request.spec())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"yaml": manifest})
}

func (h *Handler) Scale(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request scaleRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if request.Replicas == nil {
		httpx.Error(w, r, httpx.Validation("replicas", "replicas is required"))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.Scale(r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), *request.Replicas, request.ResourceVersion); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "scaled"})
}

func (h *Handler) Restart(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.Restart(r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "restarted"})
}

func (h *Handler) UpdateImage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request imageUpdateRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	err := h.service.UpdateImage(r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), domain.WorkloadImageUpdate{
		ContainerName: request.ContainerName, Image: request.Image, ResourceVersion: request.ResourceVersion,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handler) SetSuspend(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request suspendRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if request.Suspend == nil {
		httpx.Error(w, r, httpx.Validation("suspend", "suspend is required"))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	err := h.service.SetCronJobSuspend(r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), domain.CronJobSuspendUpdate{
		Suspend: *request.Suspend, ResourceVersion: request.ResourceVersion,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handler) Delete(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	propagation := httpx.Query(r, "propagation")
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.Delete(r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), propagation); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) ListPods(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	query := domain.ListQuery{Namespace: httpx.Query(r, "namespace"), Search: httpx.Query(r, "q")}
	options, err := httpx.ParseListOptions(r, []string{"name"})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, err := h.service.ListPods(r.Context(), query)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	items, meta := httpx.PageItems(items, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, items, meta)
}

func (h *Handler) GetPod(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetPod(r.Context(), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Logs(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	tailLines, err := strconv.ParseInt(httpx.Query(r, "tailLines"), 10, 64)
	if err != nil {
		tailLines = 200
	}
	stream, err := h.service.StreamLogs(
		r.Context(),
		chi.URLParam(r, "namespace"),
		chi.URLParam(r, "name"),
		httpx.Query(r, "container"),
		httpx.BoolQuery(r, "follow", false),
		tailLines,
		httpx.BoolQuery(r, "timestamps", false),
	)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer stream.Close()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(stdhttp.StatusOK)
	flusher, _ := w.(stdhttp.Flusher)
	encoder := json.NewEncoder(w)
	writeFrame := func(frame logStreamFrame) bool {
		if err := encoder.Encode(frame); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	buffer := make([]byte, 16*1024)
	var pending []byte
	for {
		n, readErr := stream.Read(buffer)
		if n > 0 {
			pending = append(pending, buffer[:n]...)
			chunk, remainder := splitUTF8Prefix(pending)
			pending = remainder
			if len(chunk) > 0 && !writeFrame(logStreamFrame{Type: "log", Data: string(chunk)}) {
				return
			}
		}
		if readErr != nil {
			if len(pending) > 0 {
				if !writeFrame(logStreamFrame{Type: "log", Data: string(pending)}) {
					return
				}
				pending = nil
			}
			if readErr == io.EOF {
				writeFrame(logStreamFrame{Type: "end"})
				return
			}
			_, problem := httpx.ProblemFor(readErr, requestid.From(r.Context()))
			writeFrame(logStreamFrame{Type: "error", Code: problem.Code, Message: problem.Message, RequestID: problem.RequestID})
			return
		}
	}
}

type logStreamFrame struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// splitUTF8Prefix holds back at most one incomplete UTF-8 rune so an arbitrary
// Kubernetes log read boundary cannot corrupt multi-byte text in the frame.
func splitUTF8Prefix(data []byte) ([]byte, []byte) {
	if len(data) == 0 || utf8.Valid(data) {
		return data, nil
	}
	for cut := 1; cut <= 3 && cut < len(data); cut++ {
		prefix := data[:len(data)-cut]
		if !utf8.Valid(prefix) {
			continue
		}
		if suffix := data[len(data)-cut:]; !utf8.FullRune(suffix) {
			return prefix, append([]byte(nil), suffix...)
		}
		break
	}
	return data, nil
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/workloads", h.List)
	r.Post("/workloads/deployments", h.CreateDeployment)
	r.Post("/workloads/deployments/preview", h.PreviewDeployment)
	r.Get("/workloads/{kind}/{namespace}/{name}", h.Get)
	r.Post("/workloads/{kind}/{namespace}/{name}/scale", h.Scale)
	r.Post("/workloads/{kind}/{namespace}/{name}/restart", h.Restart)
	r.Post("/workloads/{kind}/{namespace}/{name}/image", h.UpdateImage)
	r.Post("/workloads/{kind}/{namespace}/{name}/suspend", h.SetSuspend)
	r.Delete("/workloads/{kind}/{namespace}/{name}", h.Delete)
	r.Get("/pods", h.ListPods)
	r.Get("/pods/{namespace}/{name}", h.GetPod)
	r.Get("/pods/{namespace}/{name}/logs", h.Logs)
}
