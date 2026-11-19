package osv

// Lockfile parsing for application ecosystems (ADR 0088). The parsers are deliberately
// dependency-free: Pantau is a single binary and the lockfiles are simple enough to read with
// the standard library. An unrecognised or malformed file returns an error, never an empty
// "clean" list — the caller reports "not assessed" instead.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// AppPackage is one dependency read from an application lockfile.
type AppPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"` // lockfile path, relative to the repo/webroot when known
	Kind    string `json:"kind"`   // composer | npm | pnpm | yarn
}

// LockfileKind maps a file name to its ecosystem kind. "" means unsupported.
func LockfileKind(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "composer.lock":
		return "composer"
	case "package-lock.json":
		return "npm"
	case "pnpm-lock.yaml", "pnpm-lock.yml":
		return "pnpm"
	case "yarn.lock":
		return "yarn"
	default:
		return ""
	}
}

// LockfileNames are the file names scanned in a repository or webroot, in priority order.
var LockfileNames = []string{"composer.lock", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"}

// ParseLockfile parses the bytes of a lockfile identified by name. kind is derived from name;
// an explicit kind may be passed by the caller (e.g. from LockfileKind). source is stored on
// each returned package for traceability.
func ParseLockfile(name, source string, data []byte) ([]AppPackage, string, error) {
	kind := LockfileKind(name)
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(name))
	}
	var (
		pkgs []AppPackage
		err  error
	)
	switch kind {
	case "composer":
		pkgs, err = parseComposerLock(data)
	case "npm":
		pkgs, err = parsePackageLock(data)
	case "pnpm":
		pkgs, err = parsePnpmLock(data)
	case "yarn":
		pkgs, err = parseYarnLock(data)
	default:
		return nil, "", fmt.Errorf("unsupported lockfile %q", name)
	}
	if err != nil {
		return nil, kind, err
	}
	out := make([]AppPackage, 0, len(pkgs))
	seen := map[string]bool{}
	for _, p := range pkgs {
		p.Name, p.Version = strings.TrimSpace(p.Name), strings.TrimSpace(p.Version)
		if p.Name == "" || p.Version == "" {
			continue
		}
		p.Source, p.Kind = source, kind
		key := p.Kind + "\x00" + p.Name + "\x00" + p.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, kind, nil
}

// AppPackages converts parsed dependencies into the packages OSV queries. The ecosystem is
// taken from the lockfile kind so composer and npm never mix.
func AppPackages(pkgs []AppPackage) map[string][]Package {
	byKind := map[string][]Package{}
	for _, p := range pkgs {
		eco, ok := AppEcosystem(p.Kind)
		if !ok {
			continue
		}
		byKind[eco.Name] = append(byKind[eco.Name], Package{Name: p.Name, Version: p.Version})
	}
	return byKind
}

// ---------------------------------------------------------------------------
// composer.lock (JSON)

type composerLock struct {
	Packages    []composerEntry `json:"packages"`
	PackagesDev []composerEntry `json:"packages-dev"`
}

type composerEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func parseComposerLock(data []byte) ([]AppPackage, error) {
	var lock composerLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse composer.lock: %w", err)
	}
	entries := append(append([]composerEntry(nil), lock.Packages...), lock.PackagesDev...)
	if len(entries) == 0 {
		return nil, fmt.Errorf("composer.lock has no packages")
	}
	out := make([]AppPackage, 0, len(entries))
	for _, e := range entries {
		out = append(out, AppPackage{Name: e.Name, Version: strings.TrimPrefix(e.Version, "v")})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// package-lock.json (JSON, npm v1/v2/v3)

type packageLock struct {
	Packages map[string]struct {
		Version string `json:"version"`
	} `json:"packages"`
	Dependencies map[string]struct {
		Version string `json:"version"`
	} `json:"dependencies"`
}

func parsePackageLock(data []byte) ([]AppPackage, error) {
	var lock packageLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse package-lock.json: %w", err)
	}
	var out []AppPackage
	for path, e := range lock.Packages {
		name := npmNameFromPath(path)
		if name == "" || e.Version == "" {
			continue
		}
		out = append(out, AppPackage{Name: name, Version: e.Version})
	}
	// v1 lockfiles only have the dependencies tree.
	if len(out) == 0 {
		for name, e := range lock.Dependencies {
			if name == "" || e.Version == "" {
				continue
			}
			out = append(out, AppPackage{Name: name, Version: e.Version})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("package-lock.json has no packages")
	}
	return out, nil
}

// npmNameFromPath turns "node_modules/@scope/pkg" or "node_modules/a/node_modules/b" into the
// package name (the last node_modules segment, scope preserved). The root entry "" is skipped.
func npmNameFromPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	idx := strings.LastIndex(p, "node_modules/")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(p[idx+len("node_modules/"):])
}

// ---------------------------------------------------------------------------
// pnpm-lock.yaml (lightweight, indentation-based)

var (
	pnpmPkgKeyRe  = regexp.MustCompile(`^  ([^\s#][^:]*):\s*(?:#.*)?$`)
	pnpmSlashRe   = regexp.MustCompile(`^/?((?:@[^/]+/)?[^/@]+)/([^/]+)$`)
	pnpmAtRe      = regexp.MustCompile(`^((?:@[^/]+/)?[^@]+)@(.+)$`)
	pnpmPeerStrip = regexp.MustCompile(`\([^)]*\)`)
)

func parsePnpmLock(data []byte) ([]AppPackage, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	var out []AppPackage
	inSection := false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "packages:") || strings.HasPrefix(line, "snapshots:") {
			inSection = true
			continue
		}
		if inSection && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			inSection = false // left the section
		}
		if !inSection {
			continue
		}
		m := pnpmPkgKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.Trim(m[1], `"'`)
		key = pnpmPeerStrip.ReplaceAllString(key, "")
		name, version := pnpmSplitKey(key)
		if name != "" && version != "" {
			out = append(out, AppPackage{Name: name, Version: version})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse pnpm-lock.yaml: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pnpm-lock.yaml has no packages")
	}
	return out, nil
}

// pnpmSplitKey handles both pnpm v5 "/name/1.2.3" and v6+ "name@1.2.3" key shapes, scoped or not.
func pnpmSplitKey(key string) (string, string) {
	if m := pnpmSlashRe.FindStringSubmatch(key); m != nil {
		return m[1], m[2]
	}
	if m := pnpmAtRe.FindStringSubmatch(key); m != nil {
		return m[1], m[2]
	}
	return "", ""
}

// ---------------------------------------------------------------------------
// yarn.lock

var (
	yarnHeaderRe  = regexp.MustCompile(`^([^\s#].*):\s*$`)
	yarnVersionRe = regexp.MustCompile(`^\s+version\s+["']?([^"'\s,]+)["']?\s*,?\s*$`)
	yarnDescRe    = regexp.MustCompile(`^((?:@[^/]+/)?[^@]+)@`)
)

func parseYarnLock(data []byte) ([]AppPackage, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	var out []AppPackage
	var pending []string
	flush := func(version string) {
		if version == "" {
			return
		}
		for _, d := range pending {
			name := yarnNameFromDescriptor(d)
			if name != "" {
				out = append(out, AppPackage{Name: name, Version: version})
			}
		}
		pending = nil
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if h := yarnHeaderRe.FindStringSubmatch(line); h != nil {
			flush("")
			for _, d := range strings.Split(h[1], ",") {
				d = strings.Trim(strings.TrimSpace(d), `"'`)
				if d != "" {
					pending = append(pending, d)
				}
			}
			continue
		}
		if v := yarnVersionRe.FindStringSubmatch(line); v != nil && len(pending) > 0 {
			flush(v[1])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse yarn.lock: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("yarn.lock has no packages")
	}
	return out, nil
}

// yarnNameFromDescriptor maps "foo@^1.0.0", "@scope/foo@npm:^1" or "foo@npm:1" to "foo" /
// "@scope/foo". Yarn v2+ uses "npm:" protocol specifiers; the name is before the first @ that
// starts the range (the last @ for unscoped, the @ after the scope for scoped packages).
func yarnNameFromDescriptor(d string) string {
	d = strings.Trim(strings.TrimSpace(d), `"'`)
	// Strip the range/protocol first so only the name remains.
	if strings.HasPrefix(d, "@") {
		// scoped: @scope/name@range
		slash := strings.Index(d, "/")
		if slash < 0 {
			return ""
		}
		at := strings.LastIndex(d, "@")
		if at <= slash {
			return d // no range, unlikely but harmless
		}
		return d[:at]
	}
	if m := yarnDescRe.FindStringSubmatch(d); m != nil {
		return m[1]
	}
	return ""
}
