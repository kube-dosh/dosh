package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"dosh/internal/domain"
)

type Repository interface {
	ListHistory(ctx context.Context, query domain.AuditQuery) ([]domain.AuditRecord, error)
}

type Service struct {
	repository Repository
	authorizer domain.Authorizer
	auditor    domain.Auditor
}

func New(repository Repository, authorizer domain.Authorizer) *Service {
	auditor, _ := repository.(domain.Auditor)
	return &Service{repository: repository, authorizer: authorizer, auditor: auditor}
}

func (s *Service) List(ctx context.Context, query domain.AuditQuery) ([]domain.AuditRecord, error) {
	actor, ok := domain.ActorFromContext(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "get", APIGroup: "dosh.io", Resource: "audit-events"}); err != nil {
		return nil, err
	}
	return s.repository.ListHistory(ctx, query)
}

// Export returns the matching retained snapshot and records the export itself
// as an audit action. The new record is appended after the snapshot was read,
// so it cannot recursively appear in the file being exported.
func (s *Service) Export(ctx context.Context, query domain.AuditQuery, format string) ([]domain.AuditRecord, error) {
	records, err := s.List(ctx, query)
	if s.auditor != nil {
		if actor, ok := domain.ActorFromContext(ctx); ok {
			result := "success"
			if err != nil {
				result = "failure"
				if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
					result = "denied"
				}
			}
			s.auditor.Record(ctx, actor, domain.AuditEvent{
				Action: "export-audit", Resource: "audit-events", Namespace: query.Namespace,
				Result: result, Err: err, Details: map[string]any{"format": format, "count": len(records)},
			})
		}
	}
	return records, err
}

// Actions returns the distinct action names present in the retained audit
// snapshot. The directory is derived from actual records so new audited
// operations become filterable without maintaining a second frontend enum.
func (s *Service) Actions(ctx context.Context) ([]string, error) {
	actor, ok := domain.ActorFromContext(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	if err := s.authorizer.Require(ctx, actor, domain.Access{Verb: "get", APIGroup: "dosh.io", Resource: "audit-events"}); err != nil {
		return nil, err
	}
	records, err := s.repository.ListHistory(ctx, domain.AuditQuery{})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		action := strings.TrimSpace(record.Action)
		if action != "" {
			seen[action] = struct{}{}
		}
	}
	actions := make([]string, 0, len(seen))
	for action := range seen {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions, nil
}
