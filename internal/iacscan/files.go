package iacscan

import (
	"path/filepath"
	"strings"
)

// fileKind is the kind of file a rule family applies to.
type fileKind int

const (
	kindOther fileKind = iota
	kindTerraform
	kindAnsible
	kindCompose
	kindDockerfile
)

func kindOfFile(rel string, content string) fileKind {
	base := strings.ToLower(filepath.Base(rel))
	switch {
	case base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") || strings.HasSuffix(base, ".dockerfile"):
		return kindDockerfile
	case base == "docker-compose.yml" || base == "docker-compose.yaml" || base == "compose.yml" || base == "compose.yaml" ||
		strings.HasPrefix(base, "docker-compose."):
		return kindCompose
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".tf", ".tfvars":
		return kindTerraform
	case ".yml", ".yaml":
		if looksLikeCompose(content) {
			return kindCompose
		}
		return kindAnsible
	}
	return kindOther
}

// looksLikeCompose tells a docker compose file from an Ansible playbook by its top-level "services" key.
func looksLikeCompose(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if t == "services:" {
			return true
		}
		if !strings.HasPrefix(t, " ") && !strings.HasPrefix(t, "\t") && !strings.HasPrefix(t, "-") {
			// first top-level key that is not "services" ends the search
			return strings.HasPrefix(t, "version:")
		}
	}
	return false
}

// scanFile dispatches the rule families of one file.
func scanFile(res *Result, rel string, content string, opts Options) {
	kind := kindOfFile(rel, content)
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		if hasIgnoreComment(line) {
			continue
		}
		switch kind {
		case kindTerraform:
			scanTerraformLine(res, rel, i+1, line, content, lines)
		case kindAnsible:
			scanAnsibleLine(res, rel, i+1, line, lines)
		case kindCompose:
			scanComposeLine(res, rel, i+1, line)
		case kindDockerfile:
			scanDockerfileLine(res, rel, i+1, line)
		}
	}

	// Secret rules apply to every text file.
	scanSecrets(res, rel, lines)

	// File-level rules.
	switch kind {
	case kindTerraform:
		scanTerraformFile(res, rel, content)
	case kindAnsible:
		scanAnsibleFile(res, rel, content)
	case kindDockerfile:
		scanDockerfileFile(res, rel, content)
	}
}

// hasIgnoreComment reports whether the line carries the inline suppression comment.
func hasIgnoreComment(line string) bool {
	return strings.Contains(line, "pantau:ignore")
}

func splitLines(s string) []string { return strings.Split(s, "\n") }
