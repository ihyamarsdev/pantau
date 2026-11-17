package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// pkgUpdateCooldown is the anti-flood window of the Package Update Check: each check runs a read-only
// package manager query over SSH, so it must not be spammed.
const pkgUpdateCooldown = 60 * time.Second

// handlePackageUpdates serves GET /api/hosts/{id}/packages/updates (the stored result of the last check)
// and POST /api/hosts/{id}/packages/updates/check (run the check now).
func (s *Server) handlePackageUpdates(w http.ResponseWriter, r *http.Request, hostID int64, check bool) {
	if s.inspector == nil {
		http.Error(w, "inspector not available", http.StatusInternalServerError)
		return
	}
	host, err := s.db.GetHost(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if host == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "host not found"})
		return
	}

	stored, err := s.db.GetPackageUpdateCheck(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !check {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if stored == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"checked": false})
			return
		}
		if rem := pkgUpdateCooldownRemaining(stored); rem > 0 {
			w.Header().Set("X-Cooldown-Remaining", fmt.Sprintf("%d", rem))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"checked": true, "check": stored})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if rem := pkgUpdateCooldownRemaining(stored); rem > 0 {
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"ok":                 false,
			"error":              fmt.Sprintf("Update check cooldown active. Please wait %d seconds.", rem),
			"cooldown_remaining": rem,
		})
		return
	}

	result, err := s.inspector.CheckPackageUpdates(r.Context(), hostID)
	if err != nil {
		if errors.Is(err, inspector.ErrPackageUpdateCheckInProgress) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"ok":    false,
				"error": "Package update check already in progress for this host",
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	w.Header().Set("X-Cooldown-Remaining", fmt.Sprintf("%d", int(pkgUpdateCooldown.Seconds())))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "checked": true, "check": result})
}

// pkgUpdateCooldownRemaining returns the seconds left before another check is allowed, 0 when it is.
func pkgUpdateCooldownRemaining(stored *store.PackageUpdateCheck) int {
	if stored == nil || stored.CheckedAt.IsZero() {
		return 0
	}
	elapsed := time.Since(stored.CheckedAt)
	if elapsed >= pkgUpdateCooldown {
		return 0
	}
	rem := int(math.Ceil((pkgUpdateCooldown - elapsed).Seconds()))
	if rem < 1 {
		rem = 1
	}
	return rem
}
