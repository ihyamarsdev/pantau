package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"pantau/internal/osv"
	"pantau/internal/store"
)

// osvEnabledSetting opts in to the known-vulnerability check of the installed packages ("1").
// Off by default: the check sends package names and versions to api.osv.dev / security.archlinux.org.
const osvEnabledSetting = "osv_enabled"

// hostVulnResponse is the payload of GET /api/hosts/{id}/security/vulns.
type hostVulnResponse struct {
	Enabled         bool           `json:"enabled"`
	Status          string         `json:"status,omitempty"` // assessed | not_assessed
	Source          string         `json:"source,omitempty"`
	Ecosystem       string         `json:"ecosystem,omitempty"`
	CheckedPackages int            `json:"checked_packages"`
	ScannedAt       time.Time      `json:"scanned_at"`
	Vulns           []osv.Finding  `json:"vulns"`
	UnfixedCount    int            `json:"unfixed_count"`
	Notes           []string       `json:"notes"`
	Error           string         `json:"error,omitempty"`
	Intel           *securityIntel `json:"intel,omitempty"`
}

// handleHostSecurityVulns serves GET /api/hosts/{id}/security/vulns: known vulnerabilities of the
// packages installed on the host, looked up in OSV (Debian, Ubuntu, Alpine) or the Arch Linux
// Security Tracker (pacman).
//
// Nothing leaves the Pantau server while the feature is off: the endpoint answers {enabled:false}.
func (s *Server) handleHostSecurityVulns(w http.ResponseWriter, r *http.Request, hostID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if v, _ := s.db.GetSetting(osvEnabledSetting); v != "1" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}

	host, err := s.db.GetHost(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}

	pkgs, err := s.db.GetHostPackages(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(pkgs) == 0 {
		writeJSON(w, http.StatusOK, hostVulnResponse{
			Enabled: true, Status: "not_assessed", Vulns: []osv.Finding{}, Notes: []string{},
			Error: "the package inventory is empty: scan the host packages first",
		})
		return
	}

	eco, ok, reason := osv.EcosystemFor(host.OSInfo)
	if !ok {
		writeJSON(w, http.StatusOK, hostVulnResponse{
			Enabled: true, Status: "not_assessed", Vulns: []osv.Finding{}, Notes: []string{reason},
		})
		return
	}

	includeUnfixed := r.URL.Query().Get("unfixed") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	res, err := s.osv.Check(ctx, eco, osvPackages(pkgs))
	if err != nil {
		// A feed failure is never an empty "clean" list.
		writeJSON(w, http.StatusOK, hostVulnResponse{
			Enabled: true, Status: "not_assessed", Source: eco.Source, Ecosystem: eco.Name,
			CheckedPackages: len(pkgs), ScannedAt: time.Now().UTC(), Vulns: []osv.Finding{},
			Notes: []string{}, Error: err.Error(),
		})
		return
	}

	fixed, unfixed := []osv.Finding{}, 0
	for _, f := range res.Findings {
		if f.HasFix() {
			fixed = append(fixed, f)
		} else {
			unfixed++
		}
	}
	vulns := fixed
	if includeUnfixed {
		vulns = res.Findings
	}

	resp := hostVulnResponse{
		Enabled: true, Status: "assessed", Source: res.Source, Ecosystem: res.Ecosystem,
		CheckedPackages: res.CheckedPackages, ScannedAt: res.ScannedAt,
		Vulns: vulns, UnfixedCount: unfixed, Notes: res.Notes,
	}
	if resp.Notes == nil {
		resp.Notes = []string{}
	}

	// Exploit intelligence (CISA KEV + EPSS) for the CVEs found, when that feature is on too.
	if v, _ := s.db.GetSetting(vulnIntelSetting); v == "1" {
		var cves []string
		for _, f := range res.Findings {
			cves = append(cves, f.CVEs...)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		ir := s.vuln.Lookup(ctx, cves)
		resp.Intel = &securityIntel{Enabled: true, Result: &ir}
	}

	writeJSON(w, http.StatusOK, resp)
}

// osvPackages turns the stored inventory into the packages to query: the source package when the
// package manager records one (dpkg source, apk origin), otherwise the plain package name.
func osvPackages(pkgs []*store.HostPackage) []osv.Package {
	out := make([]osv.Package, 0, len(pkgs))
	for _, p := range pkgs {
		name := strings.TrimSpace(p.Source)
		if name == "" {
			name = p.Name
		}
		version := strings.TrimSpace(p.SourceVersion)
		if version == "" {
			version = p.Version
		}
		if name == "" {
			continue
		}
		out = append(out, osv.Package{Name: name, Version: version})
	}
	return out
}
