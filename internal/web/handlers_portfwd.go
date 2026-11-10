package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// Port forwarding ("ssh -L"): a remote host:port is exposed on a local port of
// the machine running Pantau, so a local client (psql, DBeaver) can reach it.
//
// shortcut: forwards live in memory only and are lost when Pantau restarts.

var (
	errFwdDisabled  = errors.New("port forwarding is disabled")
	errFwdRangeFull = errors.New("no free local port in the configured range")
	errFwdBusy      = errors.New("local port is already in use")
	errFwdOutRange  = errors.New("local port is outside the allowed range")
	errFwdNoHost    = errors.New("host not found")
)

// hostnameLabelRe matches one DNS label: alphanumeric ends, hyphens inside.
var hostnameLabelRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// validateRemoteHost accepts an IP address or a DNS hostname. The value is
// never passed to a shell, but it is passed to the SSH server as a dial
// target, so keep it to plain host syntax.
func validateRemoteHost(h string) bool {
	h = strings.TrimSpace(h)
	if h == "" || len(h) > 253 {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	h = strings.TrimSuffix(h, ".")
	for _, label := range strings.Split(h, ".") {
		if !hostnameLabelRe.MatchString(label) {
			return false
		}
	}
	return true
}

// portForward is one live local forward.
type portForward struct {
	id         int64
	hostID     int64
	hostName   string
	remoteHost string
	remotePort int
	localPort  int
	createdAt  time.Time

	activeConns atomic.Int64
	bytesIn     atomic.Int64
	bytesOut    atomic.Int64
	lastErr     atomic.Value // string

	mu      sync.Mutex
	runner  sshrunner.Runner
	conns   map[net.Conn]struct{}
	ln      net.Listener
	stopped bool
	mgr     *portForwardManager
}

func (f *portForward) setError(err error) {
	if err == nil {
		return
	}
	f.lastErr.Store(err.Error())
}

func (f *portForward) errorText() string {
	v, _ := f.lastErr.Load().(string)
	return v
}

// runnerFor returns the SSH runner currently held by this forward.
func (f *portForward) runnerFor() sshrunner.Runner {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runner
}

func (f *portForward) setRunner(r sshrunner.Runner) {
	f.mu.Lock()
	f.runner = r
	f.mu.Unlock()
}

// track registers live connections so shutdown() can tear them down.
func (f *portForward) track(conns ...net.Conn) {
	f.mu.Lock()
	for _, c := range conns {
		f.conns[c] = struct{}{}
	}
	f.mu.Unlock()
}

func (f *portForward) untrack(conns ...net.Conn) {
	f.mu.Lock()
	for _, c := range conns {
		delete(f.conns, c)
	}
	f.mu.Unlock()
}

// portForwardManager owns every live forward and the local port range.
type portForwardManager struct {
	bind      string
	min, max  int
	db        *store.DB
	getRunner func(*store.Host) (sshrunner.Runner, error)

	mu       sync.Mutex
	forwards map[int64]*portForward
	nextID   int64
}

// SetPortForwardRange enables port forwarding on local ports min..max bound to
// the given address (the same bind address the UI listens on). A max below min
// disables the feature.
func (s *Server) SetPortForwardRange(bind string, min, max int) {
	if min <= 0 || max < min {
		s.portFwd = nil
		return
	}
	s.portFwd = &portForwardManager{
		bind:     bind,
		min:      min,
		max:      max,
		db:       s.db,
		forwards: make(map[int64]*portForward),
		getRunner: func(h *store.Host) (sshrunner.Runner, error) {
			if s.inspector == nil {
				return nil, fmt.Errorf("inspector not available")
			}
			return s.inspector.GetRunnerForHost(h)
		},
	}
}

// list returns a snapshot of the live forwards.
func (m *portForwardManager) list() []*portForward {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*portForward, 0, len(m.forwards))
	for _, f := range m.forwards {
		out = append(out, f)
	}
	return out
}

// create validates the request, opens the local listener and starts the forward.
func (m *portForwardManager) create(hostID int64, remoteHost string, remotePort, localPort int) (*portForward, error) {
	if m == nil {
		return nil, errFwdDisabled
	}
	host, err := m.db.GetHost(hostID)
	if err != nil {
		return nil, fmt.Errorf("lookup host: %w", err)
	}
	if host == nil {
		return nil, fmt.Errorf("%w: host %d", errFwdNoHost, hostID)

	}

	runner, err := m.getRunner(host)
	if err != nil {
		return nil, fmt.Errorf("connect to host %s: %w", host.Name, err)
	}

	ln, port, err := m.listen(localPort)
	if err != nil {
		_ = runner.Close()
		return nil, err
	}

	m.mu.Lock()
	m.nextID++
	f := &portForward{
		id:         m.nextID,
		hostID:     hostID,
		hostName:   host.Name,
		remoteHost: remoteHost,
		remotePort: remotePort,
		localPort:  port,
		createdAt:  time.Now().UTC(),
		runner:     runner,
		conns:      make(map[net.Conn]struct{}),
		ln:         ln,
		mgr:        m,
	}
	m.forwards[f.id] = f
	m.mu.Unlock()

	go f.accept(ln)
	logger.Info("portfwd", host.Name, fmt.Sprintf("forward started: %s:%d -> %s:%d", m.bind, port, remoteHost, remotePort), "forward_id", f.id, "local_port", port)
	return f, nil
}

// listen binds the requested local port, or the first free port in the range
// when localPort is 0.
func (m *portForwardManager) listen(localPort int) (net.Listener, int, error) {
	if localPort != 0 {
		if localPort < m.min || localPort > m.max {
			return nil, 0, fmt.Errorf("%w: %d is outside %d-%d", errFwdOutRange, localPort, m.min, m.max)
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(m.bind, strconv.Itoa(localPort)))
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %d", errFwdBusy, localPort)
		}
		return ln, localPort, nil
	}
	for p := m.min; p <= m.max; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(m.bind, strconv.Itoa(p)))
		if err == nil {
			return ln, p, nil
		}
	}
	return nil, 0, errFwdRangeFull
}

// stop tears down one forward: listener, live connections and the SSH runner.
func (m *portForwardManager) stop(id int64) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	f, ok := m.forwards[id]
	if ok {
		delete(m.forwards, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	f.shutdown()
	logger.Info("portfwd", f.hostName, fmt.Sprintf("forward stopped: local port %d", f.localPort), "forward_id", f.id)
	return true
}

// stopHost stops every forward of a host (called when the host is deleted).
func (m *portForwardManager) stopHost(hostID int64) {
	if m == nil {
		return
	}
	for _, f := range m.list() {
		if f.hostID == hostID {
			m.stop(f.id)
		}
	}
}

// shutdown closes the listener, all live connections and the SSH runner.
func (f *portForward) shutdown() {
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return
	}
	f.stopped = true
	conns := make([]net.Conn, 0, len(f.conns))
	for c := range f.conns {
		conns = append(conns, c)
	}
	runner := f.runner
	ln := f.ln
	f.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if runner != nil {
		_ = runner.Close()
	}
}

// accept serves the local listener until it is closed.
func (f *portForward) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

// handle bridges one local connection to the remote port over SSH.
func (f *portForward) handle(local net.Conn) {
	f.activeConns.Add(1)
	defer f.activeConns.Add(-1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	remote, err := f.dialRemote(ctx)
	cancel()
	if err != nil {
		f.setError(err)
		logger.Error("portfwd", f.hostName, "forward dial failed: "+err.Error(), "forward_id", f.id)
		_ = local.Close()
		return
	}

	f.track(local, remote)
	defer func() {
		f.untrack(local, remote)
		_ = local.Close()
		_ = remote.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countingWriter{w: remote, n: &f.bytesOut}, local)

		closeWrite(remote)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countingWriter{w: local, n: &f.bytesIn}, remote)

		closeWrite(local)
	}()
	wg.Wait()
}

// countingWriter counts the bytes written through it into a counter, so the
// traffic counters are live while a connection is still open.
type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		c.n.Add(int64(n))
	}
	return n, err
}

// closeWrite half-closes a TCP connection so the peer sees EOF.
func closeWrite(c net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

// dialRemote opens the SSH direct-tcpip channel to the remote target. When the
// SSH connection died, reconnect once and retry.
func (f *portForward) dialRemote(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(f.remoteHost, strconv.Itoa(f.remotePort))
	runner := f.runnerFor()
	if runner == nil {
		return nil, fmt.Errorf("ssh runner is not available")
	}
	conn, err := runner.DialContext(ctx, "tcp", addr)
	if err == nil {
		return conn, nil
	}
	m := f.mgr
	if m == nil {
		return nil, err
	}
	host, hErr := m.db.GetHost(f.hostID)
	if hErr != nil || host == nil {
		return nil, err
	}
	fresh, fErr := m.getRunner(host)
	if fErr != nil {
		return nil, err
	}
	old := f.runnerFor()
	f.setRunner(fresh)
	if old != nil && old != fresh {
		_ = old.Close()
	}
	conn, rErr := fresh.DialContext(ctx, "tcp", addr)
	if rErr != nil {
		return nil, err
	}
	return conn, nil
}

// portForwardJSON is the wire representation of a forward.
type portForwardJSON struct {
	ID          int64  `json:"id"`
	HostID      int64  `json:"host_id"`
	HostName    string `json:"host_name"`
	RemoteHost  string `json:"remote_host"`
	RemotePort  int    `json:"remote_port"`
	LocalPort   int    `json:"local_port"`
	CreatedAt   string `json:"created_at"`
	ActiveConns int64  `json:"active_conns"`
	BytesIn     int64  `json:"bytes_in"`
	BytesOut    int64  `json:"bytes_out"`
	LastError   string `json:"last_error"`
}

func (f *portForward) toJSON() portForwardJSON {
	return portForwardJSON{
		ID:          f.id,
		HostID:      f.hostID,
		HostName:    f.hostName,
		RemoteHost:  f.remoteHost,
		RemotePort:  f.remotePort,
		LocalPort:   f.localPort,
		CreatedAt:   f.createdAt.Format(time.RFC3339),
		ActiveConns: f.activeConns.Load(),
		BytesIn:     f.bytesIn.Load(),
		BytesOut:    f.bytesOut.Load(),
		LastError:   f.errorText(),
	}
}

// registerPortForwardRoutes wires the /api/forwards endpoints.
func (s *Server) registerPortForwardRoutes() {
	s.mux.HandleFunc("/api/forwards", s.authMiddleware(s.handlePortForwards))
	s.mux.HandleFunc("/api/forwards/", s.authMiddleware(s.handlePortForwardDetail))
}

// handlePortForwards lists forwards (GET) or creates one (POST).
func (s *Server) handlePortForwards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.portFwd == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errFwdDisabled.Error()})
			return
		}
		list := s.portFwd.list()
		out := make([]portForwardJSON, 0, len(list))
		for _, f := range list {
			out = append(out, f.toJSON())
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var req struct {
			HostID     int64  `json:"host_id"`
			RemoteHost string `json:"remote_host"`
			RemotePort int    `json:"remote_port"`
			LocalPort  int    `json:"local_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if s.portFwd == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errFwdDisabled.Error()})
			return
		}
		remoteHost := strings.TrimSpace(req.RemoteHost)
		if remoteHost == "" {
			remoteHost = "127.0.0.1"
		}
		if !validateRemoteHost(remoteHost) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid remote_host: expected an IP address or hostname"})
			return
		}
		if req.RemotePort < 1 || req.RemotePort > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid remote_port: must be 1-65535"})
			return
		}
		if req.LocalPort < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid local_port"})
			return
		}
		f, err := s.portFwd.create(req.HostID, remoteHost, req.RemotePort, req.LocalPort)
		if err != nil {
			switch {
			case errors.Is(err, errFwdNoHost):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			case errors.Is(err, errFwdOutRange):
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			case errors.Is(err, errFwdRangeFull), errors.Is(err, errFwdBusy):
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			default:
				// SSH connect failures and other setup errors are upstream failures.
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			return
		}
		writeJSON(w, http.StatusCreated, f.toJSON())
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handlePortForwardDetail stops a forward (DELETE /api/forwards/{id}).
func (s *Server) handlePortForwardDetail(w http.ResponseWriter, r *http.Request) {
	idStr := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/forwards/"), "/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid forward id"})
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.portFwd == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errFwdDisabled.Error()})
		return
	}
	if !s.portFwd.stop(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "forward not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}
