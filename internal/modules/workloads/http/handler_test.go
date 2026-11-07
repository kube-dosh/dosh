package workloadshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	authapp "dosh/internal/modules/auth/app"
	authhttp "dosh/internal/modules/auth/http"
	workloadsapp "dosh/internal/modules/workloads/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"
	"dosh/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const workloadTestPassword = "a-valid-test-password"

type workloadRepo struct {
	scaleCalls       int
	restartCalls     int
	imageCalls       int
	suspendCalls     int
	deleteCalls      int
	lastKind         string
	lastNamespace    string
	lastName         string
	lastReplicas     int32
	lastResourceVer  string
	lastImage        domain.WorkloadImageUpdate
	lastSuspend      domain.CronJobSuspendUpdate
	lastPropagation  domain.DeletionPropagation
	lastTailLines    int64
	lastQuery        domain.ListQuery
	scaleErr         error
	restartErr       error
	imageErr         error
	suspendErr       error
	deleteErr        error
	logsErr          error
	logsPayload      string
	logsReader       io.ReadCloser
	workloads        []domain.WorkloadSummary
	workloadDetail   domain.WorkloadDetail
	pods             []domain.PodSummary
	podDetail        domain.PodDetail
	listWorkloadsErr error
	createCalls      []bool
	previewCalls     int
	createErr        error
	previewYAML      string
}

func (r *workloadRepo) ListWorkloads(_ context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error) {
	r.lastQuery = query
	return r.workloads, r.listWorkloadsErr
}

func (r *workloadRepo) GetWorkload(_ context.Context, kind, namespace, name string) (domain.WorkloadDetail, error) {
	r.lastKind, r.lastNamespace, r.lastName = kind, namespace, name
	return r.workloadDetail, nil
}

func (r *workloadRepo) CreateDeployment(_ context.Context, spec domain.DeploymentCreateSpec, dryRun bool) (domain.WorkloadSummary, error) {
	r.createCalls = append(r.createCalls, dryRun)
	r.lastNamespace, r.lastName = spec.Namespace, spec.Name
	if r.createErr != nil {
		return domain.WorkloadSummary{}, r.createErr
	}
	return domain.WorkloadSummary{Kind: "Deployment", Name: spec.Name, Namespace: spec.Namespace, Desired: spec.Replicas}, nil
}

func (r *workloadRepo) PreviewDeployment(_ context.Context, spec domain.DeploymentCreateSpec) (string, error) {
	r.previewCalls++
	r.lastNamespace, r.lastName = spec.Namespace, spec.Name
	return r.previewYAML, nil
}

func (r *workloadRepo) ScaleWorkload(_ context.Context, kind, namespace, name string, replicas int32, expectedResourceVersion string) error {
	r.scaleCalls++
	r.lastKind, r.lastNamespace, r.lastName = kind, namespace, name
	r.lastReplicas, r.lastResourceVer = replicas, expectedResourceVersion
	return r.scaleErr
}

func (r *workloadRepo) RestartWorkload(_ context.Context, kind, namespace, name string) error {
	r.restartCalls++
	r.lastKind, r.lastNamespace, r.lastName = kind, namespace, name
	return r.restartErr
}

func (r *workloadRepo) UpdateWorkloadImage(_ context.Context, kind, namespace, name string, update domain.WorkloadImageUpdate) error {
	r.imageCalls++
	r.lastKind, r.lastNamespace, r.lastName = kind, namespace, name
	r.lastImage = update
	return r.imageErr
}

func (r *workloadRepo) SetCronJobSuspend(_ context.Context, namespace, name string, update domain.CronJobSuspendUpdate) error {
	r.suspendCalls++
	r.lastNamespace, r.lastName = namespace, name
	r.lastSuspend = update
	return r.suspendErr
}

func (r *workloadRepo) DeleteWorkload(_ context.Context, kind, namespace, name string, propagation domain.DeletionPropagation) error {
	r.deleteCalls++
	r.lastKind, r.lastNamespace, r.lastName = kind, namespace, name
	r.lastPropagation = propagation
	return r.deleteErr
}

func (r *workloadRepo) ListPods(context.Context, domain.ListQuery) ([]domain.PodSummary, error) {
	return r.pods, nil
}

func (r *workloadRepo) GetPod(_ context.Context, namespace, name string) (domain.PodDetail, error) {
	r.lastNamespace, r.lastName = namespace, name
	return r.podDetail, nil
}

func (r *workloadRepo) DeletePod(_ context.Context, namespace, name string) error {
	r.lastNamespace, r.lastName = namespace, name
	return nil
}

func (r *workloadRepo) StreamPodLogs(_ context.Context, namespace, name, container string, _ bool, tailLines int64, _ bool, _ int64) (io.ReadCloser, error) {
	r.lastNamespace, r.lastName = namespace, name
	r.lastTailLines = tailLines
	if r.logsErr != nil {
		return nil, r.logsErr
	}
	if r.logsReader != nil {
		return r.logsReader, nil
	}
	return io.NopCloser(strings.NewReader(r.logsPayload)), nil
}

type failingLogReader struct {
	data []byte
	err  error
	sent bool
}

func (r *failingLogReader) Read(buffer []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(buffer, r.data), nil
	}
	return 0, r.err
}

func (*failingLogReader) Close() error { return nil }

type chunkLogReader struct {
	chunks [][]byte
	index  int
}

func (r *chunkLogReader) Read(buffer []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(buffer, chunk), nil
}

func (*chunkLogReader) Close() error { return nil }

type logFrame struct {
	Type      string `json:"type"`
	Data      string `json:"data"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func decodeLogFrames(t *testing.T, body io.Reader) []logFrame {
	t.Helper()
	decoder := json.NewDecoder(body)
	var frames []logFrame
	for {
		var frame logFrame
		err := decoder.Decode(&frame)
		if err == io.EOF {
			return frames
		}
		if err != nil {
			t.Fatalf("decode log frame: %v", err)
		}
		frames = append(frames, frame)
	}
}

type workloadTestServer struct {
	router http.Handler
	repo   *workloadRepo
	cookie *http.Cookie
	csrf   string
}

func newWorkloadTestServer(t *testing.T) *workloadTestServer {
	t.Helper()
	hash, err := authapp.HashPassword(workloadTestPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	manager, err := authapp.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 8*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}
	authorizer := authz.NewLocal(false)
	sessions := authapp.NewSessionService(manager, authorizer, "local")
	authService := authapp.NewService(authapp.Config{Username: "admin", PasswordHash: hash}, sessions)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	authHandler := authhttp.New(authhttp.Options{Mode: "local", Local: authService, Sessions: sessions, Audit: audit.New(logger), Logger: logger, CookieSecure: true})

	repo := &workloadRepo{logsPayload: "line one\nline two\n"}
	service := workloadsapp.New(repo, authorizer, audit.New(logger))
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.RequestIDContext)
	router.Route("/api/v1", func(r chi.Router) {
		authhttp.MountPublic(r, authHandler)
		r.Group(func(r chi.Router) {
			r.Use(authHandler.RequireSession)
			r.Use(authHandler.RequireCSRF)
			New(service).Mount(r)
		})
	})

	server := &workloadTestServer{router: router, repo: repo}
	response := performWorkloadJSON(router, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"`+workloadTestPassword+`"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	server.cookie = response.Result().Cookies()[0]
	server.csrf = payload.CSRFToken
	return server
}

func performWorkloadJSON(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func (s *workloadTestServer) authHeaders() map[string]string {
	return map[string]string{"Cookie": s.cookie.String(), "X-CSRF-Token": s.csrf}
}

func TestWorkloadListAndDetail(t *testing.T) {
	server := newWorkloadTestServer(t)
	server.repo.workloads = []domain.WorkloadSummary{{Name: "api", Namespace: "prod", Kind: "Deployment"}}
	response := performWorkloadJSON(server.router, http.MethodGet, "/api/v1/workloads?namespace=prod&kind=deployment&status=updating&q=api", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []domain.WorkloadSummary `json:"items"`
		Meta  httpx.ListMeta           `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Name != "api" || payload.Meta.Status != "updating" || payload.Meta.Kind != "deployment" {
		t.Fatalf("unexpected list body: %s", response.Body.String())
	}
	if server.repo.lastQuery.Status != "updating" || server.repo.lastQuery.Namespace != "prod" || server.repo.lastQuery.Kind != "deployment" || server.repo.lastQuery.Search != "api" {
		t.Fatalf("list query = %#v", server.repo.lastQuery)
	}

	server.repo.workloadDetail = domain.WorkloadDetail{WorkloadSummary: domain.WorkloadSummary{Name: "api", Namespace: "prod", Kind: "Deployment"}}
	response = performWorkloadJSON(server.router, http.MethodGet, "/api/v1/workloads/deployment/prod/api", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", response.Code, response.Body.String())
	}
	if server.repo.lastKind != "deployment" || server.repo.lastNamespace != "prod" || server.repo.lastName != "api" {
		t.Fatalf("detail params = %q %q %q", server.repo.lastKind, server.repo.lastNamespace, server.repo.lastName)
	}
}

func TestScaleRequiresSessionCSRFAndValidPayload(t *testing.T) {
	server := newWorkloadTestServer(t)
	body := `{"replicas":3,"resourceVersion":"42"}`
	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/scale", body, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", response.Code)
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/scale", body, map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.Code)
	}
	if server.repo.scaleCalls != 0 {
		t.Fatal("repository was called before authentication/CSRF passed")
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/scale", `{"resourceVersion":"42"}`, server.authHeaders())
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing replicas status = %d, want 400", response.Code)
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/scale", body, server.authHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("scale status = %d, body = %s", response.Code, response.Body.String())
	}
	if server.repo.scaleCalls != 1 || server.repo.lastReplicas != 3 || server.repo.lastResourceVer != "42" {
		t.Fatalf("scale call = calls %d replicas %d rv %q", server.repo.scaleCalls, server.repo.lastReplicas, server.repo.lastResourceVer)
	}
}

func TestWriteErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		repoErr    error
		wantStatus int
	}{
		{name: "conflict", repoErr: domain.ErrConflict, wantStatus: http.StatusConflict},
		{name: "forbidden", repoErr: domain.ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "not found", repoErr: domain.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "unavailable", repoErr: domain.ErrUnavailable, wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newWorkloadTestServer(t)
			server.repo.scaleErr = tt.repoErr
			response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/scale", `{"replicas":2}`, server.authHeaders())
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestUnsupportedKindAndInvalidPropagation(t *testing.T) {
	server := newWorkloadTestServer(t)
	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/custom/prod/api/scale", `{"replicas":2}`, server.authHeaders())
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported kind status = %d, want 501", response.Code)
	}
	response = performWorkloadJSON(server.router, http.MethodDelete, "/api/v1/workloads/deployment/prod/api?propagation=Invalid", "", server.authHeaders())
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid propagation status = %d, want 400", response.Code)
	}
}

func TestRestartDeleteAndLogs(t *testing.T) {
	server := newWorkloadTestServer(t)
	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/restart", "", server.authHeaders())
	if response.Code != http.StatusOK || server.repo.restartCalls != 1 {
		t.Fatalf("restart status = %d calls = %d", response.Code, server.repo.restartCalls)
	}
	response = performWorkloadJSON(server.router, http.MethodDelete, "/api/v1/workloads/deployment/prod/api?propagation=Foreground", "", server.authHeaders())
	if response.Code != http.StatusAccepted || server.repo.deleteCalls != 1 {
		t.Fatalf("delete status = %d calls = %d", response.Code, server.repo.deleteCalls)
	}
	if server.repo.lastPropagation != domain.DeletePropagationForeground {
		t.Fatalf("propagation = %q", server.repo.lastPropagation)
	}

	response = performWorkloadJSON(server.router, http.MethodGet, "/api/v1/pods/prod/api/logs?tailLines=999999", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("logs Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" {
		t.Fatalf("logs Content-Type = %q", response.Header().Get("Content-Type"))
	}
	frames := decodeLogFrames(t, response.Body)
	if len(frames) != 2 || frames[0].Type != "log" || frames[0].Data != "line one\nline two\n" || frames[1].Type != "end" {
		t.Fatalf("logs frames = %#v", frames)
	}
	if server.repo.lastTailLines != 5000 {
		t.Fatalf("tailLines = %d", server.repo.lastTailLines)
	}
}

func TestLogsEmitStructuredTerminalErrorAndPreflightErrors(t *testing.T) {
	server := newWorkloadTestServer(t)
	server.repo.logsReader = &failingLogReader{data: []byte("partial line\n"), err: domain.ErrUnavailable}
	response := performWorkloadJSON(server.router, http.MethodGet, "/api/v1/pods/prod/api/logs?follow=true", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", response.Code, response.Body.String())
	}
	frames := decodeLogFrames(t, response.Body)
	if len(frames) != 2 || frames[0].Type != "log" || frames[0].Data != "partial line\n" {
		t.Fatalf("log frames = %#v", frames)
	}
	if frames[1].Type != "error" || frames[1].Code != "dependency_unavailable" || frames[1].Message == "" || frames[1].RequestID == "" {
		t.Fatalf("terminal error frame = %#v", frames[1])
	}

	server.repo.logsReader = nil
	server.repo.logsErr = domain.ErrNotFound
	response = performWorkloadJSON(server.router, http.MethodGet, "/api/v1/pods/prod/missing/logs", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("preflight status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestLogsPreserveUTF8SplitAcrossReads(t *testing.T) {
	server := newWorkloadTestServer(t)
	firstRune := []byte("日")
	server.repo.logsReader = &chunkLogReader{chunks: [][]byte{
		append([]byte("prefix "), firstRune[0]),
		append(append([]byte(nil), firstRune[1:]...), []byte("志\n")...),
	}}

	response := performWorkloadJSON(server.router, http.MethodGet, "/api/v1/pods/prod/api/logs", "", map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", response.Code, response.Body.String())
	}
	frames := decodeLogFrames(t, response.Body)
	if len(frames) != 3 || frames[0].Type != "log" || frames[0].Data != "prefix " || frames[1].Data != "日志\n" || frames[2].Type != "end" {
		t.Fatalf("UTF-8 split frames = %#v", frames)
	}
}

func TestUpdateImageRequiresSessionCSRFAndValidPayload(t *testing.T) {
	server := newWorkloadTestServer(t)
	body := `{"containerName":"api","image":"registry.test/api:2","resourceVersion":"42"}`
	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/image", body, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", response.Code)
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/image", body, map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.Code)
	}
	if server.repo.imageCalls != 0 {
		t.Fatal("repository was called before authentication/CSRF passed")
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/image", `{"containerName":"api"}`, server.authHeaders())
	if response.Code != http.StatusBadRequest || server.repo.imageCalls != 0 {
		t.Fatalf("missing image status = %d calls = %d body = %s", response.Code, server.repo.imageCalls, response.Body.String())
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/image", body, server.authHeaders())
	if response.Code != http.StatusOK || server.repo.imageCalls != 1 {
		t.Fatalf("image status = %d calls = %d body = %s", response.Code, server.repo.imageCalls, response.Body.String())
	}
	if server.repo.lastImage != (domain.WorkloadImageUpdate{ContainerName: "api", Image: "registry.test/api:2", ResourceVersion: "42"}) {
		t.Fatalf("image update = %#v", server.repo.lastImage)
	}
	server.repo.imageErr = domain.ErrConflict
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployment/prod/api/image", body, server.authHeaders())
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", response.Code, response.Body.String())
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/job/prod/migrate/image", body, server.authHeaders())
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("job image status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSetSuspendRequiresSessionCSRFAndValidPayload(t *testing.T) {
	server := newWorkloadTestServer(t)
	body := `{"suspend":true,"resourceVersion":"42"}`
	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/cronjob/prod/backup/suspend", body, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", response.Code)
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/cronjob/prod/backup/suspend", body, map[string]string{"Cookie": server.cookie.String()})
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.Code)
	}
	if server.repo.suspendCalls != 0 {
		t.Fatal("repository was called before authentication/CSRF passed")
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/cronjob/prod/backup/suspend", `{"resourceVersion":"42"}`, server.authHeaders())
	if response.Code != http.StatusBadRequest || server.repo.suspendCalls != 0 {
		t.Fatalf("missing suspend status = %d calls = %d body = %s", response.Code, server.repo.suspendCalls, response.Body.String())
	}
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/cronjob/prod/backup/suspend", body, server.authHeaders())
	if response.Code != http.StatusOK || server.repo.suspendCalls != 1 {
		t.Fatalf("suspend status = %d calls = %d body = %s", response.Code, server.repo.suspendCalls, response.Body.String())
	}
	if server.repo.lastSuspend != (domain.CronJobSuspendUpdate{Suspend: true, ResourceVersion: "42"}) || server.repo.lastNamespace != "prod" || server.repo.lastName != "backup" {
		t.Fatalf("suspend update = %#v namespace = %q name = %q", server.repo.lastSuspend, server.repo.lastNamespace, server.repo.lastName)
	}
}

func TestCreateDeploymentValidatesBeforeRepositoryAndReturnsCreated(t *testing.T) {
	server := newWorkloadTestServer(t)
	payload := `{"name":"api","namespace":"prod","replicas":2,"containerName":"api","image":"registry.test/api:1","containerPort":8080,"cpuRequestMilli":100,"cpuLimitMilli":500,"memoryRequestBytes":134217728,"memoryLimitBytes":536870912,"env":[{"name":"MODE","value":"prod"}],"labels":{"team":"platform"}}`

	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployments", `{"name":"Bad_Name","namespace":"prod","replicas":1,"containerName":"api","image":"nginx:alpine","containerPort":80}`, server.authHeaders())
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(server.repo.createCalls) != 0 {
		t.Fatalf("repository called for invalid request: %#v", server.repo.createCalls)
	}

	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployments", payload, server.authHeaders())
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(server.repo.createCalls) != 2 || !server.repo.createCalls[0] || server.repo.createCalls[1] {
		t.Fatalf("create calls = %#v", server.repo.createCalls)
	}
	if server.repo.lastName != "api" || server.repo.lastNamespace != "prod" {
		t.Fatalf("created resource = %s/%s", server.repo.lastNamespace, server.repo.lastName)
	}
}

func TestPreviewDeploymentAndCreateConflict(t *testing.T) {
	server := newWorkloadTestServer(t)
	server.repo.previewYAML = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n"
	payload := `{"name":"api","namespace":"prod","replicas":1,"containerName":"api","image":"nginx:alpine","containerPort":80}`
	response := performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployments/preview", payload, server.authHeaders())
	if response.Code != http.StatusOK || server.repo.previewCalls != 1 || !strings.Contains(response.Body.String(), "kind: Deployment") {
		t.Fatalf("preview status=%d calls=%d body=%s", response.Code, server.repo.previewCalls, response.Body.String())
	}
	server.repo.createErr = domain.ErrAlreadyExists
	response = performWorkloadJSON(server.router, http.MethodPost, "/api/v1/workloads/deployments", payload, server.authHeaders())
	if response.Code != http.StatusConflict || len(server.repo.createCalls) != 1 || !strings.Contains(response.Body.String(), `"code":"already_exists"`) {
		t.Fatalf("conflict status=%d calls=%#v body=%s", response.Code, server.repo.createCalls, response.Body.String())
	}
}
