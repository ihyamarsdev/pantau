package iacscan

import (
	"regexp"
	"strings"
)

var (
	ansValidateCertsRe  = regexp.MustCompile(`(?i)^\s*validate_certs\s*:\s*(no|false)\s*$`)
	ansInsecurePipeRe   = regexp.MustCompile(`(?i)(curl|wget)[^|]*\|\s*(sudo\s+)?(ba)?sh`)
	ansModePermissiveRe = regexp.MustCompile(`(?i)^\s*mode\s*:\s*['"]?(0777|0666|777|666)['"]?\s*$`)
	ansStateLatestRe    = regexp.MustCompile(`(?i)^\s*state\s*:\s*latest\s*$`)
	ansStrictHostRe     = regexp.MustCompile(`StrictHostKeyChecking\s*=\s*no`)
	ansInventoryPassRe  = regexp.MustCompile(`(?i)ansible_(ssh|become)_pass\s*=`)
	ansBecomeRootRe     = regexp.MustCompile(`(?i)^\s*become_user\s*:\s*root\s*$`)
)

// ansTask is one task block of an Ansible YAML file (the lines between a "- " item and the next one).
type ansTask struct {
	lines []string
	start int // 1-based
}

// parseAnsTasks splits the YAML lines into task items.
func parseAnsTasks(lines []string) []ansTask {
	var tasks []ansTask
	cur := ansTask{start: 1}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			if len(cur.lines) > 0 {
				tasks = append(tasks, cur)
			}
			cur = ansTask{start: i + 1}
		}
		if len(cur.lines) == 0 {
			cur.start = i + 1
		}
		cur.lines = append(cur.lines, line)
	}
	if len(cur.lines) > 0 {
		tasks = append(tasks, cur)
	}
	return tasks
}

// scanAnsibleLine applies the per-line Ansible rules.
func scanAnsibleLine(res *Result, rel string, lineNo int, line string, lines []string) {
	code := stripYAMLComment(line)
	trimmed := strings.TrimSpace(code)

	if ansValidateCertsRe.MatchString(code) {
		add(res, Finding{
			RuleID: "ansible.validate-certs-disabled", Category: CategoryAnsible, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "TLS certificate validation disabled",
			Detail:      "validate_certs is off, so the connection can be intercepted.",
			Remediation: "Set validate_certs to yes and install the CA certificate on the host.",
			Snippet:     trimmed,
		})
	}

	if ansInsecurePipeRe.MatchString(code) {
		add(res, Finding{
			RuleID: "ansible.remote-script-pipe", Category: CategoryAnsible, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "Remote script piped into a shell",
			Detail:      "The command downloads a script and pipes it into a shell, so the host runs code that was never reviewed.",
			Remediation: "Download the script, check its checksum, then run it.",
			Snippet:     trimmed,
		})
	}

	if ansModePermissiveRe.MatchString(code) {
		add(res, Finding{
			RuleID: "ansible.permissive-mode", Category: CategoryAnsible, Severity: SeverityMedium, File: rel, Line: lineNo,
			Title:       "Overly permissive file mode",
			Detail:      "The task sets a world-writable or world-readable mode.",
			Remediation: "Use the narrowest mode the file needs (0644 for files, 0755 for directories).",
			Snippet:     trimmed,
		})
	}

	if ansStateLatestRe.MatchString(code) {
		add(res, Finding{
			RuleID: "ansible.unpinned-package", Category: CategoryAnsible, Severity: SeverityLow, File: rel, Line: lineNo,
			Title:       "Package version is not pinned",
			Detail:      "state: latest installs whatever the repository offers at run time.",
			Remediation: "Pin the version, or keep latest only where an update is expected.",
			Snippet:     trimmed,
		})
	}

	if ansStrictHostRe.MatchString(code) {
		add(res, Finding{
			RuleID: "ansible.strict-host-key-disabled", Category: CategoryAnsible, Severity: SeverityMedium, File: rel, Line: lineNo,
			Title:       "SSH host key check disabled",
			Detail:      "StrictHostKeyChecking=no makes the run blind to a man-in-the-middle.",
			Remediation: "Use StrictHostKeyChecking=yes and manage known_hosts on the server.",
			Snippet:     trimmed,
		})
	}

	if ansInventoryPassRe.MatchString(code) {
		add(res, Finding{
			RuleID: "ansible.inventory-password", Category: CategoryAnsible, Severity: SeverityCritical, File: rel, Line: lineNo,
			Title:       "Plaintext password in the inventory",
			Detail:      "The inventory holds an SSH or become password in plain text.",
			Remediation: "Use SSH keys or Ansible Vault for the inventory passwords.",
			Snippet:     redactLine(trimmed),
		})
	}
}

// scanAnsibleFile applies the task-level Ansible rules.
func scanAnsibleFile(res *Result, rel string, content string) {
	lines := strings.Split(content, "\n")
	for _, task := range parseAnsTasks(lines) {
		body := strings.Join(task.lines, "\n")
		code := stripYAMLComment(body)

		if ansIgnoreErrorsRe.MatchString(code) && ansBecomeRe.MatchString(code) && ansShellRe.MatchString(code) {
			add(res, Finding{
				RuleID: "ansible.ignore-errors-with-become", Category: CategoryAnsible, Severity: SeverityMedium, File: rel, Line: task.start,
				Title:       "Privileged command whose errors are ignored",
				Detail:      "A task runs a privileged shell command with ignore_errors, so a failed change is not noticed.",
				Remediation: "Remove ignore_errors, or handle the failure explicitly.",
				Snippet:     "",
			})
		}

		if ansShellRe.MatchString(code) && ansBecomeRe.MatchString(code) && ansInsecurePipeRe.MatchString(code) {
			// already reported per line
			continue
		}

		if ansSetsPasswordRe.MatchString(code) && !ansNoLogRe.MatchString(code) {
			add(res, Finding{
				RuleID: "ansible.password-without-no-log", Category: CategoryAnsible, Severity: SeverityLow, File: rel, Line: task.start,
				Title:       "Task sets a password without no_log",
				Detail:      "The task writes a password but does not hide it from the log.",
				Remediation: "Add no_log: true to the task.",
				Snippet:     "",
			})
		}
	}
}

var (
	ansIgnoreErrorsRe = regexp.MustCompile(`(?im)^\s*ignore_errors\s*:\s*(yes|true)`)
	ansBecomeRe       = regexp.MustCompile(`(?im)^\s*become\s*:\s*(yes|true)`)
	ansShellRe        = regexp.MustCompile(`(?im)^\s*(shell|command)\s*:`)
	ansNoLogRe        = regexp.MustCompile(`(?im)^\s*no_log\s*:\s*(yes|true)`)
	ansSetsPasswordRe = regexp.MustCompile(`(?im)^\s*(password|passwd|pass|api_key|secret|token)\s*[:=]`)
)

// stripYAMLComment removes a trailing "#" comment outside of quotes.
func stripYAMLComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' || c == '"':
			inStr = !inStr
		case c == '#' && !inStr && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}
