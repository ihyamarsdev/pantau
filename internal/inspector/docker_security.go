package inspector

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"pantau/internal/store"
)

// Container misconfiguration audit and image vulnerability scan for the Docker tab. Everything is
// read-only on the host: one `docker inspect` for the audit, and a vulnerability scanner that must
// already be installed on the host (trivy or grype). Pantau never installs anything.

// ErrDockerSecInProgress is returned when the same host (or host+image) is already being scanned.
var ErrDockerSecInProgress = errors.New("docker security scan already in progress")

const (
	// dockerSecTimeout bounds the misconfiguration audit (one docker inspect over all containers).
	dockerSecTimeout = 60 * time.Second
	// dockerScanTimeout bounds an image scan. It is generous because the scanner may download its
	// vulnerability database first, but still bounded so a hung scan cannot block the UI forever.
	dockerScanTimeout = 10 * time.Minute
	// dockerVulnMax caps the vulnerability list stored per image (highest severity first).
	dockerVulnMax = 200
)

// ---------------------------------------------------------------- misconfiguration audit

// dockerSecAuditScript collects the container configuration on the host. It prints a status marker
// first so an absent Docker daemon or a refused socket is reported honestly instead of looking like
// a clean host.
const dockerSecAuditScript = `if ! command -v docker >/dev/null 2>&1; then echo "===DOCKER:MISSING==="; exit 0; fi
err=$(docker ps -aq 2>&1 >/dev/null)
if [ -n "$err" ]; then
  case "$err" in
    *"ermission denied"*) echo "===DOCKER:PERMISSION==="; exit 0;;
    *"not found"*|*"No such file"*) echo "===DOCKER:MISSING==="; exit 0;;
    *"Cannot connect"*|*"daemon"*) echo "===DOCKER:UNREACHABLE==="; exit 0;;
  esac
fi
ids=$(docker ps -aq 2>/dev/null)
if [ -z "$ids" ]; then echo "===DOCKER:NO_CONTAINERS==="; exit 0; fi
echo "===DOCKER:INSPECT==="
docker inspect $ids
`

// DockerSecurityAudit runs the container misconfiguration audit on a host and stores the result.
func (ins *Inspector) DockerSecurityAudit(hostID int64) (*store.DockerSecurityScan, error) {
	if _, loaded := ins.dockerSecInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		return nil, ErrDockerSecInProgress
	}
	defer ins.dockerSecInFlight.Delete(hostID)

	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerSecAuditScript, dockerSecTimeout)
	if err != nil {
		return nil, err
	}
	scan := parseDockerSecurityAudit(stdout, stderr, exitCode)
	scan.HostID = hostID
	scan.Kind = "misconfig"
	scan.ScannedAt = time.Now()
	if err := ins.db.SaveDockerSecurityScan(scan); err != nil {
		return nil, err
	}
	return scan, nil
}

// parseDockerSecurityAudit turns the remote output into findings. Pure function: no host access.
func parseDockerSecurityAudit(stdout, stderr string, exitCode int) *store.DockerSecurityScan {
	scan := &store.DockerSecurityScan{
		Status:   "ok",
		Findings: []store.DockerSecurityFinding{},
	}
	out := stdout
	switch {
	case strings.Contains(out, "===DOCKER:MISSING==="):
		scan.Status = "unsupported"
		scan.Note = "Docker is not installed on this host, so no container configuration could be assessed."
		return scan
	case strings.Contains(out, "===DOCKER:PERMISSION==="):
		scan.Status = "unsupported"
		scan.Note = "Access to the Docker socket was refused for this SSH user, so no container configuration could be assessed."
		return scan
	case strings.Contains(out, "===DOCKER:UNREACHABLE==="):
		scan.Status = "unsupported"
		scan.Note = "The Docker daemon on this host could not be reached, so no container configuration could be assessed."
		return scan
	case strings.Contains(out, "===DOCKER:NO_CONTAINERS==="):
		scan.Note = "No containers on this host: nothing to assess."
		return scan
	}
	idx := strings.Index(out, "===DOCKER:INSPECT===")
	if idx < 0 {
		scan.Status = "error"
		scan.Error = strings.TrimSpace(dockerCommandError("docker inspect", out, stderr, exitCode).Error())
		return scan
	}
	payload := out[idx+len("===DOCKER:INSPECT==="):]
	findings, err := parseDockerSecurityInspect(payload)
	if err != nil {
		scan.Status = "error"
		scan.Error = err.Error()
		return scan
	}
	scan.Findings = findings
	scan.Counts = countDockerFindings(findings)
	if len(findings) == 0 {
		scan.Note = "No container matched a misconfiguration rule."
	}
	return scan
}

// dockerInspectContainer is the subset of `docker inspect` output the audit needs.
type dockerInspectContainer struct {
	Name   string `json:"Name"`
	Config struct {
		Image string `json:"Image"`
		User  string `json:"User"`
	} `json:"Config"`
	HostConfig struct {
		Privileged     bool     `json:"Privileged"`
		Binds          []string `json:"Binds"`
		NetworkMode    string   `json:"NetworkMode"`
		PidMode        string   `json:"PidMode"`
		IpcMode        string   `json:"IpcMode"`
		CapAdd         []string `json:"CapAdd"`
		SecurityOpt    []string `json:"SecurityOpt"`
		Memory         int64    `json:"Memory"`
		NanoCpus       int64    `json:"NanoCpus"`
		ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
		PortBindings   map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
	AppArmorProfile string `json:"AppArmorProfile"`
}

// dockerRule describes one misconfiguration rule.
type dockerRule struct {
	ID          string
	Severity    string
	Title       string
	Remediation string
}

// dockerRules is the rule catalog of the audit. The IDs are stable: they are stored with every
// finding and used by the UI to pick the translated title.
var dockerRules = map[string]dockerRule{
	"privileged": {
		ID: "privileged", Severity: "critical", Title: "Privileged container",
		Remediation: "Drop --privileged and grant only the capabilities the workload needs (--cap-add).",
	},
	"docker_socket_mounted": {
		ID: "docker_socket_mounted", Severity: "critical", Title: "Docker socket mounted",
		Remediation: "Remove the docker.sock bind. A container with the socket can control the whole host.",
	},
	"sensitive_host_mount": {
		ID: "sensitive_host_mount", Severity: "high", Title: "Sensitive host path mounted read-write",
		Remediation: "Mount the path read-only (:ro) or mount only the subdirectory the workload needs.",
	},
	"dangerous_capabilities": {
		ID: "dangerous_capabilities", Severity: "high", Title: "Dangerous capabilities added",
		Remediation: "Drop the added capabilities (--cap-drop ALL) and grant the narrowest one needed.",
	},
	"sensitive_public_port": {
		ID: "sensitive_public_port", Severity: "high", Title: "Sensitive port published on all interfaces",
		Remediation: "Bind sensitive ports to 127.0.0.1 and reach them through a reverse proxy or VPN.",
	},
	"host_namespace": {
		ID: "host_namespace", Severity: "medium", Title: "Host namespace shared",
		Remediation: "Use the default bridge network and do not share the host PID or IPC namespace.",
	},
	"unconfined_security": {
		ID: "unconfined_security", Severity: "medium", Title: "Unconfined security profile",
		Remediation: "Keep the default seccomp and AppArmor profiles; do not pass seccomp=unconfined.",
	},
	"runs_as_root": {
		ID: "runs_as_root", Severity: "medium", Title: "Runs as root",
		Remediation: "Run the process as a non-root user (--user) and fix the file permissions it needs.",
	},
	"public_port_binding": {
		ID: "public_port_binding", Severity: "medium", Title: "Port published on all interfaces",
		Remediation: "Bind the port to 127.0.0.1 (-p 127.0.0.1:port) or keep it behind a reverse proxy.",
	},
	"mutable_image_tag": {
		ID: "mutable_image_tag", Severity: "low", Title: "Mutable image tag",
		Remediation: "Pin an immutable tag or digest (image@sha256:...) so the deployed code is reproducible.",
	},
	"no_memory_limit": {
		ID: "no_memory_limit", Severity: "low", Title: "No memory limit",
		Remediation: "Set a memory limit (--memory) so the container cannot exhaust host RAM.",
	},
	"no_cpu_limit": {
		ID: "no_cpu_limit", Severity: "low", Title: "No CPU limit",
		Remediation: "Set a CPU limit (--cpus) so the container cannot starve the other workloads.",
	},
	"writable_rootfs": {
		ID: "writable_rootfs", Severity: "low", Title: "Writable root filesystem",
		Remediation: "Consider --read-only with explicit writable volumes for the paths that need them.",
	},
}

// dockerDangerCaps are the Linux capabilities that break the container boundary when added.
var dockerDangerCaps = map[string]bool{
	"ALL": true, "SYS_ADMIN": true, "SYS_MODULE": true, "SYS_PTRACE": true, "SYS_RAWIO": true,
	"SYS_BOOT": true, "DAC_READ_SEARCH": true, "NET_ADMIN": true, "NET_RAW": true, "BPF": true,
	"PERFMON": true, "SYSLOG": true, "MKNOD": true, "SETFCAP": true, "LINUX_IMMUTABLE": true,
}

// dockerSocketPaths are the Docker socket paths that hand host control to a container.
var dockerSocketPaths = map[string]bool{
	"/var/run/docker.sock": true,
	"/run/docker.sock":     true,
}

// dockerSensitivePaths are the host paths that must not be mounted read-write into a container.
var dockerSensitivePaths = []string{"/", "/etc", "/root", "/proc", "/sys", "/var/lib/docker"}

// parseDockerSecurityInspect applies the rule catalog to the `docker inspect` JSON of all containers.
func parseDockerSecurityInspect(payload string) ([]store.DockerSecurityFinding, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("empty docker inspect output")
	}
	var raw []dockerInspectContainer
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return nil, fmt.Errorf("could not read docker inspect output: %w", err)
	}
	findings := []store.DockerSecurityFinding{}
	for _, c := range raw {
		name := strings.TrimPrefix(c.Name, "/")
		image := c.Config.Image
		add := func(ruleID, detail string) {
			r := dockerRules[ruleID]
			findings = append(findings, store.DockerSecurityFinding{
				Container: name, Image: image, RuleID: ruleID, Severity: r.Severity,
				Title: r.Title, Detail: detail, Remediation: r.Remediation,
			})
		}

		if c.HostConfig.Privileged {
			add("privileged", "HostConfig.Privileged=true: the container runs with all host capabilities.")
		}
		for _, m := range dockerHostMounts(c) {
			if dockerSocketPaths[m.source] {
				add("docker_socket_mounted", fmt.Sprintf("Host path %s is mounted at %s (read-write).", m.source, m.destination))
				continue
			}
			if m.rw && dockerSensitiveHostPath(m.source) {
				add("sensitive_host_mount", fmt.Sprintf("Host path %s is mounted read-write at %s.", m.source, m.destination))
			}
		}
		var shared []string
		if strings.EqualFold(c.HostConfig.NetworkMode, "host") {
			shared = append(shared, "network")
		}
		if strings.EqualFold(c.HostConfig.PidMode, "host") {
			shared = append(shared, "pid")
		}
		if strings.EqualFold(c.HostConfig.IpcMode, "host") {
			shared = append(shared, "ipc")
		}
		if len(shared) > 0 {
			add("host_namespace", "The container shares the host "+strings.Join(shared, " and ")+" namespace.")
		}
		var caps []string
		for _, cap := range c.HostConfig.CapAdd {
			if dockerDangerCaps[strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(cap), "CAP_"))] {
				caps = append(caps, strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(cap), "CAP_")))
			}
		}
		if len(caps) > 0 {
			add("dangerous_capabilities", "Added capabilities: "+strings.Join(caps, ", ")+".")
		}
		if opt, ok := dockerUnconfined(c); ok {
			add("unconfined_security", opt)
		}
		if user := strings.TrimSpace(c.Config.User); user == "" || user == "0" || user == "root" || user == "0:0" || user == "root:root" {
			add("runs_as_root", "Config.User is empty: the container process runs as root.")
		}
		if tag, pinned := dockerImageTag(image); !pinned && (tag == "latest" || tag == "") {
			add("mutable_image_tag", fmt.Sprintf("Image %s uses the mutable tag %q.", image, tag))
		}
		for _, p := range dockerPublishedPorts(c) {
			if !p.public {
				continue
			}
			if sensitivePorts[p.port] {
				add("sensitive_public_port", fmt.Sprintf("Container port %s is published on %s (a database, cache or admin port).", p.port, p.hostIP))
				continue
			}
			add("public_port_binding", fmt.Sprintf("Container port %s is published on %s.", p.port, p.hostIP))
		}
		if c.HostConfig.Memory <= 0 {
			add("no_memory_limit", "HostConfig.Memory is 0: the container may use all host memory.")
		}
		if c.HostConfig.NanoCpus <= 0 {
			add("no_cpu_limit", "HostConfig.NanoCpus is 0: the container may use all host CPUs.")
		}
		if !c.HostConfig.ReadonlyRootfs {
			add("writable_rootfs", "HostConfig.ReadonlyRootfs is false: the container filesystem is writable.")
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Container != findings[j].Container {
			return findings[i].Container < findings[j].Container
		}
		return findings[i].RuleID < findings[j].RuleID
	})
	return findings, nil
}

// dockerHostMount is one host path bound into a container.
type dockerHostMount struct {
	source      string
	destination string
	rw          bool
}

// dockerHostMounts collects the host binds of a container from Mounts and Binds (both are read:
// Mounts is what the engine resolved, Binds what was requested).
func dockerHostMounts(c dockerInspectContainer) []dockerHostMount {
	mounts := []dockerHostMount{}
	for _, m := range c.Mounts {
		if m.Type != "" && m.Type != "bind" {
			continue
		}
		if m.Source == "" {
			continue
		}
		mounts = append(mounts, dockerHostMount{source: m.Source, destination: m.Destination, rw: m.RW})
	}
	for _, b := range c.HostConfig.Binds {
		parts := strings.Split(b, ":")
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		rw := true
		if len(parts) > 2 && strings.Contains(parts[len(parts)-1], "ro") {
			rw = false
		}
		mounts = append(mounts, dockerHostMount{source: parts[0], destination: parts[1], rw: rw})
	}
	return mounts
}

// dockerSensitiveHostPath reports whether a host path is one of the sensitive paths (or below it).
func dockerSensitiveHostPath(path string) bool {
	p := strings.TrimRight(path, "/")
	if p == "" {
		p = "/"
	}
	for _, s := range dockerSensitivePaths {
		if s == "/" {
			if p == "/" {
				return true
			}
			continue
		}
		if p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return false
}

// dockerUnconfined reports an unconfined seccomp or AppArmor profile.
func dockerUnconfined(c dockerInspectContainer) (string, bool) {
	for _, opt := range c.HostConfig.SecurityOpt {
		o := strings.ToLower(strings.TrimSpace(opt))
		if strings.Contains(o, "unconfined") {
			return "SecurityOpt " + opt + " disables the confinement profile.", true
		}
	}
	if strings.EqualFold(strings.TrimSpace(c.AppArmorProfile), "unconfined") {
		return "AppArmorProfile is unconfined: no MAC confinement is applied.", true
	}
	return "", false
}

// dockerImageTag returns the tag of an image reference and whether it is pinned by digest.
func dockerImageTag(image string) (string, bool) {
	img := strings.TrimSpace(image)
	if img == "" {
		return "", false
	}
	if strings.Contains(img, "@sha256:") {
		return "", true
	}
	slash := strings.LastIndex(img, "/")
	colon := strings.LastIndex(img, ":")
	if colon > slash {
		return img[colon+1:], false
	}
	return "", false
}

// dockerContainerPort strips the protocol suffix of a port key ("5432/tcp" -> "5432").
func dockerContainerPort(key string) string {
	if i := strings.Index(key, "/"); i >= 0 {
		return key[:i]
	}
	return key
}

// dockerPublishedPort is one published container port.
type dockerPublishedPort struct {
	port   string
	hostIP string
	public bool
}

// dockerPublishedPorts collects the published ports of a container.
func dockerPublishedPorts(c dockerInspectContainer) []dockerPublishedPort {
	ports := []dockerPublishedPort{}
	seen := map[string]bool{}
	collect := func(m map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}) {
		for cport, binds := range m {
			num := dockerContainerPort(cport)
			for _, b := range binds {
				hostIP := b.HostIP
				if hostIP == "" {
					hostIP = "0.0.0.0"
				}
				key := num + "|" + hostIP + "|" + b.HostPort
				if seen[key] {
					continue
				}
				seen[key] = true
				public := hostIP == "0.0.0.0" || hostIP == "::" || hostIP == "*"
				ports = append(ports, dockerPublishedPort{port: num, hostIP: hostIP, public: public})
			}
		}
	}
	collect(c.NetworkSettings.Ports)
	collect(c.HostConfig.PortBindings)
	sort.Slice(ports, func(i, j int) bool { return ports[i].port < ports[j].port })
	return ports
}

// countDockerFindings counts the findings per severity.
func countDockerFindings(findings []store.DockerSecurityFinding) store.DockerSeverityCounts {
	counts := store.DockerSeverityCounts{}
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			counts.Critical++
		case "high":
			counts.High++
		case "medium":
			counts.Medium++
		case "low":
			counts.Low++
		}
		counts.Total++
	}
	return counts
}

// ---------------------------------------------------------------- image vulnerability scan

// dockerScanScript runs the scanner that is already installed on the host. Pantau never installs one.
// The image reference is baked in (the script runs under `sh -c` without arguments).
func dockerScanScript(image string) string {
	q := shellQuote(image)
	return `if command -v trivy >/dev/null 2>&1; then
  echo "===SCANNER:trivy==="
  trivy image --quiet --format json --scanners vuln ` + q + `
elif command -v grype >/dev/null 2>&1; then
  echo "===SCANNER:grype==="
  grype -o json ` + q + `
else
  echo "===SCANNER:NONE==="
fi
`
}

// DockerImageScan scans one image with the scanner installed on the host and stores the result.
func (ins *Inspector) DockerImageScan(hostID int64, image string) (*store.DockerSecurityScan, error) {
	if !ValidDockerImage(image) {
		return nil, dockerInvalid("invalid image: %q", image)
	}
	key := fmt.Sprintf("%d|%s", hostID, image)
	if _, loaded := ins.dockerSecInFlight.LoadOrStore(key, struct{}{}); loaded {
		return nil, ErrDockerSecInProgress
	}
	defer ins.dockerSecInFlight.Delete(key)

	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerScanScript(image), dockerScanTimeout)
	if err != nil {
		return nil, err
	}
	scan := parseDockerImageScan(stdout, stderr, exitCode, image)
	scan.HostID = hostID
	scan.Kind = "image"
	scan.Subject = image
	scan.ScannedAt = time.Now()
	if err := ins.db.SaveDockerSecurityScan(scan); err != nil {
		return nil, err
	}
	return scan, nil
}

// parseDockerImageScan turns the scanner output into counts and a capped vulnerability list.
func parseDockerImageScan(stdout, stderr string, exitCode int, image string) *store.DockerSecurityScan {
	scan := &store.DockerSecurityScan{
		Status: "ok",
		Vulns:  []store.DockerImageVuln{},
	}
	switch {
	case strings.Contains(stdout, "===SCANNER:NONE==="):
		scan.Status = "unsupported"
		scan.Note = "No vulnerability scanner is installed on this host (trivy or grype). Install one on the host to enable image scanning; Pantau never installs software."
		return scan
	case strings.Contains(stdout, "===SCANNER:trivy==="):
		scan.Scanner = "trivy"
	case strings.Contains(stdout, "===SCANNER:grype==="):
		scan.Scanner = "grype"
	}
	idx := strings.Index(stdout, "===SCANNER:")
	body := ""
	if idx >= 0 {
		body = stdout[idx+len("===SCANNER:"):]
		if nl := strings.Index(body, "\n"); nl >= 0 {
			body = body[nl+1:]
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		scan.Status = "error"
		scan.Error = strings.TrimSpace(dockerCommandError("image scan", stdout, stderr, exitCode).Error())
		return scan
	}
	var vulns []store.DockerImageVuln
	switch scan.Scanner {
	case "trivy":
		var err error
		vulns, err = parseTrivyVulns(body)
		if err != nil {
			scan.Status = "error"
			scan.Error = err.Error()
			return scan
		}
	case "grype":
		var err error
		vulns, err = parseGrypeVulns(body)
		if err != nil {
			scan.Status = "error"
			scan.Error = err.Error()
			return scan
		}
	default:
		scan.Status = "error"
		scan.Error = "unknown scanner output"
		return scan
	}
	sortDockerVulns(vulns)
	scan.VulnCounts = countDockerVulns(vulns)
	if len(vulns) > dockerVulnMax {
		scan.Vulns = vulns[:dockerVulnMax]
		scan.Truncated = true
		scan.Note = fmt.Sprintf("Only the %d most severe of %d vulnerabilities are stored.", dockerVulnMax, len(vulns))
	} else {
		scan.Vulns = vulns
	}
	if len(vulns) == 0 {
		scan.Note = "The scanner reported no known vulnerability for this image."
	}
	return scan
}

// dockerSeverityRank orders the severities from the most to the least severe.
func dockerSeverityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

// normalizeDockerSeverity maps a scanner severity to the Pantau scale.
func normalizeDockerSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium", "moderate":
		return "medium"
	case "low", "negligible":
		return "low"
	default:
		return "unknown"
	}
}

// sortDockerVulns sorts by severity, then by package and id.
func sortDockerVulns(vulns []store.DockerImageVuln) {
	sort.SliceStable(vulns, func(i, j int) bool {
		ri, rj := dockerSeverityRank(vulns[i].Severity), dockerSeverityRank(vulns[j].Severity)
		if ri != rj {
			return ri < rj
		}
		if vulns[i].Package != vulns[j].Package {
			return vulns[i].Package < vulns[j].Package
		}
		return vulns[i].ID < vulns[j].ID
	})
}

// countDockerVulns counts the vulnerabilities per severity.
func countDockerVulns(vulns []store.DockerImageVuln) store.DockerVulnCounts {
	counts := store.DockerVulnCounts{}
	for _, v := range vulns {
		switch v.Severity {
		case "critical":
			counts.Critical++
		case "high":
			counts.High++
		case "medium":
			counts.Medium++
		case "low":
			counts.Low++
		default:
			counts.Unknown++
		}
		counts.Total++
	}
	return counts
}

// parseTrivyVulns reads the `trivy image --format json` output.
func parseTrivyVulns(body string) ([]store.DockerImageVuln, error) {
	var doc struct {
		Results []struct {
			Vulnerabilities []struct {
				ID               string `json:"VulnerabilityID"`
				Title            string `json:"Title"`
				Severity         string `json:"Severity"`
				PkgName          string `json:"PkgName"`
				InstalledVersion string `json:"InstalledVersion"`
				FixedVersion     string `json:"FixedVersion"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return nil, fmt.Errorf("could not read trivy output: %w", err)
	}
	vulns := []store.DockerImageVuln{}
	for _, r := range doc.Results {
		for _, v := range r.Vulnerabilities {
			vulns = append(vulns, store.DockerImageVuln{
				ID: v.ID, Package: v.PkgName, Installed: v.InstalledVersion, Fixed: v.FixedVersion,
				Severity: normalizeDockerSeverity(v.Severity), Title: v.Title,
			})
		}
	}
	return vulns, nil
}

// parseGrypeVulns reads the `grype -o json` output.
func parseGrypeVulns(body string) ([]store.DockerImageVuln, error) {
	var doc struct {
		Matches []struct {
			Vulnerability struct {
				ID          string `json:"id"`
				Severity    string `json:"severity"`
				Description string `json:"description"`
			} `json:"vulnerability"`
			Artifact struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"artifact"`
			Fix struct {
				Versions []string `json:"versions"`
			} `json:"fix"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return nil, fmt.Errorf("could not read grype output: %w", err)
	}
	vulns := []store.DockerImageVuln{}
	for _, m := range doc.Matches {
		vulns = append(vulns, store.DockerImageVuln{
			ID: m.Vulnerability.ID, Package: m.Artifact.Name, Installed: m.Artifact.Version,
			Fixed: strings.Join(m.Fix.Versions, ", "), Severity: normalizeDockerSeverity(m.Vulnerability.Severity),
			Title: m.Vulnerability.Description,
		})
	}
	return vulns, nil
}
