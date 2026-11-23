package systemhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dosh/internal/domain"
	systemapp "dosh/internal/modules/system/app"
	"dosh/internal/platform/egress"
)

type checkerStub struct {
	calls atomic.Int32
	force atomic.Bool
}

func (c *checkerStub) Status(_ context.Context, force bool) domain.UpdateStatus {
	c.calls.Add(1)
	c.force.Store(force)
	return domain.UpdateStatus{Enabled: true, Status: "current", CurrentVersion: "1.0.0", CheckedAt: "2026-11-22T00:00:00Z"}
}

func TestInfoSerializesOnlySafeRuntimeMetadata(t *testing.T) {
	service := systemapp.New(domain.SystemInfo{
		Version: "1.2.3", ChartVersion: "0.1.9", ClusterName: "production",
		AuthMode: "local", ReadOnly: true, RequestTimeout: "15s",
		Egress: domain.EgressStatus{
			Mode: "explicit", Enabled: true, HTTPSProxy: "https://proxy.example.test:8443",
			NoProxy: "internal.example.test", CredentialsConfigured: true, CAConfigured: true,
		},
	}, &checkerStub{}, nil, nil)
	handler := New(service)
	recorder := httptest.NewRecorder()
	handler.Info(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("Info status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"password", "token", "super-secret", "proxy-user"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("Info response leaked %q: %s", forbidden, body)
		}
	}
	var info domain.SystemInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if info.Version != "1.2.3" || info.Egress.Mode != "explicit" || !info.Egress.Enabled {
		t.Fatalf("Info = %#v, want runtime metadata", info)
	}
}

func TestCheckUpdateRequiresAdministratorAndForcesRefresh(t *testing.T) {
	checker := &checkerStub{}
	handler := New(systemapp.New(domain.SystemInfo{}, checker, nil, nil))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/update/check", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "viewer"}))
	recorder := httptest.NewRecorder()
	handler.CheckUpdate(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("CheckUpdate non-admin status = %d, want 403", recorder.Code)
	}
	if checker.calls.Load() != 0 {
		t.Fatal("non-admin request reached the update checker")
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/system/update/check", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	recorder = httptest.NewRecorder()
	handler.CheckUpdate(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("CheckUpdate admin status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if checker.calls.Load() != 1 || !checker.force.Load() {
		t.Fatalf("checker calls=%d force=%v, want one forced check", checker.calls.Load(), checker.force.Load())
	}
}

func TestUpdateDisabledStateIsReturnedWithoutError(t *testing.T) {
	checker := releaseDisabledChecker{}
	handler := New(systemapp.New(domain.SystemInfo{}, checker, nil, nil))
	recorder := httptest.NewRecorder()
	handler.Update(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("Update status = %d, want 200", recorder.Code)
	}
	var status domain.UpdateStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if status.Enabled || status.Status != "disabled" {
		t.Fatalf("Update = %#v, want disabled", status)
	}
}

type releaseDisabledChecker struct{}

func (releaseDisabledChecker) Status(context.Context, bool) domain.UpdateStatus {
	return domain.UpdateStatus{Enabled: false, Status: "disabled", CurrentVersion: "1.0.0", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
}

type egressRuntimeStub struct {
	status    egress.Status
	view      egress.View
	preflight egress.Preflight
	saved     egress.UpdateInput
	err       error
}

func (e *egressRuntimeStub) Status() egress.Status { return e.status }
func (e *egressRuntimeStub) View() egress.View     { return e.view }
func (e *egressRuntimeStub) Save(_ context.Context, input egress.UpdateInput) (egress.View, error) {
	e.saved = input
	return e.view, e.err
}
func (e *egressRuntimeStub) Preflight(_ context.Context, input egress.UpdateInput) (egress.Preflight, error) {
	e.saved = input
	return e.preflight, e.err
}

func TestEgressSettingsRequireAdministratorAndDoNotExposeSecrets(t *testing.T) {
	runtime := &egressRuntimeStub{
		status: egress.Status{Mode: "explicit", Enabled: true, Source: egress.SourceRuntime},
		view: egress.View{
			Mode: "explicit", EffectiveMode: "explicit", Enabled: true, HTTPProxy: "http://proxy.example.test:3128",
			Username: "dosh", CredentialsConfigured: true, CAConfigured: true, Source: egress.SourceRuntime, Revision: "42",
		},
	}
	handler := New(systemapp.New(domain.SystemInfo{}, &checkerStub{}, runtime, nil))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/egress", nil)
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "viewer"}))
	handler.Egress(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Egress status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{"password", "super-secret", "caPEM"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("Egress response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}

	body := `{"mode":"explicit","httpProxy":"http://proxy.example.test:3128","username":"dosh","password":"super-secret","revision":"42"}`
	request = httptest.NewRequest(http.MethodPut, "/api/v1/system/egress", strings.NewReader(body))
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "viewer"}))
	recorder = httptest.NewRecorder()
	handler.SaveEgress(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("SaveEgress non-admin status = %d, want 403", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/system/egress", strings.NewReader(body))
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	recorder = httptest.NewRecorder()
	handler.SaveEgress(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("SaveEgress admin status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if runtime.saved.Mode != "explicit" || runtime.saved.HTTPProxy != "http://proxy.example.test:3128" || runtime.saved.Password != "super-secret" || runtime.saved.Revision != "42" {
		t.Fatalf("SaveEgress input = %#v", runtime.saved)
	}
}

func TestEgressPreflightRequiresAdministratorAndReturnsPolicyDetails(t *testing.T) {
	runtime := &egressRuntimeStub{preflight: egress.Preflight{
		Status: "warning", Message: "proxy DNS may change",
		NetworkPolicy:    egress.NetworkPolicyStatus{Enabled: true, Mode: egress.NetworkPolicyModeRuntime, AutoManage: true, CanAutoManage: true},
		RequiredIPBlocks: []string{"192.0.2.10/32"}, RequiredPorts: []int{1080},
		Targets: []egress.ProxyTarget{{Kind: "http", Host: "proxy.example.test", Port: 1080, Addresses: []string{"192.0.2.10"}, CIDRs: []string{"192.0.2.10/32"}, Allowed: true, Reachable: true}},
	}}
	handler := New(systemapp.New(domain.SystemInfo{}, &checkerStub{}, runtime, nil))
	body := `{"mode":"explicit","httpProxy":"http://proxy.example.test:1080"}`

	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/egress/preflight", strings.NewReader(body))
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "viewer"}))
	recorder := httptest.NewRecorder()
	handler.PreflightEgress(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("PreflightEgress non-admin status = %d, want 403", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/system/egress/preflight", strings.NewReader(body))
	request = request.WithContext(domain.WithActor(request.Context(), domain.Actor{Username: "admin", Admin: true}))
	recorder = httptest.NewRecorder()
	handler.PreflightEgress(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PreflightEgress admin status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if runtime.saved.HTTPProxy != "http://proxy.example.test:1080" {
		t.Fatalf("PreflightEgress input = %#v", runtime.saved)
	}
	response := recorder.Body.String()
	for _, expected := range []string{"192.0.2.10/32", "runtime", "proxy DNS may change"} {
		if !strings.Contains(response, expected) {
			t.Fatalf("PreflightEgress response missing %q: %s", expected, response)
		}
	}
}
