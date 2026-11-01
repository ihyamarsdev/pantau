package inspector

import (
	"errors"
	"strings"
	"testing"
)

func TestValidSystemdUnit(t *testing.T) {
	valid := []string{"nginx.service", "getty@tty1.service", `dev-disk-by\x2duuid.service`, "systemd-resolved.service", "user@1000.service"}
	for _, u := range valid {
		if !ValidSystemdUnit(u) {
			t.Errorf("%q must be accepted", u)
		}
	}
	invalid := []string{"", "nginx", "nginx.socket", "-nginx.service", "--now.service", "a b.service", "a;reboot.service", "a$(id).service", "a'b.service", "a\nb.service", "../x.service", "nginx.service;id"}
	for _, u := range invalid {
		if ValidSystemdUnit(u) {
			t.Errorf("%q must be rejected", u)
		}
	}
}

func TestSystemdActionRejectsBadInputBeforeTouchingTheHost(t *testing.T) {
	ins := &Inspector{} // no db: validation must fail first
	for _, a := range []string{"poweroff", "kill", "edit", "", "restart; id"} {
		if _, err := ins.SystemdAction(1, "nginx.service", a); err == nil || !strings.Contains(err.Error(), "unsupported action") {
			t.Errorf("action %q must be rejected, got %v", a, err)
		}
	}
	if _, err := ins.SystemdAction(1, "nginx; reboot", "restart"); !errors.Is(err, ErrSystemdInvalidUnit) {
		t.Errorf("bad unit name must be rejected, got %v", err)
	}
	if _, err := ins.SystemdAction(1, "", "enable"); !errors.Is(err, ErrSystemdInvalidUnit) {
		t.Errorf("unit actions need a unit, got %v", err)
	}
	// stop, disable and mask would cut the host off from Pantau (now or after a reboot).
	for _, u := range []string{"ssh.service", "sshd.service", "sshd@10.service"} {
		for _, a := range []string{"stop", "disable", "mask"} {
			if _, err := ins.SystemdAction(1, u, a); !errors.Is(err, ErrSystemdProtected) {
				t.Errorf("%s %s must be blocked, got %v", a, u, err)
			}
		}
	}
}

func TestParseSystemdDetail(t *testing.T) {
	out := `===SHOW===
Id=nginx.service
ActiveState=failed
Result=exit-code
ExecMainStatus=1
NRestarts=0
===STATUS===
× nginx.service - A high performance web server
     Active: failed (Result: exit-code)
===LOGS_SUDO===
===LOGS===
2026-11-01T10:00:00+0000 host nginx[1]: nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)
2026-11-01T10:00:01+0000 host systemd[1]: nginx.service: Failed with result 'exit-code'.
`
	d := parseSystemdDetail(out)
	if d.Properties["ActiveState"] != "failed" || d.Properties["Result"] != "exit-code" || d.Properties["Id"] != "nginx.service" {
		t.Errorf("properties parsed wrong: %v", d.Properties)
	}
	if !strings.Contains(d.Status, "Active: failed") {
		t.Errorf("status missing: %q", d.Status)
	}
	if !d.LogsElevated || d.LogsNote != "" {
		t.Errorf("expected sudo-read logs without note, got elevated=%v note=%q", d.LogsElevated, d.LogsNote)
	}
	if !strings.Contains(d.Logs, "Address already in use") || strings.Contains(d.Logs, "===") {
		t.Errorf("logs parsed wrong: %q", d.Logs)
	}

	if np := parseSystemdDetail("===SHOW===\nActiveState=active\n===STATUS===\n===LOGS_NOPERM===\n===LOGS===\n"); np.LogsNote != "no_permission" {
		t.Errorf("expected no_permission note, got %q", np.LogsNote)
	}
	if un := parseSystemdDetail("===SHOW===\nActiveState=active\n===STATUS===\n===LOGS_UNAVAILABLE===\n"); un.LogsNote != "unavailable" {
		t.Errorf("expected unavailable note, got %q", un.LogsNote)
	}
}

func codes(ds []SystemdDiagnosis) string {
	var c []string
	for _, d := range ds {
		c = append(c, d.Code)
	}
	return strings.Join(c, ",")
}

func TestDiagnoseSystemd(t *testing.T) {
	failed := map[string]string{"ActiveState": "failed", "Result": "exit-code", "ExecMainStatus": "1"}

	// One log line is explained once, by the most specific rule.
	one := diagnoseSystemd(failed, "", "nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)")
	if codes(one) != "port_in_use" || !strings.Contains(one[0].Evidence, "Address already in use") {
		t.Errorf("a single bind error must yield only port_in_use with its evidence line, got %+v", one)
	}
	// Separate lines are separate causes.
	ds := diagnoseSystemd(failed, "", "nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)\nnginx: configuration file /etc/nginx/nginx.conf test failed")
	if codes(ds) != "port_in_use,config_error" {
		t.Errorf("expected port_in_use,config_error, got %s", codes(ds))
	}
	// A generic rule must not repeat the line a specific rule already explained.
	if c := codes(diagnoseSystemd(failed, "", "app.service: Unable to locate executable '/x': No such file or directory (status=203/EXEC)")); c != "exec_not_found" {
		t.Errorf("expected only exec_not_found, got %s", c)
	}

	if c := codes(diagnoseSystemd(failed, "", "app.service: Main process exited, code=exited, status=203/EXEC")); !strings.Contains(c, "exec_not_found") {
		t.Errorf("expected exec_not_found, got %s", c)
	}
	if c := codes(diagnoseSystemd(failed, "", "app.service: Failed to load environment files: No such file or directory")); !strings.Contains(c, "env_file") {
		t.Errorf("expected env_file, got %s", c)
	}
	if c := codes(diagnoseSystemd(failed, "", "Start request repeated too quickly.")); !strings.Contains(c, "start_limit") {
		t.Errorf("expected start_limit, got %s", c)
	}

	// Nothing recognisable in the logs: fall back to the exit status instead of inventing a cause.
	generic := diagnoseSystemd(failed, "", "app: something odd happened")
	if codes(generic) != "exit_code" || !strings.Contains(generic[0].Evidence, "ExecMainStatus=1") {
		t.Errorf("expected generic exit_code with status, got %+v", generic)
	}

	// A healthy unit must not be blamed for old log noise, but a restart loop is still reported.
	healthy := map[string]string{"ActiveState": "active", "Result": "success", "NRestarts": "7"}
	if c := codes(diagnoseSystemd(healthy, "", "connection refused\npermission denied")); c != "restart_loop" {
		t.Errorf("healthy unit must only report restart_loop, got %s", c)
	}
	if c := codes(diagnoseSystemd(map[string]string{"ActiveState": "active", "Result": "success"}, "", "connection refused")); c != "" {
		t.Errorf("healthy unit must report nothing, got %s", c)
	}

	// A unit that stopped cleanly is not a problem.
	if c := codes(diagnoseSystemd(map[string]string{"ActiveState": "inactive", "Result": "success"}, "", "permission denied")); c != "" {
		t.Errorf("cleanly stopped unit must report nothing, got %s", c)
	}

	if c := codes(diagnoseSystemd(map[string]string{"ActiveState": "inactive", "LoadState": "masked"}, "", "")); c != "masked" {
		t.Errorf("expected masked, got %s", c)
	}
}

func TestParseSystemdActionOutput(t *testing.T) {
	var res SystemdActionResult
	parseSystemdActionOutput("===MODE:sudo===\n===EXIT:0===\nActiveState=active\nSubState=running\n", &res)
	if res.Mode != "sudo" || res.ExitCode != 0 || res.ActiveState != "active" || res.SubState != "running" || res.Output != "" {
		t.Errorf("success parsed wrong: %+v", res)
	}

	var failed SystemdActionResult
	parseSystemdActionOutput("===MODE:none===\nFailed to restart nginx.service: Interactive authentication required.\n===EXIT:1===\nActiveState=active\nSubState=running\n", &failed)
	if failed.Mode != "none" || failed.ExitCode != 1 || !strings.Contains(failed.Output, "Interactive authentication required") {
		t.Errorf("failure parsed wrong: %+v", failed)
	}

	var broken SystemdActionResult
	parseSystemdActionOutput("garbage", &broken)
	if broken.ExitCode != -1 {
		t.Errorf("missing exit marker must yield exit -1, got %d", broken.ExitCode)
	}
}

func TestSystemdActionScriptQuotesUnitName(t *testing.T) {
	script := systemdActionScript(`weird\x2dname.service`, "restart")
	if !strings.Contains(script, `U='weird\x2dname.service'`) {
		t.Errorf("unit must be single-quoted in the script: %s", script)
	}
	if !strings.Contains(script, "--no-ask-password restart") {
		t.Errorf("systemctl must never wait for a password prompt: %s", script)
	}
}
