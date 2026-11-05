package eventshttp

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
	eventsapp "dosh/internal/modules/events/app"

	"github.com/go-chi/chi/v5"
)

type eventRepo struct {
	events   []domain.Event
	watchCh  chan domain.Event
	errCh    chan error
	watchErr error
	query    domain.ListQuery
}

func (r *eventRepo) ListEvents(_ context.Context, query domain.ListQuery) ([]domain.Event, error) {
	r.query = query
	return r.events, nil
}

func (r *eventRepo) WatchEvents(context.Context, string) (<-chan domain.Event, <-chan error, error) {
	if r.watchErr != nil {
		return nil, nil, r.watchErr
	}
	return r.watchCh, r.errCh, nil
}

func eventTestRouter(repo *eventRepo) http.Handler {
	router := chi.NewRouter()
	New(eventsapp.New(repo)).Mount(router)
	return router
}

func TestListEvents(t *testing.T) {
	repo := &eventRepo{events: []domain.Event{{Type: "Warning", Reason: "BackOff", ObjectName: "api", Namespace: "prod"}}}
	request := httptest.NewRequest(http.MethodGet, "/events?namespace=prod&q=backoff&objectKind=Pod&objectName=api&pageSize=10", nil)
	response := httptest.NewRecorder()
	eventTestRouter(repo).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"reason":"BackOff"`) || !strings.Contains(response.Body.String(), `"namespace":"prod"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	for _, want := range []string{`"page":1`, `"pageSize":10`, `"totalPages":1`, `"objectKind":"Pod"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, missing %s", response.Body.String(), want)
		}
	}
	if repo.query.ObjectKind != "Pod" || repo.query.ObjectName != "api" {
		t.Fatalf("query = %#v", repo.query)
	}
}

func TestListEventsRejectsUnsupportedSort(t *testing.T) {
	repo := &eventRepo{}
	request := httptest.NewRequest(http.MethodGet, "/events?sort=message", nil)
	response := httptest.NewRecorder()
	eventTestRouter(repo).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"fieldErrors":{"sort"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEventsStreamDeliversEventAndHeartbeatFrames(t *testing.T) {
	repo := &eventRepo{watchCh: make(chan domain.Event), errCh: make(chan error)}
	server := httptest.NewServer(eventTestRouter(repo))
	defer server.Close()

	response, err := http.Get(server.URL + "/events/stream?namespace=prod")
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content type = %q", response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != ": connected\n" {
		t.Fatalf("first frame = %q, err = %v", first, err)
	}
	repo.watchCh <- domain.Event{Type: "Normal", Reason: "Created", ObjectName: "api", Namespace: "prod"}
	deadline := time.Now().Add(2 * time.Second)
	var body strings.Builder
	for time.Now().Before(deadline) {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("read event frame: %v", readErr)
		}
		body.WriteString(line)
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}
	if !strings.Contains(body.String(), "event: event") || !strings.Contains(body.String(), `"reason":"Created"`) {
		t.Fatalf("event frame = %q", body.String())
	}
}

func TestEventsStreamMapsWatchFailure(t *testing.T) {
	repo := &eventRepo{watchErr: domain.ErrUnavailable}
	request := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	response := httptest.NewRecorder()
	eventTestRouter(repo).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"dependency_unavailable"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}
