package inspector

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runShimmed executes a generated remote script locally under `sh -c` with fake `id`, `sudo`, `systemctl` and
// `journalctl` first on PATH, so the real privilege-escalation logic (root / sudo / none) is exercised
// deterministically without root, systemd or a sudoers entry.
func runShimmed(t *testing.T, script string, uid string, sudoWorks bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("id", `echo `+uid)
	if sudoWorks {
		write("sudo", `[ "$1" = "-n" ] && shift
FAKE_ROOT=1 exec "$@"`)
	} else {
		write("sudo", `echo "sudo: a password is required" >&2; exit 1`)
	}
	// Unprivileged systemctl cannot change anything (polkit would prompt); privileged one succeeds.
	write("systemctl", `case "$*" in
  *" show "*|show*) echo "ActiveState=active"; echo "SubState=running"; echo "UnitFileState=enabled"; echo "Id=x.service"; echo "LoadState=loaded";;
  *status*) echo "x.service - test"; echo "   Active: active (running)";;
  *) if [ -z "$FAKE_ROOT" ] && [ "$(id -u)" != "0" ]; then echo "Failed to restart: Interactive authentication required." >&2; exit 1; fi; echo "did: $*";;
esac`)
	write("journalctl", `if [ -z "$FAKE_ROOT" ] && [ "$(id -u)" != "0" ]; then echo "No journal files were opened due to insufficient permissions."; else echo "journal line for $*"; fi`)
	write("sleep", `exit 0`)
	// A Docker socket that only privileged users may use.
	write("docker", `if [ -z "$FAKE_ROOT" ] && [ "$(id -u)" != "0" ]; then echo "permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock" >&2; exit 1; fi
echo "docker $*"`)

	cmd := exec.Command("sh", "-c", script)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestSystemdActionScriptPrivilegeModes(t *testing.T) {
	var res SystemdActionResult

	parseSystemdActionOutput(runShimmed(t, systemdActionScript("nginx.service", "restart"), "0", false), &res)
	if res.Mode != "root" || res.ExitCode != 0 || !strings.Contains(res.Output, "did: --no-ask-password restart nginx.service") {
		t.Errorf("root must run systemctl directly, got %+v", res)
	}

	res = SystemdActionResult{}
	parseSystemdActionOutput(runShimmed(t, systemdActionScript("nginx.service", "restart"), "1000", true), &res)
	if res.Mode != "sudo" || res.ExitCode != 0 || res.ActiveState != "active" || res.EnabledState != "enabled" {
		t.Errorf("non-root with passwordless sudo must escalate, got %+v", res)
	}

	// The real "no sudo" situation: sudo refuses, plain systemctl is refused by polkit. The failure must be
	// reported with mode none and a readable reason instead of pretending to succeed.
	res = SystemdActionResult{Action: "restart"}
	parseSystemdActionOutput(runShimmed(t, systemdActionScript("nginx.service", "restart"), "1000", false), &res)
	if res.Mode != "none" || res.ExitCode != 1 || !strings.Contains(res.Output, "Interactive authentication required") || res.Succeeded() {
		t.Errorf("non-root without sudo must fail with mode none, got %+v", res)
	}
}

func TestSystemdActionScriptsForNewActions(t *testing.T) {
	for _, a := range []string{"enable", "disable", "mask", "unmask", "reset-failed", "reload"} {
		var res SystemdActionResult
		res.Action = a
		parseSystemdActionOutput(runShimmed(t, systemdActionScript("nginx.service", a), "1000", true), &res)
		if res.ExitCode != 0 || !strings.Contains(res.Output, "did: --no-ask-password "+a+" nginx.service") || !res.Succeeded() {
			t.Errorf("%s: unexpected result %+v", a, res)
		}
	}

	// daemon-reload is host-wide: no unit argument, no per-unit state query.
	script := systemdActionScript("", systemdActionDaemonRL)
	if strings.Contains(script, `show "$U"`) || strings.Contains(script, `daemon-reload "$U"`) {
		t.Errorf("daemon-reload must not reference a unit: %s", script)
	}
	var res SystemdActionResult
	parseSystemdActionOutput(runShimmed(t, script, "1000", true), &res)
	if res.ExitCode != 0 || !strings.Contains(res.Output, "did: --no-ask-password daemon-reload") {
		t.Errorf("daemon-reload failed: %+v", res)
	}
}

func TestSystemdSucceededOnlyBlamesFailedStateForRunActions(t *testing.T) {
	cases := []struct {
		action, state string
		want          bool
	}{
		{"start", "failed", false}, {"restart", "failed", false}, {"reload", "failed", false},
		{"start", "active", true}, {"stop", "inactive", true},
		{"reset-failed", "failed", true}, // the unit is still failed until it is started again: that is expected
		{"enable", "failed", true}, {"mask", "inactive", true},
	}
	for _, c := range cases {
		if got := (&SystemdActionResult{Action: c.action, ActiveState: c.state}).Succeeded(); got != c.want {
			t.Errorf("%s with state %s: Succeeded=%v, want %v", c.action, c.state, got, c.want)
		}
	}
	if (&SystemdActionResult{Action: "start", ExitCode: 1, ActiveState: "active"}).Succeeded() {
		t.Error("a non-zero exit is never a success")
	}
}

func TestSystemdDetailScriptJournalPrivilege(t *testing.T) {
	// Readable journal: no sudo involved.
	d := parseSystemdDetail(runShimmed(t, systemdDetailScript("nginx.service", false, true), "0", false))
	if d.LogsElevated || d.LogsNote != "" || !strings.Contains(d.Logs, "journal line") {
		t.Errorf("root must read the journal directly: %+v", d)
	}

	// Unprivileged user, sudo allowed: journal read through sudo.
	d = parseSystemdDetail(runShimmed(t, systemdDetailScript("nginx.service", false, true), "1000", true))
	if !d.LogsElevated || d.LogsNote != "" || !strings.Contains(d.Logs, "journal line") {
		t.Errorf("sudo must unlock the journal: %+v", d)
	}

	// Unprivileged user, sudo refused: explained, not silently empty.
	d = parseSystemdDetail(runShimmed(t, systemdDetailScript("nginx.service", false, true), "1000", false))
	if d.LogsElevated || d.LogsNote != "no_permission" || d.LogsSudoSkipped {
		t.Errorf("a refused sudo must report no_permission after trying: %+v", d)
	}

	// Inside the refusal backoff window sudo must not be attempted at all (no failed-sudo log entry).
	script := systemdDetailScript("nginx.service", false, false)
	out := runShimmed(t, script, "1000", true) // even though sudo would work, it must not be used
	d = parseSystemdDetail(out)
	if d.LogsElevated || !d.LogsSudoSkipped || d.LogsNote != "no_permission" {
		t.Errorf("within backoff sudo must be skipped and the UI told so: %+v", d)
	}
}

func TestSystemdDetailSudoBackoffIsSharedAcrossProbes(t *testing.T) {
	ins := &Inspector{}
	if !ins.sudoProbeAllowed(5) {
		t.Fatal("a host that never failed must be probed")
	}
	ins.noteSudo(5, true)
	if ins.sudoProbeAllowed(5) {
		t.Error("after a refusal sudo must not be probed again inside the backoff window")
	}
	if !ins.sudoProbeAllowed(6) {
		t.Error("backoff is per host")
	}
	ins.noteSudo(5, false)
	if !ins.sudoProbeAllowed(5) {
		t.Error("a successful sudo must clear the backoff")
	}
}
