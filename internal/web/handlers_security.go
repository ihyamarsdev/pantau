package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// handleHostSecurity handles GET /api/hosts/{id}/security and POST /api/hosts/{id}/security/scan
func (s *Server) handleHostSecurity(w http.ResponseWriter, r *http.Request, hostID int64, parts []string) {
	var cleaned []string
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}

	// Verify host existence (404 on unknown host)
	host, err := s.db.GetHost(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}

	if len(cleaned) == 0 {
		// GET /api/hosts/{id}/security
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		scan, err := s.db.GetSecurityScan(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if scan == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"scanned": false,
			})
			return
		}

		if !scan.ScannedAt.IsZero() {
			elapsed := time.Since(scan.ScannedAt)
			if elapsed < 60*time.Second {
				rem := int(math.Ceil((60*time.Second - elapsed).Seconds()))
				if rem > 0 {
					w.Header().Set("X-Scan-Cooldown-Remaining", strconv.Itoa(rem))
				}
			}
		}

		if scan.Findings == nil {
			scan.Findings = []store.SecurityFinding{}
		}
		if scan.Hardening == nil {
			scan.Hardening = []store.HardeningCheck{}
		}
		if scan.Notes == nil {
			scan.Notes = []string{}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"scanned": true,
			"scan":    scan,
		})
		return
	}

	if len(cleaned) == 1 && cleaned[0] == "scan" {
		// POST /api/hosts/{id}/security/scan
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Cooldown guard: 60s since last scanned_at
		lastScan, err := s.db.GetSecurityScan(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if lastScan != nil && !lastScan.ScannedAt.IsZero() {
			elapsed := time.Since(lastScan.ScannedAt)
			if elapsed < 60*time.Second {
				rem := int(math.Ceil((60*time.Second - elapsed).Seconds()))
				if rem <= 0 {
					rem = 1
				}
				writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"error":              fmt.Sprintf("Scan cooldown active. Please wait %d seconds.", rem),
					"cooldown_remaining": rem,
				})
				return
			}
		}

		if s.inspector == nil {
			http.Error(w, "inspector not available", http.StatusInternalServerError)
			return
		}

		if err := s.inspector.ScanSecurity(hostID); err != nil {
			if errors.Is(err, inspector.ErrSecurityScanInProgress) {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error": "Security scan already in progress for this host",
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		updatedScan, err := s.db.GetSecurityScan(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if updatedScan == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"scanned": false,
			})
			return
		}

		if updatedScan.Findings == nil {
			updatedScan.Findings = []store.SecurityFinding{}
		}
		if updatedScan.Hardening == nil {
			updatedScan.Hardening = []store.HardeningCheck{}
		}
		if updatedScan.Notes == nil {
			updatedScan.Notes = []string{}
		}

		w.Header().Set("X-Scan-Cooldown-Remaining", "60")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"scanned": true,
			"scan":    updatedScan,
		})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}
