package snapshot

import (
	"context"
	"errors"
	"time"

	"pantau/internal/store"
)

// ErrInvalidStructure means the payload decrypted (so the GCM tag matched) but is not a usable snapshot.
var ErrInvalidStructure = errors.New("snapshot payload structure is invalid")

// VerifyResult summarises a verified snapshot. It holds counts and the timestamp only, never secret material.
type VerifyResult struct {
	Version   int          `json:"version"`
	CreatedAt time.Time    `json:"created_at"`
	Counts    VerifyCounts `json:"counts"`
}

type VerifyCounts struct {
	Hosts               int `json:"hosts"`
	SSHKeys             int `json:"ssh_keys"`
	DesiredRules        int `json:"desired_rules"`
	VaultSecrets        int `json:"vault_secrets"`
	AnsiblePlaybooks    int `json:"ansible_playbooks"`
	TerraformWorkspaces int `json:"terraform_workspaces"`
	VPNNetworks         int `json:"vpn_networks"`
}

// Verify decrypts a snapshot entirely in memory and checks its structure. It never writes to the database.
func Verify(data []byte, passphrase string) (*VerifyResult, error) {
	return verifyWith(data, passphrase, kdfIter)
}

// verifyWith is Verify with an explicit PBKDF2 iteration count, so tests can use a cheap count.
func verifyWith(data []byte, passphrase string, iterations int) (*VerifyResult, error) {
	payload, err := decryptWith(data, passphrase, iterations)
	if err != nil {
		return nil, err
	}
	return summarize(payload)
}

func summarize(p *store.SnapshotPayload) (*VerifyResult, error) {
	if p.Version < 1 || p.CreatedAt.IsZero() {
		return nil, ErrInvalidStructure
	}
	for _, h := range p.Hosts {
		if h.Name == "" || h.Host == "" {
			return nil, ErrInvalidStructure
		}
	}
	return &VerifyResult{
		Version:   p.Version,
		CreatedAt: p.CreatedAt,
		Counts: VerifyCounts{
			Hosts:               len(p.Hosts),
			SSHKeys:             len(p.SSHKeys),
			DesiredRules:        len(p.DesiredRules),
			VaultSecrets:        len(p.VaultSecrets),
			AnsiblePlaybooks:    len(p.AnsiblePlaybooks),
			TerraformWorkspaces: len(p.TerraformWorkspaces),
			VPNNetworks:         len(p.VPNNetworks),
		},
	}, nil
}

// PullSaved fetches the encrypted snapshot named by the saved GitHub Sync settings. It is read-only.
func (m *Manager) PullSaved(ctx context.Context) ([]byte, error) {
	repo, _ := m.db.GetSetting("github_repo")
	token, _ := m.db.GetSetting("github_token")
	branch, _ := m.db.GetSetting("github_branch")
	path, _ := m.db.GetSetting("github_file_path")
	if repo == "" || token == "" {
		return nil, errors.New("github repository and token must be configured")
	}
	if path == "" {
		path = "pantau-state.enc"
	}
	data, _, err := m.gh.Pull(ctx, repo, token, branch, path)
	return data, err
}
