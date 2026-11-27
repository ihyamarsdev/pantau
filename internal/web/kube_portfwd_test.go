package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// kubeFakeRunner is a host runner that can start kubectl. Its remote command prints the readiness line for a fixed
// relay port, and its dials reach a local echo server, so the whole host path runs without SSH.
type kubeFakeRunner struct {
	*sshrunner.MockRunner
	relayPort int
	echoAddr  string
	stopped   atomic.Int32
	started   atomic.Int32
}

func newKubeFakeRunner(relayPort int, echoAddr string) *kubeFakeRunner {
	r := &kubeFakeRunner{MockRunner: sshrunner.NewMockRunner(), relayPort: relayPort, echoAddr: echoAddr}
	r.DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, echoAddr)
	}
	return r
}

func (r *kubeFakeRunner) StartCommand(cmd string) (sshrunner.Command, error) {
	if !strings.Contains(cmd, "port-forward") {
		return nil, fmt.Errorf("unexpected command %q", cmd)
	}
	r.started.Add(1)
	line := fmt.Sprintf("Forwarding from 127.0.0.1:%d -> 80\n", r.relayPort)
	return &kubeFakeCommand{out: strings.NewReader(line), owner: r}, nil
}

type kubeFakeCommand struct {
	out   io.Reader
	owner *kubeFakeRunner
	once  sync.Once
}

func (c *kubeFakeCommand) Stdout() io.Reader  { return c.out }
func (c *kubeFakeCommand) StderrTail() string { return "" }
func (c *kubeFakeCommand) Stop()              { c.once.Do(func() { c.owner.stopped.Add(1) }) }

// fakeKubectlScript writes a kubectl stand-in: it prints the readiness line for every local:remote mapping, records
// its pid in pidFile, and waits. The pid proves in a test that the process is gone after Stop.
func fakeKubectlScript(t *testing.T, pidFile string) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"echo \"$$\" > '" + pidFile + "'\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    *:*) echo \"Forwarding from 127.0.0.1:${a%%:*} -> ${a#*:}\";;\n" +
		"  esac\n" +
		"done\n" +
		"exec sleep 600\n"
	path := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// setupRunnerTestServer is setupPortFwdTestServer for any runner, so a test can hand out a runner that starts kubectl.
func setupRunnerTestServer(t *testing.T, runner sshrunner.Runner) (*Server, *store.DB, func(*http.Request)) {
	t.Helper()
	db, err := store.Open(testDBCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	factory := func(sshrunner.Target) (sshrunner.Runner, error) { return runner, nil }
	srv := NewServer(db, inspector.New(db, factory, nil), nil)
	token := "test_kube_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, db, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
}

func postKubeForward(t *testing.T, srv *Server, authReq func(*http.Request), body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/kube/forwards", strings.NewReader(body))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// Bad targets are refused before any host or cluster is looked up.
func TestKubeForwardRejectsBadTargets(t *testing.T) {
	t.Parallel()
	srv, _, authReq := setupPortFwdTestServer(t, sshrunner.NewMockRunner())
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown kind", body: `{"target_id":1,"kind":"deployment","name":"web","namespace":"default","remote_port":80}`},
		{name: "shell metacharacters in name", body: `{"target_id":1,"kind":"pod","name":"web;id","namespace":"default","remote_port":80}`},
		{name: "uppercase namespace", body: `{"target_id":1,"kind":"service","name":"web","namespace":"Default","remote_port":80}`},
		{name: "zero remote port", body: `{"target_id":1,"kind":"service","name":"web","namespace":"default","remote_port":0}`},
		{name: "negative local port", body: `{"target_id":1,"kind":"service","name":"web","namespace":"default","remote_port":80,"local_port":-1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := postKubeForward(t, srv, authReq, tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

// A read-only host or cluster never opens a forward, from either the host or the cluster side.
func TestKubeForwardReadOnlyIsRefused(t *testing.T) {
	t.Parallel()
	srv, db, authReq := setupPortFwdTestServer(t, sshrunner.NewMockRunner())
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "k3s-1", Host: "10.0.0.20", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := db.GetHost(hostID)
	if err != nil || h == nil {
		t.Fatalf("GetHost: %v", err)
	}
	h.KubeMode = "read-only"
	if err := db.UpdateHost(h); err != nil {
		t.Fatal(err)
	}
	clusterID, err := db.CreateKubeCluster(&store.KubeCluster{Name: "ro", Context: "c", Server: "https://10.0.0.1:6443", Provider: "generic", KubeMode: "read-only", Kubeconfig: "apiVersion: v1\n"})
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []int64{hostID, -clusterID} {
		body := fmt.Sprintf(`{"target_id":%d,"kind":"service","name":"web","namespace":"default","remote_port":80}`, target)
		w := postKubeForward(t, srv, authReq, body)
		if w.Code != http.StatusForbidden {
			t.Fatalf("target %d: status = %d, want 403: %s", target, w.Code, w.Body.String())
		}
	}
}

// A host forward carries bytes end to end and stops both the remote kubectl and the local listener.
func TestKubeForwardHostEndToEnd(t *testing.T) {
	t.Parallel()
	echoAddr, stopEcho := echoServer(t)
	defer stopEcho()
	runner := newKubeFakeRunner(4242, echoAddr)

	srv, db, authReq := setupRunnerTestServer(t, runner)
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "k3s-1", Host: "10.0.0.21", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	w := postKubeForward(t, srv, authReq, fmt.Sprintf(`{"target_id":%d,"kind":"service","name":"web","namespace":"default","remote_port":80}`, hostID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %s", w.Code, w.Body.String())
	}
	var fwd portForwardJSON
	if err := json.Unmarshal(w.Body.Bytes(), &fwd); err != nil {
		t.Fatal(err)
	}
	if fwd.Kind != "kube" || fwd.Target != "svc/web -n default" {
		t.Fatalf("forward kind/target = %q/%q", fwd.Kind, fwd.Target)
	}

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(fwd.LocalPort)))
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo through kube forward = %q, %v", buf, err)
	}
	_ = conn.Close()

	del := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/forwards/%d", fwd.ID), nil)
	authReq(del)
	wd := httptest.NewRecorder()
	srv.ServeHTTP(wd, del)
	if wd.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", wd.Code, wd.Body.String())
	}
	if runner.stopped.Load() != 1 {
		t.Fatalf("remote kubectl stopped %d times, want 1", runner.stopped.Load())
	}
	if _, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(fwd.LocalPort))); err == nil {
		t.Fatal("local port still accepts connections after stop")
	}
}

// An external cluster runs kubectl on this machine: the port is bound by it and the process dies with the forward.
func TestKubeForwardExternalClusterStopsKubectl(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	srv, db, authReq := setupPortFwdTestServer(t, sshrunner.NewMockRunner())
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)
	pidFile := filepath.Join(t.TempDir(), "pid")
	srv.portFwd.kubectl = fakeKubectlScript(t, pidFile)

	clusterID, err := db.CreateKubeCluster(&store.KubeCluster{Name: "ext", Context: "c", Server: "https://10.0.0.2:6443", Provider: "generic", KubeMode: "full", Kubeconfig: "apiVersion: v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	w := postKubeForward(t, srv, authReq, fmt.Sprintf(`{"target_id":%d,"kind":"pod","name":"web-0","namespace":"default","remote_port":8080}`, -clusterID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %s", w.Code, w.Body.String())
	}
	var fwd portForwardJSON
	if err := json.Unmarshal(w.Body.Bytes(), &fwd); err != nil {
		t.Fatal(err)
	}
	if fwd.LocalPort < minPort || fwd.LocalPort > maxPort {
		t.Fatalf("local port %d outside the pool %d-%d", fwd.LocalPort, minPort, maxPort)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if !srv.portFwd.stop(fwd.ID) {
		t.Fatalf("forward %d not found on stop", fwd.ID)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kubectl pid %d alive after stop (kill 0 = %v)", pid, err)
	}
}

// Parallel allocations never hand out the same port, and a full pool refuses rather than reusing a port.
func TestReservePortIsSafeUnderConcurrency(t *testing.T) {
	t.Parallel()
	srv, _, _ := setupPortFwdTestServer(t, sshrunner.NewMockRunner())
	base := findFreeRun(t, 5)
	srv.SetPortForwardRange("127.0.0.1", base, base+4)
	m := srv.portFwd

	const workers = 20
	type result struct {
		port int
		err  error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := m.reservePort(0)
			results <- result{p, err}
		}()
	}
	wg.Wait()
	close(results)

	seen := map[int]bool{}
	for r := range results {
		if errors.Is(r.err, errFwdRangeFull) {
			continue
		}
		if r.err != nil {
			t.Fatalf("reservePort: %v", r.err)
		}
		if seen[r.port] {
			t.Fatalf("port %d handed out twice", r.port)
		}
		seen[r.port] = true
	}
	if len(seen) != 5 {
		t.Fatalf("reserved %d ports, want the whole pool of 5", len(seen))
	}

	// A released port is the only one free again, and reservations hold against SSH forwards too.
	var released int
	for p := range seen {
		released = p
		break
	}
	m.releasePort(released)
	again, err := m.reservePort(0)
	if err != nil || again != released {
		t.Fatalf("after release reservePort = %d, %v; want %d", again, err, released)
	}
	if _, _, err := m.listen(released); !errors.Is(err, errFwdBusy) {
		t.Fatalf("listen on a reserved port = %v, want errFwdBusy", err)
	}
}

// findFreeRun returns the first base port of n consecutive loopback ports that are all free right now.
func findFreeRun(t *testing.T, n int) int {
	t.Helper()
	for base := 31000; base < 60000; base += 37 {
		free := true
		for p := base; p < base+n; p++ {
			if !portFree("127.0.0.1", p) {
				free = false
				break
			}
		}
		if free {
			return base
		}
	}
	t.Fatal("no free run of ports")
	return 0
}
