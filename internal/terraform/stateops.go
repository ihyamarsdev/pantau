package terraform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

const addrSeg = `[A-Za-z_][A-Za-z0-9_-]*(?:\[(?:[0-9]+|"[A-Za-z0-9_./:@=+,-]*")\])*`

var (
	// Resource/module address with optional indexes: module.a["k"].aws_x.y[0]
	stateAddrRe = regexp.MustCompile(`^` + addrSeg + `(?:\.` + addrSeg + `)*$`)
	importIDRe  = regexp.MustCompile(`^[A-Za-z0-9_/:@.][A-Za-z0-9_./:@=+,%~#-]*$`)
)

// ValidateStateAddress rejects anything that is not a plain resource address.
func ValidateStateAddress(addr string) error {
	if len(addr) == 0 || len(addr) > 512 || !stateAddrRe.MatchString(addr) {
		return fmt.Errorf("invalid resource address %q", addr)
	}
	return nil
}

// ValidateImportID rejects empty, option-like or metachar-bearing import ids.
func ValidateImportID(id string) error {
	if len(id) == 0 || len(id) > 512 || !importIDRe.MatchString(id) {
		return fmt.Errorf("invalid import id %q", id)
	}
	return nil
}

// StateOpSpec describes one state operation: rm | mv | import.
type StateOpSpec struct {
	Config
	Op       string
	Address  string
	To       string // mv destination
	ImportID string // import id
	Timeout  time.Duration
}

// StateOpResult carries the redacted command output and the resulting state file.
type StateOpResult struct {
	ExitCode int
	Output   string
	State    []byte
}

// Description is the human summary stored with the new state version.
func (s StateOpSpec) Description() string {
	switch s.Op {
	case "mv":
		return "state mv " + s.Address + " " + s.To
	case "import":
		return "import " + s.Address
	}
	return "state " + s.Op + " " + s.Address
}

func (s StateOpSpec) Validate() error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	return s.ValidateRequest()
}

// ValidateRequest checks op and addresses only (no Config), for early API rejection.
func (s StateOpSpec) ValidateRequest() error {
	if err := ValidateStateAddress(s.Address); err != nil {
		return err
	}
	switch s.Op {
	case "rm":
	case "mv":
		if err := ValidateStateAddress(s.To); err != nil {
			return err
		}
		if s.To == s.Address {
			return errors.New("source and destination addresses are identical")
		}
	case "import":
		return ValidateImportID(s.ImportID)
	default:
		return fmt.Errorf("invalid op %q", s.Op)
	}
	return nil
}

// StateOp runs the operation against Config.State and returns the new state file.
// rm/mv are state-only (no init); import goes through the plan pipeline
// (files/git, vars, cloud env, init, backend override).
func StateOp(ctx context.Context, spec StateOpSpec) (*StateOpResult, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	binPath, err := exec.LookPath(spec.Binary)
	if err != nil {
		return nil, fmt.Errorf("%s binary not found in PATH: %w", spec.Binary, err)
	}
	if spec.Op != "import" && len(spec.State) == 0 {
		return nil, errors.New("workspace has no state")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var out bytes.Buffer
	rLog := newRedactionWriter(&out, spec.logSecrets())

	for _, d := range []string{filepath.Join(spec.Root, "home"), filepath.Join(spec.Root, "data"), filepath.Join(spec.DataRoot, "plugin-cache")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, fmt.Errorf("failed to create %s: %w", filepath.Base(d), err)
		}
	}
	stateFile := filepath.Join(spec.Root, "terraform.tfstate")
	if len(spec.State) > 0 {
		err = os.WriteFile(stateFile, spec.State, 0600)
	} else if err = os.Remove(stateFile); os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to prepare state file: %w", err)
	}

	envCfg, cleanupEnv, err := spec.Config.withEnvFiles()
	if err != nil {
		return nil, err
	}
	defer cleanupEnv()
	if spec.Op != "import" {
		// Configless state commands must not see the backend recorded by earlier runs.
		dataDir, err := os.MkdirTemp(spec.Root, "statedata-")
		if err != nil {
			return nil, fmt.Errorf("failed to create data dir: %w", err)
		}
		defer os.RemoveAll(dataDir)
		env := map[string]string{"TF_DATA_DIR": dataDir}
		for k, v := range envCfg.Env {
			env[k] = v
		}
		envCfg.Env = env
	}
	env := envCfg.prepareEnv()

	res := &StateOpResult{}
	run := func(dir string, args ...string) (bool, error) {
		cmd := exec.Command(binPath, args...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, rLog, rLog
		code, err := runCmd(runCtx, cmd)
		res.ExitCode = code
		return code == 0 && err == nil, err
	}
	finish := func(err error) (*StateOpResult, error) {
		res.Output = out.String()
		if err != nil {
			return nil, err
		}
		if res.ExitCode == 0 {
			res.State, _ = os.ReadFile(stateFile)
		}
		return res, nil
	}

	switch spec.Op {
	case "rm":
		_, err = run(spec.Root, "state", "rm", "-state="+stateFile, spec.Address)
	case "mv":
		_, err = run(spec.Root, "state", "mv", "-state="+stateFile, spec.Address, spec.To)
	default:
		err = spec.runImport(runCtx, rLog, run)
	}
	return finish(err)
}
