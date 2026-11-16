package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
)

type portForwardRecord struct {
	session domain.PortForwardSession
	owner   string
	closer  io.Closer
	timer   *time.Timer
}

// OpenPortForward authorizes a Pod or Service port-forward, resolves a Service
// to one ready Pod, and opens a loopback-only session. The browser receives a
// random tunnel path, never an upstream address.
func (s *Service) OpenPortForward(ctx context.Context, actor domain.Actor, target domain.PortForwardTarget) (domain.PortForwardSession, error) {
	const action = "open-port-forward"
	target.Kind = strings.ToLower(strings.TrimSpace(target.Kind))
	target.Namespace = strings.TrimSpace(target.Namespace)
	target.Name = strings.TrimSpace(target.Name)
	if target.Kind != "pod" && target.Kind != "service" {
		err := fmt.Errorf("%w: port-forward target kind must be pod or service", domain.ErrInvalid)
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}
	if target.Namespace == "" || target.Name == "" || target.Port < 1 || target.Port > 65535 {
		err := fmt.Errorf("%w: namespace, name and a port between 1 and 65535 are required", domain.ErrInvalid)
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}
	if target.Kind == "service" {
		access := domain.Access{Verb: "get", Resource: "services", Namespace: target.Namespace, Name: target.Name}
		if err := s.authorizer.Require(ctx, actor, access); err != nil {
			s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
			return domain.PortForwardSession{}, err
		}
	}
	resolved, err := s.repository.ResolvePortForwardTarget(ctx, target)
	if err != nil {
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}
	access := domain.Access{Verb: "create", Resource: "pods", Subresource: "portforward", Namespace: resolved.Namespace, Name: resolved.Pod}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, "portforward", resolved.Namespace, resolved.Pod, err)
		return domain.PortForwardSession{}, err
	}
	expired := s.prunePortForwards(time.Now())
	closePortForwards(expired)
	releaseSlot, err := s.reservePortForwardSlot(actor.Username)
	if err != nil {
		s.audit(ctx, actor, action, "portforward", resolved.Namespace, resolved.Pod, err)
		return domain.PortForwardSession{}, err
	}
	address, closer, err := s.repository.OpenPodPortForward(ctx, resolved.Namespace, resolved.Pod, resolved.Port)
	if err != nil {
		releaseSlot()
		s.audit(ctx, actor, action, "portforward", resolved.Namespace, resolved.Pod, err)
		return domain.PortForwardSession{}, err
	}
	idBytes := make([]byte, 24)
	if _, err := rand.Read(idBytes); err != nil {
		_ = closer.Close()
		releaseSlot()
		err = fmt.Errorf("generate port-forward session id: %w", err)
		s.audit(ctx, actor, action, "portforward", resolved.Namespace, resolved.Pod, err)
		return domain.PortForwardSession{}, err
	}
	id := hex.EncodeToString(idBytes)
	s.forwardMu.Lock()
	limit := s.forward.MaxDuration
	expiresAt := time.Now().Add(limit)
	record := &portForwardRecord{
		session: domain.PortForwardSession{
			ID: id, Namespace: resolved.Namespace, Pod: resolved.Pod, Service: resolved.Service,
			Port: resolved.Port, Path: "/api/v1/tunnels/" + id + "/", ExpiresAt: expiresAt, LocalAddress: address,
		},
		owner: actor.Username, closer: closer,
	}
	s.forwards[id] = record
	record.timer = time.AfterFunc(limit, func() { s.expirePortForward(id) })
	session := record.session
	s.forwardMu.Unlock()
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: action, Resource: "portforward", Namespace: resolved.Namespace, Name: resolved.Pod, Result: "success",
		Details: map[string]any{"targetKind": target.Kind, "requestedPort": target.Port, "podPort": resolved.Port, "service": resolved.Service, "expiresAt": expiresAt},
	})
	return session, nil
}

// GetPortForward returns one unexpired tunnel owned by the current actor.
func (s *Service) GetPortForward(ctx context.Context, actor domain.Actor, id string) (domain.PortForwardSession, error) {
	if strings.TrimSpace(id) == "" {
		return domain.PortForwardSession{}, fmt.Errorf("%w: tunnel id is required", domain.ErrInvalid)
	}
	expired := s.prunePortForwards(time.Now())
	closePortForwards(expired)
	s.forwardMu.Lock()
	record, ok := s.forwards[id]
	if !ok || record.owner != portForwardOwner(actor.Username) {
		s.forwardMu.Unlock()
		return domain.PortForwardSession{}, fmt.Errorf("%w: port-forward session not found", domain.ErrNotFound)
	}
	session := record.session
	s.forwardMu.Unlock()
	return session, nil
}

// ClosePortForward revokes a tunnel owned by the current actor.
func (s *Service) ClosePortForward(ctx context.Context, actor domain.Actor, id string) error {
	const action = "close-port-forward"
	record, ok := s.takePortForward(id, actor.Username)
	if !ok {
		return fmt.Errorf("%w: port-forward session not found", domain.ErrNotFound)
	}
	_ = record.closer.Close()
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: action, Resource: "portforward", Namespace: record.session.Namespace, Name: record.session.Pod,
		Result: "success", Details: map[string]any{"reason": "client_closed", "port": record.session.Port},
	})
	return nil
}

func (s *Service) reservePortForwardSlot(username string) (func(), error) {
	key := portForwardOwner(username)
	s.forwardMu.Lock()
	defer s.forwardMu.Unlock()
	if s.forwardCounts[key] >= s.forward.MaxSessionsPerUser {
		return nil, fmt.Errorf("%w: port-forward session limit reached for this user", domain.ErrRateLimited)
	}
	s.forwardCounts[key]++
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.forwardMu.Lock()
			defer s.forwardMu.Unlock()
			s.decrementForwardCountLocked(key)
		})
	}
	return release, nil
}

func portForwardOwner(username string) string {
	if username == "" {
		return "anonymous"
	}
	return username
}

func (s *Service) prunePortForwards(now time.Time) []*portForwardRecord {
	s.forwardMu.Lock()
	defer s.forwardMu.Unlock()
	expired := make([]*portForwardRecord, 0)
	for id, record := range s.forwards {
		if !record.session.ExpiresAt.After(now) {
			expired = append(expired, record)
			delete(s.forwards, id)
			s.decrementForwardCountLocked(record.owner)
		}
	}
	return expired
}

func closePortForwards(records []*portForwardRecord) {
	for _, record := range records {
		if record.timer != nil {
			record.timer.Stop()
		}
		_ = record.closer.Close()
	}
}

func (s *Service) takePortForward(id, username string) (*portForwardRecord, bool) {
	s.forwardMu.Lock()
	defer s.forwardMu.Unlock()
	record, ok := s.forwards[id]
	if !ok || record.owner != portForwardOwner(username) {
		return nil, false
	}
	delete(s.forwards, id)
	s.decrementForwardCountLocked(record.owner)
	if record.timer != nil {
		record.timer.Stop()
	}
	return record, true
}

func (s *Service) expirePortForward(id string) {
	record, ok := s.takePortForwardByID(id)
	if !ok {
		return
	}
	_ = record.closer.Close()
	s.auditor.Record(context.Background(), domain.Actor{Username: record.owner}, domain.AuditEvent{
		Action: "close-port-forward", Resource: "portforward", Namespace: record.session.Namespace, Name: record.session.Pod,
		Result: "success", Details: map[string]any{"reason": "expired", "port": record.session.Port},
	})
}

func (s *Service) takePortForwardByID(id string) (*portForwardRecord, bool) {
	s.forwardMu.Lock()
	defer s.forwardMu.Unlock()
	record, ok := s.forwards[id]
	if !ok {
		return nil, false
	}
	delete(s.forwards, id)
	s.decrementForwardCountLocked(record.owner)
	return record, true
}

func (s *Service) decrementForwardCountLocked(owner string) {
	if s.forwardCounts[owner] <= 1 {
		delete(s.forwardCounts, owner)
		return
	}
	s.forwardCounts[owner]--
}
