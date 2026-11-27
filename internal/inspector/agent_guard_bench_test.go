package inspector

import (
	"strings"
	"testing"
)

// BenchmarkRedactAgentSecrets measures the filter that every remote command output passes through
// before it reaches the model: mostly clean lines with a few secret-bearing ones.
func BenchmarkRedactAgentSecrets(b *testing.B) {
	clean := "web-1 nginx active running since 2026-11-20, 4 workers, load 0.12\n"
	secret := "DATABASE_PASSWORD=hunter2hunter2 and Authorization: Bearer abcdefgh12345678\n"
	out := strings.Repeat(clean, 40) + strings.Repeat(secret, 4)
	b.SetBytes(int64(len(out)))
	b.ReportAllocs()
	for b.Loop() {
		_ = RedactAgentSecrets(out)
	}
}
