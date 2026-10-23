package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pantau/internal/sshconfig"
	"pantau/internal/store"
)

const (
	maxImportBody  = 1 << 20 // 1 MiB of ssh config text
	maxImportHosts = 500
)

// Duplicate reasons shared with the UI (translated client-side).
const (
	importReasonExists  = "exists"            // same endpoint already registered in Pantau
	importReasonInFile  = "duplicate_in_file" // same endpoint as an earlier entry of this import
	importReasonInvalid = "invalid"
)

type importPreviewEntry struct {
	sshconfig.Entry
	Duplicate bool   `json:"duplicate"`
	Reason    string `json:"reason,omitempty"`
	SameAs    string `json:"same_as,omitempty"`
}

type importHostInput struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
}

type importSkipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	SameAs string `json:"same_as,omitempty"`
}

// importEndpointKey identifies a host by what it connects to. The user defaults
// to root and the port to 22, matching how POST /api/hosts stores them.
func importEndpointKey(host string, port int, user string) string {
	if port <= 0 {
		port = 22
	}
	if user == "" {
		user = "root"
	}
	return strings.ToLower(host) + "|" + strconv.Itoa(port) + "|" + user
}

func (s *Server) existingEndpoints() (map[string]string, error) {
	hosts, err := s.db.ListHosts()
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(hosts))
	for _, h := range hosts {
		m[importEndpointKey(h.Host, h.Port, h.User)] = h.Name
	}
	return m, nil
}

// handleHostImport serves /api/hosts/import (POST) and /api/hosts/import/preview (POST).
func (s *Server) handleHostImport(w http.ResponseWriter, r *http.Request, rest []string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch {
	case len(rest) == 1 && rest[0] == "preview":
		s.handleHostImportPreview(w, r)
	case len(rest) == 0 || (len(rest) == 1 && rest[0] == ""):
		s.handleHostImportCommit(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleHostImportPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBody)
	var req struct {
		Content string `json:"content"`
		Source  string `json:"source"` // "server" reads the server user's ~/.ssh/config
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	content := req.Content
	if req.Source == "server" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot resolve home directory"})
			return
		}
		p := filepath.Join(home, ".ssh", "config")
		data, err := os.ReadFile(p)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": fmt.Sprintf("cannot read %s: %v", p, err)})
			return
		}
		if len(data) > maxImportBody {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "ssh config too large"})
			return
		}
		content = string(data)
	}
	if strings.TrimSpace(content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ssh config is empty"})
		return
	}

	existing, err := s.existingEndpoints()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	parsed := sshconfig.Parse(content)
	seenInFile := map[string]string{}
	entries := make([]importPreviewEntry, 0, len(parsed.Entries))
	for _, e := range parsed.Entries {
		pe := importPreviewEntry{Entry: e}
		key := importEndpointKey(e.HostName, e.Port, e.User)
		if name, ok := existing[key]; ok {
			pe.Duplicate, pe.Reason, pe.SameAs = true, importReasonExists, name
		} else if alias, ok := seenInFile[key]; ok {
			pe.Duplicate, pe.Reason, pe.SameAs = true, importReasonInFile, alias
		} else {
			seenInFile[key] = e.Alias
		}
		entries = append(entries, pe)
	}

	warnings := parsed.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries, "warnings": warnings})
}

func (s *Server) handleHostImportCommit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBody)
	var req struct {
		Hosts []importHostInput `json:"hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if len(req.Hosts) > maxImportHosts {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("too many hosts (max %d)", maxImportHosts)})
		return
	}

	// Duplicates are re-checked here: the preview may be stale or hand-edited.
	endpoints, err := s.existingEndpoints()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	created := []store.Host{}
	skipped := []importSkipped{}
	for _, in := range req.Hosts {
		h := store.Host{
			Name: strings.TrimSpace(in.Name),
			Host: strings.TrimSpace(in.Host),
			Port: in.Port,
			User: strings.TrimSpace(in.User),
		}
		if h.Port == 0 {
			h.Port = 22
		}
		if h.User == "" {
			h.User = "root"
		}
		if h.Name == "" || h.Host == "" || h.Port < 1 || h.Port > 65535 {
			skipped = append(skipped, importSkipped{Name: in.Name, Reason: importReasonInvalid})
			continue
		}

		key := importEndpointKey(h.Host, h.Port, h.User)
		if name, ok := endpoints[key]; ok {
			skipped = append(skipped, importSkipped{Name: h.Name, Reason: importReasonExists, SameAs: name})
			continue
		}

		id, err := s.db.CreateHost(&h)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error":   err.Error(),
				"created": created,
				"skipped": skipped,
			})
			return
		}
		h.ID = id
		endpoints[key] = h.Name
		created = append(created, h)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"created": created, "skipped": skipped})
}

// handleHostGroups serves PUT/POST /api/hosts/groups: set the group of many hosts atomically.
func (s *Server) handleHostGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBody)
	var req struct {
		Assignments []store.HostGroupAssignment `json:"assignments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if len(req.Assignments) > maxImportHosts {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("too many assignments (max %d)", maxImportHosts)})
		return
	}
	if err := s.db.UpdateHostGroups(req.Assignments); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrHostNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "updated": len(req.Assignments)})
}
