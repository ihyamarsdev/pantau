package inspector

// Pre-flight SSH command linting and anti-hang guardrails for the in-process AI agent (ADR 0086).
// A model-supplied command is checked before it ever reaches sshrunner: interactive TUI/pager commands
// that hang a non-interactive exec channel are refused with an actionable hint, sudo is forced to
// non-interactive mode, and shell arguments are single-quoted (ADR 0031).

import (
	"fmt"
	"regexp"
	"strings"
)

// interactiveBlock describes one refused interactive command and the non-interactive alternative the
// model should use instead. Structured Pantau tools are preferred over raw shell wherever one exists.
type interactiveBlock struct {
	name string
	// match reports whether this command (already tokenized, lowercased) is the interactive form.
	match func(tokens []string, cmd string) bool
	hint  string
}

// shellWords splits a command into the leading command words of every pipeline/&&/;/|| segment. It is
// deliberately simple: enough to find the executable name and its batch flags, not a full shell parser.
func shellWords(cmd string) [][]string {
	segments := regexp.MustCompile(`[;&|]+|&&|\|\|`).Split(cmd, -1)
	out := make([][]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// Drop common prefixes that wrap a command but do not change whether it is interactive.
		seg = regexp.MustCompile(`^(?:sudo\s+(?:-n\s+)?(?:-u\s+\S+\s+)?|env\s+[\w=]+\s+)+`).ReplaceAllString(seg, "")
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields)
	}
	return out
}

func firstWord(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return strings.ToLower(strings.Trim(tokens[0], `"'`))
}

func hasFlag(tokens []string, flags ...string) bool {
	for _, t := range tokens[1:] {
		for _, f := range flags {
			if t == f || strings.HasPrefix(t, f+"=") {
				return true
			}
		}
	}
	return false
}

// interactiveBlocks are the commands known to hang a non-interactive SSH exec channel waiting for a
// TTY. `top` is only blocked without its batch (-b) or logging (-l) flag.
var interactiveBlocks = []interactiveBlock{
	{name: "vim", match: func(t []string, _ string) bool { return firstWord(t) == "vim" },
		hint: "vim needs a TTY. Use `sed -i` / `printf >` for edits, or Pantau's read_file and file manager."},
	{name: "vi", match: func(t []string, _ string) bool { return firstWord(t) == "vi" },
		hint: "vi needs a TTY. Use `sed -i` / `printf >` for edits, or Pantau's read_file and file manager."},
	{name: "nano", match: func(t []string, _ string) bool { return firstWord(t) == "nano" },
		hint: "nano needs a TTY. Use `sed -i` or Pantau's file manager for edits."},
	{name: "less", match: func(t []string, _ string) bool { return firstWord(t) == "less" },
		hint: "less is a pager. Use `cat`, `head`, `tail` or Pantau's read_file instead."},
	{name: "more", match: func(t []string, _ string) bool { return firstWord(t) == "more" },
		hint: "more is a pager. Use `cat`, `head`, `tail` or Pantau's read_file instead."},
	{name: "man", match: func(t []string, _ string) bool { return firstWord(t) == "man" },
		hint: "man pages page through a TTY. Use `man <topic> | col -b | head -100` or check the tool's `--help`."},
	{name: "htop", match: func(t []string, _ string) bool { return firstWord(t) == "htop" },
		hint: "htop is interactive. Use `ps -eo pid,user,%cpu,%mem,comm --sort=-%cpu | head -20` or Pantau's host_info processes."},
	{name: "top", match: func(t []string, _ string) bool {
		w := firstWord(t)
		return w == "top" && !hasFlag(t, "-b", "-l", "-batch")
	}, hint: "top is interactive. Use `top -b -n 1 | head -30` (batch), or Pantau's host_info processes."},
}

// LintInteractiveCommand returns an actionable hint when cmd would hang a non-interactive SSH session,
// or the empty string when it is safe to send. It classifies per pipeline segment so a legitimate
// `grep ... | less` is still caught but `echo top` is not.
func LintInteractiveCommand(cmd string) string {
	lower := strings.ToLower(cmd)
	for _, seg := range shellWords(lower) {
		for _, b := range interactiveBlocks {
			if b.match(seg, lower) {
				return fmt.Sprintf("interactive command %q is not safe over non-interactive SSH: %s", b.name, b.hint)
			}
		}
	}
	return ""
}

// sudoNoInteractive rewrites a leading `sudo` to `sudo -n` when the command uses sudo without the
// non-interactive flag. It never touches a `sudo` that already carries -n/-k/-S and never rewrites a
// sudo buried in a quoted string or in the middle of a word. Elevation follows ADR 0037 (passwordless).
func sudoNoInteractive(cmd string) string {
	// Match a bare `sudo` at the start of a command segment. A sudo already followed by a flag
	// (-n/-k/-S/-u/...) is left alone. Go regexp has no lookahead, so inspect the next char manually.
	re := regexp.MustCompile(`(^|[;&|]\s*)sudo(\s+|$)`)
	matches := re.FindAllStringSubmatchIndex(cmd, -1)
	if len(matches) == 0 {
		return cmd
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		// m[0]:m[1] full match; m[2]:m[3] separator; "sudo" ends at m[3]; m[4]:m[5] trailing whitespace.
		sudoEnd := m[3]
		next := m[5]
		if next < len(cmd) && cmd[next] == '-' {
			continue // a flag follows: already non-interactive (or explicitly configured)
		}
		b.WriteString(cmd[last:sudoEnd])
		b.WriteString("sudo -n")
		last = sudoEnd + 4 // skip the original "sudo"
	}
	b.WriteString(cmd[last:])
	return b.String()
}

// exitHint returns a structured recovery hint for a non-zero exit code or a context/transport error.
// The hint helps the model choose the next tool call instead of repeating the same failing command.
func exitHint(exitCode int, err error, cmd string) string {
	if err != nil {
		msg := strings.ToLower(err.Error())
		switch {
		case strings.Contains(msg, "deadline"), strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"):
			return "the command hit its execution deadline: narrow the filter, add a head/tail limit, or split the probe into smaller read-only commands"
		case strings.Contains(msg, "network unavailable"), strings.Contains(msg, "network down"):
			return "the host's VPN path is down (ADR 0077): ask the operator to bring the network up; do not retry the SSH command"
		}
	}
	switch exitCode {
	case 127:
		return "command not found (127): check PATH with `command -v <bin>` or use the OS-correct binary — Homebrew tools need the extended PATH on macOS (ADR 0038), legacy hosts lack modern flags (ADR 0005)"
	case 126:
		return "permission denied (126): the binary is not executable for the SSH user; try `sudo -n` (ADR 0037) or an elevated Pantau tool if permitted"
	case 124:
		return "timed out (124): narrow the filter or use a smaller tail/range so the probe finishes within its deadline"
	case 1:
		msg := strings.ToLower(cmd + " " + errText(err))
		if strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted") {
			return "permission denied: use `sudo -n` (ADR 0037) or the matching elevated Pantau tool if the operator permits it"
		}
		return "command failed (1): read stderr for the cause; re-check the command against the host's OS fingerprint before retrying"
	case -1:
		return "the command did not produce an exit code (transport or context error): check reachability and the VPN path, then retry once with a narrower probe"
	default:
		return fmt.Sprintf("command exited %d: read stderr for the cause; narrow the probe before retrying", exitCode)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
