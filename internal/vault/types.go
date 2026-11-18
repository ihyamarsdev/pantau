// Package vault provides the core primitives for the Pantau secrets vault:
// typed metadata, an AES-256-GCM encryption envelope, a template engine,
// a thread-safe log redactor, and an ephemeral environment injector.
//
// This package deliberately imports only the standard library so it can be
// used by every other internal package without creating import cycles.
package vault

import "time"

// Scope identifies the level at which a secret is visible.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeGroup   Scope = "group"
	ScopeHost    Scope = "host"
	ScopeCluster Scope = "cluster"
)

// ValidScope reports whether s is one of the recognized scope values.
func ValidScope(s string) bool {
	switch Scope(s) {
	case ScopeGlobal, ScopeGroup, ScopeHost, ScopeCluster:
		return true
	default:
		return false
	}
}

// SecretMeta is the non-sensitive metadata of a stored secret. It is safe to
// serialize to API responses.
type SecretMeta struct {
	ID            int64      `json:"id"`
	Key           string     `json:"key"`
	Scope         Scope      `json:"scope"`
	ScopeRef      string     `json:"scope_ref"`
	Version       int        `json:"version"`
	ExpiresAt     *time.Time `json:"expires_at"`
	RotationDays  int        `json:"rotation_days"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	NeedsRotation bool       `json:"needs_rotation"`
}

// Secret is a SecretMeta together with its decrypted Value. Value is never
// serialized; the explicit reveal endpoint returns it through a separate
// small struct.
type Secret struct {
	SecretMeta
	Value string `json:"-"`
}

// Version is a snapshot descriptor for a previous version of a secret.
type Version struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// Template is a named vault template body.
type Template struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SyncTarget binds a template to a host or Kubernetes cluster destination.
type SyncTarget struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"` // "host" | "kube"
	TemplateID   int64      `json:"template_id"`
	TargetRef    string     `json:"target_ref"` // host id as string, or negative cluster id as string
	Path         string     `json:"path"`
	Namespace    string     `json:"namespace"`
	PostSyncHook string     `json:"post_sync_hook"` // "systemd:<unit>:<restart|reload>" | "docker:<name>:restart" | ""
	LastSyncSHA  string     `json:"last_sync_sha"`
	LastSyncAt   *time.Time `json:"last_sync_at"`
	DriftStatus  string     `json:"drift_status"` // "unknown"|"in-sync"|"drift"|"error"
	CreatedAt    time.Time  `json:"created_at"`
}

// AuditEntry is a single vault audit log record.
type AuditEntry struct {
	ID     int64     `json:"id"`
	TS     time.Time `json:"ts"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// TemplateIssue describes one validation problem found in a template.
type TemplateIssue struct {
	Line    int    `json:"line"`
	Key     string `json:"key"`
	Message string `json:"message"`
}
