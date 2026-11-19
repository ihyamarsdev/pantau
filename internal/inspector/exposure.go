package inspector

// CVE Exposure Radar detection (ADR 0088): running-vs-installed kernel verification, runtime
// probes, and reachability correlation. Everything here is read-only over an existing SSH runner
// or already-stored telemetry; no agent is installed on the host.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pantau/internal/store"
)

// KernelVersionsCmd reports the running kernel (uname -r) and the newest installed kernel package
// across dpkg / rpm / apk / pacman. It is read-only and needs no sudo.
const KernelVersionsCmd = `echo "KERNEL:RUNNING:$(uname -r 2>/dev/null)"
if command -v dpkg-query >/dev/null 2>&1; then
  dpkg-query -W -f='${Package}\t${Version}\n' 'linux-image-*' 2>/dev/null | grep -v -E '(unsigned|dbg|extra|headers)' | sort -V | tail -n1 | awk -F'\t' '{print "KERNEL:INSTALLED:" $2}'
elif command -v rpm >/dev/null 2>&1; then
  rpm -q kernel --qf '%{VERSION}-%{RELEASE}\n' 2>/dev/null | sort -V | tail -n1 | awk '{print "KERNEL:INSTALLED:" $1}'
elif command -v apk >/dev/null 2>&1; then
  apk info -v linux-lts 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+[-_a-z0-9]*' | sort -V | tail -n1 | awk '{print "KERNEL:INSTALLED:" $1}'
elif command -v pacman >/dev/null 2>&1; then
  pacman -Q linux 2>/dev/null | awk '{print "KERNEL:INSTALLED:" $2}'
fi
`

var kernelVersionRe = regexp.MustCompile(`[0-9]+\.[0-9]+(?:\.[0-9]+)?`)

// ParseKernelVersions extracts running and installed versions from KernelVersionsCmd output.
func ParseKernelVersions(out string) (running, installed string) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "KERNEL:RUNNING:"):
			running = strings.TrimSpace(strings.TrimPrefix(line, "KERNEL:RUNNING:"))
		case strings.HasPrefix(line, "KERNEL:INSTALLED:"):
			installed = strings.TrimSpace(strings.TrimPrefix(line, "KERNEL:INSTALLED:"))
		}
	}
	return running, installed
}

// DetectKernelDrift compares the running kernel to the newest installed kernel package. A drift
// means a patched kernel is on disk but not yet booted: reboot required (ADR 0036/0088).
func DetectKernelDrift(running, installed string) *store.KernelDrift {
	if running == "" && installed == "" {
		return nil
	}
	drift := &store.KernelDrift{Running: running, InstalledLatest: installed}
	if running == "" || installed == "" {
		drift.Note = "could not compare: running or installed kernel version is unknown"
		return drift
	}
	rv, iv := kernelVersionRe.FindString(running), kernelVersionRe.FindString(installed)
	if rv == "" || iv == "" {
		drift.Note = "could not compare kernel versions (unrecognised format)"
		return drift
	}
	cmp := compareVersion(rv, iv)
	rRel, iRel := kernelReleaseNum(running), kernelReleaseNum(installed)
	if cmp > 0 || (cmp == 0 && (rRel == "" || iRel == "" || compareVersion(rRel, iRel) >= 0)) {
		// Running upstream version is newer, or equal with no older release: no reboot needed.
		return drift
	}
	// Installed is newer (higher upstream version, or same version with a higher release number).
	drift.RebootRequired = true
	drift.Note = fmt.Sprintf("a newer kernel (%s) is installed but the host is running %s; a reboot is required to activate the patched kernel", installed, running)
	return drift
}

// kernelReleaseNum extracts the package release number (e.g. "91" from "5.15.0-91-generic").
func kernelReleaseNum(v string) string {
	m := regexp.MustCompile(`-(\d+)`).FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	return m[1]
}

// compareVersion compares dotted numeric version strings. Returns -1, 0, 1. Non-numeric segments
// are compared lexically. Enough for kernel/package versions; deliberately conservative.
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv string
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		ai, aerr := strconv.Atoi(av)
		bi, berr := strconv.Atoi(bv)
		switch {
		case aerr == nil && berr == nil:
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
			}
		default:
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Reachability correlation

// PublicBind reports whether a ListeningPort is bound to all interfaces.
func PublicBind(p store.ListeningPort) bool {
	a := strings.ToLower(strings.TrimSpace(p.Address))
	host := a
	if idx := strings.LastIndex(a, ":"); idx >= 0 {
		host = a[:idx]
	}
	host = strings.Trim(host, "[]")
	return p.Public || host == "0.0.0.0" || host == "*" || host == "::" || host == ""
}

// CorrelateReachability builds a Reachability value for a component (process name or port) from
// the host's stored listening ports and the domains/proxies that match it.
func CorrelateReachability(host *store.Host, component string, ports []string, domains []store.WebSite, cfProxiedDomains map[string]bool) store.Reachability {
	r := store.Reachability{Domains: []string{}, Upstreams: []string{}, Ports: []string{}}
	portSet := map[string]bool{}
	for _, p := range ports {
		portSet[strings.TrimSpace(p)] = true
		r.Ports = append(r.Ports, p)
	}

	var listens []store.ListeningPort
	_ = json.Unmarshal([]byte(host.ListeningPorts), &listens)
	for _, lp := range listens {
		procMatch := component != "" && strings.Contains(strings.ToLower(lp.Process), strings.ToLower(component))
		portMatch := portSet[lp.Port]
		if !procMatch && !portMatch {
			continue
		}
		if PublicBind(lp) {
			r.PublicBind = true
		}
	}

	for _, d := range domains {
		match := false
		for _, up := range d.Upstreams {
			if component != "" && strings.Contains(strings.ToLower(up), strings.ToLower(component)) {
				r.Upstreams = append(r.Upstreams, up)
				match = true
			}
		}
		for _, l := range d.Listen {
			for p := range portSet {
				if strings.Contains(l, p) {
					match = true
				}
			}
		}
		if match {
			r.Domains = append(r.Domains, d.Domain)
			if cfProxiedDomains[d.Domain] {
				r.CloudflareProxied = true
			}
		}
	}
	r.Domains = dedupeStrings(r.Domains)
	r.Upstreams = dedupeStrings(r.Upstreams)
	r.Ports = dedupeStrings(r.Ports)
	return r
}

// ElevateSeverity applies the CRITICAL_EXPOSED rule: a vulnerability reachable from the internet
// is elevated to the top radar tier regardless of its feed severity.
func ElevateSeverity(base string, reach store.Reachability) string {
	if reach.IsExposed() {
		return store.ExposureCriticalExposed
	}
	switch strings.ToLower(base) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(base)
	default:
		return store.ExposureUnknown
	}
}

// ---------------------------------------------------------------------------
// Process-telemetry helpers used by both the exposure radar and the anomaly guard.

// processSnapshot is the minimal shape read from a host's stored process telemetry.
type processSnapshot struct {
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	User    string `json:"user"`
	Command string `json:"command"`
}

// parseProcSnapshot parses `ps -eo pid,ppid,user,comm,args` output used by the anomaly probe.
func parseProcSnapshot(out string) []processSnapshot {
	var list []processSnapshot
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[0] == "PID" {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		list = append(list, processSnapshot{PID: pid, PPID: ppid, User: f[2], Command: strings.Join(f[3:], " ")})
	}
	return list
}

// sortFindings orders radar findings by priority then CVE, so the UI and agent see a stable list.
func sortFindings(findings []store.ExposureFinding) {
	rank := func(s string) int {
		switch s {
		case store.ExposureCriticalExposed:
			return 0
		case store.ExposureCritical:
			return 1
		case store.ExposureHigh:
			return 2
		case store.ExposureMedium:
			return 3
		case store.ExposureLow:
			return 4
		default:
			return 5
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if rank(findings[i].Severity) != rank(findings[j].Severity) {
			return rank(findings[i].Severity) < rank(findings[j].Severity)
		}
		return findings[i].ID < findings[j].ID
	})
}

// exposureTimeout bounds the on-demand exposure scan's remote commands.
const exposureTimeout = 60 * time.Second

// ensure context is used (kept for the SSH call sites that pass it in).
var _ = context.Background
