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

func TestIsCVECode(t *testing.T) {
	for _, id := range []string{"CVE-2024-0001", "CVE-2026-8932"} {
		if !isCVECode(id) {
			t.Errorf("%q must be a CVE id", id)
		}
	}
	for _, id := range []string{"", "CVE", "GHSA-xxxx", "CVE-2024", "CVE-2024-0001-EXTRA-LONG", "cve-2024-0001"} {
		if isCVECode(id) {
			t.Errorf("%q must not be a CVE id", id)
		}
	}
}

func TestDockerSecCVEs(t *testing.T) {
	scan := &store.DockerSecurityScan{
		Vulns: []store.DockerImageVuln{
			{ID: "CVE-2024-0001"},
			{ID: "CVE-2024-0001"},
			{ID: "GHSA-xxxx"},
			{ID: "CVE-2024-0002"},
		},
	}
	cves := dockerSecCVEs(scan)
	if len(cves) != 2 {
		t.Fatalf("cves = %v", cves)
	}
	if cves[0] != "CVE-2024-0001" || cves[1] != "CVE-2024-0002" {
		t.Errorf("cves = %v", cves)
	}
}

// dockerSecTestScan stores an image scan with the given vulnerabilities.
func dockerSecTestScan(t *testing.T, db *store.DB, hostID int64, subject string, cves int) {
	t.Helper()
	scan := &store.DockerSecurityScan{
		HostID: hostID, Kind: "image", Subject: subject, ScannedAt: time.Now(), Scanner: "trivy", Status: "ok",
		VulnCounts: store.DockerVulnCounts{Total: cves},
	}
	for i := range cves {
		scan.Vulns = append(scan.Vulns, store.DockerImageVuln{
			ID: fmt.Sprintf("CVE-2024-%04d", i), Package: "lib", Installed: "1.0", Fixed: "1.1", Severity: "high",
		})
	}
	if err := db.SaveDockerSecurityScan(scan); err != nil {
		t.Fatal(err)
	}
}

// The exploit intelligence leaves the Pantau server, so it must never run inside the endpoint that answers
// the stored results: that one reads the database only. The intelligence has its own endpoint.
func TestDockerSecuritySummaryNeverLooksUpIntel(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	var hits int
	feeds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"vulnerabilities":[]}`))
	}))
	defer feeds.Close()
	srv.vuln = vulnintel.New(feeds.URL+"/kev", feeds.URL+"/epss")

	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	dockerSecTestScan(t, db, hostID, "nginx:1.25", 50)
	if err := db.SetSetting(vulnIntelSetting, "1"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/docker/security", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if hits != 0 {
		t.Errorf("the stored results must not look up the exploit intelligence, %d feed requests", hits)
	}
	var body struct {
		OK     bool                       `json:"ok"`
		Images []store.DockerSecurityScan `json:"images"`
		Intel  *securityIntel             `json:"intel"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Errorf("ok = %v", body.OK)
	}
	if body.Intel != nil {
		t.Errorf("the stored results must not carry the exploit intelligence: %+v", body.Intel)
	}
	if len(body.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(body.Images))
	}
	if body.Images[0].Subject != "nginx:1.25" {
		t.Errorf("subject = %q", body.Images[0].Subject)
	}
	if body.Images[0].ScannedAt.IsZero() {
		t.Error("the UI needs scanned_at of every image scan")
	}
	if body.Images[0].VulnCounts.Total != 50 {
		t.Errorf("vuln_counts = %+v", body.Images[0].VulnCounts)
	}
}

// The intelligence endpoint does the lookup, and answers null when the feature is off or there is no CVE.
func TestDockerSecurityIntelEndpoint(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	var hits int
	feeds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/kev" {
			_, _ = w.Write([]byte(`{"dateReleased":"2026-11-06","vulnerabilities":[{"cveID":"CVE-2024-0001","vulnerabilityName":"glibc","dateAdded":"2023-11-21","dueDate":"2023-12-12"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"cve":"CVE-2024-0001","epss":"0.6","percentile":"0.99","date":"2026-11-08"}]}`))
	}))
	defer feeds.Close()
	srv.vuln = vulnintel.New(feeds.URL+"/kev", feeds.URL+"/epss")

	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	dockerSecTestScan(t, db, hostID, "nginx:1.25", 50)

	ask := func() map[string]any {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/docker/security/intel", hostID), nil)
		authReq(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	// The feature is off: no lookup, no badges.
	if body := ask(); body["intel"] != nil || hits != 0 {
		t.Fatalf("intel with the feature off = %v with %d feed requests", body["intel"], hits)
	}
	if err := db.SetSetting(vulnIntelSetting, "1"); err != nil {
		t.Fatal(err)
	}
	body := ask()
	intel, _ := body["intel"].(map[string]any)
	if intel == nil {
		t.Fatalf("intel = %v", body["intel"])
	}
	cves, _ := intel["cves"].(map[string]any)
	cve, _ := cves["CVE-2024-0001"].(map[string]any)
	if intel["enabled"] != true || cve["kev"] == nil || cve["epss"] != 0.6 {
		t.Errorf("intel = %v", intel)
	}
	if hits == 0 {
		t.Error("the intelligence endpoint must look the CVEs up")
	}
}

// A scan of an image with thousands of vulnerabilities shows up in the stored results: the scan is
// parsed, stored and listed back, with its counts.
func TestDockerSecuritySummaryShowsManyVulnsScan(t *testing.T) {
	srv, db, ins, mock, authReq := setupSecurityTestServer(t)
	var b strings.Builder
	b.WriteString(`{"SchemaVersion":2,"ArtifactName":"nginx:1.25","Results":[`)
	for i := range 3000 {
		if i > 0 {
			b.WriteString(",")
		}
		sev := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}[i%4]
		fmt.Fprintf(&b, `{"Target":"nginx (debian 12.5)","Vulnerabilities":[{"VulnerabilityID":"CVE-2024-%04d","PkgName":"libpkg%d","InstalledVersion":"1.0","FixedVersion":"1.1","Title":"flaw %d","Severity":"%s"}]}`, i, i%97, i, sev)
	}
	b.WriteString(`]}`)
	mock.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "trivy") {
			return "===SCANNER:trivy===\n" + b.String(), "", 0, nil
		}
		return "", "", 0, nil
	}

	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/docker/security/scan-image", hostID),
		strings.NewReader(`{"image":"nginx:1.25"}`))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scan-image: code = %d, body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/docker/security", hostID), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("security: code = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Images []store.DockerSecurityScan `json:"images"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Images) != 1 {
		t.Fatalf("images = %d, want 1: a finished scan must show up", len(body.Images))
	}
	img := body.Images[0]
	if img.Subject != "nginx:1.25" || img.Status != "ok" || img.Scanner != "trivy" {
		t.Errorf("image = %+v", img)
	}
	if img.VulnCounts.Total != 3000 {
		t.Errorf("vuln_counts = %+v, want the whole report", img.VulnCounts)
	}
	if len(img.Vulns) != 200 {
		t.Errorf("stored vulns = %d, want the cap 200", len(img.Vulns))
	}
	if !img.Truncated {
		t.Error("a capped list must be marked as truncated")
	}
	if img.ScannedAt.IsZero() {
		t.Error("scanned_at must be there")
	}
	_ = ins
}

// The audit and the image scans are split by kind: the audit is one row, the images are the rest.
// Every image scan carries its counts and its scanned_at, even a failed one.
func TestDockerSecuritySummarySplitsTheKinds(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	audit := &store.DockerSecurityScan{
		HostID: hostID, Kind: "misconfig", ScannedAt: time.Now(), Status: "ok",
		Counts:   store.DockerSeverityCounts{Critical: 1, Total: 1},
		Findings: []store.DockerSecurityFinding{{Container: "web", RuleID: "privileged", Severity: "critical"}},
	}
	if err := db.SaveDockerSecurityScan(audit); err != nil {
		t.Fatal(err)
	}
	// A scan that failed: no vulnerability, but the counts are still there.
	failed := &store.DockerSecurityScan{
		HostID: hostID, Kind: "image", Subject: "broken:1", ScannedAt: time.Now(), Scanner: "trivy",
		Status: "error", Error: "scanner exited with code 2",
	}
	if err := db.SaveDockerSecurityScan(failed); err != nil {
		t.Fatal(err)
	}
	dockerSecTestScan(t, db, hostID, "registry.local/app:1.0", 3)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/docker/security", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Assessed bool                       `json:"assessed"`
		Audit    *store.DockerSecurityScan  `json:"audit"`
		Images   []store.DockerSecurityScan `json:"images"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Assessed || body.Audit == nil {
		t.Fatalf("the audit must be reported: %+v", body)
	}
	if body.Audit.Kind != "misconfig" {
		t.Errorf("the audit must be the misconfig row, got %q", body.Audit.Kind)
	}
	if len(body.Audit.Findings) != 1 {
		t.Errorf("audit findings = %d", len(body.Audit.Findings))
	}
	if len(body.Images) != 2 {
		t.Fatalf("images = %d, want 2 (the audit is not an image)", len(body.Images))
	}
	for _, img := range body.Images {
		if img.Kind != "image" {
			t.Errorf("kind = %q, want image", img.Kind)
		}
		if img.ScannedAt.IsZero() {
			t.Errorf("%s: scanned_at must be there", img.Subject)
		}
		if img.VulnCounts.Total == 0 && img.Status == "ok" {
			t.Errorf("%s: vuln_counts must be there", img.Subject)
		}
	}
}
