package web

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

var (
	webhookRateMu     sync.Mutex
	webhookLastAccess = make(map[string]time.Time)
)

// handleTerraformWebhook handles public POST /api/terraform/webhook/{token}.
// Bypasses authMiddleware and 2FA.
func (s *Server) handleTerraformWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/api/terraform/webhook/")
	token = strings.TrimSpace(token)
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	// Simple per-token rate limit: max 1 request every 2 seconds
	webhookRateMu.Lock()
	last := webhookLastAccess[token]
	if time.Since(last) < 2*time.Second {
		webhookRateMu.Unlock()
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	webhookLastAccess[token] = time.Now()
	// Clean up stale map entries if large
	if len(webhookLastAccess) > 1000 {
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, t := range webhookLastAccess {
			if t.Before(cutoff) {
				delete(webhookLastAccess, k)
			}
		}
	}
	webhookRateMu.Unlock()

	// Cap body at 1 MiB
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "request entity too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Find workspace by webhook token
	workspaces, err := s.db.ListTFWorkspaces("", "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var targetWS *store.TFWorkspace
	for _, ws := range workspaces {
		if subtle.ConstantTimeCompare([]byte(ws.WebhookToken), []byte(token)) == 1 {
			targetWS = &ws
			break
		}
	}

	if targetWS == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Load full workspace (including files/git secrets and WebhookSecret)
	fullWS, err := s.db.GetTFWorkspace(targetWS.ID)
	if err != nil {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}

	// Parse webhook payload and verify signature/secret
	branch, err := terraform.ParseWebhook(r.Header, body, fullWS.WebhookSecret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// If workspace is git-backed, verify branch matches
	if fullWS.Source == "git" && fullWS.GitBranch != "" {
		if branch != fullWS.GitBranch {
			writeJSON(w, http.StatusOK, map[string]string{
				"ignored": fmt.Sprintf("branch %s does not match %s", branch, fullWS.GitBranch),
			})
			return
		}
	}

	// Trigger plan run
	if s.tfMgr == nil {
		http.Error(w, "terraform run manager not initialized", http.StatusInternalServerError)
		return
	}

	// Check concurrency
	if status, err := s.tfMgr.CanStartRun(fullWS.ID); err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	// Lock workspace
	locked, err := s.db.TryLockTFWorkspace(fullWS.ID, "run:pending", "webhook triggered run")
	if err != nil || !locked {
		writeJSON(w, 423, map[string]string{"error": "workspace is locked"})
		return
	}

	run := &store.TFRun{
		WorkspaceID: fullWS.ID,
		Type:        "plan",
		Status:      "queued",
		TriggerKind: "webhook",
		Message:     fmt.Sprintf("Webhook push to branch %s", branch),
	}
	runID, err := s.db.CreateTFRun(run)
	if err != nil {
		_ = s.db.UnlockTFWorkspace(fullWS.ID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	run.ID = runID

	_ = s.db.SetTFWorkspaceLock(fullWS.ID, fmt.Sprintf("run:%d", runID), "webhook run")

	if err := s.tfMgr.StartPlan(fullWS, run); err != nil {
		_ = s.db.FinishTFRun(runID, "errored", nil, err.Error())
		_ = s.db.UnlockTFWorkspace(fullWS.ID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("terraform", fullWS.Name, fmt.Sprintf("Webhook triggered plan run %d", runID))
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"run_id": runID,
	})
}
