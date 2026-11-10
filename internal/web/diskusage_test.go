package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func setupDiskUsageServer(t *testing.T, runner *sshrunner.MockRunner) (*Server, *store.DB, func(*http.Request)) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau-du-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	db, err := store.Open(filepath.Join(tmpDir, "pantau.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)
	srv := NewServer(db, ins, nil)
	token := "test_du_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, db, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
}

// GNU du prints the requested path last; the parser must pick the total line and its children.
func TestParseDuOutputGNUStyle(t *testing.T) {
	out := "12\t/var/log\n300\t/var/lib\n48\t/var/cache\n360\t/var\n"
	total, entries, truncated := parseDuOutput(out, "/var")
	if total != 360*1024 {
		t.Errorf("total = %d, want %d", total, 360*1024)
	}
	if truncated {
		t.Error("truncated should be false")
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].Name != "lib" || entries[0].Bytes != 300*1024 || entries[0].Path != "/var/lib" {
		t.Errorf("first entry = %+v, want lib /var/lib 300KiB", entries[0])
	}
	if entries[1].Name != "cache" || entries[2].Name != "log" {
		t.Errorf("entries not sorted descending: %+v", entries)
	}
}

// BusyBox du prints the same shape; only the trailing slash on the root path differs.
func TestParseDuOutputBusyBox(t *testing.T) {
	out := "4\t/var/tmp\n8\t/var\n"
	total, entries, _ := parseDuOutput(out, "/var")
	if total != 8*1024 {
		t.Errorf("total = %d, want %d", total, 8*1024)
	}
	if len(entries) != 1 || entries[0].Name != "tmp" {
		t.Fatalf("entries = %+v, want one tmp entry", entries)
	}
}

func TestParseDuOutputPathsWithSpaces(t *testing.T) {
	out := "10\t/var/my dir\n4\t/var/plain\n14\t/var\n"
	total, entries, _ := parseDuOutput(out, "/var")
	if total != 14*1024 {
		t.Errorf("total = %d, want %d", total, 14*1024)
	}
	if len(entries) != 2 || entries[0].Name != "my dir" {
		t.Fatalf("entries = %+v, want a 'my dir' entry", entries)
	}
}

// A total line that does not match the requested path must not be used as the total.
func TestParseDuOutputIgnoresOtherTotalLines(t *testing.T) {
	out := "99\t/other\n10\t/var/log\n110\t/var\n"
	total, entries, _ := parseDuOutput(out, "/var")
	if total != 110*1024 {
		t.Errorf("total = %d, want %d", total, 110*1024)
	}
	if len(entries) != 1 || entries[0].Path != "/var/log" {
		t.Errorf("entries = %+v, want only /var/log", entries)
	}
}

func TestParseDuOutputCapsEntries(t *testing.T) {
	var b strings.Builder
	for i := 0; i < duMaxEntries+20; i++ {
		fmt.Fprintf(&b, "%d\t/var/d%04d\n", i+1, i)
	}
	b.WriteString("999999\t/var\n")
	total, entries, truncated := parseDuOutput(b.String(), "/var")
	if !truncated {
		t.Error("truncated should be true")
	}
	if len(entries) != duMaxEntries {
		t.Errorf("entries = %d, want %d", len(entries), duMaxEntries)
	}
	if entries[0].Bytes != (duMaxEntries+20)*1024 {
		t.Errorf("largest entry = %d, want %d", entries[0].Bytes, (duMaxEntries+20)*1024)
	}
	if total != 999999*1024 {
		t.Errorf("total = %d, want %d", total, 999999*1024)
	}
}

func TestCountDuDenied(t *testing.T) {
	stderr := "du: cannot access '/var/a': Permission denied\ndu: cannot access '/var/b': Permission denied\nother error\n"
	if n := countDuDenied(stderr); n != 2 {
		t.Errorf("denied = %d, want 2", n)
	}
}

func TestDiskUsageHandlerValidation(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	srv, db, authReq := setupDiskUsageServer(t, runner)
	hostID, err := db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		url  string
		want int
	}{
		{"relative path", fmt.Sprintf("/api/du?host_id=%d&path=var/log", hostID), http.StatusBadRequest},
		{"empty path", fmt.Sprintf("/api/du?host_id=%d&path=", hostID), http.StatusBadRequest},
		{"newline in path", fmt.Sprintf("/api/du?host_id=%d&path=/var/%%0A", hostID), http.StatusBadRequest},
		{"bad as user", fmt.Sprintf("/api/du?host_id=%d&path=/var&as=root%%3Brm+-rf+/", hostID), http.StatusBadRequest},
		{"missing host", "/api/du?host_id=999999&path=/var", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		authReq(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s: code = %d, want %d (%s)", tc.name, w.Code, tc.want, w.Body.String())
		}
	}

	// Unauthenticated requests are rejected before anything reaches the host.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/du?host_id=%d&path=/var", hostID), nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated code = %d, want 401", w.Code)
	}
}

func TestDiskUsageHandlerRunsDu(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	var mu sync.Mutex
	var captured string
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		mu.Lock()
		captured = cmd
		mu.Unlock()
		return "300\t/var/lib\n360\t/var\n", "du: cannot access '/var/secret': Permission denied\n", 1, nil
	}
	srv, db, authReq := setupDiskUsageServer(t, runner)
	hostID, _ := db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root"})

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/du?host_id=%d&path=/var", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	var res duResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.TotalBytes != 360*1024 {
		t.Errorf("total = %d, want %d", res.TotalBytes, 360*1024)
	}
	if len(res.Entries) != 1 || res.Entries[0].Name != "lib" {
		t.Errorf("entries = %+v", res.Entries)
	}
	if res.Denied != 1 {
		t.Errorf("denied = %d, want 1", res.Denied)
	}
	if res.TimedOut {
		t.Error("timed_out should be false")
	}

	mu.Lock()
	cmd := captured
	mu.Unlock()
	if !strings.Contains(cmd, "'/var'") {
		t.Errorf("command does not quote the path: %s", cmd)
	}
	if !strings.Contains(cmd, "du -x -k") {
		t.Errorf("command does not run du: %s", cmd)
	}
	if !strings.Contains(cmd, "-d 1") && !strings.Contains(cmd, "--max-depth=1") {
		t.Errorf("command has no depth limit: %s", cmd)
	}
}

// A second scan for the same host is rejected while the first one is still running.
func TestDiskUsageHandlerBusy(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	started := make(chan struct{})
	release := make(chan struct{})
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		close(started)
		<-release
		return "10\t/var\n", "", 0, nil
	}
	srv, db, authReq := setupDiskUsageServer(t, runner)
	hostID, _ := db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root"})

	go func() {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/du?host_id=%d&path=/var", hostID), nil)
		authReq(req)
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}()
	<-started

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/du?host_id=%d&path=/var", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("busy code = %d, want 429: %s", w.Code, w.Body.String())
	}
	close(release)
}

func TestDiskUsageHandlerRunAsUsesSudo(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	var mu sync.Mutex
	var captured string
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		mu.Lock()
		captured = cmd
		mu.Unlock()
		return "10\t/var\n", "", 0, nil
	}
	srv, db, authReq := setupDiskUsageServer(t, runner)
	hostID, _ := db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root"})

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/du?host_id=%d&path=/var&as=nobody", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	mu.Lock()
	cmd := captured
	mu.Unlock()
	if !strings.Contains(cmd, "sudo -n -u 'nobody' sh -c") {
		t.Errorf("run-as command does not use the file manager's sudo form: %s", cmd)
	}
}
