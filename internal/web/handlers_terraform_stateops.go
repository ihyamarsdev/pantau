package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

// handleWorkspaceStateOps serves POST /api/terraform/workspaces/{id}/state/ops:
// state rm / mv / import under the workspace lock, saved as a new state version.
func (s *Server) handleWorkspaceStateOps(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.tfMgr == nil {
		http.Error(w, "terraform run manager not initialized", http.StatusInternalServerError)
		return
	}
	var req struct {
		Op       string `json:"op"`
		Address  string `json:"address"`
		To       string `json:"to"`
		ImportID string `json:"import_id"`
	}
	if !decodeTerraformBody(w, r, &req) {
		return
	}
	spec := terraform.StateOpSpec{Op: req.Op, Address: req.Address, To: req.To, ImportID: req.ImportID}
	if err := spec.ValidateRequest(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Same guards as a run start: locked -> 423, active run / concurrency -> 409 / 429.
	if ws.Locked {
		writeJSON(w, 423, map[string]string{"error": "workspace is locked: " + ws.LockReason})
		return
	}
	if status, err := s.tfMgr.CanStartRun(ws.ID); err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	locked, err := s.db.TryLockTFWorkspace(ws.ID, "state-op", "state "+req.Op)
	if err != nil || !locked {
		writeJSON(w, 423, map[string]string{"error": "workspace is locked"})
		return
	}
	defer func() { _ = s.db.UnlockTFWorkspace(ws.ID) }()

	cfg, err := s.tfMgr.BuildRunConfig(ws)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	spec.Config = cfg

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	res, err := terraform.StateOp(ctx, spec)
	if err != nil {
		logger.Warn("terraform", ws.Name, fmt.Sprintf("Workspace %s: %s failed", ws.Name, spec.Description()))
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if res.ExitCode != 0 || len(res.State) == 0 {
		logger.Warn("terraform", ws.Name, fmt.Sprintf("Workspace %s: %s failed (exit %d)", ws.Name, spec.Description(), res.ExitCode))
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf("%s failed (exit %d)", spec.Description(), res.ExitCode), "output": res.Output,
		})
		return
	}

	sum := sha256.Sum256(res.State)
	sv := &store.TFStateVersion{
		WorkspaceID: ws.ID,
		Serial:      1,
		Source:      spec.Description(),
		Size:        int64(len(res.State)),
		SHA256:      hex.EncodeToString(sum[:]),
		State:       res.State,
	}
	if info, perr := terraform.ParseState(res.State); perr == nil {
		sv.Serial, sv.Lineage, sv.ResourceCount = info.Serial, info.Lineage, len(info.Resources)
	}
	if _, err := s.db.CreateTFStateVersion(sv); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "failed to save state: " + err.Error(), "output": res.Output})
		return
	}
	_ = s.db.PruneTFStateVersions(ws.ID, 100)
	logger.Info("terraform", ws.Name, fmt.Sprintf("Workspace %s: %s (state version %d)", ws.Name, spec.Description(), sv.ID))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": res.Output, "state_version": sv.ID})
}
