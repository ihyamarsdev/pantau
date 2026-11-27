package sshrunner

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// insecureAlgos are the algorithms a modern-profile client must never offer.
var insecureAlgos = []string{"3des-cbc", "aes128-cbc", "aes256-cbc", "diffie-hellman-group1-sha1", "ssh-dss"}

func TestConfigureSSHProfiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		legacy bool
		has    []string
		hasNot []string
	}{
		{
			name:   "modern profile excludes legacy algorithms",
			legacy: false,
			has:    []string{"curve25519-sha256", "aes256-gcm@openssh.com", "ssh-ed25519"},
			hasNot: insecureAlgos,
		},
		{
			name:   "legacy profile enables fallback algorithms",
			legacy: true,
			has:    []string{"diffie-hellman-group1-sha1", "3des-cbc", "aes128-cbc", "ssh-dss", "ssh-rsa"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &ssh.ClientConfig{}
			configureSSH(cfg, tt.legacy)

			all := slices.Concat(cfg.KeyExchanges, cfg.Ciphers, cfg.MACs, cfg.HostKeyAlgorithms)
			for _, alg := range tt.has {
				if !slices.Contains(all, alg) {
					t.Errorf("legacy=%v: expected %q in profile", tt.legacy, alg)
				}
			}
			for _, alg := range tt.hasNot {
				if slices.Contains(all, alg) {
					t.Errorf("legacy=%v: %q must not be offered", tt.legacy, alg)
				}
			}
			for _, kex := range cfg.KeyExchanges {
				if !tt.legacy && strings.Contains(kex, "sha1") {
					t.Errorf("modern KEX offers SHA-1 algorithm %q", kex)
				}
			}
		})
	}
}

func TestWithLegacySSHHint(t *testing.T) {
	t.Parallel()
	mismatch := errors.New("ssh: handshake failed: ssh: no common algorithm for key exchange")
	other := errors.New("connection reset by peer")
	tests := []struct {
		name     string
		err      error
		legacy   bool
		wantSame bool
		wantHint bool
	}{
		{name: "nil stays nil", err: nil, wantSame: true},
		{name: "legacy host keeps raw error", err: mismatch, legacy: true, wantSame: true},
		{name: "unrelated error untouched", err: other, wantSame: true},
		{name: "modern mismatch gets hint", err: mismatch, wantHint: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := withLegacySSHHint(tt.err, tt.legacy)
			if tt.wantSame && got != tt.err {
				t.Fatalf("expected same error, got %v", got)
			}
			if tt.wantHint {
				if !errors.Is(got, mismatch) {
					t.Errorf("hint must wrap the original error with %%w, got %v", got)
				}
				if !strings.Contains(got.Error(), "Allow Legacy SSH Ciphers") {
					t.Errorf("hint missing operator guidance: %v", got)
				}
			}
		})
	}
}

// TestHandshakeAlgorithmNegotiation runs real SSH handshakes over an in-memory pipe.
func TestHandshakeAlgorithmNegotiation(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	modernServer := &ssh.ServerConfig{NoClientAuth: true}
	modernServer.AddHostKey(signer)
	// A server that only offers a SHA-1 key exchange, like an old CentOS/OpenSSH build.
	legacyServer := &ssh.ServerConfig{NoClientAuth: true, Config: ssh.Config{KeyExchanges: []string{"diffie-hellman-group1-sha1"}}}
	legacyServer.AddHostKey(signer)

	tests := []struct {
		name       string
		server     *ssh.ServerConfig
		legacy     bool
		wantOK     bool
		wantHinted bool
	}{
		{name: "modern client, modern server", server: modernServer, legacy: false, wantOK: true},
		{name: "legacy client, modern server", server: modernServer, legacy: true, wantOK: true},
		{name: "legacy client, legacy-only server", server: legacyServer, legacy: true, wantOK: true},
		{name: "modern client, legacy-only server", server: legacyServer, legacy: false, wantHinted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &ssh.ClientConfig{User: "root", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second}
			configureSSH(cfg, tt.legacy)

			err := handshake(t, tt.server, cfg)
			if tt.wantOK && err != nil {
				t.Fatalf("handshake failed: %v", err)
			}
			if !tt.wantOK {
				if err == nil {
					t.Fatal("expected handshake mismatch")
				}
				if got := withLegacySSHHint(err, tt.legacy); tt.wantHinted && !strings.Contains(got.Error(), "Allow Legacy SSH Ciphers") {
					t.Errorf("mismatch on modern host lacks hint: %v", got)
				}
			}
		})
	}
}

// handshake runs the server and client ends of one SSH connection over loopback TCP and returns the client error.
// net.Pipe is unusable here: both peers write their version line first, and a synchronous pipe deadlocks.
func handshake(t *testing.T, serverCfg *ssh.ServerConfig, clientCfg *ssh.ClientConfig) error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srvErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			srvErr <- err
			return
		}
		defer conn.Close()
		sc, chans, reqs, err := ssh.NewServerConn(conn, serverCfg)
		srvErr <- err
		if err != nil {
			return
		}
		defer sc.Close()
		go ssh.DiscardRequests(reqs)
		for c := range chans {
			_ = c.Reject(ssh.Prohibited, "")
		}
	}()

	cConn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cConn.Close()
	client, chans, reqs, err := ssh.NewClientConn(cConn, "test:22", clientCfg)
	if err == nil {
		go ssh.DiscardRequests(reqs)
		go func() {
			for c := range chans {
				_ = c.Reject(ssh.Prohibited, "")
			}
		}()
		_ = client.Close()
	}
	_ = cConn.Close()
	<-srvErr
	return err
}
