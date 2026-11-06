package inspector

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// benchMockExec answers a healthy Linux host: metrics batch, df, service status, crontab.
func benchMockExec(cmd string) (string, string, int, error) {
	if strings.Contains(cmd, "uname -r") {
		parts := []string{
			"5.15.0-generic", "---",
			"NAME=\"Ubuntu\"\nVERSION=\"22.04 LTS\"", "---",
			"up 10 days, 1 user, load average: 0.10, 0.20, 0.15", "---",
			"Mem: 8000000000 4000000000 4000000000", "---",
			"/dev/sda1 100000 50000 50000 50% /", "---",
			"0", "---",
			"1000 1000", "---",
			"time=12.5", "---",
			"1.1.1.1", "---",
			"127.0.0.1:80 127.0.0.1:12345", "---",
			"LISTEN 127.0.0.1:80", "---",
			"0", "---",
			"4", "---",
			"01/01/2022", "---",
			"KVM", "---",
			"1640995200",
		}
		return strings.Join(parts, "\n"), "", 0, nil
	}
	if strings.Contains(cmd, "df -") {
		return "/dev/sda1 100000 50000 50000 50% /\n", "", 0, nil
	}
	if strings.Contains(cmd, "systemctl is-active") {
		// Batched probe lists quoted targets; per-rule probe has "$1". One state line per target, in order.
		idx := strings.Index(cmd, "systemctl is-active")
		rest := cmd[idx+len("systemctl is-active"):]
		if end := strings.Index(rest, "2>/dev/null"); end >= 0 {
			rest = rest[:end]
		}
		return strings.Repeat("active\n", len(strings.Fields(rest))), "", 0, nil
	}
	if strings.Contains(cmd, "crontab -l") {
		var crontab strings.Builder
		for i := range 6 {
			crontab.WriteString(fmt.Sprintf("0 2 * * * /usr/local/bin/backup-%d.sh\n", i))
		}
		return crontab.String(), "", 0, nil
	}
	return "", "", 0, nil
}

func newBenchInspector(tb testing.TB, db *store.DB) (*Inspector, *sshrunner.MockRunner) {
	tb.Helper()
	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = benchMockExec
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	return New(db, factory, nil), mockRunner
}

func BenchmarkInspectHost(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "Bench Server", Host: "1.2.3.4", Port: 22, User: "root"})
	if err != nil {
		b.Fatal(err)
	}

	// 20 rules: 7 service, 7 disk, 6 cron
	services := []string{"nginx", "mysql", "redis", "docker", "sshd", "postgres", "haproxy"}
	for _, svc := range services {
		if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: "service", Target: svc, Expected: "active", Enabled: true}); err != nil {
			b.Fatal(err)
		}
	}
	disks := []string{"/", "/var", "/home", "/tmp", "/opt", "/srv", "/data"}
	for _, mnt := range disks {
		if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: "disk", Target: mnt, Expected: "<85%", Enabled: true}); err != nil {
			b.Fatal(err)
		}
	}
	for i := range 6 {
		if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: "cron", Target: fmt.Sprintf("backup-%d.sh", i), Expected: "configured", Enabled: true}); err != nil {
			b.Fatal(err)
		}
	}

	ins, _ := newBenchInspector(b, db)
	for b.Loop() {
		if err := ins.InspectHost(hostID); err != nil {
			b.Fatal(err)
		}
	}
}

func TestInspectHost_CronRulesMemoizeCrontab(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-cronmemo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "cronmemo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "Cron Memo", Host: "1.2.3.4", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if err := db.SaveDesiredRule(&store.DesiredRule{
			HostID:  hostID,
			Kind:    "cron",
			Target:  fmt.Sprintf("backup-%d.sh", i),
			Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	ins, mockRunner := newBenchInspector(t, db)
	if err := ins.InspectHost(hostID); err != nil {
		t.Fatalf("unexpected inspection error: %v", err)
	}

	// Prove the rules actually evaluated ok (wrapper did not break evaluation)
	h, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "healthy" {
		t.Fatalf("expected host status 'healthy', got %q", h.Status)
	}

	count := 0
	for _, cmd := range mockRunner.ExecHistory {
		if cmd == crontabListCmd {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 %q Exec for 3 cron rules, got %d", crontabListCmd, count)
	}
}
