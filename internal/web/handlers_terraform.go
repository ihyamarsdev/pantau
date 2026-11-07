package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

// checkTerraformGate verifies that the terraform gate is enabled in store.
// If disabled, it responds with 403 {"error":"terraform is disabled"} per spec.
func (s *Server) checkTerraformGate(w http.ResponseWriter) bool {
	if s.db == nil || !s.db.GetTerraformEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "terraform is disabled"})
		return false
	}
	return true
}

func decodeTerraformBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

// formatWorkspaceResponse formats store.TFWorkspace into the spec WS JSON shape.
func (s *Server) formatWorkspaceResponse(ws *store.TFWorkspace, includeFiles bool) map[string]interface{} {
	var tags []string
	if ws.Tags != "" && ws.Tags != "[]" {
		_ = json.Unmarshal([]byte(ws.Tags), &tags)
	}
	if tags == nil {
		tags = []string{}
	}

	webhookURL := ""
	if ws.WebhookToken != "" {
		webhookURL = fmt.Sprintf("/api/terraform/webhook/%s", ws.WebhookToken)
	}

	res := map[string]interface{}{
		"id":                     ws.ID,
		"name":                   ws.Name,
		"description":            ws.Description,
		"environment":            ws.Environment,
		"group_name":             ws.GroupName,
		"tags":                   tags,
		"binary":                 ws.Binary,
		"source":                 ws.Source,
		"git_url":                ws.GitURL,
		"git_branch":             ws.GitBranch,
		"git_path":               ws.GitPath,
		"git_auth":               ws.GitAuth,
		"git_token_set":          ws.GitTokenSet,
		"git_key_id":             ws.GitKeyID,
		"webhook_enabled":        ws.WebhookEnabled,
		"webhook_url":            webhookURL,
		"drift_interval_minutes": ws.DriftIntervalMinutes,
		"status":                 ws.Status,
		"locked":                 ws.Locked,
		"lock_reason":            ws.LockReason,
		"locked_by":              ws.LockedBy,
		"drift_detected":         ws.DriftDetected,
		"drift_checked_at":       ws.DriftCheckedAt,
		"tf_version":             ws.TFVersion,
		"resource_count":         ws.ResourceCount,
		"last_run":               ws.LastRun,
		"created_at":             ws.CreatedAt,
		"updated_at":             ws.UpdatedAt,
	}
	ids := ws.CloudAccountIDs
	if ids == nil {
		ids = []int64{}
	}
	res["cloud_account_ids"] = ids
	res["cloud_accounts"] = s.cloudAccountRefs(ids)

	if includeFiles {
		var files map[string]string
		if ws.Files != "" && ws.Files != "{}" {
			_ = json.Unmarshal([]byte(ws.Files), &files)
		}
		if files == nil {
			files = map[string]string{}
		}
		res["files"] = files
	}
	return res
}

// formatRunSummary formats a store.TFRun into the RunSummary JSON shape.
func formatRunSummary(r *store.TFRun) map[string]interface{} {
	var summary terraform.PlanSummary
	if r.PlanSummary != "" && r.PlanSummary != "{}" {
		_ = json.Unmarshal([]byte(r.PlanSummary), &summary)
	}

	var pol terraform.PolicyReport
	if r.Policy != "" && r.Policy != "{}" {
		_ = json.Unmarshal([]byte(r.Policy), &pol)
	}
	policyStatus := pol.Status
	if policyStatus == "" {
		policyStatus = "passed"
	}

	var cost terraform.CostEstimate
	if r.Cost != "" && r.Cost != "{}" {
		_ = json.Unmarshal([]byte(r.Cost), &cost)
	}

	return map[string]interface{}{
		"id":           r.ID,
		"workspace_id": r.WorkspaceID,
		"type":         r.Type,
		"speculative":  r.Speculative,
		"status":       r.Status,
		"trigger_kind": r.TriggerKind,
		"message":      r.Message,
		"has_changes":  r.HasChanges,
		"counts": map[string]int{
			"create":  summary.Create,
			"update":  summary.Update,
			"delete":  summary.Delete,
			"replace": summary.Replace,
		},
		"policy_status": policyStatus,
		"cost_delta":    cost.Delta,
		"created_at":    r.CreatedAt,
		"started_at":    r.StartedAt,
		"finished_at":   r.FinishedAt,
	}
}

// formatRunDetail formats a store.TFRun into the full Run JSON shape.
func formatRunDetail(r *store.TFRun) map[string]interface{} {
	base := formatRunSummary(r)

	var targets, replace []string
	if r.Targets != "" && r.Targets != "[]" {
		_ = json.Unmarshal([]byte(r.Targets), &targets)
	}
	if targets == nil {
		targets = []string{}
	}
	if r.Replace != "" && r.Replace != "[]" {
		_ = json.Unmarshal([]byte(r.Replace), &replace)
	}
	if replace == nil {
		replace = []string{}
	}

	var planSummary terraform.PlanSummary
	if r.PlanSummary != "" && r.PlanSummary != "{}" {
		_ = json.Unmarshal([]byte(r.PlanSummary), &planSummary)
	}

	var drift []terraform.ResourceChange
	if r.Drift != "" && r.Drift != "[]" {
		_ = json.Unmarshal([]byte(r.Drift), &drift)
	}
	if drift == nil {
		drift = []terraform.ResourceChange{}
	}

	var pol terraform.PolicyReport
	if r.Policy != "" && r.Policy != "{}" {
		_ = json.Unmarshal([]byte(r.Policy), &pol)
	}

	var cost terraform.CostEstimate
	if r.Cost != "" && r.Cost != "{}" {
		_ = json.Unmarshal([]byte(r.Cost), &cost)
	}

	base["commit_sha"] = r.CommitSHA
	base["targets"] = targets
	base["replace"] = replace
	base["plan"] = planSummary
	base["drift"] = drift
	base["policy"] = pol
	base["cost"] = cost
	base["exit_code"] = r.ExitCode
	base["decision"] = r.Decision
	base["decision_comment"] = r.DecisionComment
	return base
}

// handleTerraformStatus:
// GET /api/terraform/status -> {enabled, supported, binaries:[{name,path,version,err}], infracost:{available,version,err}}
// PUT /api/terraform/status -> {enabled} -> status
func (s *Server) handleTerraformStatus(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeTerraformStatusJSON(w)

	case http.MethodPut:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}

		if err := s.db.SetTerraformEnabled(req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("terraform", "", fmt.Sprintf("Terraform gate updated: enabled=%v", req.Enabled))
		s.writeTerraformStatusJSON(w)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeTerraformStatusJSON(w http.ResponseWriter) {
	bins := terraform.Detect()
	supported := false
	for _, b := range bins {
		if b.Err == "" {
			supported = true
			break
		}
	}

	costPath, costVer, costErr := terraform.Infracost()
	costErrStr := ""
	if costErr != nil {
		costErrStr = costErr.Error()
	}
	_ = costPath

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":   s.db.GetTerraformEnabled(),
		"supported": supported,
		"binaries":  bins,
		"infracost": map[string]interface{}{
			"available": costErr == nil,
			"version":   costVer,
			"error":     costErrStr,
		},
	})
}

// handleTerraformWorkspaces:
// GET  /api/terraform/workspaces?env=&tag=&q= -> {workspaces:[WS]}
// POST /api/terraform/workspaces -> {workspace:WS} (201)
func (s *Server) handleTerraformWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		env := q.Get("env")
		tag := q.Get("tag")
		query := q.Get("q")

		workspaces, err := s.db.ListTFWorkspaces(env, tag, query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if group := strings.TrimSpace(q.Get("group")); group != "" {
			kept := workspaces[:0]
			for _, ws := range workspaces {
				if strings.EqualFold(ws.GroupName, group) {
					kept = append(kept, ws)
				}
			}
			workspaces = kept
		}

		formatted := make([]map[string]interface{}, 0, len(workspaces))
		for i := range workspaces {
			formatted = append(formatted, s.formatWorkspaceResponse(&workspaces[i], false))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"workspaces": formatted,
		})

	case http.MethodPost:
		var req struct {
			Name                 string            `json:"name"`
			Description          string            `json:"description"`
			GroupName            string            `json:"group_name"`
			Environment          string            `json:"environment"`
			Tags                 []string          `json:"tags"`
			Binary               string            `json:"binary"`
			Source               string            `json:"source"`
			Files                map[string]string `json:"files"`
			GitURL               string            `json:"git_url"`
			GitBranch            string            `json:"git_branch"`
			GitPath              string            `json:"git_path"`
			GitAuth              string            `json:"git_auth"`
			GitToken             string            `json:"git_token"`
			GitKeyID             int64             `json:"git_key_id"`
			DriftIntervalMinutes int               `json:"drift_interval_minutes"`
			CloudAccountIDs      []int64           `json:"cloud_account_ids"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}
		if err := s.checkCloudAccountIDs(req.CloudAccountIDs); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		if req.Binary == "" {
			req.Binary = "terraform"
		}
		if req.Source == "" {
			req.Source = "inline"
		}

		filesJSON := "{}"
		if req.Source == "inline" {
			if req.Files == nil {
				req.Files = map[string]string{}
			}
			if err := terraform.ValidateFiles(req.Files); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			b, _ := json.Marshal(req.Files)
			filesJSON = string(b)
		} else if req.Source == "git" {
			if err := ansible.ValidateGitURL(req.GitURL); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid git url: %v", err)})
				return
			}
			if req.GitBranch != "" {
				if err := ansible.ValidateGitBranch(req.GitBranch); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid git branch: %v", err)})
					return
				}
			}
		}

		tagsJSON := "[]"
		if len(req.Tags) > 0 {
			b, _ := json.Marshal(req.Tags)
			tagsJSON = string(b)
		}

		groupName, gerr := cleanGroupName(req.GroupName)
		if gerr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": gerr.Error()})
			return
		}
		ws := &store.TFWorkspace{
			Name:                 req.Name,
			Description:          req.Description,
			Environment:          req.Environment,
			GroupName:            groupName,
			Tags:                 tagsJSON,
			Binary:               req.Binary,
			Source:               req.Source,
			Files:                filesJSON,
			GitURL:               req.GitURL,
			GitBranch:            req.GitBranch,
			GitPath:              req.GitPath,
			GitAuth:              req.GitAuth,
			GitToken:             req.GitToken,
			GitKeyID:             req.GitKeyID,
			DriftIntervalMinutes: req.DriftIntervalMinutes,
			CloudAccountIDs:      req.CloudAccountIDs,
		}

		id, err := s.db.CreateTFWorkspace(ws)
		if err != nil {
			if errors.Is(err, store.ErrTFWorkspaceNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		created, err := s.db.GetTFWorkspace(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("terraform", created.Name, fmt.Sprintf("Created workspace %s (id %d)", created.Name, id))
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"workspace": s.formatWorkspaceResponse(created, true),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTerraformWorkspaceRoute handles /api/terraform/workspaces/{id} and all sub-routes.
func (s *Server) handleTerraformWorkspaceRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/api/terraform/workspaces/")
	tail = strings.TrimSuffix(tail, "/")
	parts := strings.Split(tail, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	ws, err := s.db.GetTFWorkspace(id)
	if err != nil {
		if errors.Is(err, store.ErrTFWorkspaceNotFound) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rest := parts[1:]
	if len(rest) == 0 {
		s.handleWorkspaceDetail(w, r, ws)
		return
	}

	// Delegation to TfWebData for {state, outputs, variables}
	switch rest[0] {
	case "state", "outputs", "variables":
		s.handleTerraformWorkspaceData(w, r, ws, rest)
		return
	}

	// Handled by TfWebCore
	switch rest[0] {
	case "vcs":
		if len(rest) >= 2 && rest[1] == "test" && r.Method == http.MethodPost {
			s.handleWorkspaceVCSTest(w, r, ws)
			return
		}
	case "webhook":
		if len(rest) >= 2 && rest[1] == "rotate" && r.Method == http.MethodPost {
			s.handleWorkspaceWebhookRotate(w, r, ws)
			return
		}
	case "runs":
		s.handleWorkspaceRuns(w, r, ws)
		return
	case "lock":
		if r.Method == http.MethodPost {
			s.handleWorkspaceLock(w, r, ws)
			return
		}
	case "unlock":
		if r.Method == http.MethodPost {
			s.handleWorkspaceUnlock(w, r, ws)
			return
		}
	case "force-unlock":
		if r.Method == http.MethodPost {
			s.handleWorkspaceForceUnlock(w, r, ws)
			return
		}
	case "drift":
		s.handleWorkspaceDrift(w, r, ws, rest[1:])
		return
	case "git":
		if len(rest) >= 2 {
			switch rest[1] {
			case "files":
				s.handleTerraformWorkspaceGitFiles(w, r, ws)
				return
			case "file":
				s.handleTerraformWorkspaceGitFile(w, r, ws)
				return
			case "commit":
				s.handleTerraformWorkspaceGitCommit(w, r, ws)
				return
			}
		}
	}

	http.NotFound(w, r)
}

func (s *Server) handleWorkspaceDetail(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"workspace": s.formatWorkspaceResponse(ws, true),
		})

	case http.MethodPut:
		var req struct {
			Name                 string            `json:"name"`
			Description          string            `json:"description"`
			Environment          string            `json:"environment"`
			GroupName            *string           `json:"group_name"` // nil keeps the stored group
			Tags                 []string          `json:"tags"`
			Binary               string            `json:"binary"`
			Source               string            `json:"source"`
			Files                map[string]string `json:"files"`
			GitURL               string            `json:"git_url"`
			GitBranch            string            `json:"git_branch"`
			GitPath              string            `json:"git_path"`
			GitAuth              string            `json:"git_auth"`
			GitToken             string            `json:"git_token"`
			GitKeyID             int64             `json:"git_key_id"`
			DriftIntervalMinutes int               `json:"drift_interval_minutes"`
			CloudAccountIDs      *[]int64          `json:"cloud_account_ids"` // nil keeps the stored links
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}
		if req.CloudAccountIDs != nil {
			if err := s.checkCloudAccountIDs(*req.CloudAccountIDs); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			ws.CloudAccountIDs = *req.CloudAccountIDs
		}

		if req.Name != "" {
			ws.Name = strings.TrimSpace(req.Name)
		}
		ws.Description = req.Description
		ws.Environment = req.Environment
		if req.GroupName != nil {
			g, gerr := cleanGroupName(*req.GroupName)
			if gerr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": gerr.Error()})
				return
			}
			ws.GroupName = g
		}
		if req.Binary != "" {
			ws.Binary = req.Binary
		}
		if req.Source != "" {
			ws.Source = req.Source
		}

		if req.Tags != nil {
			b, _ := json.Marshal(req.Tags)
			ws.Tags = string(b)
		}

		if ws.Source == "inline" {
			if req.Files != nil {
				if err := terraform.ValidateFiles(req.Files); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				b, _ := json.Marshal(req.Files)
				ws.Files = string(b)
			}
		} else if ws.Source == "git" {
			if req.GitURL != "" {
				if err := ansible.ValidateGitURL(req.GitURL); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid git url: %v", err)})
					return
				}
				ws.GitURL = req.GitURL
			}
			if req.GitBranch != "" {
				if err := ansible.ValidateGitBranch(req.GitBranch); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid git branch: %v", err)})
					return
				}
				ws.GitBranch = req.GitBranch
			}
			ws.GitPath = req.GitPath
			ws.GitAuth = req.GitAuth
			if req.GitToken != "" {
				ws.GitToken = req.GitToken
			}
			ws.GitKeyID = req.GitKeyID
		}

		ws.DriftIntervalMinutes = req.DriftIntervalMinutes

		if err := s.db.UpdateTFWorkspace(ws); err != nil {
			if errors.Is(err, store.ErrTFWorkspaceNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		updated, err := s.db.GetTFWorkspace(ws.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", ws.Name, fmt.Sprintf("Updated workspace %s (id %d)", ws.Name, ws.ID))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"workspace": s.formatWorkspaceResponse(updated, true),
		})

	case http.MethodDelete:
		if s.tfMgr != nil && s.tfMgr.HasActiveRunForWorkspace(ws.ID) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot delete workspace with an active run"})
			return
		}

		if err := s.db.DeleteTFWorkspace(ws.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Clean up on-disk workspace data dir
		if s.tfMgr != nil {
			wsDir := filepath.Join(s.tfMgr.dataRoot, strconv.FormatInt(ws.ID, 10))
			_ = os.RemoveAll(wsDir)
		}

		logger.Info("terraform", ws.Name, fmt.Sprintf("Deleted workspace %s (id %d)", ws.Name, ws.ID))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWorkspaceVCSTest(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if ws.Source != "git" || ws.GitURL == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":    false,
			"error": "workspace is not configured for git source",
		})
		return
	}

	gitBin, err := exec.LookPath("git")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":    false,
			"error": "git binary not found in PATH",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https:ssh",
	}

	sshCmd := "ssh -F /dev/null -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes"
	if ws.GitAuth == "ssh" && ws.GitKeyID > 0 {
		key, _ := s.db.GetSSHKey(ws.GitKeyID)
		if key != nil && strings.TrimSpace(key.PrivateKey) != "" {
			tmpDir, err := os.MkdirTemp("", "pantau-git-test-*")
			if err == nil {
				defer os.RemoveAll(tmpDir)
				kp := filepath.Join(tmpDir, "key")
				if err := os.WriteFile(kp, []byte(strings.TrimSpace(key.PrivateKey)+"\n"), 0600); err == nil {
					sshCmd += " -i '" + kp + "'"
				}
			}
		}
	}
	env = append(env, "GIT_SSH_COMMAND="+sshCmd)

	if ws.GitAuth == "token" && ws.GitToken != "" {
		u, perr := url.Parse(ws.GitURL)
		if perr == nil && u.Scheme == "https" {
			cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + ws.GitToken))
			env = append(env, "GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.https://"+u.Host+"/.extraHeader",
				"GIT_CONFIG_VALUE_0=Authorization: Basic "+cred)
		}
	}

	args := []string{"-c", "credential.helper=", "ls-remote", ws.GitURL}
	branch := ws.GitBranch
	if branch != "" {
		args = append(args, branch, "refs/heads/"+branch)
	}

	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(errBuf.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":     false,
			"branch": branch,
			"error":  errMsg,
		})
		return
	}

	commit := ""
	lines := strings.Split(outBuf.String(), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			commit = fields[0]
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":     true,
		"commit": commit,
		"branch": branch,
	})
}

func (s *Server) handleWorkspaceWebhookRotate(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	token, secret, err := s.db.RotateTFWorkspaceWebhook(ws.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"webhook_url":    fmt.Sprintf("/api/terraform/webhook/%s", token),
		"webhook_secret": secret, // Shown only once upon rotation
	})
}

func (s *Server) handleWorkspaceRuns(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	switch r.Method {
	case http.MethodGet:
		limit := 50
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}

		runs, err := s.db.ListTFRuns(ws.ID, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		formatted := make([]map[string]interface{}, 0, len(runs))
		for i := range runs {
			formatted = append(formatted, formatRunSummary(&runs[i]))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"runs": formatted,
		})

	case http.MethodPost:
		if s.tfMgr == nil {
			http.Error(w, "terraform run manager not initialized", http.StatusInternalServerError)
			return
		}

		// 1. Check workspace locked
		if ws.Locked {
			writeJSON(w, 423, map[string]string{"error": "workspace is locked: " + ws.LockReason})
			return
		}

		// 2. Check active runs
		if status, err := s.tfMgr.CanStartRun(ws.ID); err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}

		var req struct {
			Type        string   `json:"type"` // "plan" | "destroy" | "refresh"
			Speculative bool     `json:"speculative"`
			Message     string   `json:"message"`
			Targets     []string `json:"targets"`
			Replace     []string `json:"replace"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}

		if req.Type == "" {
			req.Type = "plan"
		}
		if req.Type != "plan" && req.Type != "destroy" && req.Type != "refresh" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid run type, must be plan, destroy, or refresh"})
			return
		}

		targetsJSON := "[]"
		if len(req.Targets) > 0 {
			b, _ := json.Marshal(req.Targets)
			targetsJSON = string(b)
		}
		replaceJSON := "[]"
		if len(req.Replace) > 0 {
			b, _ := json.Marshal(req.Replace)
			replaceJSON = string(b)
		}

		// Atomically try to lock workspace
		locked, err := s.db.TryLockTFWorkspace(ws.ID, "run:pending", "starting run")
		if err != nil || !locked {
			writeJSON(w, 423, map[string]string{"error": "workspace is locked"})
			return
		}

		run := &store.TFRun{
			WorkspaceID: ws.ID,
			Type:        req.Type,
			Speculative: req.Speculative,
			Status:      "queued",
			TriggerKind: "manual",
			Message:     req.Message,
			Targets:     targetsJSON,
			Replace:     replaceJSON,
		}

		runID, err := s.db.CreateTFRun(run)
		if err != nil {
			_ = s.db.UnlockTFWorkspace(ws.ID)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		run.ID = runID

		// Update lock with actual run ID
		_ = s.db.SetTFWorkspaceLock(ws.ID, fmt.Sprintf("run:%d", runID), "active run")

		// Launch in background
		if err := s.tfMgr.StartPlan(ws, run); err != nil {
			_ = s.db.FinishTFRun(runID, "errored", nil, err.Error())
			_ = s.db.UnlockTFWorkspace(ws.ID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		fullRun, err := s.db.GetTFRun(runID)
		if err != nil {
			fullRun = run
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"run": formatRunDetail(fullRun),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWorkspaceLock(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeTerraformBody(w, r, &req)

	if ws.Locked {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "workspace is already locked"})
		return
	}

	locked, err := s.db.TryLockTFWorkspace(ws.ID, "manual", req.Reason)
	if err != nil || !locked {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "failed to acquire lock"})
		return
	}

	updated, _ := s.db.GetTFWorkspace(ws.ID)
	logger.Info("terraform", ws.Name, fmt.Sprintf("Locked workspace %s: %s", ws.Name, req.Reason))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"workspace": s.formatWorkspaceResponse(updated, false),
	})
}

func (s *Server) handleWorkspaceUnlock(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if !ws.Locked {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"workspace": s.formatWorkspaceResponse(ws, false),
		})
		return
	}

	if ws.LockedBy != "manual" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot unlock workspace locked by active run: use force-unlock"})
		return
	}

	if err := s.db.UnlockTFWorkspace(ws.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updated, _ := s.db.GetTFWorkspace(ws.ID)
	logger.Info("terraform", ws.Name, fmt.Sprintf("Unlocked workspace %s", ws.Name))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"workspace": s.formatWorkspaceResponse(updated, false),
	})
}

func (s *Server) handleWorkspaceForceUnlock(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	var req struct {
		Comment string `json:"comment"`
	}
	_ = decodeTerraformBody(w, r, &req)

	if s.tfMgr != nil {
		s.tfMgr.ForceUnlockWorkspace(ws.ID)
	} else {
		_ = s.db.UnlockTFWorkspace(ws.ID)
	}

	updated, _ := s.db.GetTFWorkspace(ws.ID)
	logger.Warn("terraform", ws.Name, fmt.Sprintf("Force-unlocked workspace %s: %s", ws.Name, req.Comment))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"workspace": s.formatWorkspaceResponse(updated, false),
	})
}

func (s *Server) handleWorkspaceDrift(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		// GET /api/terraform/workspaces/{id}/drift -> {detected, checked_at, interval_minutes, resources:[ResourceChange]}
		// Latest drift is stored in the last drift run or latest run
		var resources []terraform.ResourceChange
		runs, err := s.db.ListTFRuns(ws.ID, 5)
		if err == nil {
			for _, run := range runs {
				if run.Drift != "" && run.Drift != "[]" {
					_ = json.Unmarshal([]byte(run.Drift), &resources)
					if len(resources) > 0 {
						break
					}
				}
			}
		}
		if resources == nil {
			resources = []terraform.ResourceChange{}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"detected":         ws.DriftDetected,
			"checked_at":       ws.DriftCheckedAt,
			"interval_minutes": ws.DriftIntervalMinutes,
			"resources":        resources,
		})
		return
	}

	if len(rest) == 1 && rest[0] == "check" && r.Method == http.MethodPost {
		// POST /api/terraform/workspaces/{id}/drift/check -> triggers drift run
		if s.tfMgr == nil {
			http.Error(w, "terraform run manager not initialized", http.StatusInternalServerError)
			return
		}

		if ws.Locked {
			writeJSON(w, 423, map[string]string{"error": "workspace is locked: " + ws.LockReason})
			return
		}

		if status, err := s.tfMgr.CanStartRun(ws.ID); err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}

		locked, err := s.db.TryLockTFWorkspace(ws.ID, "run:pending", "manual drift check")
		if err != nil || !locked {
			writeJSON(w, 423, map[string]string{"error": "workspace is locked"})
			return
		}

		run := &store.TFRun{
			WorkspaceID: ws.ID,
			Type:        "drift",
			Speculative: true,
			Status:      "queued",
			TriggerKind: "manual",
			Message:     "manual drift detection",
		}
		runID, err := s.db.CreateTFRun(run)
		if err != nil {
			_ = s.db.UnlockTFWorkspace(ws.ID)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		run.ID = runID
		_ = s.db.SetTFWorkspaceLock(ws.ID, fmt.Sprintf("run:%d", runID), "drift check")

		if err := s.tfMgr.StartPlan(ws, run); err != nil {
			_ = s.db.FinishTFRun(runID, "errored", nil, err.Error())
			_ = s.db.UnlockTFWorkspace(ws.ID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		fullRun, err := s.db.GetTFRun(runID)
		if err != nil {
			fullRun = run
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"run": formatRunDetail(fullRun),
		})
		return
	}

	http.NotFound(w, r)
}

// handleTerraformRunRoute handles /api/terraform/runs/{id} and actions {log, apply, discard, cancel}.
func (s *Server) handleTerraformRunRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/api/terraform/runs/")
	tail = strings.TrimSuffix(tail, "/")
	parts := strings.Split(tail, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	runID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || runID <= 0 {
		http.NotFound(w, r)
		return
	}

	run, err := s.db.GetTFRun(runID)
	if err != nil {
		if errors.Is(err, store.ErrTFRunNotFound) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"run": formatRunDetail(run),
			})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch parts[1] {
	case "log":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		offset := 0
		if offStr := r.URL.Query().Get("offset"); offStr != "" {
			if n, err := strconv.Atoi(offStr); err == nil && n >= 0 {
				offset = n
			}
		}

		data, nextOffset, status, finished, err := s.tfMgr.ReadLog(runID, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"log":         data,
			"next_offset": nextOffset,
			"done":        finished,
			"status":      status,
		})

	case "apply":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if run.Status != "awaiting_approval" {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": fmt.Sprintf("cannot apply run in status %q, must be awaiting_approval", run.Status),
			})
			return
		}

		var req struct {
			Comment        string `json:"comment"`
			OverridePolicy bool   `json:"override_policy"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}

		req.Comment = strings.TrimSpace(req.Comment)
		if len(req.Comment) < 3 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "approval comment is required and must be at least 3 characters",
			})
			return
		}

		// Policy check validation
		var pol terraform.PolicyReport
		if run.Policy != "" && run.Policy != "{}" {
			_ = json.Unmarshal([]byte(run.Policy), &pol)
		}
		if pol.Status == "failed" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "cannot apply: hard policy check failed",
			})
			return
		}
		if pol.Status == "warning" && !req.OverridePolicy {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "policy check has warnings: override_policy is required to apply",
			})
			return
		}

		ws, err := s.db.GetTFWorkspace(run.WorkspaceID)
		if err != nil {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}

		if err := s.tfMgr.StartApply(ws, run, req.Comment); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}

		updatedRun, _ := s.db.GetTFRun(run.ID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"run": formatRunDetail(updatedRun),
		})

	case "discard":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if run.Status != "awaiting_approval" {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": fmt.Sprintf("cannot discard run in status %q, must be awaiting_approval", run.Status),
			})
			return
		}

		var req struct {
			Comment string `json:"comment"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}

		req.Comment = strings.TrimSpace(req.Comment)
		if req.Comment == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "discard comment is required",
			})
			return
		}

		if err := s.tfMgr.DiscardRun(run.WorkspaceID, run.ID, req.Comment); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		updatedRun, _ := s.db.GetTFRun(run.ID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"run": formatRunDetail(updatedRun),
		})

	case "cancel":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s.tfMgr.CancelRun(run.ID)
		updatedRun, _ := s.db.GetTFRun(run.ID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"run": formatRunDetail(updatedRun),
		})

	default:
		http.NotFound(w, r)
	}
}
