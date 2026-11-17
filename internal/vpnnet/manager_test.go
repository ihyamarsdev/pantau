package vpnnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func testManager(t *testing.T) (*Manager, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "pantau.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	m := NewManager(Options{Root: t.TempDir(), DB: db, Gate: func() bool { return true }, Logger: func(string, ...any) {}})
	t.Cleanup(func() {
		m.Shutdown()
		db.Close()
	})
	return m, db
}

// customNetwork stores an enabled, auto-up network driven by shell commands (the custom driver) on loopback.
func customNetwork(t *testing.T, db *store.DB, name, config string) int64 {
	t.Helper()
	id, err := db.CreateVPNNetwork(&store.VPNNetwork{Name: name, Driver: DriverCustom, Enabled: true, AutoUp: true, Iface: "lo", Config: config})
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	return id
}

func waitFor(t *testing.T, what string, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A tunnel daemon that dies must be reaped and reported at once. It used to stay a zombie that still counted
// as "running": no respawn, a CLI call against its dead socket per tick, and a Down that never returned.
func TestSpawnReapsChildThatExits(t *testing.T) {
	m, _ := testManager(t)
	n := Network{ID: 1, Name: "daemon", Root: t.TempDir()}
	if err := m.spawn(n, "sh", "-c", "echo up-and-gone; exit 3"); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	waitFor(t, "the exited child to be reaped", 5*time.Second, func() bool { return !m.childRunning(1) })
	if st := m.Status(1); st.State != StateError || !strings.Contains(st.Err, "exit status 3") {
		t.Fatalf("expected an error status naming the exit, got %+v", st)
	}
	waitFor(t, "the child's output in the log", 2*time.Second, func() bool {
		return strings.Contains(strings.Join(m.Logs(1), "\n"), "up-and-gone")
	})
}

// stopChild runs under the network's lifecycle gate: a process that ignores SIGINT is killed, never waited on forever,
// and a stop Pantau asked for is not reported as a crash.
func TestStopChildKillsStubbornProcessAndDoesNotReportACrash(t *testing.T) {
	old := stopGrace
	stopGrace = 150 * time.Millisecond
	t.Cleanup(func() { stopGrace = old })

	m, _ := testManager(t)
	n := Network{ID: 2, Name: "stubborn", Root: t.TempDir()}
	if err := m.spawn(n, "sh", "-c", `trap "" INT TERM; while :; do sleep 0.1; done`); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	start := time.Now()
	m.stopChild(2)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("stopChild took %v", took)
	}
	waitFor(t, "the killed child to be reaped", 5*time.Second, func() bool { return !m.childRunning(2) })
	if st := m.Status(2); st.State == StateError {
		t.Fatalf("a requested stop was reported as a crash: %+v", st)
	}
}

// Every SSH dial calls Ensure: for a tunnel that is up it must not touch the DB (nil here) or take a lock.
func TestEnsureHotPathNeedsNoDB(t *testing.T) {
	m := NewManager(Options{Root: t.TempDir(), Gate: func() bool { return true }, Logger: func(string, ...any) {}})
	t.Cleanup(m.Shutdown)
	want := &Path{NetworkID: 7, Name: "up", Iface: "lo"}
	m.status[7] = Status{State: StateUp, Iface: "lo", CheckedAt: time.Now()}
	m.paths[7] = want

	got, err := m.Ensure(context.Background(), 7)
	if err != nil || got != want {
		t.Fatalf("Ensure = %v, %v; want the cached path", got, err)
	}
}

// A dial gives up after its budget, but the bring-up it started goes on for the next dial.
func TestEnsureGivesUpWithItsCallerNotTheBringUp(t *testing.T) {
	m, db := testManager(t)
	id := customNetwork(t, db, "slow", `{"up":"exec sleep 20","status":"false"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := m.Ensure(ctx, id)
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("expected ErrNetworkDown, got %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Ensure held its caller for %v", took)
	}
	if !m.inFlight(id) {
		t.Fatal("the bring-up was aborted together with its caller")
	}
}

// "gave up, retry manually" has to be true: a manual Up ignores the backoff and the three-strike cooldown.
func TestManualUpOverridesCooldown(t *testing.T) {
	m, db := testManager(t)
	id := customNetwork(t, db, "flaky", `{"up":"true","status":"true"}`)
	m.mu.Lock()
	m.fails[id] = maxFails
	m.attempt[id] = time.Now()
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := m.Up(ctx, id)
	if err != nil || st.State != StateUp {
		t.Fatalf("manual Up = %+v, %v; want it to bring the network up", st, err)
	}
}

// The status loop brings back a tunnel a Host needs without waiting for the next inspection, and leaves a
// tunnel the admin stopped by hand alone.
func TestTickRevivesNeededNetworkButNotAStoppedOne(t *testing.T) {
	m, db := testManager(t)
	flag := filepath.Join(t.TempDir(), "tunnel-up")
	id := customNetwork(t, db, "dead", fmt.Sprintf(`{"up":"touch %[1]s","down":"rm -f %[1]s","status":"test -f %[1]s"}`, flag))
	if _, err := db.CreateHost(&store.Host{Name: "h", Host: "10.0.0.1", Port: 22, User: "root", NetworkID: id}); err != nil {
		t.Fatalf("create host: %v", err)
	}

	m.tick()
	waitFor(t, "the status loop to revive the network", 10*time.Second, func() bool { return m.Status(id).State == StateUp })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Down(ctx, id); err != nil {
		t.Fatalf("Down: %v", err)
	}
	m.tick()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(flag); err == nil {
		t.Fatal("the status loop brought a network back that was stopped by hand")
	}
}
