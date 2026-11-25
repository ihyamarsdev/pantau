package web

import (
	"fmt"
	"net/http"
	"strconv"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
)

// handleHostKeyReset: POST /api/hosts/{id}/hostkey-reset. The host was reinstalled, so the next connection
// trusts its current key. Without this, a changed key stays refused until the trust record is removed by hand.
func (s *Server) handleHostKeyReset(w http.ResponseWriter, r *http.Request, idStr string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid host ID", http.StatusBadRequest)
		return
	}
	h, err := s.db.GetHost(id)
	if err != nil || h == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	// Same "host:port" string the SSH dial uses as the trust key.
	if err := sshrunner.ForgetHostKey(fmt.Sprintf("%s:%d", h.Host, h.Port)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Warn("ssh", h.Name, "trusted host key reset: the next connection trusts the host's current key")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
