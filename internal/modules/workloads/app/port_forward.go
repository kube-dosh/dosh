package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
)

// PortForwardSessionStore persists short-lived tunnel metadata outside a single
// Dosh process. The Kubernetes adapter stores owner-scoped sessions in the
// control plane, so a refresh or a request routed to another replica can still
// list, proxy and close the same session. Only metadata is stored; the SPDY
// channel is opened on demand by the replica serving the request.
type PortForwardSessionStore interface {
	CreatePortForward(ctx context.Context, owner string, session domain.PortForwardSession, maxSessions int) error
	GetPortForward(ctx context.Context, owner, id string) (domain.PortForwardSession, error)
	ListPortForwards(ctx context.Context, owner string) ([]domain.PortForwardSession, error)
	DeletePortForward(ctx context.Context, owner, id string) error
	DeleteAllPortForwards(ctx context.Context, owner string) (int, error)
}

type memoryPortForwardStore struct {
	mu       sync.Mutex
	sessions map[string]map[string]domain.PortForwardSession
}

func newMemoryPortForwardStore() *memoryPortForwardStore {
	return &memoryPortForwardStore{sessions: make(map[string]map[string]domain.PortForwardSession)}
}

func (s *memoryPortForwardStore) CreatePortForward(_ context.Context, owner string, session domain.PortForwardSession, maxSessions int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.sessions[owner]
	if items == nil {
		items = make(map[string]domain.PortForwardSession)
		s.sessions[owner] = items
	}
	pruneExpiredPortForwardSessions(items, time.Now())
	if maxSessions > 0 && len(items) >= maxSessions {
		return fmt.Errorf("%w: port-forward session limit reached for this user", domain.ErrRateLimited)
	}
	items[session.ID] = clonePortForwardSession(session)
	return nil
}

func (s *memoryPortForwardStore) GetPortForward(_ context.Context, owner, id string) (domain.PortForwardSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[owner][id]
	if !ok {
		return domain.PortForwardSession{}, fmt.Errorf("%w: port-forward session not found", domain.ErrNotFound)
	}
	return clonePortForwardSession(session), nil
}

func (s *memoryPortForwardStore) ListPortForwards(_ context.Context, owner string) ([]domain.PortForwardSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.sessions[owner]
	pruneExpiredPortForwardSessions(items, time.Now())
	result := make([]domain.PortForwardSession, 0, len(items))
	for _, session := range items {
		result = append(result, clonePortForwardSession(session))
	}
	return result, nil
}

func (s *memoryPortForwardStore) DeletePortForward(_ context.Context, owner, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.sessions[owner]
	if _, ok := items[id]; !ok {
		return fmt.Errorf("%w: port-forward session not found", domain.ErrNotFound)
	}
	delete(items, id)
	if len(items) == 0 {
		delete(s.sessions, owner)
	}
	return nil
}

func (s *memoryPortForwardStore) DeleteAllPortForwards(_ context.Context, owner string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := len(s.sessions[owner])
	delete(s.sessions, owner)
	return count, nil
}

func pruneExpiredPortForwardSessions(items map[string]domain.PortForwardSession, now time.Time) {
	for id, session := range items {
		if !session.ExpiresAt.After(now) {
			delete(items, id)
		}
	}
}

// OpenPortForward authorizes one or more TCP ports on a Pod or Service,
// resolves a Service to ready backing Pods, validates that each target port can
// be forwarded, and persists a user-bound session. A multi-port request is
// atomic: either every port is validated or no session is stored.
func (s *Service) OpenPortForward(ctx context.Context, actor domain.Actor, target domain.PortForwardTarget) (domain.PortForwardSession, error) {
	const action = "open-port-forward"
	target.Kind = strings.ToLower(strings.TrimSpace(target.Kind))
	target.Namespace = strings.TrimSpace(target.Namespace)
	target.Name = strings.TrimSpace(target.Name)
	target.PodName = strings.TrimSpace(target.PodName)
	if target.Kind != "pod" && target.Kind != "service" {
		err := fmt.Errorf("%w: port-forward target kind must be pod or service", domain.ErrInvalid)
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}
	if target.Namespace == "" || target.Name == "" {
		err := fmt.Errorf("%w: namespace and target name are required", domain.ErrInvalid)
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}
	ports, err := normalizePortForwardPorts(target, s.forward.MaxPortsPerSession)
	if err != nil {
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

	resolved := make([]domain.PortForwardPod, 0, len(ports))
	authorizedPods := make(map[string]struct{})
	for _, port := range ports {
		request := target
		request.Port = port
		request.Ports = nil
		item, resolveErr := s.repository.ResolvePortForwardTarget(ctx, request)
		if resolveErr != nil {
			s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, resolveErr)
			return domain.PortForwardSession{}, resolveErr
		}
		key := item.Namespace + "/" + item.Pod
		if _, ok := authorizedPods[key]; !ok {
			access := domain.Access{Verb: "create", Resource: "pods", Subresource: "portforward", Namespace: item.Namespace, Name: item.Pod}
			if err := s.authorizer.Require(ctx, actor, access); err != nil {
				s.audit(ctx, actor, action, "portforward", item.Namespace, item.Pod, err)
				return domain.PortForwardSession{}, err
			}
			authorizedPods[key] = struct{}{}
		}
		resolved = append(resolved, item)
		// A multi-port Service tunnel must stay on one backend Pod. Pinning the
		// first resolution prevents port 80 and port 443, for example, from
		// landing on different replicas with inconsistent application state.
		if target.Kind == "service" && target.PodName == "" {
			target.PodName = item.Pod
		}
	}

	idBytes := make([]byte, 24)
	if _, err := rand.Read(idBytes); err != nil {
		err = fmt.Errorf("generate port-forward session id: %w", err)
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}
	id := hex.EncodeToString(idBytes)
	now := time.Now().UTC()
	expiresAt := now.Add(s.forward.MaxDuration)
	session := domain.PortForwardSession{
		ID: id, Namespace: resolved[0].Namespace, Pod: resolved[0].Pod, Service: resolved[0].Service,
		ExpiresAt: expiresAt, CreatedAt: now,
	}
	for index, item := range resolved {
		portID, idErr := randomPortForwardID()
		if idErr != nil {
			s.audit(ctx, actor, action, "portforward", item.Namespace, item.Pod, idErr)
			return domain.PortForwardSession{}, idErr
		}
		session.Ports = append(session.Ports, domain.PortForwardPort{
			ID: portID, Namespace: item.Namespace, Pod: item.Pod, Service: item.Service,
			RequestedPort: ports[index], Port: item.Port, Protocol: "TCP", Path: tunnelPortPath(id, ports[index]), ExpiresAt: expiresAt,
		})
	}
	sort.SliceStable(session.Ports, func(left, right int) bool {
		return session.Ports[left].RequestedPort < session.Ports[right].RequestedPort
	})
	session.Port = session.Ports[0].Port
	session.Path = session.Ports[0].Path
	if err := s.portForwardStore().CreatePortForward(ctx, actor.Username, session, s.forward.MaxSessionsPerUser); err != nil {
		s.audit(ctx, actor, action, "portforward", target.Namespace, target.Name, err)
		return domain.PortForwardSession{}, err
	}

	// Reserve the session before opening any SPDY channel so a user at their
	// session limit cannot repeatedly force expensive Kubernetes connections.
	// The reservation is removed if any port fails its preflight.
	for _, item := range resolved {
		_, closer, openErr := s.repository.OpenPodPortForward(ctx, item.Namespace, item.Pod, item.Port)
		if openErr != nil {
			_ = s.portForwardStore().DeletePortForward(ctx, actor.Username, session.ID)
			s.audit(ctx, actor, action, "portforward", item.Namespace, item.Pod, openErr)
			return domain.PortForwardSession{}, openErr
		}
		if closer != nil {
			_ = closer.Close()
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: action, Resource: "portforward", Namespace: session.Namespace, Name: session.Pod, Result: "success",
		Details: map[string]any{
			"targetKind": target.Kind, "requestedPorts": ports, "service": session.Service,
			"pod": session.Pod, "expiresAt": expiresAt, "portCount": len(ports),
		},
	})
	return clonePortForwardSession(session), nil
}

// GetPortForward returns one unexpired tunnel owned by the current actor.
func (s *Service) GetPortForward(ctx context.Context, actor domain.Actor, id string) (domain.PortForwardSession, error) {
	return s.getPortForwardRecord(ctx, actor, id)
}

// ListPortForwardBackendPods returns the Pods selected by a Service without
// imposing the list-page limit used by the general Pod browser. The UI uses
// this list to pin a multi-port Service tunnel to a specific ready replica.
func (s *Service) ListPortForwardBackendPods(ctx context.Context, actor domain.Actor, namespace, serviceName string) ([]domain.PodSummary, error) {
	namespace = strings.TrimSpace(namespace)
	serviceName = strings.TrimSpace(serviceName)
	if namespace == "" || serviceName == "" {
		return nil, fmt.Errorf("%w: namespace and Service name are required", domain.ErrInvalid)
	}
	for _, access := range []domain.Access{
		{Verb: "get", Resource: "services", Namespace: namespace, Name: serviceName},
		{Verb: "list", Resource: "pods", Namespace: namespace},
	} {
		if err := s.authorizer.Require(ctx, actor, access); err != nil {
			return nil, err
		}
	}
	lister, ok := s.repository.(PortForwardBackendLister)
	if !ok {
		return nil, fmt.Errorf("%w: Service backend discovery is unavailable", domain.ErrUnsupported)
	}
	return lister.ListPortForwardBackendPods(ctx, namespace, serviceName)
}

// ListPortForwards returns every unexpired tunnel owned by the current actor.
// Because sessions are persisted in the control plane, this view is identical
// on every Dosh replica and after a browser refresh.
func (s *Service) ListPortForwards(ctx context.Context, actor domain.Actor) ([]domain.PortForwardSession, error) {
	items, err := s.portForwardStore().ListPortForwards(ctx, actor.Username)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	filtered := make([]domain.PortForwardSession, 0, len(items))
	for _, item := range items {
		if item.ExpiresAt.After(now) {
			filtered = append(filtered, item)
		}
	}
	sort.SliceStable(filtered, func(left, right int) bool {
		if filtered[left].Namespace != filtered[right].Namespace {
			return filtered[left].Namespace < filtered[right].Namespace
		}
		if filtered[left].Pod != filtered[right].Pod {
			return filtered[left].Pod < filtered[right].Pod
		}
		return filtered[left].ID < filtered[right].ID
	})
	return filtered, nil
}

// GetPortForwardEndpoint resolves one requested port and opens a fresh SPDY
// channel on the replica handling this request. The caller must close the
// returned closer when the proxied HTTP exchange or WebSocket finishes.
func (s *Service) GetPortForwardEndpoint(ctx context.Context, actor domain.Actor, id string, requestedPort int32) (domain.PortForwardEndpoint, io.Closer, error) {
	endpoint, err := s.GetPortForwardRoute(ctx, actor, id, requestedPort)
	if err != nil {
		return domain.PortForwardEndpoint{}, nil, err
	}
	localAddress, closer, err := s.repository.OpenPodPortForward(ctx, endpoint.Namespace, endpoint.Pod, endpoint.Port)
	if err != nil {
		return domain.PortForwardEndpoint{}, nil, err
	}
	endpoint.LocalAddress = localAddress
	return endpoint, closer, nil
}

// GetPortForwardRoute validates a live user-bound session and returns the
// public proxy route without opening a Kubernetes connection. Redirect and
// metadata requests use this path so they do not allocate SPDY listeners.
func (s *Service) GetPortForwardRoute(ctx context.Context, actor domain.Actor, id string, requestedPort int32) (domain.PortForwardEndpoint, error) {
	session, err := s.getPortForwardRecord(ctx, actor, id)
	if err != nil {
		return domain.PortForwardEndpoint{}, err
	}
	if requestedPort <= 0 {
		requestedPort = session.Ports[0].RequestedPort
	}
	var selected *domain.PortForwardPort
	for index := range session.Ports {
		if session.Ports[index].RequestedPort == requestedPort {
			selected = &session.Ports[index]
			break
		}
	}
	if selected == nil {
		return domain.PortForwardEndpoint{}, fmt.Errorf("%w: port-forward port %d not found", domain.ErrNotFound, requestedPort)
	}
	return domain.PortForwardEndpoint{
		ID: selected.ID, Namespace: selected.Namespace, Pod: selected.Pod, Service: selected.Service,
		RequestedPort: selected.RequestedPort, Port: selected.Port, Path: selected.Path,
		ExpiresAt: selected.ExpiresAt,
	}, nil
}

func (s *Service) getPortForwardRecord(ctx context.Context, actor domain.Actor, id string) (domain.PortForwardSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.PortForwardSession{}, fmt.Errorf("%w: tunnel id is required", domain.ErrInvalid)
	}
	session, err := s.portForwardStore().GetPortForward(ctx, actor.Username, id)
	if err != nil {
		return domain.PortForwardSession{}, err
	}
	if !session.ExpiresAt.After(time.Now()) {
		_ = s.portForwardStore().DeletePortForward(ctx, actor.Username, id)
		return domain.PortForwardSession{}, fmt.Errorf("%w: port-forward session not found", domain.ErrNotFound)
	}
	return session, nil
}

// ClosePortForward revokes every endpoint in a tunnel session owned by the
// current actor.
func (s *Service) ClosePortForward(ctx context.Context, actor domain.Actor, id string) error {
	const action = "close-port-forward"
	session, err := s.getPortForwardRecord(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := s.portForwardStore().DeletePortForward(ctx, actor.Username, id); err != nil {
		return err
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: action, Resource: "portforward", Namespace: session.Namespace, Name: session.Pod,
		Result: "success", Details: map[string]any{"reason": "client_closed", "ports": session.Ports},
	})
	return nil
}

// CloseAllPortForwards revokes every live tunnel owned by the actor.
func (s *Service) CloseAllPortForwards(ctx context.Context, actor domain.Actor) (int, error) {
	closed, err := s.portForwardStore().DeleteAllPortForwards(ctx, actor.Username)
	if err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{
			Action: "close-all-port-forwards", Resource: "portforward", Result: "failure", Err: err,
		})
		return 0, err
	}
	if closed == 0 {
		return 0, nil
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: "close-all-port-forwards", Resource: "portforward", Result: "success",
		Details: map[string]any{"closedSessions": closed},
	})
	return closed, nil
}

func (s *Service) portForwardStore() PortForwardSessionStore {
	if store, ok := s.repository.(PortForwardSessionStore); ok {
		return store
	}
	return s.memoryForwards
}

func normalizePortForwardPorts(target domain.PortForwardTarget, maximum int) ([]int32, error) {
	if maximum <= 0 || maximum > 32 {
		maximum = domain.DefaultPortForwardLimits().MaxPortsPerSession
	}
	values := target.Ports
	if len(values) == 0 && target.Port > 0 {
		values = []int32{target.Port}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: at least one port between 1 and 65535 is required", domain.ErrInvalid)
	}
	if len(values) > maximum {
		return nil, fmt.Errorf("%w: a port-forward session supports at most %d ports", domain.ErrInvalid, maximum)
	}
	seen := make(map[int32]struct{}, len(values))
	ports := make([]int32, 0, len(values))
	for _, port := range values {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("%w: every port must be between 1 and 65535", domain.ErrInvalid)
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	sort.Slice(ports, func(left, right int) bool { return ports[left] < ports[right] })
	return ports, nil
}

func randomPortForwardID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate port-forward endpoint id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func tunnelPortPath(sessionID string, port int32) string {
	return fmt.Sprintf("/api/v1/tunnels/%s/ports/%d/", sessionID, port)
}

func clonePortForwardSession(session domain.PortForwardSession) domain.PortForwardSession {
	session.Ports = append([]domain.PortForwardPort(nil), session.Ports...)
	return session
}
