package helmrepo

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"dosh/internal/domain"

	"github.com/google/go-containerregistry/pkg/v1/random"
)

func supplyChainPublicKey(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), publicKeyFingerprint(&key.PublicKey)
}

func TestVerifySupplyChainDisabledForHTTPRepository(t *testing.T) {
	client := &Client{}
	status, err := client.VerifySupplyChain(context.Background(), domain.HelmRepository{URL: "https://charts.example.com"}, "demo", "1.0.0")
	if err != nil || status.Policy != domain.HelmSupplyChainDisabled {
		t.Fatalf("VerifySupplyChain() = %#v, %v", status, err)
	}
	status, err = client.VerifySupplyChain(context.Background(), domain.HelmRepository{
		URL: "oci://registry.example.com/team/demo", SupplyChain: &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainDisabled},
	}, "demo", "1.0.0")
	if err != nil || status.Policy != domain.HelmSupplyChainDisabled {
		t.Fatalf("VerifySupplyChain(disabled) = %#v, %v", status, err)
	}
}

func TestVerifySupplyChainRejectsInvalidRequestBeforeNetwork(t *testing.T) {
	client := &Client{}
	repository := domain.HelmRepository{URL: "oci://registry.example.com/team/demo", SupplyChain: &domain.HelmSupplyChainPolicy{Mode: domain.HelmSupplyChainKey}}
	if _, err := client.VerifySupplyChain(context.Background(), repository, "other", "1.0.0"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("wrong chart error = %v, want not found", err)
	}
	if _, err := client.VerifySupplyChain(context.Background(), repository, "demo", "1.0.0/evil"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid version error = %v, want invalid", err)
	}
}

func TestPublicKeyFingerprintIsStableAndKeySpecific(t *testing.T) {
	firstPEM, firstFingerprint := supplyChainPublicKey(t)
	secondPEM, secondFingerprint := supplyChainPublicKey(t)
	if !strings.HasPrefix(firstFingerprint, "sha256:") || firstFingerprint == secondFingerprint || firstPEM == secondPEM {
		t.Fatalf("fingerprints = %q / %q", firstFingerprint, secondFingerprint)
	}
	key, err := cryptoutilsUnmarshal(firstPEM)
	if err != nil {
		t.Fatalf("unmarshal key: %v", err)
	}
	if got := publicKeyFingerprint(key); got != firstFingerprint {
		t.Fatalf("fingerprint = %q, want %q", got, firstFingerprint)
	}
}

func cryptoutilsUnmarshal(value string) (any, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("not PEM")
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}

func TestContainsPredicateType(t *testing.T) {
	payload := func(predicateType string) []byte {
		statement := map[string]any{"_type": "https://in-toto.io/Statement/v1", "predicateType": predicateType, "subject": []any{}}
		encoded, _ := json.Marshal(statement)
		envelope, _ := json.Marshal(map[string]any{"payload": base64.StdEncoding.EncodeToString(encoded)})
		return envelope
	}
	signatures := []interface{ Payload() ([]byte, error) }{
		payloadSignature{payload: payload("https://slsa.dev/provenance/v1")},
		payloadSignature{payload: []byte("not-json")},
	}
	if !containsPredicateType(signatures, "https://slsa.dev/provenance/v1") {
		t.Fatal("expected predicate type was not found")
	}
	if containsPredicateType(signatures, "https://example.com/other") {
		t.Fatal("unexpected predicate type matched")
	}
	if !containsPredicateType(signatures, "") {
		t.Fatal("empty predicate type should accept any valid in-toto statement")
	}
}

func TestChartLayerDigestBindsSingleHelmLayer(t *testing.T) {
	image, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image() error = %v", err)
	}
	layers, err := image.Layers()
	if err != nil || len(layers) != 1 {
		t.Fatalf("Layers() = %d, %v", len(layers), err)
	}
	want, err := layers[0].Digest()
	if err != nil {
		t.Fatalf("layer digest error = %v", err)
	}
	got, err := chartLayerDigest(image)
	if err != nil || got != want.String() {
		t.Fatalf("chartLayerDigest() = %q, %v; want %q", got, err, want.String())
	}
	multiLayer, err := random.Image(1024, 2)
	if err != nil {
		t.Fatalf("random.Image(multi) error = %v", err)
	}
	if _, err := chartLayerDigest(multiLayer); err == nil {
		t.Fatal("chartLayerDigest() accepted an ambiguous multi-layer artifact")
	}
}

type payloadSignature struct{ payload []byte }

func (s payloadSignature) Payload() ([]byte, error) { return s.payload, nil }

func TestSupplyChainErrorMapsToForbiddenWithSafeMessage(t *testing.T) {
	err := supplyChainError("签名失败", errors.New("upstream returned SECRET=value"))
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
	if !strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "签名失败") {
		t.Fatalf("error text = %q", err.Error())
	}
}
