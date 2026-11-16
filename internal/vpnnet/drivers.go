package vpnnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// maxOutput bounds what one driver command may contribute to a log or an error message.
	maxOutput = 4 << 10
	// maxCommandOutput bounds memory for a command that streams (netbird status can be tens of KiB);
	// the head is kept, because JSON documents must stay parseable.
	maxCommandOutput = 512 << 10
)

// Drivers supported out of the box.
const (
	DriverNetBird   = "netbird"
	DriverNetMaker  = "netmaker"
	DriverOpenVPN   = "openvpn"
	DriverWireGuard = "wireguard"
	DriverTailscale = "tailscale"
	DriverCustom    = "custom"
)

// Drivers lists every driver name in UI order.
var Drivers = []string{DriverNetBird, DriverNetMaker, DriverOpenVPN, DriverWireGuard, DriverTailscale, DriverCustom}

// secretKeys are the config fields that must never leave the API or reach a log line.
var secretKeys = map[string][]string{
	DriverNetBird:   {"setup_key"},
	DriverNetMaker:  {"token"},
	DriverOpenVPN:   {"config", "username", "password"},
	DriverWireGuard: {"config"},
	DriverTailscale: {"authkey"},
	DriverCustom:    {"up", "down", "status"},
}

// SecretKeys lists the config fields that must be redacted and never logged for a driver.
func SecretKeys(driver string) []string { return secretKeys[driver] }

// ValidDriver reports whether name is a known driver.
func ValidDriver(name string) bool {
	for _, d := range Drivers {
		if d == name {
			return true
		}
	}
	return false
}

// Network is one row of store.VPNNetwork with its config already parsed.
type Network struct {
	ID      int64
	Name    string
	Driver  string
	Enabled bool
	AutoUp  bool
	Iface   string
	Config  map[string]string
	Root    string
}

func (n Network) cfg(key, def string) string {
	if v := strings.TrimSpace(n.Config[key]); v != "" {
		return v
	}
	return def
}

func (n Network) secrets() []string {
	var out []string
	for _, k := range secretKeys[n.Driver] {
		if v := n.Config[k]; v != "" {
			out = append(out, v)
		}
	}
	return out
}

// driver is the per-vendor behaviour. Process-owning drivers use the manager to spawn children.
type driver interface {
	DefaultIface(n Network) string
	Up(ctx context.Context, m *Manager, n Network) error
	Down(ctx context.Context, m *Manager, n Network) error
	Status(ctx context.Context, m *Manager, n Network) (Status, error)
}

func driverFor(name string) driver {
	switch name {
	case DriverNetBird:
		return netbirdDriver{}
	case DriverNetMaker:
		return netmakerDriver{}
	case DriverOpenVPN:
		return openvpnDriver{}
	case DriverWireGuard:
		return wireguardDriver{}
	case DriverTailscale:
		return tailscaleDriver{}
	case DriverCustom:
		return customDriver{}
	}
	return nil
}

// childEnv keeps the vendor CLI from inheriting Pantau's environment (proxy credentials, secrets).
func (m *Manager) childEnv() []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + m.homeDir(), "LANG=C.UTF-8"}
	if runtime.GOOS == "windows" {
		env = []string{"PATH=" + os.Getenv("PATH"), "USERPROFILE=" + m.homeDir(), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT")}
	}
	return env
}

func (m *Manager) homeDir() string {
	if err := os.MkdirAll(m.root+"/home", 0o700); err == nil {
		return m.root + "/home"
	}
	return m.root
}

// runCmd runs a vendor binary (argv, never a shell) and returns its combined output.
func (m *Manager) runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Env = m.childEnv()
	buf := &headBuffer{limit: maxCommandOutput}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	text := strings.TrimSpace(buf.String())
	if err != nil {
		if cctx.Err() != nil {
			err = fmt.Errorf("%s timed out after %s", name, timeout)
		}
		if snippet := tail(text, maxOutput); snippet != "" {
			return text, fmt.Errorf("%s: %w: %s", name, err, snippet)
		}
		return text, fmt.Errorf("%s: %w", name, err)
	}
	return text, nil
}

// headBuffer keeps the first limit bytes and silently drops the rest.
type headBuffer struct {
	buf   []byte
	limit int
}

func (b *headBuffer) Write(p []byte) (int, error) {
	if room := b.limit - len(b.buf); room > 0 {
		if len(p) <= room {
			b.buf = append(b.buf, p...)
		} else {
			b.buf = append(b.buf, p[:room]...)
		}
	}
	return len(p), nil // never fail the child: the truncation is ours to hide
}

func (b *headBuffer) String() string { return string(b.buf) }

// tail returns the last n bytes of s, for error messages (the interesting part of a failed command).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// runShell is only reachable through the custom driver, whose command is written by an admin behind the gate.
func (m *Manager) runShell(ctx context.Context, timeout time.Duration, command string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(cctx, "sh", "-c", command)
	}
	cmd.Env = m.childEnv()
	buf := &headBuffer{limit: maxCommandOutput}
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(buf.String()), fmt.Errorf("command failed: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// ifaceReady reports whether the interface exists and carries at least one usable address,
// which is how every process-driven driver proves its tunnel came up.
func ifaceReady(name string) bool {
	if name == "" {
		return false
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil || ifi.Flags&net.FlagUp == 0 {
		return false
	}
	addrs, err := ifi.Addrs()
	if err != nil || len(addrs) == 0 {
		return false
	}
	return true
}

// jsonBody drops vendor warnings printed before the JSON document (they would break the decoder).
func jsonBody(out string) string {
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start >= 0 && end > start {
		return out[start : end+1]
	}
	return out
}

func firstLine(out string) string {
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		return strings.TrimSpace(out[:i])
	}
	return strings.TrimSpace(out)
}

func firstArgLine(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// vendorBins maps a driver to the binary it runs (the custom driver runs commands, not a binary).
// daemonBins are the processes Pantau may run for a driver (tailscaled is separate from the tailscale CLI).
var daemonBins = map[string]string{
	DriverTailscale: "tailscaled",
	DriverNetMaker:  "netclient",
}

var vendorBins = map[string]string{
	DriverNetBird:   "netbird",
	DriverNetMaker:  "netclient",
	DriverOpenVPN:   "openvpn",
	DriverWireGuard: "wg-quick",
	DriverTailscale: "tailscale",
}

// commonBinDirs covers service managers that hand Pantau a minimal PATH (systemd, containers) and
// package managers with their own prefix; without them an installed binary looks missing.
var commonBinDirs = func() []string {
	dirs := []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/local/sbin", "/usr/sbin", "/sbin", "/opt/homebrew/bin", "/snap/bin"}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
			if base := os.Getenv(env); base != "" {
				dirs = append(dirs, base+"\\NetBird", base+"\\Tailscale", base+"\\OpenVPN\\bin", base+"\\WireGuard")
			}
		}
	}
	return dirs
}()

// vendorPATH keeps the inherited PATH but always appends the standard install dirs: a service or container
// often hands Pantau a minimal PATH, and vendor CLIs are shell scripts that need coreutils too.
func vendorPATH() string {
	parts := []string{}
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		parts = append(parts, dir)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(dir)
	}
	for _, dir := range commonBinDirs {
		add(dir)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// resolveVendorBin finds the driver binary: an explicit "bin" config wins, then PATH, then the usual install
// directories. The error names the binary and where it looked, so the UI can tell the admin what to install.
func resolveVendorBin(driverName, override string) (string, error) {
	name := vendorBins[driverName]
	if name == "" {
		return "", fmt.Errorf("%s has no binary", driverName)
	}
	if override = strings.TrimSpace(override); override != "" {
		if st, err := os.Stat(override); err == nil && !st.IsDir() {
			return override, nil
		}
		return "", fmt.Errorf("%q does not exist on the Pantau server", override)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range commonBinDirs {
		candidate := filepath.Join(dir, name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
		if runtime.GOOS == "windows" {
			if st, err := os.Stat(candidate + ".exe"); err == nil && !st.IsDir() {
				return candidate + ".exe", nil
			}
		}
	}
	return "", fmt.Errorf("%s is not installed on the Pantau server: install it on this machine, or set the network's \"bin\" field to its full path (PATH=%s)", name, os.Getenv("PATH"))
}

// daemonBin resolves the daemon binary for a driver that can own one (tailscaled, netclient).
func (m *Manager) daemonBin(n Network) (string, error) {
	name := daemonBins[n.Driver]
	if name == "" {
		return "", fmt.Errorf("%s has no daemon", n.Driver)
	}
	if override := strings.TrimSpace(n.Config["bin"]); override != "" {
		// "bin" names the CLI; its daemon sits next to it unless daemon_bin says otherwise.
		if db := strings.TrimSpace(n.Config["daemon_bin"]); db != "" {
			return resolveVendorBin(n.Driver, db)
		}
		dir := filepath.Dir(override)
		candidate := filepath.Join(dir, name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	if db := strings.TrimSpace(n.Config["daemon_bin"]); db != "" {
		if st, err := os.Stat(db); err == nil && !st.IsDir() {
			return db, nil
		}
		return "", fmt.Errorf("%q does not exist on the Pantau server", db)
	}
	return resolveVendorBinByName(name, n.Driver)
}

// resolveVendorBinByName is resolveVendorBin without the driver-to-binary mapping.
func resolveVendorBinByName(name, driverName string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range commonBinDirs {
		candidate := filepath.Join(dir, name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
		if runtime.GOOS == "windows" {
			if st, err := os.Stat(candidate + ".exe"); err == nil && !st.IsDir() {
				return candidate + ".exe", nil
			}
		}
	}
	return "", fmt.Errorf("%s is not installed on the Pantau server (needed by the %s driver): install it on this machine, or set the network's \"daemon_bin\" field to its full path", name, driverName)
}

// vendorBin resolves this network's binary, honouring the per-network "bin" override.
func (m *Manager) vendorBin(n Network) (string, error) {
	if n.Driver == DriverCustom {
		return "", fmt.Errorf("the custom driver runs commands, not a binary")
	}
	return resolveVendorBin(n.Driver, n.Config["bin"])
}

// DetectBinary returns the resolved path and version of a vendor binary, for GET /api/vpn/status.
func DetectBinary(driverName string) (path, version, errText string) {
	if vendorBins[driverName] == "" {
		return "", "", ""
	}
	p, err := resolveVendorBin(driverName, "")
	if err != nil {
		return "", "", err.Error()
	}
	if runtime.GOOS == "windows" && driverName == DriverWireGuard {
		return p, "", "wg-quick is not supported on windows"
	}
	if driverName == DriverWireGuard {
		// wg-quick has no --version: it prints its usage, so the resolved path is the answer.
		return p, "", ""
	}
	args := []string{"version"}
	if driverName == DriverOpenVPN {
		args = []string{"--version"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, args...)
	cmd.Env = append(os.Environ(), "PATH="+vendorPATH())
	out, err := cmd.CombinedOutput()
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if driverName == DriverNetMaker {
		// netclient logs on every command; take a version-looking token instead of a log line.
		line = ""
		for _, candidate := range strings.Fields(string(out)) {
			if v := strings.Trim(candidate, "v,;[]()"); len(v) > 3 && strings.Count(v, ".") == 2 && v[0] >= '0' && v[0] <= '9' {
				line = v
				break
			}
		}
	}
	if err != nil {
		// netclient refuses to even print its version without root; report that instead of a fake version.
		if line == "" {
			line = err.Error()
		}
		return p, "", line
	}
	if line == "" {
		line = p
	}
	return p, line, ""
}

var errNotRunning = errors.New("tunnel process is not running")
