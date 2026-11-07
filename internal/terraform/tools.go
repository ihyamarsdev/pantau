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
)

const (
	fmtFileTimeout  = 20 * time.Second
	validateTimeout = 120 * time.Second
	toolOutputMax   = 4000
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
	OK          bool         `json:"ok"`
	InitError   string       `json:"init_error"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func lookTool(binary string) (string, error) {
	if binary != "terraform" && binary != "tofu" {
		return "", fmt.Errorf("invalid binary %q, must be terraform or tofu", binary)
	}
	p, err := exec.LookPath(binary)
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
// InitError (ok=false); only execution problems return an error.
func ValidateConfig(ctx context.Context, binary, dataRoot string, files map[string]string) (*ValidateResult, error) {
	if err := ValidateFiles(files); err != nil {
		return nil, err
	}
	bin, err := lookTool(binary)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()

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

	run := func(out, errOut *bytes.Buffer, args ...string) (int, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env = work, env
		cmd.Stdout, cmd.Stderr = out, errOut
		return runCmd(ctx, cmd)
	}

	var out, errOut bytes.Buffer
	code, err := run(&out, &out, "init", "-backend=false", "-input=false", "-no-color")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return &ValidateResult{InitError: clip(out.String()), Diagnostics: []Diagnostic{}}, nil
	}

	out.Reset()
	if _, err := run(&out, &errOut, "validate", "-json", "-no-color"); err != nil {
		return nil, err
	}
	var vj struct {
		Valid       bool `json:"valid"`
		Diagnostics []struct {
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
		return nil, fmt.Errorf("unexpected validate output: %s", clip(out.String()+errOut.String()))
	}
	res := &ValidateResult{OK: vj.Valid, Diagnostics: []Diagnostic{}}
	for _, d := range vj.Diagnostics {
		x := Diagnostic{Severity: d.Severity, Summary: d.Summary, Detail: d.Detail}
		if d.Range != nil {
			x.File, x.Line = filepath.Base(d.Range.Filename), d.Range.Start.Line
		}
		res.Diagnostics = append(res.Diagnostics, x)
	}
	return res, nil
}
