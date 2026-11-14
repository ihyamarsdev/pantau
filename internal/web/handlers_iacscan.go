package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/iacscan"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// IaC and secret scanning of the sources Pantau holds on its own disk: cloned Git repositories, Terraform
// workspaces and Ansible projects. The scanner is built in (internal/iacscan): no external binary, no network.
//
// Gate: with the iac_gate_enabled setting on, a Terraform apply and an Ansible run are refused (409) while the
// source still has a critical or high finding, unless the request asks to ignore the gate.

const (
	iacGateSetting = "iac_gate_enabled"

	iacSourceRepo      = "repo"
	iacSourceTerraform = "terraform"
	iacSourceAnsible   = "ansible"

	iacScanTimeout = 3 * time.Minute
)

// iacGeneratedFiles are Pantau's own files inside a Terraform work dir: they carry runtime variables and
// backend overrides, not the committed source, so they are not scanned.
var iacGeneratedFiles = []string{"pantau.auto.tfvars.json", "pantau-hcl.auto.tfvars", "pantau_backend_override.tf"}

// iacGateEnabled reports whether the gate is on.
func (s *Server) iacGateEnabled() bool {
	if s.db == nil {
		return false
	}
	v, _ := s.db.GetSetting(iacGateSetting)
	return v == "1" || v == "true"
}

// iacOptions are the scanner options Pantau uses.
func iacOptions() iacscan.Options {
	return iacscan.Options{SkipNames: iacGeneratedFiles}
}

// handleIacScan: GET /api/iac-scan (fleet summary), POST /api/iac-scan/{kind}/{id} (run), GET (latest).
func (s *Server) handleIacScan(w http.ResponseWriter, r *http.Request) {
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/iac-scan"), "/")
	if tail == "" {
		s.handleIacScanList(w, r)
		return
	}
	parts := strings.Split(tail, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	kind := parts[0]
	if kind != iacSourceRepo && kind != iacSourceTerraform && kind != iacSourceAnsible {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown source kind"})
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleIacScanLatest(w, r, kind, id)
	case http.MethodPost:
		s.handleIacScanRun(w, r, kind, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIacScanList: GET /api/iac-scan - the latest scan digest of every source.
func (s *Server) handleIacScanList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scans, err := s.db.ListIacScans()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(scans))
	for _, sc := range scans {
		out = append(out, iacScanDigest(&sc))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"scans": out, "gate_enabled": s.iacGateEnabled()})
}

// handleIacScanLatest: GET /api/iac-scan/{kind}/{id} - the stored result of the last scan.
func (s *Server) handleIacScanLatest(w http.ResponseWriter, r *http.Request, kind string, id int64) {
	sc, err := s.db.GetIacScan(kind, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no scan yet for this source"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"scan": iacScanPayload(sc)})
}

// handleIacScanRun: POST /api/iac-scan/{kind}/{id} - scan the source now and store the result.
func (s *Server) handleIacScanRun(w http.ResponseWriter, r *http.Request, kind string, id int64) {
	ctx, cancel := context.WithTimeout(r.Context(), iacScanTimeout)
	defer cancel()

	root, cleanup, err := s.iacSourceRoot(ctx, kind, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer cleanup()

	res := iacscan.ScanDir(root, iacOptions())
	if res.Status == "not_assessed" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"scan": iacResultPayload(&res), "scanned": false})
		return
	}
	if err := s.saveIacScan(kind, id, &res); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	logger.Info("iacscan", kind, fmt.Sprintf("Scanned source %d: %d finding(s)", id, res.Summary.Total))
	writeJSON(w, http.StatusOK, map[string]interface{}{"scan": iacResultPayload(&res), "scanned": true})
}

// saveIacScan stores the latest result of a source.
func (s *Server) saveIacScan(kind string, id int64, res *iacscan.Result) error {
	summary, err := json.Marshal(res.Summary)
	if err != nil {
		return err
	}
	findings, err := json.Marshal(res.Findings)
	if err != nil {
		return err
	}
	notes, err := json.Marshal(res.Notes)
	if err != nil {
		return err
	}
	return s.db.SaveIacScan(&store.IacScan{
		SourceKind: kind,
		SourceID:   id,
		ScannedAt:  res.ScannedAt,
		Summary:    summary,
		Findings:   findings,
		Status:     res.Status,
		Files:      res.FilesScanned,
		Truncated:  res.Truncated,
		Root:       res.Root,
		Notes:      notes,
	})
}

// iacSourceRoot resolves the directory to scan for a source. The cleanup function removes a temporary
// directory when the source had to be materialized (inline Terraform files, an Ansible playbook, a Git clone).
func (s *Server) iacSourceRoot(ctx context.Context, kind string, id int64) (string, func(), error) {
	noop := func() {}
	switch kind {
	case iacSourceRepo:
		repo, err := s.db.GetGitRepo(id)
		if err != nil {
			return "", noop, err
		}
		if repo == nil {
			return "", noop, errors.New("repository not found")
		}
		return s.iacRepoRoot(repo)
	case iacSourceTerraform:
		ws, err := s.db.GetTFWorkspace(id)
		if err != nil {
			return "", noop, err
		}
		if ws == nil {
			return "", noop, errors.New("workspace not found")
		}
		return s.iacTerraformRoot(ctx, ws)
	case iacSourceAnsible:
		pb, err := s.db.GetAnsiblePlaybook(id)
		if err != nil {
			return "", noop, err
		}
		if pb == nil {
			return "", noop, errors.New("playbook not found")
		}
		return s.iacAnsibleRoot(ctx, pb)
	}
	return "", noop, errors.New("unknown source kind")
}

// iacRepoRoot returns the clone directory of a repository. The stored path is absolute and comes from the
// clone job itself; it is only used when it still is a directory.
func (s *Server) iacRepoRoot(repo *store.GitRepo) (string, func(), error) {
	dir := strings.TrimSpace(repo.Dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return "", func() {}, errors.New("repository has no directory on this server")
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", func() {}, errors.New("repository folder is not available on this server")
	}
	return dir, func() {}, nil
}

// iacTerraformRoot materializes the Terraform source of a workspace: the inline files of the workspace, or a
// shallow clone of the Git source.
func (s *Server) iacTerraformRoot(ctx context.Context, ws *store.TFWorkspace) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "pantau-iacscan-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if ws.Source == "git" {
		g, err := s.terraformGitSource(ws)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		repoDir, _, err := ansible.CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, tmp, "https:ssh", io.Discard)
		if err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("cannot read the Git source: %w", err)
		}
		return iacSubDir(repoDir, ws.GitPath), cleanup, nil
	}
	files := map[string]string{}
	if ws.Files != "" && ws.Files != "{}" {
		if err := json.Unmarshal([]byte(ws.Files), &files); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("invalid workspace files: %w", err)
		}
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmp, filepath.Base(name)), []byte(content), 0600); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return tmp, cleanup, nil
}

// iacAnsibleRoot materializes the Ansible source of a project: the inline playbook with its assets, or a
// shallow clone of the Git source.
func (s *Server) iacAnsibleRoot(ctx context.Context, pb *store.AnsiblePlaybook) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "pantau-iacscan-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if pb.Source == "git" {
		g, err := s.ansibleGitSource(pb)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		repoDir, _, err := ansible.CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, tmp, "https:ssh", io.Discard)
		if err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("cannot read the Git source: %w", err)
		}
		// The whole repository is scanned: a playbook can pull roles, vars and group_vars from anywhere in it.
		return repoDir, cleanup, nil
	}
	name := "playbook.yml"
	if pb.Name != "" {
		name = pb.Name
		if !strings.HasSuffix(strings.ToLower(name), ".yml") && !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			name += ".yml"
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, name), []byte(pb.Content), 0600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	assets, err := s.db.LoadAnsibleAssets()
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, a := range assets {
		if a.Kind != "file" {
			continue
		}
		p := filepath.Join(tmp, filepath.FromSlash(a.Name))
		if !strings.HasPrefix(p, tmp+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			continue
		}
		_ = os.WriteFile(p, a.Data, 0600)
	}
	return tmp, cleanup, nil
}

// iacSubDir returns the sub directory of a cloned repository that the run actually uses (the Terraform work
// dir). It refuses a path that escapes the repository, so the scan never leaves the clone.
func iacSubDir(repoDir, sub string) string {
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return repoDir
	}
	target := filepath.Join(repoDir, filepath.FromSlash(sub))
	rootReal, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		return repoDir
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return repoDir
	}
	rel, err := filepath.Rel(rootReal, targetReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return repoDir
	}
	return targetReal
}

// iacResultPayload renders a fresh scan result for the API.
func iacResultPayload(res *iacscan.Result) map[string]interface{} {
	return map[string]interface{}{
		"source_kind":   "",
		"scanned_at":    res.ScannedAt,
		"status":        res.Status,
		"files_scanned": res.FilesScanned,
		"truncated":     res.Truncated,
		"summary":       res.Summary,
		"findings":      res.Findings,
		"notes":         res.Notes,
		"blocking":      res.Blocking(),
	}
}

// iacScanPayload renders a stored scan for the API.
func iacScanPayload(sc *store.IacScan) map[string]interface{} {
	var summary map[string]interface{}
	_ = json.Unmarshal(sc.Summary, &summary)
	var findings []map[string]interface{}
	_ = json.Unmarshal(sc.Findings, &findings)
	var notes []string
	_ = json.Unmarshal(sc.Notes, &notes)
	if findings == nil {
		findings = []map[string]interface{}{}
	}
	if notes == nil {
		notes = []string{}
	}
	blocking := false
	if summary != nil {
		if c, ok := summary["critical"].(float64); ok && c > 0 {
			blocking = true
		}
		if h, ok := summary["high"].(float64); ok && h > 0 {
			blocking = true
		}
	}
	return map[string]interface{}{
		"source_kind":   sc.SourceKind,
		"source_id":     sc.SourceID,
		"scanned_at":    sc.ScannedAt,
		"status":        sc.Status,
		"files_scanned": sc.Files,
		"truncated":     sc.Truncated,
		"summary":       summary,
		"findings":      findings,
		"notes":         notes,
		"blocking":      blocking,
	}
}

// iacScanDigest renders the short digest of a stored scan (fleet list).
func iacScanDigest(sc *store.IacScan) map[string]interface{} {
	var summary map[string]interface{}
	_ = json.Unmarshal(sc.Summary, &summary)
	if summary == nil {
		summary = map[string]interface{}{}
	}
	return map[string]interface{}{
		"source_kind":   sc.SourceKind,
		"source_id":     sc.SourceID,
		"scanned_at":    sc.ScannedAt,
		"status":        sc.Status,
		"files_scanned": sc.Files,
		"truncated":     sc.Truncated,
		"summary":       summary,
	}
}

// iacGateSummary renders the gate answer of a scan: the counts that block and the top findings.
func iacGateSummary(res *iacscan.Result) map[string]interface{} {
	blocking := []iacscan.Finding{}
	for _, f := range res.Findings {
		if f.Severity == iacscan.SeverityCritical || f.Severity == iacscan.SeverityHigh {
			blocking = append(blocking, f)
		}
	}
	if len(blocking) > 20 {
		blocking = blocking[:20]
	}
	return map[string]interface{}{
		"status":        res.Status,
		"critical":      res.Summary.Critical,
		"high":          res.Summary.High,
		"medium":        res.Summary.Medium,
		"low":           res.Summary.Low,
		"total":         res.Summary.Total,
		"files_scanned": res.FilesScanned,
		"truncated":     res.Truncated,
		"findings":      blocking,
		"notes":         res.Notes,
	}
}

// iacCheckGate scans a source and answers whether it blocks. It returns (blocks, summary).
// A source that cannot be assessed does not block: an incomplete scan must not silently stop a legitimate run.
func (s *Server) iacCheckGate(ctx context.Context, kind string, id int64) (bool, map[string]interface{}) {
	root, cleanup, err := s.iacSourceRoot(ctx, kind, id)
	if err != nil {
		return false, map[string]interface{}{"status": "not_assessed", "error": err.Error()}
	}
	defer cleanup()
	res := iacscan.ScanDir(root, iacOptions())
	_ = s.saveIacScan(kind, id, &res)
	return res.Blocking(), iacGateSummary(&res)
}
