package inspector

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/store"
)

// dockerInspectFixture builds a `docker inspect` JSON payload of one container.
func dockerInspectFixture(name, image string, mutate func(*map[string]any)) string {
	c := map[string]any{
		"Name":   "/" + name,
		"Config": map[string]any{"Image": image, "User": "app"},
		"HostConfig": map[string]any{
			"Privileged":     false,
			"NetworkMode":    "bridge",
			"PidMode":        "",
			"IpcMode":        "",
			"CapAdd":         []string{},
			"SecurityOpt":    []string{},
			"Memory":         int64(536870912),
			"NanoCpus":       int64(1000000000),
			"ReadonlyRootfs": true,
			"PortBindings":   map[string]any{},
		},
		"Mounts":          []any{},
		"NetworkSettings": map[string]any{"Ports": map[string]any{}},
	}
	if mutate != nil {
		mutate(&c)
	}
	b, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	return "[" + string(b) + "]"
}

// ruleIDsOf returns the rule ids of the findings of one container.
func ruleIDsOf(findings []store.DockerSecurityFinding, container string) []string {
	ids := []string{}
	for _, f := range findings {
		if f.Container == container {
			ids = append(ids, f.RuleID)
		}
	}
	return ids
}

func hasRule(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestDockerSecurityAuditRules(t *testing.T) {
	cases := []struct {
		name      string
		container string
		image     string
		mutate    func(*map[string]any)
		want      []string // rule ids that must be present
		notWant   []string // rule ids that must be absent
	}{
		{
			name: "privileged", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["HostConfig"].(map[string]any)["Privileged"] = true
			},
			want: []string{"privileged"},
		},
		{
			name: "docker socket", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["Mounts"] = []any{map[string]any{
					"Type": "bind", "Source": "/var/run/docker.sock", "Destination": "/var/run/docker.sock", "RW": true,
				}}
			},
			want: []string{"docker_socket_mounted"},
		},
		{
			name: "sensitive host path", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["Mounts"] = []any{map[string]any{
					"Type": "bind", "Source": "/etc", "Destination": "/host-etc", "RW": true,
				}}
			},
			want: []string{"sensitive_host_mount"},
		},
		{
			name: "sensitive host path read-only is fine", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["Mounts"] = []any{map[string]any{
					"Type": "bind", "Source": "/etc", "Destination": "/host-etc", "RW": false,
				}}
			},
			notWant: []string{"sensitive_host_mount"},
		},
		{
			name: "host namespaces", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				h := (*c)["HostConfig"].(map[string]any)
				h["NetworkMode"] = "host"
				h["PidMode"] = "host"
				h["IpcMode"] = "host"
			},
			want: []string{"host_namespace"},
		},
		{
			name: "dangerous capabilities", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["HostConfig"].(map[string]any)["CapAdd"] = []string{"SYS_ADMIN", "NET_RAW"}
			},
			want: []string{"dangerous_capabilities"},
		},
		{
			name: "harmless capability", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["HostConfig"].(map[string]any)["CapAdd"] = []string{"CHOWN", "SETUID"}
			},
			notWant: []string{"dangerous_capabilities"},
		},
		{
			name: "unconfined seccomp", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["HostConfig"].(map[string]any)["SecurityOpt"] = []string{"seccomp=unconfined"}
			},
			want: []string{"unconfined_security"},
		},
		{
			name: "runs as root", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["Config"].(map[string]any)["User"] = ""
			},
			want: []string{"runs_as_root"},
		},
		{
			name: "runs as root by name", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["Config"].(map[string]any)["User"] = "root"
			},
			want: []string{"runs_as_root"},
		},
		{
			name: "latest tag", container: "web", image: "nginx:latest",
			want: []string{"mutable_image_tag"},
		},
		{
			name: "pinned tag", container: "web", image: "nginx:1.25",
			notWant: []string{"mutable_image_tag"},
		},
		{
			name: "digest pinned", container: "web", image: "nginx@sha256:abc",
			notWant: []string{"mutable_image_tag"},
		},
		{
			name: "public sensitive port", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["NetworkSettings"].(map[string]any)["Ports"] = map[string]any{
					"5432/tcp": []any{map[string]any{"HostIp": "0.0.0.0", "HostPort": "5432"}},
				}
			},
			want: []string{"sensitive_public_port"},
		},
		{
			name: "public ordinary port", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["NetworkSettings"].(map[string]any)["Ports"] = map[string]any{
					"8080/tcp": []any{map[string]any{"HostIp": "0.0.0.0", "HostPort": "8080"}},
				}
			},
			want:    []string{"public_port_binding"},
			notWant: []string{"sensitive_public_port"},
		},
		{
			name: "localhost port is fine", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				(*c)["NetworkSettings"].(map[string]any)["Ports"] = map[string]any{
					"5432/tcp": []any{map[string]any{"HostIp": "127.0.0.1", "HostPort": "5432"}},
				}
			},
			notWant: []string{"sensitive_public_port", "public_port_binding"},
		},
		{
			name: "no limits", container: "web", image: "nginx:1.25",
			mutate: func(c *map[string]any) {
				h := (*c)["HostConfig"].(map[string]any)
				h["Memory"] = int64(0)
				h["NanoCpus"] = int64(0)
				h["ReadonlyRootfs"] = false
			},
			want: []string{"no_memory_limit", "no_cpu_limit", "writable_rootfs"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := dockerInspectFixture(tc.container, tc.image, tc.mutate)
			findings, err := parseDockerSecurityInspect(payload)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			ids := ruleIDsOf(findings, tc.container)
			for _, want := range tc.want {
				if !hasRule(ids, want) {
					t.Errorf("rule %q must be reported, got %v", want, ids)
				}
			}
			for _, notWant := range tc.notWant {
				if hasRule(ids, notWant) {
					t.Errorf("rule %q must not be reported, got %v", notWant, ids)
				}
			}
		})
	}
}

func TestDockerSecurityAuditCleanContainer(t *testing.T) {
	payload := dockerInspectFixture("web", "nginx:1.25", nil)
	findings, err := parseDockerSecurityInspect(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("a clean container must produce no finding, got %v", findings)
	}
}

func TestDockerSecurityAuditInvalidJSON(t *testing.T) {
	if _, err := parseDockerSecurityInspect("not json"); err == nil {
		t.Error("invalid docker inspect JSON must fail")
	}
	if _, err := parseDockerSecurityInspect("  "); err == nil {
		t.Error("empty docker inspect output must fail")
	}
}

func TestParseDockerSecurityAuditStates(t *testing.T) {
	cases := []struct {
		name       string
		stdout     string
		wantStatus string
	}{
		{"docker missing", "===DOCKER:MISSING===\n", "unsupported"},
		{"permission denied", "===DOCKER:PERMISSION===\n", "unsupported"},
		{"no containers", "===DOCKER:NO_CONTAINERS===\n", "ok"},
		{"daemon unreachable", "===DOCKER:UNREACHABLE===\n", "unsupported"},
		{"broken output", "something else\n", "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scan := parseDockerSecurityAudit(tc.stdout, "", 0)
			if scan.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", scan.Status, tc.wantStatus)
			}
			if tc.wantStatus != "ok" && scan.Note == "" && scan.Error == "" {
				t.Error("a not-assessed state must carry a note or an error")
			}
		})
	}
}

func TestParseDockerSecurityAuditFindings(t *testing.T) {
	payload := dockerInspectFixture("web", "nginx:latest", func(c *map[string]any) {
		(*c)["HostConfig"].(map[string]any)["Privileged"] = true
	})
	stdout := "===DOCKER:INSPECT===\n" + payload
	scan := parseDockerSecurityAudit(stdout, "", 0)
	if scan.Status != "ok" {
		t.Fatalf("status = %q", scan.Status)
	}
	if len(scan.Findings) == 0 {
		t.Fatal("expected findings")
	}
	if scan.Counts.Critical == 0 {
		t.Errorf("privileged must be counted as critical, got %+v", scan.Counts)
	}
	if scan.Counts.Total != len(scan.Findings) {
		t.Errorf("total = %d, findings = %d", scan.Counts.Total, len(scan.Findings))
	}
}

func TestParseDockerImageScanTrivy(t *testing.T) {
	body := `{
  "Results": [
    {
      "Target": "nginx:latest (debian 12.1)",
      "Vulnerabilities": [
        {"VulnerabilityID":"CVE-2024-0001","PkgName":"openssl","InstalledVersion":"3.0.11-1","FixedVersion":"3.0.13-1","Severity":"HIGH","Title":"openssl flaw"},
        {"VulnerabilityID":"CVE-2024-0002","PkgName":"zlib","InstalledVersion":"1.2.13","FixedVersion":"1.2.14","Severity":"CRITICAL","Title":"zlib flaw"},
        {"VulnerabilityID":"GHSA-xxxx","PkgName":"libxml","InstalledVersion":"2.9","FixedVersion":"","Severity":"UNKNOWN","Title":"ghsa"}
      ]
    }
  ]
}`
	scan := parseDockerImageScan("===SCANNER:trivy===\n"+body, "", 0, "nginx:latest")
	if scan.Status != "ok" {
		t.Fatalf("status = %q (%s)", scan.Status, scan.Error)
	}
	if scan.Scanner != "trivy" {
		t.Errorf("scanner = %q", scan.Scanner)
	}
	if scan.VulnCounts.Critical != 1 || scan.VulnCounts.High != 1 || scan.VulnCounts.Unknown != 1 {
		t.Errorf("counts = %+v", scan.VulnCounts)
	}
	if scan.VulnCounts.Total != 3 {
		t.Errorf("total = %d", scan.VulnCounts.Total)
	}
	if len(scan.Vulns) != 3 {
		t.Fatalf("vulns = %d", len(scan.Vulns))
	}
	if scan.Vulns[0].Severity != "critical" {
		t.Errorf("the most severe vuln must come first, got %+v", scan.Vulns[0])
	}
	if scan.Vulns[0].Package != "zlib" || scan.Vulns[0].Fixed != "1.2.14" {
		t.Errorf("vuln fields = %+v", scan.Vulns[0])
	}
}

func TestParseDockerImageScanGrype(t *testing.T) {
	body := `{
  "matches": [
    {"vulnerability":{"id":"CVE-2024-0003","severity":"Medium","description":"lib flaw"},
     "artifact":{"name":"libfoo","version":"1.2.3"},
     "fix":{"versions":["1.2.4"]}},
    {"vulnerability":{"id":"CVE-2024-0004","severity":"Negligible","description":"tiny flaw"},
     "artifact":{"name":"libbar","version":"0.1"},"fix":{"versions":[]}}
  ]
}`
	scan := parseDockerImageScan("===SCANNER:grype===\n"+body, "", 0, "nginx:latest")
	if scan.Status != "ok" {
		t.Fatalf("status = %q (%s)", scan.Status, scan.Error)
	}
	if scan.Scanner != "grype" {
		t.Errorf("scanner = %q", scan.Scanner)
	}
	if scan.VulnCounts.Medium != 1 || scan.VulnCounts.Low != 1 {
		t.Errorf("counts = %+v", scan.VulnCounts)
	}
	if scan.Vulns[0].ID != "CVE-2024-0003" || scan.Vulns[0].Installed != "1.2.3" || scan.Vulns[0].Fixed != "1.2.4" {
		t.Errorf("vuln = %+v", scan.Vulns[0])
	}
}

func TestParseDockerImageScanNoScanner(t *testing.T) {
	scan := parseDockerImageScan("===SCANNER:NONE===\n", "", 0, "nginx:latest")
	if scan.Status != "unsupported" {
		t.Errorf("status = %q", scan.Status)
	}
	if scan.Note == "" {
		t.Error("a missing scanner must carry a note")
	}
	if len(scan.Vulns) != 0 {
		t.Errorf("no scanner must not produce vulnerabilities, got %v", scan.Vulns)
	}
}

func TestParseDockerImageScanBrokenOutput(t *testing.T) {
	scan := parseDockerImageScan("===SCANNER:trivy===\nnot json", "", 0, "nginx:latest")
	if scan.Status != "error" {
		t.Errorf("status = %q", scan.Status)
	}
	if scan.Error == "" {
		t.Error("a broken scanner output must carry an error")
	}
}

func TestDockerImageScanRejectsInvalidImage(t *testing.T) {
	ins := &Inspector{}
	if _, err := ins.DockerImageScan(1, "nginx; reboot"); err == nil {
		t.Error("an image with a shell metacharacter must be rejected")
	}
}

func TestDockerSensitiveHostPath(t *testing.T) {
	for _, p := range []string{"/", "/etc", "/etc/passwd", "/root", "/proc", "/sys", "/var/lib/docker"} {
		if !dockerSensitiveHostPath(p) {
			t.Errorf("%q must be sensitive", p)
		}
	}
	for _, p := range []string{"/srv/app", "/var/log", "/home/user/data"} {
		if dockerSensitiveHostPath(p) {
			t.Errorf("%q must not be sensitive", p)
		}
	}
}

func TestDockerImageTag(t *testing.T) {
	cases := []struct {
		image, tag string
		pinned     bool
	}{
		{"nginx:latest", "latest", false},
		{"nginx:1.25", "1.25", false},
		{"registry.io/org/app:v2", "v2", false},
		{"nginx", "", false},
		{"nginx@sha256:abc", "", true},
	}
	for _, tc := range cases {
		tag, pinned := dockerImageTag(tc.image)
		if tag != tc.tag || pinned != tc.pinned {
			t.Errorf("dockerImageTag(%q) = %q,%v want %q,%v", tc.image, tag, pinned, tc.tag, tc.pinned)
		}
	}
}

func TestDockerSecurityAuditScriptHasNoInstall(t *testing.T) {
	if strings.Contains(dockerSecAuditScript, "apt-get") || strings.Contains(dockerSecAuditScript, "yum install") ||
		strings.Contains(dockerSecAuditScript, "pacman") || strings.Contains(dockerSecAuditScript, "apk add") {
		t.Error("the audit script must never install anything")
	}
	script := dockerScanScript("nginx:latest")
	if strings.Contains(script, "apt-get") || strings.Contains(script, "yum install") ||
		strings.Contains(script, "pacman") || strings.Contains(script, "apk add") {
		t.Error("the scan script must never install anything")
	}
}

func TestDockerScanScriptQuotesTheImage(t *testing.T) {
	script := dockerScanScript("nginx:latest")
	if !strings.Contains(script, "'nginx:latest'") {
		t.Errorf("the image must be single-quoted, got %s", script)
	}
}

func TestDockerSecurityAuditRootMount(t *testing.T) {
	payload := dockerInspectFixture("web", "nginx:1.25", func(c *map[string]any) {
		(*c)["Mounts"] = []any{map[string]any{
			"Type": "bind", "Source": "/", "Destination": "/host", "RW": true,
		}}
	})
	findings, err := parseDockerSecurityInspect(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ids := ruleIDsOf(findings, "web")
	if !hasRule(ids, "sensitive_host_mount") {
		t.Errorf("a read-write / mount must be reported, got %v", ids)
	}
}

func TestParseDockerImageScanInstallFailureNote(t *testing.T) {
	note := scannerInstallNote("===INSTALL:NOPRIV===")
	scan := parseDockerImageScan("===SCANNER:NONE===\n"+note, "", 0, "nginx:latest")
	if scan.Status != "unsupported" || !strings.Contains(scan.Note, "passwordless sudo") {
		t.Errorf("the reason the install failed must reach the operator: %+v", scan)
	}
}

// The scanner talks to the Docker socket itself, so when the socket needs sudo it must run under sudo too,
// otherwise it falls back to a registry pull that fails for a local-only image.
func TestDockerScanScriptRunsScannerUnderSudoWhenSocketIsRefused(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("docker", `case "$1" in ps) echo "permission denied while trying to connect" >&2; exit 1;; esac`)
	write("sudo", `[ "$1" = "-n" ] && shift; echo "SUDO $*" >> "$0.log"; [ "$1" = true ] && exit 0; exec "$@"`)
	write("trivy", `echo '{"Results":[]}'`)

	run := func() string {
		cmd := exec.Command("sh", "-c", dockerShell(dockerScanScript("nginx:latest")))
		cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		return string(out)
	}
	if out := run(); !strings.Contains(out, "===SCANNER:trivy===") {
		t.Fatalf("scanner did not run: %s", out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "sudo.log"))
	if !strings.Contains(string(log), "SUDO "+filepath.Join(dir, "trivy")+" image") {
		t.Fatalf("trivy must run under sudo with its absolute path, sudo saw:\n%s", log)
	}
}

// A realistic trivy report of a big image: thousands of vulnerabilities over many packages. The stored
// list is capped (dockerVulnMax), but the counts describe the whole report and the most severe entries
// are kept.
func TestParseDockerImageScanManyVulns(t *testing.T) {
	const total = 3000
	var b strings.Builder
	b.WriteString(`{"SchemaVersion":2,"ArtifactName":"registry.example.com:5000/app:v1.2.3","Results":[`)
	for i := range total {
		if i > 0 {
			b.WriteString(",")
		}
		sev := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"}[i%5]
		fmt.Fprintf(&b, `{"Target":"app (debian 12.5)","Class":"os-pkgs","Type":"debian","Vulnerabilities":[{"VulnerabilityID":"CVE-2024-%04d","PkgName":"libpkg%d","InstalledVersion":"1.%d.%d","FixedVersion":"1.%d.%d","Title":"flaw %d","Severity":"%s","PrimaryURL":"https://avd.aquasec.com/nvd/cve-2024-%04d"}]}`,
			i, i%97, i%10, i%7, i%10, i%7+1, i, sev, i)
	}
	b.WriteString(`]}`)
	scan := parseDockerImageScan("===SCANNER:trivy===\n"+b.String(), "", 0, "registry.example.com:5000/app:v1.2.3")
	if scan.Status != "ok" {
		t.Fatalf("status = %q (%s)", scan.Status, scan.Error)
	}
	if scan.Scanner != "trivy" {
		t.Errorf("scanner = %q", scan.Scanner)
	}
	if scan.VulnCounts.Total != total {
		t.Errorf("total = %d, want %d", scan.VulnCounts.Total, total)
	}
	if scan.VulnCounts.Critical != 600 || scan.VulnCounts.High != 600 || scan.VulnCounts.Unknown != 600 {
		t.Errorf("counts = %+v", scan.VulnCounts)
	}
	if len(scan.Vulns) != dockerVulnMax {
		t.Fatalf("stored vulns = %d, want the cap %d", len(scan.Vulns), dockerVulnMax)
	}
	if !scan.Truncated {
		t.Error("a capped list must be marked as truncated")
	}
	if scan.Vulns[0].Severity != "critical" {
		t.Errorf("the most severe vuln must come first, got %+v", scan.Vulns[0])
	}
	if scan.Vulns[0].Package == "" || scan.Vulns[0].ID == "" {
		t.Errorf("the fields of a vuln must survive: %+v", scan.Vulns[0])
	}
}

// The grype report has its own shape but the same result.
func TestParseDockerImageScanManyVulnsGrype(t *testing.T) {
	const total = 2500
	var b strings.Builder
	b.WriteString(`{"matches":[`)
	for i := range total {
		if i > 0 {
			b.WriteString(",")
		}
		sev := []string{"Critical", "High", "Medium", "Low", "Negligible"}[i%5]
		fmt.Fprintf(&b, `{"vulnerability":{"id":"CVE-2024-%04d","severity":"%s","description":"flaw %d"},"artifact":{"name":"pkg%d","version":"1.0.%d"},"fix":{"versions":["1.0.%d"]}}`,
			i, sev, i, i%53, i%9, i%9+1)
	}
	b.WriteString(`]}`)
	scan := parseDockerImageScan("===SCANNER:grype===\n"+b.String(), "", 0, "app:v1")
	if scan.Status != "ok" {
		t.Fatalf("status = %q (%s)", scan.Status, scan.Error)
	}
	if scan.Scanner != "grype" {
		t.Errorf("scanner = %q", scan.Scanner)
	}
	if scan.VulnCounts.Total != total {
		t.Errorf("total = %d, want %d", scan.VulnCounts.Total, total)
	}
	if len(scan.Vulns) != dockerVulnMax || !scan.Truncated {
		t.Errorf("stored vulns = %d, truncated = %v", len(scan.Vulns), scan.Truncated)
	}
	if scan.Vulns[0].Severity != "critical" {
		t.Errorf("the most severe vuln must come first, got %+v", scan.Vulns[0])
	}
}
