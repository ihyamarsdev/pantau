package vpnnet

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The circuit breaker must fail fast at Ensure BEFORE any DB read or bring-up. A manager with a nil DB
// would panic on any network() call, so a clean ErrNetworkDown proves the choke point short-circuits.
func TestEnsureFailsFastWhenBreakerOpen(t *testing.T) {
	m := NewManager(Options{Root: t.TempDir(), Gate: func() bool { return true }, Logger: func(string, ...any) {}})
	t.Cleanup(m.Shutdown)

	m.mu.Lock()
	m.breakerOpen(42, "vpn-prod")
	m.mu.Unlock()

	start := time.Now()
	_, err := m.Ensure(context.Background(), 42)
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Ensure = %v, want ErrNetworkDown", err)
	}
	if !strings.Contains(err.Error(), "vpn-prod") || !strings.Contains(err.Error(), "circuit open") {
		t.Fatalf("error must name the network and say circuit open, got %q", err)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("breaker did not fail fast: %v", took)
	}
	if st := m.BreakerState(42); st != breakerOpen {
		t.Fatalf("BreakerState = %q, want open", st)
	}
}

// A down/error transition opens the breaker; cooldown, a successful Probe and a manual Up all close it.
func TestBreakerClosesOnCooldownProbeAndUp(t *testing.T) {
	m, db := testManager(t)

	// Override the cooldown so the test does not wait minutes.
	oldBase := backoffBase
	backoffBase = 40 * time.Millisecond
	t.Cleanup(func() { backoffBase = oldBase })

	id := customNetwork(t, db, "flaky", `{"up":"true","status":"true"}`)

	// A failure opens this network's breaker.
	n := Network{ID: id, Name: "flaky"}
	m.setError(n, errors.New("boom"))
	if st := m.BreakerState(id); st != breakerOpen {
		t.Fatalf("breaker after error = %q, want open", st)
	}

	// Cooldown turns it half-open (Ensure lets a trial through).
	waitFor(t, "breaker to cool down", 2*time.Second, func() bool { return m.BreakerState(id) == breakerHalfOpen })

	// A successful probe closes it early.
	oldProbe := probeIface
	probeIface = func(string) (string, bool) { return "10.9.0.2", true }
	t.Cleanup(func() { probeIface = oldProbe })
	if _, err := m.Probe(context.Background(), id); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st := m.BreakerState(id); st != breakerClosed {
		t.Fatalf("breaker after probe = %q, want closed", st)
	}

	// Re-open, then a manual Up closes it.
	m.setError(n, errors.New("boom again"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.Up(ctx, id); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if st := m.BreakerState(id); st != breakerClosed {
		t.Fatalf("breaker after Up = %q, want closed", st)
	}
}

// Backoff grows exponentially from the base, is capped, and is multiplied by the jitter function.
func TestBackoffDelayExponentialJitterAndCap(t *testing.T) {
	oldBase, oldCap, oldJitter := backoffBase, backoffCap, backoffJitter
	t.Cleanup(func() { backoffBase, backoffCap, backoffJitter = oldBase, oldCap, oldJitter })

	backoffBase = 100 * time.Millisecond
	backoffCap = 500 * time.Millisecond
	backoffJitter = func() float64 { return 1.0 }

	// 100ms, 200ms, 400ms, then capped at 500ms.
	want := []time.Duration{100, 200, 400, 500, 500}
	for i, ms := range want {
		if got := backoffDelay(i + 1); got != ms*time.Millisecond {
			t.Fatalf("backoffDelay(%d) = %v, want %v", i+1, got, ms*time.Millisecond)
		}
	}
	if backoffDelay(0) != 0 {
		t.Fatal("backoffDelay(0) must be 0")
	}

	// Jitter bounds: with a 0.8 multiplier a base delay is 80ms, with 1.2 it is 120ms.
	backoffJitter = func() float64 { return 0.8 }
	if got := backoffDelay(1); got != 80*time.Millisecond {
		t.Fatalf("jitter 0.8: got %v, want 80ms", got)
	}
	backoffJitter = func() float64 { return 1.2 }
	if got := backoffDelay(1); got != 120*time.Millisecond {
		t.Fatalf("jitter 1.2: got %v, want 120ms", got)
	}
}

// Two instance networks must derive distinct socket/state paths from their per-network roots, and an
// operation on one must not touch the other's status, path or breaker.
func TestMultiInstanceIsolation(t *testing.T) {
	nb := netbirdDriver{}
	ts := tailscaleDriver{}

	n1 := Network{ID: 1, Root: "/tmp/vpn/1", Config: map[string]string{"mode": "instance", "interface_name": "wt0"}}
	n2 := Network{ID: 2, Root: "/tmp/vpn/2", Config: map[string]string{"mode": "instance", "interface_name": "wt1"}}
	if nb.daemonAddr(n1) == nb.daemonAddr(n2) {
		t.Fatal("two NetBird instances share a daemon socket")
	}
	t1 := Network{ID: 3, Root: "/tmp/vpn/3", Config: map[string]string{}}
	t2 := Network{ID: 4, Root: "/tmp/vpn/4", Config: map[string]string{}}
	if ts.socketPath(t1) == ts.socketPath(t2) {
		t.Fatal("two Tailscale instances share a socket path")
	}

	m, db := testManager(t)
	idA := customNetwork(t, db, "net-a", `{"up":"true","status":"true"}`)
	idB := customNetwork(t, db, "net-b", `{"up":"true","status":"true"}`)
	pA := &Path{NetworkID: idA, Name: "net-a", Iface: "lo"}
	pB := &Path{NetworkID: idB, Name: "net-b", Iface: "lo"}
	m.mu.Lock()
	m.status[idA] = Status{State: StateUp, Iface: "lo", CheckedAt: time.Now()}
	m.status[idB] = Status{State: StateUp, Iface: "lo", CheckedAt: time.Now()}
	m.paths[idA] = pA
	m.paths[idB] = pB
	m.mu.Unlock()

	// Down one network: the other stays up with its path intact and its breaker closed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Down(ctx, idA); err != nil {
		t.Fatalf("Down A: %v", err)
	}
	m.mu.Lock()
	stB, pathB := m.status[idB], m.paths[idB]
	breakerB := m.breakerStateLocked(idB)
	m.mu.Unlock()
	if stB.State != StateUp || pathB != pB {
		t.Fatalf("Down A mutated B: status=%+v path=%v", stB, pathB)
	}
	if breakerB != breakerClosed {
		t.Fatalf("Down A opened B's breaker: %q", breakerB)
	}

	// Restore A's path, then Invalidate B: A's cached path must survive.
	m.mu.Lock()
	m.status[idA] = Status{State: StateUp, Iface: "lo", CheckedAt: time.Now()}
	m.paths[idA] = pA
	m.mu.Unlock()
	m.Invalidate(idB)
	m.mu.Lock()
	pathAAfter := m.paths[idA]
	m.mu.Unlock()
	if pathAAfter != pA {
		t.Fatal("Invalidate B must not touch A's path")
	}
}

// Probe must read the interface (not a PID): a fake interface with an address yields a latency and a
// virtual IP; a missing interface marks the status degraded and returns an error.
func TestProbeInterfaceAndDegraded(t *testing.T) {
	m, db := testManager(t)
	id := customNetwork(t, db, "probe", `{"up":"true","status":"true"}`)

	oldProbe := probeIface
	t.Cleanup(func() { probeIface = oldProbe })

	probeIface = func(string) (string, bool) { return "10.7.0.5", true }
	st, err := m.Probe(context.Background(), id)
	if err != nil {
		t.Fatalf("Probe with address: %v", err)
	}
	if st.ProbeMs < 0 || st.VirtualIP != "10.7.0.5" {
		t.Fatalf("probe telemetry = %+v, want a latency and virtual IP", st)
	}

	probeIface = func(string) (string, bool) { return "", false }
	st, err = m.Probe(context.Background(), id)
	if err == nil || !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Probe without interface: want ErrNetworkDown, got %v", err)
	}
	if !st.Degraded {
		t.Fatalf("missing interface must be degraded: %+v", st)
	}
}

// Probe must honour a cancelled context without touching the network.
func TestProbeHonoursContext(t *testing.T) {
	m, _ := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Probe(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe with cancelled ctx = %v, want context.Canceled", err)
	}
}
