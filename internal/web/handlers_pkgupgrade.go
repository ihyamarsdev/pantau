package web

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"pantau/internal/inspector"
	"pantau/internal/logger"
)

// handlePackagesUpgradePreview serves GET /api/hosts/{id}/packages/upgrade-preview
func (s *Server) handlePackagesUpgradePreview(w http.ResponseWriter, r *http.Request, hostID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.inspector == nil {
		http.Error(w, "inspector not available", http.StatusInternalServerError)
		return
	}
	if h, err := s.db.GetHost(hostID); err != nil || h == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "host not found"})
		return
	}

	preview, err := s.inspector.GetUpgradePreview(r.Context(), hostID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, preview)
}

// handleWSPkgUpgrade handles WebSocket /ws/pkg/upgrade?host_id=&mode=security|all
func (s *Server) handleWSPkgUpgrade(w http.ResponseWriter, r *http.Request) {
	hostIDStr := r.URL.Query().Get("host_id")
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "all"
	}

	hostID, err := strconv.ParseInt(hostIDStr, 10, 64)
	if err != nil || hostID <= 0 {
		http.Error(w, "invalid host_id", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	sendTextErr := func(msg string) {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(msg+"\n[pantau] exit=1\n"))
	}

	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		sendTextErr("Host not found")
		return
	}

	// Guard in-flight per-host package upgrade
	unlock, err := inspector.TryLockUpgrade(hostID)
	if err != nil {
		sendTextErr(fmt.Sprintf("Upgrade rejected: %v", err))
		return
	}
	defer unlock()

	// Detect package manager
	preview, err := s.inspector.GetUpgradePreview(r.Context(), hostID)
	if err != nil {
		sendTextErr(fmt.Sprintf("Failed to detect package manager: %v", err))
		return
	}
	if !preview.Supported {
		sendTextErr(fmt.Sprintf("Host does not support package upgrade: %s", preview.Note))
		return
	}

	// Build upgrade command
	cmd, err := inspector.BuildUpgradeCommand(preview.Manager, mode)
	if err != nil {
		sendTextErr(fmt.Sprintf("Invalid upgrade configuration: %v", err))
		return
	}

	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		sendTextErr(fmt.Sprintf("SSH connection failed: %v", err))
		return
	}
	defer runner.Close()

	logger.Info("packages", host.Name, fmt.Sprintf("Starting package upgrade (manager: %s, mode: %s)", preview.Manager, mode))

	outReader, outWriter := io.Pipe()

	// Stream binary frames to client
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := outReader.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				_ = ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}
	}()

	// Close stream if client disconnects
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				_ = outWriter.Close()
				_ = runner.Close()
				return
			}
		}
	}()

	streamErr := runner.Stream(cmd, outWriter)
	_ = outWriter.Close()

	logger.Info("packages", host.Name, fmt.Sprintf("Package upgrade stream finished (err: %v)", streamErr))

	// Best-effort background rescans
	go func(hid int64, hname string) {
		// ponytail: small cooldown to allow package manager locks to release cleanly
		time.Sleep(2 * time.Second)
		_ = s.inspector.ScanPackages(hid)
		_ = s.inspector.ScanSecurity(hid)
		logger.Info("packages", hname, "Post-upgrade package and security rescans completed")
	}(host.ID, host.Name)
}
