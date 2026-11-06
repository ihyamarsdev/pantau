package inspector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// batchServiceMockExec answers the batched systemctl probe with one state line per quoted target,
// delegating everything else to benchMockExec.
func batchServiceMockExec(states map[string]string) func(cmd string) (string, string, int, error) {
	return func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, serviceBatchSentinel) {
			idx := strings.Index(cmd, "systemctl is-active")
			rest := cmd[idx+len("systemctl is-active"):]
			if end := strings.Index(rest, " 2>/dev/null"); end >= 0 {
				rest = rest[:end]
			}
			var b strings.Builder
			for _, f := range strings.Fields(rest) {
				state, ok := states[strings.Trim(f, "'\"")]
				if !ok {
					state = "active"
				}
				b.WriteString(state + "\n")
			}
			return b.String(), "", 0, nil
		}
		return benchMockExec(cmd)
	}
}

func countExec(cmds []string, match func(string) bool) int {
	n := 0
	for _, c := range cmds {
		if match(c) {
			n++
		}
	}
	return n
}

func TestInspectHost_ServiceRulesBatchSystemctl(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-svcbatch-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "svcbatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "Svc Batch", Host: "1.2.3.4", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range []string{"nginx", "postgres", "redis"} {
		if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: "service", Target: svc, Expected: "active", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = batchServiceMockExec(map[string]string{"nginx": "inactive"})
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := New(db, factory, nil)
	if err := ins.InspectHost(hostID); err != nil {
		t.Fatalf("unexpected inspection error: %v", err)
	}

	// Exactly one batched systemctl exec, zero per-rule service probes.
	if n := countExec(mockRunner.ExecHistory, func(c string) bool { return strings.Contains(c, serviceBatchSentinel) }); n != 1 {
		t.Fatalf("expected exactly 1 batched systemctl exec, got %d", n)
	}
	for _, svc := range []string{"nginx", "postgres", "redis"} {
		if n := countExec(mockRunner.ExecHistory, func(c string) bool { return c == serviceStatusCmd(svc) }); n != 0 {
			t.Fatalf("expected 0 per-rule probes for %s, got %d", svc, n)
		}
	}

	// Results match the per-rule path: one inactive service drifts with the same rootCause prefix.
	items, err := db.ListActualItems(hostID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]store.ActualItem{}
	for _, it := range items {
		got[it.Kind+"\x00"+it.Target] = it
	}
	if it := got["service\x00nginx"]; it.Current != "inactive" || it.Status != "drift" {
		t.Fatalf("nginx: expected inactive/drift, got %q/%q", it.Current, it.Status)
	}
	for _, svc := range []string{"postgres", "redis"} {
		if it := got["service\x00"+svc]; it.Current != "active" || it.Status != "ok" {
			t.Fatalf("%s: expected active/ok, got %q/%q", svc, it.Current, it.Status)
		}
	}
	incs, err := db.ListIncidents(hostID, 20)
	if err != nil {
		t.Fatal(err)
	}
	const wantPrefix = "Service nginx is inactive (expected active)"
	found := false
	for _, inc := range incs {
		if inc.Kind == "service" && inc.Target == "nginx" && inc.EventType == "drift_detected" {
			found = true
			if !strings.HasPrefix(inc.RootCauseExcerpt, wantPrefix) {
				t.Fatalf("root cause excerpt %q, want prefix %q", inc.RootCauseExcerpt, wantPrefix)
			}
		}
	}
	if !found {
		t.Fatal("expected a drift_detected incident for nginx")
	}
}

func TestInspectHost_ServiceBatchFallsBackOnLineMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-svcfb-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "svcfb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "Svc FB", Host: "1.2.3.4", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range []string{"nginx", "postgres", "redis"} {
		if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: "service", Target: svc, Expected: "active", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	// Answers every systemctl exec with a single "active" line -> batch line count mismatches.
	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "systemctl is-active") {
			return "active\n", "", 0, nil
		}
		return benchMockExec(cmd)
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := New(db, factory, nil)
	if err := ins.InspectHost(hostID); err != nil {
		t.Fatalf("unexpected inspection error: %v", err)
	}

	// Fallback: the batch was attempted, then each rule probed individually with the same result.
	if n := countExec(mockRunner.ExecHistory, func(c string) bool { return strings.Contains(c, serviceBatchSentinel) }); n != 1 {
		t.Fatalf("expected exactly 1 batched systemctl attempt, got %d", n)
	}
	for _, svc := range []string{"nginx", "postgres", "redis"} {
		if n := countExec(mockRunner.ExecHistory, func(c string) bool { return c == serviceStatusCmd(svc) }); n != 1 {
			t.Fatalf("expected 1 per-rule probe for %s after fallback, got %d", svc, n)
		}
	}
	items, err := db.ListActualItems(hostID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind != "service" || it.Current != "active" || it.Status != "ok" {
			t.Fatalf("unexpected actual item %+v", it)
		}
	}
}
