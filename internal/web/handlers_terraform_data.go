package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

const (
	maxTerraformStateSize = 16 * 1024 * 1024 // 16 MiB
	maxTerraformBodyLimit = 1 * 1024 * 1024  // 1 MiB for general JSON bodies
)

var (
	tfVarKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
)

func decodeTFBody(w http.ResponseWriter, r *http.Request, maxBytes int64, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func sanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	res := b.String()
	if res == "" {
		return "workspace"
	}
	return res
}

// sanitizeVariable hides value if sensitive and sets value_set: true
func sanitizeVariable(v store.TFVariable) map[string]interface{} {
	m := map[string]interface{}{
		"id":          v.ID,
		"key":         v.Key,
		"category":    v.Category,
		"hcl":         v.HCL,
		"sensitive":   v.Sensitive,
		"description": v.Description,
	}
	if v.WorkspaceID > 0 {
		m["workspace_id"] = v.WorkspaceID
	}
	if v.VarsetID > 0 {
		m["varset_id"] = v.VarsetID
	}
	if v.Origin != "" {
		m["origin"] = v.Origin
	}
	if v.Sensitive {
		m["value"] = ""
		m["value_set"] = v.Value != ""
	} else {
		m["value"] = v.Value
		m["value_set"] = v.Value != ""
	}
	return m
}

func sanitizeVariableList(vars []store.TFVariable) []map[string]interface{} {
	res := make([]map[string]interface{}, 0, len(vars))
	for _, v := range vars {
		res = append(res, sanitizeVariable(v))
	}
	return res
}

func validateVarKey(key string) error {
	if !tfVarKeyRegex.MatchString(key) {
		return errors.New("invalid variable key: must match ^[A-Za-z_][A-Za-z0-9_-]{0,63}$")
	}
	return nil
}

func validateVarCategoryAndHCL(cat string, hcl bool) error {
	if cat != "terraform" && cat != "env" {
		return errors.New("invalid category: must be 'terraform' or 'env'")
	}
	if hcl && cat != "terraform" {
		return errors.New("hcl flag is only allowed for 'terraform' category")
	}
	return nil
}

// handleTerraformWorkspaceData handles workspace subroutes:
// /state...
// /outputs...
// /variables...
func (s *Server) handleTerraformWorkspaceData(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) == 0 {
		http.NotFound(w, r)
		return
	}

	switch rest[0] {
	case "state":
		s.handleTerraformWorkspaceState(w, r, ws, rest[1:])
	case "outputs":
		s.handleTerraformWorkspaceOutputs(w, r, ws, rest[1:])
	case "variables":
		s.handleTerraformWorkspaceVariables(w, r, ws, rest[1:])
	default:
		http.NotFound(w, r)
	}
}

// --- STATE ENDPOINTS ---

func (s *Server) handleTerraformWorkspaceState(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	// rest can be:
	// [] -> GET /api/terraform/workspaces/{id}/state
	// ["import"] -> POST /api/terraform/workspaces/{id}/state/import
	// ["versions"] -> GET /api/terraform/workspaces/{id}/state/versions
	// ["versions", "{vid}"] -> GET /api/terraform/workspaces/{id}/state/versions/{vid}
	// ["versions", "{vid}", "rollback"] -> POST /api/terraform/workspaces/{id}/state/versions/{vid}/rollback
	// ["diff"] -> GET /api/terraform/workspaces/{id}/state/diff?from=&to=

	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		// GET /api/terraform/workspaces/{id}/state
		latest, err := s.db.LatestTFStateVersion(ws.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if latest == nil || len(latest.State) == 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"serial":            0,
				"lineage":           "",
				"terraform_version": "",
				"resources":         []terraform.StateResource{},
			})
			return
		}

		info, err := terraform.ParseState(latest.State)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to parse state: %v", err)})
			return
		}
		res := info.Resources
		if res == nil {
			res = []terraform.StateResource{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"serial":            info.Serial,
			"lineage":           info.Lineage,
			"terraform_version": info.TFVersion,
			"resources":         res,
		})
		return
	}

	switch rest[0] {
	case "import":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			State string `json:"state"`
		}
		if !decodeTFBody(w, r, maxTerraformStateSize, &req) {
			return
		}
		if strings.TrimSpace(req.State) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state content is required"})
			return
		}

		rawBytes := []byte(req.State)
		info, err := terraform.ParseState(rawBytes)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid terraform state: %v", err)})
			return
		}

		sum := sha256.Sum256(rawBytes)
		shaHex := hex.EncodeToString(sum[:])

		sv := &store.TFStateVersion{
			WorkspaceID:   ws.ID,
			RunID:         0,
			Serial:        info.Serial,
			Lineage:       info.Lineage,
			Source:        "import",
			Size:          int64(len(rawBytes)),
			ResourceCount: len(info.Resources),
			SHA256:        shaHex,
			State:         rawBytes,
		}
		id, err := s.db.CreateTFStateVersion(sv)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sv.ID = id
		// Update workspace resource count and tf_version if available
		ws.ResourceCount = sv.ResourceCount
		if info.TFVersion != "" {
			ws.TFVersion = info.TFVersion
		}
		_ = s.db.UpdateTFWorkspace(ws)
		logger.Info("terraform", ws.Name, fmt.Sprintf("Imported state into workspace %s (version %d)", ws.Name, id))

		writeJSON(w, http.StatusCreated, sv)

	case "versions":
		if len(rest) == 1 {
			// GET /api/terraform/workspaces/{id}/state/versions
			if r.Method != http.MethodGet {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			limit := 50
			if lStr := r.URL.Query().Get("limit"); lStr != "" {
				if n, err := strconv.Atoi(lStr); err == nil && n > 0 {
					limit = n
				}
			}
			versions, err := s.db.ListTFStateVersions(ws.ID, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if versions == nil {
				versions = []store.TFStateVersion{}
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"versions": versions,
			})
			return
		}

		// rest: ["versions", "{vid}", ...]
		vid, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil || vid <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version id"})
			return
		}

		sv, err := s.db.GetTFStateVersion(vid)
		if err != nil {
			if errors.Is(err, store.ErrTFStateVersionNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sv.WorkspaceID != ws.ID {
			http.NotFound(w, r)
			return
		}

		if len(rest) == 2 {
			// GET /api/terraform/workspaces/{id}/state/versions/{vid}
			// Raw JSON attachment
			if r.Method != http.MethodGet {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			filename := fmt.Sprintf("%s-%d.tfstate", sanitizeFilename(ws.Name), sv.Serial)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(sv.State)
			return
		}

		if len(rest) == 3 && rest[2] == "rollback" {
			// POST /api/terraform/workspaces/{id}/state/versions/{vid}/rollback
			if r.Method != http.MethodPost {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}

			// Reject if workspace is locked
			freshWS, err := s.db.GetTFWorkspace(ws.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if freshWS.Locked {
				writeJSON(w, http.StatusLocked, map[string]string{"error": "workspace is locked"})
				return
			}

			latest, err := s.db.LatestTFStateVersion(ws.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var currentStateBytes []byte
			if latest != nil {
				currentStateBytes = latest.State
			} else {
				currentStateBytes = []byte("{}")
			}

			newBytes, err := terraform.RollbackState(sv.State, currentStateBytes)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("failed to rollback state: %v", err)})
				return
			}

			info, err := terraform.ParseState(newBytes)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("parsed rollback state invalid: %v", err)})
				return
			}

			sum := sha256.Sum256(newBytes)
			shaHex := hex.EncodeToString(sum[:])

			newSV := &store.TFStateVersion{
				WorkspaceID:   ws.ID,
				RunID:         0,
				Serial:        info.Serial,
				Lineage:       info.Lineage,
				Source:        "rollback",
				Size:          int64(len(newBytes)),
				ResourceCount: len(info.Resources),
				SHA256:        shaHex,
				State:         newBytes,
			}
			newID, err := s.db.CreateTFStateVersion(newSV)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			newSV.ID = newID

			freshWS.ResourceCount = newSV.ResourceCount
			if info.TFVersion != "" {
				freshWS.TFVersion = info.TFVersion
			}
			_ = s.db.UpdateTFWorkspace(freshWS)
			logger.Info("terraform", ws.Name, fmt.Sprintf("Rolled back state of workspace %s (new version %d)", ws.Name, newID))

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"version": newSV,
			})
			return
		}

		http.NotFound(w, r)

	case "diff":
		// GET /api/terraform/workspaces/{id}/state/diff?from=&to=
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")
		if fromStr == "" || toStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "both 'from' and 'to' version ids are required"})
			return
		}
		fromID, err1 := strconv.ParseInt(fromStr, 10, 64)
		toID, err2 := strconv.ParseInt(toStr, 10, 64)
		if err1 != nil || err2 != nil || fromID <= 0 || toID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'from' or 'to' version id"})
			return
		}

		fromSV, err := s.db.GetTFStateVersion(fromID)
		if err != nil {
			if errors.Is(err, store.ErrTFStateVersionNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		toSV, err := s.db.GetTFStateVersion(toID)
		if err != nil {
			if errors.Is(err, store.ErrTFStateVersionNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if fromSV.WorkspaceID != ws.ID || toSV.WorkspaceID != ws.ID {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cross-workspace state diff is rejected"})
			return
		}

		diff, err := terraform.DiffStates(fromSV.State, toSV.State)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("failed to diff states: %v", err)})
			return
		}
		writeJSON(w, http.StatusOK, diff)

	default:
		http.NotFound(w, r)
	}
}

// --- OUTPUTS ENDPOINTS ---

func (s *Server) handleTerraformWorkspaceOutputs(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) > 0 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	reveal := r.URL.Query().Get("reveal") == "1"

	latest, err := s.db.LatestTFStateVersion(ws.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	outList := make([]map[string]interface{}, 0)
	if latest != nil && len(latest.State) > 0 {
		info, err := terraform.ParseState(latest.State)
		if err == nil && info.Outputs != nil {
			for _, o := range info.Outputs {
				item := map[string]interface{}{
					"name":      o.Name,
					"type":      o.Type,
					"sensitive": o.Sensitive,
				}
				if o.Sensitive && !reveal {
					item["value"] = nil
					item["value_hidden"] = true
				} else {
					item["value"] = o.Value
					if o.Sensitive {
						item["value_hidden"] = false
					}
				}
				outList = append(outList, item)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"outputs": outList,
	})
}

// --- WORKSPACE VARIABLES ENDPOINTS ---

func (s *Server) handleTerraformWorkspaceVariables(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("effective") == "1" {
				vars, err := s.db.EffectiveTFVariables(ws.ID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"variables": sanitizeVariableList(vars),
				})
				return
			}

			vars, err := s.db.ListTFVariables(ws.ID, 0)
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
				WorkspaceID: ws.ID,
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
			logger.Info("terraform", ws.Name, fmt.Sprintf("Created variable %s in workspace %s", v.Key, ws.Name))
			writeJSON(w, http.StatusCreated, sanitizeVariable(*v))

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	// Variables with {vid}
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
	if existing.WorkspaceID != ws.ID {
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

		// Sensitive preservation: if sensitive and value is empty, store.UpdateTFVariable handles it,
		// or if req.Value is provided, update it.
		existing.Sensitive = req.Sensitive
		if req.Value != "" || !req.Sensitive {
			existing.Value = req.Value
		}
		existing.Description = req.Description

		if err := s.db.UpdateTFVariable(existing); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// Reload fresh
		updated, _ := s.db.GetTFVariable(vid)
		if updated == nil {
			updated = existing
		}
		logger.Info("terraform", ws.Name, fmt.Sprintf("Updated variable %s in workspace %s", updated.Key, ws.Name))
		writeJSON(w, http.StatusOK, sanitizeVariable(*updated))

	case http.MethodDelete:
		if err := s.db.DeleteTFVariable(vid); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", ws.Name, fmt.Sprintf("Deleted variable %s from workspace %s", existing.Key, ws.Name))
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
