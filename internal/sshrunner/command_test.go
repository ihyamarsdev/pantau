package sshrunner

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// A long-running command streams its output, and Stop ends it without a leak or a panic.
func TestStartCommandStreamsAndStops(t *testing.T) {
	t.Parallel()
	key, pub := newTestKey(t)
	sock := startAgent(t, key)
	port, _ := startTestSSHServer(t, pub, false)

	runner, err := Connect(Target{
		Host: "127.0.0.1", Port: port, User: "tester",
		Agent: true, AgentFingerprint: ssh.FingerprintSHA256(pub), AgentSocket: sock,
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	cmd, err := runner.StartCommand("kubectl port-forward svc/web :80 -n default")
	if err != nil {
		t.Fatalf("StartCommand: %v", err)
	}
	line := readLineWithin(t, cmd.Stdout(), 5*time.Second)
	if line+"\n" != testForwardLine {
		t.Fatalf("first line = %q, want %q", line, testForwardLine)
	}
	cmd.Stop()
	cmd.Stop() // idempotent
}

// Stderr kept for an error report is bounded, so a chatty command cannot grow memory.
func TestRemoteCommandStderrIsBounded(t *testing.T) {
	t.Parallel()
	c := &remoteCommand{}
	if _, err := c.Write([]byte(strings.Repeat("x", maxCommandStderr*3))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("tail")); err != nil {
		t.Fatal(err)
	}
	got := c.StderrTail()
	if len(got) > maxCommandStderr {
		t.Fatalf("stderr kept %d bytes, want at most %d", len(got), maxCommandStderr)
	}
	if !strings.HasSuffix(got, "tail") {
		t.Fatalf("stderr tail = %q, want the newest bytes", got[len(got)-8:])
	}
}

// StartCommand on a closed runner is refused instead of dereferencing a nil client.
func TestStartCommandOnClosedRunner(t *testing.T) {
	t.Parallel()
	r := &LiveSSHRunner{}
	if _, err := r.StartCommand("true"); err == nil {
		t.Fatal("StartCommand on a runner without a client succeeded")
	}
}

// readLineWithin reads one line from r or fails the test once the bound passes, so a hung stream cannot hang the run.
func readLineWithin(t *testing.T, r interface{ Read([]byte) (int, error) }, bound time.Duration) string {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		done <- result{strings.TrimSuffix(line, "\n"), err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("read: %v", res.err)
		}
		return res.line
	case <-time.After(bound):
		t.Fatal("no output from the remote command")
		return ""
	}
}
