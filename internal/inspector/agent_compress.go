package inspector

// Token-saving output compression for the in-process AI agent (ADR 0086). Large log streams waste
// LLM context on repeated lines. The compressor collapses consecutive duplicate lines, redacts
// credentials, and keeps head+tail under a cap, with total line/byte metadata so the cut is honest.

import (
	"fmt"
	"strings"
)

// CompressAgentOutput collapses consecutive duplicate lines to "[repeated Nx]", redacts secrets, and
// applies head+tail truncation at max chars. The returned string carries line/byte metadata of the
// original (post-redaction) stream so the model knows what it did not see.
func CompressAgentOutput(s string, max, headFraction int) string {
	if s == "" {
		return s
	}
	s = RedactAgentSecrets(s)
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	totalLines := len(lines)
	totalBytes := len(s)

	var b strings.Builder
	prev := ""
	run := 0
	flush := func() {
		if run <= 0 {
			return
		}
		if run == 1 {
			b.WriteString(prev)
		} else {
			fmt.Fprintf(&b, "[repeated %dx] %s", run, prev)
		}
		b.WriteByte('\n')
	}
	for _, line := range lines {
		if line == prev && line != "" {
			run++
			continue
		}
		flush()
		prev = line
		run = 1
	}
	flush()
	compressed := strings.TrimRight(b.String(), "\n")

	if max > 0 && len(compressed) > max {
		compressed = TruncateAgentOutput(compressed, max, headFraction)
	}
	return fmt.Sprintf("[%d lines, %d bytes%s]\n%s", totalLines, totalBytes, collapseNote(s, compressed), compressed)
}

// collapseNote reports how many duplicate lines were collapsed, when any were.
func collapseNote(original, compressed string) string {
	origLines := strings.Count(original, "\n") + 1
	compLines := strings.Count(compressed, "\n") + 1
	if compLines < origLines {
		return fmt.Sprintf(", %d duplicate lines collapsed", origLines-compLines)
	}
	return ""
}
