package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/teleport"
)

// The Teleport CLI (tsh) card in the Teleport Profiles drawer. The update check is read-only (ADR 0080): it reads
// the newest stable release and, when a profile is given, the proxy's version. The install is the managed install
// (ADR 0081): it writes only <data>/cli/tsh, after the checksum and a smoke test pass.
//
//	GET  /api/teleport/cli/status        the tsh Pantau runs (source, path, version) and kubectl
//	POST /api/teleport/cli/check-update  {"profile_id":7,"force":false}
//	POST /api/teleport/cli/install       {"version":"18.11.10"}, streamed as NDJSON like the other installs

// teleportCLIStatus answers GET /api/teleport/cli/status.
func (s *Server) handleTeleportCLIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	m := s.teleportManager(w)
	if m == nil {
		return
	}
	kube := inspector.DetectLocalKubectl()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":  true,
		"tsh": m.TSHInfo(r.Context()),
		"kubectl": map[string]interface{}{
			"installed": kube.Version != "",
			"version":   kube.Version,
			"source":    kube.Source,
			"path":      kube.Path,
		},
	})
}

// teleportCheckBody is the body of POST /api/teleport/cli/check-update. An empty body is a check without a proxy.
type teleportCheckBody struct {
	ProfileID int64 `json:"profile_id"`
	Force     bool  `json:"force"`
}

// teleportCLICheckResponse is the answer of POST /api/teleport/cli/check-update.
type teleportCLICheckResponse struct {
	OK bool `json:"ok"`
	teleport.UpdateCheck
}

// handleTeleportCLICheckUpdate answers POST /api/teleport/cli/check-update. It only reads. An upstream that cannot be
// reached is status check_failed with HTTP 200, never "up_to_date".
func (s *Server) handleTeleportCLICheckUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	m := s.teleportManager(w)
	if m == nil {
		return
	}
	var body teleportCheckBody
	r.Body = http.MaxBytesReader(w, r.Body, teleportBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "bad request", "code": "INVALID_INPUT"})
		return
	}

	// The proxy comes from the stored profile, never from the request: only the profile ID is taken from the body.
	var profile *teleport.Profile
	if body.ProfileID > 0 {
		p, err := s.db.GetTeleportProfile(body.ProfileID)
		if err != nil || p == nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "profile not found", "code": "TELEPORT_PROFILE_NOT_FOUND"})
			return
		}
		tp := teleportProfileOf(p)
		profile = &tp
	}

	writeJSON(w, http.StatusOK, teleportCLICheckResponse{
		OK:          true,
		UpdateCheck: m.CheckUpdate(r.Context(), profile, body.Force),
	})
}

// handleTeleportCLIInstall answers POST /api/teleport/cli/install. Without a version the newest release is installed.
// A second install while one runs is 409 Conflict. Every other failure before the first line is a JSON 400 (or 502
// when the latest release cannot be looked up); later failures end the NDJSON stream with an error.
func (s *Server) handleTeleportCLIInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	m := s.teleportManager(w)
	if m == nil {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, teleportBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request body is not valid JSON"})
		return
	}

	version := strings.TrimSpace(body.Version)
	if version == "" {
		rel, _, err := m.Latest(r.Context(), false)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{
				"ok": false, "error": "the latest tsh release could not be looked up: " + err.Error(), "code": "CHECK_FAILED",
			})
			return
		}
		version = rel.Version
	}

	stream := &installStream{w: w}
	err := m.InstallTSH(r.Context(), version, stream)
	if errors.Is(err, teleport.ErrInstallInProgress) && !stream.started {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "error": err.Error(), "code": "INSTALL_IN_PROGRESS"})
		return
	}
	stream.finish(err)
}
