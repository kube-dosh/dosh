package audit

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"dosh/internal/domain"
	"dosh/internal/platform/requestid"
)

func TestAuditHistoryIsBoundedAndNewestFirst(t *testing.T) {
	logger, err := NewWithConfig(slog.New(slog.NewJSONHandler(io.Discard, nil)), Config{HistoryLimit: 3})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	for _, action := range []string{"one", "two", "three", "four"} {
		logger.Record(context.Background(), domain.Actor{Username: "admin"}, domain.AuditEvent{Action: action, Resource: "deployment", Result: "success"})
	}

	records, err := logger.ListHistory(context.Background(), domain.AuditQuery{})
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}
	for index, want := range []string{"four", "three", "two"} {
		if records[index].Action != want {
			t.Fatalf("records[%d].Action = %q, want %q", index, records[index].Action, want)
		}
	}
	if records[0].ID != 4 || records[2].ID != 2 {
		t.Fatalf("record IDs = %d..%d, want 4..2", records[0].ID, records[2].ID)
	}
}

func TestAuditHistoryFiltersAndCapturesRequestContext(t *testing.T) {
	logger, err := NewWithConfig(slog.New(slog.NewJSONHandler(io.Discard, nil)), Config{HistoryLimit: 10})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	ctx := requestid.With(context.Background(), "req-123")
	logger.Record(ctx, domain.Actor{Username: "alice@example.test", Groups: []string{"dosh-admins"}}, domain.AuditEvent{
		Action: "reveal", Resource: "secret", Namespace: "prod", Name: "database", Result: "success",
	})
	logger.Record(context.Background(), domain.Actor{Username: "bob@example.test"}, domain.AuditEvent{
		Action: "scale", Resource: "deployment", Namespace: "dev", Name: "api", Result: "denied", Err: domain.ErrForbidden,
	})

	records, err := logger.ListHistory(context.Background(), domain.AuditQuery{Namespace: "prod", Search: "alice", Result: "success"})
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].RequestID != "req-123" || len(records[0].Groups) != 1 || records[0].Groups[0] != "dosh-admins" {
		t.Fatalf("record = %#v, want request ID and groups", records[0])
	}

	records, err = logger.ListHistory(context.Background(), domain.AuditQuery{Search: "operation forbidden"})
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if len(records) != 1 || records[0].Error == "" {
		t.Fatalf("records = %#v, want one denied record with error", records)
	}
}

func TestAuditHistoryConcurrentAppend(t *testing.T) {
	logger, err := NewWithConfig(slog.New(slog.NewJSONHandler(io.Discard, nil)), Config{HistoryLimit: 64})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 200; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			logger.Record(context.Background(), domain.Actor{Username: "admin"}, domain.AuditEvent{Action: "scale", Resource: "deployment", Result: "success"})
		}()
	}
	wait.Wait()
	records, err := logger.ListHistory(context.Background(), domain.AuditQuery{})
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if len(records) != 64 {
		t.Fatalf("len(records) = %d, want 64", len(records))
	}
	if records[0].ID != 200 {
		t.Fatalf("newest ID = %d, want 200", records[0].ID)
	}
}
