package inspector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/gwupdate"
	"pantau/internal/teleport"
)

// forwardStartTimeout bounds how long a new kubectl port-forward may take to start listening.
const forwardStartTimeout = 20 * time.Second

// forwardReadyPrefix is the start of the line kubectl prints once a port-forward listens.
const forwardReadyPrefix = "Forwarding from "

var (
	k8sLabelRe     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	k8sSubdomainRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
)

// KubeForwardTarget is the Service or Pod that kubectl port-forward connects to.
type KubeForwardTarget struct {
	Kind       string // "service" or "pod"
	Name       string
	Namespace  string
	RemotePort int
}

// Validate rejects anything that is not a plain Kubernetes name or port. Values are passed as argv, never through a shell.
func (t KubeForwardTarget) Validate() error {
	switch t.Kind {
	case "service", "pod":
	default:
		return fmt.Errorf("kind must be service or pod")
	}
	if !k8sSubdomainRe.MatchString(t.Name) || strings.Contains(t.Name, "..") {
		return fmt.Errorf("invalid %s name %q", t.Kind, t.Name)
	}
	if !k8sLabelRe.MatchString(t.Namespace) {
		return fmt.Errorf("invalid namespace %q", t.Namespace)
	}
	if t.RemotePort < 1 || t.RemotePort > 65535 {
		return fmt.Errorf("remote port must be 1-65535")
	}
	return nil
}

// Ref is the kubectl resource argument, for example svc/web or pod/web-7f9c.
func (t KubeForwardTarget) Ref() string {
	if t.Kind == "service" {
		return "svc/" + t.Name
	}
	return "pod/" + t.Name
}

// Args is the kubectl argv for one forward. mapping is "local:remote", or ":remote" to let kubectl choose the local port.
func (t KubeForwardTarget) Args(bind, mapping string) []string {
	return []string{"port-forward", "--address", bind, t.Ref(), mapping, "-n", t.Namespace}
}

// KubectlForward is a kubectl port-forward process this machine runs against an external cluster.
type KubectlForward struct {
	cmd      *exec.Cmd
	cleanup  func()
	exited   chan struct{}
	stopOnce sync.Once
	stderr   *boundedBuffer
}

// StartLocalKubectlForward runs kubectl port-forward with the cluster's kubeconfig in a temp file and waits until
// kubectl reports the forward listening. It returns the process and that readiness line. ctx only bounds the start;
// the forward keeps running until Stop.
func StartLocalKubectlForward(ctx context.Context, kubectlPath, kubeconfig string, t KubeForwardTarget, bind, mapping string) (*KubectlForward, string, error) {
	if err := t.Validate(); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(kubeconfig) == "" {
		return nil, "", errors.New("cluster has no kubeconfig")
	}
	cfgPath, cleanup, err := writeTempKubeconfig(kubeconfig)
	if err != nil {
		return nil, "", err
	}
	return startKubectlForward(ctx, kubectlPath, cfgPath, "", cleanup, t, bind, mapping)
}

// StartTeleportKubectlForward runs kubectl port-forward against a Teleport cluster's isolated kubeconfig file.
// The file is not copied or removed: --kubeconfig and --context name it on the argv, so the forward cannot
// fall back to another cluster or to ~/.kube/config.
func StartTeleportKubectlForward(ctx context.Context, kubectlPath, kubeconfigPath, kubeContext string, t KubeForwardTarget, bind, mapping string) (*KubectlForward, string, error) {
	if err := t.Validate(); err != nil {
		return nil, "", err
	}
	if err := teleport.ValidateContextName(kubeContext); err != nil {
		return nil, "", err
	}
	return startKubectlForward(ctx, kubectlPath, kubeconfigPath, kubeContext, func() {}, t, bind, mapping)
}

// startKubectlForward starts kubectl. kubeContext is empty for a kubeconfig copy, which kubectl reads from KUBECONFIG.
func startKubectlForward(ctx context.Context, kubectlPath, cfgPath, kubeContext string, cleanup func(), t KubeForwardTarget, bind, mapping string) (*KubectlForward, string, error) {
	args := t.Args(bind, mapping)
	if kubeContext != "" {
		args = append([]string{"--kubeconfig", cfgPath, "--context", kubeContext}, args...)
	}
	cmd := exec.Command(kubectlPath, args...)
	cmd.Env = append(os.Environ(), "PATH="+gwupdate.PrependPath(os.Getenv("PATH")), "KUBECONFIG="+cfgPath)
	setProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, "", err
	}
	f := &KubectlForward{cmd: cmd, cleanup: cleanup, exited: make(chan struct{}), stderr: newBoundedBuffer(4096)}
	cmd.Stderr = f.stderr
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, "", fmt.Errorf("start kubectl: %w", err)
	}

	ready := make(chan string, 1)
	go func() {
		// Output is read to EOF before Wait, as os/exec requires for StdoutPipe.
		scanForwardOutput(stdout, ready, f.stderr)
		_ = cmd.Wait()
		close(f.exited)
	}()

	line, err := waitForwardReady(ctx, ready, f.exited)
	if err != nil {
		f.Stop()
		if tail := strings.TrimSpace(f.stderr.String()); tail != "" {
			return nil, "", fmt.Errorf("kubectl port-forward: %w: %s", err, tail)
		}
		return nil, "", fmt.Errorf("kubectl port-forward: %w", err)
	}
	return f, line, nil
}

// Stop terminates kubectl and its process group, then removes the temp kubeconfig. Safe to call more than once.
func (f *KubectlForward) Stop() {
	f.stopOnce.Do(func() {
		terminateProcessGroup(f.cmd, f.exited)
		f.cleanup()
	})
}

// scanForwardOutput reads kubectl output to EOF. The first readiness line goes to ready; every line is kept in log
// so the error that ends a forward is still visible. A line the scanner rejects still drains, so kubectl never blocks.
func scanForwardOutput(r io.Reader, ready chan<- string, log io.Writer) {
	sc := bufio.NewScanner(r)
	sent := false
	for sc.Scan() {
		line := sc.Text()
		_, _ = io.WriteString(log, line+"\n")
		if !sent && strings.HasPrefix(line, forwardReadyPrefix) {
			sent = true
			ready <- line
		}
	}
	_, _ = io.Copy(io.Discard, r)
}

// AwaitForwardReady reads a port-forward's output until kubectl reports the forward listening, and returns that line.
// Output keeps draining in the background, so the producer never blocks. It fails when the output ends first, when
// bound passes, or when ctx ends; the caller then stops the command.
func AwaitForwardReady(ctx context.Context, out io.Reader, bound time.Duration, log io.Writer) (string, error) {
	ready := make(chan string, 1)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		scanForwardOutput(out, ready, log)
	}()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case line := <-ready:
		return line, nil
	case <-ended:
		select {
		case line := <-ready:
			return line, nil
		default:
		}
		return "", errors.New("the forward ended before it was ready")
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("timed out waiting for the forward to start")
	}
}

// waitForwardReady returns the readiness line, or an error when kubectl exits, the start budget passes or ctx ends.
func waitForwardReady(ctx context.Context, ready <-chan string, exited <-chan struct{}) (string, error) {
	timer := time.NewTimer(forwardStartTimeout)
	defer timer.Stop()
	select {
	case line := <-ready:
		return line, nil
	case <-exited:
		select {
		case line := <-ready:
			return line, nil
		default:
		}
		return "", errors.New("kubectl exited before the forward was ready")
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("timed out waiting for the forward to start")
	}
}

// KubectlPath returns the kubectl binary this machine uses for external clusters.
func KubectlPath() (string, error) {
	return resolveKubectlBinary()
}

// ParseForwardPort extracts the local port from a readiness line such as "Forwarding from 127.0.0.1:4242 -> 80".
func ParseForwardPort(line string) (int, bool) {
	m := forwardPortRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	port, err := strconv.Atoi(m[1])
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

var forwardPortRe = regexp.MustCompile(`^Forwarding from \[?[0-9a-fA-F:.]+\]?:(\d+) ->`)

// boundedBuffer keeps the newest bytes written to it, so kubectl stderr cannot grow memory without limit.
type boundedBuffer struct {
	mu  sync.Mutex
	max int
	buf bytes.Buffer
}

func newBoundedBuffer(max int) *boundedBuffer { return &boundedBuffer{max: max} }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	if extra := b.buf.Len() - b.max; extra > 0 {
		b.buf.Next(extra)
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
