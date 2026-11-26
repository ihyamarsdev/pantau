package inspector

import (
	"strings"
	"testing"
)

func TestExtractDockerErrorLogs(t *testing.T) {
	raw := strings.Join([]string{
		"2026-11-26T08:00:00Z INFO server starting",
		"2026-11-26T08:00:01Z DEBUG loading config",
		"2026-11-26T08:00:02Z ERROR failed to connect to postgres: connection refused",
		"2026-11-26T08:00:03Z INFO healthcheck skipped",
		"2026-11-26T08:00:04Z FATAL panic: runtime error: nil pointer",
	}, "\n")

	lines := ExtractDockerErrorLogs(raw, 40)
	if len(lines) != 2 {
		t.Fatalf("expected 2 error lines, got %d: %+v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "ERROR failed to connect") || !strings.Contains(lines[1], "FATAL panic") {
		t.Fatalf("unexpected extracted lines: %+v", lines)
	}

	// Fallback when no error-like lines exist
	clean := "line1\nline2\nline3\n"
	fb := ExtractDockerErrorLogs(clean, 40)
	if len(fb) != 3 {
		t.Fatalf("expected 3 fallback lines, got %d", len(fb))
	}
}
