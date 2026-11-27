package store

// SSH Key Library: the stored private keys and the per-host key resolution.

import (
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHKey struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"public_key"`  // authorized_keys line derived from private key
	Fingerprint string    `json:"fingerprint"` // "SHA256:..."
	CreatedAt   time.Time `json:"created_at"`
	UsedBy      int       `json:"used_by"` // number of hosts with key_id = this id (computed on list)
	PrivateKey  string    `json:"-"`       // NEVER serialized to any API
}

// ParseSSHPrivateKey validates a private key PEM, rejecting passphrase-protected keys,
// and extracts the authorized_keys public key string and SHA256 fingerprint.
func ParseSSHPrivateKey(pemBytes []byte) (pubKeyStr string, fingerprint string, err error) {
	var signers []ssh.Signer
	rest := pemBytes
	var passphraseErr error

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		signer, err := ssh.ParsePrivateKey(pem.EncodeToMemory(block))
		if err != nil {
			var pme *ssh.PassphraseMissingError
			if errors.As(err, &pme) || strings.Contains(strings.ToLower(err.Error()), "passphrase") || strings.Contains(strings.ToLower(err.Error()), "encrypted") {
				passphraseErr = err
			}
			continue
		}
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		if passphraseErr != nil {
			return "", "", fmt.Errorf("passphrase-protected keys are not supported: %w", passphraseErr)
		}
		s, err := ssh.ParsePrivateKey(pemBytes)
		if err != nil {
			var pme *ssh.PassphraseMissingError
			if errors.As(err, &pme) || strings.Contains(strings.ToLower(err.Error()), "passphrase") || strings.Contains(strings.ToLower(err.Error()), "encrypted") {
				return "", "", fmt.Errorf("passphrase-protected keys are not supported: %w", err)
			}
			return "", "", fmt.Errorf("invalid private key: %w", err)
		}
		signers = append(signers, s)
	}

	pubKey := signers[0].PublicKey()
	pubKeyBytes := ssh.MarshalAuthorizedKey(pubKey)
	pubKeyStr = strings.TrimSpace(string(pubKeyBytes))
	fingerprint = ssh.FingerprintSHA256(pubKey)
	return pubKeyStr, fingerprint, nil
}

// SSH Key Library DB Operations

func (d *DB) CreateSSHKey(k *SSHKey) (int64, error) {
	res, err := d.Exec(`INSERT INTO ssh_keys (name, private_key, public_key, fingerprint) VALUES (?, ?, ?, ?)`,
		k.Name, k.PrivateKey, k.PublicKey, k.Fingerprint)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListSSHKeys() ([]SSHKey, error) {
	rows, err := d.Query(`SELECT k.id, k.name, k.public_key, k.fingerprint, k.created_at, COUNT(h.id) AS used_by
		FROM ssh_keys k
		LEFT JOIN hosts h ON h.key_id = k.id
		GROUP BY k.id
		ORDER BY k.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]SSHKey, 0)
	for rows.Next() {
		var k SSHKey
		if err := rows.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedAt, &k.UsedBy); err != nil {
			return nil, err
		}
		list = append(list, k)
	}
	return list, rows.Err()
}

func (d *DB) GetSSHKey(id int64) (*SSHKey, error) {
	var k SSHKey
	err := d.QueryRow(`SELECT k.id, k.name, k.private_key, k.public_key, k.fingerprint, k.created_at, COUNT(h.id) AS used_by
		FROM ssh_keys k
		LEFT JOIN hosts h ON h.key_id = k.id
		WHERE k.id = ?
		GROUP BY k.id`, id).Scan(&k.ID, &k.Name, &k.PrivateKey, &k.PublicKey, &k.Fingerprint, &k.CreatedAt, &k.UsedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (d *DB) DeleteSSHKey(id int64) error {
	var usedBy int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts WHERE key_id = ?`, id).Scan(&usedBy)
	if err != nil {
		return err
	}
	if usedBy > 0 {
		return fmt.Errorf("%w by %d host(s); reassign them first", ErrSSHKeyInUse, usedBy)
	}
	res, err := d.Exec(`DELETE FROM ssh_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSSHKeyNotFound
	}
	return nil
}

func (d *DB) ResolveHostPrivateKey(h *Host) (string, error) {
	if h == nil {
		return "", errors.New("host is nil")
	}
	// Agent hosts have no key here: sshrunner signs through the agent and never falls back to the Global key.
	if h.SSHAgent {
		return "", nil
	}
	if h.KeyID > 0 {
		key, err := d.GetSSHKey(h.KeyID)
		if err != nil {
			return "", fmt.Errorf("lookup ssh key %d: %w", h.KeyID, err)
		}
		if key == nil {
			return "", fmt.Errorf("ssh key %d not found", h.KeyID)
		}
		return key.PrivateKey, nil
	}
	if strings.TrimSpace(h.CustomKey) != "" {
		return h.CustomKey, nil
	}
	globalKey, err := d.GetSetting("ssh_private_key")
	if err != nil {
		return "", fmt.Errorf("get global ssh key: %w", err)
	}
	return globalKey, nil
}
