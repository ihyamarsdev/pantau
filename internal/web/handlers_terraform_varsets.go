package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/store"
)

// handleTerraformVarsets:
//
//	GET  /api/terraform/varsets
//	POST /api/terraform/varsets
func (s *Server) handleTerraformVarsets(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		varsets, err := s.db.ListTFVarsets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if varsets == nil {
			varsets = []store.TFVarset{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"varsets": varsets,
		})

	case http.MethodPost:
		var req struct {
			Name         string  `json:"name"`
			Description  string  `json:"description"`
			Global       bool    `json:"global"`
			WorkspaceIDs []int64 `json:"workspace_ids"`
		}
		if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		vs := &store.TFVarset{
			Name:         name,
			Description:  req.Description,
			Global:       req.Global,
			WorkspaceIDs: req.WorkspaceIDs,
		}
		id, err := s.db.CreateTFVarset(vs)
		if err != nil {
			if errors.Is(err, store.ErrTFVarsetNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		vs.ID = id
		writeJSON(w, http.StatusCreated, vs)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleTerraformVarsetRoute routes /api/terraform/varsets/{id}...
//
//	GET/PUT/DELETE /api/terraform/varsets/{id}
//	GET/POST       /api/terraform/varsets/{id}/variables
//	PUT/DELETE     /api/terraform/varsets/{id}/variables/{vid}
//	PUT            /api/terraform/varsets/{id}/workspaces
func (s *Server) handleTerraformVarsetRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	// Path starts after /api/terraform/varsets/
	path := strings.TrimPrefix(r.URL.Path, "/api/terraform/varsets/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid varset id"})
		return
	}

	vs, err := s.db.GetTFVarset(id)
	if err != nil {
		if errors.Is(err, store.ErrTFVarsetNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, vs)

		case http.MethodPut:
			var req struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Global      bool   `json:"global"`
			}
			if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
				return
			}
			name := strings.TrimSpace(req.Name)
			if name == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
				return
			}
			vs.Name = name
			vs.Description = req.Description
			vs.Global = req.Global
			if err := s.db.UpdateTFVarset(vs); err != nil {
				if errors.Is(err, store.ErrTFVarsetNameTaken) {
					writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			// Reload with fresh counts
			updated, _ := s.db.GetTFVarset(id)
			if updated == nil {
				updated = vs
			}
			writeJSON(w, http.StatusOK, updated)

		case http.MethodDelete:
			if err := s.db.DeleteTFVarset(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	switch parts[1] {
	case "workspaces":
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			WorkspaceIDs []int64 `json:"workspace_ids"`
		}
		if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
			return
		}
		if err := s.db.SetTFVarsetWorkspaces(id, req.WorkspaceIDs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated, _ := s.db.GetTFVarset(id)
		if updated == nil {
			updated = vs
		}
		writeJSON(w, http.StatusOK, updated)

	case "variables":
		s.handleVarsetVariables(w, r, vs, parts[2:])

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleVarsetVariables(w http.ResponseWriter, r *http.Request, vs *store.TFVarset, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			vars, err := s.db.ListTFVariables(0, vs.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"variables": sanitizeVariableList(vars),
			})

		case http.MethodPost:
			var req struct {
				Key         string `json:"key"`
				Value       string `json:"value"`
				Category    string `json:"category"`
				HCL         bool   `json:"hcl"`
				Sensitive   bool   `json:"sensitive"`
				Description string `json:"description"`
			}
			if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
				return
			}
			if err := validateVarKey(req.Key); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if err := validateVarCategoryAndHCL(req.Category, req.HCL); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}

			v := &store.TFVariable{
				VarsetID:    vs.ID,
				Key:         req.Key,
				Value:       req.Value,
				Category:    req.Category,
				HCL:         req.HCL,
				Sensitive:   req.Sensitive,
				Description: req.Description,
			}
			id, err := s.db.CreateTFVariable(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			v.ID = id
			writeJSON(w, http.StatusCreated, sanitizeVariable(*v))

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	vid, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil || vid <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid variable id"})
		return
	}

	existing, err := s.db.GetTFVariable(vid)
	if err != nil {
		if errors.Is(err, store.ErrTFVariableNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing.VarsetID != vs.ID {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Key         string `json:"key"`
			Value       string `json:"value"`
			Category    string `json:"category"`
			HCL         bool   `json:"hcl"`
			Sensitive   bool   `json:"sensitive"`
			Description string `json:"description"`
		}
		if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
			return
		}
		if req.Key != "" {
			if err := validateVarKey(req.Key); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			existing.Key = req.Key
		}
		if req.Category != "" {
			if err := validateVarCategoryAndHCL(req.Category, req.HCL); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			existing.Category = req.Category
			existing.HCL = req.HCL
		} else {
			existing.HCL = req.HCL
		}

		existing.Sensitive = req.Sensitive
		if req.Value != "" || !req.Sensitive {
			existing.Value = req.Value
		}
		existing.Description = req.Description

		if err := s.db.UpdateTFVariable(existing); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		updated, _ := s.db.GetTFVariable(vid)
		if updated == nil {
			updated = existing
		}
		writeJSON(w, http.StatusOK, sanitizeVariable(*updated))

	case http.MethodDelete:
		if err := s.db.DeleteTFVariable(vid); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
