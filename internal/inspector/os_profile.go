package inspector

import (
	"encoding/json"
	"strings"

	"pantau/internal/store"
)

// ParseOSCapabilityProfile derives a structured OSCapabilityProfile from the
// batched inspection sections (kernel, os-release / sw_vers, and capability probe).
// It gracefully handles legacy Linux distributions and macOS without extra SSH calls.
func ParseOSCapabilityProfile(osReleaseSec, kernelSec, capSec string) store.OSCapabilityProfile {
	kv := parseCapabilityKV(capSec)
	osLower := strings.ToLower(osReleaseSec)
	kernelLower := strings.ToLower(kernelSec)

	osFamily := "linux"
	if strings.Contains(kernelLower, "darwin") ||
		strings.Contains(osLower, "macos") ||
		strings.Contains(osLower, "mac os") ||
		strings.Contains(osLower, "darwin") {
		osFamily = "darwin"
	}

	distro := detectDistroSlug(osFamily, osReleaseSec)

	initSys := normalizeInitSystem(kv["init"])
	if initSys == "" {
		initSys = fallbackInitSystem(osFamily, distro, osLower)
	}

	pkgMgr := normalizePkgMgr(kv["pkg"])
	if pkgMgr == "" {
		pkgMgr = fallbackPkgMgr(osFamily, distro, osLower)
	}

	shellFlavor := normalizeShellFlavor(kv["shell"])
	if shellFlavor == "" {
		shellFlavor = fallbackShellFlavor(osFamily, distro)
	}

	hasSudo := kv["sudo"] == "1" || strings.EqualFold(kv["sudo"], "true")

	return store.OSCapabilityProfile{
		OSFamily:        osFamily,
		Distro:          distro,
		InitSystem:      initSys,
		PkgManager:      pkgMgr,
		ShellFlavor:     shellFlavor,
		HasSudoNopasswd: hasSudo,
	}
}

// MarshalOSCapabilityProfile serializes the profile as compact JSON for storage and prompt injection.
func MarshalOSCapabilityProfile(p store.OSCapabilityProfile) string {
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func parseCapabilityKV(capSec string) map[string]string {
	out := map[string]string{}
	for _, tok := range strings.Fields(strings.TrimSpace(capSec)) {
		if k, v, ok := strings.Cut(tok, "="); ok {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.ToLower(strings.TrimSpace(v))
		}
	}
	return out
}

func detectDistroSlug(osFamily, osReleaseSec string) string {
	if osFamily == "darwin" {
		return "macos"
	}
	var id, idLike, pretty string
	for _, line := range strings.Split(osReleaseSec, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ID="):
			id = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID="), `"' `))
		case strings.HasPrefix(line, "ID_LIKE="):
			idLike = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), `"' `))
		case strings.HasPrefix(line, "PRETTY_NAME="):
			pretty = strings.ToLower(strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"' `))
		}
	}

	switch id {
	case "ubuntu":
		return "ubuntu"
	case "debian", "raspbian":
		return "debian"
	case "alpine":
		return "alpine"
	case "arch", "archlinux", "endeavouros", "manjaro":
		return "arch"
	case "rhel", "centos", "fedora", "rocky", "almalinux", "ol", "amzn", "scientific":
		return "rhel"
	case "macos":
		return "macos"
	}

	combined := strings.ToLower(id + " " + idLike + " " + pretty + " " + osReleaseSec)
	switch {
	case strings.Contains(combined, "ubuntu"):
		return "ubuntu"
	case strings.Contains(combined, "debian"):
		return "debian"
	case strings.Contains(combined, "alpine"):
		return "alpine"
	case strings.Contains(combined, "arch linux") || strings.Contains(combined, "archlinux"):
		return "arch"
	case strings.Contains(combined, "rhel") ||
		strings.Contains(combined, "red hat") ||
		strings.Contains(combined, "centos") ||
		strings.Contains(combined, "fedora") ||
		strings.Contains(combined, "rocky") ||
		strings.Contains(combined, "almalinux"):
		return "rhel"
	}
	if id != "" {
		return id
	}
	return "linux"
}

func normalizeInitSystem(s string) string {
	switch s {
	case "systemd", "openrc", "launchd", "sysv", "none":
		return s
	}
	return ""
}

func fallbackInitSystem(osFamily, distro, osLower string) string {
	if osFamily == "darwin" || distro == "macos" {
		return "launchd"
	}
	if distro == "alpine" {
		return "openrc"
	}
	if strings.Contains(osLower, "centos release 5") || strings.Contains(osLower, "centos release 6") ||
		strings.Contains(osLower, "centos 5") || strings.Contains(osLower, "centos 6") ||
		strings.Contains(osLower, "rhel 5") || strings.Contains(osLower, "rhel 6") ||
		strings.Contains(osLower, "red hat enterprise linux server release 5") ||
		strings.Contains(osLower, "red hat enterprise linux server release 6") {
		return "sysv"
	}
	switch distro {
	case "ubuntu", "debian", "rhel", "arch":
		return "systemd"
	}
	return "none"
}

func normalizePkgMgr(s string) string {
	switch s {
	case "apt", "dnf", "yum", "apk", "pacman", "brew", "none":
		return s
	case "apt-get":
		return "apt"
	}
	return ""
}

func fallbackPkgMgr(osFamily, distro, osLower string) string {
	if osFamily == "darwin" || distro == "macos" {
		return "brew"
	}
	switch distro {
	case "ubuntu", "debian":
		return "apt"
	case "alpine":
		return "apk"
	case "arch":
		return "pacman"
	case "rhel":
		if strings.Contains(osLower, "release 5") || strings.Contains(osLower, "release 6") || strings.Contains(osLower, "release 7") {
			return "yum"
		}
		return "dnf"
	}
	return "none"
}

func normalizeShellFlavor(s string) string {
	switch s {
	case "gnu", "busybox", "bsd":
		return s
	}
	return ""
}

func fallbackShellFlavor(osFamily, distro string) string {
	if osFamily == "darwin" || distro == "macos" {
		return "bsd"
	}
	if distro == "alpine" {
		return "busybox"
	}
	return "gnu"
}
