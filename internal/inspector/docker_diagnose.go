package inspector

import (
	"regexp"
	"strings"
)

var dockerErrorLineRe = regexp.MustCompile(`(?i)\b(err(?:or)?|warn(?:ing)?|fatal|panic|exception|fail(?:ed|ure)?|traceback|oom|killed|segfault|refused|timeout|timed out|denied)\b`)

// ExtractDockerErrorLogs filters container logs for recent error/warning lines.
// If no error-like lines are found, it falls back to the last fallbackLines lines so
// the caller still sees recent container output without full-log head/tail truncation.
func ExtractDockerErrorLogs(raw string, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = 40
	}
	raw = strings.TrimRight(raw, "\r\n")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	all := strings.Split(raw, "\n")
	var matched []string
	for _, line := range all {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if dockerErrorLineRe.MatchString(trimmed) {
			matched = append(matched, line)
		}
	}
	if len(matched) > 0 {
		if len(matched) > maxLines {
			return matched[len(matched)-maxLines:]
		}
		return matched
	}
	fallback := maxLines
	if fallback > 20 {
		fallback = 20
	}
	if len(all) > fallback {
		return all[len(all)-fallback:]
	}
	return all
}
