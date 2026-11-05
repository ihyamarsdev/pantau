package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// handleAnsibleAssets:
//
//	GET /api/ansible/assets[?kind=template|file] -> {"assets":[{id,kind,name,size,updated_at}]}
//	PUT /api/ansible/assets?kind=...&name=...    -> raw body is the content; creates or replaces
func (s *Server) handleAnsibleAssets(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	kind := r.URL.Query().Get("kind")
	switch r.Method {
	case http.MethodGet:
		if kind != "" && kind != ansible.AssetTemplate && kind != ansible.AssetFile {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid kind"})
			return
		}
		assets, err := s.db.ListAnsibleAssets(kind)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"assets": assets})

	case http.MethodPut:
		name := r.URL.Query().Get("name")
		r.Body = http.MaxBytesReader(w, r.Body, ansible.MaxAssetFileBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file exceeds 5 MiB"})
			return
		}
		asset := ansible.Asset{Kind: kind, Name: name, Data: data}
		if err := ansible.ValidateAsset(asset); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		existing, err := s.db.ListAnsibleAssets("")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		total := int64(len(data))
		for _, a := range existing {
			if a.Kind != kind || a.Name != name {
				total += a.Size
			}
		}
		if total > ansible.MaxAssetsTotalBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "templates and files exceed 32 MiB in total"})
			return
		}
		if err := s.db.SaveAnsibleAsset(kind, name, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("ansible", name, fmt.Sprintf("Saved %s %s (%d bytes)", kind, name, len(data)))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAnsibleAssetRoute handles /api/ansible/assets/{id}:
// GET returns a template's text (uploaded files are never sent back), DELETE removes either kind.
func (s *Server) handleAnsibleAssetRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	id, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ansible/assets/"), "/"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a, err := s.db.GetAnsibleAsset(id)
		if err != nil {
			writeAssetErr(w, err)
			return
		}
		if a.Kind != ansible.AssetTemplate {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uploaded files cannot be downloaded"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": a.ID, "name": a.Name, "content": string(a.Data)})

	case http.MethodDelete:
		a, err := s.db.GetAnsibleAsset(id)
		if err == nil {
			err = s.db.DeleteAnsibleAsset(id)
		}
		if err != nil {
			writeAssetErr(w, err)
			return
		}
		logger.Info("ansible", a.Name, fmt.Sprintf("Deleted %s %s", a.Kind, a.Name))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeAssetErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrAnsibleAssetNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
