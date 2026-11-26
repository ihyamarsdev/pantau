package gwupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// The CLIs Pantau manages itself live in a directory it owns. When a managed binary is there it is
// used in preference to the one in PATH, so an update takes effect at once and the system copy is
// never touched (it belongs to the package manager, and Pantau usually does not run as root).

var (
	managedMu   sync.RWMutex
	managedRoot string
)

// SetManagedRoot sets the directory Pantau installs the CLIs it manages into. An empty root turns
// the managed binaries off.
func SetManagedRoot(root string) {
	managedMu.Lock()
	managedRoot = root
	managedMu.Unlock()
}

// ManagedRoot returns the directory Pantau installs the CLIs it manages into.
func ManagedRoot() string {
	managedMu.RLock()
	defer managedMu.RUnlock()
	return managedRoot
}

// Dir returns the managed directory for the current root.
func Dir() ManagedDir { return ManagedDir{Root: ManagedRoot()} }

// ResolveBinary returns the path of a CLI to run: the managed binary when Pantau installed one,
// otherwise the one in PATH. It fails when neither exists.
func ResolveBinary(name string) (string, error) {
	if p, ok := Dir().ManagedBinary(name); ok {
		return p, nil
	}
	return exec.LookPath(name)
}

// ResolveVenvCommand returns the path of a command inside the managed ansible virtualenv when
// Pantau installed one, otherwise the one in PATH.
func ResolveVenvCommand(name string) (string, error) {
	if p, ok := Dir().ManagedVenvBin(name); ok {
		return p, nil
	}
	return exec.LookPath(name)
}

// PathPrefix is the PATH fragment that puts the managed binaries first, so a child process started
// with the inherited PATH also finds the managed binary. It is empty when nothing is managed.
func PathPrefix() string {
	root := ManagedRoot()
	if root == "" {
		return ""
	}
	bin := Dir().BinDir()
	venvBin := filepath.Join(Dir().VenvLink(), "bin")
	if runtime.GOOS == "windows" {
		venvBin = filepath.Join(Dir().VenvLink(), "Scripts")
	}
	return bin + string(os.PathListSeparator) + venvBin
}

// PrependPath returns PATH with the managed binaries in front of it.
func PrependPath(path string) string {
	prefix := PathPrefix()
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	return prefix + string(os.PathListSeparator) + path
}

// kubectlPrefix names the cached kubectl binaries: versions/kubectl-<version>.
const kubectlPrefix = "kubectl-"

// ErrKubectlVersionActive is returned when a cached kubectl is asked to be deleted while it is in use.
var ErrKubectlVersionActive = errors.New("this kubectl version is in use")

// VersionsDir holds the kubectl binaries Pantau has downloaded, one per version.
func (m ManagedDir) VersionsDir() string { return filepath.Join(m.Root, "versions") }

// ActiveKubectlVersion returns the version the bin/kubectl symlink points at, or "" when it is not
// set (the system kubectl is in use).
func (m ManagedDir) ActiveKubectlVersion() string {
	if m.Root == "" {
		return ""
	}
	target, err := os.Readlink(filepath.Join(m.BinDir(), "kubectl"))
	if err != nil || filepath.Dir(target) != m.VersionsDir() {
		return ""
	}
	name, ok := strings.CutPrefix(filepath.Base(target), kubectlPrefix)
	if !ok {
		return ""
	}
	return name
}

// ListKubectlVersions returns the cached kubectl versions, newest first, and the active version
// ("" when the system kubectl is in use).
func (m ManagedDir) ListKubectlVersions() ([]string, string, error) {
	if m.Root == "" {
		return nil, "", nil
	}
	active := m.ActiveKubectlVersion()
	entries, err := os.ReadDir(m.VersionsDir())
	if os.IsNotExist(err) {
		return nil, active, nil
	}
	if err != nil {
		return nil, "", err
	}
	var versions []string
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), kubectlPrefix)
		if !ok || e.IsDir() {
			continue
		}
		if _, ok := ParseVersion(name); ok {
			versions = append(versions, name)
		}
	}
	sort.SliceStable(versions, func(i, j int) bool { return Compare(versions[i], versions[j]) > 0 })
	return versions, active, nil
}

// UseSystemKubectl reverts to the system kubectl by removing the bin/kubectl symlink. The cached
// versions and the system copy are left alone.
func (m ManagedDir) UseSystemKubectl() error {
	if m.Root == "" {
		return errors.New("the managed directory is not set")
	}
	link := filepath.Join(m.BinDir(), "kubectl")
	st, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a managed link", link)
	}
	return os.Remove(link)
}

// DeleteKubectlVersion removes one cached kubectl version. The active version is refused.
func (m ManagedDir) DeleteKubectlVersion(version string) error {
	if _, ok := ParseVersion(version); !ok {
		return fmt.Errorf("%q is not a release version", version)
	}
	if m.ActiveKubectlVersion() == version {
		return ErrKubectlVersionActive
	}
	return os.Remove(filepath.Join(m.VersionsDir(), kubectlPrefix+version))
}

// ManagedVersion reports the version of a managed CLI, and whether it is managed at all.
func ManagedVersion(target string) (string, bool) {
	switch target {
	case TargetAnsible:
		p, ok := Dir().ManagedVenvBin("ansible-playbook")
		if !ok {
			return "", false
		}
		return readVersion(p, "--version"), true
	case TargetTerraform, TargetOpenTofu:
		p, ok := Dir().ManagedBinary(target)
		if !ok {
			return "", false
		}
		return readVersion(p, "version"), true
	case TargetKubectl:
		p, ok := Dir().ManagedBinary(target)
		if !ok {
			return "", false
		}
		// --client keeps it offline: without it kubectl contacts the API server.
		return readVersion(p, "version", "--client"), true
	}
	return "", false
}

// readVersion runs a CLI with args and returns the version it reports, or "" when it does not run.
func readVersion(path string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		return ""
	}
	return ExtractVersion(string(out))
}
