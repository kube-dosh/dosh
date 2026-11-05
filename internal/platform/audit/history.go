package audit

import (
	"strings"
	"sync"

	"dosh/internal/domain"
)

const (
	defaultHistoryLimit = 1000
	maxHistoryLimit     = 10000
)

type historyStore struct {
	mu      sync.RWMutex
	records []domain.AuditRecord
	next    int
	nextID  uint64
}

func newHistoryStore(limit int) *historyStore {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	return &historyStore{records: make([]domain.AuditRecord, 0, limit)}
}

func (s *historyStore) append(record domain.AuditRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	record.ID = s.nextID
	if len(s.records) < cap(s.records) {
		s.records = append(s.records, record)
		return
	}
	s.records[s.next] = record
	s.next = (s.next + 1) % len(s.records)
}

func (s *historyStore) list(query domain.AuditQuery) []domain.AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.AuditRecord, 0, len(s.records))
	for index := 0; index < len(s.records); index++ {
		position := index
		if len(s.records) == cap(s.records) {
			position = (s.next + index) % len(s.records)
		}
		record := s.records[position]
		if matchesAuditQuery(record, query) {
			result = append(result, record)
		}
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func matchesAuditQuery(record domain.AuditRecord, query domain.AuditQuery) bool {
	if query.Namespace != "" && record.Namespace != query.Namespace {
		return false
	}
	if query.Action != "" && !strings.EqualFold(record.Action, query.Action) {
		return false
	}
	if query.Result != "" && !strings.EqualFold(record.Result, query.Result) {
		return false
	}
	search := strings.ToLower(strings.TrimSpace(query.Search))
	if search == "" {
		return true
	}
	for _, value := range []string{
		record.User, record.Action, record.Resource, record.Namespace,
		record.Name, record.Result, record.RequestID, record.Error,
	} {
		if strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}
