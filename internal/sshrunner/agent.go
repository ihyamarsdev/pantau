package sshrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ErrSSHAgentUnavailable means SSH_AUTH_SOCK is empty or the agent socket cannot be reached.
var ErrSSHAgentUnavailable = errors.New("ssh agent unavailable")

// ErrSSHAgentKeyNotFound means the agent is reachable but holds no key matching the Host's fingerprint.
var ErrSSHAgentKeyNotFound = errors.New("ssh agent key not found")

// agentSocket returns the override when set, otherwise the SSH_AUTH_SOCK of this process.
func agentSocket(override string) (string, error) {
	sock := strings.TrimSpace(override)
	if sock == "" {
		sock = strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
	}
	if sock == "" {
		return "", fmt.Errorf("%w: SSH_AUTH_SOCK is not set", ErrSSHAgentUnavailable)
	}
	return sock, nil
}

// AgentSocketPath reports the agent socket this process would use, without touching it.
func AgentSocketPath() string {
	sock, _ := agentSocket("")
	return sock
}

// AgentKey describes one key held by the local agent. It carries no private material.
type AgentKey struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
}

// ListAgentKeys returns the keys of the local agent, or an error wrapping ErrSSHAgentUnavailable.
func ListAgentKeys(ctx context.Context) ([]AgentKey, error) {
	signers, conn, err := agentSigners(ctx, "", "")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	keys := make([]AgentKey, 0, len(signers))
	for _, s := range signers {
		keys = append(keys, AgentKey{Type: s.PublicKey().Type(), Fingerprint: ssh.FingerprintSHA256(s.PublicKey())})
	}
	return keys, nil
}

// AgentPublicKey returns the authorized_keys line of the agent key matching fingerprint (fail-closed).
// It carries only public material, so Ansible can pin the agent key without any private key on disk.
func AgentPublicKey(ctx context.Context, fingerprint string) (string, error) {
	signers, conn, err := agentSigners(ctx, "", fingerprint)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signers[0].PublicKey()))), nil
}

// AgentAvailable reports whether SSH_AUTH_SOCK is set and answers. It is the status indicator in the UI.
func AgentAvailable(ctx context.Context) bool {
	sock, err := agentSocket("")
	if err != nil {
		return false
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// agentSigners connects to the agent and returns the keys matching fingerprint (every key when it is empty).
// The caller owns the returned connection and must close it once the signers are no longer used.
// No match is an error: a Host pinned to a fingerprint must never be authenticated with another key.
func agentSigners(ctx context.Context, socket, fingerprint string) ([]ssh.Signer, io.Closer, error) {
	sock, err := agentSocket(socket)
	if err != nil {
		return nil, nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: dial %s: %v", ErrSSHAgentUnavailable, sock, err)
	}
	all, err := agent.NewClient(conn).Signers()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("%w: list keys: %v", ErrSSHAgentUnavailable, err)
	}
	want := strings.TrimSpace(fingerprint)
	var matched []ssh.Signer
	for _, s := range all {
		if want == "" || ssh.FingerprintSHA256(s.PublicKey()) == want {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		_ = conn.Close()
		if want == "" {
			return nil, nil, fmt.Errorf("%w: the agent holds no keys", ErrSSHAgentKeyNotFound)
		}
		return nil, nil, fmt.Errorf("%w: no agent key has fingerprint %s", ErrSSHAgentKeyNotFound, want)
	}
	return matched, conn, nil
}

// agentAuth builds the publickey auth method backed by the agent. It is used only during the handshake.
func agentAuth(ctx context.Context, t Target) (ssh.AuthMethod, io.Closer, error) {
	signers, conn, err := agentSigners(ctx, t.AgentSocket, t.AgentFingerprint)
	if err != nil {
		return nil, nil, err
	}
	return ssh.PublicKeys(signers...), conn, nil
}
