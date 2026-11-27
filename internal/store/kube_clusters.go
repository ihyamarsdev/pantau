package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Source types of a kube_clusters row. Rows created before the Teleport integration are "kubeconfig".
const (
	KubeSourceKubeconfig = "kubeconfig"
	KubeSourceTeleport   = "teleport"
)

type KubeCluster struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Context       string    `json:"context"`
	Server        string    `json:"server"`
	Provider      string    `json:"provider"`                  // "eks" | "gke" | "aks" | "generic"
	KubeMode      string    `json:"kube_mode"`                 // full | no-exec | read-only
	KubeClusterID string    `json:"kube_cluster_id,omitempty"` // kube-system namespace UID, filled by detect
	Kubeconfig    string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`

	SourceType        string     `json:"source_type"`                   // kubeconfig | teleport
	TeleportProfileID int64      `json:"teleport_profile_id,omitempty"` // set when SourceType is teleport
	TeleportKubeName  string     `json:"teleport_kube_name,omitempty"`  // name of the cluster as tsh lists it
	KubeContextName   string     `json:"kube_context_name,omitempty"`   // context written to the isolated kubeconfig
	EnvBadge          string     `json:"env_badge"`                     // prod | staging | dev | ""
	IsFavorite        bool       `json:"is_favorite"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
}

var ErrKubeClusterNameTaken = errors.New("a cluster with this name already exists")

// kubeClusterColumns is the column list every read of kube_clusters uses, in scanKubeCluster order.
const kubeClusterColumns = `id, name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig, created_at,
	source_type, teleport_profile_id, teleport_kube_name, kube_context_name, env_badge, is_favorite, last_used_at`

func scanKubeCluster(s rowScanner) (KubeCluster, error) {
	var c KubeCluster
	var clusterUID, sourceType, envBadge sql.NullString
	var lastUsed sql.NullTime
	var isFavorite int
	err := s.Scan(&c.ID, &c.Name, &c.Context, &c.Server, &c.Provider, &c.KubeMode, &clusterUID, &c.Kubeconfig, &c.CreatedAt,
		&sourceType, &c.TeleportProfileID, &c.TeleportKubeName, &c.KubeContextName, &envBadge, &isFavorite, &lastUsed)
	if err != nil {
		return c, err
	}
	c.KubeClusterID = clusterUID.String
	c.SourceType = sourceType.String
	if c.SourceType == "" {
		c.SourceType = KubeSourceKubeconfig
	}
	c.EnvBadge = envBadge.String
	c.IsFavorite = isFavorite != 0
	if lastUsed.Valid {
		t := lastUsed.Time
		c.LastUsedAt = &t
	}
	return c, nil
}

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
	if c.SourceType == "" {
		c.SourceType = KubeSourceKubeconfig
	}
	res, err := d.Exec(`INSERT INTO kube_clusters (name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig,
		source_type, teleport_profile_id, teleport_kube_name, kube_context_name, env_badge, is_favorite) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Context, c.Server, c.Provider, c.KubeMode, c.KubeClusterID, c.Kubeconfig,
		c.SourceType, c.TeleportProfileID, c.TeleportKubeName, c.KubeContextName, c.EnvBadge, boolInt(c.IsFavorite))
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrKubeClusterNameTaken
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetKubeCluster(id int64) (*KubeCluster, error) {
	c, err := scanKubeCluster(d.QueryRow(`SELECT `+kubeClusterColumns+` FROM kube_clusters WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) ListKubeClusters() ([]KubeCluster, error) {
	rows, err := d.Query(`SELECT ` + kubeClusterColumns + ` FROM kube_clusters ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]KubeCluster, 0)
	for rows.Next() {
		c, err := scanKubeCluster(rows)
		if err != nil {
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

// SetKubeClusterContextName records the context that tsh wrote for a Teleport cluster.
func (d *DB) SetKubeClusterContextName(id int64, contextName string) error {
	_, err := d.Exec(`UPDATE kube_clusters SET kube_context_name = ? WHERE id = ?`, contextName, id)
	return err
}
