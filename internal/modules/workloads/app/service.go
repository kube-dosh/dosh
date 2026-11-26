package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dosh/internal/domain"
)

const maxNonStreamLogBytes int64 = 1 << 20
const maxWorkloadLogStreams = 20

type Repository interface {
	ListWorkloads(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error)
	ListWorkloadsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.WorkloadSummary], error)
	GetWorkload(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error)
	CreateWorkload(ctx context.Context, spec domain.WorkloadCreateSpec, dryRun bool) (domain.WorkloadSummary, error)
	PreviewWorkload(ctx context.Context, spec domain.WorkloadCreateSpec) (string, error)
	ScaleWorkload(ctx context.Context, kind, namespace, name string, replicas int32, expectedResourceVersion string) error
	RestartWorkload(ctx context.Context, kind, namespace, name string) error
	UpdateWorkloadImage(ctx context.Context, kind, namespace, name string, update domain.WorkloadImageUpdate) error
	UpdateWorkloadContainerConfig(ctx context.Context, kind, namespace, name string, update domain.WorkloadContainerConfigUpdate) error
	SetCronJobSuspend(ctx context.Context, namespace, name string, update domain.CronJobSuspendUpdate) error
	UpdateWorkloadRollout(ctx context.Context, kind, namespace, name string, update domain.WorkloadRolloutUpdate) error
	RunCronJobOnce(ctx context.Context, namespace, name string, input domain.CronJobRunInput) (domain.WorkloadSummary, error)
	ListWorkloadRevisions(ctx context.Context, kind, namespace, name string) ([]domain.WorkloadRevision, error)
	RollbackWorkload(ctx context.Context, kind, namespace, name string, revision int64) error
	DeleteWorkload(ctx context.Context, kind, namespace, name string, propagation domain.DeletionPropagation) error
	ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error)
	ListPodsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PodSummary], error)
	GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error)
	DeletePod(ctx context.Context, namespace, name string) error
	StreamPodLogs(ctx context.Context, namespace, name, container string, follow, previous bool, tailLines int64, timestamps bool, sinceSeconds int64, limitBytes int64) (io.ReadCloser, error)
	OpenPodTerminal(ctx context.Context, namespace, name string, options domain.PodTerminalOptions) (domain.PodTerminalSession, error)
	AddPodEphemeralContainer(ctx context.Context, namespace, name string, request domain.PodEphemeralContainerRequest, containerName string) (string, error)
	ResolvePortForwardTarget(ctx context.Context, target domain.PortForwardTarget) (domain.PortForwardPod, error)
	OpenPodPortForward(ctx context.Context, namespace, name string, port int32) (string, io.Closer, error)
}

// PortForwardBackendLister is optional so focused repositories that only test
// Pod port-forwarding do not need to implement Service selector discovery.
type PortForwardBackendLister interface {
	ListPortForwardBackendPods(ctx context.Context, namespace, serviceName string) ([]domain.PodSummary, error)
}

type Service struct {
	repository     Repository
	authorizer     domain.Authorizer
	auditor        domain.Auditor
	registry       ImageRegistryResolver
	terminal       domain.TerminalLimits
	terminalMu     sync.Mutex
	active         map[string]int
	forward        domain.PortForwardLimits
	forwardMu      sync.Mutex
	memoryForwards *memoryPortForwardStore
}

func New(repository Repository, authorizer domain.Authorizer, auditor domain.Auditor, limits ...domain.TerminalLimits) *Service {
	terminal := domain.DefaultTerminalLimits()
	if len(limits) > 0 {
		terminal = limits[0]
	}
	return &Service{
		repository: repository, authorizer: authorizer, auditor: auditor,
		terminal: terminal, active: make(map[string]int),
		forward: domain.DefaultPortForwardLimits(), memoryForwards: newMemoryPortForwardStore(),
	}
}

// ImageRegistryResolver validates that an image belongs to a configured
// registry and returns the optional Pod-level imagePullSecrets name.
type ImageRegistryResolver interface {
	ResolvePullSecret(ctx context.Context, actor domain.Actor, registryID, image string) (string, error)
}

// SetImageRegistryResolver wires the registry module during composition. It is
// optional so focused tests and installations without registries still work.
func (s *Service) SetImageRegistryResolver(resolver ImageRegistryResolver) {
	s.registry = resolver
}

func (s *Service) applyImagePullSecret(ctx context.Context, actor domain.Actor, registryID, image string, secrets *[]string) error {
	registryID = strings.TrimSpace(registryID)
	if registryID == "" {
		return nil
	}
	if s.registry == nil {
		return fmt.Errorf("%w: container registry module is unavailable", domain.ErrUnsupported)
	}
	secret, err := s.registry.ResolvePullSecret(ctx, actor, registryID, image)
	if err != nil {
		return err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	for _, existing := range *secrets {
		if existing == secret {
			return nil
		}
	}
	*secrets = append(*secrets, secret)
	return nil
}

func (s *Service) applyWorkloadImagePullSecrets(ctx context.Context, actor domain.Actor, spec *domain.WorkloadCreateSpec) error {
	all := append(append([]domain.WorkloadContainerSpec{}, spec.Containers...), spec.InitContainers...)
	for _, container := range all {
		if err := s.applyImagePullSecret(ctx, actor, container.RegistryID, container.Image, &spec.ImagePullSecrets); err != nil {
			return err
		}
	}
	return nil
}

// SetPortForwardLimits applies startup configuration to the tunnel service. It
// must be called during composition before requests are served.
func (s *Service) SetPortForwardLimits(limits domain.PortForwardLimits) {
	if limits.MaxDuration <= 0 {
		limits.MaxDuration = domain.DefaultPortForwardLimits().MaxDuration
	}
	if limits.MaxSessionsPerUser <= 0 {
		limits.MaxSessionsPerUser = domain.DefaultPortForwardLimits().MaxSessionsPerUser
	}
	if limits.MaxPortsPerSession <= 0 {
		limits.MaxPortsPerSession = domain.DefaultPortForwardLimits().MaxPortsPerSession
	}
	s.forwardMu.Lock()
	s.forward = limits
	s.forwardMu.Unlock()
}

var supportedKinds = map[string]bool{
	"deployment": true, "statefulset": true, "daemonset": true, "job": true, "cronjob": true,
}

func ValidateKind(kind string) error {
	if !supportedKinds[strings.ToLower(kind)] {
		return fmt.Errorf("%w: workload kind %q", domain.ErrUnsupported, kind)
	}
	return nil
}

func (s *Service) List(ctx context.Context, query domain.ListQuery) ([]domain.WorkloadSummary, error) {
	if query.Kind != "" {
		if err := ValidateKind(query.Kind); err != nil {
			return nil, err
		}
	}
	return s.repository.ListWorkloads(ctx, query)
}

func (s *Service) ListPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.WorkloadSummary], error) {
	if query.Kind != "" {
		if err := ValidateKind(query.Kind); err != nil {
			return domain.PageResult[domain.WorkloadSummary]{}, err
		}
	}
	return s.repository.ListWorkloadsPage(ctx, query, page)
}

func (s *Service) Get(ctx context.Context, kind, namespace, name string) (domain.WorkloadDetail, error) {
	if err := ValidateKind(kind); err != nil {
		return domain.WorkloadDetail{}, err
	}
	return s.repository.GetWorkload(ctx, kind, namespace, name)
}

func (s *Service) Scale(ctx context.Context, actor domain.Actor, kind, namespace, name string, replicas int32, resourceVersion string) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "scale", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "scale")
	if err := s.authorize(ctx, actor, "scale", kind, namespace, name, access); err != nil {
		return err
	}
	if strings.EqualFold(kind, "daemonset") || strings.EqualFold(kind, "job") || strings.EqualFold(kind, "cronjob") {
		err := fmt.Errorf("%w: scale is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "scale", kind, namespace, name, err)
		return err
	}
	err := s.repository.ScaleWorkload(ctx, kind, namespace, name, replicas, resourceVersion)
	s.audit(ctx, actor, "scale", kind, namespace, name, err)
	return err
}

func (s *Service) Restart(ctx context.Context, actor domain.Actor, kind, namespace, name string) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "restart", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "patch", namespace, name, "")
	if err := s.authorize(ctx, actor, "restart", kind, namespace, name, access); err != nil {
		return err
	}
	if strings.EqualFold(kind, "job") || strings.EqualFold(kind, "cronjob") {
		err := fmt.Errorf("%w: restart is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "restart", kind, namespace, name, err)
		return err
	}
	err := s.repository.RestartWorkload(ctx, kind, namespace, name)
	s.audit(ctx, actor, "restart", kind, namespace, name, err)
	return err
}

func (s *Service) Delete(ctx context.Context, actor domain.Actor, kind, namespace, name, propagation string) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "delete", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "delete", namespace, name, "")
	if err := s.authorize(ctx, actor, "delete", kind, namespace, name, access); err != nil {
		return err
	}
	policy := domain.DeletePropagationBackground
	switch propagation {
	case "", "Background":
	case "Foreground":
		policy = domain.DeletePropagationForeground
	case "Orphan":
		policy = domain.DeletePropagationOrphan
	default:
		err := fmt.Errorf("%w: unsupported deletion propagation %q", domain.ErrInvalid, propagation)
		s.audit(ctx, actor, "delete", kind, namespace, name, err)
		return err
	}
	err := s.repository.DeleteWorkload(ctx, kind, namespace, name, policy)
	s.audit(ctx, actor, "delete", kind, namespace, name, err)
	return err
}

func (s *Service) Revisions(ctx context.Context, kind, namespace, name string) ([]domain.WorkloadRevision, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	if !strings.EqualFold(kind, "deployment") && !strings.EqualFold(kind, "statefulset") {
		return nil, fmt.Errorf("%w: revisions are not supported for %s", domain.ErrUnsupported, kind)
	}
	return s.repository.ListWorkloadRevisions(ctx, kind, namespace, name)
}

func (s *Service) Rollback(ctx context.Context, actor domain.Actor, kind, namespace, name string, revision int64) error {
	if err := ValidateKind(kind); err != nil {
		s.audit(ctx, actor, "rollback", kind, namespace, name, err)
		return err
	}
	if !strings.EqualFold(kind, "deployment") && !strings.EqualFold(kind, "statefulset") {
		err := fmt.Errorf("%w: rollback is not supported for %s", domain.ErrUnsupported, kind)
		s.audit(ctx, actor, "rollback", kind, namespace, name, err)
		return err
	}
	if revision < 1 {
		err := fmt.Errorf("%w: revision must be positive", domain.ErrInvalid)
		s.audit(ctx, actor, "rollback", kind, namespace, name, err)
		return err
	}
	access := workloadAccess(kind, "update", namespace, name, "")
	if err := s.authorize(ctx, actor, "rollback", kind, namespace, name, access); err != nil {
		return err
	}
	err := s.repository.RollbackWorkload(ctx, kind, namespace, name, revision)
	s.audit(ctx, actor, "rollback", kind, namespace, name, err)
	return err
}

func (s *Service) ListPods(ctx context.Context, query domain.ListQuery) ([]domain.PodSummary, error) {
	return s.repository.ListPods(ctx, query)
}

func (s *Service) ListPodsPage(ctx context.Context, query domain.ListQuery, page domain.PageRequest) (domain.PageResult[domain.PodSummary], error) {
	return s.repository.ListPodsPage(ctx, query, page)
}

func (s *Service) GetPod(ctx context.Context, namespace, name string) (domain.PodDetail, error) {
	return s.repository.GetPod(ctx, namespace, name)
}

func (s *Service) DeletePod(ctx context.Context, actor domain.Actor, namespace, name string) error {
	access := domain.Access{Verb: "delete", Resource: "pods", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, "delete-pod", "pod", namespace, name, err)
		return err
	}
	err := s.repository.DeletePod(ctx, namespace, name)
	s.audit(ctx, actor, "delete-pod", "pod", namespace, name, err)
	return err
}

func (s *Service) StreamLogs(ctx context.Context, actor domain.Actor, namespace, name, container string, follow, previous bool, tailLines int64, timestamps bool, sinceSeconds int64) (io.ReadCloser, error) {
	if tailLines < 0 {
		return nil, fmt.Errorf("%w: tailLines must not be negative", domain.ErrInvalid)
	}
	if tailLines > 5000 {
		tailLines = 5000
	}
	if sinceSeconds < 0 {
		return nil, fmt.Errorf("%w: sinceSeconds must not be negative", domain.ErrInvalid)
	}
	if sinceSeconds > 7*24*60*60 {
		sinceSeconds = 7 * 24 * 60 * 60
	}
	limitBytes := int64(0)
	if !follow {
		limitBytes = maxNonStreamLogBytes
	}
	access := domain.Access{Verb: "get", Resource: "pods", Subresource: "log", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, "stream-pod-logs", "pod", namespace, name, err)
		return nil, err
	}
	stream, err := s.repository.StreamPodLogs(ctx, namespace, name, container, follow, previous, tailLines, timestamps, sinceSeconds, limitBytes)
	s.audit(ctx, actor, "stream-pod-logs", "pod", namespace, name, err)
	return stream, err
}

// StreamWorkloadLogs opens a bounded set of Pod/container log streams for one
// workload. Each Pod is authorized independently; a denial or unavailable Pod
// is reported as a warning so the remaining streams can still be used.
func (s *Service) StreamWorkloadLogs(ctx context.Context, actor domain.Actor, kind, namespace, name, container string, follow, previous bool, tailLines int64, timestamps bool, sinceSeconds int64) (domain.WorkloadLogStreams, error) {
	if err := ValidateKind(kind); err != nil {
		return domain.WorkloadLogStreams{}, err
	}
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domain.WorkloadLogStreams{}, fmt.Errorf("%w: namespace and workload name are required", domain.ErrInvalid)
	}
	if tailLines < 0 {
		return domain.WorkloadLogStreams{}, fmt.Errorf("%w: tailLines must not be negative", domain.ErrInvalid)
	}
	if tailLines > 5000 {
		tailLines = 5000
	}
	if sinceSeconds < 0 {
		return domain.WorkloadLogStreams{}, fmt.Errorf("%w: sinceSeconds must not be negative", domain.ErrInvalid)
	}
	if sinceSeconds > 7*24*60*60 {
		sinceSeconds = 7 * 24 * 60 * 60
	}
	workload, err := s.repository.GetWorkload(ctx, kind, namespace, name)
	if err != nil {
		s.audit(ctx, actor, "stream-workload-logs", kind, namespace, name, err)
		return domain.WorkloadLogStreams{}, err
	}
	containers := make([]string, 0, len(workload.Containers)+len(workload.InitContainers))
	for _, item := range workload.Containers {
		if item.Name != "" {
			containers = append(containers, item.Name)
		}
	}
	for _, item := range workload.InitContainers {
		if item.Name != "" {
			containers = append(containers, item.Name)
		}
	}
	if len(containers) == 0 {
		return domain.WorkloadLogStreams{}, fmt.Errorf("%w: workload has no containers", domain.ErrUnsupported)
	}
	if container != "" {
		if !containsString(containers, container) {
			return domain.WorkloadLogStreams{}, fmt.Errorf("%w: container %q is not part of the workload template", domain.ErrInvalid, container)
		}
		containers = []string{container}
	}
	limitBytes := int64(0)
	if !follow {
		limitBytes = maxNonStreamLogBytes
	}
	result := domain.WorkloadLogStreams{Streams: make([]domain.WorkloadLogStream, 0, maxWorkloadLogStreams), Warnings: make([]string, 0)}
	hadStreamFailure := false
	closeOpened := func() {
		for _, item := range result.Streams {
			_ = item.Stream.Close()
		}
	}
	for _, pod := range workload.Pods {
		for _, containerName := range containers {
			if len(result.Streams) >= maxWorkloadLogStreams {
				result.Warnings = append(result.Warnings, fmt.Sprintf("最多同时聚合 %d 条 Pod/容器日志流，其余实例已省略", maxWorkloadLogStreams))
				s.audit(ctx, actor, "stream-workload-logs", kind, namespace, name, nil)
				return result, nil
			}
			access := domain.Access{Verb: "get", Resource: "pods", Subresource: "log", Namespace: pod.Namespace, Name: pod.Name}
			if authErr := s.authorizer.Require(ctx, actor, access); authErr != nil {
				if errors.Is(authErr, domain.ErrForbidden) {
					result.Warnings = append(result.Warnings, fmt.Sprintf("无权读取 %s/%s 的日志", pod.Namespace, pod.Name))
					continue
				}
				closeOpened()
				s.audit(ctx, actor, "stream-workload-logs", kind, namespace, name, authErr)
				return domain.WorkloadLogStreams{}, authErr
			}
			stream, streamErr := s.repository.StreamPodLogs(ctx, pod.Namespace, pod.Name, containerName, follow, previous, tailLines, timestamps, sinceSeconds, limitBytes)
			if streamErr != nil {
				if errors.Is(streamErr, domain.ErrForbidden) {
					result.Warnings = append(result.Warnings, fmt.Sprintf("无权读取 %s/%s/%s 的日志", pod.Namespace, pod.Name, containerName))
					continue
				}
				hadStreamFailure = true
				result.Warnings = append(result.Warnings, fmt.Sprintf("无法打开 %s/%s/%s 的日志流", pod.Namespace, pod.Name, containerName))
				continue
			}
			result.Streams = append(result.Streams, domain.WorkloadLogStream{Namespace: pod.Namespace, Pod: pod.Name, Container: containerName, Stream: stream})
		}
	}
	if len(result.Streams) == 0 {
		err := domain.ErrForbidden
		if len(workload.Pods) == 0 {
			err = fmt.Errorf("%w: workload has no Pods to stream logs from", domain.ErrNotFound)
		} else if hadStreamFailure {
			err = fmt.Errorf("%w: no workload log stream could be opened", domain.ErrUnavailable)
		}
		s.audit(ctx, actor, "stream-workload-logs", kind, namespace, name, err)
		return domain.WorkloadLogStreams{}, err
	}
	s.audit(ctx, actor, "stream-workload-logs", kind, namespace, name, nil)
	return result, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// OpenTerminal authorizes pods/exec before opening the SPDY stream. The
// returned wrapper records both successful and failed interactive sessions.
func (s *Service) OpenTerminal(ctx context.Context, actor domain.Actor, namespace, name string, options domain.PodTerminalOptions) (domain.PodTerminalSession, error) {
	if namespace == "" || name == "" || options.Container == "" {
		return nil, fmt.Errorf("%w: namespace, pod and container are required", domain.ErrInvalid)
	}
	if len(options.Command) == 0 {
		options.Command = []string{"/bin/sh"}
	}
	if !supportedShell(options.Command) {
		return nil, fmt.Errorf("%w: terminal shell is not allow-listed", domain.ErrInvalid)
	}
	if options.Size.Columns < 20 || options.Size.Columns > 300 || options.Size.Rows < 5 || options.Size.Rows > 100 {
		return nil, fmt.Errorf("%w: terminal size is out of range", domain.ErrInvalid)
	}
	access := domain.Access{Verb: "create", Resource: "pods", Subresource: "exec", Namespace: namespace, Name: name}
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, "exec", "pod", namespace, name, err)
		return nil, err
	}
	release, err := s.acquireTerminal(actor)
	if err != nil {
		s.auditor.Record(ctx, actor, domain.AuditEvent{
			Action: "exec", Resource: "pod", Namespace: namespace, Name: name, Result: "failure", Err: err,
			Details: map[string]any{"container": options.Container, "reason": "session_limit"},
		})
		return nil, err
	}
	session, err := s.repository.OpenPodTerminal(ctx, namespace, name, options)
	if err != nil {
		release()
		s.audit(ctx, actor, "exec", "pod", namespace, name, err)
		return nil, err
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: "exec", Resource: "pod", Namespace: namespace, Name: name, Result: "success",
		Details: map[string]any{"container": options.Container, "command": options.Command, "tty": options.TTY},
	})
	return &auditedTerminalSession{
		PodTerminalSession: session, service: s, ctx: ctx, actor: actor,
		namespace: namespace, name: name, container: options.Container,
		started: time.Now(), release: release, closeReason: "client_closed",
	}, nil
}

// CreateEphemeralContainer adds a debug container through the
// pods/ephemeralcontainers subresource. The generated name and allow-listed
// shell keep the request narrow while Kubernetes remains the final validator.
func (s *Service) CreateEphemeralContainer(ctx context.Context, actor domain.Actor, namespace, name string, request domain.PodEphemeralContainerRequest) (domain.PodEphemeralContainerResult, error) {
	const action = "create-ephemeral-container"
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		err := fmt.Errorf("%w: namespace and pod are required", domain.ErrInvalid)
		s.audit(ctx, actor, action, "pod", namespace, name, err)
		return domain.PodEphemeralContainerResult{}, err
	}
	request.Image = strings.TrimSpace(request.Image)
	if request.Image == "" || len(request.Image) > 512 || strings.ContainsAny(request.Image, " \t\r\n\x00") {
		err := fmt.Errorf("%w: debug image must be a single non-empty image reference", domain.ErrInvalid)
		s.audit(ctx, actor, action, "pod", namespace, name, err)
		return domain.PodEphemeralContainerResult{}, err
	}
	request.Shell = strings.TrimSpace(request.Shell)
	if request.Shell == "" {
		request.Shell = "sh"
	}
	if !supportedShell([]string{"/bin/" + request.Shell}) {
		err := fmt.Errorf("%w: debug shell must be sh, bash or ash", domain.ErrInvalid)
		s.audit(ctx, actor, action, "pod", namespace, name, err)
		return domain.PodEphemeralContainerResult{}, err
	}
	if len(request.TargetContainerName) > 253 || strings.ContainsAny(request.TargetContainerName, "\r\n\x00") {
		err := fmt.Errorf("%w: target container name is invalid", domain.ErrInvalid)
		s.audit(ctx, actor, action, "pod", namespace, name, err)
		return domain.PodEphemeralContainerResult{}, err
	}
	access := domain.Access{Verb: "update", Resource: "pods", Subresource: "ephemeralcontainers", Namespace: namespace, Name: name}
	if err := s.authorize(ctx, actor, action, "pod", namespace, name, access); err != nil {
		return domain.PodEphemeralContainerResult{}, err
	}
	containerName, err := generatedDebugContainerName()
	if err != nil {
		s.audit(ctx, actor, action, "pod", namespace, name, err)
		return domain.PodEphemeralContainerResult{}, err
	}
	created, err := s.repository.AddPodEphemeralContainer(ctx, namespace, name, request, containerName)
	details := map[string]any{
		"container": containerName, "image": request.Image, "shell": request.Shell,
	}
	if request.TargetContainerName != "" {
		details["targetContainer"] = request.TargetContainerName
	}
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{
		Action: action, Resource: "pod", Namespace: namespace, Name: name, Result: result, Err: err, Details: details,
	})
	if err != nil {
		return domain.PodEphemeralContainerResult{}, err
	}
	if created == "" {
		created = containerName
	}
	return domain.PodEphemeralContainerResult{Name: created, Status: "created"}, nil
}

func generatedDebugContainerName() (string, error) {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate debug container name: %w", err)
	}
	return "debug-" + hex.EncodeToString(buffer), nil
}

func (s *Service) acquireTerminal(actor domain.Actor) (func(), error) {
	key := actor.Username
	if key == "" {
		key = "anonymous"
	}
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminal.MaxSessionsPerUser > 0 && s.active[key] >= s.terminal.MaxSessionsPerUser {
		return nil, fmt.Errorf("%w: terminal session limit reached for this user", domain.ErrRateLimited)
	}
	s.active[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.terminalMu.Lock()
			defer s.terminalMu.Unlock()
			s.active[key]--
			if s.active[key] <= 0 {
				delete(s.active, key)
			}
		})
	}, nil
}

func supportedShell(command []string) bool {
	if len(command) != 1 {
		return false
	}
	switch command[0] {
	case "/bin/sh", "/bin/bash", "/bin/ash", "/usr/bin/sh", "/usr/bin/bash", "/usr/bin/ash", "sh", "bash", "ash":
		return true
	default:
		return false
	}
}

type auditedTerminalSession struct {
	domain.PodTerminalSession
	service     *Service
	ctx         context.Context
	actor       domain.Actor
	namespace   string
	name        string
	container   string
	started     time.Time
	release     func()
	output      atomic.Int64
	input       atomic.Int64
	reasonMu    sync.Mutex
	closeReason string
	closeErr    error
	closeOnce   sync.Once
}

func (s *auditedTerminalSession) Close() error {
	return s.CloseWithReason("")
}

func (s *auditedTerminalSession) Read(buffer []byte) (int, error) {
	count, err := s.PodTerminalSession.Read(buffer)
	s.output.Add(int64(count))
	return count, err
}

func (s *auditedTerminalSession) Write(buffer []byte) (int, error) {
	count, err := s.PodTerminalSession.Write(buffer)
	s.input.Add(int64(count))
	return count, err
}

func (s *auditedTerminalSession) SetCloseReason(reason string) {
	if reason == "" {
		return
	}
	s.reasonMu.Lock()
	s.closeReason = reason
	s.reasonMu.Unlock()
}

func (s *auditedTerminalSession) CloseWithReason(reason string) error {
	s.SetCloseReason(reason)
	s.closeOnce.Do(func() {
		s.reasonMu.Lock()
		closeReason := s.closeReason
		s.reasonMu.Unlock()
		closeErr := s.PodTerminalSession.Close()
		if s.release != nil {
			s.release()
		}
		s.reasonMu.Lock()
		s.closeErr = closeErr
		s.reasonMu.Unlock()
		result := "success"
		if closeErr != nil {
			result = "failure"
		}
		s.service.auditor.Record(s.ctx, s.actor, domain.AuditEvent{
			Action: "exec-close", Resource: "pod", Namespace: s.namespace, Name: s.name,
			Result: result, Err: closeErr,
			Details: map[string]any{
				"container": s.container, "reason": closeReason,
				"durationMs":  time.Since(s.started).Milliseconds(),
				"outputBytes": s.output.Load(), "inputBytes": s.input.Load(),
			},
		})
	})
	s.reasonMu.Lock()
	defer s.reasonMu.Unlock()
	return s.closeErr
}

func (s *Service) audit(ctx context.Context, actor domain.Actor, action, resource, namespace, name string, err error) {
	result := "success"
	if err != nil {
		result = "failure"
		if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrReadOnly) {
			result = "denied"
		}
	}
	s.auditor.Record(ctx, actor, domain.AuditEvent{Action: action, Resource: resource, Namespace: namespace, Name: name, Result: result, Err: err})
}

func (s *Service) authorize(ctx context.Context, actor domain.Actor, action, kind, namespace, name string, access domain.Access) error {
	if err := s.authorizer.Require(ctx, actor, access); err != nil {
		s.audit(ctx, actor, action, kind, namespace, name, err)
		return err
	}
	return nil
}

func workloadAccess(kind, verb, namespace, name, subresource string) domain.Access {
	normalized := strings.ToLower(kind)
	access := domain.Access{
		Verb: verb, Namespace: namespace, Name: name, Subresource: subresource,
	}
	switch normalized {
	case "deployment", "statefulset", "daemonset":
		access.APIGroup = "apps"
		access.Resource = normalized + "s"
	case "job", "cronjob":
		access.APIGroup = "batch"
		access.Resource = normalized + "s"
	default:
		access.Resource = normalized
	}
	return access
}
