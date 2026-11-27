package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// Kubernetes port-forward: a Service or Pod port is exposed on a local port of the machine running Pantau.
// External clusters run a local kubectl that binds the port itself. Hosts (k3s/RKE2) run kubectl on the host
// over SSH; its relay port is reached through the SSH connection and exposed on the same pool as "ssh -L".

var (
	errFwdInvalid = errors.New("invalid port forward target")
	errFwdPolicy  = errors.New("port forwarding is disabled for read-only clusters")
)

// forwardProc is what serves a kube forward: a kubectl process or a remote command. Stop ends it.
type forwardProc interface {
	Stop()
}

// forwardStartBudget bounds how long a kube forward may take to report that it listens.
const forwardStartBudget = 30 * time.Second

// portFree reports whether bind:port can be listened on right now.
func portFree(bind string, port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// inUseLocked reports whether a live forward already owns the local port. The caller holds m.mu.
func (m *portForwardManager) inUseLocked(port int) bool {
	for _, f := range m.forwards {
		if f.localPort == port {
			return true
		}
	}
	return false
}

// reservePort picks a local port for a kubectl process that binds it itself. The port is proven free and held
// out of every other allocation until releasePort, so two parallel requests never receive the same port.
func (m *portForwardManager) reservePort(localPort int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reserved == nil {
		m.reserved = make(map[int]bool)
	}
	if localPort != 0 {
		if localPort < m.min || localPort > m.max {
			return 0, fmt.Errorf("%w: %d is outside %d-%d", errFwdOutRange, localPort, m.min, m.max)
		}
		if m.reserved[localPort] || m.inUseLocked(localPort) || !portFree(m.bind, localPort) {
			return 0, fmt.Errorf("%w: %d", errFwdBusy, localPort)
		}
		m.reserved[localPort] = true
		return localPort, nil
	}
	for p := m.min; p <= m.max; p++ {
		if m.reserved[p] || m.inUseLocked(p) || !portFree(m.bind, p) {
			continue
		}
		m.reserved[p] = true
		return p, nil
	}
	return 0, errFwdRangeFull
}

// releasePort returns a reserved port to the pool once its forward has stopped.
func (m *portForwardManager) releasePort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reserved, port)
}

// add registers a forward and gives it the next ID.
func (m *portForwardManager) add(f *portForward) {
	f.mgr = m
	if f.conns == nil {
		f.conns = make(map[net.Conn]struct{})
	}
	m.mu.Lock()
	m.nextID++
	f.id = m.nextID
	m.forwards[f.id] = f
	m.mu.Unlock()
}

// stopAll tears down every forward. It runs on Pantau shutdown so no kubectl process outlives it.
func (m *portForwardManager) stopAll() {
	if m == nil {
		return
	}
	for _, f := range m.list() {
		m.stop(f.id)
	}
}

// StopPortForwards ends every live forward (SSH and Kubernetes). Call it before the process exits.
func (s *Server) StopPortForwards() {
	s.portFwd.stopAll()
}

// createKube starts a forward to a Service or Pod. A negative targetID names an external cluster, a positive one a host.
func (m *portForwardManager) createKube(ctx context.Context, targetID int64, target inspector.KubeForwardTarget, localPort int) (*portForward, error) {
	if m == nil {
		return nil, errFwdDisabled
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", errFwdInvalid, err)
	}
	if inspector.IsKubeClusterTarget(targetID) {
		clusterID := -targetID
		cluster, err := m.db.GetKubeCluster(clusterID)
		if err != nil {
			return nil, fmt.Errorf("lookup cluster: %w", err)
		}
		if cluster == nil {
			return nil, fmt.Errorf("%w: cluster %d", errFwdNoHost, clusterID)
		}
		if cluster.KubeMode == "read-only" {
			return nil, errFwdPolicy
		}
		return m.createKubeLocal(ctx, cluster, target, localPort)
	}
	host, err := m.db.GetHost(targetID)
	if err != nil {
		return nil, fmt.Errorf("lookup host: %w", err)
	}
	if host == nil {
		return nil, fmt.Errorf("%w: host %d", errFwdNoHost, targetID)
	}
	if host.KubeMode == "read-only" {
		return nil, errFwdPolicy
	}
	return m.createKubeHost(ctx, host, target, localPort)
}

// createKubeLocal runs kubectl on this machine with the cluster's kubeconfig. kubectl binds the reserved port
// on the configured bind address, so no forwarding loop is needed here.
func (m *portForwardManager) createKubeLocal(ctx context.Context, cluster *store.KubeCluster, target inspector.KubeForwardTarget, localPort int) (*portForward, error) {
	port, err := m.reservePort(localPort)
	if err != nil {
		return nil, err
	}
	kubectl := m.kubectl
	if kubectl == "" {
		path, err := inspector.KubectlPath()
		if err != nil {
			m.releasePort(port)
			return nil, err
		}
		kubectl = path
	}
	fwd, _, err := inspector.StartLocalKubectlForward(ctx, kubectl, cluster.Kubeconfig, target, m.bind, fmt.Sprintf("%d:%d", port, target.RemotePort))
	if err != nil {
		m.releasePort(port)
		return nil, err
	}
	f := &portForward{
		hostName:   cluster.Name,
		remoteHost: target.Ref(),
		remotePort: target.RemotePort,
		localPort:  port,
		createdAt:  time.Now().UTC(),
		kind:       "kube",
		target:     kubeTargetLabel(target),
		proc:       fwd,
	}
	m.add(f)
	logger.Info("portfwd", cluster.Name, fmt.Sprintf("kube forward started: %s:%d -> %s", m.bind, port, f.target), "forward_id", f.id)
	return f, nil
}

// createKubeHost runs kubectl on the host over SSH and exposes the relay port it prints. The host connection
// carries the bytes, so the forward uses the same dial path as an SSH forward.
func (m *portForwardManager) createKubeHost(ctx context.Context, host *store.Host, target inspector.KubeForwardTarget, localPort int) (*portForward, error) {
	runner, err := m.getRunner(host)
	if err != nil {
		return nil, fmt.Errorf("connect to host %s: %w", host.Name, err)
	}
	starter, ok := runner.(sshrunner.CommandStarter)
	if !ok {
		_ = runner.Close()
		return nil, fmt.Errorf("host %s runner cannot run remote commands", host.Name)
	}
	body := fmt.Sprintf("trap 'kill 0' TERM HUP INT; kc port-forward --address 127.0.0.1 %s :%d -n %s & wait",
		shellQuote(target.Ref()), target.RemotePort, shellQuote(target.Namespace))
	cmd, err := starter.StartCommand(inspector.KubeShell(body))
	if err != nil {
		_ = runner.Close()
		return nil, fmt.Errorf("start kubectl on %s: %w", host.Name, err)
	}
	line, err := inspector.AwaitForwardReady(ctx, cmd.Stdout(), forwardStartBudget, io.Discard)
	if err != nil {
		tail := strings.TrimSpace(cmd.StderrTail())
		cmd.Stop()
		_ = runner.Close()
		if tail != "" {
			return nil, fmt.Errorf("kubectl port-forward on %s: %w: %s", host.Name, err, tail)
		}
		return nil, fmt.Errorf("kubectl port-forward on %s: %w", host.Name, err)
	}
	relay, ok := inspector.ParseForwardPort(line)
	if !ok {
		cmd.Stop()
		_ = runner.Close()
		return nil, fmt.Errorf("kubectl on %s printed an unexpected readiness line", host.Name)
	}
	ln, port, err := m.listen(localPort)
	if err != nil {
		cmd.Stop()
		_ = runner.Close()
		return nil, err
	}
	f := &portForward{
		hostID:     host.ID,
		hostName:   host.Name,
		remoteHost: "127.0.0.1",
		remotePort: relay,
		localPort:  port,
		createdAt:  time.Now().UTC(),
		kind:       "kube",
		target:     kubeTargetLabel(target),
		runner:     runner,
		ln:         ln,
		proc:       cmd,
	}
	m.add(f)
	go f.accept(ln)
	logger.Info("portfwd", host.Name, fmt.Sprintf("kube forward started: %s:%d -> %s", m.bind, port, f.target), "forward_id", f.id)
	return f, nil
}

// kubeTargetLabel is the human-readable target shown in the forwards list, for example "svc/web -n default".
func kubeTargetLabel(t inspector.KubeForwardTarget) string {
	return t.Ref() + " -n " + t.Namespace
}

// handleKubeForwards starts a forward to a Service or Pod (POST /api/kube/forwards).
func (s *Server) handleKubeForwards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.portFwd == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errFwdDisabled.Error()})
		return
	}
	var req struct {
		TargetID   int64  `json:"target_id"`
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Namespace  string `json:"namespace"`
		RemotePort int    `json:"remote_port"`
		LocalPort  int    `json:"local_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.LocalPort < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid local_port"})
		return
	}
	target := inspector.KubeForwardTarget{Kind: req.Kind, Name: req.Name, Namespace: req.Namespace, RemotePort: req.RemotePort}
	f, err := s.portFwd.createKube(r.Context(), req.TargetID, target, req.LocalPort)
	if err != nil {
		switch {
		case errors.Is(err, errFwdInvalid), errors.Is(err, errFwdOutRange):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, errFwdNoHost):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, errFwdPolicy):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, errFwdRangeFull), errors.Is(err, errFwdBusy):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusCreated, f.toJSON())
}
