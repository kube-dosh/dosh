package app

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dosh/internal/domain"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func normalizeSupplyChainPolicy(isOCI bool, current, input *domain.HelmSupplyChainPolicy) (*domain.HelmSupplyChainPolicy, error) {
	if !isOCI {
		if input != nil && strings.TrimSpace(input.Mode) != "" && strings.TrimSpace(input.Mode) != domain.HelmSupplyChainDisabled {
			return nil, fmt.Errorf("%w: OCI supply-chain verification is only available for OCI Chart repositories", domain.ErrInvalid)
		}
		return nil, nil
	}
	if input == nil {
		return cloneSupplyChainPolicy(current), nil
	}
	policy := cloneSupplyChainPolicy(input)
	policy.Mode = strings.ToLower(strings.TrimSpace(policy.Mode))
	if policy.Mode == "" {
		policy.Mode = domain.HelmSupplyChainDisabled
	}
	switch policy.Mode {
	case domain.HelmSupplyChainDisabled:
		return nil, nil
	case domain.HelmSupplyChainKey:
		if len(policy.PublicKeys) == 0 {
			return nil, fmt.Errorf("%w: public-key supply-chain policy requires at least one public key", domain.ErrInvalid)
		}
		if len(policy.PublicKeys) > 16 {
			return nil, fmt.Errorf("%w: at most 16 public keys are supported", domain.ErrInvalid)
		}
		for index, key := range policy.PublicKeys {
			key = strings.TrimSpace(key)
			if len(key) == 0 || len(key) > 32<<10 {
				return nil, fmt.Errorf("%w: public key %d is empty or too large", domain.ErrInvalid, index+1)
			}
			block, _ := pem.Decode([]byte(key))
			if block == nil {
				return nil, fmt.Errorf("%w: public key %d is not PEM encoded", domain.ErrInvalid, index+1)
			}
			if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
				if _, rsaErr := x509.ParsePKCS1PublicKey(block.Bytes); rsaErr != nil {
					return nil, fmt.Errorf("%w: public key %d is not a supported public key: %v", domain.ErrInvalid, index+1, err)
				}
			}
			policy.PublicKeys[index] = key
		}
		if len(policy.Identities) > 0 {
			return nil, fmt.Errorf("%w: keyless identities are only valid with keyless verification", domain.ErrInvalid)
		}
	case domain.HelmSupplyChainKeyless:
		if len(policy.PublicKeys) > 0 {
			return nil, fmt.Errorf("%w: public keys are only valid with key verification", domain.ErrInvalid)
		}
		if len(policy.Identities) == 0 {
			return nil, fmt.Errorf("%w: keyless supply-chain policy requires at least one issuer/subject identity", domain.ErrInvalid)
		}
		if len(policy.Identities) > 32 {
			return nil, fmt.Errorf("%w: at most 32 keyless identities are supported", domain.ErrInvalid)
		}
		for index := range policy.Identities {
			identity := &policy.Identities[index]
			identity.Issuer = strings.TrimSpace(identity.Issuer)
			identity.Subject = strings.TrimSpace(identity.Subject)
			identity.IssuerRegExp = strings.TrimSpace(identity.IssuerRegExp)
			identity.SubjectRegExp = strings.TrimSpace(identity.SubjectRegExp)
			if identity.Issuer == "" && identity.Subject == "" && identity.IssuerRegExp == "" && identity.SubjectRegExp == "" {
				return nil, fmt.Errorf("%w: keyless identity %d is empty", domain.ErrInvalid, index+1)
			}
			if len(identity.Issuer)+len(identity.Subject)+len(identity.IssuerRegExp)+len(identity.SubjectRegExp) > 8<<10 {
				return nil, fmt.Errorf("%w: keyless identity %d is too large", domain.ErrInvalid, index+1)
			}
			if identity.IssuerRegExp != "" {
				if _, err := regexp.Compile(identity.IssuerRegExp); err != nil {
					return nil, fmt.Errorf("%w: issuer regexp %d is invalid: %v", domain.ErrInvalid, index+1, err)
				}
			}
			if identity.SubjectRegExp != "" {
				if _, err := regexp.Compile(identity.SubjectRegExp); err != nil {
					return nil, fmt.Errorf("%w: subject regexp %d is invalid: %v", domain.ErrInvalid, index+1, err)
				}
			}
		}
	default:
		return nil, fmt.Errorf("%w: supply-chain mode must be disabled, key or keyless", domain.ErrInvalid)
	}
	policy.AttestationPredicateType = strings.TrimSpace(policy.AttestationPredicateType)
	if len(policy.AttestationPredicateType) > 512 {
		return nil, fmt.Errorf("%w: attestation predicate type is too long", domain.ErrInvalid)
	}
	if policy.RequireAttestation && policy.AttestationPredicateType == "" {
		return nil, fmt.Errorf("%w: requiring an attestation also requires a predicate type", domain.ErrInvalid)
	}
	return policy, nil
}

func cloneSupplyChainPolicy(policy *domain.HelmSupplyChainPolicy) *domain.HelmSupplyChainPolicy {
	if policy == nil {
		return nil
	}
	copy := *policy
	copy.PublicKeys = append([]string(nil), policy.PublicKeys...)
	copy.Identities = append([]domain.HelmSupplyChainIdentity(nil), policy.Identities...)
	return &copy
}

// verifyChartDigestBinding prevents a registry tag running concurrently with
// verification from substituting an unsigned package after the signed manifest
// has been checked. The Helm OCI layer is the exact archive consumed by the
// planner and executor, so its digest must equal the downloaded package digest.
func verifyChartDigestBinding(status domain.HelmSupplyChainStatus, chartDigest string) error {
	if status.Policy == "" || status.Policy == domain.HelmSupplyChainDisabled {
		return nil
	}
	if !status.Verified || strings.TrimSpace(status.ChartDigest) == "" {
		return fmt.Errorf("%w: OCI Chart 供应链结果未绑定到已验签的 Chart 包摘要", domain.ErrForbidden)
	}
	if normalizeDigest(status.ChartDigest) != normalizeDigest(chartDigest) {
		return fmt.Errorf("%w: OCI Chart 包摘要 %s 与已验签清单中的 %s 不一致", domain.ErrForbidden, chartDigest, status.ChartDigest)
	}
	return nil
}

func normalizeDigest(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !idempotencyKeyPattern.MatchString(value) {
		return "", fmt.Errorf("%w: idempotencyKey must be 1-128 characters using letters, numbers, dot, underscore, colon or dash", domain.ErrInvalid)
	}
	return value, nil
}

func operationRequestDigest(action string, payload any) (string, error) {
	encoded, err := json.Marshal(struct {
		Action  string `json:"action"`
		Payload any    `json:"payload"`
	}{Action: action, Payload: payload})
	if err != nil {
		return "", fmt.Errorf("%w: encode Helm idempotency request: %v", domain.ErrInvalid, err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func derivedIdempotencyKey(action string, digest string) string {
	return "auto:" + strings.TrimPrefix(action, "-") + ":" + strings.TrimPrefix(digest, "sha256:")
}

// findIdempotentOperation returns an earlier operation with the same client
// idempotency key. The adapter indexes explicit keys by exact operation type,
// namespace, Release and key instead of relying on a bounded history scan.
// Explicit keys are retained for 30 days; generated keys only suppress a retry
// while the earlier operation is still active or already succeeded.
func (s *Service) findIdempotentOperation(ctx context.Context, action, namespace, releaseName, key, requestDigest string) (*domain.HelmChartOperation, error) {
	if key == "" {
		return nil, nil
	}
	lister, ok := s.repository.(OperationLister)
	if !ok {
		return nil, nil
	}
	items, err := lister.ListHelmChartOperations(ctx, domain.HelmOperationFilter{
		Namespace: namespace, ReleaseName: releaseName, IdempotencyKey: key, Limit: 20,
	})
	if err != nil {
		return nil, err
	}
	explicit := !strings.HasPrefix(key, "auto:")
	for index := range items {
		operation := items[index]
		if operation.IdempotencyKey != key {
			continue
		}
		if operation.RequestDigest != "" && requestDigest != "" && operation.RequestDigest != requestDigest {
			return nil, fmt.Errorf("%w: idempotency key %s was already used for a different Helm request", domain.ErrConflict, key)
		}
		if !explicit && (operation.Status == domain.HelmOperationFailed || operation.Status == domain.HelmOperationCancelled) && time.Since(operation.StartedAt) > 0 {
			continue
		}
		if operation.Type != "" && operation.Type != action {
			return nil, fmt.Errorf("%w: idempotency key %s belongs to a different Helm operation", domain.ErrConflict, key)
		}
		return &operation, nil
	}
	return nil, nil
}

func (s *Service) idempotentOperation(ctx context.Context, action, namespace, releaseName, key, requestDigest string) (*domain.HelmChartOperation, error) {
	existing, err := s.findIdempotentOperation(ctx, action, namespace, releaseName, key, requestDigest)
	if err != nil || existing == nil {
		return existing, err
	}
	if existing.Status == domain.HelmOperationFailed || existing.Status == domain.HelmOperationCancelled {
		if strings.HasPrefix(key, "auto:") {
			return nil, nil
		}
	}
	return existing, nil
}

func idempotencyInput(action string, key, requestDigest string, namespace, releaseName string) string {
	if key != "" {
		return key
	}
	return derivedIdempotencyKey(action, requestDigest) + ":" + namespace + "/" + releaseName
}

// beginIdempotentRequest serializes the lookup/lease/save critical section for
// one idempotency key inside this process. Cross-replica correctness still
// comes from the Kubernetes operation lease; this prevents two local requests
// from racing between the initial lookup and the first persisted operation.
func (s *Service) beginIdempotentRequest(key string) func() {
	if key == "" {
		return func() {}
	}
	s.idempotencyMu.Lock()
	lock := s.idempotencyLocks[key]
	if lock == nil {
		lock = &idempotencyMutex{}
		s.idempotencyLocks[key] = lock
	}
	lock.refs++
	s.idempotencyMu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		s.idempotencyMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(s.idempotencyLocks, key)
		}
		s.idempotencyMu.Unlock()
	}
}

// waitForIdempotentOperation closes the cross-replica race where another
// replica has acquired the Release lease but has not yet persisted its first
// operation record. Callers only use this after a lease conflict.
func (s *Service) waitForIdempotentOperation(ctx context.Context, action, namespace, releaseName, key, requestDigest string) (*domain.HelmChartOperation, error) {
	if key == "" {
		return nil, nil
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		existing, err := s.idempotentOperation(ctx, action, namespace, releaseName, key, requestDigest)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case <-ticker.C:
		}
	}
}
