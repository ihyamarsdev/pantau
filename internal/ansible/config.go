package ansible

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaxConfigSize bounds the user-supplied ansible.cfg additions.
const MaxConfigSize = 16 * 1024

// ConfigSections are the only sections users may add to; everything else (inventory, galaxy servers, ...) is
// either managed by Pantau or has no meaning for a run.
var ConfigSections = []string{"defaults", "privilege_escalation", "ssh_connection"}

// ManagedConfigKeys are fixed by Pantau (generated inventory, key files, non-interactive operation, search
// paths) and rejected in user config.
var ManagedConfigKeys = []string{
	"ask_pass", "ask_vault_pass", "become_ask_pass", "become_password_file", "collections_path", "collections_paths",
	"connection_password_file", "home", "host_key_checking", "inventory", "nocows", "private_key_file",
	"retry_files_enabled", "roles_path", "ssh_args",
}

var (
	configKeyRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	// baseConfig is what every run gets. -F /dev/null: OpenSSH reads ~/.ssh/config from the passwd home (not $HOME)
	// and aborts on "Bad owner or permissions" for bind-mounted configs (Docker); hosts are fully described by the
	// generated inventory. No ControlPersist: a lingering mux master would outlive the run and the workspace.
	baseConfig = map[string]map[string]string{
		"defaults":       {"host_key_checking": "False", "retry_files_enabled": "False", "nocows": "1"},
		"ssh_connection": {"ssh_args": "-F /dev/null -o UserKnownHostsFile=/dev/null"},
	}
)

// ParseConfig validates user ansible.cfg additions: INI with allowed sections only, one `key = value` per line,
// no multi-line values, no Pantau-managed keys. "" is valid.
func ParseConfig(text string) (map[string]map[string]string, error) {
	if len(text) > MaxConfigSize {
		return nil, fmt.Errorf("config exceeds %d KiB", MaxConfigSize/1024)
	}
	managed := map[string]bool{}
	for _, k := range ManagedConfigKeys {
		managed[k] = true
	}
	allowed := map[string]bool{}
	for _, s := range ConfigSections {
		allowed[s] = true
	}
	out := map[string]map[string]string{}
	section := ""
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		fail := func(format string, a ...any) (map[string]map[string]string, error) {
			return nil, fmt.Errorf("line %d: %s", i+1, fmt.Sprintf(format, a...))
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			return fail("multi-line values are not supported")
		}
		if line[0] == '[' {
			if !strings.HasSuffix(line, "]") {
				return fail("invalid section header")
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			if !allowed[section] {
				return fail("section [%s] is not allowed (use %s)", section, strings.Join(ConfigSections, ", "))
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			continue
		}
		if section == "" {
			return fail("key outside of a [section]")
		}
		sep := strings.IndexAny(line, "=:")
		if sep < 0 {
			return fail("expected key = value")
		}
		key, val := strings.TrimSpace(line[:sep]), strings.TrimSpace(line[sep+1:])
		if !configKeyRe.MatchString(key) {
			return fail("invalid key %q", key)
		}
		if managed[key] {
			return fail("key %q is managed by Pantau", key)
		}
		if _, dup := out[section][key]; dup {
			return fail("duplicate key %q in [%s]", key, section)
		}
		out[section][key] = val
	}
	return out, nil
}

// BuildConfig renders the ansible.cfg of a run: Pantau's base settings plus the validated user additions.
func BuildConfig(user string) (string, error) {
	extra, err := ParseConfig(user)
	if err != nil {
		return "", err
	}
	merged := map[string]map[string]string{}
	for _, src := range []map[string]map[string]string{baseConfig, extra} {
		for sec, kv := range src {
			if merged[sec] == nil {
				merged[sec] = map[string]string{}
			}
			for k, v := range kv {
				merged[sec][k] = v
			}
		}
	}
	secs := make([]string, 0, len(merged))
	for s := range merged {
		secs = append(secs, s)
	}
	sort.Strings(secs)
	var b strings.Builder
	for _, s := range secs {
		keys := make([]string, 0, len(merged[s]))
		for k := range merged[s] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "[%s]\n", s)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s = %s\n", k, merged[s][k])
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
