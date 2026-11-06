package ansible

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunSpecValidate(t *testing.T) {
	validTargets := []Target{{ID: 1, Name: "web-1", Address: "10.0.0.1"}}
	validPlaybook := "--- \n- hosts: all\n  tasks: []\n"

	tests := []struct {
		name    string
		spec    RunSpec
		wantErr bool
		errSub  string
	}{
		{
			name: "valid minimal run",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "run",
			},
			wantErr: false,
		},
		{
			name: "valid syntax mode with 0 targets",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  nil,
				Mode:     "syntax",
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "invalid_mode",
			},
			wantErr: true,
			errSub:  "invalid mode",
		},
		{
			name: "empty mode",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "",
			},
			wantErr: true,
			errSub:  "mode is required",
		},
		{
			name: "run mode with 0 targets",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  []Target{},
				Mode:     "run",
			},
			wantErr: true,
			errSub:  "at least one target is required",
		},
		{
			name: "too many targets (> 200)",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  make([]Target, 201),
				Mode:     "run",
			},
			wantErr: true,
			errSub:  "exceeds maximum of 200",
		},
		{
			name: "syntax mode with too many targets (> 200)",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  make([]Target, 201),
				Mode:     "syntax",
			},
			wantErr: true,
			errSub:  "exceeds maximum of 200",
		},
		{
			name: "empty playbook",
			spec: RunSpec{
				Playbook: "",
				Targets:  validTargets,
				Mode:     "run",
			},
			wantErr: true,
			errSub:  "playbook content is required",
		},
		{
			name: "playbook too large (> 256 KiB)",
			spec: RunSpec{
				Playbook: strings.Repeat("a", 256*1024+1),
				Targets:  validTargets,
				Mode:     "run",
			},
			wantErr: true,
			errSub:  "playbook content exceeds 256 KiB",
		},
		{
			name: "invalid verbosity negative",
			spec: RunSpec{
				Playbook:  validPlaybook,
				Targets:   validTargets,
				Mode:      "run",
				Verbosity: -1,
			},
			wantErr: true,
			errSub:  "verbosity",
		},
		{
			name: "invalid verbosity > 4",
			spec: RunSpec{
				Playbook:  validPlaybook,
				Targets:   validTargets,
				Mode:      "run",
				Verbosity: 5,
			},
			wantErr: true,
			errSub:  "verbosity",
		},
		{
			name: "invalid forks negative",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "run",
				Forks:    -1,
			},
			wantErr: true,
			errSub:  "forks",
		},
		{
			name: "invalid forks > 20",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "run",
				Forks:    21,
			},
			wantErr: true,
			errSub:  "forks",
		},
		{
			name: "invalid tags regex",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "run",
				Tags:     "tag1; rm -rf /",
			},
			wantErr: true,
			errSub:  "invalid tags",
		},
		{
			name: "invalid skip_tags regex",
			spec: RunSpec{
				Playbook: validPlaybook,
				Targets:  validTargets,
				Mode:     "run",
				SkipTags: "tag$foo",
			},
			wantErr: true,
			errSub:  "invalid skip_tags",
		},
		{
			name: "extra_vars not an object",
			spec: RunSpec{
				Playbook:  validPlaybook,
				Targets:   validTargets,
				Mode:      "run",
				ExtraVars: `["item1", "item2"]`,
			},
			wantErr: true,
			errSub:  "extra_vars must be a valid JSON object",
		},
		{
			name: "extra_vars invalid json",
			spec: RunSpec{
				Playbook:  validPlaybook,
				Targets:   validTargets,
				Mode:      "run",
				ExtraVars: `{invalid json}`,
			},
			wantErr: true,
			errSub:  "extra_vars must be a valid JSON object",
		},
		{
			name: "extra_vars > 64 KiB",
			spec: RunSpec{
				Playbook:  validPlaybook,
				Targets:   validTargets,
				Mode:      "run",
				ExtraVars: "{" + strings.Repeat(`"a":"b",`, 10000) + `"k":"v"}`,
			},
			wantErr: true,
			errSub:  "extra_vars exceeds 64 KiB",
		},
		{
			name: "valid extra_vars and tags",
			spec: RunSpec{
				Playbook:  validPlaybook,
				Targets:   validTargets,
				Mode:      "diff",
				ExtraVars: `{"ansible_port": 2222, "deploy": true}`,
				Tags:      "setup,deploy",
				SkipTags:  "slow:test-1",
				Verbosity: 2,
				Become:    true,
				Forks:     10,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
				t.Errorf("expected error containing %q, got %q", tt.errSub, err.Error())
			}
		})
	}
}

func TestInventoryJSON(t *testing.T) {
	targets := []Target{
		{ID: 1, Name: "web 01", Address: "192.168.1.10", Port: 22, User: "root", Group: "web servers", PrivateKey: "KEY1"},
		{ID: 2, Name: "web/01", Address: "192.168.1.11", Port: 22, User: "deploy", Group: "web servers", PrivateKey: ""},
		{ID: 3, Name: "db'host\nwith\"quotes", Address: "10.0.0.5", Port: 2222, User: "postgres", Group: ""},
	}

	keyResolver := func(alias string) string {
		return "/keys/" + alias + ".key"
	}

	invBytes, aliases, err := InventoryJSON(targets, keyResolver)
	if err != nil {
		t.Fatalf("InventoryJSON failed: %v", err)
	}

	// Verify valid JSON
	var parsed struct {
		All struct {
			Hosts    map[string]any `json:"hosts"`
			Children map[string]struct {
				Hosts map[string]any `json:"hosts"`
			} `json:"children"`
		} `json:"all"`
	}
	if err := json.Unmarshal(invBytes, &parsed); err != nil {
		t.Fatalf("Generated inventory is not valid JSON: %v\nContent:\n%s", err, string(invBytes))
	}

	// Check aliases dedup
	// web 01 -> web_01
	// web/01 -> web_01 -> deduped to web_01_2
	if aliases["web_01"] != 1 {
		t.Errorf("expected alias web_01 for ID 1, got %v", aliases["web_01"])
	}
	if aliases["web_01_2"] != 2 {
		t.Errorf("expected alias web_01_2 for ID 2, got %v", aliases["web_01_2"])
	}

	// Hostile name check
	var hostileAlias string
	for a, id := range aliases {
		if id == 3 {
			hostileAlias = a
			break
		}
	}
	if hostileAlias == "" {
		t.Fatalf("hostile target ID 3 alias not found")
	}
	if strings.ContainsAny(hostileAlias, "'\"\n\r\t") {
		t.Errorf("hostile alias was not sanitized: %q", hostileAlias)
	}

	// Group check: "web servers" -> "web_servers"
	grp, ok := parsed.All.Children["web_servers"]
	if !ok {
		t.Fatalf("expected child group web_servers in %v", parsed.All.Children)
	}
	if len(grp.Hosts) != 2 {
		t.Errorf("expected 2 hosts in web_servers, got %d", len(grp.Hosts))
	}

	// Ungrouped host check: ID 3 has no group, should be in All.Hosts
	if _, ok := parsed.All.Hosts[hostileAlias]; !ok {
		t.Errorf("expected ungrouped host %s in all.hosts", hostileAlias)
	}
}

func TestRunFakeAnsible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell script test on windows")
	}

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Fake ansible-playbook script
	fakeBin := filepath.Join(binDir, "ansible-playbook")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
    echo "ansible-playbook [core 2.15.0]"
    exit 0
fi

# Dump env and args for testing
echo "ARGS: $@"
echo "FORCE_COLOR: $ANSIBLE_FORCE_COLOR"
if [ -n "$SECRET_ENV_VAR" ]; then
    echo "LEAKED_SECRET: $SECRET_ENV_VAR"
fi
[ -n "$ANSIBLE_CONFIG" ] && cat "$ANSIBLE_CONFIG"
[ -n "$ANSIBLE_ROLES_PATH" ] && echo "ROLES: $ANSIBLE_ROLES_PATH"
[ -n "$ANSIBLE_COLLECTIONS_PATH" ] && echo "COLLECTIONS: $ANSIBLE_COLLECTIONS_PATH"

# If special arg --simulate-sleep
for arg in "$@"; do
    if [ "$arg" = "--simulate-sleep" ]; then
        sleep 10
        exit 0
    fi
    if [ "$arg" = "--simulate-fail" ]; then
        echo "TASK [failing task] ********************"
        echo "fatal: [host1]: FAILED!"
        exit 2
    fi
done

# Check key file permission if keys directory exists
if [ -d "keys" ]; then
    for k in keys/*.key; do
        if [ -f "$k" ]; then
            # Verify 0600 (read/write only by owner)
            perms=$(ls -l "$k" | awk '{print $1}')
            echo "KEY_PERM: $perms"
        fi
    done
fi

echo "PLAY RECAP ********************"
echo "host1 : ok=2 changed=1 unreachable=0 failed=0"
exit 0
`
	if err := os.WriteFile(fakeBin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	// Prepend binDir to PATH and set a secret env var
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+":"+oldPath)
	t.Setenv("SECRET_ENV_VAR", "super-secret-password-should-not-leak")

	// Reset cached Available
	availMu.Lock()
	cachedAt = time.Time{}
	cachedPath = ""
	cachedVer = ""
	cachedErr = nil
	availMu.Unlock()

	p, ver, err := Available()
	if err != nil {
		t.Fatalf("Available() err: %v", err)
	}
	if p != fakeBin {
		t.Fatalf("Available() path = %q, want %q", p, fakeBin)
	}
	if !strings.Contains(ver, "core 2.15.0") {
		t.Fatalf("Available() version = %q", ver)
	}

	t.Run("args and clean env and key permissions", func(t *testing.T) {
		spec := RunSpec{
			Playbook:  "---\n- hosts: all\n  tasks: []\n",
			Targets:   []Target{{ID: 1, Name: "h1", Address: "1.1.1.1", PrivateKey: "PRIVATE KEY DATA"}},
			Mode:      "check",
			ExtraVars: `{"foo":"bar"}`,
			Tags:      "tagA",
			SkipTags:  "tagB",
			Verbosity: 4,
			Limit:     "web-1,web-2",
			Become:    true,
			Forks:     8,
		}

		var logBuf strings.Builder
		exitCode, err := Run(context.Background(), spec, &logBuf)
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if exitCode != 0 {
			t.Fatalf("exitCode = %d, want 0", exitCode)
		}

		out := logBuf.String()
		if strings.Contains(out, "LEAKED_SECRET") {
			t.Errorf("Secret env var was leaked to child process! Output: %s", out)
		}
		if !strings.Contains(out, "--check") {
			t.Errorf("Expected --check in args, got: %s", out)
		}
		if !strings.Contains(out, "-vvvv") {
			t.Errorf("Expected -vvvv in args, got: %s", out)
		}
		if !strings.Contains(out, "--limit web-1,web-2") {
			t.Errorf("Expected --limit in args, got: %s", out)
		}
		// Runs keep the ANSI coloring so the UI can render it.
		if !strings.Contains(out, "FORCE_COLOR: 1") {
			t.Errorf("Expected ANSIBLE_FORCE_COLOR=1 for a playbook run, got: %s", out)
		}
		if !strings.Contains(out, "--become") {
			t.Errorf("Expected --become in args, got: %s", out)
		}
		if !strings.Contains(out, "-f 8") {
			t.Errorf("Expected -f 8 in args, got: %s", out)
		}
		if !strings.Contains(out, "--tags tagA") {
			t.Errorf("Expected --tags tagA in args, got: %s", out)
		}
		if !strings.Contains(out, "--skip-tags tagB") {
			t.Errorf("Expected --skip-tags tagB in args, got: %s", out)
		}
		if !strings.Contains(out, "-e @") {
			t.Errorf("Expected -e @extra.json in args, got: %s", out)
		}
		// Regression: OpenSSH reads /root/.ssh/config from the passwd home and fails on bind-mounted configs.
		if !strings.Contains(out, "ssh_args = -F /dev/null") {
			t.Errorf("ansible.cfg must make ssh ignore the user ssh config, got:\n%s", out)
		}
		if !strings.Contains(out, "KEY_PERM: -rw-------") {
			t.Errorf("Expected key permission -rw------- (0600), got output:\n%s", out)
		}
	})

	t.Run("config additions library paths and forks", func(t *testing.T) {
		lib := t.TempDir()
		spec := RunSpec{
			Playbook:   "---\n- hosts: all\n  tasks: []\n",
			Targets:    []Target{{ID: 1, Name: "h1", Address: "1.1.1.1"}},
			Mode:       "run",
			Config:     "[defaults]\nforks = 12\n\n[privilege_escalation]\nbecome_method = doas\n",
			LibraryDir: lib,
		}
		var logBuf strings.Builder
		if _, err := Run(context.Background(), spec, &logBuf); err != nil {
			t.Fatal(err)
		}
		out := logBuf.String()
		for _, want := range []string{"forks = 12", "become_method = doas", "host_key_checking = False", "ssh_args = -F /dev/null",
			"ROLES: " + LibraryRolesDir(lib) + ":", "COLLECTIONS: " + LibraryCollectionsDir(lib) + ":"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, " -f ") {
			t.Errorf("without Forks the forks of ansible.cfg must apply, got: %s", out)
		}

		spec.Config = "[defaults]\ninventory = /etc/hosts\n"
		if _, err := Run(context.Background(), spec, &logBuf); err == nil || !strings.Contains(err.Error(), "managed by Pantau") {
			t.Errorf("a managed key must fail the run before it starts: %v", err)
		}
	})

	t.Run("non-zero exit code returned without err", func(t *testing.T) {
		spec := RunSpec{
			Playbook: "---\n- hosts: all\n  tasks: []\n",
			Targets:  []Target{{ID: 1, Name: "h1", Address: "1.1.1.1"}},
			Mode:     "run",
			Tags:     "--simulate-fail", // inject flag through tags regex? wait, tags regex disallows spaces but permits hyphen
		}
		// tags regex allows [A-Za-z0-9_.,:-]
		// "--simulate-fail" matches tagsRegex!
		var logBuf strings.Builder
		exitCode, err := Run(context.Background(), spec, &logBuf)
		if err != nil {
			t.Fatalf("Expected nil err for non-zero exit, got: %v", err)
		}
		if exitCode != 2 {
			t.Fatalf("Expected exitCode 2, got: %d", exitCode)
		}
		if !strings.Contains(logBuf.String(), "fatal: [host1]: FAILED!") {
			t.Errorf("Expected failure output in log, got: %s", logBuf.String())
		}
	})

	t.Run("ctx cancel kills sleeping child group", func(t *testing.T) {
		spec := RunSpec{
			Playbook: "---\n- hosts: all\n  tasks: []\n",
			Targets:  []Target{{ID: 1, Name: "h1", Address: "1.1.1.1"}},
			Mode:     "run",
			Tags:     "--simulate-sleep",
		}

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(150 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		var logBuf strings.Builder
		_, err := Run(ctx, spec, &logBuf)
		duration := time.Since(start)

		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("Expected context canceled error, got: %v", err)
		}
		if duration > 3*time.Second {
			t.Errorf("Run took %v, expected quick termination after cancel", duration)
		}
	})

	t.Run("timeout terminates run", func(t *testing.T) {
		spec := RunSpec{
			Playbook: "---\n- hosts: all\n  tasks: []\n",
			Targets:  []Target{{ID: 1, Name: "h1", Address: "1.1.1.1"}},
			Mode:     "run",
			Tags:     "--simulate-sleep",
			Timeout:  200 * time.Millisecond,
		}

		start := time.Now()
		var logBuf strings.Builder
		_, err := Run(context.Background(), spec, &logBuf)
		duration := time.Since(start)

		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
			t.Fatalf("Expected deadline exceeded, got: %v", err)
		}
		if duration > 3*time.Second {
			t.Errorf("Run took %v, expected quick timeout", duration)
		}
	})
}

func TestRealAnsibleSmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping real ansible test on windows")
	}

	realBin, err := exec.LookPath("ansible-playbook")
	if err != nil {
		t.Skip("ansible-playbook binary not found on system, skipping smoke test")
	}

	// Verify it's actually real, not our mock if PATH was changed
	out, err := exec.Command(realBin, "--version").Output()
	if err != nil || !strings.Contains(string(out), "ansible") {
		t.Skip("ansible-playbook not functioning, skipping smoke test")
	}

	spec := RunSpec{
		Playbook: `---
- name: Smoke test playbook
  hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - name: Debug ping
      ansible.builtin.debug:
        msg: "hello from pantau smoke test"
`,
		Mode: "syntax",
	}

	var logBuf strings.Builder
	exitCode, err := Run(context.Background(), spec, &logBuf)
	if err != nil {
		t.Fatalf("Smoke syntax check failed with error: %v, log: %s", err, logBuf.String())
	}
	if exitCode != 0 {
		t.Fatalf("Smoke syntax check returned non-zero exit code %d, log: %s", exitCode, logBuf.String())
	}
}
