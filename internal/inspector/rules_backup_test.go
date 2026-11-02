package inspector

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestValidateBackupPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"valid exact path", "/var/backups/db.sql.gz", false},
		{"valid glob path", "/var/backups/db-*.sql.gz", false},
		{"valid question mark glob", "/var/backups/backup-?.tar", false},
		{"valid characters", "/data/backup_2026-11-02:12:00.tar.zst", false},
		{"empty path", "", true},
		{"relative path", "var/backups/db.sql", true},
		{"path traversal ..", "/var/backups/../etc/passwd", true},
		{"command injection semicolon", "/var/backups/db;rm -rf /", true},
		{"command injection dollar", "/var/backups/$(whoami)", true},
		{"command injection backtick", "/var/backups/`id`", true},
		{"space in pattern", "/var/backups/db 1.sql", true},
		{"newline in pattern", "/var/backups/db.sql\n", true},
		{"pipe in pattern", "/var/backups/db|cat", true},
		{"ampersand in pattern", "/var/backups/db&id", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBackupPattern(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBackupPattern(%q) err = %v, wantErr %v", tt.pattern, err, tt.wantErr)
			}
		})
	}
}

func TestParseBackupExpected(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantHours  float64
		wantVerify string
		wantErr    bool
	}{
		{"empty default", "", 24.0, "", false},
		{"legacy fresh_24h", "fresh_24h", 24.0, "", false},
		{"fresh_12h", "fresh_12h", 12.0, "", false},
		{"fresh_2d", "fresh_2d", 48.0, "", false},
		{"fresh_48", "fresh_48", 48.0, "", false},
		{"fresh_24h with gzip verify", "fresh_24h,verify=gzip", 24.0, "gzip", false},
		{"tar verify", "fresh_12h,verify=tar", 12.0, "tar", false},
		{"zstd verify", "fresh_24h,verify=zstd", 24.0, "zstd", false},
		{"xz verify", "fresh_24h,verify=xz", 24.0, "xz", false},
		{"sha256 verify", "fresh_24h,verify=sha256", 24.0, "sha256", false},
		{"verify only default fresh", "verify=gzip", 24.0, "gzip", false},
		{"invalid verify type", "fresh_24h,verify=zip", 0, "", true},
		{"invalid fresh hours", "fresh_abch", 0, "", true},
		{"unknown token", "fresh_24h,foo", 0, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBackupExpected(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseBackupExpected(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil {
				if got.MaxHours != tt.wantHours || got.Verify != tt.wantVerify {
					t.Errorf("got (%.1fh, %q), want (%.1fh, %q)", got.MaxHours, got.Verify, tt.wantHours, tt.wantVerify)
				}
			}
		})
	}
}

func TestBuildBackupCommand_AndInjectionSafety(t *testing.T) {
	statCmd := BuildBackupStatCommand("/var/backups/db-*.sql.gz")
	if !strings.Contains(statCmd, "ls -t -d -- /var/backups/db-*.sql.gz") {
		t.Errorf("BuildBackupStatCommand missing unquoted glob in shell: %s", statCmd)
	}

}

// The verify command is executed with a real shell: pinning its text hid a quoting bug (every verify
// reported "tool missing") because the fakes never ran it.
func TestBuildBackupVerifyCommand_RunsInShell(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	run := func(file, kind string) int {
		err := exec.Command("sh", "-c", BuildBackupVerifyCommand(file, kind)).Run()
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatal(err)
		return -1
	}
	gz := filepath.Join(dir, "it's db.sql.gz") // quote and space must survive quoting
	if err := exec.Command("sh", "-c", "echo hi | gzip > \"$1\"", "sh", gz).Run(); err != nil {
		t.Skipf("gzip unavailable: %v", err)
	}
	bad := write("bad.sql.gz", "not gzip")
	if c := run(gz, "gzip"); c != 0 {
		t.Errorf("valid gzip should verify, exit=%d", c)
	}
	if c := run(bad, "gzip"); c == 0 || c == 127 {
		t.Errorf("corrupt gzip must fail verification (not 'tool missing'), exit=%d", c)
	}
	if c := run(gz, "sha256"); c != 2 {
		t.Errorf("missing sidecar should exit 2, got %d", c)
	}
	if out, err := exec.Command("sh", "-c", "cd \"$1\" && sha256sum \"$2\" > \"$2.sha256\"", "sh", dir, filepath.Base(gz)).CombinedOutput(); err != nil {
		t.Skipf("sha256sum unavailable: %v %s", err, out)
	}
	if c := run(gz, "sha256"); c != 0 {
		t.Errorf("matching checksum should verify, exit=%d", c)
	}
	write(filepath.Base(gz)+".sha256", strings.Repeat("0", 64)+"  "+filepath.Base(gz)+"\n")
	if c := run(gz, "sha256"); c == 0 || c == 127 || c == 2 {
		t.Errorf("wrong checksum must fail, exit=%d", c)
	}
}

func TestEvaluateBackupRule_Cases(t *testing.T) {
	host := &store.Host{ID: 1, Host: "10.0.0.1"}

	// 1. Missing backup file
	mockMissing := sshrunner.NewMockRunner()
	mockMissing.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "ls: /var/backups/db-*.sql.gz: No such file or directory\n", 1, nil
	}
	ruleMissing := &store.DesiredRule{Kind: "backup", Target: "/var/backups/db-*.sql.gz", Expected: "fresh_24h,verify=gzip"}
	curr, status, _, rc := evaluateBackupRule(mockMissing, ruleMissing)
	if status != "drift" || curr != "missing" {
		t.Errorf("missing case: got %s/%s", status, curr)
	}

	// 2. Empty backup file (0 bytes)
	mockEmpty := sshrunner.NewMockRunner()
	mockEmpty.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 3600 // 1 hour ago
			return fmt.Sprintf("0 %d /var/backups/db-20261102.sql.gz\n", nowEpoch), "", 0, nil
		}
		return "", "", 1, nil
	}
	curr, status, _, rc = evaluateBackupRule(mockEmpty, ruleMissing)
	if status != "drift" || curr != "empty_file (0 bytes)" {
		t.Errorf("empty case: got %s/%s (rc: %s)", status, curr, rc)
	}

	// 3. Stale backup file (30 hours old > 24h)
	mockStale := sshrunner.NewMockRunner()
	mockStale.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 30*3600 // 30 hours ago
			return fmt.Sprintf("1258291 %d /var/backups/db-20261101.sql.gz\n", nowEpoch), "", 0, nil
		}
		return "", "", 1, nil
	}
	curr, status, _, rc = evaluateBackupRule(mockStale, ruleMissing)
	if status != "drift" || !strings.HasPrefix(curr, "stale") {
		t.Errorf("stale case: got %s/%s (rc: %s)", status, curr, rc)
	}

	// 4. Fresh backup file without verify (legacy rule compatibility)
	mockFreshLegacy := sshrunner.NewMockRunner()
	mockFreshLegacy.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 3600 // 1h ago
			return fmt.Sprintf("1258291 %d /var/backups/db-20261102.sql.gz\n", nowEpoch), "", 0, nil
		}
		return "", "", 1, nil
	}
	ruleLegacy := &store.DesiredRule{Kind: "backup", Target: "/var/backups/db-*.sql.gz", Expected: "fresh_24h"}
	curr, status, _, rc = evaluateBackupRule(mockFreshLegacy, ruleLegacy)
	if status != "ok" || !strings.HasPrefix(curr, "fresh") || !strings.Contains(curr, "1258291 B") {
		t.Errorf("fresh legacy case: got %s/%s (rc: %s)", status, curr, rc)
	}

	// 5. Fresh backup file with verify=gzip OK
	mockVerifyOK := sshrunner.NewMockRunner()
	mockVerifyOK.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 11520 // 3.2h ago
			return fmt.Sprintf("1258291 %d /var/backups/db-20261102.sql.gz\n", nowEpoch), "", 0, nil
		}
		if strings.Contains(cmd, "gzip -t") {
			return "", "", 0, nil
		}
		return "", "", 1, nil
	}
	ruleVerify := &store.DesiredRule{Kind: "backup", Target: "/var/backups/db-*.sql.gz", Expected: "fresh_24h,verify=gzip"}
	curr, status, _, rc = evaluateBackupRule(mockVerifyOK, ruleVerify)
	if status != "ok" || !strings.Contains(curr, "gzip ok") {
		t.Errorf("verify ok case: got %s/%s (rc: %s)", status, curr, rc)
	}

	// 6. Fresh backup file with verify=gzip FAIL (corrupt file)
	mockVerifyFail := sshrunner.NewMockRunner()
	mockVerifyFail.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 3600
			return fmt.Sprintf("1258291 %d /var/backups/db-20261102.sql.gz\n", nowEpoch), "", 0, nil
		}
		if strings.Contains(cmd, "gzip -t") {
			return "", "gzip: stdin: unexpected end of file\n", 1, nil
		}
		return "", "", 1, nil
	}
	curr, status, _, rc = evaluateBackupRule(mockVerifyFail, ruleVerify)
	if status != "drift" || !strings.Contains(curr, "gzip verification failed") || !strings.Contains(rc, "unexpected end of file") {
		t.Errorf("verify fail case: got %s/%s (rc: %s)", status, curr, rc)
	}

	// 7. Verify tool missing on host (gzip not found, exit 127)
	mockToolMissing := sshrunner.NewMockRunner()
	mockToolMissing.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 3600
			return fmt.Sprintf("1258291 %d /var/backups/db-20261102.sql.gz\n", nowEpoch), "", 0, nil
		}
		if strings.Contains(cmd, "gzip -t") {
			return "", "sh: gzip: command not found\n", 127, nil
		}
		return "", "", 1, nil
	}
	curr, status, _, rc = evaluateBackupRule(mockToolMissing, ruleVerify)
	if status != "drift" || !strings.Contains(curr, "gzip tool missing") || !strings.Contains(rc, "Verification tool gzip not found") {
		t.Errorf("tool missing case: got %s/%s (rc: %s)", status, curr, rc)
	}

	// 8. sha256 checksum sidecar file missing
	mockNoSidecar := sshrunner.NewMockRunner()
	mockNoSidecar.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ls -t") {
			nowEpoch := time.Now().Unix() - 3600
			return fmt.Sprintf("1258291 %d /var/backups/db-20261102.sql.gz\n", nowEpoch), "", 0, nil
		}
		if strings.Contains(cmd, "sha256sum") {
			return "", "no checksum file: /var/backups/db-20261102.sql.gz.sha256\n", 2, nil
		}
		return "", "", 1, nil
	}
	ruleSha := &store.DesiredRule{Kind: "backup", Target: "/var/backups/db-*.sql.gz", Expected: "fresh_24h,verify=sha256"}
	curr, status, _, rc = evaluateBackupRule(mockNoSidecar, ruleSha)
	if status != "drift" || !strings.Contains(curr, "no checksum file") || !strings.Contains(rc, "no checksum file") {
		t.Errorf("no sidecar case: got %s/%s (rc: %s)", status, curr, rc)
	}

	_ = host
}
