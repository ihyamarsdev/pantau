package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const MaxConcurrentRuns = 3

var (
	availMu    sync.RWMutex
	cachedPath string
	cachedVer  string
	cachedErr  error
	cachedAt   time.Time
	cacheTTL   = 60 * time.Second // scheduler ticks every 15s; a shorter TTL re-spawned ansible-playbook --version each tick
	tagsRegex  = regexp.MustCompile(`^[A-Za-z0-9_.,:-]{1,256}$`)
	limitRegex = regexp.MustCompile(`^[A-Za-z0-9_.,:!&*\[\]-]{1,256}$`)
)

// ValidatePattern checks an Ansible host pattern (--limit) against the allowed charset. Empty is valid.
func ValidatePattern(p string) error {
	if p == "" {
		return nil
	}
	if !limitRegex.MatchString(p) {
		return fmt.Errorf("invalid limit %q: must match %s", p, limitRegex.String())
	}
	return nil
}

// Available reports the ansible-playbook binary in PATH and the first line of `ansible-playbook --version`.
// Result is cached with a 5s timeout during execution. On Windows it always errors.
func Available() (path, version string, err error) {
	if runtime.GOOS == "windows" {
		return "", "", errors.New("ansible controller is not supported on windows")
	}

	availMu.RLock()
	if time.Since(cachedAt) < cacheTTL && (cachedPath != "" || cachedErr != nil) {
		p, v, e := cachedPath, cachedVer, cachedErr
		availMu.RUnlock()
		return p, v, e
	}
	availMu.RUnlock()

	availMu.Lock()
	defer availMu.Unlock()

	// Double check after lock
	if time.Since(cachedAt) < cacheTTL && (cachedPath != "" || cachedErr != nil) {
		return cachedPath, cachedVer, cachedErr
	}

	p, err := exec.LookPath("ansible-playbook")
	if err != nil {
		cachedPath, cachedVer, cachedErr = "", "", err
		cachedAt = time.Now()
		return "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, p, "--version").Output()
	if err != nil {
		cachedPath, cachedVer, cachedErr = p, "", err
		cachedAt = time.Now()
		return p, "", err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	v := ""
	if len(lines) > 0 {
		v = strings.TrimSpace(lines[0])
	}

	cachedPath, cachedVer, cachedErr = p, v, nil
	cachedAt = time.Now()
	return p, v, nil
}

type Target struct {
	ID         int64
	Name       string // nama Host di Pantau -> basis alias
	Address    string // ansible_host
	Port       int
	User       string
	Group      string // Host Group (boleh kosong)
	PrivateKey string // PEM efektif (store.ResolveHostPrivateKey)
}

type RunSpec struct {
	Playbook   string // isi YAML
	Targets    []Target
	Mode       string // "run" | "check" (--check) | "diff" (--check --diff) | "syntax" (--syntax-check)
	ExtraVars  string // "" atau objek JSON
	Tags       string // koma
	SkipTags   string
	Limit      string           // --limit host pattern, "" = all targets
	Verbosity  int              // 0..4 -> -v..-vvvv
	Become     bool             // --become (sudo tanpa password)
	Forks      int              // 0 => forks from ansible.cfg (Ansible default 5)
	Timeout    time.Duration    // 0 => 1h
	Git        *GitSource       // non-nil: playbook comes from this repository, Playbook is ignored
	Assets     []Asset          // inline source only: templates/ and files/ written next to the playbook (a Git repo brings its own)
	Config     string           // validated user additions to ansible.cfg (Ansible Settings), see ParseConfig
	LibraryDir string           // shared role/collection library; "" disables it
	OnCommit   func(sha string) // called once with the checked-out commit (git source only)
}

func (s RunSpec) Validate() error {
	switch s.Mode {
	case "run", "check", "diff", "syntax":
	case "":
		return errors.New("mode is required")
	default:
		return fmt.Errorf("invalid mode %q: must be run, check, diff, or syntax", s.Mode)
	}

	if s.Mode == "syntax" {
		if len(s.Targets) > 200 {
			return errors.New("targets count exceeds maximum of 200")
		}
	} else {
		if len(s.Targets) == 0 {
			return errors.New("at least one target is required")
		}
		if len(s.Targets) > 200 {
			return errors.New("targets count exceeds maximum of 200")
		}
	}

	if s.Git != nil {
		if err := s.Git.Validate(); err != nil {
			return err
		}
	} else {
		// Playbook <= 256 KiB, and at least 1 byte
		if len(s.Playbook) == 0 {
			return errors.New("playbook content is required")
		}
		if len(s.Playbook) > 256*1024 {
			return errors.New("playbook content exceeds 256 KiB")
		}
	}

	var assetBytes int
	for _, a := range s.Assets {
		if err := ValidateAsset(a); err != nil {
			return err
		}
		assetBytes += len(a.Data)
	}
	if assetBytes > MaxAssetsTotalBytes {
		return errors.New("templates and files exceed 32 MiB in total")
	}

	// Verbosity 0..4
	if s.Verbosity < 0 || s.Verbosity > 4 {
		return fmt.Errorf("verbosity %d out of range (0..4)", s.Verbosity)
	}

	if err := ValidatePattern(s.Limit); err != nil {
		return err
	}

	// Forks 0..20 (0 defers to ansible.cfg)
	if s.Forks < 0 || s.Forks > 20 {
		return fmt.Errorf("forks %d out of range (0..20)", s.Forks)
	}
	if _, err := ParseConfig(s.Config); err != nil {
		return fmt.Errorf("invalid ansible config: %w", err)
	}

	// Tags & SkipTags validation
	if s.Tags != "" && !tagsRegex.MatchString(s.Tags) {
		return fmt.Errorf("invalid tags %q: must match %s", s.Tags, tagsRegex.String())
	}
	if s.SkipTags != "" && !tagsRegex.MatchString(s.SkipTags) {
		return fmt.Errorf("invalid skip_tags %q: must match %s", s.SkipTags, tagsRegex.String())
	}

	// ExtraVars: "" or valid JSON object <= 64 KiB
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
