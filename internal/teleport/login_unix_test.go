//go:build unix

package teleport

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shQuote quotes a path for a POSIX shell script written by a test.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// writeFakeTSH writes an executable shell script in place of tsh. Interactive login runs through a real PTY, so the
// test exercises the same code path as production, with a script instead of the real tsh.
func writeFakeTSH(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tsh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// readUntil reads from r until the output contains want. A watchdog fails the test instead of hanging.
func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var acc []byte
		buf := make([]byte, 512)
		for !strings.Contains(string(acc), want) {
			n, err := r.Read(buf)
			acc = append(acc, buf[:n]...)
			if err != nil {
				ch <- result{string(acc), err}
				return
			}
		}
		ch <- result{string(acc), nil}
	}()
	select {
	case res := <-ch:
		if res.err != nil && !strings.Contains(res.out, want) {
			t.Fatalf("read ended before %q appeared: %v (output %q)", want, res.err, res.out)
		}
		return res.out
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %q in the terminal output", want)
	}
	return ""
}

func newLoginManager(t *testing.T, tshPath string) (*Manager, string) {
	t.Helper()
	dataDir := t.TempDir()
	m, err := NewManager(dataDir, tshPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.userHome = func() (string, error) { return "/home/tester", nil }
	return m, dataDir
}

func TestLoginSessionRoundTripAndExitCode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	envFile := filepath.Join(dir, "env")
	tsh := writeFakeTSH(t, "printf '%s\\n' \"$@\" > "+shQuote(argsFile)+"\n"+
		"printf '%s\\n' \"$KUBECONFIG\" > "+shQuote(envFile)+"\n"+
		"printf 'Enter password: '\n"+
		"read pw\n"+
		"printf 'authenticated\\n'\n"+
		"exit 0\n")
	m, dataDir := newLoginManager(t, tsh)

	s, err := m.StartLogin(testProfile, 100, 30)
	if err != nil {
		t.Fatalf("StartLogin() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	readUntil(t, s, "Enter password:")
	if err := s.Resize(120, 40); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if _, err := s.Write([]byte("hunter2\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	readUntil(t, s, "authenticated")

	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("login did not finish after the script exited")
	}
	code, err := s.Result()
	if code != 0 || err != nil {
		t.Fatalf("Result() = %d, %v; want 0, nil", code, err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"login", "--proxy=teleport.setoranku.com:443", "--browser=none", "--auth=local", "--user=alice", "tc"}
	if got := strings.Split(strings.TrimRight(string(args), "\n"), "\n"); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("tsh argv = %q, want %q", got, wantArgs)
	}
	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	wantKube, _ := KubeconfigPath(dataDir, testProfile.ID)
	if got := strings.TrimSpace(string(env)); got != wantKube {
		t.Fatalf("tsh KUBECONFIG = %q, want the isolated file %q", got, wantKube)
	}
	// The password is typed into the PTY only. It must never reach argv, the environment or a file Pantau wrote.
	if strings.Contains(string(args), "hunter2") || strings.Contains(string(env), "hunter2") {
		t.Fatal("the typed password leaked into argv or the environment")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() after exit error = %v", err)
	}
}

func TestLoginSessionReportsNonZeroExit(t *testing.T) {
	t.Parallel()
	tsh := writeFakeTSH(t, "printf 'denied\\n'\nexit 3\n")
	m, _ := newLoginManager(t, tsh)

	s, err := m.StartLogin(testProfile, 0, 0)
	if err != nil {
		t.Fatalf("StartLogin() error = %v", err)
	}
	defer s.Close()
	readUntil(t, s, "denied")
	<-s.Done()
	if code, _ := s.Result(); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
}

// Close must stop the whole process group and wait until the process is reaped, so no tsh process outlives the
// WebSocket that started it.
func TestLoginSessionCloseKillsProcess(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pid")
	tsh := writeFakeTSH(t, "echo $$ > "+shQuote(pidFile)+"\nprintf 'waiting\\n'\nexec sleep 600\n")
	m, _ := newLoginManager(t, tsh)

	s, err := m.StartLogin(testProfile, 80, 24)
	if err != nil {
		t.Fatalf("StartLogin() error = %v", err)
	}
	readUntil(t, s, "waiting")

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("Done is not closed after Close: the process was not reaped")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("tsh pid %d still alive after Close (kill 0 = %v)", pid, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

// One login per profile at a time, and the lock is released when the login ends.
func TestLoginSessionOnePerProfile(t *testing.T) {
	t.Parallel()
	tsh := writeFakeTSH(t, "printf 'waiting\\n'\nexec sleep 600\n")
	m, _ := newLoginManager(t, tsh)

	first, err := m.StartLogin(testProfile, 80, 24)
	if err != nil {
		t.Fatalf("first StartLogin() error = %v", err)
	}
	readUntil(t, first, "waiting")

	if _, err := m.StartLogin(testProfile, 80, 24); !errors.Is(err, ErrLoginInProgress) {
		t.Fatalf("second StartLogin() error = %v, want ErrLoginInProgress", err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := m.StartLogin(testProfile, 80, 24)
	if err != nil {
		t.Fatalf("StartLogin after the first ended error = %v, want nil", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}
