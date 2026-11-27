package store

import (
	"encoding/json"
	"testing"
)

// The SSH agent selection round-trips through CRUD and the System Snapshot, and it excludes stored keys.
func TestHostSSHAgentRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		agent       bool
		fingerprint string
		keyID       int64
		customKey   string
		wantKeyID   int64
		wantCustom  string
		wantFP      string
	}{
		{name: "default off keeps stored key", agent: false, keyID: 3, wantKeyID: 3},
		{name: "agent with fingerprint", agent: true, fingerprint: "SHA256:abc", wantFP: "SHA256:abc"},
		{name: "agent drops library and custom key", agent: true, keyID: 3, customKey: "pem", wantFP: ""},
		{name: "fingerprint without agent is cleared", agent: false, fingerprint: "SHA256:abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := openTestDB(t)
			id, err := db.CreateHost(&Host{
				Name: "agent-box", Host: "10.0.0.7", Port: 22, User: "root",
				SSHAgent: tt.agent, SSHAgentFingerprint: tt.fingerprint, KeyID: tt.keyID, CustomKey: tt.customKey,
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := db.GetHost(id)
			if err != nil || got == nil {
				t.Fatalf("GetHost: %v %v", got, err)
			}
			if got.SSHAgent != tt.agent || got.SSHAgentFingerprint != tt.wantFP {
				t.Fatalf("after create: agent=%v fp=%q, want agent=%v fp=%q", got.SSHAgent, got.SSHAgentFingerprint, tt.agent, tt.wantFP)
			}
			if got.KeyID != tt.wantKeyID || got.CustomKey != tt.wantCustom {
				t.Fatalf("after create: key_id=%d custom=%q, want key_id=%d custom=%q", got.KeyID, got.CustomKey, tt.wantKeyID, tt.wantCustom)
			}

			// Export via JSON, as the encrypted snapshot does, then import into a fresh database.
			payload, err := db.ExportSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var decoded SnapshotPayload
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			dst := openTestDB(t)
			if err := dst.ImportSnapshot(&decoded); err != nil {
				t.Fatal(err)
			}
			imported, err := dst.GetHost(id)
			if err != nil || imported == nil {
				t.Fatalf("imported host missing: %v", err)
			}
			if imported.SSHAgent != tt.agent || imported.SSHAgentFingerprint != tt.wantFP {
				t.Fatalf("after snapshot import: agent=%v fp=%q, want agent=%v fp=%q", imported.SSHAgent, imported.SSHAgentFingerprint, tt.agent, tt.wantFP)
			}
		})
	}
}

// An agent Host has no private key in the store, so no code path can fall back to the Global key.
func TestResolveHostPrivateKeyAgentHasNoKey(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.SetSetting("ssh_private_key", "GLOBAL-PEM"); err != nil {
		t.Fatal(err)
	}
	key, err := db.ResolveHostPrivateKey(&Host{SSHAgent: true})
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		t.Fatalf("agent host resolved key %q, want none", key)
	}
	key, err = db.ResolveHostPrivateKey(&Host{})
	if err != nil {
		t.Fatal(err)
	}
	if key != "GLOBAL-PEM" {
		t.Fatalf("plain host resolved %q, want the global key", key)
	}
}
