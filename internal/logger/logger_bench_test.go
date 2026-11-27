package logger

import "testing"

// BenchmarkRedactString measures the per-log-line sanitizer that runs on every emitted message.
func BenchmarkRedactString(b *testing.B) {
	const line = "inspection finished for host web-1 in 12ms: 4 rules checked, 0 drift, uptime 10 days"
	b.ReportAllocs()
	for b.Loop() {
		_ = RedactString(line)
	}
}

// BenchmarkBroadcast measures the ring-buffer write that every activity event goes through.
func BenchmarkBroadcast(b *testing.B) {
	bc := NewBroadcaster(100)
	evt := Event{Message: "inspection finished", Attrs: map[string]any{"host": "web-1", "rules": 4}}
	b.ReportAllocs()
	for b.Loop() {
		bc.Broadcast(evt)
	}
}
