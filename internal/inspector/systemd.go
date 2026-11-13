package inspector

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SystemdService is one systemd service unit with its runtime and boot-time state.
type SystemdService struct {
	Name        string `json:"name"`
	Load        string `json:"load"`    // loaded, not-found, masked, error; unloaded = a unit file systemd has not loaded
	Active      string `json:"active"`  // active, inactive, failed, activating, deactivating, reloading
	Sub         string `json:"sub"`     // running, exited, dead, failed, ...
	Enabled     string `json:"enabled"` // enabled, disabled, static, masked, indirect, generated, alias, "" (unknown)
	Description string `json:"description"`
}

const (
	systemdScriptTimeout = 20 * time.Second
	systemdUnsupported   = "===UNSUPPORTED==="
	systemdSectionUnits  = "===UNITS==="
	systemdSectionFiles  = "===FILES==="
)

// systemdScript only reads state (no sudo needed). It runs under `sh -c` and reports UNSUPPORTED on hosts
// where systemd is not the init system (OpenRC, SysV, macOS, containers without systemd).
var systemdScript = `if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then echo "` + systemdUnsupported + `"; exit 0; fi
echo "` + systemdSectionUnits + `"
systemctl list-units --type=service --all --no-pager --no-legend --plain 2>/dev/null
echo "` + systemdSectionFiles + `"
systemctl list-unit-files --type=service --no-pager --no-legend 2>/dev/null
`

// ListSystemdServices returns every service unit known to systemd: the loaded ones and the installed unit files
// that are not loaded. supported is false when the host does not run systemd.
func (ins *Inspector) ListSystemdServices(hostID int64) (services []SystemdService, supported bool, err error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return nil, false, err
	}
	defer runner.Close()

	stdout, stderr, exitCode, err := runner.ExecTimeout("sh -c "+shellQuote(systemdScript), systemdScriptTimeout)
	if err != nil {
		return nil, false, err
	}
	if exitCode != 0 && strings.TrimSpace(stdout) == "" {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = fmt.Sprintf("systemctl failed (exit %d)", exitCode)
		}
		return nil, false, fmt.Errorf("%s", msg)
	}
	services, supported = parseSystemdServices(stdout)
	return services, supported, nil
}

var systemdTemplateInstanceRe = regexp.MustCompile(`^(.+@)[^.]+(\.service)$`)

// parseSystemdServices merges `list-units` (runtime state) with `list-unit-files` (enabled state, and the units
// that are installed but not loaded) and orders problems first: failed, then activating, active, and finally
// inactive units.
func parseSystemdServices(out string) ([]SystemdService, bool) {
	if strings.Contains(out, systemdUnsupported) {
		return []SystemdService{}, false
	}
	var unitsPart, filesPart string
	if i := strings.Index(out, systemdSectionUnits); i >= 0 {
		rest := out[i+len(systemdSectionUnits):]
		if j := strings.Index(rest, systemdSectionFiles); j >= 0 {
			unitsPart, filesPart = rest[:j], rest[j+len(systemdSectionFiles):]
		} else {
			unitsPart = rest
		}
	}

	enabled := map[string]string{}
	for _, line := range strings.Split(filesPart, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasSuffix(f[0], ".service") {
			enabled[f[0]] = f[1]
		}
	}

	services := []SystemdService{}
	seen := map[string]bool{}
	for _, line := range strings.Split(unitsPart, "\n") {
		f := strings.Fields(line)
		// Older systemd marks problem units with a leading bullet even with --plain.
		for len(f) > 0 && (f[0] == "●" || f[0] == "*" || f[0] == "○") {
			f = f[1:]
		}
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		s := SystemdService{
			Name:        f[0],
			Load:        f[1],
			Active:      f[2],
			Sub:         f[3],
			Description: strings.Join(f[4:], " "),
		}
		if st, ok := enabled[s.Name]; ok {
			s.Enabled = st
		} else if m := systemdTemplateInstanceRe.FindStringSubmatch(s.Name); m != nil {
			s.Enabled = enabled[m[1]+m[2]]
		}
		services = append(services, s)
		seen[s.Name] = true
	}
	// list-units only knows units in memory: a stopped service that nothing pulls in exists as a unit file alone.
	// A template (name@.service) cannot run without an instance, and an alias is a second name of a listed unit.
	for name, st := range enabled {
		if seen[name] || strings.Contains(name, "@.") || st == "alias" {
			continue
		}
		load := "unloaded"
		if strings.HasPrefix(st, "masked") {
			load = "masked"
		}
		services = append(services, SystemdService{Name: name, Load: load, Active: "inactive", Sub: "dead", Enabled: st})
	}

	rank := func(s SystemdService) int {
		switch {
		case s.Active == "failed":
			return 0
		case s.Active == "activating" || s.Active == "deactivating" || s.Active == "reloading":
			return 1
		case s.Active == "active":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(services, func(i, j int) bool {
		ri, rj := rank(services[i]), rank(services[j])
		if ri != rj {
			return ri < rj
		}
		return services[i].Name < services[j].Name
	})
	return services, true
}
