package osv

// Remote webroot lockfile probe (ADR 0088). Webroot paths live on the managed host, so Pantau
// reads lockfiles over the existing agentless SSH session — never from the Pantau filesystem for
// host paths. The command is read-only, bounded in depth and file size.

import (
	"fmt"
	"strings"
)

// WebrootLockfileCmd builds a read-only command that prints each known lockfile found under the
// given absolute remote directories, framed by deterministic markers. Paths are shell-quoted.
func WebrootLockfileCmd(paths []string) string {
	var b strings.Builder
	b.WriteString(`echo "===WEBROOT_LOCKFILES==="` + "\n")
	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" || !strings.HasPrefix(root, "/") {
			continue
		}
		q := shellQuoteLocal(root)
		b.WriteString(fmt.Sprintf(`if [ -d %s ]; then
  find %s -maxdepth 4 -type f \( -name composer.lock -o -name package-lock.json -o -name pnpm-lock.yaml -o -name yarn.lock \) 2>/dev/null | head -n 50 | while IFS= read -r f; do
    [ -f "$f" ] || continue
    [ "$(wc -c < "$f" 2>/dev/null)" -gt 8388608 ] && continue
    printf '===LOCKFILE:%%s:%%s===\n' "$f" "$(basename "$f")"
    cat "$f"
    printf '\n===LOCKFILE_END===\n'
  done
fi
`, q, q))
	}
	b.WriteString(`echo "===WEBROOT_LOCKFILES_END==="` + "\n")
	return b.String()
}

// shellQuoteLocal quotes a path for POSIX sh (single quotes, escaping embedded quotes).
func shellQuoteLocal(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// WebrootLockfile is one lockfile read from a remote webroot.
type WebrootLockfile struct {
	Path string
	Name string
	Data []byte
}

// ParseWebrootLockfiles parses the framed output of WebrootLockfileCmd.
func ParseWebrootLockfiles(section string) []WebrootLockfile {
	var out []WebrootLockfile
	rest := section
	for {
		i := strings.Index(rest, "===LOCKFILE:")
		if i < 0 {
			break
		}
		rest = rest[i+len("===LOCKFILE:"):]
		j := strings.Index(rest, "===\n")
		if j < 0 {
			break
		}
		header := rest[:j]
		rest = rest[j+len("===\n"):]
		parts := strings.SplitN(header, ":", 2)
		if len(parts) != 2 {
			continue
		}
		end := strings.Index(rest, "===LOCKFILE_END===")
		if end < 0 {
			break
		}
		out = append(out, WebrootLockfile{Path: parts[0], Name: parts[1], Data: []byte(strings.TrimSuffix(rest[:end], "\n"))})
		rest = rest[end+len("===LOCKFILE_END==="):]
	}
	return out
}
