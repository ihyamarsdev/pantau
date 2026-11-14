package inspector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// TestFIMAcceptance walks the whole rule lifecycle: a captured baseline evaluates to ok,
// a changed file hash evaluates to drift naming the file, and an unreadable file evaluates
// to not-assessed (never ok).
func TestFIMAcceptance(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-fim-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	manifest := "FIM:OK /etc/passwd " + hashA + "\nFIM:DONE\n"
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		return manifest, "", 0, nil
	}
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)
	hostID, _ := db.CreateHost(&store.Host{Name: "Test", Host: "192.168.1.10", User: "root", Port: 22})
	host, _ := db.GetHost(hostID)

	// 1. Capture the baseline from the host state.
	capture, err := ins.CaptureFIMManifest(hostID, runner, "/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := EncodeFIMBaseline(FIMCaptureFiles(capture))
	if err != nil {
		t.Fatal(err)
	}

	// 2. Store it as a rule.
	rule := &store.DesiredRule{HostID: hostID, Kind: "file_integrity", Target: "/etc/passwd", Expected: expected, Enabled: true}
	if err := db.SaveDesiredRule(rule); err != nil {
		t.Fatal(err)
	}

	// 3. Unchanged -> ok.
	curr, status, _, _ := ins.evaluateRule(host, runner, rule)
	if status != "ok" {
		t.Fatalf("expected ok, got %s (%s)", status, curr)
	}

	// 4. The file hash changes -> drift naming the file.
	manifest = "FIM:OK /etc/passwd " + hashB + "\nFIM:DONE\n"
	curr, status, details, rootCause := ins.evaluateRule(host, runner, rule)
	if status != "drift" {
		t.Fatalf("expected drift, got %s (%s)", status, curr)
	}
	if !strings.Contains(details, "/etc/passwd") {
		t.Fatalf("drift must name the changed file: %s", details)
	}
	if !strings.Contains(rootCause, "/etc/passwd") {
		t.Fatalf("root cause must name the changed file: %s", rootCause)
	}

	// 5. The file cannot be read -> not-assessed, never ok.
	manifest = "FIM:DENIED /etc/passwd\nFIM:DONE\n"
	curr, status, _, _ = ins.evaluateRule(host, runner, rule)
	if status != "not_assessed" {
		t.Fatalf("an unreadable file must be not_assessed, got %s (%s)", status, curr)
	}
}
