package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/gwupdate"
	"pantau/internal/terraform"
)

// Installing a newer CLI (ADR 0081).
//
// The install runs in Pantau's own directory, verified against the checksum the upstream publishes,
// so it needs neither root nor a package manager. The answer is an NDJSON stream so the UI can show
// the steps while they run:
//
//	{"log":"..."}   one line of the install output
//	{"done":true}  the install finished
//	{"done":true,"error":"..."}  the install failed
//
// A failure before anything was written is a plain JSON 400 instead.

// installStream answers an install as NDJSON so the UI can show the steps while they run.
type installStream struct {
	w       http.ResponseWriter
	started bool
}

func (l *installStream) emit(v map[string]interface{}) {
	if !l.started {
		h := l.w.Header()
		h.Set("Content-Type", "application/x-ndjson")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // keep reverse proxies from buffering the live output
		l.w.WriteHeader(http.StatusOK)
		l.started = true
	}
	_ = json.NewEncoder(l.w).Encode(v)
	_ = http.NewResponseController(l.w).Flush()
}

// Write sends a chunk of the install output to the client.
func (l *installStream) Write(p []byte) (int, error) {
	l.emit(map[string]interface{}{"log": string(p)})
	return len(p), nil
}

// finish closes the stream with the outcome of the install.
func (l *installStream) finish(err error) {
	switch {
	case err != nil && !l.started:
		writeJSON(l.w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case err != nil:
		l.emit(map[string]interface{}{"done": true, "error": err.Error()})
	default:
		l.emit(map[string]interface{}{"done": true})
	}
}

// handleCLIInstall installs one CLI: POST /api/ansible/install or /api/terraform/install.
//
// The body names the target ("ansible", "terraform" or "tofu") and, optionally, the version to
// install. Without a version the newest upstream release is installed.
func (s *Server) handleCLIInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var body struct {
		Target  string `json:"target"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request body is not valid JSON"})
		return
	}

	target := strings.TrimSpace(body.Target)
	if target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the target is required"})
		return
	}
	switch target {
	case gwupdate.TargetAnsible, gwupdate.TargetTerraform, gwupdate.TargetOpenTofu:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown target " + target})
		return
	}

	root := gwupdate.ManagedRoot()
	if root == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Pantau does not manage the CLI binaries on this server",
		})
		return
	}

	version := strings.TrimSpace(body.Version)
	if version == "" {
		// Without a version the newest upstream release is installed.
		rel, err := gwupdate.Latest(r.Context(), target)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "the latest release could not be looked up: " + err.Error(),
			})
			return
		}
		version = rel.Version
	}

	stream := &installStream{w: w}
	err := gwupdate.Install(r.Context(), target, version, root, stream)

	// The lookup of the CLI now points at the new version.
	if err == nil {
		ansible.InvalidateAvailableCache()
		terraform.InvalidateDetectCache()
	}
	stream.finish(err)
}
