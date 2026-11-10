package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// Web domains: the reverse proxies a host serves (nginx, Caddy, Traefik).
//
//	GET  /api/domains?host_id=N   the stored scan of one host
//	POST /api/domains/scan        scan one host now and store the result
//	GET  /api/domains              the fleet view of every host
//
// The discovery is on demand: the scan runs over SSH only when the user asks
// for it, the result is stored per host so the fleet view and the fleet search
// answer without SSH.

func (s *Server) registerDomainRoutes() {
	s.mux.HandleFunc("/api/domains", s.authMiddleware(s.handleDomains))
	s.mux.HandleFunc("/api/domains/scan", s.authMiddleware(s.handleDomainsScan))
}

// handleDomains answers GET /api/domains (one host or the fleet view).
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostID := strings.TrimSpace(r.URL.Query().Get("host_id"))
	if hostID == "" {
		s.handleDomainsFleet(w)
		return
	}
	id, err := strconv.ParseInt(hostID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "host_id must be a number"})
		return
	}
	host, err := s.db.GetHost(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if host == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "host not found"})
		return
	}
	scan, _, err := s.db.GetWebScan(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if scan == nil {
		// Never scanned: an empty answer, not an error.
		writeJSON(w, http.StatusOK, domainResponse(id, "", []store.WebProxy{}, []store.WebSite{}, []string{}))
		return
	}
	sites, err := s.db.GetHostDomains(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, domainResponse(scan.HostID, scan.ScannedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), scan.Proxies, sites, scan.Warnings))
}

// handleDomainsFleet answers GET /api/domains without a host: every host with
// its domain coverage and every stored domain.
func (s *Server) handleDomainsFleet(w http.ResponseWriter) {
	hosts, err := s.db.ListHostDomainSummaries()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	sites, err := s.db.ListHostDomains()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	unscanned, err := s.db.CountUnscannedHosts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if hosts == nil {
		hosts = []store.HostDomainSummary{}
	}
	if sites == nil {
		sites = []store.HostDomainSite{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hosts":           hosts,
		"sites":           sites,
		"unscanned_hosts": unscanned,
	})
}

// handleDomainsScan answers POST /api/domains/scan: it scans one host now and
// stores the result.
func (s *Server) handleDomainsScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid body"})
		return
	}
	if body.HostID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "host_id is required"})
		return
	}
	host, err := s.db.GetHost(body.HostID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if host == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "host not found"})
		return
	}
	if s.inspector == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "inspector not available"})
		return
	}
	scan, err := s.inspector.ScanWebDomains(body.HostID)
	if err != nil {
		if errors.Is(err, inspector.ErrWebScanInProgress) {
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{"error": "domain scan already in progress for this host"})
			return
		}
		if errors.Is(err, inspector.ErrWebScanSSH) {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if err := s.db.ReplaceWebScan(body.HostID, scan); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, domainResponse(scan.HostID, scan.ScannedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), scan.Proxies, scan.Sites, scan.Warnings))
}

// domainResponse builds the JSON shape of one host scan.
func domainResponse(hostID int64, scannedAt string, proxies []store.WebProxy, sites []store.WebSite, warnings []string) map[string]interface{} {
	if proxies == nil {
		proxies = []store.WebProxy{}
	}
	if sites == nil {
		sites = []store.WebSite{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	return map[string]interface{}{
		"host_id":    hostID,
		"scanned_at": scannedAt,
		"proxies":    proxies,
		"sites":      sites,
		"warnings":   warnings,
	}
}
