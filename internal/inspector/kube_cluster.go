package inspector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"

	"github.com/pkg/sftp"
)

const maxKubeconfigBytes = 128 * 1024

var allowedExecCommands = map[string]bool{
	"aws":                    true,
	"aws-iam-authenticator":  true,
	"gke-gcloud-auth-plugin": true,
	"gcloud":                 true,
	"kubelogin":              true,
	"az":                     true,
	"doctl":                  true,
	"oci":                    true,
}

// shortcut: target < 0 addresses an external cluster, hostID > 0 addresses an SSH host.
func KubeClusterTarget(clusterID int64) int64 {
	return -clusterID
}

func IsKubeClusterTarget(id int64) bool {
	return id < 0
}

type KubeconfigContexts struct {
	Contexts []string
	Current  string
}

type PreparedKubeconfig struct {
	Kubeconfig string
	Context    string
	Server     string
	Provider   string
}

func ensureKubectlInstalled() error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return errors.New("kubectl is not installed on the Pantau server")
	}
	return nil
}

// writeTempKubeconfig writes raw bytes to a 0600 temp file and returns its path and a cleanup func.
func writeTempKubeconfig(raw string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "pantau-kubeconfig-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp kubeconfig: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(tmp.Name())
	}
	if err := os.Chmod(tmp.Name(), 0600); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("chmod temp kubeconfig: %w", err)
	}
	if _, err := tmp.WriteString(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temp kubeconfig: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temp kubeconfig: %w", err)
	}
	return tmp.Name(), cleanup, nil
}

// ListKubeconfigContexts parses the contexts and current-context using local kubectl.
func ListKubeconfigContexts(raw string) (*KubeconfigContexts, error) {
	if len(raw) > maxKubeconfigBytes {
		return nil, kubeInvalid("kubeconfig exceeds 128 KB limit (got %d bytes)", len(raw))
	}
	if strings.TrimSpace(raw) == "" {
		return nil, kubeInvalid("kubeconfig is empty")
	}
	if err := ensureKubectlInstalled(); err != nil {
		return nil, err
	}

	tmpFile, cleanup, err := writeTempKubeconfig(raw)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd := exec.Command("kubectl", "config", "view", "-o", "json", "--kubeconfig", tmpFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, kubeInvalid("failed to parse kubeconfig: %s", strings.TrimSpace(stderr.String()))
	}

	var parsed struct {
		Contexts []struct {
			Name string `json:"name"`
		} `json:"contexts"`
		CurrentContext string `json:"current-context"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, kubeInvalid("failed to parse kubectl output: %v", err)
	}

	res := &KubeconfigContexts{
		Contexts: make([]string, 0, len(parsed.Contexts)),
		Current:  parsed.CurrentContext,
	}
	for _, c := range parsed.Contexts {
		if c.Name != "" {
			res.Contexts = append(res.Contexts, c.Name)
		}
	}
	return res, nil
}

// kubeconfigJSON represents the relevant fields from kubectl config view -o json.
type kubeconfigJSON struct {
	Kind           string `json:"kind"`
	APIVersion     string `json:"apiVersion"`
	CurrentContext string `json:"current-context"`
	Clusters       []struct {
		Name    string `json:"name"`
		Cluster struct {
			Server                   string `json:"server"`
			CertificateAuthority     string `json:"certificate-authority"`
			CertificateAuthorityData string `json:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `json:"insecure-skip-tls-verify"`
		} `json:"cluster"`
	} `json:"clusters"`
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster   string `json:"cluster"`
			User      string `json:"user"`
			Namespace string `json:"namespace"`
		} `json:"context"`
	} `json:"contexts"`
	Users []struct {
		Name string `json:"name"`
		User struct {
			ClientCertificate     string         `json:"client-certificate"`
			ClientCertificateData string         `json:"client-certificate-data"`
			ClientKey             string         `json:"client-key"`
			ClientKeyData         string         `json:"client-key-data"`
			Token                 string         `json:"token"`
			TokenFile             string         `json:"tokenFile"`
			AuthProvider          map[string]any `json:"auth-provider"`
			Exec                  *struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"exec"`
		} `json:"user"`
	} `json:"users"`
}

// PrepareKubeconfig validates and normalizes raw kubeconfig into a minified single-context JSON.
func PrepareKubeconfig(raw, contextName string) (*PreparedKubeconfig, error) {
	if len(raw) > maxKubeconfigBytes {
		return nil, kubeInvalid("kubeconfig exceeds 128 KB limit (got %d bytes)", len(raw))
	}
	if strings.TrimSpace(raw) == "" {
		return nil, kubeInvalid("kubeconfig is empty")
	}
	if err := ensureKubectlInstalled(); err != nil {
		return nil, err
	}

	tmpFile, cleanup, err := writeTempKubeconfig(raw)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	args := []string{"config", "view", "--raw", "--minify", "-o", "json", "--kubeconfig", tmpFile}
	if contextName != "" {
		args = append(args, "--context="+contextName)
	}

	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, kubeInvalid("kubectl config view failed: %s", strings.TrimSpace(stderr.String()))
	}

	var parsed kubeconfigJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, kubeInvalid("invalid json from kubectl: %v", err)
	}

	selectedContext := parsed.CurrentContext
	if contextName != "" && selectedContext == "" {
		selectedContext = contextName
	}
	if selectedContext == "" {
		return nil, kubeInvalid("no context selected or found in kubeconfig")
	}

	var server string
	for _, c := range parsed.Clusters {
		if c.Cluster.CertificateAuthority != "" {
			return nil, kubeInvalid("file-path certificate-authority is not allowed; use inline certificate-authority-data")
		}
		if server == "" && c.Cluster.Server != "" {
			server = c.Cluster.Server
		}
	}

	var execCmd string
	for _, u := range parsed.Users {
		if u.User.ClientCertificate != "" {
			return nil, kubeInvalid("file-path client-certificate is not allowed; use inline client-certificate-data")
		}
		if u.User.ClientKey != "" {
			return nil, kubeInvalid("file-path client-key is not allowed; use inline client-key-data")
		}
		if u.User.TokenFile != "" {
			return nil, kubeInvalid("file-path tokenFile is not allowed; use inline token")
		}
		if len(u.User.AuthProvider) > 0 {
			return nil, kubeInvalid("auth-provider is not allowed")
		}
		if u.User.Exec != nil {
			cmdName := u.User.Exec.Command
			if strings.Contains(cmdName, "/") || strings.Contains(cmdName, "\\") {
				return nil, kubeInvalid("exec.command path not allowed, must be bare name: %s", cmdName)
			}
			if !allowedExecCommands[cmdName] {
				return nil, kubeInvalid("exec.command %q is not in the allowlist", cmdName)
			}
			execCmd = cmdName
		}
	}

	provider := deriveProvider(execCmd, server)

	return &PreparedKubeconfig{
		Kubeconfig: stdout.String(),
		Context:    selectedContext,
		Server:     server,
		Provider:   provider,
	}, nil
}

func deriveProvider(execCmd, server string) string {
	// Provider derived from exec command (aws*->eks, gke-gcloud-auth-plugin|gcloud->gke, kubelogin|az->aks)
	// else server hostname (*.eks.amazonaws.com, *.azmk8s.io, container.googleapis.com) else "generic"
	switch {
	case strings.HasPrefix(execCmd, "aws"):
		return "eks"
	case execCmd == "gke-gcloud-auth-plugin" || execCmd == "gcloud":
		return "gke"
	case execCmd == "kubelogin" || execCmd == "az":
		return "aks"
	}

	host := server
	if u, err := url.Parse(server); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}

	switch {
	case strings.HasSuffix(host, ".eks.amazonaws.com"):
		return "eks"
	case strings.HasSuffix(host, ".azmk8s.io"):
		return "aks"
	case strings.Contains(host, "container.googleapis.com"):
		return "gke"
	default:
		return "generic"
	}
}

// localKubeRunner implements sshrunner.Runner for external clusters via local kubectl.
type localKubeRunner struct {
	kubeconfigPath string
	cleanupFunc    func()
	closeOnce      sync.Once

	streamMu   sync.Mutex
	streamPgid int // process group of the active Stream child, 0 when none
}

func newLocalKubeRunner(kubeconfigContent string) (*localKubeRunner, error) {
	if err := ensureKubectlInstalled(); err != nil {
		return nil, err
	}
	path, cleanup, err := writeTempKubeconfig(kubeconfigContent)
	if err != nil {
		return nil, err
	}
	return &localKubeRunner{
		kubeconfigPath: path,
		cleanupFunc:    cleanup,
	}, nil
}

func (r *localKubeRunner) Exec(cmd string) (stdout, stderr string, exitCode int, err error) {
	return r.ExecTimeout(cmd, sshrunner.DefaultExecTimeout)
}

func (r *localKubeRunner) ExecTimeout(cmdStr string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	if timeout <= 0 {
		timeout = sshrunner.DefaultExecTimeout
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "KUBECONFIG="+r.kubeconfigPath)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "", "", -1, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(timeout):
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return stdoutBuf.String(), stderrBuf.String(), -1, fmt.Errorf("command timed out after %v", timeout)
	case execErr := <-done:
		stdout = stdoutBuf.String()
		stderr = stderrBuf.String()
		if execErr != nil {
			if exitErr, ok := execErr.(*exec.ExitError); ok {
				return stdout, stderr, exitErr.ExitCode(), nil
			}
			return stdout, stderr, -1, execErr
		}
		return stdout, stderr, 0, nil
	}
}

func (r *localKubeRunner) Stream(cmdStr string, out io.Writer) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	defer pr.Close()

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "KUBECONFIG="+r.kubeconfigPath)
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	defer pw.Close()

	// ponytail: register the child so Close() can kill it when the stream consumer goes away.
	r.streamMu.Lock()
	r.streamPgid = cmd.Process.Pid
	r.streamMu.Unlock()
	defer func() {
		r.streamMu.Lock()
		r.streamPgid = 0
		r.streamMu.Unlock()
	}()

	// ponytail: copy child output ourselves so a closed/erroring consumer writer kills the process group.
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	copyDone := make(chan error, 1)
	go func() {
		_, cerr := io.Copy(out, pr)
		copyDone <- cerr
	}()

	select {
	case waitErr := <-waitCh:
		_ = pw.Close() // unblock the copier; child exit already closed its pipe end
		if cerr := <-copyDone; cerr != nil && waitErr == nil {
			waitErr = cerr
		}
		return waitErr
	case cerr := <-copyDone:
		if cerr != nil && cmd.Process != nil && cmd.Process.Pid > 0 {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		waitErr := <-waitCh
		if cerr != nil {
			waitErr = fmt.Errorf("stream stopped: %w", cerr)
		}
		return waitErr
	}
}

func (r *localKubeRunner) PipeCommand(cmdStr string, in io.Reader, out io.Writer) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "KUBECONFIG="+r.kubeconfigPath)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}

func (r *localKubeRunner) SFTP() (*sftp.Client, error) {
	return nil, errors.New("sftp is not supported for external clusters")
}

func (r *localKubeRunner) SFTPAs(runAsUser string) (*sftp.Client, error) {
	return nil, errors.New("sftp is not supported for external clusters")
}

func (r *localKubeRunner) Terminal(in io.Reader, out io.Writer, cols, rows int, resizeChan <-chan [2]int) error {
	return errors.New("terminal is not supported for external clusters")
}

func (r *localKubeRunner) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (r *localKubeRunner) Close() error {
	r.closeOnce.Do(func() {
		r.streamMu.Lock()
		if pgid := r.streamPgid; pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL) // stop a running `kubectl logs -f` child
		}
		r.streamMu.Unlock()
		if r.cleanupFunc != nil {
			r.cleanupFunc()
		}
	})
	return nil
}

// clusterPrelude returns a minimal prelude that defines kc() invoking kubectl,
// and sets KUBE_DISTRO to the cluster provider.
func clusterPrelude(provider string) string {
	return fmt.Sprintf("KUBE_DISTRO=%s\nkc() { kubectl \"$@\"; }\n", shellQuote(provider))
}

// wrapClusterCmd wraps body with clusterPrelude and sh -c invocation.
// If body is already wrapped with KubeShell / sh -c, unwrap the body first.
func wrapClusterCmd(body, provider string) string {
	if strings.HasPrefix(body, "sh -c '") && strings.HasSuffix(body, "'") {
		inner := body[7 : len(body)-1]
		inner = strings.ReplaceAll(inner, "'\\''", "'")
		if strings.HasPrefix(inner, kubePrelude) {
			body = strings.TrimPrefix(inner, kubePrelude)
		}
	}
	return "sh -c " + shellQuote(clusterPrelude(provider)+body)
}

// getClusterTarget returns the cluster or error for a negative target ID.
func (ins *Inspector) getClusterTarget(targetID int64) (*store.KubeCluster, error) {
	clusterID := -targetID
	c, err := ins.db.GetKubeCluster(clusterID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("cluster not found")
	}
	return c, nil
}

// KubeStreamRunner returns a Runner and command wrapper function for either a host or external cluster.
// For clusters, the wrapper applies the cluster prelude with the cluster's provider.
// For hosts, the wrapper applies the standard host kubePrelude.
func (ins *Inspector) KubeStreamRunner(targetID int64) (sshrunner.Runner, func(string) string, error) {
	if IsKubeClusterTarget(targetID) {
		cluster, err := ins.getClusterTarget(targetID)
		if err != nil {
			return nil, nil, err
		}
		runner, err := newLocalKubeRunner(cluster.Kubeconfig)
		if err != nil {
			return nil, nil, err
		}
		wrap := func(body string) string {
			return wrapClusterCmd(body, cluster.Provider)
		}
		return runner, wrap, nil
	}

	host, err := ins.db.GetHost(targetID)
	if err != nil || host == nil {
		return nil, nil, fmt.Errorf("host not found")
	}
	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, nil, err
	}
	return runner, kubeShell, nil
}
