package ansible

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Run creates a temporary workspace, writes playbook.yml, inventory.json, ansible.cfg, extra.json, key files,
// runs `ansible-playbook`, streams combined stdout+stderr live to log, and ALWAYS cleans up the workspace.
// If ctx is cancelled, the entire process group is killed (SIGTERM then SIGKILL after 5s) and err = ctx.Err().
// exitCode is the exit status (0 = success, 2 = host failed, etc.).
// err != nil only for execution failure / validation / timeout / cancellation.
func Run(ctx context.Context, spec RunSpec, log io.Writer) (exitCode int, err error) {
	if err := spec.Validate(); err != nil {
		return 0, err
	}

	binPath, err := exec.LookPath("ansible-playbook")
	if err != nil {
		return 0, fmt.Errorf("ansible-playbook not found in PATH: %w", err)
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 1 * time.Hour
	}

	runCtx, runCancel := context.WithTimeout(ctx, timeout)
	defer runCancel()

	// Create workspace 0700
	workDir, err := os.MkdirTemp("", "pantau-ansible-*")
	if err != nil {
		return 0, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	_ = os.Chmod(workDir, 0700)
	defer os.RemoveAll(workDir)

	// Keys directory inside workspace
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
		if strings.TrimSpace(t.PrivateKey) != "" {
			kp := filepath.Join(keysDir, alias+".key")
			content := strings.TrimSpace(t.PrivateKey) + "\n"
			if err := os.WriteFile(kp, []byte(content), 0600); err != nil {
				return 0, fmt.Errorf("failed to write key file: %w", err)
			}
			_ = os.Chmod(kp, 0600)
		}
	}

	// Write inventory.json
	invPath := filepath.Join(workDir, "inventory.json")
	if err := os.WriteFile(invPath, invBytes, 0600); err != nil {
		return 0, fmt.Errorf("failed to write inventory.json: %w", err)
	}

	// Write playbook.yml (inline source) or clone the repository (git source)
	playbookPath := filepath.Join(workDir, "playbook.yml")
	runDir := workDir
	if spec.Git != nil {
		repoDir, fullPath, sha, err := cloneGit(runCtx, *spec.Git, workDir, "https:ssh", log)
		if err != nil {
			return 0, err
		}
		runDir, playbookPath = repoDir, fullPath
		if spec.OnCommit != nil {
			spec.OnCommit(sha)
		}
	} else if err := os.WriteFile(playbookPath, []byte(spec.Playbook), 0600); err != nil {
		return 0, fmt.Errorf("failed to write playbook.yml: %w", err)
	}

	// Write ansible.cfg
	// temporary ansible.cfg incl. host_key_checking=False/retry_files_enabled=False/nocows/ANSIBLE_FORCE_COLOR=0/ANSIBLE_HOME in tmp
	ansibleCfgPath := filepath.Join(workDir, "ansible.cfg")
	// -F /dev/null: OpenSSH reads ~/.ssh/config from the passwd home (not $HOME) and aborts on "Bad owner or
	// permissions" for bind-mounted configs (Docker). Hosts are fully described by the generated inventory.
	// No ControlPersist: a lingering mux master would outlive the run and the workspace.
	ansibleCfgContent := `[defaults]
host_key_checking = False
retry_files_enabled = False
nocows = 1

[ssh_connection]
ssh_args = -F /dev/null -o UserKnownHostsFile=/dev/null
`
	if err := os.WriteFile(ansibleCfgPath, []byte(ansibleCfgContent), 0600); err != nil {
		return 0, fmt.Errorf("failed to write ansible.cfg: %w", err)
	}

	// Build arguments
	args := []string{"-i", invPath}

	switch spec.Mode {
	case "check":
		args = append(args, "--check")
	case "diff":
		args = append(args, "--check", "--diff")
	case "syntax":
		args = append(args, "--syntax-check")
	case "run":
		// default run
	}

	forks := spec.Forks
	if forks <= 0 {
		forks = 5
	}
	args = append(args, "-f", strconv.Itoa(forks))

	if spec.Verbosity > 0 {
		v := spec.Verbosity
		if v > 3 {
			v = 3
		}
		args = append(args, "-"+strings.Repeat("v", v))
	}

	if spec.Become {
		args = append(args, "--become")
	}

	if spec.Tags != "" {
		args = append(args, "--tags", spec.Tags)
	}
	if spec.SkipTags != "" {
		args = append(args, "--skip-tags", spec.SkipTags)
	}

	if strings.TrimSpace(spec.ExtraVars) != "" {
		extraPath := filepath.Join(workDir, "extra.json")
		if err := os.WriteFile(extraPath, []byte(spec.ExtraVars), 0600); err != nil {
			return 0, fmt.Errorf("failed to write extra.json: %w", err)
		}
		args = append(args, "-e", "@"+extraPath)
	}

	args = append(args, playbookPath)

	// Build clean environment
	// Clean env: PATH, LANG=C.UTF-8, HOME=<tmp>, HTTP_PROXY, HTTPS_PROXY, NO_PROXY, + ANSIBLE_*
	// ANSIBLE_CONFIG points to ansible.cfg, ANSIBLE_FORCE_COLOR=0, ANSIBLE_HOME in tmp
	ansibleHome := filepath.Join(workDir, ".ansible")
	_ = os.MkdirAll(ansibleHome, 0700)

	cleanEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"HOME=" + workDir,
		"ANSIBLE_CONFIG=" + ansibleCfgPath,
		"ANSIBLE_FORCE_COLOR=0",
		"ANSIBLE_HOME=" + ansibleHome,
	}

	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if val, ok := os.LookupEnv(k); ok {
			cleanEnv = append(cleanEnv, k+"="+val)
		}
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = runDir
	cmd.Env = cleanEnv
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	}
	return runCmd(runCtx, cmd)
}

// runCmd runs cmd in its own process group. When ctx ends the whole group is killed and ctx.Err() is returned
// (context.Canceled for a user cancel, context.DeadlineExceeded for the run timeout). A non-zero exit is reported
// through the exit code, not the error.
func runCmd(ctx context.Context, cmd *exec.Cmd) (int, error) {
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", filepath.Base(cmd.Path), err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-waitDone
		return 0, ctx.Err()
	case waitErr := <-waitDone:
		if waitErr == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, waitErr
	}
}
