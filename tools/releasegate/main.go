package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const schemaVersion = "dosh-commercial-gates-v1"

const (
	statusPending      = "pending"
	statusPassed       = "passed"
	statusRiskAccepted = "risk-accepted"
)

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	semverPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$`)
)

type gateDefinition struct {
	ID            string
	Title         string
	NoRiskAccept  bool
	RequireTicket bool
}

var gateDefinitions = []gateDefinition{
	{
		ID:    "production-siem-retention-alerting",
		Title: "Production SIEM delivery, tenancy, retention, alerting and access approval",
	},
	{
		ID:    "edge-safari-real",
		Title: "Real Microsoft Edge and macOS/iOS Safari browser matrix",
	},
	{
		ID:    "github-actions-branch-protection",
		Title: "GitHub Actions green runs, artifacts and protected main branch",
	},
	{
		ID:    "offsite-worm-retention",
		Title: "Off-host WORM evidence retention and documented access approval",
	},
	{
		ID:            "production-change-authorization",
		Title:         "Named production change authorization with maintenance window and rollback owner",
		NoRiskAccept:  true,
		RequireTicket: true,
	},
}

type Candidate struct {
	Tag                        string `json:"tag"`
	Digest                     string `json:"digest"`
	AppVersion                 string `json:"appVersion"`
	ChartVersion               string `json:"chartVersion"`
	SourceBindingSHA256        string `json:"sourceBindingSha256,omitempty"`
	SourceBindingPath          string `json:"sourceBindingPath,omitempty"`
	DocumentationBindingSHA256 string `json:"documentationBindingSha256,omitempty"`
	DocumentationBindingPath   string `json:"documentationBindingPath,omitempty"`
}

type Evidence struct {
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	Path        string `json:"path,omitempty"`
	SHA256      string `json:"sha256"`
}

type RiskAcceptance struct {
	AcceptedAt     string `json:"acceptedAt"`
	DocumentSHA256 string `json:"documentSha256"`
	Rationale      string `json:"rationale"`
	Containment    string `json:"containment"`
	RollbackPlan   string `json:"rollbackPlan"`
}

type Gate struct {
	ID             string          `json:"id"`
	Status         string          `json:"status"`
	Owner          string          `json:"owner,omitempty"`
	Approver       string          `json:"approver,omitempty"`
	VerifiedAt     string          `json:"verifiedAt,omitempty"`
	ExpiresAt      string          `json:"expiresAt,omitempty"`
	ChangeTicket   string          `json:"changeTicket,omitempty"`
	Evidence       []Evidence      `json:"evidence,omitempty"`
	RiskAcceptance *RiskAcceptance `json:"riskAcceptance,omitempty"`
}

type Manifest struct {
	Schema    string    `json:"schema"`
	Candidate Candidate `json:"candidate"`
	Gates     []Gate    `json:"gates"`
}

type ReleasePlan struct {
	Schema       string       `json:"schema"`
	CreatedAt    string       `json:"createdAt"`
	ChangeTicket string       `json:"changeTicket"`
	Window       PlanWindow   `json:"window"`
	Target       Candidate    `json:"target"`
	Baseline     PlanBaseline `json:"baseline"`
	GateManifest PlanFile     `json:"gateManifest"`
	Commands     PlanCommands `json:"commands"`
}

type PlanWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type PlanFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PlanBaseline struct {
	PlanFile
	Namespace    string `json:"namespace"`
	Release      string `json:"release"`
	Revision     string `json:"revision"`
	Chart        string `json:"chart"`
	AppVersion   string `json:"appVersion"`
	Image        string `json:"image"`
	ValuesSHA256 string `json:"valuesSha256"`
}

type PlanCommands struct {
	Preflight [][]string `json:"preflight"`
	Release   []string   `json:"release"`
	PostCheck [][]string `json:"postCheck"`
	Rollback  [][]string `json:"rollback"`
}

type releaseBaseline struct {
	Schema       string `json:"schema"`
	Namespace    string `json:"namespace"`
	Release      string `json:"release"`
	ReadOnly     bool   `json:"readOnly"`
	ValuesSHA256 string `json:"valuesSha256"`
	Helm         struct {
		Revision   string `json:"revision"`
		Status     string `json:"status"`
		Chart      string `json:"chart"`
		AppVersion string `json:"appVersion"`
	} `json:"helm"`
	Deployment struct {
		Image         string `json:"image"`
		Replicas      int32  `json:"replicas"`
		ReadyReplicas int32  `json:"readyReplicas"`
	} `json:"deployment"`
}

type validator struct {
	now          time.Time
	evidenceRoot string
	bindingRoot  string
	skipBindings bool
	errors       []string
	blockers     []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 1
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "report":
		return runReport(args[1:], stdout, stderr)
	case "plan":
		return runPlan(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", args[0])
		printUsage(stderr)
		return 1
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  releasegate init --output <file> --tag <tag> --digest <sha256> --app-version <version> --chart-version <version> [--force]
  releasegate validate --file <file> [--now <RFC3339>] [--evidence-root <dir>] [--binding-root <dir>] [--allow-blocked] [--skip-bindings]
  releasegate report --file <file> [--output <report.md>] [--now <RFC3339>] [--evidence-root <dir>]
                     [--binding-root <dir>] [--allow-blocked] [--force]
  releasegate plan --file <gate-file> --baseline <baseline.json> --ticket <change-ticket>
                   --window-start <RFC3339> --window-end <RFC3339> --output <plan.json>
                   [--namespace dosh] [--release dosh] [--chart-path deploy/helm/dosh] [--timeout 5m]
                   [--evidence-root <dir>] [--binding-root <dir>] [--now <RFC3339>] [--force]
  releasegate help

The gate file contains no credentials. Validation exits 0 only when every required
commercial gate is passed or covered by an unexpired, named risk acceptance.
Exit code 2 means the manifest is structurally valid but release is blocked.
`)
}

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "output gate manifest")
	tag := flags.String("tag", "", "candidate image tag")
	digest := flags.String("digest", "", "candidate image digest")
	appVersion := flags.String("app-version", "", "candidate application version")
	chartVersion := flags.String("chart-version", "", "candidate chart version")
	force := flags.Bool("force", false, "overwrite an existing file")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *output == "" || *tag == "" || *digest == "" || *appVersion == "" || *chartVersion == "" {
		fmt.Fprintln(stderr, "error: init requires --output, --tag, --digest, --app-version and --chart-version")
		return 1
	}
	if *digest != "" && !digestPattern.MatchString(*digest) {
		fmt.Fprintln(stderr, "error: digest must be sha256:<64 lowercase hex>")
		return 1
	}
	if !semverPattern.MatchString(*appVersion) || !semverPattern.MatchString(*chartVersion) {
		fmt.Fprintln(stderr, "error: app and chart versions must be semver")
		return 1
	}
	if !*force {
		if _, err := os.Stat(*output); err == nil {
			fmt.Fprintf(stderr, "error: %s already exists; use --force to replace it\n", *output)
			return 1
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "error: inspect output file: %v\n", err)
			return 1
		}
	}

	gates := make([]Gate, 0, len(gateDefinitions))
	for _, definition := range gateDefinitions {
		gates = append(gates, Gate{ID: definition.ID, Status: statusPending})
	}
	manifest := Manifest{
		Schema: schemaVersion,
		Candidate: Candidate{
			Tag:          *tag,
			Digest:       *digest,
			AppVersion:   *appVersion,
			ChartVersion: *chartVersion,
		},
		Gates: gates,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: encode manifest: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	if err := writeFileAtomic(*output, data); err != nil {
		fmt.Fprintf(stderr, "error: write manifest: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "commercial gate manifest initialized: %s\n", *output)
	fmt.Fprintf(stdout, "candidate: tag=%s digest=%s app=%s chart=%s\n", *tag, *digest, *appVersion, *chartVersion)
	fmt.Fprintf(stdout, "gates: %d pending; release is blocked until each gate is passed or covered by a valid risk acceptance\n", len(gates))
	return 0
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "gate manifest")
	nowText := flags.String("now", "", "evaluation time in RFC3339; defaults to current UTC time")
	evidenceRoot := flags.String("evidence-root", "", "base directory for relative evidence paths")
	bindingRoot := flags.String("binding-root", ".", "base directory for source and documentation binding files")
	allowBlocked := flags.Bool("allow-blocked", false, "return success for a valid manifest even when release gates are pending or expired")
	skipBindings := flags.Bool("skip-bindings", false, "validate candidate structure without reading source or documentation bindings")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *file == "" {
		fmt.Fprintln(stderr, "error: validate requires --file")
		return 1
	}
	now, ok := parseNow(*nowText, stderr)
	if !ok {
		return 1
	}
	manifest, err := readManifest(*file)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	validation := validateManifestWithOptions(manifest, now, *evidenceRoot, *bindingRoot, *skipBindings)
	printValidation(stdout, manifest, validation)
	if len(validation.errors) > 0 {
		return 1
	}
	if len(validation.blockers) > 0 {
		if *allowBlocked {
			return 0
		}
		return 2
	}
	return 0
}

func runReport(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "gate manifest")
	output := flags.String("output", "", "Markdown report output; defaults to stdout")
	nowText := flags.String("now", "", "evaluation time in RFC3339; defaults to current UTC time")
	evidenceRoot := flags.String("evidence-root", "", "base directory for relative evidence paths")
	bindingRoot := flags.String("binding-root", ".", "base directory for source and documentation binding files")
	allowBlocked := flags.Bool("allow-blocked", false, "return success for a valid manifest even when release gates are pending or expired")
	force := flags.Bool("force", false, "overwrite an existing report and checksum")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *file == "" {
		fmt.Fprintln(stderr, "error: report requires --file")
		return 1
	}
	if *output != "" {
		if err := ensureOutputDoesNotOverwrite(*output, []string{*file}); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	}
	now, ok := parseNow(*nowText, stderr)
	if !ok {
		return 1
	}
	manifest, err := readManifest(*file)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	manifestHash, err := fileSHA256(*file)
	if err != nil {
		fmt.Fprintf(stderr, "error: hash gate manifest: %v\n", err)
		return 1
	}
	validation := validateManifestWithRoots(manifest, now, *evidenceRoot, *bindingRoot)
	report := renderGateReport(manifest, validation, now, *file, manifestHash)
	if *output == "" {
		fmt.Fprint(stdout, report)
	} else {
		if err := writeChecksummedFile(*output, []byte(report), *force, "report"); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "commercial gate report written: %s\n", *output)
		fmt.Fprintf(stdout, "decision: %s\n", validationDecision(validation))
	}
	switch {
	case len(validation.errors) > 0:
		return 1
	case len(validation.blockers) > 0 && !*allowBlocked:
		return 2
	default:
		return 0
	}
}

func parseNow(text string, stderr io.Writer) (time.Time, bool) {
	if text == "" {
		return time.Now().UTC(), true
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		fmt.Fprintf(stderr, "error: parse --now: %v\n", err)
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func validationDecision(validation validator) string {
	switch {
	case len(validation.errors) > 0:
		return "INVALID"
	case len(validation.blockers) > 0:
		return "BLOCKED"
	default:
		return "PASS"
	}
}

func runPlan(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	gateFile := flags.String("file", "", "commercial gate manifest")
	baselinePath := flags.String("baseline", "", "read-only release baseline JSON")
	ticket := flags.String("ticket", "", "production change ticket")
	windowStartText := flags.String("window-start", "", "change window start in RFC3339")
	windowEndText := flags.String("window-end", "", "change window end in RFC3339")
	output := flags.String("output", "", "release plan JSON output")
	namespace := flags.String("namespace", "dosh", "release namespace")
	release := flags.String("release", "dosh", "Helm release and Deployment name")
	chartPath := flags.String("chart-path", "deploy/helm/dosh", "local Helm chart path")
	timeout := flags.String("timeout", "5m", "Helm and rollout wait timeout")
	nowText := flags.String("now", "", "planning time in RFC3339; defaults to current UTC time")
	evidenceRoot := flags.String("evidence-root", "", "base directory for relative evidence paths")
	bindingRoot := flags.String("binding-root", ".", "base directory for source and documentation binding files")
	force := flags.Bool("force", false, "overwrite an existing plan and checksum")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *gateFile == "" || *baselinePath == "" || *ticket == "" ||
		*windowStartText == "" || *windowEndText == "" || *output == "" {
		fmt.Fprintln(stderr, "error: plan requires --file, --baseline, --ticket, --window-start, --window-end and --output")
		return 1
	}
	if err := ensureOutputDoesNotOverwrite(*output, []string{*gateFile, *baselinePath}); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	now, ok := parseNow(*nowText, stderr)
	if !ok {
		return 1
	}
	manifest, err := readManifest(*gateFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	validation := validateManifestWithRoots(manifest, now, *evidenceRoot, *bindingRoot)
	printValidation(stdout, manifest, validation)
	if len(validation.errors) > 0 {
		return 1
	}
	if len(validation.blockers) > 0 {
		fmt.Fprintln(stderr, "error: release plan not created because commercial gates are blocked")
		return 2
	}
	var authorization *Gate
	for index := range manifest.Gates {
		if manifest.Gates[index].ID == "production-change-authorization" {
			authorization = &manifest.Gates[index]
			break
		}
	}
	if authorization == nil || authorization.Status != statusPassed {
		fmt.Fprintln(stderr, "error: production-change-authorization gate must be passed")
		return 1
	}
	if authorization.ChangeTicket != *ticket {
		fmt.Fprintf(stderr, "error: ticket %q does not match authorized gate ticket %q\n", *ticket, authorization.ChangeTicket)
		return 1
	}
	windowStart, err := time.Parse(time.RFC3339, *windowStartText)
	if err != nil {
		fmt.Fprintf(stderr, "error: parse --window-start: %v\n", err)
		return 1
	}
	windowEnd, err := time.Parse(time.RFC3339, *windowEndText)
	if err != nil {
		fmt.Fprintf(stderr, "error: parse --window-end: %v\n", err)
		return 1
	}
	windowStart = windowStart.UTC()
	windowEnd = windowEnd.UTC()
	if !windowEnd.After(windowStart) {
		fmt.Fprintln(stderr, "error: --window-end must be after --window-start")
		return 1
	}
	if windowEnd.Before(now) {
		fmt.Fprintln(stderr, "error: change window has already ended")
		return 1
	}
	baseline, baselineHash, err := readReleaseBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := validateReleaseBaseline(baseline, *namespace, *release); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	gateHash, err := fileSHA256(*gateFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: hash gate manifest: %v\n", err)
		return 1
	}
	postBaselinePath := strings.TrimSuffix(*output, filepath.Ext(*output)) + "-post-baseline.json"
	plan := ReleasePlan{
		Schema:       "dosh-release-plan-v1",
		CreatedAt:    now.UTC().Format(time.RFC3339),
		ChangeTicket: *ticket,
		Window:       PlanWindow{Start: windowStart.Format(time.RFC3339), End: windowEnd.Format(time.RFC3339)},
		Target:       manifest.Candidate,
		Baseline: PlanBaseline{
			PlanFile:     PlanFile{Path: *baselinePath, SHA256: baselineHash},
			Namespace:    baseline.Namespace,
			Release:      baseline.Release,
			Revision:     baseline.Helm.Revision,
			Chart:        baseline.Helm.Chart,
			AppVersion:   baseline.Helm.AppVersion,
			Image:        baseline.Deployment.Image,
			ValuesSHA256: baseline.ValuesSHA256,
		},
		GateManifest: PlanFile{Path: *gateFile, SHA256: gateHash},
		Commands: PlanCommands{
			Preflight: [][]string{
				{"make", "release-gate-check", "GATE_FILE=" + *gateFile},
				{"make", "release-baseline-verify", "BASELINE_IN=" + *baselinePath},
			},
			Release: []string{
				"helm", "upgrade", *release, *chartPath, "-n", *namespace,
				"--set", "image.digest=" + manifest.Candidate.Digest,
				"--wait", "--timeout", *timeout,
			},
			PostCheck: [][]string{
				{"kubectl", "-n", *namespace, "rollout", "status", "deployment/" + *release, "--timeout=" + *timeout},
				{"bash", "scripts/release-baseline.sh", "capture", "--namespace", *namespace, "--release", *release, "--output", postBaselinePath},
			},
			Rollback: [][]string{
				{"helm", "rollback", *release, baseline.Helm.Revision, "-n", *namespace, "--wait", "--timeout", *timeout},
				{"kubectl", "-n", *namespace, "rollout", "status", "deployment/" + *release, "--timeout=" + *timeout},
			},
		},
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: encode release plan: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	if err := writePlan(*output, data, *force); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	digest := sha256.Sum256(data)
	fmt.Fprintf(stdout, "release plan written: %s\n", *output)
	fmt.Fprintf(stdout, "ticket=%s baseline_revision=%s target_digest=%s window=%s..%s\n",
		*ticket, baseline.Helm.Revision, manifest.Candidate.Digest, windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
	fmt.Fprintf(stdout, "sha256=%s\n", hex.EncodeToString(digest[:]))
	return 0
}

func readReleaseBaseline(path string) (releaseBaseline, string, error) {
	var baseline releaseBaseline
	data, err := os.ReadFile(path)
	if err != nil {
		return baseline, "", fmt.Errorf("read baseline: %w", err)
	}
	if err := json.Unmarshal(data, &baseline); err != nil {
		return baseline, "", fmt.Errorf("decode baseline: %w", err)
	}
	digest := sha256.Sum256(data)
	actualHash := "sha256:" + hex.EncodeToString(digest[:])
	checksumData, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return baseline, "", fmt.Errorf("read baseline checksum %s.sha256: %w", path, err)
	}
	fields := strings.Fields(string(checksumData))
	if len(fields) < 1 || !strings.EqualFold(fields[0], strings.TrimPrefix(actualHash, "sha256:")) {
		return baseline, "", fmt.Errorf("baseline SHA-256 does not match %s.sha256", path)
	}
	return baseline, actualHash, nil
}

func validateReleaseBaseline(baseline releaseBaseline, namespace, release string) error {
	if baseline.Schema != "dosh-release-baseline-v1" {
		return fmt.Errorf("baseline schema %q is not dosh-release-baseline-v1", baseline.Schema)
	}
	if baseline.Namespace != namespace || baseline.Release != release {
		return fmt.Errorf("baseline identity %s/%s does not match %s/%s", baseline.Namespace, baseline.Release, namespace, release)
	}
	if !baseline.ReadOnly {
		return errors.New("baseline was not captured in read-only mode")
	}
	if baseline.Helm.Status != "deployed" || baseline.Helm.Revision == "" {
		return fmt.Errorf("baseline Helm state is not deployed: revision=%q status=%q", baseline.Helm.Revision, baseline.Helm.Status)
	}
	if baseline.Deployment.Replicas <= 0 || baseline.Deployment.ReadyReplicas != baseline.Deployment.Replicas {
		return fmt.Errorf("baseline Deployment is not fully ready: replicas=%d ready=%d", baseline.Deployment.Replicas, baseline.Deployment.ReadyReplicas)
	}
	if baseline.Deployment.Image == "" || !digestPattern.MatchString(baseline.ValuesSHA256) {
		return errors.New("baseline is missing deployment image or values SHA-256")
	}
	return nil
}

func writePlan(path string, data []byte, force bool) error {
	return writeChecksummedFile(path, data, force, "plan")
}

func ensureOutputDoesNotOverwrite(output string, inputs []string) error {
	outputPaths := []string{output, output + ".sha256"}
	for _, outputPath := range outputPaths {
		resolvedOutput, err := resolvedPath(outputPath)
		if err != nil {
			return fmt.Errorf("resolve output %s: %w", outputPath, err)
		}
		for _, input := range inputs {
			for _, inputPath := range []string{input, input + ".sha256"} {
				resolvedInput, err := resolvedPath(inputPath)
				if err != nil {
					return fmt.Errorf("resolve input %s: %w", inputPath, err)
				}
				if resolvedOutput == resolvedInput {
					return fmt.Errorf("output %s would overwrite input %s", outputPath, inputPath)
				}
			}
		}
	}
	return nil
}

func resolvedPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(absolute)
	if resolvedDirectory, err := filepath.EvalSymlinks(directory); err == nil {
		absolute = filepath.Join(resolvedDirectory, filepath.Base(absolute))
	}
	return filepath.Clean(absolute), nil
}

func writeChecksummedFile(path string, data []byte, force bool, label string) error {
	checksumPath := path + ".sha256"
	if !force {
		for _, candidate := range []string{path, checksumPath} {
			if _, err := os.Stat(candidate); err == nil {
				return fmt.Errorf("%s already exists; use --force to replace it", candidate)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect %s: %w", candidate, err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	digest := sha256.Sum256(data)
	checksum := []byte(hex.EncodeToString(digest[:]) + "  " + filepath.Base(path) + "\n")
	if err := writeFileAtomic(checksumPath, checksum); err != nil {
		return fmt.Errorf("write %s checksum: %w", label, err)
	}
	return nil
}

func readManifest(path string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode %s: %w", path, err)
	}
	if decoder.More() {
		return manifest, fmt.Errorf("decode %s: trailing JSON value", path)
	}
	return manifest, nil
}

func validateManifest(manifest Manifest, now time.Time, evidenceRoot string) validator {
	return validateManifestWithRoots(manifest, now, evidenceRoot, ".")
}

func validateManifestWithRoots(manifest Manifest, now time.Time, evidenceRoot, bindingRoot string) validator {
	return validateManifestWithOptions(manifest, now, evidenceRoot, bindingRoot, false)
}

func validateManifestWithOptions(manifest Manifest, now time.Time, evidenceRoot, bindingRoot string, skipBindings bool) validator {
	v := validator{now: now.UTC(), evidenceRoot: evidenceRoot, bindingRoot: bindingRoot, skipBindings: skipBindings}
	if manifest.Schema != schemaVersion {
		v.addError("schema", "got %q, want %q", manifest.Schema, schemaVersion)
	}
	v.validateCandidate(manifest.Candidate)

	required := make(map[string]gateDefinition, len(gateDefinitions))
	for _, definition := range gateDefinitions {
		required[definition.ID] = definition
	}
	seen := make(map[string]struct{}, len(manifest.Gates))
	for index, gate := range manifest.Gates {
		path := fmt.Sprintf("gates[%d]", index)
		definition, ok := required[gate.ID]
		if !ok {
			v.addError(path+".id", "unknown gate %q", gate.ID)
			continue
		}
		if _, duplicate := seen[gate.ID]; duplicate {
			v.addError(path+".id", "duplicate gate %q", gate.ID)
			continue
		}
		seen[gate.ID] = struct{}{}
		v.validateGate(path, gate, definition)
	}
	for _, definition := range gateDefinitions {
		if _, ok := seen[definition.ID]; !ok {
			v.addError("gates", "missing required gate %q", definition.ID)
		}
	}
	sort.Strings(v.errors)
	sort.Strings(v.blockers)
	return v
}

func (v *validator) validateCandidate(candidate Candidate) {
	if strings.TrimSpace(candidate.Tag) == "" {
		v.addError("candidate.tag", "must not be empty")
	}
	if !digestPattern.MatchString(candidate.Digest) {
		v.addError("candidate.digest", "must be sha256:<64 lowercase hex>")
	}
	if !semverPattern.MatchString(candidate.AppVersion) {
		v.addError("candidate.appVersion", "must be semver")
	}
	if !semverPattern.MatchString(candidate.ChartVersion) {
		v.addError("candidate.chartVersion", "must be semver")
	}
	for field, value := range map[string]string{
		"candidate.sourceBindingSha256":        candidate.SourceBindingSHA256,
		"candidate.documentationBindingSha256": candidate.DocumentationBindingSHA256,
	} {
		if value != "" && !digestPattern.MatchString(value) {
			v.addError(field, "must be empty or sha256:<64 lowercase hex>")
		}
	}
	if !v.skipBindings {
		v.validateCandidateBindings(candidate)
	}
}

func (v *validator) validateCandidateBindings(candidate Candidate) {
	type binding struct {
		name string
		path string
		hash string
	}
	for _, item := range []binding{
		{name: "sourceBinding", path: candidate.SourceBindingPath, hash: candidate.SourceBindingSHA256},
		{name: "documentationBinding", path: candidate.DocumentationBindingPath, hash: candidate.DocumentationBindingSHA256},
	} {
		if (item.path == "") != (item.hash == "") {
			v.addError("candidate."+item.name, "path and sha256 must be provided together")
		}
	}
	if candidate.SourceBindingPath != "" && candidate.SourceBindingSHA256 != "" {
		v.validateSourceBinding(candidate)
	}
	if candidate.DocumentationBindingPath != "" && candidate.DocumentationBindingSHA256 != "" {
		v.validateDocumentationBinding(candidate)
	}
}

func (v *validator) validateSourceBinding(candidate Candidate) {
	data, actual, err := v.readBindingFile("candidate.sourceBindingPath", candidate.SourceBindingPath)
	if err != nil {
		v.addError("candidate.sourceBindingPath", "%v", err)
		return
	}
	if actual != candidate.SourceBindingSHA256 {
		v.addError("candidate.sourceBindingSha256", "file hash %s does not match %s", actual, candidate.SourceBindingSHA256)
		return
	}
	headers, entries, err := parseBinding(data)
	if err != nil {
		v.addError("candidate.sourceBindingPath", "parse: %v", err)
		return
	}
	if headers["schema"] != "dosh-source-binding-v1" {
		v.addError("candidate.sourceBindingPath", "schema %q is not dosh-source-binding-v1", headers["schema"])
	}
	for field, want := range map[string]string{
		"image_tag":     candidate.Tag,
		"image_digest":  candidate.Digest,
		"app_version":   candidate.AppVersion,
		"chart_version": candidate.ChartVersion,
	} {
		if headers[field] != want {
			v.addError("candidate.sourceBindingPath", "%s=%q does not match candidate %q", field, headers[field], want)
		}
	}
	v.validateBindingEntries("candidate.sourceBindingPath", entries)
	count, err := strconv.Atoi(headers["source_count"])
	if err != nil || count != len(entries) {
		v.addError("candidate.sourceBindingPath", "source_count=%q does not match %d entries", headers["source_count"], len(entries))
	}
	treeHash := bindingTreeHash(entries)
	if headers["source_tree_sha256"] != treeHash {
		v.addError("candidate.sourceBindingPath", "source_tree_sha256=%q does not match %q", headers["source_tree_sha256"], treeHash)
	}
}

func (v *validator) validateDocumentationBinding(candidate Candidate) {
	data, actual, err := v.readBindingFile("candidate.documentationBindingPath", candidate.DocumentationBindingPath)
	if err != nil {
		v.addError("candidate.documentationBindingPath", "%v", err)
		return
	}
	if actual != candidate.DocumentationBindingSHA256 {
		v.addError("candidate.documentationBindingSha256", "file hash %s does not match %s", actual, candidate.DocumentationBindingSHA256)
		return
	}
	headers, entries, err := parseBinding(data)
	if err != nil {
		v.addError("candidate.documentationBindingPath", "parse: %v", err)
		return
	}
	if headers["schema"] != "dosh-documentation-binding-v1" {
		v.addError("candidate.documentationBindingPath", "schema %q is not dosh-documentation-binding-v1", headers["schema"])
	}
	if headers["candidate_digest"] != candidate.Digest {
		v.addError("candidate.documentationBindingPath", "candidate_digest=%q does not match %q", headers["candidate_digest"], candidate.Digest)
	}
	v.validateBindingEntries("candidate.documentationBindingPath", entries)
}

type bindingEntry struct {
	hash string
	path string
}

func (v *validator) readBindingFile(field, path string) ([]byte, string, error) {
	resolved, err := resolveUnderRoot(v.bindingRoot, path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", field, err)
	}
	actual, err := fileSHA256(resolved)
	if err != nil {
		return nil, "", err
	}
	return data, actual, nil
}

func parseBinding(data []byte) (map[string]string, []bindingEntry, error) {
	headers := make(map[string]string)
	var entries []bindingEntry
	inEntries := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inEntries = true
			continue
		}
		if !inEntries {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				return nil, nil, fmt.Errorf("invalid header line %q", line)
			}
			key := strings.TrimSpace(parts[0])
			if _, duplicate := headers[key]; duplicate {
				return nil, nil, fmt.Errorf("duplicate header %q", key)
			}
			headers[key] = strings.TrimSpace(parts[1])
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || !digestPattern.MatchString("sha256:"+parts[0]) || strings.TrimSpace(parts[1]) == "" {
			return nil, nil, fmt.Errorf("invalid checksum line %q", line)
		}
		entries = append(entries, bindingEntry{hash: "sha256:" + parts[0], path: strings.TrimSpace(parts[1])})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("binding has no checksum entries")
	}
	return headers, entries, nil
}

func (v *validator) validateBindingEntries(field string, entries []bindingEntry) {
	for _, entry := range entries {
		resolved, err := resolveUnderRoot(v.bindingRoot, entry.path)
		if err != nil {
			v.addError(field, "%v", err)
			continue
		}
		actual, err := fileSHA256(resolved)
		if err != nil {
			v.addError(field, "%v", err)
			continue
		}
		if actual != entry.hash {
			v.addError(field, "%s hash %s does not match %s", entry.path, actual, entry.hash)
		}
	}
}

func bindingTreeHash(entries []bindingEntry) string {
	sorted := append([]bindingEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })
	var builder strings.Builder
	for _, entry := range sorted {
		builder.WriteString(strings.TrimPrefix(entry.hash, "sha256:"))
		builder.WriteString("  ")
		builder.WriteString(entry.path)
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func (v *validator) validateGate(path string, gate Gate, definition gateDefinition) {
	if strings.TrimSpace(gate.Owner) == "" {
		if gate.Status == statusPassed || gate.Status == statusRiskAccepted {
			v.addError(path+".owner", "must not be empty")
		}
	}
	switch gate.Status {
	case statusPending:
		if strings.TrimSpace(gate.Owner) == "" {
			v.blockers = append(v.blockers, fmt.Sprintf("%s (%s): pending and no accountable owner recorded", definition.ID, definition.Title))
		} else {
			v.blockers = append(v.blockers, fmt.Sprintf("%s (%s): pending; owner=%s", definition.ID, definition.Title, gate.Owner))
		}
	case statusPassed:
		if definition.RequireTicket && strings.TrimSpace(gate.ChangeTicket) == "" {
			v.addError(path+".changeTicket", "must not be empty")
		}
		v.validateIdentity(path, gate)
		verifiedAt, ok := v.parseGateTime(path+".verifiedAt", gate.VerifiedAt)
		if ok && verifiedAt.After(v.now) {
			v.addError(path+".verifiedAt", "must not be in the future")
		}
		expiresAt, okExpiry := v.parseGateTime(path+".expiresAt", gate.ExpiresAt)
		if ok && okExpiry && !expiresAt.After(verifiedAt) {
			v.addError(path+".expiresAt", "must be after verifiedAt")
		}
		if okExpiry {
			if !expiresAt.After(v.now) {
				v.blockers = append(v.blockers, fmt.Sprintf("%s (%s): evidence expired at %s", definition.ID, definition.Title, expiresAt.Format(time.RFC3339)))
			}
		}
		if len(gate.Evidence) == 0 {
			v.addError(path+".evidence", "passed gate requires at least one evidence item")
		}
		for index, evidence := range gate.Evidence {
			v.validateEvidence(fmt.Sprintf("%s.evidence[%d]", path, index), evidence)
		}
	case statusRiskAccepted:
		if definition.RequireTicket && strings.TrimSpace(gate.ChangeTicket) == "" {
			v.addError(path+".changeTicket", "must not be empty")
		}
		if definition.NoRiskAccept {
			v.addError(path+".status", "gate %q cannot be risk-accepted; it must be passed", definition.ID)
			return
		}
		v.validateIdentity(path, gate)
		if gate.RiskAcceptance == nil {
			v.addError(path+".riskAcceptance", "must be present for risk-accepted status")
			return
		}
		acceptedAt, acceptedOK := v.parseGateTime(path+".riskAcceptance.acceptedAt", gate.RiskAcceptance.AcceptedAt)
		if acceptedOK && acceptedAt.After(v.now) {
			v.addError(path+".riskAcceptance.acceptedAt", "must not be in the future")
		}
		expiresAt, expiresOK := v.parseGateTime(path+".expiresAt", gate.ExpiresAt)
		if acceptedOK && expiresOK && !expiresAt.After(acceptedAt) {
			v.addError(path+".expiresAt", "must be after risk acceptance time")
		}
		if expiresOK && !expiresAt.After(v.now) {
			v.blockers = append(v.blockers, fmt.Sprintf("%s (%s): risk acceptance expired at %s", definition.ID, definition.Title, expiresAt.Format(time.RFC3339)))
		}
		if !digestPattern.MatchString(gate.RiskAcceptance.DocumentSHA256) {
			v.addError(path+".riskAcceptance.documentSha256", "must be sha256:<64 lowercase hex>")
		}
		for field, value := range map[string]string{
			"rationale":    gate.RiskAcceptance.Rationale,
			"containment":  gate.RiskAcceptance.Containment,
			"rollbackPlan": gate.RiskAcceptance.RollbackPlan,
		} {
			if strings.TrimSpace(value) == "" {
				v.addError(path+".riskAcceptance."+field, "must not be empty")
			}
		}
	default:
		v.addError(path+".status", "got %q, want %s, %s or %s", gate.Status, statusPending, statusPassed, statusRiskAccepted)
	}
}

func (v *validator) validateIdentity(path string, gate Gate) {
	if strings.TrimSpace(gate.Owner) == "" {
		return
	}
	if strings.TrimSpace(gate.Approver) == "" {
		v.addError(path+".approver", "must not be empty")
		return
	}
	if gate.Owner == gate.Approver {
		v.addError(path+".approver", "must differ from owner")
	}
}

func (v *validator) validateEvidence(path string, evidence Evidence) {
	if strings.TrimSpace(evidence.Description) == "" {
		v.addError(path+".description", "must not be empty")
	}
	if !digestPattern.MatchString(evidence.SHA256) {
		v.addError(path+".sha256", "must be sha256:<64 lowercase hex>")
	}
	if evidence.URL == "" && evidence.Path == "" {
		v.addError(path, "requires url or path")
	}
	if evidence.URL != "" {
		parsed, err := url.Parse(evidence.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			v.addError(path+".url", "must be an absolute https URL")
		}
	}
	if evidence.Path == "" {
		return
	}
	resolved, err := v.resolveEvidencePath(evidence.Path)
	if err != nil {
		v.addError(path+".path", "%v", err)
		return
	}
	actual, err := fileSHA256(resolved)
	if err != nil {
		v.addError(path+".path", "%v", err)
		return
	}
	if actual != evidence.SHA256 {
		v.addError(path+".sha256", "file hash %s does not match %s", actual, evidence.SHA256)
	}
}

func (v *validator) resolveEvidencePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return resolveUnderRoot(v.evidenceRoot, path)
}

func resolveUnderRoot(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", errors.New("absolute paths are not allowed")
	}
	if root == "" {
		root = "."
	}
	absoluteBase, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	resolved := filepath.Join(absoluteBase, path)
	relative, err := filepath.Rel(absoluteBase, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes root")
	}
	return resolved, nil
}

func (v *validator) parseGateTime(path, text string) (time.Time, bool) {
	if strings.TrimSpace(text) == "" {
		v.addError(path, "must not be empty")
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		v.addError(path, "must be RFC3339")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func (v *validator) addError(path, format string, args ...any) {
	v.errors = append(v.errors, fmt.Sprintf("%s: %s", path, fmt.Sprintf(format, args...)))
}

func renderGateReport(manifest Manifest, validation validator, now time.Time, manifestPath, manifestHash string) string {
	definitions := make(map[string]gateDefinition, len(gateDefinitions))
	for _, definition := range gateDefinitions {
		definitions[definition.ID] = definition
	}
	gates := append([]Gate(nil), manifest.Gates...)
	sort.SliceStable(gates, func(i, j int) bool {
		return gateOrder(gates[i].ID) < gateOrder(gates[j].ID)
	})

	var report strings.Builder
	fmt.Fprintln(&report, "# Commercial Gate Report")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Candidate tag: %s\n", markdownCell(manifest.Candidate.Tag))
	fmt.Fprintf(&report, "- Candidate digest: %s\n", markdownCell(manifest.Candidate.Digest))
	fmt.Fprintf(&report, "- Application version: %s\n", markdownCell(manifest.Candidate.AppVersion))
	fmt.Fprintf(&report, "- Chart version: %s\n", markdownCell(manifest.Candidate.ChartVersion))
	fmt.Fprintf(&report, "- Gate manifest: %s\n", markdownCell(manifestPath))
	fmt.Fprintf(&report, "- Gate manifest SHA-256: %s\n", markdownCell(manifestHash))
	fmt.Fprintf(&report, "- Evaluated at: %s\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Decision")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "**%s**\n", validationDecision(validation))
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "%d validation error(s), %d release blocker(s).\n", len(validation.errors), len(validation.blockers))
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Gates")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Gate | Status | Owner | Approver | Verified at | Expires at | Change ticket |")
	fmt.Fprintln(&report, "| --- | --- | --- | --- | --- | --- | --- |")
	for _, gate := range gates {
		fmt.Fprintf(&report, "| %s | %s | %s | %s | %s | %s | %s |\n",
			markdownCell(gate.ID),
			markdownCell(gateStatusLabel(gate.Status)),
			markdownCell(gate.Owner),
			markdownCell(gate.Approver),
			markdownCell(gate.VerifiedAt),
			markdownCell(gate.ExpiresAt),
			markdownCell(gate.ChangeTicket),
		)
	}
	if len(gates) == 0 {
		fmt.Fprintln(&report, "| - | - | - | - | - | - | - |")
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Evidence")
	fmt.Fprintln(&report)
	evidenceCount := 0
	for _, gate := range gates {
		if len(gate.Evidence) == 0 {
			continue
		}
		fmt.Fprintf(&report, "### %s\n", markdownCell(gate.ID))
		fmt.Fprintln(&report)
		for index, evidence := range gate.Evidence {
			evidenceCount++
			fmt.Fprintf(&report, "- Evidence %d: %s\n", index+1, markdownCell(evidence.Description))
			if evidence.URL != "" {
				fmt.Fprintf(&report, "  - URL: %s\n", markdownCell(redactEvidenceURL(evidence.URL)))
			}
			if evidence.Path != "" {
				fmt.Fprintf(&report, "  - Path: %s\n", markdownCell(evidence.Path))
			}
			fmt.Fprintf(&report, "  - SHA-256: %s\n", markdownCell(evidence.SHA256))
		}
		fmt.Fprintln(&report)
	}
	if evidenceCount == 0 {
		fmt.Fprintln(&report, "No evidence recorded.")
		fmt.Fprintln(&report)
	}

	riskAcceptanceCount := 0
	for _, gate := range gates {
		if gate.RiskAcceptance == nil {
			continue
		}
		if riskAcceptanceCount == 0 {
			fmt.Fprintln(&report, "## Risk Acceptance")
			fmt.Fprintln(&report)
		}
		riskAcceptanceCount++
		acceptance := gate.RiskAcceptance
		fmt.Fprintf(&report, "### %s\n", markdownCell(gate.ID))
		fmt.Fprintln(&report)
		fmt.Fprintf(&report, "- Accepted at: %s\n", markdownCell(acceptance.AcceptedAt))
		fmt.Fprintf(&report, "- Document SHA-256: %s\n", markdownCell(acceptance.DocumentSHA256))
		fmt.Fprintf(&report, "- Rationale: %s\n", markdownCell(acceptance.Rationale))
		fmt.Fprintf(&report, "- Containment: %s\n", markdownCell(acceptance.Containment))
		fmt.Fprintf(&report, "- Rollback plan: %s\n", markdownCell(acceptance.RollbackPlan))
		fmt.Fprintln(&report)
	}

	fmt.Fprintln(&report, "## Blockers")
	fmt.Fprintln(&report)
	if len(validation.blockers) == 0 {
		fmt.Fprintln(&report, "None.")
	} else {
		for _, blocker := range validation.blockers {
			fmt.Fprintf(&report, "- %s\n", markdownCell(blocker))
		}
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Validation Errors")
	fmt.Fprintln(&report)
	if len(validation.errors) == 0 {
		fmt.Fprintln(&report, "None.")
	} else {
		for _, validationError := range validation.errors {
			fmt.Fprintf(&report, "- %s\n", markdownCell(validationError))
		}
	}
	return report.String()
}

func gateStatusLabel(status string) string {
	switch status {
	case statusPending:
		return "PENDING"
	case statusPassed:
		return "PASS"
	case statusRiskAccepted:
		return "RISK-ACCEPTED"
	default:
		return strings.ToUpper(status)
	}
}

func markdownCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\\", "\\\\",
		"|", "\\|",
		"`", "\\`",
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
	)
	return replacer.Replace(value)
}

func redactEvidenceURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL redacted]"
	}
	hadSensitiveParts := parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != ""
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	redacted := parsed.String()
	if hadSensitiveParts {
		redacted += "?[redacted]"
	}
	return redacted
}

func printValidation(w io.Writer, manifest Manifest, validation validator) {
	fmt.Fprintf(w, "candidate: tag=%s digest=%s app=%s chart=%s\n",
		manifest.Candidate.Tag, manifest.Candidate.Digest, manifest.Candidate.AppVersion, manifest.Candidate.ChartVersion)
	definitions := make(map[string]gateDefinition, len(gateDefinitions))
	for _, definition := range gateDefinitions {
		definitions[definition.ID] = definition
	}
	gates := append([]Gate(nil), manifest.Gates...)
	sort.SliceStable(gates, func(i, j int) bool {
		return gateOrder(gates[i].ID) < gateOrder(gates[j].ID)
	})
	for _, gate := range gates {
		definition := definitions[gate.ID]
		label := strings.ToUpper(gate.Status)
		switch {
		case gate.Status == statusPending:
			label = "PENDING"
		case gate.Status == statusPassed:
			label = "PASS"
		case gate.Status == statusRiskAccepted:
			label = "RISK-ACCEPTED"
		}
		fmt.Fprintf(w, "%-13s %-42s expires=%s owner=%s\n", label, definition.ID, gate.ExpiresAt, gate.Owner)
	}
	for _, issue := range validation.blockers {
		fmt.Fprintf(w, "BLOCK: %s\n", issue)
	}
	for _, issue := range validation.errors {
		fmt.Fprintf(w, "INVALID: %s\n", issue)
	}
	switch {
	case len(validation.errors) > 0:
		fmt.Fprintf(w, "release gate: INVALID (%d errors, %d blockers)\n", len(validation.errors), len(validation.blockers))
	case len(validation.blockers) > 0:
		fmt.Fprintf(w, "release gate: BLOCKED (%d blockers)\n", len(validation.blockers))
	default:
		fmt.Fprintln(w, "release gate: PASS")
	}
}

func gateOrder(id string) int {
	for index, definition := range gateDefinitions {
		if definition.ID == id {
			return index
		}
	}
	return len(gateDefinitions)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writeFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".releasegate-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
