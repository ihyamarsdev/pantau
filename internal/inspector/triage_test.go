package inspector

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestFastPathTriage(t *testing.T) {
	cases := []struct {
		name   string
		job    TriageJob
		want   bool
		action string
	}{
		{"oom killed", TriageJob{AlertType: "service", Target: "nginx.service", RootCause: "container was OOMKilled", ExitStatus: "137"}, true, "systemd_restart"},
		{"exit 137", TriageJob{AlertType: "service", Target: "api.service", ExitStatus: "exit code 137"}, true, "systemd_restart"},
		{"container oom", TriageJob{AlertType: "container", Target: "redis", Logs: "Killed process 123 (redis) due to OOM"}, true, "docker_restart"},
		{"disk full", TriageJob{AlertType: "disk", Target: "/", RootCause: "write failed: no space left on device"}, true, ""},
		{"disk 97 percent", TriageJob{AlertType: "disk", Target: "/var", Logs: "disk usage 97% on /var"}, true, ""},
		{"expired tls", TriageJob{AlertType: "tls", Target: "example.com", RootCause: "certificate has expired"}, true, ""},
		{"stale backup", TriageJob{AlertType: "backup", Target: "db.sql.gz", RootCause: "backup is 3 days old"}, true, ""},
		{"unknown", TriageJob{AlertType: "port", Target: "8080", RootCause: "connection refused on port 8080"}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FastPathTriage(tc.job)
			if tc.want && got == nil {
				t.Fatalf("expected a fast-path diagnosis, got nil")
			}
			if !tc.want && got != nil {
				t.Fatalf("expected no fast-path diagnosis, got %+v", got)
			}
			if got != nil {
				if got.Source != "fast_path" {
					t.Errorf("source = %q, want fast_path", got.Source)
				}
				if got.TokensUsed != 0 {
					t.Errorf("fast path must use 0 tokens, got %d", got.TokensUsed)
				}
				if got.RemediationAction != tc.action {
					t.Errorf("action = %q, want %q", got.RemediationAction, tc.action)
				}
			}
		})
	}
}

func TestErrorSignatureNormalization(t *testing.T) {
	a := ErrorSignature("service", "nginx.service", "Failed at /var/log/app.log on 2026-01-02 12:34:56")
	b := ErrorSignature("service", "nginx.service", "Failed at /tmp/other.log on 2026-05-09 01:02:03")
	if a != b {
		t.Fatalf("volatile details should not change signature: %q vs %q", a, b)
	}
	c := ErrorSignature("disk", "nginx.service", "Failed at /var/log/app.log on 2026-01-02 12:34:56")
	if a == c {
		t.Fatalf("different alert types must produce different signatures")
	}
	if len(a) != 64 {
		t.Fatalf("signature should be SHA-256 hex (64 chars), got %d", len(a))
	}
}

func TestBuildDiagnosticBundleTruncation(t *testing.T) {
	// 40 error lines, each ~100 chars: far beyond the 20-line / 1500-char caps.
	var logs strings.Builder
	for range 40 {
		logs.WriteString(strings.Repeat("error detail ", 8) + "\n")
	}
	bundle := BuildDiagnosticBundle(logs.String(), "137", "nginx 90% mem")
	if !strings.Contains(bundle, "exit_status: 137") {
		t.Error("bundle must carry exit status")
	}
	if !strings.Contains(bundle, "top_consumers: nginx 90% mem") {
		t.Error("bundle must carry top consumers")
	}
	// The error_logs section is capped at 1500 chars.
	idx := strings.Index(bundle, "error_logs:\n")
	logsPart := bundle[idx+len("error_logs:\n"):]
	if len([]rune(logsPart)) > triageMaxLogChars {
		t.Fatalf("log bundle = %d chars, want <= %d", len([]rune(logsPart)), triageMaxLogChars)
	}
	if !strings.HasSuffix(logsPart, "…") {
		t.Error("truncated log bundle must end with an ellipsis")
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("abcdef", 3); got != "ab…" {
		t.Errorf("Truncate = %q, want ab…", got)
	}
	if got := Truncate("abc", 10); got != "abc" {
		t.Errorf("Truncate of short string = %q, want abc", got)
	}
}

// fakeTriageLLM records the prompt and returns a fixed diagnosis. It fails the
// test if the worker ever sends tool definitions (the worker interface has none).
type fakeTriageLLM struct {
	calls      int
	lastMax    int
	lastPrompt string
}

func (f *fakeTriageLLM) Triage(_ context.Context, _ string, _ string, prompt string, maxTokens int) (TriageDiagnosis, int, error) {
	f.calls++
	f.lastPrompt = prompt
	f.lastMax = maxTokens
	return TriageDiagnosis{RootCause: "unknown root", Impact: "impact", Remediation: "fix it", Source: "llm"}, 120, nil
}

func TestTriageWorkerSignatureCacheHitSkipsLLM(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed a diagnosis from 1 hour ago with a known signature.
	sig := ErrorSignature("service", "nginx.service", "connection refused")
	_, err = db.SaveTriageResult(&store.TriageResult{
		Signature: sig, AlertID: 1, HostID: 1, AlertType: "service", Unit: "nginx.service",
		Source: "llm", RootCause: "cached cause", Impact: "cached impact", Remediation: "cached fix",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Backdate the row so the 24h window applies.
	if _, err := db.Exec(`UPDATE agent_triage SET created_at = ? WHERE signature = ?`, time.Now().Add(-time.Hour), sig); err != nil {
		t.Fatal(err)
	}

	llm := &fakeTriageLLM{}
	w := NewTriageWorker(db, llm)
	w.process(triageJob{
		signature: sig,
		job: TriageJob{
			AlertID: 2, HostID: 1, HostName: "h1", AlertType: "service", Target: "nginx.service",
			RootCause: "connection refused",
		},
	})

	if llm.calls != 0 {
		t.Fatalf("cache hit must not call the LLM, got %d calls", llm.calls)
	}
	got, err := db.LatestTriageForAlert(2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "cache" || got.RootCause != "cached cause" {
		t.Errorf("cache hit result = %+v", got)
	}
	if got.TokensUsed != 0 {
		t.Errorf("cache hit must use 0 tokens, got %d", got.TokensUsed)
	}
}

func TestTriageWorkerLLMOnceWithTokenCap(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	llm := &fakeTriageLLM{}
	w := NewTriageWorker(db, llm)
	w.process(triageJob{
		signature: ErrorSignature("port", "8080", "unexpected handshake timeout"),
		job: TriageJob{
			AlertID: 3, HostID: 1, HostName: "h1", AlertType: "port", Target: "8080",
			RootCause: "unexpected handshake timeout", Logs: "error: unexpected handshake timeout",
		},
	})

	if llm.calls != 1 {
		t.Fatalf("unknown failure must call the LLM exactly once, got %d", llm.calls)
	}
	if llm.lastMax != triageMaxTokens {
		t.Errorf("maxTokens = %d, want %d", llm.lastMax, triageMaxTokens)
	}
	if !strings.Contains(llm.lastPrompt, "JSON") {
		t.Error("prompt must request a JSON response")
	}
	got, err := db.LatestTriageForAlert(3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "llm" || got.TokensUsed != 120 {
		t.Errorf("llm result = %+v", got)
	}
	spent, _ := db.TriageBudgetDay(time.Now().UTC().Format("2006-01-02"))
	if spent != 120 {
		t.Errorf("daily budget = %d, want 120", spent)
	}
}

func TestBriefingAllNominalUsesNoLLM(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	llm := &fakeTriageLLM{}
	svc := NewBriefingService(db, llm)
	b, err := svc.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.AnomalyCount != 0 {
		t.Fatalf("fresh DB should have no anomalies, got %d", b.AnomalyCount)
	}
	if b.TokensUsed != 0 {
		t.Errorf("all-nominal briefing must use 0 tokens, got %d", b.TokensUsed)
	}
	if !strings.Contains(b.Summary, "All systems nominal") {
		t.Errorf("summary = %q", b.Summary)
	}
	if llm.calls != 0 {
		t.Errorf("all-nominal briefing must not call the LLM, got %d calls", llm.calls)
	}
	stored, err := db.LatestBriefing()
	if err != nil || stored == nil || stored.Summary != b.Summary {
		t.Fatalf("briefing not persisted: %+v, err=%v", stored, err)
	}
}
