package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

type activeTFRun struct {
	runID       int64
	workspaceID int64
	cancel      context.CancelFunc
	logBuf      *logBuffer
	done        chan struct{}
}

// PlannerFunc executes a terraform plan. Defaults to terraform.Plan.
type PlannerFunc func(ctx context.Context, spec terraform.PlanSpec, log io.Writer) (*terraform.PlanResult, error)

// ApplierFunc executes a terraform apply. Defaults to terraform.Apply.
type ApplierFunc func(ctx context.Context, spec terraform.ApplySpec, log io.Writer) (*terraform.ApplyResult, error)

// TerraformRunManager coordinates running terraform pipelines, locks, and log streaming.
type TerraformRunManager struct {
	db          *store.DB
	dataRoot    string
	planner     PlannerFunc
	applier     ApplierFunc
	mu          sync.Mutex
	activeRuns  map[int64]*activeTFRun               // runID -> activeTFRun
	byWorkspace map[int64]int64                      // workspaceID -> runID
	events      func(source, summary, detail string) // notification sink (nil = off)
}

// NewTerraformRunManager creates a manager with default or injectable runner functions.
func NewTerraformRunManager(db *store.DB, dataRoot string, planner PlannerFunc, applier ApplierFunc) *TerraformRunManager {
	if planner == nil {
		planner = terraform.Plan
	}
	if applier == nil {
		applier = terraform.Apply
	}
	return &TerraformRunManager{
		db:          db,
		dataRoot:    dataRoot,
		planner:     planner,
		applier:     applier,
		activeRuns:  make(map[int64]*activeTFRun),
		byWorkspace: make(map[int64]int64),
	}
}

// CanStartRun checks global and per-workspace concurrency limits.
// Returns 429 if 3 active runs, 409 if workspace already has an active run.
func (m *TerraformRunManager) CanStartRun(workspaceID int64) (status int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.activeRuns) >= terraform.MaxConcurrentRuns {
		return 429, fmt.Errorf("too many concurrent runs (max %d)", terraform.MaxConcurrentRuns)
	}
	if activeID, exists := m.byWorkspace[workspaceID]; exists {
		return 409, fmt.Errorf("workspace already has an active run (run %d)", activeID)
	}
	return 0, nil
}

// resolveCloudEnv turns a cloud account into env vars (AssumeRole for AWS); tests replace it.
var resolveCloudEnv = terraform.ResolveCloudEnv

// BuildRunConfig constructs the terraform.Config for a workspace.
func (m *TerraformRunManager) BuildRunConfig(ws *store.TFWorkspace) (terraform.Config, error) {
	wsDir := filepath.Join(m.dataRoot, strconv.FormatInt(ws.ID, 10))

	cfg := terraform.Config{
		Binary:   ws.Binary,
		DataRoot: m.dataRoot,
		Root:     wsDir,
		WorkDir:  ws.GitPath,
	}
	if cfg.Binary == "" {
		cfg.Binary = "terraform"
	}

	if ws.Source == "inline" {
		files := make(map[string]string)
		if ws.Files != "" && ws.Files != "{}" {
			if err := json.Unmarshal([]byte(ws.Files), &files); err != nil {
				return cfg, fmt.Errorf("invalid workspace files JSON: %w", err)
			}
		}
		cfg.Files = files
	} else if ws.Source == "git" {
		cfg.Git = &terraform.GitSource{
			URL:    ws.GitURL,
			Branch: ws.GitBranch,
			Token:  ws.GitToken,
		}
		if ws.GitAuth == "ssh" && ws.GitKeyID > 0 {
			key, err := m.db.GetSSHKey(ws.GitKeyID)
			if err != nil {
				return cfg, fmt.Errorf("failed to load git ssh key: %w", err)
			}
			if key != nil {
				cfg.Git.PrivateKey = key.PrivateKey
			}
		}
	}

	// Effective variables
	effVars, err := m.db.EffectiveTFVariables(ws.ID)
	if err != nil {
		return cfg, fmt.Errorf("failed to load effective variables: %w", err)
	}

	cfg.Env = make(map[string]string)
	cfg.PlainEnv = make(map[string]bool)
	cfg.EnvFiles = make(map[string]string)
	// Cloud account env first so variable-set env vars with the same key override it.
	// It goes through Config.Env, so plan/apply log redaction covers the credentials;
	// only the keys fed by non-secret fields are marked plain.
	for _, id := range ws.CloudAccountIDs {
		acct, err := m.db.GetTFCloudAccount(id)
		if err != nil {
			return cfg, fmt.Errorf("failed to load cloud account %d: %w", id, err)
		}
		if acct == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		env, err := resolveCloudEnv(ctx, acct.Provider, acct.Fields, nil)
		cancel()
		if err != nil {
			return cfg, fmt.Errorf("cloud account %q: %w", acct.Name, err)
		}
		for k, v := range env {
			cfg.Env[k] = v
			delete(cfg.PlainEnv, k)
		}
		for _, k := range terraform.CloudPlainEnv(acct.Provider) {
			if _, ok := env[k]; ok {
				cfg.PlainEnv[k] = true
			}
		}
		for k, v := range terraform.CloudEnvFiles(acct.Provider, acct.Fields) {
			cfg.EnvFiles[k] = v
		}
	}
	for _, v := range effVars {
		if v.Category == "env" {
			cfg.Env[v.Key] = v.Value
			delete(cfg.PlainEnv, v.Key) // a user variable is always redacted
			delete(cfg.EnvFiles, v.Key)
		} else {
			cfg.Vars = append(cfg.Vars, terraform.Var{
				Key:       v.Key,
				Value:     v.Value,
				HCL:       v.HCL,
				Sensitive: v.Sensitive,
			})
		}
	}

	if err := m.injectOutputLinks(ws, &cfg); err != nil {
		return cfg, err
	}

	// Latest state
	latestState, err := m.db.LatestTFStateVersion(ws.ID)
	if err == nil && latestState != nil {
		cfg.State = latestState.State
	}

	return cfg, nil
}

// StartPlan launches plan execution for a run in background.
func (m *TerraformRunManager) StartPlan(ws *store.TFWorkspace, run *store.TFRun) error {
	m.mu.Lock()
	if len(m.activeRuns) >= terraform.MaxConcurrentRuns {
		m.mu.Unlock()
		return fmt.Errorf("too many concurrent runs (max %d)", terraform.MaxConcurrentRuns)
	}
	if activeID, exists := m.byWorkspace[ws.ID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("workspace already has an active run (run %d)", activeID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	logBuf := newLogBuffer()
	ar := &activeTFRun{
		runID:       run.ID,
		workspaceID: ws.ID,
		cancel:      cancel,
		logBuf:      logBuf,
		done:        make(chan struct{}),
	}
	m.activeRuns[run.ID] = ar
	m.byWorkspace[ws.ID] = run.ID
	m.mu.Unlock()

	go m.executePlan(ctx, ar, ws, run)
	return nil
}

func (m *TerraformRunManager) executePlan(ctx context.Context, ar *activeTFRun, ws *store.TFWorkspace, run *store.TFRun) {
	autoApply := false
	defer func() { // registered first, so it runs last (after the active-run bookkeeping)
		if autoApply {
			m.tfStartAutoApply(ws.ID, run.ID)
		}
	}()
	defer close(ar.done)

	retainWorkspaceLock := false
	defer func() {
		m.mu.Lock()
		delete(m.activeRuns, ar.runID)
		if !retainWorkspaceLock && m.byWorkspace[ar.workspaceID] == ar.runID {
			delete(m.byWorkspace, ar.workspaceID)
			// Unlock workspace in DB
			_ = m.db.UnlockTFWorkspace(ar.workspaceID)
		}
		m.mu.Unlock()
	}()

	cfg, err := m.BuildRunConfig(ws)
	if err != nil {
		_ = m.db.FinishTFRun(run.ID, "errored", nil, fmt.Sprintf("failed to build run config: %v", err))
		m.tfEmit(ws, "errored", "Plan errored", tfEventDetail(run.ID, nil, err.Error(), nil))
		return
	}

	var targets, replace []string
	if run.Targets != "" && run.Targets != "[]" {
		_ = json.Unmarshal([]byte(run.Targets), &targets)
	}
	if run.Replace != "" && run.Replace != "[]" {
		_ = json.Unmarshal([]byte(run.Replace), &replace)
	}

	spec := terraform.PlanSpec{
		Config:  cfg,
		RunID:   run.ID,
		Mode:    run.Type,
		Targets: targets,
		Replace: replace,
	}

	// Update DB to planning
	now := time.Now().UTC()
	run.StartedAt = &now
	run.Status = "planning"
	_ = m.db.UpdateTFRunStatus(run.ID, "planning", nil, "")

	planner := m.planner
	if planner == nil {
		planner = terraform.Plan
	}

	planRes, planErr := planner(ctx, spec, ar.logBuf)

	if ctx.Err() == context.Canceled {
		terraform.CleanupRun(cfg.Root, run.ID)
		_ = m.db.FinishTFRun(run.ID, "canceled", nil, ar.logBuf.String())
		return
	}

	if planErr != nil || (planRes != nil && planRes.ExitCode == 1) {
		terraform.CleanupRun(cfg.Root, run.ID)
		exitCode := 1
		if planRes != nil && planRes.ExitCode != 0 {
			exitCode = planRes.ExitCode
		}
		msg := ""
		if planErr != nil {
			msg = fmt.Sprintf("\n[pantau] plan error: %v\n", planErr)
			_, _ = io.WriteString(ar.logBuf, msg)
		}
		_ = m.db.FinishTFRun(run.ID, "errored", &exitCode, ar.logBuf.String())
		m.tfEmit(ws, "errored", "Plan errored", tfEventDetail(run.ID, nil, ar.logBuf.String(), tfSecrets(cfg)))
		_ = m.db.PruneTFRuns(ws.ID, 100)
		return
	}

	// Update workspace version if detected
	if planRes != nil && planRes.TFVersion != "" {
		_ = m.db.UpdateTFWorkspaceVersion(ws.ID, planRes.TFVersion)
	}

	// Plan completed successfully (exit code 0 or 2)
	summaryJSON, _ := json.Marshal(planRes.Summary)
	driftJSON, _ := json.Marshal(planRes.Summary.Drift)

	// Mode "drift" or speculative
	if run.Type == "drift" {
		terraform.CleanupRun(cfg.Root, run.ID)
		exitCode := planRes.ExitCode
		driftDetected := len(planRes.Summary.Drift) > 0
		_ = m.db.UpdateTFRunPlan(run.ID, "planned", string(summaryJSON), string(driftJSON), "{}", "{}", planRes.Commit, planRes.HasChanges)
		_ = m.db.FinishTFRun(run.ID, "planned", &exitCode, ar.logBuf.String())
		_ = m.db.UpdateTFWorkspaceDrift(ws.ID, driftDetected)
		if driftDetected && !ws.DriftDetected { // notify on the transition only, not on every scheduled check
			m.tfEmit(ws, "drift", "Drift detected", tfEventDetail(run.ID, &planRes.Summary, "", nil))
		}
		_ = m.db.PruneTFRuns(ws.ID, 100)
		return
	}

	if run.Speculative {
		terraform.CleanupRun(cfg.Root, run.ID)
		exitCode := planRes.ExitCode
		_ = m.db.UpdateTFRunPlan(run.ID, "planned", string(summaryJSON), string(driftJSON), "{}", "{}", planRes.Commit, planRes.HasChanges)
		_ = m.db.FinishTFRun(run.ID, "planned", &exitCode, ar.logBuf.String())
		_ = m.db.PruneTFRuns(ws.ID, 100)
		return
	}

	if !planRes.HasChanges {
		terraform.CleanupRun(cfg.Root, run.ID)
		exitCode := 0
		_ = m.db.UpdateTFRunPlan(run.ID, "no_changes", string(summaryJSON), string(driftJSON), "{}", "{}", planRes.Commit, false)
		_ = m.db.FinishTFRun(run.ID, "no_changes", &exitCode, ar.logBuf.String())
		_ = m.db.PruneTFRuns(ws.ID, 100)
		return
	}

	// Has changes: Policy evaluation
	policies, err := m.db.ListTFPolicies(ws.ID)
	if err != nil {
		logger.Warn("terraform", ws.Name, fmt.Sprintf("Failed to list policies: %v", err))
	}
	var enabledPolicies []terraform.Policy
	for _, p := range policies {
		if p.Enabled {
			enabledPolicies = append(enabledPolicies, terraform.Policy{
				ID:          p.ID,
				Name:        p.Name,
				Kind:        p.Kind,
				Enforcement: p.Enforcement,
				Params:      json.RawMessage(p.Params),
			})
		}
	}

	policyReport, err := terraform.EvaluatePolicies(planRes.PlanJSON, enabledPolicies)
	if err != nil {
		logger.Warn("terraform", ws.Name, fmt.Sprintf("Failed to evaluate policies: %v", err))
	}
	policyJSON, _ := json.Marshal(policyReport)

	// Cost estimation
	costEstimate := terraform.EstimateCost(ctx, planRes.PlanJSON, cfg.Env)
	costJSON, _ := json.Marshal(costEstimate)

	if policyReport.Status == "failed" {
		// Hard policy failure blocks run
		terraform.CleanupRun(cfg.Root, run.ID)
		exitCode := 1
		_ = m.db.UpdateTFRunPlan(run.ID, "policy_failed", string(summaryJSON), string(driftJSON), string(policyJSON), string(costJSON), planRes.Commit, true)
		_ = m.db.FinishTFRun(run.ID, "policy_failed", &exitCode, ar.logBuf.String())
		_ = m.db.PruneTFRuns(ws.ID, 100)
		return
	}

	// Status transitions to awaiting_approval
	// Workspace remains locked by run:<id>!
	retainWorkspaceLock = true
	_ = m.db.UpdateTFRunPlan(run.ID, "awaiting_approval", string(summaryJSON), string(driftJSON), string(policyJSON), string(costJSON), planRes.Commit, true)
	_ = m.db.UpdateTFRunStatus(run.ID, "awaiting_approval", nil, ar.logBuf.String())
	if m.tfWantsAutoApply(ws, run, policyReport.Status) {
		autoApply = true // started after the deferred cleanup below so the manager state is consistent
		return
	}
	m.tfEmit(ws, "awaiting_approval", "Plan awaiting approval", tfEventDetail(run.ID, &planRes.Summary, "", nil))
}

// StartApply launches apply execution in background for an awaiting_approval run.
func (m *TerraformRunManager) StartApply(ws *store.TFWorkspace, run *store.TFRun, comment string) error {
	m.mu.Lock()
	if activeID, exists := m.byWorkspace[ws.ID]; exists && activeID != run.ID {
		m.mu.Unlock()
		return fmt.Errorf("workspace already has an active run (run %d)", activeID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	logBuf := newLogBuffer()
	if run.Log != "" {
		_, _ = logBuf.Write([]byte(run.Log))
		_, _ = logBuf.Write([]byte("\n--- Applying ---\n"))
	}
	ar := &activeTFRun{
		runID:       run.ID,
		workspaceID: ws.ID,
		cancel:      cancel,
		logBuf:      logBuf,
		done:        make(chan struct{}),
	}
	m.activeRuns[run.ID] = ar
	m.byWorkspace[ws.ID] = run.ID
	m.mu.Unlock()

	go m.executeApply(ctx, ar, ws, run, comment)
	return nil
}

func (m *TerraformRunManager) executeApply(ctx context.Context, ar *activeTFRun, ws *store.TFWorkspace, run *store.TFRun, comment string) {
	defer close(ar.done)
	defer func() {
		m.mu.Lock()
		delete(m.activeRuns, ar.runID)
		if m.byWorkspace[ar.workspaceID] == ar.runID {
			delete(m.byWorkspace, ar.workspaceID)
			_ = m.db.UnlockTFWorkspace(ar.workspaceID)
		}
		m.mu.Unlock()
	}()

	cfg, err := m.BuildRunConfig(ws)
	if err != nil {
		_ = m.db.FinishTFRun(run.ID, "errored", nil, fmt.Sprintf("failed to build run config: %v", err))
		m.tfEmit(ws, "errored", "Apply errored", tfEventDetail(run.ID, nil, err.Error(), nil))
		return
	}

	_ = m.db.RecordTFRunDecision(run.ID, "applied", comment, comment)
	_ = m.db.UpdateTFRunStatus(run.ID, "applying", nil, ar.logBuf.String())

	spec := terraform.ApplySpec{
		Config: cfg,
		RunID:  run.ID,
	}

	applier := m.applier
	if applier == nil {
		applier = terraform.Apply
	}

	applyRes, applyErr := applier(ctx, spec, ar.logBuf)

	if ctx.Err() == context.Canceled {
		_ = m.db.FinishTFRun(run.ID, "canceled", nil, ar.logBuf.String())
		return
	}

	if applyErr != nil || (applyRes != nil && applyRes.ExitCode != 0) {
		exitCode := 1
		if applyRes != nil && applyRes.ExitCode != 0 {
			exitCode = applyRes.ExitCode
		}
		if applyErr != nil {
			_, _ = io.WriteString(ar.logBuf, fmt.Sprintf("\n[pantau] apply error: %v\n", applyErr))
		}
		_ = m.db.FinishTFRun(run.ID, "errored", &exitCode, ar.logBuf.String())
		_ = m.db.PruneTFRuns(ws.ID, 100)
		m.tfEmit(ws, "errored", "Apply errored", tfEventDetail(run.ID, tfRunSummary(run), ar.logBuf.String(), tfSecrets(cfg)))
		return
	}

	exitCode := 0
	if applyRes != nil {
		exitCode = applyRes.ExitCode
	}

	// If new state produced, save state version if sha256 differs
	if applyRes != nil && len(applyRes.State) > 0 {
		h := sha256.Sum256(applyRes.State)
		shaStr := hex.EncodeToString(h[:])

		latestState, _ := m.db.LatestTFStateVersion(ws.ID)
		if latestState == nil || latestState.SHA256 != shaStr {
			stInfo, err := terraform.ParseState(applyRes.State)
			serial := int64(1)
			lineage := ""
			resCount := 0
			if err == nil && stInfo != nil {
				serial = stInfo.Serial
				lineage = stInfo.Lineage
				resCount = len(stInfo.Resources)
			}
			source := "apply"
			if run.Type == "refresh" {
				source = "refresh"
			}
			sv := &store.TFStateVersion{
				WorkspaceID:   ws.ID,
				RunID:         run.ID,
				Serial:        serial,
				Lineage:       lineage,
				Source:        source,
				Size:          int64(len(applyRes.State)),
				ResourceCount: resCount,
				SHA256:        shaStr,
				State:         applyRes.State,
			}
			_, _ = m.db.CreateTFStateVersion(sv)
			_ = m.db.PruneTFStateVersions(ws.ID, 100)
		}
	}

	// Reset drift_detected
	_ = m.db.UpdateTFWorkspaceDrift(ws.ID, false)

	_ = m.db.FinishTFRun(run.ID, "applied", &exitCode, ar.logBuf.String())
	_ = m.db.PruneTFRuns(ws.ID, 100)
	m.tfEmit(ws, "applied", "Apply succeeded", tfEventDetail(run.ID, tfRunSummary(run), "", nil))
}

// DiscardRun marks an awaiting_approval run as discarded and unlocks workspace.
func (m *TerraformRunManager) DiscardRun(wsID int64, runID int64, comment string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	run, err := m.db.GetTFRun(runID)
	if err != nil {
		return err
	}
	if run.Status != "awaiting_approval" {
		return errors.New("run is not awaiting approval")
	}

	wsDir := filepath.Join(m.dataRoot, strconv.FormatInt(wsID, 10))
	terraform.CleanupRun(wsDir, runID)

	_ = m.db.RecordTFRunDecision(runID, "discarded", comment, "")
	_ = m.db.FinishTFRun(runID, "discarded", nil, run.Log)
	_ = m.db.PruneTFRuns(wsID, 100)

	if m.byWorkspace[wsID] == runID {
		delete(m.byWorkspace, wsID)
		_ = m.db.UnlockTFWorkspace(wsID)
	}
	return nil
}

// CancelRun cancels an active running run.
func (m *TerraformRunManager) CancelRun(runID int64) bool {
	m.mu.Lock()
	ar, exists := m.activeRuns[runID]
	m.mu.Unlock()

	if !exists {
		return false
	}
	ar.cancel()
	return true
}

// ForceUnlockWorkspace cancels any active run for workspace, deletes lingering lock files, and unlocks DB.
func (m *TerraformRunManager) ForceUnlockWorkspace(wsID int64) {
	m.mu.Lock()
	if activeID, exists := m.byWorkspace[wsID]; exists {
		if ar, ok := m.activeRuns[activeID]; ok {
			ar.cancel()
		}
		delete(m.byWorkspace, wsID)
	}
	m.mu.Unlock()

	wsDir := filepath.Join(m.dataRoot, strconv.FormatInt(wsID, 10))
	terraform.ForceUnlockFiles(wsDir)
	_ = m.db.UnlockTFWorkspace(wsID)
}

// HasActiveRun checks if a run is active.
func (m *TerraformRunManager) HasActiveRun(runID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.activeRuns[runID]
	return exists
}

// HasActiveRunForWorkspace checks if a workspace has an active run.
func (m *TerraformRunManager) HasActiveRunForWorkspace(wsID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.byWorkspace[wsID]
	return exists
}

// ReadLog streams logs from memory if active or from DB if finished.
func (m *TerraformRunManager) ReadLog(runID int64, offset int) (data string, nextOffset int, status string, finished bool, err error) {
	m.mu.Lock()
	ar, active := m.activeRuns[runID]
	m.mu.Unlock()

	if active {
		data, nextOffset = ar.logBuf.ReadFrom(offset)
		return data, nextOffset, "running", false, nil
	}

	run, err := m.db.GetTFRun(runID)
	if err != nil {
		return "", 0, "", false, err
	}
	if run == nil {
		return "", 0, "", false, store.ErrTFRunNotFound
	}

	logStr := run.Log
	totalLen := len(logStr)
	if offset < 0 {
		offset = 0
	}
	if offset >= totalLen {
		return "", totalLen, run.Status, true, nil
	}
	return logStr[offset:], totalLen, run.Status, true, nil
}
