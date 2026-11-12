package web

import (
	"strconv"
	"strings"
	"sync"

	"pantau/internal/logger"
)

// scanFleetOnLogin runs the scans that are otherwise on demand or on a timer
// (domain scan, security scan) for every reachable host, right after an admin
// logs in. A sweep still running from an earlier login makes this a no-op.
func (s *Server) scanFleetOnLogin() {
	if s.inspector == nil || !s.loginScanMu.TryLock() {
		return
	}
	defer s.loginScanMu.Unlock()

	hosts, err := s.db.ListHosts()
	if err != nil {
		return
	}
	// Same switch as the background worker: interval 0 disables security scans.
	securityOn := true
	if v, _ := s.db.GetSetting("security_scan_interval_hours"); strings.TrimSpace(v) != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n <= 0 {
			securityOn = false
		}
	}

	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for _, h := range hosts {
		if strings.EqualFold(h.Status, "down") {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(id int64) {
			defer func() { <-sem; wg.Done() }()
			// Errors include "already in progress", which is fine: that scan is the one we wanted.
			if scan, err := s.inspector.ScanWebDomains(id); err == nil {
				if err := s.db.ReplaceWebScan(id, scan); err != nil {
					logger.Warn("scan", "", "Login domain scan store failed: "+err.Error())
				}
			}
			if securityOn {
				_ = s.inspector.ScanSecurity(id)
			}
		}(h.ID)
	}
	wg.Wait()
}
