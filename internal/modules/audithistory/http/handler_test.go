package audithistoryhttp

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dosh/internal/domain"
	audithistoryapp "dosh/internal/modules/audithistory/app"
)

type fakeRepository struct{ records []domain.AuditRecord }

func (f fakeRepository) ListHistory(_ context.Context, _ domain.AuditQuery) ([]domain.AuditRecord, error) {
	return f.records, nil
}

type allowAuthorizer struct{}

func (allowAuthorizer) Require(context.Context, domain.Actor, domain.Access) error { return nil }

func TestListDefaultsToNewestFirstAndAppliesFilters(t *testing.T) {
	now := time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC)
	service := audithistoryapp.New(fakeRepository{records: []domain.AuditRecord{
		{ID: 2, Timestamp: now, Action: "reveal", Resource: "secret", Namespace: "prod", Result: "success"},
	}}, allowAuthorizer{})
	handler := New(service)
	request := httptest.NewRequest(stdhttp.MethodGet, "/audit-events?namespace=prod&result=success&q=reveal&sort=id&order=desc", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	response := httptest.NewRecorder()

	handler.List(response, request)

	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []domain.AuditRecord `json:"items"`
		Meta  struct {
			Total   int               `json:"total"`
			Sort    string            `json:"sort"`
			Order   string            `json:"order"`
			Filters map[string]string `json:"filters"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != 2 || body.Meta.Total != 1 {
		t.Fatalf("body = %#v, want only record 2", body)
	}
	if body.Meta.Sort != "id" || body.Meta.Order != "desc" || body.Meta.Filters["namespace"] != "prod" || body.Meta.Filters["result"] != "success" {
		t.Fatalf("meta = %#v, want sort/order/filters", body.Meta)
	}
}

func TestListRejectsUnauthenticatedRequest(t *testing.T) {
	handler := New(audithistoryapp.New(fakeRepository{}, allowAuthorizer{}))
	response := httptest.NewRecorder()
	handler.List(response, httptest.NewRequest(stdhttp.MethodGet, "/audit-events", nil))
	if response.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestListRejectsUnknownResult(t *testing.T) {
	handler := New(audithistoryapp.New(fakeRepository{}, allowAuthorizer{}))
	request := httptest.NewRequest(stdhttp.MethodGet, "/audit-events?result=unknown", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	response := httptest.NewRecorder()
	handler.List(response, request)
	if response.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}
