package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// Docker tab security: the container misconfiguration audit and the per-image vulnerability scans.
// Both are read-only on the host; the image scan needs a scanner (trivy or grype) already installed
// there, Pantau never installs one.

// dockerSecIntel attaches the KEV/EPSS exploit intelligence to a set of CVE ids, when the operator
// opted in (Settings -> General). Same shape as the Security tab (scanIntel).
func (s *Server) dockerSecIntel(ctx context.Context, cves []string) securityIntel {
	if v, _ := s.db.GetSetting(vulnIntelSetting); v != "1" {
		return securityIntel{}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res := s.vuln.Lookup(ctx, cves)
	return securityIntel{Enabled: true, Result: &res}
}

// dockerSecCVEs collects the CVE ids of a stored image scan.
func dockerSecCVEs(scan *store.DockerSecurityScan) []string {
	cves := []string{}
	seen := map[string]bool{}
	for _, v := range scan.Vulns {
		id := v.ID
		if !isCVECode(id) || seen[id] {
			continue
		}
		seen[id] = true
		cves = append(cves, id)
	}
	return cves
}

// dockerSecCVEre matches a CVE id the way the exploit intelligence does (CVE-YYYY-NNNN).
var dockerSecCVEre = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,}$`)

// isCVECode reports whether an id is a CVE id.
func isCVECode(id string) bool {
	return dockerSecCVEre.MatchString(id)
}

// handleDockerSecurity serves the Docker security endpoints below /api/hosts/{id}/docker/security.
//
//	GET  security            -> stored audit + image scan summaries
//	POST security/audit      -> run the container misconfiguration audit
//	POST security/scan-image -> scan one image with the scanner installed on the host
func (s *Server) handleDockerSecurity(w http.ResponseWriter, r *http.Request, hostID int64, parts []string) bool {
	if len(parts) < 3 || parts[2] != "security" {
		return false
	}
	sub := ""
	if len(parts) > 3 {
		sub = parts[3]
	}

	switch {
	case r.Method == http.MethodGet && sub == "":
		s.dockerSecuritySummary(w, r, hostID)
		return true

	case r.Method == http.MethodPost && sub == "audit":
		scan, err := s.inspector.DockerSecurityAudit(hostID)
		if err != nil {
			dockerSecurityError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "audit": scan})

	case r.Method == http.MethodPost && sub == "scan-image":
		var req struct {
			Image string `json:"image"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return true
		}
		scan, err := s.inspector.DockerImageScan(hostID, req.Image)
		if err != nil {
			dockerSecurityError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":    true,
			"scan":  scan,
			"intel": s.dockerSecIntel(r.Context(), dockerSecCVEs(scan)),
		})

	default:
		return false
	}
	return true
}

// dockerSecuritySummary returns the stored audit and image scan results of a host.
func (s *Server) dockerSecuritySummary(w http.ResponseWriter, r *http.Request, hostID int64) {
	scans, err := s.db.ListDockerSecurityScans(hostID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	audit := (*store.DockerSecurityScan)(nil)
	images := []store.DockerSecurityScan{}
	for i := range scans {
		if scans[i].Kind == "misconfig" {
			audit = &scans[i]
			continue
		}
		images = append(images, scans[i])
	}
	resp := map[string]interface{}{
		"ok":       true,
		"assessed": audit != nil,
		"audit":    audit,
		"images":   images,
	}
	// The exploit intelligence covers the CVE ids of every stored image scan.
	var cves []string
	for i := range images {
		cves = append(cves, dockerSecCVEs(&images[i])...)
	}
	if len(cves) > 0 {
		resp["intel"] = s.dockerSecIntel(r.Context(), cves)
	}
	writeJSON(w, http.StatusOK, resp)
}

// dockerSecurityError maps a Docker security error to an HTTP status.
func dockerSecurityError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, inspector.ErrDockerInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, inspector.ErrDockerSecInProgress):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]interface{}{"ok": false, "error": err.Error()})
}
