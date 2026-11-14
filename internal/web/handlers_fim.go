package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"pantau/internal/inspector"
)

// handleFIMBaseline previews the current file manifest of a host target WITHOUT saving it.
// The operator confirms the preview; the accepted manifest is then stored through the
// normal rule create/update path, so rule storage, snapshots, and drift alerts stay in one place.
func (s *Server) handleFIMBaseline(w http.ResponseWriter, r *http.Request, hostID int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	target := strings.TrimSpace(req.Target)
	if err := inspector.ValidateFIMTarget(target); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer runner.Close()

	capture, err := s.inspector.CaptureFIMManifest(hostID, runner, target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if capture.NoHashTool {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "no sha256 tool (sha256sum/shasum) on the host"})
		return
	}
	if capture.SudoDenied {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "passwordless sudo unavailable; some files could not be read"})
		return
	}
	files := inspector.FIMCaptureFiles(capture)
	if len(files) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "no readable file matched the target"})
		return
	}
	expected, err := inspector.EncodeFIMBaseline(files)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"target":   target,
		"expected": expected,
		"files":    files,
		"missing":  capture.Missing,
		"denied":   capture.Denied,
	})
}

// handleFIMRuleDiff compares the current host state with a stored file_integrity baseline.
func (s *Server) handleFIMRuleDiff(w http.ResponseWriter, r *http.Request, hostID int64, ruleID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rules, err := s.db.ListDesiredRules(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var target, expected string
	found := false
	for i := range rules {
		if rules[i].ID == ruleID {
			target, expected = rules[i].Target, rules[i].Expected
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}
	base, err := inspector.ParseFIMExpected(expected)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer runner.Close()

	diff, capture, err := s.inspector.FIMDiffAgainst(hostID, runner, target, base)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"diff":    diff,
		"denied":  capture.Denied,
		"missing": capture.Missing,
	})
}
