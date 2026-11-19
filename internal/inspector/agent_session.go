package inspector

// Stateful per-session working directory and single-round-trip batch read-only probes (ADR 0086).
// Both are in-memory and keyed by chat session, so they disappear on restart like the conversation
// history (ADR 0073). Nothing here crosses a host boundary: cwd is tracked per host_id.

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// AgentSessionState holds the mutable execution state of one agent chat session.
type AgentSessionState struct {
	mu      sync.Mutex
	cwd     map[int64]string // host_id -> absolute working directory
	lastUse time.Time
}

// NewAgentSessionState creates an empty state.
func NewAgentSessionState() *AgentSessionState {
	return &AgentSessionState{cwd: map[int64]string{}}
}

// SetCWD records the working directory for a host after a successful `cd`. Only absolute paths are
// accepted: a relative cd would compound across calls and drift from the real directory.
func (s *AgentSessionState) SetCWD(hostID int64, dir string) error {
	if !strings.HasPrefix(dir, "/") {
		return fmt.Errorf("working directory must be absolute: %q", dir)
	}
	if strings.ContainsAny(dir, "\x00\n\r") {
		return fmt.Errorf("invalid working directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cwd == nil {
		s.cwd = map[int64]string{}
	}
	s.cwd[hostID] = dir
	s.lastUse = time.Now()
	return nil
}

// GetCWD returns the stored working directory for a host, or "" when none is set.
func (s *AgentSessionState) GetCWD(hostID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd[hostID]
}

// ClearCWD forgets the working directory of one host (e.g. after a `cd` failure or on New chat).
func (s *AgentSessionState) ClearCWD(hostID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cwd, hostID)
}

// cwdPrefix builds the shell fragment that makes a command run relative to the session cwd. It cd's
// into the stored directory (failing closed if it no longer exists) and then runs the command. When no
// cwd is set it returns an empty string.
func (s *AgentSessionState) cwdPrefix(hostID int64) string {
	dir := s.GetCWD(hostID)
	if dir == "" {
		return ""
	}
	return "cd " + shellQuote(dir) + " 2>/dev/null || exit 127; "
}

// cdExtractor recognizes a leading `cd <path>` and captures the path (single/double quoted or bare).
var cdExtractor = regexp.MustCompile(`^\s*cd\s+('(?:[^']|'\\'')*'|"[^"]*"|\S+)\s*(?:&&|;)?\s*`)

// extractCD returns the target of a leading `cd` command, or "" when the command does not start with cd.
func extractCD(cmd string) string {
	m := cdExtractor.FindStringSubmatch(cmd)
	if len(m) < 2 {
		return ""
	}
	raw := m[1]
	if len(raw) >= 2 && ((raw[0] == '\'' && raw[len(raw)-1] == '\'') || (raw[0] == '"' && raw[len(raw)-1] == '"')) {
		raw = raw[1 : len(raw)-1]
	}
	return strings.ReplaceAll(raw, `\'`, `'`)
}

// BatchInspectResult is one labeled section of a batch read-only probe.
type BatchInspectResult struct {
	Label    string
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
}

// BuildBatchScript joins several read-only commands into one POSIX shell script with unique markers.
// Each command's stdout and stderr are captured under its label so the model gets cleanly delimited
// sections from a single SSH round-trip. The script is fail-tolerant: one failed probe does not abort
// the rest, and each section carries the real exit code of its command.
func BuildBatchScript(commands []string, labels []string) (script string, names []string) {
	if len(commands) == 0 {
		return "", nil
	}
	const marker = "PB9F3A"
	var b strings.Builder
	for i, cmd := range commands {
		label := fmt.Sprintf("PROBE%d", i+1)
		if i < len(labels) && strings.TrimSpace(labels[i]) != "" {
			label = sanitizeLabel(labels[i])
		}
		names = append(names, label)
		fmt.Fprintf(&b, "printf '\\n===%s_BEGIN_%s===\\n'; ", marker, label)
		fmt.Fprintf(&b, "{ %s; } 2>&1; __pantau_rc=$?; ", strings.TrimSpace(cmd))
		fmt.Fprintf(&b, "printf '\\n===%s_END_%s_RC=%%s===\\n' \"$__pantau_rc\"; ", marker, label)
	}
	return b.String(), names
}

// batchStartRe / batchEndRe match the distinct section markers emitted by a batch script.
var (
	batchStartRe = regexp.MustCompile(`(?m)^===PB9F3A_BEGIN_(.+?)===\s*$`)
	batchEndRe   = regexp.MustCompile(`(?m)^===PB9F3A_END_(.+?)_RC=(-?\d+)===\s*$`)
)

// ParseBatchOutput splits batch output into labeled sections keyed by label. The model sees one
// section per probe with its exit code, so a failing probe is obvious without a second round-trip.
func ParseBatchOutput(out string) map[string]BatchInspectResult {
	res := map[string]BatchInspectResult{}
	for _, end := range batchEndRe.FindAllStringSubmatch(out, -1) {
		label, code := end[1], 0
		fmt.Sscanf(end[2], "%d", &code)
		res[label] = BatchInspectResult{Label: label, ExitCode: code}
	}
	starts := batchStartRe.FindAllStringSubmatchIndex(out, -1)
	for i, loc := range starts {
		label := out[loc[2]:loc[3]]
		bodyStart := loc[1]
		bodyEnd := len(out)
		if i+1 < len(starts) {
			bodyEnd = starts[i+1][0]
		}
		body := strings.TrimSpace(out[bodyStart:bodyEnd])
		// Drop the trailing end marker of this section from the body when present.
		if endLoc := batchEndRe.FindStringIndex(body); endLoc != nil {
			body = strings.TrimSpace(body[:endLoc[0]])
		}
		r := res[label]
		r.Label = label
		r.Stdout = body
		res[label] = r
	}
	return res
}

// sanitizeLabel keeps a model-supplied label from producing a confusing or colliding marker.
func sanitizeLabel(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "PROBE"
	}
	return out
}
