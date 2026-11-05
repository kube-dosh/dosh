package kube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"dosh/internal/domain"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	loginAttemptAnnotation     = "dosh.io/login-attempts-v1"
	loginAttemptBucketHexChars = 2
	loginAttemptBucketEntries  = 16
	loginAttemptMaxAttempts    = 5
	loginAttemptWindow         = 5 * time.Minute
	loginAttemptBlockDuration  = 5 * time.Minute
	loginAttemptDefaultTimeout = 3 * time.Second
	loginAttemptDefaultRetries = 8
)

type loginAttemptRecord struct {
	Count       int   `json:"count"`
	First       int64 `json:"first"`
	LastSeen    int64 `json:"lastSeen"`
	BlockedTill int64 `json:"blockedTill,omitempty"`
}

// KubernetesLoginAttempts stores per-client login counters in sharded Lease
// objects. All replicas read and update the same state, so the attempt limit is
// enforced once per client IP instead of once per replica.
type KubernetesLoginAttempts struct {
	client      kubernetes.Interface
	namespace   string
	now         func() time.Time
	timeout     time.Duration
	retries     int
	maxAttempts int
	window      time.Duration
	blockFor    time.Duration
}

func NewKubernetesLoginAttempts(client kubernetes.Interface, namespace string) *KubernetesLoginAttempts {
	return &KubernetesLoginAttempts{
		client: client, namespace: namespace, now: time.Now,
		timeout: loginAttemptDefaultTimeout, retries: loginAttemptDefaultRetries,
		maxAttempts: loginAttemptMaxAttempts, window: loginAttemptWindow, blockFor: loginAttemptBlockDuration,
	}
}

func (c *Client) NewKubernetesLoginAttempts(namespace string) *KubernetesLoginAttempts {
	return NewKubernetesLoginAttempts(c.clientset, namespace)
}

func (l *KubernetesLoginAttempts) Begin(ctx context.Context, key string) (bool, error) {
	allowed := false
	err := l.mutate(ctx, key, func(state map[string]loginAttemptRecord, now time.Time) (bool, error) {
		keyHash := loginAttemptKeyHash(key)
		cleanupLoginAttempts(state, now, l.window)
		record, exists := state[keyHash]
		if exists && record.BlockedTill > 0 && now.UnixMilli() < record.BlockedTill {
			allowed = false
			return false, nil
		}
		if exists && now.Sub(time.UnixMilli(record.First)) >= l.window {
			delete(state, keyHash)
			record = loginAttemptRecord{}
			exists = false
		}
		if !exists {
			if !reserveLoginAttemptCapacity(state, now, l.window) {
				allowed = false
				return false, nil
			}
			record = loginAttemptRecord{First: now.UnixMilli()}
		}
		record.LastSeen = now.UnixMilli()
		if record.Count >= l.maxAttempts {
			record.BlockedTill = now.Add(l.blockFor).UnixMilli()
			state[keyHash] = record
			allowed = false
			return true, nil
		}
		record.Count++
		state[keyHash] = record
		allowed = true
		return true, nil
	})
	return allowed, err
}

func (l *KubernetesLoginAttempts) Fail(ctx context.Context, key string) error {
	return l.mutate(ctx, key, func(state map[string]loginAttemptRecord, now time.Time) (bool, error) {
		keyHash := loginAttemptKeyHash(key)
		record, ok := state[keyHash]
		if !ok {
			return false, nil
		}
		record.LastSeen = now.UnixMilli()
		if record.Count >= l.maxAttempts {
			record.BlockedTill = now.Add(l.blockFor).UnixMilli()
		}
		state[keyHash] = record
		return true, nil
	})
}

func (l *KubernetesLoginAttempts) Reset(ctx context.Context, key string) error {
	return l.mutate(ctx, key, func(state map[string]loginAttemptRecord, _ time.Time) (bool, error) {
		keyHash := loginAttemptKeyHash(key)
		if _, ok := state[keyHash]; !ok {
			return false, nil
		}
		delete(state, keyHash)
		return true, nil
	})
}

func (l *KubernetesLoginAttempts) mutate(ctx context.Context, key string, apply func(map[string]loginAttemptRecord, time.Time) (bool, error)) error {
	if l.client == nil || l.namespace == "" {
		return fmt.Errorf("%w: login rate limiter is not configured", domain.ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	name := loginAttemptLeaseName(key)
	for range l.retries {
		lease, err := l.client.CoordinationV1().Leases(l.namespace).Get(ctx, name, metav1.GetOptions{})
		exists := true
		if apierrors.IsNotFound(err) {
			exists = false
			lease = &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: l.namespace},
				Spec:       coordinationv1.LeaseSpec{HolderIdentity: stringPointer("dosh-login-rate-limiter")},
			}
		} else if err != nil {
			return loginAttemptKubeError(err)
		}
		state, err := decodeLoginAttempts(lease)
		if err != nil {
			return err
		}
		changed, err := apply(state, l.now())
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("%w: encode login rate limiter state", domain.ErrUnavailable)
		}
		if lease.Annotations == nil {
			lease.Annotations = map[string]string{}
		}
		lease.Annotations[loginAttemptAnnotation] = string(encoded)
		if !exists {
			_, err = l.client.CoordinationV1().Leases(l.namespace).Create(ctx, lease, metav1.CreateOptions{})
		} else {
			_, err = l.client.CoordinationV1().Leases(l.namespace).Update(ctx, lease, metav1.UpdateOptions{})
		}
		if err == nil {
			return nil
		}
		if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
			continue
		}
		return loginAttemptKubeError(err)
	}
	return fmt.Errorf("%w: login rate limiter update conflict", domain.ErrUnavailable)
}

func decodeLoginAttempts(lease *coordinationv1.Lease) (map[string]loginAttemptRecord, error) {
	state := make(map[string]loginAttemptRecord)
	raw := lease.Annotations[loginAttemptAnnotation]
	if raw == "" {
		return state, nil
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("%w: login rate limiter state is invalid", domain.ErrUnavailable)
	}
	if state == nil {
		state = make(map[string]loginAttemptRecord)
	}
	return state, nil
}

func cleanupLoginAttempts(state map[string]loginAttemptRecord, now time.Time, window time.Duration) {
	for key, record := range state {
		if record.BlockedTill > 0 && now.UnixMilli() < record.BlockedTill {
			continue
		}
		if now.Sub(time.UnixMilli(record.LastSeen)) > window {
			delete(state, key)
		}
	}
}

func reserveLoginAttemptCapacity(state map[string]loginAttemptRecord, now time.Time, window time.Duration) bool {
	if len(state) < loginAttemptBucketEntries {
		return true
	}
	cleanupLoginAttempts(state, now, window)
	if len(state) < loginAttemptBucketEntries {
		return true
	}
	oldestKey := ""
	var oldest int64
	for key, record := range state {
		if record.BlockedTill > 0 && now.UnixMilli() < record.BlockedTill {
			continue
		}
		if oldestKey == "" || record.LastSeen < oldest {
			oldestKey, oldest = key, record.LastSeen
		}
	}
	if oldestKey == "" {
		return false
	}
	delete(state, oldestKey)
	return true
}

func loginAttemptKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func loginAttemptLeaseName(key string) string {
	hash := loginAttemptKeyHash(key)
	return "dosh-login-rate-" + hash[:loginAttemptBucketHexChars]
}

func loginAttemptKubeError(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: login rate limiter unavailable", domain.ErrUnavailable)
}

func stringPointer(value string) *string { return &value }
