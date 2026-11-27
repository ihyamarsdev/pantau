package web

import (
	"testing"

	"pantau/internal/store"
)

func TestValidSSHAgentSelection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		agent   bool
		fp      string
		wantErr bool
	}{
		{name: "agent without fingerprint offers all keys", agent: true, fp: ""},
		{name: "agent with SHA256 fingerprint", agent: true, fp: "SHA256:abcDEF+/="},
		{name: "agent with spaces around fingerprint", agent: true, fp: "  SHA256:abc  "},
		{name: "agent with MD5 style fingerprint is rejected", agent: true, fp: "MD5:aa:bb", wantErr: true},
		{name: "agent with bare prefix is rejected", agent: true, fp: "SHA256:", wantErr: true},
		{name: "agent with embedded space is rejected", agent: true, fp: "SHA256:a b", wantErr: true},
		{name: "not an agent host ignores the fingerprint", agent: false, fp: "garbage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := store.Host{SSHAgent: tt.agent, SSHAgentFingerprint: tt.fp}
			err := validSSHAgentSelection(&h)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validSSHAgentSelection(%q) error = %v, wantErr %v", tt.fp, err, tt.wantErr)
			}
		})
	}
}
