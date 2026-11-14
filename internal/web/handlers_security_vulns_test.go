package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"pantau/internal/osv"
	"pantau/internal/store"
)

// createVulnTestHost creates a host with a known OS description (CreateHost does not store os_info).
func createVulnTestHost(t *testing.T, db *store.DB, name, ip, osInfo string) int64 {
	t.Helper()
	hostID, err := db.CreateHost(&store.Host{Name: name, Host: ip, Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE hosts SET os_info = ? WHERE id = ?`, osInfo, hostID); err != nil {
		t.Fatal(err)
	}
	return hostID
}

// The known-vulnerability check sends package names and versions to a public service, so it stays
// off until the administrator opts in; while off nothing leaves the server.
func TestSecurityVulnsIsOptIn(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	var hits int
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer feed.Close()
	srv.osv = osv.New(feed.URL, feed.URL)

	hostID := createVulnTestHost(t, db, "debian", "10.0.0.11", "Debian GNU/Linux 12 (bookworm)")
	if err := db.ReplaceHostPackages(hostID, []*store.HostPackage{
		{HostID: hostID, Name: "libssl3", Version: "3.0.11-1", Source: "openssl", SourceVersion: "3.0.11-1"},
	}); err != nil {
		t.Fatal(err)
	}

	call := func() map[string]any {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security/vulns", hostID), nil)
		authReq(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	if body := call(); body["enabled"] != false {
		t.Fatalf("disabled vulns = %v, want enabled:false", body)
	}
	if hits != 0 {
		t.Fatalf("the feed was contacted %d times while the feature is off", hits)
	}

	if err := db.SetSetting(osvEnabledSetting, "1"); err != nil {
		t.Fatal(err)
	}
	body := call()
	if body["enabled"] != true {
		t.Fatalf("enabled vulns = %v", body)
	}
	if body["status"] != "assessed" {
		t.Errorf("status = %v, want assessed", body["status"])
	}
	if body["ecosystem"] != "Debian:12" {
		t.Errorf("ecosystem = %v, want Debian:12", body["ecosystem"])
	}
	if hits == 0 {
		t.Error("the feed was not contacted after the feature was enabled")
	}
}

// A host whose release has no OSV data is reported as not assessed, never as a clean list.
func TestSecurityVulnsUnsupportedRelease(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the feed must not be contacted for an unsupported release")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer feed.Close()
	srv.osv = osv.New(feed.URL, feed.URL)

	hostID := createVulnTestHost(t, db, "old", "10.0.0.12", "Debian GNU/Linux 6 (squeeze)")
	if err := db.ReplaceHostPackages(hostID, []*store.HostPackage{
		{HostID: hostID, Name: "libssl3", Version: "1.0.0", Source: "openssl", SourceVersion: "1.0.0"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(osvEnabledSetting, "1"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security/vulns", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "not_assessed" {
		t.Fatalf("status = %v, want not_assessed", body["status"])
	}
	notes, _ := body["notes"].([]any)
	if len(notes) == 0 {
		t.Error("expected a reason why the host is not assessed")
	}
}

// An empty inventory means the host was never scanned: not assessed, not "clean".
func TestSecurityVulnsEmptyInventory(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	hostID := createVulnTestHost(t, db, "empty", "10.0.0.13", "Alpine Linux v3.18")
	if err := db.SetSetting(osvEnabledSetting, "1"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security/vulns", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "not_assessed" {
		t.Fatalf("status = %v, want not_assessed", body["status"])
	}
	if body["error"] == nil || body["error"] == "" {
		t.Error("expected an error explaining that the inventory is empty")
	}
}

// A feed failure is reported as an error, never as an empty (read as clean) list.
func TestSecurityVulnsFeedFailure(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer feed.Close()
	srv.osv = osv.New(feed.URL, feed.URL)

	hostID := createVulnTestHost(t, db, "debian", "10.0.0.14", "Debian GNU/Linux 12 (bookworm)")
	if err := db.ReplaceHostPackages(hostID, []*store.HostPackage{
		{HostID: hostID, Name: "libssl3", Version: "3.0.11-1", Source: "openssl", SourceVersion: "3.0.11-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(osvEnabledSetting, "1"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security/vulns", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] == nil || body["error"] == "" {
		t.Fatalf("expected an error when the feed fails, got %v", body)
	}
	if vulns, _ := body["vulns"].([]any); len(vulns) != 0 {
		t.Errorf("expected no findings on a feed failure, got %v", vulns)
	}
}

// The findings of the feed reach the API with the fixed version and the CVEs.
func TestSecurityVulnsReportsFindings(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/querybatch" {
			_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"DEBIAN-CVE-2023-5678"}]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"DEBIAN-CVE-2023-5678","summary":"openssl - security update","upstream":["CVE-2023-5678"],"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:L"}],"affected":[{"package":{"name":"openssl","ecosystem":"Debian:12"},"ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"},{"fixed":"3.0.13-1~deb12u1"}]}]}]}`))
	}))
	defer feed.Close()
	srv.osv = osv.New(feed.URL, feed.URL)

	hostID := createVulnTestHost(t, db, "debian", "10.0.0.15", "Debian GNU/Linux 12 (bookworm)")
	if err := db.ReplaceHostPackages(hostID, []*store.HostPackage{
		{HostID: hostID, Name: "libssl3", Version: "3.0.11-1", Source: "openssl", SourceVersion: "3.0.11-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(osvEnabledSetting, "1"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security/vulns", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var body struct {
		Status          string `json:"status"`
		Source          string `json:"source"`
		CheckedPackages int    `json:"checked_packages"`
		Vulns           []struct {
			Package          string   `json:"package"`
			InstalledVersion string   `json:"installed_version"`
			FixedVersion     string   `json:"fixed_version"`
			CVEs             []string `json:"cves"`
			Severity         string   `json:"severity"`
		} `json:"vulns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "assessed" || body.Source != "osv" || body.CheckedPackages != 1 {
		t.Fatalf("unexpected response: %+v", body)
	}
	if len(body.Vulns) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(body.Vulns))
	}
	v := body.Vulns[0]
	if v.Package != "openssl" || v.InstalledVersion != "3.0.11-1" || v.FixedVersion != "3.0.13-1~deb12u1" {
		t.Errorf("unexpected finding: %+v", v)
	}
	if len(v.CVEs) != 1 || v.CVEs[0] != "CVE-2023-5678" {
		t.Errorf("unexpected CVEs: %v", v.CVEs)
	}
}
