package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/inspector"
	"pantau/internal/terraform"
)

// agentPatchReq is the structured change proposal payload emitted by the AI Agent
// (via ```pantau-patch blocks) and submitted by the interactive diff/dry-run UI.
type agentPatchReq struct {
	Kind        string `json:"kind"` // sftp_file | k8s_yaml | ansible_playbook | terraform_hcl | docker_compose
	Title       string `json:"title"`
	HostID      int64  `json:"host_id"`
	ClusterID   int64  `json:"cluster_id"`
	PlaybookID  int64  `json:"playbook_id"`
	WorkspaceID int64  `json:"workspace_id"`
	StackName   string `json:"stack_name"`
	Target      string `json:"target"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

func decodeAgentPatch(w http.ResponseWriter, r *http.Request) (agentPatchReq, bool) {
	var req agentPatchReq
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return req, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON request"})
		return req, false
	}
	req.Kind = strings.TrimSpace(strings.ToLower(req.Kind))
	req.Target = strings.TrimSpace(req.Target)
	req.StackName = strings.TrimSpace(req.StackName)
	return req, true
}

func (req agentPatchReq) kubeTargetID() int64 {
	if req.ClusterID < 0 {
		return req.ClusterID
	}
	if req.ClusterID > 0 {
		return -req.ClusterID
	}
	return req.HostID
}

// handleAgentPatchPrepare resolves the live "before" content when the proposal omits it.
func (s *Server) handleAgentPatchPrepare(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeAgentPatch(w, r)
	if !ok {
		return
	}
	before := req.Before
	var warn string
	if before == "" {
		switch req.Kind {
		case "sftp_file":
			if req.HostID > 0 && req.Target != "" && !isProtectedPathString(req.Target) && !agentSecretPathRe.MatchString(path.Clean(req.Target)) {
				stdout, _, code, err := s.inspector.ExecShell(req.HostID, "cat -- "+shq(req.Target)+" 2>/dev/null || true", 10*time.Second)
				if err == nil && code == 0 {
					before = stdout
				}
			}
		case "ansible_playbook":
			if req.PlaybookID > 0 {
				if pb, err := s.db.GetAnsiblePlaybook(req.PlaybookID); err == nil && pb != nil {
					before = pb.Content
				}
			}
		case "terraform_hcl":
			if req.WorkspaceID > 0 && req.Target != "" {
				if ws, err := s.db.GetTFWorkspace(req.WorkspaceID); err == nil && ws != nil {
					files := map[string]string{}
					_ = json.Unmarshal([]byte(ws.Files), &files)
					if cur, exists := files[req.Target]; exists {
						before = cur
					}
				}
			}
		case "docker_compose":
			if req.HostID > 0 && req.StackName != "" && inspector.ValidDockerName(req.StackName) {
				if _, content, err := s.inspector.DockerStackFile(req.HostID, req.StackName); err == nil {
					before = content
				}
			}
		case "k8s_yaml":
			// K8s diff is computed server-side by kubectl diff during dry-run.
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("unsupported patch kind %q", req.Kind)})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"before":  before,
		"after":   req.After,
		"warning": warn,
	})
}

// handleAgentPatchDryRun validates the proposed change without mutating target state.
func (s *Server) handleAgentPatchDryRun(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeAgentPatch(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.After) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "proposed content (after) is empty"})
		return
	}

	switch req.Kind {
	case "k8s_yaml":
		tid := req.kubeTargetID()
		if tid == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "host_id or cluster_id is required for k8s_yaml"})
			return
		}
		if err := inspector.ValidateKubeApply(req.After); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "valid": false, "error": err.Error(), "command": "kubectl apply --dry-run=server"})
			return
		}
		out, changed, err := s.inspector.KubeDiff(tid, req.After)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "valid": false, "error": err.Error(), "output": out, "command": "kubectl apply --dry-run=server"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"valid":   true,
			"changed": changed,
			"output":  out,
			"command": "kubectl apply --dry-run=server && kubectl diff",
		})

	case "ansible_playbook":
		if _, _, err := ansible.Available(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "valid": false, "error": "ansible-playbook binary not available: " + err.Error(), "command": "ansible-playbook --syntax-check"})
			return
		}
		var buf bytes.Buffer
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		code, err := ansible.Run(ctx, ansible.RunSpec{
			Playbook: req.After,
			Mode:     "syntax",
		}, &buf)
		out := strings.TrimSpace(buf.String())
		if err != nil || code != 0 {
			errMsg := out
			if err != nil {
				errMsg = err.Error()
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "valid": false, "error": errMsg, "output": out, "command": "ansible-playbook --syntax-check"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"valid":   true,
			"output":  out,
			"command": "ansible-playbook --syntax-check",
		})

	case "terraform_hcl":
		if req.WorkspaceID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "workspace_id is required for terraform_hcl"})
			return
		}
		ws, err := s.db.GetTFWorkspace(req.WorkspaceID)
		if err != nil || ws == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "terraform workspace not found"})
			return
		}
		if ws.Source != "inline" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "only inline terraform workspaces support direct patch validation"})
			return
		}
		files := map[string]string{}
		if ws.Files != "" {
			_ = json.Unmarshal([]byte(ws.Files), &files)
		}
		target := req.Target
		if target == "" {
			target = "main.tf"
		}
		files[target] = req.After
		if err := terraform.ValidateFiles(files); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "valid": false, "error": err.Error(), "command": "terraform validate"})
			return
		}
		if s.tfMgr == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "terraform run manager not initialized"})
			return
		}
		bin := ws.Binary
		if bin == "" {
			bin = "terraform"
		}
		res, err := terraform.ValidateConfig(r.Context(), bin, s.tfMgr.dataRoot, files)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "valid": false, "error": err.Error(), "command": bin + " validate"})
			return
		}
		outBytes, _ := json.MarshalIndent(res, "", "  ")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      res.OK,
			"valid":   res.OK,
			"output":  string(outBytes),
			"command": bin + " validate",
		})

	case "sftp_file":
		if req.HostID <= 0 || req.Target == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "host_id and target path are required for sftp_file"})
			return
		}
		if isProtectedPathString(req.Target) || agentSecretPathRe.MatchString(path.Clean(req.Target)) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "valid": false, "error": "protected path: cannot modify system location"})
			return
		}
		if strings.HasSuffix(strings.ToLower(req.Target), ".json") {
			var js any
			if err := json.Unmarshal([]byte(req.After), &js); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "valid": false, "error": "invalid JSON syntax: " + err.Error(), "command": "json syntax check"})
				return
			}
		}
		dir := path.Dir(req.Target)
		stdout, stderr, code, err := s.inspector.ExecShell(req.HostID, "test -d "+shq(dir)+" && echo OK", 10*time.Second)
		if err != nil || code != 0 || !strings.Contains(stdout, "OK") {
			msg := strings.TrimSpace(stderr)
			if err != nil {
				msg = err.Error()
			}
			if msg == "" {
				msg = "target parent directory does not exist: " + dir
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "valid": false, "error": msg, "command": "sftp preflight check"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"valid":   true,
			"output":  fmt.Sprintf("Target directory %s exists (%d bytes ready to write)", dir, len(req.After)),
			"command": "sftp preflight check",
		})

	case "docker_compose":
		if req.HostID <= 0 || req.StackName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "host_id and stack_name are required for docker_compose"})
			return
		}
		if !inspector.ValidDockerName(req.StackName) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "valid": false, "error": "invalid stack name"})
			return
		}
		cmd := "d=docker; docker ps -q >/dev/null 2>&1 || d=\"sudo -n docker\"; printf '%s' " + shq(req.After) + " | $d compose -f - config -q 2>&1"
		stdout, stderr, code, err := s.inspector.ExecShell(req.HostID, cmd, 20*time.Second)
		out := strings.TrimSpace(stdout + "\n" + stderr)
		if err != nil || code != 0 {
			errMsg := out
			if err != nil && errMsg == "" {
				errMsg = err.Error()
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "valid": false, "error": errMsg, "output": out, "command": "docker compose config"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"valid":   true,
			"output":  "docker compose config: valid",
			"command": "docker compose config",
		})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("unsupported patch kind %q", req.Kind)})
	}
}

// handleAgentPatchApply applies a reviewed & validated patch to the target resource.
func (s *Server) handleAgentPatchApply(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeAgentPatch(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.After) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "proposed content (after) is empty"})
		return
	}

	switch req.Kind {
	case "k8s_yaml":
		tid := req.kubeTargetID()
		if tid == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "host_id or cluster_id is required for k8s_yaml"})
			return
		}
		if tid > 0 {
			if h, err := s.db.GetHost(tid); err == nil && h != nil && h.KubeMode == "read-only" {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "kubernetes target is in read-only mode"})
				return
			}
		}
		if err := inspector.ValidateKubeApply(req.After); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		out, err := s.inspector.KubeApply(tid, req.After)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "output": out})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})

	case "ansible_playbook":
		if req.PlaybookID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "playbook_id is required for ansible_playbook"})
			return
		}
		pb, err := s.db.GetAnsiblePlaybook(req.PlaybookID)
		if err != nil || pb == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "ansible playbook not found"})
			return
		}
		pb.Content = req.After
		if err := s.db.UpdateAnsiblePlaybook(pb); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": "Playbook updated: " + pb.Name})

	case "terraform_hcl":
		if req.WorkspaceID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "workspace_id is required for terraform_hcl"})
			return
		}
		ws, err := s.db.GetTFWorkspace(req.WorkspaceID)
		if err != nil || ws == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "terraform workspace not found"})
			return
		}
		if ws.Source != "inline" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "only inline terraform workspaces support direct patch apply"})
			return
		}
		files := map[string]string{}
		if ws.Files != "" {
			_ = json.Unmarshal([]byte(ws.Files), &files)
		}
		target := req.Target
		if target == "" {
			target = "main.tf"
		}
		files[target] = req.After
		if err := terraform.ValidateFiles(files); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		raw, _ := json.Marshal(files)
		ws.Files = string(raw)
		if err := s.db.UpdateTFWorkspace(ws); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": "Updated " + target + " in workspace " + ws.Name})

	case "sftp_file":
		if req.HostID <= 0 || req.Target == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "host_id and target path are required for sftp_file"})
			return
		}
		if isProtectedPathString(req.Target) || agentSecretPathRe.MatchString(path.Clean(req.Target)) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "protected path: cannot modify system location"})
			return
		}
		res, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/write", req.HostID),
			map[string]string{"path": req.Target, "content": req.After})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": "Saved " + req.Target, "result": res})

	case "docker_compose":
		if req.HostID <= 0 || req.StackName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "host_id and stack_name are required for docker_compose"})
			return
		}
		if !inspector.ValidDockerName(req.StackName) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid stack name"})
			return
		}
		out, err := s.inspector.DockerStackDeploy(req.HostID, req.StackName, req.After, nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "output": out})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("unsupported patch kind %q", req.Kind)})
	}
}
