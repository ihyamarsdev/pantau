package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// PackageUpdate is one pending package of a Package Update Check.
type PackageUpdate struct {
	Name     string `json:"name"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Security bool   `json:"security"`
}

// PackageUpdateCheck is the latest Package Update Check of one host: which packages the package manager
// offers a newer version for. It holds check results only, never configuration.
type PackageUpdateCheck struct {
	HostID            int64           `json:"host_id"`
	CheckedAt         time.Time       `json:"checked_at"`
	Supported         bool            `json:"supported"`
	Manager           string          `json:"manager,omitempty"`
	PendingTotal      int             `json:"pending_total"`
	PendingSecurity   int             `json:"pending_security"`
	RebootRequired    bool            `json:"reboot_required"`
	RepoCacheAgeHours int             `json:"repo_cache_age_hours"` // -1 = unknown
	Packages          []PackageUpdate `json:"packages"`
	Note              string          `json:"note,omitempty"`
	Error             string          `json:"error,omitempty"`
}

// ErrPackageUpdateCheckNotFound is returned when a host has no update check yet.
var ErrPackageUpdateCheckNotFound = errors.New("package update check not found")

// SavePackageUpdateCheck upserts the latest update check of a host.
func (d *DB) SavePackageUpdateCheck(c *PackageUpdateCheck) error {
	if c == nil {
		return fmt.Errorf("package update check cannot be nil")
	}
	pkgs := c.Packages
	if pkgs == nil {
		pkgs = []PackageUpdate{}
	}
	pkgsJSON, err := json.Marshal(pkgs)
	if err != nil {
		return fmt.Errorf("marshal package updates: %w", err)
	}
	at := c.CheckedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err = d.Exec(`INSERT INTO package_update_checks
		(host_id, checked_at, supported, manager, pending_total, pending_security, reboot_required, repo_cache_age_hours, packages_json, note, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id) DO UPDATE SET
			checked_at = excluded.checked_at,
			supported = excluded.supported,
			manager = excluded.manager,
			pending_total = excluded.pending_total,
			pending_security = excluded.pending_security,
			reboot_required = excluded.reboot_required,
			repo_cache_age_hours = excluded.repo_cache_age_hours,
			packages_json = excluded.packages_json,
			note = excluded.note,
			error = excluded.error`,
		c.HostID, at, c.Supported, c.Manager, c.PendingTotal, c.PendingSecurity,
		c.RebootRequired, c.RepoCacheAgeHours, string(pkgsJSON), c.Note, c.Error)
	return err
}

// GetPackageUpdateCheck returns the latest update check of a host, or (nil, nil) when there is none.
func (d *DB) GetPackageUpdateCheck(hostID int64) (*PackageUpdateCheck, error) {
	var c PackageUpdateCheck
	var pkgsJSON string
	err := d.QueryRow(`SELECT host_id, checked_at, supported, manager, pending_total, pending_security,
		reboot_required, repo_cache_age_hours, packages_json, note, error
		FROM package_update_checks WHERE host_id = ?`, hostID).
		Scan(&c.HostID, &c.CheckedAt, &c.Supported, &c.Manager, &c.PendingTotal, &c.PendingSecurity,
			&c.RebootRequired, &c.RepoCacheAgeHours, &pkgsJSON, &c.Note, &c.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Packages = []PackageUpdate{}
	if pkgsJSON != "" {
		if err := json.Unmarshal([]byte(pkgsJSON), &c.Packages); err != nil {
			return nil, fmt.Errorf("unmarshal package updates: %w", err)
		}
		if c.Packages == nil {
			c.Packages = []PackageUpdate{}
		}
	}
	return &c, nil
}
