package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// setupPortFwdTestServer builds a server whose SSH runner dials a real local
// TCP echo server, so a forward can be exercised end to end.
func setupPortFwdTestServer(t *testing.T, runner *sshrunner.MockRunner) (*Server, *store.DB, func(*http.Request)) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau-pf-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	db, err := store.Open(filepath.Join(tmpDir, "pantau.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "test_portfwd_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, db, authReq
}

// echoServer starts a TCP server that echoes every byte it receives.
func echoServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

// freePortRange returns a range of two free local ports.
func freePortRange(t *testing.T) (int, int) {
	t.Helper()
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	p1 := l1.Addr().(*net.TCPAddr).Port
	p2 := l2.Addr().(*net.TCPAddr).Port
	if p2 < p1 {
		p1, p2 = p2, p1
	}
	return p1, p1 + 1
}

func TestPortForwardCarriesBytes(t *testing.T) {
	echoAddr, stopEcho := echoServer(t)
	defer stopEcho()

	runner := sshrunner.NewMockRunner()
	runner.DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, echoAddr)
	}

	srv, db, authReq := setupPortFwdTestServer(t, runner)
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "db", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/forwards", strings.NewReader(fmt.Sprintf(
		`{"host_id":%d,"remote_host":"127.0.0.1","remote_port":5432}`, hostID)))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create forward = %d, want 201: %s", w.Code, w.Body.String())
	}
	var fwd portForwardJSON
	if err := json.Unmarshal(w.Body.Bytes(), &fwd); err != nil {
		t.Fatal(err)
	}
	if fwd.LocalPort < minPort || fwd.LocalPort > maxPort {
		t.Fatalf("local port %d outside range %d-%d", fwd.LocalPort, minPort, maxPort)
	}
	t.Cleanup(func() { srv.portFwd.stop(fwd.ID) })

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwd.LocalPort), 2*time.Second)
	if err != nil {
		t.Fatalf("dial forward: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello-pg")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("hello-pg"))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "hello-pg" {
		t.Fatalf("echo = %q, want %q", buf, "hello-pg")
	}

	// The list endpoint reports the forward with its traffic counters.
	reqList := httptest.NewRequest(http.MethodGet, "/api/forwards", nil)
	authReq(reqList)
	wList := httptest.NewRecorder()
	srv.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("list forwards = %d", wList.Code)
	}
	var list []portForwardJSON
	if err := json.Unmarshal(wList.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != fwd.ID {
		t.Fatalf("list = %+v", list)
	}
	if list[0].BytesIn == 0 || list[0].BytesOut == 0 {
		t.Fatalf("traffic counters not counted: %+v", list[0])
	}
}

func TestPortForwardAutoPortAndRangeFull(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	runner.DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, "127.0.0.1:1")
	}
	srv, db, authReq := setupPortFwdTestServer(t, runner)
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "db", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	create := func(local int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/forwards", strings.NewReader(fmt.Sprintf(
			`{"host_id":%d,"remote_port":5432,"local_port":%d}`, hostID, local)))
		authReq(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	// Auto port picks the first free port of the range.
	w := create(0)
	if w.Code != http.StatusCreated {
		t.Fatalf("create auto = %d: %s", w.Code, w.Body.String())
	}
	var fwd portForwardJSON
	_ = json.Unmarshal(w.Body.Bytes(), &fwd)
	if fwd.LocalPort != minPort {
		t.Fatalf("auto local port = %d, want %d", fwd.LocalPort, minPort)
	}
	t.Cleanup(func() { srv.portFwd.stop(fwd.ID) })

	// The only other port in the range is taken by the first forward.
	w2 := create(0)
	if w2.Code != http.StatusCreated {
		t.Fatalf("create second = %d: %s", w2.Code, w2.Body.String())
	}
	var fwd2 portForwardJSON
	_ = json.Unmarshal(w2.Body.Bytes(), &fwd2)
	if fwd2.LocalPort != maxPort {
		t.Fatalf("second local port = %d, want %d", fwd2.LocalPort, maxPort)
	}
	t.Cleanup(func() { srv.portFwd.stop(fwd2.ID) })

	// Range is now full.
	w3 := create(0)
	if w3.Code != http.StatusConflict {
		t.Fatalf("create with full range = %d, want 409: %s", w3.Code, w3.Body.String())
	}
}

func TestPortForwardValidation(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	srv, db, authReq := setupPortFwdTestServer(t, runner)
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "db", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/forwards", strings.NewReader(body))
		authReq(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	if w := post(fmt.Sprintf(`{"host_id":%d,"remote_host":"bad host;rm -rf /","remote_port":5432}`, hostID)); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid remote_host = %d, want 400", w.Code)
	}
	if w := post(fmt.Sprintf(`{"host_id":%d,"remote_port":0}`, hostID)); w.Code != http.StatusBadRequest {
		t.Fatalf("remote_port 0 = %d, want 400", w.Code)
	}
	if w := post(fmt.Sprintf(`{"host_id":%d,"remote_port":70000}`, hostID)); w.Code != http.StatusBadRequest {
		t.Fatalf("remote_port 70000 = %d, want 400", w.Code)
	}
	if w := post(`{"host_id":9999,"remote_port":5432}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown host = %d, want 404", w.Code)
	}
}

func TestPortForwardDeleteStopsListener(t *testing.T) {
	echoAddr, stopEcho := echoServer(t)
	defer stopEcho()

	runner := sshrunner.NewMockRunner()
	runner.DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, echoAddr)
	}
	srv, db, authReq := setupPortFwdTestServer(t, runner)
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "db", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/forwards", strings.NewReader(fmt.Sprintf(
		`{"host_id":%d,"remote_port":5432}`, hostID)))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	var fwd portForwardJSON
	_ = json.Unmarshal(w.Body.Bytes(), &fwd)

	reqDel := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/forwards/%d", fwd.ID), nil)
	authReq(reqDel)
	wDel := httptest.NewRecorder()
	srv.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", wDel.Code, wDel.Body.String())
	}

	// The local listener is gone.
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwd.LocalPort), 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("forward still accepting connections after delete")
	}

	// Deleting again reports not found.
	reqDel2 := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/forwards/%d", fwd.ID), nil)
	authReq(reqDel2)
	wDel2 := httptest.NewRecorder()
	srv.ServeHTTP(wDel2, reqDel2)
	if wDel2.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", wDel2.Code)
	}
}

func TestPortForwardDisabledReturns503(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	srv, _, authReq := setupPortFwdTestServer(t, runner)

	req := httptest.NewRequest(http.MethodGet, "/api/forwards", nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("list when disabled = %d, want 503", w.Code)
	}
}

func TestPortForwardHostDeleteStopsForwards(t *testing.T) {
	echoAddr, stopEcho := echoServer(t)
	defer stopEcho()

	runner := sshrunner.NewMockRunner()
	runner.DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, echoAddr)
	}
	srv, db, authReq := setupPortFwdTestServer(t, runner)
	minPort, maxPort := freePortRange(t)
	srv.SetPortForwardRange("127.0.0.1", minPort, maxPort)

	hostID, err := db.CreateHost(&store.Host{Name: "db", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/forwards", strings.NewReader(fmt.Sprintf(
		`{"host_id":%d,"remote_port":5432}`, hostID)))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var fwd portForwardJSON
	_ = json.Unmarshal(w.Body.Bytes(), &fwd)

	reqDel := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/hosts/%d", hostID), nil)
	authReq(reqDel)
	wDel := httptest.NewRecorder()
	srv.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete host = %d: %s", wDel.Code, wDel.Body.String())
	}

	if got := len(srv.portFwd.list()); got != 0 {
		t.Fatalf("%d forwards survived the host deletion", got)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwd.LocalPort), 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("forward still accepting connections after the host was deleted")
	}
}
