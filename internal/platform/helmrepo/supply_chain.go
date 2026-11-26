package helmrepo

import (
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
	"dosh/internal/platform/egress"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	cosign "github.com/sigstore/cosign/v2/pkg/cosign"
	cosignoci "github.com/sigstore/cosign/v2/pkg/oci"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

var trustedRootCache struct {
	sync.Mutex
	material root.TrustedMaterial
	fetched  time.Time
}

const trustedRootTTL = time.Hour

// VerifySupplyChain validates an OCI Chart reference with cosign. Public-key
// verification is deterministic and requires no network trust root. Keyless
// verification uses Sigstore's TUF trusted root, Fulcio certificates and Rekor.
// Both modes enforce the signed OCI manifest digest and, when configured, an
// in-toto attestation predicate before returning.
func (c *Client) VerifySupplyChain(ctx context.Context, repository domain.HelmRepository, chartName, version string) (domain.HelmSupplyChainStatus, error) {
	policy := repository.SupplyChain
	if !isOCI(repository.URL) || policy == nil || strings.EqualFold(policy.Mode, "") || strings.EqualFold(policy.Mode, domain.HelmSupplyChainDisabled) {
		return domain.HelmSupplyChainStatus{Policy: domain.HelmSupplyChainDisabled}, nil
	}
	target, err := parseOCITarget(repository.URL)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, err
	}
	if target.ChartName != chartName {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: OCI Chart %s", domain.ErrNotFound, chartName)
	}
	version = strings.TrimSpace(version)
	if version == "" || strings.ContainsAny(version, "/ \t\r\n") {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: a valid OCI Chart version is required", domain.ErrInvalid)
	}
	reference := target.Repository + ":" + version
	ref, err := name.ParseReference(reference, ociNameOptions(repository)...)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: parse OCI Chart reference: %v", domain.ErrInvalid, err)
	}
	tlsConfig, err := tlsConfigForRepository(repository)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, err
	}
	transport := c.egress.Transport(egress.ScopeHelm, tlsConfig)
	remoteOpts := []remote.Option{
		remote.WithContext(ctx), remote.WithAuth(ociAuthenticator(repository)), remote.WithTransport(transport), remote.WithUserAgent("dosh/0.1"),
	}
	desc, err := remote.Head(ref, remoteOpts...)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: read OCI Chart manifest digest: %v", domain.ErrUnavailable, sanitizeError(err))
	}
	// Pin every subsequent read to the manifest digest returned by the tag so a
	// concurrent tag push cannot make the signature check and layer extraction
	// observe different manifests.
	pinned := ref.Context().Digest(desc.Digest.String())
	image, err := remote.Image(pinned, remoteOpts...)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: read pinned OCI Chart manifest: %v", domain.ErrUnavailable, sanitizeError(err))
	}
	chartDigest, err := chartLayerDigest(image)
	if err != nil {
		return domain.HelmSupplyChainStatus{}, fmt.Errorf("%w: resolve OCI Chart package layer: %v", domain.ErrUnavailable, sanitizeError(err))
	}
	status := domain.HelmSupplyChainStatus{
		Policy: strings.ToLower(strings.TrimSpace(policy.Mode)), Reference: reference, ManifestDigest: desc.Digest.String(),
		ChartDigest: chartDigest, VerifiedAt: time.Now().UTC(),
	}
	checkOpts := &cosign.CheckOpts{
		RegistryClientOpts: []ociremote.Option{ociremote.WithRemoteOptions(remoteOpts...)},
		ClaimVerifier:      cosign.SimpleClaimVerifier,
	}
	switch status.Policy {
	case domain.HelmSupplyChainKey:
		verified, verifyErr := verifyWithPublicKeys(ctx, pinned, policy.PublicKeys, checkOpts)
		if verifyErr != nil {
			return status, supplyChainError("OCI Chart 签名校验失败", verifyErr)
		}
		status.SignatureVerified = true
		status.Signer = "public-key"
		status.KeyFingerprint = verified
	case domain.HelmSupplyChainKeyless:
		material, trustErr := c.trustedRoot(ctx)
		if trustErr != nil {
			return status, supplyChainError("无法获取 Sigstore 可信根", trustErr)
		}
		checkOpts.TrustedMaterial = material
		checkOpts.Identities = make([]cosign.Identity, 0, len(policy.Identities))
		for _, identity := range policy.Identities {
			checkOpts.Identities = append(checkOpts.Identities, cosign.Identity{
				Issuer: identity.Issuer, Subject: identity.Subject,
				IssuerRegExp: identity.IssuerRegExp, SubjectRegExp: identity.SubjectRegExp,
			})
		}
		signatures, _, verifyErr := cosign.VerifyImageSignatures(ctx, pinned, checkOpts)
		if verifyErr != nil || len(signatures) == 0 {
			return status, supplyChainError("OCI Chart Keyless 签名校验失败", verifyErr)
		}
		status.SignatureVerified = true
		status.Signer = "keyless"
		if certificate, certErr := signatures[0].Cert(); certErr == nil && certificate != nil {
			status.Issuer = certificate.Issuer.String()
			status.Subject = certificate.Subject.String()
		}
	default:
		return status, fmt.Errorf("%w: unsupported supply-chain policy %q", domain.ErrInvalid, policy.Mode)
	}
	if policy.RequireAttestation {
		attestationOpts := *checkOpts
		attestationOpts.ClaimVerifier = cosign.IntotoSubjectClaimVerifier
		var attestationSigs []interface{ Payload() ([]byte, error) }
		var verifyErr error
		if status.Policy == domain.HelmSupplyChainKey {
			attestationSigs, verifyErr = verifyAttestationsWithPublicKeys(ctx, pinned, policy.PublicKeys, &attestationOpts)
		} else {
			var values []cosignoci.Signature
			values, _, verifyErr = cosign.VerifyImageAttestations(ctx, pinned, &attestationOpts)
			for _, item := range values {
				attestationSigs = append(attestationSigs, item)
			}
		}
		if verifyErr != nil || len(attestationSigs) == 0 {
			return status, supplyChainError("OCI Chart 证明校验失败", verifyErr)
		}
		if !containsPredicateType(attestationSigs, policy.AttestationPredicateType) {
			return status, supplyChainError("OCI Chart 未包含预期的 in-toto 证明", fmt.Errorf("predicate type %s not found", policy.AttestationPredicateType))
		}
		status.AttestationVerified = true
	}
	status.Verified = true
	status.Message = "OCI Chart 供应链校验通过"
	return status, nil
}

func ociNameOptions(repository domain.HelmRepository) []name.Option {
	options := make([]name.Option, 0, 1)
	if repository.InsecureSkipTLS {
		options = append(options, name.Insecure)
	}
	return options
}

func ociAuthenticator(repository domain.HelmRepository) authn.Authenticator {
	switch repository.AuthType {
	case domain.HelmRepositoryAuthBasic:
		return authn.FromConfig(authn.AuthConfig{Username: repository.Credentials.Username, Password: repository.Credentials.Password})
	case domain.HelmRepositoryAuthBearer:
		return authn.FromConfig(authn.AuthConfig{RegistryToken: repository.Credentials.Token})
	default:
		return authn.Anonymous
	}
}

func (c *Client) trustedRoot(ctx context.Context) (root.TrustedMaterial, error) {
	trustedRootCache.Lock()
	defer trustedRootCache.Unlock()
	if trustedRootCache.material != nil && time.Since(trustedRootCache.fetched) < trustedRootTTL {
		return trustedRootCache.material, nil
	}
	// Sigstore TUF fetches Fulcio/Rekor metadata over the network. Route it
	// through the same administrator-managed Helm egress policy as OCI access
	// instead of cosign's implicit default HTTP client.
	fetch := fetcher.NewDefaultFetcher()
	fetch.SetHTTPUserAgent("dosh/0.1")
	fetch.SetHTTPClient(&http.Client{Transport: c.egress.Transport(egress.ScopeHelm, nil)})
	opts := tuf.DefaultOptions().WithContext(ctx).WithFetcher(fetch)
	trusted, err := root.FetchTrustedRootWithOptions(opts)
	if err != nil {
		return nil, err
	}
	trustedRootCache.material = trusted
	trustedRootCache.fetched = time.Now()
	return trusted, nil
}

func chartLayerDigest(image v1.Image) (string, error) {
	layers, err := image.Layers()
	if err != nil {
		return "", err
	}
	for _, layer := range layers {
		mediaType, err := layer.MediaType()
		if err == nil && strings.Contains(strings.ToLower(string(mediaType)), "helm.chart.content") {
			digest, err := layer.Digest()
			if err != nil {
				return "", err
			}
			return digest.String(), nil
		}
	}
	if len(layers) == 1 {
		digest, err := layers[0].Digest()
		if err != nil {
			return "", err
		}
		return digest.String(), nil
	}
	return "", fmt.Errorf("manifest does not contain a uniquely identifiable Helm chart layer")
}

func verifyWithPublicKeys(ctx context.Context, ref name.Reference, publicKeys []string, base *cosign.CheckOpts) (string, error) {
	var lastErr error
	for _, encoded := range publicKeys {
		key, err := cryptoutils.UnmarshalPEMToPublicKey([]byte(encoded))
		if err != nil {
			lastErr = err
			continue
		}
		verifier, err := signature.LoadVerifier(key, crypto.SHA256)
		if err != nil {
			lastErr = err
			continue
		}
		opts := *base
		opts.SigVerifier = verifier
		signatures, _, verifyErr := cosign.VerifyImageSignatures(ctx, ref, &opts)
		if verifyErr == nil && len(signatures) > 0 {
			return publicKeyFingerprint(key), nil
		}
		lastErr = verifyErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no trusted public key was configured")
	}
	return "", lastErr
}

func verifyAttestationsWithPublicKeys(ctx context.Context, ref name.Reference, publicKeys []string, base *cosign.CheckOpts) ([]interface{ Payload() ([]byte, error) }, error) {
	var lastErr error
	for _, encoded := range publicKeys {
		key, err := cryptoutils.UnmarshalPEMToPublicKey([]byte(encoded))
		if err != nil {
			lastErr = err
			continue
		}
		verifier, err := signature.LoadVerifier(key, crypto.SHA256)
		if err != nil {
			lastErr = err
			continue
		}
		opts := *base
		opts.SigVerifier = verifier
		signatures, _, verifyErr := cosign.VerifyImageAttestations(ctx, ref, &opts)
		if verifyErr == nil && len(signatures) > 0 {
			result := make([]interface{ Payload() ([]byte, error) }, 0, len(signatures))
			for _, item := range signatures {
				result = append(result, item)
			}
			return result, nil
		}
		lastErr = verifyErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no trusted public key was configured")
	}
	return nil, lastErr
}

func publicKeyFingerprint(key any) string {
	encoded, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func containsPredicateType(signatures []interface{ Payload() ([]byte, error) }, expected string) bool {
	for _, item := range signatures {
		payload, err := item.Payload()
		if err != nil {
			continue
		}
		var envelope struct {
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Payload == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(envelope.Payload)
		if err != nil {
			continue
		}
		var statement struct {
			PredicateType string `json:"predicateType"`
		}
		if err := json.Unmarshal(decoded, &statement); err == nil && (expected == "" || statement.PredicateType == expected) {
			return true
		}
	}
	return false
}

func supplyChainError(prefix string, err error) error {
	if err == nil {
		err = fmt.Errorf("verification did not return a trusted result")
	}
	return domain.WithPublicMessage(fmt.Errorf("%w: %s: %v", domain.ErrForbidden, prefix, sanitizeError(err)), prefix+"："+sanitizeError(err))
}
