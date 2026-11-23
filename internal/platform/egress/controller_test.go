package egress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"dosh/internal/domain"
)

type memoryStore struct {
	settings StoredSettings
	found    bool
	revision int
	conflict bool
}

type policyManagerStub struct {
	status  NetworkPolicyStatus
	allow   bool
	checked int
	ensured [][]ProxyTarget
	cleaned int
}

func (p *policyManagerStub) Status() NetworkPolicyStatus { return p.status }
func (p *policyManagerStub) CheckTargets(_ context.Context, targets []ProxyTarget) ([]ProxyTarget, error) {
	p.checked++
	result := append([]ProxyTarget(nil), targets...)
	for index := range result {
		result[index].Allowed = p.allow
		result[index].PolicySource = "test"
	}
	return result, nil
}
func (p *policyManagerStub) Ensure(_ context.Context, targets []ProxyTarget) error {
	p.ensured = append(p.ensured, append([]ProxyTarget(nil), targets...))
	return nil
}
func (p *policyManagerStub) Cleanup(context.Context) error {
	p.cleaned++
	return nil
}

func (m *memoryStore) LoadEgressSettings(context.Context) (StoredSettings, bool, error) {
	result := m.settings
	result.Revision = m.revisionString()
	return result, m.found, nil
}

func (m *memoryStore) SaveEgressSettings(_ context.Context, settings StoredSettings, expectedRevision string) (StoredSettings, error) {
	if m.conflict {
		return StoredSettings{}, domain.ErrConflict
	}
	if expectedRevision != m.revisionString() {
		if !(expectedRevision == "" && !m.found) {
			return StoredSettings{}, domain.ErrConflict
		}
	}
	m.revision++
	m.found = true
	m.settings = settings
	m.settings.UpdatedAt = time.Now().UTC()
	m.settings.Revision = m.revisionString()
	return m.settings, nil
}

func (m *memoryStore) revisionString() string {
	if !m.found {
		return ""
	}
	return string(rune('0' + m.revision))
}

func TestControllerSaveApplyAndPreserveCredentials(t *testing.T) {
	factory := NewDirect()
	store := &memoryStore{}
	controller := NewController(factory, store, nil)

	view, err := controller.Save(context.Background(), UpdateInput{
		Mode: "explicit", HTTPSProxy: "https://proxy.example.test:8443", Username: "dosh", Password: "super-secret",
		NoProxy: ".svc,.cluster.local", CAPEM: string(testCertificatePEM(t)),
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !view.Enabled || view.Mode != "explicit" || view.Source != SourceRuntime || !view.CredentialsConfigured || !view.CAConfigured {
		t.Fatalf("View() = %#v, want enabled runtime explicit proxy", view)
	}
	if status := factory.Status(); status.HTTPSProxy != "https://proxy.example.test:8443" || !status.CredentialsConfigured {
		t.Fatalf("factory status = %#v", status)
	}

	view, err = controller.Save(context.Background(), UpdateInput{
		Mode: "explicit", HTTPSProxy: "https://proxy.example.test:8443", Username: "dosh", Revision: view.Revision,
	})
	if err != nil {
		t.Fatalf("Save(preserve password) error = %v", err)
	}
	current := factory.Current()
	if current.Password != "super-secret" || len(current.CAPEM) == 0 {
		t.Fatalf("Current() = %#v, want preserved credentials and CA", current)
	}
	if bytes, _ := json.Marshal(view); strings.Contains(string(bytes), "super-secret") {
		t.Fatalf("View leaked password: %s", bytes)
	}
}

func TestControllerLoadRuntimeAndBootstrapTombstone(t *testing.T) {
	factory := NewDirect()
	store := &memoryStore{
		found: true,
		settings: StoredSettings{
			Mode: "explicit", HTTPProxy: "http://proxy.example.test:3128", NoProxy: "internal.example.test",
		},
	}
	controller := NewController(factory, store, nil)
	store.revision = 5
	if err := controller.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if status := factory.Status(); !status.Enabled || status.Mode != "explicit" || status.Source != SourceRuntime {
		t.Fatalf("factory status = %#v, want runtime explicit", status)
	}

	store.settings = StoredSettings{Mode: "bootstrap"}
	store.revision = 6
	if err := controller.Load(context.Background()); err != nil {
		t.Fatalf("Load(bootstrap) error = %v", err)
	}
	if status := factory.Status(); status.Enabled || status.Source != SourceBootstrap {
		t.Fatalf("factory status after bootstrap = %#v", status)
	}
}

func TestControllerReportsConflictsAndValidationErrors(t *testing.T) {
	controller := NewController(NewDirect(), &memoryStore{conflict: true}, nil)
	if _, err := controller.Save(context.Background(), UpdateInput{Mode: "direct"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Save() conflict error = %v", err)
	}
	if _, err := controller.Save(context.Background(), UpdateInput{Mode: "explicit"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Save() empty proxy error = %v", err)
	}
	if _, err := controller.Save(context.Background(), UpdateInput{Mode: "explicit", HTTPSProxy: "https://proxy.example.test", Username: "dosh"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Save() incomplete credentials error = %v", err)
	}
}

func TestControllerViewDoesNotContainSecretMaterial(t *testing.T) {
	factory, err := NewFromSettings(Settings{Mode: "explicit", HTTPSProxy: "https://proxy.example.test", Username: "dosh", Password: "super-secret", CAPEM: testCertificatePEM(t)})
	if err != nil {
		t.Fatalf("NewFromSettings() error = %v", err)
	}
	view := NewController(factory, &memoryStore{}, nil).View()
	if reflect.ValueOf(view).FieldByName("Username").String() != "dosh" {
		t.Fatalf("View() = %#v", view)
	}
	encoded, _ := json.Marshal(view)
	for _, forbidden := range []string{"super-secret", "BEGIN CERTIFICATE"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("View leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestControllerPreflightRuntimePolicyAndConnectivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	manager := &policyManagerStub{status: NetworkPolicyStatus{
		Enabled: true, Mode: NetworkPolicyModeRuntime, AutoManage: true, CanAutoManage: true, PolicyName: "dosh-egress-runtime",
	}, allow: true}
	controller := NewController(NewDirect(), &memoryStore{}, nil)
	controller.SetNetworkPolicyManager(manager)

	result, err := controller.Preflight(context.Background(), UpdateInput{Mode: "explicit", HTTPProxy: server.URL})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.Status != "ok" || result.NetworkPolicy.Mode != NetworkPolicyModeRuntime {
		t.Fatalf("Preflight() = %#v", result)
	}
	if len(result.Targets) != 1 || !result.Targets[0].Reachable || !result.Targets[0].Allowed {
		t.Fatalf("Preflight targets = %#v", result.Targets)
	}
	if len(result.RequiredIPBlocks) != 1 || !strings.HasSuffix(result.RequiredIPBlocks[0], "/32") {
		t.Fatalf("required blocks = %#v", result.RequiredIPBlocks)
	}
	if manager.checked != 1 {
		t.Fatalf("policy checks = %d, want 1", manager.checked)
	}
}

func TestControllerSaveRejectsHelmPolicyThatDoesNotAllowProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	store := &memoryStore{}
	manager := &policyManagerStub{status: NetworkPolicyStatus{Enabled: true, Mode: NetworkPolicyModeHelm}, allow: false}
	controller := NewController(NewDirect(), store, nil)
	controller.SetNetworkPolicyManager(manager)

	_, err := controller.Save(context.Background(), UpdateInput{Mode: "explicit", HTTPProxy: server.URL})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Save() error = %v, want invalid", err)
	}
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Field != "networkPolicy" || !strings.Contains(validation.Message, "requiredIpBlocks") {
		t.Fatalf("Save() validation error = %#v, want actionable NetworkPolicy field error", err)
	}
	if store.found {
		t.Fatal("Save() persisted settings despite a blocked NetworkPolicy preflight")
	}
	if len(manager.ensured) != 0 {
		t.Fatalf("Ensure() calls = %d, want 0", len(manager.ensured))
	}
}

func TestControllerSaveEnsuresRuntimePolicyBeforePersisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	store := &memoryStore{}
	manager := &policyManagerStub{status: NetworkPolicyStatus{Enabled: true, Mode: NetworkPolicyModeRuntime}, allow: true}
	controller := NewController(NewDirect(), store, nil)
	controller.SetNetworkPolicyManager(manager)

	view, err := controller.Save(context.Background(), UpdateInput{Mode: "explicit", HTTPProxy: server.URL})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(manager.ensured) != 1 || len(manager.ensured[0]) != 1 {
		t.Fatalf("Ensure() calls = %#v", manager.ensured)
	}
	if view.NetworkPolicy.Mode != NetworkPolicyModeRuntime || !view.Enabled {
		t.Fatalf("View() = %#v", view)
	}
	if !store.found || store.settings.Mode != "explicit" {
		t.Fatalf("stored settings = %#v", store.settings)
	}
}
