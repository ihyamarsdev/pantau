package sshrunner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// waitEnded fails the test when the connection behind ended does not close within a generous bound.
// The bound only protects against a hang; success never waits on it.
func waitEnded(t *testing.T, ended <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s was not closed", what)
	}
}

// closedPort returns a loopback port with nothing listening on it.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestConnectViaBastionChain(t *testing.T) {
	t.Parallel()
	key, pub := newTestKey(t)
	sock := startAgent(t, key)
	fp := ssh.FingerprintSHA256(pub)

	tests := []struct {
		name     string
		bastions int  // forwarding hops between Pantau and the Host
		hostUp   bool // false: the Host port has no listener, so the last hop cannot reach it
	}{
		{name: "one bastion reaches the host", bastions: 1, hostUp: true},
		{name: "two bastions reach the host", bastions: 2, hostUp: true},
		{name: "unreachable host fails and closes the bastion", bastions: 1, hostUp: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				hostPort  int
				hostEnded <-chan struct{}
			)
			if tt.hostUp {
				hostPort, hostEnded = startTestSSHServer(t, pub, false)
			} else {
				hostPort = closedPort(t)
			}

			hops := make([]Target, 0, tt.bastions)
			ended := make([]<-chan struct{}, 0, tt.bastions)
			for range tt.bastions {
				port, e := startTestSSHServer(t, pub, true)
				hops = append(hops, Target{Host: "127.0.0.1", Port: port, User: "tester", Agent: true, AgentFingerprint: fp, AgentSocket: sock})
				ended = append(ended, e)
			}

			target := Target{
				Host: "127.0.0.1", Port: hostPort, User: "tester",
				Agent: true, AgentFingerprint: fp, AgentSocket: sock,
				Via: Chain(hops),
			}
			runner, err := Connect(target, 5*time.Second)
			if !tt.hostUp {
				if err == nil {
					t.Fatal("Connect succeeded to a host with no listener")
				}
				waitEnded(t, ended[0], "bastion after failed connect")
				return
			}
			if err != nil {
				t.Fatalf("Connect via %d bastion(s): %v", tt.bastions, err)
			}

			if err := runner.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			// Close is idempotent: a second call reports the same result and does not panic.
			if err := runner.Close(); err != nil {
				t.Fatalf("second Close: %v", err)
			}
			waitEnded(t, hostEnded, "host connection after Close")
			for i, e := range ended {
				waitEnded(t, e, fmt.Sprintf("bastion %d connection after Close", i+1))
			}
		})
	}
}

func TestViaDepthGuard(t *testing.T) {
	t.Parallel()
	// A chain longer than maxViaDepth is refused before any socket is opened.
	hops := make([]Target, maxViaDepth+1)
	for i := range hops {
		hops[i] = Target{Host: "127.0.0.1", Port: 1, User: "tester", Key: ""}
	}
	_, err := Connect(Target{Host: "127.0.0.1", Port: 2, User: "tester", Via: Chain(hops)}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Fatalf("Connect over %d hops error = %v, want the depth guard", len(hops), err)
	}
}

func TestChainLinksHopsInDialOrder(t *testing.T) {
	t.Parallel()
	first := Target{Host: "first"}
	second := Target{Host: "second"}
	last := Chain([]Target{first, second})
	if last == nil || last.Host != "second" {
		t.Fatalf("Chain last hop = %+v, want second", last)
	}
	if last.Via == nil || last.Via.Host != "first" {
		t.Fatalf("second hop Via = %+v, want first", last.Via)
	}
	if last.Via.Via != nil {
		t.Fatalf("first hop must be dialed directly, got Via %+v", last.Via.Via)
	}
	if Chain(nil) != nil {
		t.Fatal("Chain of no hops must be nil")
	}
}

// A missing agent socket fails before any bastion is dialed, so there is nothing to clean up.
func TestConnectMissingAgentFailsBeforeBastion(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "none.sock")
	target := Target{
		Host: "127.0.0.1", Port: 2, User: "tester", Agent: true, AgentSocket: missing,
		Via: &Target{Host: "127.0.0.1", Port: 1, User: "tester", Agent: true, AgentSocket: missing},
	}
	_, err := ConnectContext(context.Background(), target, time.Second)
	if !errors.Is(err, ErrSSHAgentUnavailable) {
		t.Fatalf("error = %v, want ErrSSHAgentUnavailable", err)
	}
}
