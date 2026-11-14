package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
	"pantau/internal/vulnintel"
)

// vulnIntelSetting opts in to CISA KEV / EPSS lookups ("1"); off by default (they leave the Pantau server).
const vulnIntelSetting = "vuln_intel_enabled"

type securityIntel struct {
	Enabled bool `json:"enabled"`
	*vulnintel.Result
}

// scanIntel returns the exploit intelligence for the CVEs of a scan, or {enabled:false} when not opted in.
func (s *Server) scanIntel(ctx context.Context, scan *store.SecurityScan) securityIntel {
	if v, _ := s.db.GetSetting(vulnIntelSetting); v != "1" {
		return securityIntel{}
	}
	var cves []string
	for _, f := range scan.Findings {
		cves = append(cves, f.CVEs...)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res := s.vuln.Lookup(ctx, cves)
	return securityIntel{Enabled: true, Result: &res}
}

// handleVulnDatabases serves GET /api/security/vuln-databases: the embedded list of vulnerability databases with
// its attribution and the entries Pantau reads.
func (s *Server) handleVulnDatabases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": vulnintel.Source, "used_by_pantau": vulnintel.UsedByPantau, "databases": vulnintel.Databases(),
	})
}

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
			"intel":   s.scanIntel(r.Context(), scan),
		})
		return
	}

	// GET /api/hosts/{id}/security/accounts and POST .../security/accounts/scan
	if len(cleaned) == 1 && cleaned[0] == "accounts" {
		s.handleHostAccountAudit(w, r, hostID)
		return
	}

	// GET /api/hosts/{id}/security/vulns -> known vulnerabilities of the installed packages
	if len(cleaned) == 1 && cleaned[0] == "vulns" {
		s.handleHostSecurityVulns(w, r, hostID)
		return
	}

	// GET /api/hosts/{id}/security/firewall -> live ufw/firewalld/iptables state
	if len(cleaned) == 1 && cleaned[0] == "firewall" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		state, err := s.inspector.FirewallState(hostID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "firewall": state})
		return
	}

	// POST /api/hosts/{id}/security/firewall/action {backend, action, rule?, old?}
	if len(cleaned) == 2 && cleaned[0] == "firewall" && cleaned[1] == "action" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req inspector.FirewallRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		res, err := s.inspector.FirewallApply(hostID, req)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, inspector.ErrFirewallProtected):
				status = http.StatusForbidden
			case errors.Is(err, inspector.ErrFirewallInvalid):
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": res.Succeeded(), "result": res})
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
			"intel":   s.scanIntel(r.Context(), updatedScan),
		})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}
