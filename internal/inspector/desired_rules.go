package inspector

// Desired State evaluation: one Desired Rule per check, the Drift it finds,
// and the baseline that seeds new rules.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// crontabListCmd is the exact command the cron rule branch runs; memoized per inspection by crontabMemoRunner.
const crontabListCmd = `crontab -l 2>/dev/null`

// ponytail: N enabled cron rules each re-ran `crontab -l`; this wrapper memoizes one successful run per inspection.
type crontabMemoRunner struct {
	sshrunner.Runner
	cached bool
	stdout string
	stderr string
	code   int
}

func (r *crontabMemoRunner) Exec(cmd string) (string, string, int, error) {
	if cmd != crontabListCmd {
		return r.Runner.Exec(cmd)
	}
	if !r.cached {
		var err error
		r.stdout, r.stderr, r.code, err = r.Runner.Exec(cmd)
		if err != nil {
			return r.stdout, r.stderr, r.code, err // ponytail: never memoize errors
		}
		r.cached = true
	}
	return r.stdout, r.stderr, r.code, nil
}

// serviceStatusCmd is the exact per-rule service probe command; shared with serviceMemoRunner so a
// prefetched batch answer maps to the identical command string.
func serviceStatusCmd(target string) string {
	return fmt.Sprintf(`sh -c 'if [ "$(uname -s)" = "Darwin" ]; then `+
		`if launchctl print "system/$1" 2>/dev/null | grep -q "state = running"; then echo "active"; `+
		`elif launchctl list 2>/dev/null | grep -F "$1" | awk '\''{if ($1 ~ /^[0-9]+$/ && $1 > 0) exit 0; else exit 1}'\''; then echo "active"; `+
		`else echo "inactive"; fi; `+
		`elif command -v systemctl >/dev/null 2>&1; then systemctl is-active "$1" 2>/dev/null; `+
		`elif service "$1" status 2>&1 | grep -iq "running"; then echo "active"; `+
		`else echo "inactive"; fi' -- %s`, shellQuote(target))
}

// serviceBatchCmd probes all service targets in one systemctl call; it prints one state line per
// target in order, or serviceBatchSentinel when systemctl is unavailable (e.g. Darwin/SysVinit).
const serviceBatchSentinel = "__PANTAU_NO_SYSTEMD__"

func serviceBatchCmd(targets []string) string {
	quoted := make([]string, len(targets))
	for i, t := range targets {
		quoted[i] = shellQuote(t)
	}
	return fmt.Sprintf(`sh -c 'command -v systemctl >/dev/null 2>&1 || { echo %s; exit 0; }; systemctl is-active %s 2>/dev/null'`,
		serviceBatchSentinel, strings.Join(quoted, " "))
}

// ponytail: N service rules each re-ran the polyglot probe; on Linux+systemd one batched exec answers them all.
type serviceMemoRunner struct {
	sshrunner.Runner
	answers map[string]string // exact per-rule probe command -> stdout
}

func (r *serviceMemoRunner) Exec(cmd string) (string, string, int, error) {
	if out, ok := r.answers[cmd]; ok {
		return out, "", 0, nil
	}
	return r.Runner.Exec(cmd)
}

// prefetchServiceStates returns a wrapper answering each per-rule service probe from one batched
// systemctl exec, or nil to fall back to the per-rule path (any mismatch, non-Linux, or error).
func prefetchServiceStates(runner sshrunner.Runner, targets []string) *serviceMemoRunner {
	out, _, _, err := runner.Exec(serviceBatchCmd(targets))
	if err != nil {
		return nil // ponytail: never answer from a failed batch
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(targets) || lines[0] == serviceBatchSentinel {
		return nil
	}
	answers := make(map[string]string, len(targets))
	for i, t := range targets {
		answers[serviceStatusCmd(t)] = strings.TrimSpace(lines[i]) + "\n"
	}
	return &serviceMemoRunner{Runner: runner, answers: answers}
}

func (ins *Inspector) evaluateDesiredRules(h *store.Host, runner sshrunner.Runner) (bool, []string, int, error) {
	rules, err := ins.db.ListDesiredRules(h.ID)
	if err != nil {
		return false, nil, 0, err
	}
	debounceRuns := 1
	if debStr, err := ins.db.GetSetting("alert_debounce_runs"); err == nil {
		debounceRuns = ParseDebounceRuns(debStr)
	}

	// ponytail: snapshot prior statuses once; per-rule ListActualItems made evaluation O(N^2) in rules.
	existingItems, _ := ins.db.ListActualItems(h.ID)
	prevStatuses := make(map[string]string, len(existingItems))
	for _, ex := range existingItems {
		prevStatuses[ex.Kind+"\x00"+ex.Target] = ex.Status
	}

	driftFound := false
	driftSummaries := make([]string, 0, len(rules))
	checkedCount := 0
	cronMemo := &crontabMemoRunner{Runner: runner}

	// ponytail: one batched systemctl exec replaces one exec per service rule (Linux+systemd only).
	var ruleRunner sshrunner.Runner = cronMemo
	if !strings.Contains(strings.ToLower(h.OSInfo), "darwin") {
		seen := make(map[string]bool)
		var svcTargets []string
		for _, rule := range rules {
			if rule.Enabled && rule.Kind == "service" && rule.Target != "" && !seen[rule.Target] {
				seen[rule.Target] = true
				svcTargets = append(svcTargets, rule.Target)
			}
		}
		if len(svcTargets) >= 2 {
			if svcMemo := prefetchServiceStates(cronMemo, svcTargets); svcMemo != nil {
				ruleRunner = svcMemo
			}
		}
	}

	actualItems := make([]*store.ActualItem, 0, len(rules)) // ponytail: one batched transaction instead of one commit per rule

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		checkedCount++

		current, status, details, rootCause := ins.evaluateRule(h, ruleRunner, &rule)
		if status == "drift" {
			driftFound = true
			msg := fmt.Sprintf("%s '%s': %s", rule.Kind, rule.Target, details)
			if rootCause != "" {
				msg += fmt.Sprintf(" (%s)", rootCause)
			}
			driftSummaries = append(driftSummaries, msg)
		}

		prevStatus := prevStatuses[rule.Kind+"\x00"+rule.Target]

		// Record actual item state
		actualItems = append(actualItems, &store.ActualItem{
			HostID:  h.ID,
			Kind:    rule.Kind,
			Target:  rule.Target,
			Current: current,
			Status:  status,
			Details: details,
		})

		// Transition into drift -> record incident + alert + root cause excerpt
		triggerAlert := false
		if ins.debouncer != nil {
			triggerAlert = ins.debouncer.Observe(h.ID, rule.Kind, rule.Target, status, debounceRuns)
		} else if status == "drift" && prevStatus != "drift" {
			triggerAlert = true
		}

		// Transition into drift after debounce -> record incident + alert + root cause excerpt
		if triggerAlert {
			inc := &store.Incident{
				HostID:           h.ID,
				Kind:             rule.Kind,
				Target:           rule.Target,
				EventType:        "drift_detected",
				Summary:          fmt.Sprintf("Drift detected: %s '%s' is %s (expected %s)", rule.Kind, rule.Target, current, ruleExpectedSummary(rule)),
				RootCauseExcerpt: rootCause,
			}
			_ = ins.db.RecordIncident(inc)

			alt := &store.Alert{
				HostID:    h.ID,
				Level:     "critical",
				Message:   inc.Summary,
				RootCause: rootCause,
			}
			_, _ = ins.db.CreateAlert(alt)

			if ins.notifier != nil {
				ins.notifier.SendAlert(h, &rule, inc.Summary, rootCause)
			}
		} else if status == "ok" && prevStatus == "drift" {
			// Drift resolved
			inc := &store.Incident{
				HostID:           h.ID,
				Kind:             rule.Kind,
				Target:           rule.Target,
				EventType:        "drift_resolved",
				Summary:          fmt.Sprintf("Resolved: %s '%s' is back to desired state (%s)", rule.Kind, rule.Target, current),
				RootCauseExcerpt: "",
			}
			_ = ins.db.RecordIncident(inc)
		}
	}

	_ = ins.db.UpsertActualItems(actualItems)
	_ = ins.db.PruneActualItems(h.ID) // legacy leftovers of rules deleted before pruning existed
	return driftFound, driftSummaries, checkedCount, nil
}

func (ins *Inspector) evaluateRule(h *store.Host, runner sshrunner.Runner, rule *store.DesiredRule) (current, status, details, rootCause string) {
	switch rule.Kind {
	case "container":
		// Check docker container status
		inspectCmd := fmt.Sprintf(`%sdocker inspect -f '{{.State.Status}}|{{.State.ExitCode}}|{{.State.OOMKilled}}|{{.State.Error}}' %s 2>/dev/null`, DarwinPathPrelude, shellQuote(rule.Target))
		out, _, exitCode, _ := runner.Exec(inspectCmd)
		out = strings.TrimSpace(out)
		if exitCode != 0 || out == "" {
			current = "not_found"
			status = "drift"
			rootCause = fmt.Sprintf("Container %s does not exist on host", rule.Target)
			return
		}

		parts := strings.Split(out, "|")
		stateStatus := parts[0]
		current = stateStatus
		if strings.EqualFold(stateStatus, rule.Expected) {
			status = "ok"
		} else {
			status = "drift"
			// Extract Root Cause Excerpt
			var diag strings.Builder
			if len(parts) >= 4 {
				diag.WriteString(fmt.Sprintf("ExitCode: %s, OOMKilled: %s, Error: %s\n", parts[1], parts[2], parts[3]))
			}
			logOut, _, _, _ := runner.Exec(fmt.Sprintf(`%sdocker logs --tail 50 %s 2>&1`, DarwinPathPrelude, shellQuote(rule.Target)))
			diag.WriteString("--- Last 50 lines of container logs ---\n")
			diag.WriteString(strings.TrimSpace(logOut))
			rootCause = diag.String()
		}
		return

	case "disk":
		// Target is mount point (e.g. "/")
		target := rule.Target
		if target == "" {
			target = "/"
		}
		// ponytail: Use timeout wrapper to prevent hanging on stale NFS/remote mounts
		dfCmd := fmt.Sprintf(`sh -c 'if [ "$(uname -s)" = "Darwin" ]; then `+
			`if [ %[1]s = "/" ]; then `+
			`out=$(df -lPk /System/Volumes/Data 2>/dev/null) || out=$(df -lPk / 2>/dev/null); `+
			`echo "$out" | awk '\''NR==2 {used=$2-$4; print $1, $2, used, $4, int(used*100/$2)"%%", "/"}'\''; `+
			`else df -lPk %[1]s 2>/dev/null || df -Pk %[1]s 2>/dev/null; fi; `+
			`else timeout -k 2s 5s df -Pk %[1]s 2>/dev/null || df -Pk %[1]s 2>/dev/null; fi'`, shellQuote(target))
		out, _, exitCode, _ := runner.Exec(dfCmd)
		if exitCode == 124 {
			current = "unresponsive"
			status = "drift"
			rootCause = fmt.Sprintf("Storage/mount point '%s' timed out (possible hung NFS or storage deadlock)", target)
			return
		}
		if exitCode != 0 {
			current = "unreachable"
			status = "drift"
			rootCause = fmt.Sprintf("Failed to query filesystem mount: %s", target)
			return
		}
		used, total := parseDf(out)
		if total == 0 {
			// Fallback parser if mount point does not end with /
			for _, l := range strings.Split(out, "\n") {
				fields := strings.Fields(l)
				if len(fields) >= 6 && fields[1] != "1024-blocks" {
					totK, _ := strconv.ParseInt(fields[1], 10, 64)
					usdK, _ := strconv.ParseInt(fields[2], 10, 64)
					used, total = usdK*1024, totK*1024
					break
				}
			}
		}
		if total > 0 {
			pct := int(float64(used) / float64(total) * 100)
			current = fmt.Sprintf("%d%%", pct)

			// Expected format like "<85%"
			limit := 85
			cleanExp := strings.TrimPrefix(strings.TrimSuffix(rule.Expected, "%"), "<")
			if l, err := strconv.Atoi(cleanExp); err == nil {
				limit = l
			}

			if pct < limit {
				status = "ok"
			} else {
				status = "drift"
				// ponytail: Omit periodic heavy recursive du; df provides instant usage without saturating disk I/O.
				rootCause = fmt.Sprintf("Disk usage on %s is %d%%, exceeding threshold <%d%%", target, pct, limit)
			}
		} else {
			current = "unknown"
			status = "drift"
			rootCause = "Could not parse mount point usage"
		}
		return

	case "backup":
		return evaluateBackupRule(runner, rule)

	case "http":
		return evaluateHTTPRule(runner, rule)

	case "tls":
		return evaluateTLSRule(runner, rule, nil)
	case "cron":
		// Target is keyword or command in crontab
		cronCmd := `crontab -l 2>/dev/null`
		out, _, _, _ := runner.Exec(cronCmd)
		if strings.Contains(out, rule.Target) {
			current = "configured"
			status = "ok"
		} else {
			current = "missing"
			status = "drift"
			rootCause = fmt.Sprintf("Expected crontab entry containing '%s' was not found in user crontab", rule.Target)
		}
		return

	case "service":
		// Target is service name (e.g. "mysql", "mysqld", "nginx", "httpd")
		// Polyglot check supporting macOS launchctl (substring label match), systemd, and SysVinit.
		svcCmd := serviceStatusCmd(rule.Target)
		out, _, _, _ := runner.Exec(svcCmd)
		activeState := strings.TrimSpace(out)
		current = activeState
		if activeState == "active" {
			status = "ok"
		} else {
			status = "drift"
			diagCmd := fmt.Sprintf(`sh -c 'if [ "$(uname -s)" = "Darwin" ]; then `+
				`launchctl print "system/$1" 2>&1 || launchctl list 2>/dev/null | grep -F "$1"; `+
				`elif command -v journalctl >/dev/null 2>&1; then journalctl -u "$1" -n 50 --no-pager 2>&1; `+
				`elif [ -f "/var/log/$1.log" ]; then tail -n 50 "/var/log/$1.log" 2>&1; `+
				`elif [ -f "/var/log/$1/error.log" ]; then tail -n 50 "/var/log/$1/error.log" 2>&1; `+
				`else tail -n 50 /var/log/messages 2>&1 || tail -n 50 /var/log/syslog 2>&1; fi' -- %s`, shellQuote(rule.Target))
			diagOut, _, _, _ := runner.Exec(diagCmd)
			rootCause = fmt.Sprintf("Service %s is %s (expected active)\n--- Service diagnostics ---\n%s", rule.Target, activeState, strings.TrimSpace(diagOut))
		}
		return

	case "port":
		// ponytail: Portable listening port query across distros with ss/netstat fallback
		target := strings.TrimSpace(rule.Target)
		port := target
		if idx := strings.LastIndex(target, ":"); idx != -1 {
			port = target[idx+1:]
		}
		pNum, err := strconv.Atoi(port)
		if err != nil || pNum <= 0 || pNum > 65535 {
			current = "invalid_port"
			status = "drift"
			rootCause = fmt.Sprintf("Invalid port target: %s", target)
			return
		}
		portCmd := fmt.Sprintf(`sh -c 'if [ "$(uname -s)" = "Darwin" ]; then `+
			`(lsof -nP -iTCP:%[1]d -sTCP:LISTEN 2>/dev/null | grep -v "^COMMAND") || (netstat -an -p tcp 2>/dev/null | grep "LISTEN" | grep -E "\.%[1]d\b|:%[1]d\b"); `+
			`elif command -v ss >/dev/null 2>&1; then ss -tlpn 2>/dev/null | grep -E ":%[1]d\b"; `+
			`elif command -v netstat >/dev/null 2>&1; then netstat -tlpn 2>/dev/null | grep -E ":%[1]d\b"; `+
			`else lsof -iTCP -sTCP:LISTEN -P -n 2>/dev/null | grep -E ":%[1]d\b"; fi'`, pNum)
		out, _, exitCode, _ := runner.Exec(portCmd)
		isListening := exitCode == 0 && strings.TrimSpace(out) != ""

		expectedListening := !strings.EqualFold(rule.Expected, "closed") && !strings.EqualFold(rule.Expected, "stopped")
		if isListening {
			current = "listening"
			if expectedListening {
				status = "ok"
			} else {
				status = "drift"
				rootCause = fmt.Sprintf("Port %s is listening (expected %s)", target, rule.Expected)
			}
		} else {
			current = "closed"
			if !expectedListening {
				status = "ok"
			} else {
				status = "drift"
				rootCause = fmt.Sprintf("Port %s is not listening (expected %s)", target, rule.Expected)
			}
		}
		return

	case "process":
		// ponytail: pgrep -f checks matching active process with ps fallback
		procCmd := fmt.Sprintf(`sh -c 'if command -v pgrep >/dev/null 2>&1; then pgrep -fl "$1" 2>/dev/null; else ps -ef 2>/dev/null | grep -v grep | grep "$1"; fi' -- %s`, shellQuote(rule.Target))
		out, _, exitCode, _ := runner.Exec(procCmd)
		out = strings.TrimSpace(out)
		isRunning := exitCode == 0 && out != ""

		expectedRunning := !strings.EqualFold(rule.Expected, "stopped") && !strings.EqualFold(rule.Expected, "inactive")
		if isRunning {
			current = "running"
			if expectedRunning {
				status = "ok"
			} else {
				status = "drift"
				rootCause = fmt.Sprintf("Process matching '%s' is running (expected %s)", rule.Target, rule.Expected)
			}
		} else {
			current = "stopped"
			if !expectedRunning {
				status = "ok"
			} else {
				status = "drift"
				rootCause = fmt.Sprintf("Process matching '%s' is not running (expected %s)", rule.Target, rule.Expected)
			}
		}
		return

	case "security_updates":
		maxAllowed := 0
		exp := strings.TrimSpace(rule.Expected)
		validExpected := false
		if strings.EqualFold(exp, "none") {
			maxAllowed = 0
			validExpected = true
		} else if strings.HasPrefix(exp, "<=") {
			numStr := strings.TrimSpace(strings.TrimPrefix(exp, "<="))
			if n, err := strconv.Atoi(numStr); err == nil && n >= 0 {
				maxAllowed = n
				validExpected = true
			}
		} else if strings.HasPrefix(exp, "<") {
			numStr := strings.TrimSpace(strings.TrimPrefix(exp, "<"))
			if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
				maxAllowed = n - 1
				validExpected = true
			}
		} else if n, err := strconv.Atoi(exp); err == nil && n >= 0 {
			maxAllowed = n
			validExpected = true
		}

		if !validExpected {
			current = "invalid_expected"
			status = "drift"
			details = fmt.Sprintf("invalid expected value '%s'", rule.Expected)
			rootCause = fmt.Sprintf("Invalid expected value %q: must be 'none' or '<=N'", rule.Expected)
			return
		}

		scan, err := ins.db.GetSecurityScan(h.ID)
		if err != nil {
			current = "error"
			status = "drift"
			details = fmt.Sprintf("failed reading security scan: %v", err)
			rootCause = fmt.Sprintf("Failed to read security scan: %v", err)
			return
		}
		if scan == nil {
			current = "not_scanned"
			status = "drift"
			details = "host has not been scanned for security updates"
			rootCause = "Host has not been scanned for security updates yet"
			return
		}
		if scan.Status == "unsupported" {
			current = "unsupported"
			status = "drift"
			details = fmt.Sprintf("unsupported package manager: %s", scan.PackageManager)
			rootCause = fmt.Sprintf("Package manager is unsupported (%s); security updates cannot be determined", scan.PackageManager)
			return
		}
		if scan.Status == "error" {
			current = "error"
			status = "drift"
			details = fmt.Sprintf("security scan error: %s", scan.Error)
			rootCause = fmt.Sprintf("Last security scan failed: %s", scan.Error)
			return
		}
		if time.Since(scan.ScannedAt) > 72*time.Hour {
			current = "stale"
			status = "drift"
			details = fmt.Sprintf("security scan is stale (%s old)", time.Since(scan.ScannedAt).Round(time.Hour))
			rootCause = fmt.Sprintf("Security scan is stale (last scanned %s ago, threshold is 72h)", time.Since(scan.ScannedAt).Round(time.Hour))
			return
		}

		current = fmt.Sprintf("%d pending", scan.PendingSecurity)
		if scan.PendingSecurity <= maxAllowed {
			status = "ok"
			details = fmt.Sprintf("%d pending security updates (allowed: %s)", scan.PendingSecurity, rule.Expected)
			rootCause = ""
		} else {
			status = "drift"
			details = fmt.Sprintf("%d pending security updates exceeds threshold (%s)", scan.PendingSecurity, rule.Expected)
			var rcLines []string
			rcLines = append(rcLines, fmt.Sprintf("%d pending security updates found (allowed: %s):", scan.PendingSecurity, rule.Expected))
			limit := 5
			if len(scan.Findings) < limit {
				limit = len(scan.Findings)
			}
			for i := range limit {
				f := scan.Findings[i]
				adv := ""
				if f.Advisory != "" {
					adv = fmt.Sprintf(" [%s]", f.Advisory)
				}
				fromVer := f.InstalledVersion
				if fromVer == "" {
					fromVer = "none"
				}
				toVer := f.FixedVersion
				if toVer == "" {
					toVer = "latest"
				}
				rcLines = append(rcLines, fmt.Sprintf("- %s: %s -> %s%s (%s)", f.Package, fromVer, toVer, adv, f.Severity))
			}
			if len(scan.Findings) > limit {
				rcLines = append(rcLines, fmt.Sprintf("... and %d more", len(scan.Findings)-limit))
			}
			rootCause = strings.Join(rcLines, "\n")
		}
		return
	case "file_integrity":
		return ins.evaluateFIMRule(h, runner, rule)
	}

	return "unknown", "ok", "", ""
}

// GenerateBaseline autodetects containers, disks, services, and cron jobs and creates DesiredRules.
func (ins *Inspector) GenerateBaseline(hostID int64) ([]store.DesiredRule, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	var rules []store.DesiredRule

	// 1. Root disk rule
	diskRule := store.DesiredRule{
		HostID:   hostID,
		Kind:     "disk",
		Target:   "/",
		Expected: "<85%",
		Enabled:  true,
	}
	_ = ins.db.SaveDesiredRule(&diskRule)
	rules = append(rules, diskRule)

	// 2. Running Docker containers
	dockerOut, _, code, _ := runner.Exec(DarwinPathPrelude + `docker ps --format '{{.Names}}' 2>/dev/null`)
	if code == 0 && dockerOut != "" {
		for _, name := range strings.Split(strings.TrimSpace(dockerOut), "\n") {
			name = strings.TrimSpace(name)
			if name != "" {
				r := store.DesiredRule{
					HostID:   hostID,
					Kind:     "container",
					Target:   name,
					Expected: "running",
					Enabled:  true,
				}
				_ = ins.db.SaveDesiredRule(&r)
				rules = append(rules, r)
			}
		}
	}

	// 3. Known DB & Web services (supporting macOS launchctl, systemd and SysVinit / RedHat naming conventions)
	for _, svc := range []string{"mysql", "mysqld", "mariadb", "postgresql", "redis-server", "redis", "nginx", "httpd", "apache2"} {
		svcCheck := fmt.Sprintf(`sh -c 'if [ "$(uname -s)" = "Darwin" ]; then `+
			`if launchctl print "system/$1" 2>/dev/null | grep -q "state = running"; then echo "active"; `+
			`elif launchctl list 2>/dev/null | grep -F "$1" | awk '\''{if ($1 ~ /^[0-9]+$/ && $1 > 0) exit 0; else exit 1}'\''; then echo "active"; `+
			`else echo "inactive"; fi; `+
			`elif command -v systemctl >/dev/null 2>&1; then systemctl is-active "$1" 2>/dev/null; `+
			`elif service "$1" status 2>&1 | grep -iq "running"; then echo "active"; `+
			`else echo "inactive"; fi' -- %s`, svc)
		out, _, _, _ := runner.Exec(svcCheck)
		if strings.TrimSpace(out) == "active" {
			r := store.DesiredRule{
				HostID:   hostID,
				Kind:     "service",
				Target:   svc,
				Expected: "active",
				Enabled:  true,
			}
			_ = ins.db.SaveDesiredRule(&r)
			rules = append(rules, r)
		}
	}

	// 4. Crontab
	crontabOut, _, _, _ := runner.Exec(`crontab -l 2>/dev/null`)
	for _, line := range strings.Split(strings.TrimSpace(crontabOut), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			// Extract command part
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				cmd := strings.Join(fields[5:], " ")
				// shorten target
				target := cmd
				if len(target) > 40 {
					target = target[:40]
				}
				r := store.DesiredRule{
					HostID:   hostID,
					Kind:     "cron",
					Target:   target,
					Expected: "configured",
					Enabled:  true,
				}
				_ = ins.db.SaveDesiredRule(&r)
				rules = append(rules, r)
			}
		}
	}

	return rules, nil
}
