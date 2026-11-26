package web

// Host fingerprint context injection for the in-process agent (ADR 0086). The system prompt carries a
// compact, token-efficient line per host the question is about, so the model picks OS-correct commands
// on the first try instead of probing and failing.

import (
	"encoding/json"
	"fmt"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// agentFingerprintPrompt renders the fingerprint and OS capability profile section appended to the
// system message. It is empty when neither the question nor the thread targets a host.
func (s *Server) agentFingerprintPrompt(question string, threadID ...int64) string {
	var hostID int64
	if scope := s.memoryScopeOf(question); scope.HostID != nil {
		hostID = *scope.HostID
	} else if len(threadID) > 0 && threadID[0] > 0 && s.db != nil {
		if th, err := s.db.GetAgentThread(threadID[0]); err == nil && th != nil && th.ScopeType == "host" && th.ScopeID > 0 {
			hostID = th.ScopeID
		}
	}
	if hostID == 0 {
		return ""
	}
	fp, err := s.inspector.HostFingerprint(hostID)
	if err != nil {
		return ""
	}
	prof := store.OSCapabilityProfile{
		OSFamily:        fp.OSFamily,
		Distro:          fp.DistroSlug,
		InitSystem:      fp.InitSystem,
		PkgManager:      fp.PackageManager,
		ShellFlavor:     fp.ShellFlavor,
		HasSudoNopasswd: fp.HasSudoNopasswd,
	}
	if fp.OSProfile != nil {
		prof = *fp.OSProfile
	}
	profJSON, _ := json.Marshal(prof)

	var b strings.Builder
	b.WriteString("## Host Execution Fingerprint (from stored metadata; no SSH was run)\n")
	fmt.Fprintf(&b, "host_id=%d %s\n", hostID, inspector.FormatHostFingerprint(fp))
	b.WriteString("<host_os_capability_profile>\n")
	b.Write(profJSON)
	b.WriteString("\n</host_os_capability_profile>\n")
	b.WriteString("Mandatory OS & Syntax Rules:\n" +
		"- Treat <host_os_capability_profile> as the authoritative target environment; match every command, binary, and flag to it.\n" +
		"- Init system: NEVER use `systemctl` on `openrc` (use `rc-service <svc> status` / `rc-update`), `launchd` (use `launchctl`), `sysv` (use `service <svc> status`), or `none`.\n" +
		"- Shell & userland flavor: NEVER use GNU-only flags (e.g. `stat -c`, `sed -i`, `date -d`, `grep -P`, `ps --sort`) on `bsd` (macOS: use `stat -f`, `sed -i ''`, `date -r`, `ps -Ao`) or `busybox` (Alpine: BusyBox applets lack GNU long flags).\n" +
		"- Package manager: use ONLY the listed `pkg_manager` (`apt`, `dnf`, `yum`, `apk`, `pacman`, `brew`).\n" +
		"- Privilege: use `sudo -n` only when `has_sudo_nopasswd` is true; when false, do not assume passwordless sudo.\n" +
		"- Interactive TUI/pager commands are refused; prefer the structured Pantau tools.\n")
	return b.String()
}
