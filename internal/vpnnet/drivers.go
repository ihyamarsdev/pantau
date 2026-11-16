package vpnnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
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

// DetectBinary returns the resolved path and version of a vendor binary, for GET /api/vpn/status.
func DetectBinary(driverName string) (path, version, errText string) {
	bin := map[string]string{
		DriverNetBird:   "netbird",
		DriverNetMaker:  "netclient",
		DriverOpenVPN:   "openvpn",
		DriverWireGuard: "wg-quick",
		DriverTailscale: "tailscale",
	}[driverName]
	if bin == "" {
		return "", "", ""
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return "", "", bin + " not installed"
	}
	args := []string{"version"}
	if driverName == DriverOpenVPN {
		args = []string{"--version"}
	}
	if driverName == DriverWireGuard {
		args = []string{"--version"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, args...).CombinedOutput()
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
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
	if runtime.GOOS == "windows" && driverName == DriverWireGuard {
		return p, line, "wg-quick is not supported on windows"
	}
	return p, line, ""
}

var errNotRunning = errors.New("tunnel process is not running")
