package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
	"pantau/internal/vulnintel"
)

func TestAgentSecurityTools(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h := itoa(hostID)

	// The opt-in setting of the UI applies: nothing is looked up while the feature is off.
	if out := callTool(t, srv, "vuln_status", `{"host_id":`+h+`}`); !strings.Contains(out, `enabled`) || !strings.Contains(out, `false`) || strings.Contains(out, `"isError":true`) {
		t.Fatalf("vuln_status with the feature off: %s", out)
	}

	// A stored audit is returned, with its long lists cut and counted.
	audit := &store.AccountAudit{HostID: hostID, ScannedAt: time.Now().UTC(), Status: "ok"}
	for range secCap + 10 {
		audit.Findings = append(audit.Findings, store.AccountFinding{ID: "x", Severity: "low", Title: "finding"})
	}
	if err := db.SaveAccountAudit(audit); err != nil {
		t.Fatal(err)
	}
	out := callTool(t, srv, "account_audit", `{"host_id":`+h+`}`)
	if !strings.Contains(out, "findings_total") || !strings.Contains(out, "50") {
		t.Fatalf("account_audit must cut the findings and say how many there are: %s", out)
	}

	// Bad input never reaches the operator.
	for name, args := range map[string]string{
		"docker_security_scan": `{"host_id":` + h + `,"what":"image","image":"a b;rm"}`,
		"iac_scan":             `{"kind":"../x","id":1}`,
		"iac_findings":         `{"kind":"repo"}`,
		"fim_diff":             `{"host_id":` + h + `,"rule_id":0}`,
	} {
		if out := callTool(t, srv, name, args); !strings.Contains(out, `"isError":true`) || pendingCount(srv) != 0 {
			t.Fatalf("%s %s must be refused before asking: %s (pending %d)", name, args, out, pendingCount(srv))
		}
	}

	// Scans change state (they write results), so they wait for the operator like security_scan does.
	_ = db.SetSetting("agent_approval", "auto")
	if out := callTool(t, srv, "iac_scan", `{"kind":"repo","id":999}`); !strings.Contains(out, `"isError":true`) {
		t.Fatalf("scan of an unknown source: %s", out)
	}
	if out := callTool(t, srv, "iac_findings", `{}`); !strings.Contains(out, "gate_enabled") {
		t.Fatalf("iac_findings digest: %s", out)
	}
}

// The docker_security tool reads the stored results and the exploit intelligence (its own endpoint now).
// The lists stay capped.
func TestAgentSecurityDockerTool(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	scan := &store.DockerSecurityScan{
		HostID: hostID, Kind: "image", Subject: "nginx:1.25", ScannedAt: time.Now(), Scanner: "trivy", Status: "ok",
		VulnCounts: store.DockerVulnCounts{Total: secCap + 10},
	}
	for range secCap + 10 {
		scan.Vulns = append(scan.Vulns, store.DockerImageVuln{ID: "CVE-2024-0001", Package: "lib", Severity: "high"})
	}
	if err := db.SaveDockerSecurityScan(scan); err != nil {
		t.Fatal(err)
	}
	out := callTool(t, srv, "docker_security", `{"host_id":`+itoa(hostID)+`}`)
	if strings.Contains(out, `"isError":true`) {
		t.Fatalf("docker_security must work: %s", out)
	}
	if !strings.Contains(out, "nginx:1.25") {
		t.Fatalf("the stored scan must be in the answer: %s", out)
	}
	if !strings.Contains(out, "vulns_total") {
		t.Fatalf("the vulnerability list must be cut and counted: %s", out)
	}
}

func TestAgentSecurityDockerToolCapsIntel(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	_ = db.SetSetting(vulnIntelSetting, "1")
	// The feeds are fake: the tool must not reach the internet in a test. The EPSS feed scores every
	// CVE it is asked about, so the intelligence holds more entries than the tool keeps.
	feeds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/kev" {
			_, _ = w.Write([]byte(`{"vulnerabilities":[]}`))
			return
		}
		data := []map[string]string{}
		for _, joined := range r.URL.Query()["cve"] {
			for _, cve := range strings.Split(joined, ",") {
				data = append(data, map[string]string{"cve": cve, "epss": "0.5", "percentile": "0.9", "date": "2026-11-08"})
			}
		}
		b, _ := json.Marshal(map[string]any{"data": data})
		_, _ = w.Write(b)
	}))
	defer feeds.Close()
	srv.vuln = vulnintel.New(feeds.URL+"/kev", feeds.URL+"/epss")
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	scan := &store.DockerSecurityScan{
		HostID: hostID, Kind: "image", Subject: "nginx:1.25", ScannedAt: time.Now(), Scanner: "trivy", Status: "ok",
		VulnCounts: store.DockerVulnCounts{Total: secCap + 10},
	}
	for i := range secCap + 10 {
		scan.Vulns = append(scan.Vulns, store.DockerImageVuln{
			ID: fmt.Sprintf("CVE-2024-%04d", i), Package: "lib", Severity: "high",
		})
	}
	if err := db.SaveDockerSecurityScan(scan); err != nil {
		t.Fatal(err)
	}
	out := callTool(t, srv, "docker_security", `{"host_id":`+itoa(hostID)+`}`)
	if strings.Contains(out, `"isError":true`) {
		t.Fatalf("docker_security must work: %s", out)
	}
	if !strings.Contains(out, "cves_total") {
		t.Fatalf("the CVE map must be cut and counted: %s", out)
	}
}
