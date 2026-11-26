package ansible

import (
	"regexp"
	"strings"
)

// FailedTask represents one failed or unreachable host task extracted from an Ansible playbook run log.
type FailedTask struct {
	Play   string `json:"play,omitempty"`
	Task   string `json:"task"`
	Host   string `json:"host"`
	Status string `json:"status"` // "failed" | "unreachable"
	Error  string `json:"error"`
}

var (
	playHeaderRe    = regexp.MustCompile(`^PLAY \[([^\]]*)\]`)
	taskHeaderRe    = regexp.MustCompile(`^(?:TASK|RUNNING HANDLER) \[([^\]]*)\]`)
	hostOutcomeRe   = regexp.MustCompile(`^(fatal|failed):\s+\[([^\]]+)\]:\s*(.*)$`)
	anyTaskStatusRe = regexp.MustCompile(`^(ok|changed|skipping|included|ignoring|rescued|fatal|failed|PLAY RECAP)\b`)
)

// ParseFailedTasks extracts only failed and unreachable tasks (with their play, task, host, and error message)
// from an Ansible playbook run log, stripping ANSI color codes first.
func ParseFailedTasks(log string) []FailedTask {
	var out []FailedTask
	var curPlay, curTask string
	var cur *FailedTask
	var errBuf []string

	flush := func() {
		if cur == nil {
			return
		}
		cur.Error = strings.TrimSpace(strings.Join(errBuf, "\n"))
		out = append(out, *cur)
		cur = nil
		errBuf = nil
	}

	for _, rawLine := range strings.Split(log, "\n") {
		clean := recapANSIMatch.ReplaceAllString(rawLine, "")
		trimmed := strings.TrimSpace(clean)

		if m := playHeaderRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			curPlay = m[1]
			curTask = ""
			continue
		}
		if m := taskHeaderRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			curTask = m[1]
			continue
		}
		if m := hostOutcomeRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			rest := strings.TrimSpace(m[3])
			status := "failed"
			if strings.Contains(rest, "UNREACHABLE!") {
				status = "unreachable"
			}
			rest = strings.TrimPrefix(rest, "FAILED!")
			rest = strings.TrimPrefix(rest, "UNREACHABLE!")
			rest = strings.TrimSpace(rest)
			rest = strings.TrimPrefix(rest, "=>")
			rest = strings.TrimSpace(rest)
			cur = &FailedTask{
				Play:   curPlay,
				Task:   curTask,
				Host:   m[2],
				Status: status,
			}
			if rest != "" {
				errBuf = append(errBuf, rest)
			}
			continue
		}
		if cur != nil {
			if trimmed == "" || anyTaskStatusRe.MatchString(trimmed) {
				flush()
				continue
			}
			if len(errBuf) < 25 {
				errBuf = append(errBuf, clean)
			}
		}
	}
	flush()
	return out
}
