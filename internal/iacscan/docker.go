package iacscan

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	dockerFromLatestRe = regexp.MustCompile(`(?i)^\s*FROM\s+([^\s:@]+)(:latest)?(\s|$)`)
	dockerUserRe       = regexp.MustCompile(`(?i)^\s*USER\s+\S+`)
	dockerAddURLRe     = regexp.MustCompile(`(?i)^\s*ADD\s+https?://\S+`)
	dockerCurlPipeRe   = regexp.MustCompile(`(?i)(curl|wget)[^|]*\|\s*(ba)?sh`)
	dockerEnvRe        = regexp.MustCompile(`(?i)^\s*(?:ENV|ARG)\s+([A-Za-z0-9_.-]*(?:password|secret|token|api[_-]?key|access[_-]?key|credential)[A-Za-z0-9_.-]*)\s*=\s*(.*)$`)
	composePrivRe      = regexp.MustCompile(`(?i)privileged\s*:\s*true`)
	composeSockRe      = regexp.MustCompile(`(?i)/var/run/docker\.sock`)
)

// scanDockerfileLine applies the Dockerfile rules.
func scanDockerfileLine(res *Result, rel string, lineNo int, line string) {
	code := stripShellComment(line)
	trimmed := strings.TrimSpace(code)

	if m := dockerFromLatestRe.FindStringSubmatch(code); m != nil {
		if m[2] == ":latest" || (m[2] == "" && !strings.Contains(m[0], "@")) {
			add(res, Finding{
				RuleID: "dockerfile.latest-tag", Category: CategoryDockerfile, Severity: SeverityMedium, File: rel, Line: lineNo,
				Title:       "Base image is not pinned",
				Detail:      fmt.Sprintf("The image %s is pulled without a fixed version.", m[1]),
				Remediation: "Pin the base image to a version or a digest.",
				Snippet:     trimmed,
			})
		}
	}

	if dockerAddURLRe.MatchString(code) {
		add(res, Finding{
			RuleID: "dockerfile.add-from-url", Category: CategoryDockerfile, Severity: SeverityMedium, File: rel, Line: lineNo,
			Title:       "ADD from a URL",
			Detail:      "ADD with a URL fetches the content without a checksum check.",
			Remediation: "Use curl with a checksum, or COPY a file that is in the build context.",
			Snippet:     trimmed,
		})
	}

	if dockerCurlPipeRe.MatchString(code) {
		add(res, Finding{
			RuleID: "dockerfile.curl-pipe-shell", Category: CategoryDockerfile, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "Remote script piped into a shell",
			Detail:      "The build downloads a script and pipes it into a shell.",
			Remediation: "Download the script, check its checksum, then run it.",
			Snippet:     trimmed,
		})
	}

	if m := dockerEnvRe.FindStringSubmatch(code); m != nil {
		value := strings.TrimSpace(m[2])
		if value == "" || isPlaceholder(value) {
			return
		}
		add(res, Finding{
			RuleID: "dockerfile.secret-in-env", Category: CategoryDockerfile, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "Secret in an image environment variable",
			Detail:      fmt.Sprintf("%s carries a literal value that ends up in the image.", m[1]),
			Remediation: "Pass the value at run time with --env or a secret file.",
			Snippet:     redactValue(redactLine(trimmed), value),
		})
	}
}

// scanComposeLine applies the docker compose rules.
func scanComposeLine(res *Result, rel string, lineNo int, line string) {
	code := stripShellComment(line)
	trimmed := strings.TrimSpace(code)

	if composePrivRe.MatchString(code) {
		add(res, Finding{
			RuleID: "dockerfile.privileged-container", Category: CategoryDockerfile, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "Container runs privileged",
			Detail:      "A privileged container can escape its isolation and reach the host.",
			Remediation: "Drop privileged and grant only the specific capabilities the container needs.",
			Snippet:     trimmed,
		})
	}

	if composeSockRe.MatchString(code) {
		add(res, Finding{
			RuleID: "dockerfile.docker-sock-mount", Category: CategoryDockerfile, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "Docker socket mounted into a container",
			Detail:      "The Docker socket gives the container full control over the host's containers.",
			Remediation: "Use a read-only proxy or remove the mount.",
			Snippet:     trimmed,
		})
	}
}

// scanDockerfileFile reports a Dockerfile that never switches user.
func scanDockerfileFile(res *Result, rel string, content string) {
	for _, line := range strings.Split(content, "\n") {
		if dockerUserRe.MatchString(stripShellComment(line)) {
			return
		}
	}
	add(res, Finding{
		RuleID: "dockerfile.no-user", Category: CategoryDockerfile, Severity: SeverityMedium, File: rel, Line: 1,
		Title:       "Image runs as root",
		Detail:      "The Dockerfile has no USER instruction, so the container runs as root.",
		Remediation: "Create an unprivileged user and switch to it before the entrypoint.",
		Snippet:     "",
	})
}

// stripShellComment removes a trailing "#" comment (Dockerfile and compose style).
func stripShellComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' || c == '"':
			inStr = !inStr
		case c == '#' && !inStr:
			return line[:i]
		}
	}
	return line
}
