package vpnnet

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pantau/internal/store"
)

const (
	upTimeout      = 60 * time.Second
	downTimeout    = 20 * time.Second
	statusTimeout  = 10 * time.Second
	errorCooldown  = 5 * time.Minute
	maxFails       = 3
	maxChildProcs  = 5
	logRingLines   = 200
	statusLoopTick = 30 * time.Second
)

// Exponential backoff with jitter. Tests in package vpnnet override these to be deterministic.
var (
	backoffBase = 30 * time.Second
	backoffCap  = 5 * time.Minute
	// #nosec G404 -- reconnect jitter only spreads retries, not a security value
	backoffJitter = func() float64 { return 0.8 + rand.Float64()*0.4 } // [0.8, 1.2]
)

// backoffDelay returns the retry delay after `fails` consecutive failures. It grows exponentially from
// backoffBase, is capped at backoffCap, then multiplied by backoffJitter so failing networks desynchronise.
func backoffDelay(fails int) time.Duration {
	if fails <= 0 {
		return 0
	}
	d := backoffBase
	for i := 1; i < fails; i++ {
		d *= 2
		if d >= backoffCap {
			d = backoffCap
			break
		}
	}
	if d > backoffCap {
		d = backoffCap
	}
	return time.Duration(float64(d) * backoffJitter())
}

// Circuit-breaker states reported by BreakerState.
const (
	breakerClosed   = "closed"
	breakerOpen     = "open"
	breakerHalfOpen = "half-open"
)

// Status is the supervisor's verdict for one network.
type Status struct {
	State     string    `json:"state"`
	Iface     string    `json:"iface"`
	Detail    string    `json:"detail"`
	Err       string    `json:"error,omitempty"`
	Peers     int       `json:"peers"`
	CheckedAt time.Time `json:"checked_at"`
	DNS       []string  `json:"-"`

	// Active link-probe telemetry. ProbeMs is -1 until a probe has run.
	VirtualIP string `json:"virtual_ip"`
	ProbeMs   int64  `json:"probe_ms"`
	Degraded  bool   `json:"degraded"`
	probed    bool   // internal: whether ProbeMs was actually measured
}

// preserveProbe carries probe telemetry forward onto a status that carries none of its own (e.g. a driver
// refresh between two probes), so the API/UI do not lose virtual IP, latency or degraded state every tick.
func preserveProbe(st, old Status) Status {
	if !st.probed {
		st.ProbeMs = old.ProbeMs
		st.VirtualIP = old.VirtualIP
		st.Degraded = old.Degraded
		st.probed = old.probed
	}
	if !st.probed && st.ProbeMs == 0 {
		st.ProbeMs = -1
	}
	return st
}

// breakerState is the per-network circuit breaker. A breaker that has cooled down is "half-open": Ensure
// allows one trial, which closes it on success or re-opens it on failure.
type breakerState struct {
	name     string
	openedAt time.Time
}

// Tunnel states reported to the UI.
const (
	StateDown     = "down"
	StateStarting = "starting"
	StateUp       = "up"
	StateError    = "error"
)

// stopGrace is how long a tunnel process gets to exit after SIGINT before it is killed (a var so tests can shorten it).
var stopGrace = 10 * time.Second

// fastPathTTL bounds how long a dial may trust the cached "up" verdict without re-reading the network row
// (enabled flag, config). The status loop re-stamps it every tick, so a live tunnel never leaves the hot path.
const fastPathTTL = 2 * statusLoopTick

type childProc struct {
	cmd      *exec.Cmd
	done     chan struct{}
	stopping atomic.Bool // set when Pantau asked the process to stop: its exit is not a failure
}

// Options wires the manager to the app: where tunnel state/config live and whether the gate is open.
type Options struct {
	Root   string
	DB     *store.DB
	Gate   func() bool
	Logger func(format string, args ...any)
}

// Manager owns tunnel lifecycle. One lock per network serialises bring-up; the status loop keeps badges fresh.
type Manager struct {
	db   *store.DB
	root string
	gate func() bool
	log  func(string, ...any)

	mu      sync.Mutex
	procs   map[int64]*childProc
	status  map[int64]Status
	logs    map[int64][]string
	locks   map[int64]chan struct{}
	attempt map[int64]time.Time
	fails   map[int64]int
	paths   map[int64]*Path // dial paths of networks last seen "up": the hot path of Ensure
	flights map[int64]*flight
	stopped map[int64]bool // stopped by hand: the status loop leaves them down
	// breakers is the per-network circuit breaker. Keyed strictly by network_id so one network's
	// failure never affects another's dial path.
	breakers map[int64]*breakerState

	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager starts the background status loop; call Shutdown on exit.
func NewManager(opts Options) *Manager {
	if opts.Gate == nil {
		opts.Gate = func() bool { return false }
	}
	if opts.Logger == nil {
		opts.Logger = log.Printf
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		db:       opts.DB,
		root:     opts.Root,
		gate:     opts.Gate,
		log:      opts.Logger,
		procs:    map[int64]*childProc{},
		status:   map[int64]Status{},
		logs:     map[int64][]string{},
		locks:    map[int64]chan struct{}{},
		attempt:  map[int64]time.Time{},
		fails:    map[int64]int{},
		paths:    map[int64]*Path{},
		flights:  map[int64]*flight{},
		stopped:  map[int64]bool{},
		breakers: map[int64]*breakerState{},
		ctx:      ctx,
		cancel:   cancel,
	}
	go m.statusLoop()
	return m
}

// Shutdown stops the status loop and every tunnel process Pantau started (netbird instance, openvpn).
func (m *Manager) Shutdown() {
	m.cancel()
	m.mu.Lock()
	ids := make([]int64, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.stopChild(id)
	}
}

// lockFor returns the network's lifecycle gate: a one-slot channel, so a waiter can give up with its context
// instead of queueing behind a bring-up for as long as it takes.
func (m *Manager) lockFor(id int64) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[id]
	if !ok {
		l = make(chan struct{}, 1)
		m.locks[id] = l
	}
	return l
}

func acquire(ctx context.Context, l chan struct{}) error {
	select {
	case l <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func release(l chan struct{}) { <-l }

func parseConfig(raw string) map[string]string {
	cfg := map[string]string{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

// network loads one network row and materialises its config.
func (m *Manager) network(id int64) (Network, error) {
	row, err := m.db.GetVPNNetwork(id)
	if err != nil {
		return Network{}, err
	}
	if row == nil {
		return Network{}, ErrNetworkNotFound
	}
	n := Network{ID: row.ID, Name: row.Name, Driver: row.Driver, Enabled: row.Enabled, AutoUp: row.AutoUp, Iface: row.Iface, Config: parseConfig(row.Config), Identity: row.Identity}
	n.Root = filepath.Join(m.root, fmt.Sprint(id))
	if err := os.MkdirAll(n.Root, 0o700); err != nil {
		return Network{}, fmt.Errorf("create %s: %w", n.Root, err)
	}
	return n, nil
}

func (m *Manager) ifaceFor(n Network) string {
	if n.Iface != "" {
		return n.Iface
	}
	if d := driverFor(n.Driver); d != nil {
		return d.DefaultIface(n)
	}
	return ""
}

func (m *Manager) pathFor(n Network, iface string, dns []string) *Path {
	return &Path{NetworkID: n.ID, Name: n.Name, Iface: iface, DNS: dns}
}

// breaker returns the network's breaker, creating it on first use. Caller holds m.mu.
func (m *Manager) breaker(id int64) *breakerState {
	b := m.breakers[id]
	if b == nil {
		b = &breakerState{}
		m.breakers[id] = b
	}
	return b
}

// breakerCooled reports whether an open breaker has passed its cooldown (backoffDelay of the strike
// count, at least one strike) and may be given one trial (half-open). Caller holds m.mu.
func (m *Manager) breakerCooled(id int64) bool {
	b := m.breakers[id]
	if b == nil || b.openedAt.IsZero() {
		return false
	}
	fails := m.fails[id]
	if fails < 1 {
		fails = 1 // a single down/error still gets a full base cooldown
	}
	return time.Since(b.openedAt) >= backoffDelay(fails)
}

// BreakerState reports "closed", "open" or "half-open" for a network. An open breaker that has cooled
// down is half-open: Ensure lets one trial through.
func (m *Manager) BreakerState(networkID int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.breakerStateLocked(networkID)
}

// breakerStateLocked is BreakerState with m.mu already held.
func (m *Manager) breakerStateLocked(networkID int64) string {
	b := m.breakers[networkID]
	if b == nil || b.openedAt.IsZero() {
		return breakerClosed
	}
	if m.breakerCooled(networkID) {
		return breakerHalfOpen
	}
	return breakerOpen
}

// breakerOpen opens (or re-opens with a fresh cooldown) the network's breaker. The name is cached so Ensure
// can name the network in its fail-fast error without reading the DB. Caller holds m.mu.
func (m *Manager) breakerOpen(id int64, name string) {
	b := m.breaker(id)
	b.name = name
	b.openedAt = time.Now()
}

// breakerName returns the cached network name of an open breaker (empty if unknown). Caller holds m.mu.
func (m *Manager) breakerName(id int64) string {
	if b := m.breakers[id]; b != nil {
		return b.name
	}
	return ""
}

// breakerClose closes the network's breaker and clears its strike count. Caller holds m.mu.
func (m *Manager) breakerClose(id int64) {
	delete(m.breakers, id)
}

// breakerTrip is the fail-fast error emitted at the Ensure choke point when the breaker is open.
func breakerTrip(name string) error {
	return fmt.Errorf("%w: %s (circuit open)", ErrNetworkDown, name)
}

// Ensure returns the path a Host dials through, bringing the network up when needed.
// A nil path means "direct": the Host has no network or has network 0.
func (m *Manager) Ensure(ctx context.Context, networkID int64) (*Path, error) {
	if networkID == 0 {
		return nil, nil
	}
	// Circuit breaker: fail fast before any DB read, gate check or bring-up. A half-open breaker lets one
	// trial through; an open one fails in microseconds with a named error, protecting every caller
	// (inspection, transfer, key provisioning) without duplicating logic.
	m.mu.Lock()
	state := m.breakerStateLocked(networkID)
	name := m.breakerName(networkID)
	m.mu.Unlock()
	if state == breakerOpen {
		if name == "" {
			name = fmt.Sprintf("network %d", networkID)
		}
		return nil, breakerTrip(name)
	}
	if !m.gate() {
		return nil, ErrGateDisabled
	}
	// Hot path, taken by every SSH dial: a fresh "up" verdict and a live interface need no DB read and no lock.
	if p := m.livePath(networkID); p != nil {
		return p, nil
	}
	n, err := m.network(networkID)
	if err != nil {
		return nil, err
	}
	if !n.Enabled {
		return nil, fmt.Errorf("%w: %s", ErrNetworkDisabled, n.Name)
	}
	if p := m.restamp(n); p != nil {
		return p, nil
	}
	if !n.AutoUp {
		m.record(n, Status{State: StateDown, Detail: "auto-up disabled, start it from Networks"})
		return nil, fmt.Errorf("%w: %s", ErrNetworkDown, n.Name)
	}
	st, err := m.waitUp(ctx, n, m.startUp(n))
	if err != nil {
		return nil, err
	}
	return m.pathFor(n, st.Iface, st.DNS), nil
}

// livePath returns the cached path of a network whose "up" verdict is fresh and whose interface still exists.
func (m *Manager) livePath(id int64) *Path {
	m.mu.Lock()
	st, p := m.status[id], m.paths[id]
	m.mu.Unlock()
	if p == nil || st.State != StateUp || time.Since(st.CheckedAt) > fastPathTTL || !ifaceReady(p.Iface) {
		return nil
	}
	return p
}

// restamp re-validates a cached "up" once the row was re-read, so the next dials are served by the hot path again.
func (m *Manager) restamp(n Network) *Path {
	m.mu.Lock()
	st, ok := m.status[n.ID]
	m.mu.Unlock()
	if !ok || st.State != StateUp || !ifaceReady(st.Iface) {
		return nil
	}
	p := m.pathFor(n, st.Iface, st.DNS)
	m.mu.Lock()
	st.CheckedAt = time.Now()
	m.status[n.ID] = st
	m.paths[n.ID] = p
	m.mu.Unlock()
	return p
}

// flight is one bring-up in progress; every caller for the same network joins it.
type flight struct {
	done chan struct{}
	st   Status
	err  error
}

// startUp begins the bring-up of a network, or joins the one already running. It runs on the manager's own
// context, so a caller that gives up (dial budget spent, browser tab closed) never aborts a tunnel half way up.
func (m *Manager) startUp(n Network) *flight {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.flights[n.ID]; ok {
		return f
	}
	f := &flight{done: make(chan struct{})}
	m.flights[n.ID] = f
	go func() {
		l := m.lockFor(n.ID)
		if f.err = acquire(m.ctx, l); f.err == nil {
			f.st, f.err = m.bringUp(m.ctx, n)
			release(l)
		}
		m.mu.Lock()
		delete(m.flights, n.ID)
		m.mu.Unlock()
		close(f.done)
	}()
	return f
}

// waitUp waits for a bring-up for as long as the caller can afford to.
func (m *Manager) waitUp(ctx context.Context, n Network, f *flight) (Status, error) {
	select {
	case <-f.done:
		return f.st, f.err
	case <-ctx.Done():
		return Status{State: StateStarting, Detail: "still starting"}, fmt.Errorf("%w: %s (still starting)", ErrNetworkDown, n.Name)
	}
}

func (m *Manager) inFlight(id int64) bool {
	m.mu.Lock()
	_, ok := m.flights[id]
	m.mu.Unlock()
	return ok
}

func (m *Manager) isStopped(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped[id]
}

// Invalidate drops what the manager remembers about a network whose row changed, so the next dial re-reads it.
func (m *Manager) Invalidate(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.paths, id)
	m.fails[id] = 0
	// A config change invalidates this network only; reset its breaker so the next dial re-reads the row.
	m.breakerClose(id)
	if st, ok := m.status[id]; ok {
		st.CheckedAt = time.Time{}
		m.status[id] = st
	}
}

// Forget drops everything held for a deleted network, and stops the process it may still own.
func (m *Manager) Forget(id int64) {
	m.stopChild(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.paths, id)
	delete(m.status, id)
	delete(m.logs, id)
	delete(m.attempt, id)
	delete(m.fails, id)
	delete(m.stopped, id)
	delete(m.locks, id)
	delete(m.breakers, id)
}

// Reset follows a System Snapshot restore: a network ID may now name another network, so forget every network,
// delete the folders of IDs the restore dropped, and remove each registration the restore replaces (bringUp
// writes the restored one back).
func (m *Manager) Reset() {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.procs)+len(m.status))
	for id := range m.procs {
		ids = append(ids, id)
	}
	for id := range m.status {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Forget(id)
	}
	rows, err := m.db.ListVPNNetworks()
	if err != nil {
		m.log("vpn: reset after restore: %v", err)
		return
	}
	keep := make(map[string]bool, len(rows))
	for _, row := range rows {
		dir := fmt.Sprint(row.ID)
		keep[dir] = true
		n := Network{ID: row.ID, Driver: row.Driver, Config: parseConfig(row.Config), Root: filepath.Join(m.root, dir)}
		if f := identityFile(n); f != "" && len(row.Identity) > 0 {
			_ = os.Remove(f)
		}
	}
	entries, _ := os.ReadDir(m.root)
	for _, e := range entries {
		if e.IsDir() && !keep[e.Name()] {
			_ = os.RemoveAll(filepath.Join(m.root, e.Name()))
		}
	}
}

// restoreIdentity puts the saved registration back when the daemon has none (fresh machine, restored snapshot).
func (m *Manager) restoreIdentity(n Network) {
	f := identityFile(n)
	if f == "" || len(n.Identity) == 0 {
		return
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		return
	}
	if err := os.WriteFile(f, n.Identity, 0o600); err != nil {
		m.logf(n, "restore identity: %v", err)
	}
}

// saveIdentity keeps the daemon's current registration with the network row, and so in the System Snapshot.
func (m *Manager) saveIdentity(n Network) {
	f := identityFile(n)
	if f == "" {
		return
	}
	data, err := os.ReadFile(f)
	if err != nil || len(data) == 0 || bytes.Equal(data, n.Identity) {
		return
	}
	if err := m.db.SetVPNNetworkIdentity(n.ID, data); err != nil {
		m.logf(n, "save identity: %v", err)
	}
}

// Up is the manual bring-up. It overrides the retry backoff and the three-strike cooldown (that is what
// "retry manually" means) and joins a bring-up already running instead of starting a second one.
func (m *Manager) Up(ctx context.Context, networkID int64) (Status, error) {
	if !m.gate() {
		return Status{State: StateDown, Err: ErrGateDisabled.Error()}, ErrGateDisabled
	}
	n, err := m.network(networkID)
	if err != nil {
		return Status{State: StateDown, Err: err.Error()}, err
	}
	m.mu.Lock()
	m.fails[n.ID] = 0
	// Manual Up closes the breaker immediately: "retry manually" overrides the cooldown.
	m.breakerClose(n.ID)
	m.mu.Unlock()
	return m.waitUp(ctx, n, m.startUp(n))
}

func (m *Manager) bringUp(ctx context.Context, n Network) (Status, error) {
	d := driverFor(n.Driver)
	if d == nil {
		err := fmt.Errorf("unknown driver %q", n.Driver)
		m.setError(n, err)
		return Status{State: StateError, Err: err.Error()}, err
	}
	m.mu.Lock()
	last, fails := m.attempt[n.ID], m.fails[n.ID]
	m.mu.Unlock()
	// Exponential backoff with jitter replaces the fixed 30s retry. The three-strike cooldown remains as the
	// upper bound after which auto-up gives up until manual Up or the cooldown expires.
	if fails > 0 && time.Since(last) < backoffDelay(fails) {
		err := fmt.Errorf("%w: %s (retrying soon)", ErrNetworkDown, n.Name)
		return Status{State: StateDown, Err: err.Error()}, err
	}
	if fails >= maxFails && time.Since(last) < errorCooldown {
		err := fmt.Errorf("%w: %s (gave up, retry manually)", ErrNetworkDown, n.Name)
		return Status{State: StateError, Err: err.Error()}, err
	}

	m.record(n, Status{State: StateStarting})
	m.restoreIdentity(n)
	if err := d.Up(ctx, m, n); err != nil {
		m.mu.Lock()
		m.fails[n.ID]++
		m.attempt[n.ID] = time.Now()
		m.mu.Unlock()
		m.setError(n, err)
		return Status{State: StateError, Err: err.Error()}, err
	}
	st, err := d.Status(ctx, m, n)
	if err != nil || st.State != StateUp {
		if err == nil {
			err = fmt.Errorf("%w: %s did not come up", ErrNetworkDown, n.Name)
		}
		m.mu.Lock()
		m.fails[n.ID]++
		m.attempt[n.ID] = time.Now()
		m.mu.Unlock()
		m.setError(n, err)
		return Status{State: StateError, Err: err.Error()}, err
	}
	m.mu.Lock()
	m.fails[n.ID] = 0
	m.attempt[n.ID] = time.Now()
	delete(m.stopped, n.ID)
	m.mu.Unlock()
	m.record(n, st)
	m.saveIdentity(n)
	return st, nil
}

// Down stops the tunnel (and the process Pantau started for it) and keeps it down: the status loop does not
// bring a stopped network back; only a dial that needs it (auto-up) or Up does.
func (m *Manager) Down(ctx context.Context, networkID int64) error {
	if !m.gate() {
		return ErrGateDisabled
	}
	n, err := m.network(networkID)
	if err != nil {
		return err
	}
	l := m.lockFor(n.ID)
	if err := acquire(ctx, l); err != nil {
		return fmt.Errorf("%w: %s is busy (%v)", ErrNetworkDown, n.Name, err)
	}
	defer release(l)
	if d := driverFor(n.Driver); d != nil {
		if err := d.Down(ctx, m, n); err != nil {
			if ifaceReady(m.ifaceFor(n)) {
				m.setError(n, err)
				return err
			}
			// The tunnel is gone already (daemon dead): the stop the admin asked for is done.
			m.log("vpn: network %s: down: %v (interface already gone)", n.Name, err)
		}
	}
	m.stopChild(n.ID)
	m.mu.Lock()
	m.fails[n.ID] = 0
	m.stopped[n.ID] = true
	delete(m.paths, n.ID)
	old := m.status[n.ID]
	st := Status{State: StateDown, Detail: "stopped", CheckedAt: time.Now()}
	m.status[n.ID] = preserveProbe(st, old)
	// A manual down is a deliberate path failure: open this network's breaker only.
	m.breakerOpen(n.ID, n.Name)
	m.mu.Unlock()
	_ = m.db.SetVPNNetworkState(n.ID, StateDown, "stopped")
	return nil
}

// Refresh asks the driver for the current state without bringing anything up.
func (m *Manager) Refresh(ctx context.Context, networkID int64) (Status, error) {
	if m.inFlight(networkID) {
		return m.Status(networkID), nil // a bring-up is mid-way: its verdict is the one that counts
	}
	n, err := m.network(networkID)
	if err != nil {
		return Status{State: StateDown, Err: err.Error()}, err
	}
	d := driverFor(n.Driver)
	if d == nil {
		err := fmt.Errorf("unknown driver %q", n.Driver)
		m.setError(n, err)
		return Status{State: StateError, Err: err.Error()}, err
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	st, err := d.Status(ctx, m, n)
	if err != nil {
		m.setError(n, err)
		return Status{State: StateError, Err: err.Error()}, err
	}
	if !n.Enabled {
		st.State = StateDown
		st.Detail = "disabled"
	}
	m.record(n, st)
	return st, nil
}

// probeIface is the active link-probe implementation. It is a var so unit tests can substitute a fake
// interface (real interface state is a kernel property and cannot be faked portably).
var probeIface = ifaceProbe

// Probe is the active interface/link health check. It verifies the tunnel interface exists and carries a
// usable non-loopback address, measures the elapsed time, refreshes telemetry and returns the status. It is
// read-only: it never starts a process and never requires the VPN gate. A successful probe closes the
// network's breaker; a failed one marks the status degraded and opens the breaker.
func (m *Manager) Probe(ctx context.Context, networkID int64) (Status, error) {
	select {
	case <-ctx.Done():
		return Status{State: StateDown}, ctx.Err()
	default:
	}
	n, err := m.network(networkID)
	if err != nil {
		return Status{State: StateDown, Err: err.Error()}, err
	}
	iface := m.ifaceFor(n)
	start := time.Now()
	ip, ok := probeIface(iface)
	elapsed := time.Since(start).Milliseconds()

	m.mu.Lock()
	st := m.status[networkID]
	m.mu.Unlock()

	st.Iface = iface
	st.VirtualIP = ip
	st.ProbeMs = elapsed
	st.probed = true
	st.CheckedAt = time.Now()

	if !ok {
		st.Degraded = true
		st.State = StateDown
		st.Detail = "link probe failed: interface missing or has no usable address"
		m.record(n, st)
		m.mu.Lock()
		m.breakerOpen(n.ID, n.Name)
		m.mu.Unlock()
		return st, fmt.Errorf("%w: %s: %s", ErrNetworkDown, n.Name, st.Detail)
	}

	st.Degraded = false
	// A usable interface is the link-health definition of "up" (the vendor's own state stays in Detail).
	st.State = StateUp
	st.Err = ""
	st.Detail = "link probe ok"
	m.record(n, st)
	// A successful probe closes the breaker early (before the cooldown) so dials resume at once.
	m.mu.Lock()
	m.breakerClose(n.ID)
	m.mu.Unlock()
	return st, nil
}

// Status returns the cached state (no I/O).
func (m *Manager) Status(networkID int64) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.status[networkID]; ok {
		return st
	}
	return Status{State: StateDown, ProbeMs: -1}
}

// ForEach walks every network once, for the background loop and the API list.
func (m *Manager) ForEach(fn func(store.VPNNetwork, Status)) {
	rows, err := m.db.ListVPNNetworks()
	if err != nil {
		m.log("vpn: list networks: %v", err)
		return
	}
	for _, row := range rows {
		fn(row, m.Status(row.ID))
	}
}

// Logs returns the last lines a network's commands printed (already redacted).
func (m *Manager) Logs(networkID int64) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.logs[networkID]))
	copy(out, m.logs[networkID])
	return out
}

func (m *Manager) logf(n Network, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	for _, s := range n.secrets() {
		line = strings.ReplaceAll(line, s, "(redacted)")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ring := append(m.logs[n.ID], line)
	if len(ring) > logRingLines {
		ring = ring[len(ring)-logRingLines:]
	}
	m.logs[n.ID] = ring
}

func (m *Manager) record(n Network, st Status) {
	st.CheckedAt = time.Now()
	for _, s := range n.secrets() {
		st.Detail = strings.ReplaceAll(st.Detail, s, "(redacted)")
		st.Err = strings.ReplaceAll(st.Err, s, "(redacted)")
	}
	m.mu.Lock()
	st = preserveProbe(st, m.status[n.ID])
	m.status[n.ID] = st
	if st.State == StateUp {
		m.attempt[n.ID] = time.Now()
		m.paths[n.ID] = m.pathFor(n, st.Iface, st.DNS)
		// A confirmed up closes the breaker.
		m.breakerClose(n.ID)
	} else {
		delete(m.paths, n.ID)
	}
	m.mu.Unlock()
	// A state transition may change the resolver: drop this network's cached DNS lookups.
	InvalidateDNS(n.ID, "")
	_ = m.db.SetVPNNetworkState(n.ID, st.State, strings.TrimSpace(st.Err+" "+st.Detail))
}

func (m *Manager) setError(n Network, err error) {
	msg := err.Error()
	for _, s := range n.secrets() {
		msg = strings.ReplaceAll(msg, s, "(redacted)")
	}
	m.mu.Lock()
	st := m.status[n.ID]
	st.State = StateError
	st.Err = msg
	st.CheckedAt = time.Now()
	m.status[n.ID] = st
	delete(m.paths, n.ID)
	// An error is a path failure: open this network's breaker only.
	m.breakerOpen(n.ID, n.Name)
	m.mu.Unlock()
	m.log("vpn: network %s: %s", n.Name, msg)
	_ = m.db.SetVPNNetworkState(n.ID, StateError, msg)
}

// spawn runs one long-lived vendor process (netbird daemon instance, openvpn) and captures its output.
func (m *Manager) spawn(n Network, name string, args ...string) error {
	m.mu.Lock()
	if len(m.procs) >= maxChildProcs {
		m.mu.Unlock()
		return ErrTooManyChildProcs
	}
	if _, exists := m.procs[n.ID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("a tunnel process for %s is already running", n.Name)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = m.childEnvFor(n)
	cmd.Dir = n.Root
	// A daemon's helpers may keep the output pipe open after it exits; Wait must not wait for them forever.
	cmd.WaitDelay = 3 * time.Second
	// One pipe for stdout+stderr: openvpn prints "TUN/TAP device tun0 opened" on stdout, warnings on stderr.
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		_ = pw.Close()
		return fmt.Errorf("start %s: %w", name, err)
	}
	proc := &childProc{cmd: cmd, done: make(chan struct{})}
	m.procs[n.ID] = proc
	m.mu.Unlock()

	go m.logLines(n, pr)
	go func() {
		// Wait reaps the child. It must not sit behind the log reader: that reader only ends once the pipe is
		// closed, which happens here, so a daemon that died would stay "running" (and a zombie) forever.
		err := cmd.Wait()
		_ = pw.Close()
		close(proc.done)
		m.mu.Lock()
		if m.procs[n.ID] == proc {
			delete(m.procs, n.ID)
		}
		m.mu.Unlock()
		m.childExited(n, proc, err)
	}()
	return nil
}

// logLines copies a tunnel process's output into the network's log ring until the pipe closes.
func (m *Manager) logLines(n Network, pr *io.PipeReader) {
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		m.logf(n, "%s", sc.Text())
	}
	_, _ = io.Copy(io.Discard, pr) // an over-long line stops the scanner; keep draining or the child blocks on a full pipe
}

// childExited reports a tunnel process that died on its own, so the badge turns red at once and the status
// loop can bring the network back, instead of waiting for a CLI call against a dead socket to time out.
func (m *Manager) childExited(n Network, proc *childProc, err error) {
	if proc.stopping.Load() || m.ctx.Err() != nil {
		return
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		m.setError(n, fmt.Errorf("tunnel process exited unexpectedly: %w", err))
		return
	}
	m.setError(n, errors.New("tunnel process exited unexpectedly"))
}

func (m *Manager) childRunning(id int64) bool {
	m.mu.Lock()
	_, ok := m.procs[id]
	m.mu.Unlock()
	return ok
}

// stopChild terminates a process Pantau started: SIGINT, then a hard kill after stopGrace. It never waits
// without a bound: the caller usually holds the network's lifecycle gate.
func (m *Manager) stopChild(id int64) {
	m.mu.Lock()
	proc := m.procs[id]
	m.mu.Unlock()
	if proc == nil || proc.cmd.Process == nil {
		return
	}
	proc.stopping.Store(true)
	_ = proc.cmd.Process.Signal(os.Interrupt)
	select {
	case <-proc.done:
	case <-time.After(stopGrace):
		_ = proc.cmd.Process.Kill()
		select {
		case <-proc.done:
		case <-time.After(3 * time.Second):
		}
	}
}

// socketListening reports whether something serves a daemon socket (unix socket on Linux/macOS, npipe path on Windows).
func socketListening(path string) bool {
	if path == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		_, err := os.Stat(path)
		return err == nil
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// waitSocket polls until a daemon starts serving its socket.
func waitSocket(ctx context.Context, path string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if socketListening(path) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// waitIface polls until the driver's interface exists (tun devices appear a moment after the process starts).
func waitIface(ctx context.Context, iface string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if ifaceReady(iface) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (m *Manager) statusLoop() {
	t := time.NewTicker(statusLoopTick)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.tick()
		}
	}
}

// tick refreshes every enabled network's badge and restarts the ones a Host needs that fell over, so a tunnel
// that died (daemon crash, `netbird down`) is back before the next inspection instead of turning its Hosts stale.
func (m *Manager) tick() {
	rows, err := m.db.ListVPNNetworks()
	if err != nil || !m.gate() {
		return
	}
	used, _ := m.db.CountHostsByNetwork()
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		// An open breaker blocks auto-up: no bring-up, no vendor CLI, no auth/CPU storm during an outage.
		// Refresh stays cheap (cache work) and keeps the badge fresh; it never starts a process.
		if m.BreakerState(row.ID) == breakerOpen {
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, statusTimeout)
		st, _ := m.Refresh(ctx, row.ID)
		cancel()
		if !row.AutoUp || used[row.ID] == 0 || st.State == StateUp || st.State == StateStarting || m.isStopped(row.ID) {
			continue
		}
		if n, err := m.network(row.ID); err == nil {
			m.startUp(n) // asynchronous; bringUp applies the retry backoff and the error cooldown
		}
	}
}

// errNoProcess marks a driver whose process vanished under it.
func errNoProcess(name string) error {
	return fmt.Errorf("%w: %s is not running", errNotRunning, name)
}
