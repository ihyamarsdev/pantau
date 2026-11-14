package iacscan

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Redact hides a secret value: the first 4 characters stay readable, the rest becomes ****.
func Redact(value string) string {
	v := strings.TrimSpace(value)
	v = strings.Trim(v, `"'`)
	if len(v) <= 4 {
		return "****"
	}
	return v[:4] + "****"
}

// redactValue redacts one known value inside a line, so a secret that no pattern matches is still hidden.
func redactValue(line, value string) string {
	v := strings.TrimSpace(strings.Trim(value, `"'`))
	if v == "" {
		return line
	}
	return strings.Replace(line, value, Redact(value), 1)
}

// redactLine redacts every known secret pattern found in a line, so the snippet never carries a secret.
func redactLine(line string) string {
	out := line
	for _, re := range secretValuePatterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			return Redact(m)
		})
	}
	return out
}

var secretValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`sk_live_[0-9A-Za-z]{10,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}`),
}

// genericSecretAssign matches `key = "value"` or `key: value` for a secret-looking key.
var genericSecretAssign = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)[A-Za-z0-9_.-]*)\s*[:=]\s*["']?([^"'\s,}]+)`)

// secretKeywordRe spots the secret-looking part of an assignment key.
var secretKeywordRe = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)`)

// placeholderValues are never reported.
var placeholderValues = map[string]bool{
	"changeme": true, "change_me": true, "change-me": true, "placeholder": true,
	"example": true, "your_password": true, "yourpassword": true, "your-secret": true,
	"your_secret": true, "xxx": true, "todo": true, "none": true, "null": true,
	"password": true, "secret": true, "token": true, "test": true, "dummy": true,
	"redacted": true, "hidden": true, "notset": true, "unset": true, "": true,
}

// isPlaceholder reports whether a value is a template, a placeholder or a vault blob.
func isPlaceholder(value string) bool {
	v := strings.TrimSpace(value)
	v = strings.Trim(v, `"'`)
	if placeholderValues[strings.ToLower(v)] {
		return true
	}
	lower := strings.ToLower(v)
	for _, marker := range []string{
		"${", "{{", "{%", "<%", "$var", "var.", "changeme", "change_me", "change-me",
		"your_", "your-", "xxx", "example", "placeholder", "redacted", "dummy", "todo",
		"insert_", "fill_", "replace_", "notset", "not_set",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// ansible-vault encrypted blob
	if strings.Contains(v, "$ANSIBLE_VAULT;") {
		return true
	}
	return false
}

// entropyOK reports whether a value looks random enough to be a real secret.
func entropyOK(value string) bool {
	v := strings.TrimSpace(strings.Trim(value, `"'`))
	if len(v) < 8 {
		return false
	}
	classes := 0
	for _, check := range []func(rune) bool{
		func(r rune) bool { return r >= '0' && r <= '9' },
		func(r rune) bool { return r >= 'a' && r <= 'z' },
		func(r rune) bool { return r >= 'A' && r <= 'Z' },
		func(r rune) bool { return strings.ContainsRune("!@#$%^&*()-_=+[]{}<>?,.;:", r) },
	} {
		for _, r := range v {
			if check(r) {
				classes++
				break
			}
		}
	}
	return classes >= 2
}

// scanSecrets applies the secret rules to every file.
func scanSecrets(res *Result, rel string, lines []string) {
	base := strings.ToLower(filepath.Base(rel))
	ext := strings.ToLower(filepath.Ext(rel))

	// A committed .env file.
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		add(res, Finding{
			RuleID: "secret.env-file", Category: CategorySecret, Severity: SeverityHigh, File: rel, Line: 1,
			Title:       "Environment file committed",
			Detail:      "An .env file is part of the source tree; it usually holds credentials.",
			Remediation: "Add .env to .gitignore and keep the values in a secret store or the environment.",
			Snippet:     "",
		})
	}

	// A committed Terraform state file.
	if base == "terraform.tfstate" || strings.HasSuffix(base, ".tfstate") {
		add(res, Finding{
			RuleID: "secret.tfstate", Category: CategorySecret, Severity: SeverityHigh, File: rel, Line: 1,
			Title:       "Terraform state file committed",
			Detail:      "A state file holds the values of every resource, including secrets that were not marked sensitive.",
			Remediation: "Keep the state in a remote backend and add *.tfstate to .gitignore.",
			Snippet:     "",
		})
	}

	// A private key file name.
	if isPrivateKeyName(base) {
		add(res, Finding{
			RuleID: "secret.private-key-file", Category: CategorySecret, Severity: SeverityCritical, File: rel, Line: 1,
			Title:       "Private key file in the source tree",
			Detail:      "The file name looks like a private key; a committed key can be used to impersonate its owner.",
			Remediation: "Remove the key from the repository, rotate it and keep keys in the SSH Key Library or a secret store.",
			Snippet:     "",
		})
	}

	for i, line := range lines {
		ln := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || hasIgnoreComment(line) {
			continue
		}

		if strings.Contains(line, "-----BEGIN") && strings.Contains(line, "PRIVATE KEY-----") {
			add(res, Finding{
				RuleID: "secret.pem-private-key", Category: CategorySecret, Severity: SeverityCritical, File: rel, Line: ln,
				Title:       "Private key block",
				Detail:      "A PEM private key is embedded in the file.",
				Remediation: "Move the key out of the source tree and rotate it.",
				Snippet:     redactLine(trimmed),
			})
			continue
		}

		for _, r := range []struct {
			re      *regexp.Regexp
			ruleID  string
			title   string
			detail  string
			rem     string
			severit string
		}{
			{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "secret.aws-access-key", "AWS access key ID",
				"An AWS access key ID is hard-coded in the file.", "Revoke the key in IAM and read it from the environment or a secret store.", SeverityCritical},
			{regexp.MustCompile(`ASIA[0-9A-Z]{16}`), "secret.aws-access-key", "AWS access key ID",
				"An AWS access key ID is hard-coded in the file.", "Revoke the key in IAM and read it from the environment or a secret store.", SeverityCritical},
			{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), "secret.github-token", "GitHub token",
				"A GitHub personal access / OAuth / app token is hard-coded in the file.", "Revoke the token on GitHub and read it from the environment.", SeverityCritical},
			{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), "secret.github-token", "GitHub token",
				"A GitHub fine-grained token is hard-coded in the file.", "Revoke the token on GitHub and read it from the environment.", SeverityCritical},
			{regexp.MustCompile(`glpat-[A-Za-z0-9_-]{16,}`), "secret.gitlab-token", "GitLab token",
				"A GitLab personal access token is hard-coded in the file.", "Revoke the token on GitLab and read it from the environment.", SeverityCritical},
			{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), "secret.slack-token", "Slack token",
				"A Slack token is hard-coded in the file.", "Revoke the token in Slack and read it from the environment.", SeverityCritical},
			{regexp.MustCompile(`AIza[0-9A-Za-z_-]{30,}`), "secret.google-api-key", "Google API key",
				"A Google API key is hard-coded in the file.", "Restrict the key in the Cloud Console and read it from the environment.", SeverityHigh},
			{regexp.MustCompile(`sk_live_[0-9A-Za-z]{10,}`), "secret.stripe-key", "Stripe live secret key",
				"A Stripe live key is hard-coded in the file.", "Roll the key in the Stripe dashboard and read it from the environment.", SeverityCritical},
			{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}`), "secret.jwt", "JSON Web Token",
				"A JSON Web Token is hard-coded in the file.", "Do not commit tokens; mint them at runtime.", SeverityHigh},
		} {
			if r.re.MatchString(line) {
				add(res, Finding{
					RuleID: r.ruleID, Category: CategorySecret, Severity: r.severit, File: rel, Line: ln,
					Title: r.title, Detail: r.detail, Remediation: r.rem, Snippet: redactLine(trimmed),
				})
			}
		}

		// aws_secret_access_key assignment
		if m := genericSecretAssign.FindStringSubmatch(line); m != nil {
			key, value := m[1], m[2]
			if !secretKeywordRe.MatchString(key) {
				continue
			}
			if isPlaceholder(value) {
				continue
			}
			if matchesKnownToken(value) {
				continue // already reported by the specific token rule
			}
			lowerKey := strings.ToLower(key)
			severity := SeverityHigh
			switch {
			case strings.Contains(lowerKey, "password"), strings.Contains(lowerKey, "secret"), strings.Contains(lowerKey, "private_key"):
				severity = SeverityCritical
			case strings.Contains(lowerKey, "token"):
				severity = SeverityCritical
			}
			if !entropyOK(value) && severity == SeverityHigh {
				// A short, low-entropy value is more likely a name than a secret; report it only as medium.
				severity = SeverityMedium
			}
			add(res, Finding{
				RuleID: "secret.hardcoded-value", Category: CategorySecret, Severity: severity, File: rel, Line: ln,
				Title:       "Hard-coded credential",
				Detail:      fmt.Sprintf("%s holds a literal value in the source tree.", key),
				Remediation: "Read the value from the environment, an Ansible Vault or a secret store.",
				Snippet:     redactValue(redactLine(trimmed), value),
			})
		}
	}

	// A *.tfvars file with a secret-looking value.
	if ext == ".tfvars" {
		for i, line := range lines {
			m := genericSecretAssign.FindStringSubmatch(line)
			if m == nil || !secretKeywordRe.MatchString(m[1]) || isPlaceholder(m[2]) {
				continue
			}
			add(res, Finding{
				RuleID: "secret.tfvars-value", Category: CategorySecret, Severity: SeverityHigh, File: rel, Line: i + 1,
				Title:       "Secret-looking value in a tfvars file",
				Detail:      fmt.Sprintf("%s holds a literal value in a variable file that is meant to be committed.", m[1]),
				Remediation: "Mark the variable sensitive and pass it with TF_VAR_… from a secret store.",
				Snippet:     redactValue(redactLine(strings.TrimSpace(line)), m[2]),
			})
		}
	}
}

func isPrivateKeyName(base string) bool {
	switch {
	case base == "id_rsa", base == "id_dsa", base == "id_ecdsa", base == "id_ed25519":
		return true
	case strings.HasSuffix(base, ".pem"), strings.HasSuffix(base, ".key"):
		return true
	}
	return false
}

// matchesKnownToken reports whether a value is one of the known token formats (already reported).
func matchesKnownToken(value string) bool {
	for _, re := range secretValuePatterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}
