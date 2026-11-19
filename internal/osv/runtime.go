package osv

// Runtime probes (ADR 0088): lightweight version detection for manually installed or
// container-bundled runtimes (nvm, custom PHP/Node builds) that never appear in the OS package
// inventory. The probe command runs on the host during security inspection; its output is parsed
// here into OSV packages so those runtimes are checked alongside OS packages.

import (
	"bufio"
	"regexp"
	"strings"
)

// RuntimeProbeCmd is the read-only remote script appended to the security scan. Each line it
// prints is "NAME<TAB>VERSION" and is parsed by ParseRuntimeProbes. It never writes to the host.
const RuntimeProbeCmd = `
echo "===RUNTIME_PROBES==="
if command -v php >/dev/null 2>&1; then
    v=$(php -r 'echo PHP_VERSION;' 2>/dev/null || php -v 2>/dev/null | head -n1)
    [ -n "$v" ] && printf 'php\t%s\n' "$v"
fi
if command -v node >/dev/null 2>&1; then
    v=$(node -v 2>/dev/null)
    [ -n "$v" ] && printf 'node\t%s\n' "$v"
fi
if command -v nginx >/dev/null 2>&1; then
    v=$(nginx -V 2>&1 | sed -n 's/.*nginx\/\([0-9][^ ]*\).*/\1/p' | head -n1)
    [ -n "$v" ] && printf 'nginx\t%s\n' "$v"
fi
echo "===RUNTIME_PROBES_END==="
`

// RuntimePackage is one detected runtime and the OSV ecosystem its version is queried in.
type RuntimePackage struct {
	Name      string `json:"name"`      // php | node | nginx
	Version   string `json:"version"`   // detected version
	Ecosystem string `json:"ecosystem"` // OSV ecosystem (Packagist / npm / Debian:<n> ...)
	Source    string `json:"source"`    // "runtime-probe"
}

var (
	runtimeVersionRe = regexp.MustCompile(`[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[-+][0-9A-Za-z.]+)?`)
)

// ParseRuntimeProbes parses the ===RUNTIME_PROBES=== section. osEcosystem is the OSV ecosystem of
// the host (e.g. "Debian:12"); nginx versions are matched there, PHP in Packagist and Node in npm.
func ParseRuntimeProbes(section, osEcosystem string) []RuntimePackage {
	var out []RuntimePackage
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(section))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			// tolerate whitespace-separated output from older probes
			parts = strings.Fields(line)
		}
		if len(parts) != 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		version := runtimeVersionRe.FindString(strings.TrimSpace(parts[1]))
		if version == "" {
			continue
		}
		eco := ""
		switch name {
		case "php":
			eco = EcoPackagist
		case "node":
			eco = EcoNPM
		case "nginx":
			eco = osEcosystem // nginx advisories are tracked per-distro in OSV
		default:
			continue
		}
		key := name + "\x00" + version
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, RuntimePackage{Name: name, Version: version, Ecosystem: eco, Source: "runtime-probe"})
	}
	return out
}

// RuntimeOSVPackages converts runtime probes into per-ecosystem OSV queries.
func RuntimeOSVPackages(probes []RuntimePackage) map[string][]Package {
	byEco := map[string][]Package{}
	for _, p := range probes {
		if p.Ecosystem == "" {
			continue
		}
		byEco[p.Ecosystem] = append(byEco[p.Ecosystem], Package{Name: p.Name, Version: p.Version})
	}
	return byEco
}
