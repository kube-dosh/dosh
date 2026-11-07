package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func validGate(id string, now time.Time) Gate {
	gate := Gate{
		ID:         id,
		Status:     statusPassed,
		Owner:      "owner-" + id,
		Approver:   "approver-" + id,
		VerifiedAt: now.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:  now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		Evidence: []Evidence{{
			Description: "signed acceptance evidence",
			URL:         "https://evidence.example.test/" + id,
			SHA256:      testDigest,
		}},
	}
	if id == "production-change-authorization" {
		gate.ChangeTicket = "CHG-2026-0001"
	}
	return gate
}

func validManifest(now time.Time) Manifest {
	gates := make([]Gate, 0, len(gateDefinitions))
	for _, definition := range gateDefinitions {
		gates = append(gates, validGate(definition.ID, now))
	}
	return Manifest{
		Schema: schemaVersion,
		Candidate: Candidate{
			Tag:          "0.1.4-rc-test",
			Digest:       testDigest,
			AppVersion:   "0.1.4",
			ChartVersion: "0.1.6",
		},
		Gates: gates,
	}
}

func TestValidateAllPassedAllowsRelease(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	validation := validateManifest(validManifest(now), now, "")
	if len(validation.errors) != 0 || len(validation.blockers) != 0 {
		t.Fatalf("errors=%v blockers=%v", validation.errors, validation.blockers)
	}
}

func TestPendingGateBlocksRelease(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0] = Gate{ID: gateDefinitions[0].ID, Status: statusPending, Owner: "identity-team"}
	validation := validateManifest(manifest, now, "")
	if len(validation.errors) != 0 {
		t.Fatalf("unexpected errors: %v", validation.errors)
	}
	if len(validation.blockers) != 1 || !strings.Contains(validation.blockers[0], "pending") {
		t.Fatalf("blockers=%v", validation.blockers)
	}
}

func TestExpiredPassBlocksRelease(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[1].VerifiedAt = now.Add(-48 * time.Hour).Format(time.RFC3339)
	manifest.Gates[1].ExpiresAt = now.Add(-24 * time.Hour).Format(time.RFC3339)
	validation := validateManifest(manifest, now, "")
	if len(validation.errors) != 0 {
		t.Fatalf("unexpected errors: %v", validation.errors)
	}
	if len(validation.blockers) != 1 || !strings.Contains(validation.blockers[0], "expired") {
		t.Fatalf("blockers=%v", validation.blockers)
	}
}

func TestRiskAcceptanceAllowsReleaseWhenComplete(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[3] = Gate{
		ID:        gateDefinitions[3].ID,
		Status:    statusRiskAccepted,
		Owner:     "qa-lead",
		Approver:  "release-board",
		ExpiresAt: now.Add(14 * 24 * time.Hour).Format(time.RFC3339),
		RiskAcceptance: &RiskAcceptance{
			AcceptedAt:     now.Add(-time.Hour).Format(time.RFC3339),
			DocumentSHA256: testDigest,
			Rationale:      "devices unavailable during window",
			Containment:    "limit rollout to internal users and retain WebKit fallback",
			RollbackPlan:   "revert to previous image digest",
		},
	}
	validation := validateManifest(manifest, now, "")
	if len(validation.errors) != 0 || len(validation.blockers) != 0 {
		t.Fatalf("errors=%v blockers=%v", validation.errors, validation.blockers)
	}
}

func TestRiskAcceptanceRequiresContainmentAndRollback(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[3] = Gate{
		ID:        gateDefinitions[3].ID,
		Status:    statusRiskAccepted,
		Owner:     "qa-lead",
		Approver:  "release-board",
		ExpiresAt: now.Add(14 * 24 * time.Hour).Format(time.RFC3339),
		RiskAcceptance: &RiskAcceptance{
			AcceptedAt:     now.Add(-time.Hour).Format(time.RFC3339),
			DocumentSHA256: testDigest,
			Rationale:      "devices unavailable",
		},
	}
	validation := validateManifest(manifest, now, "")
	if len(validation.errors) != 2 {
		t.Fatalf("errors=%v", validation.errors)
	}
	if !strings.Contains(strings.Join(validation.errors, "\n"), "containment") ||
		!strings.Contains(strings.Join(validation.errors, "\n"), "rollbackPlan") {
		t.Fatalf("errors=%v", validation.errors)
	}
}

func TestProductionAuthorizationCannotBeRiskAccepted(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[4] = Gate{
		ID:           gateDefinitions[4].ID,
		Status:       statusRiskAccepted,
		Owner:        "release-owner",
		Approver:     "release-board",
		ChangeTicket: "CHG-2026-0001",
		ExpiresAt:    now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	validation := validateManifest(manifest, now, "")
	if len(validation.errors) == 0 || !strings.Contains(strings.Join(validation.errors, "\n"), "cannot be risk-accepted") {
		t.Fatalf("errors=%v", validation.errors)
	}
}

func TestEvidenceFileHashIsVerified(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	root := t.TempDir()
	content := []byte("release evidence\n")
	if err := os.WriteFile(filepath.Join(root, "evidence.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	manifest := validManifest(now)
	manifest.Gates[0].Evidence = []Evidence{{
		Description: "local acceptance log",
		Path:        "evidence.txt",
		SHA256:      "sha256:" + hex.EncodeToString(sum[:]),
	}}
	validation := validateManifest(manifest, now, root)
	if len(validation.errors) != 0 || len(validation.blockers) != 0 {
		t.Fatalf("errors=%v blockers=%v", validation.errors, validation.blockers)
	}

	manifest.Gates[0].Evidence[0].SHA256 = testDigest
	validation = validateManifest(manifest, now, root)
	if len(validation.errors) != 1 || !strings.Contains(validation.errors[0], "does not match") {
		t.Fatalf("errors=%v", validation.errors)
	}
}

func TestEvidencePathCannotEscapeRoot(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0].Evidence = []Evidence{{
		Description: "escape attempt",
		Path:        "../outside.txt",
		SHA256:      testDigest,
	}}
	validation := validateManifest(manifest, now, t.TempDir())
	if len(validation.errors) != 1 || !strings.Contains(validation.errors[0], "escapes root") {
		t.Fatalf("errors=%v", validation.errors)
	}
}

func TestDuplicateUnknownAndMissingGatesAreInvalid(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0].ID = "unknown"
	manifest.Gates[1].ID = gateDefinitions[2].ID
	validation := validateManifest(manifest, now, "")
	joined := strings.Join(validation.errors, "\n")
	for _, want := range []string{"unknown gate", "duplicate gate", "missing required gate"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in errors: %s", want, joined)
		}
	}
}

func TestReadManifestRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := bytes.Replace(data, []byte(`"schema":`), []byte(`"extra":true,"schema":`), 1)
	path := filepath.Join(t.TempDir(), "gates.json")
	if err := os.WriteFile(path, withUnknown, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error=%v", err)
	}
	if err := os.WriteFile(path, append(data, []byte(` {}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(path); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON error=%v", err)
	}
}

func TestInitAndValidateCommands(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "commercial-gates.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"init",
		"--output", path,
		"--tag", "0.1.4-rc-test",
		"--digest", testDigest,
		"--app-version", "0.1.4",
		"--chart-version", "0.1.6",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init exit=%d stderr=%s", code, stderr.String())
	}
	manifest, err := readManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Gates) != len(gateDefinitions) {
		t.Fatalf("gate count=%d", len(manifest.Gates))
	}
	for _, gate := range manifest.Gates {
		if gate.Status != statusPending {
			t.Fatalf("gate %s status=%s", gate.ID, gate.Status)
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"validate", "--file", path, "--now", now.Format(time.RFC3339)}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("validate exit=%d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "release gate: BLOCKED") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestValidateRejectsInvalidCandidateAndTime(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Candidate.Digest = "sha256:short"
	manifest.Candidate.ChartVersion = "latest"
	validation := validateManifest(manifest, now, "")
	joined := strings.Join(validation.errors, "\n")
	if !strings.Contains(joined, "candidate.digest") || !strings.Contains(joined, "candidate.chartVersion") {
		t.Fatalf("errors=%v", validation.errors)
	}

	path := filepath.Join(t.TempDir(), "gates.json")
	data, err := json.Marshal(validManifest(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"validate", "--file", path, "--now", "not-a-time"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestValidateAllowBlockedStillRejectsInvalidManifest(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0] = Gate{ID: gateDefinitions[0].ID, Status: statusPending}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gates.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"validate", "--file", path, "--allow-blocked", "--now", now.Format(time.RFC3339)}, &stdout, &stderr); code != 0 {
		t.Fatalf("valid blocked exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	manifest.Schema = "wrong-schema"
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"validate", "--file", path, "--allow-blocked", "--now", now.Format(time.RFC3339)}, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func writeManifestFixture(t *testing.T, manifest Manifest) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gates.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertChecksummedFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := os.ReadFile(path + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	fields := strings.Fields(string(checksum))
	if len(fields) != 2 || fields[0] != hex.EncodeToString(digest[:]) || fields[1] != filepath.Base(path) {
		t.Fatalf("checksum=%q for %s", checksum, path)
	}
	return data
}

func TestReportBlockedCanBeArchivedWithAllowBlocked(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0] = Gate{ID: gateDefinitions[0].ID, Status: statusPending, Owner: "identity-team"}
	gatePath := writeManifestFixture(t, manifest)
	reportPath := filepath.Join(t.TempDir(), "gate-report.md")
	args := []string{
		"report", "--file", gatePath, "--output", reportPath,
		"--now", now.Format(time.RFC3339), "--allow-blocked",
	}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("report exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "decision: BLOCKED") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	first := assertChecksummedFile(t, reportPath)
	report := string(first)
	for _, want := range []string{
		"# Commercial Gate Report",
		"- Candidate tag: 0.1.4-rc-test",
		"- Gate manifest SHA-256: sha256:",
		"**BLOCKED**",
		"| production-siem-retention-alerting | PENDING | identity-team |",
		"## Validation Errors",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}

	stdout.Reset()
	stderr.Reset()
	args = append(args, "--force")
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("forced report exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if second := assertChecksummedFile(t, reportPath); !bytes.Equal(first, second) {
		t.Fatalf("report is not deterministic with a fixed --now:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestReportBlockedReturnsTwoButStillWritesReport(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[1] = Gate{ID: gateDefinitions[1].ID, Status: statusPending, Owner: "platform-security"}
	gatePath := writeManifestFixture(t, manifest)
	reportPath := filepath.Join(t.TempDir(), "blocked-report.md")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"report", "--file", gatePath, "--output", reportPath, "--now", now.Format(time.RFC3339),
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("report exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	report := string(assertChecksummedFile(t, reportPath))
	if !strings.Contains(report, "**BLOCKED**") || !strings.Contains(report, gateDefinitions[1].ID) {
		t.Fatalf("report=%s", report)
	}
}

func TestReportInvalidManifestReturnsOneAndRecordsErrors(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Schema = "unsupported-schema"
	gatePath := writeManifestFixture(t, manifest)
	reportPath := filepath.Join(t.TempDir(), "invalid-report.md")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"report", "--file", gatePath, "--output", reportPath, "--now", now.Format(time.RFC3339), "--allow-blocked",
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("report exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	report := string(assertChecksummedFile(t, reportPath))
	if !strings.Contains(report, "**INVALID**") || !strings.Contains(report, "schema: got") {
		t.Fatalf("report=%s", report)
	}
}

func TestReportDefaultsToStdout(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0] = Gate{ID: gateDefinitions[0].ID, Status: statusPending}
	gatePath := writeManifestFixture(t, manifest)
	var stdout, stderr bytes.Buffer
	code := run([]string{"report", "--file", gatePath, "--now", now.Format(time.RFC3339)}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("report exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"# Commercial Gate Report", "0.1.4-rc-test", gateDefinitions[0].ID, "**BLOCKED**"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "report written") {
		t.Fatalf("stdout unexpectedly announced a file: %s", stdout.String())
	}
}

func TestReportRedactsEvidenceURLSecrets(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Gates[0].Evidence[0].URL = "https://user:password@evidence.example.test/artifact?X-Amz-Signature=top-secret&token=also-secret#fragment"
	gatePath := writeManifestFixture(t, manifest)
	var stdout, stderr bytes.Buffer
	code := run([]string{"report", "--file", gatePath, "--now", now.Format(time.RFC3339)}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("report exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	report := stdout.String()
	for _, secret := range []string{"password", "top-secret", "also-secret", "fragment"} {
		if strings.Contains(report, secret) {
			t.Fatalf("report leaked %q:\n%s", secret, report)
		}
	}
	if !strings.Contains(report, "https://evidence.example.test/artifact?[redacted]") {
		t.Fatalf("report did not retain a redacted evidence locator:\n%s", report)
	}
}

func TestReportRefusesToOverwriteGateManifest(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	gatePath := writeManifestFixture(t, validManifest(now))
	before, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"report", "--file", gatePath, "--output", gatePath, "--force", "--now", now.Format(time.RFC3339),
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "would overwrite input") {
		t.Fatalf("report exit=%d stderr=%s", code, stderr.String())
	}
	after, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("report overwrote the gate manifest")
	}
}

func TestReportRefusesOverwriteWithoutForce(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	gatePath := writeManifestFixture(t, validManifest(now))
	reportPath := filepath.Join(t.TempDir(), "passed-report.md")
	args := []string{"report", "--file", gatePath, "--output", reportPath, "--now", now.Format(time.RFC3339)}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("first report exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("overwrite exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	args = append(args, "--force")
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("forced overwrite exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestCandidateBindingsPreventDigestAndFileDrift(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bindings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("docs v234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "openapi.yaml"), []byte("openapi v234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apiHash, err := fileSHA256(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	docHash, err := fileSHA256(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	treeLine := strings.TrimPrefix(apiHash, "sha256:") + "  api/openapi.yaml\n"
	treeHash := sha256.Sum256([]byte(treeLine))
	sourceBinding := strings.Join([]string{
		"schema=dosh-source-binding-v1",
		"image_tag=0.1.4-rc-test",
		"image_digest=" + testDigest,
		"app_version=0.1.4",
		"chart_version=0.1.6",
		"source_count=1",
		"source_tree_sha256=" + hex.EncodeToString(treeHash[:]),
		"",
		"[source_sha256]",
		treeLine,
	}, "\n")
	documentationBinding := strings.Join([]string{
		"schema=dosh-documentation-binding-v1",
		"candidate_digest=" + testDigest,
		"",
		"[sha256]",
		strings.TrimPrefix(docHash, "sha256:") + "  README.md\n",
	}, "\n")
	sourcePath := filepath.Join(root, "bindings", "source.txt")
	documentationPath := filepath.Join(root, "bindings", "docs.txt")
	if err := os.WriteFile(sourcePath, []byte(sourceBinding), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(documentationPath, []byte(documentationBinding), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceBindingHash, err := fileSHA256(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	documentationBindingHash, err := fileSHA256(documentationPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := validManifest(now)
	manifest.Candidate.SourceBindingSHA256 = sourceBindingHash
	manifest.Candidate.SourceBindingPath = "bindings/source.txt"
	manifest.Candidate.DocumentationBindingSHA256 = documentationBindingHash
	manifest.Candidate.DocumentationBindingPath = "bindings/docs.txt"
	validation := validateManifestWithRoots(manifest, now, "", root)
	if len(validation.errors) != 0 || len(validation.blockers) != 0 {
		t.Fatalf("errors=%v blockers=%v", validation.errors, validation.blockers)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("modified docs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	validation = validateManifestWithRoots(manifest, now, "", root)
	if !strings.Contains(strings.Join(validation.errors, "\n"), "README.md hash") {
		t.Fatalf("documentation drift errors=%v", validation.errors)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("docs v234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "openapi.yaml"), []byte("modified api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	validation = validateManifestWithRoots(manifest, now, "", root)
	if !strings.Contains(strings.Join(validation.errors, "\n"), "api/openapi.yaml hash") {
		t.Fatalf("source drift errors=%v", validation.errors)
	}

	manifest.Candidate.Digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	validation = validateManifestWithRoots(manifest, now, "", root)
	if !strings.Contains(strings.Join(validation.errors, "\n"), "image_digest") {
		t.Fatalf("digest drift errors=%v", validation.errors)
	}
}

func TestCandidateBindingPathAndHashMustBePaired(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Candidate.SourceBindingPath = "bindings/source.txt"
	validation := validateManifest(manifest, now, "")
	if !strings.Contains(strings.Join(validation.errors, "\n"), "provided together") {
		t.Fatalf("errors=%v", validation.errors)
	}
}

func TestSkipBindingsKeepsSchemaValidation(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	manifest := validManifest(now)
	manifest.Candidate.SourceBindingPath = "bindings/missing-source.txt"
	manifest.Candidate.SourceBindingSHA256 = testDigest
	manifest.Candidate.DocumentationBindingPath = "bindings/missing-docs.txt"
	manifest.Candidate.DocumentationBindingSHA256 = testDigest

	validation := validateManifestWithRoots(manifest, now, "", t.TempDir())
	if len(validation.errors) != 2 || !strings.Contains(strings.Join(validation.errors, "\n"), "read candidate.sourceBindingPath") {
		t.Fatalf("binding errors=%v", validation.errors)
	}

	validation = validateManifestWithOptions(manifest, now, "", t.TempDir(), true)
	if len(validation.errors) != 0 || len(validation.blockers) != 0 {
		t.Fatalf("schema-only errors=%v blockers=%v", validation.errors, validation.blockers)
	}

	manifest.Candidate.SourceBindingSHA256 = "sha256:short"
	validation = validateManifestWithOptions(manifest, now, "", t.TempDir(), true)
	if !strings.Contains(strings.Join(validation.errors, "\n"), "candidate.sourceBindingSha256") {
		t.Fatalf("schema errors=%v", validation.errors)
	}
}

func writePlanFixture(t *testing.T, dir string, manifest Manifest, ready int32) (string, string) {
	t.Helper()
	gatePath := filepath.Join(dir, "gates.json")
	gateData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, append(gateData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	baseline := releaseBaseline{
		Schema:       "dosh-release-baseline-v1",
		Namespace:    "dosh",
		Release:      "dosh",
		ReadOnly:     true,
		ValuesSHA256: testDigest,
	}
	baseline.Helm.Revision = "12"
	baseline.Helm.Status = "deployed"
	baseline.Helm.Chart = "dosh-0.1.4"
	baseline.Helm.AppVersion = "0.1.2"
	baseline.Deployment.Image = "192.168.122.10:5000/dosh/dosh:0.1.2"
	baseline.Deployment.Replicas = 2
	baseline.Deployment.ReadyReplicas = ready
	baselineData, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	baselineData = append(baselineData, '\n')
	baselinePath := filepath.Join(dir, "baseline.json")
	if err := os.WriteFile(baselinePath, baselineData, 0o600); err != nil {
		t.Fatal(err)
	}
	baselineDigest := sha256.Sum256(baselineData)
	checksum := hex.EncodeToString(baselineDigest[:]) + "  baseline.json\n"
	if err := os.WriteFile(baselinePath+".sha256", []byte(checksum), 0o600); err != nil {
		t.Fatal(err)
	}
	return gatePath, baselinePath
}

func TestRunPlanWritesBoundChangePlan(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	gatePath, baselinePath := writePlanFixture(t, dir, validManifest(now), 2)
	planPath := filepath.Join(dir, "release-plan.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"plan",
		"--file", gatePath,
		"--baseline", baselinePath,
		"--ticket", "CHG-2026-0001",
		"--window-start", now.Add(time.Hour).Format(time.RFC3339),
		"--window-end", now.Add(2 * time.Hour).Format(time.RFC3339),
		"--output", planPath,
		"--now", now.Format(time.RFC3339),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("plan exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan ReleasePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Schema != "dosh-release-plan-v1" || plan.ChangeTicket != "CHG-2026-0001" {
		t.Fatalf("plan=%#v", plan)
	}
	if plan.Target.Digest != testDigest || plan.Baseline.Revision != "12" {
		t.Fatalf("plan target/baseline=%#v/%#v", plan.Target, plan.Baseline)
	}
	if plan.GateManifest.SHA256 == "" || plan.Baseline.SHA256 == "" {
		t.Fatalf("plan hashes missing: %#v", plan)
	}
	releaseCommand := strings.Join(plan.Commands.Release, " ")
	if !strings.Contains(releaseCommand, "image.digest="+testDigest) || !strings.Contains(releaseCommand, "helm upgrade") {
		t.Fatalf("release command=%s", releaseCommand)
	}
	if got := strings.Join(plan.Commands.Rollback[0], " "); !strings.Contains(got, "helm rollback dosh 12") {
		t.Fatalf("rollback command=%s", got)
	}
	checksum, err := os.ReadFile(planPath + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checksum), "release-plan.json") {
		t.Fatalf("checksum=%s", checksum)
	}
}

func TestRunPlanRejectsUnapprovedOrUnhealthyInputs(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	baseArgs := func(gatePath, baselinePath, ticket string) []string {
		return []string{
			"plan", "--file", gatePath, "--baseline", baselinePath, "--ticket", ticket,
			"--window-start", now.Add(time.Hour).Format(time.RFC3339),
			"--window-end", now.Add(2 * time.Hour).Format(time.RFC3339),
			"--output", filepath.Join(t.TempDir(), "plan.json"),
			"--now", now.Format(time.RFC3339),
		}
	}

	dir := t.TempDir()
	gatePath, baselinePath := writePlanFixture(t, dir, validManifest(now), 2)
	var stdout, stderr bytes.Buffer
	if code := run(baseArgs(gatePath, baselinePath, "CHG-WRONG"), &stdout, &stderr); code != 1 {
		t.Fatalf("ticket mismatch exit=%d stderr=%s", code, stderr.String())
	}

	dir = t.TempDir()
	manifest := validManifest(now)
	manifest.Gates[0] = Gate{ID: gateDefinitions[0].ID, Status: statusPending}
	gatePath, baselinePath = writePlanFixture(t, dir, manifest, 2)
	stdout.Reset()
	stderr.Reset()
	if code := run(baseArgs(gatePath, baselinePath, "CHG-2026-0001"), &stdout, &stderr); code != 2 {
		t.Fatalf("blocked gate exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	dir = t.TempDir()
	gatePath, baselinePath = writePlanFixture(t, dir, validManifest(now), 1)
	stdout.Reset()
	stderr.Reset()
	if code := run(baseArgs(gatePath, baselinePath, "CHG-2026-0001"), &stdout, &stderr); code != 1 {
		t.Fatalf("unhealthy baseline exit=%d stderr=%s", code, stderr.String())
	}

	args := baseArgs(gatePath, baselinePath, "CHG-2026-0001")
	for index, value := range args {
		if value == "--window-end" && index+1 < len(args) {
			args[index+1] = now.Add(-time.Hour).Format(time.RFC3339)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("expired window exit=%d stderr=%s", code, stderr.String())
	}
}

func TestRunPlanRequiresMatchingBaselineChecksum(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	gatePath, baselinePath := writePlanFixture(t, dir, validManifest(now), 2)
	args := func() []string {
		return []string{
			"plan", "--file", gatePath, "--baseline", baselinePath, "--ticket", "CHG-2026-0001",
			"--window-start", now.Add(time.Hour).Format(time.RFC3339),
			"--window-end", now.Add(2 * time.Hour).Format(time.RFC3339),
			"--output", filepath.Join(dir, "plan.json"),
			"--now", now.Format(time.RFC3339),
		}
	}
	var stdout, stderr bytes.Buffer
	if err := os.Remove(baselinePath + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if code := run(args(), &stdout, &stderr); code != 1 {
		t.Fatalf("missing checksum exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "read baseline checksum") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	if err := os.WriteFile(baselinePath+".sha256", []byte(strings.Repeat("0", 64)+"  baseline.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(args(), &stdout, &stderr); code != 1 {
		t.Fatalf("mismatched checksum exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not match") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestRunPlanRejectsExistingOutputAndInvalidWindows(t *testing.T) {
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	gatePath, baselinePath := writePlanFixture(t, dir, validManifest(now), 2)
	args := func(output, windowStart, windowEnd string) []string {
		return []string{
			"plan", "--file", gatePath, "--baseline", baselinePath, "--ticket", "CHG-2026-0001",
			"--window-start", windowStart, "--window-end", windowEnd,
			"--output", output, "--now", now.Format(time.RFC3339),
		}
	}
	var stdout, stderr bytes.Buffer
	existingOutput := filepath.Join(dir, "existing-plan.json")
	if err := os.WriteFile(existingOutput, []byte("do not replace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(args(existingOutput, now.Add(time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339)), &stdout, &stderr); code != 1 {
		t.Fatalf("existing output exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("stderr=%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(args(gatePath, now.Add(time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339)), &stdout, &stderr); code != 1 {
		t.Fatalf("gate overwrite exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "would overwrite input") {
		t.Fatalf("stderr=%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	output := filepath.Join(dir, "same-window-plan.json")
	start := now.Add(time.Hour).Format(time.RFC3339)
	if code := run(args(output, start, start), &stdout, &stderr); code != 1 {
		t.Fatalf("same window exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--window-end must be after --window-start") {
		t.Fatalf("stderr=%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	output = filepath.Join(dir, "invalid-time-plan.json")
	if code := run(args(output, "not-a-time", now.Add(2*time.Hour).Format(time.RFC3339)), &stdout, &stderr); code != 1 {
		t.Fatalf("invalid window start exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "parse --window-start") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}
