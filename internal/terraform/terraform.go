package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"pantau/internal/ansible"
)

const MaxConcurrentRuns = 3

var (
	varKeyRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
	fileNameRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)
	targetAddrRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+(\[[A-Za-z0-9_.:"-]+\])?(\.[A-Za-z0-9_.-]+(\[[A-Za-z0-9_.:"-]+\])?)*$`)
)

var allowedExtensions = map[string]bool{
	".tf":      true,
	".tf.json": true,
	".tfvars":  true,
	".tftpl":   true,
	".json":    true,
	".yaml":    true,
	".yml":     true,
	".txt":     true,
	".md":      true,
}

type Binary struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Err     string `json:"err,omitempty"`
}

type GitSource struct {
	URL        string `json:"url"`
	Branch     string `json:"branch"`
	Token      string `json:"token"`
	PrivateKey string `json:"private_key"`
}

type Var struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	HCL       bool   `json:"hcl"`
	Sensitive bool   `json:"sensitive"`
}

type Config struct {
	Binary   string            `json:"binary"`
	DataRoot string            `json:"data_root"`
	Root     string            `json:"root"`
	Files    map[string]string `json:"files,omitempty"`
	Git      *GitSource        `json:"git,omitempty"`
	WorkDir  string            `json:"work_dir,omitempty"`
	Vars     []Var             `json:"vars,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	// PlainEnv lists Env keys whose values are not secret and stay visible in logs.
	PlainEnv map[string]bool `json:"plain_env,omitempty"`
	// EnvFiles maps env key -> file content; the executor writes each to a 0600 file
	// under Root/secrets for the duration of an invocation and sets the key to its path.
	EnvFiles map[string]string `json:"-"`
	State    []byte            `json:"state,omitempty"`
}

var (
	detectMu    sync.Mutex
	detectCache []Binary
	detectTime  time.Time
)

// Detect returns availability and version for terraform and tofu.
// Results are cached for 60s. On Windows, Err indicates it is not supported.
func Detect() []Binary {
	detectMu.Lock()
	defer detectMu.Unlock()

	if time.Since(detectTime) < 60*time.Second && len(detectCache) == 2 {
		out := make([]Binary, 2)
		copy(out, detectCache)
		return out
	}

	names := []string{"terraform", "tofu"}
	res := make([]Binary, 2)

	for i, name := range names {
		b := Binary{Name: name}
		if runtime.GOOS == "windows" {
			b.Err = "terraform is not supported on windows"
			res[i] = b
			continue
		}

		p, err := exec.LookPath(name)
		if err != nil {
			b.Err = fmt.Sprintf("%s not found in PATH", name)
			res[i] = b
			continue
		}
		b.Path = p

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, p, "version").Output()
		cancel()
		if err != nil {
			b.Err = fmt.Sprintf("failed to get %s version: %v", name, err)
			res[i] = b
			continue
		}

		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 {
			b.Version = strings.TrimSpace(lines[0])
		}
		res[i] = b
	}

	detectCache = make([]Binary, 2)
	copy(detectCache, res)
	detectTime = time.Now()
	return res
}

// Infracost detects infracost binary path and version.
func Infracost() (path string, version string, err error) {
	if runtime.GOOS == "windows" {
		return "", "", errors.New("infracost is not supported on windows")
	}
	p, err := exec.LookPath("infracost")
	if err != nil {
		return "", "", errors.New("infracost not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, "--version").Output()
	if err != nil {
		return p, "", fmt.Errorf("failed to run infracost --version: %w", err)
	}
	return p, strings.TrimSpace(string(out)), nil
}

func (c Config) Validate() error {
	if c.Binary != "terraform" && c.Binary != "tofu" {
		return fmt.Errorf("invalid binary %q, must be terraform or tofu", c.Binary)
	}
	if strings.TrimSpace(c.DataRoot) == "" {
		return errors.New("data_root is required")
	}
	if strings.TrimSpace(c.Root) == "" {
		return errors.New("root is required")
	}
	if c.Files != nil && c.Git != nil {
		return errors.New("cannot specify both Files and Git")
	}
	if c.Files == nil && c.Git == nil {
		return errors.New("either Files or Git must be specified")
	}
	if c.Files != nil {
		if err := ValidateFiles(c.Files); err != nil {
			return err
		}
	}
	if c.Git != nil {
		if err := ansible.ValidateGitURL(c.Git.URL); err != nil {
			return fmt.Errorf("git url: %w", err)
		}
		if err := ansible.ValidateGitBranch(c.Git.Branch); err != nil {
			return fmt.Errorf("git branch: %w", err)
		}
		if c.Git.Token != "" && ansible.GitURLScheme(c.Git.URL) != "https" {
			return errors.New("token authentication requires an https:// git url")
		}
	}
	if c.WorkDir != "" {
		if strings.HasPrefix(c.WorkDir, "/") || strings.Contains(c.WorkDir, "..") || path.Clean(c.WorkDir) != c.WorkDir {
			return fmt.Errorf("invalid work_dir %q: must be a clean relative path", c.WorkDir)
		}
	}
	for _, v := range c.Vars {
		if !varKeyRe.MatchString(v.Key) {
			return fmt.Errorf("invalid variable key %q", v.Key)
		}
	}
	return nil
}

func ValidateFiles(files map[string]string) error {
	if len(files) > 50 {
		return fmt.Errorf("too many files: got %d, max allowed is 50", len(files))
	}
	for name, content := range files {
		if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") || !fileNameRe.MatchString(name) {
			return fmt.Errorf("invalid file name %q", name)
		}
		// check allowed extensions
		matchedExt := false
		for ext := range allowedExtensions {
			if strings.HasSuffix(name, ext) {
				matchedExt = true
				break
			}
		}
		if !matchedExt {
			return fmt.Errorf("file %q has disallowed extension", name)
		}
		if len(content) > 256*1024 {
			return fmt.Errorf("file %q exceeds 256 KiB size limit", name)
		}
	}
	return nil
}

// prepareEnv builds the sanitized environment variables for terraform/tofu executions.
func (c Config) prepareEnv() []string {
	cleanEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"HOME=" + filepath.Join(c.Root, "home"),
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"CHECKPOINT_DISABLE=1",
		"TF_PLUGIN_CACHE_DIR=" + filepath.Join(c.DataRoot, "plugin-cache"),
		"TF_DATA_DIR=" + filepath.Join(c.Root, "data"),
	}

	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if val, ok := os.LookupEnv(k); ok {
			cleanEnv = append(cleanEnv, k+"="+val)
		}
	}

	// Workspace / varset environment variables
	for k, v := range c.Env {
		cleanEnv = append(cleanEnv, k+"="+v)
	}
	return cleanEnv
}

// writeVarFiles writes non-HCL vars to pantau.auto.tfvars.json and HCL vars to pantau-hcl.auto.tfvars.
func writeVarFiles(dir string, vars []Var) error {
	jsonVars := make(map[string]any)
	var hclLines []string

	for _, v := range vars {
		if v.HCL {
			hclLines = append(hclLines, fmt.Sprintf("%s = %s", v.Key, v.Value))
		} else {
			jsonVars[v.Key] = v.Value
		}
	}

	if len(jsonVars) > 0 {
		b, err := json.MarshalIndent(jsonVars, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode vars to json: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pantau.auto.tfvars.json"), b, 0600); err != nil {
			return fmt.Errorf("failed to write pantau.auto.tfvars.json: %w", err)
		}
	}

	if len(hclLines) > 0 {
		content := strings.Join(hclLines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "pantau-hcl.auto.tfvars"), []byte(content), 0600); err != nil {
			return fmt.Errorf("failed to write pantau-hcl.auto.tfvars: %w", err)
		}
	}
	return nil
}

func RunDirExists(root string, runID int64) bool {
	runDir := filepath.Join(root, "runs", fmt.Sprintf("%d", runID))
	st, err := os.Stat(runDir)
	return err == nil && st.IsDir()
}

func CleanupRun(root string, runID int64) {
	runDir := filepath.Join(root, "runs", fmt.Sprintf("%d", runID))
	_ = os.RemoveAll(runDir)
}

func ForceUnlockFiles(root string) {
	// Best-effort cleanup of lingering lock info files in root or runs
	_ = os.Remove(filepath.Join(root, ".terraform.tfstate.lock.info"))
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info != nil && info.Name() == ".terraform.tfstate.lock.info" {
			_ = os.Remove(p)
		}
		return nil
	})
}
