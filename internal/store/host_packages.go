package store

// Host Package inventory: the packages last seen on a host.

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type HostPackage struct {
	ID            int64     `json:"id"`
	HostID        int64     `json:"host_id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	Source        string    `json:"source,omitempty"`         // source package name (dpkg), origin package (apk); empty for rpm/pacman
	SourceVersion string    `json:"source_version,omitempty"` // source package version (dpkg); empty for rpm/pacman
	SizeBytes     int64     `json:"size_bytes"`
	InstallEpoch  int64     `json:"install_epoch"`
	IsManual      bool      `json:"is_manual"`
	ServiceName   string    `json:"service_name,omitempty"`
	ServiceStatus string    `json:"service_status,omitempty"`
	LastScannedAt time.Time `json:"last_scanned_at"`
}

// Host Package operations
// ponytail: Atomic host-scoped replace eliminates orphan/uninstalled packages cleanly in one transaction.
// ponytail: Guard against accidental database wipe if scan returned empty or timed out.
func (d *DB) ReplaceHostPackages(hostID int64, pkgs []*HostPackage) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("refusing to replace with empty packages list; keeping existing scan results")
	}

	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM host_packages WHERE host_id = ?`, hostID); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT INTO host_packages (host_id, name, version, source, source_version, size_bytes, install_epoch, is_manual, service_name, service_status, last_scanned_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, p := range pkgs {
		isManualInt := 0
		if p.IsManual {
			isManualInt = 1
		}
		scannedAt := p.LastScannedAt
		if scannedAt.IsZero() {
			scannedAt = now
		}
		if _, err := stmt.Exec(hostID, p.Name, p.Version, p.Source, p.SourceVersion, p.SizeBytes, p.InstallEpoch, isManualInt, p.ServiceName, p.ServiceStatus, scannedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) GetHostPackages(hostID int64) ([]*HostPackage, error) {
	rows, err := d.Query(`SELECT id, host_id, name, version, source, source_version, size_bytes, install_epoch, is_manual, service_name, service_status, last_scanned_at FROM host_packages WHERE host_id = ? ORDER BY is_manual DESC, size_bytes DESC, name ASC`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkgs []*HostPackage
	for rows.Next() {
		var p HostPackage
		var isManualInt int
		if err := rows.Scan(&p.ID, &p.HostID, &p.Name, &p.Version, &p.Source, &p.SourceVersion, &p.SizeBytes, &p.InstallEpoch, &isManualInt, &p.ServiceName, &p.ServiceStatus, &p.LastScannedAt); err != nil {
			return nil, err
		}
		p.IsManual = isManualInt == 1
		pkgs = append(pkgs, &p)
	}
	return pkgs, nil
}

// GetHostPackagesLastScanned returns the most recent last_scanned_at timestamp for a host's packages.
func (d *DB) GetHostPackagesLastScanned(hostID int64) (time.Time, error) {
	var ts time.Time
	err := d.QueryRow(`SELECT last_scanned_at FROM host_packages WHERE host_id = ? ORDER BY last_scanned_at DESC LIMIT 1`, hostID).Scan(&ts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return ts, nil
}
