package web

// CVE Exposure Radar HTTP handlers (ADR 0088). GET reads the stored radar; POST /scan rebuilds it
// on demand. The feature honours the osv_enabled opt-in and stays airgapped when it is off.

import (
	"context"
	"net/http"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// handleHostExposure dispatches GET /api/hosts/{id}/exposure and POST /api/hosts/{id}/exposure/scan.
func (s *Server) handleHostExposure(w http.ResponseWriter, r *http.Request, hostID int64, parts []string) {
	host, err := s.db.GetHost(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}

	if len(parts) == 0 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		scan, err := s.db.GetExposureScan(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if scan == nil {
			writeJSON(w, http.StatusOK, map[string]any{"scanned": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scanned": true, "scan": scan})
		return
	}

	if len(parts) == 1 && parts[0] == "scan" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.runExposureScan(w, r, host)
		return
	}
	if len(parts) == 1 && parts[0] == "mitigate" {
		s.handleExposureMitigate(w, r, hostID)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) runExposureScan(w http.ResponseWriter, r *http.Request, host *store.Host) {
	enabled := false
	if v, _ := s.db.GetSetting(osvEnabledSetting); v == "1" {
		enabled = true
	}
	opt := inspector.ExposureScanOptions{
		OSVClient:  s.osv,
		OSVEnabled: enabled,
		RepoPaths:  s.exposureRepoPaths(),
		Webroots:   s.exposureWebroots(),
		CFProxied:  s.exposureCloudflareDomains(),
	}
	if enabled && s.vuln != nil {
		opt.KEVLookup = func(ctx context.Context, cves []string) map[string]inspector.OSVIntel {
			res := s.vuln.Lookup(ctx, cves)
			out := make(map[string]inspector.OSVIntel, len(res.CVEs))
			for id, info := range res.CVEs {
				v := inspector.OSVIntel{}
				if info.KEV != nil {
					v.KEV = true
				}
				if info.EPSS != nil {
					v.EPSS = *info.EPSS
				}
				out[id] = v
			}
			return out
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	scan := s.inspector.ScanExposure(ctx, host, opt)
	if err := s.db.SaveExposureScan(scan); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scan": scan})
}

// handleFleetExposure serves GET /api/security/exposure: the latest radar digest of every host,
// read from the store without SSH. Used by the AI agent's get_exposed_cves tool (ADR 0073).
func (s *Server) handleFleetExposure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scans, err := s.db.ListExposureScans()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hosts := make([]map[string]any, 0, len(scans))
	totalCritical := 0
	for _, scan := range scans {
		totalCritical += scan.CriticalExposed
		critical := []store.ExposureFinding{}
		for _, f := range scan.Findings {
			if f.IsCriticalExposed() {
				critical = append(critical, f)
			}
		}
		hosts = append(hosts, map[string]any{
			"host_id": scan.HostID, "scanned_at": scan.ScannedAt, "status": scan.Status,
			"enabled": scan.Enabled, "critical_exposed": scan.CriticalExposed,
			"kernel": scan.Kernel, "critical_findings": critical,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hosts": hosts, "total_critical_exposed": totalCritical, "host_count": len(hosts),
	})
}

// exposureRepoPaths returns the absolute directories of cloned Git repositories on the Pantau server.
func (s *Server) exposureRepoPaths() []string {
	repos, err := s.db.ListGitRepos()
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(repos))
	for _, repo := range repos {
		if repo.Dir != "" {
			paths = append(paths, repo.Dir)
		}
	}
	return paths
}

// exposureWebroots returns configured remote webroot paths. Webroots are stored per host in the
// domains table (nginx root); those on the Pantau server itself are read locally. For agentless
// hosts the runtime probe still covers installed runtimes; lockfile scanning applies to repos.
func (s *Server) exposureWebroots() []string {
	// No global webroot setting exists yet; cloned repos are the lockfile source. Host nginx roots
	// are remote and cannot be read from the Pantau filesystem, so they are intentionally omitted.
	return nil
}

// exposureCloudflareDomains returns the set of domains that are proxied through a configured
// Cloudflare zone, so the radar can elevate their vulnerabilities to CRITICAL_EXPOSED.
func (s *Server) exposureCloudflareDomains() map[string]bool {
	out := map[string]bool{}
	if s.cfClient == nil {
		return out
	}
	accounts, err := s.db.ListCloudflareAccounts()
	if err != nil {
		return out
	}
	for _, account := range accounts {
		if account.APIToken == "" {
			continue
		}
		cf := s.cfClient(account.APIToken)
		zones, err := cf.ListZones(context.Background(), account.AccountID)
		if err != nil {
			continue
		}
		for _, z := range zones {
			out[z.Name] = true
		}
	}
	return out
}
