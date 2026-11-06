package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAdhocTimeout = 10 * time.Minute
	maxAdhocResults     = 1000 // bound for a pathological log
)

// AdhocSpec runs one module against already-resolved targets, without a playbook.
type AdhocSpec struct {
	Module     string   // "ping", "ansible.builtin.shell", ...
	Args       string   // single -a argument, "" allowed
	Pattern    string   // host pattern, "" => "all"
	Targets    []Target // never empty (a Pattern alone cannot escape the Generated Inventory)
	Become     bool
	Forks      int
	Verbosity  int // 0..4
	ExtraVars  string
	Timeout    time.Duration // 0 => 10m
	Config     string
	LibraryDir string
}

var adhocModuleRegex = regexp.MustCompile(`^[A-Za-z0-9_.]{1,80}$`)

func (s AdhocSpec) Validate() error {
	if strings.HasPrefix(s.Module, "-") || !adhocModuleRegex.MatchString(s.Module) {
		return fmt.Errorf("invalid module %q: must match %s", s.Module, adhocModuleRegex.String())
	}
	if len(s.Args) > 4096 {
		return errors.New("args exceeds 4 KiB")
	}
	if strings.ContainsAny(s.Args, "\x00\r\n") {
		return errors.New("args must not contain NUL or line breaks")
	}
	if err := ValidatePattern(s.Pattern); err != nil {
		return err
	}
	if len(s.Targets) == 0 {
		return errors.New("at least one target is required")
	}
	if len(s.Targets) > 200 {
		return errors.New("targets count exceeds maximum of 200")
	}
	if s.Verbosity < 0 || s.Verbosity > 4 {
		return fmt.Errorf("verbosity %d out of range (0..4)", s.Verbosity)
	}
	if s.Forks < 0 || s.Forks > 20 {
		return fmt.Errorf("forks %d out of range (0..20)", s.Forks)
	}
	if _, err := ParseConfig(s.Config); err != nil {
		return fmt.Errorf("invalid ansible config: %w", err)
	}
	if s.ExtraVars != "" {
		if len(s.ExtraVars) > 64*1024 {
			return errors.New("extra_vars exceeds 64 KiB")
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(s.ExtraVars), &obj); err != nil {
			return errors.New("extra_vars must be a valid JSON object")
		}
	}
	return nil
}

// RunAdhoc runs `ansible <pattern> -m <module> [-a args] -i inventory` with the same workspace, key files, clean
// environment, ansible.cfg and guaranteed cleanup as Run. ANSI is kept (the UI renders it).
func RunAdhoc(ctx context.Context, spec AdhocSpec, log io.Writer) (int, error) {
	if err := spec.Validate(); err != nil {
		return 0, err
	}
	binPath, err := exec.LookPath("ansible")
	if err != nil {
		return 0, fmt.Errorf("ansible not found in PATH: %w", err)
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultAdhocTimeout
	}
	runCtx, runCancel := context.WithTimeout(ctx, timeout)
	defer runCancel()

	workDir, err := os.MkdirTemp("", "pantau-adhoc-*")
	if err != nil {
		return 0, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	_ = os.Chmod(workDir, 0700)
	defer os.RemoveAll(workDir)
	keysDir := filepath.Join(workDir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return 0, fmt.Errorf("failed to create keys dir: %w", err)
	}

	targetByID := make(map[int64]Target, len(spec.Targets))
	for _, t := range spec.Targets {
		targetByID[t.ID] = t
	}
	invBytes, aliasMap, err := InventoryJSON(spec.Targets, func(alias string) string {
		return filepath.Join(keysDir, alias+".key")
	})
	if err != nil {
		return 0, fmt.Errorf("failed to generate inventory: %w", err)
	}
	for alias, tid := range aliasMap {
		t := targetByID[tid]
		if strings.TrimSpace(t.PrivateKey) == "" {
			continue
		}
		kp := filepath.Join(keysDir, alias+".key")
		if err := os.WriteFile(kp, []byte(strings.TrimSpace(t.PrivateKey)+"\n"), 0600); err != nil {
			return 0, fmt.Errorf("failed to write key file: %w", err)
		}
		_ = os.Chmod(kp, 0600)
	}
	invPath := filepath.Join(workDir, "inventory.json")
	if err := os.WriteFile(invPath, invBytes, 0600); err != nil {
		return 0, fmt.Errorf("failed to write inventory.json: %w", err)
	}

	cfgPath := filepath.Join(workDir, "ansible.cfg")
	cfgContent, err := BuildConfig(spec.Config)
	if err != nil {
		return 0, fmt.Errorf("invalid ansible config: %w", err)
	}
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		return 0, fmt.Errorf("failed to write ansible.cfg: %w", err)
	}

	pattern := spec.Pattern
	if pattern == "" {
		pattern = "all"
	}
	args := []string{pattern, "-i", invPath, "-m", spec.Module}
	if spec.Args != "" {
		args = append(args, "-a", spec.Args)
	}
	if spec.Forks > 0 {
		args = append(args, "-f", strconv.Itoa(spec.Forks))
	}
	if spec.Verbosity > 0 {
		args = append(args, "-"+strings.Repeat("v", spec.Verbosity))
	}
	if spec.Become {
		args = append(args, "--become")
	}
	if spec.ExtraVars != "" {
		extraPath := filepath.Join(workDir, "extra.json")
		if err := os.WriteFile(extraPath, []byte(spec.ExtraVars), 0600); err != nil {
			return 0, fmt.Errorf("failed to write extra.json: %w", err)
		}
		args = append(args, "-e", "@"+extraPath)
	}

	ansibleHome := filepath.Join(workDir, ".ansible")
	_ = os.MkdirAll(ansibleHome, 0700)
	cleanEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"HOME=" + workDir,
		"ANSIBLE_CONFIG=" + cfgPath,
		"ANSIBLE_FORCE_COLOR=1",
		"ANSIBLE_HOME=" + ansibleHome,
	}
	var rolesPath, collectionsPath []string
	if spec.LibraryDir != "" {
		rolesPath = append(rolesPath, LibraryRolesDir(spec.LibraryDir))
		collectionsPath = append(collectionsPath, LibraryCollectionsDir(spec.LibraryDir))
	}
	if len(rolesPath) > 0 {
		rolesPath = append(rolesPath, filepath.Join(ansibleHome, "roles"), "/usr/share/ansible/roles", "/etc/ansible/roles")
		collectionsPath = append(collectionsPath, filepath.Join(ansibleHome, "collections"), "/usr/share/ansible/collections")
		cleanEnv = append(cleanEnv,
			"ANSIBLE_ROLES_PATH="+strings.Join(rolesPath, string(os.PathListSeparator)),
			"ANSIBLE_COLLECTIONS_PATH="+strings.Join(collectionsPath, string(os.PathListSeparator)),
		)
	}
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if val, ok := os.LookupEnv(k); ok {
			cleanEnv = append(cleanEnv, k+"="+val)
		}
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = workDir
	cmd.Env = cleanEnv
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	}
	return runCmd(runCtx, cmd)
}

// AdhocResult is one host's outcome from the default callback.
type AdhocResult struct {
	Alias   string `json:"alias"`
	Status  string `json:"status"` // success | changed | unreachable | failed
	Summary string `json:"summary"`
}

// Adhoc statuses.
const (
	AdhocSuccess     = "success"
	AdhocChanged     = "changed"
	AdhocUnreachable = "unreachable"
	AdhocFailed      = "failed"
)

var adhocANSIRegex = regexp.MustCompile("\x1b\\[[0-9;]*m")

var adhocHeaderRegex = regexp.MustCompile(`^([A-Za-z0-9_.-]+) \| (SUCCESS|FAILED|UNREACHABLE|CHANGED|SKIPPED)(!?) (?:\||=>|>)`)

// ParseAdhoc reads the default callback's per-host blocks (`alias | CHANGED | rc=0 >>` followed by the output) into
// one result per host. The header decides the status; the body (up to the next header, without the JSON blob of a
// module that returns structured data) is the summary. ANSI is stripped first so colored logs parse identically.
func ParseAdhoc(log string) []AdhocResult {
	results := make([]AdhocResult, 0)
	body := []string{}
	flush := func() {
		if n := len(results); n > 0 {
			results[n-1].Summary = adhocSummary(body)
		}
		body = body[:0]
	}
	for _, line := range strings.Split(adhocANSIRegex.ReplaceAllString(log, ""), "\n") {
		if m := adhocHeaderRegex.FindStringSubmatch(line); m != nil {
			if len(results) >= maxAdhocResults {
				break
			}
			flush()
			results = append(results, AdhocResult{Alias: m[1], Status: adhocStatus(m[2])})
			continue
		}
		if len(results) > 0 {
			body = append(body, line)
		}
	}
	flush()
	return results
}

func adhocStatus(s string) string {
	switch s {
	case "SUCCESS":
		return AdhocSuccess
	case "CHANGED":
		return AdhocChanged
	case "UNREACHABLE":
		return AdhocUnreachable
	}
	return AdhocFailed
}

// adhocSummary picks the first line an operator cares about. Raw module output is reported as printed; a future
// module's structured result (printed one field per line) is scanned for the top-level "msg" every module returns.
func adhocSummary(lines []string) string {
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "}") {
			continue
		}
		if strings.HasPrefix(t, "{") || strings.HasPrefix(t, `"`) {
			break
		}
		return truncateLine(t)
	}
	block := strings.Join(lines, "\n")
	if pyStart := strings.Index(block, `{'`); pyStart >= 0 {
		return truncateLine(jsonMsg(strings.ReplaceAll(block[pyStart:], "'", `"`))) // ansible's own debug dict
	}
	body := strings.TrimSpace(block)
	body = strings.TrimSpace(strings.TrimPrefix(body, "{")) // the module already printed the opening brace
	body = strings.TrimSpace(strings.TrimSuffix(body, "}"))
	return truncateLine(jsonMsg("{" + body + "}"))
}

// jsonMsg reads the top-level "msg" of a structured module result, "" when there is none.
func jsonMsg(doc string) string {
	var res struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal([]byte(doc), &res) != nil {
		return ""
	}
	return res.Msg
}

func truncateLine(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// AdhocCounts tallies a parsed result list.
type AdhocCounts struct {
	Success     int `json:"success"`
	Changed     int `json:"changed"`
	Unreachable int `json:"unreachable"`
	Failed      int `json:"failed"`
}

// CountAdhoc tallies results per status (anything that is not success/changed/unreachable counts as failed).
func CountAdhoc(results []AdhocResult) AdhocCounts {
	var c AdhocCounts
	for _, r := range results {
		switch r.Status {
		case AdhocSuccess:
			c.Success++
		case AdhocChanged:
			c.Changed++
		case AdhocUnreachable:
			c.Unreachable++
		default:
			c.Failed++
		}
	}
	return c
}
