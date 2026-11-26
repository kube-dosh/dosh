package workloadshttp

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dosh/internal/domain"
	workloadsapp "dosh/internal/modules/workloads/app"
	"dosh/internal/platform/audit"
	"dosh/internal/platform/authz"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

type tunnelTestCloser struct {
	mu     sync.Mutex
	closed int
}

func (c *tunnelTestCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *tunnelTestCloser) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type tunnelTestRepository struct {
	*workloadRepo
	mu       sync.Mutex
	address  string
	closers  []*tunnelTestCloser
	openFunc func() error
}

func (r *tunnelTestRepository) OpenPodPortForward(_ context.Context, _, _ string, _ int32) (string, io.Closer, error) {
	if r.openFunc != nil {
		if err := r.openFunc(); err != nil {
			return "", nil, err
		}
	}
	closer := &tunnelTestCloser{}
	r.mu.Lock()
	r.closers = append(r.closers, closer)
	r.mu.Unlock()
	return r.address, closer, nil
}

func (r *tunnelTestRepository) snapshotClosers() []*tunnelTestCloser {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*tunnelTestCloser(nil), r.closers...)
}

func newTunnelTestHandler(t *testing.T, repository *tunnelTestRepository) (stdhttp.Handler, *workloadsapp.Service, domain.Actor) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := workloadsapp.New(repository, authz.NewLocal(false), audit.New(logger))
	service.SetPortForwardLimits(domain.PortForwardLimits{MaxDuration: time.Minute, MaxSessionsPerUser: 4, MaxPortsPerSession: 4})
	handler := New(service)
	actor := domain.Actor{Username: "alice", Admin: true}
	router := chi.NewRouter()
	router.Use(func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			next.ServeHTTP(w, r.WithContext(domain.WithActor(r.Context(), actor)))
		})
	})
	router.Get("/api/v1/tunnels/{id}", handler.RedirectTunnel)
	router.Get("/api/v1/tunnels/{id}/ports/{port}", handler.RedirectTunnel)
	router.Get("/api/v1/tunnels/{id}/ports/{port}/*", handler.Tunnel)
	return router, service, actor
}

func TestTunnelProxiesRequestAndClosesOnDemandSPDYConnection(t *testing.T) {
	var forwardedPath, forwardedQuery, forwardedCookie, forwardedHost string
	upstream := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		forwardedPath = r.URL.Path
		forwardedQuery = r.URL.RawQuery
		forwardedCookie = r.Header.Get("Cookie")
		forwardedHost = r.Host
		w.Header().Add("Set-Cookie", "app_session=upstream; Path=/app; HttpOnly")
		w.Header().Set("Location", "/next")
		w.WriteHeader(stdhttp.StatusFound)
	}))
	defer upstream.Close()

	repository := &tunnelTestRepository{
		workloadRepo: &workloadRepo{}, address: strings.TrimPrefix(upstream.URL, "http://"),
	}
	router, service, actor := newTunnelTestHandler(t, repository)
	session, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{
		Kind: "pod", Namespace: "default", Name: "nginx", Port: 80,
	})
	if err != nil {
		t.Fatalf("OpenPortForward() error = %v", err)
	}
	closers := repository.snapshotClosers()
	if len(closers) != 1 || closers[0].count() != 1 {
		t.Fatalf("preflight closers = %#v", closers)
	}

	request := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tunnels/"+session.ID+"/ports/80/app?trace=1", nil)
	request.Header.Set("Cookie", "dosh_session=secret; app=1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusFound {
		t.Fatalf("tunnel status = %d, body = %s", response.Code, response.Body.String())
	}
	if forwardedPath != "/app" || forwardedQuery != "trace=1" || forwardedCookie != "app=1" || forwardedHost != strings.TrimPrefix(upstream.URL, "http://") {
		t.Fatalf("upstream request = path %q query %q cookie %q host %q", forwardedPath, forwardedQuery, forwardedCookie, forwardedHost)
	}
	if location := response.Header().Get("Location"); location != session.Path+"next" {
		t.Fatalf("Location = %q, want %q", location, session.Path+"next")
	}
	cookies := response.Header().Values("Set-Cookie")
	if len(cookies) != 1 || !strings.Contains(cookies[0], "Path="+session.Path+"app") || !strings.Contains(cookies[0], "HttpOnly") || strings.Contains(cookies[0], "dosh_session") {
		t.Fatalf("Set-Cookie = %#v", cookies)
	}
	closers = repository.snapshotClosers()
	if len(closers) != 2 || closers[0].count() != 1 || closers[1].count() != 1 {
		t.Fatalf("on-demand closers were not closed: %#v", closers)
	}

	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/tunnels/"+session.ID, nil))
	if redirect.Code != stdhttp.StatusTemporaryRedirect || redirect.Header().Get("Location") != session.Path {
		t.Fatalf("redirect = %d %q", redirect.Code, redirect.Header().Get("Location"))
	}
	if got := len(repository.snapshotClosers()); got != 2 {
		t.Fatalf("redirect opened %d unexpected SPDY connections", got-2)
	}
}

func TestTunnelRejectsOtherOwnerAndReleasesFailedPreflight(t *testing.T) {
	repository := &tunnelTestRepository{workloadRepo: &workloadRepo{}, address: "127.0.0.1:1"}
	_, service, actor := newTunnelTestHandler(t, repository)
	session, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{Kind: "pod", Namespace: "default", Name: "nginx", Port: 80})
	if err != nil {
		t.Fatalf("OpenPortForward() error = %v", err)
	}
	if _, _, err := service.GetPortForwardEndpoint(context.Background(), domain.Actor{Username: "bob"}, session.ID, 80); err == nil {
		t.Fatal("other owner opened a tunnel endpoint")
	}
	repository.openFunc = func() error { return domain.ErrUnavailable }
	if _, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{Kind: "pod", Namespace: "default", Name: "failed", Port: 80}); err == nil {
		t.Fatal("preflight failure returned a session")
	}
	items, err := service.ListPortForwards(context.Background(), actor)
	if err != nil {
		t.Fatalf("ListPortForwards() error = %v", err)
	}
	for _, item := range items {
		if item.Pod == "failed" {
			t.Fatalf("failed preflight session was persisted: %#v", item)
		}
	}
}

func TestTunnelProxiesWebSocketUpgradeAndClosesOnDisconnect(t *testing.T) {
	upstream := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			return
		}
		_ = websocket.Message.Send(conn, "echo:"+message)
	}))
	defer upstream.Close()

	repository := &tunnelTestRepository{
		workloadRepo: &workloadRepo{}, address: strings.TrimPrefix(upstream.URL, "http://"),
	}
	router, service, actor := newTunnelTestHandler(t, repository)
	session, err := service.OpenPortForward(context.Background(), actor, domain.PortForwardTarget{
		Kind: "pod", Namespace: "default", Name: "websocket", Port: 80,
	})
	if err != nil {
		t.Fatalf("OpenPortForward() error = %v", err)
	}
	server := httptest.NewServer(router)
	defer server.Close()

	tunnelURL := "ws" + strings.TrimPrefix(server.URL, "http") + session.Path + "socket"
	conn, err := websocket.Dial(tunnelURL, "", "http://example.com/")
	if err != nil {
		t.Fatalf("websocket.Dial(%q) error = %v", tunnelURL, err)
	}
	if err := websocket.Message.Send(conn, "hello"); err != nil {
		_ = conn.Close()
		t.Fatalf("send through tunnel: %v", err)
	}
	var response string
	if err := websocket.Message.Receive(conn, &response); err != nil {
		_ = conn.Close()
		t.Fatalf("receive through tunnel: %v", err)
	}
	if response != "echo:hello" {
		_ = conn.Close()
		t.Fatalf("tunnel response = %q", response)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close tunnel websocket: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		closers := repository.snapshotClosers()
		if len(closers) >= 2 && closers[1].count() == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("WebSocket tunnel closer was not released: %#v", repository.snapshotClosers())
}
