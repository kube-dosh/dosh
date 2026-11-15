package kube

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"dosh/internal/domain"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

type podTerminal struct {
	ctx      context.Context
	cancel   context.CancelFunc
	inputW   *io.PipeWriter
	outputR  *io.PipeReader
	sizes    chan remotecommand.TerminalSize
	done     chan struct{}
	waitOnce sync.Once
	waitErr  error
}

func (t *podTerminal) Read(buffer []byte) (int, error)  { return t.outputR.Read(buffer) }
func (t *podTerminal) Write(buffer []byte) (int, error) { return t.inputW.Write(buffer) }

func (t *podTerminal) Resize(size domain.TerminalSize) {
	if size.Columns == 0 || size.Rows == 0 {
		return
	}
	select {
	case t.sizes <- remotecommand.TerminalSize{Width: size.Columns, Height: size.Rows}:
	default:
	}
}

func (t *podTerminal) Next() *remotecommand.TerminalSize {
	select {
	case size := <-t.sizes:
		return &size
	case <-t.ctx.Done():
		return nil
	}
}

func (t *podTerminal) Wait() error {
	t.waitOnce.Do(func() {
		<-t.done
	})
	return t.waitErr
}

func (t *podTerminal) Close() error {
	t.cancel()
	_ = t.inputW.Close()
	_ = t.outputR.Close()
	return nil
}

// OpenPodTerminal establishes an SPDY exec stream using the authenticated
// caller's impersonated Kubernetes identity. The returned session owns both
// PTY pipes and the remote command lifecycle.
func (c *Client) OpenPodTerminal(ctx context.Context, namespace, name string, options domain.PodTerminalOptions) (domain.PodTerminalSession, error) {
	if c.restConfig == nil || c.clientset == nil {
		return nil, fmt.Errorf("%w: Kubernetes exec client is unavailable", domain.ErrUnavailable)
	}
	if namespace == "" || name == "" || options.Container == "" {
		return nil, fmt.Errorf("%w: namespace, pod and container are required", domain.ErrInvalid)
	}
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, mapKubeError(err)
	}
	if !podHasContainer(pod, options.Container) {
		return nil, fmt.Errorf("%w: container %q is not present in pod %s/%s", domain.ErrNotFound, options.Container, namespace, name)
	}
	command := options.Command
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	for _, arg := range command {
		if strings.ContainsRune(arg, '\x00') || len(arg) > 512 {
			return nil, fmt.Errorf("%w: invalid exec command argument", domain.ErrInvalid)
		}
	}
	request := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(name).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: options.Container,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    !options.TTY,
			TTY:       options.TTY,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", request.URL())
	if err != nil {
		return nil, fmt.Errorf("%w: create exec stream: %v", domain.ErrUnavailable, err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	inputR, inputW := io.Pipe()
	outputR, outputW := io.Pipe()
	terminal := &podTerminal{
		ctx: streamCtx, cancel: cancel, inputW: inputW, outputR: outputR,
		sizes: make(chan remotecommand.TerminalSize, 1), done: make(chan struct{}),
	}
	initial := remotecommand.TerminalSize{Width: options.Size.Columns, Height: options.Size.Rows}
	if initial.Width == 0 {
		initial.Width = 100
	}
	if initial.Height == 0 {
		initial.Height = 30
	}
	terminal.sizes <- initial
	go func() {
		err := executor.StreamWithContext(streamCtx, remotecommand.StreamOptions{
			Stdin: inputR, Stdout: outputW, Stderr: outputW,
			Tty: options.TTY, TerminalSizeQueue: terminal,
		})
		_ = outputW.CloseWithError(err)
		_ = inputR.CloseWithError(err)
		terminal.waitErr = err
		close(terminal.done)
	}()
	return terminal, nil
}

func podHasContainer(pod *corev1.Pod, name string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return true
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if container.Name == name {
			return true
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if container.Name == name {
			return true
		}
	}
	return false
}
