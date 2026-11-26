package kube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	portForwardCatalogPrefix = "dosh-port-forward-"
	portForwardCatalogKey    = "sessions.json"
	portForwardCatalogLabel  = "dosh.io/component"
	portForwardOwnerLabel    = "dosh.io/port-forward-owner"
	portForwardMutationTries = 8
)

type portForwardCatalog struct {
	Sessions []domain.PortForwardSession `json:"sessions"`
}

// CreatePortForward persists a user-bound tunnel catalog using optimistic
// concurrency. Each user has a separate catalog so unrelated users never
// contend for the same resourceVersion and the session cap is enforced
// atomically across Dosh replicas.
func (c *Client) CreatePortForward(ctx context.Context, owner string, session domain.PortForwardSession, maxSessions int) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(session.ID) == "" {
		return fmt.Errorf("%w: port-forward owner and session id are required", domain.ErrInvalid)
	}
	if !session.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("%w: port-forward session is already expired", domain.ErrInvalid)
	}
	catalog, err := c.mutatePortForwardCatalog(ctx, owner, func(items []domain.PortForwardSession, now time.Time) ([]domain.PortForwardSession, error) {
		items = prunePortForwardCatalog(items, now)
		if maxSessions > 0 && len(items) >= maxSessions {
			return nil, fmt.Errorf("%w: port-forward session limit reached for this user", domain.ErrRateLimited)
		}
		for _, item := range items {
			if item.ID == session.ID {
				return nil, fmt.Errorf("%w: port-forward session %q", domain.ErrAlreadyExists, session.ID)
			}
		}
		return append(items, clonePortForwardCatalogSession(session)), nil
	})
	if err != nil {
		return err
	}
	_ = catalog
	return nil
}

func (c *Client) GetPortForward(ctx context.Context, owner, id string) (domain.PortForwardSession, error) {
	if strings.TrimSpace(owner) == "" {
		return domain.PortForwardSession{}, fmt.Errorf("%w: port-forward owner is required", domain.ErrInvalid)
	}
	if id == "" {
		return domain.PortForwardSession{}, fmt.Errorf("%w: port-forward session id is required", domain.ErrInvalid)
	}
	items, err := c.readPortForwardCatalog(ctx, owner)
	if err != nil {
		return domain.PortForwardSession{}, err
	}
	now := time.Now().UTC()
	for _, item := range items {
		if item.ID == id && item.ExpiresAt.After(now) {
			return clonePortForwardCatalogSession(item), nil
		}
	}
	return domain.PortForwardSession{}, fmt.Errorf("%w: port-forward session %q", domain.ErrNotFound, id)
}

func (c *Client) ListPortForwards(ctx context.Context, owner string) ([]domain.PortForwardSession, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("%w: port-forward owner is required", domain.ErrInvalid)
	}
	items, err := c.readPortForwardCatalog(ctx, owner)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result := make([]domain.PortForwardSession, 0, len(items))
	for _, item := range items {
		if item.ExpiresAt.After(now) {
			result = append(result, clonePortForwardCatalogSession(item))
		}
	}
	return result, nil
}

// DeletePortForward is owner-scoped by construction: the catalog name is
// derived from a hash of the authenticated owner and never accepted from the
// caller as a raw object name.
func (c *Client) DeletePortForward(ctx context.Context, owner, id string) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: port-forward owner and session id are required", domain.ErrInvalid)
	}
	found := false
	_, err := c.mutatePortForwardCatalog(ctx, owner, func(items []domain.PortForwardSession, now time.Time) ([]domain.PortForwardSession, error) {
		items = prunePortForwardCatalog(items, now)
		result := items[:0]
		for _, item := range items {
			if item.ID == id {
				found = true
				continue
			}
			result = append(result, item)
		}
		if !found {
			return nil, fmt.Errorf("%w: port-forward session %q", domain.ErrNotFound, id)
		}
		return result, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) DeleteAllPortForwards(ctx context.Context, owner string) (int, error) {
	if strings.TrimSpace(owner) == "" {
		return 0, fmt.Errorf("%w: port-forward owner is required", domain.ErrInvalid)
	}
	closed := 0
	_, err := c.mutatePortForwardCatalog(ctx, owner, func(items []domain.PortForwardSession, now time.Time) ([]domain.PortForwardSession, error) {
		items = prunePortForwardCatalog(items, now)
		closed = len(items)
		return []domain.PortForwardSession{}, nil
	})
	return closed, err
}

func (c *Client) readPortForwardCatalog(ctx context.Context, owner string) ([]domain.PortForwardSession, error) {
	if err := c.validatePortForwardStore(); err != nil {
		return nil, err
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	item, err := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace).Get(ctx, portForwardCatalogName(owner), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return []domain.PortForwardSession{}, nil
	}
	if err != nil {
		return nil, mapKubeError(err)
	}
	return decodePortForwardCatalog(item)
}

type portForwardMutation func([]domain.PortForwardSession, time.Time) ([]domain.PortForwardSession, error)

func (c *Client) mutatePortForwardCatalog(ctx context.Context, owner string, mutate portForwardMutation) ([]domain.PortForwardSession, error) {
	if err := c.validatePortForwardStore(); err != nil {
		return nil, err
	}
	ctx = domain.WithoutActor(ctx)
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	name := portForwardCatalogName(owner)
	ownerHash := portForwardOwnerHash(owner)
	client := c.clientset.CoreV1().ConfigMaps(c.preferencesNamespace)
	for attempt := 0; attempt < portForwardMutationTries; attempt++ {
		current, err := client.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			items, mutationErr := mutate(nil, time.Now().UTC())
			if mutationErr != nil {
				return nil, mutationErr
			}
			payload, encodeErr := encodePortForwardCatalog(items)
			if encodeErr != nil {
				return nil, encodeErr
			}
			_, createErr := client.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: name, Namespace: c.preferencesNamespace,
					Labels: map[string]string{portForwardCatalogLabel: "port-forward", portForwardOwnerLabel: ownerHash},
				},
				Data: map[string]string{portForwardCatalogKey: payload},
			}, metav1.CreateOptions{FieldManager: "dosh"})
			if createErr == nil {
				return items, nil
			}
			if apierrors.IsAlreadyExists(createErr) || apierrors.IsConflict(createErr) {
				continue
			}
			return nil, mapKubeError(createErr)
		}
		if err != nil {
			return nil, mapKubeError(err)
		}
		items, err := decodePortForwardCatalog(current)
		if err != nil {
			return nil, err
		}
		updated, mutationErr := mutate(items, time.Now().UTC())
		if mutationErr != nil {
			return nil, mutationErr
		}
		payload, encodeErr := encodePortForwardCatalog(updated)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if current.Data == nil {
			current.Data = map[string]string{}
		}
		current.Data[portForwardCatalogKey] = payload
		if current.Labels == nil {
			current.Labels = map[string]string{}
		}
		current.Labels[portForwardCatalogLabel] = "port-forward"
		current.Labels[portForwardOwnerLabel] = ownerHash
		_, updateErr := client.Update(ctx, current, metav1.UpdateOptions{FieldManager: "dosh"})
		if updateErr == nil {
			return updated, nil
		}
		if apierrors.IsConflict(updateErr) {
			continue
		}
		return nil, mapKubeError(updateErr)
	}
	return nil, fmt.Errorf("%w: port-forward catalog changed concurrently; retry the request", domain.ErrConflict)
}

func (c *Client) validatePortForwardStore() error {
	if c.clientset == nil {
		return fmt.Errorf("%w: Kubernetes clientset is unavailable", domain.ErrUnavailable)
	}
	if c.preferencesNamespace == "" {
		return fmt.Errorf("%w: control-plane storage namespace is not configured", domain.ErrUnsupported)
	}
	return nil
}

func portForwardCatalogName(owner string) string {
	return portForwardCatalogPrefix + portForwardOwnerHash(owner)
}

func portForwardOwnerHash(owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return hex.EncodeToString(sum[:20])
}

func encodePortForwardCatalog(items []domain.PortForwardSession) (string, error) {
	if items == nil {
		items = []domain.PortForwardSession{}
	}
	data, err := json.Marshal(portForwardCatalog{Sessions: items})
	if err != nil {
		return "", fmt.Errorf("encode port-forward catalog: %w", err)
	}
	return string(data), nil
}

func decodePortForwardCatalog(configMap *corev1.ConfigMap) ([]domain.PortForwardSession, error) {
	raw := ""
	if configMap.Data != nil {
		raw = configMap.Data[portForwardCatalogKey]
	}
	if raw == "" {
		return []domain.PortForwardSession{}, nil
	}
	var catalog portForwardCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		return nil, fmt.Errorf("%w: decode port-forward catalog: %v", domain.ErrUnavailable, err)
	}
	if catalog.Sessions == nil {
		catalog.Sessions = []domain.PortForwardSession{}
	}
	return catalog.Sessions, nil
}

func prunePortForwardCatalog(items []domain.PortForwardSession, now time.Time) []domain.PortForwardSession {
	result := make([]domain.PortForwardSession, 0, len(items))
	for _, item := range items {
		if item.ExpiresAt.After(now) {
			result = append(result, item)
		}
	}
	return result
}

func clonePortForwardCatalogSession(session domain.PortForwardSession) domain.PortForwardSession {
	session.Ports = append([]domain.PortForwardPort(nil), session.Ports...)
	return session
}
