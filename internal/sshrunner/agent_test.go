package sshrunner

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// serveListener runs handle for every accepted connection until the test ends. The cleanup closes the listener,
// every live connection and waits for the handlers, so nothing outlives the test.
func serveListener(t *testing.T, ln net.Listener, handle func(net.Conn)) {
	t.Helper()
	var (
		mu     sync.Mutex
		closed bool
		conns  = map[net.Conn]struct{}{}
		wg     sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if closed {
				mu.Unlock()
				_ = c.Close()
				return
			}
			conns[c] = struct{}{}
			wg.Add(1)
			mu.Unlock()
			go func() {
				defer wg.Done()
				handle(c)
			}()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		closed = true
		_ = ln.Close()
		for c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
}

// startAgent serves an in-memory keyring over a unix socket in t.TempDir() and returns the socket path.
func startAgent(t *testing.T, keys ...ed25519.PrivateKey) string {
	t.Helper()
	kr := agent.NewKeyring()
	for _, k := range keys {
		if err := kr.Add(agent.AddedKey{PrivateKey: k}); err != nil {
			t.Fatal(err)
		}
	}
	sock := filepath.Join(t.TempDir(), "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	serveListener(t, ln, func(c net.Conn) { _ = agent.ServeAgent(kr, c) })
	return sock
}

// startSSHServer accepts publickey auth only for accepted and returns the port it listens on.
func newTestKey(t *testing.T) (ed25519.PrivateKey, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func TestConnectWithSSHAgent(t *testing.T) {
	t.Parallel()
	keyA, pubA := newTestKey(t)
	keyB, pubB := newTestKey(t)
	fpA := ssh.FingerprintSHA256(pubA)
	fpB := ssh.FingerprintSHA256(pubB)

	tests := []struct {
		name        string
		agentKeys   []ed25519.PrivateKey
		serverKey   ssh.PublicKey
		fingerprint string
		socketPath  string // "" means a socket that is served by the test agent
		wantErr     error  // sentinel expected from errors.Is; nil means success
		wantAnyErr  bool   // a failure that is not one of the agent sentinels (server rejected the key)
	}{
		{name: "fingerprint selects the matching key", agentKeys: []ed25519.PrivateKey{keyA, keyB}, serverKey: pubA, fingerprint: fpA},
		{name: "no fingerprint offers the agent keys", agentKeys: []ed25519.PrivateKey{keyA}, serverKey: pubA},
		{name: "fingerprint not in agent fails closed", agentKeys: []ed25519.PrivateKey{keyB}, serverKey: pubA, fingerprint: fpA, wantErr: ErrSSHAgentKeyNotFound},
		{name: "empty agent fails closed", agentKeys: nil, serverKey: pubA, wantErr: ErrSSHAgentKeyNotFound},
		{name: "missing socket fails closed", serverKey: pubA, fingerprint: fpA, socketPath: "missing", wantErr: ErrSSHAgentUnavailable},
		{name: "agent key the server rejects is not retried with another key", agentKeys: []ed25519.PrivateKey{keyB}, serverKey: pubA, fingerprint: fpB, wantAnyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			port, _ := startTestSSHServer(t, tt.serverKey, false)
			sock := ""
			if tt.socketPath == "" {
				sock = startAgent(t, tt.agentKeys...)
			} else {
				sock = filepath.Join(t.TempDir(), tt.socketPath+".sock")
			}
			runner, err := Connect(Target{
				Host: "127.0.0.1", Port: port, User: "tester",
				Agent: true, AgentFingerprint: tt.fingerprint, AgentSocket: sock,
			}, 5*time.Second)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Connect error = %v, want %v", err, tt.wantErr)
				}
			case tt.wantAnyErr:
				if err == nil {
					t.Fatal("Connect succeeded, want auth failure")
				}
				if errors.Is(err, ErrSSHAgentKeyNotFound) || errors.Is(err, ErrSSHAgentUnavailable) {
					t.Fatalf("auth failure reported as agent error: %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("Connect: %v", err)
				}
				t.Cleanup(func() { _ = runner.Close() })
			}
		})
	}
}

// Fingerprint validation is only about the agent path: the same key with a different fingerprint never matches.
func TestAgentSignersFingerprintFilter(t *testing.T) {
	t.Parallel()
	keyA, pubA := newTestKey(t)
	keyB, _ := newTestKey(t)
	sock := startAgent(t, keyA, keyB)

	tests := []struct {
		name        string
		fingerprint string
		wantCount   int
	}{
		{name: "empty matches every key", fingerprint: "", wantCount: 2},
		{name: "matching fingerprint matches one key", fingerprint: ssh.FingerprintSHA256(pubA), wantCount: 1},
		{name: "unknown fingerprint matches none", fingerprint: "SHA256:unknown", wantCount: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			signers, conn, err := agentSigners(t.Context(), sock, tt.fingerprint)
			if tt.wantCount < 0 {
				if !errors.Is(err, ErrSSHAgentKeyNotFound) {
					t.Fatalf("error = %v, want ErrSSHAgentKeyNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if len(signers) != tt.wantCount {
				t.Fatalf("got %d signers, want %d", len(signers), tt.wantCount)
			}
		})
	}
}

// The explicit socket wins over the environment, so this test never depends on the developer's SSH_AUTH_SOCK.
func TestAgentSocketOverride(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "override.sock")
	got, err := agentSocket(sock)
	if err != nil || got != sock {
		t.Fatalf("agentSocket(%q) = %q, %v", sock, got, err)
	}
}
