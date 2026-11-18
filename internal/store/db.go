package store

// Core database handle: connection setup, the default bootstrap, and the
// package-level error set. Domain queries live in the files next to this one.

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"pantau/internal/logger"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	// ponytail: GetSetting is hit on every inspection/tick; single-process app, so every settings write path clears this.
	settingsCache sync.Map
}

var (
	ErrSSHKeyNotFound    = errors.New("ssh key not found")
	ErrSSHKeyInUse       = errors.New("ssh key is in use")
	ErrHostNotFound      = errors.New("host not found")
	ErrHostGroupNotFound = errors.New("host group not found")
)

func Open(dbPath string) (*DB, error) {
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		// ponytail: DB holds plaintext private SSH keys, so a missing dir is created 0700.
		// Existing dirs are left alone (the default DB path can be the cwd).
		_ = os.MkdirAll(dir, 0700)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &DB{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := s.initDefaults(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init defaults: %w", err)
	}

	secureDBFiles(dbPath)

	return s, nil
}

// secureDBFiles restricts the SQLite database and its WAL/SHM sidecars to the
// owner. SQLite creates new sidecars with the DB file's mode, so they inherit 0600.
func secureDBFiles(dbPath string) {
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Chmod(p, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("store", "", fmt.Sprintf("restrict permissions on %s: %v", p, err))
		}
	}
}

func (d *DB) initDefaults() error {
	defer d.settingsCache.Clear()
	// 1. Ensure default admin password exists (default: "admin")
	var hash string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'admin_password_hash'").Scan(&hash)
	if err == sql.ErrNoRows {
		defaultHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := d.Exec("INSERT INTO settings(key, value) VALUES ('admin_password_hash', ?)", string(defaultHash)); err != nil {
			return err
		}
	}

	// 2. Ensure global universal RSA 4096-bit SSH keypair exists
	// ponytail: RSA 4096 is universally accepted by legacy OpenSSH 5.3p1 (CentOS 6) through modern OpenSSH 9+.
	var privKey string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_private_key'").Scan(&privKey)
	if err == sql.ErrNoRows {
		privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
		if err != nil {
			return fmt.Errorf("generate default universal rsa key: %w", err)
		}

		tx, err := d.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?)", privPEM); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?)", pubAuthorized); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else if err == nil {
		var pubKey string
		_ = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_public_key'").Scan(&pubKey)
		// If existing database was initialized with Ed25519 only, upgrade to universal RSA-4096 as primary
		// while retaining the Ed25519 private key in the PEM chain so existing modern hosts don't break.
		if strings.HasPrefix(strings.TrimSpace(pubKey), "ssh-ed25519") {
			privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
			if err == nil {
				combinedPriv := strings.TrimSpace(privPEM) + "\n\n" + strings.TrimSpace(privKey) + "\n"
				tx, err := d.Begin()
				if err == nil {
					_, _ = tx.Exec("UPDATE settings SET value = ? WHERE key = 'ssh_private_key'", combinedPriv)
					_, _ = tx.Exec("UPDATE settings SET value = ? WHERE key = 'ssh_public_key'", pubAuthorized)
					_, _ = tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key_ed25519', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", privKey)
					_, _ = tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key_ed25519', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", pubKey)
					_ = tx.Commit()
				}
			}
		}
	}

	// 3. Ensure essential default global terminal presets exist
	var presetCount int
	_ = d.QueryRow("SELECT COUNT(*) FROM terminal_presets").Scan(&presetCount)
	if presetCount == 0 {
		defaultPresets := []struct {
			name    string
			command string
			sort    int
		}{
			{"htop", "htop", 1},
			{"docker stats", "docker stats", 2},
			{"journalctl -f", "journalctl -n 100 -f", 3},
		}
		for _, p := range defaultPresets {
			_, _ = d.Exec("INSERT INTO terminal_presets(name, command, sort_order, use_tmux) VALUES (?, ?, ?, 0)", p.name, p.command, p.sort)
		}
	}

	return d.migrateLegacyS3Settings()
}

type rowScanner interface {
	Scan(dest ...any) error
}
