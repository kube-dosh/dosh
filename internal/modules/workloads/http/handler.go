package workloadshttp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dosh/internal/domain"
	workloadsapp "dosh/internal/modules/workloads/app"
	"dosh/internal/platform/httpx"
	"dosh/internal/platform/requestid"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

type Handler struct {
	service  *workloadsapp.Service
	terminal domain.TerminalLimits
}

func New(service *workloadsapp.Service, limits ...domain.TerminalLimits) *Handler {
	terminal := domain.DefaultTerminalLimits()
	if len(limits) > 0 {
		terminal = limits[0]
	}
	return &Handler{service: service, terminal: terminal}
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
	result, err := h.service.ListPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{
		"namespace": query.Namespace, "search": query.Search, "kind": query.Kind, "status": query.Status,
	})
	meta.Namespace, meta.Search, meta.Kind, meta.Status = query.Namespace, query.Search, query.Kind, query.Status
	httpx.JSONList(w, result.Items, meta)
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

type rollbackRequest struct {
	Revision int64 `json:"revision"`
}

type deploymentEnvVarRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type containerEnvValueFromRequest struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Optional bool   `json:"optional"`
}

type containerEnvVarRequest struct {
	Name      string                        `json:"name"`
	Value     string                        `json:"value"`
	ValueFrom *containerEnvValueFromRequest `json:"valueFrom"`
}

type containerVolumeMountRequest struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	SubPath   string `json:"subPath"`
	ReadOnly  bool   `json:"readOnly"`
}

type containerProbeRequest struct {
	Type                string   `json:"type"`
	Path                string   `json:"path"`
	Host                string   `json:"host"`
	Scheme              string   `json:"scheme"`
	Port                string   `json:"port"`
	Service             string   `json:"service"`
	Command             []string `json:"command"`
	InitialDelaySeconds int32    `json:"initialDelaySeconds"`
	PeriodSeconds       int32    `json:"periodSeconds"`
	TimeoutSeconds      int32    `json:"timeoutSeconds"`
	SuccessThreshold    int32    `json:"successThreshold"`
	FailureThreshold    int32    `json:"failureThreshold"`
}

type containerConfigRequest struct {
	Env             []containerEnvVarRequest      `json:"env"`
	VolumeMounts    []containerVolumeMountRequest `json:"volumeMounts"`
	LivenessProbe   *containerProbeRequest        `json:"livenessProbe"`
	ReadinessProbe  *containerProbeRequest        `json:"readinessProbe"`
	StartupProbe    *containerProbeRequest        `json:"startupProbe"`
	ResourceVersion string                        `json:"resourceVersion"`
}

func (request containerConfigRequest) update(containerName string) domain.WorkloadContainerConfigUpdate {
	env := make([]domain.WorkloadEnvVar, 0, len(request.Env))
	for _, item := range request.Env {
		variable := domain.WorkloadEnvVar{Name: item.Name, Value: item.Value}
		if item.ValueFrom != nil {
			variable.ValueFrom = &domain.WorkloadEnvValueFrom{
				Kind: item.ValueFrom.Kind, Name: item.ValueFrom.Name, Key: item.ValueFrom.Key, Optional: item.ValueFrom.Optional,
			}
		}
		env = append(env, variable)
	}
	mounts := make([]domain.WorkloadVolumeMount, 0, len(request.VolumeMounts))
	for _, item := range request.VolumeMounts {
		mounts = append(mounts, domain.WorkloadVolumeMount{
			Name: item.Name, MountPath: item.MountPath, SubPath: item.SubPath, ReadOnly: item.ReadOnly,
		})
	}
	return domain.WorkloadContainerConfigUpdate{
		ContainerName: containerName, Env: env, VolumeMounts: mounts,
		LivenessProbe: containerProbe(request.LivenessProbe), ReadinessProbe: containerProbe(request.ReadinessProbe),
		StartupProbe: containerProbe(request.StartupProbe), ResourceVersion: request.ResourceVersion,
	}
}

func containerProbe(request *containerProbeRequest) *domain.WorkloadProbe {
	if request == nil {
		return nil
	}
	return &domain.WorkloadProbe{
		Type: request.Type, Path: request.Path, Host: request.Host, Scheme: request.Scheme, Port: request.Port,
		Service: request.Service, Command: request.Command, InitialDelaySeconds: request.InitialDelaySeconds,
		PeriodSeconds: request.PeriodSeconds, TimeoutSeconds: request.TimeoutSeconds,
		SuccessThreshold: request.SuccessThreshold, FailureThreshold: request.FailureThreshold,
	}
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

func (h *Handler) UpdateContainerConfig(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request containerConfigRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	err := h.service.UpdateContainerConfig(
		r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"),
		request.update(chi.URLParam(r, "container")),
	)
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
	result, err := h.service.ListPodsPage(r.Context(), query, domain.PageRequest{Number: options.Page, Size: options.PageSize, Cursor: options.Cursor})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.SortItems(result.Items, options)
	meta := httpx.PageMeta(result, options, map[string]string{"namespace": query.Namespace, "search": query.Search})
	meta.Namespace, meta.Search = query.Namespace, query.Search
	httpx.JSONList(w, result.Items, meta)
}

func (h *Handler) GetPod(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	item, err := h.service.GetPod(r.Context(), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, item)
}

func (h *Handler) Revisions(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	items, err := h.service.Revisions(r.Context(), chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusOK, map[string]any{"items": items})
}

func (h *Handler) Rollback(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request rollbackRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.Rollback(r.Context(), actor, chi.URLParam(r, "kind"), chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), request.Revision); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "rolling-back"})
}

func (h *Handler) DeletePod(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	if err := h.service.DeletePod(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusAccepted, map[string]string{"status": "deleting"})
}

func (h *Handler) Logs(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	actor, _ := domain.ActorFromContext(r.Context())
	tailLines, err := strconv.ParseInt(httpx.Query(r, "tailLines"), 10, 64)
	if err != nil {
		tailLines = 200
	}
	stream, err := h.service.StreamLogs(
		r.Context(),
		actor,
		chi.URLParam(r, "namespace"),
		chi.URLParam(r, "name"),
		httpx.Query(r, "container"),
		httpx.BoolQuery(r, "follow", false),
		httpx.BoolQuery(r, "previous", false),
		tailLines,
		httpx.BoolQuery(r, "timestamps", false),
		parseInt64Query(r, "sinceSeconds"),
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

func parseInt64Query(r *stdhttp.Request, key string) int64 {
	value, err := strconv.ParseInt(httpx.Query(r, key), 10, 64)
	if err != nil {
		return 0
	}
	return value
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
	r.Put("/workloads/{kind}/{namespace}/{name}/containers/{container}/config", h.UpdateContainerConfig)
	r.Post("/workloads/{kind}/{namespace}/{name}/suspend", h.SetSuspend)
	r.Get("/workloads/{kind}/{namespace}/{name}/revisions", h.Revisions)
	r.Post("/workloads/{kind}/{namespace}/{name}/rollback", h.Rollback)
	r.Delete("/workloads/{kind}/{namespace}/{name}", h.Delete)
	r.Get("/pods", h.ListPods)
	r.Get("/pods/{namespace}/{name}", h.GetPod)
	r.Delete("/pods/{namespace}/{name}", h.DeletePod)
	r.Get("/pods/{namespace}/{name}/logs", h.Logs)
	r.Post("/pods/{namespace}/{name}/ephemeral-containers", h.CreateEphemeralContainer)
	r.Get("/pods/{namespace}/{name}/terminal", h.Terminal)
}

type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Columns uint16 `json:"columns,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Message string `json:"message,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (h *Handler) CreateEphemeralContainer(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var request domain.PodEphemeralContainerRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	result, err := h.service.CreateEphemeralContainer(r.Context(), actor, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), request)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, stdhttp.StatusCreated, result)
}

// Terminal upgrades the request to a same-origin WebSocket and bridges it to a
// Kubernetes SPDY exec stream. The Kubernetes SubjectAccessReview is checked by
// the application service before the remote shell is opened.
func (h *Handler) Terminal(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if !sameOriginWebSocket(r) {
		httpx.Error(w, r, fmt.Errorf("%w: websocket origin must match the request host", domain.ErrForbidden))
		return
	}
	actor, _ := domain.ActorFromContext(r.Context())
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	shell := httpx.Query(r, "shell")
	command := terminalCommand(shell)
	columns, rows := terminalSize(httpx.Query(r, "cols"), httpx.Query(r, "rows"))
	session, err := h.service.OpenTerminal(r.Context(), actor, namespace, name, domain.PodTerminalOptions{
		Container: httpx.Query(r, "container"),
		Command:   command,
		TTY:       true,
		Size:      domain.TerminalSize{Columns: columns, Rows: rows},
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer session.Close()
	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		var writeMu sync.Mutex
		send := func(message terminalMessage) bool {
			writeMu.Lock()
			defer writeMu.Unlock()
			return websocket.JSON.Send(conn, message) == nil
		}
		send(terminalMessage{Type: "ready"})
		governor := newTerminalGovernor(h.terminal)
		var terminateOnce sync.Once
		terminate := func(reason, message string) {
			terminateOnce.Do(func() {
				setTerminalCloseReason(session, reason)
				_ = send(terminalMessage{Type: "exit", Message: message, Reason: reason})
				_ = session.Close()
				_ = conn.Close()
			})
		}
		bridgeDone := make(chan struct{})
		defer close(bridgeDone)
		go func() {
			interval := terminalCheckInterval(h.terminal)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-bridgeDone:
					return
				case now := <-ticker.C:
					reason, message := governor.expired(now)
					if reason != "" {
						terminate(reason, message)
						return
					}
				}
			}
		}()
		go func() {
			buffer := make([]byte, 4096)
			for {
				count, err := session.Read(buffer)
				if count > 0 {
					now := time.Now()
					governor.touch(now)
					if !governor.accountOutput(now, count) {
						terminate("output_limit", "终端输出超过服务端限额，会话已自动关闭。请缩小命令输出范围后重新连接。")
						return
					}
					if !send(terminalMessage{Type: "output", Data: base64.StdEncoding.EncodeToString(buffer[:count])}) {
						return
					}
				}
				if err != nil {
					waitErr := session.Wait()
					message := "会话已结束"
					if waitErr != nil {
						message = waitErr.Error()
					}
					terminate("remote_exit", message)
					return
				}
			}
		}()
		for {
			var message terminalMessage
			if err := websocket.JSON.Receive(conn, &message); err != nil {
				terminate("client_disconnect", "客户端已断开终端连接。")
				return
			}
			switch message.Type {
			case "input":
				data, err := base64.StdEncoding.DecodeString(message.Data)
				if err != nil {
					_ = send(terminalMessage{Type: "error", Message: "输入编码无效"})
					continue
				}
				if len(data) > 8192 {
					_ = send(terminalMessage{Type: "error", Message: "单次输入过大"})
					continue
				}
				governor.touch(time.Now())
				if _, err := session.Write(data); err != nil {
					terminate("write_error", err.Error())
					return
				}
			case "resize":
				if message.Columns >= 20 && message.Columns <= 300 && message.Rows >= 5 && message.Rows <= 100 {
					governor.touch(time.Now())
					session.Resize(domain.TerminalSize{Columns: message.Columns, Rows: message.Rows})
				}
			}
		}
	}).ServeHTTP(w, r)
}

type terminalGovernor struct {
	limits      domain.TerminalLimits
	started     time.Time
	mu          sync.Mutex
	lastActive  time.Time
	windowStart time.Time
	windowBytes int64
}

func newTerminalGovernor(limits domain.TerminalLimits) *terminalGovernor {
	now := time.Now()
	return &terminalGovernor{limits: limits, started: now, lastActive: now, windowStart: now}
}

func (g *terminalGovernor) touch(now time.Time) {
	g.mu.Lock()
	g.lastActive = now
	g.mu.Unlock()
}

func (g *terminalGovernor) accountOutput(now time.Time, bytes int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Sub(g.windowStart) >= time.Second {
		g.windowStart = now
		g.windowBytes = 0
	}
	g.windowBytes += int64(bytes)
	return g.limits.MaxOutputBytesPerSecond <= 0 || g.windowBytes <= g.limits.MaxOutputBytesPerSecond
}

func (g *terminalGovernor) expired(now time.Time) (string, string) {
	g.mu.Lock()
	started, lastActive := g.started, g.lastActive
	g.mu.Unlock()
	if g.limits.MaxDuration > 0 && now.Sub(started) >= g.limits.MaxDuration {
		return "max_duration", "终端达到服务端最大会话时长，已自动关闭。请重新连接后继续操作。"
	}
	if g.limits.IdleTimeout > 0 && now.Sub(lastActive) >= g.limits.IdleTimeout {
		return "idle_timeout", "终端长时间无输入或输出，已自动关闭。请重新连接后继续操作。"
	}
	return "", ""
}

func terminalCheckInterval(limits domain.TerminalLimits) time.Duration {
	interval := time.Second
	if limits.IdleTimeout > 0 && limits.IdleTimeout/4 < interval {
		interval = limits.IdleTimeout / 4
	}
	if interval < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return interval
}

func setTerminalCloseReason(session domain.PodTerminalSession, reason string) {
	if setter, ok := session.(interface{ SetCloseReason(string) }); ok {
		setter.SetCloseReason(reason)
	}
}

func sameOriginWebSocket(r *stdhttp.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func terminalCommand(shell string) []string {
	switch shell {
	case "bash":
		return []string{"/bin/bash"}
	case "ash":
		return []string{"/bin/ash"}
	default:
		return []string{"/bin/sh"}
	}
}

func terminalSize(columnsRaw, rowsRaw string) (uint16, uint16) {
	columns, _ := strconv.Atoi(columnsRaw)
	rows, _ := strconv.Atoi(rowsRaw)
	if columns < 20 || columns > 300 {
		columns = 100
	}
	if rows < 5 || rows > 100 {
		rows = 30
	}
	return uint16(columns), uint16(rows)
}
