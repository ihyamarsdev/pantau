package inspector

// CVE Exposure Radar orchestration (ADR 0088). ScanExposure assembles findings from four sources:
// (1) OSV matches of installed packages, (2) application lockfiles in cloned repos and webroots,
// (3) runtime probes, and (4) kernel drift, then correlates each with reachability and elevates
// internet-facing findings to CRITICAL_EXPOSED. It is opt-in via osv_enabled and stays airgapped
// until the administrator turns it on (ADR 0036/0068).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pantau/internal/osv"
	"pantau/internal/store"
)

// ExposureScanOptions carries the dependencies the scan needs from the web layer.
type ExposureScanOptions struct {
	OSVClient  *osv.Client
	OSVEnabled bool
	RepoPaths  []string                                                     // absolute clone directories from git_repos
	Webroots   []string                                                     // configured remote webroot paths (host-local, absolute)
	CFProxied  map[string]bool                                              // domains behind a Cloudflare zone
	KEVLookup  func(ctx context.Context, cves []string) map[string]OSVIntel // optional
}

// OSVIntel is the subset of exploit intelligence a caller supplies (ADR 0064).
type OSVIntel struct {
	KEV  bool
	EPSS float64
}

// ScanExposure builds the radar for a host. It never fails the whole scan because one source is
// unavailable: partial coverage is reported honestly in Notes/Status (ADR 0036 honesty rule).
func (ins *Inspector) ScanExposure(ctx context.Context, host *store.Host, opt ExposureScanOptions) *store.ExposureScan {
	res := &store.ExposureScan{
		HostID: host.ID, ScannedAt: time.Now().UTC(), Enabled: opt.OSVEnabled,
		Status: "ok", Findings: []store.ExposureFinding{}, Notes: []string{},
	}
	if !opt.OSVEnabled {
		res.Status = "not_assessed"
		res.Notes = append(res.Notes, "OSV vulnerability lookup is disabled (enable osv_enabled in Settings)")
		return res
	}
	if opt.OSVClient == nil {
		res.Status = "error"
		res.Error = "OSV client is not available"
		return res
	}

	// Domains + listening telemetry for reachability correlation.
	domains, err := ins.db.GetHostDomains(host.ID)
	if err != nil {
		res.Notes = append(res.Notes, "domain inventory unavailable: "+err.Error())
	}
	if opt.CFProxied == nil {
		opt.CFProxied = map[string]bool{}
	}

	// 1. Installed OS packages via OSV.
	eco, ok, reason := osv.EcosystemFor(host.OSInfo)
	if ok {
		pkgs, perr := ins.db.GetHostPackages(host.ID)
		if perr != nil {
			res.Notes = append(res.Notes, "package inventory unavailable: "+perr.Error())
		} else if len(pkgs) > 0 {
			osvPkgs := make([]osv.Package, 0, len(pkgs))
			for _, p := range pkgs {
				name, version := strings.TrimSpace(p.Source), strings.TrimSpace(p.SourceVersion)
				if name == "" {
					name = p.Name
				}
				if version == "" {
					version = p.Version
				}
				if name != "" && version != "" {
					osvPkgs = append(osvPkgs, osv.Package{Name: name, Version: version})
				}
			}
			res.Findings = append(res.Findings, ins.queryOSV(ctx, opt, eco.Name, osvPkgs, store.ExposureSourceOSV, domains, host)...)
		} else {
			res.Notes = append(res.Notes, "package inventory is empty; run an inventory scan")
		}
	} else {
		res.Notes = append(res.Notes, "OS packages not assessed: "+reason)
		res.Status = "partial"
	}

	// 2. Application lockfiles (cloned repos + webroots).
	appPkgs := readLockfiles(opt.RepoPaths, opt.Webroots)
	byEco := osv.AppPackages(appPkgs)
	for ecoName, pkgs := range byEco {
		res.Findings = append(res.Findings, ins.queryOSV(ctx, opt, ecoName, pkgs, store.ExposureSourceLockfile, domains, host)...)
	}
	// 3. Runtime probes, kernel drift, nginx dump, process anomalies and remote webroot lockfiles
	// are all collected in a single agentless SSH session.
	var nginxDump string
	remoteWebroots := opt.Webroots
	if len(remoteWebroots) == 0 {
		remoteWebroots = WebshellRootsFromDomains(domains)
	}
	webrootCmd := ""
	if len(remoteWebroots) > 0 {
		webrootCmd = "\n" + osv.WebrootLockfileCmd(remoteWebroots)
	}
	runner, rerr := ins.GetRunnerForHost(host)
	if rerr != nil {
		res.Notes = append(res.Notes, "runtime/kernel probe unavailable: "+rerr.Error())
		res.Status = degrade(res.Status)
	} else {
		out, _, _, eerr := runner.ExecTimeout(osv.RuntimeProbeCmd+"\n"+KernelVersionsCmd+"\n"+NginxDumpCmd+"\n"+ProcessAnomalyCmd+webrootCmd, exposureTimeout)
		if eerr != nil {
			res.Notes = append(res.Notes, "runtime/kernel probe failed: "+eerr.Error())
			res.Status = degrade(res.Status)
		} else {
			probeSec := between(out, "===RUNTIME_PROBES===", "===RUNTIME_PROBES_END===")
			nginxDump = between(out, "===NGINX_DUMP===", "===NGINX_DUMP_END===")
			procTree := between(out, "===PROC_TREE===", "===PROC_TREE_END===")
			outbound := between(out, "===OUTBOUND===", "===OUTBOUND_END===")
			res.Findings = append(res.Findings, ins.processAnomalyFindings(host, procTree, outbound)...)
			// Remote webroot lockfiles read over SSH (ADR 0088).
			webrootSec := between(out, "===WEBROOT_LOCKFILES===", "===WEBROOT_LOCKFILES_END===")
			for _, wf := range osv.ParseWebrootLockfiles(webrootSec) {
				wpkgs, _, perr := osv.ParseLockfile(wf.Name, wf.Path, wf.Data)
				if perr != nil {
					continue
				}
				for ecoName, pkgs := range osv.AppPackages(wpkgs) {
					res.Findings = append(res.Findings, ins.queryOSV(ctx, opt, ecoName, pkgs, store.ExposureSourceLockfile, domains, host)...)
				}
			}
			osEco := eco.Name
			if !ok {
				osEco = ""
			}
			probes := osv.ParseRuntimeProbes(probeSec, osEco)
			for pEco, pkgs := range osv.RuntimeOSVPackages(probes) {
				res.Findings = append(res.Findings, ins.queryOSV(ctx, opt, pEco, pkgs, store.ExposureSourceRuntime, domains, host)...)
			}
			running, installed := ParseKernelVersions(out)
			if drift := DetectKernelDrift(running, installed); drift != nil {
				res.Kernel = drift
				if drift.RebootRequired {
					f := store.ExposureFinding{
						ID: "kernel:reboot-required", Source: store.ExposureSourceKernel,
						Severity: store.ExposureCritical, BaseSeverity: store.ExposureCritical,
						Title:   "VULNERABLE_RUNNING_KERNEL — Reboot Required",
						Summary: drift.Note, Component: "linux-kernel", Version: running,
						FixedVersion: installed, CVEs: []string{}, RebootRequired: true,
						Mitigations: []string{"upgrade_package", "ansible_patch"},
					}
					f.Reachability = CorrelateReachability(host, "kernel", nil, domains, opt.CFProxied)
					res.Findings = append(res.Findings, f)
				}
			}
		}
	}

	// 4. Nginx static hardening findings from the live config dump.
	if nginxDump != "" {
		res.Findings = append(res.Findings, NginxDumpHardeningFindings(host, nginxDump, domains, opt.CFProxied)...)
	}

	// Deduplicate, cap intelligence, elevate reachability, sort.
	res.Findings = dedupeFindings(res.Findings)
	for i := range res.Findings {
		f := &res.Findings[i]
		f.BaseSeverity = f.Severity
		f.Severity = ElevateSeverity(f.Severity, f.Reachability)
		if f.IsCriticalExposed() {
			res.CriticalExposed++
		}
	}
	sortFindings(res.Findings)
	return res
}

// queryOSV queries one ecosystem and converts hits into radar findings.
func (ins *Inspector) queryOSV(ctx context.Context, opt ExposureScanOptions, ecosystem string, pkgs []osv.Package, source string, domains []store.WebSite, host *store.Host) []store.ExposureFinding {
	if len(pkgs) == 0 {
		return nil
	}
	qctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	res, err := opt.OSVClient.Check(qctx, osv.Ecosystem{Source: osv.SourceOSV, Name: ecosystem}, pkgs)
	if err != nil {
		return []store.ExposureFinding{{
			ID: source + ":" + ecosystem + ":error", Source: source, Severity: store.ExposureUnknown,
			Title: "Vulnerability feed unavailable for " + ecosystem, Summary: err.Error(),
			Component: ecosystem, CVEs: []string{},
		}}
	}
	var cves []string
	for _, f := range res.Findings {
		cves = append(cves, f.CVEs...)
	}
	intel := map[string]OSVIntel{}
	if opt.KEVLookup != nil {
		intel = opt.KEVLookup(qctx, cves)
	}

	out := make([]store.ExposureFinding, 0, len(res.Findings))
	for _, f := range res.Findings {
		reach := CorrelateReachability(host, f.Package, nil, domains, opt.CFProxied)
		kev, epss := false, 0.0
		for _, c := range f.CVEs {
			if v, ok := intel[c]; ok {
				kev = kev || v.KEV
				if v.EPSS > epss {
					epss = v.EPSS
				}
			}
		}
		id := fmt.Sprintf("%s:%s:%s", source, f.Package, strings.Join(f.IDs, ","))
		if len(id) > 240 {
			id = id[:240]
		}
		out = append(out, store.ExposureFinding{
			ID: id, Source: source, Severity: f.Severity, Title: f.Package + " " + f.InstalledVersion,
			Summary: f.Summary, Component: f.Package, Version: f.InstalledVersion,
			FixedVersion: f.FixedVersion, CVEs: f.CVEs, KEV: kev, EPSS: epss,
			Reachability: reach, Mitigations: []string{"upgrade_package", "ansible_patch", "firewall_restrict", "cf_lockdown"},
		})
	}
	return out
}

// readLockfiles walks the given directories for known lockfiles and parses them. Only files on the
// Pantau server (cloned repos, configured webroots) are read — never arbitrary host paths.
func readLockfiles(repoPaths, webroots []string) []osv.AppPackage {
	var out []osv.AppPackage
	seenDir := map[string]bool{}
	for _, root := range append(append([]string{}, repoPaths...), webroots...) {
		root = strings.TrimSpace(root)
		if root == "" || !filepath.IsAbs(root) || seenDir[root] {
			continue
		}
		seenDir[root] = true
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		for _, name := range osv.LockfileNames {
			p := filepath.Join(root, name)
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if len(data) > 8<<20 {
				continue
			} // bound memory on hostile files
			pkgs, _, err := osv.ParseLockfile(name, p, data)
			if err == nil {
				out = append(out, pkgs...)
			}
		}
	}
	return out
}

// dedupeFindings removes findings with the same ID, keeping the first (highest priority after sort).
func dedupeFindings(in []store.ExposureFinding) []store.ExposureFinding {
	sort.SliceStable(in, func(i, j int) bool { return in[i].ID < in[j].ID })
	out := in[:0]
	for i, f := range in {
		if i > 0 && f.ID == in[i-1].ID {
			continue
		}
		out = append(out, f)
	}
	return out
}

func degrade(s string) string {
	if s == "ok" {
		return "partial"
	}
	return s
}

// between returns the substring between two markers, or the whole string when the end marker is absent.
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	body := s[i+len(start):]
	if j := strings.Index(body, end); j >= 0 {
		body = body[:j]
	}
	return strings.TrimSpace(body)
}

// marshalFindings is used by the store layer to persist the radar result.
func marshalFindings(f []store.ExposureFinding) string {
	b, _ := json.Marshal(f)
	return string(b)
}
