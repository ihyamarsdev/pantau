package vault

import (
	"fmt"
	"regexp"
	"strings"
)

// placeholderRE matches a vault template placeholder such as
// "{{ vault.DB_PASSWORD }}", allowing surrounding whitespace.
var placeholderRE = regexp.MustCompile(`\{\{\s*vault\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// Render replaces every "{{ vault.KEY }}" placeholder in template with the
// corresponding value. If a key has no entry in values, Render returns an
// error naming the unknown key.
func Render(template string, values map[string]string) (string, error) {
	var renderErr error
	result := placeholderRE.ReplaceAllStringFunc(template, func(match string) string {
		if renderErr != nil {
			return match
		}
		key := placeholderKey(match)
		value, ok := values[key]
		if !ok {
			renderErr = fmt.Errorf("unknown vault key %q in template", key)
			return match
		}
		return value
	})
	if renderErr != nil {
		return "", renderErr
	}
	return result, nil
}

// placeholderKey extracts the KEY from a full placeholder match.
func placeholderKey(match string) string {
	sub := placeholderRE.FindStringSubmatch(match)
	if len(sub) < 2 {
		return ""
	}
	return sub[1]
}

// Placeholders returns the distinct keys referenced in template, in order of
// first appearance.
func Placeholders(template string) []string {
	matches := placeholderRE.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// ValidateTemplate reports every placeholder in template whose key is not
// present in knownKeys. Each issue carries the 1-based line number of the
// placeholder's first character.
func ValidateTemplate(template string, knownKeys map[string]bool) []TemplateIssue {
	indices := placeholderRE.FindAllStringSubmatchIndex(template, -1)
	if len(indices) == 0 {
		return nil
	}
	var issues []TemplateIssue
	for _, loc := range indices {
		key := template[loc[2]:loc[3]]
		if knownKeys[key] {
			continue
		}
		line := 1 + strings.Count(template[:loc[0]], "\n")
		issues = append(issues, TemplateIssue{
			Line:    line,
			Key:     key,
			Message: fmt.Sprintf("unknown vault key %q", key),
		})
	}
	return issues
}
