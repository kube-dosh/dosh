package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const schemaVersion = "dosh-release-baseline-v1"

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, fmt.Errorf("%s failed with exit code %d: %s", name, exitError.ExitCode(), strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, fmt.Errorf("run %s: %w", name, err)
	}
	return output, nil
}

type Options struct {
	Namespace   string
	Release     string
	KubeContext string
}

type Baseline struct {
	Schema         string             `json:"schema"`
	CapturedAt     string             `json:"capturedAt"`
	Hostname       string             `json:"hostname"`
	KubeContext    string             `json:"kubeContext"`
	ServerVersion  string             `json:"serverVersion"`
	Namespace      string             `json:"namespace"`
	Release        string             `json:"release"`
	ReadOnly       bool               `json:"readOnly"`
	ValuesSHA256   string             `json:"valuesSha256"`
	Helm           HelmSnapshot       `json:"helm"`
	Deployment     DeploymentSnapshot `json:"deployment"`
	Service        ServiceSnapshot    `json:"service"`
	PodDisruptions []PDBSnapshot      `json:"podDisruptionBudgets"`
	Pods           []PodSnapshot      `json:"pods"`
	Secrets        []SecretSnapshot   `json:"secrets"`
	CommandAudit   []string           `json:"commandAudit"`
}

type HelmSnapshot struct {
	Revision   string              `json:"revision"`
	Updated    string              `json:"updated"`
	Status     string              `json:"status"`
	Chart      string              `json:"chart"`
	AppVersion string              `json:"appVersion"`
	History    []HelmHistoryRecord `json:"history"`
}

type HelmHistoryRecord struct {
	Revision    int    `json:"revision"`
	Updated     string `json:"updated"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	AppVersion  string `json:"appVersion"`
	Description string `json:"description,omitempty"`
}

type DeploymentSnapshot struct {
	Name                string `json:"name"`
	Image               string `json:"image"`
	Strategy            string `json:"strategy"`
	ServiceAccountName  string `json:"serviceAccountName"`
	Generation          int64  `json:"generation"`
	ObservedGeneration  int64  `json:"observedGeneration"`
	Replicas            int32  `json:"replicas"`
	UpdatedReplicas     int32  `json:"updatedReplicas"`
	ReadyReplicas       int32  `json:"readyReplicas"`
	AvailableReplicas   int32  `json:"availableReplicas"`
	UnavailableReplicas int32  `json:"unavailableReplicas"`
}

type ServiceSnapshot struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	ClusterIP string        `json:"clusterIP"`
	Ports     []ServicePort `json:"ports"`
}

type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort"`
	NodePort   int32  `json:"nodePort,omitempty"`
}

type PDBSnapshot struct {
	Name               string `json:"name"`
	MinAvailable       string `json:"minAvailable,omitempty"`
	MaxUnavailable     string `json:"maxUnavailable,omitempty"`
	CurrentHealthy     int32  `json:"currentHealthy"`
	DesiredHealthy     int32  `json:"desiredHealthy"`
	DisruptionsAllowed int32  `json:"disruptionsAllowed"`
	ExpectedPods       int32  `json:"expectedPods"`
}

type PodSnapshot struct {
	Name     string   `json:"name"`
	NodeName string   `json:"nodeName"`
	Phase    string   `json:"phase"`
	Ready    bool     `json:"ready"`
	Restarts int32    `json:"restarts"`
	Images   []string `json:"images"`
}

type SecretSnapshot struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Keys []string `json:"keys"`
}

type helmListEntry struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   string `json:"revision"`
	Updated    string `json:"updated"`
	Status     string `json:"status"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version"`
}

type kubeVersion struct {
	ServerVersion struct {
		GitVersion string `json:"gitVersion"`
	} `json:"serverVersion"`
}

type capturer struct {
	runner  commandRunner
	options Options
	audit   []string
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
	case "capture":
		return runCapture(args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
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
  releasebaseline capture --output <file> [--namespace dosh] [--release dosh] [--kube-context <name>] [--force]
  releasebaseline verify --baseline <file> [--kube-context <name>]
  releasebaseline help

The capture is read-only. It records Helm revision/status/chart, values SHA-256 (not values),
Deployment image and readiness, Service NodePort, PDB status, Pod placement, Secret names/keys and
the exact read-only command audit. Secret data is never emitted.
`)
}

func runCapture(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("capture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "output JSON file")
	namespace := flags.String("namespace", "dosh", "release namespace")
	release := flags.String("release", "dosh", "Helm release and Deployment name")
	kubeContext := flags.String("kube-context", "", "optional kubeconfig context override")
	force := flags.Bool("force", false, "overwrite existing baseline and checksum")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *output == "" {
		fmt.Fprintln(stderr, "error: capture requires --output")
		return 1
	}
	options := Options{Namespace: *namespace, Release: *release, KubeContext: *kubeContext}
	baseline, err := capture(context.Background(), execRunner{}, options, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: encode baseline: %v\n", err)
		return 1
	}
	data = append(data, '\n')
	if err := writeBaseline(*output, data, *force); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	digest := sha256.Sum256(data)
	fmt.Fprintf(stdout, "release baseline captured: %s\n", *output)
	fmt.Fprintf(stdout, "candidate_revision=%s chart=%s app=%s image=%s\n",
		baseline.Helm.Revision, baseline.Helm.Chart, baseline.Helm.AppVersion, baseline.Deployment.Image)
	fmt.Fprintf(stdout, "sha256=%s\n", hex.EncodeToString(digest[:]))
	return 0
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baselinePath := flags.String("baseline", "", "baseline JSON file; a sibling .sha256 file is verified when present")
	kubeContext := flags.String("kube-context", "", "optional kubeconfig context override")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *baselinePath == "" {
		fmt.Fprintln(stderr, "error: verify requires --baseline")
		return 1
	}
	expected, err := readBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	actual, err := capture(context.Background(), execRunner{}, Options{
		Namespace:   expected.Namespace,
		Release:     expected.Release,
		KubeContext: *kubeContext,
	}, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "error: capture current release state: %v\n", err)
		return 1
	}
	drift := verifyBaseline(expected, actual)
	for _, difference := range drift {
		fmt.Fprintf(stdout, "DRIFT: %s\n", difference)
	}
	if len(drift) != 0 {
		fmt.Fprintf(stdout, "baseline verification: DRIFT (%d differences)\n", len(drift))
		return 2
	}
	fmt.Fprintf(stdout, "baseline verification: PASS (namespace=%s release=%s revision=%s image=%s)\n",
		actual.Namespace, actual.Release, actual.Helm.Revision, actual.Deployment.Image)
	return 0
}

func readBaseline(path string) (Baseline, error) {
	var baseline Baseline
	data, err := os.ReadFile(path)
	if err != nil {
		return baseline, fmt.Errorf("read baseline: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&baseline); err != nil {
		return baseline, fmt.Errorf("decode baseline: %w", err)
	}
	checksumPath := path + ".sha256"
	if checksumData, err := os.ReadFile(checksumPath); err == nil {
		fields := strings.Fields(string(checksumData))
		if len(fields) < 1 || len(fields[0]) != 64 {
			return baseline, fmt.Errorf("invalid checksum file %s", checksumPath)
		}
		digest := sha256.Sum256(data)
		if !strings.EqualFold(fields[0], hex.EncodeToString(digest[:])) {
			return baseline, fmt.Errorf("baseline SHA-256 does not match %s", checksumPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return baseline, fmt.Errorf("read baseline checksum: %w", err)
	}
	return baseline, nil
}

func verifyBaseline(expected, actual Baseline) []string {
	var drift []string
	add := func(format string, args ...any) { drift = append(drift, fmt.Sprintf(format, args...)) }
	if expected.Schema != schemaVersion || actual.Schema != schemaVersion {
		add("schema expected=%q actual=%q", expected.Schema, actual.Schema)
	}
	if expected.Namespace != actual.Namespace || expected.Release != actual.Release {
		add("release identity expected=%s/%s actual=%s/%s", expected.Namespace, expected.Release, actual.Namespace, actual.Release)
	}
	if expected.ValuesSHA256 != actual.ValuesSHA256 {
		add("Helm values SHA-256 changed: expected=%s actual=%s", expected.ValuesSHA256, actual.ValuesSHA256)
	}
	if expected.Helm.Revision != actual.Helm.Revision ||
		expected.Helm.Status != actual.Helm.Status ||
		expected.Helm.Chart != actual.Helm.Chart ||
		expected.Helm.AppVersion != actual.Helm.AppVersion {
		add("Helm state changed: expected revision=%s status=%s chart=%s app=%s actual revision=%s status=%s chart=%s app=%s",
			expected.Helm.Revision, expected.Helm.Status, expected.Helm.Chart, expected.Helm.AppVersion,
			actual.Helm.Revision, actual.Helm.Status, actual.Helm.Chart, actual.Helm.AppVersion)
	}
	if expected.Deployment != actual.Deployment {
		add("Deployment changed: expected=%+v actual=%+v", expected.Deployment, actual.Deployment)
	}
	if expected.Service.Name != actual.Service.Name ||
		expected.Service.Type != actual.Service.Type ||
		expected.Service.ClusterIP != actual.Service.ClusterIP ||
		!sameServicePorts(expected.Service.Ports, actual.Service.Ports) {
		add("Service changed: expected=%+v actual=%+v", expected.Service, actual.Service)
	}
	if expectedPDB, actualPDB := pdbSpecMap(expected.PodDisruptions), pdbSpecMap(actual.PodDisruptions); !samePDBs(expectedPDB, actualPDB) {
		add("PDB specification changed: expected=%+v actual=%+v", expectedPDB, actualPDB)
	}
	if !sameSecrets(expected.Secrets, actual.Secrets) {
		add("Secret names/types/keys changed")
	}
	expectedReady := readyPodCount(expected.Pods)
	actualReady := readyPodCount(actual.Pods)
	if len(expected.Pods) != len(actual.Pods) || expectedReady != actualReady {
		add("Pod count/readiness changed: expected pods=%d ready=%d actual pods=%d ready=%d",
			len(expected.Pods), expectedReady, len(actual.Pods), actualReady)
	}
	if len(actual.Pods) == 0 || actualReady != len(actual.Pods) {
		add("current release is not fully ready: pods=%d ready=%d", len(actual.Pods), actualReady)
	}
	if actual.Deployment.Replicas <= 0 || actual.Deployment.ReadyReplicas != actual.Deployment.Replicas {
		add("current Deployment is not fully ready: replicas=%d ready=%d", actual.Deployment.Replicas, actual.Deployment.ReadyReplicas)
	}
	for _, pdb := range actual.PodDisruptions {
		if pdb.DisruptionsAllowed < 1 {
			add("PDB %s allows no voluntary disruption", pdb.Name)
		}
	}
	return drift
}

func capture(ctx context.Context, runner commandRunner, options Options, now time.Time) (Baseline, error) {
	var baseline Baseline
	if !validResourceName(options.Namespace) {
		return baseline, fmt.Errorf("invalid namespace %q", options.Namespace)
	}
	if !validResourceName(options.Release) {
		return baseline, fmt.Errorf("invalid release name %q", options.Release)
	}
	c := &capturer{runner: runner, options: options}

	helmList, err := c.run(ctx, "helm", "-n", options.Namespace, "list", "-f", "^"+regexp.QuoteMeta(options.Release)+"$", "-o", "json")
	if err != nil {
		return baseline, err
	}
	var releases []helmListEntry
	if err := json.Unmarshal(helmList, &releases); err != nil {
		return baseline, fmt.Errorf("decode helm list: %w", err)
	}
	if len(releases) != 1 {
		return baseline, fmt.Errorf("helm list returned %d releases, want exactly 1", len(releases))
	}
	release := releases[0]
	if release.Name != options.Release || release.Namespace != options.Namespace {
		return baseline, fmt.Errorf("helm release identity mismatch: %s/%s", release.Namespace, release.Name)
	}

	historyOutput, err := c.run(ctx, "helm", "-n", options.Namespace, "history", options.Release, "-o", "json")
	if err != nil {
		return baseline, err
	}
	var history []HelmHistoryRecord
	if err := json.Unmarshal(historyOutput, &history); err != nil {
		return baseline, fmt.Errorf("decode helm history: %w", err)
	}

	values, err := c.run(ctx, "helm", "-n", options.Namespace, "get", "values", options.Release, "-o", "json")
	if err != nil {
		return baseline, err
	}
	valuesHash := sha256.Sum256(values)

	deploymentOutput, err := c.run(ctx, "kubectl", "-n", options.Namespace, "get", "deployment", options.Release, "-o", "json")
	if err != nil {
		return baseline, err
	}
	var deployment appsv1.Deployment
	if err := json.Unmarshal(deploymentOutput, &deployment); err != nil {
		return baseline, fmt.Errorf("decode deployment: %w", err)
	}
	selector, err := selectorFor(&deployment)
	if err != nil {
		return baseline, err
	}

	serviceOutput, err := c.run(ctx, "kubectl", "-n", options.Namespace, "get", "service", options.Release, "-o", "json")
	if err != nil {
		return baseline, err
	}
	var service corev1.Service
	if err := json.Unmarshal(serviceOutput, &service); err != nil {
		return baseline, fmt.Errorf("decode service: %w", err)
	}

	pdbOutput, err := c.run(ctx, "kubectl", "-n", options.Namespace, "get", "pdb", "-l", selector, "-o", "json")
	if err != nil {
		return baseline, err
	}
	var pdbs policyv1.PodDisruptionBudgetList
	if err := json.Unmarshal(pdbOutput, &pdbs); err != nil {
		return baseline, fmt.Errorf("decode PDB list: %w", err)
	}

	podsOutput, err := c.run(ctx, "kubectl", "-n", options.Namespace, "get", "pods", "-l", selector, "-o", "json")
	if err != nil {
		return baseline, err
	}
	var pods corev1.PodList
	if err := json.Unmarshal(podsOutput, &pods); err != nil {
		return baseline, fmt.Errorf("decode pod list: %w", err)
	}

	secretsOutput, err := c.run(ctx, "kubectl", "-n", options.Namespace, "get", "secrets", "-o", "json")
	if err != nil {
		return baseline, err
	}
	var secrets corev1.SecretList
	if err := json.Unmarshal(secretsOutput, &secrets); err != nil {
		return baseline, fmt.Errorf("decode secret list: %w", err)
	}

	contextName := options.KubeContext
	if contextName == "" {
		contextOutput, err := c.run(ctx, "kubectl", "config", "current-context")
		if err != nil {
			return baseline, err
		}
		contextName = strings.TrimSpace(string(contextOutput))
	}
	versionOutput, err := c.run(ctx, "kubectl", "version", "-o", "json")
	if err != nil {
		return baseline, err
	}
	var version kubeVersion
	if err := json.Unmarshal(versionOutput, &version); err != nil {
		return baseline, fmt.Errorf("decode kubectl version: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return baseline, fmt.Errorf("read hostname: %w", err)
	}

	baseline = Baseline{
		Schema:        schemaVersion,
		CapturedAt:    now.UTC().Format(time.RFC3339),
		Hostname:      hostname,
		KubeContext:   contextName,
		ServerVersion: version.ServerVersion.GitVersion,
		Namespace:     options.Namespace,
		Release:       options.Release,
		ReadOnly:      true,
		ValuesSHA256:  "sha256:" + hex.EncodeToString(valuesHash[:]),
		Helm: HelmSnapshot{
			Revision:   release.Revision,
			Updated:    release.Updated,
			Status:     release.Status,
			Chart:      release.Chart,
			AppVersion: release.AppVersion,
			History:    history,
		},
		Deployment:     deploymentSnapshot(&deployment),
		Service:        serviceSnapshot(&service),
		PodDisruptions: pdbSnapshots(pdbs.Items),
		Pods:           podSnapshots(pods.Items),
		Secrets:        secretSnapshots(secrets.Items),
		CommandAudit:   append([]string(nil), c.audit...),
	}
	return baseline, nil
}

func (c *capturer) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandArgs := append([]string(nil), args...)
	auditArgs := append([]string{name}, commandArgs...)
	if c.options.KubeContext != "" {
		switch name {
		case "helm":
			commandArgs = append([]string{"--kube-context", c.options.KubeContext}, commandArgs...)
		case "kubectl":
			commandArgs = append([]string{"--context", c.options.KubeContext}, commandArgs...)
		}
		auditArgs = append([]string{name}, commandArgs...)
	}
	c.audit = append(c.audit, strings.Join(auditArgs, " "))
	return c.runner.Run(ctx, name, commandArgs...)
}

func validResourceName(name string) bool {
	return len(name) <= 63 && resourceNamePattern.MatchString(name)
}

func selectorFor(deployment *appsv1.Deployment) (string, error) {
	if deployment.Spec.Selector == nil || len(deployment.Spec.Selector.MatchLabels) == 0 {
		return "", errors.New("deployment has no matchLabels selector")
	}
	if len(deployment.Spec.Selector.MatchExpressions) != 0 {
		return "", errors.New("deployment selector expressions are not supported in baseline capture")
	}
	keys := make([]string, 0, len(deployment.Spec.Selector.MatchLabels))
	for key := range deployment.Spec.Selector.MatchLabels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+deployment.Spec.Selector.MatchLabels[key])
	}
	return strings.Join(parts, ","), nil
}

func deploymentSnapshot(deployment *appsv1.Deployment) DeploymentSnapshot {
	image := ""
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		image = deployment.Spec.Template.Spec.Containers[0].Image
	}
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	return DeploymentSnapshot{
		Name:                deployment.Name,
		Image:               image,
		Strategy:            string(deployment.Spec.Strategy.Type),
		ServiceAccountName:  deployment.Spec.Template.Spec.ServiceAccountName,
		Generation:          deployment.Generation,
		ObservedGeneration:  deployment.Status.ObservedGeneration,
		Replicas:            replicas,
		UpdatedReplicas:     deployment.Status.UpdatedReplicas,
		ReadyReplicas:       deployment.Status.ReadyReplicas,
		AvailableReplicas:   deployment.Status.AvailableReplicas,
		UnavailableReplicas: deployment.Status.UnavailableReplicas,
	}
}

func serviceSnapshot(service *corev1.Service) ServiceSnapshot {
	ports := make([]ServicePort, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, ServicePort{
			Name:       port.Name,
			Protocol:   string(port.Protocol),
			Port:       port.Port,
			TargetPort: port.TargetPort.String(),
			NodePort:   port.NodePort,
		})
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
	return ServiceSnapshot{Name: service.Name, Type: string(service.Spec.Type), ClusterIP: service.Spec.ClusterIP, Ports: ports}
}

func pdbSnapshots(items []policyv1.PodDisruptionBudget) []PDBSnapshot {
	result := make([]PDBSnapshot, 0, len(items))
	for _, item := range items {
		result = append(result, PDBSnapshot{
			Name:               item.Name,
			MinAvailable:       intOrString(item.Spec.MinAvailable),
			MaxUnavailable:     intOrString(item.Spec.MaxUnavailable),
			CurrentHealthy:     item.Status.CurrentHealthy,
			DesiredHealthy:     item.Status.DesiredHealthy,
			DisruptionsAllowed: item.Status.DisruptionsAllowed,
			ExpectedPods:       item.Status.ExpectedPods,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func intOrString(value *intstr.IntOrString) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func pdbSpecMap(items []PDBSnapshot) map[string]PDBSnapshot {
	result := make(map[string]PDBSnapshot, len(items))
	for _, item := range items {
		result[item.Name] = PDBSnapshot{
			Name:           item.Name,
			MinAvailable:   item.MinAvailable,
			MaxUnavailable: item.MaxUnavailable,
		}
	}
	return result
}

func samePDBs(expected, actual map[string]PDBSnapshot) bool {
	if len(expected) != len(actual) {
		return false
	}
	for name, item := range expected {
		if other, ok := actual[name]; !ok || item != other {
			return false
		}
	}
	return true
}

func sameServicePorts(expected, actual []ServicePort) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return false
		}
	}
	return true
}

func sameSecrets(expected, actual []SecretSnapshot) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if expected[index].Name != actual[index].Name ||
			expected[index].Type != actual[index].Type ||
			strings.Join(expected[index].Keys, "\x00") != strings.Join(actual[index].Keys, "\x00") {
			return false
		}
	}
	return true
}

func readyPodCount(pods []PodSnapshot) int {
	ready := 0
	for _, pod := range pods {
		if pod.Ready {
			ready++
		}
	}
	return ready
}

func podSnapshots(items []corev1.Pod) []PodSnapshot {
	result := make([]PodSnapshot, 0, len(items))
	for _, pod := range items {
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		var restarts int32
		for _, status := range pod.Status.ContainerStatuses {
			restarts += status.RestartCount
		}
		images := make([]string, 0, len(pod.Spec.Containers))
		for _, container := range pod.Spec.Containers {
			images = append(images, container.Image)
		}
		sort.Strings(images)
		result = append(result, PodSnapshot{
			Name:     pod.Name,
			NodeName: pod.Spec.NodeName,
			Phase:    string(pod.Status.Phase),
			Ready:    ready,
			Restarts: restarts,
			Images:   images,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func secretSnapshots(items []corev1.Secret) []SecretSnapshot {
	result := make([]SecretSnapshot, 0, len(items))
	for _, secret := range items {
		keys := make([]string, 0, len(secret.Data))
		for key := range secret.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result = append(result, SecretSnapshot{Name: secret.Name, Type: string(secret.Type), Keys: keys})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func writeBaseline(path string, data []byte, force bool) error {
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
		return fmt.Errorf("write baseline: %w", err)
	}
	digest := sha256.Sum256(data)
	checksum := []byte(hex.EncodeToString(digest[:]) + "  " + filepath.Base(path) + "\n")
	if err := writeFileAtomic(checksumPath, checksum); err != nil {
		return fmt.Errorf("write checksum: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".releasebaseline-*")
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
