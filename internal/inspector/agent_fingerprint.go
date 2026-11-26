package inspector

// Host environment fingerprint for the in-process AI agent (ADR 0086).
//
// The fingerprint is built only from SQLite host metadata already collected by Pantau — no extra SSH
// round-trip. It tells the model which OS/init/package-manager it is talking to before it writes a
// shell command, so it can pick OS-correct flags (GNU vs BSD), the right service tool and the right
// package manager. It also fast-fails a host whose bound VPN network is down (ADR 0077) instead of
// letting the agent wait out an SSH dial timeout.

import (
	"fmt"
	"strings"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// HostFingerprint is the compact execution context of one host.
type HostFingerprint struct {
	OSFamily        string // linux | darwin
	Distro          string // raw OSInfo, e.g. "Ubuntu 22.04 LTS", "macOS 26.2 (Tahoe)"
	DistroSlug      string // normalized: ubuntu | debian | rhel | alpine | arch | macos
	Arch            string // x86_64 | arm64 | aarch64 | ?
	InitSystem      string // systemd | launchd | openrc | sysv | none | ?
	PackageManager  string // apt | dnf | yum | pacman | apk | brew | none | ?
	ShellFlavor     string // gnu | busybox | bsd
	HasSudoNopasswd bool
	SudoCapability  string // how elevation works on Pantau-managed hosts (ADR 0037)
	NetworkID       int64  // bound VPN network, 0 = direct
	NetworkName     string
	NetworkState    string // direct | up | down | error | starting | unknown
	OSProfile       *store.OSCapabilityProfile
}

// vpnDownError is a fail-closed diagnostic for a host whose bound VPN path is down (ADR 0077).
type vpnDownError struct {
	Name  string
	State string
}

func (e *vpnDownError) Error() string {
	return fmt.Sprintf("%v: network %q is %s (ADR 0077); bring the VPN path up before running SSH commands on this host",
		sshrunner.ErrNetworkUnavailable, e.Name, e.State)
}

func (e *vpnDownError) Unwrap() error { return sshrunner.ErrNetworkUnavailable }

// ErrVPNDown is returned by ExecShellContext when the host's bound VPN network is down. It wraps
// sshrunner.ErrNetworkUnavailable so callers keep using errors.Is.
var ErrVPNDown = &vpnDownError{Name: "", State: "down"}

// checkVPNDown returns a typed diagnostic immediately when the host is bound to a VPN network whose
// last known state is down/error. A nil db or a direct host is a no-op. This is deliberately a fast
// metadata check: it must never dial, so a dead path cannot cost an SSH timeout.
func (ins *Inspector) checkVPNDown(hostID int64) error {
	if ins.db == nil {
		return nil
	}
	h, err := ins.db.GetHost(hostID)
	if err != nil || h == nil || h.NetworkID == 0 {
		return nil
	}
	n, err := ins.db.GetVPNNetwork(h.NetworkID)
	if err != nil || n == nil {
		return nil
	}
	state := strings.ToLower(strings.TrimSpace(n.LastState))
	if state != "down" && state != "error" {
		return nil
	}
	name := strings.TrimSpace(n.Name)
	if name == "" {
		name = fmt.Sprintf("id=%d", h.NetworkID)
	}
	return &vpnDownError{Name: name, State: state}
}

// HostFingerprint reads the host's stored metadata and derives the execution context. It never dials.
func (ins *Inspector) HostFingerprint(hostID int64) (HostFingerprint, error) {
	if ins == nil || ins.db == nil {
		return HostFingerprint{}, fmt.Errorf("host metadata is unavailable")
	}
	h, err := ins.db.GetHost(hostID)
	if err != nil {
		return HostFingerprint{}, err
	}
	if h == nil {
		return HostFingerprint{}, fmt.Errorf("host not found")
	}
	return fingerprintFromHost(ins.db, h), nil
}

// fingerprintFromHost is the pure derivation used by HostFingerprint and the unit tests. A nil db is
// allowed: the caller then gets a fingerprint from the host row alone.
func fingerprintFromHost(db *store.DB, h *store.Host) HostFingerprint {
	fp := HostFingerprint{
		Distro:         strings.TrimSpace(h.OSInfo),
		SudoCapability: "sudo -n (passwordless, ADR 0037)",
		NetworkID:      h.NetworkID,
	}
	osLower := strings.ToLower(h.OSInfo)
	kernelLower := strings.ToLower(h.Kernel)

	// Stored security scan is the most reliable source for family and package manager.
	var scanFamily, pkg string
	if db != nil {
		if scan, err := db.GetSecurityScan(h.ID); err == nil && scan != nil {
			scanFamily = strings.ToLower(strings.TrimSpace(scan.OSFamily))
			pkg = strings.ToLower(strings.TrimSpace(scan.PackageManager))
		}
	}

	fp.OSFamily = "linux"
	if strings.Contains(kernelLower, "darwin") || scanFamily == "macos" ||
		strings.Contains(osLower, "macos") || strings.Contains(osLower, "darwin") {
		fp.OSFamily = "darwin"
	}
	if fp.Distro == "" {
		fp.Distro = scanFamily
	}

	fp.Arch = archFromMetadata(h.HardwareModel, h.OSInfo)
	fp.InitSystem = initFromFamily(fp.OSFamily, scanFamily, osLower)
	fp.PackageManager = normalizePackageManager(pkg, scanFamily, fp.OSFamily)
	fp.ShellFlavor = fallbackShellFlavor(fp.OSFamily, detectDistroSlug(fp.OSFamily, h.OSInfo))
	fp.DistroSlug = detectDistroSlug(fp.OSFamily, h.OSInfo)

	if prof, ok := h.CapabilityProfile(); ok {
		if prof.OSFamily != "" {
			fp.OSFamily = prof.OSFamily
		}
		if prof.Distro != "" {
			fp.DistroSlug = prof.Distro
		}
		if prof.InitSystem != "" {
			fp.InitSystem = prof.InitSystem
		}
		if prof.PkgManager != "" {
			fp.PackageManager = prof.PkgManager
		}
		if prof.ShellFlavor != "" {
			fp.ShellFlavor = prof.ShellFlavor
		}
		fp.HasSudoNopasswd = prof.HasSudoNopasswd
		if !prof.HasSudoNopasswd {
			fp.SudoCapability = "none (sudo -n unavailable)"
		}
		fp.OSProfile = &prof
	} else {
		derived := store.OSCapabilityProfile{
			OSFamily:        fp.OSFamily,
			Distro:          fp.DistroSlug,
			InitSystem:      fp.InitSystem,
			PkgManager:      fp.PackageManager,
			ShellFlavor:     fp.ShellFlavor,
			HasSudoNopasswd: true,
		}
		fp.HasSudoNopasswd = true
		fp.OSProfile = &derived
	}

	if h.NetworkID == 0 {
		fp.NetworkState = "direct"
	} else {
		fp.NetworkState = "unknown"
		if db != nil {
			if n, err := db.GetVPNNetwork(h.NetworkID); err == nil && n != nil {
				fp.NetworkName = strings.TrimSpace(n.Name)
				fp.NetworkState = strings.ToLower(strings.TrimSpace(n.LastState))
				if fp.NetworkState == "" {
					fp.NetworkState = "unknown"
				}
			}
		}
	}
	return fp
}

// archFromMetadata infers the architecture from stored hardware/OS strings. It stays honest ("?")
// when the metadata does not carry an architecture — no SSH probe is performed here.
func archFromMetadata(hardware, osInfo string) string {
	s := strings.ToLower(hardware + " " + osInfo)
	switch {
	case strings.Contains(s, "aarch64"), strings.Contains(s, "arm64"):
		return "arm64"
	case strings.Contains(s, "apple m1"), strings.Contains(s, "apple m2"), strings.Contains(s, "apple m3"),
		strings.Contains(s, "apple m4"), strings.Contains(s, "apple m5"), strings.Contains(s, "apple m6"),
		strings.Contains(s, "apple m7"), strings.Contains(s, "apple m8"), strings.Contains(s, "apple m9"):
		return "arm64"
	case strings.Contains(s, "x86_64"), strings.Contains(s, "amd64"):
		return "x86_64"
	case strings.Contains(s, "i686"), strings.Contains(s, "i386"):
		return "i386"
	}
	return "?"
}

// initFromFamily maps the OS family/distro to its init system. Legacy CentOS 5/6 (ADR 0005/0031)
// still use SysV; macOS uses launchd (ADR 0038); Alpine uses OpenRC.
func initFromFamily(osFamily, scanFamily, osLower string) string {
	if osFamily == "darwin" || scanFamily == "macos" {
		return "launchd"
	}
	if scanFamily == "alpine" || strings.Contains(osLower, "alpine") {
		return "openrc"
	}
	if strings.Contains(osLower, "centos 5") || strings.Contains(osLower, "centos 6") ||
		strings.Contains(osLower, "rhel 5") || strings.Contains(osLower, "rhel 6") ||
		strings.Contains(osLower, "release 5") || strings.Contains(osLower, "release 6") {
		return "sysv"
	}
	if scanFamily == "ubuntu" || scanFamily == "debian" || scanFamily == "rhel" || scanFamily == "arch" {
		return "systemd"
	}
	if osFamily == "linux" && osLower != "" {
		return "systemd"
	}
	return "?"
}

// normalizePackageManager prefers the stored scan value and falls back to the OS family.
func normalizePackageManager(pkg, scanFamily, osFamily string) string {
	if pkg != "" && pkg != "unknown" {
		if pkg == "softwareupdate" {
			return "brew"
		}
		return pkg
	}
	switch scanFamily {
	case "ubuntu", "debian":
		return "apt"
	case "alpine":
		return "apk"
	case "arch":
		return "pacman"
	case "rhel":
		return "dnf"
	case "macos":
		return "brew"
	}
	if osFamily == "darwin" {
		return "brew"
	}
	return "?"
}

// FormatHostFingerprint renders one compact, key=value line for the model. Values with spaces are
// quoted. A down VPN path is called out inline so the model does not attempt SSH on a dead route.
func FormatHostFingerprint(fp HostFingerprint) string {
	distro := fp.Distro
	if distro == "" {
		distro = "?"
	}
	if strings.ContainsAny(distro, " \t") {
		distro = `"` + distro + `"`
	}
	net := fp.NetworkState
	if net == "" {
		net = "?"
	}
	if fp.NetworkID != 0 && fp.NetworkName != "" && net != "direct" {
		net = fp.NetworkName + ":" + net
	}
	line := fmt.Sprintf("os=%s distro=%s arch=%s init=%s pkg=%s shell=%s sudo=%s net=%s",
		orUnknown(fp.OSFamily), distro, orUnknown(fp.Arch), orUnknown(fp.InitSystem),
		orUnknown(fp.PackageManager), orUnknown(fp.ShellFlavor), orUnknown(fp.SudoCapability), net)
	if net == "down" || strings.HasSuffix(net, ":down") || net == "error" || strings.HasSuffix(net, ":error") {
		line += " [VPN DOWN: do not SSH; ask the operator to bring the path up]"
	}
	return line
}

func orUnknown(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	return s
}
