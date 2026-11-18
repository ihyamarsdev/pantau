package store

// Settings: the key/value application settings and the global SSH keypair.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// GenerateRSAKeyPair creates a new PEM-encoded RSA private key and OpenSSH-formatted authorized public key.
func GenerateRSAKeyPair(bits int) (privPEM, pubAuthorized string, err error) {
	if bits <= 0 {
		bits = 4096
	}
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("generate rsa key: %w", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("new ssh pubkey: %w", err)
	}
	pubAuth := string(ssh.MarshalAuthorizedKey(sshPub))
	return string(pemBlock), pubAuth, nil
}

// RegenerateGlobalSSHKey generates a new universal RSA-4096 keypair and saves it to settings.
func (d *DB) RegenerateGlobalSSHKey() (string, error) {
	defer d.settingsCache.Clear()
	privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
	if err != nil {
		return "", err
	}
	tx, err := d.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", privPEM); err != nil {
		return "", err
	}
	if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", pubAuthorized); err != nil {
		return "", err
	}
	return pubAuthorized, tx.Commit()
}

// Settings helpers
func (d *DB) GetSetting(key string) (string, error) {
	if v, ok := d.settingsCache.Load(key); ok {
		return v.(string), nil
	}
	var val string
	err := d.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err == nil {
		d.settingsCache.Store(key, val)
	}
	return val, err
}

func (d *DB) SetSetting(key, val string) error {
	defer d.settingsCache.Delete(key)
	_, err := d.Exec("INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, val)
	return err
}

func (d *DB) DeleteSetting(key string) error {
	defer d.settingsCache.Delete(key)
	_, err := d.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

func (d *DB) GetGlobalNotes() (string, error) {
	return d.GetSetting("global_notes")
}

func (d *DB) SetGlobalNotes(notes string) error {
	return d.SetSetting("global_notes", notes)
}
