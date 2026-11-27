package store

import (
	"database/sql"
	"fmt"
)

// TeleportSyncCluster is one cluster tsh listed for a profile, with the badge suggested from its name and labels.
type TeleportSyncCluster struct {
	Name     string
	EnvBadge string
}

// TeleportSyncResult reports what SyncTeleportClusters did. Skipped holds the names that clash with a cluster of
// another source, which keeps its name.
type TeleportSyncResult struct {
	Added   []string
	Kept    []string
	Removed []string
	Skipped []string
}

// SyncTeleportClusters makes the Teleport clusters of one profile match what tsh listed. A cluster that is already
// stored keeps its favorite flag, env badge, policy and context name. A new cluster gets the profile's default policy
// and the suggested badge. A stored cluster that tsh no longer lists is removed. A name taken by another cluster is
// skipped, never overwritten.
func (d *DB) SyncTeleportClusters(profileID int64, defaultPolicy string, clusters []TeleportSyncCluster) (TeleportSyncResult, error) {
	var res TeleportSyncResult
	mode := normalizeTeleportPolicy(defaultPolicy)

	tx, err := d.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	stored := map[string]int64{}
	rows, err := tx.Query(`SELECT id, teleport_kube_name FROM kube_clusters WHERE source_type = ? AND teleport_profile_id = ?`,
		KubeSourceTeleport, profileID)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var id int64
		var kubeName string
		if err := rows.Scan(&id, &kubeName); err != nil {
			rows.Close()
			return res, err
		}
		stored[kubeName] = id
	}
	if err := rows.Close(); err != nil {
		return res, err
	}

	listed := make(map[string]bool, len(clusters))
	for _, c := range clusters {
		if listed[c.Name] {
			continue
		}
		listed[c.Name] = true
		if _, ok := stored[c.Name]; ok {
			res.Kept = append(res.Kept, c.Name)
			continue
		}
		var taken int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM kube_clusters WHERE name = ? COLLATE NOCASE`, c.Name).Scan(&taken); err != nil {
			return res, err
		}
		if taken > 0 {
			res.Skipped = append(res.Skipped, c.Name)
			continue
		}
		if _, err := tx.Exec(`INSERT INTO kube_clusters (name, kube_mode, source_type, teleport_profile_id, teleport_kube_name, env_badge)
			VALUES (?, ?, ?, ?, ?, ?)`, c.Name, mode, KubeSourceTeleport, profileID, c.Name, c.EnvBadge); err != nil {
			return res, fmt.Errorf("add teleport cluster %s: %w", c.Name, err)
		}
		res.Added = append(res.Added, c.Name)
	}

	for name, id := range stored {
		if listed[name] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM kube_clusters WHERE id = ?`, id); err != nil {
			return res, fmt.Errorf("remove teleport cluster %s: %w", name, err)
		}
		res.Removed = append(res.Removed, name)
	}

	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// UpdateKubeClusterMeta sets the favorite flag, env badge and policy of any cluster. Callers pass the full values.
func (d *DB) UpdateKubeClusterMeta(id int64, isFavorite bool, envBadge, kubeMode string) error {
	_, err := d.Exec(`UPDATE kube_clusters SET is_favorite = ?, env_badge = ?, kube_mode = ? WHERE id = ?`,
		boolInt(isFavorite), envBadge, normalizeKubeMode(kubeMode), id)
	return err
}

// TouchKubeClusterUsed records that the user switched to this cluster now.
func (d *DB) TouchKubeClusterUsed(id int64) error {
	_, err := d.Exec(`UPDATE kube_clusters SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// ClearTeleportContexts forgets the context names of a profile's clusters, after its isolated kubeconfig is removed.
func (d *DB) ClearTeleportContexts(profileID int64) error {
	_, err := d.Exec(`UPDATE kube_clusters SET kube_context_name = '' WHERE source_type = ? AND teleport_profile_id = ?`,
		KubeSourceTeleport, profileID)
	return err
}

// TeleportClusterIDs returns the stored cluster ids of one profile by Teleport name. It is used to map sync results
// back to rows.
func (d *DB) TeleportClusterIDs(profileID int64) (map[string]int64, error) {
	rows, err := d.Query(`SELECT id, teleport_kube_name FROM kube_clusters WHERE source_type = ? AND teleport_profile_id = ?`,
		KubeSourceTeleport, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var name sql.NullString
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[name.String] = id
	}
	return out, rows.Err()
}
