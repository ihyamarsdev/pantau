package teleport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	statusTimeout = 20 * time.Second
	listTimeout   = 30 * time.Second
	loginTimeout  = 90 * time.Second
	// maxErrorText bounds the tsh or kubectl text copied into an error, so no large output reaches logs or the UI.
	maxErrorText = 512
)

// Manager runs tsh for Teleport profiles. Each profile gets its own kubeconfig file under
// <dataDir>/teleport; the global ~/.kube/config is never read or written.
type Manager struct {
	dataDir     string
	tshOverride string
	runner      Runner
	userHome    func() (string, error)

	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

// NewManager builds a Manager. dataDir must be absolute. tshOverride may be empty (tsh is then taken from PATH).
// A nil runner means ExecRunner.
func NewManager(dataDir, tshOverride string, runner Runner) (*Manager, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, errors.New("teleport: data directory must be absolute")
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Manager{
		dataDir:     dataDir,
		tshOverride: tshOverride,
		runner:      runner,
		userHome:    os.UserHomeDir,
		locks:       make(map[int64]*sync.Mutex),
	}, nil
}

// KubeconfigPath is the isolated kubeconfig of a profile: <dataDir>/teleport/profile-<id>.yaml.
func KubeconfigPath(dataDir string, profileID int64) (string, error) {
	if profileID <= 0 {
		return "", fmt.Errorf("%w: profile id must be positive", ErrInvalidInput)
	}
	return filepath.Join(dataDir, "teleport", "profile-"+strconv.FormatInt(profileID, 10)+".yaml"), nil
}

// prepareKubeconfigDir creates <dataDir>/teleport with mode 0700, whatever the umask.
func prepareKubeconfigDir(dataDir string) error {
	dir := filepath.Join(dataDir, "teleport")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create teleport directory: %w", err)
	}
	return os.Chmod(dir, 0o700)
}

// lockProfile serializes tsh work for one profile, so two concurrent logins cannot race on one kubeconfig.
// The returned func releases the lock.
func (m *Manager) lockProfile(id int64) func() {
	m.mu.Lock()
	l, ok := m.locks[id]
	if !ok {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// env is the environment for tsh and kubectl: the caller's environment with KUBECONFIG pointing at the isolated
// file and, when set, TELEPORT_HOME pointing at the profile's home.
func (m *Manager) env(p Profile, kubeconfig string) ([]string, error) {
	base := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "KUBECONFIG=") || strings.HasPrefix(kv, "TELEPORT_HOME=") {
			continue
		}
		base = append(base, kv)
	}
	base = append(base, "KUBECONFIG="+kubeconfig)
	if p.TeleportHome != "" {
		userHome, err := m.userHome()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		base = append(base, "TELEPORT_HOME="+ResolveHome(p.TeleportHome, userHome))
	}
	return base, nil
}

func (m *Manager) tsh() (string, error) {
	return LocateTSH(m.tshOverride, nil)
}

// run executes tsh with a deadline. A non-zero exit is returned as a Result, not an error.
func (m *Manager) run(ctx context.Context, timeout time.Duration, p Profile, kubeconfig string, args ...string) (Result, error) {
	bin, err := m.tsh()
	if err != nil {
		return Result{}, err
	}
	env, err := m.env(p, kubeconfig)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.runner.Run(ctx, env, bin, args...)
}

// failure turns a non-zero tsh or kubectl exit into the right error: an expired session wraps
// ErrTeleportSessionExpired, anything else carries a bounded copy of the output.
func failure(what string, res Result) error {
	out := strings.TrimSpace(string(res.Stderr))
	if out == "" {
		out = strings.TrimSpace(string(res.Stdout))
	}
	if IsSessionExpiredOutput(out) {
		return fmt.Errorf("%s: %w", what, ErrTeleportSessionExpired)
	}
	if len(out) > maxErrorText {
		out = out[:maxErrorText] + "..."
	}
	return fmt.Errorf("%s failed (exit %d): %s", what, res.ExitCode, out)
}

// Status reads the session of a profile. A profile with no session returns an inactive Status and no error.
func (m *Manager) Status(ctx context.Context, p Profile) (Status, error) {
	if err := ValidateProfile(p); err != nil {
		return Status{}, err
	}
	kubeconfig, err := KubeconfigPath(m.dataDir, p.ID)
	if err != nil {
		return Status{}, err
	}
	res, err := m.run(ctx, statusTimeout, p, kubeconfig, "status", "--format=json")
	if err != nil {
		return Status{}, err
	}
	if res.ExitCode != 0 {
		if strings.Contains(strings.ToLower(string(res.Stderr)+string(res.Stdout)), "not logged in") {
			return Status{}, nil
		}
		return Status{}, failure("tsh status", res)
	}
	return ParseStatus(res.Stdout, p.Proxy, p.TeleportCluster)
}

// ListKubeClusters lists the Kubernetes clusters the profile can reach.
func (m *Manager) ListKubeClusters(ctx context.Context, p Profile) ([]KubeCluster, error) {
	if err := ValidateProfile(p); err != nil {
		return nil, err
	}
	kubeconfig, err := KubeconfigPath(m.dataDir, p.ID)
	if err != nil {
		return nil, err
	}
	args := []string{"kube", "ls", "--format=json"}
	if p.TeleportCluster != "" {
		args = append(args, "--cluster", p.TeleportCluster)
	}
	res, err := m.run(ctx, listTimeout, p, kubeconfig, args...)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, failure("tsh kube ls", res)
	}
	return ParseKubeLS(res.Stdout)
}

// EnsureKubeContext logs the profile into one Kubernetes cluster. tsh writes that cluster's context into the
// profile's isolated kubeconfig; the returned name is the current-context tsh set. The profile lock covers
// the login and the read, so a concurrent login cannot change current-context in between.
func (m *Manager) EnsureKubeContext(ctx context.Context, p Profile, kubeName string) (string, error) {
	if err := ValidateProfile(p); err != nil {
		return "", err
	}
	if err := ValidateKubeName(kubeName); err != nil {
		return "", err
	}
	kubeconfig, err := KubeconfigPath(m.dataDir, p.ID)
	if err != nil {
		return "", err
	}
	if err := prepareKubeconfigDir(m.dataDir); err != nil {
		return "", err
	}

	unlock := m.lockProfile(p.ID)
	defer unlock()

	args := []string{"kube", "login"}
	if p.TeleportCluster != "" {
		args = append(args, "--cluster", p.TeleportCluster)
	}
	args = append(args, kubeName)
	res, err := m.run(ctx, loginTimeout, p, kubeconfig, args...)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", failure("tsh kube login "+kubeName, res)
	}

	raw, err := os.ReadFile(kubeconfig)
	if err != nil {
		return "", fmt.Errorf("read isolated kubeconfig: %w", err)
	}
	name, err := ParseCurrentContext(raw)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(kubeconfig, 0o600); err != nil {
		return "", fmt.Errorf("chmod isolated kubeconfig: %w", err)
	}
	return name, nil
}

// SyncAllContexts logs the profile into every cluster it lists, so each context is ready before first use.
// It stops at the first expired session. Other per-cluster failures are collected and returned together.
// It returns the context name for each cluster that succeeded.
func (m *Manager) SyncAllContexts(ctx context.Context, p Profile) (map[string]string, error) {
	clusters, err := m.ListKubeClusters(ctx, p)
	if err != nil {
		return nil, err
	}
	contexts := make(map[string]string, len(clusters))
	var errs []error
	for _, c := range clusters {
		name, err := m.EnsureKubeContext(ctx, p, c.Name)
		if errors.Is(err, ErrTeleportSessionExpired) {
			return contexts, err
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name, err))
			continue
		}
		contexts[c.Name] = name
	}
	return contexts, errors.Join(errs...)
}
