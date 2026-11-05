package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/logger"
)

// ansibleConfigSetting holds the global ansible.cfg additions (validated by ansible.ParseConfig).
const ansibleConfigSetting = "ansible_config"

// SetAnsibleLibraryDir sets the server directory of the shared role/collection library.
func (s *Server) SetAnsibleLibraryDir(dir string) { s.ansibleLibDir = dir }

// libraryGate answers 409 unless the library directory is configured (and, for installs, ansible-galaxy exists).
func (s *Server) libraryGate(w http.ResponseWriter, install bool) bool {
	if s.ansibleLibDir == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "ansible library directory is not configured"})
		return false
	}
	if install && !ansible.GalaxyAvailable() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "ansible-galaxy is not available on this server"})
		return false
	}
	return true
}

func (s *Server) writeLibrary(w http.ResponseWriter) {
	lib, err := ansible.ListLibrary(s.ansibleLibDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"available": ansible.GalaxyAvailable(), "collections": lib.Collections, "roles": lib.Roles})
}

// libraryStream answers an install as NDJSON so the UI can show the installer's output while it runs:
// {"log":"..."} lines, then {"done":true} or {"done":true,"error":"..."}. A failure before anything was written
// (validation) is a plain JSON 400 instead.
type libraryStream struct {
	w       http.ResponseWriter
	started bool
}

func (l *libraryStream) emit(v map[string]interface{}) {
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

func (l *libraryStream) Write(p []byte) (int, error) {
	l.emit(map[string]interface{}{"log": string(p)})
	return len(p), nil
}

func (l *libraryStream) finish(what string, err error) {
	switch {
	case err != nil && !l.started:
		writeJSON(l.w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case err != nil:
		l.emit(map[string]interface{}{"done": true, "error": err.Error()})
	default:
		logger.Info("ansible", "library", "Installed "+what)
		l.emit(map[string]interface{}{"done": true})
	}
}

// handleAnsibleLibrary: GET /api/ansible/library
func (s *Server) handleAnsibleLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAnsibleGate(w) || !s.libraryGate(w, false) {
		return
	}
	s.writeLibrary(w)
}

// handleAnsibleLibraryRoute handles /api/ansible/library/{galaxy|requirements|local} (POST) and
// /api/ansible/library/{collections|roles}/{name} (DELETE).
func (s *Server) handleAnsibleLibraryRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ansible/library/"), "/"), "/")
	switch {
	case len(parts) == 1 && r.Method == http.MethodPost && (parts[0] == "galaxy" || parts[0] == "requirements" || parts[0] == "local"):
		if !s.libraryGate(w, true) {
			return
		}
		switch parts[0] {
		case "galaxy":
			s.handleLibraryGalaxy(w, r)
		case "requirements":
			s.handleLibraryRequirements(w, r)
		default:
			s.handleLibraryLocal(w, r)
		}
	case len(parts) == 2 && r.Method == http.MethodDelete:
		if !s.libraryGate(w, false) {
			return
		}
		err := ansible.RemoveLibraryItem(s.ansibleLibDir, parts[0], parts[1])
		switch {
		case errors.Is(err, fs.ErrNotExist):
			http.NotFound(w, r)
		case err != nil:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			logger.Info("ansible", "library", fmt.Sprintf("Removed %s %s", parts[0], parts[1]))
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
	case len(parts) == 1 && parts[0] != "":
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleLibraryGalaxy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return
	}
	req.Name, req.Version = strings.TrimSpace(req.Name), strings.TrimSpace(req.Version)
	stream := &libraryStream{w: w}
	// The install outlives a closed browser tab: stopping ansible-galaxy halfway would leave a partial collection.
	stream.finish(req.Kind+" "+req.Name, ansible.InstallGalaxy(context.WithoutCancel(r.Context()), s.ansibleLibDir, req.Kind, req.Name, req.Version, stream))
}

func (s *Server) handleLibraryRequirements(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return
	}
	stream := &libraryStream{w: w}
	stream.finish("requirements", ansible.InstallRequirements(context.WithoutCancel(r.Context()), s.ansibleLibDir, req.Content, stream))
}

// handleLibraryLocal installs an uploaded collection (folder with galaxy.yml, or one .tar.gz) or role folder.
func (s *Server) handleLibraryLocal(w http.ResponseWriter, r *http.Request) {
	archive, fields, ok := readAnsibleImport(w, r, "kind", "name", "archive")
	if !ok {
		return
	}
	fail := func(msg string) { writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg}) }
	entries, err := ansible.ValidateArchive(archive)
	if err != nil {
		fail(err.Error())
		return
	}
	if len(entries) == 0 {
		fail("select a folder or archive to import")
		return
	}
	work, err := os.MkdirTemp("", "pantau-library-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer os.RemoveAll(work)
	dir, err := ansible.ExtractArchive(archive, work)
	if err != nil {
		fail(err.Error())
		return
	}

	switch fields["kind"] {
	case "collection":
		src := dir
		if len(entries) == 1 { // one uploaded file: a collection tarball
			for name := range entries {
				if !strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".tgz") {
					fail("a single uploaded file must be a collection tarball (.tar.gz)")
					return
				}
				src = filepath.Join(dir, name)
			}
		} else if _, err := os.Stat(filepath.Join(dir, "galaxy.yml")); err != nil {
			fail("a collection folder must contain galaxy.yml at its root")
			return
		}
		stream := &libraryStream{w: w}
		stream.finish("local collection", ansible.InstallLocalCollection(context.WithoutCancel(r.Context()), s.ansibleLibDir, src, stream))
	case "role":
		name := strings.TrimSpace(fields["name"])
		if err := ansible.InstallLocalRole(s.ansibleLibDir, name, dir); err != nil {
			fail(err.Error())
			return
		}
		(&libraryStream{w: w}).finish("local role "+name, nil)
	default:
		fail("kind must be collection or role")
	}
}

// handleAnsibleConfig: GET/PUT /api/ansible/config, the global ansible.cfg additions applied to every run.
func (s *Server) handleAnsibleConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	respond := func(content string) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"content": content, "allowed_sections": ansible.ConfigSections, "managed_keys": ansible.ManagedConfigKeys,
		})
	}
	switch r.Method {
	case http.MethodGet:
		content, _ := s.db.GetSetting(ansibleConfigSetting)
		respond(content)
	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if !decodeAnsibleBody(w, r, &req) {
			return
		}
		if _, err := ansible.ParseConfig(req.Content); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.db.SetSetting(ansibleConfigSetting, req.Content); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("ansible", "config", "Updated ansible config")
		respond(req.Content)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
