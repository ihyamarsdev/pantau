package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"pantau/internal/store"
	"pantau/internal/terraform"
)

// tfValidateSem bounds concurrent `terraform validate` runs (each one runs init).
var tfValidateSem = make(chan struct{}, 2)

func tfJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleWorkspaceVarDetect: GET /api/terraform/workspaces/{id}/variables/detect
func (s *Server) handleWorkspaceVarDetect(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if r.Method != http.MethodGet {
		tfJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	type item struct {
		terraform.DeclaredVar
		Exists bool `json:"exists"`
	}
	vars := []item{}
	if ws.Source != "inline" {
		writeJSON(w, http.StatusOK, map[string]any{"supported": false, "variables": vars})
		return
	}
	files := map[string]string{}
	if ws.Files != "" {
		if err := json.Unmarshal([]byte(ws.Files), &files); err != nil {
			tfJSONError(w, http.StatusInternalServerError, "invalid workspace files")
			return
		}
	}
	stored, err := s.db.ListTFVariables(ws.ID, 0)
	if err != nil {
		tfJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	have := map[string]bool{}
	for _, v := range stored {
		if v.Category == "terraform" {
			have[v.Key] = true
		}
	}
	for _, d := range terraform.ParseVariables(files) {
		vars = append(vars, item{d, have[d.Name]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"supported": true, "variables": vars})
}

// handleWorkspaceTools: POST /api/terraform/workspaces/{id}/tools/{fmt|validate}
// The body carries the editor's current (possibly unsaved) files.
func (s *Server) handleWorkspaceTools(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) != 1 || (rest[0] != "fmt" && rest[0] != "validate") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		tfJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if ws.Source != "inline" {
		tfJSONError(w, http.StatusBadRequest, "only inline workspaces are supported")
		return
	}
	var req struct {
		Files map[string]string `json:"files"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		tfJSONError(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := terraform.ValidateFiles(req.Files); err != nil {
		tfJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.tfMgr == nil {
		tfJSONError(w, http.StatusInternalServerError, "terraform run manager not initialized")
		return
	}
	bin := ws.Binary
	if bin == "" {
		bin = "terraform"
	}

	var res any
	var err error
	if rest[0] == "fmt" {
		res, err = terraform.FormatFiles(r.Context(), bin, req.Files)
	} else {
		select {
		case tfValidateSem <- struct{}{}:
			defer func() { <-tfValidateSem }()
		default:
			tfJSONError(w, http.StatusTooManyRequests, "too many concurrent validations, retry shortly")
			return
		}
		res, err = terraform.ValidateConfig(r.Context(), bin, s.tfMgr.dataRoot, req.Files)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		tfJSONError(w, http.StatusGatewayTimeout, "timed out")
	case err != nil:
		tfJSONError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, res)
	}
}
