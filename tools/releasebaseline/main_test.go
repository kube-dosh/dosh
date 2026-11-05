package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	responses map[string][]byte
	errs      map[string]error
	calls     []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, key)
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	output, ok := f.responses[key]
	if !ok {
		return nil, errors.New("unexpected command: " + key)
	}
	return output, nil
}

func fixtureRunner() *fakeRunner {
	selector := "app.kubernetes.io/instance=dosh"
	return &fakeRunner{responses: map[string][]byte{
		"helm -n dosh list -f ^dosh$ -o json": []byte(`[{"name":"dosh","namespace":"dosh","revision":"12","updated":"2026-11-02 11:12:35 +0000 UTC","status":"deployed","chart":"dosh-0.1.4","app_version":"0.1.2"}]`),
		"helm -n dosh history dosh -o json": []byte(`[
			{"revision":11,"updated":"2026-11-02 10:00:00 +0000 UTC","status":"superseded","chart":"dosh-0.1.4","app_version":"0.1.2","description":"Upgrade complete"},
			{"revision":12,"updated":"2026-11-02 11:12:35 +0000 UTC","status":"deployed","chart":"dosh-0.1.4","app_version":"0.1.2","description":"Upgrade complete"}
		]`),
		"helm -n dosh get values dosh -o json": []byte(`{"auth":{"mode":"local"}}`),
		"kubectl -n dosh get deployment dosh -o json": []byte(`{
			"metadata":{"name":"dosh","generation":4},
			"spec":{"replicas":2,"selector":{"matchLabels":{"app.kubernetes.io/instance":"dosh"}},"strategy":{"type":"RollingUpdate"},"template":{"spec":{"serviceAccountName":"dosh","containers":[{"name":"dosh","image":"192.168.122.10:5000/dosh/dosh:0.1.2"}]}}},
			"status":{"observedGeneration":4,"replicas":2,"updatedReplicas":2,"readyReplicas":2,"availableReplicas":2,"unavailableReplicas":0}
		}`),
		"kubectl -n dosh get service dosh -o json": []byte(`{
			"metadata":{"name":"dosh"},
			"spec":{"type":"NodePort","clusterIP":"10.98.167.75","ports":[{"name":"https","protocol":"TCP","port":32626,"targetPort":32626,"nodePort":32626}]}
		}`),
		"kubectl -n dosh get pdb -l " + selector + " -o json": []byte(`{"items":[{
			"metadata":{"name":"dosh"},
			"spec":{"minAvailable":1},
			"status":{"currentHealthy":2,"desiredHealthy":1,"disruptionsAllowed":1,"expectedPods":2}
		}]}`),
		"kubectl -n dosh get pods -l " + selector + " -o json": []byte(`{"items":[
			{"metadata":{"name":"dosh-a"},"spec":{"nodeName":"worker-1","containers":[{"name":"dosh","image":"dosh:0.1.2"}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}],"containerStatuses":[{"restartCount":0}]}},
			{"metadata":{"name":"dosh-b"},"spec":{"nodeName":"worker-2","containers":[{"name":"dosh","image":"dosh:0.1.2"}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}],"containerStatuses":[{"restartCount":2}]}}
		]}`),
		"kubectl -n dosh get secrets -o json": []byte(`{"items":[{
			"metadata":{"name":"dosh-auth"},
			"type":"Opaque",
			"data":{"admin-password-hash":"c3VwZXItc2VjcmV0","session-key":"c3VwZXItc2VjcmV0"}
		}]}`),
		"kubectl config current-context": []byte("prod-cluster\n"),
		"kubectl version -o json":        []byte(`{"serverVersion":{"gitVersion":"v1.36.5"}}`),
	}}
}

func TestCaptureReadOnlyBaseline(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	runner := fixtureRunner()
	baseline, err := capture(context.Background(), runner, Options{Namespace: "dosh", Release: "dosh"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Schema != schemaVersion || !baseline.ReadOnly {
		t.Fatalf("schema=%s readOnly=%v", baseline.Schema, baseline.ReadOnly)
	}
	if baseline.Helm.Revision != "12" || baseline.Helm.Chart != "dosh-0.1.4" || baseline.Helm.AppVersion != "0.1.2" {
		t.Fatalf("helm=%#v", baseline.Helm)
	}
	valuesHash := sha256.Sum256([]byte(`{"auth":{"mode":"local"}}`))
	if baseline.ValuesSHA256 != "sha256:"+hex.EncodeToString(valuesHash[:]) {
		t.Fatalf("values hash=%s", baseline.ValuesSHA256)
	}
	if baseline.Deployment.Image != "192.168.122.10:5000/dosh/dosh:0.1.2" || baseline.Deployment.ReadyReplicas != 2 {
		t.Fatalf("deployment=%#v", baseline.Deployment)
	}
	if len(baseline.Service.Ports) != 1 || baseline.Service.Ports[0].NodePort != 32626 {
		t.Fatalf("service=%#v", baseline.Service)
	}
	if len(baseline.PodDisruptions) != 1 || baseline.PodDisruptions[0].DisruptionsAllowed != 1 {
		t.Fatalf("pdbs=%#v", baseline.PodDisruptions)
	}
	if len(baseline.Pods) != 2 || !baseline.Pods[1].Ready || baseline.Pods[1].Restarts != 2 {
		t.Fatalf("pods=%#v", baseline.Pods)
	}
	if len(baseline.Secrets) != 1 || strings.Join(baseline.Secrets[0].Keys, ",") != "admin-password-hash,session-key" {
		t.Fatalf("secrets=%#v", baseline.Secrets)
	}
	data, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "super-secret") || strings.Contains(text, "c3VwZXItc2VjcmV0") {
		t.Fatalf("baseline leaked secret data: %s", text)
	}
	if len(baseline.CommandAudit) != 10 {
		t.Fatalf("command audit=%v", baseline.CommandAudit)
	}
	for _, call := range runner.calls {
		for _, forbidden := range []string{" upgrade ", " rollback ", " delete ", " apply ", " patch ", " create "} {
			if strings.Contains(" "+call+" ", forbidden) {
				t.Fatalf("capture ran mutation command: %s", call)
			}
		}
	}
}

func TestCaptureRejectsMissingOrAmbiguousRelease(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	for _, fixture := range []string{`[]`, `[{"name":"dosh","namespace":"dosh","revision":"12"},{"name":"dosh","namespace":"dosh","revision":"13"}]`} {
		runner := fixtureRunner()
		runner.responses["helm -n dosh list -f ^dosh$ -o json"] = []byte(fixture)
		if _, err := capture(context.Background(), runner, Options{Namespace: "dosh", Release: "dosh"}, now); err == nil {
			t.Fatalf("fixture %s did not fail", fixture)
		}
	}
}

func TestCaptureRejectsSelectorExpressionsAndCommandFailure(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	runner := fixtureRunner()
	runner.responses["kubectl -n dosh get deployment dosh -o json"] = []byte(`{
		"metadata":{"name":"dosh"},
		"spec":{"selector":{"matchLabels":{"app":"dosh"},"matchExpressions":[{"key":"app","operator":"In","values":["dosh"]}]}}
	}`)
	if _, err := capture(context.Background(), runner, Options{Namespace: "dosh", Release: "dosh"}, now); err == nil || !strings.Contains(err.Error(), "expressions") {
		t.Fatalf("selector error=%v", err)
	}

	runner = fixtureRunner()
	runner.errs = map[string]error{"kubectl -n dosh get secrets -o json": errors.New("forbidden")}
	if _, err := capture(context.Background(), runner, Options{Namespace: "dosh", Release: "dosh"}, now); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("command error=%v", err)
	}
}

func TestCaptureRejectsInvalidNames(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	for _, options := range []Options{{Namespace: "Dosh", Release: "dosh"}, {Namespace: "dosh", Release: "../dosh"}} {
		if _, err := capture(context.Background(), fixtureRunner(), options, now); err == nil {
			t.Fatalf("options %#v did not fail", options)
		}
	}
}

func TestWriteBaselineRefusesOverwriteAndWritesChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	data := []byte("{\"schema\":\"test\"}\n")
	if err := writeBaseline(path, data, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("baseline=%s", got)
	}
	checksum, err := os.ReadFile(path + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	want := hex.EncodeToString(digest[:]) + "  baseline.json\n"
	if string(checksum) != want {
		t.Fatalf("checksum=%s want=%s", checksum, want)
	}
	if err := writeBaseline(path, data, false); err == nil {
		t.Fatal("overwrite unexpectedly succeeded")
	}
	if err := writeBaseline(path, []byte("new\n"), true); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBaselineDetectsReleaseDrift(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	baseline, err := capture(context.Background(), fixtureRunner(), Options{Namespace: "dosh", Release: "dosh"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if drift := verifyBaseline(baseline, baseline); len(drift) != 0 {
		t.Fatalf("identical baseline drift=%v", drift)
	}

	runtimeChurn := baseline
	runtimeChurn.Pods = append([]PodSnapshot(nil), baseline.Pods...)
	runtimeChurn.Pods[0].Name = "dosh-new-pod"
	runtimeChurn.Pods[0].Restarts = 99
	if drift := verifyBaseline(baseline, runtimeChurn); len(drift) != 0 {
		t.Fatalf("pod name/restart churn should not be release drift: %v", drift)
	}

	for name, mutate := range map[string]func(*Baseline){
		"helm revision": func(candidate *Baseline) { candidate.Helm.Revision = "13" },
		"deployment image": func(candidate *Baseline) {
			candidate.Deployment.Image = "192.168.122.10:5000/dosh/dosh:other"
		},
		"nodeport": func(candidate *Baseline) { candidate.Service.Ports[0].NodePort = 32627 },
		"secret keys": func(candidate *Baseline) {
			candidate.Secrets = []SecretSnapshot{{Name: "dosh-auth", Type: "Opaque", Keys: []string{"different-key"}}}
		},
		"pod readiness": func(candidate *Baseline) {
			candidate.Pods = append([]PodSnapshot(nil), baseline.Pods...)
			candidate.Pods[0].Ready = false
		},
		"values hash": func(candidate *Baseline) { candidate.ValuesSHA256 = "sha256:changed" },
	} {
		t.Run(name, func(t *testing.T) {
			actual := baseline
			if name == "nodeport" {
				actual.Service.Ports = append([]ServicePort(nil), baseline.Service.Ports...)
			}
			if name == "secret keys" {
				actual.Secrets = append([]SecretSnapshot(nil), baseline.Secrets...)
			}
			mutate(&actual)
			if drift := verifyBaseline(baseline, actual); len(drift) == 0 {
				t.Fatalf("mutation %q was not detected", name)
			}
		})
	}
}

func TestReadBaselineVerifiesSiblingChecksum(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	baseline, err := capture(context.Background(), fixtureRunner(), Options{Namespace: "dosh", Release: "dosh"}, now)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeBaseline(path, data, false); err != nil {
		t.Fatal(err)
	}
	if _, err := readBaseline(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"schema\":\"tampered\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBaseline(path); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("tampered baseline error=%v", err)
	}
}
