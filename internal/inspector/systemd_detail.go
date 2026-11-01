package inspector

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
)

var (
	// ErrSystemdInvalidUnit is returned for unit names that are not plain `*.service` identifiers.
	ErrSystemdInvalidUnit = errors.New("invalid systemd service name")
	// ErrSystemdProtected is returned when an action would cut Pantau's own SSH access to the host.
	ErrSystemdProtected = errors.New("this action on the SSH daemon is blocked: Pantau would lose access to this host")
)

const (
	systemdDetailTimeout = 30 * time.Second
	systemdActionTimeout = 60 * time.Second

	sdMarkerShow          = "===SHOW==="
	sdMarkerStatus        = "===STATUS==="
	sdMarkerLogs          = "===LOGS==="
	sdMarkerLogsSudo      = "===LOGS_SUDO==="
	sdMarkerLogsNoPerm    = "===LOGS_NOPERM==="
	sdMarkerLogsSkipped   = "===LOGS_SUDO_SKIPPED==="
	sdMarkerLogsMissing   = "===LOGS_UNAVAILABLE==="
	sdMarkerModePrefix    = "===MODE:"
	sdMarkerExitPrefix    = "===EXIT:"
	systemdActionDaemonRL = "daemon-reload"
)

// systemdActions maps each supported action to whether it targets one unit and whether it is refused for the
// SSH daemon (stop/disable/mask would leave Pantau, or the next reboot, without SSH access).
var systemdActions = map[string]struct{ unit, protectSSH bool }{
	"start":               {true, false},
	"stop":                {true, true},
	"restart":             {true, false},
	"reload":              {true, false},
	"enable":              {true, false},
	"disable":             {true, true},
	"mask":                {true, true},
	"unmask":              {true, false},
	"reset-failed":        {true, false},
	systemdActionDaemonRL: {false, false},
}

// Backslash is allowed because systemd escapes special characters in unit names as \xNN.
var systemdUnitRe = regexp.MustCompile(`^[A-Za-z0-9:_.@\\-]{1,200}\.service$`)
var systemdSSHDaemonRe = regexp.MustCompile(`^(ssh|sshd)(@.*)?\.service$`)

// ValidSystemdUnit reports whether name is a plain service unit name that is safe to pass to systemctl.
func ValidSystemdUnit(name string) bool {
	return systemdUnitRe.MatchString(name) && !strings.HasPrefix(name, "-")
}

// SystemdDiagnosis is one probable cause found for a unit that is not healthy. Code is a stable identifier the
// UI translates; Evidence is the log/status line (or property) that triggered it.
type SystemdDiagnosis struct {
	Code     string `json:"code"`
	Evidence string `json:"evidence"`
}

// SystemdDetail is the status + properties + recent journal of one service unit.
type SystemdDetail struct {
	Unit            string             `json:"unit"`
	Properties      map[string]string  `json:"properties"`
	Status          string             `json:"status"`
	Logs            string             `json:"logs"`
	LogsNote        string             `json:"logs_note"`         // "", "no_permission", "unavailable"
	LogsElevated    bool               `json:"logs_elevated"`     // journal was read through passwordless sudo
	LogsSudoSkipped bool               `json:"logs_sudo_skipped"` // sudo was not tried: it failed recently on this host
	ErrorsOnly      bool               `json:"errors_only"`
	Diagnoses       []SystemdDiagnosis `json:"diagnoses"`
}

// SystemdActionResult is the outcome of a systemctl action.
type SystemdActionResult struct {
	Unit         string `json:"unit"`
	Action       string `json:"action"`
	Mode         string `json:"mode"` // root | sudo | none (plain systemctl, relies on polkit)
	ExitCode     int    `json:"exit_code"`
	Output       string `json:"output"`
	ActiveState  string `json:"active_state"`
	SubState     string `json:"sub_state"`
	EnabledState string `json:"enabled_state"`
}

func systemdDetailScript(unit string, errorsOnly, trySudo bool) string {
	prio := ""
	if errorsOnly {
		prio = "-p err"
	}
	try := "0"
	if trySudo {
		try = "1"
	}
	return `U=` + shellQuote(unit) + `
PRIO="` + prio + `"
TRY=` + try + `
echo "` + sdMarkerShow + `"
systemctl show "$U" --no-pager -p Id -p Description -p LoadState -p ActiveState -p SubState -p UnitFileState -p Result -p ExecMainStatus -p ExecMainCode -p MainPID -p NRestarts -p ActiveEnterTimestamp -p InactiveEnterTimestamp -p FragmentPath -p MemoryCurrent -p User 2>/dev/null
echo "` + sdMarkerStatus + `"
systemctl status "$U" --no-pager -l -n 0 2>&1 | head -n 60
jl() { $1 journalctl -u "$U" -n 200 --no-pager -o short-iso $PRIO 2>&1; }
if ! command -v journalctl >/dev/null 2>&1; then
  echo "` + sdMarkerLogsMissing + `"
else
  out=$(jl "")
  case "$out" in
    *"insufficient permissions"*|*"No journal files were opened"*|*"not seeing messages from other users"*)
      if [ "$(id -u)" = "0" ]; then :
      elif [ "$TRY" != "1" ]; then echo "` + sdMarkerLogsNoPerm + `"; echo "` + sdMarkerLogsSkipped + `"
      elif sudo -n true 2>/dev/null; then out=$(jl "sudo -n"); echo "` + sdMarkerLogsSudo + `"
      else echo "` + sdMarkerLogsNoPerm + `"; fi;;
  esac
  echo "` + sdMarkerLogs + `"
  printf '%s\n' "$out" | tail -n 300 | head -c 60000
fi
`
}

// SystemdDetail returns the status, key properties, recent journal lines and probable causes for one unit.
// The journal is read as the SSH user; only when that is not permitted passwordless sudo is tried, and after a
// refusal it is not tried again for sudoRetryAfter unless forceSudo is set (the "Retry with sudo" button), so a
// host that needs a sudo password does not collect a failed-sudo entry on every dialog open.
func (ins *Inspector) SystemdDetail(hostID int64, unit string, errorsOnly, forceSudo bool) (*SystemdDetail, error) {
	if !ValidSystemdUnit(unit) {
		return nil, ErrSystemdInvalidUnit
	}
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	trySudo := forceSudo || ins.sudoProbeAllowed(hostID)
	stdout, stderr, exitCode, err := runner.ExecTimeout("sh -c "+shellQuote(systemdDetailScript(unit, errorsOnly, trySudo)), systemdDetailTimeout)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stdout) == "" {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = fmt.Sprintf("systemctl failed (exit %d)", exitCode)
		}
		return nil, errors.New(msg)
	}
	d := parseSystemdDetail(stdout)
	d.Unit = unit
	d.ErrorsOnly = errorsOnly
	d.Diagnoses = diagnoseSystemd(d.Properties, d.Status, d.Logs)
	switch {
	case d.LogsElevated:
		ins.noteSudo(hostID, false)
	case d.LogsNote == "no_permission" && !d.LogsSudoSkipped:
		ins.noteSudo(hostID, true)
	}
	return d, nil
}

func parseSystemdDetail(out string) *SystemdDetail {
	d := &SystemdDetail{Properties: map[string]string{}, Diagnoses: []SystemdDiagnosis{}}
	section := ""
	var status, logs []string
	for _, line := range strings.Split(out, "\n") {
		switch strings.TrimRight(line, "\r") {
		case sdMarkerShow:
			section = "show"
			continue
		case sdMarkerStatus:
			section = "status"
			continue
		case sdMarkerLogs:
			section = "logs"
			continue
		case sdMarkerLogsSudo:
			d.LogsElevated = true
			continue
		case sdMarkerLogsNoPerm:
			d.LogsNote = "no_permission"
			continue
		case sdMarkerLogsSkipped:
			d.LogsSudoSkipped = true
			continue
		case sdMarkerLogsMissing:
			d.LogsNote = "unavailable"
			continue
		}
		switch section {
		case "show":
			if k, v, ok := strings.Cut(line, "="); ok && k != "" {
				d.Properties[k] = strings.TrimSpace(v)
			}
		case "status":
			status = append(status, line)
		case "logs":
			logs = append(logs, line)
		}
	}
	d.Status = strings.TrimSpace(strings.Join(status, "\n"))
	d.Logs = strings.TrimSpace(strings.Join(logs, "\n"))
	return d
}

type systemdDiagRule struct {
	code string
	re   *regexp.Regexp
}

// Ordered from most specific to most generic; the first lines that match become the evidence.
var systemdDiagRules = []systemdDiagRule{
	{"start_limit", regexp.MustCompile(`(?i)start request repeated too quickly|start-limit-hit`)},
	{"port_in_use", regexp.MustCompile(`(?i)address already in use|eaddrinuse`)},
	{"exec_not_found", regexp.MustCompile(`(?i)status=203/exec|failed to execute command|unable to locate executable|exec format error`)},
	{"bad_user", regexp.MustCompile(`(?i)status=217/user|failed to determine user credentials|failed at step user`)},
	{"bad_workdir", regexp.MustCompile(`(?i)status=200/chdir|failed at step chdir`)},
	{"env_file", regexp.MustCompile(`(?i)failed to load environment files`)},
	{"oom_killed", regexp.MustCompile(`(?i)oom-kill|out of memory|killed process \d+`)},
	{"disk_full", regexp.MustCompile(`(?i)no space left on device`)},
	{"config_error", regexp.MustCompile(`(?i)\[emerg\]|syntax error|configuration (file .*)?(error|test failed)|config(uration)? test failed|invalid configuration|unable to (open|read|parse) config`)},
	{"dependency_failed", regexp.MustCompile(`(?i)dependency failed for`)},
	{"timeout", regexp.MustCompile(`(?i)(start|stop|stopping|reload) operation timed out|timed out waiting`)},
	{"permission_denied", regexp.MustCompile(`(?i)permission denied|operation not permitted`)},
	{"missing_file", regexp.MustCompile(`(?i)no such file or directory`)},
	{"connection_refused", regexp.MustCompile(`(?i)connection refused`)},
}

const systemdMaxDiagnoses = 5

func shortEvidence(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 220 {
		s = s[:220] + "…"
	}
	return s
}

// diagnoseSystemd finds probable causes of a unit that is failed, stuck starting, or last ended abnormally.
// Healthy (active) units only report restart loops: old log lines like "connection refused" would be noise.
func diagnoseSystemd(props map[string]string, status, logs string) []SystemdDiagnosis {
	out := []SystemdDiagnosis{}
	add := func(code, evidence string) {
		for _, d := range out {
			if d.Code == code {
				return
			}
		}
		if len(out) < systemdMaxDiagnoses {
			out = append(out, SystemdDiagnosis{Code: code, Evidence: shortEvidence(evidence)})
		}
	}

	state, result := props["ActiveState"], props["Result"]
	switch props["LoadState"] {
	case "masked":
		add("masked", "LoadState=masked")
	case "not-found":
		add("not_found", "LoadState=not-found")
	}
	if n, err := strconv.Atoi(props["NRestarts"]); err == nil && n >= 5 {
		add("restart_loop", fmt.Sprintf("NRestarts=%d", n))
	}

	problematic := state == "failed" || state == "activating" || state == "deactivating" ||
		(state == "inactive" && result != "" && result != "success")
	if !problematic {
		return out
	}

	// Rules are ordered specific -> generic; a log line already explained by a specific rule is not
	// reported again as a generic cause (e.g. "Unable to locate executable ... No such file").
	lines := strings.Split(logs+"\n"+status, "\n")
	used := map[string]bool{}
	for _, rule := range systemdDiagRules {
		for _, line := range lines {
			if !used[line] && rule.re.MatchString(line) {
				used[line] = true
				add(rule.code, line)
				break
			}
		}
	}

	if len(out) == 0 || (len(out) == 1 && out[0].Code == "restart_loop") {
		switch result {
		case "exit-code", "signal", "core-dump", "watchdog", "timeout", "resources", "oom-kill":
			ev := "Result=" + result
			if st := props["ExecMainStatus"]; st != "" && st != "0" {
				ev += ", ExecMainStatus=" + st
			}
			add("exit_code", ev)
		}
	}
	return out
}

// Succeeded is true when systemctl exited 0 and, for actions that run the unit (start/restart/reload), the
// unit did not drop straight into the failed state: a service whose binary is missing is "started" by systemd
// and only then fails.
func (r *SystemdActionResult) Succeeded() bool {
	if r.ExitCode != 0 {
		return false
	}
	switch r.Action {
	case "start", "restart", "reload":
		return r.ActiveState != "failed"
	}
	return true
}

func systemdActionScript(unit, action string) string {
	cmd := action + ` "$U"`
	show := `systemctl show "$U" --no-pager -p ActiveState -p SubState -p UnitFileState 2>/dev/null`
	if action == systemdActionDaemonRL {
		cmd, show = action, ""
	}
	settle := ""
	if action == "start" || action == "restart" || action == "reload" {
		settle = `[ "$rc" = "0" ] && sleep 1` + "\n"
	}
	return `U=` + shellQuote(unit) + `
if [ "$(id -u)" = "0" ]; then S=""; M=root
elif sudo -n true 2>/dev/null; then S="sudo -n"; M=sudo
else S=""; M=none; fi
echo "` + sdMarkerModePrefix + `$M==="
out=$($S systemctl --no-ask-password ` + cmd + ` 2>&1); rc=$?
printf '%s\n' "$out" | head -c 8000
echo "` + sdMarkerExitPrefix + `$rc==="
` + settle + show + "\n"
}

func parseSystemdActionOutput(out string, res *SystemdActionResult) {
	res.Mode = "none"
	res.ExitCode = -1
	var body []string
	seenExit := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, sdMarkerModePrefix) && strings.HasSuffix(line, "==="):
			res.Mode = strings.TrimSuffix(strings.TrimPrefix(line, sdMarkerModePrefix), "===")
		case strings.HasPrefix(line, sdMarkerExitPrefix) && strings.HasSuffix(line, "==="):
			if rc, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, sdMarkerExitPrefix), "===")); err == nil {
				res.ExitCode = rc
			}
			seenExit = true
		case seenExit:
			if k, v, ok := strings.Cut(line, "="); ok {
				switch k {
				case "ActiveState":
					res.ActiveState = strings.TrimSpace(v)
				case "SubState":
					res.SubState = strings.TrimSpace(v)
				case "UnitFileState":
					res.EnabledState = strings.TrimSpace(v)
				}
			}
		default:
			body = append(body, line)
		}
	}
	res.Output = strings.TrimSpace(strings.Join(body, "\n"))
}

// SystemdAction runs a systemctl action (start, stop, restart, reload, enable, disable, mask, unmask,
// reset-failed on one service, or daemon-reload for the host). It uses root directly, else passwordless sudo,
// else plain systemctl (which only works when polkit allows it). Every attempt is written to the activity log.
// Actions are explicit administrator requests, so sudo is always tried (the refusal backoff only throttles
// the automatic probes in inspections and journal reads) and its outcome refreshes that backoff.
func (ins *Inspector) SystemdAction(hostID int64, unit, action string) (*SystemdActionResult, error) {
	spec, ok := systemdActions[action]
	if !ok {
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
	if spec.unit {
		if !ValidSystemdUnit(unit) {
			return nil, ErrSystemdInvalidUnit
		}
		if spec.protectSSH && systemdSSHDaemonRe.MatchString(unit) {
			return nil, ErrSystemdProtected
		}
	} else {
		unit = ""
	}

	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("host not found")
	}
	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	target := unit
	if target == "" {
		target = "host"
	}
	res := &SystemdActionResult{Unit: unit, Action: action}
	stdout, stderr, _, err := runner.ExecTimeout("sh -c "+shellQuote(systemdActionScript(unit, action)), systemdActionTimeout)
	if err != nil {
		logger.Warn("systemd", host.Name, fmt.Sprintf("%s %s failed: %v", action, target, err))
		return nil, err
	}
	parseSystemdActionOutput(stdout, res)
	if res.ExitCode == -1 && strings.TrimSpace(stderr) != "" {
		res.Output = strings.TrimSpace(stderr)
	}
	switch res.Mode {
	case "sudo":
		ins.noteSudo(hostID, false)
	case "none":
		ins.noteSudo(hostID, true)
	}

	switch {
	case res.Succeeded():
		logger.Info("systemd", host.Name, fmt.Sprintf("%s %s (via %s) -> %s/%s %s", action, target, res.Mode, res.ActiveState, res.SubState, res.EnabledState))
	case res.ExitCode == 0:
		logger.Warn("systemd", host.Name, fmt.Sprintf("%s %s (via %s) completed but the unit is now %s/%s", action, target, res.Mode, res.ActiveState, res.SubState))
	default:
		logger.Warn("systemd", host.Name, fmt.Sprintf("%s %s (via %s) failed with exit %d: %s", action, target, res.Mode, res.ExitCode, shortEvidence(res.Output)))
	}
	return res, nil
}
