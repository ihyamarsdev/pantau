package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// handleSSHAgent reports whether the local ssh-agent answers and which key fingerprints it holds.
// It returns public metadata only, so the Host editor can offer a fingerprint without touching key material.
func (s *Server) handleSSHAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	keys, err := sshrunner.ListAgentKeys(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"available": false, "keys": []sshrunner.AgentKey{}, "reason": agentReason(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"available": true, "keys": keys})
}

// agentReason keeps the status indicator honest without exposing the socket path.
func agentReason(err error) string {
	if errors.Is(err, sshrunner.ErrSSHAgentUnavailable) {
		return "unavailable"
	}
	return "error"
}

// ansibleKeyFields returns what an Ansible run needs to reach h. An agent Host gets its public key pinned from the
// agent (or just an agent check without a fingerprint) and never a private key; failure refuses the run.
func ansibleKeyFields(db *store.DB, h *store.Host) (privKey, agentPub string, err error) {
	if !h.SSHAgent {
		privKey, err = db.ResolveHostPrivateKey(h)
		return privKey, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if strings.TrimSpace(h.SSHAgentFingerprint) != "" {
		agentPub, err = sshrunner.AgentPublicKey(ctx, h.SSHAgentFingerprint)
		return "", agentPub, err
	}
	keys, err := sshrunner.ListAgentKeys(ctx)
	if err != nil {
		return "", "", err
	}
	if len(keys) == 0 {
		return "", "", fmt.Errorf("%w: the agent holds no keys", sshrunner.ErrSSHAgentKeyNotFound)
	}
	return "", "", nil
}

// validSSHAgentSelection rejects a malformed fingerprint before it is stored. An empty fingerprint is valid:
// the agent then offers all of its keys.
func validSSHAgentSelection(h *store.Host) error {
	if !h.SSHAgent {
		return nil
	}
	fp := strings.TrimSpace(h.SSHAgentFingerprint)
	if fp == "" {
		return nil
	}
	if !strings.HasPrefix(fp, "SHA256:") || len(fp) <= len("SHA256:") || strings.ContainsAny(fp, " \t\r\n") {
		return errors.New("invalid ssh_agent_fingerprint: expected an OpenSSH SHA256:<base64> fingerprint")
	}
	return nil
}
