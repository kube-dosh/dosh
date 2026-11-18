package audithistoryhttp

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
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

func TestActionsReturnsDistinctDirectory(t *testing.T) {
	service := audithistoryapp.New(fakeRepository{records: []domain.AuditRecord{
		{ID: 1, Action: "scale"}, {ID: 2, Action: "diagnose"}, {ID: 3, Action: "scale"},
	}}, allowAuthorizer{})
	handler := New(service)
	request := httptest.NewRequest(stdhttp.MethodGet, "/audit-events/actions", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	response := httptest.NewRecorder()

	handler.Actions(response, request)

	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 2 || body.Items[0] != "diagnose" || body.Items[1] != "scale" {
		t.Fatalf("items = %#v", body.Items)
	}
}

func TestExportCSVFiltersAndNeutralizesSpreadsheetFormulas(t *testing.T) {
	now := time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC)
	service := audithistoryapp.New(fakeRepository{records: []domain.AuditRecord{{
		ID: 42, Timestamp: now, User: "=cmd|' /C calc'!A0", Groups: []string{"+team"},
		Action: "-delete", Resource: "@resource", Namespace: "prod", Name: "api", Result: "success",
		RequestID: "=request", Error: "+error",
	}}}, allowAuthorizer{})
	handler := New(service)
	request := httptest.NewRequest(stdhttp.MethodGet, "/audit-events/export?format=csv&namespace=prod&result=success&action=-delete&q=api", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	response := httptest.NewRecorder()

	handler.Export(response, request)

	if response.Code != stdhttp.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), "dosh-audit-") {
		t.Fatalf("content-disposition = %q", response.Header().Get("Content-Disposition"))
	}
	records, err := csv.NewReader(strings.NewReader(response.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("decode csv: %v", err)
	}
	if len(records) != 2 || records[0][0] != "id" || records[1][0] != "42" {
		t.Fatalf("csv records = %#v", records)
	}
	for _, index := range []int{2, 3, 4, 5, 9, 10} {
		if !strings.HasPrefix(records[1][index], "'") {
			t.Fatalf("csv column %d was not neutralized: %q", index, records[1][index])
		}
	}
}

func TestExportJSONAndRejectsUnknownFormat(t *testing.T) {
	service := audithistoryapp.New(fakeRepository{records: []domain.AuditRecord{{ID: 7, Action: "diagnose"}}}, allowAuthorizer{})
	handler := New(service)
	request := httptest.NewRequest(stdhttp.MethodGet, "/audit-events/export?format=json", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	response := httptest.NewRecorder()

	handler.Export(response, request)

	if response.Code != stdhttp.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload struct {
		Count int                  `json:"count"`
		Items []domain.AuditRecord `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 || payload.Items[0].ID != 7 {
		t.Fatalf("payload = %#v", payload)
	}

	badRequest := httptest.NewRequest(stdhttp.MethodGet, "/audit-events/export?format=xml", nil)
	badRequest = badRequest.WithContext(domain.WithActor(badRequest.Context(), domain.Actor{Username: "admin", Admin: true}))
	badResponse := httptest.NewRecorder()
	handler.Export(badResponse, badRequest)
	if badResponse.Code != stdhttp.StatusBadRequest {
		t.Fatalf("unknown format status = %d, want 400", badResponse.Code)
	}
}
