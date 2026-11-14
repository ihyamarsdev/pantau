package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"pantau/internal/inspector"
)

// accountAuditCooldown is the minimum delay between two account audits of the same host.
const accountAuditCooldown = 60 * time.Second

// handleHostAccountAudit handles GET /api/hosts/{id}/security/accounts and
// POST /api/hosts/{id}/security/accounts/scan. Dispatched from handleHostSecurity.
func (s *Server) handleHostAccountAudit(w http.ResponseWriter, r *http.Request, hostID int64) {
	switch r.Method {
	case http.MethodGet:
		audit, err := s.db.GetAccountAudit(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if audit == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"audited": false})
			return
		}
		if !audit.ScannedAt.IsZero() {
			elapsed := time.Since(audit.ScannedAt)
			if elapsed < accountAuditCooldown {
				rem := int(math.Ceil((accountAuditCooldown - elapsed).Seconds()))
				if rem > 0 {
					w.Header().Set("X-Scan-Cooldown-Remaining", strconv.Itoa(rem))
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"audited": true, "audit": audit})
	case http.MethodPost:
		last, err := s.db.GetAccountAudit(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if last != nil && !last.ScannedAt.IsZero() {
			elapsed := time.Since(last.ScannedAt)
			if elapsed < accountAuditCooldown {
				rem := int(math.Ceil((accountAuditCooldown - elapsed).Seconds()))
				if rem <= 0 {
					rem = 1
				}
				writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"error":              fmt.Sprintf("Account audit cooldown active. Please wait %d seconds.", rem),
					"cooldown_remaining": rem,
				})
				return
			}
		}
		if s.inspector == nil {
			http.Error(w, "inspector not available", http.StatusInternalServerError)
			return
		}
		if err := s.inspector.ScanAccountAudit(hostID); err != nil {
			if errors.Is(err, inspector.ErrAccountAuditInProgress) {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error": "Account audit already in progress for this host",
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		audit, err := s.db.GetAccountAudit(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if audit == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"audited": false})
			return
		}
		w.Header().Set("X-Scan-Cooldown-Remaining", "60")
		writeJSON(w, http.StatusOK, map[string]interface{}{"audited": true, "audit": audit})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// FleetKeyHost is one host/user pair that authorizes a key.
type FleetKeyHost struct {
	HostID  int64    `json:"host_id"`
	Name    string   `json:"name"`
	User    string   `json:"user"`
	Options []string `json:"options"`
}

// FleetKey is one fingerprint across the fleet.
type FleetKey struct {
	Fingerprint string         `json:"fingerprint"`
	Type        string         `json:"type"`
	Bits        int            `json:"bits"`
	LibraryKey  string         `json:"library_key,omitempty"`
	Unknown     bool           `json:"unknown"`
	Weak        bool           `json:"weak"`
	Shared      bool           `json:"shared"` // authorized on more than one host or account
	Hosts       []FleetKeyHost `json:"hosts"`
}

// handleFleetKeys handles GET /api/security/keys: the authorized_keys inventory of every
// audited host, grouped by fingerprint. Only public key metadata is returned.
func (s *Server) handleFleetKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	audits, err := s.db.ListAccountAudits()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hosts, err := s.db.ListHosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hostName := map[int64]string{}
	for _, h := range hosts {
		hostName[h.ID] = h.Name
	}

	keys := map[string]*FleetKey{}
	auditedHosts := map[int64]bool{}
	for _, a := range audits {
		auditedHosts[a.HostID] = true
		for _, k := range a.Keys {
			if !k.Valid {
				continue
			}
			fk, ok := keys[k.Fingerprint]
			if !ok {
				fk = &FleetKey{
					Fingerprint: k.Fingerprint,
					Type:        k.Type,
					Bits:        k.Bits,
					LibraryKey:  k.LibraryKey,
					Unknown:     k.Unknown,
					Weak:        k.Weak,
					Hosts:       []FleetKeyHost{},
				}
				keys[k.Fingerprint] = fk
			}
			if fk.LibraryKey == "" && k.LibraryKey != "" {
				fk.LibraryKey = k.LibraryKey
				fk.Unknown = false
			}
			fk.Hosts = append(fk.Hosts, FleetKeyHost{
				HostID:  a.HostID,
				Name:    hostName[a.HostID],
				User:    k.User,
				Options: k.Options,
			})
		}
	}

	// A key authorized on many hosts/users is a wide blast radius (info).
	shared := map[string]bool{}
	for fp, fk := range keys {
		if len(fk.Hosts) > 1 {
			shared[fp] = true
		}
	}
	for fp, fk := range keys {
		if shared[fp] {
			fk.Shared = true
		}
	}

	notAudited := 0
	for _, h := range hosts {
		if !auditedHosts[h.ID] {
			notAudited++
		}
	}

	list := make([]FleetKey, 0, len(keys))
	for _, fk := range keys {
		list = append(list, *fk)
	}
	sortFleetKeys(list)

	unknownOnly := r.URL.Query().Get("unknown") == "1"
	weakOnly := r.URL.Query().Get("weak") == "1"
	filtered := make([]FleetKey, 0, len(list))
	unknownCount, weakCount := 0, 0
	for _, fk := range list {
		if fk.Unknown {
			unknownCount++
		}
		if fk.Weak {
			weakCount++
		}
		if unknownOnly && !fk.Unknown {
			continue
		}
		if weakOnly && !fk.Weak {
			continue
		}
		filtered = append(filtered, fk)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys":              filtered,
		"total":             len(list),
		"unknown_count":     unknownCount,
		"weak_count":        weakCount,
		"hosts_not_audited": notAudited,
	})
}

// sortFleetKeys orders the fleet keys by severity relevance, then by fingerprint.
func sortFleetKeys(list []FleetKey) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && fleetKeyLess(list[j], list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func fleetKeyLess(a, b FleetKey) bool {
	ra, rb := fleetKeyRank(a), fleetKeyRank(b)
	if ra != rb {
		return ra < rb
	}
	if len(a.Hosts) != len(b.Hosts) {
		return len(a.Hosts) > len(b.Hosts)
	}
	return a.Fingerprint < b.Fingerprint
}

// fleetKeyRank sorts weak keys first, then unknown keys, then known keys.
func fleetKeyRank(k FleetKey) int {
	switch {
	case k.Weak:
		return 0
	case k.Unknown:
		return 1
	default:
		return 2
	}
}
