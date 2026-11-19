package web

// Host fingerprint context injection for the in-process agent (ADR 0086). The system prompt carries a
// compact, token-efficient line per host the question is about, so the model picks OS-correct commands
// on the first try instead of probing and failing.

import (
	"fmt"
	"strings"

	"pantau/internal/inspector"
)

// agentFingerprintPrompt renders the fingerprint section appended to the system message. It is empty
// when the question names no host, so a fleet-wide question costs no extra tokens.
func (s *Server) agentFingerprintPrompt(question string) string {
	scope := s.memoryScopeOf(question)
	if scope.HostID == nil {
		return ""
	}
	fp, err := s.inspector.HostFingerprint(*scope.HostID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Host Execution Fingerprint (from stored metadata; no SSH was run)\n")
	fmt.Fprintf(&b, "host_id=%d %s\n", *scope.HostID, inspector.FormatHostFingerprint(fp))
	b.WriteString("Use this to choose OS-correct flags: GNU vs BSD tools, the listed init system and package manager, " +
		"and `sudo -n` for elevation. Interactive TUI/pager commands are refused; prefer the structured Pantau tools.\n")
	return b.String()
}
