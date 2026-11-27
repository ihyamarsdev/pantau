package ansible

import (
	"os"
	"strings"
)

// keyMaterial is what a run writes to the target's identity file. An SSH-agent Host has no private key, so the
// file holds only its public key: ssh then asks the agent for the matching private key, which never leaves it.
func (t Target) keyMaterial() string {
	if p := strings.TrimSpace(t.PrivateKey); p != "" {
		return p + "\n"
	}
	if p := strings.TrimSpace(t.AgentPublicKey); p != "" {
		return p + "\n"
	}
	return ""
}

// agentEnv passes SSH_AUTH_SOCK to the child process only when a target authenticates through the agent.
// Ansible is never given a private key for such a target, so the socket is the only route to its key.
func agentEnv(targets []Target) []string {
	for _, t := range targets {
		if !t.SSHAgent {
			continue
		}
		if sock := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")); sock != "" {
			return []string{"SSH_AUTH_SOCK=" + sock}
		}
		return nil
	}
	return nil
}
