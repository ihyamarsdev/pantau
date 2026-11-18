package gwupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	}
	return "", false
}

// readVersion runs a CLI and returns the version it reports, or "" when it does not run.
func readVersion(path string, flag string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, flag).CombinedOutput()
	if err != nil {
		return ""
	}
	return ExtractVersion(string(out))
}
