package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type KubeCluster struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Context       string    `json:"context"`
	Server        string    `json:"server"`
	Provider      string    `json:"provider"` // "eks" | "gke" | "aks" | "generic"
	KubeMode      string    `json:"kube_mode"` // full | no-exec | read-only
	KubeClusterID string    `json:"kube_cluster_id,omitempty"` // kube-system namespace UID, filled by detect
	Kubeconfig    string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

var ErrKubeClusterNameTaken = errors.New("a cluster with this name already exists")

func normalizeKubeMode(mode string) string {
	switch mode {
	case "no-exec", "read-only":
		return mode
	default:
		return "full"
	}
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed")
}

// CreateKubeCluster creates a new cluster record.
// Returns ErrKubeClusterNameTaken if a cluster with this name (case-insensitive) already exists.
func (d *DB) CreateKubeCluster(c *KubeCluster) (int64, error) {
	c.KubeMode = normalizeKubeMode(c.KubeMode)
	res, err := d.Exec(`INSERT INTO kube_clusters (name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Context, c.Server, c.Provider, c.KubeMode, c.KubeClusterID, c.Kubeconfig)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrKubeClusterNameTaken
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetKubeCluster(id int64) (*KubeCluster, error) {
	var c KubeCluster
	err := d.QueryRow(`SELECT id, name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig, created_at FROM kube_clusters WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Context, &c.Server, &c.Provider, &c.KubeMode, &c.KubeClusterID, &c.Kubeconfig, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) ListKubeClusters() ([]KubeCluster, error) {
	rows, err := d.Query(`SELECT id, name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig, created_at FROM kube_clusters ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]KubeCluster, 0)
	for rows.Next() {
		var c KubeCluster
		if err := rows.Scan(&c.ID, &c.Name, &c.Context, &c.Server, &c.Provider, &c.KubeMode, &c.KubeClusterID, &c.Kubeconfig, &c.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// UpdateKubeCluster updates an existing cluster record.
// Always updates name and kube_mode. ALSO updates context, server, provider, and kubeconfig when c.Kubeconfig != "".
func (d *DB) UpdateKubeCluster(c *KubeCluster) error {
	c.KubeMode = normalizeKubeMode(c.KubeMode)
	var err error
	if c.Kubeconfig != "" {
		_, err = d.Exec(`UPDATE kube_clusters SET name = ?, kube_mode = ?, context = ?, server = ?, provider = ?, kubeconfig = ? WHERE id = ?`,
			c.Name, c.KubeMode, c.Context, c.Server, c.Provider, c.Kubeconfig, c.ID)
	} else {
		_, err = d.Exec(`UPDATE kube_clusters SET name = ?, kube_mode = ? WHERE id = ?`,
			c.Name, c.KubeMode, c.ID)
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrKubeClusterNameTaken
		}
		return err
	}
	return nil
}

func (d *DB) DeleteKubeCluster(id int64) error {
	_, err := d.Exec(`DELETE FROM kube_clusters WHERE id = ?`, id)
	return err
}

func (d *DB) SetKubeClusterUID(id int64, uid string) error {
	_, err := d.Exec(`UPDATE kube_clusters SET kube_cluster_id = ? WHERE id = ?`, uid, id)
	return err
}
