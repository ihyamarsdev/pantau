package inspector

// Webshell FIM preset (ADR 0088/0071): a one-click profile that watches nginx root directories
// plus /tmp and /dev/shm for newly created or modified .php/.js/executable files. The preset is a
// target string accepted by the existing file_integrity rule machinery; the baseline is still
// captured only through the explicit "Capture baseline" action (ADR 0071 — never implicit).

import (
	"fmt"
	"sort"
	"strings"

	"pantau/internal/store"
)

// WebshellPresetConfig describes the directories watched by a webshell FIM preset.
type WebshellPresetConfig struct {
	NginxRoots []string `json:"nginx_roots"` // from the host's nginx server blocks
	ExtraDirs  []string `json:"extra_dirs,omitempty"`
	Extensions []string `json:"extensions,omitempty"` // default .php .js
	Executable bool     `json:"executable"`           // also watch files with the executable bit
}

// DefaultWebshellExtensions are the file extensions watched when none are supplied.
var DefaultWebshellExtensions = []string{".php", ".js"}

// BuildWebshellFIMTarget produces a FIM target string for the preset. Because the FIM evaluator
// hashes literal files (not globs), the returned target is the "preset:webshell" marker; the actual
// file set is expanded at capture/evaluation time by BuildWebshellManifestCommand. Keeping the
// marker as the stored target also keeps it inside the snapshot and the normal rule validation.
func BuildWebshellFIMTarget(cfg WebshellPresetConfig) (string, error) {
	if err := ValidateFIMTarget(FIMPresetWebshell); err != nil {
		return "", err
	}
	return FIMPresetWebshell, nil
}

// webshellWatchDirs returns the deduplicated absolute directories to watch.
func webshellWatchDirs(cfg WebshellPresetConfig) []string {
	dirs := []string{"/tmp", "/dev/shm"}
	for _, r := range cfg.NginxRoots {
		r = strings.TrimSpace(r)
		if r != "" && strings.HasPrefix(r, "/") {
			dirs = append(dirs, r)
		}
	}
	dirs = append(dirs, cfg.ExtraDirs...)
	sort.Strings(dirs)
	out := dirs[:0]
	for _, d := range dirs {
		if len(out) == 0 || out[len(out)-1] != d {
			out = append(out, d)
		}
	}
	return out
}

// BuildWebshellManifestCommand builds a read-only manifest command that hashes new/modified
// .php/.js (and optionally executable) files under the watched directories. It complements the
// regular FIM manifest command; webshell rules use this instead of hashing a fixed file list.
func BuildWebshellManifestCommand(cfg WebshellPresetConfig, trySudo bool) string {
	exts := cfg.Extensions
	if len(exts) == 0 {
		exts = DefaultWebshellExtensions
	}
	var nameArgs []string
	for _, e := range exts {
		e = strings.TrimPrefix(strings.TrimSpace(e), ".")
		if e != "" {
			nameArgs = append(nameArgs, "-name '*."+e+"'")
		}
	}
	nameExpr := strings.Join(nameArgs, " -o ")
	permExpr := ""
	if cfg.Executable {
		permExpr = " -o -perm -u+x"
	}
	findExpr := fmt.Sprintf("\\( %s%s \\) -type f", nameExpr, permExpr)

	var b strings.Builder
	if trySudo {
		b.WriteString(`S=''; if [ "$(id -u)" != 0 ]; then if sudo -n true 2>/dev/null; then S='sudo -n'; else echo 'FIM:SUDO_DENIED'; exit 3; fi; fi; `)
	} else {
		b.WriteString(`S=''; `)
	}
	b.WriteString(`if command -v sha256sum >/dev/null 2>&1; then H='sha256sum'; elif command -v shasum >/dev/null 2>&1; then H='shasum -a 256'; else echo 'FIM:NO_HASH_TOOL'; exit 127; fi; `)
	for _, dir := range webshellWatchDirs(cfg) {
		b.WriteString(`if [ -d ` + shellQuote(dir) + ` ]; then $S find ` + shellQuote(dir) + ` -xdev ` + findExpr + ` 2>/dev/null | LC_ALL=C sort | while IFS= read -r p; do `)
		b.WriteString(fimHashBlock())
		b.WriteString(`; done; fi; `)
	}
	b.WriteString(`echo 'FIM:DONE'; `)
	return "sh -c " + shellQuote(b.String())
}

// WebshellRootsFromDomains extracts unique nginx root directories from a host's stored domains.
func WebshellRootsFromDomains(domains []store.WebSite) []string {
	seen := map[string]bool{}
	var roots []string
	for _, d := range domains {
		r := strings.TrimSpace(d.Root)
		if r != "" && strings.HasPrefix(r, "/") && !seen[r] {
			seen[r] = true
			roots = append(roots, r)
		}
	}
	return roots
}
