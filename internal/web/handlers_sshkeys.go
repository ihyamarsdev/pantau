package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/logger"
	"pantau/internal/store"
)

func (s *Server) handleSSHKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := s.db.ListSSHKeys()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, keys)

	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			PrivateKey string `json:"private_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.PrivateKey = strings.TrimSpace(req.PrivateKey)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if req.PrivateKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "private_key is required"})
			return
		}

		// Normalize trailing newline of the key
		normKey := req.PrivateKey + "\n"

		pubKey, fingerprint, err := store.ParseSSHPrivateKey([]byte(normKey))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		k := &store.SSHKey{
			Name:        req.Name,
			PrivateKey:  normKey,
			PublicKey:   pubKey,
			Fingerprint: fingerprint,
		}
		id, err := s.db.CreateSSHKey(k)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "ssh key with this name already exists"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		created, err := s.db.GetSSHKey(id)
		if err != nil || created == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ssh key saved but could not be reloaded"})
			return
		}
		logger.Info("ssh_key", k.Name, "SSH key created")
		writeJSON(w, http.StatusCreated, created)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSSHKeyDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/ssh-keys/")
	if subpath == "" {
		if r.Method == http.MethodGet {
			s.handleSSHKeys(w, r)
			return
		}
		http.Error(w, "valid key id required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(subpath, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid key id required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		key, err := s.db.GetSSHKey(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if key == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "ssh key not found"})
			return
		}
		writeJSON(w, http.StatusOK, key)

	case http.MethodDelete:
		err := s.db.DeleteSSHKey(id)
		if errors.Is(err, store.ErrSSHKeyNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "ssh key not found"})
			return
		}
		if errors.Is(err, store.ErrSSHKeyInUse) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
