package terraform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var secretKeyRe = regexp.MustCompile(`(?i)token|secret|password|passphrase|key-data|certificate-data|client-key|private[_-]?key`)

// secretParts returns the sensitive pieces of a multi-line / JSON credential, so output
// that prints only part of the file (e.g. `grep token`) is still masked.
func secretParts(content string) []string {
	var out []string
	add := func(v string) {
		if v = strings.Trim(strings.TrimSpace(v), `"'`); len(v) >= 4 {
			out = append(out, v)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		k, v, ok := strings.Cut(line, ":")
		if ok && secretKeyRe.MatchString(strings.Trim(k, `"' `)) {
			add(v)
		}
	}
	var js map[string]any
	if json.Unmarshal([]byte(content), &js) == nil {
		for _, k := range []string{"private_key", "private_key_id"} {
			if v, _ := js[k].(string); len(v) >= 4 {
				out = append(out, v)
				var b bytes.Buffer
				enc := json.NewEncoder(&b)
				enc.SetEscapeHTML(false)
				if enc.Encode(v) == nil {
					out = append(out, strings.Trim(strings.TrimSpace(b.String()), `"`))
				}
			}
		}
	}
	return out
}

// logSecrets lists the values to mask in run logs: sensitive vars, every Env value
// except PlainEnv keys, and the content of EnvFiles.
func (c Config) logSecrets() []string {
	var out []string
	for _, v := range c.Vars {
		if v.Sensitive && v.Value != "" {
			out = append(out, v.Value)
		}
	}
	for k, v := range c.Env {
		if !c.PlainEnv[k] && len(v) >= 4 {
			out = append(out, v)
		}
	}
	for _, v := range c.EnvFiles {
		out = append(out, v)
	}
	// Whole values first (longest match wins), then their sensitive parts.
	n := len(out)
	for _, v := range out[:n] {
		out = append(out, secretParts(v)...)
	}
	return out
}

// withEnvFiles writes EnvFiles under Root/secrets/<random>/ (dir 0700, files 0600) and
// returns a Config whose Env points the keys at those paths, plus the cleanup func.
func (c Config) withEnvFiles() (Config, func(), error) {
	if len(c.EnvFiles) == 0 {
		return c, func() {}, nil
	}
	base := filepath.Join(c.Root, "secrets")
	if err := os.MkdirAll(base, 0700); err != nil {
		return c, nil, fmt.Errorf("failed to create secrets dir: %w", err)
	}
	dir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return c, nil, fmt.Errorf("failed to create secrets dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	env := make(map[string]string, len(c.Env)+len(c.EnvFiles))
	for k, v := range c.Env {
		env[k] = v
	}
	keys := make([]string, 0, len(c.EnvFiles))
	for k := range c.EnvFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := filepath.Join(dir, filepath.Base(k))
		if err := os.WriteFile(p, []byte(c.EnvFiles[k]), 0600); err != nil {
			cleanup()
			return c, nil, fmt.Errorf("failed to write secret file for %s: %w", k, err)
		}
		env[k] = p
	}
	c.Env = env
	return c, cleanup, nil
}
