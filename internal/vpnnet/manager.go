package vpnnet

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pantau/internal/store"
)

// Tunnel states reported to the UI.
const (
	StateDown     = "down"
	StateStarting = "starting"
	StateUp       = "up"
	StateError    = "error"
)

const (
	upTimeout      = 60 * time.Second
	downTimeout    = 20 * time.Second
	statusTimeout  = 10 * time.Second
	retryBackoff   = 30 * time.Second
	errorCooldown  = 5 * time.Minute
	maxFails       = 3
	maxChildProcs  = 5
	logRingLines   = 200
	statusLoopTick = 30 * time.Second
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
}

type childProc struct {
	cmd  *exec.Cmd
	done chan struct{}
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
	locks   map[int64]*sync.Mutex
	attempt map[int64]time.Time
	fails   map[int64]int

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
		db:      opts.DB,
		root:    opts.Root,
		gate:    opts.Gate,
		log:     opts.Logger,
		procs:   map[int64]*childProc{},
		status:  map[int64]Status{},
		logs:    map[int64][]string{},
		locks:   map[int64]*sync.Mutex{},
		attempt: map[int64]time.Time{},
		fails:   map[int64]int{},
		ctx:     ctx,
		cancel:  cancel,
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

func (m *Manager) lockFor(id int64) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[id]
	if !ok {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	return l
}

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
	n := Network{ID: row.ID, Name: row.Name, Driver: row.Driver, Enabled: row.Enabled, AutoUp: row.AutoUp, Iface: row.Iface, Config: parseConfig(row.Config)}
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

// Ensure brings the network up when needed and returns the path a Host dials through.
// A nil path means "direct": the Host has no network or has network 0.
func (m *Manager) Ensure(ctx context.Context, networkID int64) (*Path, error) {
	if networkID == 0 {
		return nil, nil
	}
	if !m.gate() {
		return nil, ErrGateDisabled
	}
	n, err := m.network(networkID)
	if err != nil {
		return nil, err
	}
	if !n.Enabled {
		return nil, fmt.Errorf("%w: %s", ErrNetworkDisabled, n.Name)
	}
	l := m.lockFor(n.ID)
	l.Lock()
	defer l.Unlock()

	m.mu.Lock()
	st, has := m.status[n.ID]
	m.mu.Unlock()
	if has && st.State == StateUp && ifaceReady(st.Iface) {
		return m.pathFor(n, st.Iface, st.DNS), nil
	}
	if !n.AutoUp {
		if !has || st.State != StateUp {
			m.record(n, Status{State: StateDown, Detail: "auto-up disabled, start it from Networks"})
			return nil, fmt.Errorf("%w: %s", ErrNetworkDown, n.Name)
		}
		return m.pathFor(n, st.Iface, st.DNS), nil
	}
	st, err = m.bringUp(ctx, n)
	if err != nil {
		return nil, err
	}
	return m.pathFor(n, st.Iface, st.DNS), nil
}

// Up is the manual/ensure bring-up with backoff and the three-strike error cooldown.
func (m *Manager) Up(ctx context.Context, networkID int64) (Status, error) {
	if !m.gate() {
		return Status{State: StateDown, Err: ErrGateDisabled.Error()}, ErrGateDisabled
	}
	n, err := m.network(networkID)
	if err != nil {
		return Status{State: StateDown, Err: err.Error()}, err
	}
	l := m.lockFor(n.ID)
	l.Lock()
	defer l.Unlock()
	return m.bringUp(ctx, n)
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
	if fails > 0 && time.Since(last) < retryBackoff {
		err := fmt.Errorf("%w: %s (retrying soon)", ErrNetworkDown, n.Name)
		return Status{State: StateDown, Err: err.Error()}, err
	}
	if fails >= maxFails && time.Since(last) < errorCooldown {
		err := fmt.Errorf("%w: %s (gave up, retry manually)", ErrNetworkDown, n.Name)
		return Status{State: StateError, Err: err.Error()}, err
	}

	m.record(n, Status{State: StateStarting})
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
	m.mu.Unlock()
	m.record(n, st)
	return st, nil
}

// Down stops the tunnel (and the process Pantau started for it).
func (m *Manager) Down(ctx context.Context, networkID int64) error {
	if !m.gate() {
		return ErrGateDisabled
	}
	n, err := m.network(networkID)
	if err != nil {
		return err
	}
	l := m.lockFor(n.ID)
	l.Lock()
	defer l.Unlock()
	if d := driverFor(n.Driver); d != nil {
		if err := d.Down(ctx, m, n); err != nil {
			m.setError(n, err)
			return err
		}
	}
	m.stopChild(n.ID)
	m.mu.Lock()
	m.fails[n.ID] = 0
	m.status[n.ID] = Status{State: StateDown, Detail: "stopped", CheckedAt: time.Now()}
	m.mu.Unlock()
	_ = m.db.SetVPNNetworkState(n.ID, StateDown, "stopped")
	return nil
}

// Refresh asks the driver for the current state without bringing anything up.
func (m *Manager) Refresh(ctx context.Context, networkID int64) (Status, error) {
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

// Status returns the cached state (no I/O).
func (m *Manager) Status(networkID int64) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.status[networkID]; ok {
		return st
	}
	return Status{State: StateDown}
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
	m.status[n.ID] = st
	if st.State == StateUp {
		m.attempt[n.ID] = time.Now()
	}
	m.mu.Unlock()
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
	cmd.Env = m.childEnv()
	cmd.Dir = n.Root
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

	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			m.logf(n, "%s", sc.Text())
		}
		_ = cmd.Wait()
		_ = pw.Close()
		close(proc.done)
		m.mu.Lock()
		delete(m.procs, n.ID)
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) childRunning(id int64) bool {
	m.mu.Lock()
	_, ok := m.procs[id]
	m.mu.Unlock()
	return ok
}

// stopChild terminates a process Pantau started: SIGTERM, then a hard kill after 10s.
func (m *Manager) stopChild(id int64) {
	m.mu.Lock()
	proc := m.procs[id]
	m.mu.Unlock()
	if proc == nil || proc.cmd.Process == nil {
		return
	}
	_ = proc.cmd.Process.Signal(os.Interrupt)
	select {
	case <-proc.done:
	case <-time.After(10 * time.Second):
		_ = proc.cmd.Process.Kill()
		<-proc.done
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
			rows, err := m.db.ListVPNNetworks()
			if err != nil {
				continue
			}
			for _, row := range rows {
				if !row.Enabled || !m.gate() {
					continue
				}
				ctx, cancel := context.WithTimeout(m.ctx, statusTimeout)
				_, _ = m.Refresh(ctx, row.ID)
				cancel()
			}
		}
	}
}

// errNoProcess marks a driver whose process vanished under it.
func errNoProcess(name string) error {
	return fmt.Errorf("%w: %s is not running", errNotRunning, name)
}
