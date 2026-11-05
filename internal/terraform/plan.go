package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pantau/internal/ansible"
)

type PlanSpec struct {
	Config
	RunID   int64
	Mode    string   // "plan" | "destroy" | "refresh" | "drift"
	Targets []string // address resource
	Replace []string // address resource
	Timeout time.Duration
}

type AttrDiff struct {
	Path      string `json:"path"`
	Before    any    `json:"before"`
	After     any    `json:"after"`
	Sensitive bool   `json:"sensitive"`
	Unknown   bool   `json:"unknown"`
}

type ResourceChange struct {
	Address  string     `json:"address"`
	Module   string     `json:"module,omitempty"`
	Mode     string     `json:"mode"`
	Type     string     `json:"type"`
	Name     string     `json:"name"`
	Provider string     `json:"provider"`
	Action   string     `json:"action"` // create | update | delete | replace | read | no-op
	Reason   string     `json:"reason,omitempty"`
	Diffs    []AttrDiff `json:"diffs,omitempty"`
}

type PlanSummary struct {
	Create        int              `json:"create"`
	Update        int              `json:"update"`
	Delete        int              `json:"delete"`
	Replace       int              `json:"replace"`
	Read          int              `json:"read"`
	Changes       []ResourceChange `json:"changes"`
	Drift         []ResourceChange `json:"drift"`
	OutputChanges []string         `json:"output_changes"`
}

type PlanResult struct {
	ExitCode   int         `json:"exit_code"`
	HasChanges bool        `json:"has_changes"`
	Summary    PlanSummary `json:"summary"`
	PlanJSON   []byte      `json:"plan_json"`
	Commit     string      `json:"commit,omitempty"`
	TFVersion  string      `json:"tf_version,omitempty"`
}

func (s PlanSpec) Validate() error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	if s.RunID <= 0 {
		return errors.New("run_id must be > 0")
	}
	mode := s.Mode
	if mode == "" {
		mode = "plan"
	}
	if mode != "plan" && mode != "destroy" && mode != "refresh" && mode != "drift" {
		return fmt.Errorf("invalid mode %q", s.Mode)
	}
	for _, t := range s.Targets {
		if !targetAddrRe.MatchString(t) {
			return fmt.Errorf("invalid target address %q", t)
		}
	}
	for _, r := range s.Replace {
		if !targetAddrRe.MatchString(r) {
			return fmt.Errorf("invalid replace address %q", r)
		}
	}
	return nil
}

// Plan prepares the workdir, runs terraform/tofu init, plan and show -json.
func Plan(ctx context.Context, spec PlanSpec, log io.Writer) (*PlanResult, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	binPath, err := exec.LookPath(spec.Binary)
	if err != nil {
		return nil, fmt.Errorf("%s binary not found in PATH: %w", spec.Binary, err)
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Collect sensitive secrets to redact from log
	var sensitiveSecrets []string
	for _, v := range spec.Vars {
		if v.Sensitive && v.Value != "" {
			sensitiveSecrets = append(sensitiveSecrets, v.Value)
		}
	}
	for _, v := range spec.Env {
		if len(v) >= 4 { // Redact reasonable length secrets
			sensitiveSecrets = append(sensitiveSecrets, v)
		}
	}
	rLog := newRedactionWriter(log, sensitiveSecrets)

	// Ensure Root, home, data, plugin-cache exist with 0700
	if err := os.MkdirAll(filepath.Join(spec.Root, "home"), 0700); err != nil {
		return nil, fmt.Errorf("failed to create root/home: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(spec.Root, "data"), 0700); err != nil {
		return nil, fmt.Errorf("failed to create root/data: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(spec.DataRoot, "plugin-cache"), 0700); err != nil {
		return nil, fmt.Errorf("failed to create plugin-cache: %w", err)
	}

	// Write latest state to Root/terraform.tfstate if provided
	stateFilePath := filepath.Join(spec.Root, "terraform.tfstate")
	if len(spec.State) > 0 {
		if err := os.WriteFile(stateFilePath, spec.State, 0600); err != nil {
			return nil, fmt.Errorf("failed to write root state: %w", err)
		}
	}

	runDir := filepath.Join(spec.Root, "runs", fmt.Sprintf("%d", spec.RunID))
	workDir := filepath.Join(runDir, "work")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	// In case of error before finish or for drift mode, cleanup
	defer func() {
		if spec.Mode == "drift" {
			CleanupRun(spec.Root, spec.RunID)
		}
	}()

	var commitSHA string
	if spec.Files != nil {
		for name, content := range spec.Files {
			p := filepath.Join(workDir, name)
			if err := os.WriteFile(p, []byte(content), 0600); err != nil {
				return nil, fmt.Errorf("failed to write inline file %q: %w", name, err)
			}
		}
	} else if spec.Git != nil {
		repoDir, sha, err := ansible.CloneGitRepo(runCtx, spec.Git.URL, spec.Git.Branch, spec.Git.Token, spec.Git.PrivateKey, runDir, "https:ssh", rLog)
		if err != nil {
			return nil, fmt.Errorf("git clone failed: %w", err)
		}
		commitSHA = sha

		targetWorkDir := repoDir
		if spec.WorkDir != "" {
			targetWorkDir = filepath.Join(repoDir, filepath.FromSlash(spec.WorkDir))
			rootReal, err := filepath.EvalSymlinks(repoDir)
			if err != nil {
				return nil, err
			}
			targetReal, err := filepath.EvalSymlinks(targetWorkDir)
			if err != nil {
				return nil, fmt.Errorf("workdir %q not found in repo: %w", spec.WorkDir, err)
			}
			if rel, err := filepath.Rel(rootReal, targetReal); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("workdir resolves outside repository")
			}
		}
		workDir = targetWorkDir
	}

	// Write var files into workDir
	if err := writeVarFiles(workDir, spec.Vars); err != nil {
		return nil, err
	}

	env := spec.prepareEnv()

	// 1. terraform init -input=false -no-color
	initCmd := exec.Command(binPath, "init", "-input=false", "-no-color")
	initCmd.Dir = workDir
	initCmd.Env = env
	initCmd.Stdout = rLog
	initCmd.Stderr = rLog

	initExit, err := runCmd(runCtx, initCmd)
	if err != nil {
		return nil, err
	}
	if initExit != 0 {
		return &PlanResult{ExitCode: initExit, Commit: commitSHA}, nil
	}

	// 2. terraform plan
	planPath := filepath.Join(runDir, "tfplan")
	planArgs := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-state=" + stateFilePath, "-out=" + planPath}

	mode := spec.Mode
	if mode == "destroy" {
		planArgs = append(planArgs, "-destroy")
	} else if mode == "refresh" || mode == "drift" {
		planArgs = append(planArgs, "-refresh-only")
	}

	for _, t := range spec.Targets {
		planArgs = append(planArgs, "-target="+t)
	}
	for _, r := range spec.Replace {
		planArgs = append(planArgs, "-replace="+r)
	}

	planCmd := exec.Command(binPath, planArgs...)
	planCmd.Dir = workDir
	planCmd.Env = env
	planCmd.Stdout = rLog
	planCmd.Stderr = rLog

	planExit, err := runCmd(runCtx, planCmd)
	if err != nil {
		return nil, err
	}

	// Exit codes: 0 = no changes, 1 = error, 2 = changes present
	if planExit == 1 {
		return &PlanResult{ExitCode: 1, Commit: commitSHA}, nil
	}

	// 3. terraform show -json tfplan
	showCmd := exec.Command(binPath, "show", "-json", planPath)
	showCmd.Dir = workDir
	showCmd.Env = env
	planJSON, err := showCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run show -json: %w", err)
	}

	if len(planJSON) > 32*1024*1024 {
		return nil, errors.New("plan json exceeds 32 MiB")
	}

	summary, tfVer, err := ParsePlanJSON(planJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan json: %w", err)
	}

	hasChanges := planExit == 2 || summary.Create > 0 || summary.Update > 0 || summary.Delete > 0 || summary.Replace > 0

	return &PlanResult{
		ExitCode:   planExit,
		HasChanges: hasChanges,
		Summary:    summary,
		PlanJSON:   planJSON,
		Commit:     commitSHA,
		TFVersion:  tfVer,
	}, nil
}

// ParsePlanJSON parses terraform show -json output into PlanSummary.
func ParsePlanJSON(b []byte) (summary PlanSummary, tfVer string, err error) {
	var raw struct {
		TerraformVersion string         `json:"terraform_version"`
		ResourceChanges  []rawResChange `json:"resource_changes"`
		ResourceDrift    []rawResChange `json:"resource_drift"`
		OutputChanges    map[string]any `json:"output_changes"`
	}

	if err := json.Unmarshal(b, &raw); err != nil {
		return summary, "", err
	}

	tfVer = raw.TerraformVersion

	for k := range raw.OutputChanges {
		summary.OutputChanges = append(summary.OutputChanges, k)
	}
	sort.Strings(summary.OutputChanges)

	parseChanges := func(rcs []rawResChange) []ResourceChange {
		var list []ResourceChange
		for _, rc := range rcs {
			action := determineAction(rc.Change.Actions)
			item := ResourceChange{
				Address:  rc.Address,
				Module:   rc.ModuleAddress,
				Mode:     rc.Mode,
				Type:     rc.Type,
				Name:     rc.Name,
				Provider: rc.ProviderName,
				Action:   action,
				Reason:   rc.Change.ActionReason,
				Diffs:    extractAttrDiffs(rc.Change),
			}
			list = append(list, item)
		}
		return list
	}

	allChanges := parseChanges(raw.ResourceChanges)
	for _, c := range allChanges {
		switch c.Action {
		case "create":
			summary.Create++
		case "update":
			summary.Update++
		case "delete":
			summary.Delete++
		case "replace":
			summary.Replace++
		case "read":
			summary.Read++
		}
		if c.Action != "no-op" {
			summary.Changes = append(summary.Changes, c)
		}
	}

	summary.Drift = parseChanges(raw.ResourceDrift)

	return summary, tfVer, nil
}

type rawResChange struct {
	Address       string    `json:"address"`
	ModuleAddress string    `json:"module_address"`
	Mode          string    `json:"mode"`
	Type          string    `json:"type"`
	Name          string    `json:"name"`
	ProviderName  string    `json:"provider_name"`
	Change        rawChange `json:"change"`
}

type rawChange struct {
	Actions         []string `json:"actions"`
	Before          any      `json:"before"`
	After           any      `json:"after"`
	AfterUnknown    any      `json:"after_unknown"`
	BeforeSensitive any      `json:"before_sensitive"`
	AfterSensitive  any      `json:"after_sensitive"`
	ActionReason    string   `json:"action_reason"`
}

func determineAction(actions []string) string {
	if len(actions) == 1 {
		switch actions[0] {
		case "create":
			return "create"
		case "delete":
			return "delete"
		case "update":
			return "update"
		case "read":
			return "read"
		case "no-op":
			return "no-op"
		}
	}
	if len(actions) == 2 {
		if (actions[0] == "create" && actions[1] == "delete") || (actions[0] == "delete" && actions[1] == "create") {
			return "replace"
		}
	}
	return strings.Join(actions, ",")
}

func extractAttrDiffs(c rawChange) []AttrDiff {
	var diffs []AttrDiff

	beforeMap, _ := c.Before.(map[string]any)
	afterMap, _ := c.After.(map[string]any)
	unknownMap, _ := c.AfterUnknown.(map[string]any)

	keySet := make(map[string]bool)
	for k := range beforeMap {
		keySet[k] = true
	}
	for k := range afterMap {
		keySet[k] = true
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		bVal := beforeMap[k]
		aVal := afterMap[k]
		unknown := false
		if unknownMap != nil {
			if u, ok := unknownMap[k]; ok {
				if ub, isBool := u.(bool); isBool && ub {
					unknown = true
				}
			}
		}

		isSens := isSensitiveKey(c.BeforeSensitive, k) || isSensitiveKey(c.AfterSensitive, k)

		// Compare before and after
		if !jsonEqual(bVal, aVal) || unknown {
			if isSens {
				bVal = "(sensitive)"
				aVal = "(sensitive)"
			}
			diffs = append(diffs, AttrDiff{
				Path:      k,
				Before:    bVal,
				After:     aVal,
				Sensitive: isSens,
				Unknown:   unknown,
			})
		}
	}

	return diffs
}

func isSensitiveKey(sensitiveObj any, key string) bool {
	if sensitiveObj == nil {
		return false
	}
	if b, ok := sensitiveObj.(bool); ok && b {
		return true
	}
	if m, ok := sensitiveObj.(map[string]any); ok {
		if val, exists := m[key]; exists {
			if b, ok := val.(bool); ok && b {
				return true
			}
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	ba, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ba) == string(bb)
}
