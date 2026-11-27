package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"pantau/internal/gwupdate"
	"pantau/internal/inspector"
)

const (
	fmtFileTimeout     = 20 * time.Second
	initTimeout        = 120 * time.Second
	validateCmdTimeout = 60 * time.Second
	toolOutputMax      = 4096
)

// FmtResult is the outcome of FormatFiles.
type FmtResult struct {
	Files   map[string]string `json:"files"`
	Changed []string          `json:"changed"`
	Errors  map[string]string `json:"errors"`
}

// Diagnostic is one terraform validate finding.
type Diagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// ValidateResult is the outcome of ValidateConfig.
type ValidateResult struct {
	OK                 bool         `json:"ok"`
	ErrorCount         int          `json:"error_count"`
	WarningCount       int          `json:"warning_count"`
	InitDurationMs     int64        `json:"init_duration_ms"`
	ValidateDurationMs int64        `json:"validate_duration_ms"`
	InitError          string       `json:"init_error"`
	InitErrorTail      string       `json:"init_error_tail,omitempty"`
	Diagnostics        []Diagnostic `json:"diagnostics"`
}

func lookTool(binary string) (string, error) {
	if binary != "terraform" && binary != "tofu" {
		return "", fmt.Errorf("invalid binary %q, must be terraform or tofu", binary)
	}
	p, err := gwupdate.ResolveBinary(binary)
	if err != nil {
		return "", fmt.Errorf("%s binary not found in PATH: %w", binary, err)
	}
	return p, nil
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > toolOutputMax {
		s = s[:toolOutputMax] + "…"
	}
	return s
}

// TailClip returns the last max bytes of s (UTF-8 safe) so trailing Error: blocks are preserved.
func TailClip(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := len(s) - max
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…\n" + strings.TrimSpace(s[cut:])
}

// ExtractErrorExcerpt extracts the relevant Error: block(s) or last 40 lines from a Terraform log,
// redacted with RedactAgentSecrets and bounded to 4 KB.
func ExtractErrorExcerpt(log string) string {
	redacted := strings.TrimSpace(inspector.RedactAgentSecrets(log))
	if redacted == "" {
		return ""
	}
	lines := strings.Split(redacted, "\n")
	// Prefer starting from the first "Error:" line (or "╷" just above it) if present near the end.
	errIdx := -1
	for i, ln := range lines {
		if strings.Contains(ln, "Error:") {
			errIdx = i
			if i > 0 && strings.TrimSpace(lines[i-1]) == "╷" {
				errIdx = i - 1
			}
			break
		}
	}
	var selected []string
	if errIdx >= 0 {
		selected = lines[errIdx:]
		if len(selected) > 60 {
			selected = selected[len(selected)-60:]
		}
	} else {
		start := len(lines) - 40
		if start < 0 {
			start = 0
		}
		selected = lines[start:]
	}
	return TailClip(strings.Join(selected, "\n"), toolOutputMax)
}

func isFmtFile(name string) bool {
	for _, ext := range []string{".tf", ".tfvars", ".hcl"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// FormatFiles runs `fmt -` on every .tf/.tfvars/.hcl file; other files come back untouched.
// A per-file syntax error is reported in Errors and the file keeps its content.
func FormatFiles(ctx context.Context, binary string, files map[string]string) (*FmtResult, error) {
	if err := ValidateFiles(files); err != nil {
		return nil, err
	}
	bin, err := lookTool(binary)
	if err != nil {
		return nil, err
	}
	res := &FmtResult{Files: map[string]string{}, Changed: []string{}, Errors: map[string]string{}}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "CHECKPOINT_DISABLE=1"}
	for _, n := range names {
		res.Files[n] = files[n]
		if !isFmtFile(n) {
			continue
		}
		fctx, cancel := context.WithTimeout(ctx, fmtFileTimeout)
		cmd := exec.Command(bin, "fmt", "-no-color", "-")
		cmd.Env, cmd.Dir = env, os.TempDir()
		cmd.Stdin = strings.NewReader(files[n])
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		code, err := runCmd(fctx, cmd)
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			res.Errors[n] = "fmt timed out"
		case err != nil:
			return nil, err
		case code != 0:
			res.Errors[n] = clip(stderr.String())
		default:
			res.Files[n] = stdout.String()
			if res.Files[n] != files[n] {
				res.Changed = append(res.Changed, n)
			}
		}
	}
	return res, nil
}

// ValidateConfig writes files to a throwaway dir and runs `init -backend=false` then
// `validate -json`. No cloud credentials are passed. An init failure is reported in
// InitError / InitErrorTail (ok=false); only execution problems return an error.
func ValidateConfig(ctx context.Context, binary, dataRoot string, files map[string]string) (*ValidateResult, error) {
	if err := ValidateFiles(files); err != nil {
		return nil, err
	}
	bin, err := lookTool(binary)
	if err != nil {
		return nil, err
	}

	root, err := os.MkdirTemp("", "pantau-validate-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	work := filepath.Join(root, "work")
	for _, d := range []string{work, filepath.Join(root, "home"), filepath.Join(root, "data"), filepath.Join(dataRoot, "plugin-cache")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
	}
	for n, content := range files {
		if err := os.WriteFile(filepath.Join(work, n), []byte(content), 0600); err != nil {
			return nil, err
		}
	}
	env := Config{Binary: binary, DataRoot: dataRoot, Root: root}.prepareEnv()

	runWithCtx := func(cctx context.Context, out, errOut *bytes.Buffer, args ...string) (int, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env = work, env
		cmd.Stdout, cmd.Stderr = out, errOut
		return runCmd(cctx, cmd)
	}

	var out, errOut bytes.Buffer
	initCtx, initCancel := context.WithTimeout(ctx, initTimeout)
	unlockCache := lockPluginCache(dataRoot)
	initStart := time.Now()
	code, err := runWithCtx(initCtx, &out, &out, "init", "-backend=false", "-input=false", "-no-color")
	initDur := time.Since(initStart).Milliseconds()
	unlockCache()
	initCancel()
	if err != nil {
		return nil, err
	}
	if code != 0 {
		tail := TailClip(inspector.RedactAgentSecrets(out.String()), toolOutputMax)
		return &ValidateResult{
			OK:             false,
			InitDurationMs: initDur,
			InitError:      tail,
			InitErrorTail:  tail,
			Diagnostics:    []Diagnostic{},
		}, nil
	}

	out.Reset()
	valCtx, valCancel := context.WithTimeout(ctx, validateCmdTimeout)
	valStart := time.Now()
	_, err = runWithCtx(valCtx, &out, &errOut, "validate", "-json", "-no-color")
	valDur := time.Since(valStart).Milliseconds()
	valCancel()
	if err != nil {
		return nil, err
	}
	var vj struct {
		Valid        bool `json:"valid"`
		ErrorCount   int  `json:"error_count"`
		WarningCount int  `json:"warning_count"`
		Diagnostics  []struct {
			Severity string `json:"severity"`
			Summary  string `json:"summary"`
			Detail   string `json:"detail"`
			Range    *struct {
				Filename string `json:"filename"`
				Start    struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(out.Bytes(), &vj); err != nil {
		return nil, fmt.Errorf("unexpected validate output: %s", TailClip(inspector.RedactAgentSecrets(out.String()+errOut.String()), toolOutputMax))
	}
	res := &ValidateResult{
		OK:                 vj.Valid,
		ErrorCount:         vj.ErrorCount,
		WarningCount:       vj.WarningCount,
		InitDurationMs:     initDur,
		ValidateDurationMs: valDur,
		Diagnostics:        []Diagnostic{},
	}
	for _, d := range vj.Diagnostics {
		x := Diagnostic{
			Severity: d.Severity,
			Summary:  inspector.RedactAgentSecrets(d.Summary),
			Detail:   inspector.RedactAgentSecrets(d.Detail),
		}
		if d.Range != nil {
			x.File, x.Line = filepath.Base(d.Range.Filename), d.Range.Start.Line
		}
		res.Diagnostics = append(res.Diagnostics, x)
	}
	if res.ErrorCount == 0 && res.WarningCount == 0 && len(res.Diagnostics) > 0 {
		for _, d := range res.Diagnostics {
			if d.Severity == "error" {
				res.ErrorCount++
			} else if d.Severity == "warning" {
				res.WarningCount++
			}
		}
	}
	return res, nil
}
